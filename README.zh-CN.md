# Sailry Code

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache--2.0-blue?style=flat-square" alt="Apache-2.0 许可证"></a>
  <a href="#安装"><img src="https://img.shields.io/badge/desktop-macOS%20·%20Windows-lightgrey?style=flat-square" alt="桌面端支持 macOS 和 Windows"></a>
  <a href="#安装"><img src="https://img.shields.io/badge/host-macOS%20·%20Linux-lightgrey?style=flat-square" alt="Host 支持 macOS 和 Linux"></a>
  <a href="#安装"><img src="https://img.shields.io/badge/mobile-Android%20·%20iOS-lightgrey?style=flat-square" alt="移动端支持 Android 和 iOS"></a>
  <a href="https://discord.gg/MCDktmDey"><img src="https://img.shields.io/badge/chat-Discord-5865F2?style=flat-square&logo=discord&logoColor=white" alt="Discord"></a>
</p>

<p align="center">
  <a href="README.md">English</a> ·
  <a href="#安装">下载安装</a> ·
  <a href="docs/workspace.md">工作区指南</a> ·
  <a href="docs/connections.md">数据库与 SSH</a> ·
  <a href="docs/profiles.md">数据目录与更新</a> ·
  <a href="https://sailry.dev/zh/docs/models/">模型配置</a> ·
  <a href="https://sailry.dev/zh/docs/hosts/">远程主机</a>
</p>

**Sailry Code** 是一款开源的 AI 研发工作站，支持 macOS、Windows、Linux 与手机端。让 AI 智能体紧挨着你的代码运行在笔记本、办公主机或远程 GPU 服务器上，通过端到端加密的 P2P 链路随时控制——模型由你选择，无需云账号，采用原生 GPU 界面（无 Electron 负担）。

> [!NOTE]
> **早期预览版。** Sailry Code 版本迭代较快，让智能体修改重要代码或文件前请先做好备份。遇到问题或有建议欢迎在 [GitHub Issues](https://github.com/sailry/sailry-code/issues) 反馈。

## 工作方式

- **任何一台电脑都能成为 Host**：可直接在本地桌面端运行，也可在 Linux 服务器、Mac 或 GPU 主机上运行后台 `sailry-host` 服务，合上笔记本电脑也能继续跑长任务。
- **随时随地安全连接**：桌面端和手机通过端到端加密的点对点链路控制各台 Host，直连受限时自动走中继（见[中继指南](https://sailry.dev/zh/docs/relays/)），无需公网端口转发、VPN 或云账号。
- **手机就是你的遥控器**：用六位配对码配对 Android 或 iOS 应用，随时查看会话进展、批准工具操作、浏览 Git 与文件、排队发送后续指令。
- **原生、轻量、流畅**：基于 Go 原生 GPU UI 工具包构建，不使用 Electron 或内嵌浏览器内核。

## 核心特性

- **工作模式与智能体团队**：支持 **标准**、**计划**（动手改文件前先只读规划）和 **协作**（并行派发多个子智能体并按角色选用不同模型），所有执行步骤与审批都清晰可见。
- **代码导航与结构图谱**：快速符号搜索、文件大纲、调用上下文与改动影响分析，并可按 worktree 开启持久化索引。
- **文件工作台**：侧边栏文件树、全局搜索、多标签编辑器（`Cmd+P`）、文件传输与系统回收站支持；点击聊天中的文件链接即可直接打开预览或编辑。
- **Git 与隔离 Worktree**：查看仓库状态与 Diff、按 Hunk 暂存、提交与切换分支、克隆仓库、查看每轮检查点，并在隔离的托管 Git worktree（`Ctrl+Shift+G`）中并行开发而互不干扰。
- **内置终端与 Python 环境**：内置工作区与对话面板终端（`Ctrl+\``）、按主机独立配置的 Shell 与环境变量，以及开箱即用的内置 Python 运行环境。
- **数据库与 SSH**：连接 PostgreSQL、MySQL 与 SQLite（支持直连或经由 SSH 隧道），以及远程 SSH 命令执行与 SFTP 文件传输；数据库密码与 SSH 私钥全程不进入模型上下文。
- **浏览器、文档、搜索与自动化**：内置及外部浏览器自动化、桌面电脑操作、Word/Excel/PowerPoint/PDF 读取与本地 OCR、可交互 HTML 预览、图片/视频生成、网络搜索、本地知识库、长期记忆、目标、提醒、定时任务与本地语音听写。
- **开放插件与主题**：兼容 [agent-plugins](https://agent-plugins.org)、`SKILL.md` 技能、MCP 服务器以及官方[插件](https://github.com/sailry/sailry-plugins)与[主题](https://github.com/sailry/sailry-themes)。新建配置目录默认不预装任何插件或主题。

## 安装

前往 [Sailry Code Releases](https://github.com/sailry/sailry-code/releases) 下载对应平台安装包：

| 目标平台 | 安装包 | 系统要求 |
| --- | --- | --- |
| **桌面端** | macOS（`Sailry-aarch64-apple-darwin.dmg`、`Sailry-x86_64-apple-darwin.dmg`）<br>Windows（`Sailry-x86_64-pc-windows-msvc-setup.exe`、便携 `.zip`） | macOS 13+ · Windows 10+（x64） |
| **Host** | macOS（`sailry-host-aarch64-apple-darwin.tar.gz`、`x86_64-apple-darwin`）<br>Linux（`sailry-host-x86_64-unknown-linux-gnu.tar.gz`、`aarch64-unknown-linux-gnu`） | macOS 13+ · Linux（glibc 2.34+） |
| **移动端** | 随 `mobile-v*` 标签独立发布（`apps/mobile/`） | Android 7.0+ · iOS 15+ |

在 macOS 或 Linux 终端中一键安装 Sailry Host：

```sh
curl -fsSL https://github.com/sailry/sailry-code/releases/latest/download/install-host.sh | bash
```

Sailry Code 默认将数据存放在 `~/.sailry-code`；若首次启动时检测到已停止的旧版 `~/.sailry` 目录，会自动安全迁移。自定义 `--data-dir` 与更新通道说明见[数据目录、更新与隐私](docs/profiles.md)。

## 开始使用

1. 在**设置 › 模型**中[添加模型服务商](https://sailry.dev/zh/docs/models/#add-a-provider)（填写 API 密钥，或登录 ChatGPT / Claude 账号）。
2. 点击**添加项目**，选择现有本地文件夹或克隆 Git 仓库。
3. 点击**创建会话**，选定模型与工作模式，描述你的开发任务。
4. 按需[添加远程 Host 或配对手机](https://sailry.dev/zh/docs/hosts/#install-and-pair)，随时随地接续工作。

使用指南：[工作区与编码工具](docs/workspace.md) · [数据库与 SSH](docs/connections.md) · [数据目录、更新与隐私](docs/profiles.md) · [模型配置](https://sailry.dev/zh/docs/models/) · [远程主机](https://sailry.dev/zh/docs/hosts/) · [项目与会话](https://sailry.dev/zh/docs/projects/) · [中继](https://sailry.dev/zh/docs/relays/)

## 隐私

Sailry Code 默认发送匿名错误报告与匿名功能使用统计（绝不包含对话内容、文件、凭据、路径或自定义名称）。可在 **设置 › 通用 › 诊断与隐私** 中分别关闭，也可通过 `SAILRY_TELEMETRY=off` 或 `DO_NOT_TRACK=1` 禁用。详情见 [docs/profiles.md](docs/profiles.md)。

## 基于 Sailry Harness 与参与贡献

Sailry Code 基于上游 **[Sailry Harness](https://github.com/sailry/sailry-harness)** 构建。核心智能体运行时、模型调度、P2P 网络、UI 组件框架、通用模块与插件 SDK 都在 `sailry-harness` 仓库开发——绝大多数框架与底座贡献应提交至上游。

如需从源码构建 Sailry Code、参与 7 个研发工作台模块的改进或了解发布打包流程，请阅读 [CONTRIBUTING.md](CONTRIBUTING.md)、[ARCHITECTURE.md](ARCHITECTURE.md) 与 [AGENTS.md](AGENTS.md)。

## 社区与许可证

欢迎加入 [Discord](https://discord.gg/MCDktmDey) 交流，或在 [GitHub Issues](https://github.com/sailry/sailry-code/issues) 提交问题。

本项目采用 [Apache-2.0](LICENSE) 许可证；第三方材料保留其原有许可声明（见 [NOTICE](NOTICE)）。
