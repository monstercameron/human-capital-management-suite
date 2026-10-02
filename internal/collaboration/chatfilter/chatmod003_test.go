package chatfilter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"
)

func custom(id, kind, action string, match ...string) Definition {
	d := Definition{ID: id, Name: "Rule " + id, Version: "1.0.0", Kind: kind, Action: action, Match: match}
	if action == "notify" {
		d.Target = "review"
	}
	return d
}

// TestTodo_CHATMOD_003_Property: evaluation is deterministic and the order of
// the definitions (and of the terms inside one) never changes the outcome; the
// strictest action wins (block > mask > flag > notify).
func TestTodo_CHATMOD_003_Property(t *testing.T) {
	defs := []Definition{
		custom("a-block", "words", "block", "quartz", "agate"),
		custom("b-mask", "words", "mask", "quartz", "topaz", "jasper"),
		custom("c-flag", "words", "flag", "topaz", "garnet"),
		custom("d-notify", "words", "notify", "opal", "garnet"),
		custom("e-mask", "pattern", "mask", `PROJECT-[0-9]{3}`, `CLIENT-[A-Z]{2}`),
		custom("f-flag", "detector", "flag", "card"),
		custom("g-block", "detector", "block", "secret"),
		custom("h-notify", "attachment", "notify", "application/x-executable"),
		custom("i-flag", "detector", "flag", "external-link", "example.com"),
	}
	rank := map[string]int{"notify": 1, "flag": 2, "mask": 3, "block": 4, "": 0}
	bodies := []struct{ body, want string }{
		{"nothing to see", ""},
		{"quartz", "block"},
		{"agate and garnet", "block"},
		{"topaz", "mask"},
		{"garnet", "flag"},
		{"opal", "notify"},
		{"opal and garnet", "flag"},
		{"opal topaz", "mask"},
		{"PROJECT-123 opal", "mask"},
		{"CLIENT-AB", "mask"},
		{"4111 1111 1111 1111", "flag"},
		{"token=neutralplaceholder 4111 1111 1111 1111", "block"},
		{"see https://other.example/x opal", "flag"},
		{"jasper garnet opal", "mask"},
	}
	in := func(body string) Input {
		return Input{Body: body, AttachmentTypes: []string{"application/x-executable"}}
	}
	reference, err := NewRegistry().Compile(defs)
	if err != nil {
		t.Fatal(err)
	}
	want := make([]Result, len(bodies))
	for i, b := range bodies {
		got, err := reference.Evaluate(in(b.body))
		if err != nil {
			t.Fatal(err)
		}
		// the attachment rule notifies on every body; strictness decides.
		top := rank[got.Action]
		for _, h := range got.Hits {
			if rank[h.Action] > top {
				t.Fatalf("%q: action %q is not the strictest of its hits", b.body, got.Action)
			}
		}
		if got.Action != b.want && !(b.want == "" && got.Action == "notify") {
			t.Fatalf("%q: action %q, want %q", b.body, got.Action, b.want)
		}
		want[i] = got
	}
	rng := rand.New(rand.NewSource(3))
	for round := 0; round < 300; round++ {
		shuffled := make([]Definition, len(defs))
		for i, j := range rng.Perm(len(defs)) {
			shuffled[i] = defs[j]
			shuffled[i].Match = append([]string(nil), defs[j].Match...)
			if k := shuffled[i].Kind; k == "words" || k == "pattern" || k == "attachment" { // a detector's first term names it
				rng.Shuffle(len(shuffled[i].Match), func(a, b int) {
					shuffled[i].Match[a], shuffled[i].Match[b] = shuffled[i].Match[b], shuffled[i].Match[a]
				})
			}
		}
		ev, err := NewRegistry().Compile(shuffled)
		if err != nil {
			t.Fatal(err)
		}
		for i, b := range bodies {
			got, err := ev.Evaluate(in(b.body))
			if err != nil || !reflect.DeepEqual(got, want[i]) {
				t.Fatalf("round %d, %q depends on order:\n got  %+v\n want %+v\n err %v", round, b.body, got, want[i], err)
			}
			again, _ := ev.Evaluate(in(b.body))
			if !reflect.DeepEqual(got, again) {
				t.Fatalf("round %d, %q is not deterministic", round, b.body)
			}
		}
	}

	t.Run("hits are ordered by rule then position", func(t *testing.T) {
		got, _ := reference.Evaluate(in("jasper quartz topaz"))
		var ids []string
		last := ""
		lastStart := -1
		for _, h := range got.Hits {
			if h.RuleID < last || h.RuleID == last && h.Span.Start < lastStart {
				t.Fatalf("hits out of order: %+v", got.Hits)
			}
			last, lastStart = h.RuleID, h.Span.Start
			ids = append(ids, h.RuleID)
		}
		if len(ids) < 5 {
			t.Fatalf("hits: %v", ids)
		}
	})

	t.Run("the strictest action wins through the service", func(t *testing.T) {
		store := newMemStore()
		for _, d := range defs[:4] {
			store.defs["t"] = append(store.defs["t"], d)
			store.rawEnable("t", Enablement{RuleID: d.ID, Enabled: true})
		}
		svc := &Service{Store: store, Registry: NewRegistry(), Delivery: &recordingDelivery{}}
		res, err := svc.Evaluate(t.Context(), Input{Tenant: "t", Body: "quartz topaz opal"}, false)
		if err != nil || res.Action != "block" || res.Refusal() == nil || !strings.Contains(res.Masked, removed) {
			t.Fatalf("%+v %v", res, err)
		}
	})
}

// TestTodo_CHATMOD_003_Security: patterns are plain RE2 and bounded; detector
// hits never store the matched secret; a refused definition is never stored.
func TestTodo_CHATMOD_003_Security(t *testing.T) {
	compile := func(patterns ...string) error {
		_, err := NewRegistry().Compile([]Definition{custom("p", "pattern", "block", patterns...)})
		return err
	}

	t.Run("refused patterns", func(t *testing.T) {
		many := make([]string, 33)
		for i := range many {
			many[i] = fmt.Sprintf("code%d", i)
		}
		for name, patterns := range map[string][]string{
			"lookahead":          {`(?=secret)`},
			"negative lookahead": {`foo(?!bar)`},
			"lookbehind":         {`(?<=a)b`},
			"backreference":      {`(a+)\1`},
			"backreference 2":    {`(x)(y)\2`},
			"empty":              {``},
			"star":               {`a*`},
			"optional":           {`a?`},
			"anchor only":        {`^`},
			"end anchor":         {`$`},
			"word boundary":      {`\b`},
			"empty alternative":  {`a|`},
			"dot star":           {`.*`},
			"dot plus":           {`.+`},
			"dot star inside":    {`x.*y`},
			"group dot plus":     {`x(.)+y`},
			"nested plus":        {`(a+)+`},
			"nested star":        {`(a*)*`},
			"nested mixed":       {`(a+)*b`},
			"nested deep":        {`((a+))+`},
			"nested alternation": {`(a|b+)+`},
			"nested repeat":      {`(a{2,})+`},
			"nested class":       {`([a-z]+[0-9]*)+x`},
			"flags":              {`(?i)abc`},
			"named group":        {`(?P<x>a)`},
			"too long":           {strings.Repeat("a", 257)},
			"too many":           many,
			"too many repeats":   {`a{2}b{2}c{2}d{2}e{2}f{2}g{2}h{2}i{2}`},
			"invalid":            {`[a-`},
		} {
			if err := compile(patterns...); !errors.Is(err, ErrInvalid) {
				t.Errorf("%s: accepted (err %v)", name, err)
			}
		}
		for name, patterns := range map[string][]string{
			"literal":        {`PROJECT-[0-9]{3}`},
			"bounded dot":    {`a.{0,5}b`},
			"alternation":    {`(red|green)-[0-9]+`},
			"plain star":     {`ab*c`},
			"max length":     {strings.Repeat("a", 256)},
			"max count":      many[:32],
			"escaped dot":    {`v1\.0`},
			"unicode":        {`مشروع-[0-9]+`},
			"escaped parens": {`\(\?x`},
		} {
			if err := compile(patterns...); err != nil {
				t.Errorf("%s: refused: %v", name, err)
			}
		}
	})

	t.Run("a refused definition is not stored", func(t *testing.T) {
		store := newMemStore()
		svc := &Service{Store: store, Registry: NewRegistry(), Authority: fixtureAuthority{workspace: true}}
		admin := Actor{Tenant: "t", Subject: "admin"}
		for _, bad := range []string{`(a+)+$`, `.*`, `(?=x)`, `(a)\1`, ``, strings.Repeat("a", 300)} {
			err := svc.CreateVersion(t.Context(), admin, custom("bad", "pattern", "block", bad))
			if !errors.Is(err, ErrInvalid) {
				t.Errorf("%q saved: %v", bad, err)
			}
		}
		if len(store.defs["t"]) != 0 {
			t.Fatal("a refused pattern reached the store")
		}
		if err := svc.CreateVersion(t.Context(), admin, custom("good", "pattern", "block", `PROJECT-[0-9]{3}`)); err != nil || len(store.defs["t"]) != 1 {
			t.Fatalf("good pattern: %v", err)
		}
	})

	t.Run("evaluation of accepted patterns is fast on a 32 KB body", func(t *testing.T) {
		patterns := []string{`(a|aa)*b`, `a{1,200}b`, `[a-z]+[0-9]{1,5}x`, `(a|aa|aaa)+c`, `a.{0,30}z`, `a[ab]*c`, `[a-z]*$z`}
		accepted := patterns[:0]
		for _, p := range patterns {
			if p != "" && compile(p) == nil {
				accepted = append(accepted, p)
			}
		}
		if len(accepted) < 5 {
			t.Fatalf("expected most adversarial patterns to be plain, got %v", accepted)
		}
		ev, err := NewRegistry().Compile([]Definition{custom("adv", "pattern", "block", accepted...)})
		if err != nil {
			t.Fatal(err)
		}
		for _, body := range []string{strings.Repeat("a", 32768), strings.Repeat("ab", 16384), strings.Repeat("a1", 16384), strings.Repeat("aaz", 10922) + "aa"} {
			start := time.Now()
			if _, err := ev.Evaluate(Input{Body: body}); err != nil {
				t.Fatal(err)
			}
			if took := time.Since(start); took > 3*time.Second {
				t.Fatalf("linear-time pattern took %s", took)
			}
		}
		many := make([]string, 32)
		for i := range many {
			many[i] = fmt.Sprintf("(a|aa)+%c", 'b'+i%20)
		}
		ev, err = NewRegistry().Compile([]Definition{custom("many", "pattern", "block", many...)})
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		if _, err := ev.Evaluate(Input{Body: strings.Repeat("a", 32768)}); err != nil || time.Since(start) > 3*time.Second {
			t.Fatalf("32 patterns on 32 KB: %v after %s", err, time.Since(start))
		}
		if _, err := ev.Evaluate(Input{Body: strings.Repeat("a", 32769)}); !errors.Is(err, ErrInvalid) {
			t.Fatal("body over 32 KB accepted")
		}
	})

	t.Run("a detector hit never stores the matched secret", func(t *testing.T) {
		secrets := []struct {
			detector, text string
			fragments      []string
		}{
			{"card", "4111 1111 1111 1111", []string{"4111 1111", "1111 1111", "1111111111111111"}},
			{"national-id", "123-45-6789", []string{"123-45", "45-6789", "6789"}},
			{"access-key", "AKIAABCDEFGHIJKLMNOP", []string{"AKIAABCD", "EFGHIJKL", "MNOP"}},
			{"secret", "token=Hunter2HunterTwoSecret", []string{"Hunter2", "HunterTwo", "SecretValue", "Secret"}},
			{"secret", "password: sup3rS3cretPassw0rd", []string{"sup3rS3cret", "Passw0rd"}},
			{"external-link", "https://evil.example/path?key=abc123", []string{"evil.example", "key=abc123", "abc123"}},
		}
		for _, action := range []string{"flag", "mask", "block", "notify"} {
			for _, s := range secrets {
				store := newMemStore()
				delivery := &recordingDelivery{}
				def := custom("det", "detector", action, s.detector)
				if s.detector == "external-link" {
					def.Match = []string{"external-link", "example.com"}
				}
				store.defs["t"] = []Definition{def}
				store.rawEnable("t", Enablement{RuleID: "det", Enabled: true})
				svc := &Service{Store: store, Registry: NewRegistry(), Delivery: delivery}
				body := "here it is: " + s.text + " end"
				res, err := svc.Evaluate(t.Context(), Input{Tenant: "t", Channel: "c", Subject: "writer", Body: body}, true)
				if err != nil || len(res.Hits) != 1 {
					t.Fatalf("%s/%s: %+v %v", action, s.detector, res, err)
				}
				sum := sha256.Sum256([]byte(s.text))
				h := res.Hits[0]
				if h.Digest != "sha256:"+hex.EncodeToString(sum[:]) || h.Masked != removed || body[h.Span.Start:h.Span.End] != s.text {
					t.Fatalf("%s/%s: hit %+v", action, s.detector, h)
				}
				stored, delivered := store.records(), delivery.records
				if len(stored) != 1 || stored[0].Hit.Digest != h.Digest || stored[0].Hit.Masked != removed {
					t.Fatalf("%s/%s: stored %+v", action, s.detector, stored)
				}
				if action == "flag" || action == "notify" {
					if len(delivered) != 1 || delivered[0].Hit.Digest != h.Digest {
						t.Fatalf("%s/%s: delivered %+v", action, s.detector, delivered)
					}
				}
				var dumps []string
				for _, v := range []any{h, stored, delivered} {
					raw, err := json.Marshal(v)
					if err != nil {
						t.Fatal(err)
					}
					dumps = append(dumps, string(raw), fmt.Sprintf("%+v", v))
				}
				if action == "mask" {
					dumps = append(dumps, res.Masked)
					if res.Masked != "here it is: "+removed+" end" {
						t.Fatalf("masked text %q", res.Masked)
					}
				}
				for _, dump := range dumps {
					for _, secret := range append([]string{s.text}, s.fragments...) {
						if strings.Contains(dump, secret) {
							t.Fatalf("%s/%s: %q is stored in %s", action, s.detector, secret, dump)
						}
					}
				}
			}
		}
	})

	t.Run("detectors", func(t *testing.T) {
		d := func(match ...string) *Evaluator {
			ev, err := NewRegistry().Compile([]Definition{custom("d", "detector", "flag", match...)})
			if err != nil {
				t.Fatal(err)
			}
			return ev
		}
		for _, c := range []struct {
			ev    *Evaluator
			body  string
			count int
		}{
			{d("card"), "4111 1111 1111 1111 and 5500-0000-0000-0004", 2},
			{d("card"), "4111 1111 1111 1112", 0},
			{d("card"), "order 2026100112345", 0},
			{d("national-id"), "123-45-6789 and 12345678901", 2},
			{d("access-key"), "AKIAABCDEFGHIJKLMNOP ASIAABCDEFGHIJKLMNOP AKIAshort", 2},
			{d("secret"), "api_key=abcdefgh1234 and API-KEY: 'zzzzzzzzzz' but token=short", 2},
			{d("external-link", "example.com"), "https://example.com/a http://EXAMPLE.com/b https://other.test/c", 1},
			{d("external-link"), "https://example.com/a", 1},
		} {
			got, err := c.ev.Evaluate(Input{Body: c.body})
			if err != nil || len(got.Hits) != c.count {
				t.Errorf("%q: %d hits (%v), want %d: %v", c.body, len(got.Hits), err, c.count, hitSlices(c.body, got))
			}
		}
		if _, err := NewRegistry().Compile([]Definition{custom("d", "detector", "flag", "unknown-detector")}); !errors.Is(err, ErrInvalid) {
			t.Fatal("unknown detector accepted")
		}
	})
}
