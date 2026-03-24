# Desktop App

This directory contains a thin Tauri desktop wrapper around the existing
`nifi-flow-upgrade` CLI.

The desktop app is intentionally not a second migration engine. Its job is to:

- scan a selected workspace or repository
- suggest likely source flows, rule packs, and manifests
- run `analyze`, `rewrite`, `validate`, and `run`
- render the generated reports in a friendlier desktop workflow

The CLI remains the source of truth for migration logic.

## Layout

- `ui/` static frontend assets
- `src-tauri/` Rust/Tauri shell and command bridge

## MVP

The first desktop milestone focuses on:

- workspace auto-detection
- one-click command execution
- in-app stdout/stderr and report viewing
- no custom migration logic beyond what the CLI already supports

## Expected Run Model

On a machine with the normal Tauri desktop prerequisites installed, the app should run from:

```bash
cargo run --manifest-path desktop-app/src-tauri/Cargo.toml
```

The frontend is plain static HTML, CSS, and JavaScript under `ui/`, so the app does not need a separate Node or Vite frontend just to get started.

## Linux Notes

On Linux the desktop wrapper defaults to `LIBGL_ALWAYS_SOFTWARE=1` unless you
set that variable yourself first. This avoids noisy WebKitGTK/EGL startup
warnings in headless CI and lower-GPU environments while keeping the app
launchable. If you want to force normal GPU rendering instead, launch it with:

```bash
LIBGL_ALWAYS_SOFTWARE=0 cargo run --manifest-path desktop-app/src-tauri/Cargo.toml
```
