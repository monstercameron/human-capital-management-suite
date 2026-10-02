// Command emojidata generates the Chat emoji picker's data files from Unicode's
// emoji-test.txt and the CLDR annotation files in a local folder:
//
//	go run ./tools/emojidata -in .artifacts/unicode -out internal/humanwork/workspace/assets
//
// The folder holds emoji-test.txt, annotations/{en,de,ar}.xml and
// annotationsDerived/{en,de,ar}.xml exactly as Unicode publishes them. The tool
// reads those files and writes emoji-order.json, emoji-<language>.json (each
// with a .gz beside it) and emoji-LICENSE.txt into -out, then brings the
// workspace asset manifest up to date. It never fetches anything.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/tools/emojidata/emojigen"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("emojidata", flag.ContinueOnError)
	flags.SetOutput(stderr)
	in := flags.String("in", ".artifacts/unicode", "folder holding emoji-test.txt, annotations/ and annotationsDerived/")
	out := flags.String("out", "internal/humanwork/workspace/assets", "folder to write the data files into")
	cldr := flags.String("cldr", "", "CLDR release the annotation files come from, recorded beside the data (e.g. 48)")
	partial := flags.Bool("allow-partial", false, "accept a source folder that lacks groups or most German and Arabic names (for the small test fixtures only)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	src, err := emojigen.ReadDir(*in)
	if err != nil {
		fmt.Fprintf(stderr, "emojidata: %v\n", err)
		return 1
	}
	src.CLDRVersion, src.AllowPartial = *cldr, *partial
	result, err := emojigen.Build(src)
	if err != nil {
		fmt.Fprintf(stderr, "emojidata: %v\n", err)
		return 1
	}
	written, err := emojigen.Write(*out, result)
	if err != nil {
		fmt.Fprintf(stderr, "emojidata: %v\n", err)
		return 1
	}
	sort.Strings(written)
	s := result.Summary
	fmt.Fprintf(stdout, "Unicode emoji %s, CLDR %s: %d emoji (%d skin-tone variants) in %d groups\n", s.EmojiVersion, s.CLDR, s.Entries, s.Variants, len(s.Groups))
	for _, g := range s.Groups {
		fmt.Fprintf(stdout, "  %-20s %d\n", g.Name, g.Count)
	}
	for _, lang := range emojigen.Languages {
		c := s.Coverage[lang]
		fmt.Fprintf(stdout, "  %s: %d of %d named, %d with keywords\n", lang, c.Names, c.Total, c.Keywords)
	}
	for _, name := range written {
		fmt.Fprintf(stdout, "wrote %s/%s\n", *out, name)
	}
	return 0
}
