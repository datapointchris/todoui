package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/datapointchris/todoui/backend"
	"github.com/datapointchris/todoui/model"
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
	parent.AddCommand(c.reopenCmd())
	parent.AddCommand(c.listCmd())
	parent.AddCommand(c.viewCmd())
	parent.AddCommand(c.editCmd())
	parent.AddCommand(c.searchCmd())
	parent.AddCommand(c.deleteCmd())
	parent.AddCommand(c.archiveCmd())
	parent.AddCommand(c.unarchiveCmd())
	parent.AddCommand(c.reorderCmd())
	parent.AddCommand(c.tasksCmd())
	parent.AddCommand(c.addTaskCmd())
	parent.AddCommand(c.completeTaskCmd())
	parent.AddCommand(c.editTaskCmd())
	parent.AddCommand(c.removeTaskCmd())
	parent.AddCommand(c.addDependencyCmd())
	parent.AddCommand(c.removeDependencyCmd())
	parent.AddCommand(c.blockersCmd())
	parent.AddCommand(c.blockedCmd())
	parent.AddCommand(c.undoCmd())
	parent.AddCommand(c.projectsCmd())
	parent.AddCommand(updateCmd())
	parent.AddCommand(versionCmd())
}

func (c *commands) addCmd() *cobra.Command {
	var (
		projects []string
		repo     string
		notes    string
	)
	cmd := &cobra.Command{
		Use:   "add <title>",
		Short: "Add a new item",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(projects) == 0 {
				return fmt.Errorf("-p/--project is required")
			}
			projectIDs, err := resolveProjects(c.backend(), projects)
			if err != nil {
				return err
			}
			input := model.CreateProjectItem{
				Title:      args[0],
				ProjectIDs: projectIDs,
			}
			if cmd.Flags().Changed("repo") {
				input.Repo = &repo
			}
			if cmd.Flags().Changed("notes") {
				input.Notes = &notes
			}
			item, err := c.backend().CreateItem(input)
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
	cmd.Flags().StringVarP(&repo, "repo", "r", "", "repo this item is work on, by ~/dev/repos.json name")
	cmd.Flags().StringVarP(&notes, "notes", "n", "", "markdown notes for the item")
	return cmd
}

func (c *commands) doneCmd() *cobra.Command {
	return c.completionCmd("done", "Mark an item as done", true, "Done")
}

func (c *commands) reopenCmd() *cobra.Command {
	return c.completionCmd("reopen", "Reopen a completed item", false, "Reopened")
}

func (c *commands) completionCmd(use, short string, completed bool, verb string) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <id>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			b := c.backend()
			id, err := resolveItemID(b, args[0])
			if err != nil {
				return err
			}
			item, err := b.UpdateItem(id, model.UpdateProjectItem{Completed: &completed})
			if err != nil {
				return err
			}
			fmt.Printf("%s: %s %s\n", verb, shortID(item.ID), item.Title)
			return nil
		},
	}
}

func (c *commands) listCmd() *cobra.Command {
	var (
		project  string
		archived bool
		asJSON   bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List active items",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			b := c.backend()
			if archived {
				if project == "" {
					return fmt.Errorf("--archived needs -p/--project")
				}
				return listArchived(b, project, asJSON)
			}
			if project != "" {
				return listByProject(b, project, asJSON)
			}
			return listAll(b, asJSON)
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "filter by project name")
	cmd.Flags().BoolVar(&archived, "archived", false, "list a project's archived items instead")
	addJSONFlag(cmd, &asJSON)
	return cmd
}

func (c *commands) archiveCmd() *cobra.Command {
	return c.archivalCmd("archive", "Archive an item", true, "Archived")
}

func (c *commands) unarchiveCmd() *cobra.Command {
	return c.archivalCmd("unarchive", "Restore an archived item", false, "Unarchived")
}

func (c *commands) archivalCmd(use, short string, archived bool, verb string) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <id>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			b := c.backend()
			id, err := resolveItemID(b, args[0])
			if err != nil {
				return err
			}
			item, err := b.UpdateItem(id, model.UpdateProjectItem{Archived: &archived})
			if err != nil {
				return err
			}
			fmt.Printf("%s: %s %s\n", verb, shortID(item.ID), item.Title)
			return nil
		},
	}
}

func (c *commands) reorderCmd() *cobra.Command {
	var (
		project  string
		position int
	)
	cmd := &cobra.Command{
		Use:   "reorder <id> --project <name> --position <n>",
		Short: "Move an item to a new position within a project",
		Long:  "Position is per-project — an item in several projects has an independent position in each.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				return fmt.Errorf("--project is required")
			}
			if !cmd.Flags().Changed("position") {
				return fmt.Errorf("--position is required")
			}
			b := c.backend()
			itemID, err := resolveItemID(b, args[0])
			if err != nil {
				return err
			}
			projectID, err := findProjectByName(b, project)
			if err != nil {
				return err
			}
			if err := b.ReorderItem(itemID, projectID, position); err != nil {
				return err
			}
			fmt.Printf("Moved %s to position %d in %s\n", shortID(itemID), position, project)
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "project the item is being reordered within")
	cmd.Flags().IntVar(&position, "position", 0, "new position")
	return cmd
}

func (c *commands) undoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "undo",
		Short: "Undo the last action",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			result, err := c.backend().Undo()
			if err != nil {
				return err
			}
			fmt.Printf("Undone: %s\n", result.Description)
			return nil
		},
	}
}

func (c *commands) viewCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "view <id>",
		Short: "Show an item with its projects, tasks, and blockers",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			b := c.backend()
			item, err := resolveItem(b, args[0])
			if err != nil {
				return err
			}
			tasks, err := b.ListTasks(item.ID)
			if err != nil {
				return err
			}
			blockers, err := b.GetBlockers(item.ID)
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(itemDetailJSON{
					ProjectItemDetail: item,
					Tasks:             tasks,
					Blockers:          blockers,
				})
			}
			printItemDetail(item, tasks, blockers)
			return nil
		},
	}
	addJSONFlag(cmd, &asJSON)
	return cmd
}

func (c *commands) editCmd() *cobra.Command {
	var (
		title string
		notes string
		repo  string
	)
	cmd := &cobra.Command{
		Use:   "edit <id> [flags]",
		Short: "Change an item's title, notes, or repo",
		Long:  "Update only the fields whose flags you pass.\n\nPass --repo \"\" to unlink an item from its repo.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f := cmd.Flags()
			input := model.UpdateProjectItem{}
			if f.Changed("title") {
				input.Title = &title
			}
			if f.Changed("notes") {
				input.Notes = &notes
			}
			if f.Changed("repo") {
				input.Repo = &repo
			}
			if input == (model.UpdateProjectItem{}) {
				return fmt.Errorf("nothing to change — pass --title, --notes, and/or --repo")
			}
			b := c.backend()
			id, err := resolveItemID(b, args[0])
			if err != nil {
				return err
			}
			item, err := b.UpdateItem(id, input)
			if err != nil {
				return err
			}
			fmt.Printf("Updated %s: %s\n", shortID(item.ID), item.Title)
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "new item title")
	cmd.Flags().StringVar(&notes, "notes", "", "new markdown notes")
	cmd.Flags().StringVar(&repo, "repo", "", "repo this item is work on, by ~/dev/repos.json name (empty string unlinks)")
	return cmd
}

func (c *commands) searchCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search items by title or notes",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			items, err := c.backend().Search(args[0])
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

func (c *commands) deleteCmd() *cobra.Command {
	var confirmed bool
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete an item permanently",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if !confirmed {
				return fmt.Errorf("refusing to delete without --yes")
			}
			item, err := resolveItem(c.backend(), args[0])
			if err != nil {
				return err
			}
			if err := c.backend().DeleteItem(item.ID); err != nil {
				return err
			}
			fmt.Printf("Deleted %s: %s\n", shortID(item.ID), item.Title)
			return nil
		},
	}
	cmd.Flags().BoolVar(&confirmed, "yes", false, "confirm the deletion")
	return cmd
}

func (c *commands) projectsCmd() *cobra.Command {
	var addProject string
	var removeProject string
	cmd := &cobra.Command{
		Use:   "projects <id>",
		Short: "View or manage an item's project memberships",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			b := c.backend()
			id, err := resolveItemID(b, args[0])
			if err != nil {
				return err
			}
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
	cmd.AddCommand(c.projectsListCmd(), c.projectsCreateCmd())
	return cmd
}

func (c *commands) projectsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all projects",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			projects, err := c.backend().ListProjects()
			if err != nil {
				return err
			}
			for _, p := range projects {
				fmt.Printf("%s  %-40s %d\n", shortID(p.ID), p.Name, p.ItemCount)
			}
			return nil
		},
	}
}

func (c *commands) projectsCreateCmd() *cobra.Command {
	var description string
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			input := model.CreateProject{Name: args[0]}
			if cmd.Flags().Changed("description") {
				input.Description = &description
			}
			project, err := c.backend().CreateProject(input)
			if err != nil {
				return err
			}
			fmt.Printf("Created project %s: %s\n", shortID(project.ID), project.Name)
			return nil
		},
	}
	cmd.Flags().StringVarP(&description, "description", "d", "", "long-form notes or decisions for the project")
	return cmd
}

// --- helpers ---

// UUIDv7 front-loads the 48-bit millisecond timestamp, so any prefix is identical
// for everything created in the same ~65s window. The entropy is in the tail.
func shortID(id string) string {
	if len(id) >= 8 {
		return id[len(id)-8:]
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

func listAll(b backend.Backend, asJSON bool) error {
	projects, err := b.ListProjects()
	if err != nil {
		return err
	}
	if asJSON {
		items, err := b.ListAllItems()
		if err != nil {
			return err
		}
		return printJSON(items)
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

func listByProject(b backend.Backend, name string, asJSON bool) error {
	projectID, err := findProjectByName(b, name)
	if err != nil {
		return err
	}
	items, err := b.ListItemsByProject(projectID)
	if err != nil {
		return err
	}
	if asJSON {
		return printJSON(items)
	}
	for _, item := range items {
		printItem(item.ProjectItem)
	}
	return nil
}

func listArchived(b backend.Backend, name string, asJSON bool) error {
	projectID, err := findProjectByName(b, name)
	if err != nil {
		return err
	}
	items, err := b.ListArchived(projectID)
	if err != nil {
		return err
	}
	if asJSON {
		return printJSON(items)
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
