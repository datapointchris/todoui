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
	Restored *ProjectItem
}
