package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/datapointchris/todoui/backend"
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
			fmt.Printf("%s now depends on %s\n", shortID(itemID), shortID(dependsOnID))
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
			fmt.Printf("%s no longer depends on %s\n", shortID(itemID), shortID(dependsOnID))
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
