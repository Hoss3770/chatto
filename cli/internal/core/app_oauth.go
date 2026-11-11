package core

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/chattocorp/chatto/cli/internal/encryption"
	"github.com/chattocorp/chatto/cli/internal/evtstream"
	evtv1 "github.com/chattocorp/chatto/cli/internal/pb/chatto/core/evt/v1"
	runtimestatev1 "github.com/chattocorp/chatto/cli/internal/pb/chatto/core/runtime_state/v1"
	"github.com/chattocorp/chatto/pkg/events"
	"github.com/chattocorp/chatto/pkg/jetstreamutil"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	AppOAuthCodeTTL       = 10 * time.Minute
	appOAuthCodeKeyPrefix = "app_oauth_code."
)

var (
	ErrAppOAuthCodeInvalid = errors.New("app OAuth authorization code is invalid")
	ErrAppOAuthGrant       = errors.New("app OAuth grant does not match")
)

// AppOAuthExchange is the target OAuth installation result.
type AppOAuthExchange struct {
	AccessToken   string   `json:"access_token"`
	TokenType     string   `json:"token_type"`
	Scope         string   `json:"scope"`
	BotUserID     string   `json:"bot_user_id"`
	AppID         string   `json:"app_id"`
	WorkspaceID   string   `json:"team_id"`
	WorkspaceName string   `json:"team_name"`
	BotScopes     []string `json:"-"`
}

func (c *ChattoCore) appOAuthCodeKey(code string) string {
	return c.runtimeTokenKey(appOAuthCodeKeyPrefix, code)
}

func appOAuthValueAAD(appID, key, field string) []byte {
	return []byte("chatto:app-oauth-value:v1\x00app_id=" + appID + "\x00key=" + key + "\x00field=" + field)
}

func appOAuthRequestFingerprint(secret, appID, redirectURI string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("app_oauth_exchange"))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(appID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(redirectURI))
	return mac.Sum(nil)
}

// CreateAppOAuthCode stores a single-use browser installation grant.
func (c *ChattoCore) CreateAppOAuthCode(ctx context.Context, userID, appID, redirectURI string, botScopes, userScopes []string) (string, error) {
	if err := c.requireAppManager(ctx, userID); err != nil {
		return "", err
	}
	state, err := c.getAppCurrent(ctx, appID, false)
	if err != nil {
		return "", err
	}
	botScopes = sortedUniqueStrings(botScopes)
	userScopes = sortedUniqueStrings(userScopes)
	if len(botScopes) == 0 {
		botScopes = append([]string(nil), state.RequestedBotScopes...)
	}
	if len(userScopes) > 0 || !scopeSubset(botScopes, state.RequestedBotScopes) {
		return "", ErrAppOAuthGrant
	}
	if strings.TrimSpace(redirectURI) == "" {
		return "", ErrAppOAuthGrant
	}
	now := time.Now().UTC()
	record := &runtimestatev1.AppOAuthAuthorizationCode{
		AppId: appID, UserId: userID, RedirectUri: redirectURI,
		BotScopes: botScopes, UserScopes: userScopes,
		CreatedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(AppOAuthCodeTTL)),
		State: runtimestatev1.AppOauthAuthorizationCodeState_APP_OAUTH_AUTHORIZATION_CODE_STATE_PENDING,
	}
	data, err := proto.Marshal(record)
	if err != nil {
		return "", err
	}
	for range 5 {
		code := NewAuthCode()
		if _, err := c.storage.runtimeStateKV.Create(ctx, c.appOAuthCodeKey(code), data, jetstream.KeyTTL(AppOAuthCodeTTL)); err != nil {
			if jetstreamutil.IsSequenceConflict(err) {
				continue
			}
			return "", fmt.Errorf("store app OAuth code: %w", err)
		}
		return code, nil
	}
	return "", fmt.Errorf("allocate app OAuth code")
}

func scopeSubset(requested, allowed []string) bool {
	for _, scope := range requested {
		if !slices.Contains(allowed, scope) {
			return false
		}
	}
	return true
}

// ExchangeAppOAuthCode consumes or resumes one exact authorization-code
// exchange. The proposed bot token is encrypted before the app fact commits, so
// a process failure can resume without issuing another token.
func (c *ChattoCore) ExchangeAppOAuthCode(ctx context.Context, code, clientID, clientSecret, redirectURI, workspaceName string) (AppOAuthExchange, error) {
	if _, err := c.ValidateAppClientSecret(ctx, clientID, clientSecret); err != nil {
		return AppOAuthExchange{}, err
	}
	key := c.appOAuthCodeKey(code)
	fingerprint := appOAuthRequestFingerprint(c.config.SecretKey, clientID, redirectURI)
	for range 12 {
		entry, err := c.storage.runtimeStateKV.Get(ctx, key)
		if err != nil {
			if errors.Is(err, jetstream.ErrKeyNotFound) {
				return AppOAuthExchange{}, ErrAppOAuthCodeInvalid
			}
			return AppOAuthExchange{}, fmt.Errorf("read app OAuth code: %w", err)
		}
		var record runtimestatev1.AppOAuthAuthorizationCode
		if err := proto.Unmarshal(entry.Value(), &record); err != nil {
			return AppOAuthExchange{}, fmt.Errorf("decode app OAuth code: %w", err)
		}
		now := time.Now().UTC()
		if record.GetExpiresAt() == nil || !now.Before(record.GetExpiresAt().AsTime()) || record.GetAppId() != clientID {
			return AppOAuthExchange{}, ErrAppOAuthCodeInvalid
		}
		if record.GetRedirectUri() != redirectURI {
			return AppOAuthExchange{}, ErrAppOAuthGrant
		}
		contentKey, err := c.appContentKeys.resolve(ctx, clientID, appContentKeyRef(clientID))
		if err != nil {
			return AppOAuthExchange{}, err
		}
		switch record.GetState() {
		case runtimestatev1.AppOauthAuthorizationCodeState_APP_OAUTH_AUTHORIZATION_CODE_STATE_PENDING:
			workspaceID, err := c.AppWorkspaceID(ctx)
			if err != nil {
				return AppOAuthExchange{}, err
			}
			token, err := newAppBotToken(workspaceID, clientID)
			if err != nil {
				return AppOAuthExchange{}, err
			}
			sealed, err := encryption.EncryptXChaCha20Poly1305(contentKey, []byte(token), appOAuthValueAAD(clientID, key, "bot_token"))
			if err != nil {
				return AppOAuthExchange{}, err
			}
			record.State = runtimestatev1.AppOauthAuthorizationCodeState_APP_OAUTH_AUTHORIZATION_CODE_STATE_CLAIMED
			record.RequestFingerprint = fingerprint
			record.EncryptedBotToken = sealed.Ciphertext
			record.BotTokenNonce = sealed.Nonce
			record.BotTokenVerifier = c.appCredentialVerifier(appBotTokenPurpose, token)
			data, err := proto.Marshal(&record)
			if err != nil {
				return AppOAuthExchange{}, err
			}
			if _, err := c.updateRuntimeStateUntil(ctx, key, data, entry.Revision(), record.GetExpiresAt().AsTime(), now); err != nil {
				if jetstreamutil.IsSequenceConflict(err) || errors.Is(err, jetstream.ErrKeyNotFound) {
					continue
				}
				return AppOAuthExchange{}, err
			}
			continue
		case runtimestatev1.AppOauthAuthorizationCodeState_APP_OAUTH_AUTHORIZATION_CODE_STATE_CLAIMED:
			if !hmac.Equal(record.GetRequestFingerprint(), fingerprint) {
				return AppOAuthExchange{}, ErrAppOAuthGrant
			}
			tokenBytes, err := encryption.DecryptXChaCha20Poly1305(contentKey, record.GetEncryptedBotToken(), record.GetBotTokenNonce(), appOAuthValueAAD(clientID, key, "bot_token"))
			if err != nil {
				return AppOAuthExchange{}, err
			}
			token := string(tokenBytes)
			state, err := c.activateAppBotTokenForOAuth(ctx, record.GetUserId(), clientID, record.GetBotScopes(), record.GetUserScopes(), token)
			if err != nil {
				return AppOAuthExchange{}, err
			}
			workspaceID, err := c.AppWorkspaceID(ctx)
			if err != nil {
				return AppOAuthExchange{}, err
			}
			response := AppOAuthExchange{
				AccessToken: token, TokenType: "bot", Scope: strings.Join(record.GetBotScopes(), ","),
				BotUserID: state.BotUserID, AppID: clientID, WorkspaceID: workspaceID,
				WorkspaceName: workspaceName, BotScopes: append([]string(nil), record.GetBotScopes()...),
			}
			responseData, err := json.Marshal(response)
			if err != nil {
				return AppOAuthExchange{}, err
			}
			sealed, err := encryption.EncryptXChaCha20Poly1305(contentKey, responseData, appOAuthValueAAD(clientID, key, "response"))
			if err != nil {
				return AppOAuthExchange{}, err
			}
			record.State = runtimestatev1.AppOauthAuthorizationCodeState_APP_OAUTH_AUTHORIZATION_CODE_STATE_COMPLETED
			record.EncryptedResponse = sealed.Ciphertext
			record.ResponseNonce = sealed.Nonce
			data, err := proto.Marshal(&record)
			if err != nil {
				return AppOAuthExchange{}, err
			}
			if _, err := c.updateRuntimeStateUntil(ctx, key, data, entry.Revision(), record.GetExpiresAt().AsTime(), now); err != nil {
				if jetstreamutil.IsSequenceConflict(err) || errors.Is(err, jetstream.ErrKeyNotFound) {
					continue
				}
				return AppOAuthExchange{}, err
			}
			return response, nil
		case runtimestatev1.AppOauthAuthorizationCodeState_APP_OAUTH_AUTHORIZATION_CODE_STATE_COMPLETED:
			if !hmac.Equal(record.GetRequestFingerprint(), fingerprint) {
				return AppOAuthExchange{}, ErrAppOAuthGrant
			}
			responseData, err := encryption.DecryptXChaCha20Poly1305(contentKey, record.GetEncryptedResponse(), record.GetResponseNonce(), appOAuthValueAAD(clientID, key, "response"))
			if err != nil {
				return AppOAuthExchange{}, err
			}
			var response AppOAuthExchange
			if err := json.Unmarshal(responseData, &response); err != nil {
				return AppOAuthExchange{}, fmt.Errorf("decode app OAuth response: %w", err)
			}
			response.BotScopes = append([]string(nil), record.GetBotScopes()...)
			return response, nil
		default:
			return AppOAuthExchange{}, ErrAppOAuthCodeInvalid
		}
	}
	return AppOAuthExchange{}, fmt.Errorf("app OAuth exchange CAS retry exhausted")
}

func appOAuthActivationMatches(state AppState, verifier []byte, botScopes, userScopes []string) bool {
	return state.Lifecycle == AppLifecycleInstalled &&
		hmac.Equal(state.botTokenVerifier, verifier) &&
		slices.Equal(state.GrantedBotScopes, botScopes) &&
		slices.Equal(state.GrantedUserScopes, userScopes)
}

func (c *ChattoCore) activateAppBotTokenForOAuth(ctx context.Context, actorID, appID string, botScopes, userScopes []string, token string) (AppState, error) {
	if err := c.requireAppManager(ctx, actorID); err != nil {
		return AppState{}, err
	}
	botScopes = sortedUniqueStrings(botScopes)
	userScopes = sortedUniqueStrings(userScopes)
	verifier := c.appCredentialVerifier(appBotTokenPurpose, token)
	state, err := c.getAppCurrent(ctx, appID, false)
	if err != nil {
		return AppState{}, err
	}
	if appOAuthActivationMatches(state, verifier, botScopes, userScopes) {
		return state, nil
	}
	permissionState := state
	permissionState.RequestedBotScopes = botScopes
	if err := c.applyAppBotPermissions(ctx, actorID, permissionState); err != nil {
		return AppState{}, err
	}
	if state.Lifecycle == AppLifecycleInstalled {
		return c.RotateAppBotTokenForOAuth(ctx, appID, botScopes, userScopes, token, actorID)
	}
	for range maxAppMutationAttempts {
		state, appAggregate, appTail, err := c.getAppAtTail(ctx, appID, false)
		if err != nil {
			return AppState{}, err
		}
		if appOAuthActivationMatches(state, verifier, botScopes, userScopes) {
			return state, nil
		}
		if state.Lifecycle == AppLifecycleInstalled {
			return c.RotateAppBotTokenForOAuth(ctx, appID, botScopes, userScopes, token, actorID)
		}
		userAggregate := evtstream.UserAggregate(state.BotUserID)
		userTail, err := c.EventPublisher.LastSubjectSeq(ctx, userAggregate.AllEventsFilter())
		if err != nil {
			return AppState{}, err
		}
		installed := newEvent(actorID, &evtv1.Event{Event: &evtv1.Event_AppInstalled{AppInstalled: &evtv1.AppInstalledEvent{
			AppId: appID, InstallGeneration: state.InstallGeneration + 1, BotTokenVerifier: verifier,
			GrantedBotScopes: botScopes, GrantedUserScopes: userScopes,
		}}})
		reactivated := newEvent(actorID, &evtv1.Event{Event: &evtv1.Event_UserAccountDeactivationChanged{
			UserAccountDeactivationChanged: &evtv1.UserAccountDeactivationChangedEvent{UserId: state.BotUserID, Deactivated: false},
		}})
		entries := []evtstream.BatchEntry{
			{Subject: appAggregate.SubjectFor(installed), Event: installed, Expect: events.ExpectFilterSeq(appAggregate.AllEventsFilter(), appTail)},
			{Subject: userAggregate.SubjectFor(reactivated), Event: reactivated, Expect: events.ExpectFilterSeq(userAggregate.AllEventsFilter(), userTail)},
		}
		sequences, err := c.EventPublisher.AppendBatch(ctx, entries)
		if errors.Is(err, events.ErrConflict) {
			continue
		}
		if err != nil {
			return AppState{}, err
		}
		if err := c.appModel.projection.Projector().WaitFor(ctx, events.SubjectPosition(entries[0].Subject, sequences[0])); err != nil {
			return AppState{}, err
		}
		if err := c.userModel.waitForUsers(ctx, events.SubjectPosition(entries[1].Subject, sequences[1])); err != nil {
			return AppState{}, err
		}
		state, _ = c.appModel.projection.Projection().get(appID)
		return state, nil
	}
	return AppState{}, errAppMutationExhausted
}
