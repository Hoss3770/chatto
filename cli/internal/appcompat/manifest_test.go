package appcompat

import (
	"os"
	"strings"
	"testing"
)

func TestParseManifestJananHRFixture(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("testdata/janan-hr-manifest.yaml")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	manifest := ParseManifest(string(raw))
	if !manifest.Valid() {
		t.Fatalf("manifest invalid: %#v", manifest.Issues)
	}
	if manifest.Summary.Name != "Janan HR" || manifest.Summary.BotDisplayName != "Janan HR" {
		t.Fatalf("summary names = %q, %q", manifest.Summary.Name, manifest.Summary.BotDisplayName)
	}
	if got := RedirectURLs(manifest); len(got) != 1 || got[0] != "https://hr.example.test/oauth/callback" {
		t.Fatalf("RedirectURLs = %#v", got)
	}
	if got := manifest.Summary.ExternalOrigins; len(got) != 1 || got[0] != "https://hr.example.test" {
		t.Fatalf("ExternalOrigins = %#v", got)
	}
	if !strings.Contains(manifest.CanonicalJSON, `"x-janan"`) || !strings.Contains(manifest.CanonicalJSON, `"approval_steps": 2`) {
		t.Fatalf("unknown keys not preserved:\n%s", manifest.CanonicalJSON)
	}
	if manifest.Summary.UnsupportedContracts == 0 {
		t.Fatal("unsupported contract count = 0, want honest compatibility summary")
	}
}

func TestParseManifestPreservesUnknownURLLikeKeyWithoutApplyingURLRules(t *testing.T) {
	t.Parallel()
	manifest := ParseManifest(`{"display_information":{"name":"App"},"x-extension":{"callback_url":"relative/value"}}`)
	if !manifest.Valid() || !strings.Contains(manifest.CanonicalJSON, `"callback_url": "relative/value"`) {
		t.Fatalf("manifest = %#v", manifest)
	}
}

func TestParseManifestCanonicalizesExternalOrigins(t *testing.T) {
	t.Parallel()
	manifest := ParseManifest(`{"display_information":{"name":"App"},"oauth_config":{"redirect_urls":["https://APP.EXAMPLE.TEST:443/callback"]}}`)
	if !manifest.Valid() {
		t.Fatalf("issues = %#v", manifest.Issues)
	}
	if got := manifest.Summary.ExternalOrigins; len(got) != 1 || got[0] != "https://app.example.test" {
		t.Fatalf("ExternalOrigins = %#v", got)
	}
}

func TestParseManifestJSONAndYAMLCanonicalizeIdentically(t *testing.T) {
	t.Parallel()
	jsonManifest := `{"display_information":{"name":"App"},"x-extra":{"enabled":true}}`
	yamlManifest := "display_information:\n  name: App\nx-extra:\n  enabled: true\n"
	jsonResult := ParseManifest(jsonManifest)
	yamlResult := ParseManifest(yamlManifest)
	if !jsonResult.Valid() || !yamlResult.Valid() {
		t.Fatalf("valid = %t, %t; issues = %#v, %#v", jsonResult.Valid(), yamlResult.Valid(), jsonResult.Issues, yamlResult.Issues)
	}
	if jsonResult.CanonicalJSON != yamlResult.CanonicalJSON {
		t.Fatalf("canonical JSON differs:\nJSON:\n%s\nYAML:\n%s", jsonResult.CanonicalJSON, yamlResult.CanonicalJSON)
	}
}

func TestParseManifestReportsSyntaxTypeAndRequiredIssues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		code string
	}{
		{name: "duplicate YAML key", raw: "display_information:\n  name: One\n  name: Two\n", code: "duplicate_key"},
		{name: "duplicate JSON key", raw: `{"display_information":{"name":"One","name":"Two"}}`, code: "duplicate_key"},
		{name: "invalid root", raw: "- one\n- two\n", code: "invalid_root"},
		{name: "missing required name", raw: `{"display_information":{}}`, code: "required"},
		{name: "wrong name type", raw: `{"display_information":{"name":42}}`, code: "invalid_type"},
		{name: "invalid redirect URL", raw: `{"display_information":{"name":"App"},"oauth_config":{"redirect_urls":["javascript:alert(1)"]}}`, code: "invalid_url"},
		{name: "insecure public redirect URL", raw: `{"display_information":{"name":"App"},"oauth_config":{"redirect_urls":["http://app.example.test/callback"]}}`, code: "invalid_url"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			manifest := ParseManifest(test.raw)
			if manifest.Valid() {
				t.Fatalf("manifest unexpectedly valid: %#v", manifest)
			}
			for _, issue := range manifest.Issues {
				if issue.Code == test.code {
					return
				}
			}
			t.Fatalf("issues = %#v, want code %q", manifest.Issues, test.code)
		})
	}
}

func TestParseManifestRejectsUnknownScope(t *testing.T) {
	t.Parallel()
	manifest := ParseManifest(`{"display_information":{"name":"App"},"oauth_config":{"scopes":{"bot":["chat:writ"]}}}`)
	if manifest.Valid() {
		t.Fatal("manifest with unknown scope is valid")
	}
	for _, issue := range manifest.Issues {
		if issue.Code == "unknown_scope" {
			return
		}
	}
	t.Fatalf("issues = %#v", manifest.Issues)
}

func TestParseManifestRejectsUserScopesUntilUserTokensAreSupported(t *testing.T) {
	t.Parallel()
	manifest := ParseManifest(`{"display_information":{"name":"App"},"oauth_config":{"scopes":{"user":["users:read"]}}}`)
	if manifest.Valid() {
		t.Fatal("manifest with user scopes is valid")
	}
	for _, issue := range manifest.Issues {
		if issue.Code == "user_scopes_unsupported" && issue.Pointer == "/oauth_config/scopes/user" {
			return
		}
	}
	t.Fatalf("issues = %#v", manifest.Issues)
}

func TestParseManifestAllowsHTTPForLoopbackDevelopmentURL(t *testing.T) {
	t.Parallel()
	manifest := ParseManifest(`{"display_information":{"name":"App"},"oauth_config":{"redirect_urls":["http://app.localhost:3000/callback"]}}`)
	if !manifest.Valid() {
		t.Fatalf("issues = %#v", manifest.Issues)
	}
}

func TestParseManifestRequiresCustomProviderOptionsPerProvider(t *testing.T) {
	t.Parallel()
	manifest := ParseManifest(`{
		"display_information":{"name":"App"},
		"external_auth_providers":{
			"custom":{"provider_type":"CUSTOM","options":{}},
			"google":{"provider_type":"GOOGLE","options":{}}
		}
	}`)
	if manifest.Valid() {
		t.Fatal("manifest valid without required custom provider options")
	}
	var requiredPointers []string
	for _, issue := range manifest.Issues {
		if issue.Code == "required" {
			requiredPointers = append(requiredPointers, issue.Pointer)
		}
	}
	if len(requiredPointers) == 0 {
		t.Fatalf("required pointers = %#v", requiredPointers)
	}
	for _, pointer := range requiredPointers {
		if strings.Contains(pointer, "/google/options/authorization_url") ||
			strings.Contains(pointer, "/google/options/provider_name") ||
			strings.Contains(pointer, "/google/options/token_url") {
			t.Fatalf("non-custom provider incorrectly required custom-only options: %#v", requiredPointers)
		}
	}
}

func TestParseManifestRejectsNormalizedDocumentAboveMaximumSize(t *testing.T) {
	t.Parallel()
	raw := `{"display_information":{"name":"App"},"x":[` + strings.Repeat("0,", 250_000) + `0]}`
	if len(raw) >= MaxManifestBytes {
		t.Fatalf("test input size = %d", len(raw))
	}
	manifest := ParseManifest(raw)
	if manifest.Valid() || len(manifest.Issues) != 1 || manifest.Issues[0].Code != "normalized_too_large" {
		t.Fatalf("issues = %#v", manifest.Issues)
	}
}

func TestParseManifestRejectsDocumentAboveMaximumSize(t *testing.T) {
	t.Parallel()
	manifest := ParseManifest(strings.Repeat(" ", MaxManifestBytes+1))
	if manifest.Valid() || len(manifest.Issues) != 1 || manifest.Issues[0].Code != "too_large" {
		t.Fatalf("manifest = %#v", manifest)
	}
}

func TestParseManifestRejectsTrailingJSONValue(t *testing.T) {
	t.Parallel()
	manifest := ParseManifest(`{"display_information":{"name":"App"}} {}`)
	if manifest.Valid() || len(manifest.Issues) != 1 || manifest.Issues[0].Code != "invalid_json" {
		t.Fatalf("manifest = %#v", manifest)
	}
}
