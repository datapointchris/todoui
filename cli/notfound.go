package cli

import (
	"errors"

	"github.com/datapointchris/goclikit"
	"github.com/spf13/cobra"

	"github.com/datapointchris/todoui/model"
)

// NotFound is the classifier [goclikit.WithNotFound] calls: the store's
// not-found, and the line naming what was missing.
//
// model.ErrNotFound is a bare sentinel reading "not found", so the subject is
// whatever the backend wrapped it with. Every return site in backend/local.go
// names its noun and the identifier that was looked up, which is why this can
// return the error's own text rather than composing one from argv.
//
// It has to come from there rather than from here for the usual reason: a verb
// can touch two things. `todoui complete-task <item-id> <task-id>` looks up the
// task, and a subject built from the command line would have to guess which id
// the store refused.
func NotFound(err error) (string, bool) {
	if !errors.Is(err, model.ErrNotFound) {
		return "", false
	}
	// An unwrapped sentinel is the literal words "not found", which names
	// nothing. goclikit reads an empty subject as false, so returning the bare
	// text would be a subject in name only -- guard it explicitly instead.
	subject := err.Error()
	if subject == model.ErrNotFound.Error() {
		return "", false
	}
	return subject, true
}

// recoveryHints are the commands a not-found names, recorded on the root.
//
// All three nouns are listed together because the command tree is flat: every
// verb is a direct child of the root, so there is no per-noun subtree to hang a
// narrower set on. That is workable here only because the wrapped subject says
// which noun failed -- `project "inbox": not found` sends the reader to the
// project line without having to work out which applies.
//
// Rejected: listing the projects themselves. help.md prefers printing the valid
// values over naming a list command, but that holds where the corpus is small
// and fixed. Items run to the hundreds and ids are generated, so the list would
// be longer than the screen and still not contain what was typed.
var recoveryHints = []string{
	"List every item: todoui list",
	"List every project: todoui projects list",
	"List an item's tasks: todoui tasks <item-id>",
}

// RegisterRecoveryHints records the hints on the root command. Exported because
// the root is assembled in main, not here.
func RegisterRecoveryHints(root *cobra.Command) {
	goclikit.WithRecoveryHints(root, recoveryHints...)
}
