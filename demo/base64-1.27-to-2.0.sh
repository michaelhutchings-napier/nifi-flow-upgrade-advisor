#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BINARY="${BINARY:-${ROOT_DIR}/nifi-flow-upgrade}"
FIXTURE_JSON="${ROOT_DIR}/demo/fixtures/base64-1.27-flow.json"
RULE_PACK="${ROOT_DIR}/examples/rulepacks/nifi-1.27-to-2.0.official.yaml"
OUT_DIR="${ROOT_DIR}/demo/out/base64-1.27-to-2.0"
SOURCE_GZ="${OUT_DIR}/source-flow.json.gz"

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

set +e
"${BINARY}" analyze \
  --source "${SOURCE_GZ}" \
  --source-format flow-json-gz \
  --source-version 1.27.0 \
  --target-version 2.0.0 \
  --rule-pack "${RULE_PACK}" \
  --output-dir "${OUT_DIR}"
analyze_rc=$?
set -e

echo
echo "analyze exit code: ${analyze_rc}"

"${BINARY}" rewrite \
  --source "${SOURCE_GZ}" \
  --source-format flow-json-gz \
  --source-version 1.27.0 \
  --target-version 2.0.0 \
  --rule-pack "${RULE_PACK}" \
  --output-dir "${OUT_DIR}"

echo
echo "Reports:"
echo "  ${OUT_DIR}/migration-report.json"
echo "  ${OUT_DIR}/migration-report.md"
echo "  ${OUT_DIR}/rewrite-report.json"
echo "  ${OUT_DIR}/rewrite-report.md"
echo "  ${OUT_DIR}/rewritten-flow.json.gz"
echo
echo "Quick checks:"
echo "  jq '.summary' ${OUT_DIR}/migration-report.json"
echo "  jq '.summary' ${OUT_DIR}/rewrite-report.json"
echo "  gzip -dc ${OUT_DIR}/rewritten-flow.json.gz | rg 'EncodeContent|Base64EncodeContent'"
