#!/usr/bin/env bash
# Capture the committed SSH/terminal plugin baseline and the native pages with
# the same fixtures, then compare them. Each run writes only to a new mktemp
# directory: git-archive copies of the pinned trees, pinned plugin sources, a
# temporary HOME and loopback-only SSH and model endpoints. Nothing that
# existed before the run is modified or removed.
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage: internal/visualparity/run.sh [options]

Baseline (plugin packages on the pinned Code commit):
  --base-ref REF         Code commit (default e22c21086fae5502ddacd288670979e653539964)
  --base-harness DIR     Harness checkout at that commit's pin (default .runtime/harness)
  --plugins-repo DIR     Repository holding the ssh, commands and databases packages;
                         required for plugin captures
  --plugins-ref REF      Their commit (default 82622995df2d0a49dd317797daedd93ea3eb3855)
  --base-capture DIR     Reuse an earlier base capture instead of capturing it

Head (native pages):
  --head-harness DIR     Harness repository (default .runtime/harness)
  --head-harness-ref REF Archive this Harness commit instead of using DIR's
                         working tree, e.g. 5645c01f1
  --head-code-ref REF    Archive this Code commit instead of using the
                         working tree (this package is copied in)
  --head-code-overlay P  Comma-separated working-tree paths copied over the
                         archived Code commit, for an uncommitted change
  --head-mode MODE       native (default), or plugin to compare the baseline
                         with itself as a determinism check

Common:
  --out DIR              Parent of the new run directory (default $TMPDIR)
  --only PREFIXES        Comma-separated state prefixes, e.g. terminal-,ssh-01
  --locales LIST         Default zh-CN,en
  --themes LIST          Default light,dark
  --size WxH             Default 1280x820
  --scale N              Default 1
EOF
  exit 2
}

code=$(cd "$(dirname "$0")/../.." && pwd)
base_ref=e22c21086fae5502ddacd288670979e653539964 plugins_ref=82622995df2d0a49dd317797daedd93ea3eb3855
plugins_repo= base_capture= base_harness=$code/.runtime/harness head_harness=$code/.runtime/harness head_ref= head_code_ref= head_code_overlay=
head_mode=native parent=${TMPDIR:-/tmp}
export SAILRY_PARITY_ONLY=${SAILRY_PARITY_ONLY:-} SAILRY_PARITY_LOCALES=${SAILRY_PARITY_LOCALES:-zh-CN,en}
export SAILRY_PARITY_THEMES=${SAILRY_PARITY_THEMES:-light,dark} SAILRY_PARITY_SIZE=${SAILRY_PARITY_SIZE:-1280x820} SAILRY_PARITY_SCALE=${SAILRY_PARITY_SCALE:-1}
while test $# -gt 0; do
  test $# -ge 2 || usage
  case $1 in
    --base-ref) base_ref=$2 ;;
    --base-harness) base_harness=$2 ;;
    --plugins-repo) plugins_repo=$2 ;;
    --plugins-ref) plugins_ref=$2 ;;
    --base-capture) base_capture=$2 ;;
    --head-harness) head_harness=$2 ;;
    --head-harness-ref) head_ref=$2 ;;
    --head-code-ref) head_code_ref=$2 ;;
    --head-code-overlay) head_code_overlay=$2 ;;
    --head-mode) head_mode=$2 ;;
    --out) parent=$2 ;;
    --only) SAILRY_PARITY_ONLY=$2 ;;
    --locales) SAILRY_PARITY_LOCALES=$2 ;;
    --themes) SAILRY_PARITY_THEMES=$2 ;;
    --size) SAILRY_PARITY_SIZE=$2 ;;
    --scale) SAILRY_PARITY_SCALE=$2 ;;
    *) usage ;;
  esac
  shift 2
done
case $head_mode in native | plugin) ;; *) usage ;; esac
if test -z "$base_capture" || test "$head_mode" = plugin; then
  test -n "$plugins_repo" || { echo 'Plugin captures need --plugins-repo' >&2; exit 2; }
fi
if test -n "$base_capture"; then
  test -f "$base_capture/.sailry-visual-parity-capture" || { echo "$base_capture is not a visual parity capture" >&2; exit 1; }
  base_capture=$(cd "$base_capture" && pwd)
fi
base_harness=$(cd "$base_harness" && pwd) head_harness=$(cd "$head_harness" && pwd)
test -d "$parent" || { echo "--out $parent does not exist" >&2; exit 1; }
out=$(mktemp -d "$(cd "$parent" && pwd)/sailry-visual-parity.XXXXXX")
touch "$out/.sailry-visual-parity-run"
echo "Run directory: $out" >&2

# The base commit must be built against the Harness revision it pins.
version=$(git -C "$code" show "$base_ref:go.mod" | sed -n 's/^[[:space:]]*github.com\/sailry\/sailry-harness \(v[^[:space:]]*\)$/\1/p')
pin=${version##*-}
actual=$(git -C "$base_harness" rev-parse --short=12 HEAD)
test -n "$pin" && test "$pin" = "$actual" ||
  { echo "Base Harness $base_harness is at $actual; $base_ref pins ${pin:-an unknown revision}" >&2; exit 1; }
test -z "$(git -C "$base_harness" status --porcelain --untracked-files=no)" ||
  { echo "Base Harness $base_harness has local changes" >&2; exit 1; }

library() {
  for candidate in "$1"/.runtime/native/{darwin-arm64,darwin-amd64}/libturso_sync_sdk_kit.dylib "$1"/.runtime/native/{linux-amd64,linux-arm64}/libturso_sync_sdk_kit.so; do
    if test -f "$candidate"; then echo "$candidate"; return; fi
  done
  echo "No staged Turso library under $1/.runtime/native; run ./dev.sh native-build" >&2
  return 1
}
base_library=$(library "$base_harness")
head_library=$(library "$head_harness" 2>/dev/null || echo "$base_library")

# Workspace builds still read the pinned Harness version's go.mod. Serve it
# from the base checkout through a file proxy; go.sum verifies it, and every
# other module must already be in the module cache: no network is used.
proxy=$out/goproxy/github.com/sailry/sailry-harness/@v
mkdir -p "$proxy"
git -C "$base_harness" show HEAD:go.mod >"$proxy/$version.mod"
printf '{"Version":"%s"}\n' "$version" >"$proxy/$version.info"
printf '%s\n' "$version" >"$proxy/list"
GOCACHE=$(go env GOCACHE) GOMODCACHE=$(go env GOMODCACHE)
export GOCACHE GOMODCACHE GOTOOLCHAIN=go1.27.3 CGO_ENABLED=0 GOFLAGS=-mod=readonly GOPROXY=file://$out/goproxy GONOSUMDB=github.com/sailry GOSUMDB=off
unset GOROOT
mkdir -p "$out/home"
export HOME=$out/home

workspace() { # code harness file
  printf 'go 1.27.3\n\nuse (\n\t%s\n\t%s\n)\n' "$1" "$2" >"$3"
  if test -f "$code/.runtime/go.work.sum"; then cp "$code/.runtime/go.work.sum" "$3.sum"; fi
}

# The baseline tree is the pinned commit plus this package; the comparison
# also builds there, so it works while the native side does not compile.
mkdir -p "$out/base-src"
git -C "$code" archive "$base_ref" | tar -x -C "$out/base-src"
mkdir -p "$out/base-src/internal/visualparity"
cp "$code"/internal/visualparity/*_test.go "$out/base-src/internal/visualparity/"
echo "Base Code $(git -C "$code" rev-parse --short=12 "$base_ref"), Harness $actual" >&2

if test -n "$plugins_repo"; then
  mkdir -p "$out/plugins"
  git -C "$plugins_repo" archive "$plugins_ref" ssh commands databases | tar -x -C "$out/plugins"
  echo "Plugins ssh, commands and databases at $(git -C "$plugins_repo" rev-parse --short=12 "$plugins_ref")" >&2
fi

head_tree=$head_harness
if test -n "$head_ref"; then
  head_tree=$out/head-harness
  mkdir -p "$head_tree"
  git -C "$head_harness" archive "$head_ref" | tar -x -C "$head_tree"
  echo "Head Harness $(git -C "$head_harness" rev-parse --short=12 "$head_ref") (archived)" >&2
else
  echo "Head Harness working tree $head_harness at $(git -C "$head_harness" rev-parse --short=12 HEAD), local changes included" >&2
fi
head_code=$code
if test -n "$head_code_ref"; then
  head_code=$out/head-src
  mkdir -p "$head_code/internal/visualparity"
  git -C "$code" archive "$head_code_ref" | tar -x -C "$head_code"
  cp "$code"/internal/visualparity/*_test.go "$head_code/internal/visualparity/"
  echo "Head Code $(git -C "$code" rev-parse --short=12 "$head_code_ref") (archived)" >&2
  IFS=, read -r -a overlay <<<"$head_code_overlay,"
  for path in "${overlay[@]}"; do
    test -n "$path" || continue
    case $path in /* | *..*) echo "Overlay paths are relative to the Code tree: $path" >&2; exit 2 ;; esac
    mkdir -p "$head_code/$(dirname "$path")"
    cp -R "$code/$path" "$head_code/$path"
    echo "  overlaid working-tree $path" >&2
  done
else
  test -z "$head_code_overlay" || { echo '--head-code-overlay needs --head-code-ref' >&2; exit 2; }
  echo "Head Code working tree $code at $(git -C "$code" rev-parse --short=12 HEAD), local changes included" >&2
fi

status=0
capture() { # name tree harness library mode
  local name=$1 tree=$2 harness=$3 lib=$4 mode=$5 plugins=
  if test "$mode" = plugin; then plugins=$out/plugins; fi
  workspace "$tree" "$harness" "$out/$name.work"
  mkdir -p "$out/$name"
  touch "$out/$name/.sailry-visual-parity-capture"
  echo "Capturing $name ($mode)" >&2
  (cd "$tree" && GOWORK=$out/$name.work SAILRY_TURSO_LIB=$lib SAILRY_PARITY_OUT=$out/$name SAILRY_PARITY_PLUGINS=$plugins \
    SAILRY_PARITY_FIXTURE=$out/fixture go test -tags visualparity -run '^TestCapture$' -count=1 -timeout 60m ./internal/visualparity) ||
    { status=1; echo "Capture $name reported failures; its failed and skipped states are in the report" >&2; }
}

base=$base_capture
if test -z "$base"; then
  capture base "$out/base-src" "$base_harness" "$base_library" plugin
  base=$out/base
fi
capture head "$head_code" "$head_tree" "$head_library" "$head_mode"

workspace "$out/base-src" "$base_harness" "$out/compare.work"
(cd "$out/base-src" && GOWORK=$out/compare.work SAILRY_PARITY_BASE=$base SAILRY_PARITY_HEAD=$out/head SAILRY_PARITY_REPORT=$out/report \
  go test -tags visualparity -run '^TestCompare$' -count=1 ./internal/visualparity) || status=1
echo "Report: $out/report/report.md" >&2
exit $status
