#!/usr/bin/env bash
set -euo pipefail

case "${1:-}" in
  -h | --help)
    cat <<'HELP'
Usage: ./dev.sh [build|test|vet GO_ARGUMENTS...]

Without arguments, prepare the Harness revision pinned by go.mod, including its
recursive submodules, and an ignored .runtime/go.work. Requires Bash, Go, Git and jq.

Examples:
  ./dev.sh build ./cmd/sailry-code-desktop
  ./dev.sh build ./cmd/sailry-code-host
  ./dev.sh test ./...

Only this entry's clean, detached .runtime/harness checkout is reused. Existing
development checkouts, changed pins and custom workspaces are never overwritten.
Move an obsolete managed checkout and workspace aside explicitly before retrying.
Go commands use -mod=readonly; no Go/Dart manifests or lockfiles are rewritten.
Tests need the pinned Turso runtime supplied separately through SAILRY_TURSO_LIB.
HELP
    exit 0 ;;
  '' | build | test | vet) ;;
  *) echo 'usage: ./dev.sh [build|test|vet GO_ARGUMENTS...]' >&2; exit 2 ;;
esac
for argument in "$@"; do
  case "$argument" in
    -mod | -mod=* | -modfile | -modfile=*) echo 'dev.sh keeps module manifests read-only' >&2; exit 2 ;;
  esac
done

root=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
cd "$root"
runtime=$root/.runtime
harness=$runtime/harness
workspace=$runtime/go.work
repository=https://github.com/sailry/sailry-harness.git
test ! -L "$runtime" && test ! -L "$harness" && test ! -L "$workspace" && test ! -L "$workspace.sum" ||
  { echo 'Bootstrap paths must not be symlinks' >&2; exit 1; }
mkdir -p "$runtime"
lock=$runtime/.harness-bootstrap.lock
mkdir "$lock" || { echo 'Another bootstrap owns .runtime/.harness-bootstrap.lock; inspect it before retrying' >&2; exit 1; }
trap 'rmdir "$lock"' EXIT

if test -e "$harness"; then
  test -d "$harness/.git" && test ! -L "$harness/.git" &&
    test "$(git -C "$harness" config --local --get sailry.codeBootstrap)" = 1 ||
    { echo '.runtime/harness is not a managed Code bootstrap checkout; leave the development checkout unchanged' >&2; exit 1; }
  test "$(git -C "$harness" config --local --get remote.origin.url)" = "$repository" ||
    { echo 'The managed Harness origin changed; refusing to modify it' >&2; exit 1; }
  if git -C "$harness" symbolic-ref --quiet HEAD >/dev/null; then
    echo 'The managed Harness checkout is on a development branch; refusing to modify it' >&2
    exit 1
  fi
  status=$(git -C "$harness" status --porcelain --untracked-files=all --ignore-submodules=none)
  submodules=$(git -C "$harness" submodule status --recursive)
  test -z "$status" && ! grep -Eq '^[-+U]' <<<"$submodules" ||
    { echo 'The managed Harness checkout or its submodules changed; refusing to modify it' >&2; exit 1; }
fi

unset GOROOT
export GOTOOLCHAIN=go1.27.3 CGO_ENABLED=0 GOWORK=off GOFLAGS=
revision=$(bash .github/scripts/harness-revision.sh)
if test -e "$workspace"; then
  legacy_root=$root
  case "${OSTYPE:-}" in msys*|cygwin*) legacy_root=$(cygpath -am "$root") ;; esac
  GOWORK=$workspace go work edit -json | jq -e --arg code "$legacy_root" \
    '([.Use[].DiskPath | gsub("\\\\"; "/")] | sort) as $paths |
     ($paths == (["..", "./harness"] | sort) or $paths == ([$code, ($code + "/.runtime/harness")] | sort))
     and ((.Replace // []) | length == 0)' >/dev/null ||
    { echo '.runtime/go.work is a custom workspace; refusing to overwrite it' >&2; exit 1; }
fi
if test -e "$harness"; then
  test "$(git -C "$harness" rev-parse --verify HEAD)" = "$revision" ||
    { echo 'The managed Harness checkout has another pin; move it and .runtime/go.work aside explicitly' >&2; exit 1; }
else
  mkdir "$harness"
  git -C "$harness" init --quiet
  git -C "$harness" remote add origin "$repository"
  git -C "$harness" fetch --quiet --depth=1 origin "$revision"
  git -C "$harness" checkout --quiet --detach "$revision"
  git -C "$harness" submodule update --init --recursive
  git -C "$harness" config --local sailry.codeBootstrap 1
fi
if test ! -e "$workspace"; then
  (cd "$runtime" && go work init .. ./harness)
fi
export GOWORK=$workspace SAILRY_HARNESS_DIR=$harness GOFLAGS=-mod=readonly
printf 'Harness %s; workspace .runtime/go.work is ready.\n' "$revision" >&2
rmdir "$lock"
trap - EXIT
if test $# -gt 0; then
  exec go "$@"
fi
