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

// withConfig points $XDG_CONFIG_HOME at a directory holding body as todoui's
// config, or at an empty one when body is "". Every DefaultPath test needs it:
// the machine running the test maintains a real config, and without the
// redirect the middle rung answers from that instead of from the case at hand.
func withConfig(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	if body != "" {
		if err := os.MkdirAll(filepath.Join(dir, "todoui"), 0o755); err != nil {
			t.Fatalf("creating config directory: %v", err)
		}
		path := filepath.Join(dir, "todoui", "config.toml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("writing config: %v", err)
		}
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
}

// The compiled default names nothing outside the tool's own XDG data directory,
// which is what keeps a generic tool generic.
func TestDefaultPathIsTheToolsOwnDataDirectory(t *testing.T) {
	t.Setenv("TODOUI_REPOS_REGISTRY", "")
	withConfig(t, "")
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg")
	if got, want := DefaultPath(), "/tmp/xdg/todoui/repos.json"; got != want {
		t.Errorf("DefaultPath() = %q, want %q", got, want)
	}
}

// The three rungs in order: the tool's own variable, then its own config key,
// then its own data directory. A machine that maintains its registry elsewhere
// says so in the config, which replaced a hand-made symlink from the data
// directory that was declared nowhere.
func TestDefaultPathResolvesInThreeRungs(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg")
	withConfig(t, "repos_registry = \"/declared/repos.json\"\n")

	t.Setenv("TODOUI_REPOS_REGISTRY", "")
	if got, want := DefaultPath(), "/declared/repos.json"; got != want {
		t.Errorf("the config key must beat the data directory: got %q, want %q", got, want)
	}

	t.Setenv("TODOUI_REPOS_REGISTRY", "/from/env.json")
	if got, want := DefaultPath(), "/from/env.json"; got != want {
		t.Errorf("the variable must beat the config key: got %q, want %q", got, want)
	}
}

// A hand-edited config carries a ~, and left literal it names a directory that
// does not exist — which reads here as an empty registry that bans nothing
// rather than as a bad path.
func TestTheConfiguredPathExpandsALeadingTilde(t *testing.T) {
	t.Setenv("TODOUI_REPOS_REGISTRY", "")
	withConfig(t, "repos_registry = \"~/dev/repos.json\"\n")

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	if got, want := DefaultPath(), filepath.Join(home, "dev", "repos.json"); got != want {
		t.Errorf("DefaultPath() = %q, want %q", got, want)
	}
}

// $REPOS_JSON is never read. It was a rung here once, below no config key at
// all, and it came out: the variable is exported from ~/.env, so a process that
// sources no profile never sees it, and the rung was empty in exactly the
// unattended runs it existed to serve. The invariant is broader than that one
// variable — a tool consults nothing that is not prefixed with its own name.
func TestTheUnprefixedVariableIsNeverConsulted(t *testing.T) {
	t.Setenv("REPOS_JSON", "/unprefixed/repos.json")
	t.Setenv("TODOUI_REPOS_REGISTRY", "")
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg")

	withConfig(t, "")
	if got, want := DefaultPath(), "/tmp/xdg/todoui/repos.json"; got != want {
		t.Errorf("the unprefixed variable displaced the default: got %q, want %q", got, want)
	}

	withConfig(t, "repos_registry = \"/declared/repos.json\"\n")
	if got, want := DefaultPath(), "/declared/repos.json"; got != want {
		t.Errorf("the unprefixed variable displaced the config key: got %q, want %q", got, want)
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

func TestKnowsIdentifiesARepoName(t *testing.T) {
	path := writeRegistry(t, `{"repos":[{"name":"todoui"},{"name":"dotfiles"}]}`)
	registry, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	for _, name := range []string{"todoui", "Todoui", "  dotfiles  ", "DOTFILES"} {
		if !registry.Knows(name) {
			t.Errorf("Knows(%q) = false; a ban a capital letter walks around is decoration", name)
		}
	}
	for _, name := range []string{"", "todoui sync improvements", "Extract xx from dotfiles"} {
		if registry.Knows(name) {
			t.Errorf("Knows(%q) = true; bounded work named after a repo is still bounded work", name)
		}
	}
}

func TestKnowsIsFalseWithoutARegistry(t *testing.T) {
	// Same policy as Validate: no registry means no opinion, because refusing
	// to file work on a machine without one is worse than the wrong name.
	registry, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if registry.Knows("todoui") {
		t.Error("a missing registry must not ban anything")
	}
}
