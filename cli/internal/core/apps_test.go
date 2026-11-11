package core

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/chattocorp/chatto/cli/internal/evtstream"
	evtv1 "github.com/chattocorp/chatto/cli/internal/pb/chatto/core/evt/v1"
	runtimestatev1 "github.com/chattocorp/chatto/cli/internal/pb/chatto/core/runtime_state/v1"
	"github.com/chattocorp/chatto/cli/internal/testutil"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func testAppInput() AppDefinitionInput {
	return AppDefinitionInput{
		Name:           "Janan HR",
		Description:    "Internal leave requests",
		BotDisplayName: "Janan HR",
		ManifestJSON:   "{\"display_information\":{\"name\":\"Janan HR\"}}\n",
		RequestedBotScopes: []string{
			"chat:write",
		},
		RedirectOrigins: []string{"https://hr.example.test"},
	}
}

func testAppOwner(t *testing.T, c *ChattoCore, login string) string {
	t.Helper()
	ctx := testContext(t)
	user, err := c.CreateUser(ctx, SystemActorID, login, "App Owner", "password123")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := c.AssignOwnerRole(ctx, user.GetId()); err != nil {
		t.Fatalf("AssignOwnerRole: %v", err)
	}
	if err := c.CompleteBootstrappedSetup(ctx); err != nil {
		t.Fatalf("CompleteBootstrappedSetup: %v", err)
	}
	return user.GetId()
}

func TestAppLifecycleRevokesCredentialsAndPreservesBotIdentity(t *testing.T) {
	t.Parallel()
	c, _ := setupTestCore(t)
	ctx := testContext(t)
	ownerID := testAppOwner(t, c, "app-lifecycle-owner")

	created, err := c.CreateApp(ctx, ownerID, testAppInput(), true)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if created.App.Lifecycle != AppLifecycleInstalled || created.Credentials.ClientSecret == "" || created.Credentials.SigningSecret == "" || created.Credentials.BotToken == "" || created.Credentials.AppToken == "" {
		t.Fatalf("created = %#v", created)
	}
	credential, err := c.ValidateAppBotToken(ctx, created.Credentials.BotToken)
	if err != nil {
		t.Fatalf("ValidateAppBotToken: %v", err)
	}
	if credential.Bot.GetId() != created.App.BotUserID || credential.Bot.GetDeactivated() {
		t.Fatalf("bot = %#v", credential.Bot)
	}
	if _, err := c.ValidateAppLevelToken(ctx, created.Credentials.AppToken); err != nil {
		t.Fatalf("ValidateAppLevelToken: %v", err)
	}
	if _, err := c.ValidateAppClientSecret(ctx, created.App.ID, created.Credentials.ClientSecret); err != nil {
		t.Fatalf("ValidateAppClientSecret: %v", err)
	}
	if _, err := c.DeleteBot(ctx, ownerID, created.App.BotUserID); !errors.Is(err, ErrAppManagedBot) {
		t.Fatalf("DeleteBot managed app bot error = %v", err)
	}
	if err := c.DeleteUser(ctx, ownerID, created.App.BotUserID); !errors.Is(err, ErrAppManagedBot) {
		t.Fatalf("DeleteUser managed app bot error = %v", err)
	}

	uninstalled, err := c.UninstallApp(ctx, ownerID, created.App.ID)
	if err != nil {
		t.Fatalf("UninstallApp: %v", err)
	}
	if uninstalled.Lifecycle != AppLifecycleUninstalled || uninstalled.BotUserID != created.App.BotUserID {
		t.Fatalf("uninstalled = %#v", uninstalled)
	}
	if _, err := c.ValidateAppBotToken(ctx, created.Credentials.BotToken); !errors.Is(err, ErrAuthTokenNotFound) {
		t.Fatalf("old bot token error = %v", err)
	}
	if _, err := c.ValidateAppLevelToken(ctx, created.Credentials.AppToken); !errors.Is(err, ErrAuthTokenNotFound) {
		t.Fatalf("old app token error = %v", err)
	}
	bot, err := c.GetUser(ctx, created.App.BotUserID)
	if err != nil || !bot.GetDeactivated() {
		t.Fatalf("uninstalled bot = %#v, %v", bot, err)
	}

	reinstalled, err := c.InstallApp(ctx, ownerID, created.App.ID)
	if err != nil {
		t.Fatalf("InstallApp: %v", err)
	}
	if reinstalled.App.BotUserID != created.App.BotUserID || reinstalled.Credentials.BotToken == created.Credentials.BotToken {
		t.Fatalf("reinstalled = %#v", reinstalled)
	}
	if _, err := c.ValidateAppBotToken(ctx, reinstalled.Credentials.BotToken); err != nil {
		t.Fatalf("new bot token error = %v", err)
	}
	if err := c.DeleteApp(ctx, ownerID, created.App.ID); err == nil {
		t.Fatal("DeleteApp installed app succeeded")
	}
	if _, err := c.UninstallApp(ctx, ownerID, created.App.ID); err != nil {
		t.Fatalf("second UninstallApp: %v", err)
	}
	if err := c.DeleteApp(ctx, ownerID, created.App.ID); err != nil {
		t.Fatalf("DeleteApp: %v", err)
	}
	if _, err := c.GetApp(ctx, ownerID, created.App.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetApp deleted error = %v", err)
	}
	if deleted, err := c.DeleteBot(ctx, ownerID, created.App.BotUserID); err != nil || !deleted {
		t.Fatalf("DeleteBot released app bot = %t, %v", deleted, err)
	}
	if err := c.DeleteApp(ctx, ownerID, created.App.ID); err != nil {
		t.Fatalf("repeated DeleteApp after bot deletion: %v", err)
	}
}

func TestAppReadsFollowCurrentManagedBotOwner(t *testing.T) {
	t.Parallel()
	c, _ := setupTestCore(t)
	ctx := testContext(t)
	ownerID := testAppOwner(t, c, "app-owner-reassignment")
	newOwner, err := c.CreateUser(ctx, ownerID, "app-new-owner", "New Owner", "password123")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	created, err := c.CreateApp(ctx, ownerID, testAppInput(), false)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if err := c.DeleteUser(ctx, SystemActorID, ownerID); !errors.Is(err, ErrAppManagedBot) {
		t.Fatalf("DeleteUser app owner error = %v", err)
	}
	if _, err := c.ReassignBotOwner(ctx, ownerID, created.App.BotUserID, newOwner.GetId()); err != nil {
		t.Fatalf("ReassignBotOwner: %v", err)
	}
	app, err := c.GetApp(ctx, ownerID, created.App.ID)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if app.OwnerUserID != newOwner.GetId() {
		t.Fatalf("owner = %q, want %q", app.OwnerUserID, newOwner.GetId())
	}
	apps, err := c.ListApps(ctx, ownerID)
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if len(apps) != 1 || apps[0].OwnerUserID != newOwner.GetId() {
		t.Fatalf("apps = %#v", apps)
	}
}

func TestAppCreateAmbiguityRetainsKeysUnlessAuthoritativeStateIsEmpty(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)
	if creationMayHaveCommitted(ctx, nil) {
		t.Fatal("nil confirmation retained keys")
	}
	if creationMayHaveCommitted(ctx, func(context.Context) (bool, error) { return false, nil }) {
		t.Fatal("confirmed empty aggregate retained keys")
	}
	if !creationMayHaveCommitted(ctx, func(context.Context) (bool, error) { return true, nil }) {
		t.Fatal("committed aggregate did not retain keys")
	}
	if !creationMayHaveCommitted(ctx, func(context.Context) (bool, error) { return false, errors.New("storage unavailable") }) {
		t.Fatal("unavailable confirmation did not fail closed")
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if !creationMayHaveCommitted(canceled, func(checkCtx context.Context) (bool, error) {
		if checkCtx.Err() != nil {
			t.Fatalf("confirmation inherited caller cancellation: %v", checkCtx.Err())
		}
		if _, ok := checkCtx.Deadline(); !ok {
			t.Fatal("confirmation has no cleanup deadline")
		}
		return true, nil
	}) {
		t.Fatal("canceled caller prevented committed-state retention")
	}
}

func TestAppCredentialsAreEncryptedOrVerifiedInDurableFacts(t *testing.T) {
	t.Parallel()
	c, _ := setupTestCore(t)
	ctx := testContext(t)
	ownerID := testAppOwner(t, c, "app-encryption-owner")
	input := testAppInput()
	input.ManifestJSON = "{\"display_information\":{\"name\":\"confidential-manifest-marker\"}}"
	created, err := c.CreateApp(ctx, ownerID, input, false)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	events, _, err := c.EventPublisher.SubjectEvents(ctx, evtstream.AppAggregate(created.App.ID).Subject(evtstream.EventAppCreated))
	if err != nil || len(events) != 1 {
		t.Fatalf("SubjectEvents = %d, %v", len(events), err)
	}
	wire, err := proto.Marshal(events[0])
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, secret := range []string{input.ManifestJSON, created.Credentials.ClientSecret, created.Credentials.SigningSecret} {
		if bytes.Contains(wire, []byte(secret)) {
			t.Fatalf("durable fact contains plaintext secret %q", secret)
		}
	}
	payload := events[0].GetAppCreated()
	if len(payload.GetManifest().GetCiphertext()) == 0 || len(payload.GetSigningSecret().GetCiphertext()) == 0 || len(payload.GetClientSecretVerifier()) == 0 {
		t.Fatalf("encrypted payload = %#v", payload)
	}
}

func TestConcurrentAppInstallCommitsOneCredentialGeneration(t *testing.T) {
	t.Parallel()
	c, _ := setupTestCore(t)
	ctx := testContext(t)
	ownerID := testAppOwner(t, c, "app-concurrent-install-owner")
	created, err := c.CreateApp(ctx, ownerID, testAppInput(), false)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	results := make(chan AppCreateResult, 2)
	errorsByInstall := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			result, err := c.InstallApp(ctx, ownerID, created.App.ID)
			results <- result
			errorsByInstall <- err
		})
	}
	wg.Wait()
	close(results)
	close(errorsByInstall)
	var succeeded, conflicted int
	for err := range errorsByInstall {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrAppAlreadyInstalled):
			conflicted++
		default:
			t.Fatalf("InstallApp error = %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("succeeded = %d, already installed = %d", succeeded, conflicted)
	}
	var activeToken string
	for result := range results {
		if result.Credentials.BotToken != "" {
			activeToken = result.Credentials.BotToken
		}
	}
	if activeToken == "" {
		t.Fatal("successful install did not return a bot token")
	}
	credential, err := c.ValidateAppBotToken(ctx, activeToken)
	if err != nil {
		t.Fatalf("ValidateAppBotToken: %v", err)
	}
	if credential.App.InstallGeneration != 1 {
		t.Fatalf("install generation = %d, want 1", credential.App.InstallGeneration)
	}
}

func TestRegenerateAppCredentialsInvalidatesOnlyClientAndSigningSecrets(t *testing.T) {
	t.Parallel()
	c, _ := setupTestCore(t)
	ctx := testContext(t)
	ownerID := testAppOwner(t, c, "app-regenerate-owner")
	created, err := c.CreateApp(ctx, ownerID, testAppInput(), true)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	regenerated, err := c.RegenerateAppCredentials(ctx, ownerID, created.App.ID)
	if err != nil {
		t.Fatalf("RegenerateAppCredentials: %v", err)
	}
	if regenerated.Credentials.ClientSecret == "" || regenerated.Credentials.SigningSecret == "" || regenerated.Credentials.ClientSecret == created.Credentials.ClientSecret {
		t.Fatalf("regenerated credentials = %#v", regenerated.Credentials)
	}
	if _, err := c.ValidateAppClientSecret(ctx, created.App.ID, created.Credentials.ClientSecret); !errors.Is(err, ErrAppClientSecret) {
		t.Fatalf("old client secret error = %v", err)
	}
	if _, err := c.ValidateAppClientSecret(ctx, created.App.ID, regenerated.Credentials.ClientSecret); err != nil {
		t.Fatalf("new client secret error = %v", err)
	}
	if _, err := c.ValidateAppBotToken(ctx, created.Credentials.BotToken); err != nil {
		t.Fatalf("bot token was invalidated: %v", err)
	}
}

func TestAppOwnerDeactivationInvalidatesBotToken(t *testing.T) {
	t.Parallel()
	c, _ := setupTestCore(t)
	ctx := testContext(t)
	owner, err := c.CreateUser(ctx, SystemActorID, "app-deactivated-owner", "App Owner", "password123")
	if err != nil {
		t.Fatalf("CreateUser owner: %v", err)
	}
	ownerID := owner.GetId()
	for _, permission := range []Permission{PermServerManage, PermBotCreate, PermMessagePost} {
		if err := c.GrantUserPermission(ctx, SystemActorID, ownerID, permission); err != nil {
			t.Fatalf("GrantUserPermission owner %s: %v", permission, err)
		}
	}
	if err := c.CompleteBootstrappedSetup(ctx); err != nil {
		t.Fatalf("CompleteBootstrappedSetup: %v", err)
	}
	manager, err := c.CreateUser(ctx, SystemActorID, "app-account-manager", "Account Manager", "password123")
	if err != nil {
		t.Fatalf("CreateUser manager: %v", err)
	}
	if err := c.GrantUserPermission(ctx, SystemActorID, manager.GetId(), PermUserManageAccounts); err != nil {
		t.Fatalf("GrantUserPermission: %v", err)
	}
	created, err := c.CreateApp(ctx, ownerID, testAppInput(), true)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if err := c.SetUserDeactivated(ctx, manager.GetId(), ownerID, true); err != nil {
		t.Fatalf("SetUserDeactivated: %v", err)
	}
	if _, err := c.ValidateAppBotToken(ctx, created.Credentials.BotToken); !errors.Is(err, ErrAuthTokenNotFound) {
		t.Fatalf("ValidateAppBotToken deactivated owner error = %v", err)
	}
	if _, err := c.ValidateAppLevelToken(ctx, created.Credentials.AppToken); !errors.Is(err, ErrAuthTokenNotFound) {
		t.Fatalf("ValidateAppLevelToken deactivated owner error = %v", err)
	}
}

func TestAppOAuthExchangeIsExactlyRecoverable(t *testing.T) {
	t.Parallel()
	c, _ := setupTestCore(t)
	ctx := testContext(t)
	ownerID := testAppOwner(t, c, "app-oauth-owner")
	created, err := c.CreateApp(ctx, ownerID, testAppInput(), false)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	redirectURI := "https://hr.example.test/oauth/callback"
	code, err := c.CreateAppOAuthCode(ctx, ownerID, created.App.ID, redirectURI, nil, nil)
	if err != nil {
		t.Fatalf("CreateAppOAuthCode: %v", err)
	}
	first, err := c.ExchangeAppOAuthCode(ctx, code, created.App.ID, created.Credentials.ClientSecret, redirectURI, "Test workspace")
	if err != nil {
		t.Fatalf("ExchangeAppOAuthCode: %v", err)
	}
	second, err := c.ExchangeAppOAuthCode(ctx, code, created.App.ID, created.Credentials.ClientSecret, redirectURI, "Test workspace")
	if err != nil {
		t.Fatalf("retry ExchangeAppOAuthCode: %v", err)
	}
	if first.AccessToken == "" || first.AccessToken != second.AccessToken || first.BotUserID != created.App.BotUserID || first.Scope != "chat:write" {
		t.Fatalf("exchange results = %#v, %#v", first, second)
	}
	if _, err := c.ValidateAppBotToken(ctx, first.AccessToken); err != nil {
		t.Fatalf("ValidateAppBotToken exchanged token: %v", err)
	}
	if _, err := c.ExchangeAppOAuthCode(ctx, code, created.App.ID, created.Credentials.ClientSecret, redirectURI+"/other", "Test workspace"); !errors.Is(err, ErrAppOAuthGrant) {
		t.Fatalf("changed redirect exchange error = %v", err)
	}
}

func TestAppOAuthExchangeConcurrentClaimsReturnOneExactResult(t *testing.T) {
	t.Parallel()
	c, _ := setupTestCore(t)
	ctx := testContext(t)
	ownerID := testAppOwner(t, c, "app-oauth-concurrent-owner")
	created, err := c.CreateApp(ctx, ownerID, testAppInput(), false)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	redirectURI := "https://hr.example.test/oauth/callback"
	code, err := c.CreateAppOAuthCode(ctx, ownerID, created.App.ID, redirectURI, nil, nil)
	if err != nil {
		t.Fatalf("CreateAppOAuthCode: %v", err)
	}

	results := make(chan AppOAuthExchange, 2)
	errorsByClaim := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			result, err := c.ExchangeAppOAuthCode(ctx, code, created.App.ID, created.Credentials.ClientSecret, redirectURI, "Test workspace")
			results <- result
			errorsByClaim <- err
		})
	}
	wg.Wait()
	close(results)
	close(errorsByClaim)
	for err := range errorsByClaim {
		if err != nil {
			t.Fatalf("concurrent ExchangeAppOAuthCode: %v", err)
		}
	}
	var first AppOAuthExchange
	for result := range results {
		if first.AccessToken == "" {
			first = result
			continue
		}
		if result.AccessToken != first.AccessToken || result.TokenType != first.TokenType || result.Scope != first.Scope ||
			result.BotUserID != first.BotUserID || result.AppID != first.AppID || result.WorkspaceID != first.WorkspaceID ||
			result.WorkspaceName != first.WorkspaceName || !slices.Equal(result.BotScopes, first.BotScopes) {
			t.Fatalf("concurrent results differ: %#v, %#v", first, result)
		}
	}
	if first.AccessToken == "" {
		t.Fatal("concurrent exchange returned an empty access token")
	}
	state, err := c.GetApp(ctx, ownerID, created.App.ID)
	if err != nil {
		t.Fatalf("GetApp after concurrent exchange: %v", err)
	}
	if state.InstallGeneration != 1 {
		t.Fatalf("install generation = %d, want 1", state.InstallGeneration)
	}
}

func TestAppOAuthExchangeRejectsExpiredCode(t *testing.T) {
	t.Parallel()
	c, _ := setupTestCore(t)
	ctx := testContext(t)
	ownerID := testAppOwner(t, c, "app-oauth-expired-owner")
	created, err := c.CreateApp(ctx, ownerID, testAppInput(), false)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	redirectURI := "https://hr.example.test/oauth/callback"
	code, err := c.CreateAppOAuthCode(ctx, ownerID, created.App.ID, redirectURI, nil, nil)
	if err != nil {
		t.Fatalf("CreateAppOAuthCode: %v", err)
	}
	key := c.appOAuthCodeKey(code)
	entry, err := c.storage.runtimeStateKV.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get code: %v", err)
	}
	var record runtimestatev1.AppOAuthAuthorizationCode
	if err := proto.Unmarshal(entry.Value(), &record); err != nil {
		t.Fatalf("Unmarshal code: %v", err)
	}
	record.ExpiresAt = timestamppb.New(time.Now().Add(-time.Minute))
	value, err := proto.Marshal(&record)
	if err != nil {
		t.Fatalf("Marshal code: %v", err)
	}
	if _, err := c.storage.runtimeStateKV.Update(ctx, key, value, entry.Revision()); err != nil {
		t.Fatalf("Update code: %v", err)
	}
	if _, err := c.ExchangeAppOAuthCode(ctx, code, created.App.ID, created.Credentials.ClientSecret, redirectURI, "Test workspace"); !errors.Is(err, ErrAppOAuthCodeInvalid) {
		t.Fatalf("expired exchange error = %v", err)
	}
}

func TestAppRateLimitIsSharedAcrossReplicas(t *testing.T) {
	t.Parallel()
	ns, _ := testutil.StartNATS(t)
	first := startReplica(t, ns, testCoreConfig())
	second := startReplica(t, ns, testCoreConfig())
	ctx := testContext(t)
	now := time.Unix(1_800_000_000, 0).UTC()

	var wg sync.WaitGroup
	errorsByReplica := make(chan error, 2)
	for _, replica := range []*ChattoCore{first, second} {
		wg.Add(1)
		go func(replica *ChattoCore) {
			defer wg.Done()
			errorsByReplica <- replica.ConsumeAppRateLimit(ctx, "Ashared", "auth.test", 1, now)
		}(replica)
	}
	wg.Wait()
	close(errorsByReplica)
	var allowed, limited int
	for err := range errorsByReplica {
		if err == nil {
			allowed++
			continue
		}
		if _, ok := errors.AsType[*AppRateLimitError](err); ok {
			limited++
			continue
		}
		t.Fatalf("ConsumeAppRateLimit error = %v", err)
	}
	if allowed != 1 || limited != 1 {
		t.Fatalf("allowed = %d, limited = %d", allowed, limited)
	}
}

func TestAppRateLimitFailsClosedWhenStorageIsUnavailable(t *testing.T) {
	t.Parallel()
	c, nc := setupTestCore(t)
	nc.Close()
	err := c.ConsumeAppRateLimit(testContext(t), "Astorage", "auth.test", 1, time.Now().UTC())
	if err == nil {
		t.Fatal("ConsumeAppRateLimit succeeded with unavailable storage")
	}
	if _, ok := errors.AsType[*AppRateLimitError](err); ok {
		t.Fatalf("storage failure reported as an ordinary limit: %v", err)
	}
}

func TestAppCredentialRevocationIsVisibleAcrossReplicas(t *testing.T) {
	t.Parallel()
	ns, _ := testutil.StartNATS(t)
	first := startReplica(t, ns, testCoreConfig())
	second := startReplica(t, ns, testCoreConfig())
	ctx := testContext(t)
	ownerID := testAppOwner(t, first, "app-replica-owner")
	created, err := first.CreateApp(ctx, ownerID, testAppInput(), true)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if _, err := second.ValidateAppBotToken(ctx, created.Credentials.BotToken); err != nil {
		t.Fatalf("second replica initial validation: %v", err)
	}
	if _, err := first.UninstallApp(ctx, ownerID, created.App.ID); err != nil {
		t.Fatalf("UninstallApp: %v", err)
	}
	if _, err := second.ValidateAppBotToken(ctx, created.Credentials.BotToken); !errors.Is(err, ErrAuthTokenNotFound) {
		t.Fatalf("second replica stale token error = %v", err)
	}
}

func TestAppTokenParsersAllowURLSafeSecretHyphens(t *testing.T) {
	t.Parallel()
	appID := "A12345678901234"
	workspaceID, parsedAppID, ok := parseAppBotToken("xoxb-Tworkspace-" + appID + "-part-one-two")
	if !ok || workspaceID != "Tworkspace" || parsedAppID != appID {
		t.Fatalf("parseAppBotToken = %q, %q, %t", workspaceID, parsedAppID, ok)
	}
	parsedAppID, ok = parseAppLevelToken("xapp-1-" + appID + "-part-one-two")
	if !ok || parsedAppID != appID {
		t.Fatalf("parseAppLevelToken = %q, %t", parsedAppID, ok)
	}
}

func TestAppAliasProjectionUsesStableAliasesAndMonotonicTimestamps(t *testing.T) {
	t.Parallel()
	projection := NewAppAliasProjection()
	initialized := &evtv1.Event{
		Id: "Einitialization", CreatedAt: timestamppb.New(time.Unix(100, 0)),
		Event: &evtv1.Event_ServerInitialized{ServerInitialized: &evtv1.ServerInitializedEvent{}},
	}
	if err := projection.Apply(initialized, 1); err != nil {
		t.Fatalf("Apply initialized: %v", err)
	}
	if got := projection.workspace(); got != "Tinitialization" {
		t.Fatalf("workspace = %q", got)
	}
	createdAt := timestamppb.New(time.Unix(200, 123_456_000))
	first := &evtv1.Event{Id: "Mfirst", CreatedAt: createdAt, Event: &evtv1.Event_MessagePosted{MessagePosted: &evtv1.MessagePostedEvent{RoomId: "Rroom"}}}
	second := &evtv1.Event{Id: "Msecond", CreatedAt: createdAt, Event: &evtv1.Event_MessagePosted{MessagePosted: &evtv1.MessagePostedEvent{RoomId: "Rroom"}}}
	if err := projection.Apply(first, 2); err != nil {
		t.Fatalf("Apply first: %v", err)
	}
	if err := projection.Apply(second, 3); err != nil {
		t.Fatalf("Apply second: %v", err)
	}
	_, firstTimestamp, _ := projection.messageTimestamp(first.GetId())
	_, secondTimestamp, _ := projection.messageTimestamp(second.GetId())
	if firstTimestamp != "200.123456" || secondTimestamp != "200.123457" {
		t.Fatalf("timestamps = %q, %q", firstTimestamp, secondTimestamp)
	}
	if err := projection.Apply(first, 4); err != nil {
		t.Fatalf("replay first: %v", err)
	}
	_, replayedTimestamp, _ := projection.messageTimestamp(first.GetId())
	if replayedTimestamp != firstTimestamp {
		t.Fatalf("replayed timestamp = %q, want %q", replayedTimestamp, firstTimestamp)
	}
}
