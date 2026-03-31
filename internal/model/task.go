package model

import "time"

// ProjectItemTask is a sub-task within a project item.
type ProjectItemTask struct {
	ID        string    `json:"id"`
	ItemID    string    `json:"item_id"`
	Title     string    `json:"title"`
	Completed bool      `json:"completed"`
	Position  int       `json:"position"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateProjectItemTask is the input for creating a new task on a project item.
type CreateProjectItemTask struct {
	Title string `json:"title"`
}

// UpdateProjectItemTask is the input for updating an existing task.
// All fields are optional — only non-nil fields are applied.
type UpdateProjectItemTask struct {
	Title     *string `json:"title,omitempty"`
	Completed *bool   `json:"completed,omitempty"`
	Position  *int    `json:"position,omitempty"`
}
