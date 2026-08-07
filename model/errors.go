package model

import "errors"

var (
	ErrNotFound         = errors.New("not found")
	ErrDuplicateName    = errors.New("duplicate name")
	ErrCyclicDependency = errors.New("would create a cyclic dependency")
	ErrLastProject      = errors.New("item must belong to at least one project")
	ErrNothingToUndo    = errors.New("nothing to undo")
	// A dropped project with no reason is indistinguishable from a deferred one,
	// and deferred invites the same idea back next month.
	ErrDropReasonRequired = errors.New("dropping a project requires a reason")
)
