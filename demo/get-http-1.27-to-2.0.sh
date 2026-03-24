#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/demo/_lib.sh"
FIXTURE_JSON="${ROOT_DIR}/demo/fixtures/get-http-1.27-flow.json"
RULE_PACK="${ROOT_DIR}/examples/rulepacks/nifi-1.27-to-2.0.official.yaml"
OUT_DIR="${ROOT_DIR}/demo/out/get-http-1.27-to-2.0"
SOURCE_GZ="${OUT_DIR}/source-flow.json.gz"

rm -rf "${OUT_DIR}"
ensure_demo_binary
gzip_fixture "${FIXTURE_JSON}" "${SOURCE_GZ}"

echo "Running GetHTTP 1.27.0 -> 2.0.0 assisted rewrite demo"
if run_demo_analyze "${SOURCE_GZ}" "1.27.0" "2.0.0" "${RULE_PACK}" "${OUT_DIR}" "never"; then
  ANALYZE_EXIT=0
else
  ANALYZE_EXIT=$?
fi

echo
echo "Running rewrite to show assisted scaffolding for the target InvokeHTTP shape"
run_demo_rewrite "${OUT_DIR}/migration-report.json" "${OUT_DIR}"

echo
echo "Expected outcome:"
echo "  - analyze exits 0 with assisted-rewrite findings"
echo "  - rewrite replaces GetHTTP with InvokeHTTP and scaffolds key target properties"
print_demo_footer "${OUT_DIR}" \
  "${OUT_DIR}/migration-report.json" \
  "${OUT_DIR}/migration-report.md" \
  "${OUT_DIR}/rewrite-report.json" \
  "${OUT_DIR}/rewrite-report.md" \
  "${OUT_DIR}/rewritten-flow.json.gz"
echo "  gzip -dc ${OUT_DIR}/rewritten-flow.json.gz | rg 'InvokeHTTP|HTTP Method|Response FlowFile Naming Strategy|HTTP URL'"
echo "Analyze exit code: ${ANALYZE_EXIT}"
