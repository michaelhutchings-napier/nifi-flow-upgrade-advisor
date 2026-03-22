package flowupgrade

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type registryClient struct {
	BaseURL    string
	HTTPClient *http.Client
	Auth       registryAuth
}

type registryAuth struct {
	BearerToken string
	Username    string
	Password    string
}

type registryBucket struct {
	ID   string
	Name string
}

type registryFlow struct {
	ID   string
	Name string
}

func publishToNiFiRegistry(cfg PublishConfig, format SourceFormat, content string) (string, int, error) {
	if format != SourceFormatVersionedFlowSnap && format != SourceFormatNiFiRegistryExport {
		return "", 0, newExitError(exitCodeUsage, "--publisher nifi-registry supports versioned-flow-snapshot and nifi-registry-export inputs only")
	}

	client, err := newRegistryClient(cfg)
	if err != nil {
		return "", 0, err
	}

	var payload any
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return "", 0, newExitError(exitCodeSourceRead, "parse publish input as json: %v", err)
	}

	bucket, err := resolveRegistryBucket(client, cfg, payload)
	if err != nil {
		return "", 0, err
	}
	flow, err := resolveRegistryFlow(client, cfg, bucket.ID, payload)
	if err != nil {
		return "", 0, err
	}

	importResponse, err := registryPostJSON(client, fmt.Sprintf("%s/buckets/%s/flows/%s/versions/import", client.BaseURL, url.PathEscape(bucket.ID), url.PathEscape(flow.ID)), payload)
	if err != nil {
		return "", 0, err
	}

	version := firstIntField(importResponse, "version")
	if version == 0 {
		version = firstIntField(importResponse, "versionNumber")
	}

	publishedPath := fmt.Sprintf("%s/buckets/%s/flows/%s", client.BaseURL, url.PathEscape(bucket.ID), url.PathEscape(flow.ID))
	if version > 0 {
		publishedPath = fmt.Sprintf("%s/versions/%d", publishedPath, version)
	}
	return publishedPath, 1, nil
}

func newRegistryClient(cfg PublishConfig) (*registryClient, error) {
	if strings.TrimSpace(cfg.RegistryURL) == "" {
		return nil, newExitError(exitCodeUsage, "--registry-url is required for --publisher nifi-registry")
	}
	baseURL, err := normalizeRegistryBaseURL(cfg.RegistryURL)
	if err != nil {
		return nil, err
	}
	auth, err := resolveRegistryAuth(cfg)
	if err != nil {
		return nil, err
	}
	return &registryClient{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.RegistryInsecureSkipTLSVerify},
			},
		},
		Auth: auth,
	}, nil
}

func normalizeRegistryBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", newExitError(exitCodeUsage, "invalid --registry-url %q: %v", raw, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", newExitError(exitCodeUsage, "--registry-url must be an absolute URL")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func resolveRegistryAuth(cfg PublishConfig) (registryAuth, error) {
	auth := registryAuth{
		BearerToken: strings.TrimSpace(cfg.RegistryBearerToken),
		Username:    strings.TrimSpace(cfg.RegistryBasicUsername),
		Password:    strings.TrimSpace(cfg.RegistryBasicPassword),
	}
	if auth.BearerToken == "" && strings.TrimSpace(cfg.RegistryBearerTokenEnv) != "" {
		value, ok := os.LookupEnv(cfg.RegistryBearerTokenEnv)
		if !ok || strings.TrimSpace(value) == "" {
			return registryAuth{}, newExitError(exitCodeUsage, "environment variable %q does not contain a NiFi Registry Bearer token", cfg.RegistryBearerTokenEnv)
		}
		auth.BearerToken = strings.TrimSpace(value)
	}
	if auth.Password == "" && strings.TrimSpace(cfg.RegistryBasicPasswordEnv) != "" {
		value, ok := os.LookupEnv(cfg.RegistryBasicPasswordEnv)
		if !ok || strings.TrimSpace(value) == "" {
			return registryAuth{}, newExitError(exitCodeUsage, "environment variable %q does not contain a NiFi Registry password", cfg.RegistryBasicPasswordEnv)
		}
		auth.Password = value
	}
	if auth.BearerToken != "" && auth.Username != "" {
		return registryAuth{}, newExitError(exitCodeUsage, "configure either Bearer token auth or basic auth for NiFi Registry, not both")
	}
	if auth.Username != "" && auth.Password == "" {
		return registryAuth{}, newExitError(exitCodeUsage, "--registry-basic-username requires a password")
	}
	return auth, nil
}

func resolveRegistryBucket(client *registryClient, cfg PublishConfig, payload any) (*registryBucket, error) {
	if strings.TrimSpace(cfg.RegistryBucketID) != "" {
		return &registryBucket{ID: strings.TrimSpace(cfg.RegistryBucketID), Name: strings.TrimSpace(cfg.RegistryBucketName)}, nil
	}

	buckets, err := listRegistryBuckets(client)
	if err != nil {
		return nil, err
	}

	bucketName := firstNonEmpty(strings.TrimSpace(cfg.RegistryBucketName), inferRegistryBucketName(payload), strings.TrimSpace(cfg.Bucket))
	if bucketName == "" {
		return nil, newExitError(exitCodeUsage, "set --registry-bucket-id or --registry-bucket-name for --publisher nifi-registry")
	}
	for _, bucket := range buckets {
		if bucket.Name == bucketName {
			return &bucket, nil
		}
	}
	if !cfg.RegistryCreateBucket {
		return nil, newExitError(exitCodeSourceRead, "NiFi Registry bucket %q was not found", bucketName)
	}

	body := map[string]any{"name": bucketName}
	created, err := registryPostJSON(client, client.BaseURL+"/buckets", body)
	if err != nil {
		return nil, err
	}
	id := firstStringField(created, "identifier", "id", "bucketIdentifier")
	if id == "" {
		return nil, newExitError(exitCodeSourceRead, "NiFi Registry created bucket %q but did not return an identifier", bucketName)
	}
	return &registryBucket{ID: id, Name: firstNonEmpty(firstStringField(created, "name"), bucketName)}, nil
}

func resolveRegistryFlow(client *registryClient, cfg PublishConfig, bucketID string, payload any) (*registryFlow, error) {
	if strings.TrimSpace(cfg.RegistryFlowID) != "" {
		return &registryFlow{ID: strings.TrimSpace(cfg.RegistryFlowID), Name: strings.TrimSpace(cfg.RegistryFlowName)}, nil
	}

	flows, err := listRegistryFlows(client, bucketID)
	if err != nil {
		return nil, err
	}

	flowName := firstNonEmpty(strings.TrimSpace(cfg.RegistryFlowName), inferRegistryFlowName(payload), strings.TrimSpace(cfg.Flow))
	if flowName == "" {
		return nil, newExitError(exitCodeUsage, "set --registry-flow-id or --registry-flow-name for --publisher nifi-registry")
	}
	for _, flow := range flows {
		if flow.Name == flowName {
			return &flow, nil
		}
	}
	if !cfg.RegistryCreateFlow {
		return nil, newExitError(exitCodeSourceRead, "NiFi Registry flow %q was not found in bucket %q", flowName, bucketID)
	}

	body := map[string]any{"name": flowName}
	created, err := registryPostJSON(client, fmt.Sprintf("%s/buckets/%s/flows", client.BaseURL, url.PathEscape(bucketID)), body)
	if err != nil {
		return nil, err
	}
	id := firstStringField(created, "identifier", "id", "flowIdentifier")
	if id == "" {
		return nil, newExitError(exitCodeSourceRead, "NiFi Registry created flow %q but did not return an identifier", flowName)
	}
	return &registryFlow{ID: id, Name: firstNonEmpty(firstStringField(created, "name"), flowName)}, nil
}

func listRegistryBuckets(client *registryClient) ([]registryBucket, error) {
	payload, err := registryGetJSON(client, client.BaseURL+"/buckets")
	if err != nil {
		return nil, err
	}
	items := collectNamedEntities(payload)
	buckets := make([]registryBucket, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		id := firstStringField(item, "identifier", "id", "bucketIdentifier")
		name := firstStringField(item, "name")
		if id == "" || name == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		buckets = append(buckets, registryBucket{ID: id, Name: name})
	}
	return buckets, nil
}

func listRegistryFlows(client *registryClient, bucketID string) ([]registryFlow, error) {
	payload, err := registryGetJSON(client, fmt.Sprintf("%s/buckets/%s/flows", client.BaseURL, url.PathEscape(bucketID)))
	if err != nil {
		return nil, err
	}
	items := collectNamedEntities(payload)
	flows := make([]registryFlow, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		id := firstStringField(item, "identifier", "id", "flowIdentifier")
		name := firstStringField(item, "name")
		if id == "" || name == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		flows = append(flows, registryFlow{ID: id, Name: name})
	}
	return flows, nil
}

func collectNamedEntities(node any) []map[string]any {
	var results []map[string]any
	collectNamedEntitiesRecursive(node, &results)
	return results
}

func collectNamedEntitiesRecursive(node any, results *[]map[string]any) {
	switch typed := node.(type) {
	case map[string]any:
		if firstStringField(typed, "identifier", "id") != "" && firstStringField(typed, "name") != "" {
			*results = append(*results, typed)
		}
		for _, value := range typed {
			collectNamedEntitiesRecursive(value, results)
		}
	case []any:
		for _, item := range typed {
			collectNamedEntitiesRecursive(item, results)
		}
	}
}

func inferRegistryBucketName(payload any) string {
	return firstNonEmpty(
		firstStringField(payload, "bucketName"),
		baseNameFromPath(firstStringField(payload, "bucketIdentifier", "bucketId")),
	)
}

func inferRegistryFlowName(payload any) string {
	return firstNonEmpty(
		firstStringField(payload, "flowName", "name"),
		baseNameFromPath(firstStringField(payload, "flowIdentifier", "flowId")),
	)
}

func baseNameFromPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return filepath.Base(strings.Trim(value, "/"))
}

func registryGetJSON(client *registryClient, endpoint string) (map[string]any, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, newExitError(exitCodeInternal, "build NiFi Registry request %q: %v", endpoint, err)
	}
	applyRegistryAuth(req, client.Auth)
	req.Header.Set("Accept", "application/json")
	return doRegistryJSON(req, client.HTTPClient)
}

func registryPostJSON(client *registryClient, endpoint string, body any) (map[string]any, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, newExitError(exitCodeInternal, "marshal NiFi Registry request body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, newExitError(exitCodeInternal, "build NiFi Registry request %q: %v", endpoint, err)
	}
	applyRegistryAuth(req, client.Auth)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	return doRegistryJSON(req, client.HTTPClient)
}

func applyRegistryAuth(req *http.Request, auth registryAuth) {
	if auth.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+auth.BearerToken)
		return
	}
	if auth.Username != "" {
		req.SetBasicAuth(auth.Username, auth.Password)
	}
}

func doRegistryJSON(req *http.Request, client *http.Client) (map[string]any, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, newExitError(exitCodeSourceRead, "call NiFi Registry API %q: %v", req.URL.String(), err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, newExitError(exitCodeSourceRead, "read NiFi Registry API response %q: %v", req.URL.String(), err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		return nil, newExitError(exitCodeSourceRead, "NiFi Registry API %q returned status %d: %s", req.URL.String(), resp.StatusCode, message)
	}

	var payload map[string]any
	if len(bytes.TrimSpace(body)) == 0 {
		return map[string]any{}, nil
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		return payload, nil
	}

	var arrayPayload []any
	if err := json.Unmarshal(body, &arrayPayload); err == nil {
		return map[string]any{"items": arrayPayload}, nil
	}
	return nil, newExitError(exitCodeSourceRead, "parse NiFi Registry API response %q: invalid json", req.URL.String())
}
