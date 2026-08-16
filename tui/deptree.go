package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/datapointchris/todoui/backend"
	"github.com/datapointchris/todoui/graph"
	"github.com/datapointchris/todoui/model"
)

// depTreeState is the dependency-tree overlay.
//
// roots is what the walk starts from, and drilling into a row replaces it —
// pushing the previous roots so the walk can be retraced. That is the tag-stack
// shape rather than a single fixed view, because following a chain and getting
// back is the whole reason to open this.
type depTreeState struct {
	tree   *graph.Tree
	lookup map[string]graph.ItemNode
	roots  []string
	rows   []graph.Row
	cursor int
	scroll int
	invert bool
	stack  []depTreeFrame
	// origin is the item the overlay opened on, so closing can return the main
	// pane's cursor to where it was rather than wherever the walk ended.
	origin string
}

type depTreeFrame struct {
	roots  []string
	invert bool
	cursor int
}

type depTreeMsg struct {
	tree   *graph.Tree
	lookup map[string]graph.ItemNode
	rootID string
}

// fetchDepTreeCmd reads the whole store in four queries. Every item is fetched
// whatever is being drawn: an edge crosses status and project boundaries, so a
// filtered read severs edges and splits one tree into several.
func fetchDepTreeCmd(b backend.Backend, rootID string) tea.Cmd {
	return func() tea.Msg {
		items, err := b.ListAllItemsIncludingArchived()
		if err != nil {
			return errMsg{err}
		}
		deps, err := b.ListDependencies()
		if err != nil {
			return errMsg{err}
		}
		memberships, err := b.ListMemberships()
		if err != nil {
			return errMsg{err}
		}
		projects, err := b.ListProjectsByStatus(model.StatusAll)
		if err != nil {
			return errMsg{err}
		}
		names := make(map[string]string, len(projects))
		for _, p := range projects {
			names[p.ID] = p.Name
		}
		tree, lookup := graph.BuildItems(items, deps, memberships, names)
		return depTreeMsg{tree: tree, lookup: lookup, rootID: rootID}
	}
}

// rebuild reflattens the rows after the roots or the direction changed, and
// keeps the cursor inside them.
func (s *depTreeState) rebuild() {
	if s.tree == nil {
		s.rows = nil
		return
	}
	s.rows = s.tree.Rows(s.roots, s.invert, graph.Unlimited)
	if s.cursor >= len(s.rows) {
		s.cursor = len(s.rows) - 1
	}
	if s.cursor < 0 {
		s.cursor = 0
	}
}

func (s *depTreeState) currentID() string {
	if s.cursor < 0 || s.cursor >= len(s.rows) {
		return ""
	}
	return s.rows[s.cursor].ID
}

// forestRoots is every tree that still has open work, which is the view the
// overlay shows when nothing in particular is being followed. Finished trees
// accumulate without bound and would fill the page with history.
func (s *depTreeState) forestRoots() []string {
	var roots []string
	for _, component := range s.tree.Components() {
		if !graph.HasOpenWork(component, s.lookup) {
			continue
		}
		roots = append(roots, s.tree.Roots(component, s.invert)...)
	}
	return roots
}

func (s *depTreeState) push(roots []string) {
	s.stack = append(s.stack, depTreeFrame{roots: s.roots, invert: s.invert, cursor: s.cursor})
	s.roots = roots
	s.cursor = 0
	s.scroll = 0
	s.rebuild()
}

// pop retraces one step. It reports false at the bottom of the stack, where
// there is nothing left to go back to and the overlay should close instead.
func (s *depTreeState) pop() bool {
	if len(s.stack) == 0 {
		return false
	}
	frame := s.stack[len(s.stack)-1]
	s.stack = s.stack[:len(s.stack)-1]
	s.roots = frame.roots
	s.invert = frame.invert
	s.cursor = frame.cursor
	s.rebuild()
	return true
}

func (m *App) handleDepTreeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := &m.depTree
	if s.tree == nil {
		return m, nil
	}

	switch msg.String() {
	case "esc", "q":
		m.closeDepTree()
		return m, nil

	case "j", "down":
		if s.cursor < len(s.rows)-1 {
			s.cursor++
			s.scroll = syncScroll(s.cursor, s.scroll, m.depTreeHeight())
		}
		return m, nil

	case "k", "up":
		if s.cursor > 0 {
			s.cursor--
			s.scroll = syncScroll(s.cursor, s.scroll, m.depTreeHeight())
		}
		return m, nil

	case "g":
		s.cursor = 0
		s.scroll = 0
		return m, nil

	case "G":
		s.cursor = len(s.rows) - 1
		s.scroll = syncScroll(s.cursor, s.scroll, m.depTreeHeight())
		return m, nil

	case "l", "right":
		// Drill: the row under the cursor becomes the root. A row with nothing
		// under it in this direction would redraw the same single line, so it
		// says why instead of looking broken.
		id := s.currentID()
		if id == "" {
			return m, nil
		}
		if len(s.tree.Children(id, s.invert)) == 0 {
			flashCmd := m.flash(m.depTreeDeadEnd())
			return m, flashCmd
		}
		s.push([]string{id})
		return m, nil

	case "h", "left", "backspace":
		if !s.pop() {
			m.closeDepTree()
		}
		return m, nil

	case "i":
		// Same roots, read the other way. The stack keeps the direction so
		// going back restores the view that was actually on screen.
		s.stack = append(s.stack, depTreeFrame{roots: s.roots, invert: s.invert, cursor: s.cursor})
		s.invert = !s.invert
		s.cursor = 0
		s.scroll = 0
		s.rebuild()
		return m, nil

	case "a":
		s.push(s.forestRoots())
		return m, nil

	case "enter":
		id := s.currentID()
		if id == "" {
			return m, nil
		}
		return m, fetchItemDetailCmd(m.backend, id, m.blockedSet[id])

	case "u":
		return m, undoCmd(m.backend)
	}

	return m, nil
}

// depTreeDeadEnd names what is missing in the direction being read, since "no
// children" means two different things depending on which way the edges run.
func (m *App) depTreeDeadEnd() string {
	if m.depTree.invert {
		return "Nothing depends on this item"
	}
	return "This item depends on nothing"
}

func (m *App) closeDepTree() {
	m.appMode = modeNormal
	m.depTree = depTreeState{}
}

// openDepTree enters the overlay on one item, which becomes the root.
func (m *App) openDepTree(itemID string) tea.Cmd {
	m.depTree = depTreeState{origin: itemID}
	return fetchDepTreeCmd(m.backend, itemID)
}

func (m *App) depTreeHeight() int {
	height := m.height - 8
	if height < 3 {
		height = 3
	}
	return height
}

func (m *App) renderDepTreeOverlay() string {
	s := &m.depTree
	direction := "waiting on"
	if s.invert {
		direction = "unblocks"
	}

	var lines []string
	lines = append(lines, overlayTitleStyle.Render(fmt.Sprintf("Dependencies — %s", direction)))
	lines = append(lines, "")

	switch {
	case s.tree == nil:
		lines = append(lines, dimStyle.Render("  Loading..."))
	case len(s.rows) == 0:
		lines = append(lines, dimStyle.Render("  No dependency trees."))
	case len(s.rows) == 1 && len(s.stack) == 0:
		lines = append(lines, m.depTreeRow(s.rows[0], 0))
		lines = append(lines, "")
		lines = append(lines, dimStyle.Render("  This item has no dependencies either way."))
	default:
		height := m.depTreeHeight()
		end := s.scroll + height
		if end > len(s.rows) {
			end = len(s.rows)
		}
		for i := s.scroll; i < end; i++ {
			lines = append(lines, m.depTreeRow(s.rows[i], i))
		}
		if end < len(s.rows) {
			lines = append(lines, dimStyle.Render(fmt.Sprintf("  … %d more, [j] to keep going", len(s.rows)-end)))
		}
	}

	lines = append(lines, "")
	lines = append(lines, dimStyle.Render("  [j/k]move  [l]drill in  [h]back  [i]flip  [a]all trees  [Enter]detail  [Esc]close"))

	boxWidth := m.width - 4
	if boxWidth > 90 {
		boxWidth = 90
	}
	if boxWidth < 40 {
		boxWidth = 40
	}
	return overlayBoxStyle.Width(boxWidth).Render(strings.Join(lines, "\n"))
}

func (m *App) depTreeRow(row graph.Row, index int) string {
	s := &m.depTree
	node := s.lookup[row.ID]

	mark := "○"
	switch {
	case node.Item.Archived:
		mark = "▪"
	case node.Item.Completed:
		mark = "✓"
	}

	title := node.Item.Title
	if node.Item.Completed || node.Item.Archived {
		title = taskCompletedStyle.Render(title)
	}

	line := fmt.Sprintf("%s%s %s %s", graph.Prefix(row), mark, itemHandle(node.Item), title)
	if tag := graph.CrossProjectTag(node, s.lookup[row.Parent], row.Parent != ""); tag != "" {
		line += blockerProjectStyle.Render(" [" + tag + "]")
	}
	if row.Repeated {
		line += dimStyle.Render(" (*)")
	}

	if index == s.cursor {
		return itemSelectedStyle.Render("> " + line)
	}
	return "  " + line
}
