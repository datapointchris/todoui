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

// openTree enters the browser cold, the way it is reached without first
// finding an item.
func openTree(t *testing.T, app *App) {
	t.Helper()
	pressKey(t, app, "T")
	if app.appMode != modeDepTree {
		t.Fatalf("expected modeDepTree after T, got %v", app.appMode)
	}
}

// openTreeOn enters the browser standing on a named item.
func openTreeOn(t *testing.T, app *App, title string) {
	t.Helper()
	for i := 0; i < 30; i++ {
		if item := app.currentItem(); item != nil && item.Title == title {
			break
		}
		pressKey(t, app, "j")
	}
	if item := app.currentItem(); item == nil || item.Title != title {
		t.Fatalf("could not put the cursor on %q", title)
	}
	openTree(t, app)
}

func forestTitles(app *App) []string {
	titles := make([]string, 0, len(app.depTree.forestRows))
	for _, row := range app.depTree.forestRows {
		titles = append(titles, app.depTree.lookup[row.ID].Item.Title)
	}
	return titles
}

func focusTitles(app *App) []string {
	titles := make([]string, 0, len(app.depTree.focusRows))
	for _, row := range app.depTree.focusRows {
		titles = append(titles, app.depTree.lookup[row.ID].Item.Title)
	}
	return titles
}

func typeInto(t *testing.T, app *App, text string) {
	t.Helper()
	for _, r := range text {
		send(t, app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

// The complaint the browser exists to answer: reaching the dependency view
// should not require finding an item first.
func TestDepTree_OpensFromTheProjectPaneWithNoItemSelected(t *testing.T) {
	app := newTestApp(t, 120, 30, "serve", "schema")
	dependsOn(t, app, "serve", "schema")
	app.activePane = projectPane

	openTree(t, app)

	if len(app.depTree.forestRows) != 2 {
		t.Errorf("forest = %v, want both items — the whole map, not one item's tree", forestTitles(app))
	}
	if app.depTree.active != forestPane {
		t.Errorf("active pane = %v, want the forest", app.depTree.active)
	}
}

func TestDepTree_ForestCarriesEveryTreeNotJustTheSelectedOne(t *testing.T) {
	app := newTestApp(t, 120, 30, "a1", "a2", "b1", "b2")
	dependsOn(t, app, "a1", "a2")
	dependsOn(t, app, "b1", "b2")

	openTree(t, app)

	got := strings.Join(forestTitles(app), ",")
	if !strings.Contains(got, "a1") || !strings.Contains(got, "b1") {
		t.Errorf("forest = %s, want both trees side by side", got)
	}
}

func TestDepTree_OpeningOnAnItemStandsOnItInTheForest(t *testing.T) {
	app := newTestApp(t, 120, 30, "serve", "schema", "bootstrap")
	dependsOn(t, app, "serve", "schema")
	dependsOn(t, app, "schema", "bootstrap")

	openTreeOn(t, app, "schema")

	if got := app.depTree.lookup[app.depTree.forestID()].Item.Title; got != "schema" {
		t.Errorf("forest cursor on %q, want schema", got)
	}
	if app.depTree.pinned != itemIDByTitle(t, app, "schema") {
		t.Error("the focus pane is not pinned to the item the browser opened on")
	}
}

// The two panes answer different questions, so the map has to drive the
// working surface.
func TestDepTree_MovingOnTheMapRepointsTheFocusedTree(t *testing.T) {
	app := newTestApp(t, 120, 30, "a1", "a2", "b1", "b2")
	dependsOn(t, app, "a1", "a2")
	dependsOn(t, app, "b1", "b2")

	openTree(t, app)
	first := strings.Join(focusTitles(app), ",")
	for i := 0; i < len(app.depTree.forestRows); i++ {
		pressKey(t, app, "j")
	}
	second := strings.Join(focusTitles(app), ",")

	if first == second {
		t.Errorf("focus pane unchanged (%s) after walking the whole map", first)
	}
}

// The focused pane shows the tree you are standing in, drawn from its own
// roots rather than from the row you happen to be on.
func TestDepTree_FocusShowsTheWholeTreeYouAreStandingIn(t *testing.T) {
	app := newTestApp(t, 120, 30, "serve", "schema", "bootstrap")
	dependsOn(t, app, "serve", "schema")
	dependsOn(t, app, "schema", "bootstrap")

	openTreeOn(t, app, "bootstrap")

	got := focusTitles(app)
	if len(got) != 3 || got[0] != "serve" {
		t.Errorf("focus = %v, want the whole chain from its root", got)
	}
}

func TestDepTree_LMovesIntoTheTreeThenDrills(t *testing.T) {
	app := newTestApp(t, 120, 30, "serve", "schema", "bootstrap")
	dependsOn(t, app, "serve", "schema")
	dependsOn(t, app, "schema", "bootstrap")

	openTreeOn(t, app, "schema")
	pressKey(t, app, "l")
	if app.depTree.active != focusPane {
		t.Fatalf("active = %v, want the focus pane after l", app.depTree.active)
	}
	if app.depTree.lookup[app.depTree.focusID()].Item.Title != "schema" {
		t.Fatalf("focus cursor landed on %q, want the pinned item", app.depTree.lookup[app.depTree.focusID()].Item.Title)
	}

	pressKey(t, app, "l")

	if got := focusTitles(app); len(got) != 2 || got[0] != "schema" {
		t.Errorf("after drilling = %v, want schema as the new root", got)
	}
}

func TestDepTree_HRetracesThenLeavesTheTreeThenCloses(t *testing.T) {
	app := newTestApp(t, 120, 30, "serve", "schema", "bootstrap")
	dependsOn(t, app, "serve", "schema")
	dependsOn(t, app, "schema", "bootstrap")

	openTreeOn(t, app, "schema")
	pressKey(t, app, "l") // into the focus pane
	pressKey(t, app, "l") // drill
	pressKey(t, app, "h")
	if got := focusTitles(app); len(got) != 3 {
		t.Errorf("after h = %v, want the previous view back", got)
	}
	if app.depTree.active != focusPane {
		t.Errorf("active = %v, want to still be in the tree", app.depTree.active)
	}

	pressKey(t, app, "h")
	if app.depTree.active != forestPane {
		t.Errorf("active = %v, want back on the map", app.depTree.active)
	}

	pressKey(t, app, "h")
	if app.appMode != modeNormal {
		t.Errorf("mode = %v, want out of the browser", app.appMode)
	}
}

func TestDepTree_FlipReadsTheEdgesTheOtherWay(t *testing.T) {
	app := newTestApp(t, 120, 30, "serve", "cli", "agent")
	dependsOn(t, app, "cli", "serve")
	dependsOn(t, app, "agent", "serve")

	openTreeOn(t, app, "serve")
	pressKey(t, app, "l")
	pressKey(t, app, "i")

	got := focusTitles(app)
	if len(got) != 3 || got[0] != "serve" {
		t.Errorf("inverted focus = %v, want serve on top with what it unblocks under it", got)
	}
	if !strings.Contains(app.View(), "unblocks") {
		t.Errorf("pane title does not say which way it is being read:\n%s", app.View())
	}
}

func TestDepTree_SearchNarrowsTheMapAndKeepsThePathToTheMatch(t *testing.T) {
	app := newTestApp(t, 120, 30, "serve", "schema", "bootstrap", "unrelated-a", "unrelated-b")
	dependsOn(t, app, "serve", "schema")
	dependsOn(t, app, "schema", "bootstrap")
	dependsOn(t, app, "unrelated-a", "unrelated-b")

	openTree(t, app)
	pressKey(t, app, "/")
	typeInto(t, app, "bootstrap")
	send(t, app, tea.KeyMsg{Type: tea.KeyEnter})

	got := forestTitles(app)
	if len(got) != 3 {
		t.Fatalf("forest = %v, want the match and the two rows above it", got)
	}
	if got[0] != "serve" || got[2] != "bootstrap" {
		t.Errorf("forest = %v, want the path from the root down to the match", got)
	}
}

func TestDepTree_SearchMatchesTheNumberAndTheRepo(t *testing.T) {
	app := newTestApp(t, 120, 30, "serve", "schema")
	dependsOn(t, app, "serve", "schema")
	repo := "day"
	if _, err := app.backend.UpdateItem(itemIDByTitle(t, app, "schema"), model.UpdateProjectItem{Repo: &repo}); err != nil {
		t.Fatalf("tagging repo: %v", err)
	}

	openTree(t, app)
	pressKey(t, app, "/")
	typeInto(t, app, "day")
	send(t, app, tea.KeyMsg{Type: tea.KeyEnter})

	if got := forestTitles(app); len(got) != 2 || got[1] != "schema" {
		t.Errorf("forest = %v, want the repo-tagged item and its path", got)
	}
}

func TestDepTree_SearchWithNoMatchSaysSo(t *testing.T) {
	app := newTestApp(t, 120, 30, "serve", "schema")
	dependsOn(t, app, "serve", "schema")

	openTree(t, app)
	pressKey(t, app, "/")
	typeInto(t, app, "zzzz")
	send(t, app, tea.KeyMsg{Type: tea.KeyEnter})

	if view := app.View(); !strings.Contains(view, "No item matches") {
		t.Errorf("view does not report the empty result:\n%s", view)
	}
}

func TestDepTree_EscInSearchClearsTheQueryRatherThanClosing(t *testing.T) {
	app := newTestApp(t, 120, 30, "serve", "schema")
	dependsOn(t, app, "serve", "schema")

	openTree(t, app)
	pressKey(t, app, "/")
	typeInto(t, app, "zzzz")
	send(t, app, tea.KeyMsg{Type: tea.KeyEsc})

	if app.appMode != modeDepTree {
		t.Fatalf("mode = %v, want to still be in the browser", app.appMode)
	}
	if app.depTree.query != "" {
		t.Errorf("query = %q, want it cleared", app.depTree.query)
	}
	if len(app.depTree.forestRows) != 2 {
		t.Errorf("forest = %v, want the unfiltered map back", forestTitles(app))
	}
}

// A finished chain is history and they accumulate, so the map leaves them out
// until asked.
func TestDepTree_FinishedTreesAreHiddenUntilToggled(t *testing.T) {
	app := newTestApp(t, 120, 30, "live", "live-dep", "done", "done-dep")
	dependsOn(t, app, "live", "live-dep")
	dependsOn(t, app, "done", "done-dep")
	completed := true
	for _, title := range []string{"done-dep", "done"} {
		if _, err := app.backend.UpdateItem(itemIDByTitle(t, app, title), model.UpdateProjectItem{Completed: &completed}); err != nil {
			t.Fatalf("completing %s: %v", title, err)
		}
	}

	openTree(t, app)
	if got := strings.Join(forestTitles(app), ","); strings.Contains(got, "done") {
		t.Errorf("forest = %s, want the finished tree hidden", got)
	}

	pressKey(t, app, "a")

	if got := strings.Join(forestTitles(app), ","); !strings.Contains(got, "done") {
		t.Errorf("forest = %s, want the finished tree after toggling", got)
	}
}

func TestDepTree_EnterOpensTheDetailAndEscComesBack(t *testing.T) {
	app := newTestApp(t, 120, 30, "serve", "schema")
	dependsOn(t, app, "serve", "schema")

	openTree(t, app)
	pressKey(t, app, "j")
	send(t, app, tea.KeyMsg{Type: tea.KeyEnter})

	if app.appMode != modeItemDetail {
		t.Fatalf("mode = %v, want modeItemDetail", app.appMode)
	}
	if app.itemDetail == nil || app.itemDetail.Title != "schema" {
		t.Errorf("detail = %v, want the row the cursor was on", app.itemDetail)
	}

	send(t, app, tea.KeyMsg{Type: tea.KeyEsc})

	if app.appMode != modeDepTree {
		t.Errorf("mode = %v, want back in the browser", app.appMode)
	}
}

func TestDepTree_EscLeavesAndRestoresNavigation(t *testing.T) {
	app := newTestApp(t, 120, 30, "serve", "schema")
	dependsOn(t, app, "serve", "schema")

	openTree(t, app)
	send(t, app, tea.KeyMsg{Type: tea.KeyEsc})

	if app.appMode != modeNormal {
		t.Fatalf("mode = %v, want modeNormal", app.appMode)
	}
	if app.depTree.tree != nil {
		t.Error("the tree snapshot outlived the browser")
	}
	before := app.rowCursor
	pressKey(t, app, "j")
	if app.rowCursor == before {
		t.Error("cursor did not move after esc; keys still captured by the browser")
	}
}

// An item in no tree cannot be stood on. Opening somewhere else without
// saying so reads as the browser ignoring the key.
func TestDepTree_OpeningOnAnItemWithNoEdgesSaysWhyItLandedElsewhere(t *testing.T) {
	app := newTestApp(t, 120, 30, "alone", "serve", "schema")
	dependsOn(t, app, "serve", "schema")

	openTreeOn(t, app, "alone")

	if !strings.Contains(app.statusMsg, "no dependencies") {
		t.Errorf("status = %q, want it to say the item is in no tree", app.statusMsg)
	}
	if len(app.depTree.forestRows) != 2 {
		t.Errorf("forest = %v, want the browser open on everything else", forestTitles(app))
	}
}

// An item whose only tree is finished is reachable: the map reveals the
// finished trees rather than opening somewhere unrelated.
func TestDepTree_OpeningOnAnItemInAFinishedTreeRevealsThatTree(t *testing.T) {
	app := newTestApp(t, 120, 30, "live", "live-dep", "done", "done-dep")
	dependsOn(t, app, "live", "live-dep")
	dependsOn(t, app, "done", "done-dep")
	completed := true
	for _, title := range []string{"done-dep", "done"} {
		if _, err := app.backend.UpdateItem(itemIDByTitle(t, app, title), model.UpdateProjectItem{Completed: &completed}); err != nil {
			t.Fatalf("completing %s: %v", title, err)
		}
	}

	openTreeOn(t, app, "done")

	if !app.depTree.showFinished {
		t.Error("finished trees are still hidden, so the item asked for is not on the map")
	}
	if got := app.depTree.lookup[app.depTree.forestID()].Item.Title; got != "done" {
		t.Errorf("forest cursor on %q, want done", got)
	}
}

// A shared dependency is drawn once and marked rather than repeating its whole
// subtree under every parent.
func TestDepTree_SharedDependencyIsMarkedRatherThanRepeated(t *testing.T) {
	app := newTestApp(t, 120, 30, "keystone", "base", "left", "right")
	dependsOn(t, app, "keystone", "base")
	dependsOn(t, app, "left", "keystone")
	dependsOn(t, app, "right", "keystone")

	openTree(t, app)

	if view := app.View(); !strings.Contains(view, "(*)") {
		t.Errorf("view has no repeat marker, so a shared subtree was drawn twice:\n%s", view)
	}
}

func TestDepTree_TheStatusBarAdvertisesTheKey(t *testing.T) {
	app := newTestApp(t, 140, 30, "serve")

	hints := app.statusBarHints()

	if !strings.Contains(hints, "[T]ree") {
		t.Errorf("item-row hints = %q, want the tree key beside the dependency keys", hints)
	}
}

func TestDepTree_BothPanesRenderInsideTheWindow(t *testing.T) {
	titles := []string{"root"}
	for i := 0; i < 14; i++ {
		titles = append(titles, "leaf"+string(rune('a'+i)))
	}
	app := newTestApp(t, 100, 14, titles...)
	for _, leaf := range titles[1:] {
		dependsOn(t, app, "root", leaf)
	}

	openTree(t, app)
	view := app.View()

	if lines := strings.Count(view, "\n") + 1; lines > 14 {
		t.Errorf("view is %d lines in a 14-line window:\n%s", lines, view)
	}
	if !strings.Contains(view, "/") {
		t.Errorf("no scroll position shown for a list taller than the pane:\n%s", view)
	}
}
