package flowupgrade

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

func RunAnalyze(cfg AnalyzeConfig) (*AnalyzeResult, error) {
	if strings.TrimSpace(cfg.SourcePath) == "" {
		return nil, newExitError(exitCodeUsage, "--source is required")
	}
	if strings.TrimSpace(cfg.SourceVersion) == "" {
		return nil, newExitError(exitCodeUsage, "--source-version is required")
	}
	if strings.TrimSpace(cfg.TargetVersion) == "" {
		return nil, newExitError(exitCodeUsage, "--target-version is required")
	}
	if cfg.FailOn == "" {
		cfg.FailOn = "blocked"
	}
	if cfg.SourceFormat == "" {
		cfg.SourceFormat = SourceFormatAuto
	}
	if _, ok := allowedSourceFormats[cfg.SourceFormat]; !ok {
		return nil, newExitError(exitCodeUsage, "unsupported --source-format %q", cfg.SourceFormat)
	}
	if !slices.Contains([]string{"never", "blocked", "manual-change"}, cfg.FailOn) {
		return nil, newExitError(exitCodeUsage, "unsupported --fail-on %q", cfg.FailOn)
	}

	packs, err := LoadRulePacks(cfg.RulePackPaths)
	if err != nil {
		return nil, err
	}

	var extensionsManifest *ExtensionsManifest
	if strings.TrimSpace(cfg.ExtensionsManifestPath) != "" {
		extensionsManifest, err = LoadExtensionsManifest(cfg.ExtensionsManifestPath)
		if err != nil {
			return nil, err
		}
	}

	document, err := LoadFlowDocument(cfg.SourcePath, cfg.SourceFormat)
	if err != nil {
		return nil, err
	}

	matchingPacks, err := filterMatchingRulePacks(packs, cfg.SourceVersion, cfg.TargetVersion, document.Format)
	if err != nil {
		return nil, err
	}

	findings := make([]MigrationFinding, 0)
	if len(matchingPacks) == 0 {
		if !cfg.AllowUnsupportedVersionPair {
			return nil, newExitError(exitCodeVersionPair, "no loaded rule pack supports source %s and target %s", cfg.SourceVersion, cfg.TargetVersion)
		}
		findings = append(findings, MigrationFinding{
			RuleID:   "system.unsupported-version-pair",
			Class:    "blocked",
			Severity: "error",
			Message:  fmt.Sprintf("No loaded rule pack supports source %s and target %s.", cfg.SourceVersion, cfg.TargetVersion),
		})
	}

	for _, pack := range matchingPacks {
		for _, rule := range pack.Spec.Rules {
			findings = append(findings, ruleFindings(rule, document)...)
		}
	}
	findings = append(findings, extensionsManifestFindings(document, matchingPacks, extensionsManifest)...)
	slices.SortFunc(findings, func(a, b MigrationFinding) int {
		if cmp := strings.Compare(a.RuleID, b.RuleID); cmp != 0 {
			return cmp
		}
		aPath := ""
		if a.Component != nil {
			aPath = a.Component.Path
		}
		bPath := ""
		if b.Component != nil {
			bPath = b.Component.Path
		}
		if cmp := strings.Compare(aPath, bPath); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.Message, b.Message)
	})

	analysisName := cfg.AnalysisName
	if strings.TrimSpace(analysisName) == "" {
		analysisName = fmt.Sprintf("analysis-%s", time.Now().UTC().Format("20060102T150405Z"))
	}

	report := MigrationReport{
		APIVersion: reportAPIVersion,
		Kind:       reportKind,
		Metadata: ReportMetadata{
			Name:        analysisName,
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		},
		Source: ReportSource{
			Path:        cfg.SourcePath,
			Format:      string(document.Format),
			NiFiVersion: cfg.SourceVersion,
		},
		Target: ReportTarget{
			NiFiVersion:            cfg.TargetVersion,
			ExtensionsManifestPath: cfg.ExtensionsManifestPath,
		},
		RulePacks: summarizeRulePacks(matchingPacks),
		Findings:  findings,
	}
	report.Summary = summarizeFindings(findings)

	jsonPath, markdownPath, err := writeReportFiles(report, cfg)
	if err != nil {
		return nil, err
	}

	result := &AnalyzeResult{
		Report:             report,
		ReportJSONPath:     jsonPath,
		ReportMarkdownPath: markdownPath,
		ExceededFailOn:     thresholdExceeded(report.Summary.ByClass, cfg.FailOn),
	}
	return result, nil
}

func detectSourceFormat(path string) (SourceFormat, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", newExitError(exitCodeSourceRead, "read source %q: %v", path, err)
	}
	if info.IsDir() {
		return SourceFormatGitRegistryDir, nil
	}
	switch {
	case strings.HasSuffix(path, ".xml.gz"), strings.HasSuffix(path, ".flow.xml.gz"), strings.HasSuffix(path, ".xml.gzip"):
		return SourceFormatFlowXMLGZ, nil
	case strings.HasSuffix(path, ".json.gz"), strings.HasSuffix(path, ".flow.json.gz"), strings.HasSuffix(path, ".json.gzip"):
		return SourceFormatFlowJSONGZ, nil
	case strings.HasSuffix(path, ".json"):
		return SourceFormatVersionedFlowSnap, nil
	default:
		return "", newExitError(exitCodeUsage, "could not auto-detect source format for %q", path)
	}
}

func buildFinding(rule Rule, component *FlowComponent, evidence []FindingEvidence) MigrationFinding {
	finding := MigrationFinding{
		RuleID:     rule.ID,
		Class:      rule.Class,
		Severity:   rule.Severity,
		Message:    rule.Message,
		Notes:      rule.Notes,
		References: rule.References,
		Evidence:   evidence,
	}
	if component != nil {
		finding.Component = &FindingComponent{
			ID:    component.ID,
			Name:  component.Name,
			Type:  component.Type,
			Scope: component.Scope,
			Path:  component.Path,
		}
	} else if componentType := firstNonEmpty(rule.Selector.ComponentType, firstSliceValue(rule.Selector.ComponentTypes)); componentType != "" {
		finding.Component = &FindingComponent{Type: componentType}
	}
	for _, action := range rule.Actions {
		finding.SuggestedActions = append(finding.SuggestedActions, suggestedAction(action))
	}
	return finding
}

func ruleFindings(rule Rule, document FlowDocument) []MigrationFinding {
	if ruleTargetsFlowRoot(rule) {
		evidence, matched := ruleMatchesRoot(rule, document)
		if matched {
			return []MigrationFinding{buildFinding(rule, nil, evidence)}
		}
		return nil
	}

	findings := make([]MigrationFinding, 0)
	for i := range document.Components {
		component := document.Components[i]
		evidence, matched := ruleMatchesComponent(rule, component, document)
		if matched {
			findings = append(findings, buildFinding(rule, &component, evidence))
		}
	}
	return findings
}

func ruleTargetsFlowRoot(rule Rule) bool {
	return rule.Selector.Scope == "flow-root"
}

func ruleMatchesRoot(rule Rule, document FlowDocument) ([]FindingEvidence, bool) {
	if !rootSelectorMatches(rule.Selector, document) {
		return nil, false
	}
	return rootMatchMatches(rule.Match, document)
}

func rootSelectorMatches(selector RuleSelector, document FlowDocument) bool {
	if selector.Scope != "" && selector.Scope != "flow-root" {
		return false
	}
	if selector.PropertyName != "" && !strings.Contains(document.RawText, selector.PropertyName) {
		if _, ok := document.RootVariables[selector.PropertyName]; !ok {
			return false
		}
	}
	if selector.BundleGroup != "" && !strings.Contains(document.RawText, selector.BundleGroup) {
		return false
	}
	if selector.BundleArtifact != "" && !strings.Contains(document.RawText, selector.BundleArtifact) {
		return false
	}
	if selector.ComponentType != "" && !strings.Contains(document.RawText, selector.ComponentType) {
		return false
	}
	if len(selector.ComponentTypes) > 0 {
		found := false
		for _, componentType := range selector.ComponentTypes {
			if strings.Contains(document.RawText, componentType) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func rootMatchMatches(match RuleMatch, document FlowDocument) ([]FindingEvidence, bool) {
	evidence := make([]FindingEvidence, 0)
	if match.PropertyExists != "" {
		if value, ok := document.RootVariables[match.PropertyExists]; ok {
			evidence = append(evidence, FindingEvidence{
				Type:        "root-variable-exists",
				Field:       match.PropertyExists,
				ActualValue: value,
			})
		} else if strings.Contains(document.RawText, match.PropertyExists) {
			evidence = append(evidence, FindingEvidence{
				Type:  "text-contains",
				Field: match.PropertyExists,
			})
		} else {
			return nil, false
		}
	}
	if match.PropertyAbsent != "" {
		if _, ok := document.RootVariables[match.PropertyAbsent]; ok || strings.Contains(document.RawText, match.PropertyAbsent) {
			return nil, false
		}
	}
	if match.AnnotationContains != "" {
		if snippet := firstContainingValue(document.RootAnnotations, match.AnnotationContains); snippet != "" {
			evidence = append(evidence, FindingEvidence{
				Type:          "root-annotation-contains",
				Field:         "annotation",
				ActualValue:   snippet,
				ExpectedValue: match.AnnotationContains,
			})
		} else if strings.Contains(document.RawText, match.AnnotationContains) {
			evidence = append(evidence, FindingEvidence{
				Type:          "text-contains",
				Field:         "annotation",
				ExpectedValue: match.AnnotationContains,
			})
		} else {
			return nil, false
		}
	}
	if match.PropertyValueEquals != nil {
		if value, ok := document.RootVariables[match.PropertyValueEquals.Property]; ok {
			if value != match.PropertyValueEquals.Value {
				return nil, false
			}
			evidence = append(evidence, FindingEvidence{
				Type:          "root-variable-value-equals",
				Field:         match.PropertyValueEquals.Property,
				ActualValue:   value,
				ExpectedValue: match.PropertyValueEquals.Value,
			})
		} else if strings.Contains(document.RawText, match.PropertyValueEquals.Property) && strings.Contains(document.RawText, match.PropertyValueEquals.Value) {
			evidence = append(evidence, FindingEvidence{
				Type:          "text-value-equals",
				Field:         match.PropertyValueEquals.Property,
				ExpectedValue: match.PropertyValueEquals.Value,
			})
		} else {
			return nil, false
		}
	}
	if match.PropertyValueIn != nil {
		if value, ok := document.RootVariables[match.PropertyValueIn.Property]; ok {
			if !slices.Contains(match.PropertyValueIn.Values, value) {
				return nil, false
			}
			evidence = append(evidence, FindingEvidence{
				Type:          "root-variable-value-in",
				Field:         match.PropertyValueIn.Property,
				ActualValue:   value,
				AllowedValues: append([]string{}, match.PropertyValueIn.Values...),
			})
		} else {
			if !strings.Contains(document.RawText, match.PropertyValueIn.Property) {
				return nil, false
			}
			found := false
			var matchedValue string
			for _, value := range match.PropertyValueIn.Values {
				if strings.Contains(document.RawText, value) {
					found = true
					matchedValue = value
					break
				}
			}
			if !found {
				return nil, false
			}
			evidence = append(evidence, FindingEvidence{
				Type:          "text-value-in",
				Field:         match.PropertyValueIn.Property,
				ActualValue:   matchedValue,
				AllowedValues: append([]string{}, match.PropertyValueIn.Values...),
			})
		}
	}
	if match.PropertyValueRegex != nil {
		re := regexp.MustCompile(match.PropertyValueRegex.Regex)
		if value, ok := document.RootVariables[match.PropertyValueRegex.Property]; ok {
			if !re.MatchString(value) {
				return nil, false
			}
			evidence = append(evidence, FindingEvidence{
				Type:          "root-variable-value-regex",
				Field:         match.PropertyValueRegex.Property,
				ActualValue:   value,
				ExpectedValue: match.PropertyValueRegex.Regex,
			})
		} else {
			return nil, false
		}
	}
	if match.ComponentNameRegex != "" {
		re := regexp.MustCompile(match.ComponentNameRegex)
		if !re.MatchString(document.RawText) {
			return nil, false
		}
		evidence = append(evidence, FindingEvidence{
			Type:          "component-name-regex",
			Field:         "componentName",
			ExpectedValue: match.ComponentNameRegex,
		})
	}
	return evidence, true
}

func ruleMatchesComponent(rule Rule, component FlowComponent, document FlowDocument) ([]FindingEvidence, bool) {
	if !componentSelectorMatches(rule.Selector, component) {
		return nil, false
	}
	return componentMatchMatches(rule.Match, component, document)
}

func componentSelectorMatches(selector RuleSelector, component FlowComponent) bool {
	if selector.Scope != "" && selector.Scope != component.Scope {
		return false
	}
	if selector.ComponentType != "" && selector.ComponentType != component.Type {
		return false
	}
	if len(selector.ComponentTypes) > 0 && !slices.Contains(selector.ComponentTypes, component.Type) {
		return false
	}
	if selector.BundleGroup != "" && selector.BundleGroup != component.BundleGroup {
		return false
	}
	if selector.BundleArtifact != "" && selector.BundleArtifact != component.BundleArtifact {
		return false
	}
	if selector.PropertyName != "" {
		if _, ok := component.Properties[selector.PropertyName]; !ok {
			return false
		}
	}
	return true
}

func componentMatchMatches(match RuleMatch, component FlowComponent, document FlowDocument) ([]FindingEvidence, bool) {
	evidence := make([]FindingEvidence, 0)
	if match.PropertyExists != "" {
		if value, ok := component.Properties[match.PropertyExists]; ok {
			evidence = append(evidence, FindingEvidence{
				Type:        "property-exists",
				Field:       match.PropertyExists,
				ActualValue: value,
			})
		} else {
			return nil, false
		}
	}
	if match.PropertyAbsent != "" {
		if _, ok := component.Properties[match.PropertyAbsent]; ok {
			return nil, false
		}
	}
	if match.AnnotationContains != "" {
		snippet := firstContainingValue(component.Annotations, match.AnnotationContains)
		if snippet == "" {
			return nil, false
		}
		evidence = append(evidence, FindingEvidence{
			Type:          "annotation-contains",
			Field:         "annotation",
			ActualValue:   snippet,
			ExpectedValue: match.AnnotationContains,
		})
	}
	if match.PropertyValueEquals != nil {
		value, ok := component.Properties[match.PropertyValueEquals.Property]
		if !ok || value != match.PropertyValueEquals.Value {
			return nil, false
		}
		evidence = append(evidence, FindingEvidence{
			Type:          "property-value-equals",
			Field:         match.PropertyValueEquals.Property,
			ActualValue:   value,
			ExpectedValue: match.PropertyValueEquals.Value,
		})
	}
	if match.PropertyValueIn != nil {
		value, ok := component.Properties[match.PropertyValueIn.Property]
		if !ok || !slices.Contains(match.PropertyValueIn.Values, value) {
			return nil, false
		}
		evidence = append(evidence, FindingEvidence{
			Type:          "property-value-in",
			Field:         match.PropertyValueIn.Property,
			ActualValue:   value,
			AllowedValues: append([]string{}, match.PropertyValueIn.Values...),
		})
	}
	if match.PropertyValueRegex != nil {
		re := regexp.MustCompile(match.PropertyValueRegex.Regex)
		value, ok := component.Properties[match.PropertyValueRegex.Property]
		if !ok || !re.MatchString(value) {
			return nil, false
		}
		evidence = append(evidence, FindingEvidence{
			Type:          "property-value-regex",
			Field:         match.PropertyValueRegex.Property,
			ActualValue:   value,
			ExpectedValue: match.PropertyValueRegex.Regex,
		})
	}
	if match.ComponentNameRegex != "" {
		re := regexp.MustCompile(match.ComponentNameRegex)
		if !re.MatchString(component.Name) {
			return nil, false
		}
		evidence = append(evidence, FindingEvidence{
			Type:          "component-name-regex",
			Field:         "componentName",
			ActualValue:   component.Name,
			ExpectedValue: match.ComponentNameRegex,
		})
	}
	if component.Scope == "flow-root" {
		rootEvidence, ok := rootMatchMatches(match, document)
		if !ok {
			return nil, false
		}
		evidence = append(evidence, rootEvidence...)
	}
	return evidence, true
}

func containsAny(values []string, target string) bool {
	for _, value := range values {
		if strings.Contains(value, target) {
			return true
		}
	}
	return false
}

func firstContainingValue(values []string, target string) string {
	for _, value := range values {
		if strings.Contains(value, target) {
			return value
		}
	}
	return ""
}

func suggestedAction(action RuleAction) SuggestedAction {
	params := map[string]any{}
	switch action.Type {
	case "rename-property", "replace-component-type":
		params["from"] = action.From
		params["to"] = action.To
	case "set-property":
		params["property"] = action.Property
		params["value"] = action.Value
	case "remove-property":
		params["name"] = action.Name
	case "replace-property-value":
		params["property"] = action.Property
		params["from"] = action.From
		params["to"] = action.To
	case "update-bundle-coordinate":
		params["group"] = action.Group
		params["artifact"] = action.Artifact
	case "emit-parameter-scaffold":
		params["parameterName"] = action.ParameterName
		params["sensitive"] = action.Sensitive
	}
	if len(params) == 0 {
		params = nil
	}
	return SuggestedAction{Type: action.Type, Params: params}
}

func summarizeRulePacks(packs []RulePack) []RulePackRef {
	refs := make([]RulePackRef, 0, len(packs))
	for _, pack := range packs {
		refs = append(refs, RulePackRef{Name: pack.Metadata.Name, Path: pack.Path})
	}
	return refs
}

func summarizeFindings(findings []MigrationFinding) ReportSummary {
	byClass := map[string]int{
		"auto-fix":          0,
		"manual-change":     0,
		"manual-inspection": 0,
		"blocked":           0,
		"info":              0,
	}
	for _, finding := range findings {
		byClass[finding.Class]++
	}
	return ReportSummary{
		TotalFindings: len(findings),
		ByClass:       byClass,
	}
}

func thresholdExceeded(byClass map[string]int, failOn string) bool {
	switch failOn {
	case "never":
		return false
	case "manual-change":
		return byClass["manual-change"] > 0 || byClass["blocked"] > 0
	default:
		return byClass["blocked"] > 0
	}
}

func writeReportFiles(report MigrationReport, cfg AnalyzeConfig) (string, string, error) {
	outputDir := cfg.OutputDir
	if outputDir == "" {
		outputDir = filepath.Join(".", "flow-upgrade-out", report.Metadata.Name)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", "", newExitError(exitCodeInternal, "create output directory %q: %v", outputDir, err)
	}

	jsonPath := cfg.ReportJSONPath
	if jsonPath == "" {
		jsonPath = filepath.Join(outputDir, "migration-report.json")
	}
	markdownPath := cfg.ReportMarkdownPath
	if markdownPath == "" {
		markdownPath = filepath.Join(outputDir, "migration-report.md")
	}

	reportJSON, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", "", newExitError(exitCodeInternal, "marshal report json: %v", err)
	}
	if err := os.WriteFile(jsonPath, append(reportJSON, '\n'), 0o644); err != nil {
		return "", "", newExitError(exitCodeInternal, "write report json %q: %v", jsonPath, err)
	}
	if err := os.WriteFile(markdownPath, []byte(renderMarkdownReport(report)), 0o644); err != nil {
		return "", "", newExitError(exitCodeInternal, "write report markdown %q: %v", markdownPath, err)
	}
	return jsonPath, markdownPath, nil
}

func renderMarkdownReport(report MigrationReport) string {
	var builder strings.Builder
	builder.WriteString("# Flow Upgrade Report\n\n")
	builder.WriteString(fmt.Sprintf("- Analysis: `%s`\n", report.Metadata.Name))
	builder.WriteString(fmt.Sprintf("- Generated: `%s`\n", report.Metadata.GeneratedAt))
	builder.WriteString(fmt.Sprintf("- Source: `%s` (`%s`)\n", report.Source.Path, report.Source.NiFiVersion))
	builder.WriteString(fmt.Sprintf("- Target: `%s`\n", report.Target.NiFiVersion))
	if report.Target.ExtensionsManifestPath != "" {
		builder.WriteString(fmt.Sprintf("- Extensions Manifest: `%s`\n", report.Target.ExtensionsManifestPath))
	}
	builder.WriteString(fmt.Sprintf("- Format: `%s`\n\n", report.Source.Format))

	builder.WriteString("## Summary\n\n")
	builder.WriteString(fmt.Sprintf("- Total findings: `%d`\n", report.Summary.TotalFindings))
	builder.WriteString(fmt.Sprintf("- Auto-fix: `%d`\n", report.Summary.ByClass["auto-fix"]))
	builder.WriteString(fmt.Sprintf("- Manual-change: `%d`\n", report.Summary.ByClass["manual-change"]))
	builder.WriteString(fmt.Sprintf("- Manual-inspection: `%d`\n", report.Summary.ByClass["manual-inspection"]))
	builder.WriteString(fmt.Sprintf("- Blocked: `%d`\n", report.Summary.ByClass["blocked"]))
	builder.WriteString(fmt.Sprintf("- Info: `%d`\n\n", report.Summary.ByClass["info"]))

	order := []string{"blocked", "manual-change", "manual-inspection", "auto-fix", "info"}
	grouped := map[string][]MigrationFinding{}
	for _, finding := range report.Findings {
		grouped[finding.Class] = append(grouped[finding.Class], finding)
	}
	for _, class := range order {
		builder.WriteString(fmt.Sprintf("## %s\n\n", class))
		if len(grouped[class]) == 0 {
			builder.WriteString("- none\n\n")
			continue
		}
		for _, finding := range grouped[class] {
			builder.WriteString(fmt.Sprintf("- `%s` [%s]: %s\n", finding.RuleID, finding.Severity, finding.Message))
			if finding.Component != nil {
				detail := firstNonEmpty(finding.Component.Name, finding.Component.ID, finding.Component.Type)
				builder.WriteString(fmt.Sprintf("  Component: `%s`\n", detail))
				if finding.Component.Scope != "" {
					builder.WriteString(fmt.Sprintf("  Scope: `%s`\n", finding.Component.Scope))
				}
				if finding.Component.Path != "" {
					builder.WriteString(fmt.Sprintf("  Path: `%s`\n", finding.Component.Path))
				}
			}
			for _, item := range finding.Evidence {
				line := fmt.Sprintf("  Evidence: `%s`", item.Type)
				if item.Field != "" {
					line += fmt.Sprintf(" field=`%s`", item.Field)
				}
				if item.ActualValue != "" {
					line += fmt.Sprintf(" actual=`%s`", item.ActualValue)
				}
				if item.ExpectedValue != "" {
					line += fmt.Sprintf(" expected=`%s`", item.ExpectedValue)
				}
				if len(item.AllowedValues) > 0 {
					line += fmt.Sprintf(" allowed=`%s`", strings.Join(item.AllowedValues, ", "))
				}
				builder.WriteString(line + "\n")
			}
		}
		builder.WriteString("\n")
	}

	builder.WriteString("## Recommended Next Steps\n\n")
	if report.Summary.ByClass["blocked"] > 0 {
		builder.WriteString("- Resolve blocked findings before upgrade.\n")
	} else if report.Summary.ByClass["manual-change"] > 0 || report.Summary.ByClass["manual-inspection"] > 0 {
		builder.WriteString("- Review manual-change and manual-inspection findings before moving to rewrite or validation.\n")
	} else {
		builder.WriteString("- The analyzed artifact has no blocking findings in the loaded rule-pack set.\n")
	}
	return builder.String()
}

func firstSliceValue(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
