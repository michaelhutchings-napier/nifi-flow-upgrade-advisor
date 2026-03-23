#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT_DIR}/demo/_lib.sh"

FIXTURE_JSON="${ROOT_DIR}/demo/fixtures/asana-2.7-flow.json"
RULE_PACK="${ROOT_DIR}/examples/rulepacks/nifi-2.7-to-2.8.official.yaml"
OUT_DIR="${ROOT_DIR}/demo/out/asana-2.7-to-2.8"
SOURCE_GZ="${OUT_DIR}/source-flow.json.gz"
TARGET_MANIFEST="${1:-}"

rm -rf "${OUT_DIR}"
ensure_demo_binary
gzip_fixture "${FIXTURE_JSON}" "${SOURCE_GZ}"

echo "Running Asana 2.7.1 -> 2.8.0 analyze demo"
if run_demo_analyze "${SOURCE_GZ}" "2.7.1" "2.8.0" "${RULE_PACK}" "${OUT_DIR}" "blocked"; then
  ANALYZE_EXIT=0
else
  ANALYZE_EXIT=$?
fi
echo
echo "Analyze exit code: ${ANALYZE_EXIT}"

if [[ -n "${TARGET_MANIFEST}" ]]; then
  echo
  echo "Running validate against target manifest ${TARGET_MANIFEST}"
  set +e
  "${DEMO_BINARY}" validate \
    --input "${SOURCE_GZ}" \
    --input-format flow-json-gz \
    --target-version 2.8.0 \
    --extensions-manifest "${TARGET_MANIFEST}" \
    --output-dir "${OUT_DIR}"
  VALIDATE_EXIT=$?
  set -e
  echo
  echo "Validate exit code: ${VALIDATE_EXIT}"
  if [[ "${VALIDATE_EXIT}" != "0" && "${VALIDATE_EXIT}" != "2" ]]; then
    echo "validate failed unexpectedly" >&2
    exit "${VALIDATE_EXIT}"
  fi
fi

echo "Expected outcome:"
echo "  - analyze exits 2"
echo "  - the report blocks the upgrade for removed Asana components"
if [[ -n "${TARGET_MANIFEST}" ]]; then
  echo "  - validate exits 2 when the target runtime does not include those components"
fi
echo
echo "Reports:"
echo "  ${OUT_DIR}/migration-report.json"
echo "  ${OUT_DIR}/migration-report.md"
if [[ -n "${TARGET_MANIFEST}" ]]; then
  echo "  ${OUT_DIR}/validation-report.json"
  echo "  ${OUT_DIR}/validation-report.md"
fi
echo
echo "Quick checks:"
echo "  jq '.summary' ${OUT_DIR}/migration-report.json"
if [[ -n "${TARGET_MANIFEST}" ]]; then
  echo "  jq '.summary' ${OUT_DIR}/validation-report.json"
fi
