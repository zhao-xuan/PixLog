#!/usr/bin/env bash

set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <visual-history|policy-check|visual-blame|ai-provenance>" >&2
  exit 2
fi

scenario="$1"
project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
destination="${project_root}/dist/marketing/recordings/${scenario}"

case "$scenario" in
  visual-history | policy-check | visual-blame | ai-provenance) ;;
  *)
    echo "unknown recording scenario: $scenario" >&2
    exit 2
    ;;
esac

PIXLOG_BIN="${project_root}/bin/pixlog" PIXLOG_DEMO_WORKSPACE="$destination" \
  bash "${project_root}/demo/setup.sh" --force >/dev/null
mkdir -p "$destination/artifacts"

if [[ "$scenario" == "visual-history" ]]; then
  git -C "$destination" reset --hard HEAD~1 >/dev/null
fi

echo "READY $scenario"
