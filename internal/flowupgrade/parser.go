package flowupgrade

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func LoadFlowDocument(path string, requested SourceFormat) (FlowDocument, error) {
	format, content, err := readSourceArtifact(path, requested)
	if err != nil {
		return FlowDocument{}, err
	}

	switch format {
	case SourceFormatFlowJSONGZ, SourceFormatVersionedFlowSnap, SourceFormatNiFiRegistryExport:
		doc, err := parseJSONFlowDocument(content, format)
		if err != nil {
			return FlowDocument{}, newExitError(exitCodeSourceRead, "parse source %q: %v", path, err)
		}
		return doc, nil
	case SourceFormatFlowXMLGZ:
		doc, err := parseXMLFlowDocument(content, format)
		if err != nil {
			return FlowDocument{}, newExitError(exitCodeSourceRead, "parse source %q: %v", path, err)
		}
		return doc, nil
	case SourceFormatGitRegistryDir:
		return parseGitRegistryDocument(path)
	default:
		return FlowDocument{}, newExitError(exitCodeUsage, "unsupported --source-format %q", format)
	}
}

func parseGitRegistryDocument(path string) (FlowDocument, error) {
	doc := FlowDocument{
		Format: SourceFormatGitRegistryDir,
	}

	err := filepath.Walk(path, func(current string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(current), ".json") {
			return nil
		}
		content, err := os.ReadFile(current)
		if err != nil {
			return err
		}
		child, err := parseJSONFlowDocument(string(content), SourceFormatGitRegistryDir)
		if err != nil {
			return fmt.Errorf("%s: %w", current, err)
		}
		doc.RawText += "\n" + child.RawText
		doc.RootAnnotations = append(doc.RootAnnotations, child.RootAnnotations...)
		doc.Components = append(doc.Components, child.Components...)
		return nil
	})
	if err != nil {
		return FlowDocument{}, newExitError(exitCodeSourceRead, "parse source directory %q: %v", path, err)
	}

	return doc, nil
}

func readSourceArtifact(path string, requested SourceFormat) (SourceFormat, string, error) {
	format := requested
	if requested == SourceFormatAuto {
		detected, err := detectSourceFormat(path)
		if err != nil {
			return "", "", err
		}
		format = detected
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", "", newExitError(exitCodeSourceRead, "read source %q: %v", path, err)
	}

	switch format {
	case SourceFormatGitRegistryDir:
		if !info.IsDir() {
			return "", "", newExitError(exitCodeSourceRead, "source %q is not a directory for format %q", path, format)
		}
		return format, "", nil
	case SourceFormatFlowJSONGZ:
		if info.IsDir() {
			return "", "", newExitError(exitCodeSourceRead, "source %q must be a file for format %q", path, format)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return "", "", newExitError(exitCodeSourceRead, "read source %q: %v", path, err)
		}
		reader, err := gzip.NewReader(bytes.NewReader(content))
		if err != nil {
			return "", "", newExitError(exitCodeSourceRead, "decompress source %q: %v", path, err)
		}
		defer reader.Close()
		decoded, err := io.ReadAll(reader)
		if err != nil {
			return "", "", newExitError(exitCodeSourceRead, "decompress source %q: %v", path, err)
		}
		return format, string(decoded), nil
	case SourceFormatFlowXMLGZ:
		if info.IsDir() {
			return "", "", newExitError(exitCodeSourceRead, "source %q must be a file for format %q", path, format)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return "", "", newExitError(exitCodeSourceRead, "read source %q: %v", path, err)
		}
		reader, err := gzip.NewReader(bytes.NewReader(content))
		if err != nil {
			return "", "", newExitError(exitCodeSourceRead, "decompress source %q: %v", path, err)
		}
		defer reader.Close()
		decoded, err := io.ReadAll(reader)
		if err != nil {
			return "", "", newExitError(exitCodeSourceRead, "decompress source %q: %v", path, err)
		}
		return format, string(decoded), nil
	case SourceFormatVersionedFlowSnap, SourceFormatNiFiRegistryExport:
		if info.IsDir() {
			return "", "", newExitError(exitCodeSourceRead, "source %q must be a file for format %q", path, format)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return "", "", newExitError(exitCodeSourceRead, "read source %q: %v", path, err)
		}
		return format, string(content), nil
	default:
		return "", "", newExitError(exitCodeUsage, "unsupported --source-format %q", format)
	}
}

func parseJSONFlowDocument(content string, format SourceFormat) (FlowDocument, error) {
	var payload any
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return FlowDocument{}, err
	}

	doc := FlowDocument{
		Format:        format,
		RawText:       content,
		RootVariables: map[string]string{},
	}
	collectFlowNodes(payload, nil, &doc)
	return doc, nil
}

type xmlNode struct {
	Name     string
	Attrs    map[string]string
	Text     string
	Children []*xmlNode
}

func parseXMLFlowDocument(content string, format SourceFormat) (FlowDocument, error) {
	root, err := parseXMLTree(content)
	if err != nil {
		return FlowDocument{}, err
	}

	doc := FlowDocument{
		Format:        format,
		RawText:       content,
		RootVariables: map[string]string{},
	}
	collectXMLFlowNodes(root, nil, &doc)
	return doc, nil
}

func parseXMLTree(content string) (*xmlNode, error) {
	decoder := xml.NewDecoder(strings.NewReader(content))
	var stack []*xmlNode
	var root *xmlNode

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		switch typed := token.(type) {
		case xml.StartElement:
			node := &xmlNode{
				Name:  typed.Name.Local,
				Attrs: map[string]string{},
			}
			for _, attr := range typed.Attr {
				node.Attrs[attr.Name.Local] = attr.Value
			}
			if len(stack) == 0 {
				root = node
			} else {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, node)
			}
			stack = append(stack, node)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) == 0 {
				continue
			}
			text := strings.TrimSpace(string(typed))
			if text == "" {
				continue
			}
			current := stack[len(stack)-1]
			if current.Text == "" {
				current.Text = text
			} else {
				current.Text += " " + text
			}
		}
	}

	if root == nil {
		return nil, fmt.Errorf("empty xml document")
	}
	return root, nil
}

func collectXMLFlowNodes(node *xmlNode, path []string, doc *FlowDocument) {
	if node == nil {
		return
	}

	component, annotations, ok := extractXMLComponent(node, path)
	if ok {
		doc.Components = append(doc.Components, component)
	}
	if len(path) == 0 || slicesContain(path, "rootGroup") || slicesContain(path, "processGroups") {
		doc.RootAnnotations = append(doc.RootAnnotations, annotations...)
		mergeVariables(doc.RootVariables, extractXMLVariables(node))
	}

	for _, child := range node.Children {
		nextPath := xmlChildPath(path, node, child)
		collectXMLFlowNodes(child, nextPath, doc)
	}
}

func xmlChildPath(path []string, parent, child *xmlNode) []string {
	switch child.Name {
	case "rootGroup":
		return append(path, "rootGroup")
	case "processor":
		return append(path, "processors")
	case "controllerService":
		return append(path, "controllerServices")
	case "reportingTask":
		return append(path, "reportingTasks")
	case "processGroup":
		return append(path, "processGroups")
	case "inputPort":
		return append(path, "inputPorts")
	case "outputPort":
		return append(path, "outputPorts")
	case "funnel":
		return append(path, "funnels")
	case "label":
		return append(path, "labels")
	case "connection":
		return append(path, "connections")
	case "remoteProcessGroup":
		return append(path, "remoteProcessGroups")
	default:
		return path
	}
}

func extractXMLComponent(node *xmlNode, path []string) (FlowComponent, []string, bool) {
	scope := xmlNodeScope(node.Name)
	if scope == "" {
		return FlowComponent{}, extractXMLAnnotations(node), false
	}

	properties := extractXMLProperties(node)
	bundleGroup, bundleArtifact := extractXMLBundle(node)
	annotations := extractXMLAnnotations(node)
	component := FlowComponent{
		ID:             xmlNodeValue(node, "id", "identifier"),
		Name:           xmlNodeValue(node, "name"),
		Type:           xmlNodeValue(node, "class", "type"),
		BundleGroup:    bundleGroup,
		BundleArtifact: bundleArtifact,
		Scope:          scope,
		Path:           renderComponentPath(path, firstNonEmpty(xmlNodeValue(node, "name"), xmlNodeValue(node, "id", "identifier"))),
		Properties:     properties,
		Annotations:    annotations,
	}
	if !isComponentLike(component, properties) {
		return FlowComponent{}, annotations, false
	}
	return component, annotations, true
}

func xmlNodeScope(name string) string {
	switch name {
	case "processor":
		return "processor"
	case "controllerService":
		return "controller-service"
	case "reportingTask":
		return "reporting-task"
	case "processGroup":
		return "process-group"
	case "inputPort":
		return "input-port"
	case "outputPort":
		return "output-port"
	case "funnel":
		return "funnel"
	case "label":
		return "label"
	case "connection":
		return "connection"
	case "remoteProcessGroup":
		return "remote-process-group"
	default:
		return ""
	}
}

func extractXMLProperties(node *xmlNode) map[string]string {
	result := map[string]string{}
	for _, child := range node.Children {
		if child.Name != "property" {
			continue
		}
		name := firstNonEmpty(xmlNodeValue(child, "name"), child.Attrs["name"])
		value := firstNonEmpty(xmlNodeValue(child, "value"), child.Text, child.Attrs["value"])
		if strings.TrimSpace(name) == "" {
			continue
		}
		result[name] = value
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func extractXMLVariables(node *xmlNode) map[string]string {
	result := map[string]string{}
	for _, child := range node.Children {
		if child.Name != "variables" && child.Name != "variable" {
			continue
		}
		if child.Name == "variable" {
			name := xmlNodeValue(child, "name")
			value := xmlNodeValue(child, "value")
			if name != "" {
				result[name] = value
			}
			continue
		}
		for _, variable := range child.Children {
			if variable.Name != "variable" {
				continue
			}
			name := xmlNodeValue(variable, "name")
			value := xmlNodeValue(variable, "value")
			if name != "" {
				result[name] = value
			}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func extractXMLBundle(node *xmlNode) (string, string) {
	for _, child := range node.Children {
		if child.Name != "bundle" {
			continue
		}
		return xmlNodeValue(child, "group", "groupId"), xmlNodeValue(child, "artifact", "artifactId")
	}
	return "", ""
}

func extractXMLAnnotations(node *xmlNode) []string {
	values := make([]string, 0)
	for _, key := range []string{"comment", "comments", "annotationData", "description"} {
		if value := xmlNodeValue(node, key); strings.TrimSpace(value) != "" {
			values = append(values, value)
		}
	}
	return values
}

func xmlNodeValue(node *xmlNode, childNames ...string) string {
	for _, name := range childNames {
		if value := strings.TrimSpace(node.Attrs[name]); value != "" {
			return value
		}
		for _, child := range node.Children {
			if child.Name == name {
				if value := strings.TrimSpace(child.Text); value != "" {
					return value
				}
			}
		}
	}
	return ""
}

func collectFlowNodes(node any, path []string, doc *FlowDocument) {
	switch typed := node.(type) {
	case map[string]any:
		mergedNode, wrapped := mergedComponentNode(typed)
		component, annotations, ok := extractComponent(mergedNode, path)
		if ok {
			doc.Components = append(doc.Components, component)
		}
		if len(path) == 0 || slicesContain(path, "rootGroup") || slicesContain(path, "flowContents") {
			doc.RootAnnotations = append(doc.RootAnnotations, annotations...)
			mergeVariables(doc.RootVariables, extractVariables(typed))
		}
		for key, value := range typed {
			if wrapped && key == "component" {
				continue
			}
			collectFlowNodes(value, append(path, key), doc)
		}
	case []any:
		for _, item := range typed {
			collectFlowNodes(item, path, doc)
		}
	}
}

func mergedComponentNode(node map[string]any) (map[string]any, bool) {
	raw, ok := node["component"]
	if !ok {
		return node, false
	}
	component, ok := raw.(map[string]any)
	if !ok {
		return node, false
	}

	merged := cloneMap(component)
	for _, key := range []string{
		"id",
		"identifier",
		"instanceIdentifier",
		"componentId",
		"name",
		"componentName",
		"type",
		"componentType",
		"class",
		"bundle",
		"properties",
		"comments",
		"comment",
		"annotationData",
		"annotations",
		"description",
		"variables",
	} {
		if _, exists := merged[key]; !exists {
			if value, ok := node[key]; ok {
				merged[key] = value
			}
		}
	}
	return merged, true
}

func cloneMap(values map[string]any) map[string]any {
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func extractComponent(node map[string]any, path []string) (FlowComponent, []string, bool) {
	if shouldSkipComponentExtraction(path) {
		return FlowComponent{}, nil, false
	}

	properties := extractProperties(node)
	bundle := extractBundle(node)
	annotations := extractAnnotations(node)
	component := FlowComponent{
		ID:             firstMapString(node, "id", "identifier", "instanceIdentifier", "componentId"),
		Name:           firstMapString(node, "name", "componentName"),
		Type:           firstMapString(node, "type", "componentType", "class"),
		BundleGroup:    bundle["group"],
		BundleArtifact: bundle["artifact"],
		Scope:          inferScope(node, path),
		Path:           renderComponentPath(path, firstMapString(node, "name", "componentName", "id", "identifier", "instanceIdentifier", "componentId")),
		Properties:     properties,
		Annotations:    annotations,
	}

	if !isComponentLike(component, properties) {
		return FlowComponent{}, annotations, false
	}

	return component, annotations, true
}

func extractProperties(node map[string]any) map[string]string {
	result := map[string]string{}
	mergePropertyMap(result, mapStringAny(node["properties"]))
	if config := mapStringAny(node["config"]); len(config) > 0 {
		mergePropertyMap(result, mapStringAny(config["properties"]))
	}
	mergePropertyMap(result, extractParameterContextProperties(node))
	if len(result) == 0 {
		return nil
	}
	return result
}

func mergePropertyMap(target map[string]string, source map[string]any) {
	for key, value := range source {
		target[key] = stringify(value)
	}
}

func extractParameterContextProperties(node map[string]any) map[string]any {
	raw, ok := node["parameters"]
	if !ok {
		return nil
	}
	parameters, ok := raw.([]any)
	if !ok {
		return nil
	}
	result := map[string]any{}
	for _, entry := range parameters {
		parameterWrapper, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		parameter := mapStringAny(parameterWrapper["parameter"])
		if len(parameter) == 0 {
			parameter = parameterWrapper
		}
		name := firstMapString(parameter, "name")
		if strings.TrimSpace(name) == "" {
			continue
		}
		if value := stringify(parameter["value"]); value != "" {
			result[name] = value
			continue
		}
		if value := stringify(parameter["parameterValue"]); value != "" {
			result[name] = value
		}
	}
	return result
}

func extractBundle(node map[string]any) map[string]string {
	raw, ok := node["bundle"]
	if !ok {
		return map[string]string{}
	}
	bundle, ok := raw.(map[string]any)
	if !ok {
		return map[string]string{}
	}
	return map[string]string{
		"group":    firstMapString(bundle, "group", "groupId"),
		"artifact": firstMapString(bundle, "artifact", "artifactId"),
	}
}

func extractAnnotations(node map[string]any) []string {
	keys := []string{"comments", "comment", "annotationData", "annotations", "description"}
	var annotations []string
	for _, key := range keys {
		if value := stringify(node[key]); strings.TrimSpace(value) != "" {
			annotations = append(annotations, value)
		}
	}
	return annotations
}

func extractVariables(node map[string]any) map[string]string {
	raw, ok := node["variables"]
	if !ok {
		return nil
	}
	variables, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	result := make(map[string]string, len(variables))
	for key, value := range variables {
		result[key] = stringify(value)
	}
	return result
}

func mergeVariables(target map[string]string, incoming map[string]string) {
	if len(incoming) == 0 {
		return
	}
	for key, value := range incoming {
		target[key] = value
	}
}

func inferScope(node map[string]any, path []string) string {
	for i := len(path) - 1; i >= 0; i-- {
		part := path[i]
		switch part {
		case "processors":
			return "processor"
		case "controllerServices", "controller-services":
			return "controller-service"
		case "reportingTasks", "reporting-tasks":
			return "reporting-task"
		case "parameterContexts", "parameter-contexts":
			return "parameter-context"
		case "processGroups", "process-groups":
			return "process-group"
		case "inputPorts", "input-ports":
			return "input-port"
		case "outputPorts", "output-ports":
			return "output-port"
		case "funnels":
			return "funnel"
		case "labels":
			return "label"
		case "connections":
			return "connection"
		case "remoteProcessGroups", "remote-process-groups":
			return "remote-process-group"
		}
	}

	if value := strings.ToLower(firstMapString(node, "componentType")); value != "" {
		switch {
		case strings.Contains(value, "processor"):
			return "processor"
		case strings.Contains(value, "controllerservice"):
			return "controller-service"
		case strings.Contains(value, "reportingtask"):
			return "reporting-task"
		case strings.Contains(value, "controllerservice"):
			return "controller-service"
		case strings.Contains(value, "processgroup"):
			return "process-group"
		case strings.Contains(value, "inputport"):
			return "input-port"
		case strings.Contains(value, "outputport"):
			return "output-port"
		case strings.Contains(value, "funnel"):
			return "funnel"
		case strings.Contains(value, "label"):
			return "label"
		case strings.Contains(value, "connection"):
			return "connection"
		}
	}
	return "flow-root"
}

func isComponentLike(component FlowComponent, properties map[string]string) bool {
	return strings.TrimSpace(component.Type) != "" ||
		strings.TrimSpace(component.Name) != "" ||
		strings.TrimSpace(component.ID) != "" ||
		strings.TrimSpace(component.BundleGroup) != "" ||
		strings.TrimSpace(component.BundleArtifact) != "" ||
		len(properties) > 0
}

func firstMapString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringify(values[key]); strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func stringify(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case json.Number:
		return typed.String()
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case float64:
		return fmt.Sprintf("%v", typed)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if rendered := stringify(item); rendered != "" {
				parts = append(parts, rendered)
			}
		}
		return strings.Join(parts, " ")
	case map[string]any:
		rendered, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		return string(rendered)
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func slicesContain(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func shouldSkipComponentExtraction(path []string) bool {
	if len(path) == 0 {
		return false
	}
	last := path[len(path)-1]
	switch last {
	case "component", "config", "properties", "bundle", "style", "position", "revision", "parameter", "parameterValue":
		return true
	default:
		return false
	}
}

func renderComponentPath(path []string, identity string) string {
	if len(path) == 0 {
		return firstNonEmpty(identity, "root")
	}
	segments := append([]string{}, path...)
	if strings.TrimSpace(identity) != "" {
		segments = append(segments, identity)
	}
	return strings.Join(segments, "/")
}

func mapStringAny(value any) map[string]any {
	typed, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return typed
}
