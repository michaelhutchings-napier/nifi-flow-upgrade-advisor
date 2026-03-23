#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/demo/_lib.sh"

FIXTURE_JSON="${ROOT_DIR}/demo/fixtures/invoke-http-url-encoding-1.23-flow.json"
RULE_PACK="${ROOT_DIR}/examples/rulepacks/nifi-1.23-to-1.24.official.yaml"
OUT_DIR="${ROOT_DIR}/demo/out/invoke-http-url-encoding-1.23-to-1.24"
SOURCE_GZ="${OUT_DIR}/source-flow.json.gz"

rm -rf "${OUT_DIR}"
ensure_demo_binary
gzip_fixture "${FIXTURE_JSON}" "${SOURCE_GZ}"

echo "Running InvokeHTTP URL encoding 1.23.0 -> 1.24.0 manual-change demo"
if run_demo_analyze "${SOURCE_GZ}" "1.23.0" "1.24.0" "${RULE_PACK}" "${OUT_DIR}"; then
  ANALYZE_EXIT=0
else
  ANALYZE_EXIT=$?
fi
echo
echo "Analyze exit code: ${ANALYZE_EXIT}"
run_demo_rewrite "${OUT_DIR}/migration-report.json" "${OUT_DIR}"
echo
echo "Expected outcome:"
echo "  - analyze exits 0"
echo "  - HTTP URL review is flagged as manual-change"
echo "  - rewrite applies 0 operations"
print_demo_footer "${OUT_DIR}" \
  "${OUT_DIR}/migration-report.json" \
  "${OUT_DIR}/migration-report.md" \
  "${OUT_DIR}/rewrite-report.json" \
  "${OUT_DIR}/rewrite-report.md"
