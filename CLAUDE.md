# todoui — Claude Code instructions

The main package is at the module root — `go build .`, `go install github.com/datapointchris/todoui@latest`.
Any path naming `./cmd/todoui` is stale.

## How to run and test

**Use `go run .` from the repo root. Do not build throwaway binaries to `/tmp`
and do not seed synthetic databases.**

`.envrc` (loaded by direnv on `cd`) exports `TODOUI_DB` pointing at
`~/.local/share/todoui/dev.db` — a dedicated dev database separate from any
production data. The dev DB carries real projects, items, tasks, dependencies
and notes, which makes it a far better manual-test target than an empty scratch
DB.

⚠️ **`.envrc` is machine-local and is not in this repo. Check it exists before
running anything here.** Without it nothing is overridden, so `go run .` opens
the production database against the production API — which is the accident the
three-coupled-variables rule below exists to prevent, arrived at from the other
direction. `env | rg '^TODOUI'` returning nothing means you do not have it.

```bash
go run .            # launch TUI against dev.db
go run . list       # run CLI subcommands against dev.db
```

Build, vet and test check code correctness, not TUI correctness. Do not claim a TUI change
verified without driving it with `go run .`; if you cannot, say so and hand the manual steps to
the user rather than substituting a synthetic smoke test.

`tui/app_test.go` drives the model directly: `newTestApp` builds an app over an in-memory DB, and a
test feeds it `tea.KeyMsg` values and asserts on state or on `View()`. `send` follows the whole
command chain, so a create and its refresh have both landed before the assertion, and it abandons
`tea.Tick` commands rather than sleeping out their duration. A layout bug that appears only at one
window size is reproducible here and invisible in a manual pass. It does not replace driving the TUI.

## Sub-tasks, projects, dependencies

`todoui create "title" -p foo` errors if project `foo` doesn't exist, so create it first with
`todoui projects create foo` or in the TUI project pane with `a`. It also errors if `foo` is closed:
`resolveProjects` reads the active list, and filing new work into a finished project is not
something to make easy.

**The CLI exists for Claude Code and automation, not for Chris** — he drives todoui through the
TUI. That is why the verb set mirrors `icb projects items` name for name: an agent uses one grammar
whether it reaches the data through the ichrisbirch API or through a local SQLite file. On a machine
that cannot reach the API, this CLI is the *only* way an agent can touch todoui, so a verb missing
here is a capability that does not exist there.

An item is named by its **number** — short, unique, and what every command prints and takes back
(`resolveItemID` in `cli/resolve.go`). The UUID is the sync key and stops there; it is in `--json`
and nowhere a person reads.

The number is assigned by whichever database is the authority. With sync on that is the API, and
`pushCreatedItem` writes the number from the create response into the local row, so `todoui create`
prints the handle in the same invocation. With sync off, `AssigningItemNumbers` makes todoui
allocate `max+1` itself, which cannot collide because that database is a disjoint universe.

An item created while the API is unreachable has no number until its push lands, and shows its UUID
tail until then (`itemHandle`). That is the only reason suffix resolution survives: a handle a
command printed has to keep resolving after the number arrives. Do not switch the fallback to prefix
matching — `standards/cli-design.md` § "A UUID-keyed resource needs a short handle of its own" is why
a UUIDv7 prefix cannot work.

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
paragraph explaining which work belonged to it — a modeling gap patched with
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

`projects.status` is `active`/`completed`/`dropped`. For an item, complete and archive are
orthogonal; for a project they collapse, because a project is a finite effort with a definition of
done, so completion *is* the hide signal. `dropped` requires a reason: "deferred" invites
re-proposal, "dropped, and here is why" closes the question.

`SetProjectStatus` is the only write path, and `closed_at` and `status_reason` are derived inside
its statement rather than accepted from a caller, so an active project can never carry either.
`UpdateProject` deliberately does not touch status.

**Closing a project does not cascade to its items.** An item still open when the project was
dropped WAS still open; "shipped 8 of 11, dropped with 3 open" is a real signal and eleven archived
items is not. Visibility is derived from the project instead.

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

## A database and its API are a pair — never mix a dev half with a prod half

`.envrc` exports **three** coupled variables: `TODOUI_DB`, `TODOUI_SYNC_URL`,
`TODOUI_SYNC_KEY`. They name one environment. Overriding one of them names an
environment that does not exist, and for this tool that is destructive rather
than merely wrong: a pull deletes every local row the server did not return, so
a production database reconciled against the dev API is emptied of everything
dev has never heard of. `standards/infrastructure.md` § "A dev environment
override is all-or-nothing" carries what that cost, and this repo is where it
happened.

**To run against production data, run from outside this repo**, where direnv has
loaded nothing. Un-setting your way out is how the accident happened: `env -u`
on the variable you remembered still leaves the two you did not.

`sync_origin` is the enforcement, because a rule only prose enforces is
decoration. The first pull records which API the database belongs to, and every
pull and push after that refuses any other (`OriginMismatch`). `--adopt` is the
only way the pairing changes. A database that has pulled before but carries no
origin — anything predating this — refuses to guess and asks to be bound, since
inferring it from whichever environment is loaded *is* the mistake.

`guardSweep` is the second layer, for when the API is right but its answer is
not: a pull deleting more than half of at least ten local projects or items is
refused, because a truncated response, an auth failure that still returns 200,
and a genuine mass deletion are the same event seen from here. `--force` allows
it. The floor exists so the guard stays silent on small databases, where losing
most of it is a handful of rows; a guard that fires on ordinary days gets forced
past by reflex and stops guarding anything.

Neither override is reachable from the background timers. A guard something
automatic can waive is not a guard.

## Sync is automatic; `todoui sync` is a convenience, not a requirement

`sync.interval` (default 2m, floored at 15s) is the single knob for how stale todoui may ever be,
and it drives three loops:

- **CLI** — `refreshForCLI` in `main.go` pulls before a command when the last pull is older than
  the interval. `commandsThatSkipPull` is a denylist rather than an allowlist, so forgetting to
  register a new command cannot silently serve stale data.
- **TUI** — `syncPullTickMsg` re-arms itself every interval, and always reschedules, even on the
  ticks it skips: a tick that returns no command kills background sync for the session.
  `safeToAutoPull` gates the reconcile to `modeNormal`, because a pull rewrites items, memberships
  and ordering wholesale and would move the ground under a grab or a text entry.
- **Push** — `pushLoop` carries a retry ticker alongside `Notify`, which fires only on a local
  mutation, so a push that failed while the API was down retries without another edit.

An automatic pull neither flashes on success nor claims the status bar on failure; failure surfaces
through the engine status (`SYNC ERR`). A CLI pull failure warns and continues — local-first means
an unreachable API degrades to local data, never an error.

`Pull` records a pending-sync high-water mark before it fetches, then clears only the ops at or
below it and spares entities queued above it from the "deleted upstream" sweeps. Without that mark,
an item typed mid-pull is deleted by the same pull and its queued create dropped with it. Do not
restore `DeleteAllPendingSync` here.

**Absent is not empty, and that is what keeps deploy order between this repo and ichrisbirch
irrelevant.** The item list embeds memberships, dependencies and tasks; when the server omits
`dependency_ids`/`tasks`, the pull falls back to the per-item endpoints — decode into slices and
branch on nil, never on empty, or the first pull against an older API deletes every local task and
dependency. The project pull asks for `?status=all` because the sweep deletes any local project the
server did not return, so an active-only response would read "completed" as "deleted" and cascade
through the memberships. A server sending no project status is read as `active`. Do not drop either
fallback just because the API has shipped.

## Never write the breaking-change trailer in a commit message

Those two words anywhere in a message cut a major here, and a major on a Go module
with no `/vN` path is an outage: `go install …@latest` stops seeing the tag, every
installed binary is stranded, and recovery is a reinstall on each machine. The
analyzer matches unanchored and ORs past `.semrelrc`, so nothing switches it off.
A commit that merely *discusses* the trailer cuts one too — say "that marker".
Deliberate majors are `chore(release-major)`. Reset procedure and the measurement:
`standards/release.md` § "Never write the breaking-change trailer in a Go repo's
commit message".
