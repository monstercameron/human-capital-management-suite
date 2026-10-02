package chatfilter

import (
	"math/rand"
	"strings"
	"testing"
)

// invisible characters that must never help a listed term past the filter:
// zero width characters, soft hyphen, bidi marks and isolates, the Arabic
// letter mark, invisible fillers, tatweel, and combining characters of Latin,
// Arabic and general use.
var invisibleInserts = []string{
	"\u200b", "\u200c", "\u200d", "\u2060", "\ufeff", "\u00ad",
	"\u200e", "\u200f", "\u202a", "\u202b", "\u202c", "\u202d", "\u202e", "\u2066", "\u2067", "\u2068", "\u2069", "\u061c",
	"\u3164", "\u115f", "\u0640", "\u034f", "\ufe0f", "\u180e",
	"\u0301", "\u0308", "\u0327", "\u0338", "\u0300", "\u0323", "\u064e", "\u064f", "\u0650", "\u0651", "\u0652", "\u0670",
}

// TestTodo_CHATMOD_002_Property: for every term of every built-in list, in
// every language, no insertion of zero width or combining characters between
// the letters or around the term lets it through, and the span still covers
// exactly the visible word plus the characters that trail it.
func TestTodo_CHATMOD_002_Property(t *testing.T) {
	rng := rand.New(rand.NewSource(20261001))
	block := compileBuiltins(t, "block")
	prefixes := []string{"", "go ", "(", "\"", "you: ", "well, "}
	suffixes := []string{"", " now", "!", ".)", "?!", ", ok"}
	cases := 0
	for _, l := range builtinLists() {
		for _, term := range l.Terms {
			runes := []rune(term)
			for round := 0; round < 14; round++ {
				gaps := map[int]string{}
				for n := 1 + rng.Intn(4); n > 0; n-- {
					gaps[rng.Intn(len(runes)+1)] += invisibleInserts[rng.Intn(len(invisibleInserts))]
				}
				if round == 0 { // always cover both ends
					gaps[0], gaps[len(runes)] = invisibleInserts[rng.Intn(len(invisibleInserts))], invisibleInserts[rng.Intn(len(invisibleInserts))]
				}
				var core strings.Builder
				for i, r := range runes {
					core.WriteString(gaps[i])
					core.WriteRune(r)
				}
				core.WriteString(gaps[len(runes)])
				prefix, suffix := prefixes[rng.Intn(len(prefixes))], suffixes[rng.Intn(len(suffixes))]
				body := prefix + core.String() + suffix
				wantStart, wantEnd := len(prefix)+len(gaps[0]), len(prefix)+core.Len()
				res, err := block.Evaluate(Input{Body: body, Language: l.Language})
				if err != nil || res.Action != "block" || !hasSpan(res, wantStart, wantEnd) {
					t.Fatalf("%s/%s: %q passed as %+q (want span %d-%d, got %+v, err %v)", l.Language, l.Category, term, body, wantStart, wantEnd, res.Hits, err)
				}
				cases++
			}
		}
	}
	if cases < 2000 {
		t.Fatalf("only %d cases ran", cases)
	}
	t.Logf("%d insertion cases, all caught", cases)

	t.Run("invisible characters join the word they sit in, as on screen", func(t *testing.T) {
		// "you<ZWSP>idiot" reads as one word, youidiot, so it is not the listed
		// word idiot; the same characters beside a space or punctuation are.
		for _, body := range []string{"you\u200bidiot", "you\u200didiot", "x\u0301damn"} {
			if res, _ := block.Evaluate(Input{Body: body}); len(res.Hits) != 0 {
				t.Errorf("%+q is one different word", body)
			}
		}
		for _, body := range []string{"you \u200bidiot\u200b now", "idiot\u200b.", "hello, \u2060damn\u2060"} {
			if res, _ := block.Evaluate(Input{Body: body}); res.Action != "block" {
				t.Errorf("%+q passed", body)
			}
		}
	})

	t.Run("invisible characters never make an innocent word a hit", func(t *testing.T) {
		for _, lang := range []string{"en", "de", "ar"} {
			for _, word := range knownFalsePositives(lang) {
				runes := []rune(word)
				for round := 0; round < 4; round++ {
					var b strings.Builder
					for _, r := range runes {
						b.WriteRune(r)
						if rng.Intn(3) == 0 {
							b.WriteString(invisibleInserts[rng.Intn(len(invisibleInserts))])
						}
					}
					if res, _ := block.Evaluate(Input{Body: "see " + b.String() + " now", Language: lang}); len(res.Hits) != 0 {
						t.Errorf("%s: %+q became a hit %v", lang, b.String(), res.Hits)
					}
				}
			}
		}
	})
}
