package emojigen

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/emojiset"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

const fixtureDir = "../testdata/interim"

func fixtureSource(t *testing.T) *Source {
	t.Helper()
	src, err := ReadDir(fixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	src.AllowPartial = true
	return src
}

func loadSet(t *testing.T, out *Output, reader string) (*emojiset.Set, *emojiset.Index) {
	t.Helper()
	set, err := emojiset.ParseOrder(out.Order)
	if err != nil {
		t.Fatal(err)
	}
	en, err := emojiset.ParseLang(out.Langs["en"], set)
	if err != nil {
		t.Fatal(err)
	}
	lang, err := emojiset.ParseLang(out.Langs[reader], set)
	if err != nil {
		t.Fatal(err)
	}
	return set, emojiset.NewIndex(set, lang, en)
}

func find(set *emojiset.Set, glyph string) (int, emojiset.Entry) {
	for i, e := range set.Entries {
		if e.Glyph == glyph {
			return i, e
		}
	}
	return -1, emojiset.Entry{}
}

// The generator reads Unicode's own file formats (here small hand-made files in
// those formats), groups and orders the emoji as Unicode does, attaches the
// skin-tone variants to their base, and writes data the picker can search in
// every language.
func TestTodo_CHATEMOJI_001(t *testing.T) {
	out, err := Build(fixtureSource(t))
	if err != nil {
		t.Fatal(err)
	}
	set, ix := loadSet(t, out, "de")

	if set.Meta.Emoji != "17.0" || set.Meta.Format != emojiset.FormatVersion {
		t.Fatalf("meta = %+v", set.Meta)
	}
	if len(set.Groups) != 9 {
		t.Fatalf("%d groups, want Unicode's nine", len(set.Groups))
	}
	var order []string
	for _, g := range set.Groups {
		order = append(order, g.Name)
		if g.Count == 0 {
			t.Errorf("group %q is empty", g.Name)
		}
	}
	if got, want := strings.Join(order, ","), strings.Join(ExpectedGroups, ","); got != want {
		t.Fatalf("groups in order %q, want %q (Component must not be a group)", got, want)
	}
	if len(set.Entries) != 58 || out.Summary.Variants != 30 {
		t.Fatalf("%d emoji and %d variants, want 58 and 30", len(set.Entries), out.Summary.Variants)
	}
	// Each entry sits inside its group's range, in group order.
	for gi, g := range set.Groups {
		for i := g.First; i < g.First+g.Count; i++ {
			if set.Entries[i].Group != gi {
				t.Fatalf("entry %d (%s) is in group %d but sits in group %d's range", i, set.Entries[i].Glyph, set.Entries[i].Group, gi)
			}
		}
	}
	// Skin tones are folded under their base, never listed as emoji of their own.
	_, thumbs := find(set, "👍")
	if len(thumbs.Variants) != 5 || thumbs.For(3) != "👍🏽" {
		t.Fatalf("thumbs up variants = %v", thumbs.Variants)
	}
	if i, _ := find(set, "👍🏽"); i >= 0 {
		t.Fatal("a skin-tone variant is listed as its own emoji")
	}
	// A ZWJ sequence, a flag, a keycap and a variation-selector emoji are all present once.
	for _, glyph := range []string{"👨‍👩‍👧", "🧑‍💻", "🇩🇪", "#️⃣", "1️⃣", "❤️", "✈️", "🏳️"} {
		count := 0
		for _, e := range set.Entries {
			if e.Glyph == glyph {
				count++
			}
		}
		if count != 1 {
			t.Errorf("%q is present %d times", glyph, count)
		}
	}
	// Names that are not fully-qualified never become emoji.
	for _, e := range set.Entries {
		if e.Glyph == "❤" || e.Glyph == "✈" || e.Glyph == "#⃣" {
			t.Errorf("an unqualified form %q was listed", e.Glyph)
		}
	}
	// The technologist has tones too (a ZWJ sequence with the tone after the person).
	if _, tech := find(set, "🧑‍💻"); len(tech.Variants) != 5 || tech.For(1) != "🧑🏻‍💻" {
		t.Fatalf("technologist variants = %v", tech.Variants)
	}

	// Names and keywords in every language, found through the CLDR files and, for
	// skin-tone and ZWJ sequences, the derived files.
	_, enIndex := loadSet(t, out, "en")
	_, arIndex := loadSet(t, out, "ar")
	for _, tc := range []struct {
		ix        *emojiset.Index
		query     string
		wantGlyph string
	}{
		{ix, "Feuer", "🔥"}, {ix, "feuer", "🔥"}, {ix, "fire", "🔥"}, {ix, ":fire:", "🔥"}, {ix, "Flamme", "🔥"},
		{ix, "Freudentränen", "😂"}, {ix, "Daumen hoch", "👍"}, {ix, ":thumbs_up:", "👍"}, {ix, "thumb", "👍"},
		{ix, "Deutschland", "🇩🇪"}, {ix, "flag germany", "🇩🇪"}, {ix, "Tastenkappe", "#️⃣"},
		{ix, "party", "🎉"}, {ix, "family", "👨‍👩‍👧"}, {ix, "technologist", "🧑‍💻"},
		{enIndex, "party", "🎉"}, {enIndex, "pray", "🙏"}, {enIndex, "coffee", "☕"},
		{arIndex, "نار", "🔥"}, {arIndex, "منزل", "🏠"}, {arIndex, "مفتاح", "🔑"}, {arIndex, "fire", "🔥"},
	} {
		results := tc.ix.Search(tc.query, nil)
		if len(results) == 0 || set.Entries[results[0]].Glyph != tc.wantGlyph {
			var got []string
			for _, r := range results {
				got = append(got, set.Entries[r].Glyph)
			}
			t.Errorf("Search(%q) = %v, want %s first", tc.query, got, tc.wantGlyph)
		}
	}
	fire, _ := find(set, "🔥")
	if ix.Name(fire) != "Feuer" || enIndex.Name(fire) != "fire" || arIndex.Name(fire) != "نار" || ix.Shortcode(fire) != ":fire:" {
		t.Fatalf("names of fire: %q %q %q %q", ix.Name(fire), enIndex.Name(fire), arIndex.Name(fire), ix.Shortcode(fire))
	}
	// An emoji with no German name is named in English rather than left blank.
	party, _ := find(set, "🎉")
	if ix.Name(party) != "party popper" {
		t.Fatalf("a missing German name did not fall back to English: %q", ix.Name(party))
	}
	if ix.ToneName(1) != "helle Hautfarbe" || enIndex.ToneName(5) != "dark skin tone" {
		t.Fatalf("tone names %q %q", ix.ToneName(1), enIndex.ToneName(5))
	}

	// The data carries its notice and versions.
	notice := string(out.Notice)
	for _, want := range []string{"UNICODE LICENSE V3", "Permission is hereby granted", "version 17.0", "emoji-test.txt", "annotations/de.xml", "© 2025 Unicode", "Copyright © 1991-2025 Unicode, Inc."} {
		if !strings.Contains(notice, want) {
			t.Errorf("licence notice is missing %q", want)
		}
	}
	var recordedOrder struct {
		Licence string            `json:"licence"`
		Sources map[string]string `json:"sources"`
	}
	if err := json.Unmarshal(out.Order, &recordedOrder); err != nil || recordedOrder.Licence == "" || len(recordedOrder.Sources) != 7 {
		t.Fatalf("order file does not record its licence and sources: %+v %v", recordedOrder, err)
	}

	// The same input always produces the same bytes.
	again, err := Build(fixtureSource(t))
	if err != nil {
		t.Fatal(err)
	}
	files, _ := out.Files()
	againFiles, _ := again.Files()
	for name, data := range files {
		if !bytes.Equal(data, againFiles[name]) {
			t.Errorf("%s differs between two runs", name)
		}
	}
	// Every JSON file travels with a gzip form that decompresses to it.
	for _, name := range []string{OrderFile, LangFile("en"), LangFile("de"), LangFile("ar")} {
		reader, err := gzip.NewReader(bytes.NewReader(files[name+".gz"]))
		if err != nil {
			t.Fatalf("%s.gz: %v", name, err)
		}
		plain, _ := io.ReadAll(reader)
		if !bytes.Equal(plain, files[name]) {
			t.Errorf("%s.gz does not decompress to %s", name, name)
		}
	}
}

func TestStrictRunRefusesIncompleteSources(t *testing.T) {
	src, err := ReadDir(fixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Build(src); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("a source folder with most German names missing was accepted: %v", err)
	}
	// An English-only folder cannot pass for the full list either.
	src.AllowPartial = false
	src.EmojiTest = []byte("# Version: 17.0\n# group: Smileys & Emotion\n1F600 ; fully-qualified # 😀 E1.0 grinning face\n")
	if _, err := Build(src); err == nil || !strings.Contains(err.Error(), "not Unicode's emoji list") {
		t.Fatalf("a list with one group was accepted: %v", err)
	}
}

func TestParseEmojiTestRejectsDamage(t *testing.T) {
	const good = "# Version: 17.0\n# group: Smileys & Emotion\n"
	for name, tc := range map[string]struct{ data, want string }{
		"no version":      {"# group: Smileys & Emotion\n1F600 ; fully-qualified # 😀 E1.0 grinning face\n", "Version"},
		"empty":           {"# Version: 17.0\n", "no fully-qualified"},
		"garbled line":    {good + "this is not an emoji line\n", "not an emoji line"},
		"wrong glyph":     {good + "1F600 ; fully-qualified # 😂 E1.0 grinning face\n", "do not make the emoji"},
		"bad code":        {good + "ZZZZ ; fully-qualified # 😀 E1.0 grinning face\n", "not an emoji line"},
		"before group":    {"# Version: 17.0\n1F600 ; fully-qualified # 😀 E1.0 grinning face\n", "before any group"},
		"zero code point": {good + "0000 ; fully-qualified # \x00 E1.0 nothing\n", "bad code point"},
	} {
		if _, err := parseEmojiTest([]byte(tc.data)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to mention %q", name, err, tc.want)
		}
	}
	parsed, err := parseEmojiTest([]byte(good + "# subgroup: face-smiling\n1F600  ; fully-qualified     # 😀 E1.0 grinning face\n1F600 ; minimally-qualified # 😀 E1.0 grinning face\n"))
	if err != nil || len(parsed.entries) != 1 || parsed.entries[0].name != "grinning face" {
		t.Fatalf("parsed = %+v, %v", parsed, err)
	}
}

func miniSource(emojiTest string) *Source {
	empty := []byte(`<ldml><annotations></annotations></ldml>`)
	src := &Source{EmojiTest: []byte(emojiTest), Annotations: map[string][]byte{}, Derived: map[string][]byte{}, AllowPartial: true}
	for _, lang := range Languages {
		src.Annotations[lang], src.Derived[lang] = empty, empty
	}
	return src
}

func TestVariantNeedsItsBaseAndDuplicatesAreRefused(t *testing.T) {
	const head = "# Version: 17.0\n# group: People & Body\n"
	if _, err := Build(miniSource(head + "1F44D 1F3FB ; fully-qualified # 👍🏻 E1.0 thumbs up: light skin tone\n")); err == nil || !strings.Contains(err.Error(), "without its base") {
		t.Fatalf("variant without a base: %v", err)
	}
	dup := head + "1F44D ; fully-qualified # 👍 E0.6 thumbs up\n1F44D ; fully-qualified # 👍 E0.6 thumbs up\n"
	if _, err := Build(miniSource(dup)); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("duplicate emoji: %v", err)
	}
}

// CLDR files sometimes omit the variation selector Unicode's list carries, and
// name an emoji by its text form. Both spellings must find the name.
func TestAnnotationsMatchWithOrWithoutVariationSelector(t *testing.T) {
	src := miniSource("# Version: 17.0\n# group: Smileys & Emotion\n2764 FE0F ; fully-qualified # ❤️ E0.6 red heart\n")
	src.Annotations["en"] = []byte("<ldml><annotations><annotation cp=\"❤\">heart | love</annotation><annotation cp=\"❤\" type=\"tts\">red heart (CLDR)</annotation></annotations></ldml>")
	src.Annotations["de"] = []byte("<ldml><annotations><annotation cp=\"❤️\" type=\"tts\">rotes Herz</annotation></annotations></ldml>")
	out, err := Build(src)
	if err != nil {
		t.Fatal(err)
	}
	set, ix := loadSet(t, out, "de")
	if ix.Name(0) != "rotes Herz" {
		t.Fatalf("name = %q", ix.Name(0))
	}
	if got := ix.Search("love", nil); len(got) != 1 || set.Entries[got[0]].Glyph != "❤️" {
		t.Fatalf("keyword from the text-form annotation not found: %v", got)
	}
}

func TestMissingInputIsNamed(t *testing.T) {
	dir := t.TempDir()
	if _, err := ReadDir(dir); err == nil || !strings.Contains(err.Error(), "emoji-test.txt") || !strings.Contains(err.Error(), dir) {
		t.Fatalf("an empty folder was accepted or the error does not name the file and folder: %v", err)
	}
}

// The tool never opens a connection: it imports no networking package.
func TestGeneratorNeverFetches(t *testing.T) {
	for _, dir := range []string{".", ".."} {
		matches, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, file := range matches {
			parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, spec := range parsed.Imports {
				path := strings.Trim(spec.Path.Value, `"`)
				if path == "net" || strings.HasPrefix(path, "net/") || strings.Contains(path, "x/net") {
					t.Errorf("%s imports %s", file, path)
				}
			}
		}
	}
}

// Writing next to the workspace asset manifest keeps the manifest in step, so a
// build that embeds the new files still starts.
func TestWriteRefreshesTheAssetManifest(t *testing.T) {
	out, err := Build(fixtureSource(t))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	logo := []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)
	if err := os.WriteFile(filepath.Join(dir, "harborcare-logo.svg"), logo, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, workspace.AssetIntegrityManifestName), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(dir, out); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, workspace.AssetIntegrityManifestName))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := workspace.ParseAssetIntegrityManifest(body)
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string][]string{}
	for _, a := range manifest.Assets {
		for _, r := range a.Representations {
			paths[a.Path] = append(paths[a.Path], r.Encoding)
		}
	}
	for _, name := range []string{OrderFile, LangFile("en"), LangFile("de"), LangFile("ar")} {
		got := paths[workspace.PathAssetPrefix+name]
		if len(got) != 2 || got[0] != "identity" || got[1] != "gzip" {
			t.Errorf("%s is catalogued as %v, want identity and gzip", name, got)
		}
	}
	if got := paths[workspace.PathAssetPrefix+NoticeFile]; len(got) != 1 {
		t.Errorf("licence notice is catalogued as %v", got)
	}
	// Re-running leaves identical bytes (no timestamps, no ordering noise).
	if _, err := Write(dir, out); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(filepath.Join(dir, workspace.AssetIntegrityManifestName))
	if !bytes.Equal(body, again) {
		t.Fatal("the manifest changed between two identical runs")
	}
}

// The data checked in under the workspace assets is the generator's output for
// the hand-made fixture until Unicode's own files are placed and the generator is
// run on them; then this test steps aside, because the data is no longer the
// fixture's.
func TestCheckedInDataMatchesItsSource(t *testing.T) {
	assets := filepath.Join("..", "..", "..", "internal", "humanwork", "workspace", "assets")
	checked, err := os.ReadFile(filepath.Join(assets, OrderFile))
	if err != nil {
		t.Fatalf("the generated emoji data is not checked in: %v", err)
	}
	var recorded struct {
		Sources map[string]string `json:"sources"`
	}
	if err := json.Unmarshal(checked, &recorded); err != nil {
		t.Fatal(err)
	}
	src := fixtureSource(t)
	sum := sha256.Sum256(src.EmojiTest)
	if recorded.Sources["emoji-test.txt"] != hex.EncodeToString(sum[:]) {
		t.Skip("the checked-in data was generated from Unicode's own files, not from the interim fixture")
	}
	out, err := Build(src)
	if err != nil {
		t.Fatal(err)
	}
	files, err := out.Files()
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(assets, name))
		if err != nil {
			t.Errorf("%s is not checked in: %v", name, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s is out of date; run the generator again", name)
		}
	}
}
