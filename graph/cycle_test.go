package graph

import "testing"

func TestWouldCycle_NoCycle(t *testing.T) {
	adj := map[string][]string{
		"a": {"b"},
		"b": {"c"},
	}
	if WouldCycle(adj, "a", "c") {
		t.Error("expected no cycle when adding a->c")
	}
}

func TestWouldCycle_DirectCycle(t *testing.T) {
	adj := map[string][]string{
		"a": {"b"},
	}
	if !WouldCycle(adj, "b", "a") {
		t.Error("expected cycle when adding b->a (creates a->b->a)")
	}
}

func TestWouldCycle_IndirectCycle(t *testing.T) {
	adj := map[string][]string{
		"a": {"b"},
		"b": {"c"},
	}
	if !WouldCycle(adj, "c", "a") {
		t.Error("expected cycle when adding c->a (creates a->b->c->a)")
	}
}

func TestWouldCycle_EmptyGraph(t *testing.T) {
	adj := map[string][]string{}
	if WouldCycle(adj, "a", "b") {
		t.Error("expected no cycle in empty graph")
	}
}

func TestWouldCycle_SelfLoop(t *testing.T) {
	adj := map[string][]string{}
	if !WouldCycle(adj, "a", "a") {
		t.Error("expected cycle for self-loop")
	}
}
