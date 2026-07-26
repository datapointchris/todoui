package main

import (
	"testing"

	"github.com/spf13/cobra"
)

// cobra builds `completion` and `help` during Execute, not construction.
func testRoot(t *testing.T) *cobra.Command {
	t.Helper()
	root := rootCmd()
	root.InitDefaultCompletionCmd()
	root.InitDefaultHelpCmd()
	return root
}

func find(t *testing.T, root *cobra.Command, path ...string) *cobra.Command {
	t.Helper()
	cmd, _, err := root.Find(path)
	if err != nil {
		t.Fatalf("finding %v: %v", path, err)
	}
	if cmd.Name() != path[len(path)-1] {
		t.Fatalf("finding %v resolved to %q", path, cmd.Name())
	}
	return cmd
}

// The shells source `todoui completion zsh` from their rc files, so a pull here
// is paid by every shell that opens. Registering "completion" was not enough:
// cobra gives that command a subcommand per shell and executes the leaf, so a
// leaf-only check missed it and a stale pull blocked startup for the API timeout.
func TestSkipsPullMatchesCompletionSubcommands(t *testing.T) {
	root := testRoot(t)

	for _, path := range [][]string{
		{"completion", "zsh"},
		{"completion", "bash"},
		{"completion", "fish"},
		{"help"},
		{"version"},
		{"update"},
		{"sync"},
		{"ui"},
	} {
		if !skipsPull(find(t, root, path...)) {
			t.Errorf("%v should skip the pre-command pull, got a pull", path)
		}
	}
}

// cobra adds the completion-protocol commands from an unexported initialiser, so
// mirror how it parents them. These run on every TAB press; pulling there hung
// the keypress for as long as the API took to answer.
func TestSkipsPullMatchesCompletionProtocolCommands(t *testing.T) {
	root := testRoot(t)

	for _, name := range []string{cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd} {
		cmd := &cobra.Command{Use: name}
		root.AddCommand(cmd)
		if !skipsPull(cmd) {
			t.Errorf("%s should skip the pre-command pull, got a pull", name)
		}
	}
}

// The denylist only works if everything absent from it still pulls; a command
// silently serving stale data is the bug refreshForCLI exists to prevent.
func TestSkipsPullLeavesDataCommandsAlone(t *testing.T) {
	root := testRoot(t)

	for _, path := range [][]string{
		{"list"},
		{"add"},
		{"search"},
		{"view"},
		{"blocked"},
		{"projects"},
	} {
		if skipsPull(find(t, root, path...)) {
			t.Errorf("%v should pull before reading, got a skip", path)
		}
	}
}
