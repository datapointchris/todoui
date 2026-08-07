package model

import "time"

// ProjectItem is the base representation of an item in the system.
type ProjectItem struct {
	ID string `json:"id"`
	// Number is the handle — short, server-assigned, and the only one of the two
	// identifiers meant to be read or typed. Nil for an item created while sync
	// was unreachable, which has no number until its create is pushed. Both
	// forms resolve on the command line; see cli/resolve.go.
	Number    *int      `json:"number,omitempty"`
	Title     string    `json:"title"`
	Notes     *string   `json:"notes,omitempty"`
	Repo      *string   `json:"repo,omitempty"`
	Completed bool      `json:"completed"`
	Archived  bool      `json:"archived"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ProjectItemDetail is a ProjectItem with its project memberships, dependency
// IDs, and sub-tasks. Returned by GET /project-items/ (the list), GET
// /project-items/{id}/, and POST /project-items/ — the list carries the same
// shape so a full reconcile is two requests rather than two per item.
//
// DependencyIDs and Tasks decode to nil when the server omits the field and to
// an empty slice when it sends one, which is the only way a pull can tell a
// server predating the embed from an item that genuinely has neither. Keep them
// as plain slices and keep the API sending `[]`; anything that collapses nil and
// empty makes an old server look like an instruction to delete every task.
type ProjectItemDetail struct {
	ProjectItem
	Projects      []Project         `json:"projects"`
	DependencyIDs []string          `json:"dependency_ids"`
	Tasks         []ProjectItemTask `json:"tasks"`
}

// ProjectItemInProject is a ProjectItem as seen within a specific project context,
// including its position within that project.
// Returned by GET /projects/{id}/items/.
type ProjectItemInProject struct {
	ProjectItem
	Position     int `json:"position"`
	ProjectCount int `json:"project_count,omitempty"`
}

// CreateProjectItem is the input for creating a new project item.
type CreateProjectItem struct {
	Title      string   `json:"title"`
	Notes      *string  `json:"notes,omitempty"`
	Repo       *string  `json:"repo,omitempty"`
	ProjectIDs []string `json:"project_ids"`
}

// UpdateProjectItem is the input for updating an existing project item.
// All fields are optional — only non-nil fields are applied.
type UpdateProjectItem struct {
	Title     *string `json:"title,omitempty"`
	Notes     *string `json:"notes,omitempty"`
	Repo      *string `json:"repo,omitempty"`
	Completed *bool   `json:"completed,omitempty"`
	Archived  *bool   `json:"archived,omitempty"`
}
