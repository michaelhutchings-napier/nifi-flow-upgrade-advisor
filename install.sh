#!/usr/bin/env bash

set -euo pipefail

REPO_OWNER="michaelhutchings-napier"
REPO_NAME="nifi-flow-upgrade-advisor"
REPO_SLUG="${REPO_OWNER}/${REPO_NAME}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
VERSION="${VERSION:-latest}"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "install.sh requires '$1' to be available" >&2
    exit 1
  fi
}

detect_os() {
  case "$(uname -s)" in
    Linux) echo "linux" ;;
    Darwin) echo "darwin" ;;
    MINGW*|MSYS*|CYGWIN*|Windows_NT) echo "windows" ;;
    *)
      echo "unsupported operating system: $(uname -s)" >&2
      exit 1
      ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *)
      echo "unsupported architecture: $(uname -m)" >&2
      exit 1
      ;;
  esac
}

resolve_version() {
  if [[ "${VERSION}" != "latest" ]]; then
    echo "${VERSION}"
    return
  fi

  curl -fsSL "https://api.github.com/repos/${REPO_SLUG}/releases/latest" |
    sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' |
    head -n 1
}

version_candidates() {
  local version="$1"

  echo "${version}"
  if [[ "${version}" == v* ]]; then
    echo "${version#v}"
  else
    echo "v${version}"
  fi
}

verify_checksum() {
  local checksum_file="$1"
  local archive_name="$2"

  if command -v sha256sum >/dev/null 2>&1; then
    (cd "${TMP_DIR}" && sha256sum -c --ignore-missing "${checksum_file}")
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    (
      cd "${TMP_DIR}"
      shasum -a 256 -c "${checksum_file}" 2>/dev/null | grep "${archive_name}: OK"
    )
    return
  fi

  echo "warning: no sha256 tool found; skipping checksum verification" >&2
}

fallback_source_install() {
  local version="$1"

  if ! command -v go >/dev/null 2>&1 || ! command -v git >/dev/null 2>&1; then
    echo "release assets were not found and source fallback requires both 'go' and 'git'" >&2
    exit 1
  fi

  local src_dir="${TMP_DIR}/source"
  mkdir -p "${INSTALL_DIR}"
  echo "Release assets were not found for ${version}; falling back to source install from git"
  local selected_version=""
  while IFS= read -r candidate; do
    if git ls-remote --tags "https://github.com/${REPO_SLUG}.git" "refs/tags/${candidate}" | grep -q .; then
      selected_version="${candidate}"
      break
    fi
  done < <(version_candidates "${version}")

  if [[ -z "${selected_version}" ]]; then
    echo "could not find a matching git tag for ${version}" >&2
    exit 1
  fi

  git clone --depth 1 --branch "${selected_version}" "https://github.com/${REPO_SLUG}.git" "${src_dir}" >/dev/null 2>&1
  (
    cd "${src_dir}"
    GOBIN="${INSTALL_DIR}" go install ./cmd/nifi-flow-upgrade
  )
  echo "Installed to ${INSTALL_DIR}/nifi-flow-upgrade"
}

need_cmd curl
need_cmd tar
need_cmd install

OS="$(detect_os)"
ARCH="$(detect_arch)"
RESOLVED_VERSION="$(resolve_version)"

if [[ -z "${RESOLVED_VERSION}" ]]; then
  echo "failed to resolve release version from GitHub" >&2
  exit 1
fi

ARCHIVE_EXT="tar.gz"
BINARY_NAME="nifi-flow-upgrade-${OS}-${ARCH}"
if [[ "${OS}" == "windows" ]]; then
  ARCHIVE_EXT="zip"
fi

ARCHIVE_NAME="${BINARY_NAME}.${ARCHIVE_EXT}"
CHECKSUMS_NAME="checksums.txt"
RELEASE_BASE="https://github.com/${REPO_SLUG}/releases/download/${RESOLVED_VERSION}"

echo "Installing ${REPO_NAME} ${RESOLVED_VERSION} for ${OS}/${ARCH}"
DOWNLOAD_VERSION=""
while IFS= read -r candidate; do
  if curl -fsSL "https://github.com/${REPO_SLUG}/releases/download/${candidate}/${ARCHIVE_NAME}" -o "${TMP_DIR}/${ARCHIVE_NAME}"; then
    DOWNLOAD_VERSION="${candidate}"
    break
  fi
done < <(version_candidates "${RESOLVED_VERSION}")

if [[ -z "${DOWNLOAD_VERSION}" ]]; then
  fallback_source_install "${RESOLVED_VERSION}"
  exit 0
fi

curl -fsSL "https://github.com/${REPO_SLUG}/releases/download/${DOWNLOAD_VERSION}/${CHECKSUMS_NAME}" -o "${TMP_DIR}/${CHECKSUMS_NAME}"
verify_checksum "${CHECKSUMS_NAME}" "${ARCHIVE_NAME}"

mkdir -p "${TMP_DIR}/unpack"
if [[ "${ARCHIVE_EXT}" == "zip" ]]; then
  need_cmd unzip
  unzip -q "${TMP_DIR}/${ARCHIVE_NAME}" -d "${TMP_DIR}/unpack"
else
  tar -xzf "${TMP_DIR}/${ARCHIVE_NAME}" -C "${TMP_DIR}/unpack"
fi

mkdir -p "${INSTALL_DIR}"
install -m 0755 "${TMP_DIR}/unpack/${BINARY_NAME}" "${INSTALL_DIR}/nifi-flow-upgrade"
echo "Installed to ${INSTALL_DIR}/nifi-flow-upgrade"
