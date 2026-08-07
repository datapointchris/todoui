package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/datapointchris/todoui/backend"
	"github.com/datapointchris/todoui/model"
)

// An item is named by its number, which is what every command prints. The two
// older forms still resolve, because ids written into a note or held by an
// agent from before the number existed have to keep working: a full UUID, and a
// unique suffix of one. Matching is on the suffix rather than a prefix because
// UUIDv7 front-loads its millisecond timestamp — a prefix is identical for
// everything created in the same ~65s window and would collide constantly.

// resolveItemID turns an item number, a full UUID, or a unique id suffix into a
// full item id. Archived items are included: an archived item that cannot be
// named is an item that cannot be unarchived.
func resolveItemID(b backend.Backend, ref string) (string, error) {
	items, err := b.ListAllItemsIncludingArchived()
	if err != nil {
		return "", fmt.Errorf("listing items to resolve %q: %w", ref, err)
	}

	// A number names exactly one item, so it wins over a suffix that happens to
	// be all digits. Falling through rather than erroring keeps an all-digit
	// UUID tail resolvable for an item that has no number yet.
	if number, err := strconv.Atoi(ref); err == nil {
		for _, item := range items {
			if item.Number != nil && *item.Number == number {
				return item.ID, nil
			}
		}
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
