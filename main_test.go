package main

import (
	"errors"
	"io"
	"testing"

	"github.com/datapointchris/goclikit"
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

// A positional on a leaf is a typing mistake, so it exits 2 like a bad flag.
// cobra validates arguments before PersistentPreRunE, so `sync` refuses the
// word before it opens a database.
func TestAPositionalOnALeafIsAUsageError(t *testing.T) {
	for _, line := range [][]string{{"sync", "bogus"}, {"version", "bogus"}} {
		root := testRoot(t)
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		root.SetArgs(line)

		err := root.Execute()
		if !errors.Is(err, goclikit.ErrUsage) {
			t.Errorf("%v answered %v, want a usage error", line, err)
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
