#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/demo/_lib.sh"

ensure_demo_binary

FIXTURE_JSON="${ROOT_DIR}/demo/fixtures/messaging-platform-1.21-flow.json"
RULE_PACK="${ROOT_DIR}/examples/rulepacks/nifi-1.21-to-1.22.official.yaml"
OUT_DIR="${ROOT_DIR}/demo/out/messaging-platform-1.21-to-1.22"
SOURCE_GZ="${OUT_DIR}/source-flow.json.gz"

rm -rf "${OUT_DIR}"
mkdir -p "${OUT_DIR}"
gzip_fixture "${FIXTURE_JSON}" "${SOURCE_GZ}"

echo "Running Messaging Platform 1.21.0 -> 1.22.0 customer story"
if run_demo_analyze "${SOURCE_GZ}" "1.21.0" "1.22.0" "${RULE_PACK}" "${OUT_DIR}" "never"; then
  ANALYZE_STATUS=0
else
  ANALYZE_STATUS=$?
fi

echo
echo "Analyze exit code: ${ANALYZE_STATUS}"
echo "Running rewrite to scaffold the deterministic subset"
run_demo_rewrite "${OUT_DIR}/migration-report.json" "${OUT_DIR}"

echo
echo "Expected outcome:"
echo "  - analyze shows a mix of assisted and manual findings"
echo "  - rewrite removes the deprecated Cassandra Compression Type property"
echo "  - Azure Queue moves to the v12 processor shape while LDAP-backed JMS stays as guided manual review"
echo
print_demo_footer "${OUT_DIR}" \
  "${OUT_DIR}/migration-report.json" \
  "${OUT_DIR}/migration-report.md" \
  "${OUT_DIR}/rewrite-report.json" \
  "${OUT_DIR}/rewrite-report.md"
echo "  gzip -dc ${OUT_DIR}/rewritten-flow.json.gz | rg 'Compression Type|GetAzureQueueStorage|ruby|ldap://'"
