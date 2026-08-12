package model

import "time"

// Project statuses. A project is a finite effort with a definition of done, so
// completing it is what hides it — there is no separate archived flag the way
// items have one. Dropped exists beside Done because Done alone would force you
// to lie about anything you merely stopped caring about, and it always carries a
// reason: "deferred" invites the same idea back, "dropped, and here is why"
// does not.
const (
	StatusActive = "active"
	// StatusCompleted and not "done": the API stores this value, an item stores a
	// `completed` boolean, and one concept spelled two ways across a resource and
	// its own children is what the rename on 2026-08-12 removed. todoui syncs
	// against that API, so the word has to be the same one or a push writes a
	// value its FK rejects.
	StatusCompleted = "completed"
	StatusDropped   = "dropped"

	// StatusAll asks for every project. Not a status a project can be in — it is
	// the absence of the filter, which is why the API keeps it out of its lookup
	// table too.
	StatusAll = "all"
)

// IsTerminalStatus reports whether a project in this status is closed and
// therefore hidden by default.
func IsTerminalStatus(status string) bool {
	return status == StatusCompleted || status == StatusDropped
}

// Project represents a project that items are organized into.
type Project struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	Status      string  `json:"status"`
	// Why a project was dropped rather than deferred. Nil for anything still
	// active, and cleared by the server on reopen.
	StatusReason *string `json:"status_reason,omitempty"`
	// When the project reached a terminal status. CreatedAt orders by when work
	// started, which is the wrong axis for a list of finished things.
	ClosedAt  *time.Time `json:"closed_at,omitempty"`
	Position  int        `json:"position"`
	CreatedAt time.Time  `json:"created_at"`
}

// ProjectWithItemCount is a Project with the count of active (non-archived) items.
// Returned by GET /projects/ and GET /projects/{id}/.
type ProjectWithItemCount struct {
	Project
	ItemCount int `json:"item_count"`
	// How many of those are done. A total alone cannot distinguish a project
	// with nothing left in it from one that has not been started. Derived from
	// the local tables on every read rather than carried in the sync payload,
	// so an API that does not send it costs nothing.
	CompletedCount int `json:"completed_count"`
}

// OpenCount is the number that decides whether a project still wants work.
func (p ProjectWithItemCount) OpenCount() int {
	return p.ItemCount - p.CompletedCount
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
