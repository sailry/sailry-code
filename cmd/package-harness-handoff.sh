#!/usr/bin/env bash
# Prepare (never upload) the one-time macOS assets understood by Harness v0.2.13.
set -euo pipefail
if [[ $# != 2 ]]; then echo 'Usage: bash cmd/package-harness-handoff.sh CODE_DISTRIBUTION_ARTIFACT_DIR NEW_OUTPUT_DIR' >&2; exit 2; fi
root=$(cd "$(dirname "$0")/.." && pwd)
source_dir=$(cd "$1" && pwd)
output=$2
harness=${SAILRY_HARNESS_DIR:?set to the pinned Harness checkout}
version=$(jq -er .version "$root/cmd/sailry-code-desktop/mygo.json")
[[ $version == 0.3.0 ]] || { echo 'The Harness handoff is only for Code 0.3.0' >&2; exit 1; }
[[ $(uname -s) == Darwin ]] || { echo 'Prepare the handoff on macOS for signature verification' >&2; exit 1; }
targets=(aarch64-apple-darwin x86_64-apple-darwin)
archives=()
for target in "${targets[@]}"; do
  archive="$source_dir/Sailry-$target-unsigned.zip"
  [[ -s $archive && -s "$source_dir/Sailry-$target.dmg" ]] || { echo "Missing Code distribution artifacts: $target" >&2; exit 1; }
  archives+=("$target=$archive")
done
# An incomplete run is not a release. Never overwrite an existing output directory.
mkdir "$output"
output=$(cd "$output" && pwd)
unset GOROOT
export GOTOOLCHAIN=go1.27.3 CGO_ENABLED=0 GOFLAGS=-mod=readonly GOWORK=off
go -C "$harness" run ./internal/desktop/updater/cmd/update-feed -version "$version" -output "$output" "${archives[@]}"
cd "$output"
for target in "${targets[@]}"; do
  cp "$source_dir/Sailry-$target.dmg" .
  shasum -a 256 "Sailry-$target.dmg" "Sailry-$target.zip" "Sailry-$target.zip.update.json" > "SHA256SUMS-desktop-$target"
done
