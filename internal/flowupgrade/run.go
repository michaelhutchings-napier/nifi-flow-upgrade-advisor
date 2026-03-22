package flowupgrade

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func RunRun(cfg RunConfig) (*RunResult, error) {
	if strings.TrimSpace(cfg.Analyze.SourcePath) == "" {
		return nil, newExitError(exitCodeUsage, "--source is required")
	}
	if strings.TrimSpace(cfg.Analyze.SourceVersion) == "" {
		return nil, newExitError(exitCodeUsage, "--source-version is required")
	}
	if strings.TrimSpace(cfg.Analyze.TargetVersion) == "" {
		return nil, newExitError(exitCodeUsage, "--target-version is required")
	}
	if len(cfg.Analyze.RulePackPaths) == 0 {
		return nil, newExitError(exitCodeUsage, "at least one --rule-pack is required")
	}

	runName := cfg.RunName
	if strings.TrimSpace(runName) == "" {
		runName = fmt.Sprintf("run-%s", time.Now().UTC().Format("20060102T150405Z"))
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = filepath.Join(".", "flow-upgrade-out", runName)
	}

	if cfg.Analyze.OutputDir == "" {
		cfg.Analyze.OutputDir = cfg.OutputDir
	}
	if cfg.Rewrite.OutputDir == "" {
		cfg.Rewrite.OutputDir = cfg.OutputDir
	}
	if cfg.Validate.OutputDir == "" {
		cfg.Validate.OutputDir = cfg.OutputDir
	}
	if cfg.Publish.OutputDir == "" {
		cfg.Publish.OutputDir = cfg.OutputDir
	}

	steps := make([]RunStep, 0, 4)

	analyzeResult, err := RunAnalyze(cfg.Analyze)
	if err != nil {
		return nil, err
	}
	steps = append(steps, RunStep{
		Name:           "analyze",
		Status:         "completed",
		Message:        fmt.Sprintf("completed with %d findings", analyzeResult.Report.Summary.TotalFindings),
		ReportJSONPath: analyzeResult.ReportJSONPath,
		ReportMDPath:   analyzeResult.ReportMarkdownPath,
	})

	result := &RunResult{}
	if analyzeResult.ExceededFailOn {
		steps = append(steps, RunStep{
			Name:    "rewrite",
			Status:  "skipped",
			Message: "analysis threshold exceeded",
		})
		result.ExceededFailOn = true
		result.Report = buildRunReport(runName, cfg, steps, "stopped", true, false)
		result.ReportJSONPath, result.ReportMarkdownPath, err = writeRunReportFiles(result.Report, cfg)
		return result, err
	}

	cfg.Rewrite.PlanPath = analyzeResult.ReportJSONPath
	rewriteResult, err := RunRewrite(cfg.Rewrite)
	if err != nil {
		return nil, err
	}
	steps = append(steps, RunStep{
		Name:           "rewrite",
		Status:         "completed",
		Message:        fmt.Sprintf("completed with %d applied operations", rewriteResult.Report.Summary.AppliedOperations),
		OutputPath:     rewriteResult.RewrittenFlowPath,
		ReportJSONPath: rewriteResult.RewriteReportJSONPath,
		ReportMDPath:   rewriteResult.RewriteReportMDPath,
	})

	if cfg.Validate.InputPath == "" {
		cfg.Validate.InputPath = rewriteResult.RewrittenFlowPath
	}
	if cfg.Validate.TargetVersion == "" {
		cfg.Validate.TargetVersion = cfg.Analyze.TargetVersion
	}
	validateResult, err := RunValidate(cfg.Validate)
	if err != nil {
		return nil, err
	}
	steps = append(steps, RunStep{
		Name:           "validate",
		Status:         "completed",
		Message:        fmt.Sprintf("completed with %d findings", validateResult.Report.Summary.TotalFindings),
		ReportJSONPath: validateResult.ReportJSONPath,
		ReportMDPath:   validateResult.ReportMarkdownPath,
	})

	result.Blocked = validateResult.Blocked
	if validateResult.Blocked {
		if cfg.PublishEnabled {
			steps = append(steps, RunStep{
				Name:    "publish",
				Status:  "skipped",
				Message: "validation reported blocked findings",
			})
		}
		result.Report = buildRunReport(runName, cfg, steps, "blocked", false, true)
		result.ReportJSONPath, result.ReportMarkdownPath, err = writeRunReportFiles(result.Report, cfg)
		return result, err
	}

	if cfg.PublishEnabled {
		if cfg.Publish.InputPath == "" {
			cfg.Publish.InputPath = rewriteResult.RewrittenFlowPath
		}
		publishResult, err := RunPublish(cfg.Publish)
		if err != nil {
			return nil, err
		}
		steps = append(steps, RunStep{
			Name:           "publish",
			Status:         "completed",
			Message:        fmt.Sprintf("published with %s", publishResult.Report.Publisher),
			OutputPath:     publishResult.PublishedPath,
			ReportJSONPath: publishResult.ReportJSONPath,
			ReportMDPath:   publishResult.ReportMarkdownPath,
		})
	}

	result.Report = buildRunReport(runName, cfg, steps, "completed", false, false)
	result.ReportJSONPath, result.ReportMarkdownPath, err = writeRunReportFiles(result.Report, cfg)
	return result, err
}

func buildRunReport(runName string, cfg RunConfig, steps []RunStep, status string, exceededFailOn, blocked bool) RunReport {
	completed := 0
	for _, step := range steps {
		if step.Status == "completed" {
			completed++
		}
	}
	return RunReport{
		APIVersion: reportAPIVersion,
		Kind:       "RunReport",
		Metadata: ReportMetadata{
			Name:        runName,
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		},
		Source: ReportSource{
			Path:        cfg.Analyze.SourcePath,
			Format:      string(cfg.Analyze.SourceFormat),
			NiFiVersion: cfg.Analyze.SourceVersion,
		},
		Target: ReportTarget{
			NiFiVersion:            cfg.Analyze.TargetVersion,
			ExtensionsManifestPath: cfg.Analyze.ExtensionsManifestPath,
			TargetAPIURL:           cfg.Validate.TargetAPIURL,
			TargetProcessGroupID:   cfg.Validate.TargetProcessGroupID,
			TargetProcessGroupMode: cfg.Validate.TargetProcessGroupMode,
		},
		Summary: RunSummary{
			Status:                   status,
			CompletedSteps:           completed,
			PublishEnabled:           cfg.PublishEnabled,
			AnalyzeThresholdExceeded: exceededFailOn,
			ValidationBlocked:        blocked,
		},
		Steps: steps,
	}
}

func writeRunReportFiles(report RunReport, cfg RunConfig) (string, string, error) {
	outputDir := cfg.OutputDir
	if outputDir == "" {
		outputDir = filepath.Join(".", "flow-upgrade-out", report.Metadata.Name)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", "", newExitError(exitCodeInternal, "create output directory %q: %v", outputDir, err)
	}

	jsonPath := cfg.ReportJSONPath
	if jsonPath == "" {
		jsonPath = filepath.Join(outputDir, "run-report.json")
	}
	markdownPath := cfg.ReportMarkdownPath
	if markdownPath == "" {
		markdownPath = filepath.Join(outputDir, "run-report.md")
	}

	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", "", newExitError(exitCodeInternal, "marshal run report json: %v", err)
	}
	if err := os.WriteFile(jsonPath, append(body, '\n'), 0o644); err != nil {
		return "", "", newExitError(exitCodeInternal, "write run report json %q: %v", jsonPath, err)
	}
	if err := os.WriteFile(markdownPath, []byte(renderRunMarkdownReport(report)), 0o644); err != nil {
		return "", "", newExitError(exitCodeInternal, "write run report markdown %q: %v", markdownPath, err)
	}
	return jsonPath, markdownPath, nil
}

func renderRunMarkdownReport(report RunReport) string {
	var builder strings.Builder
	builder.WriteString("# Flow Upgrade Run Report\n\n")
	builder.WriteString(fmt.Sprintf("- Run: `%s`\n", report.Metadata.Name))
	builder.WriteString(fmt.Sprintf("- Generated: `%s`\n", report.Metadata.GeneratedAt))
	builder.WriteString(fmt.Sprintf("- Source: `%s` (`%s`)\n", report.Source.Path, report.Source.NiFiVersion))
	builder.WriteString(fmt.Sprintf("- Target: `%s`\n", report.Target.NiFiVersion))
	builder.WriteString(fmt.Sprintf("- Status: `%s`\n\n", report.Summary.Status))
	builder.WriteString("## Steps\n\n")
	if len(report.Steps) == 0 {
		builder.WriteString("- none\n")
		return builder.String()
	}
	for _, step := range report.Steps {
		builder.WriteString(fmt.Sprintf("- `%s` `%s`\n", step.Name, step.Status))
		if step.Message != "" {
			builder.WriteString(fmt.Sprintf("  Message: %s\n", step.Message))
		}
		if step.OutputPath != "" {
			builder.WriteString(fmt.Sprintf("  Output: `%s`\n", step.OutputPath))
		}
		if step.ReportJSONPath != "" {
			builder.WriteString(fmt.Sprintf("  Report JSON: `%s`\n", step.ReportJSONPath))
		}
		if step.ReportMDPath != "" {
			builder.WriteString(fmt.Sprintf("  Report Markdown: `%s`\n", step.ReportMDPath))
		}
	}
	return builder.String()
}
