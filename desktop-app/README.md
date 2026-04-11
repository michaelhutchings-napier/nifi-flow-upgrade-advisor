# Desktop App

This directory contains a thin Tauri desktop wrapper around the existing
`nifi-flow-upgrade` CLI.

The desktop app is intentionally not a second migration engine. Its job is to:

- scan a selected workspace or repository
- suggest likely source flows, built-in upgrade coverage, and optional target manifests
- run `analyze`, `rewrite`, `validate`, and `run`
- render the generated reports in a friendlier desktop workflow

The CLI remains the source of truth for migration logic.

The current desktop workflow also adds a few presentation-only behaviors on top of the CLI reports:

- bundled demo manifests are labeled `(sample)` and are not auto-selected
- review-only results are framed as advisory guidance instead of blocked upgrades
- repeated review findings can be grouped in the desktop summary while exports keep all occurrences
- controller-service review items can show active-versus-unreferenced usage insight for JSON-based flows

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

Desktop prerequisites:

- Ubuntu or Debian:

```bash
sudo apt update
sudo apt install libwebkit2gtk-4.1-dev \
  build-essential \
  curl \
  wget \
  file \
  libxdo-dev \
  libssl-dev \
  libayatana-appindicator3-dev \
  librsvg2-dev
curl --proto '=https' --tlsv1.2 https://sh.rustup.rs -sSf | sh
source "$HOME/.cargo/env"
```

- macOS:

```bash
xcode-select --install
curl --proto '=https' --tlsv1.2 https://sh.rustup.rs -sSf | sh
source "$HOME/.cargo/env"
```

- Windows PowerShell:

```powershell
winget install --id Microsoft.VisualStudio.2022.Community --source winget --force --override "--add Microsoft.VisualStudio.Component.VC.Tools.x86.x64 --add Microsoft.VisualStudio.Component.VC.Tools.ARM64 --add Microsoft.VisualStudio.Component.Windows11SDK.22621 --addProductLang En-us"
winget install --id Rustlang.Rustup
rustup default stable-msvc
```

On Windows, Tauri also needs Microsoft Edge WebView2. Windows 10 version 1803 and later usually already include it. If not, install the Evergreen Bootstrapper from the official WebView2 download page.

Then the app should run from:

```bash
cargo run --manifest-path desktop-app/src-tauri/Cargo.toml
```

Tagged releases can also publish a Windows desktop archive containing
`nifi-flow-upgrade-advisor-desktop.exe` when the release workflow runs on GitHub
Actions.

The frontend is plain static HTML, CSS, and JavaScript under `ui/`, so the app does not need a separate Node or Vite frontend just to get started.

## Linux Notes

On Linux the desktop wrapper defaults to `LIBGL_ALWAYS_SOFTWARE=1` unless you
set that variable yourself first. This avoids noisy WebKitGTK/EGL startup
warnings in headless CI and lower-GPU environments while keeping the app
launchable. If you want to force normal GPU rendering instead, launch it with:

```bash
LIBGL_ALWAYS_SOFTWARE=0 cargo run --manifest-path desktop-app/src-tauri/Cargo.toml
```
