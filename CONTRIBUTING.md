# Contributing to Sailry Code

Read [ARCHITECTURE.md](ARCHITECTURE.md) and [AGENTS.md](AGENTS.md) before making
changes. The Go module is `github.com/sailry/sailry-code`, built on the pinned
`github.com/sailry/sailry-harness` SDK.

## Where changes belong

Most core development happens upstream in **[Sailry Harness](https://github.com/sailry/sailry-harness)** (see Harness's `CONTRIBUTING.md` and `AGENTS.md`). Contribute there for:

- Core agent runtime, tool execution/sandboxing, approvals, model routing, and storage
- P2P Link v2 networking, relays, pairing, and Client state recovery
- Desktop Kit/shell, Flutter Mobile core (`sailry_mobile` / `sailry_bridge`), general modules, and Go/Flutter/Plugin SDKs

Contribute here in **`sailry-code`** for:

- Code's 6 native developer modules (`modules/code/*`: `files`, `git`, `worktrees`, `terminal`, `databases`, `ssh`)
- Product identity, `~/.sailry-code` / legacy migration (`internal/product`), and Code's Mobile views (`apps/mobile`)
- Desktop, Host, and Mobile packaging, signing, and end-user releases (`cmd/*`, `.github/`)

## Setup

### Prerequisites

- **Go 1.27.3**, **Git**, **Bash**, and **jq**.
- **Rust (`rustup` + system C linker)** for building the local FTS-enabled Turso
  shared library (toolchain version pinned in `<harness>/native/turso/rust-toolchain.toml`).
- **Flutter** matching `$SAILRY_HARNESS_DIR/apps/mobile/.flutter-version` (plus
  Android NDK or Xcode) for `apps/mobile/`.
- **Python 3** for closed workflow/bootstrap script unit tests (`just test-scripts`).
- **Linux runtime sandboxes** require system Bubblewrap (`--bind-fd` / `--ro-bind-fd`),
  `openat2` (Linux 5.6+), user namespaces, and `git`.

Code does **not** depend on `sailry-plugins` or `sailry-themes` checkouts; new
profiles start with zero preinstalled plugins or themes.

### Option A: Independent bootstrap (default)

`dev.sh` and `justfile` prepare the exact Harness SDK commit pinned by `go.mod`
into `.runtime/harness` (shallow detached checkout, no submodules) and write
`.runtime/go.work` (first setup needs network access):

```sh
just bootstrap
(cd .runtime/harness/native/turso && rustup toolchain install)
just native
```

Verify the staged Turso library (`file` and `otool -L` on macOS, or `ldd` on
Linux), then launch Code Desktop:

```sh
just desktop               # Default profile ~/.sailry-code (preserves ~/.sailry migration)
just desktop dev           # Isolated Code dev profile ~/.sailry-code-dev
just desktop native-smoke  # Isolated checkout profile .runtime/profiles/native-smoke
```

Run `just desktop native-smoke` twice and confirm the window and history open
without loader errors. `just desktop` serves Go profiles on
`http://127.0.0.1:6061/debug/pprof/` by default (override with
`SAILRY_PPROF=<loopback-addr>` or disable with `SAILRY_PPROF=`). Use
`just desktop <fixed-name>` for routine manual tests instead of touching `~/.sailry-code`.

### Option B: Co-development with a local Sailry Harness checkout

When developing changes across both `sailry-harness` and `sailry-code`, create a
root `go.work` (gitignored) referencing both local checkouts:

```sh
unset GOROOT
export GOTOOLCHAIN=go1.27.3 CGO_ENABLED=0
go work init . ../sailry-go
```

With a root `go.work`, `just build`, `just vet`, `just test`, `just native`, and
`just desktop` use the local Harness checkout. The last two commands stage or
reuse its Turso library. Keep `go.work` and `go.work.sum` local.

## Layout

| Path | Responsibility |
| --- | --- |
| `cmd/sailry-code-desktop` | Desktop entry point, `mygo.json` metadata (`0.1.0`, `ai.sailry.desktop`), macOS/Windows packaging wrappers |
| `cmd/sailry-code-host` | Headless Host entry point and archive packaging wrapper |
| `cmd/update-feed` | Ed25519 update feed signer for macOS and Windows Desktop archives |
| `cmd/package-env.sh` | Shared packaging environment (`SAILRY_PRODUCT=code`, release repo, `ProductVersion` ldflags) |
| `internal/product` | Product identity, release URLs, `~/.sailry-code` / `~/.sailry` defaults, update public key |
| `internal/assembly/codedesktop` | Combines Code's 6 Desktop modules with `sdk/desktop/harness.Modules()` |
| `internal/assembly/codehost` | Combines `sdk/native/node.HarnessAssembly()` (empty `BundledDefaults`) with Code's 5 Node modules |
| `modules/code/{databases,files,git,ssh,terminal,worktrees}` | Code-owned Desktop, Node, and backend service packages |
| `apps/mobile` | Flutter controller (`sailry_code_mobile`), Code project views (`git`, `files`), native FFI bridge |
| `internal/testsupport`, `internal/visualparity` | Shared Go test fixtures and visual parity captures against legacy plugin baselines |
| `docs/` | Code user guides |
| `.github/` | CI, Release, Mobile workflows, composite actions, and Python/shell script test suites |

## Checks

All `just` and `dev.sh` commands enforce `unset GOROOT`, `GOTOOLCHAIN=go1.27.3`,
`CGO_ENABLED=0`, and `GOFLAGS=-mod=readonly` (read-only `go.mod`/`go.sum`).

### Standard Go and script checks

```sh
just build          # ./dev.sh build ./...
just vet            # ./dev.sh vet ./...
just test           # ./dev.sh test ./... (requires the staged Turso runtime)
just test-scripts   # python3 -B -m unittest discover -s .github/scripts -p 'test_*.py'
bash -n dev.sh
just --fmt --check
```

For storage tests, run `just native` and set `SAILRY_TURSO_LIB` to the library
path it prints before running `just test`.

### Cross-OS vet, race detector, integration & parity

Matches `.github/workflows/ci.yml`:

```sh
# Cross-OS vet including integration-tagged test code:
unset GOROOT && export GOTOOLCHAIN=go1.27.3 CGO_ENABLED=0 GOWORK="$PWD/.runtime/go.work" GOFLAGS=-mod=readonly
for goos in linux windows darwin; do GOOS=$goos go vet -tags integration ./...; done

# Race detector on concurrency-heavy Code modules (requires cgo + SAILRY_TURSO_LIB):
CGO_ENABLED=1 go test -race -p 2 -parallel 4 \
  ./modules/code/ssh/service ./modules/code/databases/service \
  ./modules/code/git/service ./modules/code/terminal/node ./modules/code/worktrees/node

# SSH and database integration tests (require PostgreSQL 17, MySQL 8.4, and SSH fixtures):
go test -p 1 -timeout 20m -tags integration \
  ./modules/code/ssh/service ./modules/code/databases/service

# Visual parity captures against legacy baseline fixtures:
bash internal/visualparity/run.sh
```

### Mobile checks

```sh
sh apps/mobile/tool/prepare.sh
cd apps/mobile
flutter analyze
flutter test --concurrency=4
```

Both `go.mod` and `apps/mobile/pubspec.yaml` (plus `pubspec.lock`) must pin the
exact same `sailry-harness` commit SHA; do not add `apps/mobile/pubspec_overrides.yaml`.

## Continuous integration

- **`.github/workflows/ci.yml`** runs on pull requests, pushes to `main`, manual
  dispatch (with optional `integration: true`), and `workflow_call` from `release.yml`:
  - `scope` uses `.github/scripts/ci-scope.sh` to skip Go/package jobs when only
    non-Go docs/licenses change.
  - `checks` runs `test_workflows.py` / `test_bootstrap.py` / `test_mobile_native.py`,
    Linux Bubblewrap setup, `gofmt`, cross-OS `go vet -tags integration ./...`,
    `go test ./...`, and `-race` tests across the 5 Code service/node packages.
  - `native` stages Turso, Capture, and Windows CRT (`SHA256SUMS`) for `linux-arm64`,
    `darwin-arm64`, `darwin-amd64`, and `windows-amd64`; `platform-tests` validates
    OS-sensitive Code modules on macOS ARM64 and Windows x64.
  - `macos` builds ARM64/Intel DMGs, unsigned ZIPs, and Host archives (`ad-hoc` on PRs,
    Developer ID + notarization when `distribution: true`); `windows` cross-builds
    NSIS installer + portable ZIP on Ubuntu; `host-linux` builds x64 and ARM64 Host tarballs.
  - Optional `integration` and `repository-integration` run live Postgres 17, MySQL 8.4,
    SSH, and multi-OS Git/worktree suites.
- **`.github/workflows/mobile.yml`** runs **only via `workflow_dispatch`** (never on
  PRs or `main`). It runs `flutter analyze` and `flutter test`. Package builds produce
  an Android ARM64 APK or an unsigned iOS app. With uploads enabled, all seven
  signing secrets, and the selected platform’s distribution settings, Android
  publishes the signed APK as a GitHub Actions artifact. iOS exports a signed IPA
  as an artifact and uploads it to TestFlight.

## Release packages

Desktop/Host releases trigger automatically on `v*` tags via `.github/workflows/release.yml`:

1. Keep `internal/product/identity.go` (`ID = "code"`, `Version`) and
   `cmd/sailry-code-desktop/mygo.json` (`.version`) equal to the `v<Version>` tag.
2. `ci.yml` (`distribution: true`) produces signed macOS DMGs/ZIPs, macOS/Linux Host
   tarballs, and Windows x64 setup/portable ZIPs.
3. `./cmd/update-feed` signs Ed25519 `update-v1` manifests (`*.zip.update.json`) for all
   three Desktop targets (`aarch64-apple-darwin`, `x86_64-apple-darwin`,
   `x86_64-pc-windows-msvc`) using `SAILRY_UPDATE_SIGNING_KEY`.
4. `install-host.sh` is verified (`task_repository=https://github.com/sailry/sailry-code`),
   checksums are generated, and assets are uploaded to a **draft** GitHub release
   without modifying existing differing assets or published releases.

## Updating Harness

When advancing the pinned `sailry-harness` revision:

1. Update `github.com/sailry/sailry-harness` in `go.mod` / `go.sum` AND the matching
   exact commit `ref` in `apps/mobile/pubspec.yaml` + `apps/mobile/pubspec.lock`.
2. Move any obsolete `.runtime/harness` and `.runtime/go.work` aside before running
   `just bootstrap` (since `dev.sh` refuses to overwrite a mismatched checkout automatically).
3. Run `just test-scripts`, `just vet`, `just test`, and Mobile checks (`sh apps/mobile/tool/prepare.sh && cd apps/mobile && flutter analyze && flutter test`).
