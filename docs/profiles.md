# Profiles, Updates, and Privacy

## Data profiles

Sailry Code keeps its local data separate from standalone Sailry Harness profiles:

- **Default Desktop and foreground Host profile**: `~/.sailry-code`
- **Installed remote Host service profile**: `~/.sailry-host`

Pass `--data-dir <path>` when launching Desktop or Host to point to a custom data directory without moving your default profile. Only one running process can open a profile directory at a time.

Contributors developing Sailry Code from source can use isolated development and smoke profiles via `just desktop dev` (`~/.sailry-code-dev`) and `just desktop <name>`; see [CONTRIBUTING.md](../CONTRIBUTING.md#setup).

## Automatic migration (`~/.sailry` → `~/.sailry-code`)

If you previously used legacy `~/.sailry` builds and launch Sailry Code Desktop **without** `--data-dir`:

1. If `~/.sailry-code` already exists, Sailry Code opens it directly and leaves `~/.sailry` untouched.
2. If `~/.sailry-code` does not exist and a stopped `~/.sailry` profile is present, Sailry Code automatically performs a crash-safe, lock-checked migration:
   - Stages progress in `~/..sailry-code.legacy-profile/`.
   - Migrates stored sessions, settings, credentials, and knowledge databases while preserving a backup of the original database files.
   - Moves the migrated profile cleanly to `~/.sailry-code` and updates paths for managed Git worktrees.
   - Resumes safely on the next launch if an earlier migration was interrupted.

Passing `--data-dir` disables automatic migration.

## Updates

- **Desktop (macOS ARM64, macOS Intel, Windows x64)**: In-app updates fetch signed update catalogs directly from [Sailry Code Releases](https://github.com/sailry/sailry-code/releases) and verify Ed25519 signatures before installing.
  - Switch between **Stable** and **Experience** (prerelease) channels in Settings.
  - On Windows, install initially via the setup executable (`Sailry-x86_64-pc-windows-msvc-setup.exe`) or portable ZIP (`Sailry-x86_64-pc-windows-msvc.zip`); in-app updates cleanly replace files on exit.
- **Host**: One-click remote SSH updates from Desktop (or re-running `install-host.sh`) upgrade remote `sailry-host` binaries from Sailry Code Releases while keeping your Host data directory and paired identity intact.
- **Mobile (`apps/mobile`)**: Follows an independent mobile update feed ensuring only newer semantic versions and increasing build numbers are offered.

## Telemetry and privacy

- **Diagnostics & usage**: In **Settings → General → Diagnostics and privacy**, you can toggle **Error reports** and **Anonymous usage** independently, or reset your random installation ID.
- **Process-wide opt-out**: Set `SAILRY_TELEMETRY=off` or `DO_NOT_TRACK=1` in your environment (or pass `--telemetry off` to `sailry-host`).
- **What stays private**: Conversations, prompts, replies, file contents, credentials, filesystem paths, custom project or provider names, and MCP server names are never collected.
