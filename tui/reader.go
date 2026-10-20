package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	markdownansi "github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/datapointchris/todoui/model"
)

// The reader is a full screen showing one item or one project, its markdown
// rendered. Bubble Tea keeps only the last `height` lines of a view taller than
// the terminal, so a long note drawn whole loses its top. The reader instead
// builds every row it could show, each already wrapped to the width, and draws
// the window of them that fits.

func ptr[T any](v T) *T { return &v }

// readerMarkdownStyle is the one style every rendered note and description
// uses. Its colors are ANSI 0–15, so they follow the terminal's theme, which
// glamour's built-in 256-color styles do not. Code blocks name no chroma theme,
// which leaves them unhighlighted: every chroma theme is 256-color too.
var readerMarkdownStyle = markdownansi.StyleConfig{
	BlockQuote: markdownansi.StyleBlock{
		StylePrimitive: markdownansi.StylePrimitive{Color: ptr("8"), Italic: ptr(true)},
		Indent:         ptr(uint(1)),
		IndentToken:    ptr("│ "),
	},
	List: markdownansi.StyleList{LevelIndent: 2},
	Heading: markdownansi.StyleBlock{
		StylePrimitive: markdownansi.StylePrimitive{BlockSuffix: "\n", Color: ptr("4"), Bold: ptr(true)},
	},
	H1: markdownansi.StyleBlock{StylePrimitive: markdownansi.StylePrimitive{Prefix: "# ", Color: ptr("6")}},
	H2: markdownansi.StyleBlock{StylePrimitive: markdownansi.StylePrimitive{Prefix: "## "}},
	H3: markdownansi.StyleBlock{StylePrimitive: markdownansi.StylePrimitive{Prefix: "### "}},
	H4: markdownansi.StyleBlock{StylePrimitive: markdownansi.StylePrimitive{Prefix: "#### "}},
	H5: markdownansi.StyleBlock{StylePrimitive: markdownansi.StylePrimitive{Prefix: "##### "}},
	H6: markdownansi.StyleBlock{StylePrimitive: markdownansi.StylePrimitive{Prefix: "###### ", Bold: ptr(false)}},

	Strikethrough:  markdownansi.StylePrimitive{CrossedOut: ptr(true)},
	Emph:           markdownansi.StylePrimitive{Italic: ptr(true)},
	Strong:         markdownansi.StylePrimitive{Bold: ptr(true)},
	HorizontalRule: markdownansi.StylePrimitive{Color: ptr("8"), Format: "\n────────\n"},

	Item:        markdownansi.StylePrimitive{BlockPrefix: "• "},
	Enumeration: markdownansi.StylePrimitive{BlockPrefix: ". "},
	Task:        markdownansi.StyleTask{Ticked: "[✓] ", Unticked: "[ ] "},

	Link:      markdownansi.StylePrimitive{Color: ptr("4"), Underline: ptr(true)},
	LinkText:  markdownansi.StylePrimitive{Color: ptr("6"), Bold: ptr(true)},
	Image:     markdownansi.StylePrimitive{Color: ptr("5"), Underline: ptr(true)},
	ImageText: markdownansi.StylePrimitive{Color: ptr("8"), Format: "Image: {{.text}} →"},

	Code: markdownansi.StyleBlock{StylePrimitive: markdownansi.StylePrimitive{Color: ptr("3")}},
	CodeBlock: markdownansi.StyleCodeBlock{
		StyleBlock: markdownansi.StyleBlock{
			StylePrimitive: markdownansi.StylePrimitive{Color: ptr("3")},
			Margin:         ptr(uint(2)),
		},
	},
	Table: markdownansi.StyleTable{
		CenterSeparator: ptr("┼"),
		ColumnSeparator: ptr("│"),
		RowSeparator:    ptr("─"),
	},
	DefinitionDescription: markdownansi.StylePrimitive{BlockPrefix: "\n→ "},
}

// markdownRender is the last markdown the reader rendered. The reader redraws
// on every key and rendering is not free, so the rows are kept until the
// entity, the width or the text changes.
type markdownRender struct {
	entityID string
	width    int
	source   string
	rows     []string
}

func (m *App) renderedMarkdown(entityID, source string, width int) []string {
	c := &m.readerMarkdown
	if c.rows != nil && c.entityID == entityID && c.width == width && c.source == source {
		return c.rows
	}
	*c = markdownRender{entityID: entityID, width: width, source: source, rows: renderMarkdown(source, width)}
	return c.rows
}

// renderMarkdown renders source to rows no wider than width. A text glamour
// cannot render is shown as written rather than not at all.
func renderMarkdown(source string, width int) []string {
	out := source
	r, err := glamour.NewTermRenderer(glamour.WithStyles(readerMarkdownStyle), glamour.WithWordWrap(width))
	if err == nil {
		if rendered, err := r.Render(source); err == nil {
			out = rendered
		}
	}
	return wrapRows(strings.Trim(out, "\n"), width)
}

// wrapRows splits s into the rows it occupies at width. glamour wraps prose
// but not a long token or a wide table row, and the scroll counts on one entry
// being one row on screen.
func wrapRows(s string, width int) []string {
	return strings.Split(ansi.Wrap(s, width, ""), "\n")
}

// styledRows wraps plain text, then styles each row on its own, so a row
// scrolled to the top of the screen still carries its style.
func styledRows(style lipgloss.Style, text string, width int) []string {
	rows := wrapRows(text, width)
	for i, r := range rows {
		rows[i] = style.Render(r)
	}
	return rows
}

func readerSection(title string) string {
	return dimStyle.Render("─── " + title + " ───")
}

func (m *App) readingItem() bool {
	return m.appMode == modeReader && m.itemDetail != nil
}

// readerWidth leaves a column of margin on each side.
func (m *App) readerWidth() int {
	return max(m.width-2, 10)
}

// readerHeight is the rows the text gets: all of them bar the status bar.
func (m *App) readerHeight() int {
	return max(m.height-1, 1)
}

func (m *App) readerRows(width int) []string {
	switch {
	case m.itemDetail != nil:
		return m.itemReaderRows(width)
	case m.projectDetail != nil:
		return m.projectReaderRows(width)
	}
	return nil
}

func (m *App) itemReaderRows(width int) []string {
	d := m.itemDetail
	var rows []string
	add := func(style lipgloss.Style, text string) {
		rows = append(rows, styledRows(style, text, width)...)
	}

	status := "○ open"
	if d.Completed {
		status = "✓ done"
	}
	if d.Archived {
		status = "▪ archived"
	}
	var projectNames []string
	inItemProjects := make(map[string]bool)
	for _, p := range d.Projects {
		projectNames = append(projectNames, p.Name)
		inItemProjects[p.Name] = true
	}

	add(readerTitleStyle, d.Title)
	add(dimStyle, fmt.Sprintf("Item %s · %s", itemHandle(d.ProjectItem), status))
	add(dimStyle, "Projects: "+strings.Join(projectNames, ", "))
	if d.Repo != nil && *d.Repo != "" {
		add(dimStyle, "Repo: "+*d.Repo)
	}
	add(dimStyle, "Created: "+d.CreatedAt.Format("Jan 2, 2006"))

	if len(m.detailBlockers) > 0 {
		rows = append(rows, "", readerSection("Blocked by"))
		for _, b := range m.detailBlockers {
			// A blocker filed outside this item's projects is named by its own.
			prefix := ""
			if names := m.detailBlockerProjects[b.ID]; len(names) > 0 && !anyIn(names, inItemProjects) {
				prefix = names[0] + ": "
			}
			add(readerBlockerStyle, fmt.Sprintf("○ %s%s (%s)", prefix, b.Title, itemHandle(b)))
		}
	}

	if len(m.detailDependents) > 0 {
		rows = append(rows, "", readerSection("Blocks"))
		for _, dep := range m.detailDependents {
			if dep.Completed {
				add(taskCompletedStyle, fmt.Sprintf("✓ %s (%s)", dep.Title, itemHandle(dep)))
			} else {
				add(lipgloss.NewStyle(), fmt.Sprintf("○ %s (%s)", dep.Title, itemHandle(dep)))
			}
		}
	}

	if len(m.detailTasks) > 0 {
		done := 0
		for _, t := range m.detailTasks {
			if t.Completed {
				done++
			}
		}
		rows = append(rows, "", readerSection(fmt.Sprintf("Tasks (%d/%d)", done, len(m.detailTasks))))
		for _, t := range m.detailTasks {
			if t.Completed {
				add(taskCompletedStyle, "✓ "+t.Title)
			} else {
				add(lipgloss.NewStyle(), "○ "+t.Title)
			}
		}
	}

	if d.Notes != nil && strings.TrimSpace(*d.Notes) != "" {
		rows = append(rows, "", readerSection("Notes"), "")
		rows = append(rows, m.renderedMarkdown(d.ID, *d.Notes, width)...)
	}
	return rows
}

func (m *App) projectReaderRows(width int) []string {
	p := m.projectDetail
	var rows []string
	add := func(style lipgloss.Style, text string) {
		rows = append(rows, styledRows(style, text, width)...)
	}

	add(readerTitleStyle, p.Name)
	add(dimStyle, fmt.Sprintf("Project %s · %s", shortID(p.ID), p.Status))
	rows = append(rows, projectCounts(p.OpenCount(), p.CompletedCount)+dimStyle.Render("  open · done"))
	add(dimStyle, "Created: "+p.CreatedAt.Format("Jan 2, 2006"))
	if model.IsTerminalStatus(p.Status) {
		closed := "Closed"
		if p.ClosedAt != nil {
			closed += ": " + p.ClosedAt.Format("Jan 2, 2006")
		}
		add(dimStyle, closed)
		if p.StatusReason != nil && *p.StatusReason != "" {
			add(dimStyle, "Reason: "+*p.StatusReason)
		}
	}

	if p.Description != nil && strings.TrimSpace(*p.Description) != "" {
		rows = append(rows, "", readerSection("Description"), "")
		rows = append(rows, m.renderedMarkdown(p.ID, *p.Description, width)...)
	}
	return rows
}

func anyIn(names []string, set map[string]bool) bool {
	for _, n := range names {
		if set[n] {
			return true
		}
	}
	return false
}

// clampScroll keeps the window inside the rows: never above the first, and
// never so far down that the last row leaves the bottom of the screen.
func clampScroll(scroll, total, height int) int {
	return max(min(scroll, total-height), 0)
}

func (m *App) scrollReader(delta int) {
	total := len(m.readerRows(m.readerWidth()))
	m.readerScroll = clampScroll(m.readerScroll+delta, total, m.readerHeight())
}

func (m *App) renderReader() string {
	rows := m.readerRows(m.readerWidth())
	height := m.readerHeight()
	m.readerScroll = clampScroll(m.readerScroll, len(rows), height)

	var b strings.Builder
	for i := m.readerScroll; i < m.readerScroll+height; i++ {
		if i < len(rows) {
			b.WriteString(" " + rows[i])
		}
		b.WriteByte('\n')
	}
	b.WriteString(m.renderReaderBar(len(rows), height))
	return b.String()
}

func (m *App) readerHints() string {
	const scroll = "[esc]back [j/k]scroll [^d/^u]half page [g/G]top/bottom"
	if m.itemDetail != nil {
		return scroll + " [space]done [e]dit [n]otes [p]rojects [t]ask [b]lock [B]unblock [T]ree [x]archive [u]ndo"
	}
	if m.projectDetail != nil && model.IsTerminalStatus(m.projectDetail.Status) {
		return scroll + " [e]dit name [d]escription [r]eopen"
	}
	return scroll + " [e]dit name [d]escription [c]omplete [x]drop"
}

// renderReaderBar is the reader's status bar: the hints, or a flash, on the
// left and how far down the text the window sits on the right.
func (m *App) renderReaderBar(total, height int) string {
	last := min(m.readerScroll+height, total)
	position := fmt.Sprintf("%d/%d", last, total)
	positionWidth := lipgloss.Width(position)

	var left string
	switch {
	case m.errorMsg != "":
		left = errorMsgStyle.Render("Error: " + m.errorMsg)
	case m.statusMsg != "":
		left = statusMsgStyle.Render(m.statusMsg)
	default:
		left = clipHints(m.readerHints(), m.width-positionWidth-5)
	}
	padding := max(m.width-lipgloss.Width(left)-positionWidth-4, 1)
	return statusBarStyle.Render(fmt.Sprintf(" %s%s%s ", left, strings.Repeat(" ", padding), position))
}

func (m *App) handleReaderKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	half := max(m.readerHeight()/2, 1)
	key := msg.String()
	switch key {
	case "esc", "q", "h", "left":
		m.leaveReader()
		return m, nil
	case "j", "down":
		m.scrollReader(1)
		return m, nil
	case "k", "up":
		m.scrollReader(-1)
		return m, nil
	case "ctrl+d", "pgdown":
		m.scrollReader(half)
		return m, nil
	case "ctrl+u", "pgup":
		m.scrollReader(-half)
		return m, nil
	case "g", "home":
		m.readerScroll = 0
		return m, nil
	case "G", "end":
		m.scrollReader(len(m.readerRows(m.readerWidth())))
		return m, nil
	case "u":
		return m, undoCmd(m.backend)
	}

	var cmd tea.Cmd
	switch {
	case m.itemDetail != nil:
		cmd = m.handleItemReaderKey(key)
	case m.projectDetail != nil:
		cmd = m.handleProjectReaderKey(key)
	}
	return m, cmd
}

// handleItemReaderKey sends an action key to the handler the list uses, aimed
// at the item on screen. Archiving, linking and the browser each end somewhere
// other than this item's text, so the reader closes onto the list first.
func (m *App) handleItemReaderKey(key string) tea.Cmd {
	d := m.itemDetail
	incomplete := false
	for _, t := range m.detailTasks {
		if !t.Completed {
			incomplete = true
		}
	}
	target := itemActionTarget{item: d.ProjectItem, hasIncompleteTasks: incomplete, blocked: m.blockedSet[d.ID]}

	switch key {
	case "x", "b", "B", "T":
		m.leaveReaderForList()
	}
	cmd, _ := m.handleItemActionKey(key, target, modeReader)
	return cmd
}

func (m *App) handleProjectReaderKey(key string) tea.Cmd {
	p := m.projectDetail
	switch key {
	case "e":
		m.titleInput.SetValue(p.Name)
		m.titleInput.Placeholder = ""
		m.returnMode = modeReader
		m.appMode = modeEditProjectName
		return m.titleInput.Focus()

	case "d":
		desc := ""
		if p.Description != nil {
			desc = *p.Description
		}
		return m.openInEditor(editTarget{projectID: p.ID}, projectDescriptionFileName(p.Project), desc)

	case "c":
		if model.IsTerminalStatus(p.Status) {
			return m.flash("Already closed — r reopens it")
		}
		return setProjectStatusCmd(m.backend, p.ID, model.StatusCompleted, nil)

	case "x":
		if model.IsTerminalStatus(p.Status) {
			return m.flash("Already closed — r reopens it")
		}
		// A reason is required, so dropping is a prompt rather than a keypress:
		// "deferred" invites the same idea back, "dropped, and here is why" does not.
		m.titleInput.SetValue("")
		m.titleInput.Placeholder = "Why dropped rather than deferred?"
		m.returnMode = modeReader
		m.appMode = modeDropProject
		return m.titleInput.Focus()

	case "r":
		if !model.IsTerminalStatus(p.Status) {
			return m.flash("Already active")
		}
		return setProjectStatusCmd(m.backend, p.ID, model.StatusActive, nil)
	}
	return nil
}

// clearReader forgets what the reader shows. The scroll and the rendered
// markdown are kept: the first is reset by whoever opens the next entity, and
// the second checks its own key.
func (m *App) clearReader() {
	m.itemDetail = nil
	m.detailBlockers = nil
	m.detailBlockerProjects = nil
	m.detailDependents = nil
	m.detailTasks = nil
	m.projectDetail = nil
}

// leaveReader returns to where the reader was opened from: the dependency
// browser, or the list on the row the cursor was on.
func (m *App) leaveReader() {
	if m.detailFromTree && m.depTree.tree != nil {
		m.clearReader()
		m.detailFromTree = false
		m.appMode = modeDepTree
		return
	}
	m.leaveReaderForList()
}

func (m *App) leaveReaderForList() {
	m.clearReader()
	m.detailFromTree = false
	// The tree holds a snapshot of every item; drop it with the reader rather
	// than carrying it for the rest of the session.
	m.depTree = depTreeState{}
	m.appMode = modeNormal
}
