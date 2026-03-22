# Contributing

Thanks for contributing to `nifi-flow-upgrade-advisor`.

## What to optimize for

- make migration behavior explicit and explainable
- prefer conservative findings over risky automation
- cite official Apache NiFi migration guidance where possible
- keep rewrites mechanically safe and well tested

## Development loop

```bash
go test ./...
go build ./cmd/nifi-flow-upgrade
```

If you change the CLI contract, rule-pack format, or workflow behavior, update:

- [`README.md`](/home/michael/Work/nifi-flow-upgrade-advisor/README.md)
- [`docs/cli.md`](/home/michael/Work/nifi-flow-upgrade-advisor/docs/cli.md)
- [`docs/design.md`](/home/michael/Work/nifi-flow-upgrade-advisor/docs/design.md)
- [`docs/rule-pack-format.md`](/home/michael/Work/nifi-flow-upgrade-advisor/docs/rule-pack-format.md)

## Pull requests

- use pull requests for changes to `main`; direct pushes should be treated as exceptions only
- include tests for executable rewrite or publish behavior
- include fixtures or example rule-pack updates when adding official migration coverage
- call out any inference when Apache documentation does not provide a direct migration rule

## Scope boundaries

- do not add runtime controller logic here
- do not claim secret recovery that NiFi never exported
- do not auto-fix ambiguous processor replacements without clear mechanical mapping
