# Flow Upgrade Advisor CLI Contract

## Status

This document defines the implementation contract for the Flow Upgrade Advisor CLI.

This document started as the Phase 1 CLI contract and now records the implemented command surface for `analyze`, `rewrite`, `validate`, `publish`, and `run`.

The current user experience is desktop-first for people and CLI-first for automation.

The desktop app is the default way to use the tool interactively. The CLI remains the stable command surface for:

- one boring command per step
- concise stdout summaries
- machine-readable JSON output for CI
- human-readable Markdown reports for pull requests and review

The intended user-facing modes are:

- `analyze`: inspection and preflight
- `rewrite`: deterministic upgrade or conversion where the rule pack declares it safe
- `validate`: target-facing checks on the produced or selected artifact

The desktop app sits on top of these commands and generated reports instead of inventing a separate workflow.

## Binary Name

The canonical binary name is:

```text
nifi-flow-upgrade
```

## Phase 1 Command Surface

Implemented now:

- `analyze`
- `rewrite`
- `validate`
- `publish`
- `run`
- `rule-pack lint`
- `version`

## Command Summary

### `analyze`

Analyze a source flow artifact against a selected target NiFi version and emit a machine-readable report plus an optional Markdown summary.

Example:

```text
nifi-flow-upgrade analyze \
  --source ./fixtures/source/flow.json.gz \
  --source-format flow-json-gz \
  --source-version 1.27.0 \
  --target-version 2.0.0 \
  --rule-pack ./rulepacks/nifi-1.27-to-2.0.yaml \
  --output-dir ./out
```

### `rule-pack lint`

Validate that one or more rule-pack files are structurally valid and internally consistent.

Example:

```text
nifi-flow-upgrade rule-pack lint --rule-pack ./rulepacks/nifi-1.27-to-2.0.yaml
```

### `rewrite`

Apply deterministic `auto-fix` actions to a source flow artifact and emit a rewritten artifact plus a rewrite report.

Example:

```text
nifi-flow-upgrade rewrite \
  --plan ./out/migration-report.json \
  --output-dir ./out
```

Or without a prior plan file:

```text
nifi-flow-upgrade rewrite \
  --source ./fixtures/source/flow.json.gz \
  --source-format flow-json-gz \
  --source-version 1.27.0 \
  --target-version 2.0.0 \
  --rule-pack ./rulepacks/nifi-1.27-to-2.0.yaml \
  --output-dir ./out
```

Current rewrite support is intentionally narrow:

- only rules with `class: auto-fix` execute actions
- only deterministic action types execute
- current artifact support includes file-based JSON inputs such as `flow-json-gz`, `versioned-flow-snapshot`, and `nifi-registry-export`
- `git-registry-dir` rewrite support is also implemented for JSON files inside Git-backed registry trees
- component replacements only auto-execute when the rule pack provides an explicit, mechanically safe property mapping
- rewrite reports should preserve rule notes and references so official Apache migration caveats remain visible during execution, not just analysis

Current source-format boundary:

- JSON-based flow artifacts work now
- Git-backed registry directories work for both analysis and rewrite
- raw NiFi `1.x` `flow.xml.gz` works for `analyze` and `validate`
- `rewrite` remains limited to JSON-structured artifacts and does not support raw `flow.xml.gz`

### `validate`

Validate an input flow artifact against selected target checks and emit a validation report.

Example:

```text
nifi-flow-upgrade validate \
  --input ./out/rewritten-flow.json.gz \
  --input-format flow-json-gz \
  --target-version 2.0.0 \
  --extensions-manifest ./manifests/nifi-2.0-core.yaml \
  --output-dir ./out
```

### `version`

Print the CLI version, build metadata, and supported spec versions.

Example:

```text
nifi-flow-upgrade version
```

### `publish`

Publish a selected artifact into a filesystem destination, Git-backed registry layout, or NiFi Registry and emit a publish report.

Example:

```text
nifi-flow-upgrade publish \
  --input ./out/rewritten-flow.json.gz \
  --input-format flow-json-gz \
  --publisher fs \
  --destination ./published \
  --output-dir ./out
```

Example Git-backed registry publish:

```text
nifi-flow-upgrade publish \
  --input ./out/rewritten-snapshot.json \
  --input-format versioned-flow-snapshot \
  --publisher git-registry-dir \
  --destination ./registry \
  --bucket customer-a \
  --flow orders \
  --output-dir ./out
```

Example NiFi Registry publish:

```text
nifi-flow-upgrade publish \
  --input ./out/rewritten-snapshot.json \
  --input-format versioned-flow-snapshot \
  --publisher nifi-registry \
  --registry-url https://registry.example.com \
  --registry-bucket-name customer-a \
  --registry-flow-name orders \
  --registry-create-flow \
  --output-dir ./out
```

### `run`

Execute `analyze`, `rewrite`, `validate`, and optional `publish` as one explicit workflow while still emitting the intermediate artifacts and reports.

Example:

```text
nifi-flow-upgrade run \
  --source ./fixtures/source/flow.json.gz \
  --source-format flow-json-gz \
  --source-version 1.27.0 \
  --target-version 2.0.0 \
  --rule-pack ./rulepacks/nifi-1.27-to-2.0.yaml \
  --publish \
  --publisher fs \
  --destination ./published \
  --output-dir ./out
```

## `analyze` Flags

### Required

- `--source <path>`
- `--source-version <version>`
- `--target-version <version>`
- at least one `--rule-pack <path>`

### Optional

- `--source-format <format>`
- `--output-dir <path>`
- `--report-json <path>`
- `--report-md <path>`
- `--fail-on <level>`
- `--strict`
- `--name <analysis-name>`
- `--extensions-manifest <path>`
- `--parameter-contexts <path>`
- `--allow-unsupported-version-pair`

### `--source-format`

Allowed values:

- `auto`
- `flow-json-gz`
- `flow-xml-gz`
- `versioned-flow-snapshot`
- `git-registry-dir`
- `nifi-registry-export`

Default:

- `auto`

Phase 1 may support only a subset of formats in the implementation, but the contract should stay stable. Unsupported formats must fail clearly.

### `--fail-on`

Allowed values:

- `never`
- `blocked`
- `manual-change`

Meaning:

- `never`: return success unless the tool itself fails
- `blocked`: return non-zero when blocked findings exist
- `manual-change`: return non-zero when either manual-change or blocked findings exist

Default:

- `blocked`

### `--strict`

When `--strict` is set, unknown source content, unparseable properties, unknown component types, or unmatched version-pair gaps should be emitted as `blocked` findings instead of lower-severity informational findings.

### `--extensions-manifest`

When `--extensions-manifest` is provided, `analyze` should validate each source component against the declared target inventory.

Current behavior:

- the analyzer uses `auto-fix` type-replacement actions when determining the planned target component type
- if the manifest does not include the planned target component, the analyzer emits a `blocked` `system.target-extension-unavailable` finding
- this is intended to catch version-pair rules that are correct in principle but still not satisfiable against the selected target extension set

### `--allow-unsupported-version-pair`

By default, a source and target pair with no matching loaded rule pack is an error.

When `--allow-unsupported-version-pair` is set, the tool should:

- continue analysis
- emit one `blocked` finding explaining that no rule pack supports the selected version pair
- return according to the selected `--fail-on` threshold instead of exit code `5`

## `rule-pack lint` Flags

### Required

- at least one `--rule-pack <path>`

### Optional

- `--fail-on-warn`
- `--format <text|json>`

Default output format:

- `text`

## `validate` Flags

### Required

- `--input <path>`
- `--target-version <version>`

### Optional

- `--input-format <format>`
- `--extensions-manifest <path>`
- `--target-api-url <url>`
- `--target-api-bearer-token <token>`
- `--target-api-bearer-token-env <env-var>`
- `--target-api-insecure-skip-tls-verify`
- `--target-process-group-id <id>`
- `--target-process-group-mode <auto|replace|update>`
- `--output-dir <path>`
- `--report-json <path>`
- `--report-md <path>`
- `--name <validation-name>`

## `publish` Flags

### Required

- `--input <path>`
- `--publisher <fs|git-registry-dir|nifi-registry>`
- `--destination <path>` for `fs` and `git-registry-dir`
- `--registry-url <url>` for `nifi-registry`

### Optional

- `--input-format <format>`
- `--bucket <name>`
- `--flow <name>`
- `--file-name <name>`
- `--registry-bucket-id <id>`
- `--registry-bucket-name <name>`
- `--registry-flow-id <id>`
- `--registry-flow-name <name>`
- `--registry-create-bucket`
- `--registry-create-flow`
- `--registry-bearer-token <token>`
- `--registry-bearer-token-env <env-var>`
- `--registry-basic-username <username>`
- `--registry-basic-password <password>`
- `--registry-basic-password-env <env-var>`
- `--registry-insecure-skip-tls-verify`
- `--output-dir <path>`
- `--report-json <path>`
- `--report-md <path>`
- `--name <publish-name>`

### `--publisher`

Allowed values:

- `fs`
- `git-registry-dir`
- `nifi-registry`

Current behavior:

- `fs` copies the input artifact or directory into the selected destination
- `git-registry-dir` writes JSON content into a Git-backed registry directory layout under `destination/bucket/flow/`
- `git-registry-dir` accepts JSON-based artifacts and existing `git-registry-dir` trees
- legacy `flow.xml.gz` is not a publish source for `git-registry-dir`
- `nifi-registry` imports the next version into an existing or explicitly-created Registry flow
- `nifi-registry` currently supports `versioned-flow-snapshot` and `nifi-registry-export` inputs only

## `run` Flags

### Required

- `--source <path>`
- `--source-version <version>`
- `--target-version <version>`
- at least one `--rule-pack <path>`

### Optional

- `--source-format <format>`
- `--output-dir <path>`
- `--report-json <path>`
- `--report-md <path>`
- `--name <run-name>`
- `--fail-on <level>`
- `--strict`
- `--extensions-manifest <path>`
- `--parameter-contexts <path>`
- `--allow-unsupported-version-pair`
- `--target-api-url <url>`
- `--target-api-bearer-token <token>`
- `--target-api-bearer-token-env <env-var>`
- `--target-api-insecure-skip-tls-verify`
- `--target-process-group-id <id>`
- `--target-process-group-mode <auto|replace|update>`
- `--publish`
- all publish flags from `publish`

Current behavior:

- `run` stops after `analyze` when the selected `--fail-on` threshold is exceeded
- `run` stops before `publish` when `validate` reports blocked findings
- `run` writes a `run-report.json` and `run-report.md` alongside the per-step reports
- `run` does not hide intermediate files; it still writes the migration report, rewritten artifact, validation report, and optional publish report

### `--target-api-url`

When `--target-api-url` is provided, `validate` runs live target validation in addition to local artifact validation.

Current behavior:

- the validator normalizes either a NiFi base URL or an API root to the `/nifi-api` base path
- the validator queries `GET /flow/about` to confirm connectivity and discover the running NiFi version
- the validator queries live extension inventory endpoints to compare the artifact against what the target can actually run
- a mismatch between `--target-version` and the target NiFi API version should be emitted as a blocking finding
- Bearer-token authentication is supported from either `--target-api-bearer-token` or `--target-api-bearer-token-env`

### `--target-process-group-id`

When `--target-process-group-id` is provided together with `--target-api-url`, `validate` performs target process group readiness checks.

Current behavior:

- the validator loads the target process group from `GET /flow/process-groups/{id}`
- the validator chooses an effective mode from `auto`, `replace`, or `update`
- `replace` blocks when the target process group is already under version control
- `update` blocks when the target process group is not under version control
- locally modified, stale, synchronization-failed, or invalid target process groups are treated conservatively
- when the input artifact is a versioned flow snapshot, the validator compares the snapshot flow identity with the target process group's version-control identity

### `--target-process-group-mode`

Allowed values:

- `auto`
- `replace`
- `update`

Meaning:

- `auto`: infer `update` for version-controlled process groups and `replace` otherwise
- `replace`: validate as if the destination process group will be replaced by an imported flow
- `update`: validate as if the destination process group will receive a versioned-flow update

Production guidance:

- prefer `--target-api-bearer-token-env` in CI and shared shells so credentials do not land in shell history
- use `--target-api-insecure-skip-tls-verify` only for local testing or temporary break-glass scenarios

## `rewrite` Flags

### Required

Choose one of:

- `--plan <migration-report.json>`
- or the direct input set:
  - `--source <path>`
  - `--source-version <version>`
  - `--target-version <version>`
  - at least one `--rule-pack <path>`

### Optional

- `--source-format <format>`
- `--output-dir <path>`
- `--rewritten-flow <path>`
- `--rewrite-report-json <path>`
- `--rewrite-report-md <path>`
- `--name <rewrite-name>`
- `--allow-unsupported-version-pair`

### `--plan`

When `--plan` is supplied, `rewrite` should load the prior `migration-report.json` and use it as the default source of:

- source artifact path
- source format
- source NiFi version
- target NiFi version
- rule-pack paths

Explicit rewrite flags override the values loaded from the plan.

## Input Resolution Rules

### Source Version

`--source-version` is required in Phase 1 even if the artifact appears to carry version metadata. The tool should not silently infer a source version when version-pair correctness matters.

### Target Version

`--target-version` is always required.

### Rule Packs

Multiple `--rule-pack` flags are allowed.

The tool should:

1. load all provided packs
2. reject duplicate rule IDs across the loaded set
3. filter rules by version-pair match
4. analyze the artifact against the resulting rule set

The tool must not auto-discover rule packs from implicit directories in Phase 1.

### Extensions Manifest

The target extensions manifest is optional but recommended for production usage.

The current manifest file format is YAML:

```yaml
apiVersion: flow-upgrade.nifi.advisor/v1alpha1
kind: ExtensionsManifest
metadata:
  name: nifi-2.0-core
spec:
  nifiVersion: 2.0.0
  components:
    - scope: processor
      type: org.apache.nifi.processors.standard.InvokeHTTP
    - scope: controller-service
      type: org.apache.nifi.distributed.cache.client.MapCacheClientService
```

If no loaded rule pack matches the selected version pair:

- return exit code `5` by default
- or emit a `blocked` finding when `--allow-unsupported-version-pair` is set

## Output Contract

### Default Output Directory

If `--output-dir` is omitted, the tool should write files under:

```text
./flow-upgrade-out/<analysis-name>/
```

If `--name` is omitted, the tool should generate a stable timestamped name.

### Files

Phase 1 should write:

- `migration-report.json`
- `migration-report.md`

If explicit report paths are provided, they override the default file locations.

### Standard Output

`analyze` should print:

- a short analysis summary
- the number of findings by class
- the location of written reports
- whether the selected `--fail-on` threshold was exceeded

### JSON Report Shape

The JSON report should contain at least:

```json
{
  "apiVersion": "flow-upgrade.nifi.advisor/v1alpha1",
  "kind": "MigrationReport",
  "metadata": {
    "name": "example-analysis",
    "generatedAt": "2026-03-22T18:00:00Z"
  },
  "source": {
    "path": "./fixtures/source/flow.json.gz",
    "format": "flow-json-gz",
    "nifiVersion": "1.27.0"
  },
  "target": {
    "nifiVersion": "2.0.0"
  },
  "rulePacks": [
    {
      "name": "nifi-1.27-to-2.0-core",
      "path": "./rulepacks/nifi-1.27-to-2.0.yaml"
    }
  ],
  "summary": {
    "totalFindings": 3,
    "byClass": {
      "auto-fix": 1,
      "manual-change": 1,
      "manual-inspection": 0,
      "blocked": 1,
      "info": 0
    }
  },
  "findings": [
    {
      "ruleId": "core.kafka.bundle-rename",
      "class": "auto-fix",
      "severity": "warning",
      "component": {
        "id": "1234-5678",
        "name": "ConsumeKafka",
        "type": "org.apache.nifi.processors.kafka.pubsub.ConsumeKafka"
      },
      "message": "Bundle coordinates changed in the selected target line.",
      "references": [
        "https://cwiki.apache.org/confluence/display/NIFI/Migration%2BGuidance"
      ],
      "suggestedActions": [
        {
          "type": "update-bundle-coordinate",
          "params": {
            "group": "org.apache.nifi",
            "artifact": "nifi-kafka-2-processors"
          }
        }
      ]
    }
  ]
}
```

Additional fields may be added later, but existing fields must remain backward compatible within the `v1alpha1` report line.

### Markdown Report Shape

The Markdown report should include:

- analysis metadata
- source and target version pair
- loaded rule packs
- summary counts by finding class
- grouped findings
- a short recommended next-step section

The Markdown report is for human review, not stable machine parsing.

## Finding Model

Phase 1 findings use two orthogonal fields:

- `class`
- `severity`

Allowed `class` values:

- `auto-fix`
- `manual-change`
- `manual-inspection`
- `blocked`
- `info`

Allowed `severity` values:

- `info`
- `warning`
- `error`

`class` is the workflow recommendation. `severity` is the urgency signal.

## Exit Codes

Phase 1 exit codes:

- `0`: analysis or lint completed and the selected failure threshold was not exceeded
- `1`: CLI usage error, invalid flags, or missing required inputs
- `2`: analysis completed, but the selected `--fail-on` threshold was exceeded
- `3`: source artifact could not be read or parsed
- `4`: one or more rule packs are invalid
- `5`: no loaded rule pack supports the selected source and target version pair
- `10`: internal tool error

## Determinism Rules

Phase 1 analysis must be deterministic for the same:

- input artifact bytes
- source and target versions
- rule-pack set
- CLI flags

The tool must not fetch external metadata, extension catalogs, or migration guidance by default during analysis.

## Compatibility Promise

Within Phase 1:

- flag names and meanings should remain stable
- exit codes should remain stable
- report top-level fields should remain stable

Future phases may add new commands and optional fields, but should not silently change the behavior of the Phase 1 contract.
