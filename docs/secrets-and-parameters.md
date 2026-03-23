# Secrets And Parameter Contexts

## The Important Boundary

`nifi-flow-upgrade-advisor` preserves exported structure, references, and non-sensitive configuration.

It does **not** recreate sensitive values Apache NiFi never exported.

## What Usually Survives

The advisor can preserve:

- processor and controller-service structure
- parameter-context references
- component identifiers and names where present in the source artifact
- non-sensitive property values
- deterministic rewrites that explicit rule packs allow

## What Does Not Magically Come Back

If the source artifact did not include a sensitive value, the advisor cannot restore it later.

Typical examples:

- sensitive Parameter Context values
- passwords
- API tokens
- keystore and truststore passphrases

## Recommended Production Pattern

1. keep the flow artifact versioned
2. keep secret names and Parameter Context structure versioned
3. rehydrate sensitive values in the target environment

Preferred secret sources:

- NiFi Parameter Providers
- Vault
- Kubernetes Secrets
- other environment-local secret systems

## Practical Advice

- treat the advisor as a migration engine, not a secret store
- preserve and review Parameter Context references during rewrite
- document target-environment rehydration alongside publish/import steps
- keep manual-change findings visible instead of forcing everything into auto-fix

