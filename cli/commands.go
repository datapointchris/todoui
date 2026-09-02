package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/datapointchris/goclikit"
	"github.com/spf13/cobra"

	"github.com/datapointchris/todoui/backend"
	"github.com/datapointchris/todoui/model"
	"github.com/datapointchris/todoui/repos"
)

// commands takes a pointer to the Backend interface so that commands can be
// registered before PersistentPreRunE initializes the backend. Each RunE
// dereferences the pointer at execution time, when it's guaranteed to be set.
type commands struct {
	b *backend.Backend
	// flushSync pushes what is queued and waits for the server, so a create can
	// print the number it was just given. Nil when sync is off, where the
	// number was assigned locally and there is nothing to wait for.
	flushSync func()
}

func (c *commands) backend() backend.Backend { return *c.b }

// flush blocks until queued writes have reached the server, or returns straight
// away when there is no server to reach.
func (c *commands) flush() {
	if c.flushSync != nil {
		c.flushSync()
	}
}

// RegisterAll adds all CLI subcommands to the given parent command. flushSync
// may be nil.
func RegisterAll(parent *cobra.Command, b *backend.Backend, flushSync func()) {
	c := &commands{b: b, flushSync: flushSync}
	parent.AddCommand(c.createCmd())
	parent.AddCommand(c.completeCmd())
	parent.AddCommand(c.reopenCmd())
	parent.AddCommand(c.listCmd())
	parent.AddCommand(c.showCmd())
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
	parent.AddCommand(c.treeCmd())
	parent.AddCommand(c.undoCmd())
	parent.AddCommand(c.projectsCmd())
	parent.AddCommand(updateCmd())
	parent.AddCommand(versionCmd())
}

func (c *commands) createCmd() *cobra.Command {
	var (
		projects []string
		repo     string
		notes    string
	)
	cmd := &cobra.Command{
		Use:   "create <title>",
		Short: "Create a new item",
		// `add` was the spelling here until 2026-08-12. It is the fleet's word
		// for attaching something to a parent that already exists, and this
		// brings an item into existence, so it was the clearest misuse of `add`
		// on the fleet. SuggestFor routes the muscle memory without making a
		// second spelling runnable.
		SuggestFor: []string{"add", "new"},
		Args:       cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(projects) == 0 {
				return goclikit.UsageError(fmt.Errorf("-p/--project is required"))
			}
			// Flags are checked before the lookups: validating what was typed is
			// free, and reporting the project miss first would hide a second
			// error the user has to come back for.
			if cmd.Flags().Changed("repo") {
				if err := validateRepo(repo); err != nil {
					return err
				}
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
			// The number is assigned by the server, so the row has to be read
			// back after the push to print the handle rather than the fallback.
			c.flush()
			if pushed, err := c.backend().GetItem(item.ID); err == nil {
				item = pushed
			}
			fmt.Printf("Created item %s: %s\n", itemHandle(item.ProjectItem), item.Title)
			for _, p := range item.Projects {
				fmt.Printf("  → %s\n", p.Name)
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVarP(&projects, "project", "p", nil, "project name (repeatable)")
	cmd.Flags().StringVarP(&repo, "repo", "r", "", "repo this item is work on, by registry name")
	cmd.Flags().StringVarP(&notes, "notes", "n", "", "markdown notes for the item")
	return cmd
}

func (c *commands) completeCmd() *cobra.Command {
	cmd := c.completionCmd("complete", "Mark an item completed", true, "Completed")
	// `done` until 2026-08-12. icb spells the same act on the same rows
	// `complete`, and `done` is the fleet's word for a recurring thing finished
	// for this cycle, which an item is not.
	cmd.SuggestFor = []string{"done", "finish"}
	return cmd
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
			fmt.Printf("%s: %s %s\n", verb, itemHandle(*item), item.Title)
			return nil
		},
	}
}

func (c *commands) listCmd() *cobra.Command {
	var (
		project  string
		repo     string
		archived bool
		asJSON   bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List active items",
		Long: "With no flags, every active item grouped by project. --repo narrows to one\n" +
			"repo's work across every project holding it; --repo \"\" gives the items that\n" +
			"are not repo work at all.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			b := c.backend()
			if archived {
				if project == "" {
					return fmt.Errorf("--archived needs -p/--project")
				}
				return listArchived(b, project, asJSON)
			}
			if cmd.Flags().Changed("repo") {
				if project != "" {
					return fmt.Errorf("--repo and --project are different questions — pass one")
				}
				return listByRepo(b, repoFilter(cmd, repo), asJSON)
			}
			if project != "" {
				return listByProject(b, project, asJSON)
			}
			return listAll(b, asJSON)
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "filter by project name")
	cmd.Flags().StringVar(&repo, "repo", "", "filter by repo name (empty string for untagged work)")
	cmd.Flags().BoolVar(&archived, "archived", false, "list a project's archived items instead")
	addJSONFlag(cmd, &asJSON)
	return cmd
}

// validateRepo rejects a --repo the registry does not know. Loaded per call
// rather than once at startup: the registry is a few dozen names read from disk,
// and every command that does not touch --repo should not pay for it.
func validateRepo(repo string) error {
	registry, err := repos.Load(repos.DefaultPath())
	if err != nil {
		return err
	}
	return registry.Validate(repo)
}

// repoFilter renders --repo in the backend's convention, where nil means the
// untagged items rather than "no filter". Callers gate on Changed() first, so an
// absent flag never reaches here and the only nil it produces is `--repo ""`.
func repoFilter(cmd *cobra.Command, repo string) *string {
	if !cmd.Flags().Changed("repo") || repo == "" {
		return nil
	}
	return &repo
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
			fmt.Printf("%s: %s %s\n", verb, itemHandle(*item), item.Title)
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
				return goclikit.UsageError(fmt.Errorf("--project is required"))
			}
			if !cmd.Flags().Changed("position") {
				return goclikit.UsageError(fmt.Errorf("--position is required"))
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
			fmt.Printf("Moved %s to position %d in %s\n", itemHandleByID(b, itemID), position, project)
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

func (c *commands) showCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show an item with its projects, tasks, and blockers",
		// `view` until 2026-08-12. `show` is the fleet's word for displaying one
		// record, and it is the only candidate that survives a singleton.
		SuggestFor: []string{"view", "info", "get"},
		Args:       cobra.ExactArgs(1),
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
				if err := validateRepo(repo); err != nil {
					return err
				}
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
			fmt.Printf("Updated %s: %s\n", itemHandle(*item), item.Title)
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "new item title")
	cmd.Flags().StringVar(&notes, "notes", "", "new markdown notes")
	cmd.Flags().StringVar(&repo, "repo", "", "repo this item is work on, by registry name (empty string unlinks)")
	return cmd
}

func (c *commands) searchCmd() *cobra.Command {
	var (
		asJSON bool
		repo   string
	)
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search items by title or notes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var items []model.ProjectItem
			var err error
			if cmd.Flags().Changed("repo") {
				items, err = c.backend().SearchByRepo(args[0], repoFilter(cmd, repo))
			} else {
				items, err = c.backend().Search(args[0])
			}
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
	cmd.Flags().StringVar(&repo, "repo", "", "only items tagged with this repo (empty string for untagged work)")
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
			fmt.Printf("Deleted %s: %s\n", itemHandle(item.ProjectItem), item.Title)
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
	cmd.AddCommand(
		c.projectsListCmd(),
		c.projectsCreateCmd(),
		c.projectsCompleteCmd(),
		c.projectsDropCmd(),
		c.projectsReopenCmd(),
	)
	return cmd
}

func (c *commands) projectsListCmd() *cobra.Command {
	var status string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the active projects",
		Long: "Completed and dropped projects are hidden until --status asks for them.\n" +
			"Closing a project is what takes it out of the list; that is the whole point.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			projects, err := c.backend().ListProjectsByStatus(status)
			if err != nil {
				return err
			}
			// The status column earns its width only when the rows can differ
			// in it, which the flag decides.
			showStatus := status != model.StatusActive
			for _, p := range projects {
				if showStatus {
					fmt.Printf("%s  %-40s %-8s %d\n", shortID(p.ID), p.Name, p.Status, p.ItemCount)
					continue
				}
				fmt.Printf("%s  %-40s %d\n", shortID(p.ID), p.Name, p.ItemCount)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&status, "status", model.StatusActive, "active, done, dropped, or all")
	return cmd
}

// projectsCompleteCmd and its siblings write through SetProjectStatus rather
// than three flavors of update, so the closing timestamp and the reason are
// derived from the transition in one place.
func (c *commands) projectsCompleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "complete <project>",
		Short: "Mark a project finished, which hides it",
		Long: "A project is a finite effort, so completing it is what takes it out of the\n" +
			"list — there is no separate archive step. Its items are left as they are: an\n" +
			"item still open when the project finished was still open.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return c.setProjectStatus(args[0], model.StatusCompleted, nil, "Completed")
		},
	}
}

func (c *commands) projectsDropCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "drop <project> --reason <why>",
		Short: "Close a project you are not going to do",
		Long: "--reason is required, and that is the point: \"deferred\" invites the same idea\n" +
			"back next month, whereas dropped-and-here-is-why closes the question.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if strings.TrimSpace(reason) == "" {
				return model.ErrDropReasonRequired
			}
			return c.setProjectStatus(args[0], model.StatusDropped, &reason, "Dropped")
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why it is dropped rather than deferred (required)")
	return cmd
}

func (c *commands) projectsReopenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reopen <project>",
		Short: "Return a closed project to the active list",
		Long: "Clears the closing reason and date along with the status. Refused if an active\n" +
			"project has taken the name in the meantime — only the live project holds a name.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return c.setProjectStatus(args[0], model.StatusActive, nil, "Reopened")
		},
	}
}

func (c *commands) setProjectStatus(ref, status string, reason *string, verb string) error {
	b := c.backend()
	id, err := resolveProjectRef(b, ref)
	if err != nil {
		return err
	}
	project, err := b.SetProjectStatus(id, status, reason)
	if err != nil {
		return err
	}
	fmt.Printf("%s project %s: %s\n", verb, shortID(project.ID), project.Name)
	return nil
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

// itemHandle names an item the way a person types it. The number is the handle;
// the UUID tail is what an item has to answer to in the window between being
// created offline and the push that earns it one. Both resolve, so a handle
// printed before the number arrives keeps working after it does.
func itemHandle(item model.ProjectItem) string {
	if item.Number != nil {
		return strconv.Itoa(*item.Number)
	}
	return shortID(item.ID)
}

// itemHandleByID is itemHandle for the write paths that thread an id rather
// than the row. The lookup is one indexed read on a command that has already
// resolved and mutated the item.
func itemHandleByID(b backend.Backend, id string) string {
	if item, err := b.GetItem(id); err == nil {
		return itemHandle(item.ProjectItem)
	}
	return shortID(id)
}

// Projects and tasks have no number of their own, so they still show the tail.
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

// resolveProjectRef names a project the way `projects list` prints it — its name
// or the 8-character id tail.
//
// A name is unique only among ACTIVE projects, so the active one wins outright:
// while a live effort holds the name, that is what the name means. Falling back
// to a lone closed match is what makes `projects reopen ifiles` addressable at
// all, since by then no active project holds it. Several closed projects sharing
// a name is genuinely ambiguous and says so rather than picking one.
func resolveProjectRef(b backend.Backend, ref string) (string, error) {
	all, err := b.ListProjectsByStatus(model.StatusAll)
	if err != nil {
		return "", fmt.Errorf("listing projects: %w", err)
	}

	var byName []model.ProjectWithItemCount
	for _, p := range all {
		if p.ID == ref || shortID(p.ID) == ref {
			return p.ID, nil
		}
		if strings.EqualFold(p.Name, ref) {
			byName = append(byName, p)
		}
	}

	for _, p := range byName {
		if p.Status == model.StatusActive {
			return p.ID, nil
		}
	}
	switch len(byName) {
	case 0:
		return "", fmt.Errorf("project %q not found", ref)
	case 1:
		return byName[0].ID, nil
	default:
		handles := make([]string, len(byName))
		for i, p := range byName {
			handles[i] = fmt.Sprintf("%s (%s)", shortID(p.ID), p.Status)
		}
		return "", fmt.Errorf("%q names %d closed projects and no active one; use an id: %s",
			ref, len(byName), strings.Join(handles, ", "))
	}
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

func listByRepo(b backend.Backend, repo *string, asJSON bool) error {
	items, err := b.ListItemsByRepo(repo)
	if err != nil {
		return err
	}
	if asJSON {
		return printJSON(items)
	}
	if len(items) == 0 {
		if repo == nil {
			fmt.Println("No untagged items.")
		} else {
			fmt.Printf("No items for repo %s.\n", *repo)
		}
		return nil
	}
	for _, item := range items {
		printItem(item)
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
	fmt.Printf("%s %s\n", itemHandle(item.ProjectItem), item.Title)
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
	fmt.Printf("Added %s to %s\n", itemHandleByID(b, itemID), projectName)
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
	fmt.Printf("Removed %s from %s\n", itemHandleByID(b, itemID), projectName)
	return nil
}
