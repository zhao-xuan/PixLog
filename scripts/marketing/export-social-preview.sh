#!/usr/bin/env bash

set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
assets="${project_root}/marketing/assets/demo"
output="${project_root}/docs/assets/pixlog-social-preview.png"
temporary="$(mktemp -d "${TMPDIR:-/tmp}/pixlog-social-preview.XXXXXX")"
temporary="$(cd "$temporary" && pwd -P)"
trap 'rm -rf "$temporary"' EXIT

for command in git magick; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "$command is required to render the social preview." >&2
    exit 1
  fi
done

if [[ -n "${PIXLOG_BIN:-}" ]]; then
  pixlog_bin="$PIXLOG_BIN"
elif [[ -x "${project_root}/bin/pixlog" ]]; then
  pixlog_bin="${project_root}/bin/pixlog"
elif command -v pixlog >/dev/null 2>&1; then
  pixlog_bin="$(command -v pixlog)"
else
  echo "PixLog is not installed. Run 'make build' first." >&2
  exit 1
fi

bash "${project_root}/scripts/marketing/export-readme-assets.sh" >/dev/null
PIXLOG_BIN="$pixlog_bin" PIXLOG_DEMO_WORKSPACE="${temporary}/workspace" \
  bash "${project_root}/demo/setup.sh" --force >/dev/null
(
  cd "${temporary}/workspace"
  "$pixlog_bin" diff --heatmap "${assets}/hero-heatmap.png" \
    HEAD~1 HEAD -- assets/hero.png >/dev/null
)

font="${PIXLOG_SOCIAL_FONT:-}"
if [[ -z "$font" ]]; then
  available_fonts="$(magick -list font)"
  for candidate in Avenir-Next-Demi-Bold Avenir-Next Helvetica DejaVu-Sans AvantGarde-Demi; do
    if grep -Fqx "  Font: ${candidate}" <<< "$available_fonts"; then
      font="$candidate"
      break
    fi
  done
fi
if [[ -z "$font" ]]; then
  echo "No suitable font found for the social preview." >&2
  exit 1
fi

mkdir -p "$(dirname "$output")"
magick -size 1280x640 xc:'#1C232A' \
  \( "${assets}/hero-original.png" -resize '300x169^' -gravity center -extent 300x169 \) \
  -gravity northwest -geometry +36+92 -composite \
  \( "${assets}/hero-safe-edit.png" -resize '300x169^' -gravity center -extent 300x169 \) \
  -gravity northwest -geometry +356+92 -composite \
  \( "${assets}/hero-heatmap.png" -resize '300x169^' -gravity center -extent 300x169 \) \
  -gravity northwest -geometry +196+330 -composite \
  -fill none -stroke '#FFFDF8' -strokewidth 2 \
  -draw 'roundrectangle 35,91 337,262 6,6' \
  -draw 'roundrectangle 355,91 657,262 6,6' \
  -draw 'roundrectangle 195,329 497,500 6,6' \
  -font "$font" -fill '#FFFDF8' -stroke none -pointsize 22 \
  -annotate +36+78 'BEFORE' -annotate +356+78 'AFTER' \
  -annotate +196+316 'HEATMAP' \
  \( "${project_root}/docs/assets/pixlog-logo.png" -resize '390x142' \) \
  -gravity northwest -geometry +762+70 -composite \
  -font "$font" -fill '#FFFDF8' -pointsize 48 \
  -annotate +742+300 $'Git says\nhero.png changed.' \
  -fill '#91D0E2' -pointsize 29 \
  -annotate +742+430 $'PixLog shows which pixels\nchanged - and why.' \
  -fill '#F3C969' -pointsize 20 \
  -annotate +742+565 'Visual diff  |  Pixel blame' \
  -annotate +742+596 'AI provenance  |  CI policy' \
  -strip "$output"

printf 'Social preview written to %s\n' "$output"