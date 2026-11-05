package productui

import (
	"strings"
	"testing"
)

func TestDedupeKeyframesBlocks(t *testing.T) {
	for _, test := range []struct {
		name  string
		sheet string
		want  string
	}{
		{
			name:  "empty",
			sheet: "",
			want:  "",
		},
		{
			name:  "no keyframes",
			sheet: `.a{color:red}@media(x){.b{color:blue}}`,
			want:  `.a{color:red}@media(x){.b{color:blue}}`,
		},
		{
			name:  "one block stays",
			sheet: `.a{color:red}@keyframes spin{from{opacity:0}to{opacity:1}}.b{color:blue}`,
			want:  `.a{color:red}@keyframes spin{from{opacity:0}to{opacity:1}}.b{color:blue}`,
		},
		{
			name:  "identical twins keep the last",
			sheet: `@keyframes spin{from{opacity:0}to{opacity:1}}.a{color:red}@keyframes spin{from{opacity:0}to{opacity:1}}`,
			want:  `.a{color:red}@keyframes spin{from{opacity:0}to{opacity:1}}`,
		},
		{
			name:  "identical triplets keep the last",
			sheet: `@keyframes spin{from{opacity:0}}.x{color:red}@keyframes spin{from{opacity:0}}.y{color:blue}@keyframes spin{from{opacity:0}}`,
			want:  `.x{color:red}.y{color:blue}@keyframes spin{from{opacity:0}}`,
		},
		{
			name:  "whitespace differences still match",
			sheet: "@keyframes spin {\n  from { opacity: 0 }\n}\n.a{color:red}\n@keyframes spin{from{opacity:0}}",
			want:  "\n.a{color:red}\n@keyframes spin{from{opacity:0}}",
		},
		{
			name:  "different bodies stay",
			sheet: `@keyframes spin{from{opacity:0}}.a{color:red}@keyframes spin{from{opacity:.5}}`,
			want:  `@keyframes spin{from{opacity:0}}.a{color:red}@keyframes spin{from{opacity:.5}}`,
		},
		{
			name:  "different names stay",
			sheet: `@keyframes one{from{opacity:0}}@keyframes two{from{opacity:0}}`,
			want:  `@keyframes one{from{opacity:0}}@keyframes two{from{opacity:0}}`,
		},
		{
			name:  "different media contexts stay",
			sheet: `@media(x){@keyframes spin{from{opacity:0}}}@keyframes spin{from{opacity:0}}`,
			want:  `@media(x){@keyframes spin{from{opacity:0}}}@keyframes spin{from{opacity:0}}`,
		},
		{
			name:  "same media context dedupes",
			sheet: `@media(x){@keyframes spin{from{opacity:0}}.a{color:red}@keyframes spin{from{opacity:0}}}`,
			want:  `@media(x){.a{color:red}@keyframes spin{from{opacity:0}}}`,
		},
		{
			name:  "braces in strings do not confuse the scan",
			sheet: `@keyframes spin{from{content:"}{"}}.a{content:"@"}@keyframes spin{from{content:"}{"}}`,
			want:  `.a{content:"@"}@keyframes spin{from{content:"}{"}}`,
		},
		{
			name:  "comments do not confuse the scan",
			sheet: `@keyframes spin{/* } */from{opacity:0}}/* @keyframes spin{from{opacity:0}} */@keyframes spin{from{opacity:0}}`,
			want:  `/* @keyframes spin{from{opacity:0}} */@keyframes spin{from{opacity:0}}`,
		},
		{
			name:  "keyframes-like prefix is not a block",
			sheet: `@keyframesX{from{opacity:0}}@keyframes spin{from{opacity:0}}`,
			want:  `@keyframesX{from{opacity:0}}@keyframes spin{from{opacity:0}}`,
		},
		{
			name:  "unterminated block keeps the sheet",
			sheet: `@keyframes spin{from{opacity:0}`,
			want:  `@keyframes spin{from{opacity:0}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := dedupeKeyframesBlocks(test.sheet); got != test.want {
				t.Fatalf("got  %q\nwant %q", got, test.want)
			}
		})
	}
}

// TestDedupeKeyframesBlocksKeepsAnimationNames proves the served control
// sheet still names every animation its rules reference after the dedupe:
// dropping a hoisted block must never strand an animation-name.
func TestDedupeKeyframesBlocksKeepsAnimationNames(t *testing.T) {
	platformStylesOnce.Do(initPlatformStyles)
	for name, sheet := range map[string]string{
		"control": ControlStylesheet(),
		"chat":    ChatStylesheet(),
	} {
		chunks := strings.Split(sheet, "animation-name:")
		for index, chunk := range chunks {
			if index == 0 {
				continue
			}
			end := strings.IndexAny(chunk, ";}")
			if end < 0 {
				t.Fatalf("%s sheet: unterminated animation-name", name)
			}
			animation := strings.TrimSpace(chunk[:end])
			if !strings.Contains(sheet, "@keyframes "+animation+"{") {
				t.Fatalf("%s sheet: animation-name %q has no @keyframes block", name, animation)
			}
		}
	}
}
