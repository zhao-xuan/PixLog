#!/usr/bin/env bash

set -euo pipefail

policy="${PIXLOG_POLICY:-.pixlog-policy.json}"
revision_range="${PIXLOG_RANGE:-}"

if [[ -z "$revision_range" && -n "${GITHUB_BASE_REF:-}" ]]; then
  revision_range="origin/${GITHUB_BASE_REF}...HEAD"
fi
if [[ -z "$revision_range" ]] && git rev-parse --verify HEAD^ >/dev/null 2>&1; then
  revision_range="HEAD^..HEAD"
fi

arguments=(check --policy "$policy")
if [[ -n "$revision_range" ]]; then
  arguments+=(--range "$revision_range")
fi

set +e
output="$(pixlog "${arguments[@]}" 2>&1)"
exit_code=$?
set -e
printf '%s\n' "$output"

if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
  {
    echo "### PixLog visual policy"
    echo
    if [[ -n "$revision_range" ]]; then
      echo "Range: \`$revision_range\`"
      echo
    fi
    echo '```text'
    printf '%s\n' "$output"
    echo '```'
  } >> "$GITHUB_STEP_SUMMARY"
fi

exit "$exit_code"
