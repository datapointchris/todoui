package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/datapointchris/todoui/backend"
	"github.com/datapointchris/todoui/graph"
	"github.com/datapointchris/todoui/model"
)

func (c *commands) addDependencyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add-dependency <item-id> <depends-on-id>",
		Short: "Record that an item is blocked by another item",
		Long:  "The item cannot be completed until the item it depends on is. Cycles are rejected.",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			b := c.backend()
			itemID, dependsOnID, err := resolveItemPair(b, args[0], args[1])
			if err != nil {
				return err
			}
			if err := b.AddDependency(itemID, dependsOnID); err != nil {
				return err
			}
			fmt.Printf("%s now depends on %s\n", itemHandleByID(b, itemID), itemHandleByID(b, dependsOnID))
			return nil
		},
	}
}

func (c *commands) removeDependencyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove-dependency <item-id> <depends-on-id>",
		Short: "Remove a dependency edge between two items",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			b := c.backend()
			itemID, dependsOnID, err := resolveItemPair(b, args[0], args[1])
			if err != nil {
				return err
			}
			if err := b.RemoveDependency(itemID, dependsOnID); err != nil {
				return err
			}
			fmt.Printf("%s no longer depends on %s\n", itemHandleByID(b, itemID), itemHandleByID(b, dependsOnID))
			return nil
		},
	}
}

func (c *commands) blockersCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "blockers <item-id>",
		Short: "List the incomplete dependencies blocking an item",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			b := c.backend()
			itemID, err := resolveItemID(b, args[0])
			if err != nil {
				return err
			}
			blockers, err := b.GetBlockers(itemID)
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(blockers)
			}
			for _, blocker := range blockers {
				printItem(blocker)
			}
			return nil
		},
	}
	addJSONFlag(cmd, &asJSON)
	return cmd
}

func (c *commands) blockedCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "blocked",
		Short: "List items with at least one incomplete dependency",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			items, err := c.backend().ListBlocked()
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(items)
			}
			for _, item := range items {
				printItem(item)
			}
			return nil
		},
	}
	addJSONFlag(cmd, &asJSON)
	return cmd
}

func (c *commands) treeCmd() *cobra.Command {
	var (
		asJSON bool
		invert bool
		depth  int
		all    bool
	)
	cmd := &cobra.Command{
		Use:   "tree [item-id]",
		Short: "Draw the dependency trees, or the one an item sits in",
		Long: "Name an item to draw the tree it belongs to, with that item as the root.\n" +
			"Name nothing to draw every tree.\n" +
			"\n" +
			"A child is something its parent is waiting on. --invert reads the edges\n" +
			"the other way, so the children become the work the root unblocks.\n" +
			"\n" +
			"A tree with no open work is hidden unless --all is passed. Rows are never\n" +
			"dropped from a tree that is drawn: removing one would orphan everything\n" +
			"below it, and a finished item mid-chain is what holds the two halves\n" +
			"together.\n" +
			"\n" +
			"(*) marks an item whose dependencies were already drawn higher up.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b := c.backend()
			tree, lookup, err := loadItemTree(b)
			if err != nil {
				return err
			}

			maxDepth := graph.Unlimited
			if cmd.Flags().Changed("depth") {
				if depth < 0 {
					return fmt.Errorf("--depth cannot be negative")
				}
				maxDepth = depth
			}

			var drawings [][]string
			var members [][]string
			if len(args) == 1 {
				itemID, err := resolveItemID(b, args[0])
				if err != nil {
					return err
				}
				component := tree.ComponentOf(itemID)
				if component == nil {
					component = []string{itemID}
					fmt.Fprintf(os.Stderr, "%s has no dependencies either way.\n", itemHandle(lookup[itemID].Item))
				}
				drawings = append(drawings, []string{itemID})
				members = append(members, component)
			} else {
				for _, component := range tree.Components() {
					if !all && !graph.HasOpenWork(component, lookup) {
						continue
					}
					drawings = append(drawings, tree.Roots(component, invert))
					members = append(members, component)
				}
			}

			if asJSON {
				return printJSON(treeDocumentFor(tree, lookup, drawings, members, invert, maxDepth))
			}
			printTrees(tree, lookup, drawings, invert, maxDepth)
			if !all && len(args) == 0 {
				fmt.Fprintln(os.Stderr, "\nTrees with no open work are hidden: todoui tree --all")
			}
			return nil
		},
	}
	addJSONFlag(cmd, &asJSON)
	cmd.Flags().BoolVar(&invert, "invert", false, "draw what each item unblocks instead of what it waits on")
	cmd.Flags().IntVar(&depth, "depth", 0, "stop drawing below this many levels (default every level)")
	cmd.Flags().BoolVar(&all, "all", false, "include trees with no open work")
	return cmd
}

// loadItemTree reads the whole store in four queries. Every item is fetched
// whatever is being drawn, because an edge crosses status and project
// boundaries and a subset would sever it.
func loadItemTree(b backend.Backend) (*graph.Tree, map[string]graph.ItemNode, error) {
	items, err := b.ListAllItemsIncludingArchived()
	if err != nil {
		return nil, nil, err
	}
	deps, err := b.ListDependencies()
	if err != nil {
		return nil, nil, err
	}
	memberships, err := b.ListMemberships()
	if err != nil {
		return nil, nil, err
	}
	projects, err := b.ListProjectsByStatus(model.StatusAll)
	if err != nil {
		return nil, nil, err
	}
	names := make(map[string]string, len(projects))
	for _, p := range projects {
		names[p.ID] = p.Name
	}
	tree, lookup := graph.BuildItems(items, deps, memberships, names)
	return tree, lookup, nil
}

func printTrees(tree *graph.Tree, lookup map[string]graph.ItemNode, drawings [][]string, invert bool, maxDepth int) {
	if len(drawings) == 0 {
		fmt.Println("No dependency trees.")
		return
	}
	for i, roots := range drawings {
		if i > 0 {
			fmt.Println()
		}
		for _, row := range tree.Rows(roots, invert, maxDepth) {
			fmt.Println(treeLine(row, lookup))
		}
	}
}

func treeLine(row graph.Row, lookup map[string]graph.ItemNode) string {
	node := lookup[row.ID]
	line := fmt.Sprintf("%s%s %s %s", graph.Prefix(row), itemMark(node.Item), itemHandle(node.Item), node.Item.Title)
	if tag := graph.CrossProjectTag(node, lookup[row.Parent], row.Parent != ""); tag != "" {
		line += " [" + tag + "]"
	}
	if row.Repeated {
		line += " (*)"
	}
	return line
}

func itemMark(item model.ProjectItem) string {
	switch {
	case item.Archived:
		return "▪"
	case item.Completed:
		return "✓"
	default:
		return "○"
	}
}

// treeDocument is the machine rendering: the nodes and the edges between them,
// never the drawing. A consumer wanting another shape derives it from these
// rather than parsing box-drawing characters.
type treeDocument struct {
	Nodes []treeNode `json:"nodes"`
	Edges []treeEdge `json:"edges"`
	Roots []string   `json:"roots"`
}

type treeNode struct {
	ID       string   `json:"id"`
	Number   *int     `json:"number,omitempty"`
	Title    string   `json:"title"`
	Status   string   `json:"status"`
	Repo     *string  `json:"repo,omitempty"`
	Projects []string `json:"projects"`
}

// treeEdge always runs from the item to the thing it depends on, whichever way
// the drawing was asked to run. Only Roots follows --invert.
type treeEdge struct {
	Item      string `json:"item"`
	DependsOn string `json:"depends_on"`
}

func treeDocumentFor(tree *graph.Tree, lookup map[string]graph.ItemNode, drawings, members [][]string, invert bool, maxDepth int) treeDocument {
	doc := treeDocument{Nodes: []treeNode{}, Edges: []treeEdge{}, Roots: []string{}}
	for i, roots := range drawings {
		drawn := make(map[string]bool)
		for _, row := range tree.Rows(roots, invert, maxDepth) {
			drawn[row.ID] = true
		}
		for _, id := range members[i] {
			if !drawn[id] {
				continue
			}
			node := lookup[id]
			doc.Nodes = append(doc.Nodes, treeNode{
				ID:       node.Item.ID,
				Number:   node.Item.Number,
				Title:    node.Item.Title,
				Status:   itemStatusWord(node.Item),
				Repo:     node.Item.Repo,
				Projects: node.Projects,
			})
			for _, dep := range tree.Children(id, false) {
				if drawn[dep] {
					doc.Edges = append(doc.Edges, treeEdge{Item: id, DependsOn: dep})
				}
			}
		}
		doc.Roots = append(doc.Roots, roots...)
	}
	return doc
}

func itemStatusWord(item model.ProjectItem) string {
	switch {
	case item.Archived:
		return "archived"
	case item.Completed:
		return "completed"
	default:
		return "open"
	}
}

func resolveItemPair(b backend.Backend, first, second string) (string, string, error) {
	firstID, err := resolveItemID(b, first)
	if err != nil {
		return "", "", err
	}
	secondID, err := resolveItemID(b, second)
	if err != nil {
		return "", "", err
	}
	return firstID, secondID, nil
}
