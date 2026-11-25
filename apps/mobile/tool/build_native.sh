#!/bin/sh
set -eu

root=${SAILRY_HARNESS_ROOT:?Run tool/prepare.sh before building Sailry Code Mobile}
exec sh "$root/apps/mobile/tool/build_native.sh" "$@"
