# Sailry Code engineering rules

Read [ARCHITECTURE.md](ARCHITECTURE.md) for the implemented Code system and
[CONTRIBUTING.md](CONTRIBUTING.md) for bootstrap, validation and release
commands. Framework contracts and core runtime rules belong to upstream
[Sailry Harness](https://github.com/sailry/sailry-harness).

## Scope and Harness boundary

- Implement the requested work and fix defects found within that scope. Optional
  improvements, new features and new validation gates are not completion criteria.
- Use current code, schemas and accepted behavior as the baseline. Historical
  implementations and internal plans explain provenance; they do not constrain a
  new implementation or authorize restoring removed UI.
- The Go module is `github.com/sailry/sailry-code` and the Flutter package is
  `sailry_code_mobile`. Sailry Code owns its product identity (`internal/product`),
  Desktop/Host/Mobile assemblies (`internal/assembly/*`, `apps/mobile/lib/assembly/`),
  and the six developer modules under `modules/code/`.
- Depend one-way on `github.com/sailry/sailry-harness` solely through its public
  `sdk/*` Go facades and `apps/mobile` Dart packages (`sailry_mobile`, `sailry_bridge`).
  Never import Harness `internal/*` packages, fork Harness internals into this
  repository, or add `replace` directives for `sailry-harness` in `go.mod` or
  `apps/mobile/pubspec_overrides.yaml`.
- Keep the Harness revision synchronized: `go.mod` and `apps/mobile/pubspec.yaml`
  (with `pubspec.lock`) must pin the exact same `sailry-harness` Git commit SHA
  (enforced by `.github/actions/mobile/action.yml`).
- Sailry Code starts with **zero preinstalled plugins or themes**. Keep
  `internal/assembly/codehost/defaults.go` empty; `sailry-plugins` and
  `sailry-themes` are independent external repositories and never build dependencies.
- Keep architecture documentation consistent with code. Explain a non-obvious
  ownership decision where it applies, without copying plans into source comments.

## Ownership and module design

- Harness Node owns execution, admission, credentials and canonical history;
  Client owns projection and recovery; Desktop/Flutter own presentation and drafts.
  Code modules extend Node, Desktop and Mobile strictly through Harness's
  registration contracts (`sdk/native/node`, `sdk/desktop/native`, `sdk/desktop/view`,
  `sailry_mobile` `MobileAssembly`).
- Keep entry points (`cmd/sailry-code-desktop`, `cmd/sailry-code-host`,
  `apps/mobile/lib/main.dart`) thin; isolate product identity in
  `internal/product` and assembly wiring in `internal/assembly/*`.
- Organize each developer feature under `modules/code/<name>/{desktop,node,service}`
  with tests and fixtures beside the code they test. Keep shared Go test helpers
  scoped to `internal/testsupport/`.
- Keep `internal/product/identity.go` (`ID`, `Version`) and
  `cmd/sailry-code-desktop/mygo.json` (`.version`) synchronized; `release.yml`
  enforces exact parity with the release tag `v<Version>`.
- Preserve imported source notices and licenses.

## Git and workspace

- Check `git status` first. Preserve other work and stage only your changes. Make
  cohesive local commits with imperative English subjects. Push or open a pull
  request only when requested.
- Do not modify `dev.sh --help` line count/required phrases or add `import` to
  `justfile` without updating `.github/scripts/test_bootstrap.py`.
- For co-development against an unpublished local `sailry-harness` checkout,
  create a gitignored root `go.work` referencing `.` and your local Harness
  checkout (`dev.sh` uses it automatically). Never commit `go.work`, `go.work.sum`,
  `.runtime/`, `.preview/`, profiles, credentials, native binaries, build output,
  or signing keys/material.

## Environment and validation

- Every Go command uses `unset GOROOT` and
  `export GOTOOLCHAIN=go1.27.3 CGO_ENABLED=0 GOFLAGS=-mod=readonly` (or runs via
  `dev.sh` / `just`, which sets them and manages `.runtime/go.work` and
  `.runtime/harness` without overwriting modified or branched checkouts).
- Keep `go.mod`, `go.sum`, `apps/mobile/pubspec.yaml`, and `apps/mobile/pubspec.lock`
  read-only during routine commands (`dev.sh` rejects `-mod` / `-modfile`).
- Validate changed behavior with [CONTRIBUTING.md](CONTRIBUTING.md#checks):
  `just build`, `just vet`, `just test` (with `SAILRY_TURSO_LIB`), `just test-scripts`,
  `bash -n dev.sh`, `just --fmt --check`, cross-OS `go vet -tags integration ./...`,
  and `-race` checks for concurrency-sensitive `modules/code/*` packages.
- Mobile checks require `sh apps/mobile/tool/prepare.sh`, `flutter analyze`, and
  `flutter test --concurrency=4` with Flutter matching `$SAILRY_HARNESS_DIR/apps/mobile/.flutter-version`.
  Desktop tests do not prove Flutter or physical-device behavior.
- Tests use `t.TempDir()`. Manual development runs use
  `just desktop <fixed-name>` (`.runtime/profiles/<fixed-name>`). Code acceptance
  uses `just desktop dev` (`~/.sailry-code-dev`); the default profile is
  `~/.sailry-code` (migrating stopped `~/.sailry` on first default launch).

## UI and product copy

- Preserve accepted shell layout, columns, widths, and shared components from
  `sdk/desktop/controls` and `sdk/desktop/theme`. Modules own page/panel content
  and release listeners, watches and buffers when unmounted or closed.
- Keep `modules/code/<name>/desktop/locales/*.yml` aligned with all locale keys
  registered by that module, and keep user-facing copy short and actionable.
- Source, comments, logs, diagnostics and public docs are English (except
  `README.zh-CN.md`); user reports are Chinese.

## Security and data protection

- Enforce real boundaries inside each Code service:
  - **Databases**: enforce SQL/row/result bounds (`64 KiB` SQL, `10_000` rows,
    `512 KiB` output), force `database_query` read-only, gate `database_execute`
    under session permissions, block SQLite files inside Node storage, and keep
    passwords/keys out of agent payloads.
  - **SSH**: verify pinned host keys and use connection IDs without exposing
    secrets or `HostPath` / agent sockets across Nodes.
  - **Terminal**: reject reserved environment variables (`SAILRY_*`, `DMUX_*`,
    `LD_*`, `DYLD_*`, `*_PRELOAD`, shell startup hooks) in saved settings.
  - **Profiles**: never delete or overwrite user profiles; rely on Harness's
    lock-checked, journaled `~/.sailry` → `~/.sailry-code` migration only when
    `--data-dir` is omitted.
