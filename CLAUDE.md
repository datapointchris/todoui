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

Sub-tasks can only be created from the TUI (press `t` on an item). Dependencies
are likewise TUI-only (`b`/`B`). Neither has a CLI verb.

Projects do have CLI verbs — `todoui projects list` and `todoui projects create`.
`todoui add "title" -p foo` still errors if project `foo` doesn't exist, so
create it first with `todoui projects create foo` or in the TUI project pane
with `a`.

## Planning docs

Planning documents live in `.planning/` (gitignored). `.planning/status.md`
follows the convention documented in `~/.claude/CLAUDE.md` — update it
alongside any substantive change, and record architectural decisions in the
**Decisions Made** section so they aren't re-litigated.
