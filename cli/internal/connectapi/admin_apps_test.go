package connectapi

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/chattocorp/chatto/cli/internal/config"
	"github.com/chattocorp/chatto/cli/internal/core"
	adminv1 "github.com/chattocorp/chatto/cli/internal/pb/chatto/admin/v1"
)

const adminAppTestManifest = `
display_information:
  name: Janan HR
oauth_config:
  redirect_urls:
    - https://hr.example.test/oauth/callback
  scopes:
    bot:
      - chat:write
`

func TestAdminAppServiceLifecycleAndShowOnceCredentials(t *testing.T) {
	env := newConnectAPITestEnv(t)
	if err := env.core.AssignOwnerRole(env.ctx, env.viewer.Id); err != nil {
		t.Fatalf("AssignOwnerRole: %v", err)
	}
	if err := env.core.CompleteBootstrappedSetup(env.ctx); err != nil {
		t.Fatalf("CompleteBootstrappedSetup: %v", err)
	}
	api := New(env.core, config.ChattoConfig{
		Apps:      config.AppCompatibilityConfig{Enabled: true},
		Webserver: config.WebserverConfig{URL: "https://chat.example.test"},
	}, "test")
	service := &adminAppService{api: api}
	ctx := withCaller(env.ctx, env.viewer)

	status, err := service.GetAppPlatform(ctx, connect.NewRequest(&adminv1.GetAppPlatformRequest{}))
	if err != nil {
		t.Fatalf("GetAppPlatform: %v", err)
	}
	if !status.Msg.GetEnabled() || status.Msg.GetApiUrl() != "https://chat.example.test/api/" || status.Msg.GetAuthorizeUrl() != "https://chat.example.test/oauth/v2/authorize" {
		t.Fatalf("platform = %#v", status.Msg)
	}
	validation, err := service.ValidateAppManifest(ctx, connect.NewRequest(&adminv1.ValidateAppManifestRequest{Manifest: adminAppTestManifest}))
	if err != nil {
		t.Fatalf("ValidateAppManifest: %v", err)
	}
	if !validation.Msg.GetValid() || validation.Msg.GetManifestJson() == "" || validation.Msg.GetSummary().GetName() != "Janan HR" {
		t.Fatalf("validation = %#v", validation.Msg)
	}

	created, err := service.CreateApp(ctx, connect.NewRequest(&adminv1.CreateAppRequest{Manifest: adminAppTestManifest}))
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if created.Msg.GetApp().GetState() != adminv1.AppLifecycleState_APP_LIFECYCLE_STATE_INSTALLED {
		t.Fatalf("created app = %#v", created.Msg.GetApp())
	}
	credentials := created.Msg.GetCredentials()
	if credentials.GetClientSecret() == "" || credentials.GetSigningSecret() == "" || credentials.GetBotToken() == "" || credentials.GetAppToken() == "" {
		t.Fatalf("created credentials = %#v", credentials)
	}
	if credentials.GetApiUrl() != "https://chat.example.test/api/" {
		t.Fatalf("api URL = %q", credentials.GetApiUrl())
	}

	listed, err := service.ListApps(ctx, connect.NewRequest(&adminv1.ListAppsRequest{}))
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if len(listed.Msg.GetApps()) != 1 || listed.Msg.GetApps()[0].GetManifestJson() != "" {
		t.Fatalf("list = %#v", listed.Msg)
	}
	got, err := service.GetApp(ctx, connect.NewRequest(&adminv1.GetAppRequest{AppId: created.Msg.GetApp().GetAppId()}))
	if err != nil || got.Msg.GetApp().GetManifestJson() == "" {
		t.Fatalf("GetApp = %#v, %v", got, err)
	}

	uninstalled, err := service.UninstallApp(ctx, connect.NewRequest(&adminv1.UninstallAppRequest{AppId: created.Msg.GetApp().GetAppId()}))
	if err != nil || uninstalled.Msg.GetApp().GetState() != adminv1.AppLifecycleState_APP_LIFECYCLE_STATE_UNINSTALLED {
		t.Fatalf("UninstallApp = %#v, %v", uninstalled, err)
	}
	installed, err := service.InstallApp(ctx, connect.NewRequest(&adminv1.InstallAppRequest{AppId: created.Msg.GetApp().GetAppId()}))
	if err != nil || installed.Msg.GetCredentials().GetBotToken() == "" || installed.Msg.GetCredentials().GetClientSecret() != "" {
		t.Fatalf("InstallApp = %#v, %v", installed, err)
	}
	regenerated, err := service.RegenerateAppCredentials(ctx, connect.NewRequest(&adminv1.RegenerateAppCredentialsRequest{AppId: created.Msg.GetApp().GetAppId()}))
	if err != nil || regenerated.Msg.GetCredentials().GetClientSecret() == "" || regenerated.Msg.GetCredentials().GetBotToken() != "" {
		t.Fatalf("RegenerateAppCredentials = %#v, %v", regenerated, err)
	}
}

func TestAdminAppServiceAuthorizationAndFeatureGate(t *testing.T) {
	env := newConnectAPITestEnv(t)
	api := New(env.core, config.ChattoConfig{
		Webserver: config.WebserverConfig{URL: "https://chat.example.test"},
	}, "test")
	service := &adminAppService{api: api}
	ctx := withCaller(env.ctx, env.viewer)
	if _, err := service.ListApps(ctx, connect.NewRequest(&adminv1.ListAppsRequest{})); errorCode(err) != connect.CodePermissionDenied {
		t.Fatalf("ListApps error = %v, want permission denied", err)
	}
	if err := env.core.GrantUserPermission(env.ctx, core.SystemActorID, env.viewer.Id, core.PermServerManage); err != nil {
		t.Fatalf("GrantUserPermission server.manage: %v", err)
	}
	if _, err := service.ListApps(ctx, connect.NewRequest(&adminv1.ListAppsRequest{})); errorCode(err) != connect.CodePermissionDenied {
		t.Fatalf("ListApps without bot.create error = %v, want permission denied", err)
	}
	if err := env.core.AssignOwnerRole(env.ctx, env.viewer.Id); err != nil {
		t.Fatalf("AssignOwnerRole: %v", err)
	}
	if _, err := service.CreateApp(ctx, connect.NewRequest(&adminv1.CreateAppRequest{Manifest: adminAppTestManifest})); errorCode(err) != connect.CodeFailedPrecondition {
		t.Fatalf("CreateApp error = %v, want failed precondition", err)
	}
}
