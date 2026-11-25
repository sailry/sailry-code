#!/usr/bin/env bash
set -euo pipefail

test -f go.mod && test -f cmd/sailry-code-desktop/mygo.json || { echo 'run from the Code repository root' >&2; exit 1; }
SAILRY_HARNESS_DIR=${SAILRY_HARNESS_DIR:-.runtime/harness}
test -f "$SAILRY_HARNESS_DIR/go.mod" || { echo 'SAILRY_HARNESS_DIR must name the pinned Harness checkout' >&2; exit 1; }
SAILRY_HARNESS_DIR=$(cd "$SAILRY_HARNESS_DIR" && pwd)
export SAILRY_HARNESS_DIR
export SAILRY_PRODUCT=code
export SAILRY_DESKTOP_DIR=cmd/sailry-code-desktop
export SAILRY_HOST_COMMAND=./cmd/sailry-code-host
export SAILRY_RELEASE_REPOSITORY=https://github.com/sailry/sailry-code
version=$(jq -er .version cmd/sailry-code-desktop/mygo.json)
export SAILRY_PACKAGE_LDFLAGS="${SAILRY_PACKAGE_LDFLAGS:-} -X github.com/sailry/sailry-harness/internal/link.ProductVersion=$version"
