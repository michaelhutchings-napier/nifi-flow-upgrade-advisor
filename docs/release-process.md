# Release Process

This project releases from signed Git tags using the GitHub Actions workflow in [`.github/workflows/release.yaml`](../.github/workflows/release.yaml).

## What A Release Produces

Every tagged release should publish:

- platform archives for Linux, macOS, and Windows
- a `checksums.txt` file with SHA-256 hashes
- a signed checksum bundle
- an SPDX SBOM
- generated GitHub release notes

## Maintainer Checklist

1. Make sure `main` is green:
   - CI passing
   - workflow lint passing
   - docs and demos updated for new behavior
2. Update [CHANGELOG.md](../CHANGELOG.md).
3. Confirm the release version is reflected in any user-facing notes that need it.
4. Create and push the tag:

```bash
git checkout main
git pull --ff-only origin main
git tag v1.0.1
git push origin v1.0.1
```

5. Watch the `release` workflow complete.
6. Open the GitHub release and sanity-check:
   - archive names
   - checksums
   - generated notes
7. Verify one install path from the published assets.

## Local Verification Before Tagging

```bash
go test ./...
go build -o ./bin/nifi-flow-upgrade ./cmd/nifi-flow-upgrade
./demo/base64-1.27-to-2.0.sh
./demo/get-http-1.27-to-2.0.sh
./demo/asana-2.7-to-2.8.sh
```

## Install Guidance For Users

Users should be able to:

- run the install script directly
- or download a platform archive from GitHub Releases
- verify the checksum
- unpack the binary somewhere on `PATH`

If a release predates packaged assets, the install script can fall back to `go install` when Go is available locally.

Example:

```bash
curl -fsSL https://raw.githubusercontent.com/michaelhutchings-napier/nifi-flow-upgrade-advisor/main/install.sh | bash
```

## Notes

- Releases should come from reviewed changes on `main`, not ad-hoc local branches.
- If the release workflow needs to be rerun, rerun the workflow for the pushed tag rather than creating a new tag unless the artifact contents changed.
