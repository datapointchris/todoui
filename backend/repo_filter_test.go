package backend

import (
	"sort"
	"testing"

	"github.com/datapointchris/todoui/model"
)

func strptr(s string) *string { return &s }

// seedRepoWork builds one project holding work on two repos plus one item that
// is not repo work at all, which is the shape the repo axis has to separate.
func seedRepoWork(t *testing.T, b *LocalBackend) {
	t.Helper()
	project := mustCreateProject(t, b, "Extract the tools out of dotfiles")
	for _, seed := range []struct {
		title string
		repo  *string
	}{
		{"Move font to its own repo", strptr("dotfiles")},
		{"Move theme to its own repo", strptr("dotfiles")},
		{"Teach todoui the repo axis", strptr("todoui")},
		{"Buy a longer HDMI cable", nil},
	} {
		mustCreateItem(t, b, model.CreateProjectItem{
			Title:      seed.title,
			Repo:       seed.repo,
			ProjectIDs: []string{project.ID},
		})
	}
}

func titlesOf(items []model.ProjectItem) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.Title
	}
	sort.Strings(out)
	return out
}

func TestListItemsByRepo_NarrowsToOneRepo(t *testing.T) {
	b := newTestBackend(t)
	seedRepoWork(t, b)

	items, err := b.ListItemsByRepo(strptr("dotfiles"))
	if err != nil {
		t.Fatalf("ListItemsByRepo: %v", err)
	}
	got := titlesOf(items)
	if len(got) != 2 || got[0] != "Move font to its own repo" || got[1] != "Move theme to its own repo" {
		t.Errorf("dotfiles items = %v, want only the two dotfiles ones", got)
	}
}

// A nil repo is the untagged work, not "no filter". Reading it as no-filter
// would make errands and home projects unreachable through this axis.
func TestListItemsByRepo_NilIsTheUntaggedWork(t *testing.T) {
	b := newTestBackend(t)
	seedRepoWork(t, b)

	items, err := b.ListItemsByRepo(nil)
	if err != nil {
		t.Fatalf("ListItemsByRepo: %v", err)
	}
	if got := titlesOf(items); len(got) != 1 || got[0] != "Buy a longer HDMI cable" {
		t.Errorf("untagged items = %v, want only the HDMI cable", got)
	}
}

func TestListItemsByRepo_UnknownRepoIsEmptyNotEverything(t *testing.T) {
	b := newTestBackend(t)
	seedRepoWork(t, b)

	items, err := b.ListItemsByRepo(strptr("nonexistent"))
	if err != nil {
		t.Fatalf("ListItemsByRepo: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("unknown repo returned %v, want nothing", titlesOf(items))
	}
}

func TestListItemsByRepo_ExcludesArchived(t *testing.T) {
	b := newTestBackend(t)
	seedRepoWork(t, b)
	items, _ := b.ListItemsByRepo(strptr("dotfiles"))
	archived := true
	if _, err := b.UpdateItem(items[0].ID, model.UpdateProjectItem{Archived: &archived}); err != nil {
		t.Fatalf("archiving: %v", err)
	}

	remaining, err := b.ListItemsByRepo(strptr("dotfiles"))
	if err != nil {
		t.Fatalf("ListItemsByRepo: %v", err)
	}
	if len(remaining) != 1 {
		t.Errorf("got %d items, want the archived one excluded", len(remaining))
	}
}

// The repo tag crosses projects — that is the whole point of it, and the reason
// it can replace a project named after the repo.
func TestListItemsByRepo_SpansProjects(t *testing.T) {
	b := newTestBackend(t)
	seedRepoWork(t, b)
	other := mustCreateProject(t, b, "Linux-first migration")
	mustCreateItem(t, b, model.CreateProjectItem{
		Title:      "Port the shell config",
		Repo:       strptr("dotfiles"),
		ProjectIDs: []string{other.ID},
	})

	items, err := b.ListItemsByRepo(strptr("dotfiles"))
	if err != nil {
		t.Fatalf("ListItemsByRepo: %v", err)
	}
	if len(items) != 3 {
		t.Errorf("got %v, want all three dotfiles items across both projects", titlesOf(items))
	}
}

func TestSearchByRepo_AppliesBothTheQueryAndTheRepo(t *testing.T) {
	b := newTestBackend(t)
	seedRepoWork(t, b)

	items, err := b.SearchByRepo("repo", strptr("todoui"))
	if err != nil {
		t.Fatalf("SearchByRepo: %v", err)
	}
	if got := titlesOf(items); len(got) != 1 || got[0] != "Teach todoui the repo axis" {
		t.Errorf("search = %v, want only the todoui match", got)
	}
}

func TestSearchByRepo_NilRepoSearchesTheUntaggedWork(t *testing.T) {
	b := newTestBackend(t)
	seedRepoWork(t, b)

	items, err := b.SearchByRepo("HDMI", nil)
	if err != nil {
		t.Fatalf("SearchByRepo: %v", err)
	}
	if got := titlesOf(items); len(got) != 1 || got[0] != "Buy a longer HDMI cable" {
		t.Errorf("search = %v, want the untagged match", got)
	}
}
