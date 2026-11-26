# Sailry Code

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache--2.0-blue?style=flat-square" alt="Apache-2.0 License"></a>
  <a href="#install"><img src="https://img.shields.io/badge/desktop-macOS%20·%20Windows-lightgrey?style=flat-square" alt="Desktop on macOS and Windows"></a>
  <a href="#install"><img src="https://img.shields.io/badge/host-macOS%20·%20Linux-lightgrey?style=flat-square" alt="Host on macOS and Linux"></a>
  <a href="#install"><img src="https://img.shields.io/badge/mobile-Android%20·%20iOS-lightgrey?style=flat-square" alt="Mobile on Android and iOS"></a>
  <a href="https://discord.gg/MCDktmDey"><img src="https://img.shields.io/badge/chat-Discord-5865F2?style=flat-square&logo=discord&logoColor=white" alt="Discord"></a>
</p>

<p align="center">
  <a href="README.zh-CN.md">简体中文</a> ·
  <a href="#install">Install</a> ·
  <a href="docs/workspace.md">Workspace</a> ·
  <a href="docs/connections.md">Databases &amp; SSH</a> ·
  <a href="docs/profiles.md">Profiles &amp; Updates</a> ·
  <a href="https://sailry.dev/docs/models/">Models</a> ·
  <a href="https://sailry.dev/docs/hosts/">Hosts</a>
</p>

**Sailry Code** is an open-source AI coding workstation for macOS, Windows, Linux, and mobile. Run AI agents directly next to your code on your laptop, office machine, or remote GPU server over end-to-end encrypted peer-to-peer links — using the models you choose, with no cloud account and fast native GPU performance (no Electron).

> [!NOTE]
> **Early preview.** Sailry Code moves quickly between releases. Back up important work before agents edit it, and report bugs or feedback in [GitHub Issues](https://github.com/sailry/sailry-code/issues).

## How it works

- **Any computer becomes a Host** — Work directly on Desktop, or run `sailry-host` as a background service on a Linux server, Mac, or GPU box so long tasks keep running after you close your laptop.
- **Reach it from anywhere** — End-to-end encrypted peer-to-peer links with automatic relay fallback ([Relay guide](https://sailry.dev/docs/relays/)) — no port forwarding, VPN, or cloud account required.
- **Your phone is the remote control** — Pair Android or iOS with a six-digit code to follow active sessions, approve tool calls, inspect Git status and files, and queue messages from anywhere.
- **Native, light, and fast** — Built with a native GPU interface rather than Electron or a bundled browser engine.

## Features

- **AI modes and agent teams** — Switch between **Standard**, **Plan** (read-only planning before changes), and **Collaborate** (parallel subagents with per-role model choices), with every step and approval visible in chat.
- **Files workbench** — Sidebar file tree, full search, multi-tab editor (`Cmd+P`), file transfers, and system Trash support; clicking file links inside chat opens them directly.
- **Git and isolated worktrees** — Inspect diffs, stage hunks, commit, switch branches, clone repositories, review per-turn checkpoints, and isolate parallel agent tasks in managed Git worktrees (`Ctrl+Shift+G`).
- **Built-in terminals and Python** — Integrated workspace and conversation-panel terminals (`Ctrl+\``), per-host shell and environment preferences, and a ready-to-use bundled Python runtime.
- **Databases and SSH** — Connect to PostgreSQL, MySQL, and SQLite (directly or through SSH tunnels) and run remote SSH commands or SFTP transfers, while keeping passwords and private keys out of AI prompts.
- **Browser, documents, search, and automation** — Built-in and external browser automation, desktop computer control, Office/PDF reading with local OCR, interactive HTML previews, image/video generation, web search, local knowledge collections, long-term memory, goals, reminders, scheduled tasks, and local voice dictation.
- **Open plugins and themes** — Compatible with [agent-plugins](https://agent-plugins.org), `SKILL.md` skills, MCP servers, official [plugins](https://github.com/sailry/sailry-plugins), and [themes](https://github.com/sailry/sailry-themes). New profiles start clean with zero preinstalled plugins or themes.

## Install

Download Desktop and Host installers from [Sailry Code Releases](https://github.com/sailry/sailry-code/releases):

| Target | Packages | Requirements |
| --- | --- | --- |
| **Desktop** | macOS (`Sailry-aarch64-apple-darwin.dmg`, `Sailry-x86_64-apple-darwin.dmg`)<br>Windows (`Sailry-x86_64-pc-windows-msvc-setup.exe`, portable `.zip`) | macOS 13+ · Windows 10+ (x64) |
| **Host** | macOS (`sailry-host-aarch64-apple-darwin.tar.gz`, `x86_64-apple-darwin`)<br>Linux (`sailry-host-x86_64-unknown-linux-gnu.tar.gz`, `aarch64-unknown-linux-gnu`) | macOS 13+ · Linux (glibc 2.34+) |
| **Mobile** | Released independently under `mobile-v*` tags (`apps/mobile/`) | Android 7.0+ · iOS 15+ |

Install Sailry Host from a macOS or Linux terminal:

```sh
curl -fsSL https://github.com/sailry/sailry-code/releases/latest/download/install-host.sh | bash
```

By default, Sailry Code stores local data under `~/.sailry-code` and safely migrates an existing stopped `~/.sailry` profile on first launch. See [Profiles, Updates & Privacy](docs/profiles.md) for custom `--data-dir` usage and update channels.

## Get started

1. [Add a model provider](https://sailry.dev/docs/models/#add-a-provider) in **Settings → Models** using an API key or a ChatGPT / Claude account.
2. Click **Add project** to open an existing local folder or clone a Git repository.
3. Choose **Create session**, pick a model and work mode, and describe your task.
4. Optional: [add a remote Host or pair your phone](https://sailry.dev/docs/hosts/#install-and-pair) to continue working from anywhere.

User guides: [Workspace & Coding Tools](docs/workspace.md) · [Databases & SSH](docs/connections.md) · [Profiles, Updates & Privacy](docs/profiles.md) · [Models](https://sailry.dev/docs/models/) · [Remote hosts](https://sailry.dev/docs/hosts/) · [Projects & sessions](https://sailry.dev/docs/projects/) · [Relays](https://sailry.dev/docs/relays/)

## Privacy

Sailry Code sends anonymous error reports and anonymous feature usage counts by default (never conversations, files, credentials, paths, or custom names). Toggle either in **Settings → General → Diagnostics and privacy**, or disable telemetry per process with `SAILRY_TELEMETRY=off` or `DO_NOT_TRACK=1`. See [docs/profiles.md](docs/profiles.md) for details.

## Built on Sailry Harness & Contributing

Sailry Code is built on upstream **[Sailry Harness](https://github.com/sailry/sailry-harness)**, where the core agent runtime, model layer, P2P networking, UI framework, general modules, and Plugin SDK are developed — most framework and core runtime contributions belong in `sailry-harness`.

To build Sailry Code from source, contribute to Code's developer workbench modules, or inspect product packaging and CI, see [CONTRIBUTING.md](CONTRIBUTING.md), [ARCHITECTURE.md](ARCHITECTURE.md), and [AGENTS.md](AGENTS.md).

## Community & License

Join the [Discord](https://discord.gg/MCDktmDey) or submit bug reports in [GitHub Issues](https://github.com/sailry/sailry-code/issues).

Sailry Code is licensed under [Apache-2.0](LICENSE); third-party materials retain their own licenses (see [NOTICE](NOTICE)).
