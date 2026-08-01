#!/usr/bin/env bash

set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
destination="${1:-${project_root}/marketing/assets/sources}"
temporary="$(mktemp -d "${TMPDIR:-/tmp}/pixlog-sources.XXXXXX")"
trap 'rm -rf "$temporary"' EXIT

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required to update the demo source images." >&2
  exit 1
fi

hash_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

download() {
  local name="$1"
  local url="$2"
  local expected="$3"
  local target="${temporary}/${name}"

  curl --fail --location --retry 3 --output "$target" "$url"
  local actual
  actual="$(hash_file "$target")"
  if [[ "$actual" != "$expected" ]]; then
    echo "checksum mismatch for ${name}: expected ${expected}, got ${actual}" >&2
    exit 1
  fi
}

download \
  "earthrise.jpg" \
  "https://upload.wikimedia.org/wikipedia/commons/a/a8/NASA-Apollo8-Dec24-Earthrise.jpg" \
  "24fd2a4f833534ca6741ed96d03b028ac1d8fda2cd129ded4d208535f541d4da"
download \
  "stable-diffusion-3.5-astronaut.webp" \
  "https://upload.wikimedia.org/wikipedia/commons/8/82/Astronaut_Riding_a_Horse_%28SD3.5%29.webp" \
  "0426c90afa64550e939e9f93403f2ed1e04bc52de55f12bb9b82a0a53cd8b1fd"

mkdir -p "$destination"
install -m 0644 "${temporary}/earthrise.jpg" "${destination}/earthrise.jpg"
install -m 0644 "${temporary}/stable-diffusion-3.5-astronaut.webp" \
  "${destination}/stable-diffusion-3.5-astronaut.webp"
printf 'Updated verified source images in %s\n' "$destination"