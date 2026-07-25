package model

// Action represents a recorded action that can be undone.
type Action struct {
	ID            int64  `json:"id"`
	ActionType    string `json:"action"`
	EntityType    string `json:"entity_type"`
	EntityID      int64  `json:"entity_id"`
	PreviousState string `json:"previous_state"`
}

// UndoResult describes what an undo actually did. A sync layer needs more than
// a display string: the pending queue still holds operations describing the
// state the undo just reversed, and reconciling them requires knowing which
// entity changed and what it looks like now.
type UndoResult struct {
	Description string
	Action      string
	EntityType  string
	EntityID    string

	// Restored is the local row after the undo, or nil when the undo removed it.
	// Exactly one of Restored / RestoredProject can be set, matching EntityType.
	Restored        *ProjectItem
	RestoredProject *Project

	// Rows that cascaded off a delete and came back with it. Sync has to push
	// these, not just the row: a pull replaces memberships and dependencies
	// wholesale from server state and drops tasks the server does not know
	// about, so anything restored only locally is erased on the next pull.
	RestoredProjectIDs   []string // projects an item was restored into
	RestoredItemIDs      []string // items a restored project held
	RestoredTasks        []ProjectItemTask
	RestoredDependencies []ItemDependency
}

// ItemDependency is one edge of the blocking graph: ItemID is blocked until
// DependsOnID is complete.
type ItemDependency struct {
	ItemID      string `json:"item_id"`
	DependsOnID string `json:"depends_on_id"`
}
