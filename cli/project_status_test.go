package cli

import (
	"strings"
	"testing"

	"github.com/datapointchris/todoui/backend"
	"github.com/datapointchris/todoui/model"
)

func mustProject(t *testing.T, b backend.Backend, name string) *model.Project {
	t.Helper()
	p, err := b.CreateProject(model.CreateProject{Name: name})
	if err != nil {
		t.Fatalf("creating project %q: %v", name, err)
	}
	return p
}

func mustClose(t *testing.T, b backend.Backend, id string) {
	t.Helper()
	if _, err := b.SetProjectStatus(id, model.StatusDone, nil); err != nil {
		t.Fatalf("completing project: %v", err)
	}
}

func TestResolveProjectRefAcceptsTheNameAndTheTail(t *testing.T) {
	b := newTestBackend(t)
	p := mustProject(t, b, "clisteno")

	for _, ref := range []string{"clisteno", "CLISTENO", shortID(p.ID), p.ID} {
		got, err := resolveProjectRef(b, ref)
		if err != nil {
			t.Fatalf("resolving %q: %v", ref, err)
		}
		if got != p.ID {
			t.Errorf("%q resolved to %q, want %q", ref, got, p.ID)
		}
	}
}

// While a live effort holds the name, that is what the name means.
func TestResolveProjectRefPrefersTheActiveProject(t *testing.T) {
	b := newTestBackend(t)
	closed := mustProject(t, b, "clisteno")
	mustClose(t, b, closed.ID)
	live := mustProject(t, b, "clisteno")

	got, err := resolveProjectRef(b, "clisteno")
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	if got != live.ID {
		t.Errorf("resolved to the closed project; the active one holds the name")
	}
}

// Without this fallback `projects reopen ifiles` cannot name the thing it exists
// to reopen — by then no active project holds the name.
func TestResolveProjectRefFallsBackToALoneClosedProject(t *testing.T) {
	b := newTestBackend(t)
	closed := mustProject(t, b, "ifiles")
	mustClose(t, b, closed.ID)

	got, err := resolveProjectRef(b, "ifiles")
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	if got != closed.ID {
		t.Errorf("got %q, want the closed project %q", got, closed.ID)
	}
}

func TestResolveProjectRefRefusesToGuessBetweenClosedProjects(t *testing.T) {
	b := newTestBackend(t)
	var ids []string
	for range 2 {
		p := mustProject(t, b, "clisteno")
		mustClose(t, b, p.ID)
		ids = append(ids, p.ID)
	}

	_, err := resolveProjectRef(b, "clisteno")
	if err == nil {
		t.Fatal("expected an ambiguity error, got a resolution")
	}
	for _, id := range ids {
		if !strings.Contains(err.Error(), shortID(id)) {
			t.Errorf("error must list the candidates to disambiguate, got: %v", err)
		}
	}
}

func TestResolveProjectRefReportsAnUnknownName(t *testing.T) {
	b := newTestBackend(t)

	if _, err := resolveProjectRef(b, "nothing"); err == nil {
		t.Fatal("expected an error for an unknown project")
	}
}

// resolveProjects is what `todoui add -p foo` uses, and filing new work into a
// finished project is not something to make easy.
func TestAddingWorkCannotNameAClosedProject(t *testing.T) {
	b := newTestBackend(t)
	p := mustProject(t, b, "clisteno")
	mustClose(t, b, p.ID)

	if _, err := resolveProjects(b, []string{"clisteno"}); err == nil {
		t.Error("expected a closed project to be unavailable for new items")
	}
}
