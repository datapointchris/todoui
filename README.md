# todoui

Personal project organization tool. Local-first SQLite with optional background sync to [ichrisbirch](https://github.com/datapointchris/ichrisbirch).

## Features

- **TUI** (primary) — two-pane Bubble Tea interface with vim keybindings
- **CLI** — quick actions without entering the TUI
- **All Items / multi-select** — view all items at once or toggle specific projects to compare
- **Multi-project items** — items can belong to multiple projects
- **Dependencies** — items can block other items, with cycle detection
- **Sub-tasks** — checklist tasks on each item
- **Notes** — multiline notes on items
- **Repo links** — items can name the repo they are work on, and `--repo` filters
  every read by it. Validated against an optional registry at
  `$XDG_DATA_HOME/todoui/repos.json`; without one, any name is accepted
- **Project descriptions** — long-form notes/decisions per project, viewable and editable in project detail
- **Undo** — revert the last mutation
- **Search** — find items across all projects
- **Sync** — optional background push/pull to ichrisbirch API

## Install

```bash
go install github.com/datapointchris/todoui@latest
```

Or build from source:

```bash
git clone https://github.com/datapointchris/todoui
cd todoui
go build -o todoui .
```

## Usage

```bash
# Launch the TUI (default)
todoui

# Create
todoui add "Fix the auth bug" -p work
todoui add "Setup monitoring" -p work -p homelab
todoui add "Bump the CI matrix" -p work -r todoui -n "Node 20 is EOL"

# Read
todoui list
todoui list -p work
todoui list -p work --archived
todoui view <id>
todoui search "auth"
todoui blocked                        # items waiting on something

# Change
todoui edit <id> --title "New title"
todoui edit <id> --notes "Updated notes"
todoui edit <id> --repo ""            # unlink from its repo
todoui done <id>
todoui reopen <id>
todoui archive <id>
todoui unarchive <id>
todoui reorder <id> -p work --position 0
todoui delete <id> --yes
todoui undo

# Sync
todoui sync                           # push queued changes, then pull
todoui --no-sync list                 # use local data as-is

# Sub-tasks
todoui tasks <id>
todoui add-task <id> "wire up the endpoint"
todoui complete-task <id> <task-id>
todoui edit-task <id> <task-id> --title "..." --completed
todoui remove-task <id> <task-id>

# Dependencies
todoui add-dependency <id> <depends-on-id>
todoui remove-dependency <id> <depends-on-id>
todoui blockers <id>

# Projects
todoui projects list
todoui projects create "homelab"
todoui projects <id>                  # view item's projects
todoui projects <id> --add homelab    # add item to project
todoui projects <id> --remove work    # remove from project
```

**Ids**: an item is named by its number — `todoui view 118`. It is short because
it is not the primary key: the key stays a UUID so items can be created offline
and pushed without a round trip to claim an id, and the number is the handle
that gets printed and typed.

The number comes from the ichrisbirch API when sync is on and from todoui
itself when it is off. An item created while the API is unreachable has no
number yet and shows the last 8 characters of its id instead; that form still
resolves afterwards, as does a full UUID, so anything a command has ever
printed can be pasted into the next one. Matching is on the suffix, not a
prefix, because UUIDv7 front-loads its timestamp and every item created in the
same minute shares a prefix. An ambiguous id is reported rather than guessed at.

**`--json`** is available on `list`, `view`, `search`, `tasks`, `blockers`, and
`blocked`, and emits full ids.

**Freshness**: when sync is enabled, todoui reconciles on its own and running
`todoui sync` by hand is never required. `sync.interval` (default two minutes)
sets the longest it will go without reconciling, and governs both halves:

- A **CLI command** refreshes from the API first if the last pull is older than
  the interval, so a burst of commands pulls once rather than every time.
- The **TUI** pulls on launch and again every interval for as long as it is
  open. A pull is deferred while a text input, overlay, or move is active and
  runs on the next tick instead, so it never moves the ground under an edit.

Queued pushes retry on their own too — a change made while the API was
unreachable is sent once it comes back, without another edit to trigger it.

An unreachable API warns on stderr (CLI) or shows `SYNC ERR` in the status bar
(TUI) and falls back to local data — todoui is local-first and never blocks on
the network. Force a reconcile with `todoui sync` or `R` in the TUI; skip the
pre-command refresh with `--no-sync`.

## TUI Keybindings

| Key | Action |
| --- | ------ |
| `j/k` | Navigate up/down (walks items AND tasks as one list) |
| `J/K` | Jump to next/prev item (skip tasks) |
| `h/l` | Switch panes |
| `Enter` | Detail view (item or project) |
| **Project pane** | |
| `space` | Toggle multi-select |
| `Esc` | Clear selections |
| `a` | Add project |
| `m` | Reorder (move mode) |
| `e` | Edit project name (in detail) |
| `d` | Edit project description (in detail) |
| **On an item row** | |
| `space` | Toggle done/incomplete |
| `a` | Add item to current project |
| `A` | Add item to multiple projects |
| `e` | Edit title |
| `n` | Edit notes |
| `t` | Add a sub-task |
| `x` | Archive |
| `m` | Reorder (move mode) |
| `b/B` | Link/unlink dependency |
| `p` | Manage project membership |
| **On a task row** | |
| `space` | Toggle task done |
| `d` | Delete task |
| `t` | Add a sibling task to the same item |
| **Global** | |
| `u` | Undo |
| `/` | Search |
| `1` | Filter: blocked items |
| `2` | Filter: archived items |
| `0` | Clear filter |
| `R` | Sync now (also runs automatically every `sync.interval`) |
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
# interval = "2m"  # longest todoui goes without reconciling; floored at 15s
```

A pull is two requests — the item list embeds memberships, dependencies, and
sub-tasks. Against an API predating that embed it falls back to 2+2N, fetching
each item's detail and tasks separately, so `interval` still exists to keep a
burst of commands from paying that repeatedly. Lower it to sync more eagerly.

Environment variable overrides:

| Variable | Purpose |
| -------- | ------- |
| `TODOUI_DB` | SQLite database path |
| `TODOUI_SYNC` | Enable sync (`true`/`false`) |
| `TODOUI_SYNC_URL` | API URL for sync |
| `TODOUI_SYNC_KEY` | Personal API key for sync auth |
| `TODOUI_SYNC_INTERVAL` | Reconcile interval (e.g. `30s`, `5m`) |

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
