# todoui — Claude Code instructions

## Package layout

The main package lives at the module root. There is no `cmd/` or `internal/`
subdirectory — they were removed in commit `e3f71c1 refactor: flatten package
layout`. Siblings to the root `main.go` are `tui/`, `backend/`, `cli/`,
`config/`, `db/`, `graph/`, `model/`, `sync/`.

Any path referencing `./cmd/todoui` or `./todoui` as a build target is stale
— the module builds from the repo root: `go build .` /
`go install github.com/datapointchris/todoui@latest`.

## How to run and test

**Use `go run .` from the repo root. Do not build throwaway binaries to `/tmp`
and do not seed synthetic databases.**

`.envrc` (loaded by direnv on `cd`) exports `TODOUI_DB` pointing at
`~/.local/share/todoui/dev.db` — a dedicated dev database separate from any
production data. The dev DB already has real projects, items, tasks,
dependencies, and notes, which makes it a far better manual-test target than
an empty scratch DB.

```bash
go run .            # launch TUI against dev.db
go run . list       # run CLI subcommands against dev.db
go run . add "title" -p test
```

For any TUI change, the verification path is:

1. `go build ./... && go vet ./... && go test ./...` — must all pass.
2. `go run .` — drive the feature interactively against the dev DB.

Do not claim a TUI change is "verified" on build/vet/test alone. Those check
code correctness, not feature correctness. If you cannot run the TUI
interactively (no human in the loop), say so explicitly and hand the manual
test steps to the user — do not substitute a synthetic smoke test for a real
one.

`tui/app_test.go` drives the model directly: build an app over an in-memory
DB with `newTestApp`, feed it `tea.KeyMsg` values, and assert on state or on
`View()` output. `send` follows the whole command chain so a create and its
refresh have both landed before the assertion, and abandons `tea.Tick`
commands (flash timers) rather than sleeping out their duration. This is the
right place for a rendering regression — a layout bug that only appears at a
particular window size is reproducible here and invisible in a manual pass.
It does not replace step 2.

## Sub-tasks, projects, dependencies

The CLI covers sub-tasks, dependencies, reordering, and archival both ways —
see the README for the verb list. `todoui add "title" -p foo` still errors if
project `foo` doesn't exist, so create it first with `todoui projects create
foo` or in the TUI project pane with `a`. It also errors if `foo` is closed:
`resolveProjects` reads the active list, and filing new work into a finished
project is not something to make easy.

**The CLI exists for Claude Code and automation, not for Chris** — he drives
todoui through the TUI. That is why the verb set mirrors `icb projects items`
name for name: an agent uses one grammar whether it reaches the data through
the ichrisbirch API or through a local SQLite file. The work machine is never
allowed to reach the API, so on that machine this CLI is the *only* way an
agent can touch todoui, and any verb missing here is a capability that simply
does not exist there.

An item is named by its **number** — short, unique, and what every command
prints and takes back (`resolveItemID` in `cli/resolve.go`). The UUID is the
sync key and stops there; it is in `--json` and nowhere a person reads.

The number is assigned by whichever database is the authority. With sync on
that is the ichrisbirch API, and `pushCreatedItem` writes the number out of the
create response into the local row, so `todoui add` prints the handle in the
same invocation. With sync off — the work machine, which can never reach the
API — `AssigningItemNumbers` makes todoui allocate `max+1` itself, which cannot
collide because that database is a disjoint universe.

An item created while the API is unreachable has no number until its push
lands, and shows its UUID tail until then (`itemHandle`). That is the only
reason suffix resolution survives: a handle a command printed has to keep
resolving after the number arrives. Do not switch the fallback to prefix
matching — UUIDv7 front-loads its millisecond timestamp, so prefixes collide
for everything created in the same window.

## A project name is bounded work, never a repo

`projects create` refuses a name the repo registry knows, and so does a rename.
The test is whether the thing ENDS, not whether the name reads like a verb
phrase:

    "todoui sync improvements"    OK — the sync improvements end
    "Extract xx from dotfiles"    OK
    "Migrate neovim to vim.pack"  OK
    "todoui"                      banned — names a thing that exists

The failure it prevents: a repo gets a project while it is being BUILT, which is
finite and does complete. The repo then keeps existing, the next papercut has
nowhere else to go, and the finished effort silently becomes the eternal bucket.
The tell was dotfiles' own description, which had grown a hand-written BOUNDARY
paragraph explaining which work belonged to it — a modelling gap patched with
prose.

The repo association is the item's `--repo` tag, which already crosses project
boundaries and outlives any single project. "What is the dotfiles work" is a
`--repo dotfiles` query spanning live projects, finished ones, and whatever is
filed elsewhere.

A one-item project is the floor, not the target: fifteen papercuts are four or
five small thematic projects, not fifteen projects. If the ban produced one
project per item the list would read like items, which is the complaint that
started this.

A missing registry bans nothing, the same policy `--repo` validation follows.

Enforcement is on the backend (`RefusingRepoNames`), not in the CLI, because
the TUI creates projects too and a rule only one surface enforces is decoration.

## A project has a status, and it is not an `archived` flag

`projects.status` is `active`/`done`/`dropped`. For an item, complete and
archive are orthogonal and both make sense; for a project they collapse, because
a project is a finite effort with a definition of done — so completion *is* the
hide signal and there is no second flag. `dropped` exists beside `done` because
`done` alone would force you to lie about anything you merely stopped caring
about, and it requires a reason: "deferred" invites re-proposal, "dropped, and
here is why" closes the question.

`SetProjectStatus` is the only write path, and `closed_at` and `status_reason`
are derived inside its statement rather than accepted from a caller, so an
active project can never carry either. `UpdateProject` deliberately does not
touch status.

**Closing a project does not cascade to its items.** An item still open when the
project was dropped WAS still open; "shipped 8 of 11, dropped with 3 open" is a
real signal and eleven archived items is not. Visibility is derived from the
project instead. This was tried by hand the other way once and reverted.

**A name is held only by the active project bearing it.** That is
`idx_projects_name_active` in `indexes.sql`, a partial unique index, and it is
what lets a finished `clisteno` give the name back. `resolveProjectRef` in
`cli/commands.go` mirrors the API's rule: active first, then a lone terminal
match — without which `projects reopen ifiles` could not name its own target —
then an error listing candidates. Never guess between several closed projects.

`migrateProjectStatus` is the one migration that rebuilds a table rather than
adding columns, because SQLite attaches the old column-level `UNIQUE(name)` as
an implicit index no DDL can drop. Foreign keys are off across the swap:
`project_item_memberships` cascades off `projects`, so dropping the old table
with them on would take every membership. Do not simplify it into three
`ALTER TABLE ADD COLUMN`s — leaving the global constraint in place fails the
entire pull transaction the first time the server returns a done project beside
a live one sharing its name.

## Sync is automatic; `todoui sync` is a convenience, not a requirement

`sync.interval` (default 2m, floored at 15s) is the single knob for how stale
todoui may ever be, and it drives three loops:

- **CLI** — `refreshForCLI` in `main.go` pulls before a command when the last
  pull is older than the interval. A new command pulls by default;
  `commandsThatSkipPull` is a denylist rather than an allowlist so that
  forgetting to register one cannot silently serve stale data.
- **TUI** — `syncPullTickMsg` re-arms itself every interval. It always
  reschedules, even on the ticks it skips; a tick that returns no command kills
  background sync for the rest of the session. `safeToAutoPull` gates the
  reconcile to `modeNormal` because a pull rewrites items, memberships, and
  ordering wholesale and would move the ground under a grab or a text entry.
- **Push** — `pushLoop` carries a retry ticker alongside `Notify`. Notify only
  fires on a local mutation, so without it a push that failed while the API was
  down stayed queued until the user happened to edit something else.

Anything user-visible distinguishes automatic from manual: an automatic pull
neither flashes on success nor claims the status bar on failure, because it
runs every interval and would otherwise bury real messages under noise. Failure
surfaces through the engine status (`SYNC ERR`) instead. A CLI pull failure
warns and continues — local-first means an unreachable API degrades to local
data, never an error.

`Pull` records a pending-sync high-water mark before it fetches, then clears
only the ops at or below it and spares entities queued above it from the
"deleted upstream" sweeps. The user keeps working through a pull; without that
mark, an item typed mid-pull is deleted by the same pull and its queued create
is dropped with it. Do not restore `DeleteAllPendingSync` here.

The item list embeds memberships, dependencies, and tasks, so a pull is two
requests. When the server omits `dependency_ids`/`tasks` the pull falls back to
the per-item endpoints — decode into slices and branch on nil, never on empty.
Absent and empty are different answers, and reading absent as empty would delete
every task and dependency locally on the first pull against an older API. That
fallback is what makes deploy order between this repo and ichrisbirch irrelevant;
do not drop it just because the API has shipped.

The project pull asks for `?status=all` for the same reason in reverse: the sweep
below it deletes any local project the server did not return, so an active-only
response would read "completed upstream" as "deleted upstream" and cascade
through to the memberships. Terminal projects are filtered for display locally.
A server predating the column sends no status at all, which the upsert reads as
`active` — absent is not empty here either, and that is what keeps deploy order
between the two repos irrelevant for projects too.

## Planning docs

Planning documents live in `.planning/` (gitignored). `.planning/status.md`
follows the convention documented in `~/.claude/CLAUDE.md` — update it
alongside any substantive change, and record architectural decisions in the
**Decisions Made** section so they aren't re-litigated.
