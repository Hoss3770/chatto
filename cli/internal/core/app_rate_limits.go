package core

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chattocorp/chatto/pkg/jetstreamutil"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	runtimestatev1 "github.com/chattocorp/chatto/cli/internal/pb/chatto/core/runtime_state/v1"
)

const appRateLimitKeyPrefix = "app_rate."

// AppRateLimitError reports the fixed-window retry time for one rejected call.
type AppRateLimitError struct {
	RetryAfter time.Duration
}

func (e *AppRateLimitError) Error() string { return "app method rate limit exceeded" }

func (c *ChattoCore) appRateLimitKey(appID, method string, window time.Time) string {
	scope := appID
	if scope == "" {
		scope = "anonymous"
	}
	methodHash := c.runtimeTokenHash("app_rate_method", method)
	return fmt.Sprintf("%s%s.%s.%d", appRateLimitKeyPrefix, scope, methodHash[:24], window.Unix())
}

// ConsumeAppRateLimit atomically consumes one shared fixed-window method unit.
// A storage failure is returned to the caller; it never falls back to a local
// counter.
func (c *ChattoCore) ConsumeAppRateLimit(ctx context.Context, appID, method string, limit uint32, now time.Time) error {
	if limit == 0 {
		return &AppRateLimitError{RetryAfter: time.Minute}
	}
	window := now.UTC().Truncate(time.Minute)
	windowEnd := window.Add(time.Minute)
	expiresAt := windowEnd.Add(time.Minute)
	key := c.appRateLimitKey(appID, method, window)
	for range 8 {
		entry, err := c.storage.runtimeStateKV.Get(ctx, key)
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			record := &runtimestatev1.AppRateLimitBucket{
				WindowStartedAt: timestamppb.New(window), ExpiresAt: timestamppb.New(expiresAt), Count: 1,
			}
			data, marshalErr := proto.Marshal(record)
			if marshalErr != nil {
				return marshalErr
			}
			if _, createErr := c.storage.runtimeStateKV.Create(ctx, key, data, jetstream.KeyTTL(expiresAt.Sub(now))); createErr != nil {
				if jetstreamutil.IsSequenceConflict(createErr) {
					continue
				}
				return fmt.Errorf("create app rate-limit bucket: %w", createErr)
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("read app rate-limit bucket: %w", err)
		}
		var record runtimestatev1.AppRateLimitBucket
		if err := proto.Unmarshal(entry.Value(), &record); err != nil {
			return fmt.Errorf("decode app rate-limit bucket: %w", err)
		}
		if record.GetWindowStartedAt() == nil || record.GetExpiresAt() == nil || !record.GetWindowStartedAt().AsTime().Equal(window) {
			return fmt.Errorf("invalid app rate-limit bucket")
		}
		if record.GetCount() >= limit {
			retryAfter := max(windowEnd.Sub(now), 0)
			return &AppRateLimitError{RetryAfter: retryAfter}
		}
		record.Count++
		data, err := proto.Marshal(&record)
		if err != nil {
			return err
		}
		if _, err := c.updateRuntimeStateUntil(ctx, key, data, entry.Revision(), expiresAt, now); err != nil {
			if jetstreamutil.IsSequenceConflict(err) || errors.Is(err, jetstream.ErrKeyNotFound) {
				continue
			}
			return fmt.Errorf("update app rate-limit bucket: %w", err)
		}
		return nil
	}
	return fmt.Errorf("app rate-limit CAS retry exhausted")
}
