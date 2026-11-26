set shell := ["bash", "-euo", "pipefail", "-c"]

# List the independent Code development commands.
default:
    @just --list

# Prepare the exact Harness SDK pin without plugin/theme repositories or manifest changes.
bootstrap:
    bash dev.sh

# Run Code's default without --data-dir; dev uses ~/.sailry-code-dev, other names use .runtime/profiles/<name>.
[positional-arguments]
desktop name="":
    bash dev.sh desktop "$1"

# Prepare the required local Turso library, or use SAILRY_TURSO_LIB.
native:
    bash dev.sh native

# Rebuild Turso after changing its native source, ABI, pins or target.
native-build:
    bash dev.sh native-build

# Build the Code module with the selected workspace and read-only manifests.
build:
    bash dev.sh build ./...

# Run the Code module's Go tests; requires the native runtime.
test:
    bash dev.sh test ./...

# Vet the Code module with the selected workspace and read-only manifests.
vet:
    bash dev.sh vet ./...

# Run closed script fixtures without compiling Go or native code.
test-scripts:
    python3 -B -m unittest discover -s .github/scripts -p 'test_*.py'
