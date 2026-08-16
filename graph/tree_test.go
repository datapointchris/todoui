package graph

import (
	"strings"
	"testing"
)

func rowIDs(rows []Row) string {
	parts := make([]string, 0, len(rows))
	for _, r := range rows {
		id := r.ID
		if r.Repeated {
			id += "(*)"
		}
		parts = append(parts, id)
	}
	return strings.Join(parts, " ")
}

// a → b means a depends on b.
func diamond() *Tree {
	return BuildTree([]Node{
		{ID: "a", Order: 1, Deps: []string{"b", "c"}},
		{ID: "b", Order: 2, Deps: []string{"d"}},
		{ID: "c", Order: 3, Deps: []string{"d"}},
		{ID: "d", Order: 4, Deps: []string{"e"}},
		{ID: "e", Order: 5},
	})
}

func TestRows_SharedDependencyIsExpandedOnceThenMarked(t *testing.T) {
	tree := diamond()

	rows := tree.Rows(tree.Roots([]string{"a", "b", "c", "d", "e"}, false), false, Unlimited)

	want := "a b d e c d(*)"
	if got := rowIDs(rows); got != want {
		t.Errorf("rows = %q, want %q — the second d keeps its row and loses its subtree", got, want)
	}
}

func TestRows_ARepeatedLeafIsNotMarkedBecauseNothingWasElided(t *testing.T) {
	tree := BuildTree([]Node{
		{ID: "a", Order: 1, Deps: []string{"b", "c"}},
		{ID: "b", Order: 2, Deps: []string{"d"}},
		{ID: "c", Order: 3, Deps: []string{"d"}},
		{ID: "d", Order: 4},
	})

	if got := rowIDs(tree.Rows([]string{"a"}, false, Unlimited)); strings.Contains(got, "(*)") {
		t.Errorf("rows = %q, want no marker — d has no children to hide", got)
	}
}

func TestRoots_InvertSwapsWhichEndIsTheRoot(t *testing.T) {
	tree := diamond()
	members := []string{"a", "b", "c", "d", "e"}

	forward := tree.Roots(members, false)
	inverted := tree.Roots(members, true)

	if len(forward) != 1 || forward[0] != "a" {
		t.Errorf("forward roots = %v, want [a] — nothing depends on a", forward)
	}
	if len(inverted) != 1 || inverted[0] != "e" {
		t.Errorf("inverted roots = %v, want [e] — e depends on nothing", inverted)
	}
}

// The measured case: six items queued behind one. The forward drawing repeats
// the shared node once per root; the inverted one says it a single time.
func TestRows_InvertCollapsesAFanIntoOneTree(t *testing.T) {
	nodes := []Node{{ID: "keystone", Order: 1}}
	for _, id := range []string{"w", "x", "y", "z"} {
		nodes = append(nodes, Node{ID: id, Order: len(nodes) + 1, Deps: []string{"keystone"}})
	}
	tree := BuildTree(nodes)
	members := []string{"keystone", "w", "x", "y", "z"}

	forward := tree.Rows(tree.Roots(members, false), false, Unlimited)
	inverted := tree.Rows(tree.Roots(members, true), true, Unlimited)

	if len(forward) != 8 {
		t.Errorf("forward rows = %d (%s), want 8", len(forward), rowIDs(forward))
	}
	if len(inverted) != 5 {
		t.Errorf("inverted rows = %d (%s), want 5", len(inverted), rowIDs(inverted))
	}
}

func TestComponents_SeparatesUnconnectedTreesAndDropsIsolatedItems(t *testing.T) {
	tree := BuildTree([]Node{
		{ID: "a", Order: 1, Deps: []string{"b"}},
		{ID: "b", Order: 2},
		{ID: "lonely", Order: 3},
		{ID: "c", Order: 4, Deps: []string{"d"}},
		{ID: "d", Order: 5},
	})

	components := tree.Components()

	if len(components) != 2 {
		t.Fatalf("components = %v, want 2 — an item with no edges is in no tree", components)
	}
	for _, members := range components {
		for _, id := range members {
			if id == "lonely" {
				t.Errorf("components = %v, want lonely left out", components)
			}
		}
	}
}

func TestComponents_OrderFollowsTheItemNumberNotMapIteration(t *testing.T) {
	tree := BuildTree([]Node{
		{ID: "high", Order: 90, Deps: []string{"high-dep"}},
		{ID: "high-dep", Order: 91},
		{ID: "low", Order: 2, Deps: []string{"low-dep"}},
		{ID: "low-dep", Order: 3},
	})

	first := tree.Components()
	for i := 0; i < 20; i++ {
		if got := tree.Components(); got[0][0] != first[0][0] {
			t.Fatalf("components reordered between runs: %v then %v", first, got)
		}
	}
	if first[0][0] != "low" {
		t.Errorf("first component starts at %q, want low", first[0][0])
	}
}

func TestBuildTree_DropsAnEdgeToAnItemThatIsNotPresent(t *testing.T) {
	tree := BuildTree([]Node{{ID: "a", Order: 1, Deps: []string{"missing"}}})

	if tree.Connected("a") {
		t.Error("a is connected, want isolated — an edge to a missing item draws a row with no title")
	}
}

func TestRows_DepthStopsBelowTheLimit(t *testing.T) {
	tree := diamond()

	if got := rowIDs(tree.Rows([]string{"a"}, false, 1)); got != "a b c" {
		t.Errorf("rows at depth 1 = %q, want %q", got, "a b c")
	}
	if rows := tree.Rows([]string{"a"}, false, 0); len(rows) != 1 {
		t.Errorf("rows at depth 0 = %s, want the root alone", rowIDs(rows))
	}
}

// AddDependency refuses a cycle, so this guards against bad data. It must
// terminate rather than overflow the stack.
func TestRows_ACycleTerminatesInsteadOfRecursingForever(t *testing.T) {
	tree := BuildTree([]Node{
		{ID: "a", Order: 1, Deps: []string{"b"}},
		{ID: "b", Order: 2, Deps: []string{"a"}},
	})

	rows := tree.Rows(tree.Roots([]string{"a", "b"}, false), false, Unlimited)

	if len(rows) == 0 || len(rows) > 4 {
		t.Errorf("rows = %s, want a short marked walk", rowIDs(rows))
	}
	if !strings.Contains(rowIDs(rows), "(*)") {
		t.Errorf("rows = %s, want the closing edge marked", rowIDs(rows))
	}
}

func TestPrefix_ContinuationBarOnlyOnLevelsThatKeepGoing(t *testing.T) {
	if got := Prefix(Row{Last: []bool{false, true}}); got != "│   └── " {
		t.Errorf("prefix = %q, want the ancestor level to keep its bar", got)
	}
	if got := Prefix(Row{Last: []bool{true, true}}); got != "    └── " {
		t.Errorf("prefix = %q, want the ancestor level cleared to spaces", got)
	}
	if got := Prefix(Row{}); got != "" {
		t.Errorf("prefix = %q, want empty at a root", got)
	}
}

func TestChildren_FollowTheDrawingDirection(t *testing.T) {
	tree := diamond()

	if kids := tree.Children("b", false); len(kids) != 1 || kids[0] != "d" {
		t.Errorf("children(b) = %v, want [d] — what b waits on", kids)
	}
	if kids := tree.Children("d", true); len(kids) != 2 {
		t.Errorf("children(d, invert) = %v, want both items waiting on d", kids)
	}
}

func TestComponentOf_ReturnsNilForAnItemWithNoEdges(t *testing.T) {
	tree := BuildTree([]Node{{ID: "lonely", Order: 1}, {ID: "a", Order: 2, Deps: []string{"b"}}, {ID: "b", Order: 3}})

	if members := tree.ComponentOf("lonely"); members != nil {
		t.Errorf("ComponentOf(lonely) = %v, want nil", members)
	}
	if members := tree.ComponentOf("b"); len(members) != 2 {
		t.Errorf("ComponentOf(b) = %v, want both ends of the edge", members)
	}
}
