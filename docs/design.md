# Flow Upgrade Advisor Design

## Summary

NiFi-Fabric should support a Flow Upgrade Advisor as external-first tooling, not as a controller feature.

The tool solves a narrow problem:

- inspect a source flow artifact
- compare it to a selected target NiFi version
- report what changed
- apply deterministic safe rewrites and assisted scaffolds when possible
- validate the rewritten result
- publish the upgraded flow explicitly

This keeps the runtime product simple while still giving users a practical migration path.

## Why This Exists

Apache NiFi provides migration guidance, deprecation logging, flow analysis rules, versioned flows, and registry integration, but not one upstream open-source tool that covers the full workflow end to end.

Users want one repeatable flow:

1. select source and target versions
2. load a user flow
3. see the impact
4. auto-fix what is safe
5. validate the result
6. publish it into the right registry or Git-backed source

## Product Shape

Use two layers:

- dedicated repo: the main reusable tool, named `nifi-flow-upgrade-advisor`
- repo-local wrapper: a thin pointer in consumer repositories such as NiFi-Fabric for pinned versions, example configs, and workflow integration

The current repository should not own the core migration engine long term. It should only make the tool easy to use alongside NiFi-Fabric.

## User Experience

The intended user experience should stay simple:

- desktop app first for most humans
- CLI first for automation, CI, and GitOps
- `analyze`: tell me whether this source version can move to this target version, what is deprecated or removed, and what must change
- `rewrite`: apply deterministic safe conversions plus assisted scaffolds and produce a rewritten artifact plus a rewrite report

The polished default experience should be a desktop wrapper over the same engine, not a second migration engine.

The preferred desktop direction is a Tauri application that:

- auto-detects likely flow artifacts in a selected workspace or repository
- lets users select source and target versions
- runs the existing `analyze`, `rewrite`, `validate`, and `run` commands
- renders the existing JSON and Markdown reports
- never forks the rule engine or rewrite logic away from the CLI

The CLI remains the stable automation surface underneath the desktop experience.

## Core Principles

- offline first
- explicit source and target version selection
- version-pair rule packs instead of hidden generic behavior
- safe deterministic rewrites only
- human-readable reports before publish
- publish is explicit, never a side effect of analysis
- no new CRDs
- no controller-managed flow migration
- no broad live graph management

## Supported Workflow

### 1. Load

Supported source inputs should include:

- `flow.json.gz`
- exported versioned flow snapshots
- Git-backed registry flow directories
- NiFi Registry exported flow versions

Current implementation note:

- JSON-based flow artifacts are supported now
- Git-backed registry directories are supported for both analysis and rewrite
- raw legacy `flow.xml.gz` parsing from older NiFi `1.x` is supported for analysis and validation
- rewrite remains JSON-only for now

Optional side inputs:

- source NiFi version
- target NiFi version
- source extension inventory
- target extension inventory
- parameter-context exports

### 2. Analyze

The tool builds a migration plan by combining:

- version-pair rules
- component and bundle availability checks
- property and allowable-value changes
- parameter and variable compatibility checks
- extension coordinate renames or removals
- optional target extension inventory validation from an extensions manifest

The report should classify findings as:

- `auto-fix`
- `assisted-rewrite`
- `manual-change`
- `manual-inspection`
- `blocked`
- `info`

### 3. Rewrite

The tool may apply safe rewrites such as:

- component type renames with compatible configuration mapping
- property renames
- assisted property scaffolding where the target shape is known but still needs review
- removed property cleanup where the replacement is unambiguous
- bundle coordinate updates
- variable-to-parameter conversion scaffolding when deterministic

The tool must not guess through domain-specific intent. If a rewrite depends on business meaning, credentials, or ambiguous routing logic, it should stay manual.

### 4. Validate

Validation should happen before publish and should be runnable in CI.

Validation layers:

- artifact schema validation
- extension availability against the selected target
- reference validation for controller services and parameter contexts
- live target NiFi API validation for version, extension inventory, and process group readiness

### 5. Publish

Publishing should be a separate command and should support:

- filesystem output only
- Git-backed registry layout for GitHub, GitLab, or Bitbucket workflows
- NiFi Registry import

### 6. Deploy

NiFi-Fabric deployment stays on the existing product paths:

- Flow Registry Client catalogs
- Git-based registry workflows
- `versionedFlowImports.*`

The upgrade tool prepares artifacts. NiFi-Fabric deploys them.

## CLI Shape

The core CLI should stay boring and scriptable.

Phase 1 implementation detail lives in:

- [Flow Upgrade Advisor Phase 1 CLI Contract](cli.md)
- [Flow Upgrade Advisor Rule-Pack File Format](rule-pack-format.md)
- [Release Process](release-process.md)
- [Troubleshooting](troubleshooting.md)

Longer-term command direction:

```text
nifi-flow-upgrade analyze  --source ... --source-version ... --target-version ...
nifi-flow-upgrade rewrite  --plan migration-report.json --out ./out
nifi-flow-upgrade validate --input ./out --target-version ...
nifi-flow-upgrade publish  --input ./out --publisher git|nifi-registry|fs ...
```

Optional convenience command:

```text
nifi-flow-upgrade run --source ... --source-version ... --target-version ... --publish ...
```

`run` should still emit the intermediate report and rewritten artifacts, not hide them.

The original Phase 1 baseline implemented `analyze`, `rule-pack lint`, and `version`.

The current branch also includes:

- the first Phase 2 command: `rewrite`
- an initial `validate` command focused on artifact readability, target extension inventory checks, live target NiFi API validation, and process group readiness checks before import or update
- a `publish` command for filesystem, Git-backed registry layout output, and NiFi Registry import
- a `run` command that orchestrates `analyze`, `rewrite`, `validate`, and optional `publish` without hiding the intermediate artifacts

If the Tauri wrapper is enabled, it should remain a shell around these commands rather than a replacement for them. The desktop application may add:

- workspace and flow auto-detection
- one-click command execution
- in-app report viewing
- recent-project history

## Rule Engine

The migration engine should use explicit rule packs keyed by source and target versions.

The concrete `v1alpha1` file format lives in [Flow Upgrade Advisor Rule-Pack File Format](rule-pack-format.md).

Rule-pack authors should treat the official Apache NiFi Migration Guidance as a primary source for version caveats and encode those caveats into rule `references` and `notes` instead of assuming the CLI can infer them automatically.

Recommended rule categories:

- `component-removed`
- `component-replaced`
- `bundle-renamed`
- `property-renamed`
- `property-value-changed`
- `property-removed`
- `variable-migration`
- `manual-inspection`
- `blocked`

Recommended rule fields:

- source version range
- target version range
- component or bundle selector
- matching property selector
- message
- optional deterministic transform
- evidence or reference URL

This gives predictable growth from `1.27 -> 2.0`, then `2.0 -> 2.4`, `2.4 -> 2.8`, and so on, instead of pretending one huge ruleset covers every path equally well.

Where Apache migration guidance includes patch-specific caveats, the tool should support patch-targeted rule packs such as `2.6.x -> 2.7.0` in addition to broader minor-line packs like `2.6.x -> 2.7.x`.

## Outputs

Minimum outputs:

- `migration-report.json`
- `migration-report.md`
- rewritten flow artifact in target format
- optional generated parameter-context patch data
- optional publish metadata for Git or registry import

The JSON report should be stable enough for CI filtering. The Markdown report should be optimized for pull requests and human review.

## Productionization

To productionize this tool, the focus should be reliability and explainability over aggressive automation.

Recommended production shape:

- signed releases for the CLI or container image
- pinned rule-pack bundles versioned independently from the engine
- CI execution with JSON report gating and Markdown PR summaries
- optional live validation against a real target NiFi API before publish
- strong fixture coverage for each supported version pair and each deterministic rewrite action
- clear provenance in findings and rewrite operations linking back to official Apache migration sources
- a confidence-first rewrite policy where unsupported or ambiguous migrations stay manual by default

Recommended feature priorities after the current analyzer and rewrite baseline:

- richer deterministic actions such as explicit property set and controller-service scaffold generation
- deeper target-side update and import orchestration for publish-time workflows
- rewrite support for additional source formats such as `git-registry-dir`
- optional local visual wrapper on top of the CLI and reports

## NiFi-Fabric Integration

This repository should only add thin integration:

- pinned tool version or container image reference
- sample configs for supported NiFi-Fabric workflows
- examples that publish to Git-backed registries or NiFi Registry
- optional generated values snippet for `versionedFlowImports.*`

Useful generated output for this repo:

- a versioned flow artifact ready for Git-based registry storage
- a values overlay snippet pointing at the upgraded flow version
- a report that can be attached to a GitOps pull request

## Non-Goals

- live synchronization of user flows
- in-cluster mutation of arbitrary process groups
- user and policy migration
- generic registry administration
- support for running NiFi `1.x` in NiFi-Fabric
- a second deployment API surface

## MVP

### Phase 1

- analyze source flow artifacts
- implement the stable `analyze`, `rule-pack lint`, and `version` command surface
- validate and load explicit rule-pack files
- emit structured report
- no automated target mutation

### Phase 2

- add deterministic rewrites
- implement `rewrite` for deterministic `auto-fix` actions on JSON-based flow artifacts and Git-backed registry trees
- allow `rewrite` to consume the prior `migration-report.json` as a plan bridge from analysis into execution
- add target extension validation
- add explicit `publish` support for filesystem and Git-backed registry layout output
- emit PR-friendly Markdown summaries

### Phase 3

- generate NiFi-Fabric values snippets for deployment handoff

## Verification Notes

Each supported version pair needs fixture coverage for:

- no-op compatible flow
- auto-fix case
- manual-change case
- blocked case

End-to-end validation should cover:

- upgrade a known source fixture
- validate against a target NiFi `2.x` image
- publish the result
- deploy it through the documented NiFi-Fabric import path on kind
