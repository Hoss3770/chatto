package core

import (
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/chattocorp/chatto/cli/internal/evtstream"
	evtv1 "github.com/chattocorp/chatto/cli/internal/pb/chatto/core/evt/v1"
	"github.com/chattocorp/chatto/cli/internal/readmodel"
	"github.com/chattocorp/chatto/pkg/events"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const maxAppMutationAttempts = 5

var (
	ErrAppNotInstalled      = errors.New("app is not installed")
	ErrAppAlreadyInstalled  = errors.New("app is already installed")
	ErrAppClientSecret      = errors.New("app client secret is invalid")
	ErrAppManagedBot        = errors.New("bot belongs to an app definition")
	ErrAppPlatformDisabled  = errors.New("app platform is disabled")
	errAppMutationExhausted = errors.New("app mutation OCC retry exhausted")
)

// AppModel owns the projection used by app commands and credential checks.
type AppModel struct {
	projection events.ProjectionHandle[*AppProjection]
}

func newAppModel(projection events.ProjectionHandle[*AppProjection]) *AppModel {
	return &AppModel{projection: projection}
}

// AppDefinitionInput is a validated, target-neutral app definition accepted by
// core. Compatibility manifest parsing stays in the edge adapter.
type AppDefinitionInput struct {
	Name                string
	Description         string
	BotDisplayName      string
	ManifestJSON        string
	RequestedBotScopes  []string
	RequestedUserScopes []string
	RedirectOrigins     []string
}

// AppCredentialIssue contains show-once raw values. The values must never be
// logged or persisted outside encrypted exchange-recovery state.
type AppCredentialIssue struct {
	ClientID      string
	ClientSecret  string
	SigningSecret string
	BotToken      string
	AppToken      string
}

// AppCreateResult returns the durable app plus credentials created by the
// request. Credentials are empty when that class was not created.
type AppCreateResult struct {
	App         AppState
	Credentials AppCredentialIssue
}

// ValidatedAppBotCredential identifies one installed managed-bot principal.
type ValidatedAppBotCredential struct {
	App AppState
	Bot *readmodel.User
}

func isCanonicalAppID(id string) bool {
	if len(id) != idLength+1 || id[0] != 'A' {
		return false
	}
	for i := 1; i < len(id); i++ {
		if !strings.ContainsRune(idAlphabet, rune(id[i])) {
			return false
		}
	}
	return true
}

func validateAppDefinitionInput(input AppDefinitionInput) error {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || utf8.RuneCountInString(input.Name) > 35 {
		return invalidArgument("app name must contain 1 to 35 characters")
	}
	if utf8.RuneCountInString(input.Description) > 140 {
		return invalidArgument("app description must contain at most 140 characters")
	}
	if strings.TrimSpace(input.ManifestJSON) == "" || len(input.ManifestJSON) > 1<<20 {
		return invalidArgument("app manifest must contain 1 byte to 1 MiB")
	}
	if len(input.RequestedUserScopes) > 0 {
		return invalidArgument("app user scopes are not supported")
	}
	return nil
}

func appBotLogin(appID string) string {
	return "app_" + strings.ToLower(appID[1:9])
}

func (c *ChattoCore) requireAppManager(ctx context.Context, actorID string) error {
	if err := c.requireHumanUser(ctx, actorID); err != nil {
		return err
	}
	if err := c.requireServerPermission(ctx, actorID, PermServerManage); err != nil {
		return err
	}
	allowed, err := c.CanCreateBots(ctx, actorID)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrPermissionDenied
	}
	return nil
}

// AuthorizeAppManagement verifies that a human caller can use the server app
// administration surface without reading or mutating an app.
func (c *ChattoCore) AuthorizeAppManagement(ctx context.Context, actorID string) error {
	return c.requireAppManager(ctx, actorID)
}

func (c *ChattoCore) appBotIsManaged(ctx context.Context, botUserID string) (bool, error) {
	if c.appModel == nil {
		return false, nil
	}
	if err := c.appModel.projection.Projector().WaitForCurrent(ctx); err != nil {
		return false, err
	}
	_, managed := c.appModel.projection.Projection().activeAppForBot(botUserID)
	return managed, nil
}

// CreateApp creates one durable app definition and a deactivated managed bot in
// one atomic EVT batch. When install is true, it then applies canonical bot
// grants and activates the installation.
func (c *ChattoCore) CreateApp(ctx context.Context, actorID string, input AppDefinitionInput, install bool) (AppCreateResult, error) {
	if err := validateAppDefinitionInput(input); err != nil {
		return AppCreateResult{}, err
	}
	if err := c.requireAppManager(ctx, actorID); err != nil {
		return AppCreateResult{}, err
	}
	if err := c.appAliases.Projector().WaitForCurrent(ctx); err != nil {
		return AppCreateResult{}, err
	}
	workspaceID := c.appAliases.Projection().workspace()
	if workspaceID == "" {
		return AppCreateResult{}, fmt.Errorf("workspace alias is unavailable")
	}

	appID := NewAppID()
	clientSecret, err := newAppClientSecret()
	if err != nil {
		return AppCreateResult{}, err
	}
	signingSecret, err := newAppSigningSecret()
	if err != nil {
		return AppCreateResult{}, err
	}
	contentKey, contentKeyRef, cleanupAppKey, err := c.createAppContentKey(ctx, appID)
	if err != nil {
		return AppCreateResult{}, err
	}
	keepAppKey := false
	defer func() {
		if !keepAppKey {
			cleanupAppKey()
		}
	}()

	displayName := strings.TrimSpace(input.BotDisplayName)
	if displayName == "" {
		displayName = strings.TrimSpace(input.Name)
	}
	_, err = c.createUserWithOptions(ctx, actorID, appBotLogin(appID), displayName, "", userCreationOptions{
		isBot:         true,
		botOwnerID:    actorID,
		botAPIKeyName: "App internal key",
		authorize: func() error {
			return c.requireAppManager(ctx, actorID)
		},
		additionalEntries: func(botID string, now *timestamppb.Timestamp) ([]evtstream.BatchEntry, error) {
			created := newEvent(actorID, &evtv1.Event{Event: &evtv1.Event_AppCreated{AppCreated: &evtv1.AppCreatedEvent{
				AppId: appID, OwnerUserId: actorID, BotUserId: botID,
				Name: strings.TrimSpace(input.Name), Description: strings.TrimSpace(input.Description), ManifestRevision: 1,
				ClientSecretVerifier: c.appCredentialVerifier(appClientSecretPurpose, clientSecret),
				RequestedBotScopes:   sortedUniqueStrings(input.RequestedBotScopes), RequestedUserScopes: sortedUniqueStrings(input.RequestedUserScopes),
				RedirectOrigins: sortedUniqueStrings(input.RedirectOrigins),
			}}})
			created.CreatedAt = now
			manifest, err := encryptAppValue(contentKey, appID, created.GetId(), "manifest", contentKeyRef, []byte(input.ManifestJSON))
			if err != nil {
				return nil, err
			}
			signing, err := encryptAppValue(contentKey, appID, created.GetId(), "signing_secret", contentKeyRef, []byte(signingSecret))
			if err != nil {
				return nil, err
			}
			created.GetAppCreated().Manifest = manifest
			created.GetAppCreated().SigningSecret = signing

			deactivated := newEvent(actorID, &evtv1.Event{Event: &evtv1.Event_UserAccountDeactivationChanged{
				UserAccountDeactivationChanged: &evtv1.UserAccountDeactivationChangedEvent{UserId: botID, Deactivated: true},
			}})
			deactivated.CreatedAt = now
			appAggregate := evtstream.AppAggregate(appID)
			return []evtstream.BatchEntry{
				{Subject: evtstream.UserAggregate(botID).SubjectFor(deactivated), Event: deactivated},
				{Subject: appAggregate.SubjectFor(created), Event: created, Expect: events.ExpectFilterSeq(appAggregate.AllEventsFilter(), 0)},
			}, nil
		},
	})
	if err != nil {
		// An ambiguous atomic publish can commit. Retain the app key unless an
		// authoritative EVT read confirms that the new app aggregate is empty.
		checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		tail, checkErr := c.EventPublisher.LastSubjectSeq(checkCtx, evtstream.AppAggregate(appID).AllEventsFilter())
		keepAppKey = checkErr != nil || tail > 0
		return AppCreateResult{}, err
	}
	keepAppKey = true
	if err := c.appModel.projection.Projector().WaitForCurrent(ctx); err != nil {
		return AppCreateResult{}, err
	}
	state, ok := c.appModel.projection.Projection().get(appID)
	if !ok {
		return AppCreateResult{}, fmt.Errorf("created app did not reach projection")
	}
	result := AppCreateResult{App: state, Credentials: AppCredentialIssue{
		ClientID: appID, ClientSecret: clientSecret, SigningSecret: signingSecret,
	}}
	if !install {
		return result, nil
	}
	installed, err := c.InstallApp(ctx, actorID, appID)
	if err != nil {
		return AppCreateResult{}, err
	}
	result.App = installed.App
	result.Credentials.BotToken = installed.Credentials.BotToken
	result.Credentials.AppToken = installed.Credentials.AppToken
	return result, nil
}

// GetApp returns one app visible to a server manager.
func (c *ChattoCore) GetApp(ctx context.Context, actorID, appID string) (AppState, error) {
	if err := c.requireAppManager(ctx, actorID); err != nil {
		return AppState{}, err
	}
	return c.getAppCurrent(ctx, appID, false)
}

// GetAppOAuthClient returns the public definition needed to validate an app
// authorization request. It does not expose credentials or verifiers.
func (c *ChattoCore) GetAppOAuthClient(ctx context.Context, appID string) (AppState, error) {
	return c.getAppCurrent(ctx, appID, false)
}

func (c *ChattoCore) getAppCurrent(ctx context.Context, appID string, includeDeleted bool) (AppState, error) {
	if !isCanonicalAppID(appID) {
		return AppState{}, ErrNotFound
	}
	if err := c.appModel.projection.Projector().WaitForCurrent(ctx); err != nil {
		return AppState{}, err
	}
	state, ok := c.appModel.projection.Projection().get(appID)
	if !ok || !includeDeleted && state.Lifecycle == AppLifecycleDeleted {
		return AppState{}, ErrNotFound
	}
	return c.withCurrentAppOwner(ctx, state)
}

func (c *ChattoCore) withCurrentAppOwner(ctx context.Context, state AppState) (AppState, error) {
	bot, err := c.GetUserReference(ctx, state.BotUserID)
	if errors.Is(err, ErrNotFound) {
		return state, nil
	}
	if err != nil {
		return AppState{}, err
	}
	if bot.GetIsBot() && bot.GetBotOwnerUserId() != "" {
		state.OwnerUserID = bot.GetBotOwnerUserId()
	}
	return state, nil
}

// getAppAtTail synchronizes the app projection to an authoritative aggregate
// tail and returns the OCC position that corresponds to that read.
func (c *ChattoCore) getAppAtTail(ctx context.Context, appID string, includeDeleted bool) (AppState, evtstream.Aggregate, uint64, error) {
	if !isCanonicalAppID(appID) {
		return AppState{}, evtstream.Aggregate{}, 0, ErrNotFound
	}
	aggregate := evtstream.AppAggregate(appID)
	tail, err := c.EventPublisher.LastSubjectSeq(ctx, aggregate.AllEventsFilter())
	if err != nil {
		return AppState{}, evtstream.Aggregate{}, 0, err
	}
	if err := c.appModel.projection.Projector().WaitFor(ctx, events.SubjectPosition(aggregate.AllEventsFilter(), tail)); err != nil {
		return AppState{}, evtstream.Aggregate{}, 0, err
	}
	state, ok := c.appModel.projection.Projection().get(appID)
	if !ok || !includeDeleted && state.Lifecycle == AppLifecycleDeleted {
		return AppState{}, evtstream.Aggregate{}, 0, ErrNotFound
	}
	state, err = c.withCurrentAppOwner(ctx, state)
	if err != nil {
		return AppState{}, evtstream.Aggregate{}, 0, err
	}
	return state, aggregate, tail, nil
}

// ListApps returns active management records in stable creation order.
func (c *ChattoCore) ListApps(ctx context.Context, actorID string) ([]AppState, error) {
	if err := c.requireAppManager(ctx, actorID); err != nil {
		return nil, err
	}
	if err := c.appModel.projection.Projector().WaitForCurrent(ctx); err != nil {
		return nil, err
	}
	states := c.appModel.projection.Projection().all()
	botIDs := make([]string, len(states))
	for index, state := range states {
		botIDs[index] = state.BotUserID
	}
	bots, err := c.GetUserReferences(ctx, botIDs)
	if err != nil {
		return nil, err
	}
	for index, bot := range bots {
		if bot.GetIsBot() && bot.GetBotOwnerUserId() != "" {
			states[index].OwnerUserID = bot.GetBotOwnerUserId()
		}
	}
	return states, nil
}

// UpdateAppManifest accepts a new complete manifest revision without changing
// the currently granted scopes.
func (c *ChattoCore) UpdateAppManifest(ctx context.Context, actorID, appID string, input AppDefinitionInput) (AppState, error) {
	if err := validateAppDefinitionInput(input); err != nil {
		return AppState{}, err
	}
	if err := c.requireAppManager(ctx, actorID); err != nil {
		return AppState{}, err
	}
	for range maxAppMutationAttempts {
		state, aggregate, tail, err := c.getAppAtTail(ctx, appID, false)
		if err != nil {
			return AppState{}, err
		}
		contentKey, err := c.appContentKeys.resolve(ctx, appID, appContentKeyRef(appID))
		if err != nil {
			return AppState{}, err
		}
		event := newEvent(actorID, &evtv1.Event{Event: &evtv1.Event_AppManifestUpdated{AppManifestUpdated: &evtv1.AppManifestUpdatedEvent{
			AppId: appID, Name: strings.TrimSpace(input.Name), Description: strings.TrimSpace(input.Description),
			ManifestRevision:   state.ManifestRevision + 1,
			RequestedBotScopes: sortedUniqueStrings(input.RequestedBotScopes), RequestedUserScopes: sortedUniqueStrings(input.RequestedUserScopes),
			RedirectOrigins: sortedUniqueStrings(input.RedirectOrigins),
		}}})
		event.GetAppManifestUpdated().Manifest, err = encryptAppValue(contentKey, appID, event.GetId(), "manifest", appContentKeyRef(appID), []byte(input.ManifestJSON))
		if err != nil {
			return AppState{}, err
		}
		subject := aggregate.SubjectFor(event)
		sequence, err := c.EventPublisher.AppendAt(ctx, subject, event, events.ExpectFilterSeq(aggregate.AllEventsFilter(), tail))
		if errors.Is(err, events.ErrConflict) {
			continue
		}
		if err != nil {
			return AppState{}, err
		}
		if err := c.appModel.projection.Projector().WaitFor(ctx, events.SubjectPosition(subject, sequence)); err != nil {
			return AppState{}, err
		}
		updated, _ := c.appModel.projection.Projection().get(appID)
		return updated, nil
	}
	return AppState{}, errAppMutationExhausted
}

var appScopePermissions = map[string][]Permission{
	"app_mentions:read": {PermMessageReadInteractions},
	"channels:history":  {PermMessageRead},
	"channels:read":     {PermRoomList},
	"chat:write":        {PermMessagePost},
	"chat:write.public": {PermMessagePost, PermRoomJoin},
	"files:read":        {PermMessageRead},
	"files:write":       {PermMessageAttach},
	"groups:history":    {PermMessageRead},
	"groups:read":       {PermRoomList},
	"im:history":        {PermMessageRead},
	"im:read":           {PermRoomList},
	"mpim:history":      {PermMessageRead},
	"mpim:read":         {PermRoomList},
	"pins:read":         {PermMessageRead},
	"pins:write":        {PermMessagePin},
	"reactions:read":    {PermMessageRead},
	"reactions:write":   {PermMessageReact},
}

func permissionsForAppScopes(scopes []string) map[Permission]struct{} {
	permissions := make(map[Permission]struct{})
	for _, scope := range scopes {
		for _, permission := range appScopePermissions[scope] {
			permissions[permission] = struct{}{}
		}
	}
	return permissions
}

func (c *ChattoCore) applyAppBotPermissions(ctx context.Context, actorID string, state AppState) error {
	desired := permissionsForAppScopes(state.RequestedBotScopes)
	all := make(map[Permission]struct{})
	for _, permissions := range appScopePermissions {
		for _, permission := range permissions {
			all[permission] = struct{}{}
		}
	}
	for permission := range all {
		permissionState := PermissionStateNone
		if _, ok := desired[permission]; ok {
			permissionState = PermissionStateAllow
		}
		if err := c.SetUserPermissionState(ctx, actorID, state.BotUserID, PermissionTargetScope{Kind: MatrixScopeServer}, permission, permissionState); err != nil {
			return fmt.Errorf("apply app scope permission %s: %w", permission, err)
		}
	}
	return nil
}

// InstallApp applies scope-derived canonical bot grants, issues new access
// tokens, and atomically activates the app and managed bot.
func (c *ChattoCore) InstallApp(ctx context.Context, actorID, appID string) (AppCreateResult, error) {
	if err := c.requireAppManager(ctx, actorID); err != nil {
		return AppCreateResult{}, err
	}
	state, err := c.getAppCurrent(ctx, appID, false)
	if err != nil {
		return AppCreateResult{}, err
	}
	if state.Lifecycle == AppLifecycleInstalled {
		return AppCreateResult{}, ErrAppAlreadyInstalled
	}
	if err := c.appAliases.Projector().WaitForCurrent(ctx); err != nil {
		return AppCreateResult{}, err
	}
	workspaceID := c.appAliases.Projection().workspace()
	botToken, err := newAppBotToken(workspaceID, appID)
	if err != nil {
		return AppCreateResult{}, err
	}
	appToken, err := newAppLevelToken(appID)
	if err != nil {
		return AppCreateResult{}, err
	}
	for range maxAppMutationAttempts {
		candidate, err := c.getAppCurrent(ctx, appID, false)
		if err != nil {
			return AppCreateResult{}, err
		}
		if candidate.Lifecycle == AppLifecycleInstalled {
			return AppCreateResult{}, ErrAppAlreadyInstalled
		}
		if err := c.applyAppBotPermissions(ctx, actorID, candidate); err != nil {
			return AppCreateResult{}, err
		}
		state, appAggregate, appTail, err := c.getAppAtTail(ctx, appID, false)
		if err != nil {
			return AppCreateResult{}, err
		}
		if state.Lifecycle == AppLifecycleInstalled {
			return AppCreateResult{}, ErrAppAlreadyInstalled
		}
		if state.ManifestRevision != candidate.ManifestRevision {
			continue
		}
		userAggregate := evtstream.UserAggregate(state.BotUserID)
		userTail, err := c.EventPublisher.LastSubjectSeq(ctx, userAggregate.AllEventsFilter())
		if err != nil {
			return AppCreateResult{}, err
		}
		if err := c.userModel.waitForUsers(ctx, events.SubjectPosition(userAggregate.AllEventsFilter(), userTail)); err != nil {
			return AppCreateResult{}, err
		}
		generation := state.InstallGeneration + 1
		installed := newEvent(actorID, &evtv1.Event{Event: &evtv1.Event_AppInstalled{AppInstalled: &evtv1.AppInstalledEvent{
			AppId: appID, InstallGeneration: generation,
			BotTokenVerifier: c.appCredentialVerifier(appBotTokenPurpose, botToken),
			AppTokenVerifier: c.appCredentialVerifier(appTokenPurpose, appToken),
			GrantedBotScopes: sortedUniqueStrings(state.RequestedBotScopes),
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
			return AppCreateResult{}, err
		}
		if err := c.appModel.projection.Projector().WaitFor(ctx, events.SubjectPosition(entries[0].Subject, sequences[0])); err != nil {
			return AppCreateResult{}, err
		}
		if err := c.userModel.waitForUsers(ctx, events.SubjectPosition(entries[1].Subject, sequences[1])); err != nil {
			return AppCreateResult{}, err
		}
		if err := c.userModel.waitForUserAuth(ctx, events.SubjectPosition(entries[1].Subject, sequences[1])); err != nil {
			return AppCreateResult{}, err
		}
		state, _ = c.appModel.projection.Projection().get(appID)
		return AppCreateResult{App: state, Credentials: AppCredentialIssue{ClientID: appID, BotToken: botToken, AppToken: appToken}}, nil
	}
	return AppCreateResult{}, errAppMutationExhausted
}

// UninstallApp revokes every app credential and atomically deactivates the
// managed bot without deleting its identity or historical authorship.
func (c *ChattoCore) UninstallApp(ctx context.Context, actorID, appID string) (AppState, error) {
	if err := c.requireAppManager(ctx, actorID); err != nil {
		return AppState{}, err
	}
	return c.uninstallApp(ctx, actorID, appID)
}

func (c *ChattoCore) uninstallApp(ctx context.Context, actorID, appID string) (AppState, error) {
	for range maxAppMutationAttempts {
		state, appAggregate, appTail, err := c.getAppAtTail(ctx, appID, false)
		if err != nil {
			return AppState{}, err
		}
		if state.Lifecycle == AppLifecycleUninstalled {
			return state, nil
		}
		userAggregate := evtstream.UserAggregate(state.BotUserID)
		userTail, err := c.EventPublisher.LastSubjectSeq(ctx, userAggregate.AllEventsFilter())
		if err != nil {
			return AppState{}, err
		}
		uninstalled := newEvent(actorID, &evtv1.Event{Event: &evtv1.Event_AppUninstalled{AppUninstalled: &evtv1.AppUninstalledEvent{
			AppId: appID, InstallGeneration: state.InstallGeneration + 1,
		}}})
		deactivated := newEvent(actorID, &evtv1.Event{Event: &evtv1.Event_UserAccountDeactivationChanged{
			UserAccountDeactivationChanged: &evtv1.UserAccountDeactivationChangedEvent{UserId: state.BotUserID, Deactivated: true},
		}})
		entries := []evtstream.BatchEntry{
			{Subject: appAggregate.SubjectFor(uninstalled), Event: uninstalled, Expect: events.ExpectFilterSeq(appAggregate.AllEventsFilter(), appTail)},
			{Subject: userAggregate.SubjectFor(deactivated), Event: deactivated, Expect: events.ExpectFilterSeq(userAggregate.AllEventsFilter(), userTail)},
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
		if err := c.userModel.waitForUserAuth(ctx, events.SubjectPosition(entries[1].Subject, sequences[1])); err != nil {
			return AppState{}, err
		}
		state, _ = c.appModel.projection.Projection().get(appID)
		return state, nil
	}
	return AppState{}, errAppMutationExhausted
}

// DeleteApp removes one inactive definition from management. It deliberately
// retains encrypted EVT facts and the managed bot for historical references.
func (c *ChattoCore) DeleteApp(ctx context.Context, actorID, appID string) error {
	if err := c.requireAppManager(ctx, actorID); err != nil {
		return err
	}
	for range maxAppMutationAttempts {
		state, aggregate, tail, err := c.getAppAtTail(ctx, appID, true)
		if err != nil {
			return err
		}
		if state.Lifecycle == AppLifecycleDeleted {
			return nil
		}
		if state.Lifecycle == AppLifecycleInstalled {
			return invalidArgument("app must be uninstalled before deletion")
		}
		event := newEvent(actorID, &evtv1.Event{Event: &evtv1.Event_AppDeleted{AppDeleted: &evtv1.AppDeletedEvent{AppId: appID}}})
		subject := aggregate.SubjectFor(event)
		sequence, err := c.EventPublisher.AppendAt(ctx, subject, event, events.ExpectFilterSeq(aggregate.AllEventsFilter(), tail))
		if errors.Is(err, events.ErrConflict) {
			continue
		}
		if err != nil {
			return err
		}
		return c.appModel.projection.Projector().WaitFor(ctx, events.SubjectPosition(subject, sequence))
	}
	return errAppMutationExhausted
}

// RegenerateAppCredentials replaces the client and signing secrets and returns
// their raw values once.
func (c *ChattoCore) RegenerateAppCredentials(ctx context.Context, actorID, appID string) (AppCreateResult, error) {
	if err := c.requireAppManager(ctx, actorID); err != nil {
		return AppCreateResult{}, err
	}
	clientSecret, err := newAppClientSecret()
	if err != nil {
		return AppCreateResult{}, err
	}
	signingSecret, err := newAppSigningSecret()
	if err != nil {
		return AppCreateResult{}, err
	}
	for range maxAppMutationAttempts {
		state, aggregate, tail, err := c.getAppAtTail(ctx, appID, false)
		if err != nil {
			return AppCreateResult{}, err
		}
		contentKey, err := c.appContentKeys.resolve(ctx, appID, appContentKeyRef(appID))
		if err != nil {
			return AppCreateResult{}, err
		}
		event := newEvent(actorID, &evtv1.Event{Event: &evtv1.Event_AppCredentialsRegenerated{AppCredentialsRegenerated: &evtv1.AppCredentialsRegeneratedEvent{
			AppId: appID, ClientSecretVerifier: c.appCredentialVerifier(appClientSecretPurpose, clientSecret),
		}}})
		event.GetAppCredentialsRegenerated().SigningSecret, err = encryptAppValue(contentKey, appID, event.GetId(), "signing_secret", appContentKeyRef(appID), []byte(signingSecret))
		if err != nil {
			return AppCreateResult{}, err
		}
		subject := aggregate.SubjectFor(event)
		sequence, err := c.EventPublisher.AppendAt(ctx, subject, event, events.ExpectFilterSeq(aggregate.AllEventsFilter(), tail))
		if errors.Is(err, events.ErrConflict) {
			continue
		}
		if err != nil {
			return AppCreateResult{}, err
		}
		if err := c.appModel.projection.Projector().WaitFor(ctx, events.SubjectPosition(subject, sequence)); err != nil {
			return AppCreateResult{}, err
		}
		state, _ = c.appModel.projection.Projection().get(appID)
		return AppCreateResult{App: state, Credentials: AppCredentialIssue{ClientID: appID, ClientSecret: clientSecret, SigningSecret: signingSecret}}, nil
	}
	return AppCreateResult{}, errAppMutationExhausted
}

// ValidateAppClientSecret authenticates the app's OAuth client secret through
// the current durable projection.
func (c *ChattoCore) ValidateAppClientSecret(ctx context.Context, appID, clientSecret string) (AppState, error) {
	state, err := c.getAppCurrent(ctx, appID, false)
	if err != nil {
		return AppState{}, ErrAppClientSecret
	}
	presented := c.appCredentialVerifier(appClientSecretPurpose, clientSecret)
	if len(state.clientSecretVerifier) == 0 || subtle.ConstantTimeCompare(presented, state.clientSecretVerifier) != 1 {
		return AppState{}, ErrAppClientSecret
	}
	return state, nil
}

func parseAppBotToken(token string) (workspaceID, appID string, ok bool) {
	parts := strings.SplitN(token, "-", 4)
	if len(parts) != 4 || parts[0] != "xoxb" || parts[1] == "" || !isCanonicalAppID(parts[2]) || parts[3] == "" {
		return "", "", false
	}
	return parts[1], parts[2], true
}

func parseAppLevelToken(token string) (appID string, ok bool) {
	parts := strings.SplitN(token, "-", 4)
	if len(parts) != 4 || parts[0] != "xapp" || parts[1] != "1" || !isCanonicalAppID(parts[2]) || parts[3] == "" {
		return "", false
	}
	return parts[2], true
}

// ValidateAppBotToken resolves a compatibility token to the canonical managed
// bot and current scope generation.
func (c *ChattoCore) ValidateAppBotToken(ctx context.Context, token string) (ValidatedAppBotCredential, error) {
	workspaceID, appID, ok := parseAppBotToken(token)
	if !ok {
		return ValidatedAppBotCredential{}, ErrAuthTokenNotFound
	}
	if err := c.appAliases.Projector().WaitForCurrent(ctx); err != nil {
		return ValidatedAppBotCredential{}, err
	}
	if workspaceID != c.appAliases.Projection().workspace() {
		return ValidatedAppBotCredential{}, ErrAuthTokenNotFound
	}
	state, err := c.getAppCurrent(ctx, appID, false)
	if err != nil || state.Lifecycle != AppLifecycleInstalled {
		return ValidatedAppBotCredential{}, ErrAuthTokenNotFound
	}
	presented := c.appCredentialVerifier(appBotTokenPurpose, token)
	if len(state.botTokenVerifier) == 0 || subtle.ConstantTimeCompare(presented, state.botTokenVerifier) != 1 {
		return ValidatedAppBotCredential{}, ErrAuthTokenNotFound
	}
	bot, err := c.GetUser(ctx, state.BotUserID)
	if err != nil || !bot.GetIsBot() || bot.GetDeactivated() {
		return ValidatedAppBotCredential{}, ErrAuthTokenNotFound
	}
	owner, err := c.GetUser(ctx, bot.GetBotOwnerUserId())
	if err != nil || owner.GetIsBot() || owner.GetDeactivated() {
		return ValidatedAppBotCredential{}, ErrAuthTokenNotFound
	}
	return ValidatedAppBotCredential{App: state, Bot: bot}, nil
}

// ValidateAppLevelToken authenticates an installed app-level token.
func (c *ChattoCore) ValidateAppLevelToken(ctx context.Context, token string) (AppState, error) {
	appID, ok := parseAppLevelToken(token)
	if !ok {
		return AppState{}, ErrAuthTokenNotFound
	}
	state, err := c.getAppCurrent(ctx, appID, false)
	if err != nil || state.Lifecycle != AppLifecycleInstalled {
		return AppState{}, ErrAuthTokenNotFound
	}
	presented := c.appCredentialVerifier(appTokenPurpose, token)
	if len(state.appTokenVerifier) == 0 || subtle.ConstantTimeCompare(presented, state.appTokenVerifier) != 1 {
		return AppState{}, ErrAuthTokenNotFound
	}
	bot, err := c.GetUser(ctx, state.BotUserID)
	if err != nil || !bot.GetIsBot() || bot.GetDeactivated() {
		return AppState{}, ErrAuthTokenNotFound
	}
	owner, err := c.GetUser(ctx, bot.GetBotOwnerUserId())
	if err != nil || owner.GetIsBot() || owner.GetDeactivated() {
		return AppState{}, ErrAuthTokenNotFound
	}
	return state, nil
}

// RevokeAppBotToken revokes the current bot token. test=true validates without
// changing state.
func (c *ChattoCore) RevokeAppBotToken(ctx context.Context, token string, test bool) (bool, error) {
	credential, err := c.ValidateAppBotToken(ctx, token)
	if err != nil {
		return false, err
	}
	if test {
		return false, nil
	}
	appID := credential.App.ID
	for range maxAppMutationAttempts {
		state, aggregate, tail, err := c.getAppAtTail(ctx, appID, false)
		if err != nil {
			return false, err
		}
		if len(state.botTokenVerifier) == 0 {
			return true, nil
		}
		if subtle.ConstantTimeCompare(state.botTokenVerifier, credential.App.botTokenVerifier) != 1 {
			return false, ErrAuthTokenNotFound
		}
		event := newEvent(state.BotUserID, &evtv1.Event{Event: &evtv1.Event_AppBotTokenRevoked{AppBotTokenRevoked: &evtv1.AppBotTokenRevokedEvent{
			AppId: appID, InstallGeneration: state.InstallGeneration + 1,
		}}})
		subject := aggregate.SubjectFor(event)
		sequence, err := c.EventPublisher.AppendAt(ctx, subject, event, events.ExpectFilterSeq(aggregate.AllEventsFilter(), tail))
		if errors.Is(err, events.ErrConflict) {
			continue
		}
		if err != nil {
			return false, err
		}
		if err := c.appModel.projection.Projector().WaitFor(ctx, events.SubjectPosition(subject, sequence)); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, errAppMutationExhausted
}

// UninstallAppWithCredentials validates the target method's token and client
// secret before it uses the canonical uninstall operation.
func (c *ChattoCore) UninstallAppWithCredentials(ctx context.Context, token, clientID, clientSecret string) error {
	credential, err := c.ValidateAppBotToken(ctx, token)
	if err != nil {
		return err
	}
	if credential.App.ID != clientID {
		return ErrAppClientSecret
	}
	if _, err := c.ValidateAppClientSecret(ctx, clientID, clientSecret); err != nil {
		return err
	}
	_, err = c.uninstallApp(ctx, credential.Bot.GetId(), clientID)
	return err
}

// RotateAppBotTokenForOAuth replaces the current bot token with a caller-owned
// proposed value. Reusing the same verifier is idempotent after a lost response.
func (c *ChattoCore) RotateAppBotTokenForOAuth(ctx context.Context, appID string, botScopes, userScopes []string, token, actorID string) (AppState, error) {
	verifier := c.appCredentialVerifier(appBotTokenPurpose, token)
	for range maxAppMutationAttempts {
		state, aggregate, tail, err := c.getAppAtTail(ctx, appID, false)
		if err != nil {
			return AppState{}, err
		}
		if state.Lifecycle != AppLifecycleInstalled {
			return AppState{}, ErrAppNotInstalled
		}
		if bytes.Equal(state.botTokenVerifier, verifier) {
			return state, nil
		}
		event := newEvent(actorID, &evtv1.Event{Event: &evtv1.Event_AppBotTokenRotated{AppBotTokenRotated: &evtv1.AppBotTokenRotatedEvent{
			AppId: appID, InstallGeneration: state.InstallGeneration + 1, BotTokenVerifier: verifier,
			GrantedBotScopes: sortedUniqueStrings(botScopes), GrantedUserScopes: sortedUniqueStrings(userScopes),
		}}})
		subject := aggregate.SubjectFor(event)
		sequence, err := c.EventPublisher.AppendAt(ctx, subject, event, events.ExpectFilterSeq(aggregate.AllEventsFilter(), tail))
		if errors.Is(err, events.ErrConflict) {
			continue
		}
		if err != nil {
			return AppState{}, err
		}
		if err := c.appModel.projection.Projector().WaitFor(ctx, events.SubjectPosition(subject, sequence)); err != nil {
			return AppState{}, err
		}
		state, _ = c.appModel.projection.Projection().get(appID)
		return state, nil
	}
	return AppState{}, errAppMutationExhausted
}

// AppSigningSecret returns request-signing material only to trusted delivery
// code after current app state has been loaded.
func (c *ChattoCore) AppSigningSecret(ctx context.Context, appID string) (string, error) {
	state, err := c.getAppCurrent(ctx, appID, false)
	if err != nil {
		return "", err
	}
	if state.Lifecycle != AppLifecycleInstalled {
		return "", ErrAppNotInstalled
	}
	if state.signingSecret == "" {
		return "", ErrNotFound
	}
	return state.signingSecret, nil
}

// AppWorkspaceID returns the stable target workspace alias.
func (c *ChattoCore) AppWorkspaceID(ctx context.Context) (string, error) {
	if err := c.appAliases.Projector().WaitForCurrent(ctx); err != nil {
		return "", err
	}
	workspaceID := c.appAliases.Projection().workspace()
	if workspaceID == "" {
		return "", ErrNotFound
	}
	return workspaceID, nil
}

// AppWorkspaceName returns the current Chatto server name for compatibility
// responses.
func (c *ChattoCore) AppWorkspaceName() string {
	name := strings.TrimSpace(c.configModel.GetEffectiveServerName())
	if name == "" {
		return "Chatto"
	}
	return name
}

// AppBotAlias returns the deterministic compatibility bot identifier.
func AppBotAlias(botUserID string) string {
	return compatibilityAlias("B", botUserID)
}

// AppMessageTimestamp returns the deterministic room-local message timestamp.
func (c *ChattoCore) AppMessageTimestamp(ctx context.Context, messageID string) (roomID, timestamp string, err error) {
	if err := c.appAliases.Projector().WaitForCurrent(ctx); err != nil {
		return "", "", err
	}
	roomID, timestamp, ok := c.appAliases.Projection().messageTimestamp(messageID)
	if !ok {
		return "", "", ErrNotFound
	}
	return roomID, timestamp, nil
}

// AppMessageID resolves a room-local compatibility timestamp.
func (c *ChattoCore) AppMessageID(ctx context.Context, roomID, timestamp string) (string, error) {
	if err := c.appAliases.Projector().WaitForCurrent(ctx); err != nil {
		return "", err
	}
	messageID, ok := c.appAliases.Projection().messageID(roomID, timestamp)
	if !ok {
		return "", ErrNotFound
	}
	return messageID, nil
}

// WatchAppCredentialsInvalidated closes when this replica observes a token
// generation change, uninstall, or deletion.
func (c *ChattoCore) WatchAppCredentialsInvalidated(appID string) (<-chan struct{}, func()) {
	if c.appModel == nil {
		return nil, func() {}
	}
	return c.appModel.projection.Projection().watchCredentialsInvalidated(appID)
}
