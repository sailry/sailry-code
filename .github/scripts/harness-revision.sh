#!/usr/bin/env bash
set -euo pipefail

unset GOROOT
export GOTOOLCHAIN=go1.27.3 CGO_ENABLED=0 GOWORK=off GOFLAGS=
module=github.com/sailry/sailry-harness
repository=https://github.com/sailry/sailry-harness
metadata=$(go mod edit -json)
jq -e --arg module "$module" '[.Replace[]? | select(.Old.Path == $module)] | length == 0' <<<"$metadata" >/dev/null
version=$(jq -er --arg module "$module" '.Require[] | select(.Path == $module) | .Version' <<<"$metadata" | tr -d '\r')
timestamp= prefix=
if [[ $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+-([0-9A-Za-z.-]+\.)?([0-9]{14})-([0-9a-f]{12})(\+incompatible)?$ ]]; then
  timestamp=${BASH_REMATCH[2]}
  prefix=${BASH_REMATCH[3]}
  # dev.sh supplies these only after checking the clean, managed, detached checkout.
  verified_revision=${1:-}
  verified_timestamp=${2:-}
  if [[ $verified_revision =~ ^[0-9a-f]{40}$ ]] && test "${verified_revision:0:12}" = "$prefix" &&
    test "$verified_timestamp" = "$timestamp"; then
    printf '%s\n' "$verified_revision"
    exit 0
  fi
fi
scratch=$(mktemp -d "${TMPDIR:-/tmp}/code-harness-revision-XXXXXX")
trap 'rm -rf -- "$scratch"' EXIT
dependency=$(cd "$scratch" && go mod download -json "$module@$version")
jq -e --arg module "$module" --arg version "$version" '.Path == $module and .Version == $version and .Error == null' <<<"$dependency" >/dev/null
revision=$(jq -r '.Origin.Hash // empty' <<<"$dependency" | tr -d '\r')
if test -z "$revision"; then
  if test -n "$prefix"; then
    git init --bare --quiet "$scratch/source.git"
    git -C "$scratch/source.git" fetch --quiet --filter=blob:none --no-tags "$repository" \
      '+refs/heads/*:refs/remotes/source/*' '+refs/tags/*:refs/tags/*'
    revision=$(git -C "$scratch/source.git" rev-parse --verify "$prefix^{commit}")
    test "${revision:0:12}" = "$prefix"
    actual=$(TZ=UTC0 git -C "$scratch/source.git" show -s --date=format-local:%Y%m%d%H%M%S --format=%cd "$revision")
    test "$actual" = "$timestamp" || { echo 'Harness pseudo-version timestamp does not match its Git commit' >&2; exit 1; }
  else
    [[ $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+incompatible)?$ ]] || { echo 'Harness must require an exact module version' >&2; exit 1; }
    tag=${version%+incompatible}
    git ls-remote --exit-code "$repository" "refs/tags/$tag" "refs/tags/$tag^{}" > "$scratch/tags"
    revision=$(awk -v tag="refs/tags/$tag" '$2 == tag { direct=$1 } $2 == tag "^{}" { peeled=$1 } END { print peeled ? peeled : direct }' "$scratch/tags")
  fi
fi
[[ $revision =~ ^[0-9a-f]{40}$ ]] || { echo 'Harness must resolve to a full Git revision' >&2; exit 1; }
printf '%s\n' "$revision"
