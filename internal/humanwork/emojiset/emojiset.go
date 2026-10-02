// Package emojiset is the emoji data model, the readers for the generated data
// files, and emoji search.
//
// The list itself (which emoji exist, how Unicode groups and orders them, which
// skin-tone variants belong to which base emoji) is language independent and is
// held in one "order" file. Each product language has one more file with the
// emoji's names and search keywords, index for index. tools/emojidata writes
// the files from Unicode's emoji-test.txt and the CLDR annotations; the chat
// client reads them. Nothing here fetches anything, touches the DOM, or is
// compiled for one platform only, so search is an ordinary unit-tested function.
package emojiset

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// FormatVersion is the version of the order and language files.
const FormatVersion = 1

// Tones is the number of Fitzpatrick skin tones (light to dark).
const Tones = 5

// Group is one of Unicode's emoji groups, in Unicode's order.
type Group struct {
	ID    string // stable identifier, e.g. "smileys-emotion"
	Name  string // Unicode's English group name, e.g. "Smileys & Emotion"
	First int    // index of the group's first emoji in Set.Entries
	Count int
}

// Entry is one emoji with every skin-tone or other variant attached to it.
type Entry struct {
	Glyph    string
	Group    int
	Variants []string
}

// Meta records which data the files were generated from.
type Meta struct {
	Format    int
	Emoji     string // Unicode emoji version, e.g. "17.0"
	CLDR      string
	Generator string
	Entries   int
}

// Set is the language-independent list.
type Set struct {
	Meta    Meta
	Groups  []Group
	Entries []Entry
}

// Lang holds one language's names and keywords, aligned with Set.Entries. An
// empty name means the language has no translation for that emoji.
type Lang struct {
	Code     string
	Names    []string
	Keywords []string // keywords separated by "|"
	Tones    [Tones]string
}

type orderFile struct {
	V         int               `json:"v"`
	Emoji     string            `json:"emoji"`
	CLDR      string            `json:"cldr"`
	Generator string            `json:"generator"`
	Groups    []orderFileGroup  `json:"groups"`
	Licence   string            `json:"licence,omitempty"`
	Sources   map[string]string `json:"sources,omitempty"`
}

type orderFileGroup struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Emoji    []string          `json:"e"`
	Variants map[string]string `json:"v,omitempty"` // position within the group -> variants separated by a space
}

type langFile struct {
	V     int      `json:"v"`
	Lang  string   `json:"lang"`
	Names []string `json:"n"`
	Words []string `json:"k"`
	Tones []string `json:"t,omitempty"`
}

// ParseOrder reads an order file.
func ParseOrder(data []byte) (*Set, error) {
	var file orderFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("emoji order file: %w", err)
	}
	if file.V != FormatVersion {
		return nil, fmt.Errorf("emoji order file has format %d, want %d", file.V, FormatVersion)
	}
	set := &Set{Meta: Meta{Format: file.V, Emoji: file.Emoji, CLDR: file.CLDR, Generator: file.Generator}}
	for gi, group := range file.Groups {
		set.Groups = append(set.Groups, Group{ID: group.ID, Name: group.Name, First: len(set.Entries), Count: len(group.Emoji)})
		for pos, glyph := range group.Emoji {
			if glyph == "" {
				return nil, fmt.Errorf("emoji order file: group %q has an empty emoji at %d", group.ID, pos)
			}
			entry := Entry{Glyph: glyph, Group: gi}
			if variants, ok := group.Variants[strconv.Itoa(pos)]; ok {
				entry.Variants = strings.Fields(variants)
			}
			set.Entries = append(set.Entries, entry)
		}
	}
	for key := range file.Groups {
		for position := range file.Groups[key].Variants {
			index, err := strconv.Atoi(position)
			if err != nil || index < 0 || index >= len(file.Groups[key].Emoji) {
				return nil, fmt.Errorf("emoji order file: group %q has variants for position %q outside the group", file.Groups[key].ID, position)
			}
		}
	}
	set.Meta.Entries = len(set.Entries)
	return set, nil
}

// ParseLang reads a language file and checks it against set.
func ParseLang(data []byte, set *Set) (*Lang, error) {
	var file langFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("emoji language file: %w", err)
	}
	if file.V != FormatVersion {
		return nil, fmt.Errorf("emoji language file has format %d, want %d", file.V, FormatVersion)
	}
	if set == nil {
		return nil, errors.New("emoji language file needs the order file first")
	}
	if len(file.Names) != len(set.Entries) || len(file.Words) != len(set.Entries) {
		return nil, fmt.Errorf("emoji language file %q has %d names and %d keyword lists for %d emoji", file.Lang, len(file.Names), len(file.Words), len(set.Entries))
	}
	lang := &Lang{Code: file.Lang, Names: file.Names, Keywords: file.Words}
	copy(lang.Tones[:], file.Tones)
	return lang, nil
}

// EncodeOrder writes an order file. licence and sources are recorded beside the
// data so the file carries its own notice.
func EncodeOrder(set *Set, licence string, sources map[string]string) ([]byte, error) {
	file := orderFile{V: FormatVersion, Emoji: set.Meta.Emoji, CLDR: set.Meta.CLDR, Generator: set.Meta.Generator, Licence: licence, Sources: sources}
	for _, group := range set.Groups {
		out := orderFileGroup{ID: group.ID, Name: group.Name}
		for pos := 0; pos < group.Count; pos++ {
			entry := set.Entries[group.First+pos]
			out.Emoji = append(out.Emoji, entry.Glyph)
			if len(entry.Variants) > 0 {
				if out.Variants == nil {
					out.Variants = map[string]string{}
				}
				out.Variants[strconv.Itoa(pos)] = strings.Join(entry.Variants, " ")
			}
		}
		file.Groups = append(file.Groups, out)
	}
	return json.Marshal(file)
}

// EncodeLang writes a language file.
func EncodeLang(lang *Lang) ([]byte, error) {
	file := langFile{V: FormatVersion, Lang: lang.Code, Names: lang.Names, Words: lang.Keywords}
	for _, tone := range lang.Tones {
		if tone != "" {
			file.Tones = lang.Tones[:]
			break
		}
	}
	return json.Marshal(file)
}

// ToneModifier is the code point of skin tone 1 to 5 (U+1F3FB to U+1F3FF).
func ToneModifier(tone int) rune { return 0x1F3FA + rune(tone) }

// ToneOf reports which skin tone (1 to 5) a variant uses, or 0 when it uses
// none or mixes two tones, as the couple and handshake sequences can.
func ToneOf(glyph string) int {
	tone := 0
	for _, r := range glyph {
		if r >= 0x1F3FB && r <= 0x1F3FF {
			t := int(r - 0x1F3FA)
			if tone != 0 && tone != t {
				return 0
			}
			tone = t
		}
	}
	return tone
}

// For returns the emoji in the given skin tone (0 is the default yellow). An
// emoji without a variant for that tone is returned as it is.
func (e Entry) For(tone int) string {
	if tone <= 0 || tone > Tones {
		return e.Glyph
	}
	for _, variant := range e.Variants {
		if ToneOf(variant) == tone {
			return variant
		}
	}
	return e.Glyph
}

// HasTones reports whether the emoji has skin-tone variants.
func (e Entry) HasTones() bool { return len(e.Variants) > 0 }

// Index is a Set with the names of the reader's language and of English,
// prepared for search.
type Index struct {
	Set     *Set
	langs   []*langIndex // reader's language first, then English
	english *langIndex
	byGlyph map[string]int
}

type langIndex struct {
	lang  *Lang
	name  []string // folded names
	words []string // folded keywords, space separated
}

// NewIndex prepares search. reader is the person's language; english is always
// searched as well (it may be the same language, or nil if it could not be
// loaded). Either may be nil.
func NewIndex(set *Set, reader, english *Lang) *Index {
	ix := &Index{Set: set, byGlyph: make(map[string]int, len(set.Entries))}
	for i, entry := range set.Entries {
		ix.byGlyph[entry.Glyph] = i
		for _, variant := range entry.Variants {
			ix.byGlyph[variant] = i
		}
	}
	add := func(lang *Lang) *langIndex {
		if lang == nil {
			return nil
		}
		li := &langIndex{lang: lang, name: make([]string, len(set.Entries)), words: make([]string, len(set.Entries))}
		for i := range set.Entries {
			li.name[i] = Fold(lang.Names[i])
			li.words[i] = Fold(lang.Keywords[i])
		}
		ix.langs = append(ix.langs, li)
		return li
	}
	if reader != nil && english != nil && reader.Code == english.Code {
		reader = nil
	}
	add(reader)
	ix.english = add(english)
	return ix
}

// Lookup finds the emoji a glyph (or one of its variants) belongs to.
func (ix *Index) Lookup(glyph string) (int, bool) {
	i, ok := ix.byGlyph[glyph]
	return i, ok
}

// Name is the emoji's name in the reader's language, or in English when the
// reader's language has none.
func (ix *Index) Name(i int) string {
	for _, li := range ix.langs {
		if i >= 0 && i < len(li.lang.Names) && li.lang.Names[i] != "" {
			return li.lang.Names[i]
		}
	}
	return ""
}

// ToneName is the name of skin tone 1 to 5 in the reader's language, falling
// back to English.
func (ix *Index) ToneName(tone int) string {
	if tone < 1 || tone > Tones {
		return ""
	}
	for _, li := range ix.langs {
		if li.lang.Tones[tone-1] != "" {
			return li.lang.Tones[tone-1]
		}
	}
	return ""
}

// Shortcode is the colon form of an emoji, derived from its English name:
// ":thumbs_up:". It is empty when English is not loaded.
func (ix *Index) Shortcode(i int) string {
	if ix.english == nil || i < 0 || i >= len(ix.english.name) || ix.english.name[i] == "" {
		return ""
	}
	return ":" + strings.ReplaceAll(ix.english.name[i], " ", "_") + ":"
}

// FromShortcode returns the emoji a complete ":shortcode:" names. The first
// emoji in Unicode order wins when two English names fold to the same code.
func (ix *Index) FromShortcode(code string) (int, bool) {
	if len(code) < 3 || code[0] != ':' || code[len(code)-1] != ':' || ix.english == nil {
		return 0, false
	}
	want := Fold(code)
	if want == "" {
		return 0, false
	}
	for i, name := range ix.english.name {
		if name == want {
			return i, true
		}
	}
	return 0, false
}

// Search ranking tiers, best first.
const (
	rankExactName = iota
	rankNamePrefix
	rankNameWord
	rankKeywordWord
	rankNameSubstring
	rankKeywordSubstring
	rankNone
)

// Search returns the emoji matching query, best first: an exact name, then a
// name that starts with the query, then words that start with it (in the name,
// then in the keywords), then substrings; within one tier the emoji the person
// uses most come first (usage maps a glyph to how often it was chosen), then
// Unicode order. It matches in the reader's language and in English. An empty
// query returns nil; the caller shows the whole set instead.
func (ix *Index) Search(query string, usage map[string]int) []int {
	whole := Fold(query)
	if whole == "" {
		return nil
	}
	tokens := strings.Fields(whole)
	type hit struct{ index, rank, used int }
	var hits []hit
	pasted, pastedOK := ix.byGlyph[strings.TrimSpace(query)]
	for i := range ix.Set.Entries {
		best := rankNone
		for _, li := range ix.langs {
			if r := rankEntry(li.name[i], li.words[i], whole, tokens); r < best {
				best = r
			}
		}
		if pastedOK && i == pasted {
			best = rankExactName
		}
		if best == rankNone {
			continue
		}
		used := 0
		if usage != nil {
			used = usage[ix.Set.Entries[i].Glyph]
			for _, variant := range ix.Set.Entries[i].Variants {
				used += usage[variant]
			}
		}
		hits = append(hits, hit{i, best, used})
	}
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].rank != hits[b].rank {
			return hits[a].rank < hits[b].rank
		}
		if hits[a].used != hits[b].used {
			return hits[a].used > hits[b].used
		}
		return hits[a].index < hits[b].index
	})
	result := make([]int, len(hits))
	for i, h := range hits {
		result[i] = h.index
	}
	return result
}

func rankEntry(name, words, whole string, tokens []string) int {
	if name != "" {
		if name == whole {
			return rankExactName
		}
		if strings.HasPrefix(name, whole) {
			return rankNamePrefix
		}
	}
	worst := rankNameWord
	for _, token := range tokens {
		r := rankToken(name, words, token)
		if r == rankNone {
			return rankNone
		}
		if r > worst {
			worst = r
		}
	}
	return worst
}

func rankToken(name, words, token string) int {
	if name != "" && (strings.HasPrefix(name, token) || strings.Contains(name, " "+token)) {
		return rankNameWord
	}
	if words != "" && (strings.HasPrefix(words, token) || strings.Contains(words, " "+token)) {
		return rankKeywordWord
	}
	if name != "" && strings.Contains(name, token) {
		return rankNameSubstring
	}
	if words != "" && strings.Contains(words, token) {
		return rankKeywordSubstring
	}
	return rankNone
}
