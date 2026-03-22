package flowupgrade

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func RunPublish(cfg PublishConfig) (*PublishResult, error) {
	if strings.TrimSpace(cfg.InputPath) == "" {
		return nil, newExitError(exitCodeUsage, "--input is required")
	}
	if cfg.InputFormat == "" {
		cfg.InputFormat = SourceFormatAuto
	}
	if _, ok := allowedSourceFormats[cfg.InputFormat]; !ok {
		return nil, newExitError(exitCodeUsage, "unsupported --input-format %q", cfg.InputFormat)
	}
	if strings.TrimSpace(cfg.Publisher) == "" {
		return nil, newExitError(exitCodeUsage, "--publisher is required")
	}
	if cfg.Publisher != "nifi-registry" && strings.TrimSpace(cfg.Destination) == "" {
		return nil, newExitError(exitCodeUsage, "--destination is required")
	}

	format, content, err := readSourceArtifact(cfg.InputPath, cfg.InputFormat)
	if err != nil {
		return nil, err
	}

	publishName := cfg.PublishName
	if strings.TrimSpace(publishName) == "" {
		publishName = fmt.Sprintf("publish-%s", time.Now().UTC().Format("20060102T150405Z"))
	}

	var publishedPath string
	var files int
	switch cfg.Publisher {
	case "fs":
		publishedPath, files, err = publishToFilesystem(cfg, format)
	case "git-registry-dir":
		publishedPath, files, err = publishToGitRegistryDir(cfg, format, content)
	case "nifi-registry":
		publishedPath, files, err = publishToNiFiRegistry(cfg, format, content)
	default:
		return nil, newExitError(exitCodeUsage, "unsupported --publisher %q", cfg.Publisher)
	}
	if err != nil {
		return nil, err
	}

	report := PublishReport{
		APIVersion: reportAPIVersion,
		Kind:       "PublishReport",
		Metadata: ReportMetadata{
			Name:        publishName,
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		},
		Source: ReportSource{
			Path:   cfg.InputPath,
			Format: string(format),
		},
		Publisher:     cfg.Publisher,
		Destination:   publishDestination(cfg),
		PublishedPath: publishedPath,
		Summary: PublishSummary{
			Publisher: cfg.Publisher,
			Files:     files,
		},
	}

	reportJSONPath, reportMarkdownPath, err := writePublishReportFiles(report, cfg)
	if err != nil {
		return nil, err
	}

	return &PublishResult{
		Report:             report,
		PublishedPath:      publishedPath,
		ReportJSONPath:     reportJSONPath,
		ReportMarkdownPath: reportMarkdownPath,
	}, nil
}

func publishDestination(cfg PublishConfig) string {
	if cfg.Publisher == "nifi-registry" {
		return cfg.RegistryURL
	}
	return cfg.Destination
}

func publishToFilesystem(cfg PublishConfig, format SourceFormat) (string, int, error) {
	info, err := os.Stat(cfg.InputPath)
	if err != nil {
		return "", 0, newExitError(exitCodeSourceRead, "stat input %q: %v", cfg.InputPath, err)
	}

	baseName := cfg.FileName
	if strings.TrimSpace(baseName) == "" {
		baseName = filepath.Base(cfg.InputPath)
	}
	targetPath := filepath.Join(cfg.Destination, baseName)

	if info.IsDir() {
		files, err := copyDirectory(cfg.InputPath, targetPath)
		if err != nil {
			return "", 0, err
		}
		return targetPath, files, nil
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return "", 0, newExitError(exitCodeInternal, "create publish destination %q: %v", filepath.Dir(targetPath), err)
	}
	if err := copyFile(cfg.InputPath, targetPath); err != nil {
		return "", 0, err
	}
	return targetPath, 1, nil
}

func publishToGitRegistryDir(cfg PublishConfig, format SourceFormat, content string) (string, int, error) {
	if strings.TrimSpace(cfg.Bucket) == "" {
		return "", 0, newExitError(exitCodeUsage, "--bucket is required for --publisher git-registry-dir")
	}
	if strings.TrimSpace(cfg.Flow) == "" {
		return "", 0, newExitError(exitCodeUsage, "--flow is required for --publisher git-registry-dir")
	}

	targetDir := filepath.Join(cfg.Destination, cfg.Bucket, cfg.Flow)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", 0, newExitError(exitCodeInternal, "create publish destination %q: %v", targetDir, err)
	}

	if format == SourceFormatGitRegistryDir {
		files, err := copyDirectory(cfg.InputPath, targetDir)
		if err != nil {
			return "", 0, err
		}
		return targetDir, files, nil
	}

	fileName := cfg.FileName
	if strings.TrimSpace(fileName) == "" {
		fileName = "snapshot.json"
	}
	if !strings.HasSuffix(strings.ToLower(fileName), ".json") {
		fileName += ".json"
	}

	targetPath := filepath.Join(targetDir, fileName)
	body, err := normalizePublishJSON(format, content)
	if err != nil {
		return "", 0, err
	}
	if err := os.WriteFile(targetPath, append(body, '\n'), 0o644); err != nil {
		return "", 0, newExitError(exitCodeInternal, "write published git registry snapshot %q: %v", targetPath, err)
	}
	return targetDir, 1, nil
}

func normalizePublishJSON(format SourceFormat, content string) ([]byte, error) {
	var payload any
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return nil, newExitError(exitCodeSourceRead, "parse publish input as json: %v", err)
	}
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, newExitError(exitCodeInternal, "marshal publish payload: %v", err)
	}
	return body, nil
}

func copyFile(sourcePath, targetPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return newExitError(exitCodeSourceRead, "open source file %q: %v", sourcePath, err)
	}
	defer source.Close()

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return newExitError(exitCodeInternal, "create target directory %q: %v", filepath.Dir(targetPath), err)
	}
	target, err := os.Create(targetPath)
	if err != nil {
		return newExitError(exitCodeInternal, "create target file %q: %v", targetPath, err)
	}
	defer target.Close()

	if _, err := io.Copy(target, source); err != nil {
		return newExitError(exitCodeInternal, "copy file %q to %q: %v", sourcePath, targetPath, err)
	}
	return nil
}

func copyDirectory(sourceDir, targetDir string) (int, error) {
	files := 0
	err := filepath.Walk(sourceDir, func(current string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(sourceDir, current)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(targetDir, relative)
		if info.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}
		if err := copyFile(current, targetPath); err != nil {
			return err
		}
		files++
		return nil
	})
	if err != nil {
		return 0, newExitError(exitCodeInternal, "copy directory %q to %q: %v", sourceDir, targetDir, err)
	}
	return files, nil
}

func writePublishReportFiles(report PublishReport, cfg PublishConfig) (string, string, error) {
	outputDir := cfg.OutputDir
	if outputDir == "" {
		outputDir = filepath.Join(".", "flow-upgrade-out", report.Metadata.Name)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", "", newExitError(exitCodeInternal, "create output directory %q: %v", outputDir, err)
	}

	jsonPath := cfg.ReportJSONPath
	if jsonPath == "" {
		jsonPath = filepath.Join(outputDir, "publish-report.json")
	}
	markdownPath := cfg.ReportMarkdownPath
	if markdownPath == "" {
		markdownPath = filepath.Join(outputDir, "publish-report.md")
	}

	reportJSON, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", "", newExitError(exitCodeInternal, "marshal publish report json: %v", err)
	}
	if err := os.WriteFile(jsonPath, append(reportJSON, '\n'), 0o644); err != nil {
		return "", "", newExitError(exitCodeInternal, "write publish report json %q: %v", jsonPath, err)
	}
	if err := os.WriteFile(markdownPath, []byte(renderPublishMarkdownReport(report)), 0o644); err != nil {
		return "", "", newExitError(exitCodeInternal, "write publish report markdown %q: %v", markdownPath, err)
	}
	return jsonPath, markdownPath, nil
}

func renderPublishMarkdownReport(report PublishReport) string {
	var builder strings.Builder
	builder.WriteString("# Flow Publish Report\n\n")
	builder.WriteString(fmt.Sprintf("- Publish: `%s`\n", report.Metadata.Name))
	builder.WriteString(fmt.Sprintf("- Generated: `%s`\n", report.Metadata.GeneratedAt))
	builder.WriteString(fmt.Sprintf("- Source: `%s`\n", report.Source.Path))
	builder.WriteString(fmt.Sprintf("- Format: `%s`\n", report.Source.Format))
	builder.WriteString(fmt.Sprintf("- Publisher: `%s`\n", report.Publisher))
	builder.WriteString(fmt.Sprintf("- Destination: `%s`\n", report.Destination))
	builder.WriteString(fmt.Sprintf("- Published Path: `%s`\n\n", report.PublishedPath))
	builder.WriteString("## Summary\n\n")
	builder.WriteString(fmt.Sprintf("- Files published: `%d`\n", report.Summary.Files))
	return builder.String()
}

func readMaybeCompressedJSON(path string) ([]byte, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(strings.ToLower(path), ".gz") {
		reader, err := gzip.NewReader(bytes.NewReader(content))
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		return io.ReadAll(reader)
	}
	return content, nil
}
