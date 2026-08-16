package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/datapointchris/todoui/model"
)

// itemIDByTitle finds a created item so a test can wire dependencies through
// the backend directly, which is the only way to arrange a chain.
func itemIDByTitle(t *testing.T, app *App, title string) string {
	t.Helper()
	items, err := app.backend.ListAllItemsIncludingArchived()
	if err != nil {
		t.Fatalf("listing items: %v", err)
	}
	for _, item := range items {
		if item.Title == title {
			return item.ID
		}
	}
	t.Fatalf("no item titled %q", title)
	return ""
}

func dependsOn(t *testing.T, app *App, item, dependency string) {
	t.Helper()
	if err := app.backend.AddDependency(itemIDByTitle(t, app, item), itemIDByTitle(t, app, dependency)); err != nil {
		t.Fatalf("adding dependency %s -> %s: %v", item, dependency, err)
	}
}

// openTreeOn puts the main cursor on the named item and opens the overlay.
func openTreeOn(t *testing.T, app *App, title string) {
	t.Helper()
	for i := 0; i < 20; i++ {
		if item := app.currentItem(); item != nil && item.Title == title {
			break
		}
		pressKey(t, app, "j")
	}
	if item := app.currentItem(); item == nil || item.Title != title {
		t.Fatalf("could not put the cursor on %q", title)
	}
	pressKey(t, app, "T")
	if app.appMode != modeDepTree {
		t.Fatalf("expected modeDepTree after T, got %v", app.appMode)
	}
}

func treeTitles(app *App) []string {
	titles := make([]string, 0, len(app.depTree.rows))
	for _, row := range app.depTree.rows {
		titles = append(titles, app.depTree.lookup[row.ID].Item.Title)
	}
	return titles
}

func TestDepTree_OpensRootedAtTheItemUnderTheCursor(t *testing.T) {
	app := newTestApp(t, 100, 30, "serve", "schema", "bootstrap")
	dependsOn(t, app, "serve", "schema")
	dependsOn(t, app, "schema", "bootstrap")

	openTreeOn(t, app, "serve")

	got := treeTitles(app)
	want := []string{"serve", "schema", "bootstrap"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("rows = %v, want %v — the named item is the root and the chain hangs under it", got, want)
	}
}

// The blockers section of the item detail is one level deep. The tree is the
// same relation with the rest of the chain attached.
func TestDepTree_ReachesPastTheFirstLevelOfBlockers(t *testing.T) {
	app := newTestApp(t, 100, 30, "serve", "schema", "bootstrap")
	dependsOn(t, app, "serve", "schema")
	dependsOn(t, app, "schema", "bootstrap")

	openTreeOn(t, app, "serve")

	if view := app.View(); !strings.Contains(view, "bootstrap") {
		t.Errorf("view does not reach the second level:\n%s", view)
	}
}

func TestDepTree_InvertFlipsWhichWayTheEdgesAreRead(t *testing.T) {
	app := newTestApp(t, 100, 30, "serve", "cli", "agent")
	dependsOn(t, app, "cli", "serve")
	dependsOn(t, app, "agent", "serve")

	openTreeOn(t, app, "serve")
	if got := treeTitles(app); len(got) != 1 {
		t.Fatalf("forward rows = %v, want just serve — it waits on nothing", got)
	}

	pressKey(t, app, "i")

	got := treeTitles(app)
	if len(got) != 3 || got[0] != "serve" {
		t.Errorf("inverted rows = %v, want serve and the two items it unblocks", got)
	}
	if !strings.Contains(app.View(), "unblocks") {
		t.Errorf("title does not say which way the tree is being read:\n%s", app.View())
	}
}

func TestDepTree_DrillReRootsAndBackRestoresThePreviousView(t *testing.T) {
	app := newTestApp(t, 100, 30, "serve", "schema", "bootstrap")
	dependsOn(t, app, "serve", "schema")
	dependsOn(t, app, "schema", "bootstrap")

	openTreeOn(t, app, "serve")
	pressKey(t, app, "j") // onto schema
	pressKey(t, app, "l")

	if got := treeTitles(app); len(got) != 2 || got[0] != "schema" {
		t.Errorf("after drilling = %v, want schema as the new root", got)
	}

	pressKey(t, app, "h")

	if got := treeTitles(app); len(got) != 3 || got[0] != "serve" {
		t.Errorf("after going back = %v, want the original view", got)
	}
	if app.depTree.cursor != 1 {
		t.Errorf("cursor = %d, want 1 — going back returns to the row you drilled from", app.depTree.cursor)
	}
}

// Nothing left to retrace means the overlay itself is what you are backing out
// of. Leaving it open with h doing nothing reads as a stuck key.
func TestDepTree_BackAtTheBottomOfTheStackClosesTheOverlay(t *testing.T) {
	app := newTestApp(t, 100, 30, "serve", "schema")
	dependsOn(t, app, "serve", "schema")

	openTreeOn(t, app, "serve")
	pressKey(t, app, "h")

	if app.appMode != modeNormal {
		t.Errorf("mode = %v, want modeNormal — h at the bottom leaves the overlay", app.appMode)
	}
}

func TestDepTree_DrillingIntoADeadEndSaysWhichDirectionIsEmpty(t *testing.T) {
	app := newTestApp(t, 100, 30, "serve", "schema")
	dependsOn(t, app, "serve", "schema")

	openTreeOn(t, app, "serve")
	pressKey(t, app, "j") // onto schema, which waits on nothing
	pressKey(t, app, "l")

	if got := treeTitles(app); len(got) != 2 || got[0] != "serve" {
		t.Errorf("rows = %v, want the view unchanged — there was nothing to drill into", got)
	}
	if !strings.Contains(app.statusMsg, "depends on nothing") {
		t.Errorf("status = %q, want it to name what is missing in this direction", app.statusMsg)
	}
}

func TestDepTree_EnterOpensTheDetailForTheRowUnderTheCursor(t *testing.T) {
	app := newTestApp(t, 100, 30, "serve", "schema")
	dependsOn(t, app, "serve", "schema")

	openTreeOn(t, app, "serve")
	pressKey(t, app, "j")
	send(t, app, tea.KeyMsg{Type: tea.KeyEnter})

	if app.appMode != modeItemDetail {
		t.Fatalf("mode = %v, want modeItemDetail", app.appMode)
	}
	if app.itemDetail == nil || app.itemDetail.Title != "schema" {
		t.Errorf("detail = %v, want the row the cursor was on", app.itemDetail)
	}
}

// A key nothing advertises does not exist. The tree was reachable from the
// help overlay and the item detail, and the status bar — the one hint line
// always on screen — did not mention it.
func TestDepTree_TheStatusBarAdvertisesTheKey(t *testing.T) {
	app := newTestApp(t, 140, 30, "serve")

	hints := app.statusBarHints()

	if !strings.Contains(hints, "[T]ree") {
		t.Errorf("item-row hints = %q, want the tree key beside the dependency keys", hints)
	}
}

// Peeking at one item along the chain must not end the walk.
func TestDepTree_ClosingADetailOpenedFromTheTreeReturnsToTheTree(t *testing.T) {
	app := newTestApp(t, 100, 30, "serve", "schema", "bootstrap")
	dependsOn(t, app, "serve", "schema")
	dependsOn(t, app, "schema", "bootstrap")

	openTreeOn(t, app, "serve")
	pressKey(t, app, "j")
	send(t, app, tea.KeyMsg{Type: tea.KeyEnter})
	send(t, app, tea.KeyMsg{Type: tea.KeyEsc})

	if app.appMode != modeDepTree {
		t.Fatalf("mode = %v, want modeDepTree — esc returns to where the detail was opened from", app.appMode)
	}
	if app.depTree.cursor != 1 {
		t.Errorf("cursor = %d, want 1 — the walk resumes on the row it left", app.depTree.cursor)
	}
	send(t, app, tea.KeyMsg{Type: tea.KeyEsc})
	if app.appMode != modeNormal {
		t.Errorf("mode = %v, want modeNormal — a second esc leaves the tree too", app.appMode)
	}
}

// A detail opened from the main pane still closes to the main pane, and takes
// the tree's whole-store snapshot with it.
func TestDetail_ClosingADetailOpenedFromThePaneDropsTheTreeSnapshot(t *testing.T) {
	app := newTestApp(t, 100, 30, "serve", "schema")
	dependsOn(t, app, "serve", "schema")

	openTreeOn(t, app, "serve")
	send(t, app, tea.KeyMsg{Type: tea.KeyEsc}) // out of the tree
	send(t, app, tea.KeyMsg{Type: tea.KeyEnter})
	send(t, app, tea.KeyMsg{Type: tea.KeyEsc}) // out of the detail

	if app.appMode != modeNormal {
		t.Fatalf("mode = %v, want modeNormal", app.appMode)
	}
	if app.depTree.tree != nil {
		t.Error("the tree snapshot outlived its overlay")
	}
}

// A shared dependency is drawn once and marked, rather than repeating its
// whole subtree under every parent.
func TestDepTree_SharedDependencyIsMarkedRatherThanRepeated(t *testing.T) {
	app := newTestApp(t, 100, 30, "keystone", "base", "left", "right")
	dependsOn(t, app, "keystone", "base")
	dependsOn(t, app, "left", "keystone")
	dependsOn(t, app, "right", "keystone")

	// The forest read forward has two roots, left and right, and both hang the
	// same keystone subtree.
	openTreeOn(t, app, "left")
	pressKey(t, app, "a")

	view := app.View()
	if !strings.Contains(view, "(*)") {
		t.Errorf("view has no repeat marker, so a shared subtree was drawn twice:\n%s", view)
	}
}

func TestDepTree_AnItemWithNoEdgesSaysSoInsteadOfDrawingNothing(t *testing.T) {
	app := newTestApp(t, 100, 30, "alone")

	openTreeOn(t, app, "alone")

	if view := app.View(); !strings.Contains(view, "no dependencies either way") {
		t.Errorf("view does not explain the empty tree:\n%s", view)
	}
}

func TestDepTree_EscClosesAndRestoresNavigation(t *testing.T) {
	app := newTestApp(t, 100, 30, "serve", "schema")
	dependsOn(t, app, "serve", "schema")

	openTreeOn(t, app, "serve")
	send(t, app, tea.KeyMsg{Type: tea.KeyEsc})

	if app.appMode != modeNormal {
		t.Fatalf("mode = %v, want modeNormal", app.appMode)
	}
	before := app.rowCursor
	pressKey(t, app, "j")
	if app.rowCursor == before {
		t.Error("cursor did not move after esc; keys still captured by the overlay")
	}
}

// A finished chain is history and they accumulate, so the forest view leaves
// them out.
func TestDepTree_ForestHidesTreesWithNoOpenWork(t *testing.T) {
	app := newTestApp(t, 100, 30, "live", "live-dep", "done", "done-dep")
	dependsOn(t, app, "live", "live-dep")
	dependsOn(t, app, "done", "done-dep")
	completed := true
	for _, title := range []string{"done-dep", "done"} {
		if _, err := app.backend.UpdateItem(itemIDByTitle(t, app, title), model.UpdateProjectItem{Completed: &completed}); err != nil {
			t.Fatalf("completing %s: %v", title, err)
		}
	}

	openTreeOn(t, app, "live")
	pressKey(t, app, "a")

	titles := strings.Join(treeTitles(app), ",")
	if !strings.Contains(titles, "live") {
		t.Errorf("forest = %s, want the tree with open work", titles)
	}
	if strings.Contains(titles, "done") {
		t.Errorf("forest = %s, want the finished tree hidden", titles)
	}
}

func TestDepTree_ScrollsRatherThanClippingSilently(t *testing.T) {
	titles := []string{"root"}
	for i := 0; i < 12; i++ {
		titles = append(titles, "leaf"+string(rune('a'+i)))
	}
	app := newTestApp(t, 100, 12, titles...)
	for _, leaf := range titles[1:] {
		dependsOn(t, app, "root", leaf)
	}

	openTreeOn(t, app, "root")

	if view := app.View(); !strings.Contains(view, "more") {
		t.Errorf("a tree taller than the window shows no remainder line:\n%s", view)
	}
	for i := 0; i < 12; i++ {
		pressKey(t, app, "j")
	}
	if app.depTree.scroll == 0 {
		t.Error("scroll never moved, so rows past the window are unreachable")
	}
}
