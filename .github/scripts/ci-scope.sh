#!/usr/bin/env bash
set -euo pipefail

write_scope() {
  printf 'go=%s\npackages=%s\n' "$1" "$2" >> "$GITHUB_OUTPUT"
}

fail() {
  printf '%s\n' "$*" >&2
  exit 1
}

desktop_version() {
  git show "$1:cmd/sailry-code-desktop/mygo.json" |
    jq -ers 'select(length == 1) | .[0].version | strings | select(test("\\A[0-9]+\\.[0-9]+\\.[0-9]+(-[0-9A-Za-z.-]+)?\\z"))'
}

require_release() {
  local tag=$1 tag_sha pushed_at release_run
  local advice="Use tag-first: push only $tag, wait for Code Release to succeed, then fast-forward main to that exact commit."

  # The main checkout fetches the repository's tags; ^{commit} peels annotated tags.
  if ! tag_sha=$(git rev-parse --verify --quiet "refs/tags/$tag^{commit}") || [ "$tag_sha" != "$GITHUB_SHA" ]; then
    fail "$tag does not resolve to $GITHUB_SHA in $GITHUB_REPOSITORY. $advice"
  fi

  # Use the timestamp captured on the main push, not the clock of a delayed run or rerun.
  pushed_at=$(jq -er --arg repo "$GITHUB_REPOSITORY" --arg before "$BEFORE" --arg after "$GITHUB_SHA" '
    select(.ref == "refs/heads/main" and .before == $before and .after == $after and .repository.full_name == $repo) |
    .repository.pushed_at | if type == "string" then fromdateiso8601 else numbers end | select(. > 0 and . == floor)
  ' "$GITHUB_EVENT_PATH") || fail "Cannot verify the main push timestamp. $advice"

  # Read public Actions metadata without elevating the reusable workflow's token.
  # The caller's head_branch alone does not distinguish a tag from a same-named branch.
  release_run=$(curl -fsS -H 'Accept: application/vnd.github+json' \
    "$GITHUB_API_URL/repos/$GITHUB_REPOSITORY/actions/workflows/release.yml/runs?event=push&branch=$tag&head_sha=$GITHUB_SHA&status=success&per_page=100" |
    jq -er --arg repo "$GITHUB_REPOSITORY" --arg sha "$GITHUB_SHA" --arg tag "$tag" --argjson cutoff "$pushed_at" '
      first(.workflow_runs[] | select(
        .repository.full_name == $repo and .head_repository.full_name == $repo and
        .event == "push" and .head_branch == $tag and .head_sha == $sha and
        .path == ".github/workflows/release.yml" and .status == "completed" and .conclusion == "success" and
        any(.referenced_workflows[]?; .ref == ("refs/tags/" + $tag) and .sha == $sha and
          .path == ($repo + "/.github/workflows/ci.yml@" + $sha)) and
        (.updated_at | fromdateiso8601) < $cutoff
      ) | .id | numbers)') || fail "No verified Code Release succeeded for $tag at $GITHUB_SHA before this main push. $advice"
  printf 'Reusing Code Release run %s for %s at %s\n' "$release_run" "$tag" "$GITHUB_SHA"
}

# Formal and manual integration runs always get the full matrix.
if [ "${DISTRIBUTION:-false}" = true ] || [ "${INTEGRATION:-false}" = true ]; then
  write_scope true true
  exit 0
fi

changed=
case "${GITHUB_EVENT_NAME:-}:${GITHUB_REF:-}" in
  push:refs/heads/main)
    if ! [[ ${BEFORE:-} =~ ^[0-9a-f]{40}$ && ${GITHUB_SHA:-} =~ ^[0-9a-f]{40}$ ]] ||
      [ "$BEFORE" = '0000000000000000000000000000000000000000' ] ||
      [ "$GITHUB_SHA" != "$(git rev-parse HEAD)" ] || ! git merge-base --is-ancestor "$BEFORE" "$GITHUB_SHA"; then
      fail 'Cannot verify the main push range: expected the previous main SHA and a fast-forward to the checked-out commit.'
    fi
    previous=$(desktop_version "$BEFORE") || fail 'Cannot read the previous main Desktop version.'
    version=$(desktop_version "$GITHUB_SHA") || fail 'Cannot read the pushed Desktop version.'
    if [ "$version" != "$previous" ]; then
      require_release "v$version"
      write_scope false false
      exit 0
    fi
    changed=$(git diff --name-only --no-renames "$BEFORE" "$GITHUB_SHA")
    ;;
  pull_request:*)
    changed=$(git diff --name-only --no-renames HEAD^1 HEAD)
    ;;
esac

go_checks=true
packages=true
if [ -n "$changed" ]; then
  non_go_paths='^(docs/|third_party_licenses/|\.github/ISSUE_TEMPLATE/|\.github/pull_request_template\.md$|(README(\.zh-CN)?|CONTRIBUTING|ARCHITECTURE|AGENTS|CHANGELOG|SECURITY|LICENSE[^/]*)\.md$|LICENSE$)'
  go_inputs=$(grep -Ev "$non_go_paths" <<<"$changed" || true)
  if [ -z "$go_inputs" ]; then
    go_checks=false
    packages=false
  elif [ "${GITHUB_EVENT_NAME:-}" = pull_request ]; then
    # Ordinary pull requests run full Go/vet/race verification on Linux and build
    # all five distribution packages (plus macOS/Windows platform tests) when
    # packaging, entrypoints, OS-specific files, or pinned dependencies change.
    # Ordinary main code pushes and releases verify full packaging and platform tests.
    packaging_inputs='^(cmd/|internal/product/|go\.(mod|sum)$|dev\.sh$|modules/code/terminal/node/|\.github/(workflows/(ci|release)\.yml$|actions/(harness|native-runtime)/|scripts/(harness-revision|ci-scope)\.sh$))|_(darwin|windows|unix)(_test)?\.go$'
    if ! grep -Eq "$packaging_inputs" <<<"$changed"; then
      packages=false
    fi
  fi
fi

write_scope "$go_checks" "$packages"
