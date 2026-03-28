package model

import "errors"

var (
	ErrNotFound         = errors.New("not found")
	ErrDuplicateName    = errors.New("duplicate name")
	ErrCyclicDependency = errors.New("would create a cyclic dependency")
	ErrLastProject      = errors.New("todo must belong to at least one project")
	ErrNothingToUndo    = errors.New("nothing to undo")
)
