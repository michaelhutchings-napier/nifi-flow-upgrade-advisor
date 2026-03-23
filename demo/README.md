# Demo

This directory contains runnable examples for `nifi-flow-upgrade-advisor`.

## Demo Catalog

- `./demo/orders-platform-1.27-to-2.0.sh`: featured mixed-result story with blocked, manual-change, auto-fix, and info findings in one flow
- `./demo/orders-platform-2.7-to-2.8.sh`: featured mixed-result story with blocked removals, a safe property rewrite, and manual review items
- `./demo/integration-platform-1.22-to-1.23.sh`: featured policy-review story with broader affected-component coverage
- `./demo/base64-1.27-to-2.0.sh`: safe auto-fix replacing `Base64EncodeContent` with `EncodeContent`
- `./demo/get-http-1.27-to-2.0.sh`: manual-change path for `GetHTTP -> InvokeHTTP`
- `./demo/asana-2.7-to-2.8.sh`: blocked upgrade for removed Asana components
- `./demo/bridge-upgrade-1.21-to-2.0.sh`: blocked bridge-upgrade requirement before `2.0.x`
- `./demo/h2-dbcp-1.21-to-1.22.sh`: manual-change for H2 JDBC URLs on DBCP/Hikari
- `./demo/jndi-jms-ldap-1.21-to-1.22.sh`: manual-change for LDAP Provider URLs on JNDI JMS
- `./demo/invoke-http-url-encoding-1.23-to-1.24.sh`: manual-change for URL encoding review
- `./demo/listen-http-2.3-to-2.4.sh`: manual-change for removed ListenHTTP rate limiting
- `./demo/listen-syslog-2.6-to-2.7.sh`: safe auto-fix for `Port -> TCP Port`
- `./demo/jolt-custom-class-2.7-to-2.8.sh`: manual-inspection for Jolt recompilation
- `./demo/content-viewer-2.4-to-2.5.sh`: info-only example for content-viewer contract changes
- `./demo/reference-remote-resources-1.22-to-1.23.sh`: policy-review example for new restricted-resource access

Run all demos:

```bash
./demo/all.sh
```

## Featured Customer Stories

These are the best starting points if you want something closer to a real migration review than a single-issue fixture.

### Orders Platform 1.27 to 2.0

```bash
./demo/orders-platform-1.27-to-2.0.sh
```

This flow combines:

- blocked `VARIABLE_REGISTRY` usage
- safe `DistributedMapCacheClientService -> MapCacheClientService`
- safe `Base64EncodeContent -> EncodeContent`
- manual `GetHTTP -> InvokeHTTP`
- manual `InvokeHTTP` proxy-service migration

Observed summary:

- total findings: `8`
- auto-fix: `2`
- manual-change: `2`
- blocked: `1`
- info: `3`
- rewrite operations applied: `2`

### Orders Platform 2.7 to 2.8

```bash
./demo/orders-platform-2.7-to-2.8.sh
```

This flow combines:

- blocked Asana removals
- manual `StandardRestrictedSSLContextService` review
- manual-inspection for custom Jolt recompilation
- safe `ListenSyslog Port -> TCP Port`

Observed summary:

- total findings: `6`
- auto-fix: `1`
- manual-change: `1`
- manual-inspection: `1`
- blocked: `2`
- rewrite operations applied: `1`

### Integration Platform 1.22 to 1.23

```bash
./demo/integration-platform-1.22-to-1.23.sh
```

This flow shows a broader policy-review scenario:

- root-level `Reference Remote Resources` guidance
- several affected components in one flow
- a forward-looking RethinkDB warning before the later `2.0` step

Observed summary:

- total findings: `7`
- manual-change: `1`
- manual-inspection: `4`
- info: `2`

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

## GetHTTP Manual-Change Demo

This demo models a NiFi `1.27.0` flow containing:

- `org.apache.nifi.processors.standard.GetHTTP`

The official `1.27 -> 2.0` rule pack treats this as a documented migration that still needs a human decision. Apache maps it to `InvokeHTTP`, but the tool does not silently replace it because timeout, SSL, and response-handling choices still matter.

Run it:

```bash
./demo/get-http-1.27-to-2.0.sh
```

Expected result:

- `analyze` shows a `manual-change` finding
- `rewrite` applies `0` operations
- the rewritten artifact exists, but the tool does not guess through the processor replacement

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

For the GetHTTP manual-change demo, the middle path should be clear:

- `manual-change` is non-zero during `analyze`
- `rewrite` produces a reviewed copy but applies zero operations
- the report stays explicit about what a human still needs to change
