package cli

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/datapointchris/goclikit"
	"github.com/spf13/cobra"

	"github.com/datapointchris/todoui/model"
)

func TestNotFoundCarriesTheWrappedSubject(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("project %q: %w", "inbox", model.ErrNotFound)

	subject, ok := NotFound(err)
	if !ok {
		t.Fatal("NotFound() = false, want true for a wrapped sentinel")
	}
	if subject != `project "inbox": not found` {
		t.Errorf("subject = %q, want the noun and the identifier", subject)
	}
}

func TestNotFoundDeclinesABareSentinel(t *testing.T) {
	t.Parallel()

	// The unwrapped sentinel reads "not found", which names nothing. Handing
	// that to goclikit would render a recovery screen whose first line is two
	// words that do not say what was missing.
	if _, ok := NotFound(model.ErrNotFound); ok {
		t.Error("NotFound() = true for the bare sentinel")
	}
}

func TestNotFoundDeclinesOtherFailures(t *testing.T) {
	t.Parallel()

	for _, err := range []error{
		model.ErrDuplicateName,
		model.ErrCyclicDependency,
		errors.New("database is locked"),
	} {
		if _, ok := NotFound(err); ok {
			t.Errorf("NotFound(%v) = true, want false", err)
		}
	}
}

func TestAMembershipFailureNamesBothIds(t *testing.T) {
	t.Parallel()

	// Both ids can exist while the pairing does not, so naming one would send
	// the reader after something that is there.
	err := fmt.Errorf("item %q is not in project %q: %w", "abc", "inbox", model.ErrNotFound)

	subject, ok := NotFound(err)
	if !ok {
		t.Fatal("NotFound() = false for a membership failure")
	}
	if subject != `item "abc" is not in project "inbox": not found` {
		t.Errorf("subject = %q, want both ids", subject)
	}
}

func TestTheRootCarriesAWayInForEveryNoun(t *testing.T) {
	t.Parallel()

	root := &cobra.Command{Use: "todoui"}
	RegisterRecoveryHints(root)

	joined := root.Annotations[goclikit.RecoveryHintsAnnotation]
	if joined == "" {
		t.Fatal("the root carries no recovery hints")
	}
	// The tree is flat, so the root set is the only set. A noun missing from it
	// is a noun whose not-found names no way back.
	for _, noun := range []string{"todoui list", "todoui projects list", "todoui tasks"} {
		if !strings.Contains(joined, noun) {
			t.Errorf("the hints name no way to %q", noun)
		}
	}
}
