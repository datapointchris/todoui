package graph

import "testing"

func TestWouldCycle_NoCycle(t *testing.T) {
	adj := map[int64][]int64{
		1: {2},
		2: {3},
	}
	if WouldCycle(adj, 1, 3) {
		t.Error("expected no cycle when adding 1->3")
	}
}

func TestWouldCycle_DirectCycle(t *testing.T) {
	adj := map[int64][]int64{
		1: {2},
	}
	if !WouldCycle(adj, 2, 1) {
		t.Error("expected cycle when adding 2->1 (creates 1->2->1)")
	}
}

func TestWouldCycle_IndirectCycle(t *testing.T) {
	adj := map[int64][]int64{
		1: {2},
		2: {3},
	}
	if !WouldCycle(adj, 3, 1) {
		t.Error("expected cycle when adding 3->1 (creates 1->2->3->1)")
	}
}

func TestWouldCycle_EmptyGraph(t *testing.T) {
	adj := map[int64][]int64{}
	if WouldCycle(adj, 1, 2) {
		t.Error("expected no cycle in empty graph")
	}
}

func TestWouldCycle_SelfLoop(t *testing.T) {
	adj := map[int64][]int64{}
	if !WouldCycle(adj, 1, 1) {
		t.Error("expected cycle for self-loop")
	}
}
