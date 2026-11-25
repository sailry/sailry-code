#!/bin/sh
set -eu

mobile=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
root=${SAILRY_HARNESS_ROOT:-$mobile/.dart_tool/harness}
export SAILRY_HARNESS_ROOT=$root
exec sh "$root/apps/mobile/tool/build_native.sh" "$@"
