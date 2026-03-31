package model

import "time"

// Project represents a project that items are organized into.
type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description *string   `json:"description,omitempty"`
	Position    int       `json:"position"`
	CreatedAt   time.Time `json:"created_at"`
}

// ProjectWithItemCount is a Project with the count of active (non-archived) items.
// Returned by GET /projects/ and GET /projects/{id}/.
type ProjectWithItemCount struct {
	Project
	ItemCount int `json:"item_count"`
}

// CreateProject is the input for creating a new project.
type CreateProject struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

// UpdateProject is the input for updating a project.
// All fields are optional — only non-nil fields are applied.
type UpdateProject struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Position    *int    `json:"position,omitempty"`
}
