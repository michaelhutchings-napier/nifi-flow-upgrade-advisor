#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/demo/_lib.sh"

FIXTURE_JSON="${ROOT_DIR}/demo/fixtures/listen-http-2.3-flow.json"
RULE_PACK="${ROOT_DIR}/examples/rulepacks/nifi-2.3-to-2.4.official.yaml"
OUT_DIR="${ROOT_DIR}/demo/out/listen-http-2.3-to-2.4"
SOURCE_GZ="${OUT_DIR}/source-flow.json.gz"

rm -rf "${OUT_DIR}"
ensure_demo_binary
gzip_fixture "${FIXTURE_JSON}" "${SOURCE_GZ}"

echo "Running ListenHTTP 2.3.0 -> 2.4.0 manual-change demo"
if run_demo_analyze "${SOURCE_GZ}" "2.3.0" "2.4.0" "${RULE_PACK}" "${OUT_DIR}"; then
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
echo "  - removed rate-limit property is flagged for review"
echo "  - rewrite applies 0 operations because the rule remains manual-change"
print_demo_footer "${OUT_DIR}" \
  "${OUT_DIR}/migration-report.json" \
  "${OUT_DIR}/migration-report.md" \
  "${OUT_DIR}/rewrite-report.json" \
  "${OUT_DIR}/rewrite-report.md"
