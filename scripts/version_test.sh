#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

cp -R "${root_dir}/scripts" "${tmp_dir}/scripts"
cp "${root_dir}/manifest.json" "${tmp_dir}/manifest.json"

(
  cd "${tmp_dir}"
  ./scripts/version set 2.3.4
  test "$(./scripts/version get)" = "2.3.4"
  python3 - <<'PY'
import json

with open("manifest.json", "r", encoding="utf-8") as f:
    manifest = json.load(f)
assert manifest["name"] == "jcemb"
assert manifest["author"]
assert manifest["version"] == "2.3.4"
PY
)
