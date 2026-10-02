// Package emojigen turns Unicode's emoji-test.txt and the CLDR annotation files
// into the compact data files the chat emoji picker loads: one order file shared
// by every language and one file of names and keywords per language.
//
// It reads a local folder and writes files; it never opens a network connection.
// The data files are checked in, so the build and the running product fetch
// nothing from Unicode.
package emojigen

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/emojiset"
)

// Languages are the product's languages, in the order their files are written.
var Languages = []string{"en", "de", "ar"}

// Generator names this tool in the data files.
const Generator = "tools/emojidata 1"

// ExpectedGroups are Unicode's nine emoji groups. The "Component" group (skin
// tone and hair swatches) is not offered as emoji, so it is not one of them.
var ExpectedGroups = []string{"Smileys & Emotion", "People & Body", "Animals & Nature", "Food & Drink", "Travel & Places", "Activities", "Objects", "Symbols", "Flags"}

// Source is the content of the input folder.
type Source struct {
	EmojiTest    []byte
	Annotations  map[string][]byte // language -> annotations/<lang>.xml
	Derived      map[string][]byte // language -> annotationsDerived/<lang>.xml
	CLDRVersion  string            // optional override, e.g. "48"
	AllowPartial bool              // accept names missing in de and ar
}

// Output is the generated content, ready to write.
type Output struct {
	Order   []byte
	Langs   map[string][]byte
	Notice  []byte
	Summary Summary
}

// Summary describes what was generated, for the person running the tool.
type Summary struct {
	EmojiVersion string
	CLDR         string
	Groups       []GroupCount
	Entries      int
	Variants     int
	Coverage     map[string]Coverage
}

// GroupCount is the number of emoji in one group.
type GroupCount struct {
	Name  string
	Count int
}

// Coverage counts how many emoji have a name and keywords in one language.
type Coverage struct{ Names, Keywords, Total int }

// SourceFile is one input with its digest, recorded beside the data.
type sourceFile struct {
	name string
	data []byte
}

func (s *Source) files() []sourceFile {
	files := []sourceFile{{"emoji-test.txt", s.EmojiTest}}
	for _, lang := range Languages {
		files = append(files, sourceFile{"annotations/" + lang + ".xml", s.Annotations[lang]}, sourceFile{"annotationsDerived/" + lang + ".xml", s.Derived[lang]})
	}
	return files
}

type testEntry struct {
	glyph, name, since string
	group              int
}

var (
	testLine = regexp.MustCompile(`^([0-9A-Fa-f]+(?: [0-9A-Fa-f]+)*)\s*;\s*([a-z-]+)\s*#\s*(\S+)\s+E(\d+(?:\.\d+)?)\s+(.+?)\s*$`)
	// "<!-- ... -->" blocks in CLDR files carry the copyright line.
	cldrCopyright = regexp.MustCompile(`Copyright[^\n]*`)
)

type parsedTest struct {
	version, date string
	groups        []string
	entries       []testEntry // fully-qualified emoji, including skin-tone variants
	tones         map[string]bool
	copyright     []string
}

func parseEmojiTest(data []byte) (*parsedTest, error) {
	parsed := &parsedTest{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	group := -1
	skipGroup := false
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimRight(scanner.Text(), "\r")
		if text == "" {
			continue
		}
		if strings.HasPrefix(text, "#") {
			body := strings.TrimSpace(strings.TrimPrefix(text, "#"))
			switch {
			case strings.HasPrefix(body, "group:"):
				name := strings.TrimSpace(strings.TrimPrefix(body, "group:"))
				if name == "Component" {
					skipGroup = true
					continue
				}
				skipGroup = false
				parsed.groups = append(parsed.groups, name)
				group = len(parsed.groups) - 1
			case strings.HasPrefix(body, "Version:") && parsed.version == "":
				parsed.version = strings.TrimSpace(strings.TrimPrefix(body, "Version:"))
			case strings.HasPrefix(body, "Date:") && parsed.date == "":
				parsed.date = strings.TrimSpace(strings.TrimPrefix(body, "Date:"))
			case strings.Contains(body, "©") && len(parsed.copyright) < 3 && group < 0:
				parsed.copyright = append(parsed.copyright, body)
			case strings.HasPrefix(body, "For terms of use and license") && len(parsed.copyright) < 4 && group < 0:
				parsed.copyright = append(parsed.copyright, body)
			}
			continue
		}
		match := testLine.FindStringSubmatch(text)
		if match == nil {
			return nil, fmt.Errorf("emoji-test.txt line %d is not an emoji line: %q", line, text)
		}
		glyph, err := glyphOf(match[1])
		if err != nil {
			return nil, fmt.Errorf("emoji-test.txt line %d: %w", line, err)
		}
		if glyph != match[3] {
			return nil, fmt.Errorf("emoji-test.txt line %d: the code points %q do not make the emoji %q the line shows", line, match[1], match[3])
		}
		if match[2] == "component" {
			if r := []rune(glyph); len(r) == 1 && r[0] >= 0x1F3FB && r[0] <= 0x1F3FF {
				if parsed.tones == nil {
					parsed.tones = map[string]bool{}
				}
				parsed.tones[glyph] = true
			}
			continue
		}
		if match[2] != "fully-qualified" || skipGroup {
			continue
		}
		if group < 0 {
			return nil, fmt.Errorf("emoji-test.txt line %d: an emoji before any group", line)
		}
		parsed.entries = append(parsed.entries, testEntry{glyph: glyph, name: match[5], since: match[4], group: group})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if parsed.version == "" {
		return nil, errors.New("emoji-test.txt has no \"# Version:\" line")
	}
	if len(parsed.entries) == 0 {
		return nil, errors.New("emoji-test.txt holds no fully-qualified emoji")
	}
	return parsed, nil
}

func glyphOf(codes string) (string, error) {
	var b strings.Builder
	for _, field := range strings.Fields(codes) {
		var r rune
		if _, err := fmt.Sscanf(field, "%X", &r); err != nil || r <= 0 || r > unicode.MaxRune {
			return "", fmt.Errorf("bad code point %q", field)
		}
		b.WriteRune(r)
	}
	return b.String(), nil
}

// stripTones removes skin-tone modifiers.
func stripTones(glyph string) string {
	if !strings.ContainsAny(glyph, "\U0001F3FB\U0001F3FC\U0001F3FD\U0001F3FE\U0001F3FF") {
		return glyph
	}
	var b strings.Builder
	for _, r := range glyph {
		if r < 0x1F3FB || r > 0x1F3FF {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func stripSelectors(glyph string) string { return strings.ReplaceAll(glyph, "️", "") }

type cldrFile struct {
	Identity struct {
		Version struct {
			Number string `xml:"number,attr"`
		} `xml:"version"`
	} `xml:"identity"`
	Annotations []struct {
		CP   string `xml:"cp,attr"`
		Type string `xml:"type,attr"`
		Text string `xml:",chardata"`
	} `xml:"annotations>annotation"`
}

type annotation struct {
	name     string
	keywords []string
}

func parseCLDR(data []byte, into map[string]*annotation) (string, error) {
	var file cldrFile
	if err := xml.Unmarshal(data, &file); err != nil {
		return "", err
	}
	for _, a := range file.Annotations {
		key := stripSelectors(a.CP)
		if key == "" {
			continue
		}
		entry := into[key]
		if entry == nil {
			entry = &annotation{}
			into[key] = entry
		}
		text := strings.TrimSpace(a.Text)
		if a.Type == "tts" {
			if entry.name == "" {
				entry.name = text
			}
			continue
		}
		if len(entry.keywords) == 0 {
			for _, word := range strings.Split(text, "|") {
				if word = strings.TrimSpace(word); word != "" {
					entry.keywords = append(entry.keywords, word)
				}
			}
		}
	}
	// Released CLDR files carry the unexpanded keyword "$Revision$", which names
	// nothing; only a real revision number is worth recording.
	revision := strings.TrimSpace(strings.TrimPrefix(strings.Trim(strings.TrimSpace(file.Identity.Version.Number), "$ "), "Revision:"))
	if revision == "Revision" {
		revision = ""
	}
	return revision, nil
}

// Build generates the data files.
func Build(src *Source) (*Output, error) {
	parsed, err := parseEmojiTest(src.EmojiTest)
	if err != nil {
		return nil, err
	}
	if !src.AllowPartial {
		for _, want := range ExpectedGroups {
			found := false
			for _, got := range parsed.groups {
				found = found || got == want
			}
			if !found {
				return nil, fmt.Errorf("emoji-test.txt has no %q group; this is not Unicode's emoji list", want)
			}
		}
	}

	set := &emojiset.Set{Meta: emojiset.Meta{Format: emojiset.FormatVersion, Emoji: parsed.version, Generator: Generator}}
	groupOf := make([]int, len(parsed.groups))
	for i, name := range parsed.groups {
		groupOf[i] = len(set.Groups)
		set.Groups = append(set.Groups, emojiset.Group{ID: slug(name), Name: name})
	}
	// Bases first in Unicode order, then every variant is attached to its base.
	type pending struct {
		entry emojiset.Entry
		name  string
	}
	var bases []pending
	baseAt := map[string]int{}
	var variants []testEntry
	for _, e := range parsed.entries {
		if stripTones(e.glyph) != e.glyph {
			variants = append(variants, e)
			continue
		}
		if _, dup := baseAt[e.glyph]; dup {
			return nil, fmt.Errorf("emoji-test.txt lists %q twice", e.glyph)
		}
		baseAt[e.glyph] = len(bases)
		bases = append(bases, pending{entry: emojiset.Entry{Glyph: e.glyph, Group: groupOf[e.group]}, name: e.name})
	}
	for _, v := range variants {
		i, ok := baseAt[stripTones(v.glyph)]
		if !ok {
			return nil, fmt.Errorf("emoji-test.txt lists the skin-tone variant %q before or without its base emoji", v.glyph)
		}
		bases[i].entry.Variants = append(bases[i].entry.Variants, v.glyph)
	}
	// Entries must be contiguous by group, in Unicode's order.
	lastGroup := -1
	for _, b := range bases {
		if b.entry.Group < lastGroup {
			return nil, errors.New("emoji-test.txt lists a group's emoji out of order")
		}
		lastGroup = b.entry.Group
		set.Entries = append(set.Entries, b.entry)
		set.Groups[b.entry.Group].Count++
	}
	first := 0
	for i := range set.Groups {
		set.Groups[i].First = first
		first += set.Groups[i].Count
	}
	// Drop groups with no emoji (a group listed only with non-fully-qualified lines).
	kept := set.Groups[:0]
	remap := map[int]int{}
	for i, g := range set.Groups {
		if g.Count > 0 {
			remap[i] = len(kept)
			kept = append(kept, g)
		}
	}
	set.Groups = kept
	for i := range set.Entries {
		set.Entries[i].Group = remap[set.Entries[i].Group]
	}
	set.Meta.Entries = len(set.Entries)

	out := &Output{Langs: map[string][]byte{}, Summary: Summary{EmojiVersion: parsed.version, Coverage: map[string]Coverage{}, Entries: len(set.Entries)}}
	revisions := map[string]string{}
	for _, lang := range Languages {
		table := map[string]*annotation{}
		if len(src.Annotations[lang]) == 0 {
			return nil, fmt.Errorf("annotations/%s.xml is missing", lang)
		}
		rev, err := parseCLDR(src.Annotations[lang], table)
		if err != nil {
			return nil, fmt.Errorf("annotations/%s.xml: %w", lang, err)
		}
		revisions[lang] = rev
		if len(src.Derived[lang]) == 0 {
			return nil, fmt.Errorf("annotationsDerived/%s.xml is missing", lang)
		}
		if _, err := parseCLDR(src.Derived[lang], table); err != nil {
			return nil, fmt.Errorf("annotationsDerived/%s.xml: %w", lang, err)
		}
		l := &emojiset.Lang{Code: lang, Names: make([]string, len(set.Entries)), Keywords: make([]string, len(set.Entries))}
		cov := Coverage{Total: len(set.Entries)}
		for i, entry := range set.Entries {
			if a := table[stripSelectors(entry.Glyph)]; a != nil {
				l.Names[i] = a.name
				l.Keywords[i] = strings.Join(a.keywords, "|")
			}
			if l.Names[i] == "" && lang == "en" {
				l.Names[i] = bases[i].name
			}
			if l.Names[i] != "" {
				cov.Names++
			}
			if l.Keywords[i] != "" {
				cov.Keywords++
			}
		}
		for tone := 1; tone <= emojiset.Tones; tone++ {
			swatch := string(emojiset.ToneModifier(tone))
			if a := table[swatch]; a != nil {
				l.Tones[tone-1] = a.name
			}
		}
		if lang != "en" && !src.AllowPartial && cov.Names*100 < cov.Total*95 {
			return nil, fmt.Errorf("only %d of %d emoji have a %s name; the CLDR annotation files look incomplete (use -allow-partial to accept that)", cov.Names, cov.Total, lang)
		}
		data, err := emojiset.EncodeLang(l)
		if err != nil {
			return nil, err
		}
		out.Langs[lang] = data
		out.Summary.Coverage[lang] = cov
	}
	set.Meta.CLDR = src.CLDRVersion
	if set.Meta.CLDR == "" && revisions["en"] != "" {
		set.Meta.CLDR = "annotations revision " + revisions["en"]
	}
	if set.Meta.CLDR == "" {
		set.Meta.CLDR = "not stated in the source files (see the SHA-256 of each file)"
	}
	out.Summary.CLDR = set.Meta.CLDR
	for _, g := range set.Groups {
		out.Summary.GroupsAppend(g.Name, g.Count)
	}
	for _, e := range set.Entries {
		out.Summary.Variants += len(e.Variants)
	}

	hashes := map[string]string{}
	for _, f := range src.files() {
		sum := sha256.Sum256(f.data)
		hashes[f.name] = hex.EncodeToString(sum[:])
	}
	order, err := emojiset.EncodeOrder(set, "Unicode License v3; see emoji-LICENSE.txt", hashes)
	if err != nil {
		return nil, err
	}
	out.Order = order
	out.Notice = notice(parsed, src, set, revisions, hashes)
	return out, nil
}

// GroupsAppend adds one group's count.
func (s *Summary) GroupsAppend(name string, count int) {
	s.Groups = append(s.Groups, GroupCount{Name: name, Count: count})
}

func slug(name string) string {
	var parts []string
	var word strings.Builder
	flush := func() {
		if word.Len() > 0 {
			parts = append(parts, word.String())
			word.Reset()
		}
	}
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return strings.Join(parts, "-")
}

func notice(parsed *parsedTest, src *Source, set *emojiset.Set, revisions, hashes map[string]string) []byte {
	var b strings.Builder
	b.WriteString("Emoji data for the Chat emoji picker\n")
	b.WriteString("=====================================\n\n")
	b.WriteString("These files (emoji-order.json and emoji-<language>.json) are generated by tools/emojidata\n")
	b.WriteString("from Unicode's emoji data and the Unicode CLDR annotations. Do not edit them by hand:\n")
	b.WriteString("place the source files in .artifacts/unicode/ and run\n\n")
	b.WriteString("    go run ./tools/emojidata -in .artifacts/unicode -out internal/humanwork/workspace/assets\n\n")
	b.WriteString("Data versions\n-------------\n")
	fmt.Fprintf(&b, "Unicode emoji data: version %s (emoji-test.txt", parsed.version)
	if parsed.date != "" {
		fmt.Fprintf(&b, ", dated %s", parsed.date)
	}
	b.WriteString(")\n")
	fmt.Fprintf(&b, "CLDR annotations: %s\n", set.Meta.CLDR)
	for _, lang := range Languages {
		if revisions[lang] != "" {
			fmt.Fprintf(&b, "  annotations/%s.xml: revision %s\n", lang, revisions[lang])
		}
	}
	fmt.Fprintf(&b, "Emoji in the list: %d (%d groups)\n", len(set.Entries), len(set.Groups))
	b.WriteString("\nSource files (SHA-256)\n----------------------\n")
	names := make([]string, 0, len(hashes))
	for name := range hashes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(&b, "%s  %s\n", hashes[name], name)
	}
	b.WriteString("\nCopyright notices carried by the sources\n----------------------------------------\n")
	notices := append([]string(nil), parsed.copyright...)
	for _, lang := range Languages {
		for _, data := range [][]byte{src.Annotations[lang], src.Derived[lang]} {
			if m := cldrCopyright.Find(data); m != nil {
				line := strings.TrimSpace(string(m))
				seen := false
				for _, n := range notices {
					seen = seen || n == line
				}
				if !seen {
					notices = append(notices, line)
				}
			}
		}
	}
	if len(notices) == 0 {
		b.WriteString("(none found in the source files)\n")
	}
	for _, n := range notices {
		b.WriteString(n + "\n")
	}
	b.WriteString("\n")
	b.WriteString(UnicodeLicenseV3)
	return []byte(b.String())
}

// UnicodeLicenseV3 is the licence the Unicode emoji and CLDR data are published
// under (https://www.unicode.org/license.txt). It must accompany every copy.
const UnicodeLicenseV3 = `UNICODE LICENSE V3

COPYRIGHT AND PERMISSION NOTICE

Copyright © 1991-2025 Unicode, Inc.

NOTICE TO USER: Carefully read the following legal agreement. BY
DOWNLOADING, INSTALLING, COPYING OR OTHERWISE USING DATA FILES, AND/OR
SOFTWARE, YOU UNEQUIVOCALLY ACCEPT, AND AGREE TO BE BOUND BY, ALL OF THE
TERMS AND CONDITIONS OF THIS AGREEMENT. IF YOU DO NOT AGREE, DO NOT
DOWNLOAD, INSTALL, COPY, DISTRIBUTE OR USE THE DATA FILES OR SOFTWARE.

Permission is hereby granted, free of charge, to any person obtaining a
copy of data files and any associated documentation (the "Data Files") or
software and any associated documentation (the "Software") to deal in the
Data Files or Software without restriction, including without limitation
the rights to use, copy, modify, merge, publish, distribute, and/or sell
copies of the Data Files or Software, and to permit persons to whom the
Data Files or Software are furnished to do so, provided that either (a)
this copyright and permission notice appear with all copies of the Data
Files or Software, or (b) this copyright and permission notice appear in
associated Documentation.

THE DATA FILES AND SOFTWARE ARE PROVIDED "AS IS", WITHOUT WARRANTY OF ANY
KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT OF
THIRD PARTY RIGHTS.

IN NO EVENT SHALL THE COPYRIGHT HOLDER OR HOLDERS INCLUDED IN THIS NOTICE
BE LIABLE FOR ANY CLAIM, OR ANY SPECIAL INDIRECT OR CONSEQUENTIAL DAMAGES,
OR ANY DAMAGES WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS,
WHETHER IN AN ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION,
ARISING OUT OF OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THE DATA
FILES OR SOFTWARE.

Except as contained in this notice, the name of a copyright holder shall
not be used in advertising or otherwise to promote the sale, use or other
dealings in these Data Files or Software without prior written
authorization of the copyright holder.
`
