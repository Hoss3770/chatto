package evtstream

import (
	"testing"

	evtv1 "github.com/chattocorp/chatto/cli/internal/pb/chatto/core/evt/v1"
)

func TestAppAggregateSubjects(t *testing.T) {
	t.Parallel()
	aggregate := AppAggregate("A12345678901234")
	if got, want := aggregate.AllEventsFilter(), "evt.app.A12345678901234.>"; got != want {
		t.Fatalf("AllEventsFilter = %q, want %q", got, want)
	}
	if got, want := AppSubjectFilter(), "evt.app.>"; got != want {
		t.Fatalf("AppSubjectFilter = %q, want %q", got, want)
	}
	event := &evtv1.Event{Event: &evtv1.Event_AppInstalled{AppInstalled: &evtv1.AppInstalledEvent{AppId: "A12345678901234"}}}
	if got, want := aggregate.SubjectFor(event), "evt.app.A12345678901234.installed"; got != want {
		t.Fatalf("SubjectFor = %q, want %q", got, want)
	}
}

func TestAppEventTypeTokens(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		event *evtv1.Event
		want  string
	}{
		{name: "created", event: &evtv1.Event{Event: &evtv1.Event_AppCreated{AppCreated: &evtv1.AppCreatedEvent{}}}, want: EventAppCreated},
		{name: "manifest", event: &evtv1.Event{Event: &evtv1.Event_AppManifestUpdated{AppManifestUpdated: &evtv1.AppManifestUpdatedEvent{}}}, want: EventAppManifestUpdated},
		{name: "installed", event: &evtv1.Event{Event: &evtv1.Event_AppInstalled{AppInstalled: &evtv1.AppInstalledEvent{}}}, want: EventAppInstalled},
		{name: "rotated", event: &evtv1.Event{Event: &evtv1.Event_AppBotTokenRotated{AppBotTokenRotated: &evtv1.AppBotTokenRotatedEvent{}}}, want: EventAppBotTokenRotated},
		{name: "revoked", event: &evtv1.Event{Event: &evtv1.Event_AppBotTokenRevoked{AppBotTokenRevoked: &evtv1.AppBotTokenRevokedEvent{}}}, want: EventAppBotTokenRevoked},
		{name: "uninstalled", event: &evtv1.Event{Event: &evtv1.Event_AppUninstalled{AppUninstalled: &evtv1.AppUninstalledEvent{}}}, want: EventAppUninstalled},
		{name: "credentials", event: &evtv1.Event{Event: &evtv1.Event_AppCredentialsRegenerated{AppCredentialsRegenerated: &evtv1.AppCredentialsRegeneratedEvent{}}}, want: EventAppCredentialsRegenerated},
		{name: "deleted", event: &evtv1.Event{Event: &evtv1.Event_AppDeleted{AppDeleted: &evtv1.AppDeletedEvent{}}}, want: EventAppDeleted},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := EventTypeOf(test.event); got != test.want {
				t.Fatalf("EventTypeOf = %q, want %q", got, test.want)
			}
		})
	}
}
