package connectapi

import (
	"context"
	"strings"

	"connectrpc.com/connect"
	"github.com/chattocorp/chatto/cli/internal/appcompat"
	"github.com/chattocorp/chatto/cli/internal/core"
	adminv1 "github.com/chattocorp/chatto/cli/internal/pb/chatto/admin/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	defaultAppLimit = 20
	maxAppLimit     = 100
)

type adminAppService struct{ api *API }

func (s *adminAppService) GetAppPlatform(ctx context.Context, _ *connect.Request[adminv1.GetAppPlatformRequest]) (*connect.Response[adminv1.GetAppPlatformResponse], error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.api.core.AuthorizeAppManagement(ctx, caller.UserID); err != nil {
		return nil, err
	}
	apiURL, authorizeURL := s.api.appPlatformURLs()
	return connect.NewResponse(&adminv1.GetAppPlatformResponse{
		Enabled:      s.api.config.Apps.Enabled,
		ApiUrl:       apiURL,
		AuthorizeUrl: authorizeURL,
	}), nil
}

func (s *adminAppService) ValidateAppManifest(ctx context.Context, req *connect.Request[adminv1.ValidateAppManifestRequest]) (*connect.Response[adminv1.ValidateAppManifestResponse], error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.api.core.AuthorizeAppManagement(ctx, caller.UserID); err != nil {
		return nil, err
	}
	manifest := appcompat.ParseManifest(req.Msg.GetManifest())
	return connect.NewResponse(apiManifestValidation(manifest)), nil
}

func (s *adminAppService) ListApps(ctx context.Context, req *connect.Request[adminv1.ListAppsRequest]) (*connect.Response[adminv1.ListAppsResponse], error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	states, err := s.api.core.ListApps(ctx, caller.UserID)
	if err != nil {
		return nil, err
	}
	limit, offset := apiPagination(req.Msg.GetPage(), defaultAppLimit, maxAppLimit)
	total := len(states)
	if offset > total {
		offset = total
	}
	end := min(offset+limit, total)
	apps := make([]*adminv1.App, 0, end-offset)
	for _, state := range states[offset:end] {
		apps = append(apps, apiApp(state, false))
	}
	return connect.NewResponse(&adminv1.ListAppsResponse{Apps: apps, Page: apiPageInfo(total, end < total)}), nil
}

func (s *adminAppService) GetApp(ctx context.Context, req *connect.Request[adminv1.GetAppRequest]) (*connect.Response[adminv1.GetAppResponse], error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	state, err := s.api.core.GetApp(ctx, caller.UserID, req.Msg.GetAppId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&adminv1.GetAppResponse{App: apiApp(state, true)}), nil
}

func (s *adminAppService) CreateApp(ctx context.Context, req *connect.Request[adminv1.CreateAppRequest]) (*connect.Response[adminv1.CreateAppResponse], error) {
	if err := s.api.requireAppPlatformEnabled(); err != nil {
		return nil, err
	}
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	manifest, input, err := appDefinition(req.Msg.GetManifest())
	if err != nil {
		return nil, err
	}
	result, err := s.api.core.CreateApp(ctx, caller.UserID, input, !req.Msg.GetCreateOnly())
	if err != nil {
		return nil, err
	}
	_ = manifest
	return connect.NewResponse(&adminv1.CreateAppResponse{
		App: apiApp(result.App, true), Credentials: s.api.apiAppCredentials(result.Credentials),
	}), nil
}

func (s *adminAppService) ReplaceAppManifest(ctx context.Context, req *connect.Request[adminv1.ReplaceAppManifestRequest]) (*connect.Response[adminv1.ReplaceAppManifestResponse], error) {
	if err := s.api.requireAppPlatformEnabled(); err != nil {
		return nil, err
	}
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	_, input, err := appDefinition(req.Msg.GetManifest())
	if err != nil {
		return nil, err
	}
	state, err := s.api.core.UpdateAppManifest(ctx, caller.UserID, req.Msg.GetAppId(), input)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&adminv1.ReplaceAppManifestResponse{App: apiApp(state, true)}), nil
}

func (s *adminAppService) ExportAppManifest(ctx context.Context, req *connect.Request[adminv1.ExportAppManifestRequest]) (*connect.Response[adminv1.ExportAppManifestResponse], error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	state, err := s.api.core.GetApp(ctx, caller.UserID, req.Msg.GetAppId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&adminv1.ExportAppManifestResponse{ManifestJson: state.ManifestJSON}), nil
}

func (s *adminAppService) InstallApp(ctx context.Context, req *connect.Request[adminv1.InstallAppRequest]) (*connect.Response[adminv1.InstallAppResponse], error) {
	if err := s.api.requireAppPlatformEnabled(); err != nil {
		return nil, err
	}
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.api.core.InstallApp(ctx, caller.UserID, req.Msg.GetAppId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&adminv1.InstallAppResponse{
		App: apiApp(result.App, true), Credentials: s.api.apiAppCredentials(result.Credentials),
	}), nil
}

func (s *adminAppService) UninstallApp(ctx context.Context, req *connect.Request[adminv1.UninstallAppRequest]) (*connect.Response[adminv1.UninstallAppResponse], error) {
	if err := s.api.requireAppPlatformEnabled(); err != nil {
		return nil, err
	}
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	state, err := s.api.core.UninstallApp(ctx, caller.UserID, req.Msg.GetAppId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&adminv1.UninstallAppResponse{App: apiApp(state, true)}), nil
}

func (s *adminAppService) RegenerateAppCredentials(ctx context.Context, req *connect.Request[adminv1.RegenerateAppCredentialsRequest]) (*connect.Response[adminv1.RegenerateAppCredentialsResponse], error) {
	if err := s.api.requireAppPlatformEnabled(); err != nil {
		return nil, err
	}
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.api.core.RegenerateAppCredentials(ctx, caller.UserID, req.Msg.GetAppId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&adminv1.RegenerateAppCredentialsResponse{
		App: apiApp(result.App, true), Credentials: s.api.apiAppCredentials(result.Credentials),
	}), nil
}

func (s *adminAppService) DeleteApp(ctx context.Context, req *connect.Request[adminv1.DeleteAppRequest]) (*connect.Response[adminv1.DeleteAppResponse], error) {
	if err := s.api.requireAppPlatformEnabled(); err != nil {
		return nil, err
	}
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.api.core.DeleteApp(ctx, caller.UserID, req.Msg.GetAppId()); err != nil {
		return nil, err
	}
	return connect.NewResponse(&adminv1.DeleteAppResponse{}), nil
}

func (a *API) requireAppPlatformEnabled() error {
	if !a.config.Apps.Enabled {
		return core.ErrAppPlatformDisabled
	}
	return nil
}

func (a *API) appPlatformURLs() (string, string) {
	origin := strings.TrimRight(a.config.Webserver.URL, "/")
	if origin == "" {
		return "", ""
	}
	return origin + "/api/", origin + "/oauth/v2/authorize"
}

func (a *API) apiAppCredentials(credentials core.AppCredentialIssue) *adminv1.AppCredentials {
	apiURL, authorizeURL := a.appPlatformURLs()
	return &adminv1.AppCredentials{
		ClientId: credentials.ClientID, ClientSecret: credentials.ClientSecret,
		SigningSecret: credentials.SigningSecret, BotToken: credentials.BotToken,
		AppToken: credentials.AppToken, ApiUrl: apiURL, AuthorizeUrl: authorizeURL,
	}
}

func appDefinition(raw string) (appcompat.Manifest, core.AppDefinitionInput, error) {
	manifest := appcompat.ParseManifest(raw)
	if !manifest.Valid() {
		return manifest, core.AppDefinitionInput{}, invalidArgument("the app manifest has validation errors")
	}
	return manifest, core.AppDefinitionInput{
		Name: manifest.Summary.Name, Description: manifest.Summary.Description,
		BotDisplayName: manifest.Summary.BotDisplayName, ManifestJSON: manifest.CanonicalJSON,
		RequestedBotScopes: manifest.Summary.BotScopes, RequestedUserScopes: manifest.Summary.UserScopes,
		RedirectOrigins: manifest.Summary.ExternalOrigins,
	}, nil
}

func apiManifestValidation(manifest appcompat.Manifest) *adminv1.ValidateAppManifestResponse {
	issues := make([]*adminv1.ManifestIssue, 0, len(manifest.Issues))
	for _, issue := range manifest.Issues {
		severity := adminv1.ManifestIssueSeverity_MANIFEST_ISSUE_SEVERITY_WARNING
		if issue.Severity == appcompat.ManifestIssueError {
			severity = adminv1.ManifestIssueSeverity_MANIFEST_ISSUE_SEVERITY_ERROR
		}
		issues = append(issues, &adminv1.ManifestIssue{
			Pointer: issue.Pointer, Line: uint32(max(issue.Line, 0)), Code: issue.Code,
			Message: issue.Message, Severity: severity,
		})
	}
	summary := manifest.Summary
	return &adminv1.ValidateAppManifestResponse{
		ManifestJson: manifest.CanonicalJSON, Valid: manifest.Valid(), Issues: issues,
		Summary: &adminv1.ManifestSummary{
			Name: summary.Name, Description: summary.Description,
			BotScopes: summary.BotScopes, UserScopes: summary.UserScopes,
			Events: summary.Events, Commands: summary.Commands, ExternalOrigins: summary.ExternalOrigins,
			ImplementedContracts: uint32(summary.ImplementedContracts), PartialContracts: uint32(summary.PartialContracts),
			UnsupportedContracts: uint32(summary.UnsupportedContracts),
		},
	}
}

func apiApp(state core.AppState, includeManifest bool) *adminv1.App {
	app := &adminv1.App{
		AppId: state.ID, OwnerUserId: state.OwnerUserID, BotUserId: state.BotUserID,
		Name: state.Name, Description: state.Description, State: apiAppLifecycle(state.Lifecycle),
		ManifestRevision: state.ManifestRevision, InstallGeneration: state.InstallGeneration,
		RequestedBotScopes: append([]string(nil), state.RequestedBotScopes...), RequestedUserScopes: append([]string(nil), state.RequestedUserScopes...),
		GrantedBotScopes: append([]string(nil), state.GrantedBotScopes...), GrantedUserScopes: append([]string(nil), state.GrantedUserScopes...),
		ExternalOrigins: append([]string(nil), state.RedirectOrigins...),
		CreatedAt:       timestamppb.New(state.CreatedAt), UpdatedAt: timestamppb.New(state.UpdatedAt),
	}
	if includeManifest {
		app.ManifestJson = state.ManifestJSON
	}
	return app
}

func apiAppLifecycle(lifecycle core.AppLifecycle) adminv1.AppLifecycleState {
	switch lifecycle {
	case core.AppLifecycleNotInstalled:
		return adminv1.AppLifecycleState_APP_LIFECYCLE_STATE_NOT_INSTALLED
	case core.AppLifecycleInstalled:
		return adminv1.AppLifecycleState_APP_LIFECYCLE_STATE_INSTALLED
	case core.AppLifecycleUninstalled:
		return adminv1.AppLifecycleState_APP_LIFECYCLE_STATE_UNINSTALLED
	case core.AppLifecycleDeleted:
		return adminv1.AppLifecycleState_APP_LIFECYCLE_STATE_DELETED
	default:
		return adminv1.AppLifecycleState_APP_LIFECYCLE_STATE_UNSPECIFIED
	}
}
