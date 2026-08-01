#!/usr/bin/env bash

set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"
pixlog_bin="${PIXLOG_BIN:-pixlog}"

"$pixlog_bin" run -- magick assets/hero.png \
  -fill 'rgba(238,107,86,0.18)' -stroke 'rgba(255,255,255,0.30)' \
  -strokewidth 2 -draw 'roundrectangle 704,116 846,184 10,10' \
  -strip -define png:exclude-chunk=date,time assets/hero.png