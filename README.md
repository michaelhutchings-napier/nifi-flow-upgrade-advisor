# nifi-flow-upgrade-advisor

`nifi-flow-upgrade-advisor` is an offline CLI for Apache NiFi flow upgrade analysis, deterministic rewrites, and target validation.

It is intentionally separate from runtime platforms such as NiFi-Fabric. The job of this repo is to inspect a source flow artifact, compare it with a selected target NiFi version, explain what changed, apply only mechanically safe rewrites, and validate the result before publish.

## Scope

Current commands:

- `analyze`
- `rewrite`
- `validate`
- `publish`
- `run`
- `rule-pack lint`
- `version`

Current source support:

- `flow.json.gz`
- versioned flow snapshots
- NiFi Registry export JSON
- Git-backed registry directories for analysis and rewrite
- legacy `flow.xml.gz` for analysis and validation

Current safety boundary:

- safe deterministic rewrites only
- no guessing through deprecated processors that require architecture decisions
- no secret recovery
- no in-cluster mutation

What `validate` covers now:

- target NiFi version checks via `/flow/about`
- target runtime extension inventory checks via `/flow/runtime-manifest`
- target process group readiness checks for pre-import or pre-update validation

What `publish` covers now:

- filesystem publish for rewritten artifacts and directories
- Git-backed registry layout publish for JSON-based snapshots and registry trees
- NiFi Registry version import for versioned flow snapshots and Registry export JSON

## Layout

- [`cmd/nifi-flow-upgrade`](/home/michael/Work/nifi-flow-upgrade-advisor/cmd/nifi-flow-upgrade)
- [`internal/flowupgrade`](/home/michael/Work/nifi-flow-upgrade-advisor/internal/flowupgrade)
- [`docs/design.md`](/home/michael/Work/nifi-flow-upgrade-advisor/docs/design.md)
- [`docs/cli.md`](/home/michael/Work/nifi-flow-upgrade-advisor/docs/cli.md)
- [`docs/rule-pack-format.md`](/home/michael/Work/nifi-flow-upgrade-advisor/docs/rule-pack-format.md)
- [`examples/rulepacks`](/home/michael/Work/nifi-flow-upgrade-advisor/examples/rulepacks)
- [`examples/manifests`](/home/michael/Work/nifi-flow-upgrade-advisor/examples/manifests)

## Quick Start

Build:

```bash
go build ./cmd/nifi-flow-upgrade
```

Analyze:

```bash
./nifi-flow-upgrade analyze \
  --source ./fixtures/source/flow.json.gz \
  --source-format flow-json-gz \
  --source-version 1.27.0 \
  --target-version 2.0.0 \
  --rule-pack ./examples/rulepacks/nifi-1.27-to-2.0.official.yaml \
  --output-dir ./out
```

Rewrite:

```bash
./nifi-flow-upgrade rewrite \
  --plan ./out/migration-report.json \
  --output-dir ./out
```

Validate:

```bash
./nifi-flow-upgrade validate \
  --input ./out/rewritten-flow.json.gz \
  --input-format flow-json-gz \
  --target-version 2.0.0 \
  --target-api-url https://nifi.example.com \
  --target-process-group-id 1234-abcd \
  --output-dir ./out
```

Publish:

```bash
./nifi-flow-upgrade publish \
  --input ./out/rewritten-flow.json.gz \
  --input-format flow-json-gz \
  --publisher fs \
  --destination ./published \
  --output-dir ./out
```

Run:

```bash
./nifi-flow-upgrade run \
  --source ./fixtures/source/flow.json.gz \
  --source-format flow-json-gz \
  --source-version 1.27.0 \
  --target-version 2.0.0 \
  --rule-pack ./examples/rulepacks/nifi-1.27-to-2.0.official.yaml \
  --publish \
  --publisher fs \
  --destination ./published \
  --output-dir ./out
```

Publish to NiFi Registry:

```bash
./nifi-flow-upgrade publish \
  --input ./out/rewritten-snapshot.json \
  --input-format versioned-flow-snapshot \
  --publisher nifi-registry \
  --registry-url https://registry.example.com \
  --registry-bucket-name customer-a \
  --registry-flow-name orders \
  --registry-create-flow \
  --output-dir ./out
```

Website:

- local site files: [`site/`](/home/michael/Work/nifi-flow-upgrade-advisor/site)
- GitHub Pages workflow: [`.github/workflows/pages.yaml`](/home/michael/Work/nifi-flow-upgrade-advisor/.github/workflows/pages.yaml)

Repo ownership:

- [`CODEOWNERS`](/home/michael/Work/nifi-flow-upgrade-advisor/.github/CODEOWNERS)
- [`AGENTS.md`](/home/michael/Work/nifi-flow-upgrade-advisor/AGENTS.md)
- design docs: [`docs/design.md`](/home/michael/Work/nifi-flow-upgrade-advisor/docs/design.md), [`docs/cli.md`](/home/michael/Work/nifi-flow-upgrade-advisor/docs/cli.md), [`docs/rule-pack-format.md`](/home/michael/Work/nifi-flow-upgrade-advisor/docs/rule-pack-format.md)

## Notes

- sensitive values that NiFi never exported cannot be reconstructed by this tool
- use Parameter Contexts, Parameter Providers, and environment-local secret sources for secret rehydration
- rule packs should cite official Apache migration notes and release caveats

## Relationship To NiFi-Fabric

NiFi-Fabric should consume this repo as external tooling. It can carry a thin pointer under `tools/` and reference pinned versions, but it should not own this engine directly.
