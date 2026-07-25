package cli

import (
	"fmt"
	"strings"

	"github.com/datapointchris/todoui/backend"
	"github.com/datapointchris/todoui/model"
)

// Every command prints ids through shortID, so every command has to accept one
// back. Matching is on the suffix rather than a prefix because UUIDv7
// front-loads its millisecond timestamp — a prefix is identical for everything
// created in the same ~65s window and would collide constantly.
//
// A full UUID still works, which is what makes output from one command safe to
// paste into the next regardless of which form produced it.

// resolveItemID turns a full UUID or a unique id suffix into a full item id.
// Archived items are included: an archived item that cannot be named is an
// item that cannot be unarchived.
func resolveItemID(b backend.Backend, ref string) (string, error) {
	items, err := b.ListAllItemsIncludingArchived()
	if err != nil {
		return "", fmt.Errorf("listing items to resolve %q: %w", ref, err)
	}
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	return resolveID(ref, ids, "item")
}

// resolveTaskID resolves a task reference within one item. Task ids are scoped
// to their item, so an ambiguous suffix here means two tasks on the same item.
func resolveTaskID(b backend.Backend, itemID, ref string) (string, error) {
	tasks, err := b.ListTasks(itemID)
	if err != nil {
		return "", fmt.Errorf("listing tasks to resolve %q: %w", ref, err)
	}
	ids := make([]string, len(tasks))
	for i, task := range tasks {
		ids[i] = task.ID
	}
	return resolveID(ref, ids, "task")
}

func resolveID(ref string, candidates []string, kind string) (string, error) {
	if ref == "" {
		return "", fmt.Errorf("no %s id given", kind)
	}

	needle := strings.ToLower(ref)
	var matches []string
	for _, id := range candidates {
		lowered := strings.ToLower(id)
		if lowered == needle {
			return id, nil
		}
		if strings.HasSuffix(lowered, needle) {
			matches = append(matches, id)
		}
	}

	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("no %s matching %q", kind, ref)
	default:
		return "", fmt.Errorf("%q matches %d %ss: %s — use more characters",
			ref, len(matches), kind, strings.Join(shortIDs(matches), ", "))
	}
}

func shortIDs(ids []string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = shortID(id)
	}
	return out
}

// resolveItem resolves a reference and fetches the item in one step, which is
// what most commands actually want.
func resolveItem(b backend.Backend, ref string) (*model.ProjectItemDetail, error) {
	id, err := resolveItemID(b, ref)
	if err != nil {
		return nil, err
	}
	return b.GetItem(id)
}
