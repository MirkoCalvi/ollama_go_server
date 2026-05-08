// Package personalities holds the concrete Character definitions used by the
// chat handler. Each character lives in its own file (frank.go, olivia.go, …)
// and is registered in the byName map below so handlers can resolve a request
// body's "character" string to a *ollama.Character.
package personalities

import (
	"sort"

	"github.com/MirkoCalvi/httpserver/backend/internal/ollama"
)

var byName = map[string]*ollama.Character{
	"Frank":  &Frank,
	"Olivia": &Olivia,
	"August": &August,
	"Robert": &Robert,
	"James":  &James,
}

// Get returns the Character for the given name, or nil if unknown.
// Names are case-sensitive and must match the canonical capitalised form
// (e.g. "Frank", not "frank"). Drop-in replacement for the legacy
// ollama.CreateCharacter.
func Get(name string) *ollama.Character {
	return byName[name]
}

// Names returns the registered character names in sorted order. Sorted so the
// public /characters endpoint has a stable response that doesn't depend on
// map iteration order.
func Names() []string {
	out := make([]string, 0, len(byName))
	for name := range byName {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
