# Demo

This directory contains small, reproducible demos for `nifi-flow-upgrade-advisor`.

## Base64 Auto-Fix Demo

This demo models a NiFi `1.27.0` flow containing:

- `org.apache.nifi.processors.standard.Base64EncodeContent`

The official `1.27 -> 2.0` rule pack treats this as a safe deterministic replacement to `EncodeContent`.

Run it:

```bash
./demo/base64-1.27-to-2.0.sh
```

Expected result:

- `analyze` shows at least one `auto-fix`
- `rewrite` applies the replacement
- the rewritten artifact contains `org.apache.nifi.processors.standard.EncodeContent`

## Asana Removal Demo

The first demo models a NiFi `2.7.1` flow that contains:

- `org.apache.nifi.processors.asana.GetAsanaObject`
- `org.apache.nifi.controller.asana.StandardAsanaClientProviderService`

Those components were deprecated in NiFi `2.7.x` and removed in NiFi `2.8.x`, so this is a good “blocked upgrade” example.

Run it:

```bash
./demo/asana-2.7-to-2.8.sh
```

That runs:

- `analyze` against the official `2.7 -> 2.8` rule pack
- `validate` as well if you pass a target extensions manifest

Example with a live or exported target manifest:

```bash
./demo/asana-2.7-to-2.8.sh /path/to/live-target-extensions.yaml
```

Expected result:

- `analyze` exits with code `2`
- `validate` exits with code `2` when the manifest does not contain the removed Asana extensions

The generated outputs go under:

- `demo/out/asana-2.7-to-2.8/`

Open the Markdown reports with:

```bash
less demo/out/asana-2.7-to-2.8/migration-report.md
less demo/out/asana-2.7-to-2.8/validation-report.md
```

## About `Auto-fix`

The analyzer summary includes an `auto-fix` count.

That count means:

- how many findings matched rules marked `class: auto-fix`
- and therefore may be eligible for deterministic rewrite actions

It does **not** mean the tool already changed the flow during `analyze`.

For this Asana demo, `auto-fix: 0` is the correct outcome because the components were removed, not safely renamed. The tool should block and explain the upgrade instead of pretending it can convert those parts automatically.

For the Base64 demo, you should see the opposite pattern:

- a non-zero `auto-fix` count during `analyze`
- one applied rewrite operation during `rewrite`
