package flowupgrade

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

func RunValidate(cfg ValidateConfig) (*ValidateResult, error) {
	if strings.TrimSpace(cfg.InputPath) == "" {
		return nil, newExitError(exitCodeUsage, "--input is required")
	}
	if strings.TrimSpace(cfg.TargetVersion) == "" {
		return nil, newExitError(exitCodeUsage, "--target-version is required")
	}
	if cfg.InputFormat == "" {
		cfg.InputFormat = SourceFormatAuto
	}
	if _, ok := allowedSourceFormats[cfg.InputFormat]; !ok {
		return nil, newExitError(exitCodeUsage, "unsupported --input-format %q", cfg.InputFormat)
	}
	if strings.TrimSpace(cfg.TargetProcessGroupID) != "" && strings.TrimSpace(cfg.TargetAPIURL) == "" {
		return nil, newExitError(exitCodeUsage, "--target-process-group-id requires --target-api-url")
	}
	if cfg.TargetProcessGroupMode == "" {
		cfg.TargetProcessGroupMode = "auto"
	}
	if !slices.Contains([]string{"auto", "replace", "update"}, cfg.TargetProcessGroupMode) {
		return nil, newExitError(exitCodeUsage, "unsupported --target-process-group-mode %q", cfg.TargetProcessGroupMode)
	}

	document, err := LoadFlowDocument(cfg.InputPath, cfg.InputFormat)
	if err != nil {
		return nil, err
	}
	artifactIdentity, err := extractArtifactFlowIdentity(cfg.InputPath, document.Format)
	if err != nil {
		return nil, err
	}

	var manifest *ExtensionsManifest
	if strings.TrimSpace(cfg.ExtensionsManifestPath) != "" {
		manifest, err = LoadExtensionsManifest(cfg.ExtensionsManifestPath)
		if err != nil {
			return nil, err
		}
	}
	targetAPI, err := loadTargetNiFiAPI(cfg)
	if err != nil {
		return nil, err
	}

	findings := make([]MigrationFinding, 0)
	findings = append(findings, extensionsManifestFindings(document, nil, manifest)...)
	if targetAPI != nil {
		findings = append(findings, buildTargetAPIVersionFindings(cfg.TargetVersion, targetAPI.NiFiVersion)...)
		findings = append(findings, extensionsManifestFindings(document, nil, targetAPI.Manifest)...)
		if strings.TrimSpace(cfg.TargetProcessGroupID) != "" {
			processGroup, processGroupErr := loadTargetProcessGroup(
				newTargetNiFiHTTPClient(cfg.TargetAPIInsecureSkipTLSVerify),
				targetAPI.BaseURL,
				resolveMustTargetAPIBearerToken(cfg),
				cfg.TargetProcessGroupID,
			)
			if processGroupErr != nil {
				return nil, processGroupErr
			}
			findings = append(findings, buildTargetProcessGroupFindings(processGroup, cfg.TargetProcessGroupMode, artifactIdentity)...)
			cfg.TargetProcessGroupMode = resolveTargetProcessGroupMode(cfg.TargetProcessGroupMode, processGroup)
		}
	}

	validationName := cfg.ValidationName
	if strings.TrimSpace(validationName) == "" {
		validationName = fmt.Sprintf("validate-%s", time.Now().UTC().Format("20060102T150405Z"))
	}

	report := ValidationReport{
		APIVersion: reportAPIVersion,
		Kind:       "ValidationReport",
		Metadata: ReportMetadata{
			Name:        validationName,
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		},
		Source: ReportSource{
			Path:   cfg.InputPath,
			Format: string(document.Format),
		},
		Target: ReportTarget{
			NiFiVersion:            cfg.TargetVersion,
			ExtensionsManifestPath: cfg.ExtensionsManifestPath,
			TargetProcessGroupID:   cfg.TargetProcessGroupID,
			TargetProcessGroupMode: cfg.TargetProcessGroupMode,
		},
		Findings: findings,
	}
	if targetAPI != nil {
		report.Target.ActualNiFiVersion = targetAPI.NiFiVersion
		report.Target.TargetAPIURL = targetAPI.BaseURL
	} else {
		report.Target.TargetAPIURL = cfg.TargetAPIURL
	}
	report.Summary = summarizeFindings(findings)

	jsonPath, markdownPath, err := writeValidationReportFiles(report, cfg)
	if err != nil {
		return nil, err
	}

	return &ValidateResult{
		Report:             report,
		ReportJSONPath:     jsonPath,
		ReportMarkdownPath: markdownPath,
		Blocked:            report.Summary.ByClass["blocked"] > 0,
	}, nil
}

func writeValidationReportFiles(report ValidationReport, cfg ValidateConfig) (string, string, error) {
	outputDir := cfg.OutputDir
	if outputDir == "" {
		outputDir = filepath.Join(".", "flow-upgrade-out", report.Metadata.Name)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", "", newExitError(exitCodeInternal, "create output directory %q: %v", outputDir, err)
	}

	jsonPath := cfg.ReportJSONPath
	if jsonPath == "" {
		jsonPath = filepath.Join(outputDir, "validation-report.json")
	}
	markdownPath := cfg.ReportMarkdownPath
	if markdownPath == "" {
		markdownPath = filepath.Join(outputDir, "validation-report.md")
	}

	reportJSON, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", "", newExitError(exitCodeInternal, "marshal validation report json: %v", err)
	}
	if err := os.WriteFile(jsonPath, append(reportJSON, '\n'), 0o644); err != nil {
		return "", "", newExitError(exitCodeInternal, "write validation report json %q: %v", jsonPath, err)
	}
	if err := os.WriteFile(markdownPath, []byte(renderValidationMarkdownReport(report)), 0o644); err != nil {
		return "", "", newExitError(exitCodeInternal, "write validation report markdown %q: %v", markdownPath, err)
	}

	return jsonPath, markdownPath, nil
}

func renderValidationMarkdownReport(report ValidationReport) string {
	var builder strings.Builder
	builder.WriteString("# Flow Validation Report\n\n")
	builder.WriteString(fmt.Sprintf("- Validation: `%s`\n", report.Metadata.Name))
	builder.WriteString(fmt.Sprintf("- Generated: `%s`\n", report.Metadata.GeneratedAt))
	builder.WriteString(fmt.Sprintf("- Input: `%s`\n", report.Source.Path))
	builder.WriteString(fmt.Sprintf("- Target: `%s`\n", report.Target.NiFiVersion))
	if report.Target.ActualNiFiVersion != "" {
		builder.WriteString(fmt.Sprintf("- Target API Version: `%s`\n", report.Target.ActualNiFiVersion))
	}
	if report.Target.ExtensionsManifestPath != "" {
		builder.WriteString(fmt.Sprintf("- Extensions Manifest: `%s`\n", report.Target.ExtensionsManifestPath))
	}
	if report.Target.TargetAPIURL != "" {
		builder.WriteString(fmt.Sprintf("- Target API URL: `%s`\n", report.Target.TargetAPIURL))
	}
	if report.Target.TargetProcessGroupID != "" {
		builder.WriteString(fmt.Sprintf("- Target Process Group ID: `%s`\n", report.Target.TargetProcessGroupID))
		builder.WriteString(fmt.Sprintf("- Target Process Group Mode: `%s`\n", report.Target.TargetProcessGroupMode))
	}
	builder.WriteString(fmt.Sprintf("- Format: `%s`\n\n", report.Source.Format))

	builder.WriteString("## Summary\n\n")
	builder.WriteString(fmt.Sprintf("- Total findings: `%d`\n", report.Summary.TotalFindings))
	builder.WriteString(fmt.Sprintf("- Blocked: `%d`\n", report.Summary.ByClass["blocked"]))
	builder.WriteString(fmt.Sprintf("- Manual-change: `%d`\n", report.Summary.ByClass["manual-change"]))
	builder.WriteString(fmt.Sprintf("- Manual-inspection: `%d`\n", report.Summary.ByClass["manual-inspection"]))
	builder.WriteString(fmt.Sprintf("- Auto-fix: `%d`\n", report.Summary.ByClass["auto-fix"]))
	builder.WriteString(fmt.Sprintf("- Info: `%d`\n\n", report.Summary.ByClass["info"]))

	builder.WriteString("## Findings\n\n")
	if len(report.Findings) == 0 {
		builder.WriteString("- none\n\n")
	} else {
		for _, finding := range report.Findings {
			builder.WriteString(fmt.Sprintf("- `%s` [%s]: %s\n", finding.RuleID, finding.Severity, finding.Message))
			if finding.Component != nil {
				builder.WriteString(fmt.Sprintf("  Component: `%s`\n", firstNonEmpty(finding.Component.Name, finding.Component.ID, finding.Component.Type)))
				if finding.Component.Path != "" {
					builder.WriteString(fmt.Sprintf("  Path: `%s`\n", finding.Component.Path))
				}
			}
			if finding.Notes != "" {
				builder.WriteString(fmt.Sprintf("  Notes: %s\n", finding.Notes))
			}
		}
		builder.WriteString("\n")
	}

	builder.WriteString("## Recommended Next Steps\n\n")
	if report.Summary.ByClass["blocked"] > 0 {
		builder.WriteString("- Resolve blocked validation findings before publish or deployment.\n")
	} else {
		builder.WriteString("- Validation found no blocking issues in the selected checks.\n")
	}

	return builder.String()
}

func buildTargetAPIVersionFindings(expectedVersion, actualVersion string) []MigrationFinding {
	if strings.TrimSpace(expectedVersion) == "" || strings.TrimSpace(actualVersion) == "" {
		return nil
	}
	if expectedVersion == actualVersion {
		return nil
	}

	return []MigrationFinding{
		{
			RuleID:   "system.target-api-version-mismatch",
			Class:    "blocked",
			Severity: "error",
			Message:  fmt.Sprintf("Target NiFi API version %s does not match the requested target version %s.", actualVersion, expectedVersion),
			Notes:    "Validate against the same NiFi version that you plan to publish or import into.",
			Evidence: []FindingEvidence{
				{
					Type:          "target-api-version",
					Field:         "actualVersion",
					ActualValue:   actualVersion,
					ExpectedValue: expectedVersion,
				},
			},
		},
	}
}

type artifactFlowIdentity struct {
	FlowID   string
	BucketID string
	Version  int
}

func extractArtifactFlowIdentity(path string, format SourceFormat) (artifactFlowIdentity, error) {
	if format == SourceFormatGitRegistryDir || format == SourceFormatFlowJSONGZ || format == SourceFormatFlowXMLGZ {
		return artifactFlowIdentity{}, nil
	}
	_, raw, err := readSourceArtifact(path, format)
	if err != nil {
		return artifactFlowIdentity{}, err
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return artifactFlowIdentity{}, nil
	}
	return artifactFlowIdentity{
		FlowID:   firstStringField(payload, "flowId", "flowID", "flowIdentifier"),
		BucketID: firstStringField(payload, "bucketId", "bucketID", "bucketIdentifier"),
		Version:  firstIntField(payload, "version"),
	}, nil
}

func resolveMustTargetAPIBearerToken(cfg ValidateConfig) string {
	token, err := resolveTargetAPIBearerToken(cfg)
	if err != nil {
		return ""
	}
	return token
}

func resolveTargetProcessGroupMode(requested string, group *targetProcessGroup) string {
	if requested != "auto" {
		return requested
	}
	if group != nil && group.VersionControlled {
		return "update"
	}
	return "replace"
}

func buildTargetProcessGroupFindings(group *targetProcessGroup, requestedMode string, artifact artifactFlowIdentity) []MigrationFinding {
	if group == nil {
		return nil
	}

	mode := resolveTargetProcessGroupMode(requestedMode, group)
	findings := []MigrationFinding{
		{
			RuleID:   "system.target-process-group.loaded",
			Class:    "info",
			Severity: "info",
			Message:  fmt.Sprintf("Loaded target process group %s for %s validation.", firstNonEmpty(group.Name, group.ID), mode),
			Evidence: []FindingEvidence{
				{Type: "target-process-group", Field: "processGroupId", ActualValue: group.ID},
				{Type: "target-process-group", Field: "mode", ActualValue: mode},
			},
			Component: &FindingComponent{ID: group.ID, Name: group.Name, Scope: "flow-root"},
		},
	}

	if mode == "replace" && group.VersionControlled {
		findings = append(findings, MigrationFinding{
			RuleID:   "system.target-process-group.replace-under-version-control",
			Class:    "blocked",
			Severity: "error",
			Message:  "Replace-mode validation cannot target a process group that is already under version control.",
			Notes:    "NiFi replace operations expect the target flow not to be under version control. Use update mode against version-controlled process groups.",
			Component: &FindingComponent{
				ID:    group.ID,
				Name:  group.Name,
				Scope: "flow-root",
			},
		})
	}
	if mode == "update" && !group.VersionControlled {
		findings = append(findings, MigrationFinding{
			RuleID:   "system.target-process-group.update-requires-version-control",
			Class:    "blocked",
			Severity: "error",
			Message:  "Update-mode validation requires a target process group that is already under version control.",
			Notes:    "Use replace mode for non-version-controlled process groups, or connect the target process group to a versioned flow first.",
			Component: &FindingComponent{
				ID:    group.ID,
				Name:  group.Name,
				Scope: "flow-root",
			},
		})
	}

	if slices.Contains([]string{"LOCALLY_MODIFIED", "LOCALLY_MODIFIED_AND_STALE", "STALE", "SYNC_FAILURE"}, group.VersionedFlowState) {
		findings = append(findings, MigrationFinding{
			RuleID:   "system.target-process-group.versioned-flow-state",
			Class:    "blocked",
			Severity: "error",
			Message:  fmt.Sprintf("Target process group is in versioned flow state %s.", group.VersionedFlowState),
			Notes:    "Resolve local changes or synchronization failures before attempting an update against this process group.",
			Evidence: []FindingEvidence{
				{Type: "target-process-group", Field: "versionedFlowState", ActualValue: group.VersionedFlowState},
			},
			Component: &FindingComponent{ID: group.ID, Name: group.Name, Scope: "flow-root"},
		})
	}

	if group.InvalidCount > 0 {
		findings = append(findings, MigrationFinding{
			RuleID:   "system.target-process-group.invalid-components",
			Class:    "blocked",
			Severity: "error",
			Message:  fmt.Sprintf("Target process group currently reports %d invalid components.", group.InvalidCount),
			Notes:    "Bring the target process group back to a valid baseline before attempting replace or update operations.",
			Evidence: []FindingEvidence{
				{Type: "target-process-group", Field: "invalidCount", ActualValue: fmt.Sprintf("%d", group.InvalidCount)},
			},
			Component: &FindingComponent{ID: group.ID, Name: group.Name, Scope: "flow-root"},
		})
	}

	if group.RunningCount > 0 || group.DisabledCount > 0 {
		findings = append(findings, MigrationFinding{
			RuleID:   "system.target-process-group.live-components",
			Class:    "manual-inspection",
			Severity: "warning",
			Message:  fmt.Sprintf("Target process group currently has %d running and %d disabled components.", group.RunningCount, group.DisabledCount),
			Notes:    "NiFi may stop or disable components during update-style operations. Review operational impact before applying the migrated flow.",
			Evidence: []FindingEvidence{
				{Type: "target-process-group", Field: "runningCount", ActualValue: fmt.Sprintf("%d", group.RunningCount)},
				{Type: "target-process-group", Field: "disabledCount", ActualValue: fmt.Sprintf("%d", group.DisabledCount)},
			},
			Component: &FindingComponent{ID: group.ID, Name: group.Name, Scope: "flow-root"},
		})
	}

	if artifact.FlowID != "" && group.FlowID != "" && artifact.FlowID != group.FlowID {
		findings = append(findings, MigrationFinding{
			RuleID:   "system.target-process-group.flow-mismatch",
			Class:    "blocked",
			Severity: "error",
			Message:  "The input artifact flow identity does not match the target process group's versioned flow identity.",
			Notes:    "Validate or update using a snapshot that belongs to the same registered flow, or choose a different target process group.",
			Evidence: []FindingEvidence{
				{Type: "target-process-group", Field: "artifactFlowId", ActualValue: artifact.FlowID, ExpectedValue: group.FlowID},
				{Type: "target-process-group", Field: "artifactBucketId", ActualValue: artifact.BucketID, ExpectedValue: group.BucketID},
			},
			Component: &FindingComponent{ID: group.ID, Name: group.Name, Scope: "flow-root"},
		})
	}

	if artifact.Version != 0 && group.Version != 0 && mode == "update" && artifact.Version == group.Version {
		findings = append(findings, MigrationFinding{
			RuleID:   "system.target-process-group.version-already-current",
			Class:    "info",
			Severity: "info",
			Message:  fmt.Sprintf("Target process group is already on version %d.", group.Version),
			Component: &FindingComponent{
				ID:    group.ID,
				Name:  group.Name,
				Scope: "flow-root",
			},
		})
	}

	return findings
}
