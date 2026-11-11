package protocompat

import (
	"testing"

	evtv1 "github.com/chattocorp/chatto/cli/internal/pb/chatto/core/evt/v1"
	runtimestatev1 "github.com/chattocorp/chatto/cli/internal/pb/chatto/core/runtime_state/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestAppEventDurableFieldNumbers(t *testing.T) {
	t.Parallel()
	fields := (&evtv1.Event{}).ProtoReflect().Descriptor().Fields()
	want := map[protoreflect.Name]protoreflect.FieldNumber{
		"app_created":                 960,
		"app_manifest_updated":        961,
		"app_installed":               962,
		"app_bot_token_rotated":       963,
		"app_bot_token_revoked":       964,
		"app_uninstalled":             965,
		"app_credentials_regenerated": 966,
		"app_deleted":                 967,
	}
	for name, number := range want {
		field := fields.ByName(name)
		if field == nil {
			t.Errorf("Event.%s is missing", name)
			continue
		}
		if field.Number() != number {
			t.Errorf("Event.%s number = %d, want %d", name, field.Number(), number)
		}
		if oneof := field.ContainingOneof(); oneof == nil || oneof.Name() != "event" {
			t.Errorf("Event.%s is not in the durable event oneof", name)
		}
	}
}

func TestAppRuntimeStateKeepsExplicitExpiryAndClaimState(t *testing.T) {
	t.Parallel()
	code := (&runtimestatev1.AppOAuthAuthorizationCode{}).ProtoReflect().Descriptor()
	for _, name := range []protoreflect.Name{"created_at", "expires_at", "state", "request_fingerprint", "encrypted_response", "response_nonce"} {
		if code.Fields().ByName(name) == nil {
			t.Errorf("AppOAuthAuthorizationCode.%s is missing", name)
		}
	}
	bucket := (&runtimestatev1.AppRateLimitBucket{}).ProtoReflect().Descriptor()
	for _, name := range []protoreflect.Name{"window_started_at", "expires_at", "count"} {
		if bucket.Fields().ByName(name) == nil {
			t.Errorf("AppRateLimitBucket.%s is missing", name)
		}
	}
}
