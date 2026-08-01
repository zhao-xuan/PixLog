#!/usr/bin/env bash

set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"
git restore --source=HEAD --staged --worktree assets/hero.png
rm -rf artifacts
echo "Demo restored to HEAD."