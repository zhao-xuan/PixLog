#!/usr/bin/env bash

set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$project_root"

if ! command -v vhs >/dev/null 2>&1; then
  echo "VHS is required. Install it with: brew install vhs" >&2
  exit 1
fi
if ! command -v ffmpeg >/dev/null 2>&1; then
  echo "FFmpeg is required. Install it with: brew install ffmpeg" >&2
  exit 1
fi
if ! command -v chafa >/dev/null 2>&1; then
  echo "Chafa is required. Install it with: brew install chafa" >&2
  exit 1
fi
if ! command -v magick >/dev/null 2>&1; then
  echo "ImageMagick is required. Install it with: brew install imagemagick" >&2
  exit 1
fi
if [[ ! -x bin/pixlog ]]; then
  echo "PixLog binary is missing. Run: make build" >&2
  exit 1
fi

bash scripts/marketing/export-readme-assets.sh
mkdir -p docs/assets/demos
scenarios=("$@")
if [[ ${#scenarios[@]} -eq 0 ]]; then
  scenarios=(visual-history policy-check visual-blame ai-provenance)
fi

for scenario in "${scenarios[@]}"; do
  tape="marketing/tapes/${scenario}.tape"
  if [[ ! -f "$tape" ]]; then
    echo "unknown recording scenario: $scenario" >&2
    exit 2
  fi
  echo "Recording $scenario"
  PIXLOG_PROJECT_ROOT="$project_root" PIXLOG_CHAFA_FORMAT="symbols" PIXLOG_PREVIEW_SIZE="72x16" PATH="${project_root}/bin:${PATH}" vhs "$tape"
  case "$scenario" in
    visual-history) poster_time="10" ;;
    policy-check) poster_time="7" ;;
    visual-blame) poster_time="7" ;;
    ai-provenance) poster_time="9" ;;
  esac
  ffmpeg -hide_banner -loglevel error -y \
    -ss "$poster_time" -i "docs/assets/demos/pixlog-${scenario}.mp4" \
    -frames:v 1 "docs/assets/demos/pixlog-${scenario}.png"
done

for output in docs/assets/demos/*.gif docs/assets/demos/*.mp4; do
  [[ -s "$output" ]] || {
    echo "recording output is empty: $output" >&2
    exit 1
  }
done

echo "Recordings written to docs/assets/demos"
