#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BINARY="${BINARY:-${ROOT_DIR}/nifi-flow-upgrade}"
FIXTURE_JSON="${ROOT_DIR}/demo/fixtures/asana-2.7-flow.json"
RULE_PACK="${ROOT_DIR}/examples/rulepacks/nifi-2.7-to-2.8.official.yaml"
OUT_DIR="${ROOT_DIR}/demo/out/asana-2.7-to-2.8"
SOURCE_GZ="${OUT_DIR}/source-flow.json.gz"
TARGET_MANIFEST="${1:-}"

mkdir -p "${OUT_DIR}"

export FIXTURE_JSON
export SOURCE_GZ

python3 - <<'PY'
import gzip
import os
from pathlib import Path

source = Path(os.environ["FIXTURE_JSON"])
target = Path(os.environ["SOURCE_GZ"])
target.write_bytes(gzip.compress(source.read_bytes()))
print(target)
PY

run_and_capture() {
  local name="$1"
  shift
  set +e
  "$@"
  local rc=$?
  set -e
  echo
  echo "${name} exit code: ${rc}"
  if [[ "${rc}" != "0" && "${rc}" != "2" ]]; then
    echo "${name} failed unexpectedly" >&2
    exit "${rc}"
  fi
}

echo "Running Asana 2.7.1 -> 2.8.0 analyze demo"
run_and_capture "analyze" \
  "${BINARY}" analyze \
  --source "${SOURCE_GZ}" \
  --source-format flow-json-gz \
  --source-version 2.7.1 \
  --target-version 2.8.0 \
  --rule-pack "${RULE_PACK}" \
  --output-dir "${OUT_DIR}"

if [[ -n "${TARGET_MANIFEST}" ]]; then
  echo
  echo "Running validate against target manifest ${TARGET_MANIFEST}"
  run_and_capture "validate" \
    "${BINARY}" validate \
    --input "${SOURCE_GZ}" \
    --input-format flow-json-gz \
    --target-version 2.8.0 \
    --extensions-manifest "${TARGET_MANIFEST}" \
    --output-dir "${OUT_DIR}"
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
echo "Quick view:"
echo "  less ${OUT_DIR}/migration-report.md"
if [[ -n "${TARGET_MANIFEST}" ]]; then
  echo "  less ${OUT_DIR}/validation-report.md"
fi
