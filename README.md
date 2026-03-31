# todoui

Personal project organization tool. Local-first SQLite with optional background sync to [ichrisbirch](https://github.com/datapointchris/ichrisbirch).

## Features

- **TUI** (primary) — two-pane Bubble Tea interface with vim keybindings
- **CLI** — quick actions without entering the TUI
- **Multi-project items** — items can belong to multiple projects
- **Dependencies** — items can block other items, with cycle detection
- **Sub-tasks** — checklist tasks on each item
- **Notes** — multiline notes on items
- **Undo** — revert the last mutation
- **Search** — find items across all projects
- **Sync** — optional background push/pull to ichrisbirch API

## Install

```bash
go install github.com/datapointchris/todoui/cmd/todoui@latest
```

Or build from source:

```bash
git clone https://github.com/datapointchris/todoui
cd todoui
go build -o todoui ./cmd/todoui
```

## Usage

```bash
# Launch the TUI (default)
todoui

# CLI commands
todoui add "Fix the auth bug" -p work
todoui add "Setup monitoring" -p work -p homelab
todoui list
todoui list -p work
todoui done <id>
todoui archive <id>
todoui undo
todoui projects <id>                  # view item's projects
todoui projects <id> --add homelab    # add item to project
todoui projects <id> --remove work    # remove from project
```

## TUI Keybindings

| Key | Action |
| --- | ------ |
| `j/k` | Navigate up/down |
| `h/l` | Switch panes |
| `a` | Add item/project |
| `A` | Add item to multiple projects |
| `e` | Edit title |
| `n` | Edit notes |
| `d` | Mark done |
| `D` | Delete |
| `x` | Archive |
| `m` | Reorder (move mode) |
| `b/B` | Link/unlink dependency |
| `p` | Manage project membership |
| `u` | Undo |
| `/` | Search |
| `1` | Filter: blocked items |
| `2` | Filter: archived items |
| `0` | Clear filter |
| `?` | Help |
| `q` | Quit |

## Configuration

Config file: `~/.config/todoui/config.toml`

```toml
[local]
# db_path = "/custom/path/todoui.db"  # default: ~/.local/share/todoui/todoui.db

[sync]
enabled = false
# api_url = "https://api.ichrisbirch.com"
# api_key = "icb_..."  # personal API key from POST /api-keys/
```

Environment variable overrides:

| Variable | Purpose |
| -------- | ------- |
| `TODOUI_DB` | SQLite database path |
| `TODOUI_SYNC` | Enable sync (`true`/`false`) |
| `TODOUI_SYNC_URL` | API URL for sync |
| `TODOUI_SYNC_KEY` | Personal API key for sync auth |

## Architecture

Always local-first: reads and writes go to embedded SQLite. The optional sync engine pushes mutations and pulls remote state in the background.

```text
TUI / CLI
    |
    v
Backend interface
    |
    +-- LocalBackend (SQLite, always)
    |
    +-- SyncBackend (wraps LocalBackend, optional)
            |
            +-- Push: pending_sync queue -> HTTP to ichrisbirch
            +-- Pull: HTTP from ichrisbirch -> upsert local SQLite
```

Data model: projects, items (many-to-many via memberships), dependencies, sub-tasks. All IDs are UUID v7.
