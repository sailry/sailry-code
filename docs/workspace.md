# Workspace & Coding Tools

Sailry Code adds built-in developer workbenches and coding tools on top of Sailry's multi-host execution and workspace sandbox.

## Projects and Git cloning

1. Select **Local device** or a connected Host, then choose **Add project**.
2. Choose **Local** to open an existing folder, or **Clone** to clone a Git repository directly onto that device or Host.
3. Choose **Create session** inside any project or managed worktree.

For adding models and organizing sessions, see the [Models guide](https://sailry.dev/docs/models/) and [Projects guide](https://sailry.dev/docs/projects/).

## Files workbench (`Cmd+P`)

- Open **Files** in the sidebar or toggle the **Files** side panel inside a conversation (`Cmd+P`) to browse the directory tree, search files, edit in multi-tab views, upload or download files, and move items to Trash.
- **File links**: Clicking any file or folder link in a conversation opens it directly in the Files view.
- **Agent file discovery**: Agents use `find_files` to locate paths, `search_files` to find symbols or text, and `read_file` to read matching files.
- **Worktree isolation**: All agent file reads, edits, and patches stay confined to the active project or worktree scope.

## Git workbench & checkpoints (`Ctrl+Shift+G`)

- Open **Git** (`Ctrl+Shift+G` panel or the workspace Git page) to inspect repository status, diffs, commits, history, and branches, stage or discard individual hunks, and review per-turn checkpoints created as agents make changes.
- **Agent Git inspection**: Agents have read-only Git tools to inspect repository status, diffs, commit logs, commit details, blame annotations, and pending workspace changes without running ad-hoc shell commands.

## Managed Git worktrees

- Use the project menu or composer worktree selector to create, register, or fork branches into isolated managed worktrees inside `.sailry/worktrees/`.
- Each worktree keeps its own file scope, terminals, and sessions so multiple tasks or parallel subagents can edit separate checkouts without interfering with each other.
- **Managing worktrees**: Agents can list, create, register, and remove worktrees. Removing a worktree deletes its checkout folder while keeping the Git branch intact (close active terminals and finish referencing sessions first).

## Terminals (`Ctrl+\``) and Python runtime

- Toggle the conversation terminal panel (`Ctrl+\``) or open standalone workspace terminals from a project's menu.
- Main-workspace terminal tabs stay alive in the background when you close the tab view, while closing a conversation-panel terminal tab stops that shell session.
- Configure per-host default shell and environment variables in **Settings → Terminal** (security-sensitive runtime variables and shell startup hooks are rejected automatically).
- **Bundled Python**: Sailry Code includes a built-in Python 3.12 runtime and a ready-to-use `python` skill so agents can set up isolated per-session virtual environments for scripts and analysis without relying on your system Python install.
