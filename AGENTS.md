# AGENTS.md

## Project intent
This repository owns the standalone Flow Upgrade Advisor CLI.

It is offline tooling, not a runtime control-plane component.

## Core principles
- Prefer explicit source and target version selection.
- Prefer rule-pack driven behavior over hidden heuristics.
- Prefer mechanically safe rewrites only.
- Keep reports explainable and auditable.
- Keep live validation optional and read-only.
- Never pretend to recover secrets that NiFi did not export.

## Working rules
- Update the docs in `docs/` when the CLI contract or rule-pack format changes.
- Keep examples under `examples/` aligned with implemented behavior.
- Use official Apache NiFi migration guidance and docs as the primary source for version caveats.
- Mark inferred guidance clearly when Apache docs do not provide a dedicated caveat.

## Quality bar
- Every executable rewrite action needs tests.
- Every official rule-pack addition should have coverage.
- Every target-validation behavior should fail clearly and conservatively.
