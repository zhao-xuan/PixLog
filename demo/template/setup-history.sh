#!/usr/bin/env bash

set -euo pipefail

pixlog_bin="${PIXLOG_BIN:-pixlog}"
root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "$root" ]]; then
  echo "Run this script inside the generated Git repository." >&2
  exit 1
fi
cd "$root"

if [[ -n "$(git status --porcelain)" ]]; then
  echo "The demo workspace must be clean before history setup." >&2
  exit 1
fi

"$pixlog_bin" init
printf '\n.demo-source/** -filter -diff -merge -text\n' >> .gitattributes
git add .gitattributes .pixlog.toml
git commit -m "Configure PixLog" >/dev/null

mkdir -p assets
"$pixlog_bin" run -- magick .demo-source/earthrise.jpg \
  -auto-orient -resize '960x540^' -gravity center -extent 960x540 \
  -strip -define png:exclude-chunk=date,time assets/hero.png
git commit -m "Add launch artwork" >/dev/null

"$pixlog_bin" run -- magick .demo-source/stable-diffusion-3.5-astronaut.webp \
  -resize '960x540^' -gravity center -extent 960x540 \
  -strip -define png:exclude-chunk=date,time assets/ai-poster.png
"$pixlog_bin" recipe import assets/ai-poster.png recipes/ai-poster.json
git commit -m "Add AI-generated poster with provenance" >/dev/null

PIXLOG_BIN="$pixlog_bin" bash scripts/apply-safe-edit.sh
git commit -m "Refresh launch artwork" >/dev/null

"$pixlog_bin" check --range HEAD~1..HEAD
git config pixlog.demo.ready true