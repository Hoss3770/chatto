// Package appcompat implements the additive external app protocol adapter. It
// is the only package that combines target manifest and wire semantics with
// Chatto app-domain operations.
package appcompat

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// ManifestIssueSeverity identifies whether a manifest issue blocks storage.
type ManifestIssueSeverity string

const (
	ManifestIssueError   ManifestIssueSeverity = "error"
	ManifestIssueWarning ManifestIssueSeverity = "warning"

	// MaxManifestBytes is the largest complete JSON or YAML manifest that the
	// compatibility edge accepts.
	MaxManifestBytes = 1 << 20
)

// ManifestIssue identifies one syntax, validation, or support issue.
type ManifestIssue struct {
	Pointer  string
	Line     int
	Code     string
	Message  string
	Severity ManifestIssueSeverity
}

// ManifestSummary contains the safe operator review fields extracted from a
// complete manifest.
type ManifestSummary struct {
	Name                 string
	Description          string
	BotDisplayName       string
	BotScopes            []string
	UserScopes           []string
	Events               []string
	Commands             []string
	ExternalOrigins      []string
	ImplementedContracts int
	PartialContracts     int
	UnsupportedContracts int
}

// Manifest is a parsed document with deterministic canonical JSON. Unknown
// nested keys remain in Document and round-trip through CanonicalJSON.
type Manifest struct {
	Document      map[string]any
	CanonicalJSON string
	Summary       ManifestSummary
	Issues        []ManifestIssue
}

// Valid reports whether the manifest has no blocking issue.
func (m Manifest) Valid() bool {
	for _, issue := range m.Issues {
		if issue.Severity == ManifestIssueError {
			return false
		}
	}
	return true
}

type manifestContract struct {
	Path            string
	Type            string
	Required        bool
	Requirement     string
	AllowedLiterals []string
	MinLength       int
	MaxLength       int
	MinItems        int
	MaxItems        int
	MinKeyLength    int
	MaxKeyLength    int
	MaxItemLength   int
	Status          string
	Reason          string
}

type manifestSlot struct {
	value   any
	present bool
	pointer string
	line    int
}

var dynamicManifestRoots = map[string]struct{}{
	"datastores":              {},
	"types":                   {},
	"workflows":               {},
	externalAuthProvidersRoot: {},
}

const externalAuthProvidersRoot = "external_auth_providers"

// ParseManifest parses JSON or YAML, validates every known registry path, and
// preserves unknown values for export.
func ParseManifest(raw string) Manifest {
	manifest := Manifest{}
	if len(raw) > MaxManifestBytes {
		manifest.Issues = []ManifestIssue{{
			Code: "too_large", Message: "The manifest is larger than 1 MiB.", Severity: ManifestIssueError,
		}}
		return manifest
	}
	document, syntaxIssue := decodeManifest(raw)
	if syntaxIssue != nil {
		manifest.Issues = []ManifestIssue{*syntaxIssue}
		return manifest
	}
	manifest.Document = document
	canonical, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		manifest.Issues = []ManifestIssue{{Code: "encode_failed", Message: "The manifest could not be normalized.", Severity: ManifestIssueError}}
		return manifest
	}
	if len(canonical)+1 > MaxManifestBytes {
		manifest.Issues = []ManifestIssue{{
			Code: "normalized_too_large", Message: "The normalized manifest is larger than 1 MiB.", Severity: ManifestIssueError,
		}}
		return manifest
	}
	manifest.CanonicalJSON = string(canonical) + "\n"
	manifest.Issues = validateManifest(document)
	manifest.Summary = summarizeManifest(document)
	for _, scope := range append(append([]string(nil), manifest.Summary.BotScopes...), manifest.Summary.UserScopes...) {
		if _, known := knownAppScopes[scope]; !known {
			manifest.Issues = append(manifest.Issues, ManifestIssue{
				Pointer: "/oauth_config/scopes", Code: "unknown_scope",
				Message: fmt.Sprintf("The app scope %q is not in the compatibility registry.", scope), Severity: ManifestIssueError,
			})
		}
	}
	if len(manifest.Summary.UserScopes) > 0 {
		manifest.Issues = append(manifest.Issues, ManifestIssue{
			Pointer: "/oauth_config/scopes/user", Code: "user_scopes_unsupported",
			Message: "Chatto does not support app user tokens in this release.", Severity: ManifestIssueError,
		})
	}
	countManifestContracts(document, &manifest.Summary)
	return manifest
}

func decodeManifest(raw string) (map[string]any, *ManifestIssue) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, &ManifestIssue{Code: "empty_manifest", Message: "The manifest is empty.", Severity: ManifestIssueError}
	}
	if strings.HasPrefix(raw, "{") {
		return decodeJSONManifest(raw)
	}
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(raw), &node); err != nil {
		line := yamlErrorLine(err.Error())
		return nil, &ManifestIssue{Line: line, Code: "invalid_yaml", Message: err.Error(), Severity: ManifestIssueError}
	}
	if len(node.Content) != 1 || node.Content[0].Kind != yaml.MappingNode {
		return nil, &ManifestIssue{Line: node.Line, Code: "invalid_root", Message: "The manifest root must be an object.", Severity: ManifestIssueError}
	}
	value, issue := yamlNodeValue(node.Content[0], "")
	if issue != nil {
		return nil, issue
	}
	document, ok := value.(map[string]any)
	if !ok {
		return nil, &ManifestIssue{Code: "invalid_root", Message: "The manifest root must be an object.", Severity: ManifestIssueError}
	}
	return document, nil
}

func decodeJSONManifest(raw string) (map[string]any, *ManifestIssue) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	value, issue := decodeJSONValue(decoder, "")
	if issue != nil {
		return nil, issue
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return nil, &ManifestIssue{Code: "invalid_json", Message: err.Error(), Severity: ManifestIssueError}
		}
		return nil, &ManifestIssue{Code: "invalid_json", Message: "The JSON document has trailing values.", Severity: ManifestIssueError}
	}
	document, ok := value.(map[string]any)
	if !ok {
		return nil, &ManifestIssue{Code: "invalid_root", Message: "The manifest root must be an object.", Severity: ManifestIssueError}
	}
	return document, nil
}

func decodeJSONValue(decoder *json.Decoder, pointer string) (any, *ManifestIssue) {
	token, err := decoder.Token()
	if err != nil {
		return nil, &ManifestIssue{Pointer: pointer, Code: "invalid_json", Message: err.Error(), Severity: ManifestIssueError}
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return token, nil
	}
	switch delimiter {
	case '{':
		result := make(map[string]any)
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, &ManifestIssue{Pointer: pointer, Code: "invalid_json", Message: err.Error(), Severity: ManifestIssueError}
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, &ManifestIssue{Pointer: pointer, Code: "invalid_key", Message: "Manifest object keys must be strings.", Severity: ManifestIssueError}
			}
			childPointer := pointer + "/" + escapeJSONPointer(key)
			if _, duplicate := result[key]; duplicate {
				return nil, &ManifestIssue{Pointer: childPointer, Code: "duplicate_key", Message: "The manifest repeats this key.", Severity: ManifestIssueError}
			}
			value, issue := decodeJSONValue(decoder, childPointer)
			if issue != nil {
				return nil, issue
			}
			result[key] = value
		}
		if _, err := decoder.Token(); err != nil {
			return nil, &ManifestIssue{Pointer: pointer, Code: "invalid_json", Message: err.Error(), Severity: ManifestIssueError}
		}
		return result, nil
	case '[':
		result := make([]any, 0)
		for index := 0; decoder.More(); index++ {
			value, issue := decodeJSONValue(decoder, pointer+"/"+strconv.Itoa(index))
			if issue != nil {
				return nil, issue
			}
			result = append(result, value)
		}
		if _, err := decoder.Token(); err != nil {
			return nil, &ManifestIssue{Pointer: pointer, Code: "invalid_json", Message: err.Error(), Severity: ManifestIssueError}
		}
		return result, nil
	default:
		return nil, &ManifestIssue{Pointer: pointer, Code: "invalid_json", Message: "The JSON document has an unexpected delimiter.", Severity: ManifestIssueError}
	}
}

func yamlNodeValue(node *yaml.Node, pointer string) (any, *ManifestIssue) {
	switch node.Kind {
	case yaml.MappingNode:
		result := make(map[string]any, len(node.Content)/2)
		for i := 0; i < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			if keyNode.Kind != yaml.ScalarNode || keyNode.Tag != "!!str" {
				return nil, &ManifestIssue{Pointer: pointer, Line: keyNode.Line, Code: "invalid_key", Message: "Manifest object keys must be strings.", Severity: ManifestIssueError}
			}
			key := keyNode.Value
			childPointer := pointer + "/" + escapeJSONPointer(key)
			if _, duplicate := result[key]; duplicate {
				return nil, &ManifestIssue{Pointer: childPointer, Line: keyNode.Line, Code: "duplicate_key", Message: "The manifest repeats this key.", Severity: ManifestIssueError}
			}
			value, issue := yamlNodeValue(node.Content[i+1], childPointer)
			if issue != nil {
				return nil, issue
			}
			result[key] = value
		}
		return result, nil
	case yaml.SequenceNode:
		result := make([]any, 0, len(node.Content))
		for index, child := range node.Content {
			value, issue := yamlNodeValue(child, pointer+"/"+strconv.Itoa(index))
			if issue != nil {
				return nil, issue
			}
			result = append(result, value)
		}
		return result, nil
	case yaml.ScalarNode:
		switch node.Tag {
		case "!!str":
			return node.Value, nil
		case "!!bool":
			value, err := strconv.ParseBool(node.Value)
			if err != nil {
				return nil, &ManifestIssue{Pointer: pointer, Line: node.Line, Code: "invalid_boolean", Message: err.Error(), Severity: ManifestIssueError}
			}
			return value, nil
		case "!!int":
			return json.Number(node.Value), nil
		case "!!float":
			return json.Number(node.Value), nil
		case "!!null":
			return nil, nil
		default:
			return node.Value, nil
		}
	case yaml.AliasNode:
		return nil, &ManifestIssue{Pointer: pointer, Line: node.Line, Code: "yaml_alias_unsupported", Message: "YAML aliases are not accepted in app manifests.", Severity: ManifestIssueError}
	default:
		return nil, &ManifestIssue{Pointer: pointer, Line: node.Line, Code: "invalid_yaml_node", Message: "The YAML value is not supported.", Severity: ManifestIssueError}
	}
}

func yamlErrorLine(message string) int {
	const marker = "line "
	_, value, ok := strings.Cut(message, marker)
	if !ok {
		return 0
	}
	end := strings.IndexAny(value, ": ,")
	if end >= 0 {
		value = value[:end]
	}
	line, _ := strconv.Atoi(value)
	return line
}

func validateManifest(document map[string]any) []ManifestIssue {
	var issues []ManifestIssue
	for _, contract := range manifestContracts {
		slots := contractSlots(document, contract.Path)
		if contract.Required {
			for _, missing := range requiredSlots(document, contract, slots) {
				issues = append(issues, ManifestIssue{Pointer: missing.pointer, Line: missing.line, Code: "required", Message: "This manifest value is required.", Severity: ManifestIssueError})
			}
		}
		warnedSupport := false
		for _, slot := range slots {
			if !slot.present {
				continue
			}
			issues = append(issues, validateManifestValue(contract, slot)...)
			if !warnedSupport && contract.Status != "implemented" {
				message := contract.Reason
				if message == "" {
					message = "Chatto preserves this value but does not apply its complete behavior."
				}
				issues = append(issues, ManifestIssue{Pointer: slot.pointer, Code: "compatibility_" + contract.Status, Message: message, Severity: ManifestIssueWarning})
				warnedSupport = true
			}
		}
	}
	issues = append(issues, validateManifestURLs(document)...)
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Pointer == issues[j].Pointer {
			return issues[i].Code < issues[j].Code
		}
		return issues[i].Pointer < issues[j].Pointer
	})
	return deduplicateManifestIssues(issues)
}

func contractSlots(document map[string]any, path string) []manifestSlot {
	segments := strings.Split(path, ".")
	root := segments[0]
	if _, dynamic := dynamicManifestRoots[root]; dynamic && len(segments) > 1 {
		container, ok := document[root].(map[string]any)
		if !ok {
			return nil
		}
		var slots []manifestSlot
		for key, value := range container {
			object, ok := value.(map[string]any)
			if !ok {
				continue
			}
			slots = append(slots, walkManifestPath(object, segments[1:], "/"+escapeJSONPointer(root)+"/"+escapeJSONPointer(key))...)
		}
		return slots
	}
	return walkManifestPath(document, segments, "")
}

func walkManifestPath(current any, segments []string, pointer string) []manifestSlot {
	if len(segments) == 0 {
		return []manifestSlot{{value: current, present: true, pointer: pointer}}
	}
	segment := segments[0]
	array := strings.HasSuffix(segment, "[]")
	key := strings.TrimSuffix(segment, "[]")
	object, ok := current.(map[string]any)
	if !ok {
		return nil
	}
	value, present := object[key]
	childPointer := pointer + "/" + escapeJSONPointer(key)
	if !present {
		if len(segments) > 1 {
			return nil
		}
		return []manifestSlot{{present: false, pointer: childPointer}}
	}
	if array {
		values, ok := value.([]any)
		if !ok {
			return []manifestSlot{{value: value, present: true, pointer: childPointer}}
		}
		if len(segments) == 1 {
			return []manifestSlot{{value: value, present: true, pointer: childPointer}}
		}
		var slots []manifestSlot
		for index, item := range values {
			slots = append(slots, walkManifestPath(item, segments[1:], childPointer+"/"+strconv.Itoa(index))...)
		}
		return slots
	}
	return walkManifestPath(value, segments[1:], childPointer)
}

func requiredSlots(document map[string]any, contract manifestContract, slots []manifestSlot) []manifestSlot {
	var missing []manifestSlot
	for _, slot := range slots {
		if !slot.present && requiredContractApplies(document, contract, slot.pointer) {
			missing = append(missing, slot)
		}
	}
	if len(slots) == 0 && contract.Requirement == "Required" {
		missing = append(missing, manifestSlot{pointer: pathJSONPointer(contract.Path)})
	}
	return missing
}

func requiredContractApplies(document map[string]any, contract manifestContract, pointer string) bool {
	if contract.Requirement == "Required" || strings.Contains(contract.Requirement, "for each") {
		return true
	}
	if strings.Contains(contract.Requirement, "provider_type") {
		providerPointer := pointer
		if index := strings.Index(providerPointer, "/options/"); index >= 0 {
			providerPointer = providerPointer[:index]
		}
		value, ok := valueAtJSONPointer(document, providerPointer+"/provider_type")
		return ok && value == "CUSTOM"
	}
	if strings.Contains(contract.Requirement, "using `functions`") {
		_, ok := document["functions"]
		return ok
	}
	if strings.Contains(contract.Requirement, "using `interactivity`") {
		_, ok := valueAtJSONPointer(document, "/settings/interactivity")
		return ok
	}
	parent := pointer
	if index := strings.LastIndex(parent, "/"); index >= 0 {
		parent = parent[:index]
	}
	_, ok := valueAtJSONPointer(document, parent)
	return ok
}

func validateManifestValue(contract manifestContract, slot manifestSlot) []ManifestIssue {
	issue := func(code, message string) []ManifestIssue {
		return []ManifestIssue{{Pointer: slot.pointer, Line: slot.line, Code: code, Message: message, Severity: ManifestIssueError}}
	}
	typeOK := false
	switch contract.Type {
	case "object":
		_, typeOK = slot.value.(map[string]any)
	case "array":
		_, typeOK = slot.value.([]any)
	case "string":
		_, typeOK = slot.value.(string)
	case "boolean":
		_, typeOK = slot.value.(bool)
	case "integer":
		switch value := slot.value.(type) {
		case json.Number:
			_, err := value.Int64()
			typeOK = err == nil
		case int, int32, int64, uint, uint32, uint64:
			typeOK = true
		}
	}
	if !typeOK {
		return issue("invalid_type", "Expected "+contract.Type+".")
	}
	var issues []ManifestIssue
	switch value := slot.value.(type) {
	case string:
		length := utf8.RuneCountInString(value)
		if contract.MinLength >= 0 && length < contract.MinLength {
			issues = append(issues, issue("too_short", fmt.Sprintf("Must contain at least %d characters.", contract.MinLength))...)
		}
		if contract.MaxLength >= 0 && length > contract.MaxLength {
			issues = append(issues, issue("too_long", fmt.Sprintf("Must contain at most %d characters.", contract.MaxLength))...)
		}
		if len(contract.AllowedLiterals) > 0 && !containsString(contract.AllowedLiterals, value) {
			issues = append(issues, issue("invalid_literal", "Use one of: "+strings.Join(contract.AllowedLiterals, ", ")+".")...)
		}
	case []any:
		if contract.MinItems >= 0 && len(value) < contract.MinItems {
			issues = append(issues, issue("too_few_items", fmt.Sprintf("Must contain at least %d items.", contract.MinItems))...)
		}
		if contract.MaxItems >= 0 && len(value) > contract.MaxItems {
			issues = append(issues, issue("too_many_items", fmt.Sprintf("Must contain at most %d items.", contract.MaxItems))...)
		}
		if contract.MaxItemLength >= 0 {
			for index, item := range value {
				text, ok := item.(string)
				if ok && utf8.RuneCountInString(text) > contract.MaxItemLength {
					issues = append(issues, ManifestIssue{Pointer: slot.pointer + "/" + strconv.Itoa(index), Code: "item_too_long", Message: fmt.Sprintf("Must contain at most %d characters.", contract.MaxItemLength), Severity: ManifestIssueError})
				}
			}
		}
	case map[string]any:
		if contract.MinItems >= 0 && len(value) < contract.MinItems {
			issues = append(issues, issue("too_few_keys", fmt.Sprintf("Must contain at least %d keys.", contract.MinItems))...)
		}
		if contract.MaxItems >= 0 && len(value) > contract.MaxItems {
			issues = append(issues, issue("too_many_keys", fmt.Sprintf("Must contain at most %d keys.", contract.MaxItems))...)
		}
		for key := range value {
			length := utf8.RuneCountInString(key)
			if contract.MinKeyLength >= 0 && length < contract.MinKeyLength {
				issues = append(issues, ManifestIssue{Pointer: slot.pointer + "/" + escapeJSONPointer(key), Code: "key_too_short", Message: fmt.Sprintf("Key must contain at least %d characters.", contract.MinKeyLength), Severity: ManifestIssueError})
			}
			if contract.MaxKeyLength >= 0 && length > contract.MaxKeyLength {
				issues = append(issues, ManifestIssue{Pointer: slot.pointer + "/" + escapeJSONPointer(key), Code: "key_too_long", Message: fmt.Sprintf("Key must contain at most %d characters.", contract.MaxKeyLength), Severity: ManifestIssueError})
			}
		}
	}
	return issues
}

func validateManifestURLs(document map[string]any) []ManifestIssue {
	var issues []ManifestIssue
	for _, contract := range manifestContracts {
		if !manifestContractContainsURL(contract.Path) {
			continue
		}
		for _, slot := range contractSlots(document, contract.Path) {
			if !slot.present {
				continue
			}
			values := []struct {
				pointer string
				text    string
			}{}
			switch value := slot.value.(type) {
			case string:
				values = append(values, struct {
					pointer string
					text    string
				}{pointer: slot.pointer, text: value})
			case []any:
				for index, item := range value {
					if text, ok := item.(string); ok {
						values = append(values, struct {
							pointer string
							text    string
						}{pointer: slot.pointer + "/" + strconv.Itoa(index), text: text})
					}
				}
			}
			for _, value := range values {
				if !validManifestHTTPURL(value.text) {
					issues = append(issues, ManifestIssue{Pointer: value.pointer, Code: "invalid_url", Message: "Use an absolute HTTPS URL without user info or a fragment. Loopback development URLs can use HTTP.", Severity: ManifestIssueError})
				}
			}
		}
	}
	return issues
}

func manifestContractContainsURL(path string) bool {
	parts := strings.Split(path, ".")
	name := strings.TrimSuffix(parts[len(parts)-1], "[]")
	return name == "url" || name == "redirect_urls" || strings.HasSuffix(name, "_url") || strings.HasSuffix(name, "_uri")
}

func validManifestHTTPURL(text string) bool {
	parsed, err := url.Parse(text)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	return parsed.Scheme == "http" && manifestLoopbackHost(parsed.Hostname())
}

func manifestLoopbackHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	address, err := netip.ParseAddr(host)
	return err == nil && address.IsLoopback()
}

// RedirectURLs returns the exact registered OAuth callback URLs in stable order.
func RedirectURLs(manifest Manifest) []string {
	return sortedStrings(stringSliceAt(manifest.Document, "oauth_config", "redirect_urls"))
}

func summarizeManifest(document map[string]any) ManifestSummary {
	summary := ManifestSummary{
		Name:           stringAt(document, "display_information", "name"),
		Description:    stringAt(document, "display_information", "description"),
		BotDisplayName: stringAt(document, "features", "bot_user", "display_name"),
		BotScopes:      sortedStrings(stringSliceAt(document, "oauth_config", "scopes", "bot")),
		UserScopes:     sortedStrings(stringSliceAt(document, "oauth_config", "scopes", "user")),
	}
	summary.Events = sortedStrings(append(
		stringSliceAt(document, "settings", "event_subscriptions", "bot_events"),
		stringSliceAt(document, "settings", "event_subscriptions", "user_events")...,
	))
	if commands, ok := valueAtPath(document, "features", "slash_commands").([]any); ok {
		for _, raw := range commands {
			if command, ok := raw.(map[string]any); ok {
				if name, ok := command["command"].(string); ok && name != "" {
					summary.Commands = append(summary.Commands, name)
				}
			}
		}
	}
	summary.Commands = sortedStrings(summary.Commands)
	originSet := make(map[string]struct{})
	walkManifestValues(document, "", func(_ string, key string, value any) {
		text, ok := value.(string)
		if !ok || !(key == "url" || strings.HasSuffix(key, "_url") || strings.HasSuffix(key, "_uri")) {
			return
		}
		parsed, err := url.Parse(text)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return
		}
		originSet[manifestOrigin(parsed)] = struct{}{}
	})
	for _, redirectURL := range stringSliceAt(document, "oauth_config", "redirect_urls") {
		parsed, err := url.Parse(redirectURL)
		if err == nil && parsed.Scheme != "" && parsed.Host != "" {
			originSet[manifestOrigin(parsed)] = struct{}{}
		}
	}
	for origin := range originSet {
		summary.ExternalOrigins = append(summary.ExternalOrigins, origin)
	}
	sort.Strings(summary.ExternalOrigins)
	return summary
}

func manifestOrigin(parsed *url.URL) string {
	scheme := strings.ToLower(parsed.Scheme)
	hostname := strings.ToLower(parsed.Hostname())
	if address, err := netip.ParseAddr(hostname); err == nil {
		hostname = address.String()
	}
	port := parsed.Port()
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	host := hostname
	if port != "" {
		host = net.JoinHostPort(hostname, port)
	} else if strings.Contains(hostname, ":") {
		host = "[" + hostname + "]"
	}
	return scheme + "://" + host
}

func countManifestContracts(document map[string]any, summary *ManifestSummary) {
	for _, contract := range manifestContracts {
		present := false
		for _, slot := range contractSlots(document, contract.Path) {
			present = present || slot.present
		}
		if !present {
			continue
		}
		switch contract.Status {
		case "implemented":
			summary.ImplementedContracts++
		case "partial":
			summary.PartialContracts++
		default:
			summary.UnsupportedContracts++
		}
	}
}

func walkManifestValues(value any, pointer string, visit func(pointer, key string, value any)) {
	switch current := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(current))
		for key := range current {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			childPointer := pointer + "/" + escapeJSONPointer(key)
			visit(childPointer, key, current[key])
			walkManifestValues(current[key], childPointer, visit)
		}
	case []any:
		for index, child := range current {
			walkManifestValues(child, pointer+"/"+strconv.Itoa(index), visit)
		}
	}
}

func valueAtPath(document map[string]any, path ...string) any {
	var current any = document
	for _, segment := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[segment]
	}
	return current
}

func stringAt(document map[string]any, path ...string) string {
	value, _ := valueAtPath(document, path...).(string)
	return value
}

func stringSliceAt(document map[string]any, path ...string) []string {
	values, ok := valueAtPath(document, path...).([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func valueAtJSONPointer(document map[string]any, pointer string) (any, bool) {
	if pointer == "" {
		return document, true
	}
	var current any = document
	for encoded := range strings.SplitSeq(strings.TrimPrefix(pointer, "/"), "/") {
		segment := strings.ReplaceAll(strings.ReplaceAll(encoded, "~1", "/"), "~0", "~")
		switch value := current.(type) {
		case map[string]any:
			var ok bool
			current, ok = value[segment]
			if !ok {
				return nil, false
			}
		case []any:
			index, err := strconv.Atoi(segment)
			if err != nil || index < 0 || index >= len(value) {
				return nil, false
			}
			current = value[index]
		default:
			return nil, false
		}
	}
	return current, true
}

func pathJSONPointer(path string) string {
	parts := strings.Split(strings.ReplaceAll(path, "[]", ""), ".")
	for i := range parts {
		parts[i] = escapeJSONPointer(parts[i])
	}
	return "/" + strings.Join(parts, "/")
}

func escapeJSONPointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}

func containsString(values []string, value string) bool {
	return slices.Contains(values, value)
}

func sortedStrings(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func deduplicateManifestIssues(issues []ManifestIssue) []ManifestIssue {
	if len(issues) < 2 {
		return issues
	}
	result := issues[:0]
	last := ""
	for _, issue := range issues {
		key := issue.Pointer + "\x00" + issue.Code
		if key == last {
			continue
		}
		last = key
		result = append(result, issue)
	}
	return result
}
