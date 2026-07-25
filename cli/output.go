package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/datapointchris/todoui/model"
)

// printJSON writes v to stdout for the machine-readable path. Ids are emitted
// in full — a consumer parsing JSON has no reason to work with the truncated
// form, and full ids round-trip into any command without resolution.
func printJSON(v any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(v)
}

// addJSONFlag registers the --json flag shared by every read command.
func addJSONFlag(cmd *cobra.Command, target *bool) {
	cmd.Flags().BoolVar(target, "json", false, "output as JSON to stdout")
}

func printItem(item model.ProjectItem) {
	marker := "○"
	if item.Completed {
		marker = "✓"
	}
	fmt.Printf("  %s %-8s %s\n", marker, shortID(item.ID), item.Title)
}

func printTask(task model.ProjectItemTask) {
	marker := "○"
	if task.Completed {
		marker = "✓"
	}
	fmt.Printf("  %s %-8s %s\n", marker, shortID(task.ID), task.Title)
}

func printItemDetail(item *model.ProjectItemDetail, tasks []model.ProjectItemTask, blockers []model.ProjectItem) {
	fmt.Printf("%s %s\n", shortID(item.ID), item.Title)
	if item.Repo != nil && *item.Repo != "" {
		fmt.Printf("  repo:     %s\n", *item.Repo)
	}
	if item.Completed {
		fmt.Println("  status:   done")
	}
	if item.Archived {
		fmt.Println("  archived: yes")
	}
	if len(item.Projects) > 0 {
		names := make([]string, 0, len(item.Projects))
		for _, p := range item.Projects {
			names = append(names, p.Name)
		}
		fmt.Printf("  projects: %s\n", strings.Join(names, ", "))
	}
	if item.Notes != nil && *item.Notes != "" {
		fmt.Printf("\n%s\n", *item.Notes)
	}
	if len(tasks) > 0 {
		fmt.Println("\nTasks:")
		for _, t := range tasks {
			printTask(t)
		}
	}
	if len(blockers) > 0 {
		fmt.Println("\nBlocked by:")
		for _, b := range blockers {
			fmt.Printf("  %s %s\n", shortID(b.ID), b.Title)
		}
	}
}

// itemDetailJSON is the machine-readable shape of `view`, flattening the three
// separate reads the human view interleaves.
type itemDetailJSON struct {
	*model.ProjectItemDetail
	Tasks    []model.ProjectItemTask `json:"tasks"`
	Blockers []model.ProjectItem     `json:"blockers"`
}
