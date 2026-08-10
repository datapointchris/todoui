package cli

import (
	"strings"
	"testing"

	"github.com/datapointchris/todoui/v2/backend"
	"github.com/datapointchris/todoui/v2/db"
	"github.com/datapointchris/todoui/v2/model"
)

func newTestBackend(t *testing.T) backend.Backend {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return backend.NewLocalBackend(database)
}

func TestResolveIDAcceptsFullID(t *testing.T) {
	ids := []string{"019f9a84-801a-7251-b4ea-62cb9cebb3c7", "019f9a84-801a-7251-b4ea-62cb4bdd1671"}

	got, err := resolveID(ids[0], ids, "item")
	if err != nil {
		t.Fatalf("resolving full id: %v", err)
	}
	if got != ids[0] {
		t.Errorf("expected %q, got %q", ids[0], got)
	}
}

// The tail is where a UUIDv7's entropy lives, so that is what commands print
// and what has to resolve.
func TestResolveIDAcceptsShortSuffix(t *testing.T) {
	ids := []string{"019f9a84-801a-7251-b4ea-62cb9cebb3c7", "019f9a84-801a-7251-b4ea-62cb4bdd1671"}

	got, err := resolveID("4bdd1671", ids, "item")
	if err != nil {
		t.Fatalf("resolving short id: %v", err)
	}
	if got != ids[1] {
		t.Errorf("expected %q, got %q", ids[1], got)
	}
}

func TestResolveIDIsCaseInsensitive(t *testing.T) {
	ids := []string{"019f9a84-801a-7251-b4ea-62cb9CEBB3C7"}

	got, err := resolveID("9cebb3c7", ids, "item")
	if err != nil {
		t.Fatalf("resolving mixed-case id: %v", err)
	}
	if got != ids[0] {
		t.Errorf("expected %q, got %q", ids[0], got)
	}
}

// A prefix must not resolve. UUIDv7 front-loads its timestamp, so everything
// created in the same ~65s shares one — matching prefixes would hand back an
// arbitrary item from that window.
func TestResolveIDRejectsPrefix(t *testing.T) {
	ids := []string{"019f9a84-801a-7251-b4ea-62cb9cebb3c7"}

	if _, err := resolveID("019f9a84", ids, "item"); err == nil {
		t.Error("expected a prefix not to resolve")
	}
}

func TestResolveIDReportsAmbiguity(t *testing.T) {
	ids := []string{"aaaa-111111ff", "bbbb-222222ff"}

	_, err := resolveID("ff", ids, "item")
	if err == nil {
		t.Fatal("expected an ambiguous suffix to error")
	}
	if !strings.Contains(err.Error(), "matches 2") {
		t.Errorf("error should say how many matched, got %q", err)
	}
}

func TestResolveIDReportsNoMatch(t *testing.T) {
	_, err := resolveID("deadbeef", []string{"aaaa-11111111"}, "item")
	if err == nil {
		t.Fatal("expected an unmatched id to error")
	}
	if !strings.Contains(err.Error(), "no item matching") {
		t.Errorf("unexpected error: %q", err)
	}
}

// Every command prints shortID, so what a command prints has to resolve in the
// next command. This is the round-trip that was broken.
func TestShortIDRoundTripsThroughResolution(t *testing.T) {
	b := newTestBackend(t)
	project, err := b.CreateProject(model.CreateProject{Name: "work"})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	item, err := b.CreateItem(model.CreateProjectItem{
		Title:      "round trip",
		ProjectIDs: []string{project.ID},
	})
	if err != nil {
		t.Fatalf("creating item: %v", err)
	}

	resolved, err := resolveItemID(b, shortID(item.ID))
	if err != nil {
		t.Fatalf("resolving the id the CLI printed: %v", err)
	}
	if resolved != item.ID {
		t.Errorf("expected %q, got %q", item.ID, resolved)
	}
}

// Resolution reads through ListAllItemsIncludingArchived precisely so that
// `unarchive` has something to name.
func TestArchivedItemsStillResolve(t *testing.T) {
	b := newTestBackend(t)
	project, err := b.CreateProject(model.CreateProject{Name: "work"})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	item, err := b.CreateItem(model.CreateProjectItem{
		Title:      "archived",
		ProjectIDs: []string{project.ID},
	})
	if err != nil {
		t.Fatalf("creating item: %v", err)
	}

	archived := true
	if _, err := b.UpdateItem(item.ID, model.UpdateProjectItem{Archived: &archived}); err != nil {
		t.Fatalf("archiving item: %v", err)
	}

	if _, err := resolveItemID(b, shortID(item.ID)); err != nil {
		t.Errorf("an archived item must still resolve, or nothing can unarchive it: %v", err)
	}
}

func TestResolveTaskIDIsScopedToItsItem(t *testing.T) {
	b := newTestBackend(t)
	project, err := b.CreateProject(model.CreateProject{Name: "work"})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	item, err := b.CreateItem(model.CreateProjectItem{Title: "item", ProjectIDs: []string{project.ID}})
	if err != nil {
		t.Fatalf("creating item: %v", err)
	}
	other, err := b.CreateItem(model.CreateProjectItem{Title: "other", ProjectIDs: []string{project.ID}})
	if err != nil {
		t.Fatalf("creating other item: %v", err)
	}

	task, err := b.CreateTask(item.ID, model.CreateProjectItemTask{Title: "step"})
	if err != nil {
		t.Fatalf("creating task: %v", err)
	}

	resolved, err := resolveTaskID(b, item.ID, shortID(task.ID))
	if err != nil {
		t.Fatalf("resolving task: %v", err)
	}
	if resolved != task.ID {
		t.Errorf("expected %q, got %q", task.ID, resolved)
	}

	if _, err := resolveTaskID(b, other.ID, shortID(task.ID)); err == nil {
		t.Error("a task must not resolve against an item it does not belong to")
	}
}

// The number is what commands print now, so it is the first thing that has to
// resolve back.
func TestResolveItemIDAcceptsTheNumber(t *testing.T) {
	b := newNumberingTestBackend(t)
	project, err := b.CreateProject(model.CreateProject{Name: "work"})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	first, err := b.CreateItem(model.CreateProjectItem{Title: "first", ProjectIDs: []string{project.ID}})
	if err != nil {
		t.Fatalf("creating item: %v", err)
	}
	second, err := b.CreateItem(model.CreateProjectItem{Title: "second", ProjectIDs: []string{project.ID}})
	if err != nil {
		t.Fatalf("creating item: %v", err)
	}

	resolved, err := resolveItemID(b, itemHandle(second.ProjectItem))
	if err != nil {
		t.Fatalf("resolving the number the CLI printed: %v", err)
	}
	if resolved != second.ID {
		t.Errorf("expected %q, got %q", second.ID, resolved)
	}

	// And the older form keeps working, because ids already written down have to.
	if resolved, err := resolveItemID(b, shortID(first.ID)); err != nil || resolved != first.ID {
		t.Errorf("resolveItemID(suffix) = %q, %v; want %q, nil", resolved, err, first.ID)
	}
}

// An item created while the API was unreachable has no number, and the tail is
// the only handle it has until the push lands.
func TestItemHandleFallsBackToTheTailBeforeTheNumberArrives(t *testing.T) {
	b := newTestBackend(t)
	project, err := b.CreateProject(model.CreateProject{Name: "work"})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	item, err := b.CreateItem(model.CreateProjectItem{Title: "unnumbered", ProjectIDs: []string{project.ID}})
	if err != nil {
		t.Fatalf("creating item: %v", err)
	}

	handle := itemHandle(item.ProjectItem)
	if handle != shortID(item.ID) {
		t.Errorf("handle = %q, want the UUID tail %q", handle, shortID(item.ID))
	}
	if _, err := resolveItemID(b, handle); err != nil {
		t.Errorf("the handle a command printed must resolve: %v", err)
	}
}

func newNumberingTestBackend(t *testing.T) backend.Backend {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return backend.NewLocalBackend(database, backend.AssigningItemNumbers())
}
