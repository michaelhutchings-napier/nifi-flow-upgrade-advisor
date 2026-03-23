#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/demo/_lib.sh"

FIXTURE_JSON="${ROOT_DIR}/demo/fixtures/reference-remote-resources-1.22-flow.json"
RULE_PACK="${ROOT_DIR}/examples/rulepacks/nifi-1.22-to-1.23.official.yaml"
OUT_DIR="${ROOT_DIR}/demo/out/reference-remote-resources-1.22-to-1.23"
SOURCE_GZ="${OUT_DIR}/source-flow.json.gz"

rm -rf "${OUT_DIR}"
ensure_demo_binary
gzip_fixture "${FIXTURE_JSON}" "${SOURCE_GZ}"

echo "Running Reference Remote Resources 1.22.0 -> 1.23.0 policy demo"
if run_demo_analyze "${SOURCE_GZ}" "1.22.0" "1.23.0" "${RULE_PACK}" "${OUT_DIR}"; then
  ANALYZE_EXIT=0
else
  ANALYZE_EXIT=$?
fi
echo
echo "Analyze exit code: ${ANALYZE_EXIT}"
echo "Expected outcome:"
echo "  - analyze exits 0"
echo "  - report carries both root-level policy guidance and component-level review findings"
print_demo_footer "${OUT_DIR}" \
  "${OUT_DIR}/migration-report.json" \
  "${OUT_DIR}/migration-report.md"
