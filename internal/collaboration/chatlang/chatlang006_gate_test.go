package chatlang

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

var gatePairs = []GatePair{{"en", "de"}, {"en", "fr"}, {"de", "fr"}}

// TestTodo_CHATLANG_006: the gate decides per pair. An engine that keeps every
// protected item and its meaning earns "offer"; one that loses numbers, answers
// the same text for a sentence and its opposite, does not vouch for the meaning
// or does not answer is withheld; the decision table follows the pass marks.
func TestTodo_CHATLANG_006(t *testing.T) {
	ctx := context.Background()
	good, err := RunGate(ctx, &FixtureEngine{}, Glossary{}, gatePairs)
	if err != nil || len(good) != len(gatePairs) {
		t.Fatalf("%v %v", good, err)
	}
	for _, r := range good {
		if r.Decision != GateOffer || r.Passed != r.Cases || r.Cases == 0 || len(r.Failures) != 0 {
			t.Errorf("an engine that keeps everything: %+v", r)
		}
	}
	// de to fr has fewer cases than en to de: only the cases of the pair's source.
	if good[0].Cases <= good[2].Cases || good[2].Cases != 3 {
		t.Errorf("cases per pair: %d %d", good[0].Cases, good[2].Cases)
	}

	constant := &FixtureEngine{Mutate: func(Request, string) string {
		return "Ja, das ist in Ordnung so, bitte weitermachen und danke schön für alles."
	}}
	if bad, _ := RunGate(ctx, constant, Glossary{}, gatePairs[:1]); bad[0].Decision != GateWithhold || !strings.Contains(strings.Join(bad[0].Failures, "\n"), "same text as its opposite") {
		t.Errorf("an engine that answers one text for everything: %+v", bad[0])
	}
	dropping := &FixtureEngine{Mutate: func(_ Request, out string) string { return markerPattern.ReplaceAllString(out, "") }}
	if bad, _ := RunGate(ctx, dropping, Glossary{}, gatePairs[:1]); bad[0].Decision != GateWithhold || bad[0].Passed == bad[0].Cases {
		t.Errorf("an engine that loses protected items: %+v", bad[0])
	}
	if down, _ := RunGate(ctx, &FixtureEngine{Fail: ErrUnavailable}, Glossary{}, gatePairs[:1]); down[0].Decision != GateWithhold || down[0].Passed != 0 {
		t.Errorf("an engine that does not answer: %+v", down[0])
	}
	unsure := engineFunc(func(ctx context.Context, r Request) (Response, error) {
		response, err := (&FixtureEngine{}).Translate(ctx, r)
		response.MeaningChecked, response.MeaningPreserved = true, false
		return response, err
	})
	if bad, _ := RunGate(ctx, unsure, Glossary{}, gatePairs[:1]); bad[0].Decision != GateWithhold {
		t.Errorf("an engine that does not vouch for the meaning: %+v", bad[0])
	}
	if _, err := RunGate(ctx, nil, Glossary{}, gatePairs); !errors.Is(err, ErrUnavailable) {
		t.Errorf("no engine: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := RunGate(cancelled, &FixtureEngine{}, Glossary{}, gatePairs); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled gate: %v", err)
	}
	for _, c := range []struct {
		cases, passed int
		want          GateDecision
	}{{0, 0, GateWithhold}, {10, 10, GateOffer}, {10, 9, GateOffer}, {10, 8, GateOfferMarked}, {10, 7, GateOfferMarked}, {10, 6, GateWithhold}, {10, 0, GateWithhold}} {
		if got := GateDecisionFor(c.cases, c.passed); got != c.want {
			t.Errorf("%d of %d = %s, want %s", c.passed, c.cases, got, c.want)
		}
	}
	if (GateResult{}).Share() != 0 || (GateResult{Cases: 4, Passed: 3}).Share() != 0.75 {
		t.Error("share")
	}
}

type engineFunc func(context.Context, Request) (Response, error)

func (f engineFunc) Translate(ctx context.Context, r Request) (Response, error) { return f(ctx, r) }

// TestTodo_CHATLANG_006_Golden pins the labelled set: every case, with its
// category, source, text, what must survive and its opposite. Changing the set
// changes what a pair has to pass, so it is a visible change here.
func TestTodo_CHATLANG_006_Golden(t *testing.T) {
	cases := GateCases()
	raw, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	const golden = "aae8672b20e2023eac2dd35386fca594eb53369a8309b85d9a0d344903670744"
	if got := hex.EncodeToString(sum[:]); got != golden {
		t.Fatalf("the labelled set changed: sha256 %s", got)
	}
	seen := map[string]bool{}
	categories := map[GateCategory]bool{}
	for _, c := range cases {
		if seen[c.ID] {
			t.Errorf("duplicate case %s", c.ID)
		}
		seen[c.ID] = true
		categories[c.Category] = true
		for _, keep := range c.Keep {
			if !strings.Contains(c.Text, keep) {
				t.Errorf("%s: %q is not in its own text", c.ID, keep)
			}
		}
	}
	for _, c := range cases {
		if c.Opposite != "" && !seen[c.Opposite] {
			t.Errorf("%s names a missing opposite %s", c.ID, c.Opposite)
		}
	}
	for _, want := range []GateCategory{GateNegation, GateDeadline, GateQuantity, GateSafety, GateLegal, GateName} {
		if !categories[want] {
			t.Errorf("the set has no %s case", want)
		}
	}
}
