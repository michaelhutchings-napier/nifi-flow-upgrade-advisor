#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

examples=(
  "base64-1.27-to-2.0.sh"
  "get-http-1.27-to-2.0.sh"
  "asana-2.7-to-2.8.sh"
  "orders-platform-1.27-to-2.0.sh"
  "orders-platform-2.7-to-2.8.sh"
  "bridge-upgrade-1.21-to-2.0.sh"
  "h2-dbcp-1.21-to-1.22.sh"
  "jndi-jms-ldap-1.21-to-1.22.sh"
  "invoke-http-url-encoding-1.23-to-1.24.sh"
  "integration-platform-1.22-to-1.23.sh"
  "listen-http-2.3-to-2.4.sh"
  "listen-syslog-2.6-to-2.7.sh"
  "jolt-custom-class-2.7-to-2.8.sh"
  "content-viewer-2.4-to-2.5.sh"
  "reference-remote-resources-1.22-to-1.23.sh"
)

for example in "${examples[@]}"; do
  echo
  echo "=== ${example} ==="
  "${ROOT_DIR}/demo/${example}"
done
