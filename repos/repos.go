// Package repos reads the repo registry that names what `--repo` may be set to.
//
// A typo'd tag is invisible rather than wrong: the item drops out of every repo
// view and nothing reports an error. Since the repo tag is the axis that
// organizes work across projects, catching the typo at write time is what keeps
// the axis trustworthy.
//
// The registry is optional. Where it does not exist, `--repo` accepts anything —
// refusing to file work on a machine without a registry would make the tag
// harder to set than to leave wrong.
package repos

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/datapointchris/todoui/v2/config"
)

type registryFile struct {
	Repos []struct {
		Name string `json:"name"`
	} `json:"repos"`
}

// Registry is the set of known repo names, empty when the file is absent.
type Registry struct {
	names map[string]bool
	path  string
}

// DefaultPath is todoui's own data directory, per the XDG split — the tool asks
// for the registry where its own data lives and knows nothing about where the
// file is actually maintained. Point it at a shared registry with a symlink.
func DefaultPath() string {
	return filepath.Join(config.UserDataDir(), "todoui", "repos.json")
}

// Load reads the registry at path. A missing file is not an error: it yields a
// registry that accepts anything.
func Load(path string) (*Registry, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Registry{path: path}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading repo registry %s: %w", path, err)
	}
	var parsed registryFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("parsing repo registry %s: %w", path, err)
	}
	names := make(map[string]bool, len(parsed.Repos))
	for _, repo := range parsed.Repos {
		names[repo.Name] = true
	}
	return &Registry{names: names, path: path}, nil
}

// Available reports whether the registry was found and had any repos in it.
func (r *Registry) Available() bool {
	return r != nil && len(r.names) > 0
}

// Names returns every registered repo name, sorted.
func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.names))
	for name := range r.names {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Validate rejects a repo name the registry does not know. An empty name is the
// deliberate "not repo work" case and always passes, as does any name when the
// registry is unavailable.
func (r *Registry) Validate(name string) error {
	if name == "" || !r.Available() || r.names[name] {
		return nil
	}
	if suggestions := r.suggest(name); len(suggestions) > 0 {
		return fmt.Errorf("unknown repo %q — did you mean %s?", name, strings.Join(suggestions, ", "))
	}
	return fmt.Errorf("unknown repo %q — see %s for the registered names", name, r.path)
}

// Knows reports whether name is a registered repo.
//
// The inverse question to Validate, and a separate method because the two want
// opposite answers: Validate wants a repo tag to BE a registry name, and this
// exists to refuse a PROJECT name that is one. Case- and space-insensitive,
// because "Todoui " names the same thing "todoui" does and a ban a capital
// letter walks around is decoration.
func (r *Registry) Knows(name string) bool {
	if !r.Available() {
		return false
	}
	wanted := strings.ToLower(strings.TrimSpace(name))
	if wanted == "" {
		return false
	}
	for known := range r.names {
		if strings.ToLower(known) == wanted {
			return true
		}
	}
	return false
}

// suggest returns registered names sharing a prefix or containing the input,
// which covers the realistic typo: a near-miss on a name you already know.
func (r *Registry) suggest(name string) []string {
	lowered := strings.ToLower(name)
	var matches []string
	for _, known := range r.Names() {
		lowerKnown := strings.ToLower(known)
		if strings.Contains(lowerKnown, lowered) || strings.HasPrefix(lowered, lowerKnown) {
			matches = append(matches, known)
		}
	}
	if len(matches) > 3 {
		matches = matches[:3]
	}
	return matches
}
