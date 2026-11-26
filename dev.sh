#!/usr/bin/env bash
set -euo pipefail

case "${1:-}" in
  -h | --help)
    cat <<'HELP'
Usage: ./dev.sh [build|test|vet GO_ARGUMENTS...]
       ./dev.sh desktop [dev|NAME]
       ./dev.sh native[-build]

No command: prepare the exact Harness SDK pin and .runtime/go.work; no submodules.
For local commands, a root go.work can use Code and a local Harness checkout.
Code preinstalls no plugins or themes; their repositories are not build dependencies.
Requires Bash, Go, Git and jq; native builds also need rustup and a system linker.
Go 1.27.3, CGO_ENABLED=0, no inherited GOROOT; manifests/locks stay read-only.
  just desktop        Code default; no --data-dir, preserving first-start migration
  just desktop dev    ~/.sailry-code-dev, separate from Harness development data
  just desktop NAME   This Code checkout's .runtime/profiles/NAME
Names use letters/digits/._-, cannot start with a dot; close other profile users.

Native setup: install the pinned Rust toolchain in the selected Harness checkout
  (cd native/turso && rustup toolchain install)
just native builds/reuses FTS Turso; Desktop prepares it and sets SAILRY_TURSO_LIB.
Libraries stage in <harness>/.runtime/native/<goos>-<goarch>/; tests use SAILRY_TURSO_LIB.
Verify the matching library with file and otool -L (macOS) or ldd (Linux), then
run just desktop native-smoke twice: window/history must open without loader errors.
Optional native features need the pinned Harness native setup and separate checks.

Script-only checks: bash -n dev.sh; just --fmt --check; just test-scripts.
Only clean, managed, detached checkouts are reused; mismatched pins, development
checkouts and custom workspaces are never overwritten. First setup needs network.
HELP
    exit 0 ;;
  '' | build | test | vet | desktop | native | native-build) ;;
  *) echo 'usage: ./dev.sh [build|test|vet GO_ARGUMENTS...] | desktop [dev|NAME] | native[-build]' >&2; exit 2 ;;
esac
mode=${1:-}
case "$mode" in
  desktop)
    test $# -le 2 || { echo 'usage: ./dev.sh desktop [dev|NAME]' >&2; exit 2; }
    name=${2:-}
    if test -n "$name" && [[ ! $name =~ ^[A-Za-z0-9_-][A-Za-z0-9_.-]*$ ]]; then
      echo 'Profile names must be a single directory name using letters, digits, hyphens, underscores or dots' >&2
      exit 2
    fi ;;
  native | native-build)
    test $# -eq 1 || { echo 'usage: ./dev.sh native[-build]' >&2; exit 2; } ;;
esac
for argument in "$@"; do
  case "$argument" in
    -mod | -mod=* | -modfile | -modfile=*) echo 'dev.sh keeps module manifests read-only' >&2; exit 2 ;;
  esac
done

root=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
cd "$root"
runtime=$root/.runtime
unset GOROOT
export GOTOOLCHAIN=go1.27.3 CGO_ENABLED=0 GOFLAGS=-mod=readonly
if test -n "$mode" && test -f "$root/go.work"; then
  workspace=$root/go.work
  export GOWORK=$workspace
  harness=$(go list -m -f '{{if eq .Path "github.com/sailry/sailry-harness"}}{{.Dir}}{{end}}')
  test -n "$harness" || { echo 'Include the local Harness checkout in go.work with go work use' >&2; exit 1; }
  case "${OSTYPE:-}" in msys*|cygwin*) harness=$(cygpath -au "$harness") ;; esac
else
  export GOWORK=off
  harness=$runtime/harness
  workspace=$runtime/go.work
  repository=https://github.com/sailry/sailry-harness.git
  test ! -L "$runtime" && test ! -L "$harness" && test ! -L "$workspace" && test ! -L "$workspace.sum" ||
    { echo 'Bootstrap paths must not be symlinks' >&2; exit 1; }
  mkdir -p "$runtime"
  lock=$runtime/.harness-bootstrap.lock
  mkdir "$lock" || { echo 'Another bootstrap owns .runtime/.harness-bootstrap.lock; inspect it before retrying' >&2; exit 1; }
  trap 'rmdir "$lock"' EXIT

  verified_revision= verified_timestamp=
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
    test -z "$status" ||
      { echo 'The managed Harness checkout or its submodules changed; refusing to modify it' >&2; exit 1; }
    verified_revision=$(git -C "$harness" rev-parse --verify HEAD)
    verified_timestamp=$(TZ=UTC0 git -C "$harness" show -s --date=format-local:%Y%m%d%H%M%S --format=%cd "$verified_revision")
  fi

  revision=$(bash .github/scripts/harness-revision.sh "$verified_revision" "$verified_timestamp")
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
    git -C "$harness" config --local sailry.codeBootstrap 1
  fi
  if test ! -e "$workspace"; then
    (cd "$runtime" && go work init .. ./harness)
  fi
  printf 'Harness %s; workspace .runtime/go.work is ready.\n' "$revision" >&2
  rmdir "$lock"
  trap - EXIT
fi
export GOWORK=$workspace SAILRY_HARNESS_DIR=$harness
case "$mode" in
  desktop | native | native-build)
    case "$(uname -s)" in
      Darwin) platform=darwin; library=libturso_sync_sdk_kit.dylib ;;
      Linux) platform=linux; library=libturso_sync_sdk_kit.so ;;
      MINGW* | MSYS*) platform=windows; library=turso_sync_sdk_kit.dll ;;
      *) echo 'Unsupported native build platform' >&2; exit 1 ;;
    esac
    case "$(uname -m)" in
      arm64 | aarch64) architecture=arm64 ;;
      x86_64 | amd64) architecture=amd64 ;;
      *) echo 'Unsupported native build architecture' >&2; exit 1 ;;
    esac
    native=$harness/.runtime/native/$platform-$architecture
    if test "$mode" = native-build; then
      exec sh "$harness/native/turso/build.sh"
    fi
    if test -n "${SAILRY_TURSO_LIB:-}"; then
      test -f "$SAILRY_TURSO_LIB" ||
        { echo "SAILRY_TURSO_LIB must name an existing $library" >&2; exit 1; }
    else
      export SAILRY_TURSO_LIB=$native/$library
      test -f "$SAILRY_TURSO_LIB" || sh "$harness/native/turso/build.sh"
    fi
    if test "$mode" = native; then
      printf 'Turso runtime: %s\n' "$SAILRY_TURSO_LIB" >&2
      exit 0
    fi
    export SAILRY_HELPERS_DIR=${SAILRY_HELPERS_DIR:-$native/helpers}
    arguments=(run ./cmd/sailry-code-desktop)
    case "$name" in
      '') ;;
      dev) arguments+=(--data-dir "$HOME/.sailry-code-dev") ;;
      *) arguments+=(--data-dir "$runtime/profiles/$name") ;;
    esac
    exec go "${arguments[@]}" ;;
esac
if test $# -gt 0; then
  exec go "$@"
fi
