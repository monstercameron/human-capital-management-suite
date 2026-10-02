package emojiset

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// testSet is a small set shaped like the generated one: two groups, one emoji
// with five skin-tone variants and one with a mixed-tone pair among them.
func testSet() (*Set, *Lang, *Lang, *Lang) {
	set := &Set{
		Meta:   Meta{Format: FormatVersion, Emoji: "17.0"},
		Groups: []Group{{ID: "smileys-emotion", Name: "Smileys & Emotion", First: 0, Count: 3}, {ID: "people-body", Name: "People & Body", First: 3, Count: 3}},
		Entries: []Entry{
			{Glyph: "😀", Group: 0},
			{Glyph: "😂", Group: 0},
			{Glyph: "🔥", Group: 0},
			{Glyph: "👍", Group: 1, Variants: []string{"👍🏻", "👍🏼", "👍🏽", "👍🏾", "👍🏿"}},
			{Glyph: "🙏", Group: 1, Variants: []string{"🙏🏻", "🙏🏼", "🙏🏽", "🙏🏾", "🙏🏿"}},
			{Glyph: "🤝", Group: 1, Variants: []string{"🤝🏻", "🤝🏼", "🫱🏻‍🫲🏿"}},
		},
	}
	en := &Lang{Code: "en", Names: []string{"grinning face", "face with tears of joy", "fire", "thumbs up", "folded hands", "handshake"},
		Keywords: []string{"face|grin|grinning face", "face|joy|laugh|tear", "flame|tool", "+1|hand|thumb|up", "ask|please|pray|thanks", "agreement|deal|meeting|shake"},
		Tones:    [Tones]string{"light skin tone", "medium-light skin tone", "medium skin tone", "medium-dark skin tone", "dark skin tone"}}
	de := &Lang{Code: "de", Names: []string{"grinsendes Gesicht", "Gesicht mit Freudentränen", "Feuer", "Daumen hoch", "zum Beten gefaltete Hände", ""},
		Keywords: []string{"Gesicht|grinsen", "Freude|lachen|Träne", "Flamme|heiß", "Daumen|gut", "beten|bitte|danke", ""},
		Tones:    [Tones]string{"helle Hautfarbe", "", "", "", ""}}
	ar := &Lang{Code: "ar", Names: []string{"وجه مبتسم", "", "نار", "", "", ""}, Keywords: []string{"", "", "حريق|لهب", "", "", ""}}
	return set, en, de, ar
}

func TestFoldIsCaseAccentAndArabicInsensitive(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Feuer", "feuer"}, {"FEUER", "feuer"}, {"Café", "cafe"}, {"Crème brûlée", "creme brulee"}, {"Gesicht mit Freudentränen", "gesicht mit freudentranen"},
		{":thumbs_up:", "thumbs up"}, {"flag: Côte d’Ivoire", "flag cote d ivoire"}, {"  many   spaces ", "many spaces"}, {"Straße", "strasse"},
		{"أحمر", "احمر"}, {"إبهام", "ابهام"}, {"آلة", "اله"}, {"مَنزِل", "منزل"}, {"كتـــاب", "كتاب"}, {"مدرسة", "مدرسه"}, {"على", "علي"},
		{"٣٠٠", "300"}, {"keycap: #", "keycap #"}, {"👍", "👍"}, {"é", "e"},
	} {
		if got := Fold(tc.in); got != tc.want {
			t.Errorf("Fold(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSearchRanksExactThenPrefixThenWordThenSubstring(t *testing.T) {
	set := &Set{Groups: []Group{{ID: "g", Name: "G", Count: 5}}, Entries: []Entry{{Glyph: "1"}, {Glyph: "2"}, {Glyph: "3"}, {Glyph: "4"}, {Glyph: "5"}}}
	en := &Lang{Code: "en", Names: []string{"red fire", "fire engine", "fire", "campfire", "bonfire truck"}, Keywords: []string{"", "", "", "", "burn"}}
	ix := NewIndex(set, nil, en)
	got := ix.Search("fire", nil)
	// fire (exact), fire engine (prefix), red fire (word), campfire (substring), bonfire truck (substring).
	if want := []int{2, 1, 0, 3, 4}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Search(fire) = %v, want %v", got, want)
	}
	// A keyword match ranks below a name word and above a name substring.
	if got := ix.Search("burn", nil); !reflect.DeepEqual(got, []int{4}) {
		t.Fatalf("keyword search = %v", got)
	}
	// Usage breaks ties inside one tier, never across tiers.
	got = ix.Search("fire", map[string]int{"5": 99, "4": 5})
	if want := []int{2, 1, 0, 4, 3}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Search(fire) with usage = %v, want %v", got, want)
	}
}

func TestSearchMatchesReaderLanguageEnglishShortcodeAndPastedGlyph(t *testing.T) {
	set, en, de, ar := testSet()
	german := NewIndex(set, de, en)
	for _, tc := range []struct {
		ix    *Index
		query string
		first int
	}{
		{german, "Feuer", 2}, {german, "feuer", 2}, {german, "FEUER", 2}, {german, "fire", 2}, {german, ":fire:", 2},
		{german, "Freudentranen", 1}, {german, "freudentränen", 1}, {german, "Gesicht", 1},
		{german, ":thumbs_up:", 3}, {german, "thumbs_up", 3}, {german, "daumen", 3}, {german, "beten", 4}, {german, "pray", 4},
		{german, "handshake", 5}, {german, "👍", 3}, {german, "👍🏽", 3},
		{NewIndex(set, ar, en), "نار", 2}, {NewIndex(set, ar, en), "وجه", 0}, {NewIndex(set, ar, en), "حريق", 2}, {NewIndex(set, ar, en), "fire", 2},
		{NewIndex(set, en, en), "party", -1},
	} {
		got := tc.ix.Search(tc.query, nil)
		if tc.first < 0 {
			if len(got) != 0 {
				t.Errorf("Search(%q) = %v, want nothing", tc.query, got)
			}
			continue
		}
		if len(got) == 0 || got[0] != tc.first {
			t.Errorf("Search(%q) = %v, want %d first", tc.query, got, tc.first)
		}
	}
	if got := german.Search("", nil); got != nil {
		t.Errorf("an empty query returned %v, want nil so the caller shows the full set", got)
	}
	if got := german.Search("  :: ", nil); got != nil {
		t.Errorf("a query of separators returned %v", got)
	}
}

func TestArabicSpellingVariantsMatch(t *testing.T) {
	set := &Set{Groups: []Group{{ID: "g", Name: "G", Count: 2}}, Entries: []Entry{{Glyph: "1"}, {Glyph: "2"}}}
	ar := &Lang{Code: "ar", Names: []string{"أحمر", "مدرسة"}, Keywords: []string{"", ""}}
	ix := NewIndex(set, ar, nil)
	for query, want := range map[string]int{"احمر": 0, "إحمر": 0, "أَحْمَر": 0, "مدرسه": 1, "مدرسة": 1} {
		if got := ix.Search(query, nil); len(got) != 1 || got[0] != want {
			t.Errorf("Search(%q) = %v, want [%d]", query, got, want)
		}
	}
}

func TestShortcodeIsDerivedFromTheEnglishName(t *testing.T) {
	set, en, de, _ := testSet()
	ix := NewIndex(set, de, en)
	if got := ix.Shortcode(3); got != ":thumbs_up:" {
		t.Fatalf("Shortcode = %q", got)
	}
	if got := ix.Shortcode(1); got != ":face_with_tears_of_joy:" {
		t.Fatalf("Shortcode = %q", got)
	}
	if i, ok := ix.FromShortcode(":thumbs_up:"); !ok || i != 3 {
		t.Fatalf("FromShortcode = %d %v", i, ok)
	}
	if _, ok := ix.FromShortcode(":nonsense:"); ok {
		t.Fatal("an unknown shortcode resolved")
	}
	if _, ok := ix.FromShortcode("::"); ok {
		t.Fatal("an empty shortcode resolved")
	}
	if got := NewIndex(set, de, nil).Shortcode(3); got != "" {
		t.Fatalf("Shortcode without English = %q", got)
	}
}

func TestNameFallsBackToEnglish(t *testing.T) {
	set, en, de, _ := testSet()
	ix := NewIndex(set, de, en)
	if got := ix.Name(2); got != "Feuer" {
		t.Fatalf("Name = %q", got)
	}
	if got := ix.Name(5); got != "handshake" {
		t.Fatalf("a missing German name did not fall back to English: %q", got)
	}
	if got, want := ix.ToneName(1), "helle Hautfarbe"; got != want {
		t.Fatalf("ToneName(1) = %q, want %q", got, want)
	}
	if got, want := ix.ToneName(3), "medium skin tone"; got != want {
		t.Fatalf("ToneName(3) = %q, want %q", got, want)
	}
}

func TestEntryForChoosesTheSkinToneVariant(t *testing.T) {
	set, _, _, _ := testSet()
	thumbs := set.Entries[3]
	if got := thumbs.For(0); got != "👍" {
		t.Fatalf("For(0) = %q", got)
	}
	for tone := 1; tone <= 5; tone++ {
		if got, want := thumbs.For(tone), thumbs.Variants[tone-1]; got != want {
			t.Errorf("For(%d) = %q, want %q", tone, got, want)
		}
	}
	// A mixed-tone pair is never picked as "the" variant of one tone.
	hands := set.Entries[5]
	if got := hands.For(5); got != "🤝" {
		t.Fatalf("handshake in a tone it has no variant for = %q, want the default", got)
	}
	if got := hands.For(2); got != "🤝🏼" {
		t.Fatalf("handshake in tone 2 = %q", got)
	}
	if set.Entries[0].For(3) != "😀" || set.Entries[0].HasTones() {
		t.Fatal("an emoji without tones changed")
	}
	if ToneOf("🫱🏻‍🫲🏿") != 0 || ToneOf("👍🏽") != 3 || ToneOf("👍") != 0 {
		t.Fatal("ToneOf misclassified a sequence")
	}
}

func TestLookupFindsVariantsAndFilesRoundTrip(t *testing.T) {
	set, en, de, _ := testSet()
	ix := NewIndex(set, de, en)
	if i, ok := ix.Lookup("🙏🏾"); !ok || i != 4 {
		t.Fatalf("Lookup of a variant = %d %v", i, ok)
	}
	order, err := EncodeOrder(set, "Unicode License v3", map[string]string{"emoji-test.txt": "abc"})
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseOrder(order)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.Entries, set.Entries) || !reflect.DeepEqual(back.Groups, set.Groups) {
		t.Fatalf("order file did not round trip:\n%+v\n%+v", back, set)
	}
	for _, lang := range []*Lang{en, de} {
		data, err := EncodeLang(lang)
		if err != nil {
			t.Fatal(err)
		}
		again, err := ParseLang(data, back)
		if err != nil || !reflect.DeepEqual(again, lang) {
			t.Fatalf("language file %s did not round trip: %v\n%+v\n%+v", lang.Code, err, again, lang)
		}
	}
	data, _ := EncodeLang(en)
	short := &Set{Entries: set.Entries[:2], Groups: set.Groups[:1]}
	if _, err := ParseLang(data, short); err == nil {
		t.Fatal("a language file for a different list was accepted")
	}
	if _, err := ParseOrder([]byte(`{"v":2}`)); err == nil {
		t.Fatal("an unknown format was accepted")
	}
	if _, err := ParseOrder([]byte(`{"v":1,"groups":[{"id":"a","name":"A","e":["x"],"v":{"3":"y"}}]}`)); err == nil {
		t.Fatal("variants outside the group were accepted")
	}
}

// syntheticSet builds n entries named from a vocabulary, like a real list.
func syntheticSet(n int, seed int64) (*Set, *Lang, *Lang) {
	rng := rand.New(rand.NewSource(seed))
	vocabulary := make([]string, 0, 400)
	for i := 0; i < 400; i++ {
		vocabulary = append(vocabulary, fmt.Sprintf("%s%s", []string{"fi", "ba", "mo", "ra", "su", "ti", "ne", "ko"}[rng.Intn(8)], []string{"re", "ker", "lon", "ter", "pan", "mer", "gul", "ven"}[rng.Intn(8)])+fmt.Sprint(i%97))
	}
	set := &Set{Groups: []Group{{ID: "g", Name: "G", Count: n}}}
	en := &Lang{Code: "en"}
	de := &Lang{Code: "de"}
	seen := map[string]bool{}
	for len(set.Entries) < n {
		words := []string{vocabulary[rng.Intn(len(vocabulary))], vocabulary[rng.Intn(len(vocabulary))]}
		if rng.Intn(2) == 0 {
			words = append(words, vocabulary[rng.Intn(len(vocabulary))])
		}
		name := strings.Join(words, " ")
		if seen[name] {
			continue
		}
		seen[name] = true
		set.Entries = append(set.Entries, Entry{Glyph: fmt.Sprintf("g%d", len(set.Entries))})
		en.Names = append(en.Names, name)
		en.Keywords = append(en.Keywords, vocabulary[rng.Intn(len(vocabulary))]+"|"+vocabulary[rng.Intn(len(vocabulary))])
		de.Names = append(de.Names, "Größe "+name)
		de.Keywords = append(de.Keywords, "Stück|"+vocabulary[rng.Intn(len(vocabulary))])
	}
	return set, en, de
}

func TestTodo_CHATEMOJI_001_Property(t *testing.T) {
	set, en, de := syntheticSet(1500, 1)
	ix := NewIndex(set, de, en)
	rng := rand.New(rand.NewSource(2))
	for round := 0; round < 400; round++ {
		i := rng.Intn(len(set.Entries))
		name := en.Names[i]
		// 1. An entry's own name finds it, in the best tier, before anything that is only a partial match.
		got := ix.Search(name, nil)
		if len(got) == 0 {
			t.Fatalf("round %d: the name %q of emoji %d finds nothing", round, name, i)
		}
		found := -1
		for pos, g := range got {
			if g == i {
				found = pos
			}
		}
		if found < 0 {
			t.Fatalf("round %d: the name %q does not find its own emoji", round, name)
		}
		for _, g := range got[:found] {
			if Fold(en.Names[g]) != Fold(name) {
				t.Fatalf("round %d: %q ranks %q above its exact match", round, name, en.Names[g])
			}
		}
		// 2. Case, accent and separator changes never change the answer.
		same := ix.Search(strings.ToUpper(name), nil)
		if !reflect.DeepEqual(got, same) {
			t.Fatalf("round %d: upper-case query for %q gave a different answer", round, name)
		}
		if shortcode := ix.Shortcode(i); shortcode != "" {
			if again := ix.Search(shortcode, nil); !reflect.DeepEqual(got, again) {
				t.Fatalf("round %d: shortcode %q answers differently from the name %q", round, shortcode, name)
			}
		}
		// 3. A German reader finds the same emoji by its German name, accents or not.
		if g := ix.Search(strings.NewReplacer("ö", "o", "ß", "ss").Replace(de.Names[i]), nil); len(g) == 0 || g[0] != i {
			t.Fatalf("round %d: the German name %q does not find its emoji first: %v", round, de.Names[i], g)
		}
		// 4. Tiers never go backwards: every result has a rank no better than the one before.
		prefix := name[:1+rng.Intn(len(name))]
		results := ix.Search(prefix, nil)
		last := -1
		for _, r := range results {
			rank := bestRank(ix, r, prefix)
			if rank < last {
				t.Fatalf("round %d: results for %q are not in tier order at %d", round, prefix, r)
			}
			last = rank
		}
	}
}

func bestRank(ix *Index, i int, query string) int {
	whole := Fold(query)
	tokens := strings.Fields(whole)
	best := rankNone
	for _, li := range ix.langs {
		if r := rankEntry(li.name[i], li.words[i], whole, tokens); r < best {
			best = r
		}
	}
	return best
}

func TestTodo_CHATEMOJI_001_Performance(t *testing.T) {
	set, en, de := syntheticSet(4000, 3)
	started := time.Now()
	ix := NewIndex(set, de, en)
	build := time.Since(started)
	queries := []string{"fi", "fire", "größe", "größe fire", "ba mo", ":fi_re:", "zzzz", "e", "1", "stück"}
	var worst time.Duration
	for round := 0; round < 20; round++ {
		for _, query := range queries {
			started := time.Now()
			ix.Search(query, map[string]int{"g5": 7, "g1": 2})
			if elapsed := time.Since(started); elapsed > worst {
				worst = elapsed
			}
		}
	}
	t.Logf("index of 4000 entries built in %v; slowest of %d queries: %v", build, 20*len(queries), worst)
	if worst > 30*time.Millisecond {
		t.Fatalf("a query over 4,000 emoji took %v, the limit is 30ms", worst)
	}
}

func BenchmarkSearch4000(b *testing.B) {
	set, en, de := syntheticSet(4000, 3)
	ix := NewIndex(set, de, en)
	usage := map[string]int{"g5": 7}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ix.Search("fi", usage)
	}
}

func BenchmarkNewIndex4000(b *testing.B) {
	set, en, de := syntheticSet(4000, 3)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NewIndex(set, de, en)
	}
}

// emojiOracle is the plain rule the owner stated: an emoji matches a query only
// when every word of the query is a word start or a substring of its name or one
// of its keywords, in the reader's language or in English. It shares no code with
// Search.
func emojiOracle(ix *Index, i int, query string) bool {
	for _, token := range strings.Fields(Fold(query)) {
		found := false
		for _, li := range ix.langs {
			if strings.Contains(li.name[i], token) || strings.Contains(li.words[i], token) {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return len(strings.Fields(Fold(query))) > 0
}

// A query finds the emoji it names and nothing else: no fuzzy or per-letter
// match, no match through the group name, and no padding when there is little.
func TestSearchFindsOnlyWhatContainsTheQuery(t *testing.T) {
	set := &Set{
		Meta:   Meta{Format: FormatVersion, Emoji: "17.0"},
		Groups: []Group{{ID: "smileys-emotion", Name: "Smileys & Emotion", First: 0, Count: 2}, {ID: "food-drink", Name: "Food & Drink", First: 2, Count: 2}, {ID: "objects", Name: "Objects", First: 4, Count: 4}},
		Entries: []Entry{
			{Glyph: "🎉", Group: 0}, {Glyph: "🥳", Group: 0}, {Glyph: "🍕", Group: 1}, {Glyph: "🥧", Group: 1},
			{Glyph: "📌", Group: 2}, {Glyph: "💯", Group: 2}, {Glyph: "🏆", Group: 2}, {Glyph: "🔥", Group: 2},
		},
	}
	en := &Lang{Code: "en",
		Names:    []string{"party popper", "partying face", "pizza", "pie", "pushpin", "hundred points", "trophy", "fire"},
		Keywords: []string{"celebration|party|tada", "celebration|party", "cheese|slice", "dessert|slice", "pin|tack", "100|score|perfect", "prize|win", "flame|hot"}}
	de := &Lang{Code: "de",
		Names:    []string{"Konfettikanone", "Partygesicht", "Pizza", "Kuchen", "Reißzwecke", "Hundert Punkte", "Pokal", "Feuer"},
		Keywords: []string{"Feier|Party", "Feier|Party", "Käse", "Dessert", "Pin", "100|perfekt", "Preis|Sieg", "Flamme|heiß"}}
	ar := &Lang{Code: "ar", Names: []string{"", "", "", "", "", "", "", "نار"}, Keywords: []string{"", "", "", "", "", "", "", "حريق|لهب"}}
	for _, tc := range []struct {
		reader *Lang
		query  string
		want   []string
	}{
		{en, "party", []string{"🎉", "🥳"}},
		{en, "Party", []string{"🎉", "🥳"}},
		{en, "partying", []string{"🥳"}},
		{en, "pa", []string{"🎉", "🥳"}}, // two letters: names and keywords with "pa" only
		{en, "pi", []string{"🍕", "🥧", "📌"}},
		{en, "zzzz", nil},
		{en, "partyy", nil},
		{en, "ptry", nil}, // letters in order are not a match
		{en, "ypart", nil},
		{en, "emotion", nil}, // the group's name is not an emoji's name
		{en, "smileys", nil},
		{de, "party", []string{"🎉", "🥳"}}, // German reader, English keyword
		{de, "Partyg", []string{"🥳"}},
		{de, "feuer", []string{"🔥"}},
		{de, "FEU", []string{"🔥"}},
		{de, "Kase", []string{"🍕"}}, // accent-insensitive: Käse
		{de, "fire", []string{"🔥"}},
		{ar, "نار", []string{"🔥"}},
		{ar, "حريق", []string{"🔥"}},
		{ar, "party", []string{"🎉", "🥳"}},
		{ar, "فاكهة", nil},
	} {
		ix := NewIndex(set, tc.reader, en)
		got := []string{}
		for _, i := range ix.Search(tc.query, nil) {
			got = append(got, set.Entries[i].Glyph)
			if !emojiOracle(ix, i, tc.query) {
				t.Errorf("%s %q returned %s, which does not contain the query", tc.reader.Code, tc.query, set.Entries[i].Glyph)
			}
		}
		if !reflect.DeepEqual(got, append([]string{}, tc.want...)) {
			t.Errorf("%s %q = %v, want %v", tc.reader.Code, tc.query, got, tc.want)
		}
	}
	// Using an emoji a lot never lets it into a result it does not match.
	ix := NewIndex(set, en, en)
	heavy := map[string]int{"🍕": 99, "📌": 98, "💯": 97, "🏆": 96}
	if got := ix.Search("party", heavy); len(got) != 2 || set.Entries[got[0]].Glyph != "🎉" {
		t.Errorf("a frequently used emoji padded or reordered a result: %v", got)
	}
}

// The same rule holds on the data the product ships.
func TestSearchOnTheShippedDataReturnsOnlyMatches(t *testing.T) {
	read := func(name string) []byte {
		data, err := os.ReadFile(filepath.Join("..", "workspace", "assets", name))
		if err != nil {
			t.Fatalf("the emoji data is not in the workspace assets: %v", err)
		}
		return data
	}
	set, err := ParseOrder(read("emoji-order.json"))
	if err != nil {
		t.Fatal(err)
	}
	langs := map[string]*Lang{}
	for _, code := range []string{"en", "de", "ar"} {
		if langs[code], err = ParseLang(read("emoji-"+code+".json"), set); err != nil {
			t.Fatal(err)
		}
	}
	for _, code := range []string{"en", "de", "ar"} {
		ix := NewIndex(set, langs[code], langs["en"])
		for _, q := range []string{"party", "pa", "p", "fi", "face", "zzzz", "feuer", "gesicht", "نار", "وجه", "thumbs up", ":fire:", "heart"} {
			for _, i := range ix.Search(q, nil) {
				if !emojiOracle(ix, i, q) {
					t.Errorf("%s %q returned %s (%s), which does not contain the query", code, q, set.Entries[i].Glyph, ix.Name(i))
				}
			}
		}
		if got := ix.Search("party", nil); len(got) != 1 || set.Entries[got[0]].Glyph != "🎉" {
			t.Errorf("%s: \"party\" found %d emoji on the shipped data, want party popper alone", code, len(got))
		}
		if got := ix.Search("zzzz", nil); len(got) != 0 {
			t.Errorf("%s: a query matching nothing found %d emoji", code, len(got))
		}
	}
}
