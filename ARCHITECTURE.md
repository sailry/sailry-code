# Architecture

**Sailry Code** (`github.com/sailry/sailry-code`) is a downstream open-source AI developer workstation built as a single-direction product assembly over [Sailry Harness](https://github.com/sailry/sailry-harness). Engineering rules live in [AGENTS.md](AGENTS.md) and build/release procedures in [CONTRIBUTING.md](CONTRIBUTING.md).

## System context

```text
cmd/sailry-code-desktop ─▶ codedesktop.Assembly() ─▶ Harness sdk/desktop + 7 Code desktop modules
                         └▶ codehost.Assembly()    ─▶ Harness sdk/native/node + 6 Code node modules

cmd/sailry-code-host    ─▶ codehost.Assembly()    ─▶ Headless Host runtime + Code node modules

apps/mobile (Flutter)   ─▶ codeAssembly           ─▶ Harness sailry_mobile + sailry_bridge (Dart FFI)
                                                   + Code git & files project resources
```

Sailry Code depends one-way on `github.com/sailry/sailry-harness` through its public `sdk/*` Go packages and Flutter packages (`sailry_mobile`, `sailry_bridge`):

- **`go.mod` and `apps/mobile/pubspec.yaml`** pin the exact same `sailry-harness` Git commit SHA (verified by `.github/actions/mobile/action.yml`).
- **Node execution, Link v2 P2P transport, Client projections, Shell/Kit, and general modules** (browser, computer use, files core, memory, OCR, web search, scheduled tasks, reminders, statistics, provider status) come from Harness via `sdk/native/node.HarnessAssembly()` and `sdk/desktop/harness.Modules()`.
- **Code adds compile-time native developer modules** under `modules/code/*`, product identity under `internal/product`, Mobile views under `apps/mobile`, and end-user Desktop/Host/Mobile releases.
- **Zero preinstalled plugins or themes**: `internal/assembly/codehost/defaults.go` returns an empty `[]node.DefaultPlugin{}`. Users install optional packages from [sailry-plugins](https://github.com/sailry/sailry-plugins) and themes from [sailry-themes](https://github.com/sailry/sailry-themes) at runtime.

## Repository layout and assemblies

| Component | Responsibility |
| --- | --- |
| `cmd/sailry-code-desktop` | Desktop binary (`mygo.json` v`0.1.0`, `ai.sailry.desktop`, macOS/Windows packaging scripts) |
| `cmd/sailry-code-host` | Headless `sailry-host` binary and Host archive packaging wrapper |
| `cmd/update-feed` | Ed25519 Desktop update catalog signer using `sdk/release.PublishDesktop` |
| `cmd/package-env.sh` | Packaging environment (`SAILRY_PRODUCT=code`, release repository, `ProductVersion` ldflags) |
| `internal/product` | Product identity (`ID="code"`, `~/.sailry-code`, `~/.sailry` legacy migration, embedded update public key) |
| `internal/assembly/codedesktop` | Combines Code's 7 Desktop modules with `sdk/desktop/harness.Modules()` |
| `internal/assembly/codehost` | Combines `sdk/native/node.HarnessAssembly()` with Code's 6 Node modules |
| `modules/code/*` | Native Code Desktop, Node, and backend service implementations |
| `apps/mobile` | Flutter controller (`sailry_code_mobile`), `codeAssembly`, Code `git` & `files` project resources |
| `internal/testsupport`, `internal/visualparity` | Shared Go test fixtures and visual parity comparisons against legacy plugin baselines |

## Native developer modules (`modules/code/`)

Each Code module registers directly with Harness host contracts in Go (no JS VM callback overhead for Node tool execution) and declares legacy plugin replacements so historical transcripts and user layouts transition cleanly:

| Module | Replaces | Desktop (`codedesktop`) | Node (`codehost`) & Backend Service |
| --- | --- | --- | --- |
| `code-index` | `code` | Conversation settings (`ExecutionSettings`: `enabled`, `persistent` switches via `read/save_code_index_settings`) | `codeindex.Module()` (`sdk/native/codeindex.Service`), `gotreesitter` parser + BLAKE3 cache, 6 tools (`code_search`, `code_outline`, `code_context`, `code_impact`, `code_map`, `code_pipeline`) |
| `databases` | `databases` | `page:databases` (default-pinned, `moejs` view, `database` link handler) | `databasesnode.Module()` (`DatabaseServiceProvider`), MySQL (`go-sql-driver/mysql`), PostgreSQL (`pgx/v5`), SQLite (`modernc.org/sqlite`), optional SSH tunnel, 5 tools (`database_catalog`, `database_query`, `database_execute`, `database_connections`, `database_save_connection`) |
| `files` | `files` / `files-workspace` | `native.FilesPage` (default-pinned), `native.Files` conversation panel (`Cmd+P`), file tree/search/tabs/trash/transfers, `file`/`directory` links | Uses Harness's `modules/files/node` included via `HarnessAssembly()` |
| `git` | `git` | `page:git` (`Ctrl+Shift+G`, default-pinned, checkpoint history), project context menu, replaces `git` slot controls (`branch`, `manage`, `changes`) | `gitnode.Module()` (`WorkspaceServiceProvider{Git: true}`), native Git + `go-git/v5` engine, repo watch, `clone` preparer, hunk checkpoints, workspace commands, 7 read-only tools (`git_status`, `git_diff`, `git_log`, `git_show`, `git_blame`, `list_git_changes`, `read_git_change`) |
| `ssh` | `ssh` | `page:ssh` (default-pinned, `moejs` view), SFTP transfer worker, session tabs, `ssh` link handler | `sshnode.Module()` (`SSHServiceProvider`), `x/crypto/ssh` + `pkg/sftp` pool, remote command & file transfers, remote `sailry-host` install/update, 4 tools (`ssh_run`, `ssh_transfer`, `ssh_connections`, `ssh_save_connection`) |
| `terminal` | `commands` slots | Registered main-workspace terminal pane for project rows and header tabs, `native.Terminal` panel (`Ctrl+\``), project "new terminal" action, `settings.Terminal` page, replaces `commands` terminal slots (`terminal-cli`, `terminal-menu-new`, `terminal-menu-cli`) | `terminalnode.Module()` (`TerminalServiceProvider` via `node.NewTerminalService`) + embedded `python` skill (`$SAILRY_PYTHON`, Python 3.12.13) |
| `worktrees` | `worktrees` | Project/conversation worktree workflows (`worktree_create`, `worktree_manage`, `worktree_fork_from`, branch forks), replaces `worktrees` controls (`location`, `manage`, `create`, `fork`) | `worktreesnode.Module()` (`WorkspaceServiceProvider{Worktrees: true}`), managed `.sailry/worktrees/<id>` lifecycle & cleanup, 5 workspace commands, 4 tools (`list_worktrees`, `create_worktree`, `register_worktree`, `remove_worktree`) |

### Service boundaries and resource budgets

- **Code Index**: Live scans inspect workspace files under ignore/confinement rules; persistent per-worktree indexes store derived declarations/references/imports in Turso under the profile cache. Scans cap worktrees at `maxFiles = 50_000`, share a bounded parser permit, retain at most `grammarCacheLimit = 8` decoded `gotreesitter` grammar tables in memory (purged when idle), and close inactive worktree handles/watches after `idleRelease = 2m`.
- **Databases**: Enforces `64 KiB` max SQL, `10,000` max rows, `512 KiB` max serialized response, and `3,600,000 ms` max timeout. `database_query` forces read-only execution; `database_execute` goes through session permission checks and honors read-only profiles. SQLite paths inside the Node storage directory are rejected; credentials remain isolated on the owning Node.
- **Terminal**: Settings reject reserved environment variables (`SAILRY_*`, `DMUX_*`, `LD_*`, `DYLD_*`, `*_PRELOAD`, `TERM`, `PWD`, `BASH_ENV`, `ENV`, `ZDOTDIR`, `IFS`, `NODE_OPTIONS`, `PERL5OPT`, `PYTHONSTARTUP`).
- **Git & Worktrees**: Managed worktrees isolate parallel conversations/subagents; Git operations preserve worktree locks, revision checks, and sandbox constraints inherited from Harness.
- **SSH**: Enforces host-key verification and `HostPath` Node ownership across remote commands, SFTP transfers, and `sailry-host` deployment.

## Product identity, profiles, and legacy migration

- **Identity (`internal/product/identity.go`)**: Sets `ID = "code"`, `Name = "Sailry Code"`, `Version = "0.1.0"`, and preserves installed package compatibility (`ai.sailry.desktop`, `sailry-desktop`, and `product.CodeHost()` keeping `sailry-host` / `~/.sailry-host` service layout while switching release feeds to `sailry/sailry-code`).
- **Profiles**: Default Desktop and foreground Host profile is `~/.sailry-code`; `just desktop dev` uses `~/.sailry-code-dev`; `just desktop <name>` uses `.runtime/profiles/<name>`.
- **Crash-safe legacy adoption**: With `LegacyProfile = "~/.sailry"`, starting Desktop without `--data-dir` when `~/.sailry-code` is absent and `~/.sailry` is stopped runs Harness's journaled migration (`~/..sailry-code.legacy-profile/`), relocating stored profile paths, keeping database backups, and repairing linked Git worktree metadata.

## Mobile architecture (`apps/mobile`)

- Package `sailry_code_mobile` (`0.2.13+31`) shares Harness's Go C ABI bridge (`sailry_bridge`) prepared by `apps/mobile/tool/prepare.sh` and `tool/build_native.sh`.
- `lib/assembly/code.dart` mounts Harness's core pages (`chat`, `hosts`, `projects`, `settings`) alongside ordered project resources (`gitResource`, `filesResource`, `terminalResource`, `portsResource`), `DraftWorkspace` (worktree/branch selection), and `ModePicker` (`code`, `plan`, `flagship`).
- Uses its own release feed (`sailry/sailry-code`, `mobile-v<SemVer>+<build>`) independent of Desktop `0.1.0` releases.

## Packaging, signing, and updates

- `cmd/sailry-code-desktop/{macos,windows}/package.sh` and `cmd/sailry-code-host/package.sh` source `cmd/package-env.sh` and delegate directly to `$SAILRY_HARNESS_DIR` packaging and Host archive scripts.
- `.github/workflows/release.yml` validates tag parity (`v<Version>` == `product.Version` == `mygo.json`), produces macOS ARM64/Intel DMGs & unsigned ZIPs, Windows x64 NSIS installer & portable ZIP, and macOS/Linux Host tarballs, then runs `./cmd/update-feed` to generate Ed25519-signed `*.zip.update.json` catalogs for all three Desktop targets (`aarch64-apple-darwin`, `x86_64-apple-darwin`, `x86_64-pc-windows-msvc`).
