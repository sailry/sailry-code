#!/usr/bin/env bash
set -euo pipefail

printf '| Platform | Package | Upload |\n| --- | --- | --- |\n' >> "$GITHUB_STEP_SUMMARY"
for platform in ANDROID IOS; do
  requested=${platform}_REQUESTED
  result=${platform}_RESULT
  built=${platform}_BUILT
  uploaded=${platform}_UPLOADED
  if test "${!built:-false}" = true; then
    package=built
  elif test "${!result:-skipped}" = failure; then
    package='build failed'
  elif test "${!requested:-false}" = true; then
    package='not built (checks failed or run cancelled)'
  else
    package='not built (not requested)'
  fi
  if test "${!uploaded:-false}" = true; then
    upload=uploaded
  else
    upload='not uploaded'
  fi
  printf '| %s | %s | %s |\n' "$platform" "$package" "$upload" >> "$GITHUB_STEP_SUMMARY"
done
