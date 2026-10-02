package chatui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/emojiset"
)

// The emoji data the product ships today is generated from a small hand-checked
// source (tools/emojidata/testdata/interim) because Unicode's own files are not
// on this machine. These tests hold what ships to Unicode's facts: each emoji is
// in the group Unicode puts it in, in Unicode's order within it, named and given
// search words in every product language. TestTodo_CHATEMOJI_001_CompleteSet
// steps aside until the full data is in place and then proves it complete.

// emojiReference is one shipped emoji as Unicode has it. English is Unicode's own
// name for the emoji (emoji-test.txt); order is the position in this table, which
// is Unicode's order for the emoji listed.
type emojiReference struct {
	english string
	group   string
	tones   bool
	glyph   string
}

func emojiRef(english, group string, tones bool, codes ...rune) emojiReference {
	return emojiReference{english, group, tones, string(codes)}
}

// The reference is written as code points so that no invisible character (a
// variation selector, a joiner) can be lost in an editor.
var emojiReferenceList = []emojiReference{
	emojiRef("grinning face", "smileys-emotion", false, 0x1F600),
	emojiRef("face with tears of joy", "smileys-emotion", false, 0x1F602),
	emojiRef("smiling face with smiling eyes", "smileys-emotion", false, 0x1F60A),
	emojiRef("smiling face with heart-eyes", "smileys-emotion", false, 0x1F60D),
	emojiRef("thinking face", "smileys-emotion", false, 0x1F914),
	emojiRef("sleeping face", "smileys-emotion", false, 0x1F634),
	emojiRef("smiling face with sunglasses", "smileys-emotion", false, 0x1F60E),
	emojiRef("crying face", "smileys-emotion", false, 0x1F622),
	emojiRef("broken heart", "smileys-emotion", false, 0x1F494),
	emojiRef("red heart", "smileys-emotion", false, 0x2764, 0xFE0F),
	emojiRef("hundred points", "smileys-emotion", false, 0x1F4AF),

	emojiRef("waving hand", "people-body", true, 0x1F44B),
	emojiRef("thumbs up", "people-body", true, 0x1F44D),
	emojiRef("clapping hands", "people-body", true, 0x1F44F),
	emojiRef("folded hands", "people-body", true, 0x1F64F),
	emojiRef("flexed biceps", "people-body", true, 0x1F4AA),
	emojiRef("eyes", "people-body", false, 0x1F440),
	emojiRef("technologist", "people-body", true, 0x1F9D1, 0x200D, 0x1F4BB),
	emojiRef("family: man, woman, girl", "people-body", false, 0x1F468, 0x200D, 0x1F469, 0x200D, 0x1F467),

	emojiRef("dog face", "animals-nature", false, 0x1F436),
	emojiRef("cat face", "animals-nature", false, 0x1F431),
	emojiRef("bear", "animals-nature", false, 0x1F43B),
	emojiRef("cherry blossom", "animals-nature", false, 0x1F338),
	emojiRef("rose", "animals-nature", false, 0x1F339),
	emojiRef("deciduous tree", "animals-nature", false, 0x1F333),

	emojiRef("red apple", "food-drink", false, 0x1F34E),
	emojiRef("pizza", "food-drink", false, 0x1F355),
	emojiRef("hot beverage", "food-drink", false, 0x2615),
	emojiRef("beer mug", "food-drink", false, 0x1F37A),

	emojiRef("globe showing Europe-Africa", "travel-places", false, 0x1F30D),
	emojiRef("house", "travel-places", false, 0x1F3E0),
	emojiRef("automobile", "travel-places", false, 0x1F697),
	emojiRef("airplane", "travel-places", false, 0x2708, 0xFE0F),
	emojiRef("sun", "travel-places", false, 0x2600, 0xFE0F),
	emojiRef("star", "travel-places", false, 0x2B50),
	emojiRef("fire", "travel-places", false, 0x1F525),

	emojiRef("party popper", "activities", false, 0x1F389),
	emojiRef("trophy", "activities", false, 0x1F3C6),
	emojiRef("soccer ball", "activities", false, 0x26BD),
	emojiRef("video game", "activities", false, 0x1F3AE),

	emojiRef("telephone receiver", "objects", false, 0x1F4DE),
	emojiRef("laptop", "objects", false, 0x1F4BB),
	emojiRef("light bulb", "objects", false, 0x1F4A1),
	emojiRef("calendar", "objects", false, 0x1F4C5),
	emojiRef("pushpin", "objects", false, 0x1F4CC),
	emojiRef("locked", "objects", false, 0x1F512),
	emojiRef("key", "objects", false, 0x1F511),

	emojiRef("warning", "symbols", false, 0x26A0, 0xFE0F),
	emojiRef("check mark button", "symbols", false, 0x2705),
	emojiRef("cross mark", "symbols", false, 0x274C),
	emojiRef("keycap: #", "symbols", false, 0x0023, 0xFE0F, 0x20E3),
	emojiRef("keycap: 1", "symbols", false, 0x0031, 0xFE0F, 0x20E3),

	// Country flags follow the chequered and white flags, in the alphabetical
	// order of their English names: France before Germany.
	emojiRef("chequered flag", "flags", false, 0x1F3C1),
	emojiRef("white flag", "flags", false, 0x1F3F3, 0xFE0F),
	emojiRef("flag: France", "flags", false, 0x1F1EB, 0x1F1F7),
	emojiRef("flag: Germany", "flags", false, 0x1F1E9, 0x1F1EA),
	emojiRef("flag: Japan", "flags", false, 0x1F1EF, 0x1F1F5),
	emojiRef("flag: United States", "flags", false, 0x1F1FA, 0x1F1F8),
}

// emojiCompleteFloor is the number of emoji (skin-tone and gender variants not
// counted) below which the shipped data is certainly not Unicode's set.
const emojiCompleteFloor = 1500

type emojiShipped struct {
	set   *emojiset.Set
	names map[string]*emojiset.Lang
}

func emojiShippedData(t testing.TB) emojiShipped {
	t.Helper()
	read := func(name string) []byte {
		data, err := os.ReadFile(emojiAssetPathFor(name))
		if err != nil {
			t.Fatalf("the emoji data is not in the workspace assets: %v", err)
		}
		return data
	}
	set, err := emojiset.ParseOrder(read("emoji-order.json"))
	if err != nil {
		t.Fatal(err)
	}
	shipped := emojiShipped{set: set, names: map[string]*emojiset.Lang{}}
	for _, code := range []string{"en", "de", "ar"} {
		lang, err := emojiset.ParseLang(read("emoji-"+code+".json"), set)
		if err != nil {
			t.Fatal(err)
		}
		shipped.names[code] = lang
	}
	return shipped
}

func emojiHasArabicLetter(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Arabic, r) {
			return true
		}
	}
	return false
}

func emojiHasLatinLetter(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Latin, r) {
			return true
		}
	}
	return false
}

// Every emoji that ships has a name and search words in English, German and
// Arabic, every flag is named in all three, and the skin tones are named.
func TestTodo_CHATEMOJI_001_EveryShippedEmojiIsNamedInEveryLanguage(t *testing.T) {
	data := emojiShippedData(t)
	for _, code := range []string{"en", "de", "ar"} {
		lang := data.names[code]
		for i, entry := range data.set.Entries {
			if strings.TrimSpace(lang.Names[i]) == "" {
				t.Errorf("%s: %s has no name", code, entry.Glyph)
			}
			if strings.TrimSpace(lang.Keywords[i]) == "" {
				t.Errorf("%s: %s (%s) has no search words", code, entry.Glyph, data.names["en"].Names[i])
			}
		}
		for tone, name := range lang.Tones {
			if strings.TrimSpace(name) == "" {
				t.Errorf("%s: skin tone %d has no name", code, tone+1)
			}
		}
	}
	// A reader's name is the reader's own: Arabic names are Arabic words, and a
	// flag is named for its country in German and in Arabic, not left in English.
	for i, entry := range data.set.Entries {
		if name := data.names["ar"].Names[i]; !emojiHasArabicLetter(name) || emojiHasLatinLetter(name) {
			t.Errorf("ar: %s is named %q, which is not an Arabic name", entry.Glyph, name)
		}
		english := data.names["en"].Names[i]
		if country, isFlag := strings.CutPrefix(english, "flag: "); isFlag {
			if got := data.names["de"].Names[i]; !strings.HasPrefix(got, "Flagge: ") || got == english {
				t.Errorf("de: %s is named %q, want a German name for %s", entry.Glyph, got, country)
			}
			if got := data.names["ar"].Names[i]; !strings.HasPrefix(got, "علم: ") {
				t.Errorf("ar: %s is named %q, want an Arabic name for %s", entry.Glyph, got, country)
			}
		}
	}
	// A search in each language finds the flag by its country's name in that language.
	for _, tc := range []struct{ code, query, glyph string }{
		{"en", "france", "\U0001F1EB\U0001F1F7"}, {"de", "frankreich", "\U0001F1EB\U0001F1F7"}, {"ar", "فرنسا", "\U0001F1EB\U0001F1F7"},
		{"en", "united states", "\U0001F1FA\U0001F1F8"}, {"de", "vereinigte staaten", "\U0001F1FA\U0001F1F8"}, {"ar", "الولايات المتحدة", "\U0001F1FA\U0001F1F8"},
		{"de", "japan", "\U0001F1EF\U0001F1F5"}, {"ar", "اليابان", "\U0001F1EF\U0001F1F5"},
	} {
		ix := emojiTestIndex(t, tc.code)
		found := ix.Search(tc.query, nil)
		if len(found) == 0 || ix.Set.Entries[found[0]].Glyph != tc.glyph {
			t.Errorf("%s: searching %q does not lead with the flag %s", tc.code, tc.query, tc.glyph)
		}
	}
}

// Every shipped emoji sits in the Unicode group the reference gives it, in
// Unicode's order, with its English name as Unicode spells it and its skin-tone
// variants folded under it.
func TestTodo_CHATEMOJI_001_EveryShippedEmojiIsInItsUnicodeGroup(t *testing.T) {
	data := emojiShippedData(t)
	position := map[string]int{}
	for i, ref := range emojiReferenceList {
		if _, twice := position[ref.glyph]; twice {
			t.Fatalf("the reference lists %q twice", ref.english)
		}
		position[ref.glyph] = i
	}
	reference := map[string]emojiReference{}
	for _, ref := range emojiReferenceList {
		reference[ref.glyph] = ref
	}
	complete := len(data.set.Entries) >= emojiCompleteFloor

	lastInGroup := map[string]int{}
	for i, entry := range data.set.Entries {
		ref, known := reference[entry.Glyph]
		if !known {
			if !complete {
				t.Errorf("%s (%s) is shipped but the reference does not list it: add it, with its Unicode group", entry.Glyph, data.names["en"].Names[i])
			}
			continue
		}
		group := data.set.Groups[entry.Group]
		if group.ID != ref.group {
			t.Errorf("%s (%s) is in %q, Unicode puts it in %q", entry.Glyph, ref.english, group.ID, ref.group)
		}
		if got := data.names["en"].Names[i]; got != ref.english {
			t.Errorf("%s is named %q, Unicode names it %q", entry.Glyph, got, ref.english)
		}
		if earlier, seen := lastInGroup[group.ID]; seen && position[entry.Glyph] < earlier {
			t.Errorf("%s (%s) comes after emoji Unicode lists later than it in %s", entry.Glyph, ref.english, group.Name)
		}
		lastInGroup[group.ID] = position[entry.Glyph]
		if ref.tones {
			if len(entry.Variants) != emojiset.Tones {
				t.Errorf("%s (%s) has %d skin-tone variants, want %d", entry.Glyph, ref.english, len(entry.Variants), emojiset.Tones)
			}
			for tone := 1; tone <= emojiset.Tones; tone++ {
				if entry.For(tone) == entry.Glyph {
					t.Errorf("%s (%s) has no variant in skin tone %d", entry.Glyph, ref.english, tone)
				}
			}
		} else if len(entry.Variants) != 0 {
			t.Errorf("%s (%s) has variants, which the reference does not give it", entry.Glyph, ref.english)
		}
	}
	// Nothing the reference lists has gone missing.
	if !complete {
		for _, ref := range emojiReferenceList {
			found := false
			for _, entry := range data.set.Entries {
				if entry.Glyph == ref.glyph {
					found = true
				}
			}
			if !found {
				t.Errorf("%s (%s) is in the reference but not in the shipped data", ref.glyph, ref.english)
			}
		}
	}
}

// emojiTestLine is one emoji line of Unicode's emoji-test.txt: the code points,
// the status, the emoji drawn, the Unicode version and the name.
var emojiTestLine = regexp.MustCompile(`^([0-9A-F ]+?)\s*;\s*([a-z-]+)\s*#\s*(\S+)\s+E[0-9.]+\s+(.+)$`)

// TestTodo_CHATEMOJI_001_CompleteSet proves the full Unicode set is shipped. It
// is skipped, with the reason, until the owner has approved fetching Unicode's
// files and tools/emojidata has been run on them; from then on it fails if one
// emoji of Unicode's list is missing, in another group or out of order, or lacks
// a name or search words in any product language.
func TestTodo_CHATEMOJI_001_CompleteSet(t *testing.T) {
	data := emojiShippedData(t)
	if n := len(data.set.Entries); n < emojiCompleteFloor {
		t.Skipf("the shipped emoji data holds %d emoji, not Unicode's set (about 1,900 without skin-tone variants). "+
			"To complete it the owner must approve fetching, from unicode.org, Unicode Emoji 17.0 emoji-test.txt (about 0.6 MB) and "+
			"the CLDR annotation files annotations/{en,de,ar}.xml and annotationsDerived/{en,de,ar}.xml (six XML files, a few MB in all) "+
			"into .artifacts/unicode, then run: go run ./tools/emojidata -in .artifacts/unicode -out internal/humanwork/workspace/assets -cldr <release>. "+
			"This test then proves the set complete", n)
	}
	if len(data.set.Groups) != 9 {
		t.Fatalf("%d groups, want Unicode's nine", len(data.set.Groups))
	}
	for _, code := range []string{"en", "de", "ar"} {
		lang := data.names[code]
		missingNames, missingWords := 0, 0
		for i := range data.set.Entries {
			if strings.TrimSpace(lang.Names[i]) == "" {
				missingNames++
			}
			if strings.TrimSpace(lang.Keywords[i]) == "" {
				missingWords++
			}
		}
		if missingNames != 0 || missingWords != 0 {
			t.Errorf("%s: %d emoji without a name and %d without search words", code, missingNames, missingWords)
		}
	}
	source := filepath.Join("..", "..", "..", ".artifacts", "unicode", "emoji-test.txt")
	body, err := os.ReadFile(source)
	if err != nil {
		t.Skipf("%s is not in place, so the shipped list cannot be compared with Unicode's: %v", source, err)
	}
	ix := emojiTestIndex(t, "en")
	group, last := "", -1
	listed := 0
	for _, line := range strings.Split(string(body), "\n") {
		if name, ok := strings.CutPrefix(line, "# group: "); ok {
			group = strings.TrimSpace(name)
			continue
		}
		match := emojiTestLine.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if match == nil || match[2] != "fully-qualified" {
			continue
		}
		listed++
		glyph := match[3]
		i, ok := ix.Lookup(glyph)
		if !ok {
			t.Errorf("%s (%s) is in Unicode's list and not in the shipped data", glyph, match[4])
			continue
		}
		entry := ix.Set.Entries[i]
		if got := ix.Set.Groups[entry.Group].Name; got != group {
			t.Errorf("%s (%s) is in %q, Unicode puts it in %q", glyph, match[4], got, group)
		}
		if entry.Glyph == glyph { // a base emoji, not a variant folded under one
			if i < last {
				t.Errorf("%s (%s) is out of Unicode's order", glyph, match[4])
			}
			last = i
		}
	}
	if listed < emojiCompleteFloor {
		t.Errorf("only %d emoji read from %s", listed, source)
	}
	if shipped := len(data.set.Entries); shipped > listed {
		t.Errorf("%d emoji shipped, Unicode's list has %d", shipped, listed)
	}
}
