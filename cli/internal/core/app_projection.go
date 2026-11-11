package core

import (
	"bytes"
	"context"
	"sort"
	"sync"
	"time"

	"github.com/chattocorp/chatto/cli/internal/evtstream"
	evtv1 "github.com/chattocorp/chatto/cli/internal/pb/chatto/core/evt/v1"
	"github.com/chattocorp/chatto/pkg/events"
)

// AppLifecycle is the durable state of one app definition and installation.
type AppLifecycle uint8

const (
	AppLifecycleUnspecified AppLifecycle = iota
	AppLifecycleNotInstalled
	AppLifecycleInstalled
	AppLifecycleUninstalled
	AppLifecycleDeleted
)

// AppState is the safe management view of one durable app. Secret verifiers and
// decrypted signing material stay private to core authentication and delivery.
type AppState struct {
	ID                   string
	OwnerUserID          string
	BotUserID            string
	Name                 string
	Description          string
	ManifestJSON         string
	ManifestRevision     uint64
	InstallGeneration    uint64
	Lifecycle            AppLifecycle
	RequestedBotScopes   []string
	RequestedUserScopes  []string
	GrantedBotScopes     []string
	GrantedUserScopes    []string
	RedirectOrigins      []string
	CreatedAt            time.Time
	UpdatedAt            time.Time
	firstSequence        uint64
	clientSecretVerifier []byte
	botTokenVerifier     []byte
	appTokenVerifier     []byte
	signingSecret        string
}

// AppProjection rebuilds app definitions and credential generations from EVT.
type AppProjection struct {
	events.MemoryProjection
	resolver *appContentKeyResolver
	apps     map[string]*AppState

	nextCredentialWatcherID uint64
	credentialWatchers      map[string]map[uint64]chan struct{}
}

func NewAppProjection(resolver *appContentKeyResolver) *AppProjection {
	return &AppProjection{
		resolver:           resolver,
		apps:               make(map[string]*AppState),
		credentialWatchers: make(map[string]map[uint64]chan struct{}),
	}
}

func (p *AppProjection) Subjects() []string { return []string{evtstream.AppSubjectFilter()} }

func (p *AppProjection) Apply(event *evtv1.Event, sequence uint64) error {
	if event == nil {
		return nil
	}
	p.Lock()
	defer p.Unlock()
	createdAt := event.GetCreatedAt().AsTime()
	switch value := event.GetEvent().(type) {
	case *evtv1.Event_AppCreated:
		payload := value.AppCreated
		if payload == nil || payload.GetAppId() == "" || p.apps[payload.GetAppId()] != nil {
			return nil
		}
		manifest, err := p.resolver.decrypt(context.Background(), payload.GetAppId(), event.GetId(), "manifest", payload.GetManifest())
		if err != nil {
			return err
		}
		signingSecret, err := p.resolver.decrypt(context.Background(), payload.GetAppId(), event.GetId(), "signing_secret", payload.GetSigningSecret())
		if err != nil {
			return err
		}
		p.apps[payload.GetAppId()] = &AppState{
			ID: payload.GetAppId(), OwnerUserID: payload.GetOwnerUserId(), BotUserID: payload.GetBotUserId(),
			Name: payload.GetName(), Description: payload.GetDescription(), ManifestJSON: string(manifest),
			ManifestRevision: payload.GetManifestRevision(), Lifecycle: AppLifecycleNotInstalled,
			RequestedBotScopes:  sortedUniqueStrings(payload.GetRequestedBotScopes()),
			RequestedUserScopes: sortedUniqueStrings(payload.GetRequestedUserScopes()),
			RedirectOrigins:     sortedUniqueStrings(payload.GetRedirectOrigins()),
			CreatedAt:           createdAt, UpdatedAt: createdAt, firstSequence: sequence,
			clientSecretVerifier: bytes.Clone(payload.GetClientSecretVerifier()), signingSecret: string(signingSecret),
		}
	case *evtv1.Event_AppManifestUpdated:
		payload := value.AppManifestUpdated
		state := p.apps[payload.GetAppId()]
		if state == nil || state.Lifecycle == AppLifecycleDeleted || payload.GetManifestRevision() <= state.ManifestRevision {
			return nil
		}
		manifest, err := p.resolver.decrypt(context.Background(), payload.GetAppId(), event.GetId(), "manifest", payload.GetManifest())
		if err != nil {
			return err
		}
		state.Name = payload.GetName()
		state.Description = payload.GetDescription()
		state.ManifestJSON = string(manifest)
		state.ManifestRevision = payload.GetManifestRevision()
		state.RequestedBotScopes = sortedUniqueStrings(payload.GetRequestedBotScopes())
		state.RequestedUserScopes = sortedUniqueStrings(payload.GetRequestedUserScopes())
		state.RedirectOrigins = sortedUniqueStrings(payload.GetRedirectOrigins())
		state.UpdatedAt = createdAt
	case *evtv1.Event_AppInstalled:
		payload := value.AppInstalled
		state := p.apps[payload.GetAppId()]
		if state == nil || state.Lifecycle == AppLifecycleDeleted || payload.GetInstallGeneration() <= state.InstallGeneration {
			return nil
		}
		p.closeCredentialWatchersLocked(state.ID)
		state.Lifecycle = AppLifecycleInstalled
		state.InstallGeneration = payload.GetInstallGeneration()
		state.botTokenVerifier = bytes.Clone(payload.GetBotTokenVerifier())
		state.appTokenVerifier = bytes.Clone(payload.GetAppTokenVerifier())
		state.GrantedBotScopes = sortedUniqueStrings(payload.GetGrantedBotScopes())
		state.GrantedUserScopes = sortedUniqueStrings(payload.GetGrantedUserScopes())
		state.UpdatedAt = createdAt
	case *evtv1.Event_AppBotTokenRotated:
		payload := value.AppBotTokenRotated
		state := p.apps[payload.GetAppId()]
		if state == nil || state.Lifecycle == AppLifecycleDeleted || payload.GetInstallGeneration() <= state.InstallGeneration {
			return nil
		}
		p.closeCredentialWatchersLocked(state.ID)
		state.Lifecycle = AppLifecycleInstalled
		state.InstallGeneration = payload.GetInstallGeneration()
		state.botTokenVerifier = bytes.Clone(payload.GetBotTokenVerifier())
		state.GrantedBotScopes = sortedUniqueStrings(payload.GetGrantedBotScopes())
		state.GrantedUserScopes = sortedUniqueStrings(payload.GetGrantedUserScopes())
		state.UpdatedAt = createdAt
	case *evtv1.Event_AppBotTokenRevoked:
		payload := value.AppBotTokenRevoked
		state := p.apps[payload.GetAppId()]
		if state == nil || payload.GetInstallGeneration() <= state.InstallGeneration {
			return nil
		}
		p.closeCredentialWatchersLocked(state.ID)
		state.InstallGeneration = payload.GetInstallGeneration()
		state.botTokenVerifier = nil
		state.UpdatedAt = createdAt
	case *evtv1.Event_AppUninstalled:
		payload := value.AppUninstalled
		state := p.apps[payload.GetAppId()]
		if state == nil || payload.GetInstallGeneration() <= state.InstallGeneration {
			return nil
		}
		p.closeCredentialWatchersLocked(state.ID)
		state.Lifecycle = AppLifecycleUninstalled
		state.InstallGeneration = payload.GetInstallGeneration()
		state.botTokenVerifier = nil
		state.appTokenVerifier = nil
		state.GrantedBotScopes = nil
		state.GrantedUserScopes = nil
		state.UpdatedAt = createdAt
	case *evtv1.Event_AppCredentialsRegenerated:
		payload := value.AppCredentialsRegenerated
		state := p.apps[payload.GetAppId()]
		if state == nil || state.Lifecycle == AppLifecycleDeleted {
			return nil
		}
		signingSecret, err := p.resolver.decrypt(context.Background(), payload.GetAppId(), event.GetId(), "signing_secret", payload.GetSigningSecret())
		if err != nil {
			return err
		}
		state.clientSecretVerifier = bytes.Clone(payload.GetClientSecretVerifier())
		state.signingSecret = string(signingSecret)
		state.UpdatedAt = createdAt
	case *evtv1.Event_AppDeleted:
		payload := value.AppDeleted
		state := p.apps[payload.GetAppId()]
		if state == nil {
			return nil
		}
		p.closeCredentialWatchersLocked(state.ID)
		state.Lifecycle = AppLifecycleDeleted
		state.Name = ""
		state.Description = ""
		state.ManifestJSON = ""
		state.RequestedBotScopes = nil
		state.RequestedUserScopes = nil
		state.GrantedBotScopes = nil
		state.GrantedUserScopes = nil
		state.RedirectOrigins = nil
		state.botTokenVerifier = nil
		state.appTokenVerifier = nil
		state.clientSecretVerifier = nil
		state.signingSecret = ""
		state.UpdatedAt = createdAt
	}
	return nil
}

func (p *AppProjection) get(appID string) (AppState, bool) {
	p.RLock()
	defer p.RUnlock()
	state, ok := p.apps[appID]
	if !ok {
		return AppState{}, false
	}
	return cloneAppState(state), true
}

func (p *AppProjection) activeAppForBot(botUserID string) (AppState, bool) {
	p.RLock()
	defer p.RUnlock()
	for _, state := range p.apps {
		if state.BotUserID == botUserID && state.Lifecycle != AppLifecycleDeleted {
			return cloneAppState(state), true
		}
	}
	return AppState{}, false
}

func (p *AppProjection) all() []AppState {
	p.RLock()
	defer p.RUnlock()
	result := make([]AppState, 0, len(p.apps))
	for _, state := range p.apps {
		if state.Lifecycle != AppLifecycleDeleted {
			result = append(result, cloneAppState(state))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].firstSequence < result[j].firstSequence })
	return result
}

func cloneAppState(state *AppState) AppState {
	clone := *state
	clone.RequestedBotScopes = append([]string(nil), state.RequestedBotScopes...)
	clone.RequestedUserScopes = append([]string(nil), state.RequestedUserScopes...)
	clone.GrantedBotScopes = append([]string(nil), state.GrantedBotScopes...)
	clone.GrantedUserScopes = append([]string(nil), state.GrantedUserScopes...)
	clone.RedirectOrigins = append([]string(nil), state.RedirectOrigins...)
	clone.clientSecretVerifier = bytes.Clone(state.clientSecretVerifier)
	clone.botTokenVerifier = bytes.Clone(state.botTokenVerifier)
	clone.appTokenVerifier = bytes.Clone(state.appTokenVerifier)
	return clone
}

func (p *AppProjection) watchCredentialsInvalidated(appID string) (<-chan struct{}, func()) {
	invalidated := make(chan struct{})
	p.Lock()
	state := p.apps[appID]
	if state == nil || state.Lifecycle != AppLifecycleInstalled {
		close(invalidated)
		p.Unlock()
		return invalidated, func() {}
	}
	p.nextCredentialWatcherID++
	id := p.nextCredentialWatcherID
	watchers := p.credentialWatchers[appID]
	if watchers == nil {
		watchers = make(map[uint64]chan struct{})
		p.credentialWatchers[appID] = watchers
	}
	watchers[id] = invalidated
	p.Unlock()
	var once sync.Once
	return invalidated, func() {
		once.Do(func() {
			p.Lock()
			defer p.Unlock()
			delete(p.credentialWatchers[appID], id)
			if len(p.credentialWatchers[appID]) == 0 {
				delete(p.credentialWatchers, appID)
			}
		})
	}
}

func (p *AppProjection) closeCredentialWatchersLocked(appID string) {
	for _, watcher := range p.credentialWatchers[appID] {
		close(watcher)
	}
	delete(p.credentialWatchers, appID)
}

func (p *AppProjection) adminProjectionEstimate() (int64, int64, []ProjectionAdminMetric) {
	p.RLock()
	defer p.RUnlock()
	var bytesEstimate int64
	for _, state := range p.apps {
		bytesEstimate += int64(len(state.ID) + len(state.OwnerUserID) + len(state.BotUserID) + len(state.Name) + len(state.Description) + len(state.ManifestJSON) + len(state.signingSecret) + 128)
		for _, values := range [][]string{state.RequestedBotScopes, state.RequestedUserScopes, state.GrantedBotScopes, state.GrantedUserScopes, state.RedirectOrigins} {
			for _, value := range values {
				bytesEstimate += int64(len(value))
			}
		}
	}
	return int64(len(p.apps)), bytesEstimate, nil
}
