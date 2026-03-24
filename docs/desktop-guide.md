# Desktop Guide

The desktop app is now the easiest way to use `nifi-flow-upgrade-advisor`.

## Quick flow

1. Launch the desktop app.
2. Scan your workspace or repository.
3. Pick a flow artifact.
4. Confirm or enter the source NiFi version.
5. Choose the target NiFi version.
6. Click `Analyze`.
7. If safe fixes are available, click `Rewrite`.
8. Click `Validate` when you want to check the rewritten result against target readiness.
9. Use `Run` when you want the guided end-to-end sequence.

## What each action means

- `Analyze`: shows blockers, review items, safe fixes, and info notes
- `Rewrite`: applies only deterministic safe changes and writes a separate upgraded artifact
- `Validate`: checks whether the current artifact is ready for the target runtime
- `Run`: executes analyze, rewrite, and validate in sequence

## Safety

- The original selected source flow is not overwritten by default.
- Rewrites are written to a separate `rewritten-flow...` artifact in the chosen output directory.
- If there are no safe fixes, rewrite is effectively a no-op and the app will tell you that before you click it.

## When to use the CLI

Use the CLI for:

- CI gates
- scripted workflows
- GitOps automation
- advanced debugging or explicit rule-pack overrides

The desktop app and CLI use the same engine and reports.
