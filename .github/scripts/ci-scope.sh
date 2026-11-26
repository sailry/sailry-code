#!/usr/bin/env bash
set -euo pipefail

# Determine which Code CI lanes must run for the current event. Releases,
# manual runs, tags, and truncated diffs fall back to the full set.
go_checks=true
packages=true

if [ "${DISTRIBUTION:-false}" = true ] || [ "${INTEGRATION:-false}" = true ]; then
  printf 'go=%s\npackages=%s\n' "$go_checks" "$packages" >> "$GITHUB_OUTPUT"
  exit 0
fi

changed=
have_changed=false
if [ -n "${CHANGED_FILES:-}" ]; then
  changed=$CHANGED_FILES
  have_changed=true
else
  case "${GITHUB_EVENT_NAME:-}" in
    pull_request)
      changed=$(git diff --name-only HEAD^1 HEAD)
      have_changed=true
      ;;
    push)
      case "${GITHUB_REF:-}" in
        refs/tags/*) ;;
        *)
          if [ -n "${BEFORE:-}" ] && [ "$BEFORE" != '0000000000000000000000000000000000000000' ]; then
            if compare=$(gh api "repos/$GH_REPO/compare/$BEFORE...$GITHUB_SHA" 2>/dev/null); then
              count=$(jq '.files | length' <<<"$compare")
              if [ "$count" -lt 300 ]; then
                changed=$(jq -r '.files[] | .filename, (.previous_filename // empty)' <<<"$compare" | sort -u)
                have_changed=true
              fi
            fi
          fi
          ;;
      esac
      ;;
  esac
fi

if [ "$have_changed" = true ] && [ -n "$changed" ]; then
  non_go_paths='^(docs/|third_party_licenses/|\.github/ISSUE_TEMPLATE/|\.github/pull_request_template\.md$|(README|CONTRIBUTING|ARCHITECTURE|AGENTS|CHANGELOG|SECURITY|LICENSE[^/]*)\.md$|LICENSE$)'
  go_inputs=$(grep -Ev "$non_go_paths" <<<"$changed" || true)
  if [ -z "$go_inputs" ]; then
    go_checks=false
    packages=false
  elif [ "${GITHUB_EVENT_NAME:-}" = pull_request ]; then
    # Ordinary pull requests run full Go/vet/race verification on Linux and build
    # all five distribution packages (plus macOS/Windows platform tests) when
    # packaging, entrypoints, OS-specific files, or pinned dependencies change.
    # Main pushes and releases always verify full packaging and platform tests.
    packaging_inputs='^(cmd/|internal/product/|go\.(mod|sum)$|dev\.sh$|modules/code/terminal/node/|\.github/(workflows/(ci|release)\.yml$|actions/(harness|native-runtime)/|scripts/(harness-revision|ci-scope)\.sh$))|_(darwin|windows|unix)(_test)?\.go$'
    if ! grep -Eq "$packaging_inputs" <<<"$changed"; then
      packages=false
    fi
  fi
fi

printf 'go=%s\npackages=%s\n' "$go_checks" "$packages" >> "$GITHUB_OUTPUT"
