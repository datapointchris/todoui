package cli

import (
	"errors"
	"fmt"

	"github.com/datapointchris/goclikit"
	"github.com/spf13/cobra"
)

// NoArgs refuses a positional on a command that takes none, marked as a usage
// error so it exits 2. Cobra's own NoArgs returns an unmarked error, which
// exits 1 as if the command had run and failed, and calls the word an unknown
// command, which names a subcommand a leaf never had.
func NoArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	refusal := fmt.Sprintf("%s takes no arguments, and was given %q\n\nRun '%s --help' for its flags.",
		cmd.CommandPath(), args[0], cmd.CommandPath())
	return goclikit.UsageError(errors.New(refusal))
}
