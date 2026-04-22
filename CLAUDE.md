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

## Sub-tasks, projects, dependencies

Sub-tasks can only be created from the TUI (press `t` on an item). There is
no CLI command to create them. Projects likewise have no CLI create command —
`todoui add "title" -p foo` errors if project `foo` doesn't exist; create it
in the TUI project pane with `a` first.

## Planning docs

Planning documents live in `.planning/` (gitignored). `.planning/status.md`
follows the convention documented in `~/.claude/CLAUDE.md` — update it
alongside any substantive change, and record architectural decisions in the
**Decisions Made** section so they aren't re-litigated.
