package undo

// Action represents a recorded action that can be undone.
type Action struct {
	ID            int64  `json:"id"`
	ActionType    string `json:"action"`
	EntityType    string `json:"entity_type"`
	EntityID      int64  `json:"entity_id"`
	PreviousState string `json:"previous_state"`
}
