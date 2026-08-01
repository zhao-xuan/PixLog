#!/usr/bin/env bash

set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"
pixlog_bin="${PIXLOG_BIN:-pixlog}"

"$pixlog_bin" run -- magick assets/hero.png \
  -modulate 72,145,100 -strip -define png:exclude-chunk=date,time \
  assets/hero.png
echo "Staged an edit that changes protected pixels. Run: pixlog check"