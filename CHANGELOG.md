# Changelog

All notable changes to this project should be documented in this file.

## Unreleased

### Added

- documented release process and install verification guidance
- install script for release-based binary installation
- golden regression tests for official migration rule packs
- manual-change demo for `GetHTTP -> InvokeHTTP` on `1.27.x -> 2.0.x`
- troubleshooting guide for upgrade, rewrite, validation, and secret-handling questions
- secrets and parameter-context guidance for production handoffs
- site pages for release process, troubleshooting, examples, and secret handling
- additional official bridge-path rule packs for `1.21 -> 1.22`, `1.22 -> 1.23`, and `1.23 -> 1.24`
- more realistic local fixtures for `1.27.x` and `2.7.x` product testing

### Changed

- rule-pack matching now supports `propertyValueRegex` for precise official migration caveats such as H2 and LDAP URL detection
- release workflow verifies the project before packaging artifacts, generates release notes, creates an SPDX SBOM, and signs the release checksum bundle

## v1.0.0

### Added

- initial CLI with `analyze`, `rewrite`, `validate`, `publish`, `run`, `rule-pack lint`, and `version`
- official rule-pack coverage for `1.27 -> 2.0`, `2.3 -> 2.4`, `2.4 -> 2.5`, `2.6 -> 2.7`, and `2.7 -> 2.8`
- demo flows for blocked and auto-fix outcomes
- GitHub Pages product site and GitHub release workflow
