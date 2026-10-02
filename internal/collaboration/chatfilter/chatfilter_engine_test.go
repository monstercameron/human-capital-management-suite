package chatfilter

import (
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func rule(action string) Definition {
	return Definition{ID: action, Name: "Restricted term", Version: "1.0.0", Kind: "words", Match: []string{"quartz"}, Action: action}
}
func TestChatfilterBaseline_Words(t *testing.T) {
	e, err := NewRegistry().Compile([]Definition{rule("block")})
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"QUARTZ", "quártz", "qu4rtz", "q u a r t z", "q.u.a.r.t.z", "q\u200buartz", "qu\u0301artz", "q\u00a0u\u2009artz", "q\x00uartz"} {
		r, err := e.Evaluate(Input{Body: body})
		if err != nil || r.Action != "block" {
			t.Fatalf("%q: %+v %v", body, r, err)
		}
		if r.Hits[0].Span.End != len(body) {
			t.Fatalf("original span: %+v", r.Hits[0])
		}
		var blocked *BlockedError
		if !errors.As(r.Refusal(), &blocked) || !errors.Is(r.Refusal(), ErrBlocked) {
			t.Fatal("typed refusal missing")
		}
	}
	for _, body := range []string{"quartzite", "aquartz", "class assistant Scunthorpe", "klassisch Schifffahrt", "أحماض مخلص"} {
		r, _ := e.Evaluate(Input{Body: body})
		if len(r.Hits) != 0 {
			t.Fatalf("false positive %q", body)
		}
	}
	if len(Builtins()) != 9 {
		t.Fatal("language/category definitions missing")
	}
	d := rule("block")
	d.Match = []string{"straße"}
	d.Language = "de"
	german, err := NewRegistry().Compile([]Definition{d})
	if err != nil {
		t.Fatal(err)
	}
	out, err := german.Evaluate(Input{Body: "STRASSE", Language: "DE-DE"})
	if err != nil || out.Action != "block" {
		t.Fatal("full Unicode case folding missing", err)
	}
	d.Match = []string{"حجر"}
	d.Language = "ar"
	arabic, err := NewRegistry().Compile([]Definition{d})
	if err != nil {
		t.Fatal(err)
	}
	out, err = arabic.Evaluate(Input{Body: "حَجَـر", Language: "ar"})
	if err != nil || out.Action != "block" {
		t.Fatal("Arabic marks and elongation evaded matching", err)
	}
}
func TestChatfilterBaseline_ZeroWidth(t *testing.T) {
	e, _ := NewRegistry().Compile([]Definition{rule("block")})
	for _, insert := range []string{"\u200b", "\u200c", "\u200d", "\u2060", "\ufeff", "\u0301", "\u064e"} {
		for i := 0; i <= len("quartz"); i++ {
			body := "quartz"[:i] + insert + "quartz"[i:]
			r, _ := e.Evaluate(Input{Body: body})
			if r.Action != "block" {
				t.Fatalf("bypass %q", body)
			}
		}
	}
}
func TestTodo_CHATMOD_003(t *testing.T) {
	r := NewRegistry()
	for _, fixture := range []struct {
		kind  string
		match []string
		body  string
		count int
	}{
		{"pattern", []string{`PROJECT-[0-9]{3}`}, "PROJECT-123", 1},
		{"detector", []string{"card"}, "4111 1111 1111 1111", 1},
		{"detector", []string{"card"}, "4111 1111 1111 1112", 0},
		{"detector", []string{"national-id"}, "123-45-6789", 1},
		{"detector", []string{"access-key"}, "AKIAABCDEFGHIJKLMNOP", 1},
		{"detector", []string{"secret"}, "token=neutralplaceholder", 1},
		{"detector", []string{"external-link", "example.com"}, "https://example.com/a https://other.example/b", 1},
		{"attachment", []string{"application/x-executable"}, "", 1},
	} {
		d := rule("mask")
		d.Kind = fixture.kind
		d.Match = fixture.match
		e, err := r.Compile([]Definition{d})
		if err != nil {
			t.Fatal(err)
		}
		result, err := e.Evaluate(Input{Body: fixture.body, AttachmentTypes: []string{"application/x-executable"}})
		if err != nil || len(result.Hits) != fixture.count {
			t.Fatalf("%s %+v %v", fixture.kind, result, err)
		}
		for _, h := range result.Hits {
			if h.Masked != "[removed word]" || !strings.HasPrefix(h.Digest, "sha256:") {
				t.Fatal("unsafe hit")
			}
		}
	}
	if err := r.RegisterKind("fixture", Kind{Schema: "empty", Compile: func(Definition) (Matcher, error) { return func(Input) []Span { return []Span{{0, 1}} }, nil }}); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterAction("custom", Action{Schema: "empty", Rank: 5, Mask: true}); err != nil {
		t.Fatal(err)
	}
	d := rule("mask")
	d.Kind = "fixture"
	d.Action = "custom"
	e, err := r.Compile([]Definition{d})
	if err != nil {
		t.Fatal(err)
	}
	out, _ := e.Evaluate(Input{Body: "x"})
	if out.Action != "custom" || out.Masked != "[removed word]" {
		t.Fatal("registration did not extend evaluator")
	}
}
func TestChatfilterBaseline_RuleOrder(t *testing.T) {
	defs := []Definition{rule("mask"), rule("block"), rule("flag")}
	a, _ := NewRegistry().Compile(defs)
	defs[0], defs[2] = defs[2], defs[0]
	b, _ := NewRegistry().Compile(defs)
	x, _ := a.Evaluate(Input{Body: "quartz"})
	y, _ := b.Evaluate(Input{Body: "quartz"})
	if !reflect.DeepEqual(x, y) || x.Action != "block" {
		t.Fatalf("order dependent: %+v %+v", x, y)
	}
	if got := Mask("abcdef", []Span{{1, 3}, {2, 5}}, "*"); got != "a*f" {
		t.Fatalf("overlap: %s", got)
	}
	if got := Mask("abc", []Span{{5, 1}}, "*"); got != "abc" {
		t.Fatal("invalid span accepted")
	}
}
func TestChatfilterBaseline_Patterns(t *testing.T) {
	for _, pattern := range []string{`(a+)\1`, `(?=secret)`, `.*`, `(a+)+$`, ``} {
		d := rule("block")
		d.Kind = "pattern"
		d.Match = []string{pattern}
		_, err := NewRegistry().Compile([]Definition{d})
		if pattern == `(a+)+$` {
			if err != nil {
				continue
			}
			t.Fatal("nested repetition accepted")
		}
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted %q", pattern)
		}
	}
	d := rule("mask")
	d.ExemptRoles = []string{"reviewer"}
	d.Channels = []string{"c"}
	e, _ := NewRegistry().Compile([]Definition{d})
	for _, in := range []Input{{Body: "quartz", Channel: "other"}, {Body: "quartz", Channel: "c", Direct: true}, {Body: "quartz", Channel: "c", Roles: []string{"reviewer"}}} {
		out, _ := e.Evaluate(in)
		if len(out.Hits) > 0 {
			t.Fatal("scope/exemption ignored")
		}
	}
	_, err := e.Evaluate(Input{Body: strings.Repeat("a", 32769)})
	if !errors.Is(err, ErrInvalid) {
		t.Fatal("unbounded input")
	}
}
func TestChatfilterBaseline_EvaluatorLatency(t *testing.T) {
	e, err := NewRegistry().Compile(Builtins())
	if err != nil {
		t.Fatal(err)
	}
	in := Input{Body: strings.Repeat("A calm ordinary message. ", 8)}
	durations := make([]time.Duration, 100)
	for i := range durations {
		start := time.Now()
		if _, err := e.Evaluate(in); err != nil {
			t.Fatal(err)
		}
		durations[i] = time.Since(start)
	}
	// No external provider or scheduling deadline is involved in evaluation.
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	t.Logf("p95 with every built-in list: %s", durations[94])
	if durations[94] >= 2*time.Millisecond {
		t.Fatalf("p95 exceeds 2ms: %s", durations[94])
	}
}
