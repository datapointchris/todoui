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

// The shells source `todoui completion zsh` from their rc files, so opening the
// database here is paid by every shell that starts, and on a new machine it
// creates one. cobra gives `completion` a subcommand per shell and executes the
// leaf, so a check on the leaf's name alone misses it.
func TestNeedsBackendSkipsCommandsWithNothingToRead(t *testing.T) {
	root := testRoot(t)

	for _, path := range [][]string{
		{"completion", "zsh"},
		{"completion", "bash"},
		{"completion", "fish"},
		{"help"},
		{"version"},
		{"update"},
	} {
		if needsBackend(find(t, root, path...)) {
			t.Errorf("%v opened the database, want it skipped", path)
		}
	}
}

// cobra adds the completion-protocol commands from an unexported initialiser, so
// mirror how it parents them. These run on every TAB press.
func TestNeedsBackendSkipsCompletionProtocolCommands(t *testing.T) {
	root := testRoot(t)

	for _, name := range []string{cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd} {
		cmd := &cobra.Command{Use: name}
		root.AddCommand(cmd)
		if needsBackend(cmd) {
			t.Errorf("%s opened the database, want it skipped", name)
		}
	}
}

func TestNeedsBackendOpensTheDatabaseForTheTUIAndSync(t *testing.T) {
	root := testRoot(t)

	if !needsBackend(root) {
		t.Error("bare todoui skipped the database, want the TUI to have one")
	}
	if !needsBackend(find(t, root, "sync")) {
		t.Error("sync skipped the database, want it opened")
	}
}
