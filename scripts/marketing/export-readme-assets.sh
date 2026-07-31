#!/usr/bin/env bash

set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source_dir="${project_root}/marketing/assets/sources"
output_dir="${project_root}/marketing/assets/demo"

if ! command -v magick >/dev/null 2>&1; then
  echo "ImageMagick is required. Install it with: brew install imagemagick" >&2
  exit 1
fi

mkdir -p "$output_dir"

magick "${source_dir}/earthrise.jpg" \
  -auto-orient -resize '960x540^' -gravity center -extent 960x540 \
  -strip -define png:exclude-chunk=date,time "${output_dir}/hero-original.png"

magick "${output_dir}/hero-original.png" \
  -fill 'rgba(238,107,86,0.18)' -stroke 'rgba(255,255,255,0.30)' \
  -strokewidth 2 -draw 'roundrectangle 704,116 846,184 10,10' \
  -strip -define png:exclude-chunk=date,time "${output_dir}/hero-safe-edit.png"

magick "${output_dir}/hero-safe-edit.png" \
  -modulate 72,145,100 -strip -define png:exclude-chunk=date,time \
  "${output_dir}/hero-policy-violation.png"

magick "${source_dir}/stable-diffusion-3.5-astronaut.webp" \
  -resize '960x540^' -gravity center -extent 960x540 \
  -strip -define png:exclude-chunk=date,time "${output_dir}/ai-poster.png"

printf 'README demo assets written to %s\n' "$output_dir"