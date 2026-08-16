package cli

import (
	"strings"
	"testing"

	"github.com/datapointchris/todoui/backend"
	"github.com/datapointchris/todoui/graph"
	"github.com/datapointchris/todoui/model"
)

// treeFixture builds a chain and a fork over an in-memory database, then loads
// it the way the command does.
//
//	serve → schema → bootstrap        (serve waits on schema, and so on)
//	cli   → serve
func treeFixture(t *testing.T) (backend.Backend, *graph.Tree, map[string]graph.ItemNode, map[string]string) {
	t.Helper()
	b := newNumberingTestBackend(t)

	project, err := b.CreateProject(model.CreateProject{Name: "day"})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	other, err := b.CreateProject(model.CreateProject{Name: "home"})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}

	ids := map[string]string{}
	for _, spec := range []struct {
		title   string
		project string
	}{
		{"serve", project.ID},
		{"schema", project.ID},
		{"bootstrap", project.ID},
		{"cli", other.ID},
	} {
		item, err := b.CreateItem(model.CreateProjectItem{Title: spec.title, ProjectIDs: []string{spec.project}})
		if err != nil {
			t.Fatalf("creating %s: %v", spec.title, err)
		}
		ids[spec.title] = item.ID
	}

	for _, edge := range [][2]string{{"serve", "schema"}, {"schema", "bootstrap"}, {"cli", "serve"}} {
		if err := b.AddDependency(ids[edge[0]], ids[edge[1]]); err != nil {
			t.Fatalf("adding %s -> %s: %v", edge[0], edge[1], err)
		}
	}

	tree, lookup, err := loadItemTree(b)
	if err != nil {
		t.Fatalf("loading tree: %v", err)
	}
	return b, tree, lookup, ids
}

func TestTreeLine_DrawsTheCornerTheMarkAndTheHandle(t *testing.T) {
	_, tree, lookup, ids := treeFixture(t)

	rows := tree.Rows([]string{ids["serve"]}, false, graph.Unlimited)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want serve and the two below it", len(rows))
	}

	root := treeLine(rows[0], lookup)
	child := treeLine(rows[1], lookup)

	if !strings.HasPrefix(root, "○ ") {
		t.Errorf("root = %q, want the open mark and no indent", root)
	}
	if !strings.Contains(root, "serve") {
		t.Errorf("root = %q, want the title", root)
	}
	if !strings.HasPrefix(child, "└── ") {
		t.Errorf("child = %q, want a last-child corner", child)
	}
	if !strings.Contains(child, "schema") {
		t.Errorf("child = %q, want the dependency's title", child)
	}
}

// The 7 edges of 121 that no single project's list can show.
func TestTreeLine_TagsADependencyThatLeavesItsParentsProject(t *testing.T) {
	_, tree, lookup, ids := treeFixture(t)

	rows := tree.Rows([]string{ids["cli"]}, false, graph.Unlimited)

	crossing := treeLine(rows[1], lookup) // cli is in "home", serve is in "day"
	sameProject := treeLine(rows[2], lookup)

	if !strings.Contains(crossing, "[day]") {
		t.Errorf("line = %q, want the project the dependency lands in", crossing)
	}
	if strings.Contains(sameProject, "[") {
		t.Errorf("line = %q, want no tag — both ends are in day", sameProject)
	}
}

func TestLoadItemTree_ReadsEveryItemSoAChainStaysWhole(t *testing.T) {
	b, _, _, ids := treeFixture(t)

	completed := true
	if _, err := b.UpdateItem(ids["schema"], model.UpdateProjectItem{Completed: &completed}); err != nil {
		t.Fatalf("completing schema: %v", err)
	}

	tree, lookup, err := loadItemTree(b)
	if err != nil {
		t.Fatalf("loading tree: %v", err)
	}
	rows := tree.Rows([]string{ids["serve"]}, false, graph.Unlimited)

	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 — a finished item mid-chain still joins the two halves", len(rows))
	}
	if !strings.Contains(treeLine(rows[1], lookup), "✓") {
		t.Errorf("line = %q, want the completed mark rather than a dropped row", treeLine(rows[1], lookup))
	}
}

func TestTreeDocument_EdgesKeepTheStoredDirectionUnderInvert(t *testing.T) {
	_, tree, lookup, ids := treeFixture(t)
	members := tree.ComponentOf(ids["serve"])

	forward := treeDocumentFor(tree, lookup, [][]string{{ids["cli"]}}, [][]string{members}, false, graph.Unlimited)
	inverted := treeDocumentFor(tree, lookup, [][]string{{ids["bootstrap"]}}, [][]string{members}, true, graph.Unlimited)

	if len(forward.Edges) != len(inverted.Edges) {
		t.Fatalf("edge counts differ: %d vs %d", len(forward.Edges), len(inverted.Edges))
	}
	for _, edge := range inverted.Edges {
		if edge.Item == ids["bootstrap"] {
			t.Errorf("edges = %+v, want none starting at bootstrap — it depends on nothing", inverted.Edges)
		}
	}
	if len(inverted.Roots) != 1 || inverted.Roots[0] != ids["bootstrap"] {
		t.Errorf("roots = %v, want bootstrap — only the roots follow the drawing", inverted.Roots)
	}
}

func TestTreeDocument_EmptySelectionEncodesAsEmptyArraysNotNull(t *testing.T) {
	_, tree, lookup, _ := treeFixture(t)

	doc := treeDocumentFor(tree, lookup, nil, nil, false, graph.Unlimited)

	if doc.Nodes == nil || doc.Edges == nil || doc.Roots == nil {
		t.Errorf("doc = %+v, want empty arrays — a consumer should not branch on null", doc)
	}
}

func TestHasOpenWork_FinishedChainIsHiddenFromTheForest(t *testing.T) {
	b, _, _, ids := treeFixture(t)

	completed := true
	for _, title := range []string{"bootstrap", "schema", "serve", "cli"} {
		if _, err := b.UpdateItem(ids[title], model.UpdateProjectItem{Completed: &completed}); err != nil {
			t.Fatalf("completing %s: %v", title, err)
		}
	}

	tree, lookup, err := loadItemTree(b)
	if err != nil {
		t.Fatalf("loading tree: %v", err)
	}
	for _, component := range tree.Components() {
		if graph.HasOpenWork(component, lookup) {
			t.Errorf("component %v still reports open work after everything was completed", component)
		}
	}
}
