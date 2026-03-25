#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/demo/_lib.sh"

FIXTURE_JSON="${ROOT_DIR}/demo/fixtures/content-viewer-2.4-flow.json"
RULE_PACK="${ROOT_DIR}/examples/rulepacks/nifi-2.4-to-2.5.official.yaml"
OUT_DIR="${ROOT_DIR}/demo/out/content-viewer-2.4-to-2.5"
SOURCE_GZ="${OUT_DIR}/source-flow.json.gz"

rm -rf "${OUT_DIR}"
ensure_demo_binary
gzip_fixture "${FIXTURE_JSON}" "${SOURCE_GZ}"

echo "Running content-viewer 2.4.0 -> 2.5.0 quiet-path demo"
if run_demo_analyze "${SOURCE_GZ}" "2.4.0" "2.5.0" "${RULE_PACK}" "${OUT_DIR}"; then
  ANALYZE_EXIT=0
else
  ANALYZE_EXIT=$?
fi
echo
echo "Analyze exit code: ${ANALYZE_EXIT}"
echo "Expected outcome:"
echo "  - analyze exits 0"
echo "  - no flow-specific findings are produced for this minimal fixture"
echo "  - this is a clean-path example rather than a migration-warning example"
print_demo_footer "${OUT_DIR}" \
  "${OUT_DIR}/migration-report.json" \
  "${OUT_DIR}/migration-report.md"
