package flowupgrade

type SourceFormat string

const (
	SourceFormatAuto               SourceFormat = "auto"
	SourceFormatFlowJSONGZ         SourceFormat = "flow-json-gz"
	SourceFormatFlowXMLGZ          SourceFormat = "flow-xml-gz"
	SourceFormatVersionedFlowSnap  SourceFormat = "versioned-flow-snapshot"
	SourceFormatGitRegistryDir     SourceFormat = "git-registry-dir"
	SourceFormatNiFiRegistryExport SourceFormat = "nifi-registry-export"
	reportAPIVersion                            = "flow-upgrade.nifi.advisor/v1alpha1"
	rulePackKind                                = "RulePack"
	reportKind                                  = "MigrationReport"
)

var allowedSourceFormats = map[SourceFormat]struct{}{
	SourceFormatAuto:               {},
	SourceFormatFlowJSONGZ:         {},
	SourceFormatFlowXMLGZ:          {},
	SourceFormatVersionedFlowSnap:  {},
	SourceFormatGitRegistryDir:     {},
	SourceFormatNiFiRegistryExport: {},
}

type RulePack struct {
	APIVersion string           `yaml:"apiVersion"`
	Kind       string           `yaml:"kind"`
	Metadata   RulePackMetadata `yaml:"metadata"`
	Spec       RulePackSpec     `yaml:"spec"`
	Path       string           `yaml:"-"`
}

type RulePackMetadata struct {
	Name        string   `yaml:"name"`
	Title       string   `yaml:"title,omitempty"`
	Description string   `yaml:"description,omitempty"`
	Owners      []string `yaml:"owners,omitempty"`
	References  []string `yaml:"references,omitempty"`
}

type RulePackSpec struct {
	SourceVersionRange string         `yaml:"sourceVersionRange"`
	TargetVersionRange string         `yaml:"targetVersionRange"`
	AppliesToFormats   []SourceFormat `yaml:"appliesToFormats,omitempty"`
	Rules              []Rule         `yaml:"rules"`
}

type Rule struct {
	ID         string       `yaml:"id"`
	Category   string       `yaml:"category"`
	Class      string       `yaml:"class"`
	Severity   string       `yaml:"severity"`
	Message    string       `yaml:"message"`
	Selector   RuleSelector `yaml:"selector,omitempty"`
	Match      RuleMatch    `yaml:"match,omitempty"`
	Actions    []RuleAction `yaml:"actions,omitempty"`
	References []string     `yaml:"references,omitempty"`
	Notes      string       `yaml:"notes,omitempty"`
}

type RuleSelector struct {
	ComponentType  string   `yaml:"componentType,omitempty"`
	ComponentTypes []string `yaml:"componentTypes,omitempty"`
	BundleGroup    string   `yaml:"bundleGroup,omitempty"`
	BundleArtifact string   `yaml:"bundleArtifact,omitempty"`
	PropertyName   string   `yaml:"propertyName,omitempty"`
	Scope          string   `yaml:"scope,omitempty"`
}

type PropertyValueEqualsMatch struct {
	Property string `yaml:"property"`
	Value    string `yaml:"value"`
}

type PropertyValueInMatch struct {
	Property string   `yaml:"property"`
	Values   []string `yaml:"values"`
}

type PropertyValueRegexMatch struct {
	Property string `yaml:"property"`
	Regex    string `yaml:"regex"`
}

type RuleMatch struct {
	PropertyExists      string                    `yaml:"propertyExists,omitempty"`
	PropertyAbsent      string                    `yaml:"propertyAbsent,omitempty"`
	PropertyValueEquals *PropertyValueEqualsMatch `yaml:"propertyValueEquals,omitempty"`
	PropertyValueIn     *PropertyValueInMatch     `yaml:"propertyValueIn,omitempty"`
	PropertyValueRegex  *PropertyValueRegexMatch  `yaml:"propertyValueRegex,omitempty"`
	AnnotationContains  string                    `yaml:"annotationContains,omitempty"`
	ComponentNameRegex  string                    `yaml:"componentNameMatches,omitempty"`
}

type RuleAction struct {
	Type          string `yaml:"type"`
	From          string `yaml:"from,omitempty"`
	To            string `yaml:"to,omitempty"`
	Value         string `yaml:"value,omitempty"`
	Name          string `yaml:"name,omitempty"`
	Property      string `yaml:"property,omitempty"`
	Group         string `yaml:"group,omitempty"`
	Artifact      string `yaml:"artifact,omitempty"`
	ParameterName string `yaml:"parameterName,omitempty"`
	Sensitive     bool   `yaml:"sensitive,omitempty"`
}

type AnalyzeConfig struct {
	SourcePath                  string
	SourceFormat                SourceFormat
	SourceVersion               string
	TargetVersion               string
	RulePackPaths               []string
	OutputDir                   string
	ReportJSONPath              string
	ReportMarkdownPath          string
	FailOn                      string
	Strict                      bool
	AnalysisName                string
	ExtensionsManifestPath      string
	ParameterContextsPath       string
	AllowUnsupportedVersionPair bool
}

type RewriteConfig struct {
	PlanPath                    string
	SourcePath                  string
	SourceFormat                SourceFormat
	SourceVersion               string
	TargetVersion               string
	RulePackPaths               []string
	OutputDir                   string
	RewrittenFlowPath           string
	RewriteReportJSONPath       string
	RewriteReportMarkdownPath   string
	RewriteName                 string
	AllowUnsupportedVersionPair bool
}

type RulePackLintConfig struct {
	RulePackPaths []string
	Format        string
	FailOnWarn    bool
}

type ValidateConfig struct {
	InputPath                      string
	InputFormat                    SourceFormat
	TargetVersion                  string
	ExtensionsManifestPath         string
	TargetAPIURL                   string
	TargetAPIBearerToken           string
	TargetAPIBearerTokenEnv        string
	TargetAPIInsecureSkipTLSVerify bool
	TargetProcessGroupID           string
	TargetProcessGroupMode         string
	OutputDir                      string
	ReportJSONPath                 string
	ReportMarkdownPath             string
	ValidationName                 string
}

type PublishConfig struct {
	InputPath                     string
	InputFormat                   SourceFormat
	Publisher                     string
	Destination                   string
	Bucket                        string
	Flow                          string
	FileName                      string
	PublishName                   string
	ReportJSONPath                string
	ReportMarkdownPath            string
	OutputDir                     string
	RegistryURL                   string
	RegistryBucketID              string
	RegistryBucketName            string
	RegistryFlowID                string
	RegistryFlowName              string
	RegistryCreateBucket          bool
	RegistryCreateFlow            bool
	RegistryBearerToken           string
	RegistryBearerTokenEnv        string
	RegistryBasicUsername         string
	RegistryBasicPassword         string
	RegistryBasicPasswordEnv      string
	RegistryInsecureSkipTLSVerify bool
}

type RunConfig struct {
	Analyze  AnalyzeConfig
	Rewrite  RewriteConfig
	Validate ValidateConfig
	Publish  PublishConfig

	RunName            string
	OutputDir          string
	ReportJSONPath     string
	ReportMarkdownPath string

	PublishEnabled bool
}

type RulePackRef struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type MigrationReport struct {
	APIVersion string             `json:"apiVersion"`
	Kind       string             `json:"kind"`
	Metadata   ReportMetadata     `json:"metadata"`
	Source     ReportSource       `json:"source"`
	Target     ReportTarget       `json:"target"`
	RulePacks  []RulePackRef      `json:"rulePacks"`
	Summary    ReportSummary      `json:"summary"`
	Findings   []MigrationFinding `json:"findings"`
}

type ReportMetadata struct {
	Name        string `json:"name"`
	GeneratedAt string `json:"generatedAt"`
}

type ReportSource struct {
	Path        string `json:"path"`
	Format      string `json:"format"`
	NiFiVersion string `json:"nifiVersion"`
}

type ReportTarget struct {
	NiFiVersion            string `json:"nifiVersion"`
	ExtensionsManifestPath string `json:"extensionsManifestPath,omitempty"`
	TargetAPIURL           string `json:"targetApiUrl,omitempty"`
	ActualNiFiVersion      string `json:"actualNiFiVersion,omitempty"`
	TargetProcessGroupID   string `json:"targetProcessGroupId,omitempty"`
	TargetProcessGroupMode string `json:"targetProcessGroupMode,omitempty"`
}

type ReportSummary struct {
	TotalFindings int            `json:"totalFindings"`
	ByClass       map[string]int `json:"byClass"`
}

type MigrationFinding struct {
	RuleID           string            `json:"ruleId"`
	Class            string            `json:"class"`
	Severity         string            `json:"severity"`
	Component        *FindingComponent `json:"component,omitempty"`
	Message          string            `json:"message"`
	Notes            string            `json:"notes,omitempty"`
	References       []string          `json:"references,omitempty"`
	Evidence         []FindingEvidence `json:"evidence,omitempty"`
	SuggestedActions []SuggestedAction `json:"suggestedActions,omitempty"`
}

type FindingComponent struct {
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Type  string `json:"type,omitempty"`
	Scope string `json:"scope,omitempty"`
	Path  string `json:"path,omitempty"`
}

type SuggestedAction struct {
	Type   string         `json:"type"`
	Params map[string]any `json:"params,omitempty"`
}

type FindingEvidence struct {
	Type          string   `json:"type"`
	Field         string   `json:"field,omitempty"`
	ActualValue   string   `json:"actualValue,omitempty"`
	ExpectedValue string   `json:"expectedValue,omitempty"`
	AllowedValues []string `json:"allowedValues,omitempty"`
}

type AnalyzeResult struct {
	Report             MigrationReport
	ReportJSONPath     string
	ReportMarkdownPath string
	ExceededFailOn     bool
}

type RewriteResult struct {
	Report                RewriteReport
	RewrittenFlowPath     string
	RewriteReportJSONPath string
	RewriteReportMDPath   string
}

type ValidateResult struct {
	Report             ValidationReport
	ReportJSONPath     string
	ReportMarkdownPath string
	Blocked            bool
}

type PublishResult struct {
	Report             PublishReport
	PublishedPath      string
	ReportJSONPath     string
	ReportMarkdownPath string
}

type RunResult struct {
	Report             RunReport
	ReportJSONPath     string
	ReportMarkdownPath string
	ExceededFailOn     bool
	Blocked            bool
}

type RulePackLintResult struct {
	RulePacks    []RulePackRef         `json:"rulePacks"`
	Count        int                   `json:"count"`
	WarningCount int                   `json:"warningCount,omitempty"`
	Warnings     []RulePackLintWarning `json:"warnings,omitempty"`
	FailedOnWarn bool                  `json:"failedOnWarn,omitempty"`
}

type RulePackLintWarning struct {
	RulePackName string `json:"rulePackName"`
	RulePackPath string `json:"rulePackPath"`
	RuleID       string `json:"ruleId"`
	Message      string `json:"message"`
}

type FlowDocument struct {
	Format          SourceFormat
	RawText         string
	RootAnnotations []string
	RootVariables   map[string]string
	Components      []FlowComponent
}

type FlowComponent struct {
	ID             string
	Name           string
	Type           string
	BundleGroup    string
	BundleArtifact string
	Scope          string
	Path           string
	Properties     map[string]string
	Annotations    []string
}

type ExtensionsManifest struct {
	APIVersion string                     `yaml:"apiVersion"`
	Kind       string                     `yaml:"kind"`
	Metadata   ExtensionsManifestMetadata `yaml:"metadata"`
	Spec       ExtensionsManifestSpec     `yaml:"spec"`
	Path       string                     `yaml:"-"`
}

type ExtensionsManifestMetadata struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
}

type ExtensionsManifestSpec struct {
	NiFiVersion string                        `yaml:"nifiVersion,omitempty"`
	Components  []ExtensionsManifestComponent `yaml:"components"`
}

type ExtensionsManifestComponent struct {
	Type           string `yaml:"type"`
	Scope          string `yaml:"scope,omitempty"`
	BundleGroup    string `yaml:"bundleGroup,omitempty"`
	BundleArtifact string `yaml:"bundleArtifact,omitempty"`
}

type RewriteReport struct {
	APIVersion string             `json:"apiVersion"`
	Kind       string             `json:"kind"`
	Metadata   ReportMetadata     `json:"metadata"`
	Source     ReportSource       `json:"source"`
	Target     ReportTarget       `json:"target"`
	RulePacks  []RulePackRef      `json:"rulePacks"`
	Summary    RewriteSummary     `json:"summary"`
	Operations []RewriteOperation `json:"operations"`
}

type RewriteSummary struct {
	TotalOperations   int            `json:"totalOperations"`
	AppliedOperations int            `json:"appliedOperations"`
	SkippedOperations int            `json:"skippedOperations"`
	ByClass           map[string]int `json:"byClass"`
	AppliedByClass    map[string]int `json:"appliedByClass"`
}

type RewriteOperation struct {
	RuleID     string            `json:"ruleId"`
	Class      string            `json:"class"`
	ActionType string            `json:"actionType"`
	Status     string            `json:"status"`
	Message    string            `json:"message,omitempty"`
	Reason     string            `json:"reason,omitempty"`
	Notes      string            `json:"notes,omitempty"`
	References []string          `json:"references,omitempty"`
	Component  *FindingComponent `json:"component,omitempty"`
	Evidence   []FindingEvidence `json:"evidence,omitempty"`
	Params     map[string]any    `json:"params,omitempty"`
}

type ValidationReport struct {
	APIVersion string             `json:"apiVersion"`
	Kind       string             `json:"kind"`
	Metadata   ReportMetadata     `json:"metadata"`
	Source     ReportSource       `json:"source"`
	Target     ReportTarget       `json:"target"`
	Summary    ReportSummary      `json:"summary"`
	Findings   []MigrationFinding `json:"findings"`
}

type PublishReport struct {
	APIVersion    string         `json:"apiVersion"`
	Kind          string         `json:"kind"`
	Metadata      ReportMetadata `json:"metadata"`
	Source        ReportSource   `json:"source"`
	Publisher     string         `json:"publisher"`
	Destination   string         `json:"destination"`
	PublishedPath string         `json:"publishedPath"`
	Summary       PublishSummary `json:"summary"`
}

type PublishSummary struct {
	Publisher string `json:"publisher"`
	Files     int    `json:"files"`
}

type RunReport struct {
	APIVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Metadata   ReportMetadata `json:"metadata"`
	Source     ReportSource   `json:"source"`
	Target     ReportTarget   `json:"target"`
	Summary    RunSummary     `json:"summary"`
	Steps      []RunStep      `json:"steps"`
}

type RunSummary struct {
	Status                   string `json:"status"`
	CompletedSteps           int    `json:"completedSteps"`
	PublishEnabled           bool   `json:"publishEnabled"`
	AnalyzeThresholdExceeded bool   `json:"analyzeThresholdExceeded"`
	ValidationBlocked        bool   `json:"validationBlocked"`
}

type RunStep struct {
	Name           string `json:"name"`
	Status         string `json:"status"`
	Message        string `json:"message,omitempty"`
	ReportJSONPath string `json:"reportJsonPath,omitempty"`
	ReportMDPath   string `json:"reportMarkdownPath,omitempty"`
	OutputPath     string `json:"outputPath,omitempty"`
}
