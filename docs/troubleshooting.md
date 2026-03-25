# Troubleshooting

## Unsupported Version Pair

Symptom:

- `analyze` exits with `no loaded rule pack supports source ... and target ...`

What it means:

- the loaded rule-pack set does not cover that exact source and target version path

What to do:

- load the correct official pack for that path
- include any patch-specific packs if the Apache notes call out patch-only caveats
- use `--allow-unsupported-version-pair` only when you want a report entry instead of an immediate CLI error

## Blocked Findings

Symptom:

- `analyze` or `validate` exits with code `2`

What it means:

- the selected path is not safe to continue as-is

Typical causes:

- removed processors or controller services
- target extensions not present in the selected target inventory
- required bridge upgrade not performed yet

What to do:

- read the Markdown report first
- resolve the blocked findings
- rerun `analyze`, then `rewrite`, then `validate`

## Manual-Change Findings And Zero Rewrite Operations

Symptom:

- `analyze` reports `manual-change > 0`
- `rewrite` reports `appliedOperations: 0`

What it means:

- the tool found documented migration work, but not a mechanically safe conversion

This is intentional:

- `rewrite` only executes `auto-fix` actions
- `manual-change` findings stay visible for a person to handle

Use a manual-change demo for a concrete example:

```bash
./demo/h2-dbcp-1.21-to-1.22.sh
```

## Assisted-Rewrite Findings

Symptom:

- `analyze` reports `assisted-rewrite > 0`
- `rewrite` applies scaffold operations into a separate output artifact

What it means:

- the tool knows enough to scaffold the target shape
- but a human still needs to review the result before import

This is intentional:

- `assisted-rewrite` is the middle tier between `auto-fix` and `manual-change`
- the rewritten flow is reviewable output, not a claim that the migration is fully finished

Use the assisted rewrite demo for a concrete example:

```bash
./demo/get-http-1.27-to-2.0.sh
```

## Missing Secrets After Import

Symptom:

- the upgraded flow imports, but sensitive values are blank or need re-entry

What it means:

- NiFi intentionally does not export secrets in a form this tool can recreate

What to do:

- preserve Parameter Context references where possible
- rehydrate sensitive values from your target environment
- prefer Parameter Providers, Vault, Kubernetes Secrets, or another environment-local secret source

## Target Validation Fails Against A Manifest

Symptom:

- `validate` reports `system.target-extension-unavailable`

What it means:

- the selected target manifest or live target inventory does not contain the component type required by the source flow or its planned replacement

What to do:

- verify that the target NiFi line includes the expected NARs
- use a fuller extensions manifest
- if the component was removed upstream, treat the finding as a real migration blocker instead of a packaging problem

## Live Target API Validation Fails

Symptom:

- `validate` cannot reach `--target-api-url`
- version or process-group readiness checks fail

What to do:

- verify the URL points at the NiFi API endpoint
- verify the bearer token and TLS settings
- confirm the target process group exists and is the right destination for import or update

## Legacy XML Inputs Analyze But Do Not Rewrite

Symptom:

- `analyze` works for `flow.xml.gz`
- `rewrite` refuses the same artifact

What it means:

- raw legacy XML is currently supported for analysis and validation only

What to do:

- export a JSON-based artifact if you need rewrite support
- or use the analyzer report as a migration checklist before import into a newer JSON-based flow workflow
