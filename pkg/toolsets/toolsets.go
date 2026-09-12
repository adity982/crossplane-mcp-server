// Package toolsets collects the tool groups this server can expose and lets
// the command line pick between them.
package toolsets

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/crossplane-contrib/crossplane-mcp-server/pkg/api"
)

var (
	mu         sync.RWMutex
	registered = map[string]api.Toolset{}
)

// Register adds a toolset to the registry. Toolsets call this from an init
// function, so importing the package is enough to make them available.
//
// Register panics on a duplicate name, because that can only be a build time
// mistake.
func Register(ts api.Toolset) {
	mu.Lock()
	defer mu.Unlock()
	if _, exists := registered[ts.Name()]; exists {
		panic(fmt.Sprintf("toolset %q is already registered", ts.Name()))
	}
	registered[ts.Name()] = ts
}

// All returns every registered toolset, ordered by name.
func All() []api.Toolset {
	mu.RLock()
	defer mu.RUnlock()

	all := make([]api.Toolset, 0, len(registered))
	for _, ts := range registered {
		all = append(all, ts)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name() < all[j].Name() })
	return all
}

// Names returns the names of every registered toolset, ordered.
func Names() []string {
	names := make([]string, 0)
	for _, ts := range All() {
		names = append(names, ts.Name())
	}
	return names
}

// Select resolves a list of toolset names, keeping the registry order so that
// the tool list a client sees does not depend on flag ordering.
//
// The special name "all" selects everything.
func Select(names []string) ([]api.Toolset, error) {
	if len(names) == 0 {
		return All(), nil
	}

	wanted := map[string]bool{}
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		if name == "all" {
			return All(), nil
		}
		wanted[name] = true
	}

	selected := make([]api.Toolset, 0, len(wanted))
	for _, ts := range All() {
		if wanted[ts.Name()] {
			selected = append(selected, ts)
			delete(wanted, ts.Name())
		}
	}
	if len(wanted) > 0 {
		unknown := make([]string, 0, len(wanted))
		for name := range wanted {
			unknown = append(unknown, name)
		}
		sort.Strings(unknown)
		return nil, fmt.Errorf("unknown toolset(s) %s: available toolsets are %s",
			strings.Join(unknown, ", "), strings.Join(Names(), ", "))
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no toolsets selected")
	}
	return selected, nil
}
