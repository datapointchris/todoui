package repos

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = `{"repos":[{"name":"dotfiles"},{"name":"todoui"},{"name":"doit"},{"name":"doit-content"}]}`

func writeRegistry(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "repos.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing registry: %v", err)
	}
	return path
}

func TestKnownNamePasses(t *testing.T) {
	registry, err := Load(writeRegistry(t, sample))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := registry.Validate("todoui"); err != nil {
		t.Errorf("registered name must validate, got %v", err)
	}
}

// `--repo ""` unlinks an item, so an empty name has to pass rather than read as
// an unknown repo.
func TestEmptyNameIsNotRepoWork(t *testing.T) {
	registry, _ := Load(writeRegistry(t, sample))
	if err := registry.Validate(""); err != nil {
		t.Errorf("empty repo must pass, got %v", err)
	}
}

func TestUnknownNameSuggestsNearMatches(t *testing.T) {
	registry, _ := Load(writeRegistry(t, sample))
	err := registry.Validate("doit-conten")
	if err == nil {
		t.Fatal("a typo'd repo must be rejected")
	}
	if !strings.Contains(err.Error(), "doit-content") {
		t.Errorf("expected a suggestion naming doit-content, got %q", err)
	}
}

func TestUnknownNameWithNoNearMatchPointsAtTheRegistry(t *testing.T) {
	registry, _ := Load(writeRegistry(t, sample))
	err := registry.Validate("zzzz")
	if err == nil {
		t.Fatal("an unregistered repo must be rejected")
	}
	if !strings.Contains(err.Error(), "repos.json") {
		t.Errorf("expected the message to name the registry, got %q", err)
	}
}

// The work machine may not carry ~/dev/, and filing work there must not fail.
func TestMissingRegistryAcceptsAnything(t *testing.T) {
	registry, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("a missing registry must not be an error, got %v", err)
	}
	if registry.Available() {
		t.Error("a missing registry must report itself unavailable")
	}
	if err := registry.Validate("whatever"); err != nil {
		t.Errorf("validation must be skipped without a registry, got %v", err)
	}
}

func TestMalformedRegistryIsAnError(t *testing.T) {
	if _, err := Load(writeRegistry(t, "{not json")); err == nil {
		t.Error("a corrupt registry must surface rather than silently disabling validation")
	}
}

// The tool asks for the registry inside its own XDG data directory and knows
// nothing about where the file is really maintained — a symlink points it at a
// shared one, so no repo carries a fleet-specific path.
func TestDefaultPathIsTheToolsOwnDataDirectory(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg")
	if got, want := DefaultPath(), "/tmp/xdg/todoui/repos.json"; got != want {
		t.Errorf("DefaultPath() = %q, want %q", got, want)
	}
}

func TestUnknownNameNamesTheRegistryItRead(t *testing.T) {
	path := writeRegistry(t, sample)
	registry, _ := Load(path)
	err := registry.Validate("zzzz")
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("the message must name the registry actually read, got %v", err)
	}
}
