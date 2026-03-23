#!/usr/bin/env bash

set -euo pipefail

DEMO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEMO_BINARY="${BINARY:-${DEMO_ROOT}/bin/nifi-flow-upgrade}"

ensure_demo_binary() {
  mkdir -p "${DEMO_ROOT}/bin"
  if [[ ! -x "${DEMO_BINARY}" ]]; then
    echo "Building nifi-flow-upgrade into ${DEMO_BINARY}"
    (
      cd "${DEMO_ROOT}"
      go build -o "${DEMO_BINARY}" ./cmd/nifi-flow-upgrade
    )
  fi
}

gzip_fixture() {
  local fixture_json="$1"
  local source_gz="$2"

  mkdir -p "$(dirname "${source_gz}")"
  python3 - <<'PY' "${fixture_json}" "${source_gz}"
import gzip
import pathlib
import sys

src = pathlib.Path(sys.argv[1])
dst = pathlib.Path(sys.argv[2])
dst.parent.mkdir(parents=True, exist_ok=True)

with src.open("rb") as source, gzip.open(dst, "wb") as target:
    target.write(source.read())
PY
}

run_demo_analyze() {
  local source_gz="$1"
  local source_version="$2"
  local target_version="$3"
  local rule_pack="$4"
  local out_dir="$5"
  local fail_on="${6:-never}"
  local status

  set +e
  "${DEMO_BINARY}" analyze \
    --source "${source_gz}" \
    --source-format flow-json-gz \
    --source-version "${source_version}" \
    --target-version "${target_version}" \
    --rule-pack "${rule_pack}" \
    --fail-on "${fail_on}" \
    --output-dir "${out_dir}"
  status=$?
  set -e
  return "${status}"
}

run_demo_rewrite() {
  local plan_path="$1"
  local out_dir="$2"

  "${DEMO_BINARY}" rewrite \
    --plan "${plan_path}" \
    --output-dir "${out_dir}"
}

print_demo_footer() {
  local out_dir="$1"
  shift

  echo
  echo "Reports:"
  for path in "$@"; do
    echo "  ${path}"
  done
  echo
  echo "Quick checks:"
  echo "  jq '.summary' ${out_dir}/migration-report.json"
  if [[ -f "${out_dir}/rewrite-report.json" ]]; then
    echo "  jq '.summary' ${out_dir}/rewrite-report.json"
  fi
}
