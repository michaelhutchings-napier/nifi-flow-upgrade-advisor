#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/demo/_lib.sh"

FIXTURE_JSON="${ROOT_DIR}/demo/fixtures/orders-platform-2.7-flow.json"
RULE_PACK="${ROOT_DIR}/examples/rulepacks/nifi-2.7-to-2.8.official.yaml"
OUT_DIR="${ROOT_DIR}/demo/out/orders-platform-2.7-to-2.8"
SOURCE_GZ="${OUT_DIR}/source-flow.json.gz"

rm -rf "${OUT_DIR}"
ensure_demo_binary
gzip_fixture "${FIXTURE_JSON}" "${SOURCE_GZ}"

echo "Running Orders Platform 2.7.1 -> 2.8.0 featured story"
if run_demo_analyze "${SOURCE_GZ}" "2.7.1" "2.8.0" "${RULE_PACK}" "${OUT_DIR}" "blocked"; then
  ANALYZE_EXIT=0
else
  ANALYZE_EXIT=$?
fi
echo
echo "Analyze exit code: ${ANALYZE_EXIT}"
echo "Running rewrite to apply the safe ListenSyslog property migration"
run_demo_rewrite "${OUT_DIR}/migration-report.json" "${OUT_DIR}"
echo
echo "Expected outcome:"
echo "  - analyze exits 2 because Asana was removed in NiFi 2.8.x"
echo "  - report also includes a safe ListenSyslog property rename and a Jolt review finding"
echo "  - rewrite applies the property rename while leaving blocked/manual review items visible"
print_demo_footer "${OUT_DIR}" \
  "${OUT_DIR}/migration-report.json" \
  "${OUT_DIR}/migration-report.md" \
  "${OUT_DIR}/rewrite-report.json" \
  "${OUT_DIR}/rewrite-report.md"
echo "  gzip -dc ${OUT_DIR}/rewritten-flow.json.gz | rg 'TCP Port|StandardAsanaClientProviderService|Custom Transformation Class Name'"

