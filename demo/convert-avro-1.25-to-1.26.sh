#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/demo/_lib.sh"

ensure_demo_binary

FIXTURE_JSON="${ROOT_DIR}/demo/fixtures/convert-avro-1.25-flow.json"
RULE_PACK="${ROOT_DIR}/examples/rulepacks/nifi-1.25-to-1.26.official.yaml"
OUT_DIR="${ROOT_DIR}/demo/out/convert-avro-1.25-to-1.26"
SOURCE_GZ="${OUT_DIR}/source-flow.json.gz"

rm -rf "${OUT_DIR}"
mkdir -p "${OUT_DIR}"
gzip_fixture "${FIXTURE_JSON}" "${SOURCE_GZ}"

echo "Running ConvertAvroToJSON 1.25.0 -> 1.26.0 assisted rewrite demo"
if run_demo_analyze "${SOURCE_GZ}" "1.25.0" "1.26.0" "${RULE_PACK}" "${OUT_DIR}" "never"; then
  ANALYZE_STATUS=0
else
  ANALYZE_STATUS=$?
fi

echo
echo "Analyze exit code: ${ANALYZE_STATUS}"
echo "Running rewrite to scaffold the ConvertRecord replacement"
run_demo_rewrite "${OUT_DIR}/migration-report.json" "${OUT_DIR}"

echo
echo "Expected outcome:"
echo "  - analyze shows an assisted rewrite for ConvertAvroToJSON"
echo "  - rewrite swaps the processor type to ConvertRecord in a separate reviewed artifact"
echo "  - record-reader and record-writer service choices still remain a human review step"
echo
print_demo_footer "${OUT_DIR}" \
  "${OUT_DIR}/migration-report.json" \
  "${OUT_DIR}/migration-report.md" \
  "${OUT_DIR}/rewrite-report.json" \
  "${OUT_DIR}/rewrite-report.md"
echo "  gzip -dc ${OUT_DIR}/rewritten-flow.json.gz | rg 'ConvertRecord|ConvertAvroToJSON'"
