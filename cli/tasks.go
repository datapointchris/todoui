package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/datapointchris/todoui/v2/backend"
	"github.com/datapointchris/todoui/v2/model"
)

func (c *commands) tasksCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "tasks <item-id>",
		Short:   "List an item's sub-tasks",
		Args:    cobra.ExactArgs(1),
		Example: "  todoui tasks 4bdd1671\n  todoui tasks 4bdd1671 --json",
		RunE: func(_ *cobra.Command, args []string) error {
			b := c.backend()
			itemID, err := resolveItemID(b, args[0])
			if err != nil {
				return err
			}
			tasks, err := b.ListTasks(itemID)
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(tasks)
			}
			for _, task := range tasks {
				printTask(task)
			}
			return nil
		},
	}
	addJSONFlag(cmd, &asJSON)
	return cmd
}

func (c *commands) addTaskCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "add-task <item-id> <title>",
		Short:   "Add a sub-task to an item",
		Args:    cobra.ExactArgs(2),
		Example: `  todoui add-task 4bdd1671 "wire up the endpoint"`,
		RunE: func(_ *cobra.Command, args []string) error {
			b := c.backend()
			itemID, err := resolveItemID(b, args[0])
			if err != nil {
				return err
			}
			task, err := b.CreateTask(itemID, model.CreateProjectItemTask{Title: args[1]})
			if err != nil {
				return err
			}
			fmt.Printf("Added task %s: %s\n", shortID(task.ID), task.Title)
			return nil
		},
	}
}

func (c *commands) completeTaskCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "complete-task <item-id> <task-id>",
		Short: "Mark a sub-task complete",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			b := c.backend()
			itemID, taskID, err := resolveItemAndTask(b, args[0], args[1])
			if err != nil {
				return err
			}
			if err := b.CompleteTask(itemID, taskID); err != nil {
				return err
			}
			fmt.Printf("Completed task %s\n", shortID(taskID))
			return nil
		},
	}
}

func (c *commands) editTaskCmd() *cobra.Command {
	var (
		title     string
		completed bool
		position  int
	)
	cmd := &cobra.Command{
		Use:   "edit-task <item-id> <task-id> [flags]",
		Short: "Change a sub-task's title, completion, or position",
		Long:  "Update only the fields whose flags you pass.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			b := c.backend()
			itemID, taskID, err := resolveItemAndTask(b, args[0], args[1])
			if err != nil {
				return err
			}

			f := cmd.Flags()
			input := model.UpdateProjectItemTask{}
			if f.Changed("title") {
				input.Title = &title
			}
			if f.Changed("completed") {
				input.Completed = &completed
			}
			if f.Changed("position") {
				input.Position = &position
			}
			if input == (model.UpdateProjectItemTask{}) {
				return fmt.Errorf("nothing to change — pass --title, --completed, and/or --position")
			}

			task, err := b.UpdateTask(itemID, taskID, input)
			if err != nil {
				return err
			}
			fmt.Printf("Updated task %s: %s\n", shortID(task.ID), task.Title)
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "new task title")
	cmd.Flags().BoolVar(&completed, "completed", false, "completion state")
	cmd.Flags().IntVar(&position, "position", 0, "new position within the item")
	return cmd
}

func (c *commands) removeTaskCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove-task <item-id> <task-id>",
		Short: "Delete a sub-task from an item",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			b := c.backend()
			itemID, taskID, err := resolveItemAndTask(b, args[0], args[1])
			if err != nil {
				return err
			}
			if err := b.DeleteTask(itemID, taskID); err != nil {
				return err
			}
			fmt.Printf("Removed task %s\n", shortID(taskID))
			return nil
		},
	}
}

// resolveItemAndTask resolves both references, scoping the task lookup to the
// resolved item so a task id suffix only has to be unique within that item.
func resolveItemAndTask(b backend.Backend, itemRef, taskRef string) (string, string, error) {
	itemID, err := resolveItemID(b, itemRef)
	if err != nil {
		return "", "", err
	}
	taskID, err := resolveTaskID(b, itemID, taskRef)
	if err != nil {
		return "", "", err
	}
	return itemID, taskID, nil
}
