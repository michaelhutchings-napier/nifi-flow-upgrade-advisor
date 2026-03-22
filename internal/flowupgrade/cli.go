package flowupgrade

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
)

type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringSliceFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func Main(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return exitCodeUsage
	}

	switch args[0] {
	case "analyze":
		return runAnalyzeCommand(args[1:], stdout, stderr)
	case "rewrite":
		return runRewriteCommand(args[1:], stdout, stderr)
	case "validate":
		return runValidateCommand(args[1:], stdout, stderr)
	case "rule-pack":
		return runRulePackCommand(args[1:], stdout, stderr)
	case "version":
		return runVersionCommand(stdout)
	case "publish":
		return runPublishCommand(args[1:], stdout, stderr)
	case "run":
		return runRunCommand(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		printUsage(stderr)
		return exitCodeUsage
	}
}

func runAnalyzeCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("analyze", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var cfg AnalyzeConfig
	cfg.SourceFormat = SourceFormatAuto
	cfg.FailOn = "blocked"
	var rulePacks stringSliceFlag

	fs.StringVar(&cfg.SourcePath, "source", "", "Source flow artifact path")
	fs.StringVar((*string)(&cfg.SourceFormat), "source-format", string(SourceFormatAuto), "Source artifact format")
	fs.StringVar(&cfg.SourceVersion, "source-version", "", "Source NiFi version")
	fs.StringVar(&cfg.TargetVersion, "target-version", "", "Target NiFi version")
	fs.Var(&rulePacks, "rule-pack", "Rule pack path")
	fs.StringVar(&cfg.OutputDir, "output-dir", "", "Output directory")
	fs.StringVar(&cfg.ReportJSONPath, "report-json", "", "Explicit report JSON path")
	fs.StringVar(&cfg.ReportMarkdownPath, "report-md", "", "Explicit report Markdown path")
	fs.StringVar(&cfg.FailOn, "fail-on", "blocked", "Failure threshold")
	fs.BoolVar(&cfg.Strict, "strict", false, "Treat unknown analysis gaps as blocked findings")
	fs.StringVar(&cfg.AnalysisName, "name", "", "Analysis name")
	fs.StringVar(&cfg.ExtensionsManifestPath, "extensions-manifest", "", "Optional extensions manifest path")
	fs.StringVar(&cfg.ParameterContextsPath, "parameter-contexts", "", "Optional parameter contexts path")
	fs.BoolVar(&cfg.AllowUnsupportedVersionPair, "allow-unsupported-version-pair", false, "Continue with a blocked finding when no rule pack matches the selected version pair")

	if err := fs.Parse(args); err != nil {
		return exitCodeUsage
	}
	cfg.RulePackPaths = rulePacks

	result, err := RunAnalyze(cfg)
	if err != nil {
		return printError(stderr, err)
	}

	fmt.Fprintf(stdout, "Analysis %s completed\n", result.Report.Metadata.Name)
	fmt.Fprintf(stdout, "Findings: total=%d auto-fix=%d manual-change=%d manual-inspection=%d blocked=%d info=%d\n",
		result.Report.Summary.TotalFindings,
		result.Report.Summary.ByClass["auto-fix"],
		result.Report.Summary.ByClass["manual-change"],
		result.Report.Summary.ByClass["manual-inspection"],
		result.Report.Summary.ByClass["blocked"],
		result.Report.Summary.ByClass["info"],
	)
	fmt.Fprintf(stdout, "Report JSON: %s\n", result.ReportJSONPath)
	fmt.Fprintf(stdout, "Report Markdown: %s\n", result.ReportMarkdownPath)
	if result.ExceededFailOn {
		fmt.Fprintf(stdout, "Fail threshold exceeded: %s\n", cfg.FailOn)
		return exitCodeThreshold
	}
	fmt.Fprintf(stdout, "Fail threshold exceeded: no\n")
	return exitCodeSuccess
}

func runRewriteCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("rewrite", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var cfg RewriteConfig
	cfg.SourceFormat = SourceFormatAuto
	var rulePacks stringSliceFlag

	fs.StringVar(&cfg.PlanPath, "plan", "", "Migration report JSON path from analyze")
	fs.StringVar(&cfg.SourcePath, "source", "", "Source flow artifact path")
	fs.StringVar((*string)(&cfg.SourceFormat), "source-format", string(SourceFormatAuto), "Source artifact format")
	fs.StringVar(&cfg.SourceVersion, "source-version", "", "Source NiFi version")
	fs.StringVar(&cfg.TargetVersion, "target-version", "", "Target NiFi version")
	fs.Var(&rulePacks, "rule-pack", "Rule pack path")
	fs.StringVar(&cfg.OutputDir, "output-dir", "", "Output directory")
	fs.StringVar(&cfg.RewrittenFlowPath, "rewritten-flow", "", "Explicit rewritten artifact path")
	fs.StringVar(&cfg.RewriteReportJSONPath, "rewrite-report-json", "", "Explicit rewrite report JSON path")
	fs.StringVar(&cfg.RewriteReportMarkdownPath, "rewrite-report-md", "", "Explicit rewrite report Markdown path")
	fs.StringVar(&cfg.RewriteName, "name", "", "Rewrite name")
	fs.BoolVar(&cfg.AllowUnsupportedVersionPair, "allow-unsupported-version-pair", false, "Continue when no rule pack matches the selected version pair")

	if err := fs.Parse(args); err != nil {
		return exitCodeUsage
	}
	cfg.RulePackPaths = rulePacks

	result, err := RunRewrite(cfg)
	if err != nil {
		return printError(stderr, err)
	}

	fmt.Fprintf(stdout, "Rewrite %s completed\n", result.Report.Metadata.Name)
	fmt.Fprintf(stdout, "Operations: total=%d applied=%d skipped=%d\n",
		result.Report.Summary.TotalOperations,
		result.Report.Summary.AppliedOperations,
		result.Report.Summary.SkippedOperations,
	)
	fmt.Fprintf(stdout, "Rewritten flow: %s\n", result.RewrittenFlowPath)
	fmt.Fprintf(stdout, "Rewrite report JSON: %s\n", result.RewriteReportJSONPath)
	fmt.Fprintf(stdout, "Rewrite report Markdown: %s\n", result.RewriteReportMDPath)
	return exitCodeSuccess
}

func runValidateCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var cfg ValidateConfig
	cfg.InputFormat = SourceFormatAuto

	fs.StringVar(&cfg.InputPath, "input", "", "Input flow artifact path")
	fs.StringVar((*string)(&cfg.InputFormat), "input-format", string(SourceFormatAuto), "Input artifact format")
	fs.StringVar(&cfg.TargetVersion, "target-version", "", "Target NiFi version")
	fs.StringVar(&cfg.ExtensionsManifestPath, "extensions-manifest", "", "Optional target extensions manifest path")
	fs.StringVar(&cfg.TargetAPIURL, "target-api-url", "", "Optional target NiFi API base URL")
	fs.StringVar(&cfg.TargetAPIBearerToken, "target-api-bearer-token", "", "Optional Bearer token for target NiFi API")
	fs.StringVar(&cfg.TargetAPIBearerTokenEnv, "target-api-bearer-token-env", "", "Optional environment variable containing the target NiFi API Bearer token")
	fs.BoolVar(&cfg.TargetAPIInsecureSkipTLSVerify, "target-api-insecure-skip-tls-verify", false, "Skip TLS verification for target NiFi API connections")
	fs.StringVar(&cfg.TargetProcessGroupID, "target-process-group-id", "", "Optional target process group ID for pre-import validation")
	fs.StringVar(&cfg.TargetProcessGroupMode, "target-process-group-mode", "auto", "Target process group mode: auto, replace, or update")
	fs.StringVar(&cfg.OutputDir, "output-dir", "", "Output directory")
	fs.StringVar(&cfg.ReportJSONPath, "report-json", "", "Explicit validation report JSON path")
	fs.StringVar(&cfg.ReportMarkdownPath, "report-md", "", "Explicit validation report Markdown path")
	fs.StringVar(&cfg.ValidationName, "name", "", "Validation name")

	if err := fs.Parse(args); err != nil {
		return exitCodeUsage
	}

	result, err := RunValidate(cfg)
	if err != nil {
		return printError(stderr, err)
	}

	fmt.Fprintf(stdout, "Validation %s completed\n", result.Report.Metadata.Name)
	fmt.Fprintf(stdout, "Findings: total=%d blocked=%d manual-change=%d manual-inspection=%d auto-fix=%d info=%d\n",
		result.Report.Summary.TotalFindings,
		result.Report.Summary.ByClass["blocked"],
		result.Report.Summary.ByClass["manual-change"],
		result.Report.Summary.ByClass["manual-inspection"],
		result.Report.Summary.ByClass["auto-fix"],
		result.Report.Summary.ByClass["info"],
	)
	fmt.Fprintf(stdout, "Report JSON: %s\n", result.ReportJSONPath)
	fmt.Fprintf(stdout, "Report Markdown: %s\n", result.ReportMarkdownPath)
	if result.Blocked {
		fmt.Fprintf(stdout, "Blocked findings present: yes\n")
		return exitCodeThreshold
	}
	fmt.Fprintf(stdout, "Blocked findings present: no\n")
	return exitCodeSuccess
}

func runRulePackCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "rule-pack requires a subcommand")
		return exitCodeUsage
	}
	switch args[0] {
	case "lint":
		return runRulePackLintCommand(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown rule-pack subcommand %q\n", args[0])
		return exitCodeUsage
	}
}

func runRulePackLintCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("rule-pack lint", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var cfg RulePackLintConfig
	cfg.Format = "text"
	var rulePacks stringSliceFlag

	fs.Var(&rulePacks, "rule-pack", "Rule pack path")
	fs.BoolVar(&cfg.FailOnWarn, "fail-on-warn", false, "Reserved for future warning-level lint failures")
	fs.StringVar(&cfg.Format, "format", "text", "Output format: text or json")

	if err := fs.Parse(args); err != nil {
		return exitCodeUsage
	}
	cfg.RulePackPaths = rulePacks

	result, err := RunRulePackLint(cfg)
	if err != nil {
		return printError(stderr, err)
	}

	switch cfg.Format {
	case "json":
		body, marshalErr := json.MarshalIndent(result, "", "  ")
		if marshalErr != nil {
			return printError(stderr, newExitError(exitCodeInternal, "marshal lint result: %v", marshalErr))
		}
		fmt.Fprintln(stdout, string(body))
	default:
		fmt.Fprintf(stdout, "Validated %d rule pack(s)\n", result.Count)
		for _, pack := range result.RulePacks {
			fmt.Fprintf(stdout, "- %s (%s)\n", pack.Name, pack.Path)
		}
	}
	return exitCodeSuccess
}

func runVersionCommand(stdout io.Writer) int {
	fmt.Fprintf(stdout, "nifi-flow-upgrade version=%s commit=%s buildDate=%s reportSpec=%s rulePackSpec=%s\n",
		Version, Commit, BuildDate, reportAPIVersion, reportAPIVersion)
	return exitCodeSuccess
}

func runPublishCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("publish", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var cfg PublishConfig
	cfg.InputFormat = SourceFormatAuto

	fs.StringVar(&cfg.InputPath, "input", "", "Input artifact path")
	fs.StringVar((*string)(&cfg.InputFormat), "input-format", string(SourceFormatAuto), "Input artifact format")
	fs.StringVar(&cfg.Publisher, "publisher", "", "Publish target: fs, git-registry-dir, or nifi-registry")
	fs.StringVar(&cfg.Destination, "destination", "", "Destination path")
	fs.StringVar(&cfg.Bucket, "bucket", "", "Bucket or folder name for git-registry-dir publish")
	fs.StringVar(&cfg.Flow, "flow", "", "Flow name or directory for git-registry-dir publish")
	fs.StringVar(&cfg.FileName, "file-name", "", "Optional destination file name")
	fs.StringVar(&cfg.RegistryURL, "registry-url", "", "NiFi Registry base URL for nifi-registry publish")
	fs.StringVar(&cfg.RegistryBucketID, "registry-bucket-id", "", "NiFi Registry bucket identifier")
	fs.StringVar(&cfg.RegistryBucketName, "registry-bucket-name", "", "NiFi Registry bucket name")
	fs.StringVar(&cfg.RegistryFlowID, "registry-flow-id", "", "NiFi Registry flow identifier")
	fs.StringVar(&cfg.RegistryFlowName, "registry-flow-name", "", "NiFi Registry flow name")
	fs.BoolVar(&cfg.RegistryCreateBucket, "registry-create-bucket", false, "Create the target NiFi Registry bucket if it is missing")
	fs.BoolVar(&cfg.RegistryCreateFlow, "registry-create-flow", false, "Create the target NiFi Registry flow if it is missing")
	fs.StringVar(&cfg.RegistryBearerToken, "registry-bearer-token", "", "Bearer token for NiFi Registry")
	fs.StringVar(&cfg.RegistryBearerTokenEnv, "registry-bearer-token-env", "", "Environment variable containing the NiFi Registry Bearer token")
	fs.StringVar(&cfg.RegistryBasicUsername, "registry-basic-username", "", "Basic auth username for NiFi Registry")
	fs.StringVar(&cfg.RegistryBasicPassword, "registry-basic-password", "", "Basic auth password for NiFi Registry")
	fs.StringVar(&cfg.RegistryBasicPasswordEnv, "registry-basic-password-env", "", "Environment variable containing the NiFi Registry basic auth password")
	fs.BoolVar(&cfg.RegistryInsecureSkipTLSVerify, "registry-insecure-skip-tls-verify", false, "Skip TLS verification for NiFi Registry connections")
	fs.StringVar(&cfg.OutputDir, "output-dir", "", "Output directory")
	fs.StringVar(&cfg.ReportJSONPath, "report-json", "", "Explicit publish report JSON path")
	fs.StringVar(&cfg.ReportMarkdownPath, "report-md", "", "Explicit publish report Markdown path")
	fs.StringVar(&cfg.PublishName, "name", "", "Publish name")

	if err := fs.Parse(args); err != nil {
		return exitCodeUsage
	}

	result, err := RunPublish(cfg)
	if err != nil {
		return printError(stderr, err)
	}

	fmt.Fprintf(stdout, "Publish %s completed\n", result.Report.Metadata.Name)
	fmt.Fprintf(stdout, "Publisher: %s\n", result.Report.Publisher)
	fmt.Fprintf(stdout, "Published path: %s\n", result.PublishedPath)
	fmt.Fprintf(stdout, "Files: %d\n", result.Report.Summary.Files)
	fmt.Fprintf(stdout, "Report JSON: %s\n", result.ReportJSONPath)
	fmt.Fprintf(stdout, "Report Markdown: %s\n", result.ReportMarkdownPath)
	return exitCodeSuccess
}

func runRunCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var cfg RunConfig
	cfg.Analyze.SourceFormat = SourceFormatAuto
	cfg.Analyze.FailOn = "blocked"
	cfg.Validate.InputFormat = SourceFormatAuto
	cfg.Publish.InputFormat = SourceFormatAuto
	var rulePacks stringSliceFlag

	fs.StringVar(&cfg.Analyze.SourcePath, "source", "", "Source flow artifact path")
	fs.StringVar((*string)(&cfg.Analyze.SourceFormat), "source-format", string(SourceFormatAuto), "Source artifact format")
	fs.StringVar(&cfg.Analyze.SourceVersion, "source-version", "", "Source NiFi version")
	fs.StringVar(&cfg.Analyze.TargetVersion, "target-version", "", "Target NiFi version")
	fs.Var(&rulePacks, "rule-pack", "Rule pack path")
	fs.StringVar(&cfg.OutputDir, "output-dir", "", "Output directory")
	fs.StringVar(&cfg.ReportJSONPath, "report-json", "", "Explicit run report JSON path")
	fs.StringVar(&cfg.ReportMarkdownPath, "report-md", "", "Explicit run report Markdown path")
	fs.StringVar(&cfg.RunName, "name", "", "Run name")
	fs.StringVar(&cfg.Analyze.FailOn, "fail-on", "blocked", "Failure threshold for analyze")
	fs.BoolVar(&cfg.Analyze.Strict, "strict", false, "Treat unknown analysis gaps as blocked findings")
	fs.StringVar(&cfg.Analyze.ExtensionsManifestPath, "extensions-manifest", "", "Optional extensions manifest path")
	fs.StringVar(&cfg.Analyze.ParameterContextsPath, "parameter-contexts", "", "Optional parameter contexts path")
	fs.BoolVar(&cfg.Analyze.AllowUnsupportedVersionPair, "allow-unsupported-version-pair", false, "Continue analysis when no rule pack matches the selected version pair")
	fs.StringVar(&cfg.Validate.TargetAPIURL, "target-api-url", "", "Optional target NiFi API base URL")
	fs.StringVar(&cfg.Validate.TargetAPIBearerToken, "target-api-bearer-token", "", "Optional Bearer token for target NiFi API")
	fs.StringVar(&cfg.Validate.TargetAPIBearerTokenEnv, "target-api-bearer-token-env", "", "Optional environment variable containing the target NiFi API Bearer token")
	fs.BoolVar(&cfg.Validate.TargetAPIInsecureSkipTLSVerify, "target-api-insecure-skip-tls-verify", false, "Skip TLS verification for target NiFi API connections")
	fs.StringVar(&cfg.Validate.TargetProcessGroupID, "target-process-group-id", "", "Optional target process group ID for pre-import validation")
	fs.StringVar(&cfg.Validate.TargetProcessGroupMode, "target-process-group-mode", "auto", "Target process group mode: auto, replace, or update")
	fs.BoolVar(&cfg.PublishEnabled, "publish", false, "Publish after successful validation")
	fs.StringVar(&cfg.Publish.Publisher, "publisher", "", "Publish target: fs, git-registry-dir, or nifi-registry")
	fs.StringVar(&cfg.Publish.Destination, "destination", "", "Destination path for fs or git-registry-dir publish")
	fs.StringVar(&cfg.Publish.Bucket, "bucket", "", "Bucket or folder name for git-registry-dir publish")
	fs.StringVar(&cfg.Publish.Flow, "flow", "", "Flow name or directory for git-registry-dir publish")
	fs.StringVar(&cfg.Publish.FileName, "file-name", "", "Optional destination file name")
	fs.StringVar(&cfg.Publish.RegistryURL, "registry-url", "", "NiFi Registry base URL for nifi-registry publish")
	fs.StringVar(&cfg.Publish.RegistryBucketID, "registry-bucket-id", "", "NiFi Registry bucket identifier")
	fs.StringVar(&cfg.Publish.RegistryBucketName, "registry-bucket-name", "", "NiFi Registry bucket name")
	fs.StringVar(&cfg.Publish.RegistryFlowID, "registry-flow-id", "", "NiFi Registry flow identifier")
	fs.StringVar(&cfg.Publish.RegistryFlowName, "registry-flow-name", "", "NiFi Registry flow name")
	fs.BoolVar(&cfg.Publish.RegistryCreateBucket, "registry-create-bucket", false, "Create the target NiFi Registry bucket if it is missing")
	fs.BoolVar(&cfg.Publish.RegistryCreateFlow, "registry-create-flow", false, "Create the target NiFi Registry flow if it is missing")
	fs.StringVar(&cfg.Publish.RegistryBearerToken, "registry-bearer-token", "", "Bearer token for NiFi Registry")
	fs.StringVar(&cfg.Publish.RegistryBearerTokenEnv, "registry-bearer-token-env", "", "Environment variable containing the NiFi Registry Bearer token")
	fs.StringVar(&cfg.Publish.RegistryBasicUsername, "registry-basic-username", "", "Basic auth username for NiFi Registry")
	fs.StringVar(&cfg.Publish.RegistryBasicPassword, "registry-basic-password", "", "Basic auth password for NiFi Registry")
	fs.StringVar(&cfg.Publish.RegistryBasicPasswordEnv, "registry-basic-password-env", "", "Environment variable containing the NiFi Registry basic auth password")
	fs.BoolVar(&cfg.Publish.RegistryInsecureSkipTLSVerify, "registry-insecure-skip-tls-verify", false, "Skip TLS verification for NiFi Registry connections")

	if err := fs.Parse(args); err != nil {
		return exitCodeUsage
	}

	cfg.Analyze.RulePackPaths = rulePacks
	cfg.Rewrite.SourcePath = cfg.Analyze.SourcePath
	cfg.Rewrite.SourceFormat = cfg.Analyze.SourceFormat
	cfg.Rewrite.SourceVersion = cfg.Analyze.SourceVersion
	cfg.Rewrite.TargetVersion = cfg.Analyze.TargetVersion
	cfg.Rewrite.RulePackPaths = append([]string(nil), rulePacks...)
	cfg.Rewrite.AllowUnsupportedVersionPair = cfg.Analyze.AllowUnsupportedVersionPair
	cfg.Validate.TargetVersion = cfg.Analyze.TargetVersion
	cfg.Validate.ExtensionsManifestPath = cfg.Analyze.ExtensionsManifestPath

	result, err := RunRun(cfg)
	if err != nil {
		return printError(stderr, err)
	}

	fmt.Fprintf(stdout, "Run %s completed with status=%s\n", result.Report.Metadata.Name, result.Report.Summary.Status)
	for _, step := range result.Report.Steps {
		fmt.Fprintf(stdout, "Step %s: %s\n", step.Name, step.Status)
	}
	fmt.Fprintf(stdout, "Run report JSON: %s\n", result.ReportJSONPath)
	fmt.Fprintf(stdout, "Run report Markdown: %s\n", result.ReportMarkdownPath)
	if result.ExceededFailOn || result.Blocked {
		return exitCodeThreshold
	}
	return exitCodeSuccess
}

func RunRulePackLint(cfg RulePackLintConfig) (*RulePackLintResult, error) {
	if len(cfg.RulePackPaths) == 0 {
		return nil, newExitError(exitCodeUsage, "at least one --rule-pack is required")
	}
	if cfg.Format != "" && cfg.Format != "text" && cfg.Format != "json" {
		return nil, newExitError(exitCodeUsage, "unsupported --format %q", cfg.Format)
	}
	packs, err := LoadRulePacks(cfg.RulePackPaths)
	if err != nil {
		return nil, err
	}
	refs := make([]RulePackRef, 0, len(packs))
	for _, pack := range packs {
		refs = append(refs, RulePackRef{Name: pack.Metadata.Name, Path: pack.Path})
	}
	return &RulePackLintResult{
		RulePacks: refs,
		Count:     len(refs),
	}, nil
}

func printError(stderr io.Writer, err error) int {
	var exitErr *ExitError
	if ok := errorAs(err, &exitErr); ok {
		fmt.Fprintln(stderr, exitErr.Error())
		return exitErr.Code
	}
	fmt.Fprintln(stderr, err)
	return exitCodeInternal
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  nifi-flow-upgrade analyze [flags]")
	fmt.Fprintln(w, "  nifi-flow-upgrade rewrite [--plan <migration-report.json>] [flags]")
	fmt.Fprintln(w, "  nifi-flow-upgrade validate --input <path> --target-version <version> [flags]")
	fmt.Fprintln(w, "  nifi-flow-upgrade publish --input <path> --publisher <fs|git-registry-dir|nifi-registry> [flags]")
	fmt.Fprintln(w, "  nifi-flow-upgrade run --source <path> --source-version <version> --target-version <version> --rule-pack <path> [flags]")
	fmt.Fprintln(w, "  nifi-flow-upgrade rule-pack lint --rule-pack <path> [--rule-pack <path>...]")
	fmt.Fprintln(w, "  nifi-flow-upgrade version")
}

func errorAs(err error, target **ExitError) bool {
	if err == nil {
		return false
	}
	typed, ok := err.(*ExitError)
	if ok {
		*target = typed
		return true
	}
	return false
}
