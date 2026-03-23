#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/demo/_lib.sh"

FIXTURE_JSON="${ROOT_DIR}/demo/fixtures/bridge-upgrade-1.21-flow.json"
RULE_PACK="${ROOT_DIR}/examples/rulepacks/nifi-1.0-to-1.26-pre-2.0.blocked.yaml"
OUT_DIR="${ROOT_DIR}/demo/out/bridge-upgrade-1.21-to-2.0"
SOURCE_GZ="${OUT_DIR}/source-flow.json.gz"

rm -rf "${OUT_DIR}"
ensure_demo_binary
gzip_fixture "${FIXTURE_JSON}" "${SOURCE_GZ}"

echo "Running bridge-upgrade 1.21.0 -> 2.0.0 blocked demo"
if run_demo_analyze "${SOURCE_GZ}" "1.21.0" "2.0.0" "${RULE_PACK}" "${OUT_DIR}" "blocked"; then
  ANALYZE_EXIT=0
else
  ANALYZE_EXIT=$?
fi
echo
echo "Analyze exit code: ${ANALYZE_EXIT}"
echo "Expected outcome:"
echo "  - analyze exits 2"
echo "  - report explains that 1.27.x is required before 2.0.x"
print_demo_footer "${OUT_DIR}" \
  "${OUT_DIR}/migration-report.json" \
  "${OUT_DIR}/migration-report.md"
