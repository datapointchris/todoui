package cli

import (
	"errors"
	"fmt"

	"github.com/datapointchris/goclikit"
	"github.com/spf13/cobra"
)

// NoArgs refuses a positional on a command that takes none. Cobra's own NoArgs
// calls the word an unknown command, which names a subcommand a leaf never had.
// The refusal is marked as a usage error, so goclikit.Execute keeps its wording
// and the caller exits 2.
func NoArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	refusal := fmt.Sprintf("%s takes no arguments, and was given %q\n\nRun '%s --help' for its flags.",
		cmd.CommandPath(), args[0], cmd.CommandPath())
	return goclikit.UsageError(errors.New(refusal))
}
