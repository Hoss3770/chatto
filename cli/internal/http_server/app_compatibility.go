package http_server

import (
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chattocorp/chatto/cli/internal/appcompat"
	"github.com/chattocorp/chatto/cli/internal/core"
	"github.com/gin-gonic/gin"
)

const (
	appCompatibilityRequestLimit = 1 << 20
	appAuthorizeRequestLimit     = 64 << 10
)

var appAuthorizeTemplate = template.Must(template.New("app-authorize").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Install {{.AppName}} · Chatto</title>
<style>
:root{color-scheme:light dark;font-family:Inter,ui-sans-serif,system-ui,sans-serif;background:#f5f5f4;color:#1c1917}*{box-sizing:border-box}body{min-height:100vh;margin:0;display:grid;place-items:center;padding:24px}.card{width:min(560px,100%);background:#fff;border:1px solid #e7e5e4;border-radius:18px;box-shadow:0 20px 60px #1c191712;padding:30px}h1{font-size:1.5rem;margin:0 0 8px}.muted{color:#57534e}.scope{border:1px solid #e7e5e4;border-radius:10px;padding:10px 12px;margin:8px 0;font-family:ui-monospace,monospace;font-size:.875rem}.origin{word-break:break-all;background:#f5f5f4;border-radius:8px;padding:10px}.actions{display:flex;gap:10px;justify-content:flex-end;margin-top:24px}button{border:0;border-radius:9px;padding:10px 16px;font:inherit;font-weight:650;cursor:pointer}.deny{background:#e7e5e4;color:#292524}.approve{background:#2563eb;color:white}@media(prefers-color-scheme:dark){:root{background:#0c0a09;color:#fafaf9}.card{background:#1c1917;border-color:#44403c}.muted{color:#d6d3d1}.scope{border-color:#44403c}.origin{background:#292524}.deny{background:#44403c;color:#fafaf9}}
</style>
</head>
<body><main class="card">
<p class="muted">Chatto app installation</p>
<h1>Install {{.AppName}}?</h1>
{{if .Description}}<p>{{.Description}}</p>{{end}}
<p class="muted">This app will use the managed bot <strong>{{.BotName}}</strong> and receive the selected access.</p>
{{range .Scopes}}<div class="scope">{{.}}</div>{{else}}<p class="scope">No target scopes requested</p>{{end}}
<p class="muted">After approval, Chatto sends an authorization code to:</p><p class="origin">{{.RedirectOrigin}}</p>
<form method="post" action="/oauth/v2/authorize">
{{range $key, $value := .Fields}}<input type="hidden" name="{{$key}}" value="{{$value}}">{{end}}
<div class="actions"><button class="deny" name="decision" value="deny">Cancel</button><button class="approve" name="decision" value="approve">Install app</button></div>
</form>
</main></body></html>`))

type appAuthorizePage struct {
	AppName        string
	Description    string
	BotName        string
	RedirectOrigin string
	Scopes         []string
	Fields         map[string]string
}

func (s *HTTPServer) setupAppCompatibilityRoutes() {
	if !s.config.Apps.Enabled {
		return
	}
	s.router.Any("/api/:appMethod", s.handleAppCompatibilityMethod)
	s.router.GET("/oauth/v2/authorize", s.handleAppAuthorize)
	s.router.POST("/oauth/v2/authorize", s.handleAppAuthorize)
}

func (s *HTTPServer) handleAppCompatibilityMethod(c *gin.Context) {
	prepareAppCompatibilityResponse(c)
	method := c.Param("appMethod")
	args, err := appCompatibilityArguments(c)
	if err != nil {
		writeAppCompatibilityError(c, http.StatusOK, "invalid_arguments")
		return
	}
	if method == "api.test" {
		if err := s.consumeAppMethodRate(c, "", method, 100); err != nil {
			writeAppRateLimit(c, err)
			return
		}
		responseArgs := publicAppArguments(args)
		if requestedError := args["error"]; requestedError != "" {
			response := gin.H{"ok": false, "error": requestedError}
			if len(responseArgs) > 0 {
				response["args"] = responseArgs
			}
			c.JSON(http.StatusOK, response)
			return
		}
		response := gin.H{"ok": true}
		if len(responseArgs) > 0 {
			response["args"] = responseArgs
		}
		c.JSON(http.StatusOK, response)
		return
	}
	if method == "oauth.v2.access" {
		if c.Request.Method != http.MethodPost || c.Request.URL.Query().Has("client_secret") {
			writeAppCompatibilityError(c, http.StatusOK, "invalid_arguments")
			return
		}
		s.handleAppOAuthExchange(c, args)
		return
	}
	if method != "auth.test" && method != "auth.revoke" && method != "apps.uninstall" {
		writeAppCompatibilityError(c, http.StatusOK, "not_implemented")
		return
	}
	if method == "apps.uninstall" && (c.Request.Method != http.MethodPost || c.Request.URL.Query().Has("client_secret")) {
		writeAppCompatibilityError(c, http.StatusOK, "invalid_arguments")
		return
	}

	token := appCompatibilityToken(c, args)
	if token == "" {
		writeAppCompatibilityError(c, http.StatusOK, "not_authed")
		return
	}
	credential, err := s.core.ValidateAppBotToken(c.Request.Context(), token)
	if err != nil {
		writeAppCompatibilityError(c, http.StatusOK, "invalid_auth")
		return
	}
	limit := uint32(60)
	switch method {
	case "auth.test":
		limit = 100
	case "auth.revoke":
		limit = 50
	case "apps.uninstall":
		limit = 10
	}
	if err := s.consumeAppMethodRate(c, credential.App.ID, method, limit); err != nil {
		writeAppRateLimit(c, err)
		return
	}

	switch method {
	case "auth.test":
		workspaceID, err := s.core.AppWorkspaceID(c.Request.Context())
		if err != nil {
			writeAppCompatibilityError(c, http.StatusOK, "internal_error")
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"ok": true, "url": strings.TrimRight(s.config.Webserver.URL, "/") + "/",
			"team": s.core.AppWorkspaceName(), "team_id": workspaceID,
			"user": credential.Bot.GetDisplayName(), "user_id": credential.Bot.GetId(),
			"bot_id": core.AppBotAlias(credential.Bot.GetId()),
		})
	case "auth.revoke":
		test, _ := strconv.ParseBool(args["test"])
		revoked, err := s.core.RevokeAppBotToken(c.Request.Context(), token, test)
		if err != nil {
			writeAppCompatibilityError(c, http.StatusOK, "invalid_auth")
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "revoked": revoked})
	case "apps.uninstall":
		clientID, clientSecret := args["client_id"], args["client_secret"]
		if clientID == "" || clientSecret == "" {
			writeAppCompatibilityError(c, http.StatusOK, "missing_argument")
			return
		}
		if err := s.core.UninstallAppWithCredentials(c.Request.Context(), token, clientID, clientSecret); err != nil {
			errorCode := "bad_client_secret"
			if errors.Is(err, core.ErrAuthTokenNotFound) {
				errorCode = "invalid_auth"
			} else if credential.App.ID != clientID {
				errorCode = "client_id_token_mismatch"
			}
			writeAppCompatibilityError(c, http.StatusOK, errorCode)
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

func (s *HTTPServer) handleAppOAuthExchange(c *gin.Context, args map[string]string) {
	clientID, clientSecret := args["client_id"], args["client_secret"]
	if basicID, basicSecret, ok := c.Request.BasicAuth(); ok {
		clientID, clientSecret = basicID, basicSecret
	}
	code, redirectURI := args["code"], args["redirect_uri"]
	if clientID == "" || clientSecret == "" || code == "" || redirectURI == "" {
		writeAppCompatibilityError(c, http.StatusOK, "missing_argument")
		return
	}
	if grantType := args["grant_type"]; grantType != "" && grantType != "authorization_code" {
		writeAppCompatibilityError(c, http.StatusOK, "invalid_grant_type")
		return
	}
	app, err := s.core.GetAppOAuthClient(c.Request.Context(), clientID)
	if err != nil {
		writeAppCompatibilityError(c, http.StatusOK, "invalid_client_id")
		return
	}
	if err := s.consumeAppMethodRate(c, app.ID, "oauth.v2.access", 100); err != nil {
		writeAppRateLimit(c, err)
		return
	}
	result, err := s.core.ExchangeAppOAuthCode(c.Request.Context(), code, clientID, clientSecret, redirectURI, s.core.AppWorkspaceName())
	if err != nil {
		switch {
		case errors.Is(err, core.ErrAppClientSecret):
			writeAppCompatibilityError(c, http.StatusOK, "bad_client_secret")
		case errors.Is(err, core.ErrAppOAuthGrant):
			writeAppCompatibilityError(c, http.StatusOK, "bad_redirect_uri")
		case errors.Is(err, core.ErrAppOAuthCodeInvalid):
			writeAppCompatibilityError(c, http.StatusOK, "invalid_code")
		default:
			writeAppCompatibilityError(c, http.StatusOK, "internal_error")
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok": true, "access_token": result.AccessToken, "token_type": result.TokenType,
		"scope": result.Scope, "bot_user_id": result.BotUserID, "app_id": result.AppID,
		"team":       gin.H{"id": result.WorkspaceID, "name": result.WorkspaceName},
		"enterprise": nil, "is_enterprise_install": false,
	})
}

func (s *HTTPServer) handleAppAuthorize(c *gin.Context) {
	prepareAppCompatibilityResponse(c)
	if c.Request.Method == http.MethodPost {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, appAuthorizeRequestLimit)
		if err := c.Request.ParseForm(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid_request"})
			return
		}
	}
	params := appAuthorizeParameters(c)
	state, manifest, redirectURI, errCode := s.validateAppAuthorize(c, params)
	if errCode != "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": errCode})
		return
	}
	if len(splitAppScopes(params["scope"])) == 0 {
		params["scope"] = strings.Join(state.RequestedBotScopes, ",")
	}
	credential, authenticated, err := s.oauthCookieCredential(c)
	if err != nil {
		writeAuthenticationUnavailable(c)
		return
	}
	if !authenticated {
		target := c.Request.URL.Path
		if c.Request.Method == http.MethodPost {
			query := make(url.Values, len(params))
			for key, value := range params {
				if value != "" {
					query.Set(key, value)
				}
			}
			if encoded := query.Encode(); encoded != "" {
				target += "?" + encoded
			}
		} else if c.Request.URL.RawQuery != "" {
			target += "?" + c.Request.URL.RawQuery
		}
		status := http.StatusTemporaryRedirect
		if c.Request.Method == http.MethodPost {
			status = http.StatusSeeOther
		}
		c.Redirect(status, "/login?redirect="+url.QueryEscape(target))
		return
	}
	if c.Request.Method == http.MethodPost {
		if !s.appApprovalIsSameOrigin(c.Request) {
			c.JSON(http.StatusForbidden, gin.H{"ok": false, "error": "access_denied"})
			return
		}
		if c.PostForm("decision") != "approve" {
			redirectAppAuthorization(c, redirectURI, params["state"], "", "access_denied")
			return
		}
		code, err := s.core.CreateAppOAuthCode(c.Request.Context(), credential.auth.UserID, state.ID, redirectURI, splitAppScopes(params["scope"]), splitAppScopes(params["user_scope"]))
		if err != nil {
			if errors.Is(err, core.ErrPermissionDenied) {
				redirectAppAuthorization(c, redirectURI, params["state"], "", "access_denied")
				return
			}
			c.JSON(http.StatusServiceUnavailable, gin.H{"ok": false, "error": "temporarily_unavailable"})
			return
		}
		redirectAppAuthorization(c, redirectURI, params["state"], code, "")
		return
	}
	redirectOrigin := redirectURI
	if parsed, err := url.Parse(redirectURI); err == nil {
		redirectOrigin = parsed.Scheme + "://" + parsed.Host
	}
	fields := map[string]string{
		"client_id": params["client_id"], "redirect_uri": redirectURI,
		"scope": params["scope"], "user_scope": params["user_scope"],
		"state": params["state"], "response_type": "code",
	}
	scopes := append(splitAppScopes(params["scope"]), splitAppScopes(params["user_scope"])...)
	sort.Strings(scopes)
	botName := manifest.Summary.BotDisplayName
	if botName == "" {
		botName = manifest.Summary.Name
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	if err := appAuthorizeTemplate.Execute(c.Writer, appAuthorizePage{
		AppName: manifest.Summary.Name, Description: manifest.Summary.Description,
		BotName: botName, RedirectOrigin: redirectOrigin, Scopes: scopes, Fields: fields,
	}); err != nil {
		s.logger.Error("Failed to render app authorization page", "error", err)
	}
}

func (s *HTTPServer) validateAppAuthorize(c *gin.Context, params map[string]string) (core.AppState, appcompat.Manifest, string, string) {
	if responseType := params["response_type"]; responseType != "" && responseType != "code" {
		return core.AppState{}, appcompat.Manifest{}, "", "unsupported_response_type"
	}
	clientID := params["client_id"]
	if clientID == "" {
		return core.AppState{}, appcompat.Manifest{}, "", "invalid_client_id"
	}
	state, err := s.core.GetAppOAuthClient(c.Request.Context(), clientID)
	if err != nil {
		return core.AppState{}, appcompat.Manifest{}, "", "invalid_client_id"
	}
	manifest := appcompat.ParseManifest(state.ManifestJSON)
	redirects := appcompat.RedirectURLs(manifest)
	redirectURI := params["redirect_uri"]
	if redirectURI == "" && len(redirects) == 1 {
		redirectURI = redirects[0]
	}
	if redirectURI == "" || !containsAppString(redirects, redirectURI) {
		return core.AppState{}, appcompat.Manifest{}, "", "bad_redirect_uri"
	}
	botScopes := splitAppScopes(params["scope"])
	userScopes := splitAppScopes(params["user_scope"])
	if (len(botScopes) > 0 && !appScopeSubset(botScopes, state.RequestedBotScopes)) || len(userScopes) > 0 {
		return core.AppState{}, appcompat.Manifest{}, "", "invalid_scope"
	}
	return state, manifest, redirectURI, ""
}

// appApprovalIsSameOrigin requires browser-origin evidence for the cookie-bound
// approval POST. Origin is preferred. Referer is the fallback for user agents
// that omit Origin on same-origin form submissions.
func (s *HTTPServer) appApprovalIsSameOrigin(request *http.Request) bool {
	if strings.TrimSpace(request.Header.Get("Origin")) != "" {
		return s.requestIsSameOrigin(request)
	}
	referer, err := url.Parse(strings.TrimSpace(request.Referer()))
	if err != nil || referer.Scheme == "" || referer.Host == "" {
		return false
	}
	check := request.Clone(request.Context())
	check.Header = request.Header.Clone()
	check.Header.Set("Origin", referer.Scheme+"://"+referer.Host)
	return s.requestIsSameOrigin(check)
}

func appAuthorizeParameters(c *gin.Context) map[string]string {
	keys := []string{"client_id", "redirect_uri", "scope", "user_scope", "state", "response_type"}
	params := make(map[string]string, len(keys))
	for _, key := range keys {
		if c.Request.Method == http.MethodPost {
			params[key] = c.PostForm(key)
		} else {
			params[key] = c.Query(key)
		}
	}
	return params
}

func redirectAppAuthorization(c *gin.Context, redirectURI, state, code, errorCode string) {
	parsed, err := url.Parse(redirectURI)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "bad_redirect_uri"})
		return
	}
	query := parsed.Query()
	if code != "" {
		query.Set("code", code)
	}
	if errorCode != "" {
		query.Set("error", errorCode)
	}
	if state != "" {
		query.Set("state", state)
	}
	parsed.RawQuery = query.Encode()
	c.Redirect(http.StatusSeeOther, parsed.String())
}

func appCompatibilityArguments(c *gin.Context) (map[string]string, error) {
	args := make(map[string]string)
	for key, values := range c.Request.URL.Query() {
		if len(values) > 0 {
			args[key] = values[len(values)-1]
		}
	}
	if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
		return args, nil
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, appCompatibilityRequestLimit)
	if strings.HasPrefix(c.ContentType(), "application/json") {
		decoder := json.NewDecoder(c.Request.Body)
		decoder.UseNumber()
		var document map[string]any
		if err := decoder.Decode(&document); err != nil {
			return nil, err
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			if err == nil {
				return nil, errors.New("request body contains multiple JSON values")
			}
			return nil, err
		}
		for key, value := range document {
			switch value := value.(type) {
			case string:
				args[key] = value
			case bool:
				args[key] = strconv.FormatBool(value)
			case json.Number:
				args[key] = value.String()
			default:
				encoded, err := json.Marshal(value)
				if err != nil {
					return nil, err
				}
				args[key] = string(encoded)
			}
		}
		return args, nil
	}
	if err := c.Request.ParseForm(); err != nil {
		return nil, err
	}
	for key, values := range c.Request.PostForm {
		if len(values) > 0 {
			args[key] = values[len(values)-1]
		}
	}
	return args, nil
}

func appCompatibilityToken(c *gin.Context, args map[string]string) string {
	if authorization := c.GetHeader("Authorization"); strings.HasPrefix(strings.ToLower(authorization), "bearer ") {
		return strings.TrimSpace(authorization[len("Bearer "):])
	}
	return args["token"]
}

func publicAppArguments(args map[string]string) map[string]string {
	result := make(map[string]string)
	for key, value := range args {
		if key != "token" {
			result[key] = value
		}
	}
	return result
}

func (s *HTTPServer) consumeAppMethodRate(c *gin.Context, appID, method string, limit uint32) error {
	return s.core.ConsumeAppRateLimit(c.Request.Context(), appID, method, limit, time.Now().UTC())
}

func writeAppRateLimit(c *gin.Context, err error) {
	if rateError, ok := errors.AsType[*core.AppRateLimitError](err); ok {
		retry := max(1, int(math.Ceil(rateError.RetryAfter.Seconds())))
		c.Header("Retry-After", strconv.Itoa(retry))
		writeAppCompatibilityError(c, http.StatusTooManyRequests, "ratelimited")
		return
	}
	writeAppCompatibilityError(c, http.StatusServiceUnavailable, "service_unavailable")
}

func prepareAppCompatibilityResponse(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Frame-Options", "DENY")
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
}

func writeAppCompatibilityError(c *gin.Context, status int, code string) {
	prepareAppCompatibilityResponse(c)
	c.JSON(status, gin.H{"ok": false, "error": code})
}

func splitAppScopes(value string) []string {
	set := make(map[string]struct{})
	for _, scope := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' }) {
		if scope != "" {
			set[scope] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for scope := range set {
		result = append(result, scope)
	}
	sort.Strings(result)
	return result
}

func appScopeSubset(requested, allowed []string) bool {
	for _, scope := range requested {
		if !containsAppString(allowed, scope) {
			return false
		}
	}
	return true
}

func containsAppString(values []string, value string) bool {
	return slices.Contains(values, value)
}
