#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$root"
source cmd/package-env.sh
exec bash "$SAILRY_HARNESS_DIR/internal/node/host/deployment/archive.sh" "$@"
