package core

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/chattocorp/chatto/cli/internal/encryption"
	"github.com/chattocorp/chatto/cli/internal/kms"
	evtv1 "github.com/chattocorp/chatto/cli/internal/pb/chatto/core/evt/v1"
	runtimestatev1 "github.com/chattocorp/chatto/cli/internal/pb/chatto/core/runtime_state/v1"
	"github.com/chattocorp/chatto/pkg/jetstreamutil"
)

const appContentKeyPrefix = "app_dek."

const (
	appClientSecretPurpose = "app_client_secret"
	appBotTokenPurpose     = "app_bot_token"
	appTokenPurpose        = "app_level_token"
)

// appContentKeyResolver unwraps immutable app data-encryption keys and caches
// them only for this process. RUNTIME_STATE and the KMS remain authoritative.
type appContentKeyResolver struct {
	kv         *jetstreamutil.KeyValue
	keyWrapper kms.KeyWrapper
	mu         sync.Mutex
	keys       map[string][]byte
}

func newAppContentKeyResolver(kv *jetstreamutil.KeyValue, keyWrapper kms.KeyWrapper) *appContentKeyResolver {
	return &appContentKeyResolver{kv: kv, keyWrapper: keyWrapper, keys: make(map[string][]byte)}
}

func appContentKeyRef(appID string) string { return appContentKeyPrefix + appID }

func appContentKeyAAD(appID string) []byte {
	return []byte("chatto:app-content-key:v1\x00app_id=" + appID)
}

func appValueAAD(appID, eventID, field string) []byte {
	return []byte("chatto:app-value:v1\x00app_id=" + appID + "\x00event_id=" + eventID + "\x00field=" + field)
}

func (r *appContentKeyResolver) resolve(ctx context.Context, appID, contentKeyRef string) ([]byte, error) {
	if appID == "" || contentKeyRef != appContentKeyRef(appID) {
		return nil, fmt.Errorf("invalid app content key reference")
	}
	r.mu.Lock()
	if key := r.keys[contentKeyRef]; len(key) > 0 {
		clone := bytes.Clone(key)
		r.mu.Unlock()
		return clone, nil
	}
	r.mu.Unlock()

	entry, err := r.kv.GetAnyReplica(ctx, contentKeyRef)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		entry, err = r.kv.Get(ctx, contentKeyRef)
	}
	if err != nil {
		return nil, fmt.Errorf("load app content key: %w", err)
	}
	var stored runtimestatev1.AppDataEncryptionKey
	if err := proto.Unmarshal(entry.Value(), &stored); err != nil {
		return nil, fmt.Errorf("decode app content key: %w", err)
	}
	if len(stored.GetEncryptedContentKey()) == 0 || len(stored.GetContentKeyNonce()) == 0 || stored.GetWrappingKeyRef() == "" {
		return nil, fmt.Errorf("invalid app content key")
	}
	key, err := r.keyWrapper.UnwrapContentKey(ctx, stored.GetWrappingKeyRef(), kms.WrappedContentKey{
		EncryptedContentKey: stored.GetEncryptedContentKey(),
		Nonce:               stored.GetContentKeyNonce(),
		Algorithm:           stored.GetWrappingAlgorithm(),
		Metadata:            stored.GetWrappingMetadata(),
	}, appContentKeyAAD(appID))
	if err != nil {
		return nil, fmt.Errorf("unwrap app content key: %w", err)
	}
	r.mu.Lock()
	if current := r.keys[contentKeyRef]; len(current) > 0 {
		key = current
	} else {
		r.keys[contentKeyRef] = bytes.Clone(key)
	}
	clone := bytes.Clone(key)
	r.mu.Unlock()
	return clone, nil
}

func (r *appContentKeyResolver) decrypt(ctx context.Context, appID, eventID, field string, value *evtv1.EncryptedAppValue) ([]byte, error) {
	if value == nil || len(value.GetCiphertext()) == 0 || len(value.GetNonce()) == 0 {
		return nil, fmt.Errorf("encrypted app value is missing")
	}
	key, err := r.resolve(ctx, appID, value.GetContentKeyRef())
	if err != nil {
		return nil, err
	}
	plaintext, err := encryption.DecryptXChaCha20Poly1305(key, value.GetCiphertext(), value.GetNonce(), appValueAAD(appID, eventID, field))
	if err != nil {
		return nil, fmt.Errorf("decrypt app %s: %w", field, err)
	}
	return plaintext, nil
}

func encryptAppValue(contentKey []byte, appID, eventID, field, contentKeyRef string, plaintext []byte) (*evtv1.EncryptedAppValue, error) {
	sealed, err := encryption.EncryptXChaCha20Poly1305(contentKey, plaintext, appValueAAD(appID, eventID, field))
	if err != nil {
		return nil, err
	}
	return &evtv1.EncryptedAppValue{Ciphertext: sealed.Ciphertext, Nonce: sealed.Nonce, ContentKeyRef: contentKeyRef}, nil
}

// createAppContentKey creates the app KEK and immutable wrapped DEK. The caller
// must invoke cleanup unless the app creation event commits.
func (c *ChattoCore) createAppContentKey(ctx context.Context, appID string) ([]byte, string, func(), error) {
	wrappingKeyRef, err := c.encryption.keyWrapper.CreateKey(ctx, appID)
	if err != nil {
		return nil, "", nil, fmt.Errorf("create app wrapping key: %w", err)
	}
	storedAppKey := false
	cleanup := func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if storedAppKey {
			if err := c.deleteRuntimeStateKey(cleanupCtx, appContentKeyRef(appID)); err != nil {
				c.logger.Warn("failed to clean up app content key", "app_id", appID, "error", err)
			}
		}
		if err := c.encryption.keyWrapper.ShredKey(cleanupCtx, wrappingKeyRef); err != nil {
			c.logger.Warn("failed to clean up app wrapping key", "app_id", appID, "error", err)
		}
	}
	contentKey, err := encryption.GenerateKey()
	if err != nil {
		cleanup()
		return nil, "", nil, err
	}
	wrapped, err := c.encryption.keyWrapper.WrapContentKey(ctx, wrappingKeyRef, contentKey, appContentKeyAAD(appID))
	if err != nil {
		cleanup()
		return nil, "", nil, fmt.Errorf("wrap app content key: %w", err)
	}
	stored, err := proto.Marshal(&runtimestatev1.AppDataEncryptionKey{
		EncryptedContentKey: wrapped.EncryptedContentKey,
		ContentKeyNonce:     wrapped.Nonce,
		WrappingKeyRef:      wrappingKeyRef,
		WrappingAlgorithm:   wrapped.Algorithm,
		WrappingMetadata:    wrapped.Metadata,
		CreatedAt:           timestamppb.Now(),
	})
	if err != nil {
		cleanup()
		return nil, "", nil, fmt.Errorf("encode app content key: %w", err)
	}
	ref := appContentKeyRef(appID)
	if _, err := c.storage.runtimeStateKV.Create(ctx, ref, stored); err != nil {
		cleanup()
		return nil, "", nil, fmt.Errorf("store app content key: %w", err)
	}
	storedAppKey = true
	return contentKey, ref, cleanup, nil
}

func (c *ChattoCore) appCredentialVerifier(purpose, raw string) []byte {
	mac := hmac.New(sha256.New, []byte(c.config.SecretKey))
	_, _ = mac.Write([]byte(purpose))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(raw))
	return mac.Sum(nil)
}

func newAppClientSecret() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate app client secret: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func newAppSigningSecret() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate app signing secret: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func newAppBotToken(workspaceID, appID string) (string, error) {
	secret := make([]byte, 24)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("generate app bot token: %w", err)
	}
	return "xoxb-" + workspaceID + "-" + appID + "-" + base64.RawURLEncoding.EncodeToString(secret), nil
}

func newAppLevelToken(appID string) (string, error) {
	secret := make([]byte, 24)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("generate app-level token: %w", err)
	}
	return "xapp-1-" + appID + "-" + base64.RawURLEncoding.EncodeToString(secret), nil
}
