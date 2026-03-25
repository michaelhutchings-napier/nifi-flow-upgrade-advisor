package flowupgrade

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type rewriteTarget struct {
	MutationNode map[string]any
	Component    FlowComponent
}

func RunRewrite(cfg RewriteConfig) (*RewriteResult, error) {
	resolvedCfg, err := resolveRewriteConfig(cfg)
	if err != nil {
		return nil, err
	}
	cfg = resolvedCfg

	if strings.TrimSpace(cfg.SourcePath) == "" {
		return nil, newExitError(exitCodeUsage, "--source is required")
	}
	if strings.TrimSpace(cfg.SourceVersion) == "" {
		return nil, newExitError(exitCodeUsage, "--source-version is required")
	}
	if strings.TrimSpace(cfg.TargetVersion) == "" {
		return nil, newExitError(exitCodeUsage, "--target-version is required")
	}
	if cfg.SourceFormat == "" {
		cfg.SourceFormat = SourceFormatAuto
	}
	if _, ok := allowedSourceFormats[cfg.SourceFormat]; !ok {
		return nil, newExitError(exitCodeUsage, "unsupported --source-format %q", cfg.SourceFormat)
	}

	packs, err := LoadRulePacks(cfg.RulePackPaths)
	if err != nil {
		return nil, err
	}

	format, content, err := readSourceArtifact(cfg.SourcePath, cfg.SourceFormat)
	if err != nil {
		return nil, err
	}
	if format == SourceFormatFlowXMLGZ {
		return nil, newExitError(exitCodeUsage, "rewrite currently supports JSON artifacts and git-registry-dir inputs only")
	}

	document, err := LoadFlowDocument(cfg.SourcePath, cfg.SourceFormat)
	if err != nil {
		return nil, err
	}

	matchingPacks, err := filterMatchingRulePacks(packs, cfg.SourceVersion, cfg.TargetVersion, format)
	if err != nil {
		return nil, err
	}
	if len(matchingPacks) == 0 && !cfg.AllowUnsupportedVersionPair {
		return nil, newExitError(exitCodeVersionPair, "no loaded rule pack supports source %s and target %s", cfg.SourceVersion, cfg.TargetVersion)
	}

	operations := make([]RewriteOperation, 0)
	var payload any
	var gitFiles []gitRegistryRewriteFile

	switch format {
	case SourceFormatGitRegistryDir:
		gitFiles, operations, err = rewriteGitRegistryDirectory(cfg.SourcePath, matchingPacks)
		if err != nil {
			return nil, err
		}
	default:
		if err := json.Unmarshal([]byte(content), &payload); err != nil {
			return nil, newExitError(exitCodeSourceRead, "parse source %q for rewrite: %v", cfg.SourcePath, err)
		}

		targets := collectRewriteTargets(payload)
		for _, pack := range matchingPacks {
			for _, rule := range pack.Spec.Rules {
				if !rewriteClassExecutable(rule.Class) {
					continue
				}
				for i := range targets {
					target := targets[i]
					evidence, matched := ruleMatchesComponent(rule, target.Component, document)
					if !matched {
						continue
					}
					for _, action := range rule.Actions {
						op := applyRewriteAction(rule, action, target, evidence)
						operations = append(operations, op)
					}
				}
			}
		}
	}

	rewriteName := cfg.RewriteName
	if strings.TrimSpace(rewriteName) == "" {
		rewriteName = fmt.Sprintf("rewrite-%s", time.Now().UTC().Format("20060102T150405Z"))
	}

	rewrittenFlowPath, reportJSONPath, reportMDPath, err := writeRewriteOutputs(payload, gitFiles, format, cfg, rewriteName, matchingPacks, operations)
	if err != nil {
		return nil, err
	}

	report := RewriteReport{
		APIVersion: reportAPIVersion,
		Kind:       "RewriteReport",
		Metadata: ReportMetadata{
			Name:        rewriteName,
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		},
		Source: ReportSource{
			Path:        cfg.SourcePath,
			Format:      string(format),
			NiFiVersion: cfg.SourceVersion,
		},
		Target: ReportTarget{
			NiFiVersion: cfg.TargetVersion,
		},
		RulePacks:  summarizeRulePacks(matchingPacks),
		Summary:    summarizeRewriteOperations(operations),
		Operations: operations,
	}

	if err := overwriteRewriteReports(reportJSONPath, reportMDPath, report); err != nil {
		return nil, err
	}

	return &RewriteResult{
		Report:                report,
		RewrittenFlowPath:     rewrittenFlowPath,
		RewriteReportJSONPath: reportJSONPath,
		RewriteReportMDPath:   reportMDPath,
	}, nil
}

type gitRegistryRewriteFile struct {
	RelativePath string
	Payload      any
}

func rewriteGitRegistryDirectory(path string, packs []RulePack) ([]gitRegistryRewriteFile, []RewriteOperation, error) {
	files := make([]gitRegistryRewriteFile, 0)
	operations := make([]RewriteOperation, 0)

	err := filepath.Walk(path, func(current string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() || !strings.HasSuffix(strings.ToLower(current), ".json") {
			return nil
		}
		content, err := os.ReadFile(current)
		if err != nil {
			return err
		}

		var payload any
		if err := json.Unmarshal(content, &payload); err != nil {
			return fmt.Errorf("%s: %w", current, err)
		}
		localDocument, err := parseJSONFlowDocument(string(content), SourceFormatGitRegistryDir)
		if err != nil {
			return fmt.Errorf("%s: %w", current, err)
		}
		targets := collectRewriteTargets(payload)

		for _, pack := range packs {
			for _, rule := range pack.Spec.Rules {
				if !rewriteClassExecutable(rule.Class) {
					continue
				}
				for i := range targets {
					target := targets[i]
					evidence, matched := ruleMatchesComponent(rule, target.Component, localDocument)
					if !matched {
						continue
					}
					for _, action := range rule.Actions {
						op := applyRewriteAction(rule, action, target, evidence)
						operations = append(operations, op)
					}
				}
			}
		}

		relativePath, err := filepath.Rel(path, current)
		if err != nil {
			return err
		}
		files = append(files, gitRegistryRewriteFile{
			RelativePath: relativePath,
			Payload:      payload,
		})
		return nil
	})
	if err != nil {
		return nil, nil, newExitError(exitCodeSourceRead, "rewrite git registry directory %q: %v", path, err)
	}

	return files, operations, nil
}

func resolveRewriteConfig(cfg RewriteConfig) (RewriteConfig, error) {
	if strings.TrimSpace(cfg.PlanPath) == "" {
		return cfg, nil
	}

	body, err := os.ReadFile(cfg.PlanPath)
	if err != nil {
		return RewriteConfig{}, newExitError(exitCodeSourceRead, "read rewrite plan %q: %v", cfg.PlanPath, err)
	}

	var plan MigrationReport
	if err := json.Unmarshal(body, &plan); err != nil {
		return RewriteConfig{}, newExitError(exitCodeSourceRead, "parse rewrite plan %q: %v", cfg.PlanPath, err)
	}
	if plan.APIVersion != reportAPIVersion {
		return RewriteConfig{}, newExitError(exitCodeUsage, "rewrite plan %q apiVersion must be %q", cfg.PlanPath, reportAPIVersion)
	}
	if plan.Kind != reportKind {
		return RewriteConfig{}, newExitError(exitCodeUsage, "rewrite plan %q kind must be %q", cfg.PlanPath, reportKind)
	}

	if strings.TrimSpace(cfg.SourcePath) == "" {
		cfg.SourcePath = plan.Source.Path
	}
	if cfg.SourceFormat == "" || cfg.SourceFormat == SourceFormatAuto {
		cfg.SourceFormat = SourceFormat(plan.Source.Format)
	}
	if strings.TrimSpace(cfg.SourceVersion) == "" {
		cfg.SourceVersion = plan.Source.NiFiVersion
	}
	if strings.TrimSpace(cfg.TargetVersion) == "" {
		cfg.TargetVersion = plan.Target.NiFiVersion
	}
	if len(cfg.RulePackPaths) == 0 {
		for _, pack := range plan.RulePacks {
			if strings.TrimSpace(pack.Path) != "" {
				cfg.RulePackPaths = append(cfg.RulePackPaths, pack.Path)
			}
		}
	}
	if strings.TrimSpace(cfg.RewriteName) == "" && plan.Metadata.Name != "" {
		cfg.RewriteName = plan.Metadata.Name + "-rewrite"
	}

	return cfg, nil
}

func collectRewriteTargets(payload any) []rewriteTarget {
	targets := make([]rewriteTarget, 0)
	collectRewriteTargetsRecursive(payload, nil, &targets)
	return targets
}

func collectRewriteTargetsRecursive(node any, path []string, targets *[]rewriteTarget) {
	switch typed := node.(type) {
	case map[string]any:
		mergedNode, wrapped := mergedComponentNode(typed)
		component, _, ok := extractComponent(mergedNode, path)
		if ok {
			mutationNode := typed
			if wrapped {
				if inner, ok := typed["component"].(map[string]any); ok {
					mutationNode = inner
				}
			}
			*targets = append(*targets, rewriteTarget{
				MutationNode: mutationNode,
				Component:    component,
			})
		}
		for key, value := range typed {
			if wrapped && key == "component" {
				continue
			}
			collectRewriteTargetsRecursive(value, append(path, key), targets)
		}
	case []any:
		for _, item := range typed {
			collectRewriteTargetsRecursive(item, path, targets)
		}
	}
}

func applyRewriteAction(rule Rule, action RuleAction, target rewriteTarget, evidence []FindingEvidence) RewriteOperation {
	op := RewriteOperation{
		RuleID:     rule.ID,
		Class:      rule.Class,
		ActionType: action.Type,
		Status:     "skipped",
		Message:    rule.Message,
		Reason:     "action not applied",
		Notes:      rule.Notes,
		References: append([]string(nil), rule.References...),
		Evidence:   evidence,
		Params:     suggestedAction(action).Params,
		Component: &FindingComponent{
			ID:    target.Component.ID,
			Name:  target.Component.Name,
			Type:  target.Component.Type,
			Scope: target.Component.Scope,
			Path:  target.Component.Path,
		},
	}

	var applied bool
	var reason string

	switch action.Type {
	case "rename-property":
		applied, reason = rewriteRenameProperty(target.MutationNode, action.From, action.To)
	case "set-property":
		applied, reason = rewriteSetProperty(target.MutationNode, action.Property, action.Value)
	case "set-property-if-absent":
		applied, reason = rewriteSetPropertyIfAbsent(target.MutationNode, action.Property, action.Value)
	case "copy-property":
		applied, reason = rewriteCopyProperty(target.MutationNode, action.From, action.To)
	case "remove-property":
		applied, reason = rewriteRemoveProperty(target.MutationNode, action.Name)
	case "update-bundle-coordinate":
		applied, reason = rewriteUpdateBundleCoordinate(target.MutationNode, action.Group, action.Artifact)
	case "replace-component-type":
		applied, reason = rewriteReplaceComponentType(target.MutationNode, action.From, action.To)
	case "replace-property-value":
		applied, reason = rewriteReplacePropertyValue(target.MutationNode, action.Property, action.From, action.To)
	default:
		reason = "action type is not executable in the current rewrite phase"
	}

	if applied {
		op.Status = "applied"
		op.Reason = ""
	} else {
		op.Reason = reason
	}
	return op
}

func rewriteClassExecutable(class string) bool {
	return class == "auto-fix" || class == "assisted-rewrite"
}

func rewriteRenameProperty(node map[string]any, from, to string) (bool, string) {
	if props := directPropertiesMap(node); props != nil {
		if _, exists := props[to]; exists {
			return false, fmt.Sprintf("target property %q already exists", to)
		}
		if value, ok := props[from]; ok {
			props[to] = value
			delete(props, from)
			return true, ""
		}
	}
	if props := configPropertiesMap(node); props != nil {
		if _, exists := props[to]; exists {
			return false, fmt.Sprintf("target property %q already exists", to)
		}
		if value, ok := props[from]; ok {
			props[to] = value
			delete(props, from)
			return true, ""
		}
	}
	if params := parameterList(node); len(params) > 0 {
		for _, parameter := range params {
			if firstMapString(parameter, "name") == to {
				return false, fmt.Sprintf("target property %q already exists", to)
			}
		}
		for _, parameter := range params {
			if firstMapString(parameter, "name") == from {
				parameter["name"] = to
				return true, ""
			}
		}
	}
	return false, fmt.Sprintf("property %q not found", from)
}

func rewriteSetProperty(node map[string]any, property, value string) (bool, string) {
	if props := directPropertiesMap(node); props != nil {
		if current, ok := props[property]; ok && firstAnyString(current) == value {
			return false, fmt.Sprintf("property %q already set to %q", property, value)
		}
		props[property] = value
		return true, ""
	}
	if props := configPropertiesMap(node); props != nil {
		if current, ok := props[property]; ok && firstAnyString(current) == value {
			return false, fmt.Sprintf("property %q already set to %q", property, value)
		}
		props[property] = value
		return true, ""
	}
	if params := parameterList(node); len(params) > 0 {
		for _, parameter := range params {
			if firstMapString(parameter, "name") == property {
				if firstMapString(parameter, "value") == value {
					return false, fmt.Sprintf("property %q already set to %q", property, value)
				}
				parameter["value"] = value
				return true, ""
			}
		}
	}
	return false, fmt.Sprintf("property %q not found", property)
}

func rewriteSetPropertyIfAbsent(node map[string]any, property, value string) (bool, string) {
	if props := directPropertiesMap(node); props != nil {
		if current, ok := props[property]; ok {
			if firstAnyString(current) == value {
				return false, fmt.Sprintf("property %q already set to %q", property, value)
			}
			return false, fmt.Sprintf("property %q already exists", property)
		}
		props[property] = value
		return true, ""
	}
	if props := configPropertiesMap(node); props != nil {
		if current, ok := props[property]; ok {
			if firstAnyString(current) == value {
				return false, fmt.Sprintf("property %q already set to %q", property, value)
			}
			return false, fmt.Sprintf("property %q already exists", property)
		}
		props[property] = value
		return true, ""
	}
	if params := parameterList(node); len(params) > 0 {
		for _, parameter := range params {
			if firstMapString(parameter, "name") == property {
				if firstMapString(parameter, "value") == value {
					return false, fmt.Sprintf("property %q already set to %q", property, value)
				}
				return false, fmt.Sprintf("property %q already exists", property)
			}
		}
	}
	return false, fmt.Sprintf("property %q not found", property)
}

func rewriteCopyProperty(node map[string]any, from, to string) (bool, string) {
	if props := directPropertiesMap(node); props != nil {
		if _, exists := props[to]; exists {
			return false, fmt.Sprintf("target property %q already exists", to)
		}
		if value, ok := props[from]; ok {
			props[to] = value
			return true, ""
		}
	}
	if props := configPropertiesMap(node); props != nil {
		if _, exists := props[to]; exists {
			return false, fmt.Sprintf("target property %q already exists", to)
		}
		if value, ok := props[from]; ok {
			props[to] = value
			return true, ""
		}
	}
	if params := parameterList(node); len(params) > 0 {
		var value string
		for _, parameter := range params {
			if firstMapString(parameter, "name") == to {
				return false, fmt.Sprintf("target property %q already exists", to)
			}
			if firstMapString(parameter, "name") == from {
				value = firstMapString(parameter, "value")
			}
		}
		if value != "" {
			return false, fmt.Sprintf("copy-property does not support parameter-list append for %q", to)
		}
	}
	return false, fmt.Sprintf("property %q not found", from)
}

func rewriteRemoveProperty(node map[string]any, name string) (bool, string) {
	if props := directPropertiesMap(node); props != nil {
		if _, ok := props[name]; ok {
			delete(props, name)
			return true, ""
		}
	}
	if props := configPropertiesMap(node); props != nil {
		if _, ok := props[name]; ok {
			delete(props, name)
			return true, ""
		}
	}
	if raw, ok := node["parameters"].([]any); ok {
		for i, entry := range raw {
			entryMap, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			parameter := mapStringAny(entryMap["parameter"])
			if len(parameter) == 0 {
				parameter = entryMap
			}
			if firstMapString(parameter, "name") == name {
				node["parameters"] = append(raw[:i], raw[i+1:]...)
				return true, ""
			}
		}
	}
	return false, fmt.Sprintf("property %q not found", name)
}

func rewriteReplacePropertyValue(node map[string]any, property, from, to string) (bool, string) {
	if props := directPropertiesMap(node); props != nil {
		if value, ok := props[property]; ok {
			if from != "" && firstAnyString(value) != from {
				return false, fmt.Sprintf("property %q value %q did not match expected %q", property, firstAnyString(value), from)
			}
			props[property] = to
			return true, ""
		}
	}
	if props := configPropertiesMap(node); props != nil {
		if value, ok := props[property]; ok {
			if from != "" && firstAnyString(value) != from {
				return false, fmt.Sprintf("property %q value %q did not match expected %q", property, firstAnyString(value), from)
			}
			props[property] = to
			return true, ""
		}
	}
	if params := parameterList(node); len(params) > 0 {
		for _, parameter := range params {
			if firstMapString(parameter, "name") == property {
				if from != "" && firstMapString(parameter, "value") != from {
					return false, fmt.Sprintf("property %q value %q did not match expected %q", property, firstMapString(parameter, "value"), from)
				}
				parameter["value"] = to
				return true, ""
			}
		}
	}
	return false, fmt.Sprintf("property %q not found", property)
}

func rewriteUpdateBundleCoordinate(node map[string]any, group, artifact string) (bool, string) {
	bundle := mapStringAny(node["bundle"])
	if len(bundle) == 0 {
		return false, "bundle not found"
	}
	bundle["group"] = group
	bundle["artifact"] = artifact
	return true, ""
}

func rewriteReplaceComponentType(node map[string]any, from, to string) (bool, string) {
	for _, key := range []string{"type", "componentType", "class"} {
		if value, ok := node[key]; ok {
			if from != "" && firstAnyString(value) != from {
				return false, fmt.Sprintf("component type %q did not match expected %q", firstAnyString(value), from)
			}
			node[key] = to
			return true, ""
		}
	}
	if from != "" {
		return false, fmt.Sprintf("component type %q not found", from)
	}
	node["type"] = to
	return true, ""
}

func directPropertiesMap(node map[string]any) map[string]any {
	return mapStringAny(node["properties"])
}

func configPropertiesMap(node map[string]any) map[string]any {
	config := mapStringAny(node["config"])
	if len(config) == 0 {
		return nil
	}
	return mapStringAny(config["properties"])
}

func parameterList(node map[string]any) []map[string]any {
	raw, ok := node["parameters"].([]any)
	if !ok {
		return nil
	}
	result := make([]map[string]any, 0, len(raw))
	for _, entry := range raw {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		parameter := mapStringAny(entryMap["parameter"])
		if len(parameter) == 0 {
			parameter = entryMap
		}
		result = append(result, parameter)
	}
	return result
}

func firstAnyString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return fmt.Sprint(value)
	}
}

func summarizeRewriteOperations(operations []RewriteOperation) RewriteSummary {
	summary := RewriteSummary{
		TotalOperations: len(operations),
		ByClass: map[string]int{
			"auto-fix":         0,
			"assisted-rewrite": 0,
		},
		AppliedByClass: map[string]int{
			"auto-fix":         0,
			"assisted-rewrite": 0,
		},
	}
	for _, operation := range operations {
		if _, ok := summary.ByClass[operation.Class]; ok {
			summary.ByClass[operation.Class]++
		}
		if operation.Status == "applied" {
			summary.AppliedOperations++
			if _, ok := summary.AppliedByClass[operation.Class]; ok {
				summary.AppliedByClass[operation.Class]++
			}
		} else {
			summary.SkippedOperations++
		}
	}
	return summary
}

func writeRewriteOutputs(payload any, gitFiles []gitRegistryRewriteFile, format SourceFormat, cfg RewriteConfig, rewriteName string, packs []RulePack, operations []RewriteOperation) (string, string, string, error) {
	outputDir := cfg.OutputDir
	if outputDir == "" {
		outputDir = filepath.Join(".", "flow-upgrade-out", rewriteName)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", "", "", newExitError(exitCodeInternal, "create output directory %q: %v", outputDir, err)
	}

	rewrittenFlowPath := cfg.RewrittenFlowPath
	if rewrittenFlowPath == "" {
		if format == SourceFormatGitRegistryDir {
			rewrittenFlowPath = filepath.Join(outputDir, "rewritten-flow")
		} else {
			rewrittenFlowPath = filepath.Join(outputDir, "rewritten-flow"+defaultRewriteExtension(format))
		}
	}
	reportJSONPath := cfg.RewriteReportJSONPath
	if reportJSONPath == "" {
		reportJSONPath = filepath.Join(outputDir, "rewrite-report.json")
	}
	reportMDPath := cfg.RewriteReportMarkdownPath
	if reportMDPath == "" {
		reportMDPath = filepath.Join(outputDir, "rewrite-report.md")
	}

	if err := writeRewrittenArtifact(rewrittenFlowPath, format, payload, gitFiles); err != nil {
		return "", "", "", err
	}
	placeholder := RewriteReport{
		APIVersion: reportAPIVersion,
		Kind:       "RewriteReport",
		Metadata: ReportMetadata{
			Name:        rewriteName,
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		},
		RulePacks:  summarizeRulePacks(packs),
		Summary:    summarizeRewriteOperations(operations),
		Operations: operations,
	}
	if err := overwriteRewriteReports(reportJSONPath, reportMDPath, placeholder); err != nil {
		return "", "", "", err
	}
	return rewrittenFlowPath, reportJSONPath, reportMDPath, nil
}

func writeRewrittenArtifact(path string, format SourceFormat, payload any, gitFiles []gitRegistryRewriteFile) error {
	if format == SourceFormatGitRegistryDir {
		for _, file := range gitFiles {
			targetPath := filepath.Join(path, file.RelativePath)
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return newExitError(exitCodeInternal, "create rewritten git registry directory %q: %v", filepath.Dir(targetPath), err)
			}
			body, err := json.MarshalIndent(file.Payload, "", "  ")
			if err != nil {
				return newExitError(exitCodeInternal, "marshal rewritten git registry file %q: %v", file.RelativePath, err)
			}
			if err := os.WriteFile(targetPath, append(body, '\n'), 0o644); err != nil {
				return newExitError(exitCodeInternal, "write rewritten git registry file %q: %v", targetPath, err)
			}
		}
		return nil
	}

	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return newExitError(exitCodeInternal, "marshal rewritten flow: %v", err)
	}
	switch format {
	case SourceFormatFlowJSONGZ:
		var buffer bytes.Buffer
		writer := gzip.NewWriter(&buffer)
		if _, err := writer.Write(body); err != nil {
			return newExitError(exitCodeInternal, "compress rewritten flow: %v", err)
		}
		if err := writer.Close(); err != nil {
			return newExitError(exitCodeInternal, "compress rewritten flow: %v", err)
		}
		if err := os.WriteFile(path, buffer.Bytes(), 0o644); err != nil {
			return newExitError(exitCodeInternal, "write rewritten flow %q: %v", path, err)
		}
	default:
		if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
			return newExitError(exitCodeInternal, "write rewritten flow %q: %v", path, err)
		}
	}
	return nil
}

func defaultRewriteExtension(format SourceFormat) string {
	switch format {
	case SourceFormatFlowJSONGZ:
		return ".json.gz"
	default:
		return ".json"
	}
}

func overwriteRewriteReports(jsonPath, mdPath string, report RewriteReport) error {
	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return newExitError(exitCodeInternal, "marshal rewrite report json: %v", err)
	}
	if err := os.WriteFile(jsonPath, append(body, '\n'), 0o644); err != nil {
		return newExitError(exitCodeInternal, "write rewrite report json %q: %v", jsonPath, err)
	}
	if err := os.WriteFile(mdPath, []byte(renderRewriteMarkdownReport(report)), 0o644); err != nil {
		return newExitError(exitCodeInternal, "write rewrite report markdown %q: %v", mdPath, err)
	}
	return nil
}

func renderRewriteMarkdownReport(report RewriteReport) string {
	var builder strings.Builder
	builder.WriteString("# Flow Rewrite Report\n\n")
	builder.WriteString(fmt.Sprintf("- Rewrite: `%s`\n", report.Metadata.Name))
	builder.WriteString(fmt.Sprintf("- Generated: `%s`\n", report.Metadata.GeneratedAt))
	builder.WriteString(fmt.Sprintf("- Source: `%s` (`%s`)\n", report.Source.Path, report.Source.NiFiVersion))
	builder.WriteString(fmt.Sprintf("- Target: `%s`\n\n", report.Target.NiFiVersion))
	builder.WriteString("## Summary\n\n")
	builder.WriteString(fmt.Sprintf("- Total operations: `%d`\n", report.Summary.TotalOperations))
	builder.WriteString(fmt.Sprintf("- Applied: `%d`\n", report.Summary.AppliedOperations))
	builder.WriteString(fmt.Sprintf("- Skipped: `%d`\n", report.Summary.SkippedOperations))
	builder.WriteString(fmt.Sprintf("- Applied auto-fix: `%d`\n", report.Summary.AppliedByClass["auto-fix"]))
	builder.WriteString(fmt.Sprintf("- Applied assisted-rewrite: `%d`\n\n", report.Summary.AppliedByClass["assisted-rewrite"]))
	builder.WriteString("## Operations\n\n")
	if len(report.Operations) == 0 {
		builder.WriteString("- none\n")
		return builder.String()
	}
	for _, op := range report.Operations {
		builder.WriteString(fmt.Sprintf("- `%s` `%s` `%s` `%s`\n", op.RuleID, op.Class, op.ActionType, op.Status))
		if op.Message != "" {
			builder.WriteString(fmt.Sprintf("  Message: %s\n", op.Message))
		}
		if op.Component != nil {
			builder.WriteString(fmt.Sprintf("  Component: `%s`\n", firstNonEmpty(op.Component.Name, op.Component.ID, op.Component.Type)))
			if op.Component.Path != "" {
				builder.WriteString(fmt.Sprintf("  Path: `%s`\n", op.Component.Path))
			}
		}
		if len(op.Evidence) > 0 {
			for _, evidence := range op.Evidence {
				builder.WriteString(fmt.Sprintf("  Evidence: `%s`", evidence.Type))
				if evidence.Field != "" {
					builder.WriteString(fmt.Sprintf(" field=`%s`", evidence.Field))
				}
				if evidence.ActualValue != "" {
					builder.WriteString(fmt.Sprintf(" actual=`%s`", evidence.ActualValue))
				}
				if evidence.ExpectedValue != "" {
					builder.WriteString(fmt.Sprintf(" expected=`%s`", evidence.ExpectedValue))
				}
				builder.WriteString("\n")
			}
		}
		if op.Reason != "" {
			builder.WriteString(fmt.Sprintf("  Reason: %s\n", op.Reason))
		}
		if op.Notes != "" {
			builder.WriteString(fmt.Sprintf("  Notes: %s\n", op.Notes))
		}
		if len(op.References) > 0 {
			for _, reference := range op.References {
				builder.WriteString(fmt.Sprintf("  Reference: %s\n", reference))
			}
		}
	}
	return builder.String()
}
