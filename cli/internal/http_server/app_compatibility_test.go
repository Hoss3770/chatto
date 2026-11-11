package http_server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/chattocorp/chatto/cli/internal/config"
	"github.com/chattocorp/chatto/cli/internal/core"
)

type appMethodTestEnvironment struct {
	server *HTTPServer
	http   *httptest.Server
	owner  string
}

func setupAppMethodTestEnvironment(t *testing.T) appMethodTestEnvironment {
	t.Helper()
	s := setupHTTPServerTestServer(t, config.AuthConfig{})
	s.config.Apps.Enabled = true
	s.config.Webserver.URL = "https://chat.example.test"
	// Register both route families to prove the short target API route does not
	// shadow the native ConnectRPC service tree.
	s.setupConnectAPI()
	s.setupAppCompatibilityRoutes()
	ts := httptest.NewServer(s.router)
	t.Cleanup(ts.Close)
	ctx := context.Background()
	owner, err := s.core.CreateUser(ctx, core.SystemActorID, "app-http-owner", "App HTTP Owner", "password123")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.core.AssignOwnerRole(ctx, owner.GetId()); err != nil {
		t.Fatalf("AssignOwnerRole: %v", err)
	}
	if err := s.core.CompleteBootstrappedSetup(ctx); err != nil {
		t.Fatalf("CompleteBootstrappedSetup: %v", err)
	}
	return appMethodTestEnvironment{server: s, http: ts, owner: owner.GetId()}
}

func createHTTPTestApp(t *testing.T, env appMethodTestEnvironment) core.AppCreateResult {
	t.Helper()
	result, err := env.server.core.CreateApp(context.Background(), env.owner, core.AppDefinitionInput{
		Name: "Janan HR", BotDisplayName: "Janan HR",
		ManifestJSON:       "{\"display_information\":{\"name\":\"Janan HR\"}}\n",
		RequestedBotScopes: []string{"chat:write"},
		RedirectOrigins:    []string{"https://hr.example.test"},
	}, true)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	return result
}

func callAppMethod(t *testing.T, env appMethodTestEnvironment, method string, values url.Values, token string) (int, http.Header, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, env.http.URL+"/api/"+method, strings.NewReader(values.Encode()))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := env.http.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("Unmarshal %q: %v", body, err)
	}
	return response.StatusCode, response.Header, payload
}

func TestAppCompatibilityFoundationMethods(t *testing.T) {
	env := setupAppMethodTestEnvironment(t)
	created := createHTTPTestApp(t, env)

	t.Run("api.test echoes safe arguments and requested error", func(t *testing.T) {
		status, headers, payload := callAppMethod(t, env, "api.test", url.Values{"hello": {"world"}}, "")
		if status != http.StatusOK || payload["ok"] != true || payload["args"].(map[string]any)["hello"] != "world" {
			t.Fatalf("response = %d %#v", status, payload)
		}
		if headers.Get("Cache-Control") != "no-store" || headers.Get("Referrer-Policy") != "no-referrer" {
			t.Fatalf("privacy headers = %#v", headers)
		}
		_, _, payload = callAppMethod(t, env, "api.test", url.Values{"error": {"test_error"}, "token": {"secret"}}, "")
		if payload["ok"] != false || payload["error"] != "test_error" {
			t.Fatalf("error response = %#v", payload)
		}
		if _, leaked := payload["args"].(map[string]any)["token"]; leaked {
			t.Fatalf("api.test echoed token: %#v", payload)
		}
	})

	t.Run("auth.test rejects missing and malformed credentials", func(t *testing.T) {
		_, _, payload := callAppMethod(t, env, "auth.test", nil, "")
		if payload["error"] != "not_authed" {
			t.Fatalf("missing token response = %#v", payload)
		}
		_, _, payload = callAppMethod(t, env, "auth.test", nil, "xoxb-malformed")
		if payload["error"] != "invalid_auth" {
			t.Fatalf("malformed token response = %#v", payload)
		}
	})

	t.Run("auth.test resolves the managed bot and canonical origin", func(t *testing.T) {
		status, _, payload := callAppMethod(t, env, "auth.test", nil, created.Credentials.BotToken)
		if status != http.StatusOK || payload["ok"] != true || payload["user_id"] != created.App.BotUserID || payload["url"] != "https://chat.example.test/" {
			t.Fatalf("response = %d %#v", status, payload)
		}
		if !strings.HasPrefix(payload["team_id"].(string), "T") || !strings.HasPrefix(payload["bot_id"].(string), "B") {
			t.Fatalf("aliases = %#v", payload)
		}
	})

	t.Run("unsupported method returns an explicit machine error", func(t *testing.T) {
		for _, token := range []string{"", created.Credentials.BotToken} {
			status, _, payload := callAppMethod(t, env, "chat.postMessage", nil, token)
			if status != http.StatusOK || payload["ok"] != false || payload["error"] != "not_implemented" {
				t.Fatalf("response = %d %#v", status, payload)
			}
		}
	})

	t.Run("auth.revoke supports validation and revocation", func(t *testing.T) {
		_, _, payload := callAppMethod(t, env, "auth.revoke", url.Values{"test": {"true"}}, created.Credentials.BotToken)
		if payload["ok"] != true || payload["revoked"] != false {
			t.Fatalf("test response = %#v", payload)
		}
		if _, err := env.server.core.ValidateAppBotToken(context.Background(), created.Credentials.BotToken); err != nil {
			t.Fatalf("test mode revoked token: %v", err)
		}
		_, _, payload = callAppMethod(t, env, "auth.revoke", nil, created.Credentials.BotToken)
		if payload["ok"] != true || payload["revoked"] != true {
			t.Fatalf("revoke response = %#v", payload)
		}
		_, _, payload = callAppMethod(t, env, "auth.test", nil, created.Credentials.BotToken)
		if payload["error"] != "invalid_auth" {
			t.Fatalf("revoked auth response = %#v", payload)
		}
	})
}

func TestAppOAuthExchangeMethodReturnsTargetShapeAndStableRetry(t *testing.T) {
	env := setupAppMethodTestEnvironment(t)
	created, err := env.server.core.CreateApp(context.Background(), env.owner, core.AppDefinitionInput{
		Name: "Janan HR", BotDisplayName: "Janan HR",
		ManifestJSON:       `{"display_information":{"name":"Janan HR"},"oauth_config":{"redirect_urls":["https://hr.example.test/oauth/callback"],"scopes":{"bot":["chat:write"]}}}`,
		RequestedBotScopes: []string{"chat:write"},
		RedirectOrigins:    []string{"https://hr.example.test"},
	}, false)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	redirectURI := "https://hr.example.test/oauth/callback"
	code, err := env.server.core.CreateAppOAuthCode(context.Background(), env.owner, created.App.ID, redirectURI, nil, nil)
	if err != nil {
		t.Fatalf("CreateAppOAuthCode: %v", err)
	}
	values := url.Values{
		"client_id": {created.App.ID}, "client_secret": {"wrong-secret"},
		"code": {code}, "redirect_uri": {redirectURI},
	}
	_, _, rejected := callAppMethod(t, env, "oauth.v2.access", values, "")
	if rejected["error"] != "bad_client_secret" {
		t.Fatalf("wrong-secret response = %#v", rejected)
	}
	values.Set("client_secret", created.Credentials.ClientSecret)
	_, _, first := callAppMethod(t, env, "oauth.v2.access", values, "")
	_, _, second := callAppMethod(t, env, "oauth.v2.access", values, "")
	if first["ok"] != true || first["token_type"] != "bot" || first["access_token"] == "" {
		t.Fatalf("first response = %#v", first)
	}
	if first["access_token"] != second["access_token"] {
		t.Fatalf("retry issued a different token: %#v %#v", first, second)
	}
	values.Set("redirect_uri", redirectURI+"/other")
	_, _, mismatched := callAppMethod(t, env, "oauth.v2.access", values, "")
	if mismatched["error"] != "bad_redirect_uri" {
		t.Fatalf("redirect mismatch response = %#v", mismatched)
	}
	team, ok := first["team"].(map[string]any)
	if !ok || !strings.HasPrefix(team["id"].(string), "T") {
		t.Fatalf("team = %#v", first["team"])
	}
}

func TestAppAuthorizeValidRequestRedirectsUnauthenticatedUserToLogin(t *testing.T) {
	env := setupAppMethodTestEnvironment(t)
	created, err := env.server.core.CreateApp(context.Background(), env.owner, core.AppDefinitionInput{
		Name: "Janan HR", BotDisplayName: "Janan HR",
		ManifestJSON:       `{"display_information":{"name":"Janan HR"},"oauth_config":{"redirect_urls":["https://hr.example.test/oauth/callback"],"scopes":{"bot":["chat:write"]}}}`,
		RequestedBotScopes: []string{"chat:write"},
		RedirectOrigins:    []string{"https://hr.example.test"},
	}, false)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	client := *env.http.Client()
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	invalidURL := env.http.URL + "/oauth/v2/authorize?client_id=" + url.QueryEscape(created.App.ID) +
		"&redirect_uri=" + url.QueryEscape("https://evil.example.test/oauth/callback") + "&scope=chat%3Awrite&state=opaque"
	invalidResponse, err := client.Get(invalidURL)
	if err != nil {
		t.Fatalf("Get invalid redirect: %v", err)
	}
	invalidResponse.Body.Close()
	if invalidResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid redirect status = %d", invalidResponse.StatusCode)
	}
	requestURL := env.http.URL + "/oauth/v2/authorize?client_id=" + url.QueryEscape(created.App.ID) +
		"&redirect_uri=" + url.QueryEscape("https://hr.example.test/oauth/callback") + "&scope=chat%3Awrite&state=opaque"
	response, err := client.Get(requestURL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusTemporaryRedirect || !strings.HasPrefix(response.Header.Get("Location"), "/login?redirect=") {
		t.Fatalf("response = %d location=%q", response.StatusCode, response.Header.Get("Location"))
	}

	postResponse, err := client.PostForm(env.http.URL+"/oauth/v2/authorize", url.Values{
		"client_id":     {created.App.ID},
		"redirect_uri":  {"https://hr.example.test/oauth/callback"},
		"scope":         {"chat:write"},
		"state":         {"opaque"},
		"response_type": {"code"},
		"decision":      {"approve"},
	})
	if err != nil {
		t.Fatalf("PostForm: %v", err)
	}
	defer postResponse.Body.Close()
	location, err := url.Parse(postResponse.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse login location: %v", err)
	}
	continuation, err := url.Parse(location.Query().Get("redirect"))
	if err != nil {
		t.Fatalf("parse authorize continuation: %v", err)
	}
	if postResponse.StatusCode != http.StatusSeeOther || continuation.Query().Get("client_id") != created.App.ID || continuation.Query().Get("state") != "opaque" {
		t.Fatalf("POST response = %d location=%q", postResponse.StatusCode, postResponse.Header.Get("Location"))
	}
}

func TestAppsUninstallMethodRequiresMatchingTokenAndClientSecret(t *testing.T) {
	env := setupAppMethodTestEnvironment(t)
	created := createHTTPTestApp(t, env)
	values := url.Values{
		"client_id":     {created.App.ID},
		"client_secret": {"wrong-secret"},
	}
	_, _, payload := callAppMethod(t, env, "apps.uninstall", values, created.Credentials.BotToken)
	if payload["error"] != "bad_client_secret" {
		t.Fatalf("wrong-secret response = %#v", payload)
	}
	state, err := env.server.core.GetApp(context.Background(), env.owner, created.App.ID)
	if err != nil || state.Lifecycle != core.AppLifecycleInstalled {
		t.Fatalf("wrong secret changed state = %#v, %v", state, err)
	}
	values.Set("client_secret", created.Credentials.ClientSecret)
	_, _, payload = callAppMethod(t, env, "apps.uninstall", values, created.Credentials.BotToken)
	if payload["ok"] != true {
		t.Fatalf("uninstall response = %#v", payload)
	}
	state, err = env.server.core.GetApp(context.Background(), env.owner, created.App.ID)
	if err != nil || state.Lifecycle != core.AppLifecycleUninstalled {
		t.Fatalf("app state = %#v, %v", state, err)
	}
}

func TestAppCompatibilityRejectsCredentialExchangeSecretsInQuery(t *testing.T) {
	env := setupAppMethodTestEnvironment(t)
	for _, method := range []string{"oauth.v2.access", "apps.uninstall"} {
		response, err := env.http.Client().Get(env.http.URL + "/api/" + method + "?client_id=A123&client_secret=secret")
		if err != nil {
			t.Fatalf("GET %s: %v", method, err)
		}
		var payload map[string]any
		decodeErr := json.NewDecoder(response.Body).Decode(&payload)
		response.Body.Close()
		if decodeErr != nil {
			t.Fatalf("decode %s response: %v", method, decodeErr)
		}
		if response.StatusCode != http.StatusOK || payload["error"] != "invalid_arguments" {
			t.Fatalf("%s response = %d %#v", method, response.StatusCode, payload)
		}
	}
}

func TestAppCompatibilityRateLimitResponse(t *testing.T) {
	env := setupAppMethodTestEnvironment(t)
	for range 205 {
		status, headers, payload := callAppMethod(t, env, "api.test", nil, "")
		if status == http.StatusTooManyRequests {
			if payload["error"] != "ratelimited" || headers.Get("Retry-After") == "" {
				t.Fatalf("limited response = %d %#v headers=%#v", status, payload, headers)
			}
			return
		}
		if status != http.StatusOK || payload["ok"] != true {
			t.Fatalf("admitted response = %d %#v", status, payload)
		}
	}
	t.Fatal("api.test did not enforce the shared minute limit")
}

func TestAppCompatibilityRejectsTrailingJSONValues(t *testing.T) {
	env := setupAppMethodTestEnvironment(t)
	req, err := http.NewRequest(http.MethodPost, env.http.URL+"/api/api.test", strings.NewReader(`{"one":1}{"two":2}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := env.http.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer response.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if payload["ok"] != false || payload["error"] != "invalid_arguments" {
		t.Fatalf("response = %#v", payload)
	}
}

func TestAppAuthorizeRejectsOversizedForm(t *testing.T) {
	env := setupAppMethodTestEnvironment(t)
	body := "client_id=" + strings.Repeat("x", appAuthorizeRequestLimit)
	request, err := http.NewRequest(http.MethodPost, env.http.URL+"/oauth/v2/authorize", strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := env.http.Client().Do(request)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.StatusCode)
	}
}

func TestAppApprovalRequiresSameOriginEvidence(t *testing.T) {
	env := setupAppMethodTestEnvironment(t)
	request := httptest.NewRequest(http.MethodPost, "http://local.test/oauth/v2/authorize", nil)
	if env.server.appApprovalIsSameOrigin(request) {
		t.Fatal("approval accepted without Origin or Referer")
	}
	request.Header.Set("Origin", "https://evil.example.test")
	if env.server.appApprovalIsSameOrigin(request) {
		t.Fatal("approval accepted a cross-origin Origin")
	}
	request.Header.Del("Origin")
	request.Header.Set("Referer", "https://chat.example.test/oauth/v2/authorize")
	if !env.server.appApprovalIsSameOrigin(request) {
		t.Fatal("approval rejected the canonical same-origin Referer")
	}
}

func TestAppCompatibilityRoutesStayAbsentWhenDisabled(t *testing.T) {
	s := setupHTTPServerTestServer(t, config.AuthConfig{})
	s.setupAppCompatibilityRoutes()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/api.test", strings.NewReader(""))
	s.router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}
