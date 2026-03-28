package model

import "time"

// ProjectItem is the base representation of an item in the system.
type ProjectItem struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Notes     *string   `json:"notes,omitempty"`
	Completed bool      `json:"completed"`
	Archived  bool      `json:"archived"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ProjectItemDetail is a ProjectItem with its project memberships and dependency IDs.
// Returned by GET /project-items/{id}/ and POST /project-items/.
type ProjectItemDetail struct {
	ProjectItem
	Projects      []Project `json:"projects"`
	DependencyIDs []int64   `json:"dependency_ids"`
}

// ProjectItemInProject is a ProjectItem as seen within a specific project context,
// including its position within that project.
// Returned by GET /projects/{id}/items/.
type ProjectItemInProject struct {
	ProjectItem
	Position int `json:"position"`
}

// CreateProjectItem is the input for creating a new project item.
type CreateProjectItem struct {
	Title      string  `json:"title"`
	Notes      *string `json:"notes,omitempty"`
	ProjectIDs []int64 `json:"project_ids"`
}

// UpdateProjectItem is the input for updating an existing project item.
// All fields are optional — only non-nil fields are applied.
type UpdateProjectItem struct {
	Title     *string `json:"title,omitempty"`
	Notes     *string `json:"notes,omitempty"`
	Completed *bool   `json:"completed,omitempty"`
	Archived  *bool   `json:"archived,omitempty"`
}
