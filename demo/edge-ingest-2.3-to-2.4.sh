#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/demo/_lib.sh"

ensure_demo_binary

FIXTURE_JSON="${ROOT_DIR}/demo/fixtures/edge-ingest-2.3-flow.json"
RULE_PACK="${ROOT_DIR}/examples/rulepacks/nifi-2.3-to-2.4.official.yaml"
OUT_DIR="${ROOT_DIR}/demo/out/edge-ingest-2.3-to-2.4"
SOURCE_GZ="${OUT_DIR}/source-flow.json.gz"

rm -rf "${OUT_DIR}"
mkdir -p "${OUT_DIR}"
gzip_fixture "${FIXTURE_JSON}" "${SOURCE_GZ}"

echo "Running Edge Ingest 2.3.0 -> 2.4.0 customer story"
if run_demo_analyze "${SOURCE_GZ}" "2.3.0" "2.4.0" "${RULE_PACK}" "${OUT_DIR}" "never"; then
  ANALYZE_STATUS=0
else
  ANALYZE_STATUS=$?
fi

echo
echo "Analyze exit code: ${ANALYZE_STATUS}"
echo "Running rewrite to remove the deprecated ListenHTTP rate-limit property"
run_demo_rewrite "${OUT_DIR}/migration-report.json" "${OUT_DIR}"

echo
echo "Expected outcome:"
echo "  - analyze shows an assisted rewrite for ListenHTTP"
echo "  - rewrite removes Max Data to Receive per Second"
echo "  - the flow still needs a human decision if external rate limiting is required"
echo
print_demo_footer "${OUT_DIR}" \
  "${OUT_DIR}/migration-report.json" \
  "${OUT_DIR}/migration-report.md" \
  "${OUT_DIR}/rewrite-report.json" \
  "${OUT_DIR}/rewrite-report.md"
echo "  gzip -dc ${OUT_DIR}/rewritten-flow.json.gz | rg 'Max Data to Receive per Second|ListenHTTP'"
