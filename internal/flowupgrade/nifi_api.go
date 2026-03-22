package flowupgrade

import (
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type targetNiFiAPI struct {
	BaseURL     string
	NiFiVersion string
	Manifest    *ExtensionsManifest
}

type targetProcessGroup struct {
	ID                         string
	Name                       string
	VersionControlled          bool
	VersionedFlowState         string
	FlowID                     string
	BucketID                   string
	RegistryID                 string
	Version                    int
	RunningCount               int
	StoppedCount               int
	DisabledCount              int
	InvalidCount               int
	ProcessGroupCount          int
	ProcessGroupUpdateStrategy string
}

func loadTargetNiFiAPI(cfg ValidateConfig) (*targetNiFiAPI, error) {
	if strings.TrimSpace(cfg.TargetAPIURL) == "" {
		return nil, nil
	}

	baseURL, err := normalizeNiFiAPIBaseURL(cfg.TargetAPIURL)
	if err != nil {
		return nil, err
	}

	token, err := resolveTargetAPIBearerToken(cfg)
	if err != nil {
		return nil, err
	}

	client := newTargetNiFiHTTPClient(cfg.TargetAPIInsecureSkipTLSVerify)

	aboutPayload, err := getTargetNiFiJSON(client, baseURL+"/flow/about", token)
	if err != nil {
		return nil, err
	}
	version := extractTargetNiFiVersion(aboutPayload)
	if strings.TrimSpace(version) == "" {
		return nil, newExitError(exitCodeSourceRead, "target NiFi API %q did not return a readable version from /flow/about", baseURL)
	}

	runtimeManifestPayload, err := getTargetNiFiJSON(client, baseURL+"/flow/runtime-manifest", token)
	if err != nil {
		return nil, err
	}

	manifest := extractExtensionsManifestFromRuntimeManifest(baseURL, version, runtimeManifestPayload)
	if err := validateExtensionsManifest(*manifest); err != nil {
		return nil, newExitError(exitCodeSourceRead, "target NiFi API %q returned an invalid runtime manifest: %v", baseURL, err)
	}

	return &targetNiFiAPI{
		BaseURL:     baseURL,
		NiFiVersion: version,
		Manifest:    manifest,
	}, nil
}

func newTargetNiFiHTTPClient(insecureSkipTLSVerify bool) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: insecureSkipTLSVerify}
	return &http.Client{
		Timeout:   15 * time.Second,
		Transport: transport,
	}
}

func normalizeNiFiAPIBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", newExitError(exitCodeUsage, "invalid --target-api-url %q: %v", raw, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", newExitError(exitCodeUsage, "--target-api-url must be an absolute URL")
	}

	path := strings.TrimRight(parsed.Path, "/")
	if path == "" {
		path = "/nifi-api"
	} else if !strings.HasSuffix(path, "/nifi-api") {
		path += "/nifi-api"
	}
	parsed.Path = path
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func resolveTargetAPIBearerToken(cfg ValidateConfig) (string, error) {
	if strings.TrimSpace(cfg.TargetAPIBearerToken) != "" {
		return cfg.TargetAPIBearerToken, nil
	}
	if strings.TrimSpace(cfg.TargetAPIBearerTokenEnv) == "" {
		return "", nil
	}
	value, ok := os.LookupEnv(cfg.TargetAPIBearerTokenEnv)
	if !ok || strings.TrimSpace(value) == "" {
		return "", newExitError(exitCodeUsage, "environment variable %q does not contain a target NiFi API Bearer token", cfg.TargetAPIBearerTokenEnv)
	}
	return value, nil
}

func getTargetNiFiJSON(client *http.Client, endpoint, bearerToken string) (map[string]any, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, newExitError(exitCodeInternal, "build target NiFi API request %q: %v", endpoint, err)
	}
	req.Header.Set("Accept", "application/json")
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, newExitError(exitCodeSourceRead, "call target NiFi API %q: %v", endpoint, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, newExitError(exitCodeSourceRead, "read target NiFi API response %q: %v", endpoint, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		return nil, newExitError(exitCodeSourceRead, "target NiFi API %q returned status %d: %s", endpoint, resp.StatusCode, message)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, newExitError(exitCodeSourceRead, "parse target NiFi API response %q: %v", endpoint, err)
	}
	return payload, nil
}

func loadTargetProcessGroup(client *http.Client, apiBaseURL, bearerToken, processGroupID string) (*targetProcessGroup, error) {
	if strings.TrimSpace(processGroupID) == "" {
		return nil, nil
	}
	payload, err := getTargetNiFiJSON(client, apiBaseURL+"/flow/process-groups/"+url.PathEscape(processGroupID), bearerToken)
	if err != nil {
		return nil, err
	}

	group := &targetProcessGroup{
		ID:                         firstNonEmpty(firstStringField(payload, "id"), processGroupID),
		Name:                       firstStringField(payload, "name"),
		VersionedFlowState:         firstStringField(payload, "versionedFlowState"),
		FlowID:                     firstStringField(payload, "flowId", "flowID"),
		BucketID:                   firstStringField(payload, "bucketId", "bucketID"),
		RegistryID:                 firstStringField(payload, "registryId", "registryID"),
		Version:                    firstIntField(payload, "version"),
		RunningCount:               firstIntField(payload, "runningCount"),
		StoppedCount:               firstIntField(payload, "stoppedCount"),
		DisabledCount:              firstIntField(payload, "disabledCount"),
		InvalidCount:               firstIntField(payload, "invalidCount"),
		ProcessGroupCount:          firstIntField(payload, "processGroupCount"),
		ProcessGroupUpdateStrategy: firstStringField(payload, "processGroupUpdateStrategy"),
	}
	group.VersionControlled = group.FlowID != "" || group.BucketID != "" || group.RegistryID != "" || group.VersionedFlowState != ""
	return group, nil
}

func firstStringField(node any, keys ...string) string {
	values := collectStringFields(node, keys...)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func firstIntField(node any, keys ...string) int {
	allowed := map[string]struct{}{}
	for _, key := range keys {
		allowed[key] = struct{}{}
	}
	values := collectIntFields(node, allowed)
	if len(values) == 0 {
		return 0
	}
	return values[0]
}

func collectIntFields(node any, keys map[string]struct{}) []int {
	var results []int
	collectIntFieldsRecursive(node, keys, &results)
	return results
}

func collectIntFieldsRecursive(node any, keys map[string]struct{}, results *[]int) {
	switch typed := node.(type) {
	case map[string]any:
		for key, value := range typed {
			if _, ok := keys[key]; ok {
				if parsed, ok := anyToInt(value); ok {
					*results = append(*results, parsed)
				}
			}
			collectIntFieldsRecursive(value, keys, results)
		}
	case []any:
		for _, item := range typed {
			collectIntFieldsRecursive(item, keys, results)
		}
	}
}

func anyToInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return 0, false
		}
		return int(parsed), true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func extractTargetNiFiVersion(payload map[string]any) string {
	for _, candidate := range collectStringFields(payload, "version", "niFiVersion", "nifiVersion") {
		if looksLikeVersion(candidate) {
			return candidate
		}
	}
	return ""
}

func collectStringFields(node any, keys ...string) []string {
	allowed := map[string]struct{}{}
	for _, key := range keys {
		allowed[key] = struct{}{}
	}
	var results []string
	collectStringFieldsRecursive(node, allowed, &results)
	return results
}

func collectStringFieldsRecursive(node any, keys map[string]struct{}, results *[]string) {
	switch typed := node.(type) {
	case map[string]any:
		for key, value := range typed {
			if _, ok := keys[key]; ok {
				if str, ok := value.(string); ok && strings.TrimSpace(str) != "" {
					*results = append(*results, str)
				}
			}
			collectStringFieldsRecursive(value, keys, results)
		}
	case []any:
		for _, item := range typed {
			collectStringFieldsRecursive(item, keys, results)
		}
	}
}

var versionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+([-.][0-9A-Za-z.]+)?$`)

func looksLikeVersion(value string) bool {
	return versionPattern.MatchString(strings.TrimSpace(value))
}

func extractExtensionsManifestFromRuntimeManifest(baseURL, version string, payload map[string]any) *ExtensionsManifest {
	components := make([]ExtensionsManifestComponent, 0)
	components = append(components, extractRuntimeManifestComponents(payload, "processorTypes", "processor")...)
	components = append(components, extractRuntimeManifestComponents(payload, "controllerServiceTypes", "controller-service")...)
	components = append(components, extractRuntimeManifestComponents(payload, "reportingTaskTypes", "reporting-task")...)

	seen := map[string]struct{}{}
	deduped := make([]ExtensionsManifestComponent, 0, len(components))
	for _, component := range components {
		if strings.TrimSpace(component.Type) == "" {
			continue
		}
		key := component.Scope + "|" + component.Type + "|" + component.BundleGroup + "|" + component.BundleArtifact
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, component)
	}

	return &ExtensionsManifest{
		APIVersion: reportAPIVersion,
		Kind:       extensionsManifestKind,
		Metadata: ExtensionsManifestMetadata{
			Name:        "target-runtime-manifest",
			Description: "Discovered from a live NiFi API runtime manifest.",
		},
		Spec: ExtensionsManifestSpec{
			NiFiVersion: version,
			Components:  deduped,
		},
		Path: baseURL,
	}
}

func extractRuntimeManifestComponents(payload map[string]any, arrayKey, scope string) []ExtensionsManifestComponent {
	items := findArraysByKey(payload, arrayKey)
	components := make([]ExtensionsManifestComponent, 0)
	for _, item := range items {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		componentType := firstMapString(itemMap, "type")
		if componentType == "" {
			componentType = firstMapString(mapStringAny(itemMap["component"]), "type")
		}
		if componentType == "" {
			continue
		}
		bundle := mapStringAny(itemMap["bundle"])
		if len(bundle) == 0 {
			bundle = mapStringAny(mapStringAny(itemMap["component"])["bundle"])
		}
		components = append(components, ExtensionsManifestComponent{
			Type:           componentType,
			Scope:          scope,
			BundleGroup:    firstMapString(bundle, "group"),
			BundleArtifact: firstMapString(bundle, "artifact"),
		})
	}
	return components
}

func findArraysByKey(node any, targetKey string) []any {
	var results []any
	findArraysByKeyRecursive(node, targetKey, &results)
	return results
}

func findArraysByKeyRecursive(node any, targetKey string, results *[]any) {
	switch typed := node.(type) {
	case map[string]any:
		for key, value := range typed {
			if key == targetKey {
				if entries, ok := value.([]any); ok {
					*results = append(*results, entries...)
				}
			}
			findArraysByKeyRecursive(value, targetKey, results)
		}
	case []any:
		for _, item := range typed {
			findArraysByKeyRecursive(item, targetKey, results)
		}
	}
}
