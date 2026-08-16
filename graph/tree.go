package graph

import "sort"

// Node is one item as the tree builder sees it. Order is the stable sort key
// for siblings and roots — the item number, so a drawing is reproducible
// between runs rather than following map iteration.
type Node struct {
	ID    string
	Order int
	Deps  []string
}

// Row is one line of a drawn tree.
//
// Last carries an is-last-child flag per ancestor level, which is what decides
// whether a level continues with "│" or clears to spaces. A renderer cannot
// derive it from Depth alone.
type Row struct {
	ID     string
	Depth  int
	Last   []bool
	Parent string
	// Repeated marks a node whose children were already drawn under an earlier
	// parent. Only a node that actually has children is ever marked, because
	// the marker's job is to say something was elided.
	Repeated bool
}

// Unlimited is the depth that draws every level.
const Unlimited = -1

// Tree indexes dependency edges both ways and flattens them into drawable rows.
// It is the shape `icb projects items tree` draws over the API; the two are
// kept identical so one dataset reads the same through either front door.
type Tree struct {
	nodes map[string]Node
	deps  map[string][]string
	users map[string][]string
	ids   []string
}

// BuildTree indexes the nodes. An edge naming an id that is not in nodes is
// dropped: a caller may hold a subset, and a dangling edge would otherwise draw
// a row with no title.
func BuildTree(nodes []Node) *Tree {
	t := &Tree{
		nodes: make(map[string]Node, len(nodes)),
		deps:  make(map[string][]string),
		users: make(map[string][]string),
		ids:   make([]string, 0, len(nodes)),
	}
	for _, n := range nodes {
		t.nodes[n.ID] = n
		t.ids = append(t.ids, n.ID)
	}
	for _, n := range nodes {
		for _, dep := range n.Deps {
			if _, ok := t.nodes[dep]; !ok {
				continue
			}
			t.deps[n.ID] = append(t.deps[n.ID], dep)
			t.users[dep] = append(t.users[dep], n.ID)
		}
	}
	t.sortByOrder(t.ids)
	for id := range t.deps {
		t.sortByOrder(t.deps[id])
	}
	for id := range t.users {
		t.sortByOrder(t.users[id])
	}
	return t
}

func (t *Tree) sortByOrder(ids []string) {
	sort.Slice(ids, func(i, j int) bool {
		a, b := t.nodes[ids[i]], t.nodes[ids[j]]
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.ID < b.ID
	})
}

// Connected reports whether an id has any edge at all.
func (t *Tree) Connected(id string) bool {
	return len(t.deps[id]) > 0 || len(t.users[id]) > 0
}

// Components returns the weakly connected components, each Order-sorted, the
// components themselves ordered by their lowest member. Isolated nodes are left
// out — a tree of one has no shape to show.
func (t *Tree) Components() [][]string {
	parent := make(map[string]string, len(t.ids))
	var find func(string) string
	find = func(x string) string {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	for _, id := range t.ids {
		parent[id] = id
	}
	for _, id := range t.ids {
		for _, dep := range t.deps[id] {
			if a, b := find(id), find(dep); a != b {
				parent[a] = b
			}
		}
	}

	grouped := make(map[string][]string)
	for _, id := range t.ids {
		if !t.Connected(id) {
			continue
		}
		root := find(id)
		grouped[root] = append(grouped[root], id)
	}

	components := make([][]string, 0, len(grouped))
	for _, members := range grouped {
		t.sortByOrder(members)
		components = append(components, members)
	}
	sort.Slice(components, func(i, j int) bool {
		a, b := t.nodes[components[i][0]], t.nodes[components[j][0]]
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.ID < b.ID
	})
	return components
}

// ComponentOf returns the component holding id, or nil when the item has no
// edges.
func (t *Tree) ComponentOf(id string) []string {
	for _, members := range t.Components() {
		for _, member := range members {
			if member == id {
				return members
			}
		}
	}
	return nil
}

// Roots returns the members nothing else in the drawing direction points at.
//
// A row's children are the things it waits on, so the thing pointing AT a node
// is whatever depends on it. Inverted, the drawing runs the other way and the
// two swap. A component that is entirely a cycle has no root at all, and
// returning nothing would silently drop items, so its lowest member stands in.
func (t *Tree) Roots(members []string, invert bool) []string {
	incoming := t.users
	if invert {
		incoming = t.deps
	}
	inComponent := make(map[string]bool, len(members))
	for _, id := range members {
		inComponent[id] = true
	}

	var roots []string
	for _, id := range members {
		pointed := false
		for _, other := range incoming[id] {
			if inComponent[other] {
				pointed = true
				break
			}
		}
		if !pointed {
			roots = append(roots, id)
		}
	}
	if len(roots) == 0 && len(members) > 0 {
		roots = []string{members[0]}
	}
	t.sortByOrder(roots)
	return roots
}

// Children returns what a node points at in the drawing direction: what it
// waits on, or inverted, what it unblocks.
func (t *Tree) Children(id string, invert bool) []string {
	if invert {
		return t.users[id]
	}
	return t.deps[id]
}

// Rows walks the trees under roots and flattens them into drawable lines.
//
// A node expanded once is not expanded again: the second appearance is marked
// Repeated and its children are left off, which keeps a shared dependency from
// multiplying the drawing. maxDepth of Unlimited draws every level; 0 draws the
// roots alone.
func (t *Tree) Rows(roots []string, invert bool, maxDepth int) []Row {
	expanded := make(map[string]bool)
	ancestors := make(map[string]bool)
	var rows []Row

	var walk func(id, parent string, last []bool)
	walk = func(id, parent string, last []bool) {
		depth := len(last)
		kids := t.Children(id, invert)
		// An ancestor reappearing is a cycle. AddDependency refuses to create
		// one, so this guards against bad data rather than an expected shape —
		// draw the row, mark it, and stop before recursing forever.
		repeated := (expanded[id] || ancestors[id]) && len(kids) > 0
		rows = append(rows, Row{ID: id, Depth: depth, Last: append([]bool(nil), last...), Parent: parent, Repeated: repeated})
		if repeated || len(kids) == 0 {
			return
		}
		if maxDepth != Unlimited && depth >= maxDepth {
			return
		}
		expanded[id] = true
		ancestors[id] = true
		for i, kid := range kids {
			next := make([]bool, len(last)+1)
			copy(next, last)
			next[len(last)] = i == len(kids)-1
			walk(kid, id, next)
		}
		ancestors[id] = false
	}

	for _, root := range roots {
		walk(root, "", nil)
	}
	return rows
}

// FilterRows narrows a drawn forest to the rows on a path to a match, keeping
// a row when it matches or anything under it does.
//
// The corner flags are recomputed rather than carried over: dropping a row
// changes which of its siblings is last, so reusing the originals draws a
// branch hanging off nothing. The kept set stays ancestor-closed and in walk
// order, which is what makes one forward pass enough.
func FilterRows(rows []Row, matches func(id string) bool) []Row {
	keep := make([]bool, len(rows))
	for i, row := range rows {
		if !matches(row.ID) {
			continue
		}
		keep[i] = true
		// Mark the path back to the root: the nearest earlier row at each
		// shallower depth is this row's ancestor.
		depth := row.Depth
		for j := i - 1; j >= 0 && depth > 0; j-- {
			if rows[j].Depth < depth {
				keep[j] = true
				depth = rows[j].Depth
			}
		}
	}

	kept := make([]Row, 0, len(rows))
	for i, row := range rows {
		if keep[i] {
			kept = append(kept, row)
		}
	}
	return recomputeCorners(kept)
}

// recomputeCorners rebuilds each row's Last flags from the rows that survived.
func recomputeCorners(rows []Row) []Row {
	isLast := make([]bool, len(rows))
	for i, row := range rows {
		isLast[i] = true
		for j := i + 1; j < len(rows); j++ {
			if rows[j].Depth < row.Depth {
				break
			}
			if rows[j].Depth == row.Depth {
				isLast[i] = false
				break
			}
		}
	}

	var flags []bool
	out := make([]Row, len(rows))
	for i, row := range rows {
		out[i] = row
		if row.Depth == 0 {
			flags = flags[:0]
			out[i].Last = nil
			continue
		}
		if len(flags) > row.Depth-1 {
			flags = flags[:row.Depth-1]
		}
		flags = append(flags, isLast[i])
		out[i].Last = append([]bool(nil), flags...)
	}
	return out
}

// Prefix renders a row's box-drawing indent.
func Prefix(row Row) string {
	var b []rune
	for i, last := range row.Last {
		switch {
		case i < len(row.Last)-1 && last:
			b = append(b, []rune("    ")...)
		case i < len(row.Last)-1:
			b = append(b, []rune("│   ")...)
		case last:
			b = append(b, []rune("└── ")...)
		default:
			b = append(b, []rune("├── ")...)
		}
	}
	return string(b)
}
