# Custom Processors And Services

## Purpose

This guide is for teams that own private NiFi processors, controller services, or reporting tasks.

The advisor already supports private rule packs. The goal of this page is to show how to teach the
tool about your own components without pretending it can guess through internal migrations on its own.

## Default Product Stance

By default, custom components should be treated conservatively:

- analyze them
- emit findings
- avoid auto-fixing unless your team controls the migration contract

That keeps the product honest. Private components often depend on custom NARs, internal APIs,
different property contracts, or environment-specific deployment assumptions.

## Recommended Migration Tiers

Use these tiers for private rules:

- `auto-fix`
  Use only when the migration is mechanically safe and your team can prove the mapping.

- `assisted-rewrite`
  Use when the tool can scaffold the target shape, but a person still needs to review the result.

- `manual-change`
  Use when the migration depends on architecture, secrets, external libraries, or behavior changes.

## Example: Safe Internal Rename

```yaml
apiVersion: flow-upgrade.nifi.advisor/v1alpha1
kind: RulePack
metadata:
  name: acme-2.4-to-2.5-private
spec:
  sourceVersionRange: ">=2.4.0 <2.5.0"
  targetVersionRange: ">=2.5.0 <2.6.0"
  appliesToFormats:
    - flow-json-gz
  rules:
    - id: acme.customer-cache.rename
      category: component-replaced
      class: auto-fix
      severity: warning
      message: CustomerCacheService moved to the v2 package in 2.5.x.
      selector:
        scope: controller-service
        componentType: com.acme.nifi.CustomerCacheService
      actions:
        - type: replace-component-type
          from: com.acme.nifi.CustomerCacheService
          to: com.acme.nifi.v2.CustomerCacheService
```

## Example: Assisted Internal Migration

This is the useful middle tier.

```yaml
apiVersion: flow-upgrade.nifi.advisor/v1alpha1
kind: RulePack
metadata:
  name: acme-1.9-to-2.0-private
spec:
  sourceVersionRange: ">=1.9.0 <2.0.0"
  targetVersionRange: ">=2.0.0 <2.1.0"
  appliesToFormats:
    - flow-json-gz
  rules:
    - id: acme.customer-api.fetcher.assisted
      category: component-replaced
      class: assisted-rewrite
      severity: warning
      message: CustomerApiFetcher should move to HttpCustomerFetcher in 2.0.x.
      selector:
        scope: processor
        componentType: com.acme.nifi.CustomerApiFetcher
      match:
        propertyExists: Legacy URL
      actions:
        - type: replace-component-type
          from: com.acme.nifi.CustomerApiFetcher
          to: com.acme.nifi.HttpCustomerFetcher
        - type: copy-property
          from: Legacy URL
          to: Remote URL
        - type: set-property-if-absent
          property: HTTP Method
          value: GET
      notes: This scaffolds the new processor shape, but auth headers and timeout behavior still need human review.
```

## Example: Manual Internal Migration

```yaml
apiVersion: flow-upgrade.nifi.advisor/v1alpha1
kind: RulePack
metadata:
  name: acme-2.0-to-2.1-private
spec:
  sourceVersionRange: ">=2.0.0 <2.1.0"
  targetVersionRange: ">=2.1.0 <2.2.0"
  rules:
    - id: acme.jms.publisher.provider-change
      category: component-replaced
      class: manual-change
      severity: warning
      message: InternalJmsPublisher now requires the AcmeConnectionFactoryProvider service.
      selector:
        scope: processor
        componentType: com.acme.nifi.InternalJmsPublisher
      notes: This migration requires a new controller service, environment-specific credentials, and updated client libraries.
```

## Practical Advice

- keep private packs in your own repo
- pin them in CI
- test them with `rule-pack lint`
- prefer `assisted-rewrite` before `auto-fix` when you are still learning the migration
- promote a rule to `auto-fix` only after repeated real-flow validation

## Good Boundaries

Good candidates for private `auto-fix`:

- package/type renames
- property renames
- bundle-coordinate updates
- direct value replacements with no behavior change

Good candidates for `assisted-rewrite`:

- new target properties can be scaffolded
- target component type is known
- old values can be copied into the new shape
- a human still needs to review behavior

Keep as `manual-change`:

- custom NAR rebuilds
- changed authentication models
- new external client libraries
- migrations that depend on deployment architecture
