# Flow Upgrade Advisor Rule-Pack File Format

## Status

This document defines the `v1alpha1` rule-pack file format for the Flow Upgrade Advisor.

The format is designed so the tool can analyze flows cleanly now and execute a narrow deterministic or assisted rewrite subset safely.

## Goals

- explicit source and target version coverage
- human-reviewable YAML
- stable rule identifiers
- deterministic matching
- future-safe transform declarations

## File Format

Rule packs use YAML with this top-level shape:

```yaml
apiVersion: flow-upgrade.nifi.advisor/v1alpha1
kind: RulePack
metadata:
  name: nifi-1.27-to-2.0-core
  title: NiFi 1.27 to 2.0 core migration rules
  description: Rules for common upstream component and property changes.
spec:
  sourceVersionRange: ">=1.27.0 <2.0.0"
  targetVersionRange: ">=2.0.0 <2.1.0"
  appliesToFormats:
    - flow-json-gz
    - versioned-flow-snapshot
  rules:
    - id: core.example-rule
      category: property-renamed
      class: assisted-rewrite
      severity: warning
      message: Example message.
      selector:
        componentType: org.apache.nifi.example.Processor
      match:
        propertyExists: Old Property
      actions:
        - type: rename-property
          from: Old Property
          to: New Property
      references:
        - https://cwiki.apache.org/confluence/display/NIFI/Migration%2BGuidance
```

## Top-Level Fields

### `apiVersion`

Required.

Phase 1 value:

```text
flow-upgrade.nifi.advisor/v1alpha1
```

### `kind`

Required.

Phase 1 value:

```text
RulePack
```

### `metadata`

Required fields:

- `name`

Optional fields:

- `title`
- `description`
- `owners`
- `references`

`metadata.name` must be unique within the loaded rule-pack set.

### `spec.sourceVersionRange`

Required.

Semantic version range describing the supported source line.

### `spec.targetVersionRange`

Required.

Semantic version range describing the supported target line.

### `spec.appliesToFormats`

Optional.

If omitted, the pack applies to all source formats supported by the CLI.

Allowed values:

- `flow-xml-gz`
- `flow-json-gz`
- `versioned-flow-snapshot`
- `git-registry-dir`
- `nifi-registry-export`

### `spec.rules`

Required list. It may be empty for placeholder or coverage-only packs that exist to make a multi-hop path explicit without adding findings of their own.

## Rule Fields

### `id`

Required.

Must be stable across revisions of the same rule.

Recommended convention:

```text
<pack-scope>.<short-rule-name>
```

Examples:

- `core.kafka.bundle-rename`
- `core.invokehttp.ssl-context-review`

### `category`

Required.

Allowed values in `v1alpha1`:

- `component-removed`
- `component-replaced`
- `bundle-renamed`
- `property-renamed`
- `property-value-changed`
- `property-removed`
- `variable-migration`
- `manual-inspection`
- `blocked`

### `class`

Required.

Allowed values:

- `auto-fix`
- `assisted-rewrite`
- `manual-change`
- `manual-inspection`
- `blocked`
- `info`

`assisted-rewrite` sits between `auto-fix` and `manual-change`:

- it is executable during `rewrite`
- it may scaffold target properties or component replacements
- it still requires human review before import

### `severity`

Required.

Allowed values:

- `info`
- `warning`
- `error`

### `message`

Required.

Short human-readable explanation emitted directly into findings.

### `selector`

Optional, but at least one of `selector` or `match` must narrow the rule.

Allowed fields:

- `componentType`
- `componentTypes`
- `bundleGroup`
- `bundleArtifact`
- `propertyName`
- `scope`

`scope` allowed values:

- `processor`
- `controller-service`
- `reporting-task`
- `parameter-context`
- `flow-root`

### `match`

Optional.

All declared match conditions must evaluate true for the rule to match.

Allowed fields in `v1alpha1`:

- `propertyExists`
- `propertyAbsent`
- `propertyValueEquals`
- `propertyValueIn`
- `propertyValueRegex`
- `annotationContains`
- `componentNameMatches`

`propertyExists` is still supported, but `rule-pack lint` warns on it because exported NiFi JSON can preserve a property key with a `null` value. Prefer a value-based matcher such as `propertyValueRegex`, `propertyValueEquals`, or `propertyValueIn` when you need proof of a real configured value.

Example:

```yaml
match:
  propertyValueRegex:
    property: SSL Context Service
    regex: '.+'
```

Example:

```yaml
match:
  propertyValueIn:
    property: Scheduling Strategy
    values:
      - CRON_DRIVEN
      - TIMER_DRIVEN
```

Example:

```yaml
match:
  propertyValueRegex:
    property: Database Connection URL
    regex: '^jdbc:h2:'
```

### `actions`

Optional.

`rewrite` now executes a narrow deterministic subset of actions for JSON-based flow artifacts.

`analyze` still surfaces the same actions as suggested next steps in findings.

Allowed action types in `v1alpha1`:

- `rename-property`
- `set-property`
- `set-property-if-absent`
- `copy-property`
- `remove-property`
- `replace-property-value`
- `update-bundle-coordinate`
- `replace-component-type`

The common split is:

- `auto-fix`: actions that should produce a mechanically finished rewrite
- `assisted-rewrite`: actions that scaffold the target shape while keeping the final decision visible to a human
- `replace-component-type`
- `emit-parameter-scaffold`
- `mark-blocked`

Each action type has its own required fields.

Recommended authoring rule:

- back each version-pair rule pack with the relevant official Apache NiFi migration notes and release caveats
- include those URLs in `metadata.references` and `rules[].references`
- use `notes` for the operator-facing caveat text that should appear directly in reports

Required fields by action type:

- `rename-property`: `from`, `to`
- `set-property`: `property`, `value`
- `remove-property`: `name`
- `replace-property-value`: `property`, `from`, `to`
- `update-bundle-coordinate`: `group`, `artifact`
- `replace-component-type`: `from`, `to`
- `emit-parameter-scaffold`: `parameterName`, `sensitive`
- `mark-blocked`: no additional fields

Example:

```yaml
actions:
  - type: rename-property
    from: Old Property
    to: New Property
```

Example:

```yaml
actions:
  - type: update-bundle-coordinate
    group: org.apache.nifi
    artifact: nifi-kafka-2-processors
```

### `references`

Optional list of URLs or document identifiers.

The tool should preserve them into generated findings.

### `notes`

Optional free-text operator guidance.

## Matching Rules

Rule evaluation order in Phase 1:

1. pack version-range filter
2. pack format filter
3. per-rule selector filter
4. per-rule match evaluation

All matched rules should emit findings. Phase 1 must not stop after the first match.

## Duplicate and Conflict Rules

### Duplicate IDs

Two loaded rule packs must not define the same `id`.

That is a lint error.

### Duplicate Pack Names

Two loaded rule packs must not define the same `metadata.name`.

That is also a lint error.

### Conflicting Actions

Phase 1 should allow multiple matching rules to suggest different actions because Phase 1 does not execute rewrites.

Future rewrite phases may tighten this.

## Lint Requirements

`rule-pack lint` should reject:

- invalid `apiVersion`
- invalid `kind`
- missing required fields
- empty `spec.rules`
- duplicate rule IDs across loaded packs
- duplicate `metadata.name` values across loaded packs
- unknown `category`, `class`, `severity`, or action `type`
- malformed version ranges
- empty `message`
- impossible selectors such as both empty `componentTypes` and empty `scope` when no `match` is present

`rule-pack lint` may also warn on valid-but-risky patterns such as `match.propertyExists`. Use `--fail-on-warn` in CI if you want those warnings to fail the build.

## Forward-Compatibility Rules

Phase 1 parsers should:

- reject unknown top-level fields in strict lint mode
- ignore unknown optional fields during analysis only if `--strict` is not set

This keeps the format evolvable without making analysis behavior magical.
