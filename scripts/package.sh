#!/usr/bin/env bash

set -euo pipefail

if [[ $# -lt 3 || $# -gt 4 ]]; then
  echo "usage: $0 <version> <goos> <goarch> [output-directory]" >&2
  exit 2
fi

version="${1#v}"
goos="$2"
goarch="$3"
output_dir="${4:-dist}"

case "${goos}/${goarch}" in
  linux/amd64 | linux/arm64 | darwin/amd64 | darwin/arm64 | windows/amd64 | windows/arm64) ;;
  *)
    echo "unsupported release target: ${goos}/${goarch}" >&2
    exit 2
    ;;
esac

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_root"

mkdir -p "$output_dir"
output_dir="$(cd "$output_dir" && pwd)"

archive_name="pixlog_${version}_${goos}_${goarch}"
staging_dir="${output_dir}/${archive_name}"
binary_extension=""
if [[ "$goos" == "windows" ]]; then
  binary_extension=".exe"
fi

rm -rf "$staging_dir"
mkdir -p "$staging_dir"

ldflags="-s -w -X github.com/pixlog/pixlog/internal/cli.Version=${version}"
CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
  go build -trimpath -ldflags "$ldflags" -o "${staging_dir}/pixlog${binary_extension}" ./cmd/pixlog
CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
  go build -trimpath -ldflags "$ldflags" -o "${staging_dir}/git-pixlog${binary_extension}" ./cmd/git-pixlog
cp LICENSE README.md README.zh-CN.md "$staging_dir/"

if [[ "$goos" == "windows" ]]; then
  asset_name="${archive_name}.zip"
  (cd "$output_dir" && zip -qr "$asset_name" "$archive_name")
else
  asset_name="${archive_name}.tar.gz"
  tar -C "$output_dir" -czf "${output_dir}/${asset_name}" "$archive_name"
fi

rm -rf "$staging_dir"

if command -v sha256sum >/dev/null 2>&1; then
  (cd "$output_dir" && sha256sum "$asset_name" > "${asset_name}.sha256")
else
  (cd "$output_dir" && shasum -a 256 "$asset_name" > "${asset_name}.sha256")
fi

printf '%s\n' "${output_dir}/${asset_name}"