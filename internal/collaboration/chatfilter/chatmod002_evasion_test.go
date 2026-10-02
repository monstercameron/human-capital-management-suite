package chatfilter

import (
	"strings"
	"testing"
)

// stretchLetters writes every letter of the term three times, keeping spaces
// and apostrophes: "fuck" becomes "fffuuucccckkk"-style stretching.
func stretchLetters(term string) string {
	var b strings.Builder
	for _, r := range term {
		b.WriteRune(r)
		if r != ' ' && r != '\'' {
			b.WriteRune(r)
			b.WriteRune(r)
		}
	}
	return b.String()
}

// censorLetter replaces one interior letter of a term of four or more letters
// with a censor mark. A letter that folds to more than one (the German sharp s)
// is never chosen: a mark stands for exactly one letter.
func censorLetter(term, mark string) (string, bool) {
	var total int
	for _, r := range term {
		if r != ' ' && r != '\'' {
			total++
		}
	}
	if total < 4 {
		return "", false
	}
	runes := []rune(term)
	index := 0
	for i, r := range runes {
		if r == ' ' || r == '\'' {
			continue
		}
		if index >= 1 && index <= total-2 && r != 'ß' {
			runes[i] = []rune(mark)[0]
			return string(runes), true
		}
		index++
	}
	return "", false
}

// TestTodo_CHATMOD_002_Evasions: repeated letters and censor marks.
func TestTodo_CHATMOD_002_Evasions(t *testing.T) {
	mask := compileBuiltins(t, "mask")
	clean := compileBuiltins(t, "block")

	t.Run("stretched letters match", func(t *testing.T) {
		for _, c := range []struct{ in, want string }{
			{"fuuuck", removed},
			{"fuuuck!!", removed + "!!"},
			{"daaamn", removed},
			{"DAAAMN it", removed}, // the listed phrase "damn it"
			{"shiiit", removed},
			{"shiiiit!", removed + "!"},
			{"verdaaammt", removed},
			{"Verdammmmt!", removed + "!"},
			{"asssss", removed},
			{"aaass", removed},
			{"idiiiot", removed},
			{"اخررررس", removed},
			{"you biiitch", "you " + removed},
		} {
			r, err := mask.Evaluate(Input{Body: c.in})
			if err != nil || r.Masked != c.want {
				t.Errorf("%q masked to %q, want %q (%v)", c.in, r.Masked, c.want, err)
			}
		}
	})

	t.Run("a doubled letter of the term still needs two", func(t *testing.T) {
		d := rule("block")
		d.Match = []string{"hell", "ass"}
		ev, err := NewRegistry().Compile([]Definition{d})
		if err != nil {
			t.Fatal(err)
		}
		for body, hit := range map[string]bool{
			"hell": true, "helllll": true, "hhheeellll": true, "hello": false, "hel": false, "helm": false,
			"ass": true, "assss": true, "aass": true, "as": false, "asp": false, "assess": false, "ssa": false,
		} {
			r, _ := ev.Evaluate(Input{Body: body})
			if (len(r.Hits) > 0) != hit {
				t.Errorf("%q: hit=%v want %v", body, len(r.Hits) > 0, hit)
			}
			if hit && !hasSpan(r, 0, len(body)) {
				t.Errorf("%q: span %+v", body, r.Hits)
			}
		}
	})

	t.Run("a stretched letter before a word break", func(t *testing.T) {
		d := rule("block")
		d.Match = []string{"shut the"}
		ev, _ := NewRegistry().Compile([]Definition{d})
		for body, hit := range map[string]bool{"shut the": true, "shutthe": true, "shuuutthe": true, "shuuuttt   the": true, "shu the": false, "shutt he": false} {
			if r, _ := ev.Evaluate(Input{Body: body}); (len(r.Hits) > 0) != hit {
				t.Errorf("%q: hit=%v want %v", body, len(r.Hits) > 0, hit)
			}
		}
	})

	t.Run("censor marks match", func(t *testing.T) {
		for _, c := range []struct{ in, want string }{
			{"f*ck", removed},
			{"F*CK!", removed + "!"},
			{"sh*t", removed},
			{"b**ch", removed},
			{"f**k", removed},
			{"s**t", removed},
			{"a$$hole", removed},
			{"f#ck", removed},
			{"d_mn", removed},
			{"d*mn it", removed + " it"},
			{"you f*cking idiot", "you " + removed + " " + removed},
			{"(sh*t)", "(" + removed + ")"},
			{"f*ck", removed},
			{"v*rdammt", removed},
			{"ar*chloch", removed},
			{"اخ*س الآن", removed + " الآن"},
		} {
			r, err := mask.Evaluate(Input{Body: c.in})
			if err != nil || r.Masked != c.want {
				t.Errorf("%q masked to %q, want %q (%v)", c.in, r.Masked, c.want, err)
			}
		}
	})

	t.Run("a mark replaces letters one for one", func(t *testing.T) {
		for _, body := range []string{"f*k", "sh**t", "f***k", "***ck", "f***", "*uck", "fuc*", "*hit*", "sh*"} {
			if r, _ := clean.Evaluate(Input{Body: body}); len(r.Hits) != 0 {
				t.Errorf("%q matched %v", body, hitSlices(body, r))
			}
		}
		// more than half hidden is refused; exactly half is allowed.
		if r, _ := clean.Evaluate(Input{Body: "f**c*"}); len(r.Hits) != 0 {
			t.Error("trailing mark accepted")
		}
		if r, _ := clean.Evaluate(Input{Body: "b***h"}); len(r.Hits) != 0 {
			t.Error("three of five hidden accepted")
		}
		if r, _ := clean.Evaluate(Input{Body: "d**n"}); len(r.Hits) == 0 {
			t.Error("two of four hidden is allowed")
		}
		// terms of three letters are never matched through marks.
		if r, _ := clean.Evaluate(Input{Body: "k*s"}); len(r.Hits) != 0 {
			t.Error("three-letter term matched through a mark")
		}
	})

	t.Run("ordinary text with asterisks and stretching is clean", func(t *testing.T) {
		for _, s := range []string{
			"5*3 is 15 and 2*4*6 is 48",
			"a*b + c*d = f(x)*g(x)",
			"This is *bold* and **important** and _italic_ and __strong__ text.",
			"**Important:** please read the *attached* note, thanks!",
			"See the footnote* below and the second one**.",
			"Rated 5* by users, 4.5* on average (*terms apply).",
			"SELECT * FROM users WHERE name LIKE 'a*' ; ls *.txt ; rm -rf build/*",
			"Use snake_case or camel_case_names, not my_dmn_name or user_id_42.",
			"Tag #release and #hotfix, issue #42, 100% sure, ~5 minutes, 2^10 = 1024.",
			"x*y*z, s*t, c*k, **assess**, *class*, **document**",
			"Soooo good, yesss, noooo way, helloooo, heyyy, looool, aaaah, hmmmm, ahhh, coool breeze.",
			"Mississippi, committee, bookkeeper, cooperate, balloon, address, assessment, success, necessary.",
			"Die Klassik ist schööön, das war suuuper und die Kasse ist offeeen.",
			"مرحباااا كيف حالك *شكرا* لك",
		} {
			r, err := clean.Evaluate(Input{Body: s})
			if err != nil || len(r.Hits) != 0 {
				t.Errorf("%q: false positive %v %v", s, hitSlices(s, r), err)
			}
		}
	})

	t.Run("spans cover the stretched and marked text", func(t *testing.T) {
		for _, c := range []struct {
			in         string
			start, end int
		}{
			{"go fuuuck now", 3, len("go fuuuck")},
			{"go f*ck now", 3, len("go f*ck")},
			{"go b**ch!", 3, len("go b**ch")},
			{"é d_mn.", len("é "), len("é d_mn")},
		} {
			r, _ := clean.Evaluate(Input{Body: c.in})
			if !hasSpan(r, c.start, c.end) {
				t.Errorf("%q: want %d-%d got %+v", c.in, c.start, c.end, r.Hits)
			}
		}
	})
}
