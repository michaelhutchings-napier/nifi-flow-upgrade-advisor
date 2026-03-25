# Desktop Guide

The desktop app is now the easiest way to use `nifi-flow-upgrade-advisor`.

## Prerequisites

Ubuntu or Debian:

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

macOS:

```bash
xcode-select --install
curl --proto '=https' --tlsv1.2 https://sh.rustup.rs -sSf | sh
source "$HOME/.cargo/env"
```

Windows PowerShell:

```powershell
winget install --id Microsoft.VisualStudio.2022.Community --source winget --force --override "--add Microsoft.VisualStudio.Component.VC.Tools.x86.x64 --add Microsoft.VisualStudio.Component.VC.Tools.ARM64 --add Microsoft.VisualStudio.Component.Windows11SDK.22621 --addProductLang En-us"
winget install --id Rustlang.Rustup
rustup default stable-msvc
```

On Windows, Tauri also needs Microsoft Edge WebView2. Windows 10 version 1803 and later usually already include it. If not, install the Evergreen Bootstrapper from the official WebView2 download page.

## Quick flow

1. Launch the desktop app.
2. Scan your workspace or repository.
3. Pick a flow artifact.
4. Confirm or enter the source NiFi version.
5. Choose the target NiFi version.
6. Click `Analyze`.
7. If safe fixes are available, click `Rewrite`.
8. Click `Validate` when you want to check the rewritten result against target readiness.
9. Use `Run` when you want the guided end-to-end sequence.

## What each action means

- `Analyze`: shows blockers, review items, safe fixes, and info notes
- `Rewrite`: applies only deterministic safe changes and writes a separate upgraded artifact
- `Validate`: checks whether the current artifact is ready for the target runtime
- `Run`: executes analyze, rewrite, and validate in sequence

## Safety

- The original selected source flow is not overwritten by default.
- Rewrites are written to a separate `rewritten-flow...` artifact in the chosen output directory.
- If there are no safe fixes, rewrite is effectively a no-op and the app will tell you that before you click it.

## When to use the CLI

Use the CLI for:

- CI gates
- scripted workflows
- GitOps automation
- advanced debugging or explicit rule-pack overrides

The desktop app and CLI use the same engine and reports.
