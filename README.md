# todoui

Personal project organization tool. A terminal UI over local-first SQLite, with optional background sync to [ichrisbirch](https://github.com/datapointchris/ichrisbirch).

## Features

- **TUI** — two-pane Bubble Tea interface with vim keybindings
- **All Items / multi-select** — view all items at once or toggle specific projects to compare
- **Multi-project items** — items can belong to multiple projects
- **Dependencies** — items can block other items, with cycle detection
- **Dependency browser** — `T` opens a two-pane view: every tree on the left,
  searchable, and the tree you are standing in on the right, where you drill,
  flip direction, and open items
- **Sub-tasks** — checklist tasks on each item
- **Notes** — multiline notes on items
- **Repo links** — an item's detail names the repo it is work on. The tag is set
  through ichrisbirch and arrives with sync
- **Project descriptions** — long-form notes/decisions per project, viewable and editable in project detail
- **Project status** — a project is finished (`done`) or abandoned with a reason
  (`dropped`), and either one hides it. A name is held only by the active project
  bearing it, so a finished `clisteno` gives the name back
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
todoui                    # open the TUI
todoui sync               # push queued changes, then pull
todoui update             # install the latest release
todoui version
todoui completion zsh     # also bash, fish, powershell
```

Everything else happens in the TUI. Scripts and agents reach the same projects
and items through ichrisbirch's own CLI.

A project name must be bounded work — something that ends — so creating a
project refuses a name the repo registry knows, and so does a rename. `todoui`
is banned; `todoui sync improvements` and `Extract xx from dotfiles` are not. A
repo does not end, so a project named after one becomes the bucket every later
papercut falls into. The repo association is the item's repo tag instead, which
crosses project boundaries and outlives any single project. The registry is
`repos_registry` in the config, else `$XDG_DATA_HOME/todoui/repos.json`.
Without one nothing is banned.

A project is a finite effort, so completing it is what takes it out of the list
— there is no separate archive step the way items have one. Dropping is the
other way to close one and requires a reason, because "deferred" invites the
same idea back next month. Closing a project does **not** touch its items: an
item still open when the project finished was still open, and that is worth
knowing.

Only the active project holds its name, so completing `clisteno` frees the name
for the next `clisteno` effort.

**Item numbers**: an item shows its number, the same handle ichrisbirch uses.
It is not the primary key. The key stays a UUID, so items can be created
offline and pushed without a round trip to claim an id. The number comes from
the ichrisbirch API when sync is on and from todoui itself when it is off. An
item created while the API is unreachable shows the last 8 characters of its id
until its push lands and earns it a number.

**Freshness**: when sync is enabled, todoui reconciles on its own and running
`todoui sync` by hand is never required. The TUI pulls on launch and again every
`sync.interval` (default two minutes) for as long as it is open. A pull is
deferred while a text input, overlay, or move is active and runs on the next
tick instead, so it never moves the ground under an edit.

Queued pushes retry on their own too — a change made while the API was
unreachable is sent once it comes back, without another edit to trigger it.

An unreachable API shows `SYNC ERR` in the status bar and falls back to local
data — todoui is local-first and never blocks on the network. Force a reconcile
with `R` in the TUI or `todoui sync`.

### A database belongs to one API

A pull deletes every local row the server did not return, so a database and the
API it reconciles against are a pair. The first pull records which API a
database belongs to, and every pull and push after that refuses any other —
pointing a database at the wrong API deletes everything that API has never heard
of, and the reconcile has no way to tell that from a real deletion.

```bash
todoui sync --adopt    # bind this database to the configured API (it moved)
todoui sync --force    # allow a pull that would delete most of what is here
```

`--force` exists because a truncated response, an authentication failure that
still returns `200`, and a genuine mass deletion look identical from the client:
a pull removing more than half of at least ten projects or items stops and asks.
Neither flag is reachable from the background timers.

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
| `C` | Show/hide closed projects |
| `e` | Edit project name (in detail) |
| `d` | Edit project description (in detail) |
| `c` | Complete project (in detail) |
| `x` | Drop project, prompting for why (in detail) |
| `r` | Reopen a closed project (in detail) |
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
| `T` | Dependency browser (works from either pane) |
| `p` | Manage project membership |
| **Dependency browser — left pane, the map** | |
| `j/k` | Move; the right pane follows |
| `l` | Into the focused tree |
| `/` | Filter to matching items, keeping the path to each |
| `a` | Show finished trees too |
| **Dependency browser — right pane, the tree** | |
| `l` | Drill in — the row under the cursor becomes the root |
| `h` | Back one drill, then back to the map |
| `i` | Flip between what it waits on and what it unblocks |
| `Enter` | Item detail; `Esc` returns to the row you left |
| `Esc` | Leave the browser |
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
# repos_registry = "~/dev/repos.json"  # default: ~/.local/share/todoui/repos.json

[local]
# db_path = "/custom/path/todoui.db"  # default: ~/.local/share/todoui/todoui.db

[sync]
enabled = false
# api_url = "https://example.com/api"
# token_command = "<cli> auth token"  # prints a bearer token for api_url
# interval = "2m"  # longest todoui goes without reconciling; floored at 15s
```

`token_command` is how todoui authenticates to the API without holding a
secret itself. It is split on spaces and run without a shell or stdin, and
whatever it prints becomes the bearer token. todoui keeps the token until the
API refuses it, then runs the command once more and retries once. A failing
command's first line of stderr is what the status bar shows. Leave it unset for
an API that needs no token, such as a dev stack that sets its own identity.

The command belongs to the URL beside it. When `TODOUI_SYNC_URL` supplies the
URL, the config file's `token_command` is ignored and `TODOUI_SYNC_TOKEN_COMMAND`
is the only source, so overriding the URL never sends one API's token to
another.

A pull is two requests — the item list embeds memberships, dependencies, and
sub-tasks. Against an API predating that embed it falls back to 2+2N, fetching
each item's detail and tasks separately. Lower `interval` to sync more eagerly.

Environment variable overrides:

| Variable | Purpose |
| -------- | ------- |
| `TODOUI_DB` | SQLite database path |
| `TODOUI_SYNC` | Enable sync (`true`/`false`) |
| `TODOUI_SYNC_URL` | API URL for sync |
| `TODOUI_SYNC_TOKEN_COMMAND` | Command printing a bearer token for sync |
| `TODOUI_SYNC_INTERVAL` | Reconcile interval (e.g. `30s`, `5m`) |
| `TODOUI_REPOS_REGISTRY` | Repo registry path |

## Architecture

Always local-first: reads and writes go to embedded SQLite. The optional sync engine pushes mutations and pulls remote state in the background.

```text
TUI
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
