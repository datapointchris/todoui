package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/datapointchris/todoui/internal/backend"
	"github.com/datapointchris/todoui/internal/model"
)

// commands takes a pointer to the Backend interface so that commands can be
// registered before PersistentPreRunE initializes the backend. Each RunE
// dereferences the pointer at execution time, when it's guaranteed to be set.
type commands struct {
	b *backend.Backend
}

func (c *commands) backend() backend.Backend { return *c.b }

// RegisterAll adds all CLI subcommands to the given parent command.
func RegisterAll(parent *cobra.Command, b *backend.Backend) {
	c := &commands{b: b}
	parent.AddCommand(c.addCmd())
	parent.AddCommand(c.doneCmd())
	parent.AddCommand(c.listCmd())
	parent.AddCommand(c.archiveCmd())
	parent.AddCommand(c.undoCmd())
	parent.AddCommand(c.projectsCmd())
}

func (c *commands) addCmd() *cobra.Command {
	var projects []string
	cmd := &cobra.Command{
		Use:   "add <title>",
		Short: "Add a new item",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if len(projects) == 0 {
				return fmt.Errorf("-p/--project is required")
			}
			projectIDs, err := resolveProjects(c.backend(), projects)
			if err != nil {
				return err
			}
			item, err := c.backend().CreateItem(model.CreateProjectItem{
				Title:      args[0],
				ProjectIDs: projectIDs,
			})
			if err != nil {
				return err
			}
			fmt.Printf("Created item %s: %s\n", shortID(item.ID), item.Title)
			for _, p := range item.Projects {
				fmt.Printf("  → %s\n", p.Name)
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVarP(&projects, "project", "p", nil, "project name (repeatable)")
	return cmd
}

func (c *commands) doneCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "done <id>",
		Short: "Mark an item as done",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			id := args[0]
			done := true
			item, err := c.backend().UpdateItem(id, model.UpdateProjectItem{Completed: &done})
			if err != nil {
				return err
			}
			fmt.Printf("Done: %s %s\n", shortID(item.ID), item.Title)
			return nil
		},
	}
}

func (c *commands) listCmd() *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List active items",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if project != "" {
				return listByProject(c.backend(), project)
			}
			return listAll(c.backend())
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "filter by project name")
	return cmd
}

func (c *commands) archiveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "archive <id>",
		Short: "Archive an item",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			id := args[0]
			archived := true
			item, err := c.backend().UpdateItem(id, model.UpdateProjectItem{Archived: &archived})
			if err != nil {
				return err
			}
			fmt.Printf("Archived: %s %s\n", shortID(item.ID), item.Title)
			return nil
		},
	}
}

func (c *commands) undoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "undo",
		Short: "Undo the last action",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			desc, err := c.backend().Undo()
			if err != nil {
				return err
			}
			fmt.Printf("Undone: %s\n", desc)
			return nil
		},
	}
}

func (c *commands) projectsCmd() *cobra.Command {
	var addProject string
	var removeProject string
	cmd := &cobra.Command{
		Use:   "projects <id>",
		Short: "View or manage an item's project memberships",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			id := args[0]
			b := c.backend()
			if addProject != "" {
				return addItemToProject(b, id, addProject)
			}
			if removeProject != "" {
				return removeItemFromProject(b, id, removeProject)
			}
			return showItemProjects(b, id)
		},
	}
	cmd.Flags().StringVar(&addProject, "add", "", "add item to this project")
	cmd.Flags().StringVar(&removeProject, "remove", "", "remove item from this project")
	return cmd
}

// --- helpers ---

func shortID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}

func resolveProjects(b backend.Backend, names []string) ([]string, error) {
	allProjects, err := b.ListProjects()
	if err != nil {
		return nil, fmt.Errorf("listing projects: %w", err)
	}
	byName := make(map[string]string, len(allProjects))
	for _, p := range allProjects {
		byName[strings.ToLower(p.Name)] = p.ID
	}
	ids := make([]string, 0, len(names))
	for _, name := range names {
		id, ok := byName[strings.ToLower(name)]
		if !ok {
			return nil, fmt.Errorf("project %q not found", name)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func findProjectByName(b backend.Backend, name string) (string, error) {
	ids, err := resolveProjects(b, []string{name})
	if err != nil {
		return "", err
	}
	return ids[0], nil
}

func listAll(b backend.Backend) error {
	projects, err := b.ListProjects()
	if err != nil {
		return err
	}
	for _, p := range projects {
		items, err := b.ListItemsByProject(p.ID)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			continue
		}
		fmt.Printf("%s (%d)\n", p.Name, p.ItemCount)
		for _, item := range items {
			printItem(item.ProjectItem)
		}
		fmt.Println()
	}
	return nil
}

func listByProject(b backend.Backend, name string) error {
	projectID, err := findProjectByName(b, name)
	if err != nil {
		return err
	}
	items, err := b.ListItemsByProject(projectID)
	if err != nil {
		return err
	}
	for _, item := range items {
		printItem(item.ProjectItem)
	}
	return nil
}

func showItemProjects(b backend.Backend, id string) error {
	item, err := b.GetItem(id)
	if err != nil {
		return err
	}
	fmt.Printf("%s %s\n", shortID(item.ID), item.Title)
	fmt.Println("Projects:")
	for _, p := range item.Projects {
		fmt.Printf("  • %s\n", p.Name)
	}
	return nil
}

func addItemToProject(b backend.Backend, itemID string, projectName string) error {
	projectID, err := findProjectByName(b, projectName)
	if err != nil {
		return err
	}
	if err := b.AddToProject(itemID, projectID); err != nil {
		return err
	}
	fmt.Printf("Added %s to %s\n", shortID(itemID), projectName)
	return nil
}

func removeItemFromProject(b backend.Backend, itemID string, projectName string) error {
	projectID, err := findProjectByName(b, projectName)
	if err != nil {
		return err
	}
	if err := b.RemoveFromProject(itemID, projectID); err != nil {
		return err
	}
	fmt.Printf("Removed %s from %s\n", shortID(itemID), projectName)
	return nil
}

func printItem(item model.ProjectItem) {
	marker := "○"
	if item.Completed {
		marker = "✓"
	}
	fmt.Printf("  %s %-8s %s\n", marker, shortID(item.ID), item.Title)
}
