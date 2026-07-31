#!/usr/bin/env sh

set -eu

if command -v chafa >/dev/null 2>&1; then
  chafa --version | sed -n '1p'
  exit 0
fi

run_as_root() {
  if [ "$(id -u)" -eq 0 ]; then
    "$@"
  elif command -v sudo >/dev/null 2>&1; then
    sudo "$@"
  else
    echo "Installing Chafa with this package manager requires root access." >&2
    exit 1
  fi
}

if command -v brew >/dev/null 2>&1; then
  brew install chafa
elif command -v port >/dev/null 2>&1; then
  run_as_root port install chafa
elif command -v apt-get >/dev/null 2>&1; then
  run_as_root apt-get update
  run_as_root apt-get install -y chafa
elif command -v dnf >/dev/null 2>&1; then
  run_as_root dnf install -y chafa
elif command -v pacman >/dev/null 2>&1; then
  run_as_root pacman -S --needed chafa
elif command -v zypper >/dev/null 2>&1; then
  run_as_root zypper install -y chafa
elif command -v pkg >/dev/null 2>&1; then
  run_as_root pkg install -y chafa
elif command -v pkg_add >/dev/null 2>&1; then
  run_as_root pkg_add chafa
else
  echo "No supported package manager found. Install Chafa from https://hpjansson.org/chafa/download/" >&2
  exit 1
fi

if ! command -v chafa >/dev/null 2>&1; then
  echo "Chafa was installed but is not available on PATH." >&2
  exit 1
fi
chafa --version | sed -n '1p'