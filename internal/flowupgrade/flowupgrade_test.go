package flowupgrade

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	semver "github.com/Masterminds/semver/v3"
)

func TestRunRulePackLintSampleSucceeds(t *testing.T) {
	t.Parallel()

	result, err := RunRulePackLint(RulePackLintConfig{
		RulePackPaths: []string{filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.27-to-2.0.sample.yaml")},
	})
	if err != nil {
		t.Fatalf("RunRulePackLint() error = %v", err)
	}
	if result.Count != 1 {
		t.Fatalf("expected 1 rule pack, got %d", result.Count)
	}
}

func TestRunRulePackLintAllExamplePacksSucceed(t *testing.T) {
	t.Parallel()

	paths, err := filepath.Glob(filepath.Join("..", "..", "examples", "rulepacks", "*.yaml"))
	if err != nil {
		t.Fatalf("glob example rule packs: %v", err)
	}
	if len(paths) == 0 {
		t.Fatalf("expected example rule packs")
	}

	result, err := RunRulePackLint(RulePackLintConfig{
		RulePackPaths: paths,
	})
	if err != nil {
		t.Fatalf("RunRulePackLint() error = %v", err)
	}
	if result.Count != len(paths) {
		t.Fatalf("expected %d rule packs, got %d", len(paths), result.Count)
	}
	if result.WarningCount != 0 {
		t.Fatalf("expected 0 lint warnings for bundled example rule packs, got %d", result.WarningCount)
	}
}

func TestRunRulePackLintWarnsOnPropertyExists(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	rulePackPath := filepath.Join(tmpDir, "lint-warning.yaml")
	if err := os.WriteFile(rulePackPath, []byte(`
apiVersion: flow-upgrade.nifi.advisor/v1alpha1
kind: RulePack
metadata:
  name: lint-warning
spec:
  sourceVersionRange: ">=1.0.0 <1.1.0"
  targetVersionRange: ">=1.1.0 <1.2.0"
  rules:
    - id: test.property-exists.warning
      category: manual-inspection
      class: manual-inspection
      severity: warning
      message: Review property existence.
      selector:
        scope: processor
        componentType: org.apache.nifi.processors.standard.InvokeHTTP
      match:
        propertyExists: HTTP URL
`), 0o644); err != nil {
		t.Fatalf("write rule pack: %v", err)
	}

	result, err := RunRulePackLint(RulePackLintConfig{
		RulePackPaths: []string{rulePackPath},
	})
	if err != nil {
		t.Fatalf("RunRulePackLint() error = %v", err)
	}
	if result.WarningCount != 1 {
		t.Fatalf("expected 1 lint warning, got %d", result.WarningCount)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("expected 1 lint warning entry, got %d", len(result.Warnings))
	}
	if result.Warnings[0].RuleID != "test.property-exists.warning" {
		t.Fatalf("unexpected warning rule id %q", result.Warnings[0].RuleID)
	}
	if result.FailedOnWarn {
		t.Fatalf("did not expect fail-on-warn without config")
	}
}

func TestRunRulePackLintFailOnWarn(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	rulePackPath := filepath.Join(tmpDir, "lint-warning.yaml")
	if err := os.WriteFile(rulePackPath, []byte(`
apiVersion: flow-upgrade.nifi.advisor/v1alpha1
kind: RulePack
metadata:
  name: lint-warning
spec:
  sourceVersionRange: ">=1.0.0 <1.1.0"
  targetVersionRange: ">=1.1.0 <1.2.0"
  rules:
    - id: test.property-exists.warning
      category: manual-inspection
      class: manual-inspection
      severity: warning
      message: Review property existence.
      selector:
        scope: processor
        componentType: org.apache.nifi.processors.standard.InvokeHTTP
      match:
        propertyExists: HTTP URL
`), 0o644); err != nil {
		t.Fatalf("write rule pack: %v", err)
	}

	result, err := RunRulePackLint(RulePackLintConfig{
		RulePackPaths: []string{rulePackPath},
		FailOnWarn:    true,
	})
	if err != nil {
		t.Fatalf("RunRulePackLint() error = %v", err)
	}
	if !result.FailedOnWarn {
		t.Fatalf("expected fail-on-warn to mark result as failed")
	}
}

func TestRunAnalyzeWritesReportsAndThreshold(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "id": "root-1",
    "comments": "root has #{legacy.variable}",
    "processors": [
      {
        "identifier": "proc-1",
        "name": "ConsumeKafkaOrders",
        "type": "org.apache.nifi.processors.kafka.pubsub.ConsumeKafka",
        "bundle": {
          "group": "org.apache.nifi",
          "artifact": "nifi-kafka-1-processors"
        },
        "properties": {
          "SSL Context Service": "ssl-context-1"
        }
      }
    ]
  }
}`)

	outputDir := filepath.Join(tmpDir, "out")
	result, err := RunAnalyze(AnalyzeConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowJSONGZ,
		SourceVersion: "1.27.0",
		TargetVersion: "2.0.0",
		RulePackPaths: []string{filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.27-to-2.0.sample.yaml")},
		OutputDir:     outputDir,
		AnalysisName:  "test-analysis",
	})
	if err != nil {
		t.Fatalf("RunAnalyze() error = %v", err)
	}
	if !result.ExceededFailOn {
		t.Fatalf("expected blocked threshold to be exceeded")
	}

	reportJSON, err := os.ReadFile(result.ReportJSONPath)
	if err != nil {
		t.Fatalf("read report json: %v", err)
	}
	var report MigrationReport
	if err := json.Unmarshal(reportJSON, &report); err != nil {
		t.Fatalf("unmarshal report json: %v", err)
	}
	if report.Summary.ByClass["blocked"] != 1 {
		t.Fatalf("expected 1 blocked finding, got %d", report.Summary.ByClass["blocked"])
	}
	if report.Summary.TotalFindings != 3 {
		t.Fatalf("expected 3 findings, got %d", report.Summary.TotalFindings)
	}
	if report.Findings[0].Component == nil && report.Findings[1].Component == nil && report.Findings[2].Component == nil {
		t.Fatalf("expected at least one finding to include component context")
	}

	reportMD, err := os.ReadFile(result.ReportMarkdownPath)
	if err != nil {
		t.Fatalf("read report markdown: %v", err)
	}
	if !strings.Contains(string(reportMD), "Resolve blocked findings before upgrade.") {
		t.Fatalf("expected markdown report to contain next-step guidance")
	}
}

func TestRunAnalyzeUnsupportedVersionPairAllowed(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	writeGzipFile(t, sourcePath, `{"name":"example"}`)

	result, err := RunAnalyze(AnalyzeConfig{
		SourcePath:                  sourcePath,
		SourceFormat:                SourceFormatFlowJSONGZ,
		SourceVersion:               "2.4.0",
		TargetVersion:               "2.8.0",
		RulePackPaths:               []string{filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.27-to-2.0.sample.yaml")},
		OutputDir:                   filepath.Join(tmpDir, "out"),
		AnalysisName:                "unsupported-pair",
		AllowUnsupportedVersionPair: true,
	})
	if err != nil {
		t.Fatalf("RunAnalyze() error = %v", err)
	}
	if len(result.Report.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(result.Report.Findings))
	}
	if result.Report.Findings[0].RuleID != "system.unsupported-version-pair" {
		t.Fatalf("unexpected finding rule id %q", result.Report.Findings[0].RuleID)
	}
}

func TestRunAnalyzePopulatesComponentIdentity(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "processors": [
      {
        "id": "proc-identity",
        "name": "ConsumeKafkaOrders",
        "type": "org.apache.nifi.processors.kafka.pubsub.ConsumeKafka",
        "bundle": {
          "group": "org.apache.nifi",
          "artifact": "nifi-kafka-1-processors"
        },
        "properties": {
          "SSL Context Service": "ssl-context-1"
        }
      }
    ]
  }
}`)

	result, err := RunAnalyze(AnalyzeConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowJSONGZ,
		SourceVersion: "1.27.0",
		TargetVersion: "2.0.0",
		RulePackPaths: []string{filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.27-to-2.0.sample.yaml")},
		OutputDir:     filepath.Join(tmpDir, "out"),
		AnalysisName:  "component-identity",
		FailOn:        "never",
	})
	if err != nil {
		t.Fatalf("RunAnalyze() error = %v", err)
	}

	var kafkaFinding *MigrationFinding
	for i := range result.Report.Findings {
		if result.Report.Findings[i].RuleID == "core.kafka.bundle-rename" {
			kafkaFinding = &result.Report.Findings[i]
			break
		}
	}
	if kafkaFinding == nil {
		t.Fatalf("expected kafka bundle rename finding")
	}
	if kafkaFinding.Component == nil {
		t.Fatalf("expected kafka finding to include component context")
	}
	if kafkaFinding.Component.ID != "proc-identity" {
		t.Fatalf("expected component id proc-identity, got %q", kafkaFinding.Component.ID)
	}
	if kafkaFinding.Component.Name != "ConsumeKafkaOrders" {
		t.Fatalf("expected component name ConsumeKafkaOrders, got %q", kafkaFinding.Component.Name)
	}
}

func TestRunAnalyzeSupportsLegacyFlowXMLGZ(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.xml.gz")
	writeGzipFile(t, sourcePath, `<?xml version="1.0" encoding="UTF-8"?>
<flowController>
  <rootGroup>
    <id>root-1</id>
    <name>Root</name>
    <comment>root has #{legacy.variable}</comment>
    <variable>
      <name>legacy.api.url</name>
      <value>https://legacy.example</value>
    </variable>
    <processor>
      <id>proc-1</id>
      <name>FetchHTTP</name>
      <class>org.apache.nifi.processors.standard.GetHTTP</class>
      <property>
        <name>URL</name>
        <value>https://example.com/data.txt</value>
      </property>
      <property>
        <name>Filename</name>
        <value>data.txt</value>
      </property>
    </processor>
    <controllerService>
      <id>cache-1</id>
      <name>MapCache</name>
      <class>org.apache.nifi.distributed.cache.client.DistributedMapCacheClientService</class>
    </controllerService>
  </rootGroup>
</flowController>`)

	result, err := RunAnalyze(AnalyzeConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowXMLGZ,
		SourceVersion: "1.27.0",
		TargetVersion: "2.0.0",
		RulePackPaths: []string{filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.27-to-2.0.official.yaml")},
		OutputDir:     filepath.Join(tmpDir, "out"),
		AnalysisName:  "legacy-xml",
		FailOn:        "never",
	})
	if err != nil {
		t.Fatalf("RunAnalyze() error = %v", err)
	}

	ruleIDs := map[string]bool{}
	for _, finding := range result.Report.Findings {
		ruleIDs[finding.RuleID] = true
	}
	expected := []string{
		"core.get-http.replace",
		"core.distributed-map-cache-client.renamed",
		"core.variable-registry.removed",
	}
	for _, ruleID := range expected {
		if !ruleIDs[ruleID] {
			t.Fatalf("expected XML-backed finding for %s", ruleID)
		}
	}
	if result.Report.Source.Format != string(SourceFormatFlowXMLGZ) {
		t.Fatalf("expected source format flow-xml-gz, got %q", result.Report.Source.Format)
	}
}

func TestRunAnalyzeSupportsWrappedComponentEntries(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	writeGzipFile(t, sourcePath, `{
  "processGroupFlow": {
    "flow": {
      "processors": [
        {
          "id": "wrapped-proc-1",
          "component": {
            "name": "WrappedConsumeKafka",
            "type": "org.apache.nifi.processors.kafka.pubsub.ConsumeKafka",
            "bundle": {
              "group": "org.apache.nifi",
              "artifact": "nifi-kafka-1-processors"
            },
            "properties": {
              "SSL Context Service": "ssl-context-1"
            }
          }
        }
      ]
    }
  }
}`)

	result, err := RunAnalyze(AnalyzeConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowJSONGZ,
		SourceVersion: "1.27.0",
		TargetVersion: "2.0.0",
		RulePackPaths: []string{filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.27-to-2.0.sample.yaml")},
		OutputDir:     filepath.Join(tmpDir, "out"),
		AnalysisName:  "wrapped-component",
		FailOn:        "never",
	})
	if err != nil {
		t.Fatalf("RunAnalyze() error = %v", err)
	}

	var kafkaFindings int
	for _, finding := range result.Report.Findings {
		if finding.RuleID != "core.kafka.bundle-rename" {
			continue
		}
		kafkaFindings++
		if finding.Component == nil {
			t.Fatalf("expected wrapped component finding to include component context")
		}
		if finding.Component.ID != "wrapped-proc-1" {
			t.Fatalf("expected wrapped component id wrapped-proc-1, got %q", finding.Component.ID)
		}
	}
	if kafkaFindings != 1 {
		t.Fatalf("expected exactly 1 kafka finding for wrapped component, got %d", kafkaFindings)
	}
}

func TestRunAnalyzeMatchesRootVariables(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	rulePackPath := filepath.Join(tmpDir, "variables-rulepack.yaml")

	writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "variables": {
      "legacy.api.url": "https://legacy.example"
    }
  }
}`)

	if err := os.WriteFile(rulePackPath, []byte(`
apiVersion: flow-upgrade.nifi.advisor/v1alpha1
kind: RulePack
metadata:
  name: root-variable-pack
spec:
  sourceVersionRange: ">=1.27.0 <2.0.0"
  targetVersionRange: ">=2.0.0 <2.1.0"
  appliesToFormats:
    - flow-json-gz
  rules:
    - id: root.variable.detected
      category: variable-migration
      class: blocked
      severity: error
      message: Root variable detected.
      selector:
        scope: flow-root
      match:
        propertyValueEquals:
          property: legacy.api.url
          value: https://legacy.example
`), 0o644); err != nil {
		t.Fatalf("write rule pack: %v", err)
	}

	result, err := RunAnalyze(AnalyzeConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowJSONGZ,
		SourceVersion: "1.27.0",
		TargetVersion: "2.0.0",
		RulePackPaths: []string{rulePackPath},
		OutputDir:     filepath.Join(tmpDir, "out"),
		AnalysisName:  "root-variables",
	})
	if err != nil {
		t.Fatalf("RunAnalyze() error = %v", err)
	}
	if len(result.Report.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(result.Report.Findings))
	}
	if result.Report.Findings[0].RuleID != "root.variable.detected" {
		t.Fatalf("unexpected rule id %q", result.Report.Findings[0].RuleID)
	}
}

func TestRunAnalyzeMatchesControllerServiceNestedConfig(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	rulePackPath := filepath.Join(tmpDir, "controller-service-rulepack.yaml")

	writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "controllerServices": [
      {
        "id": "ssl-1",
        "name": "SharedSSL",
        "type": "org.apache.nifi.ssl.StandardRestrictedSSLContextService",
        "config": {
          "properties": {
            "Keystore Type": "PKCS12"
          }
        }
      }
    ]
  }
}`)

	if err := os.WriteFile(rulePackPath, []byte(`
apiVersion: flow-upgrade.nifi.advisor/v1alpha1
kind: RulePack
metadata:
  name: controller-service-pack
spec:
  sourceVersionRange: ">=1.27.0 <2.0.0"
  targetVersionRange: ">=2.0.0 <2.1.0"
  appliesToFormats:
    - flow-json-gz
  rules:
    - id: controller.ssl.keystore
      category: manual-inspection
      class: manual-inspection
      severity: warning
      message: Review SSL controller service configuration.
      selector:
        scope: controller-service
      match:
        propertyValueEquals:
          property: Keystore Type
          value: PKCS12
`), 0o644); err != nil {
		t.Fatalf("write rule pack: %v", err)
	}

	result, err := RunAnalyze(AnalyzeConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowJSONGZ,
		SourceVersion: "1.27.0",
		TargetVersion: "2.0.0",
		RulePackPaths: []string{rulePackPath},
		OutputDir:     filepath.Join(tmpDir, "out"),
		AnalysisName:  "controller-service",
		FailOn:        "never",
	})
	if err != nil {
		t.Fatalf("RunAnalyze() error = %v", err)
	}
	if len(result.Report.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(result.Report.Findings))
	}
	finding := result.Report.Findings[0]
	if finding.Component == nil {
		t.Fatalf("expected controller-service finding to include component context")
	}
	if finding.Component.Scope != "controller-service" {
		t.Fatalf("expected scope controller-service, got %q", finding.Component.Scope)
	}
	if !strings.Contains(finding.Component.Path, "controllerServices") {
		t.Fatalf("expected controller service path, got %q", finding.Component.Path)
	}
	if len(finding.Evidence) == 0 {
		t.Fatalf("expected controller-service finding to include evidence")
	}
	if finding.Evidence[0].Field != "Keystore Type" {
		t.Fatalf("expected evidence field Keystore Type, got %q", finding.Evidence[0].Field)
	}
	if finding.Evidence[0].ActualValue != "PKCS12" {
		t.Fatalf("expected evidence actual value PKCS12, got %q", finding.Evidence[0].ActualValue)
	}
}

func TestRunAnalyzeMatchesParameterContextParameters(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	rulePackPath := filepath.Join(tmpDir, "parameter-context-rulepack.yaml")

	writeGzipFile(t, sourcePath, `{
  "parameterContexts": [
    {
      "id": "pc-1",
      "component": {
        "name": "OrdersContext",
        "parameters": [
          {
            "parameter": {
              "name": "legacy.api.url",
              "value": "https://legacy.example"
            }
          }
        ]
      }
    }
  ]
}`)

	if err := os.WriteFile(rulePackPath, []byte(`
apiVersion: flow-upgrade.nifi.advisor/v1alpha1
kind: RulePack
metadata:
  name: parameter-context-pack
spec:
  sourceVersionRange: ">=1.27.0 <2.0.0"
  targetVersionRange: ">=2.0.0 <2.1.0"
  appliesToFormats:
    - flow-json-gz
  rules:
    - id: parameter-context.legacy.url
      category: variable-migration
      class: manual-change
      severity: warning
      message: Legacy parameter should be reviewed.
      selector:
        scope: parameter-context
      match:
        propertyValueEquals:
          property: legacy.api.url
          value: https://legacy.example
`), 0o644); err != nil {
		t.Fatalf("write rule pack: %v", err)
	}

	result, err := RunAnalyze(AnalyzeConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowJSONGZ,
		SourceVersion: "1.27.0",
		TargetVersion: "2.0.0",
		RulePackPaths: []string{rulePackPath},
		OutputDir:     filepath.Join(tmpDir, "out"),
		AnalysisName:  "parameter-context",
		FailOn:        "never",
	})
	if err != nil {
		t.Fatalf("RunAnalyze() error = %v", err)
	}
	if len(result.Report.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(result.Report.Findings))
	}
	finding := result.Report.Findings[0]
	if finding.Component == nil {
		t.Fatalf("expected parameter-context finding to include component context")
	}
	if finding.Component.Scope != "parameter-context" {
		t.Fatalf("expected scope parameter-context, got %q", finding.Component.Scope)
	}
	if finding.Component.Name != "OrdersContext" {
		t.Fatalf("expected parameter-context name OrdersContext, got %q", finding.Component.Name)
	}
	if len(finding.Evidence) == 0 {
		t.Fatalf("expected parameter-context finding to include evidence")
	}
	if finding.Evidence[0].Field != "legacy.api.url" {
		t.Fatalf("expected evidence field legacy.api.url, got %q", finding.Evidence[0].Field)
	}
	if finding.Evidence[0].ActualValue != "https://legacy.example" {
		t.Fatalf("expected evidence actual value https://legacy.example, got %q", finding.Evidence[0].ActualValue)
	}
}

func TestRunAnalyzeBlocksPre127DirectUpgradeTo20(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "id": "root-1"
  }
}`)

	result, err := RunAnalyze(AnalyzeConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowJSONGZ,
		SourceVersion: "1.21.0",
		TargetVersion: "2.0.0",
		RulePackPaths: []string{filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.0-to-1.26-pre-2.0.blocked.yaml")},
		OutputDir:     filepath.Join(tmpDir, "out"),
		AnalysisName:  "bridge-upgrade-required",
	})
	if err != nil {
		t.Fatalf("RunAnalyze() error = %v", err)
	}
	if len(result.Report.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(result.Report.Findings))
	}
	if result.Report.Findings[0].RuleID != "core.bridge-upgrade.requires-1.27" {
		t.Fatalf("unexpected rule id %q", result.Report.Findings[0].RuleID)
	}
	if result.Report.Findings[0].Class != "blocked" {
		t.Fatalf("expected blocked finding class, got %q", result.Report.Findings[0].Class)
	}
}

func TestRunAnalyzeBlocksPre127DirectUpgradeToLater2x(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "id": "root-1"
  }
}`)

	result, err := RunAnalyze(AnalyzeConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowJSONGZ,
		SourceVersion: "1.21.0",
		TargetVersion: "2.8.0",
		RulePackPaths: []string{filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.0-to-1.26-pre-2.0.blocked.yaml")},
		OutputDir:     filepath.Join(tmpDir, "out"),
		AnalysisName:  "bridge-upgrade-required-later-2x",
	})
	if err != nil {
		t.Fatalf("RunAnalyze() error = %v", err)
	}
	if len(result.Report.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(result.Report.Findings))
	}
	if result.Report.Findings[0].RuleID != "core.bridge-upgrade.requires-1.27" {
		t.Fatalf("unexpected rule id %q", result.Report.Findings[0].RuleID)
	}
	if result.Report.Findings[0].Class != "blocked" {
		t.Fatalf("expected blocked finding class, got %q", result.Report.Findings[0].Class)
	}
}

func TestRunAnalyzeBlocksPre121UntilSupportedBaseline(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "id": "root-1"
  }
}`)

	result, err := RunAnalyze(AnalyzeConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowJSONGZ,
		SourceVersion: "1.20.0",
		TargetVersion: "1.27.0",
		RulePackPaths: []string{filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.0-to-1.20-pre-1.21.blocked.yaml")},
		OutputDir:     filepath.Join(tmpDir, "out"),
		AnalysisName:  "pre-121-baseline-required",
	})
	if err != nil {
		t.Fatalf("RunAnalyze() error = %v", err)
	}
	if len(result.Report.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(result.Report.Findings))
	}
	if result.Report.Findings[0].RuleID != "core.pre-1.21.support-baseline-required" {
		t.Fatalf("unexpected rule id %q", result.Report.Findings[0].RuleID)
	}
	if result.Report.Findings[0].Class != "blocked" {
		t.Fatalf("expected blocked finding class, got %q", result.Report.Findings[0].Class)
	}
}

func TestBuiltInRulePackCoverageAcrossSupportedUpgradePairs(t *testing.T) {
	t.Parallel()

	paths, err := filepath.Glob(filepath.Join("..", "..", "examples", "rulepacks", "*.yaml"))
	if err != nil {
		t.Fatalf("glob example rule packs: %v", err)
	}
	filtered := make([]string, 0, len(paths))
	for _, path := range paths {
		if strings.Contains(filepath.Base(path), ".sample.") {
			continue
		}
		filtered = append(filtered, path)
	}
	packs, err := LoadRulePacks(filtered)
	if err != nil {
		t.Fatalf("LoadRulePacks() error = %v", err)
	}

	versions := []string{
		"1.0.0", "1.1.0", "1.2.0", "1.3.0", "1.4.0", "1.5.0", "1.6.0", "1.7.0", "1.8.0", "1.9.0",
		"1.10.0", "1.11.0", "1.12.0", "1.13.0", "1.14.0", "1.15.0", "1.16.0", "1.17.0", "1.18.0", "1.19.0",
		"1.20.0", "1.21.0", "1.22.0", "1.23.0", "1.24.0", "1.25.0", "1.26.0", "1.27.0",
		"2.0.0", "2.1.0", "2.2.0", "2.3.0", "2.4.0", "2.5.0", "2.6.0", "2.7.0", "2.7.1", "2.8.0",
	}
	const minimumSupportedTarget = "1.21.0"

	var missing []string
	for i := range versions {
		for j := i + 1; j < len(versions); j++ {
			source := versions[i]
			target := versions[j]
			if compareVersionStrings(target, minimumSupportedTarget) < 0 {
				continue
			}
			matching, err := filterMatchingRulePacks(packs, source, target, SourceFormatFlowJSONGZ)
			if err != nil {
				t.Fatalf("filterMatchingRulePacks(%s, %s) error = %v", source, target, err)
			}
			if len(matching) == 0 {
				missing = append(missing, source+"->"+target)
			}
		}
	}
	if len(missing) > 0 {
		t.Fatalf("expected built-in coverage for all supported upgrade pairs, missing %d: %s", len(missing), strings.Join(missing, ", "))
	}
}

func TestRunAnalyzeSupportsChained127To28Path(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "processors": [
      {
        "id": "base64-proc-1",
        "name": "EncodePayload",
        "type": "org.apache.nifi.processors.standard.Base64EncodeContent",
        "bundle": {
          "group": "org.apache.nifi",
          "artifact": "nifi-standard-nar",
          "version": "1.27.0"
        },
        "properties": {
          "Mode": "Encode"
        }
      }
    ]
  }
}`)

	result, err := RunAnalyze(AnalyzeConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowJSONGZ,
		SourceVersion: "1.27.0",
		TargetVersion: "2.8.0",
		RulePackPaths: []string{
			filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.27-to-2.0.official.yaml"),
			filepath.Join("..", "..", "examples", "rulepacks", "nifi-2.0-to-2.1.official.yaml"),
			filepath.Join("..", "..", "examples", "rulepacks", "nifi-2.1-to-2.2.official.yaml"),
			filepath.Join("..", "..", "examples", "rulepacks", "nifi-2.2-to-2.3.official.yaml"),
			filepath.Join("..", "..", "examples", "rulepacks", "nifi-2.3-to-2.4.official.yaml"),
			filepath.Join("..", "..", "examples", "rulepacks", "nifi-2.4-to-2.5.official.yaml"),
			filepath.Join("..", "..", "examples", "rulepacks", "nifi-2.5-to-2.6.official.yaml"),
			filepath.Join("..", "..", "examples", "rulepacks", "nifi-2.6-to-2.7.official.yaml"),
			filepath.Join("..", "..", "examples", "rulepacks", "nifi-2.7-to-2.8.official.yaml"),
		},
		OutputDir:    filepath.Join(tmpDir, "out"),
		AnalysisName: "chain-127-to-28",
	})
	if err != nil {
		t.Fatalf("RunAnalyze() error = %v", err)
	}
	if len(result.Report.RulePacks) != 9 {
		t.Fatalf("expected 9 rule packs in chained path, got %d", len(result.Report.RulePacks))
	}
	if result.Report.Summary.ByClass["auto-fix"] == 0 {
		t.Fatalf("expected chained path to include at least one auto-fix finding")
	}
}

func TestRunAnalyzeOfficial23To24PackFindsExpectedChanges(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "processors": [
      {
        "id": "listen-http-1",
        "name": "InboundHTTP",
        "type": "org.apache.nifi.processors.standard.ListenHTTP",
        "properties": {
          "Max Data to Receive per Second": "10 MB"
        }
      }
    ]
  }
}`)

	result, err := RunAnalyze(AnalyzeConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowJSONGZ,
		SourceVersion: "2.3.0",
		TargetVersion: "2.4.0",
		RulePackPaths: []string{filepath.Join("..", "..", "examples", "rulepacks", "nifi-2.3-to-2.4.official.yaml")},
		OutputDir:     filepath.Join(tmpDir, "out"),
		AnalysisName:  "official-23-to-24",
		FailOn:        "never",
	})
	if err != nil {
		t.Fatalf("RunAnalyze() error = %v", err)
	}

	ruleIDs := map[string]bool{}
	for _, finding := range result.Report.Findings {
		ruleIDs[finding.RuleID] = true
	}
	expected := []string{
		"core.listen-http.rate-limit.removed",
	}
	for _, ruleID := range expected {
		if !ruleIDs[ruleID] {
			t.Fatalf("expected finding for %s", ruleID)
		}
	}
}

func TestRunAnalyzeOfficial24To25PackFindsExpectedChanges(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "id": "root-1"
  }
}`)

	result, err := RunAnalyze(AnalyzeConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowJSONGZ,
		SourceVersion: "2.4.0",
		TargetVersion: "2.5.0",
		RulePackPaths: []string{filepath.Join("..", "..", "examples", "rulepacks", "nifi-2.4-to-2.5.official.yaml")},
		OutputDir:     filepath.Join(tmpDir, "out"),
		AnalysisName:  "official-24-to-25",
		FailOn:        "never",
	})
	if err != nil {
		t.Fatalf("RunAnalyze() error = %v", err)
	}
	if len(result.Report.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(result.Report.Findings))
	}
	if result.Report.Findings[0].RuleID != "core.custom-content-viewer.review-25" {
		t.Fatalf("unexpected rule id %q", result.Report.Findings[0].RuleID)
	}
}

func TestRunAnalyzeOfficial25To26PackFindsExpectedChanges(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "id": "root-1",
    "processors": [
      {
        "id": "compress-1",
        "name": "Compress Legacy",
        "type": "org.apache.nifi.processors.standard.CompressContent",
        "properties": {}
      },
      {
        "id": "eventhub-1",
        "name": "Get Event Hub",
        "type": "org.apache.nifi.processors.azure.eventhub.GetAzureEventHub",
        "properties": {}
      },
      {
        "id": "parquet-1",
        "name": "Convert Avro To Parquet",
        "type": "org.apache.nifi.processors.parquet.ConvertAvroToParquet",
        "properties": {}
      },
      {
        "id": "netflow-1",
        "name": "Parse Netflow",
        "type": "org.apache.nifi.processors.network.ParseNetflowv5",
        "properties": {}
      },
      {
        "id": "geohash-1",
        "name": "Geohash Record",
        "type": "org.apache.nifi.processors.geohash.GeohashRecord",
        "properties": {}
      },
      {
        "id": "sequence-1",
        "name": "Create Sequence File",
        "type": "org.apache.nifi.processors.hadoop.CreateHadoopSequenceFile",
        "properties": {}
      }
    ]
  }
}`)

	result, err := RunAnalyze(AnalyzeConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowJSONGZ,
		SourceVersion: "2.5.0",
		TargetVersion: "2.6.0",
		RulePackPaths: []string{filepath.Join("..", "..", "examples", "rulepacks", "nifi-2.5-to-2.6.official.yaml")},
		OutputDir:     filepath.Join(tmpDir, "out"),
		AnalysisName:  "official-25-to-26",
		FailOn:        "never",
	})
	if err != nil {
		t.Fatalf("RunAnalyze() error = %v", err)
	}

	ruleIDs := map[string]bool{}
	for _, finding := range result.Report.Findings {
		ruleIDs[finding.RuleID] = true
	}
	expected := []string{
		"core.compress-content.deprecated-26",
		"core.get-azure-eventhub.deprecated-26",
		"core.convert-avro-to-parquet.deprecated-26",
		"core.parse-netflowv5.deprecated-26",
		"core.geohash-record.deprecated-26",
		"core.create-hadoop-sequence-file.deprecated-26",
	}
	if len(result.Report.Findings) != len(expected) {
		t.Fatalf("expected %d findings, got %d", len(expected), len(result.Report.Findings))
	}
	for _, ruleID := range expected {
		if !ruleIDs[ruleID] {
			t.Fatalf("expected finding for %s", ruleID)
		}
	}
}

func TestRunAnalyzeOfficial20To23QuietPacksRemainEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		sourceVersion string
		targetVersion string
		rulePackPath  string
		analysisName  string
	}{
		{
			name:          "20-to-21",
			sourceVersion: "2.0.0",
			targetVersion: "2.1.0",
			rulePackPath:  filepath.Join("..", "..", "examples", "rulepacks", "nifi-2.0-to-2.1.official.yaml"),
			analysisName:  "official-20-to-21",
		},
		{
			name:          "21-to-22",
			sourceVersion: "2.1.0",
			targetVersion: "2.2.0",
			rulePackPath:  filepath.Join("..", "..", "examples", "rulepacks", "nifi-2.1-to-2.2.official.yaml"),
			analysisName:  "official-21-to-22",
		},
		{
			name:          "22-to-23",
			sourceVersion: "2.2.0",
			targetVersion: "2.3.0",
			rulePackPath:  filepath.Join("..", "..", "examples", "rulepacks", "nifi-2.2-to-2.3.official.yaml"),
			analysisName:  "official-22-to-23",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			sourcePath := filepath.Join(tmpDir, "flow.json.gz")
			writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "id": "root-1"
  }
}`)

			result, err := RunAnalyze(AnalyzeConfig{
				SourcePath:    sourcePath,
				SourceFormat:  SourceFormatFlowJSONGZ,
				SourceVersion: tt.sourceVersion,
				TargetVersion: tt.targetVersion,
				RulePackPaths: []string{tt.rulePackPath},
				OutputDir:     filepath.Join(tmpDir, "out"),
				AnalysisName:  tt.analysisName,
				FailOn:        "never",
			})
			if err != nil {
				t.Fatalf("RunAnalyze() error = %v", err)
			}
			if len(result.Report.Findings) != 0 {
				t.Fatalf("expected 0 findings, got %d", len(result.Report.Findings))
			}
		})
	}
}

func TestRunAnalyzeOfficial127To20PackFindsExpectedChanges(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "comments": "root has #{legacy.variable}",
    "processors": [
      {
        "id": "base64-1",
        "name": "Base64",
        "type": "org.apache.nifi.processors.standard.Base64EncodeContent",
        "properties": {
          "Mode": "Encode"
        }
      },
      {
        "id": "get-http-1",
        "name": "FetchHTTP",
        "type": "org.apache.nifi.processors.standard.GetHTTP",
        "properties": {
          "URL": "https://example.com/data.txt",
          "Filename": "data.txt"
        }
      },
      {
        "id": "invoke-http-1",
        "name": "InvokeWithProxy",
        "type": "org.apache.nifi.processors.standard.InvokeHTTP",
        "properties": {
          "Proxy Host": "proxy.example.com"
        }
      },
      {
        "id": "post-http-1",
        "name": "PostLegacy",
        "type": "org.apache.nifi.processors.standard.PostHTTP",
        "properties": {
          "URL": "https://example.com/submit"
        }
      },
      {
        "id": "put-jms-1",
        "name": "PublishLegacyJMS",
        "type": "org.apache.nifi.processors.standard.PutJMS",
        "properties": {
          "URL": "tcp://mq.example.com:61616"
        }
      }
    ],
    "controllerServices": [
      {
        "id": "cache-1",
        "name": "MapCache",
        "type": "org.apache.nifi.distributed.cache.client.DistributedMapCacheClientService",
        "properties": {}
      }
    ]
  }
}`)

	result, err := RunAnalyze(AnalyzeConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowJSONGZ,
		SourceVersion: "1.27.0",
		TargetVersion: "2.0.0",
		RulePackPaths: []string{filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.27-to-2.0.official.yaml")},
		OutputDir:     filepath.Join(tmpDir, "out"),
		AnalysisName:  "official-127-to-20",
		FailOn:        "never",
	})
	if err != nil {
		t.Fatalf("RunAnalyze() error = %v", err)
	}

	ruleIDs := map[string]bool{}
	for _, finding := range result.Report.Findings {
		ruleIDs[finding.RuleID] = true
	}
	expected := []string{
		"core.distributed-map-cache-client.renamed",
		"core.variable-registry.removed",
		"core.base64-encode-content.replace",
		"core.get-http.replace",
		"core.invoke-http.proxy-properties.replace",
		"core.post-http.replace",
		"core.put-jms.replace",
	}
	for _, ruleID := range expected {
		if !ruleIDs[ruleID] {
			t.Fatalf("expected finding for %s", ruleID)
		}
	}
}

func TestRunAnalyzeExtensionsManifestBlocksUnavailableComponent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	manifestPath := filepath.Join(tmpDir, "extensions-manifest.yaml")

	writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "processors": [
      {
        "id": "legacy-1",
        "name": "LegacyBase64",
        "type": "org.apache.nifi.processors.standard.Base64EncodeContent",
        "properties": {
          "Mode": "Encode"
        }
      }
    ]
  }
}`)

	if err := os.WriteFile(manifestPath, []byte(`
apiVersion: flow-upgrade.nifi.advisor/v1alpha1
kind: ExtensionsManifest
metadata:
  name: target-2x-minimal
spec:
  nifiVersion: 2.0.0
  components:
    - scope: processor
      type: org.apache.nifi.processors.standard.InvokeHTTP
`), 0o644); err != nil {
		t.Fatalf("write extensions manifest: %v", err)
	}

	result, err := RunAnalyze(AnalyzeConfig{
		SourcePath:             sourcePath,
		SourceFormat:           SourceFormatFlowJSONGZ,
		SourceVersion:          "1.27.0",
		TargetVersion:          "2.0.0",
		RulePackPaths:          []string{filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.27-to-2.0.official.yaml")},
		ExtensionsManifestPath: manifestPath,
		OutputDir:              filepath.Join(tmpDir, "out"),
		AnalysisName:           "manifest-missing-component",
		FailOn:                 "never",
	})
	if err != nil {
		t.Fatalf("RunAnalyze() error = %v", err)
	}

	var manifestFinding *MigrationFinding
	for i := range result.Report.Findings {
		if result.Report.Findings[i].RuleID == "system.target-extension-unavailable" {
			manifestFinding = &result.Report.Findings[i]
			break
		}
	}
	if manifestFinding == nil {
		t.Fatalf("expected target extension availability finding")
	}
	if manifestFinding.Component == nil || manifestFinding.Component.Type != "org.apache.nifi.processors.standard.Base64EncodeContent" {
		t.Fatalf("expected manifest finding to reference source component type")
	}
	if result.Report.Target.ExtensionsManifestPath != manifestPath {
		t.Fatalf("expected report target manifest path %q, got %q", manifestPath, result.Report.Target.ExtensionsManifestPath)
	}
}

func TestRunAnalyzeExtensionsManifestUsesPlannedReplacementType(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	manifestPath := filepath.Join(tmpDir, "extensions-manifest.yaml")

	writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "controllerServices": [
      {
        "id": "cache-1",
        "name": "MapCache",
        "type": "org.apache.nifi.distributed.cache.client.DistributedMapCacheClientService",
        "properties": {}
      }
    ]
  }
}`)

	if err := os.WriteFile(manifestPath, []byte(`
apiVersion: flow-upgrade.nifi.advisor/v1alpha1
kind: ExtensionsManifest
metadata:
  name: target-2x-cache
spec:
  nifiVersion: 2.0.0
  components:
    - scope: controller-service
      type: org.apache.nifi.distributed.cache.client.MapCacheClientService
`), 0o644); err != nil {
		t.Fatalf("write extensions manifest: %v", err)
	}

	result, err := RunAnalyze(AnalyzeConfig{
		SourcePath:             sourcePath,
		SourceFormat:           SourceFormatFlowJSONGZ,
		SourceVersion:          "1.27.0",
		TargetVersion:          "2.0.0",
		RulePackPaths:          []string{filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.27-to-2.0.official.yaml")},
		ExtensionsManifestPath: manifestPath,
		OutputDir:              filepath.Join(tmpDir, "out"),
		AnalysisName:           "manifest-planned-replacement",
		FailOn:                 "never",
	})
	if err != nil {
		t.Fatalf("RunAnalyze() error = %v", err)
	}

	for _, finding := range result.Report.Findings {
		if finding.RuleID == "system.target-extension-unavailable" {
			t.Fatalf("did not expect unavailable target extension finding when replacement type exists in manifest")
		}
	}
}

func TestRunValidateBlocksUnavailableTargetComponent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "flow.json.gz")
	manifestPath := filepath.Join(tmpDir, "extensions-manifest.yaml")

	writeGzipFile(t, inputPath, `{
  "rootGroup": {
    "processors": [
      {
        "id": "legacy-1",
        "name": "LegacyBase64",
        "type": "org.apache.nifi.processors.standard.Base64EncodeContent",
        "properties": {}
      }
    ]
  }
}`)

	if err := os.WriteFile(manifestPath, []byte(`
apiVersion: flow-upgrade.nifi.advisor/v1alpha1
kind: ExtensionsManifest
metadata:
  name: validate-target
spec:
  nifiVersion: 2.0.0
  components:
    - scope: processor
      type: org.apache.nifi.processors.standard.InvokeHTTP
`), 0o644); err != nil {
		t.Fatalf("write extensions manifest: %v", err)
	}

	result, err := RunValidate(ValidateConfig{
		InputPath:              inputPath,
		InputFormat:            SourceFormatFlowJSONGZ,
		TargetVersion:          "2.0.0",
		ExtensionsManifestPath: manifestPath,
		OutputDir:              filepath.Join(tmpDir, "out"),
		ValidationName:         "validate-missing-target",
	})
	if err != nil {
		t.Fatalf("RunValidate() error = %v", err)
	}
	if !result.Blocked {
		t.Fatalf("expected validation to be blocked")
	}
	if len(result.Report.Findings) != 1 {
		t.Fatalf("expected 1 validation finding, got %d", len(result.Report.Findings))
	}
	if result.Report.Findings[0].RuleID != "system.target-extension-unavailable" {
		t.Fatalf("unexpected validation rule id %q", result.Report.Findings[0].RuleID)
	}
}

func TestRunValidateAcceptsAvailableTargetComponent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "flow.json.gz")
	manifestPath := filepath.Join(tmpDir, "extensions-manifest.yaml")

	writeGzipFile(t, inputPath, `{
  "rootGroup": {
    "processors": [
      {
        "id": "invoke-1",
        "name": "InvokeHTTP",
        "type": "org.apache.nifi.processors.standard.InvokeHTTP",
        "properties": {}
      }
    ]
  }
}`)

	if err := os.WriteFile(manifestPath, []byte(`
apiVersion: flow-upgrade.nifi.advisor/v1alpha1
kind: ExtensionsManifest
metadata:
  name: validate-target
spec:
  nifiVersion: 2.0.0
  components:
    - scope: processor
      type: org.apache.nifi.processors.standard.InvokeHTTP
`), 0o644); err != nil {
		t.Fatalf("write extensions manifest: %v", err)
	}

	result, err := RunValidate(ValidateConfig{
		InputPath:              inputPath,
		InputFormat:            SourceFormatFlowJSONGZ,
		TargetVersion:          "2.0.0",
		ExtensionsManifestPath: manifestPath,
		OutputDir:              filepath.Join(tmpDir, "out"),
		ValidationName:         "validate-available-target",
	})
	if err != nil {
		t.Fatalf("RunValidate() error = %v", err)
	}
	if result.Blocked {
		t.Fatalf("did not expect blocked validation")
	}
	if len(result.Report.Findings) != 0 {
		t.Fatalf("expected 0 validation findings, got %d", len(result.Report.Findings))
	}
}

func TestRunValidateAcceptsNestedProcessorAgainstManifest(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "flow.json.gz")
	manifestPath := filepath.Join(tmpDir, "extensions-manifest.yaml")

	writeGzipFile(t, inputPath, `{
  "rootGroup": {
    "id": "root-1",
    "name": "Root",
    "processGroups": [
      {
        "id": "pg-1",
        "name": "Nested",
        "componentType": "PROCESS_GROUP",
        "processors": [
          {
            "id": "invoke-1",
            "name": "InvokeHTTP",
            "type": "org.apache.nifi.processors.standard.InvokeHTTP",
            "properties": {}
          }
        ]
      }
    ]
  }
}`)

	if err := os.WriteFile(manifestPath, []byte(`
apiVersion: flow-upgrade.nifi.advisor/v1alpha1
kind: ExtensionsManifest
metadata:
  name: validate-target
spec:
  nifiVersion: 2.8.0
  components:
    - scope: processor
      type: org.apache.nifi.processors.standard.InvokeHTTP
`), 0o644); err != nil {
		t.Fatalf("write extensions manifest: %v", err)
	}

	result, err := RunValidate(ValidateConfig{
		InputPath:              inputPath,
		InputFormat:            SourceFormatFlowJSONGZ,
		TargetVersion:          "2.8.0",
		ExtensionsManifestPath: manifestPath,
		OutputDir:              filepath.Join(tmpDir, "out"),
		ValidationName:         "validate-nested-processor",
	})
	if err != nil {
		t.Fatalf("RunValidate() error = %v", err)
	}
	if result.Blocked {
		t.Fatalf("did not expect blocked validation")
	}
	for _, finding := range result.Report.Findings {
		if finding.RuleID == "system.target-extension-unavailable" {
			t.Fatalf("did not expect unavailable target extension finding for nested processor")
		}
	}
}

func TestRunValidateIgnoresBuiltInFlowNodesForManifestChecks(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "flow.json.gz")
	manifestPath := filepath.Join(tmpDir, "extensions-manifest.yaml")

	writeGzipFile(t, inputPath, `{
  "rootGroup": {
    "id": "root-1",
    "name": "Root",
    "componentType": "PROCESS_GROUP",
    "processGroups": [
      {
        "id": "pg-1",
        "name": "Nested",
        "componentType": "PROCESS_GROUP",
        "labels": [
          {
            "id": "label-1",
            "name": "Marker",
            "componentType": "LABEL"
          }
        ],
        "processors": [
          {
            "id": "invoke-1",
            "name": "InvokeHTTP",
            "type": "org.apache.nifi.processors.standard.InvokeHTTP",
            "properties": {}
          }
        ]
      }
    ]
  }
}`)

	if err := os.WriteFile(manifestPath, []byte(`
apiVersion: flow-upgrade.nifi.advisor/v1alpha1
kind: ExtensionsManifest
metadata:
  name: validate-target
spec:
  nifiVersion: 2.8.0
  components:
    - scope: processor
      type: org.apache.nifi.processors.standard.InvokeHTTP
`), 0o644); err != nil {
		t.Fatalf("write extensions manifest: %v", err)
	}

	result, err := RunValidate(ValidateConfig{
		InputPath:              inputPath,
		InputFormat:            SourceFormatFlowJSONGZ,
		TargetVersion:          "2.8.0",
		ExtensionsManifestPath: manifestPath,
		OutputDir:              filepath.Join(tmpDir, "out"),
		ValidationName:         "validate-builtins-ignored",
	})
	if err != nil {
		t.Fatalf("RunValidate() error = %v", err)
	}
	if result.Blocked {
		t.Fatalf("did not expect blocked validation")
	}
	for _, finding := range result.Report.Findings {
		if finding.RuleID == "system.target-extension-unavailable" {
			t.Fatalf("did not expect unavailable target extension finding for built-in flow nodes")
		}
	}
}

func TestRunValidateUsesTargetNiFiAPIAndDetectsVersionMismatch(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "flow.json.gz")
	writeGzipFile(t, inputPath, `{
  "rootGroup": {
    "processors": [
      {
        "id": "invoke-1",
        "name": "InvokeHTTP",
        "type": "org.apache.nifi.processors.standard.InvokeHTTP",
        "bundle": {
          "group": "org.apache.nifi",
          "artifact": "nifi-standard-nar"
        },
        "properties": {}
      }
    ]
  }
}`)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/nifi-api/flow/about":
			_, _ = w.Write([]byte(`{"about":{"title":"Apache NiFi","version":"2.0.1"}}`))
		case "/nifi-api/flow/runtime-manifest":
			_, _ = w.Write([]byte(`{
  "runtimeManifest": {
    "processorTypes": [
      {
        "type": "org.apache.nifi.processors.standard.InvokeHTTP",
        "bundle": {
          "group": "org.apache.nifi",
          "artifact": "nifi-standard-nar"
        }
      }
    ],
    "controllerServiceTypes": [],
    "reportingTaskTypes": []
  }
}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, err := RunValidate(ValidateConfig{
		InputPath:      inputPath,
		InputFormat:    SourceFormatFlowJSONGZ,
		TargetVersion:  "2.0.0",
		TargetAPIURL:   server.URL,
		OutputDir:      filepath.Join(tmpDir, "out"),
		ValidationName: "validate-live-api-version-mismatch",
	})
	if err != nil {
		t.Fatalf("RunValidate() error = %v", err)
	}
	if !result.Blocked {
		t.Fatalf("expected validation to be blocked on API version mismatch")
	}
	if result.Report.Target.ActualNiFiVersion != "2.0.1" {
		t.Fatalf("expected actual NiFi version 2.0.1, got %q", result.Report.Target.ActualNiFiVersion)
	}
	var versionFinding *MigrationFinding
	for i := range result.Report.Findings {
		if result.Report.Findings[i].RuleID == "system.target-api-version-mismatch" {
			versionFinding = &result.Report.Findings[i]
			break
		}
	}
	if versionFinding == nil {
		t.Fatalf("expected target API version mismatch finding")
	}
}

func TestRunAnalyzeJoltNullCustomPropertiesDoNotTriggerManualInspection(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")

	writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "processors": [
      {
        "id": "jolt-1",
        "name": "Create Customer JSON",
        "type": "org.apache.nifi.processors.jolt.JoltTransformJSON",
        "properties": {
          "Custom Transformation Class Name": null,
          "Custom Module Directory": null
        }
      }
    ]
  }
}`)

	result, err := RunAnalyze(AnalyzeConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowJSONGZ,
		SourceVersion: "2.7.0",
		TargetVersion: "2.8.0",
		RulePackPaths: []string{filepath.Join("..", "..", "examples", "rulepacks", "nifi-2.7-to-2.8.official.yaml")},
		OutputDir:     filepath.Join(tmpDir, "out"),
		AnalysisName:  "jolt-null-custom-properties",
		FailOn:        "never",
	})
	if err != nil {
		t.Fatalf("RunAnalyze() error = %v", err)
	}

	for _, finding := range result.Report.Findings {
		if finding.RuleID == "core.jolt.custom-class.recompile" || finding.RuleID == "core.jolt.custom-modules.recompile" {
			t.Fatalf("did not expect Jolt manual inspection finding for null custom properties")
		}
	}
}

func TestRunAnalyzeJoltNonEmptyCustomPropertiesTriggerManualInspection(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")

	writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "processors": [
      {
        "id": "jolt-1",
        "name": "Create Customer JSON",
        "type": "org.apache.nifi.processors.jolt.JoltTransformJSON",
        "properties": {
          "Custom Transformation Class Name": "com.example.CustomTransform",
          "Custom Module Directory": "/opt/nifi/custom-jolt"
        }
      }
    ]
  }
}`)

	result, err := RunAnalyze(AnalyzeConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowJSONGZ,
		SourceVersion: "2.7.0",
		TargetVersion: "2.8.0",
		RulePackPaths: []string{filepath.Join("..", "..", "examples", "rulepacks", "nifi-2.7-to-2.8.official.yaml")},
		OutputDir:     filepath.Join(tmpDir, "out"),
		AnalysisName:  "jolt-custom-properties-present",
		FailOn:        "never",
	})
	if err != nil {
		t.Fatalf("RunAnalyze() error = %v", err)
	}

	ruleIDs := map[string]bool{}
	for _, finding := range result.Report.Findings {
		ruleIDs[finding.RuleID] = true
	}
	if !ruleIDs["core.jolt.custom-class.recompile"] {
		t.Fatalf("expected custom class manual inspection finding")
	}
	if !ruleIDs["core.jolt.custom-modules.recompile"] {
		t.Fatalf("expected custom module manual inspection finding")
	}
}

func TestRulePacksIgnoreNullPlaceholderProperties(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		sourceVersion string
		targetVersion string
		rulePackPath  string
		ruleID        string
		componentType string
		properties    string
	}{
		{
			name:          "invoke-http-url-encoding",
			sourceVersion: "1.23.0",
			targetVersion: "1.24.0",
			rulePackPath:  filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.23-to-1.24.official.yaml"),
			ruleID:        "core.invoke-http.url-encoding.review",
			componentType: "org.apache.nifi.processors.standard.InvokeHTTP",
			properties:    `"HTTP URL": null`,
		},
		{
			name:          "cassandra-compression-type",
			sourceVersion: "1.21.0",
			targetVersion: "1.22.0",
			rulePackPath:  filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.21-to-1.22.official.yaml"),
			ruleID:        "core.cassandra.compression-type.removed",
			componentType: "org.apache.nifi.processors.cassandra.PutCassandraQL",
			properties:    `"Compression Type": null`,
		},
		{
			name:          "listen-http-rate-limit",
			sourceVersion: "2.3.0",
			targetVersion: "2.4.0",
			rulePackPath:  filepath.Join("..", "..", "examples", "rulepacks", "nifi-2.3-to-2.4.official.yaml"),
			ruleID:        "core.listen-http.rate-limit.removed",
			componentType: "org.apache.nifi.processors.standard.ListenHTTP",
			properties:    `"Max Data to Receive per Second": null`,
		},
		{
			name:          "invoke-http-proxy-host",
			sourceVersion: "1.27.0",
			targetVersion: "2.0.0",
			rulePackPath:  filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.27-to-2.0.official.yaml"),
			ruleID:        "core.invoke-http.proxy-properties.replace",
			componentType: "org.apache.nifi.processors.standard.InvokeHTTP",
			properties:    `"Proxy Host": null`,
		},
		{
			name:          "sample-ssl-context-service",
			sourceVersion: "1.27.0",
			targetVersion: "2.0.0",
			rulePackPath:  filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.27-to-2.0.sample.yaml"),
			ruleID:        "core.ssl-context.manual-review",
			componentType: "org.apache.nifi.processors.standard.InvokeHTTP",
			properties:    `"SSL Context Service": null`,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			sourcePath := filepath.Join(tmpDir, "flow.json.gz")

			writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "processors": [
      {
        "id": "proc-1",
        "name": "Processor",
        "type": "`+tc.componentType+`",
        "properties": {
          `+tc.properties+`
        }
      }
    ]
  }
}`)

			result, err := RunAnalyze(AnalyzeConfig{
				SourcePath:    sourcePath,
				SourceFormat:  SourceFormatFlowJSONGZ,
				SourceVersion: tc.sourceVersion,
				TargetVersion: tc.targetVersion,
				RulePackPaths: []string{tc.rulePackPath},
				OutputDir:     filepath.Join(tmpDir, "out"),
				AnalysisName:  tc.name,
				FailOn:        "never",
			})
			if err != nil {
				t.Fatalf("RunAnalyze() error = %v", err)
			}

			for _, finding := range result.Report.Findings {
				if finding.RuleID == tc.ruleID {
					t.Fatalf("did not expect rule %q to match a null placeholder property", tc.ruleID)
				}
			}
		})
	}
}

func TestRulePacksStillMatchNonEmptyProperties(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		sourceVersion string
		targetVersion string
		rulePackPath  string
		ruleID        string
		componentType string
		properties    string
	}{
		{
			name:          "invoke-http-url-encoding",
			sourceVersion: "1.23.0",
			targetVersion: "1.24.0",
			rulePackPath:  filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.23-to-1.24.official.yaml"),
			ruleID:        "core.invoke-http.url-encoding.review",
			componentType: "org.apache.nifi.processors.standard.InvokeHTTP",
			properties:    `"HTTP URL": "https://example.test/a b"`,
		},
		{
			name:          "cassandra-compression-type",
			sourceVersion: "1.21.0",
			targetVersion: "1.22.0",
			rulePackPath:  filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.21-to-1.22.official.yaml"),
			ruleID:        "core.cassandra.compression-type.removed",
			componentType: "org.apache.nifi.processors.cassandra.PutCassandraQL",
			properties:    `"Compression Type": "LZ4"`,
		},
		{
			name:          "listen-http-rate-limit",
			sourceVersion: "2.3.0",
			targetVersion: "2.4.0",
			rulePackPath:  filepath.Join("..", "..", "examples", "rulepacks", "nifi-2.3-to-2.4.official.yaml"),
			ruleID:        "core.listen-http.rate-limit.removed",
			componentType: "org.apache.nifi.processors.standard.ListenHTTP",
			properties:    `"Max Data to Receive per Second": "1 MB"`,
		},
		{
			name:          "invoke-http-proxy-host",
			sourceVersion: "1.27.0",
			targetVersion: "2.0.0",
			rulePackPath:  filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.27-to-2.0.official.yaml"),
			ruleID:        "core.invoke-http.proxy-properties.replace",
			componentType: "org.apache.nifi.processors.standard.InvokeHTTP",
			properties:    `"Proxy Host": "proxy.internal"`,
		},
		{
			name:          "sample-ssl-context-service",
			sourceVersion: "1.27.0",
			targetVersion: "2.0.0",
			rulePackPath:  filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.27-to-2.0.sample.yaml"),
			ruleID:        "core.ssl-context.manual-review",
			componentType: "org.apache.nifi.processors.standard.InvokeHTTP",
			properties:    `"SSL Context Service": "ssl-service-id"`,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			sourcePath := filepath.Join(tmpDir, "flow.json.gz")

			writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "processors": [
      {
        "id": "proc-1",
        "name": "Processor",
        "type": "`+tc.componentType+`",
        "properties": {
          `+tc.properties+`
        }
      }
    ]
  }
}`)

			result, err := RunAnalyze(AnalyzeConfig{
				SourcePath:    sourcePath,
				SourceFormat:  SourceFormatFlowJSONGZ,
				SourceVersion: tc.sourceVersion,
				TargetVersion: tc.targetVersion,
				RulePackPaths: []string{tc.rulePackPath},
				OutputDir:     filepath.Join(tmpDir, "out"),
				AnalysisName:  tc.name,
				FailOn:        "never",
			})
			if err != nil {
				t.Fatalf("RunAnalyze() error = %v", err)
			}

			matched := false
			for _, finding := range result.Report.Findings {
				if finding.RuleID == tc.ruleID {
					matched = true
					break
				}
			}
			if !matched {
				t.Fatalf("expected rule %q to match a non-empty property value", tc.ruleID)
			}
		})
	}
}

func TestRunValidateUsesTargetNiFiAPIInventory(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "flow.json.gz")
	writeGzipFile(t, inputPath, `{
  "rootGroup": {
    "processors": [
      {
        "id": "base64-1",
        "name": "LegacyBase64",
        "type": "org.apache.nifi.processors.standard.Base64EncodeContent",
        "properties": {}
      }
    ]
  }
}`)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/nifi-api/flow/about":
			_, _ = w.Write([]byte(`{"about":{"version":"2.0.0"}}`))
		case "/nifi-api/flow/runtime-manifest":
			_, _ = w.Write([]byte(`{
  "runtimeManifest": {
    "processorTypes": [
      {
        "type": "org.apache.nifi.processors.standard.InvokeHTTP"
      }
    ],
    "controllerServiceTypes": [],
    "reportingTaskTypes": []
  }
}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, err := RunValidate(ValidateConfig{
		InputPath:      inputPath,
		InputFormat:    SourceFormatFlowJSONGZ,
		TargetVersion:  "2.0.0",
		TargetAPIURL:   server.URL,
		OutputDir:      filepath.Join(tmpDir, "out"),
		ValidationName: "validate-live-api-inventory",
	})
	if err != nil {
		t.Fatalf("RunValidate() error = %v", err)
	}
	if !result.Blocked {
		t.Fatalf("expected validation to be blocked when target API inventory lacks the component")
	}
	if len(result.Report.Findings) != 1 {
		t.Fatalf("expected 1 validation finding, got %d", len(result.Report.Findings))
	}
	if result.Report.Findings[0].RuleID != "system.target-extension-unavailable" {
		t.Fatalf("unexpected validation rule id %q", result.Report.Findings[0].RuleID)
	}
}

func TestRunValidateTargetProcessGroupBlocksLocallyModifiedState(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "flow.json.gz")
	writeGzipFile(t, inputPath, `{
  "rootGroup": {
    "processors": [
      {
        "id": "invoke-1",
        "name": "InvokeHTTP",
        "type": "org.apache.nifi.processors.standard.InvokeHTTP",
        "properties": {}
      }
    ]
  }
}`)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/nifi-api/flow/about":
			_, _ = w.Write([]byte(`{"about":{"version":"2.0.0"}}`))
		case "/nifi-api/flow/runtime-manifest":
			_, _ = w.Write([]byte(`{
  "runtimeManifest": {
    "processorTypes": [
      {
        "type": "org.apache.nifi.processors.standard.InvokeHTTP"
      }
    ],
    "controllerServiceTypes": [],
    "reportingTaskTypes": []
  }
}`))
		case "/nifi-api/flow/process-groups/pg-1":
			_, _ = w.Write([]byte(`{
  "processGroupFlow": {
    "id": "pg-1",
    "breadcrumb": {
      "breadcrumb": {
        "id": "pg-1",
        "name": "Orders"
      }
    },
    "flow": {
      "versionedFlowState": "LOCALLY_MODIFIED",
      "runningCount": 2,
      "disabledCount": 1,
      "invalidCount": 0,
      "versionControlInformation": {
        "bucketId": "bucket-1",
        "flowId": "flow-1",
        "version": 4
      }
    }
  }
}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, err := RunValidate(ValidateConfig{
		InputPath:              inputPath,
		InputFormat:            SourceFormatFlowJSONGZ,
		TargetVersion:          "2.0.0",
		TargetAPIURL:           server.URL,
		TargetProcessGroupID:   "pg-1",
		TargetProcessGroupMode: "update",
		OutputDir:              filepath.Join(tmpDir, "out"),
		ValidationName:         "validate-process-group-modified",
	})
	if err != nil {
		t.Fatalf("RunValidate() error = %v", err)
	}
	if !result.Blocked {
		t.Fatalf("expected validation to be blocked")
	}

	ruleIDs := map[string]bool{}
	for _, finding := range result.Report.Findings {
		ruleIDs[finding.RuleID] = true
	}
	if !ruleIDs["system.target-process-group.versioned-flow-state"] {
		t.Fatalf("expected locally modified process group finding")
	}
	if !ruleIDs["system.target-process-group.live-components"] {
		t.Fatalf("expected live components process group finding")
	}
	if result.Report.Target.TargetProcessGroupMode != "update" {
		t.Fatalf("expected target process group mode update, got %q", result.Report.Target.TargetProcessGroupMode)
	}
}

func TestRunValidateTargetProcessGroupBlocksFlowMismatch(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "snapshot.json")
	if err := os.WriteFile(inputPath, []byte(`{
  "bucketIdentifier": "bucket-a",
  "flowIdentifier": "flow-a",
  "version": 7,
  "flowContents": {
    "processors": [
      {
        "identifier": "invoke-1",
        "name": "InvokeHTTP",
        "type": "org.apache.nifi.processors.standard.InvokeHTTP",
        "properties": {}
      }
    ]
  }
}`), 0o644); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/nifi-api/flow/about":
			_, _ = w.Write([]byte(`{"about":{"version":"2.0.0"}}`))
		case "/nifi-api/flow/runtime-manifest":
			_, _ = w.Write([]byte(`{
  "runtimeManifest": {
    "processorTypes": [
      {
        "type": "org.apache.nifi.processors.standard.InvokeHTTP"
      }
    ],
    "controllerServiceTypes": [],
    "reportingTaskTypes": []
  }
}`))
		case "/nifi-api/flow/process-groups/pg-2":
			_, _ = w.Write([]byte(`{
  "processGroupFlow": {
    "id": "pg-2",
    "flow": {
      "versionedFlowState": "UP_TO_DATE",
      "versionControlInformation": {
        "bucketId": "bucket-b",
        "flowId": "flow-b",
        "version": 9
      }
    }
  }
}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, err := RunValidate(ValidateConfig{
		InputPath:              inputPath,
		InputFormat:            SourceFormatVersionedFlowSnap,
		TargetVersion:          "2.0.0",
		TargetAPIURL:           server.URL,
		TargetProcessGroupID:   "pg-2",
		TargetProcessGroupMode: "update",
		OutputDir:              filepath.Join(tmpDir, "out"),
		ValidationName:         "validate-process-group-mismatch",
	})
	if err != nil {
		t.Fatalf("RunValidate() error = %v", err)
	}
	if !result.Blocked {
		t.Fatalf("expected validation to be blocked on flow mismatch")
	}

	var mismatchFinding *MigrationFinding
	for i := range result.Report.Findings {
		if result.Report.Findings[i].RuleID == "system.target-process-group.flow-mismatch" {
			mismatchFinding = &result.Report.Findings[i]
			break
		}
	}
	if mismatchFinding == nil {
		t.Fatalf("expected flow mismatch finding")
	}
}

func TestRunAnalyzeOfficial26To27PackFindsExpectedChanges(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "processors": [
      {
        "id": "syslog-1",
        "name": "InboundSyslog",
        "type": "org.apache.nifi.processors.standard.ListenSyslog",
        "properties": {
          "Protocol": "UDP",
          "Port": "5514"
        }
      },
      {
        "id": "asana-1",
        "name": "AsanaImport",
        "type": "org.apache.nifi.processors.asana.GetAsanaObject",
        "properties": {}
      }
    ],
    "controllerServices": [
      {
        "id": "ssl-1",
        "name": "RestrictedSSL",
        "type": "org.apache.nifi.ssl.StandardRestrictedSSLContextService",
        "properties": {}
      }
    ]
  }
}`)

	result, err := RunAnalyze(AnalyzeConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowJSONGZ,
		SourceVersion: "2.6.0",
		TargetVersion: "2.7.0",
		RulePackPaths: []string{
			filepath.Join("..", "..", "examples", "rulepacks", "nifi-2.6-to-2.7.official.yaml"),
			filepath.Join("..", "..", "examples", "rulepacks", "nifi-2.6-to-2.7.0.patch-caveats.yaml"),
		},
		OutputDir:    filepath.Join(tmpDir, "out"),
		AnalysisName: "official-26-to-27",
		FailOn:       "never",
	})
	if err != nil {
		t.Fatalf("RunAnalyze() error = %v", err)
	}

	ruleIDs := map[string]bool{}
	for _, finding := range result.Report.Findings {
		ruleIDs[finding.RuleID] = true
	}
	expected := []string{
		"core.get-asana-object.deprecated",
		"core.get-asana-object.clear-state",
		"core.restricted-ssl-context.deprecated-27",
		"core.listen-syslog.port-to-udp-port-27",
		"core.scripted-components.27.0-review",
	}
	for _, ruleID := range expected {
		if !ruleIDs[ruleID] {
			t.Fatalf("expected finding for %s", ruleID)
		}
	}
}

func TestRunAnalyzeOfficial26To271DoesNotLoad27PatchCaveats(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "processors": [
      {
        "id": "syslog-1",
        "name": "InboundSyslog",
        "type": "org.apache.nifi.processors.standard.ListenSyslog",
        "properties": {
          "Protocol": "UDP",
          "Port": "5514"
        }
      }
    ]
  }
}`)

	result, err := RunAnalyze(AnalyzeConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowJSONGZ,
		SourceVersion: "2.6.0",
		TargetVersion: "2.7.1",
		RulePackPaths: []string{
			filepath.Join("..", "..", "examples", "rulepacks", "nifi-2.6-to-2.7.official.yaml"),
			filepath.Join("..", "..", "examples", "rulepacks", "nifi-2.6-to-2.7.0.patch-caveats.yaml"),
		},
		OutputDir:    filepath.Join(tmpDir, "out"),
		AnalysisName: "official-26-to-271",
		FailOn:       "never",
	})
	if err != nil {
		t.Fatalf("RunAnalyze() error = %v", err)
	}

	for _, finding := range result.Report.Findings {
		if finding.RuleID == "core.scripted-components.27.0-review" {
			t.Fatalf("did not expect 2.7.0 patch-specific finding %s for target 2.7.1", finding.RuleID)
		}
	}

	ruleIDs := map[string]bool{}
	for _, finding := range result.Report.Findings {
		ruleIDs[finding.RuleID] = true
	}
	if ruleIDs["core.external-api.property-renames.review"] {
		t.Fatalf("did not expect general 2.7.x external API rename advisory after removing generic flow-root noise")
	}
}

func TestRunAnalyzeOfficial27To28PackFindsExpectedChanges(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "processors": [
      {
        "id": "syslog-1",
        "name": "InboundSyslog",
        "type": "org.apache.nifi.processors.standard.ListenSyslog",
        "properties": {
          "Protocol": "TCP",
          "Port": "5514"
        }
      },
      {
        "id": "asana-1",
        "name": "AsanaImport",
        "type": "org.apache.nifi.processors.asana.GetAsanaObject",
        "properties": {}
      },
      {
        "id": "jolt-1",
        "name": "CustomJolt",
        "type": "org.apache.nifi.processors.jolt.JoltTransformJSON",
        "properties": {
          "Custom Transformation Class Name": "com.example.CustomTransform"
        }
      }
    ],
    "controllerServices": [
      {
        "id": "ssl-1",
        "name": "RestrictedSSL",
        "type": "org.apache.nifi.ssl.StandardRestrictedSSLContextService",
        "properties": {}
      }
    ]
  }
}`)

	result, err := RunAnalyze(AnalyzeConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowJSONGZ,
		SourceVersion: "2.7.0",
		TargetVersion: "2.8.0",
		RulePackPaths: []string{filepath.Join("..", "..", "examples", "rulepacks", "nifi-2.7-to-2.8.official.yaml")},
		OutputDir:     filepath.Join(tmpDir, "out"),
		AnalysisName:  "official-27-to-28",
		FailOn:        "never",
	})
	if err != nil {
		t.Fatalf("RunAnalyze() error = %v", err)
	}

	ruleIDs := map[string]bool{}
	for _, finding := range result.Report.Findings {
		ruleIDs[finding.RuleID] = true
	}
	expected := []string{
		"core.get-asana-object.removed",
		"core.restricted-ssl-context.deprecated",
		"core.listen-syslog.port-to-tcp-port",
		"core.jolt.custom-class.recompile",
	}
	for _, ruleID := range expected {
		if !ruleIDs[ruleID] {
			t.Fatalf("expected finding for %s", ruleID)
		}
	}
}

func TestRunRewriteAppliesDeterministicActions(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	rulePackPath := filepath.Join(tmpDir, "rewrite-rulepack.yaml")

	writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "processors": [
      {
        "id": "proc-1",
        "name": "OrdersProcessor",
        "type": "org.apache.nifi.example.OldProcessor",
        "bundle": {
          "group": "org.apache.nifi",
          "artifact": "nifi-old-processors"
        },
        "properties": {
          "Old Property": "alpha",
          "Remove Me": "beta"
        }
      }
    ]
  }
}`)

	if err := os.WriteFile(rulePackPath, []byte(`
apiVersion: flow-upgrade.nifi.advisor/v1alpha1
kind: RulePack
metadata:
  name: rewrite-pack
spec:
  sourceVersionRange: ">=1.27.0 <2.0.0"
  targetVersionRange: ">=2.0.0 <2.1.0"
  appliesToFormats:
    - flow-json-gz
  rules:
    - id: rewrite.old.processor
      category: component-replaced
      class: auto-fix
      severity: warning
      message: Rewrite old processor shape.
      selector:
        componentType: org.apache.nifi.example.OldProcessor
      match:
        propertyExists: Old Property
      actions:
        - type: rename-property
          from: Old Property
          to: New Property
        - type: remove-property
          name: Remove Me
        - type: update-bundle-coordinate
          group: org.apache.nifi
          artifact: nifi-new-processors
        - type: replace-component-type
          from: org.apache.nifi.example.OldProcessor
          to: org.apache.nifi.example.NewProcessor
      notes: Safe deterministic rewrite for a known processor migration.
      references:
        - https://cwiki.apache.org/confluence/display/NIFI/Migration%2BGuidance
`), 0o644); err != nil {
		t.Fatalf("write rule pack: %v", err)
	}

	result, err := RunRewrite(RewriteConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowJSONGZ,
		SourceVersion: "1.27.0",
		TargetVersion: "2.0.0",
		RulePackPaths: []string{rulePackPath},
		OutputDir:     filepath.Join(tmpDir, "out"),
		RewriteName:   "rewrite-basic",
	})
	if err != nil {
		t.Fatalf("RunRewrite() error = %v", err)
	}

	if result.Report.Summary.AppliedOperations != 4 {
		t.Fatalf("expected 4 applied operations, got %d", result.Report.Summary.AppliedOperations)
	}
	if len(result.Report.Operations) == 0 {
		t.Fatalf("expected rewrite operations to be reported")
	}
	if len(result.Report.Operations[0].References) != 1 {
		t.Fatalf("expected operation references to be preserved, got %d", len(result.Report.Operations[0].References))
	}
	if result.Report.Operations[0].Notes == "" {
		t.Fatalf("expected operation notes to be preserved")
	}

	_, content, err := readSourceArtifact(result.RewrittenFlowPath, SourceFormatFlowJSONGZ)
	if err != nil {
		t.Fatalf("read rewritten artifact: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		t.Fatalf("unmarshal rewritten artifact: %v", err)
	}
	processor := payload["rootGroup"].(map[string]any)["processors"].([]any)[0].(map[string]any)
	properties := processor["properties"].(map[string]any)
	if _, ok := properties["Old Property"]; ok {
		t.Fatalf("expected Old Property to be renamed")
	}
	if properties["New Property"] != "alpha" {
		t.Fatalf("expected New Property alpha, got %v", properties["New Property"])
	}
	if _, ok := properties["Remove Me"]; ok {
		t.Fatalf("expected Remove Me to be removed")
	}
	if processor["type"] != "org.apache.nifi.example.NewProcessor" {
		t.Fatalf("expected rewritten component type, got %v", processor["type"])
	}
	bundle := processor["bundle"].(map[string]any)
	if bundle["artifact"] != "nifi-new-processors" {
		t.Fatalf("expected updated bundle artifact, got %v", bundle["artifact"])
	}
}

func TestRunRewriteSupportsSetProperty(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	rulePackPath := filepath.Join(tmpDir, "set-property-rulepack.yaml")

	writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "processors": [
      {
        "id": "proc-1",
        "name": "OrdersProcessor",
        "type": "org.apache.nifi.example.OldProcessor",
        "properties": {
          "Mode": "Encode"
        }
      }
    ]
  }
}`)

	if err := os.WriteFile(rulePackPath, []byte(`
apiVersion: flow-upgrade.nifi.advisor/v1alpha1
kind: RulePack
metadata:
  name: set-property-pack
spec:
  sourceVersionRange: ">=1.27.0 <2.0.0"
  targetVersionRange: ">=2.0.0 <2.1.0"
  appliesToFormats:
    - flow-json-gz
  rules:
    - id: rewrite.set.property
      category: property-value-changed
      class: auto-fix
      severity: warning
      message: Set encoding property.
      selector:
        componentType: org.apache.nifi.example.OldProcessor
      match:
        propertyExists: Mode
      actions:
        - type: set-property
          property: Encoding
          value: base64
`), 0o644); err != nil {
		t.Fatalf("write rule pack: %v", err)
	}

	result, err := RunRewrite(RewriteConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowJSONGZ,
		SourceVersion: "1.27.0",
		TargetVersion: "2.0.0",
		RulePackPaths: []string{rulePackPath},
		OutputDir:     filepath.Join(tmpDir, "out"),
		RewriteName:   "rewrite-set-property",
	})
	if err != nil {
		t.Fatalf("RunRewrite() error = %v", err)
	}
	if result.Report.Summary.AppliedOperations != 1 {
		t.Fatalf("expected 1 applied operation, got %d", result.Report.Summary.AppliedOperations)
	}

	_, content, err := readSourceArtifact(result.RewrittenFlowPath, SourceFormatFlowJSONGZ)
	if err != nil {
		t.Fatalf("read rewritten artifact: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		t.Fatalf("unmarshal rewritten artifact: %v", err)
	}
	properties := payload["rootGroup"].(map[string]any)["processors"].([]any)[0].(map[string]any)["properties"].(map[string]any)
	if properties["Encoding"] != "base64" {
		t.Fatalf("expected Encoding base64, got %v", properties["Encoding"])
	}
}

func TestRunAnalyzeIncludesAssistedRewriteFindings(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	rulePackPath := filepath.Join(tmpDir, "assisted-analyze-rulepack.yaml")

	writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "processors": [
      {
        "id": "proc-1",
        "name": "LegacyFetcher",
        "type": "org.apache.nifi.example.LegacyFetcher",
        "properties": {
          "URL": "https://example.com/orders"
        }
      }
    ]
  }
}`)

	if err := os.WriteFile(rulePackPath, []byte(`
apiVersion: flow-upgrade.nifi.advisor/v1alpha1
kind: RulePack
metadata:
  name: assisted-analyze-pack
spec:
  sourceVersionRange: ">=1.27.0 <2.0.0"
  targetVersionRange: ">=2.0.0 <2.1.0"
  appliesToFormats:
    - flow-json-gz
  rules:
    - id: assisted.fetcher.scaffold
      category: component-replaced
      class: assisted-rewrite
      severity: warning
      message: Scaffold the replacement fetcher configuration.
      selector:
        componentType: org.apache.nifi.example.LegacyFetcher
      match:
        propertyExists: URL
      actions:
        - type: copy-property
          from: URL
          to: Remote URL
        - type: set-property-if-absent
          property: HTTP Method
          value: GET
`), 0o644); err != nil {
		t.Fatalf("write rule pack: %v", err)
	}

	result, err := RunAnalyze(AnalyzeConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowJSONGZ,
		SourceVersion: "1.27.0",
		TargetVersion: "2.0.0",
		RulePackPaths: []string{rulePackPath},
		OutputDir:     filepath.Join(tmpDir, "out"),
		AnalysisName:  "assisted-analyze",
		FailOn:        "never",
	})
	if err != nil {
		t.Fatalf("RunAnalyze() error = %v", err)
	}

	if got := result.Report.Summary.ByClass["assisted-rewrite"]; got != 1 {
		t.Fatalf("expected 1 assisted-rewrite finding, got %d", got)
	}
	if len(result.Report.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(result.Report.Findings))
	}
	actions := result.Report.Findings[0].SuggestedActions
	if len(actions) != 2 {
		t.Fatalf("expected 2 suggested actions, got %d", len(actions))
	}
	if actions[0].Type != "copy-property" {
		t.Fatalf("expected first action copy-property, got %q", actions[0].Type)
	}
	if actions[1].Type != "set-property-if-absent" {
		t.Fatalf("expected second action set-property-if-absent, got %q", actions[1].Type)
	}
}

func TestRunRewriteSupportsAssistedRewriteScaffolds(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	rulePackPath := filepath.Join(tmpDir, "assisted-rewrite-rulepack.yaml")

	writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "processors": [
      {
        "id": "proc-1",
        "name": "LegacyFetcher",
        "type": "org.apache.nifi.example.LegacyFetcher",
        "properties": {
          "URL": "https://example.com/orders"
        }
      }
    ]
  }
}`)

	if err := os.WriteFile(rulePackPath, []byte(`
apiVersion: flow-upgrade.nifi.advisor/v1alpha1
kind: RulePack
metadata:
  name: assisted-rewrite-pack
spec:
  sourceVersionRange: ">=1.27.0 <2.0.0"
  targetVersionRange: ">=2.0.0 <2.1.0"
  appliesToFormats:
    - flow-json-gz
  rules:
    - id: assisted.fetcher.scaffold
      category: component-replaced
      class: assisted-rewrite
      severity: warning
      message: Scaffold the replacement fetcher configuration.
      selector:
        componentType: org.apache.nifi.example.LegacyFetcher
      match:
        propertyExists: URL
      actions:
        - type: replace-component-type
          from: org.apache.nifi.example.LegacyFetcher
          to: org.apache.nifi.example.HttpFetcher
        - type: copy-property
          from: URL
          to: Remote URL
        - type: set-property-if-absent
          property: HTTP Method
          value: GET
`), 0o644); err != nil {
		t.Fatalf("write rule pack: %v", err)
	}

	result, err := RunRewrite(RewriteConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowJSONGZ,
		SourceVersion: "1.27.0",
		TargetVersion: "2.0.0",
		RulePackPaths: []string{rulePackPath},
		OutputDir:     filepath.Join(tmpDir, "out"),
		RewriteName:   "assisted-rewrite",
	})
	if err != nil {
		t.Fatalf("RunRewrite() error = %v", err)
	}

	if result.Report.Summary.AppliedOperations != 3 {
		t.Fatalf("expected 3 applied operations, got %d", result.Report.Summary.AppliedOperations)
	}
	if got := result.Report.Summary.AppliedByClass["assisted-rewrite"]; got != 3 {
		t.Fatalf("expected 3 applied assisted-rewrite operations, got %d", got)
	}
	if result.Report.Operations[0].Class != "assisted-rewrite" {
		t.Fatalf("expected assisted-rewrite class on operation, got %q", result.Report.Operations[0].Class)
	}

	_, content, err := readSourceArtifact(result.RewrittenFlowPath, SourceFormatFlowJSONGZ)
	if err != nil {
		t.Fatalf("read rewritten artifact: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		t.Fatalf("unmarshal rewritten artifact: %v", err)
	}
	processor := payload["rootGroup"].(map[string]any)["processors"].([]any)[0].(map[string]any)
	properties := processor["properties"].(map[string]any)
	if processor["type"] != "org.apache.nifi.example.HttpFetcher" {
		t.Fatalf("expected rewritten component type, got %v", processor["type"])
	}
	if properties["URL"] != "https://example.com/orders" {
		t.Fatalf("expected original URL property to remain, got %v", properties["URL"])
	}
	if properties["Remote URL"] != "https://example.com/orders" {
		t.Fatalf("expected scaffolded Remote URL property, got %v", properties["Remote URL"])
	}
	if properties["HTTP Method"] != "GET" {
		t.Fatalf("expected scaffolded HTTP Method GET, got %v", properties["HTTP Method"])
	}
}

func TestRunRewriteSkipsPropertyValueReplacementWhenValueDiffers(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	rulePackPath := filepath.Join(tmpDir, "replace-value-rulepack.yaml")

	writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "processors": [
      {
        "id": "proc-1",
        "name": "OrdersProcessor",
        "type": "org.apache.nifi.example.OldProcessor",
        "properties": {
          "Mode": "manual"
        }
      }
    ]
  }
}`)

	if err := os.WriteFile(rulePackPath, []byte(`
apiVersion: flow-upgrade.nifi.advisor/v1alpha1
kind: RulePack
metadata:
  name: replace-value-pack
spec:
  sourceVersionRange: ">=1.27.0 <2.0.0"
  targetVersionRange: ">=2.0.0 <2.1.0"
  appliesToFormats:
    - flow-json-gz
  rules:
    - id: rewrite.mode.value
      category: property-value-changed
      class: auto-fix
      severity: warning
      message: Replace mode value.
      selector:
        componentType: org.apache.nifi.example.OldProcessor
      match:
        propertyExists: Mode
      actions:
        - type: replace-property-value
          property: Mode
          from: auto
          to: automatic
`), 0o644); err != nil {
		t.Fatalf("write rule pack: %v", err)
	}

	result, err := RunRewrite(RewriteConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowJSONGZ,
		SourceVersion: "1.27.0",
		TargetVersion: "2.0.0",
		RulePackPaths: []string{rulePackPath},
		OutputDir:     filepath.Join(tmpDir, "out"),
		RewriteName:   "rewrite-replace-value",
	})
	if err != nil {
		t.Fatalf("RunRewrite() error = %v", err)
	}
	if result.Report.Summary.AppliedOperations != 0 {
		t.Fatalf("expected 0 applied operations, got %d", result.Report.Summary.AppliedOperations)
	}
	if result.Report.Summary.SkippedOperations != 1 {
		t.Fatalf("expected 1 skipped operation, got %d", result.Report.Summary.SkippedOperations)
	}
	if got := result.Report.Operations[0].Reason; !strings.Contains(got, "did not match expected") {
		t.Fatalf("expected mismatch reason, got %q", got)
	}

	_, content, err := readSourceArtifact(result.RewrittenFlowPath, SourceFormatFlowJSONGZ)
	if err != nil {
		t.Fatalf("read rewritten artifact: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		t.Fatalf("unmarshal rewritten artifact: %v", err)
	}
	mode := payload["rootGroup"].(map[string]any)["processors"].([]any)[0].(map[string]any)["properties"].(map[string]any)["Mode"]
	if mode != "manual" {
		t.Fatalf("expected property value to remain manual, got %v", mode)
	}
}

func TestRunRewriteSupportsParameterContextRenames(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	rulePackPath := filepath.Join(tmpDir, "parameter-rewrite-rulepack.yaml")

	writeGzipFile(t, sourcePath, `{
  "parameterContexts": [
    {
      "id": "pc-1",
      "component": {
        "name": "OrdersContext",
        "parameters": [
          {
            "parameter": {
              "name": "old.parameter",
              "value": "legacy"
            }
          }
        ]
      }
    }
  ]
}`)

	if err := os.WriteFile(rulePackPath, []byte(`
apiVersion: flow-upgrade.nifi.advisor/v1alpha1
kind: RulePack
metadata:
  name: parameter-rewrite-pack
spec:
  sourceVersionRange: ">=1.27.0 <2.0.0"
  targetVersionRange: ">=2.0.0 <2.1.0"
  appliesToFormats:
    - flow-json-gz
  rules:
    - id: rewrite.parameter.name
      category: property-renamed
      class: auto-fix
      severity: warning
      message: Rename parameter.
      selector:
        scope: parameter-context
      match:
        propertyExists: old.parameter
      actions:
        - type: rename-property
          from: old.parameter
          to: new.parameter
`), 0o644); err != nil {
		t.Fatalf("write rule pack: %v", err)
	}

	result, err := RunRewrite(RewriteConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowJSONGZ,
		SourceVersion: "1.27.0",
		TargetVersion: "2.0.0",
		RulePackPaths: []string{rulePackPath},
		OutputDir:     filepath.Join(tmpDir, "out"),
		RewriteName:   "rewrite-parameter",
	})
	if err != nil {
		t.Fatalf("RunRewrite() error = %v", err)
	}
	if result.Report.Summary.AppliedOperations != 1 {
		t.Fatalf("expected 1 applied operation, got %d", result.Report.Summary.AppliedOperations)
	}

	_, content, err := readSourceArtifact(result.RewrittenFlowPath, SourceFormatFlowJSONGZ)
	if err != nil {
		t.Fatalf("read rewritten artifact: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		t.Fatalf("unmarshal rewritten artifact: %v", err)
	}
	parameters := payload["parameterContexts"].([]any)[0].(map[string]any)["component"].(map[string]any)["parameters"].([]any)
	name := parameters[0].(map[string]any)["parameter"].(map[string]any)["name"]
	if name != "new.parameter" {
		t.Fatalf("expected renamed parameter new.parameter, got %v", name)
	}
}

func TestRunRewriteCanUseAnalyzePlan(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	rulePackPath := filepath.Join(tmpDir, "rewrite-plan-rulepack.yaml")

	writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "processors": [
      {
        "id": "proc-1",
        "name": "OrdersProcessor",
        "type": "org.apache.nifi.example.OldProcessor",
        "properties": {
          "Old Property": "alpha"
        }
      }
    ]
  }
}`)

	if err := os.WriteFile(rulePackPath, []byte(`
apiVersion: flow-upgrade.nifi.advisor/v1alpha1
kind: RulePack
metadata:
  name: rewrite-plan-pack
spec:
  sourceVersionRange: ">=1.27.0 <2.0.0"
  targetVersionRange: ">=2.0.0 <2.1.0"
  appliesToFormats:
    - flow-json-gz
  rules:
    - id: rewrite.plan.rename
      category: property-renamed
      class: auto-fix
      severity: warning
      message: Rename old property through plan.
      selector:
        componentType: org.apache.nifi.example.OldProcessor
      match:
        propertyExists: Old Property
      actions:
        - type: rename-property
          from: Old Property
          to: New Property
`), 0o644); err != nil {
		t.Fatalf("write rule pack: %v", err)
	}

	analyzeResult, err := RunAnalyze(AnalyzeConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowJSONGZ,
		SourceVersion: "1.27.0",
		TargetVersion: "2.0.0",
		RulePackPaths: []string{rulePackPath},
		OutputDir:     filepath.Join(tmpDir, "analysis-out"),
		AnalysisName:  "rewrite-plan-analysis",
		FailOn:        "never",
	})
	if err != nil {
		t.Fatalf("RunAnalyze() error = %v", err)
	}

	result, err := RunRewrite(RewriteConfig{
		PlanPath:    analyzeResult.ReportJSONPath,
		OutputDir:   filepath.Join(tmpDir, "rewrite-out"),
		RewriteName: "rewrite-from-plan",
	})
	if err != nil {
		t.Fatalf("RunRewrite() with plan error = %v", err)
	}
	if result.Report.Summary.AppliedOperations != 1 {
		t.Fatalf("expected 1 applied operation, got %d", result.Report.Summary.AppliedOperations)
	}

	_, content, err := readSourceArtifact(result.RewrittenFlowPath, SourceFormatFlowJSONGZ)
	if err != nil {
		t.Fatalf("read rewritten artifact: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		t.Fatalf("unmarshal rewritten artifact: %v", err)
	}
	properties := payload["rootGroup"].(map[string]any)["processors"].([]any)[0].(map[string]any)["properties"].(map[string]any)
	if _, ok := properties["Old Property"]; ok {
		t.Fatalf("expected Old Property to be renamed via plan-driven rewrite")
	}
	if properties["New Property"] != "alpha" {
		t.Fatalf("expected New Property alpha via plan-driven rewrite, got %v", properties["New Property"])
	}
}

func TestRunRewriteSupportsGitRegistryDir(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourceDir := filepath.Join(tmpDir, "git-registry")
	rulePackPath := filepath.Join(tmpDir, "git-registry-rulepack.yaml")

	if err := os.MkdirAll(filepath.Join(sourceDir, "bucket-a", "flow-a"), 0o755); err != nil {
		t.Fatalf("mkdir sourceDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "bucket-a", "flow-a", "snapshot.json"), []byte(`{
  "flowContents": {
    "processors": [
      {
        "id": "proc-1",
        "name": "OrdersProcessor",
        "type": "org.apache.nifi.example.OldProcessor",
        "properties": {
          "Old Property": "alpha"
        }
      }
    ]
  }
}`), 0o644); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	if err := os.WriteFile(rulePackPath, []byte(`
apiVersion: flow-upgrade.nifi.advisor/v1alpha1
kind: RulePack
metadata:
  name: git-registry-rewrite-pack
spec:
  sourceVersionRange: ">=1.27.0 <2.0.0"
  targetVersionRange: ">=2.0.0 <2.1.0"
  appliesToFormats:
    - git-registry-dir
  rules:
    - id: rewrite.git-registry.rename
      category: property-renamed
      class: auto-fix
      severity: warning
      message: Rename old property in git registry directory.
      selector:
        componentType: org.apache.nifi.example.OldProcessor
      match:
        propertyExists: Old Property
      actions:
        - type: rename-property
          from: Old Property
          to: New Property
`), 0o644); err != nil {
		t.Fatalf("write rule pack: %v", err)
	}

	result, err := RunRewrite(RewriteConfig{
		SourcePath:    sourceDir,
		SourceFormat:  SourceFormatGitRegistryDir,
		SourceVersion: "1.27.0",
		TargetVersion: "2.0.0",
		RulePackPaths: []string{rulePackPath},
		OutputDir:     filepath.Join(tmpDir, "out"),
		RewriteName:   "rewrite-git-registry",
	})
	if err != nil {
		t.Fatalf("RunRewrite() error = %v", err)
	}
	if result.Report.Summary.AppliedOperations != 1 {
		t.Fatalf("expected 1 applied operation, got %d", result.Report.Summary.AppliedOperations)
	}

	rewrittenPath := filepath.Join(result.RewrittenFlowPath, "bucket-a", "flow-a", "snapshot.json")
	body, err := os.ReadFile(rewrittenPath)
	if err != nil {
		t.Fatalf("read rewritten snapshot: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal rewritten snapshot: %v", err)
	}
	properties := payload["flowContents"].(map[string]any)["processors"].([]any)[0].(map[string]any)["properties"].(map[string]any)
	if _, ok := properties["Old Property"]; ok {
		t.Fatalf("expected Old Property to be renamed")
	}
	if properties["New Property"] != "alpha" {
		t.Fatalf("expected New Property alpha, got %v", properties["New Property"])
	}
}

func TestRunRewriteUsesOfficial27To28ListenSyslogRule(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "processors": [
      {
        "id": "syslog-1",
        "name": "InboundSyslog",
        "type": "org.apache.nifi.processors.standard.ListenSyslog",
        "properties": {
          "Protocol": "TCP",
          "Port": "5514"
        }
      }
    ]
  }
}`)

	result, err := RunRewrite(RewriteConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowJSONGZ,
		SourceVersion: "2.7.0",
		TargetVersion: "2.8.0",
		RulePackPaths: []string{filepath.Join("..", "..", "examples", "rulepacks", "nifi-2.7-to-2.8.official.yaml")},
		OutputDir:     filepath.Join(tmpDir, "out"),
		RewriteName:   "official-listensyslog-rewrite",
	})
	if err != nil {
		t.Fatalf("RunRewrite() error = %v", err)
	}
	if result.Report.Summary.AppliedOperations != 1 {
		t.Fatalf("expected 1 applied operation, got %d", result.Report.Summary.AppliedOperations)
	}

	_, content, err := readSourceArtifact(result.RewrittenFlowPath, SourceFormatFlowJSONGZ)
	if err != nil {
		t.Fatalf("read rewritten artifact: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		t.Fatalf("unmarshal rewritten artifact: %v", err)
	}
	properties := payload["rootGroup"].(map[string]any)["processors"].([]any)[0].(map[string]any)["properties"].(map[string]any)
	if _, ok := properties["Port"]; ok {
		t.Fatalf("expected Port property to be renamed")
	}
	if properties["TCP Port"] != "5514" {
		t.Fatalf("expected TCP Port 5514, got %v", properties["TCP Port"])
	}
}

func TestRunRewriteUsesOfficial26To27ListenSyslogRule(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "processors": [
      {
        "id": "syslog-1",
        "name": "InboundSyslog",
        "type": "org.apache.nifi.processors.standard.ListenSyslog",
        "properties": {
          "Protocol": "UDP",
          "Port": "5514"
        }
      }
    ]
  }
}`)

	result, err := RunRewrite(RewriteConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowJSONGZ,
		SourceVersion: "2.6.0",
		TargetVersion: "2.7.0",
		RulePackPaths: []string{filepath.Join("..", "..", "examples", "rulepacks", "nifi-2.6-to-2.7.official.yaml")},
		OutputDir:     filepath.Join(tmpDir, "out"),
		RewriteName:   "official-listensyslog-rewrite-27",
	})
	if err != nil {
		t.Fatalf("RunRewrite() error = %v", err)
	}
	if result.Report.Summary.AppliedOperations != 1 {
		t.Fatalf("expected 1 applied operation, got %d", result.Report.Summary.AppliedOperations)
	}

	_, content, err := readSourceArtifact(result.RewrittenFlowPath, SourceFormatFlowJSONGZ)
	if err != nil {
		t.Fatalf("read rewritten artifact: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		t.Fatalf("unmarshal rewritten artifact: %v", err)
	}
	properties := payload["rootGroup"].(map[string]any)["processors"].([]any)[0].(map[string]any)["properties"].(map[string]any)
	if _, ok := properties["Port"]; ok {
		t.Fatalf("expected Port property to be renamed")
	}
	if properties["UDP Port"] != "5514" {
		t.Fatalf("expected UDP Port 5514, got %v", properties["UDP Port"])
	}
}

func TestRunRewriteUsesOfficial127To20Base64Rule(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "processors": [
      {
        "id": "base64-1",
        "name": "Base64",
        "type": "org.apache.nifi.processors.standard.Base64EncodeContent",
        "properties": {
          "Mode": "Encode"
        }
      }
    ]
  }
}`)

	result, err := RunRewrite(RewriteConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowJSONGZ,
		SourceVersion: "1.27.0",
		TargetVersion: "2.0.0",
		RulePackPaths: []string{filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.27-to-2.0.official.yaml")},
		OutputDir:     filepath.Join(tmpDir, "out"),
		RewriteName:   "official-base64-rewrite",
	})
	if err != nil {
		t.Fatalf("RunRewrite() error = %v", err)
	}
	if result.Report.Summary.AppliedOperations != 1 {
		t.Fatalf("expected 1 applied operation, got %d", result.Report.Summary.AppliedOperations)
	}

	_, content, err := readSourceArtifact(result.RewrittenFlowPath, SourceFormatFlowJSONGZ)
	if err != nil {
		t.Fatalf("read rewritten artifact: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		t.Fatalf("unmarshal rewritten artifact: %v", err)
	}
	processor := payload["rootGroup"].(map[string]any)["processors"].([]any)[0].(map[string]any)
	if processor["type"] != "org.apache.nifi.processors.standard.EncodeContent" {
		t.Fatalf("expected rewritten type EncodeContent, got %v", processor["type"])
	}
	properties := processor["properties"].(map[string]any)
	if properties["Mode"] != "Encode" {
		t.Fatalf("expected Mode Encode to be preserved, got %v", properties["Mode"])
	}
}

func TestRunRewriteRejectsLegacyFlowXMLGZ(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.xml.gz")
	writeGzipFile(t, sourcePath, `<?xml version="1.0" encoding="UTF-8"?><flowController><rootGroup/></flowController>`)

	_, err := RunRewrite(RewriteConfig{
		SourcePath:    sourcePath,
		SourceFormat:  SourceFormatFlowXMLGZ,
		SourceVersion: "1.27.0",
		TargetVersion: "2.0.0",
		RulePackPaths: []string{filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.27-to-2.0.official.yaml")},
		OutputDir:     filepath.Join(tmpDir, "out"),
		RewriteName:   "rewrite-xml-rejected",
	})
	if err == nil {
		t.Fatalf("expected rewrite to reject flow.xml.gz")
	}
}

func TestRunPublishToFilesystemCopiesArtifact(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "rewritten-flow.json.gz")
	destination := filepath.Join(tmpDir, "published")
	writeGzipFile(t, inputPath, `{"rootGroup":{"id":"root-1"}}`)

	result, err := RunPublish(PublishConfig{
		InputPath:   inputPath,
		InputFormat: SourceFormatFlowJSONGZ,
		Publisher:   "fs",
		Destination: destination,
		PublishName: "publish-fs",
		OutputDir:   filepath.Join(tmpDir, "out"),
	})
	if err != nil {
		t.Fatalf("RunPublish() error = %v", err)
	}
	if result.Report.Summary.Files != 1 {
		t.Fatalf("expected 1 published file, got %d", result.Report.Summary.Files)
	}
	targetPath := filepath.Join(destination, filepath.Base(inputPath))
	if _, err := os.Stat(targetPath); err != nil {
		t.Fatalf("expected published artifact at %s: %v", targetPath, err)
	}
}

func TestRunPublishToGitRegistryDirWritesSnapshot(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "snapshot.json")
	destination := filepath.Join(tmpDir, "registry")
	if err := os.WriteFile(inputPath, []byte(`{
  "flowContents": {
    "processors": [
      {
        "id": "proc-1",
        "name": "OrdersProcessor",
        "type": "org.apache.nifi.example.NewProcessor",
        "properties": {
          "New Property": "alpha"
        }
      }
    ]
  }
}`), 0o644); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	result, err := RunPublish(PublishConfig{
		InputPath:   inputPath,
		InputFormat: SourceFormatVersionedFlowSnap,
		Publisher:   "git-registry-dir",
		Destination: destination,
		Bucket:      "orders",
		Flow:        "customer-imports",
		PublishName: "publish-git-registry",
		OutputDir:   filepath.Join(tmpDir, "out"),
	})
	if err != nil {
		t.Fatalf("RunPublish() error = %v", err)
	}
	if result.Report.Summary.Files != 1 {
		t.Fatalf("expected 1 published file, got %d", result.Report.Summary.Files)
	}
	targetPath := filepath.Join(destination, "orders", "customer-imports", "snapshot.json")
	body, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("expected published snapshot at %s: %v", targetPath, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal published snapshot: %v", err)
	}
	if _, ok := payload["flowContents"]; !ok {
		t.Fatalf("expected flowContents in published snapshot")
	}
}

func TestRunPublishToNiFiRegistryCreatesBucketFlowAndVersion(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "snapshot.json")
	if err := os.WriteFile(inputPath, []byte(`{
  "bucketName": "orders",
  "flowName": "customer-imports",
  "flowContents": {
    "processors": []
  }
}`), 0o644); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	callLog := make([]string, 0, 5)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer registry-token" {
			t.Fatalf("expected bearer token header, got %q", got)
		}
		callLog = append(callLog, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/buckets":
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			_, _ = w.Write([]byte(`{"identifier":"bucket-1","name":"orders"}`))
		case "/buckets/bucket-1/flows":
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			_, _ = w.Write([]byte(`{"identifier":"flow-1","name":"customer-imports"}`))
		case "/buckets/bucket-1/flows/flow-1/versions/import":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read request body: %v", err)
			}
			if !strings.Contains(string(body), `"flowContents"`) {
				t.Fatalf("expected imported snapshot body, got %s", string(body))
			}
			_, _ = w.Write([]byte(`{"version":3}`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	result, err := RunPublish(PublishConfig{
		InputPath:            inputPath,
		InputFormat:          SourceFormatVersionedFlowSnap,
		Publisher:            "nifi-registry",
		RegistryURL:          server.URL,
		RegistryBucketName:   "orders",
		RegistryFlowName:     "customer-imports",
		RegistryCreateBucket: true,
		RegistryCreateFlow:   true,
		RegistryBearerToken:  "registry-token",
		PublishName:          "publish-registry",
		OutputDir:            filepath.Join(tmpDir, "out"),
	})
	if err != nil {
		t.Fatalf("RunPublish() error = %v", err)
	}
	if result.Report.Summary.Files != 1 {
		t.Fatalf("expected 1 published file, got %d", result.Report.Summary.Files)
	}
	if !strings.Contains(result.PublishedPath, "/buckets/bucket-1/flows/flow-1/versions/3") {
		t.Fatalf("expected published version path, got %s", result.PublishedPath)
	}
	if len(callLog) != 5 {
		t.Fatalf("expected 5 registry calls, got %d: %v", len(callLog), callLog)
	}
}

func TestRunRunExecutesAnalyzeRewriteValidateAndPublish(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "flow.json.gz")
	destination := filepath.Join(tmpDir, "published")
	writeGzipFile(t, sourcePath, `{
  "rootGroup": {
    "id": "root-1",
    "processors": [
      {
        "id": "proc-1",
        "name": "EncodeOrders",
        "type": "org.apache.nifi.processors.standard.Base64EncodeContent",
        "properties": {
          "Mode": "Encode"
        }
      }
    ]
  }
}`)

	result, err := RunRun(RunConfig{
		Analyze: AnalyzeConfig{
			SourcePath:    sourcePath,
			SourceFormat:  SourceFormatFlowJSONGZ,
			SourceVersion: "1.27.0",
			TargetVersion: "2.0.0",
			RulePackPaths: []string{filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.27-to-2.0.official.yaml")},
			FailOn:        "blocked",
		},
		Publish: PublishConfig{
			Publisher:   "fs",
			Destination: destination,
		},
		PublishEnabled: true,
		RunName:        "run-end-to-end",
		OutputDir:      filepath.Join(tmpDir, "out"),
	})
	if err != nil {
		t.Fatalf("RunRun() error = %v", err)
	}
	if result.Report.Summary.Status != "completed" {
		t.Fatalf("expected completed run status, got %s", result.Report.Summary.Status)
	}
	if len(result.Report.Steps) != 4 {
		t.Fatalf("expected 4 steps, got %d", len(result.Report.Steps))
	}
	publishedPath := filepath.Join(destination, "rewritten-flow.json.gz")
	if _, err := os.Stat(publishedPath); err != nil {
		t.Fatalf("expected published rewritten flow at %s: %v", publishedPath, err)
	}

	_, content, err := readSourceArtifact(publishedPath, SourceFormatFlowJSONGZ)
	if err != nil {
		t.Fatalf("read published rewritten flow: %v", err)
	}
	if !strings.Contains(content, "org.apache.nifi.processors.standard.EncodeContent") {
		t.Fatalf("expected published rewritten flow to contain EncodeContent, got %s", content)
	}
}

func writeGzipFile(t *testing.T, path, content string) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create gzip file: %v", err)
	}
	defer file.Close()

	zw := gzip.NewWriter(file)
	if _, err := zw.Write([]byte(content)); err != nil {
		t.Fatalf("write gzip content: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
}

func compareVersionStrings(left, right string) int {
	leftVersion, err := semver.NewVersion(left)
	if err != nil {
		panic(err)
	}
	rightVersion, err := semver.NewVersion(right)
	if err != nil {
		panic(err)
	}
	return leftVersion.Compare(rightVersion)
}
