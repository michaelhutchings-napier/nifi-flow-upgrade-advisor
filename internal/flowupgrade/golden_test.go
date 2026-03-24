package flowupgrade

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type goldenSnapshot struct {
	RulePacks     []string          `json:"rulePacks"`
	SourceVersion string            `json:"sourceVersion"`
	TargetVersion string            `json:"targetVersion"`
	Summary       ReportSummary     `json:"summary"`
	Findings      []goldenFinding   `json:"findings"`
}

type goldenFinding struct {
	RuleID           string            `json:"ruleId"`
	Class            string            `json:"class"`
	Severity         string            `json:"severity"`
	Message          string            `json:"message"`
	Component        *FindingComponent `json:"component,omitempty"`
	Evidence         []FindingEvidence `json:"evidence,omitempty"`
	SuggestedActions []string          `json:"suggestedActions,omitempty"`
}

func TestOfficialRulePackGoldenSnapshots(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		sourceVersion string
		targetVersion string
		rulePacks     []string
		payload       string
		format        SourceFormat
		goldenFile    string
	}{
		{
			name:          "official_1_21_to_1_22",
			sourceVersion: "1.21.0",
			targetVersion: "1.22.0",
			rulePacks: []string{
				filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.21-to-1.22.official.yaml"),
			},
			format: SourceFormatFlowJSONGZ,
			payload: `{
  "rootGroup": {
    "controllerServices": [
      {
        "id": "dbcp-1",
        "name": "LegacyH2Pool",
        "type": "org.apache.nifi.dbcp.DBCPConnectionPool",
        "properties": {
          "Database Connection URL": "jdbc:h2:file:./database/nifi"
        }
      },
      {
        "id": "jms-1",
        "name": "LegacyJndiJms",
        "type": "org.apache.nifi.jms.cf.JndiJmsConnectionFactoryProvider",
        "properties": {
          "Provider URL": "ldap://ldap.example.com:389/o=messaging"
        }
      }
    ],
    "processors": [
      {
        "id": "script-1",
        "name": "LegacyScript",
        "type": "org.apache.nifi.processors.script.ExecuteScript",
        "properties": {
          "Script Engine": "ruby"
        }
      },
      {
        "id": "azure-1",
        "name": "LegacyQueue",
        "type": "org.apache.nifi.processors.azure.storage.queue.GetAzureQueueStorage",
        "properties": {}
      },
      {
        "id": "ignite-1",
        "name": "IgniteCache",
        "type": "org.apache.nifi.processors.ignite.cache.GetIgniteCache",
        "properties": {}
      },
      {
        "id": "cassandra-1",
        "name": "WriteCassandra",
        "type": "org.apache.nifi.processors.cassandra.PutCassandraQL",
        "properties": {
          "Compression Type": "LZ4"
        }
      }
    ]
  }
}`,
			goldenFile: "official_1_21_to_1_22.json",
		},
		{
			name:          "official_1_22_to_1_23",
			sourceVersion: "1.22.0",
			targetVersion: "1.23.0",
			rulePacks: []string{
				filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.22-to-1.23.official.yaml"),
			},
			format: SourceFormatFlowJSONGZ,
			payload: `{
  "rootGroup": {
    "controllerServices": [
      {
        "id": "dbcp-1",
        "name": "OrdersDatabase",
        "type": "org.apache.nifi.dbcp.DBCPConnectionPool",
        "properties": {}
      }
    ],
    "processors": [
      {
        "id": "publish-jms-1",
        "name": "PublishOrders",
        "type": "org.apache.nifi.jms.processors.PublishJMS",
        "properties": {}
      },
      {
        "id": "legacy-1",
        "name": "OrdersRethinkDBLookup",
        "type": "org.apache.nifi.processors.rethinkdb.GetRethinkDB",
        "properties": {}
      }
    ]
  }
}`,
			goldenFile: "official_1_22_to_1_23.json",
		},
		{
			name:          "official_1_23_to_1_24",
			sourceVersion: "1.23.0",
			targetVersion: "1.24.0",
			rulePacks: []string{
				filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.23-to-1.24.official.yaml"),
			},
			format: SourceFormatFlowJSONGZ,
			payload: `{
  "rootGroup": {
    "processors": [
      {
        "id": "invoke-http-1",
        "name": "FetchOrders",
        "type": "org.apache.nifi.processors.standard.InvokeHTTP",
        "properties": {
          "HTTP URL": "https://example.com/orders path"
        }
      }
    ]
  }
}`,
			goldenFile: "official_1_23_to_1_24.json",
		},
		{
			name:          "official_1_24_to_1_25",
			sourceVersion: "1.24.0",
			targetVersion: "1.25.0",
			rulePacks: []string{
				filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.24-to-1.25.official.yaml"),
			},
			format: SourceFormatFlowJSONGZ,
			payload: `{
  "rootGroup": {
    "processors": [
      {
        "id": "script-1",
        "name": "LegacyJythonScript",
        "type": "org.apache.nifi.processors.script.ExecuteScript",
        "properties": {
          "Script Engine": "Jython"
        }
      },
      {
        "id": "encrypt-1",
        "name": "EncryptOrders",
        "type": "org.apache.nifi.processors.standard.EncryptContent",
        "properties": {}
      }
    ]
  }
}`,
			goldenFile: "official_1_24_to_1_25.json",
		},
		{
			name:          "official_1_25_to_1_26",
			sourceVersion: "1.25.0",
			targetVersion: "1.26.0",
			rulePacks: []string{
				filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.25-to-1.26.official.yaml"),
			},
			format: SourceFormatFlowJSONGZ,
			payload: `{
  "rootGroup": {
    "controllerServices": [
      {
        "id": "cass-1",
        "name": "OrdersCassandra",
        "type": "org.apache.nifi.service.CassandraSessionProvider",
        "properties": {}
      }
    ],
    "processors": [
      {
        "id": "avro-1",
        "name": "ConvertOrders",
        "type": "org.apache.nifi.processors.avro.ConvertAvroToJSON",
        "properties": {}
      },
      {
        "id": "solr-1",
        "name": "FetchFromSolr",
        "type": "org.apache.nifi.processors.solr.GetSolr",
        "properties": {}
      }
    ]
  }
}`,
			goldenFile: "official_1_25_to_1_26.json",
		},
		{
			name:          "official_1_26_to_1_27",
			sourceVersion: "1.26.0",
			targetVersion: "1.27.0",
			rulePacks: []string{
				filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.26-to-1.27.official.yaml"),
			},
			format: SourceFormatFlowJSONGZ,
			payload: `{
  "rootGroup": {
    "controllerServices": [
      {
        "id": "cb-1",
        "name": "OrdersCouchbase",
        "type": "org.apache.nifi.couchbase.CouchbaseClusterService",
        "properties": {}
      }
    ],
    "reportingTasks": [
      {
        "id": "datadog-1",
        "name": "DogStats",
        "type": "org.apache.nifi.reporting.datadog.DataDogReportingTask",
        "properties": {}
      }
    ],
    "processors": [
      {
        "id": "api-1",
        "name": "PublishApiGateway",
        "type": "org.apache.nifi.processors.aws.wag.InvokeAWSGatewayApi",
        "properties": {}
      },
      {
        "id": "cb-get-1",
        "name": "FetchFromCouchbase",
        "type": "org.apache.nifi.processors.couchbase.GetCouchbaseKey",
        "properties": {}
      }
    ]
  }
}`,
			goldenFile: "official_1_26_to_1_27.json",
		},
		{
			name:          "official_1_27_to_2_0",
			sourceVersion: "1.27.0",
			targetVersion: "2.0.0",
			rulePacks: []string{
				filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.27-to-2.0.official.yaml"),
			},
			format: SourceFormatFlowJSONGZ,
			payload: `{
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
			}`,
			goldenFile: "official_1_27_to_2_0.json",
		},
		{
			name:          "official_2_6_to_2_7",
			sourceVersion: "2.6.0",
			targetVersion: "2.7.0",
			rulePacks: []string{
				filepath.Join("..", "..", "examples", "rulepacks", "nifi-2.6-to-2.7.official.yaml"),
			},
			format: SourceFormatFlowJSONGZ,
			payload: `{
  "rootGroup": {
    "controllerServices": [
      {
        "id": "ssl-1",
        "name": "RestrictedSSL",
        "type": "org.apache.nifi.ssl.StandardRestrictedSSLContextService",
        "properties": {}
      }
    ],
    "processors": [
      {
        "id": "asana-1",
        "name": "AsanaImport",
        "type": "org.apache.nifi.processors.asana.GetAsanaObject",
        "properties": {}
      },
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
        "id": "gridfs-1",
        "name": "FetchGrid",
        "type": "org.apache.nifi.processors.mongodb.gridfs.FetchGridFS",
        "properties": {}
      }
    ]
  }
}`,
			goldenFile: "official_2_6_to_2_7.json",
		},
		{
			name:          "official_2_7_to_2_8",
			sourceVersion: "2.7.1",
			targetVersion: "2.8.0",
			rulePacks: []string{
				filepath.Join("..", "..", "examples", "rulepacks", "nifi-2.7-to-2.8.official.yaml"),
			},
			format: SourceFormatFlowJSONGZ,
			payload: `{
  "rootGroup": {
    "controllerServices": [
      {
        "id": "asana-service-1",
        "name": "AsanaService",
        "type": "org.apache.nifi.controller.asana.StandardAsanaClientProviderService",
        "properties": {}
      }
    ],
    "processors": [
      {
        "id": "asana-1",
        "name": "AsanaImport",
        "type": "org.apache.nifi.processors.asana.GetAsanaObject",
        "properties": {
          "Asana Client Service": "asana-service-1"
        }
      },
      {
        "id": "jolt-1",
        "name": "CustomJolt",
        "type": "org.apache.nifi.processors.jolt.JoltTransformJSON",
        "properties": {
          "Custom Transformation Class Name": "com.example.CustomTransform"
        }
      }
    ]
  }
}`,
			goldenFile: "official_2_7_to_2_8.json",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			sourcePath := filepath.Join(tmpDir, "source.json.gz")
			writeGzipFile(t, sourcePath, tc.payload)

			result, err := RunAnalyze(AnalyzeConfig{
				SourcePath:    sourcePath,
				SourceFormat:  tc.format,
				SourceVersion: tc.sourceVersion,
				TargetVersion: tc.targetVersion,
				RulePackPaths: tc.rulePacks,
				OutputDir:     filepath.Join(tmpDir, "out"),
				AnalysisName:  tc.name,
				FailOn:        "never",
			})
			if err != nil {
				t.Fatalf("RunAnalyze() error = %v", err)
			}

			snapshot := buildGoldenSnapshot(result.Report)
			assertGoldenJSON(t, filepath.Join("testdata", "golden", tc.goldenFile), snapshot)
		})
	}
}

func TestRealisticFixturesAnalyze(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		fixture       string
		sourceVersion string
		targetVersion string
		rulePacks     []string
		minFindings   int
	}{
		{
			name:          "orders_1_27_to_2_0",
			fixture:       filepath.Join("..", "..", "demo", "fixtures", "orders-platform-1.27-flow.json"),
			sourceVersion: "1.27.0",
			targetVersion: "2.0.0",
			rulePacks: []string{
				filepath.Join("..", "..", "examples", "rulepacks", "nifi-1.27-to-2.0.official.yaml"),
			},
			minFindings: 4,
		},
		{
			name:          "orders_2_7_to_2_8",
			fixture:       filepath.Join("..", "..", "demo", "fixtures", "orders-platform-2.7-flow.json"),
			sourceVersion: "2.7.1",
			targetVersion: "2.8.0",
			rulePacks: []string{
				filepath.Join("..", "..", "examples", "rulepacks", "nifi-2.7-to-2.8.official.yaml"),
			},
			minFindings: 3,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			sourcePath := filepath.Join(tmpDir, "fixture.json.gz")
			gzipFixtureToPath(t, tc.fixture, sourcePath)

			result, err := RunAnalyze(AnalyzeConfig{
				SourcePath:    sourcePath,
				SourceFormat:  SourceFormatFlowJSONGZ,
				SourceVersion: tc.sourceVersion,
				TargetVersion: tc.targetVersion,
				RulePackPaths: tc.rulePacks,
				OutputDir:     filepath.Join(tmpDir, "out"),
				AnalysisName:  tc.name,
				FailOn:        "never",
			})
			if err != nil {
				t.Fatalf("RunAnalyze() error = %v", err)
			}
			if len(result.Report.Findings) < tc.minFindings {
				t.Fatalf("expected at least %d findings, got %d", tc.minFindings, len(result.Report.Findings))
			}
		})
	}
}

func buildGoldenSnapshot(report MigrationReport) goldenSnapshot {
	rulePacks := make([]string, 0, len(report.RulePacks))
	for _, pack := range report.RulePacks {
		rulePacks = append(rulePacks, pack.Name)
	}

	findings := make([]goldenFinding, 0, len(report.Findings))
	for _, finding := range report.Findings {
		actionTypes := make([]string, 0, len(finding.SuggestedActions))
		for _, action := range finding.SuggestedActions {
			actionTypes = append(actionTypes, action.Type)
		}
		findings = append(findings, goldenFinding{
			RuleID:           finding.RuleID,
			Class:            finding.Class,
			Severity:         finding.Severity,
			Message:          finding.Message,
			Component:        finding.Component,
			Evidence:         finding.Evidence,
			SuggestedActions: actionTypes,
		})
	}

	return goldenSnapshot{
		RulePacks:     rulePacks,
		SourceVersion: report.Source.NiFiVersion,
		TargetVersion: report.Target.NiFiVersion,
		Summary:       report.Summary,
		Findings:      findings,
	}
}

func assertGoldenJSON(t *testing.T, goldenPath string, snapshot goldenSnapshot) {
	t.Helper()

	actual, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		t.Fatalf("marshal actual snapshot: %v", err)
	}
	actual = append(actual, '\n')

	update := os.Getenv("UPDATE_GOLDEN") == "1"
	if update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, actual, 0o644); err != nil {
			t.Fatalf("write golden file: %v", err)
		}
	}

	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden file %s: %v", goldenPath, err)
	}
	if string(expected) != string(actual) {
		t.Fatalf("golden mismatch for %s\nexpected:\n%s\nactual:\n%s", goldenPath, string(expected), string(actual))
	}
}

func gzipFixtureToPath(t *testing.T, fixturePath, outPath string) {
	t.Helper()

	src, err := os.Open(fixturePath)
	if err != nil {
		t.Fatalf("open fixture %s: %v", fixturePath, err)
	}
	defer src.Close()

	dst, err := os.Create(outPath)
	if err != nil {
		t.Fatalf("create gzip output %s: %v", outPath, err)
	}
	defer dst.Close()

	zw := gzip.NewWriter(dst)
	if _, err := io.Copy(zw, src); err != nil {
		t.Fatalf("copy fixture into gzip output: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
}
