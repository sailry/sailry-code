# Databases & SSH

Sailry Code connects directly to external databases and remote SSH servers from the selected execution machine while keeping passwords and private keys out of AI prompts and replies.

## Databases

- **Supported engines**: PostgreSQL, MySQL, and SQLite, with optional tunneling over any saved SSH connection profile.
- **Managing connections**: Open the **Databases** page to add, edit, test, and browse schemas and tables. Saved connections belong to the selected execution device or Host.
- **Read-only queries vs. write execution**:
  - **Schema inspection & read-only queries** (`database_catalog`, `database_query`) run on a read-only path without prompting for write approval.
  - **Write execution** (`database_execute` for inserts, updates, deletes, or DDL) always follows the conversation's session permission policy and respects each connection profile's read-only setting.
  - **Connection management**: Agents inspect or update saved connection profiles only when allowed by session permissions, invoke connections strictly by ID, and never receive passwords, passphrases, or private keys.
- **Safety limits**:
  - Statements are bounded to `64 KiB` of SQL, `10,000` rows, `512 KiB` response payloads, and a maximum `3,600,000 ms` (1 hour) timeout.
  - SQLite databases located inside Sailry's own internal storage directory cannot be targeted.

## SSH

- **Remote servers & SFTP**: Open the **SSH** page to save a connection with a password or a private key, pasted or selected from a file. Open a remote terminal or use the SFTP browser to browse, upload, download, move, and delete files.
- **Host-key verification**: First-time connections and changed host-key fingerprints always require explicit confirmation before connecting.
- **Agent actions**: With session permissions, agents can list or update SSH connection profiles, run commands on saved connections (up to a `600,000 ms` timeout), and transfer files over SFTP — referencing saved profiles by ID rather than seeing credentials.
- **One-click Host installation**: From **Add host**, you can install or update `sailry-host` over a verified SSH connection on macOS 13+ or Linux (glibc 2.34+) and pair it automatically (see the [Remote hosts guide](https://sailry.dev/docs/hosts/) for prerequisites).

## P2P Hosts & Relays

Once a remote machine runs `sailry-host` (installed via SSH or via `install-host.sh`), Desktop and Mobile connect over end-to-end encrypted peer-to-peer links with automatic relay fallback:

- [Remote hosts guide](https://sailry.dev/docs/hosts/)
- [Models guide](https://sailry.dev/docs/models/)
- [Relays guide](https://sailry.dev/docs/relays/)
