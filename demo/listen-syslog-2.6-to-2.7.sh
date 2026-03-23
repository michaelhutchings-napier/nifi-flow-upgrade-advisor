#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/demo/_lib.sh"

FIXTURE_JSON="${ROOT_DIR}/demo/fixtures/listen-syslog-2.6-flow.json"
RULE_PACK="${ROOT_DIR}/examples/rulepacks/nifi-2.6-to-2.7.official.yaml"
OUT_DIR="${ROOT_DIR}/demo/out/listen-syslog-2.6-to-2.7"
SOURCE_GZ="${OUT_DIR}/source-flow.json.gz"

rm -rf "${OUT_DIR}"
ensure_demo_binary
gzip_fixture "${FIXTURE_JSON}" "${SOURCE_GZ}"

echo "Running ListenSyslog 2.6.0 -> 2.7.0 auto-fix demo"
if run_demo_analyze "${SOURCE_GZ}" "2.6.0" "2.7.0" "${RULE_PACK}" "${OUT_DIR}"; then
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
echo "  - ListenSyslog Port -> TCP Port is flagged as auto-fix"
echo "  - rewrite applies 1 operation"
print_demo_footer "${OUT_DIR}" \
  "${OUT_DIR}/migration-report.json" \
  "${OUT_DIR}/migration-report.md" \
  "${OUT_DIR}/rewrite-report.json" \
  "${OUT_DIR}/rewrite-report.md"
echo "  gzip -dc ${OUT_DIR}/rewritten-flow.json.gz | rg 'TCP Port|UDP Port|\"Port\"'"
