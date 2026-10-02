package chatfilter

import (
	"errors"
	"strings"
	"testing"
)

const removed = "[removed word]"

// TestTodo_CHATMOD_002 is the primary proof of CHATMOD-002: matching that is
// correct for real text in English, German and Arabic, the defects found by
// probing, the false-positive tables over the real built-in lists, the list
// shape, and the spans of a refusal.
func TestTodo_CHATMOD_002(t *testing.T) {
	t.Run("masked output of real text", func(t *testing.T) {
		mask := compileBuiltins(t, "mask")
		for _, c := range []struct{ in, want string }{
			// 1. punctuation after a word never hides it.
			{"damn!", removed + "!"},
			{"damn!!", removed + "!!"},
			{"(damn)", "(" + removed + ")"},
			{"What the DAMN?!", "What the " + removed + "?!"},
			{"shut up!", removed + "!"},
			{"you idiot!", "you " + removed + "!"},
			{"Verdammt!", removed + "!"},
			{"VERDAMMT.", removed + "."},
			{"اللعنة!", removed + "!"},
			{"يا أحمق!", "يا " + removed + "!"},
			// letter substitutions inside a word still match.
			{"d@mn", removed},
			{"d4mn it", removed}, // the phrase "damn it" is listed
			{"d4mn, it", removed + ", it"},
			{"sh!t", removed},
			{"a$$", removed},
			{"a$$hole", removed},
			{"@ss", removed},
			{"5h1t", removed},
			// 2. compatibility forms and look-alike letters.
			{"\uff44\uff41\uff4d\uff4e", removed},
			{"\uff33\uff28\uff35\uff34 \uff35\uff30", removed},
			{"d\u0430mn", removed},                 // Cyrillic a
			{"\u0455h\u0456t", removed},            // Cyrillic dze, i
			{"d\u03b1mn", removed},                 // Greek alpha
			{"\u03b9d\u03b9\u03bf\u03c4", removed}, // Greek iota, omicron, tau
			{"dámn", removed},
			{"DÀMN", removed},
			{"\uff44\uff41\uff4d\uff4e!", removed + "!"},
			{"اذهب الي الجحيم", removed}, // alef maqsura written as yeh
			{"اذهب إلى الجحيم", removed},
			{"الل\u064e\u0651عن\u064eة", removed}, // tashkeel
			{"الل\u0640\u0640\u0640عنة", removed}, // tatweel
			{"أحمق", removed},
			{"احمق", removed},
			// German folding.
			{"SCHEISSE", removed},
			{"Scheiße!", removed + "!"},
			{"scheiß drauf", removed + " drauf"},
			{"ARSCHLÖCHER", removed},
			{"verdämmt", removed},
			// 3. spacing tricks.
			{"d a m n", removed},
			{"d.a.m.n", removed},
			{"d-a-m-n", removed},
			{"s h u t   u p", removed},
			{"I'LL KILL YOU", removed},
			{"ill kill you", removed},
			// invisible characters.
			{"d\u200ba\u200cm\u200dn", removed},
			{"damn\u200b!", removed + "!"},
			{"da\u00admn", removed},
			{"idiot\u200b\u0301!", removed + "!"},
		} {
			r, err := mask.Evaluate(Input{Body: c.in})
			if err != nil {
				t.Fatalf("%q: %v", c.in, err)
			}
			if r.Masked != c.want {
				t.Errorf("%q masked to %q, want %q (hits %v)", c.in, r.Masked, c.want, hitSlices(c.in, r))
			}
		}
	})

	t.Run("spans are original byte offsets", func(t *testing.T) {
		block := compileBuiltins(t, "block")
		for _, c := range []struct {
			in         string
			start, end int
		}{
			{"héllo damn", len("héllo "), len("héllo damn")},
			{"\uff44\uff41\uff4d\uff4e now", 0, len("\uff44\uff41\uff4d\uff4e")},
			{"日本語 damn!", len("日本語 "), len("日本語 damn")},
			{"damn\u200b\u200d!", 0, len("damn\u200b\u200d")},
			{"dam\u0301n\u0308 ok", 0, len("dam\u0301n\u0308")},
			{"d a m n.", 0, len("d a m n")},
			{"قال اللعنة!", len("قال "), len("قال اللعنة")},
			{"أنت أحمق\u064f", len("أنت "), len("أنت أحمق\u064f")},
			{"x \u200bdamn", len("x \u200b"), len("x \u200bdamn")},
		} {
			r, err := block.Evaluate(Input{Body: c.in})
			if err != nil || !hasSpan(r, c.start, c.end) {
				t.Errorf("%q: want span %d-%d got %+v %v", c.in, c.start, c.end, r.Hits, err)
			}
		}
	})

	t.Run("ordinary sentences are clean", func(t *testing.T) {
		ev := compileBuiltins(t, "block")
		for _, s := range []string{
			"This is a calm and ordinary message about the quarterly plan.",
			"As soon as possible, the sales manager has a simple answer.",
			"Mr. S hit the ball and a student sat at a desk.",
			"He was a big fan of the a s d f keyboard row, it said s s s once.",
			"Take a pass at the class list, then assess the assistant.",
			"Lunch is at 12.30, meet at room 4 or room 7, thanks!",
			"Price: $5 for 3 items @ the corner, 100% done :-) (ok!)",
			"user@example.com wrote: see https://example.com/a-b_c today",
			"He'll say she'll go; they'd stay; I'm sure it isn't so.",
			"Die Kasse ist offen; der Spaß war groß und die Masse jubelte.",
			"Der Teufel steckt im Detail, aber der Mist ist gefahren.",
			"Wir treffen uns um 18 Uhr, bis dahin ist alles gut!",
			"مرحبا كيف حالك اليوم؟ ذهبت الى المدرسة وكتبت الدرس.",
			"سأقابلك غدا في الملعب بعد العمل، شكرا لك!",
		} {
			r, err := ev.Evaluate(Input{Body: s})
			if err != nil || len(r.Hits) != 0 {
				t.Errorf("%q: false positive %v %v", s, hitSlices(s, r), err)
			}
		}
	})

	t.Run("short terms need every letter spaced", func(t *testing.T) {
		block := compileBuiltins(t, "block")
		for _, s := range []string{"s hit", "da mn", "sh it", "as s", "a ss", "wan k"} {
			r, _ := block.Evaluate(Input{Body: s})
			if len(r.Hits) != 0 {
				t.Errorf("%q matched %v", s, hitSlices(s, r))
			}
		}
		// five or more letters tolerate a split, and a phrase may be typed with
		// or without its own spaces.
		for _, s := range []string{"idi ot", "shutup", "shut  up", "shut-up", "shut_up", "s hut up"} {
			r, _ := block.Evaluate(Input{Body: s})
			if len(r.Hits) == 0 {
				t.Errorf("%q passed", s)
			}
		}
	})

	t.Run("language selects the lists", func(t *testing.T) {
		ev := compileBuiltins(t, "block")
		for _, c := range []struct {
			body, lang string
			hit        bool
		}{
			{"damn", "en-US", true}, {"damn", "de-DE", false}, {"damn", "ar", false}, {"damn", "", true},
			{"verdammt", "de", true}, {"verdammt", "en", false},
			{"اخرس", "ar-EG", true}, {"اخرس", "en", false},
		} {
			r, _ := ev.Evaluate(Input{Body: c.body, Language: c.lang})
			if (len(r.Hits) > 0) != c.hit {
				t.Errorf("%q in %q: hit=%v want %v", c.body, c.lang, len(r.Hits) > 0, c.hit)
			}
		}
	})

	t.Run("built-in list shape", func(t *testing.T) {
		defs := Builtins()
		if len(defs) != 9 {
			t.Fatalf("want 9 lists, got %d", len(defs))
		}
		bounds := map[string][2]int{"profanity": {25, 60}, "slurs": {10, 25}, "harassment": {10, 25}}
		for _, d := range defs {
			cat := strings.TrimPrefix(d.ID, "builtin-"+d.Language+"-")
			b, ok := bounds[cat]
			if !ok || d.Name != cat || d.Version != "1.1.0" || d.Kind != "words" || d.Action != "block" || !d.Product || d.Hard || len(d.Channels) != 0 {
				t.Fatalf("definition %+v", d)
			}
			if len(d.Match) < b[0] || len(d.Match) > b[1] {
				t.Errorf("%s has %d terms, want %d-%d", d.ID, len(d.Match), b[0], b[1])
			}
			seen := map[string]string{}
			for _, term := range d.Match {
				wt, ok := newWordTerm(term)
				if !ok || len(wt.letters) < 3 {
					t.Errorf("%s: term %q is too short to be listed", d.ID, term)
				}
				if prior, dup := seen[wt.key()]; dup {
					t.Errorf("%s: %q duplicates %q once folded", d.ID, term, prior)
				}
				seen[wt.key()] = term
			}
		}
		if _, err := NewRegistry().Compile(defs); err != nil {
			t.Fatal(err)
		}
		// off by default: no enablement row, nothing is judged.
		svc := &Service{Store: newMemStore(), Registry: NewRegistry()}
		r, err := svc.Evaluate(t.Context(), Input{Tenant: "t", Body: "damn you idiot"}, false)
		if err != nil || len(r.Hits) != 0 || r.Action != "" {
			t.Fatalf("lists are on by default: %+v %v", r, err)
		}
	})

	t.Run("no list term is a known innocent word", func(t *testing.T) {
		for _, l := range builtinLists() {
			for _, term := range l.Terms {
				folded := string(fold(term).text)
				for _, other := range []string{"en", "de", "ar"} {
					if falsePositive(other, folded) {
						t.Errorf("%s term %q is a known innocent word of %s", l.Language, term, other)
					}
				}
			}
		}
		if !falsePositive("en-GB", "scunthorpe") || !falsePositive("de", "klassisch") || falsePositive("de", "scunthorpe") || falsePositive("fr", "class") {
			t.Fatal("false positive lookup by locale is wrong")
		}
	})

	t.Run("custom lists are not filtered by the product false positive list", func(t *testing.T) {
		d := rule("block")
		d.Language, d.Match = "en", []string{"button"}
		ev, err := NewRegistry().Compile([]Definition{d})
		if err != nil {
			t.Fatal(err)
		}
		if r, _ := ev.Evaluate(Input{Body: "press the button"}); r.Action != "block" {
			t.Fatal("an administrator's own term was swallowed by the product list")
		}
	})
}

// TestTodo_CHATMOD_002_FalsePositives runs the false-positive and bypass
// tables per language over the real built-in lists, all nine switched on
// through the service.
func TestTodo_CHATMOD_002_FalsePositives(t *testing.T) {
	svc, _ := allBuiltinsOn(t)
	judge := func(body, lang string) Result {
		r, err := svc.Evaluate(t.Context(), Input{Tenant: "t", Channel: "general", Body: body, Language: lang}, false)
		if err != nil {
			t.Fatalf("%q: %v", body, err)
		}
		return r
	}
	innocent := map[string][]string{
		"en": {
			"Scunthorpe", "assess", "assessment", "assistant", "class", "classic", "bass", "cocktail", "Essex", "Sussex", "Penistone", "Lightwater",
			"Hancock", "passage", "grass", "mass", "glass", "analysis", "document", "button", "therapist", "shitake", "shiitake", "Twatt", "Wankel",
			"arsenal", "retardant", "scrape", "harassment", "assassin", "compass", "embassy",
		},
		"de": {"Klassik", "klassisch", "Schifffahrt", "Kasse", "Hass", "Masse", "Spaß", "Assistent", "Glasscheibe", "Pissoir", "Mongolei", "Idiotie", "Passagier", "Scheibe", "spastisch"},
		"ar": {"أحماض", "مخلص", "خرافة", "خرائط", "خراب", "خراج", "تباهى", "تبادل", "تباين", "تحقير", "أسافل", "الأخرس", "عرصة", "حقيقة", "ملعب", "كسوة"},
	}
	for lang, words := range innocent {
		for _, w := range words {
			for _, body := range []string{w, strings.ToUpper(w), "The " + w + " is here.", "see (" + w + ")!", w + ", " + w + "."} {
				if r := judge(body, lang); len(r.Hits) != 0 {
					t.Errorf("%s: %q is a false positive: %v", lang, body, hitSlices(body, r))
				}
				if r := judge(body, ""); len(r.Hits) != 0 {
					t.Errorf("any language: %q is a false positive: %v", body, hitSlices(body, r))
				}
			}
		}
	}

	letters := strings.NewReplacer("a", "4", "e", "3", "i", "1", "o", "0", "s", "5", "t", "7")
	vowels := map[rune]bool{'a': true, 'e': true, 'i': true, 'o': true, 'u': true}
	accent := func(term string, arabic bool) string {
		var b strings.Builder
		for _, r := range term {
			b.WriteRune(r)
			switch {
			case arabic && r != ' ':
				b.WriteRune('\u064e')
			case vowels[r]:
				b.WriteRune('\u0301')
			}
		}
		return b.String()
	}
	for _, l := range builtinLists() {
		arabic := l.Language == "ar"
		for _, term := range l.Terms {
			variants := map[string]string{
				"plain":   term,
				"upper":   strings.ToUpper(term),
				"accents": accent(term, arabic),
				"spacing": joinLetters(term, " "),
				"dots":    joinLetters(term, "."),
				"dashes":  joinLetters(term, "-"),
			}
			if !arabic {
				variants["leet"] = letters.Replace(term)
			}
			variants["stretched"] = stretchLetters(term)
			for _, mark := range []string{"*", "#", "_"} {
				if censored, ok := censorLetter(term, mark); ok {
					variants["censored "+mark] = censored
				}
			}
			for kind, core := range variants {
				for _, punct := range []string{"", "!", "?", ".", ",", ")", "!!", "?!", "..."} {
					const prefix, suffix = "well ", " now"
					body := prefix + core + punct + suffix
					r := judge(body, l.Language)
					if r.Action != "block" || !hasSpan(r, len(prefix), len(prefix)+len(core)) {
						t.Errorf("%s/%s %s variant %q (%q) was not caught: %v", l.Language, l.Category, kind, core, body, hitSlices(body, r))
					}
				}
			}
		}
	}
}

// TestTodo_CHATMOD_002_BlockedSpans proves the refusal carries every span of
// every blocking hit, sorted, without duplicates, at most 16, with Span first.
func TestTodo_CHATMOD_002_BlockedSpans(t *testing.T) {
	block := compileBuiltins(t, "block")
	body := "damn it you idiot, shut up! damn"
	r, err := block.Evaluate(Input{Body: body})
	if err != nil {
		t.Fatal(err)
	}
	var blocked *BlockedError
	if !errorsAs(r.Refusal(), &blocked) {
		t.Fatalf("no typed refusal: %v", r.Refusal())
	}
	want := []Span{{0, 4}, {0, 7}, {12, 17}, {19, 26}, {28, 32}} // damn, "damn it", idiot (two lists, one span), shut up, damn
	if len(blocked.Spans) != len(want) {
		t.Fatalf("spans %v want %v", blocked.Spans, want)
	}
	for i, s := range want {
		if blocked.Spans[i] != s {
			t.Fatalf("spans %v want %v", blocked.Spans, want)
		}
	}
	if blocked.Span != blocked.Spans[0] || blocked.RuleName == "" {
		t.Fatalf("Span must be the first of Spans: %+v", blocked)
	}

	// two rules that match the same word give one span; the cap is 16.
	a, b := rule("block"), rule("block")
	a.ID, b.ID, b.Name = "a", "b", "Other"
	twice, _ := NewRegistry().Compile([]Definition{a, b})
	r, _ = twice.Evaluate(Input{Body: "quartz"})
	if len(r.Hits) != 2 || len(errorsAsSpans(r)) != 1 {
		t.Fatalf("duplicate spans not removed: %d hits, spans %v", len(r.Hits), errorsAsSpans(r))
	}
	r, _ = block.Evaluate(Input{Body: strings.Repeat("damn ", 30)})
	if got := errorsAsSpans(r); len(got) != 16 || got[0] != (Span{0, 4}) || got[15] != (Span{75, 79}) {
		t.Fatalf("cap or order wrong: %v", got)
	}

	// a mask rule that outranks nothing leaves no refusal; a hit list built by
	// hand still yields typed spans.
	manual := Result{Action: "block", Hits: []Hit{
		{RuleName: "R", Action: "block", Span: Span{9, 12}}, {RuleName: "R", Action: "block", Span: Span{1, 3}},
		{RuleName: "R", Action: "block", Span: Span{1, 3}}, {RuleName: "Dry", Action: "block", DryRun: true, Span: Span{20, 24}}, {Action: "mask", Span: Span{30, 31}},
	}}
	if !errorsAs(manual.Refusal(), &blocked) || blocked.Span != (Span{1, 3}) || len(blocked.Spans) != 2 || blocked.Spans[1] != (Span{9, 12}) || blocked.RuleName != "R" {
		t.Fatalf("fallback refusal: %+v", blocked)
	}

	// through the service: dry-run rules do not contribute.
	svc, _ := allBuiltinsOn(t)
	r, err = svc.Evaluate(t.Context(), Input{Tenant: "t", Body: "go damn!"}, false)
	if err != nil || !errorsAs(r.Refusal(), &blocked) || len(blocked.Spans) != 1 || blocked.Spans[0] != (Span{3, 7}) {
		t.Fatalf("service refusal: %+v %v", blocked, err)
	}
}

func errorsAs(err error, target **BlockedError) bool {
	return errors.As(err, target) && errors.Is(err, ErrBlocked)
}

func errorsAsSpans(r Result) []Span {
	var b *BlockedError
	if !errorsAs(r.Refusal(), &b) {
		return nil
	}
	return b.Spans
}
