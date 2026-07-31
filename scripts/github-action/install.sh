#!/usr/bin/env bash

set -euo pipefail

version="${PIXLOG_VERSION:-v0.1.0}"
version="v${version#v}"
release_version="${version#v}"

case "$(uname -s)" in
  Linux) goos="linux" ;;
  Darwin) goos="darwin" ;;
  *)
    echo "PixLog Action supports Linux and macOS runners." >&2
    exit 1
    ;;
esac

case "$(uname -m)" in
  x86_64 | amd64) goarch="amd64" ;;
  arm64 | aarch64) goarch="arm64" ;;
  *)
    echo "Unsupported runner architecture: $(uname -m)" >&2
    exit 1
    ;;
esac

archive_base="pixlog_${release_version}_${goos}_${goarch}"
archive="${archive_base}.tar.gz"
release_url="https://github.com/zhao-xuan/PixLog/releases/download/${version}"
temporary="$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/pixlog-action.XXXXXX")"
trap 'rm -rf "$temporary"' EXIT

curl --fail --location --silent --show-error \
  "${release_url}/${archive}" --output "${temporary}/${archive}"
curl --fail --location --silent --show-error \
  "${release_url}/${archive}.sha256" --output "${temporary}/${archive}.sha256"

(
  cd "$temporary"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum --check "${archive}.sha256"
  else
    shasum -a 256 --check "${archive}.sha256"
  fi
  tar -xzf "$archive"
)

install_dir="${RUNNER_TEMP:-${TMPDIR:-/tmp}}/pixlog-${release_version}"
mkdir -p "$install_dir"
install -m 0755 "${temporary}/${archive_base}/pixlog" "$install_dir/pixlog"
install -m 0755 "${temporary}/${archive_base}/git-pixlog" "$install_dir/git-pixlog"
echo "$install_dir" >> "$GITHUB_PATH"

"$install_dir/pixlog" version
