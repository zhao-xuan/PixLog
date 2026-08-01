#!/usr/bin/env bash

set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workspace="${PIXLOG_DEMO_WORKSPACE:-${project_root}/demo/workspace}"
force=false

if [[ "${1:-}" == "--force" ]]; then
  force=true
  shift
fi
if [[ $# -ne 0 ]]; then
  echo "usage: $0 [--force]" >&2
  exit 2
fi

if [[ -e "$workspace" ]]; then
  if [[ "$force" == "true" ]]; then
    rm -rf "$workspace"
  else
    echo "Demo workspace already exists: $workspace" >&2
    echo "Run '$0 --force' to rebuild it." >&2
    exit 1
  fi
fi

if [[ -n "${PIXLOG_BIN:-}" ]]; then
  pixlog_bin="$PIXLOG_BIN"
elif [[ -x "${project_root}/bin/pixlog" ]]; then
  pixlog_bin="${project_root}/bin/pixlog"
elif command -v pixlog >/dev/null 2>&1; then
  pixlog_bin="$(command -v pixlog)"
else
  echo "PixLog is not installed. Run 'make build' or install the latest release." >&2
  exit 1
fi

for command in git magick; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "$command is required to build the demo workspace." >&2
    exit 1
  fi
done

mkdir -p "$workspace"
cp -R "${project_root}/demo/template/." "$workspace/"
mkdir -p "${workspace}/.demo-source"
cp -R "${project_root}/marketing/assets/sources/." "${workspace}/.demo-source/"

git -C "$workspace" init -b main >/dev/null
git -C "$workspace" config user.name "PixLog Demo"
git -C "$workspace" config user.email "demo@pixlog.dev"
git -C "$workspace" add .
git -C "$workspace" commit -m "Add PixLog demo scaffold" >/dev/null

(
  cd "$workspace"
  PIXLOG_BIN="$pixlog_bin" bash setup-history.sh
)

cat <<EOF

Demo workspace ready:
  cd demo/workspace
  pixlog diff HEAD~1 HEAD -- assets/hero.png
  pixlog blame --point 760,145 assets/hero.png
  pixlog recipe show --revision HEAD assets/ai-poster.png
  bash scripts/apply-unsafe-edit.sh && pixlog check
EOF