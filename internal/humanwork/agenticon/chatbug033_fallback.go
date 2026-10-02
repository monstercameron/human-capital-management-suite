package agenticon

import (
	"crypto/sha256"
	"encoding/binary"
	"sort"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Fallbacks gives every seed (an agent's id) the icon to draw while the agent
// has no stored icon. It uses the generator's own vocabulary and the same
// digest-to-shape-and-colour step Generate uses, so a fallback is a real icon
// from the same set, never the one neutral glyph; and it hands two seeds two
// different glyphs for as long as unused glyphs remain. The neutral default is
// not a fallback: it would be the same picture for every agent.
//
// The result depends only on the set of seeds, so one set always yields the
// same icons. It builds in the browser as well as on the server, where Generate
// (which needs the keyword vocabulary) is not available.
func Fallbacks(seeds []string) map[string]Value {
	unique := make([]string, 0, len(seeds))
	seen := make(map[string]bool, len(seeds))
	for _, seed := range seeds {
		seed = strings.TrimSpace(seed)
		if seed != "" && !seen[seed] {
			seen[seed] = true
			unique = append(unique, seed)
		}
	}
	sort.Strings(unique)
	catalog := glyphs()[1:]
	taken := make(map[int]bool, len(unique))
	out := make(map[string]Value, len(unique))
	for _, seed := range unique {
		digest := sha256.Sum256([]byte("agent-fallback\x00" + seed))
		start := int(binary.BigEndian.Uint32(digest[:4]) % uint32(len(catalog)))
		pick := start
		for step := 0; step < len(catalog); step++ {
			candidate := (start + step) % len(catalog)
			if !taken[candidate] {
				pick = candidate
				break
			}
		}
		taken[pick] = true
		index := int(digest[4]) % Variations
		pair := ColourPairs()[index/len(shapes())]
		out[seed] = Value{Glyph: catalog[pick].ID, Shape: shapes()[index%len(shapes())].id, Foreground: pair.Foreground, Background: pair.Background, Variation: uint32(index)}
	}
	return out
}

// Fallback is the icon for one seed on its own; see Fallbacks.
func Fallback(seed string) Value {
	return Fallbacks([]string{seed})[strings.TrimSpace(seed)]
}

// ValueFor is the icon an agent wears: its stored icon when it has one, else its
// own fallback among the siblings listed with it, so two agents on one page are
// never given the same picture. The zero Value comes back only for an agent with
// no stored icon and no id to derive one from.
func ValueFor(stored Value, seed string, siblings []string) Value {
	if stored.Valid() {
		return stored
	}
	seed = strings.TrimSpace(seed)
	if seed == "" {
		return Value{}
	}
	return Fallbacks(append(append([]string(nil), siblings...), seed))[seed]
}

// NodeFor draws ValueFor. A page that lists agents passes every agent's id as
// siblings; the one neutral glyph Node draws for an unknown value is never drawn.
func NodeFor(stored Value, seed string, siblings ...string) ui.Node {
	return Node(ValueFor(stored, seed, siblings))
}
