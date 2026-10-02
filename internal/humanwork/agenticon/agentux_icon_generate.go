//go:build !(js && wasm)

package agenticon

import (
	"crypto/sha256"
	"encoding/binary"
	"sort"
	"strings"
	"unicode"
)

func normalize(text string) string { return strings.Join(strings.Fields(strings.ToLower(text)), " ") }
func words(text string) string {
	return " " + strings.Join(strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }), " ") + " "
}

func Generate(input Input) Value { return GenerateVariation(input, 0) }

// rankedGlyphs orders every glyph for an agent: glyphs whose keywords match
// its own text first, strongest match first, then the rest in an order taken
// from the agent's digest. What the agent is called and what it is for count
// far more than words that merely occur in its instructions or skill names,
// so "Policy Helper" is a book even though its skills all mention chat.
func rankedGlyphs(input Input, digest [32]byte) []Glyph {
	texts := []string{words(input.Name), words(input.Description), words(strings.Join(input.Skills, " ")), words(input.Instructions)}
	weights := []int{12, 6, 2, 1}
	all := glyphs()
	scores := make([]int, len(all))
	// The neutral default (index 0) has no keywords and never scores.
	best, candidates := 0, []int{0}
	for g := 1; g < len(all); g++ {
		for _, keywords := range all[g].Keywords {
			for _, keyword := range strings.Split(keywords, ",") {
				if strings.TrimSpace(keyword) == "" {
					continue
				}
				for i, text := range texts {
					if strings.Contains(text, words(keyword)) {
						scores[g] += weights[i]
					}
				}
			}
		}
		if scores[g] > best {
			best, candidates = scores[g], []int{g}
		} else if scores[g] == best && best > 0 {
			candidates = append(candidates, g)
		}
	}
	// First choice: the strongest match, the digest choosing among equals, and
	// the neutral default when nothing matches.
	first := candidates[int(binary.BigEndian.Uint32(digest[:4]))%len(candidates)]
	// The rest follow by strength, equals in an order taken from the digest, so
	// two agents with no matching words are still offered different glyphs.
	tie := func(g int) uint32 {
		mixed := sha256.Sum256(append(digest[:8:8], all[g].ID...))
		return binary.BigEndian.Uint32(mixed[:4])
	}
	rest := make([]int, 0, len(all)-1)
	for g := range all {
		if g != first {
			rest = append(rest, g)
		}
	}
	sort.SliceStable(rest, func(a, b int) bool {
		if scores[rest[a]] != scores[rest[b]] {
			return scores[rest[a]] > scores[rest[b]]
		}
		return tie(rest[a]) < tie(rest[b])
	})
	ranked := make([]Glyph, 0, len(all))
	ranked = append(ranked, all[first])
	for _, g := range rest {
		ranked = append(ranked, all[g])
	}
	return ranked
}

func GenerateVariation(input Input, variation uint32) Value {
	digest := sha256.Sum256([]byte(normalize(input.Name) + "\x00" + normalize(input.Instructions)))
	return variationOf(rankedGlyphs(input, digest)[0], digest, variation)
}

func variationOf(glyph Glyph, digest [32]byte, variation uint32) Value {
	v := variation % Variations
	index := (int(digest[4])%Variations + int(v)) % Variations
	pair := ColourPairs()[index/len(shapes())]
	return Value{Glyph: glyph.ID, Shape: shapes()[index%len(shapes())].id, Foreground: pair.Foreground, Background: pair.Background, Variation: v}
}

// Unique gives an agent an icon no other agent in the workspace has. It first
// looks for a glyph nobody else uses, in the agent's own order of preference,
// so two agents never share a picture while unused glyphs remain; only then
// does it fall back to the same glyph on a different shape and colour.
func Unique(input Input, used []Value, start uint32) (Value, bool) {
	digest := sha256.Sum256([]byte(normalize(input.Name) + "\x00" + normalize(input.Instructions)))
	occupied := make(map[string]bool, len(used))
	taken := make(map[string]bool, len(used))
	for _, value := range used {
		occupied[value.Glyph+"/"+value.Shape+"/"+value.Foreground+"/"+value.Background] = true
		taken[value.Glyph] = true
	}
	ranked := rankedGlyphs(input, digest)
	for _, glyph := range ranked {
		if !taken[glyph.ID] {
			return variationOf(glyph, digest, start), true
		}
	}
	for i := uint32(0); i < Variations; i++ {
		value := variationOf(ranked[0], digest, start+i)
		if !occupied[value.Glyph+"/"+value.Shape+"/"+value.Foreground+"/"+value.Background] {
			return value, true
		}
	}
	return variationOf(ranked[0], digest, start), false
}

// Shuffle preserves the recognized glyph even when the agent has a newer
// instruction version. Regenerate is the operation that reinterprets meaning.
func Shuffle(current Value, used []Value) (Value, bool) {
	if !current.Valid() {
		current = Generate(Input{})
	}
	index := 0
	for i, pair := range ColourPairs() {
		if pair.Foreground == current.Foreground && pair.Background == current.Background {
			for j, shape := range shapes() {
				if shape.id == current.Shape {
					index = i*len(shapes()) + j
				}
			}
		}
	}
	for step := uint32(1); step < Variations; step++ {
		next := (index + int(step)) % Variations
		pair := ColourPairs()[next/len(shapes())]
		value := Value{Glyph: current.Glyph, Shape: shapes()[next%len(shapes())].id, Foreground: pair.Foreground, Background: pair.Background, Variation: (current.Variation + step) % Variations}
		available := true
		for _, other := range used {
			if value.Glyph == other.Glyph && value.Shape == other.Shape && value.Foreground == other.Foreground && value.Background == other.Background {
				available = false
				break
			}
		}
		if available {
			return value, true
		}
	}
	return current, false
}
