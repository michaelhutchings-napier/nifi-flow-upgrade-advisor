#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_PATH="${ROOT_DIR}/bin/nifi-flow-upgrade"
FIXTURE_JSON="${ROOT_DIR}/demo/fixtures/get-http-1.27-flow.json"
RULE_PACK="${ROOT_DIR}/examples/rulepacks/nifi-1.27-to-2.0.official.yaml"
OUT_DIR="${ROOT_DIR}/demo/out/get-http-1.27-to-2.0"

mkdir -p "${ROOT_DIR}/bin"
rm -rf "${OUT_DIR}"
mkdir -p "${OUT_DIR}"

if [[ ! -x "${BIN_PATH}" ]]; then
  echo "Building nifi-flow-upgrade into ${BIN_PATH}"
  (
    cd "${ROOT_DIR}"
    go build -o "${BIN_PATH}" ./cmd/nifi-flow-upgrade
  )
fi

python3 - <<'PY' "${FIXTURE_JSON}" "${OUT_DIR}/source-flow.json.gz"
import gzip
import pathlib
import sys

src = pathlib.Path(sys.argv[1])
dst = pathlib.Path(sys.argv[2])
dst.parent.mkdir(parents=True, exist_ok=True)

with src.open("rb") as source, gzip.open(dst, "wb") as target:
    target.write(source.read())
PY

echo "Running GetHTTP 1.27.0 -> 2.0.0 manual-change demo"
set +e
"${BIN_PATH}" analyze \
  --source "${OUT_DIR}/source-flow.json.gz" \
  --source-format flow-json-gz \
  --source-version 1.27.0 \
  --target-version 2.0.0 \
  --rule-pack "${RULE_PACK}" \
  --fail-on never \
  --output-dir "${OUT_DIR}"
ANALYZE_EXIT=$?
set -e

echo
echo "Running rewrite to show that manual-change findings do not trigger silent conversion"
"${BIN_PATH}" rewrite \
  --plan "${OUT_DIR}/migration-report.json" \
  --output-dir "${OUT_DIR}"

echo
echo "Expected outcome:"
echo "  - analyze exits 0 with manual-change findings"
echo "  - rewrite writes a rewritten artifact but applies 0 operations"
echo
echo "Open the reports with:"
echo "  less ${OUT_DIR}/migration-report.md"
echo "  less ${OUT_DIR}/rewrite-report.md"
echo
echo "Useful checks:"
echo "  jq '.summary' ${OUT_DIR}/migration-report.json"
echo "  jq '.summary' ${OUT_DIR}/rewrite-report.json"
echo
echo "Analyze exit code: ${ANALYZE_EXIT}"

