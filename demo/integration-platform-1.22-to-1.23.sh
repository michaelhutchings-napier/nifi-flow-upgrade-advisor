#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/demo/_lib.sh"

FIXTURE_JSON="${ROOT_DIR}/demo/fixtures/integration-platform-1.22-flow.json"
RULE_PACK="${ROOT_DIR}/examples/rulepacks/nifi-1.22-to-1.23.official.yaml"
OUT_DIR="${ROOT_DIR}/demo/out/integration-platform-1.22-to-1.23"
SOURCE_GZ="${OUT_DIR}/source-flow.json.gz"

rm -rf "${OUT_DIR}"
ensure_demo_binary
gzip_fixture "${FIXTURE_JSON}" "${SOURCE_GZ}"

echo "Running Integration Platform 1.22.0 -> 1.23.0 featured story"
if run_demo_analyze "${SOURCE_GZ}" "1.22.0" "1.23.0" "${RULE_PACK}" "${OUT_DIR}" "never"; then
  ANALYZE_EXIT=0
else
  ANALYZE_EXIT=$?
fi
echo
echo "Analyze exit code: ${ANALYZE_EXIT}"
echo "Expected outcome:"
echo "  - analyze exits 0"
echo "  - report combines root-level policy notes, affected-component policy review, and a pre-2.0 RethinkDB warning"
print_demo_footer "${OUT_DIR}" \
  "${OUT_DIR}/migration-report.json" \
  "${OUT_DIR}/migration-report.md"

