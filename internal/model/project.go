package model

import "time"

// Project represents a project that items are organized into.
type Project struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Position  int       `json:"position"`
	CreatedAt time.Time `json:"created_at"`
}

// ProjectWithItemCount is a Project with the count of active (non-archived) items.
// Returned by GET /projects/ and GET /projects/{id}/.
type ProjectWithItemCount struct {
	Project
	ItemCount int `json:"item_count"`
}

// UpdateProject is the input for updating a project.
// All fields are optional — only non-nil fields are applied.
type UpdateProject struct {
	Name     *string `json:"name,omitempty"`
	Position *int    `json:"position,omitempty"`
}
