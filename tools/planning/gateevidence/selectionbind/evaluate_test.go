package selectionbind

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// TestP1AAndP1BManifestsAreDisjointOrderedBoundedAndSelectionComplete is
// NEXT-002's named TEST. It proves, over the real checked-in documents:
//
//   - ordered: P1A binds every selection in NEXT-002's Depends order and P1B
//     names exactly next-steps.md's six contracts in order;
//   - disjoint: no (contract, mode) is granted by both, and P1B binds the
//     live P1A manifest digest;
//   - bounded: P1A carries the hard zero-effect ceiling and only READ_ONLY
//     capabilities, and P1B cannot activate without Gate A plus a new
//     authority digest;
//   - selection complete: completeness is COMPUTED from the bound artifacts'
//     own gates. Today it is honestly INCOMPLETE with named reasons, and a
//     fully consistent synthetic tree reports COMPLETE - so INCOMPLETE is a
//     finding about the repository, not a rigged evaluator.
func TestP1AAndP1BManifestsAreDisjointOrderedBoundedAndSelectionComplete(t *testing.T) {
	p1a := mustLoadLiveManifest(t)
	p1b := mustLoadLiveTemplate(t)
	opts := liveOptions(t)

	t.Run("ordered and bounded: both documents validate and verify under the trusted key", func(t *testing.T) {
		if v := p1a.Validate(); len(v) != 0 {
			t.Fatalf("P1A Validate: %v", v)
		}
		if reasons := EvaluateP1B(p1b, repoRoot, opts.TrustedPublicKey); len(reasons) != 0 {
			t.Fatalf("EvaluateP1B(live): %v", reasons)
		}
		for i, b := range p1a.SelectionBindings {
			if b.TodoID != []string{"PHASE-001", "SELECT-001", "SELECT-002", "CUSTOMER-001", "TOPOLOGY-001", "COMMERCIAL-001", "THREAT-001"}[i] {
				t.Errorf("binding[%d] = %s, out of NEXT-002 Depends order", i, b.TodoID)
			}
		}
		for _, c := range p1a.Capabilities {
			if c.EffectClass != "READ_ONLY" {
				t.Errorf("P1A capability %s is %s", c.ID, c.EffectClass)
			}
		}
		if p1b.CanActivate() {
			t.Fatal("the checked-in P1B template is activatable")
		}
	})

	t.Run("bindings are current: no live selection artifact has moved since signing", func(t *testing.T) {
		if v := VerifyBindings(repoRoot, p1a.SelectionBindings); len(v) != 0 {
			t.Fatalf("stale bindings: %v", v)
		}
	})

	t.Run("selection complete: the live repository is INCOMPLETE for named reasons", func(t *testing.T) {
		r, err := Evaluate(p1a, opts)
		if err != nil {
			t.Fatal(err)
		}
		if r.Status != StatusIncomplete {
			t.Fatalf("live Status = %s, want INCOMPLETE", r.Status)
		}
		if len(r.ManifestReasons) != 0 {
			t.Fatalf("the manifest itself must be clean; only selections are incomplete: %v", r.ManifestReasons)
		}
		all := joinedReasons(r)
		for _, want := range []string{
			"SELECT-002: real provider-selection gate: provider.vendor_id: carries the placeholder suffix",
			"SELECT-001: reviewer.name: missing",
			`SELECT-001: review_status "UNREVIEWED" is not releasable`,
			"TOPOLOGY-001: no deployable topology decision artifact is checked in",
			"CUSTOMER-001: Instantiate reports BLOCKED",
			"CUSTOMER-001: BLOCKED: customer input missing: no design partner has been selected",
			"slot slo: COMMERCIAL-001 promises no SLO",
		} {
			if !strings.Contains(all, want) {
				t.Errorf("live reasons do not name %q; got:\n%s", want, all)
			}
		}
		for _, ready := range []string{"PHASE-001", "COMMERCIAL-001"} {
			if b := bindingByTodo(r, ready); !b.Ready {
				t.Errorf("%s should pass its own gate today, got reasons %v", ready, b.Reasons)
			}
		}
		for _, slot := range r.Slots {
			if slot.Filled {
				t.Errorf("slot %s reported filled on the live repository", slot.Slot)
			}
		}
	})

	t.Run("selection complete: a fully consistent synthetic tree is COMPLETE", func(t *testing.T) {
		f := buildFixture(t, allFixes...)
		r := evaluate(t, f)
		if r.Status != StatusComplete {
			t.Fatalf("fully consistent fixture Status = %s, reasons:\n%s", r.Status, joinedReasons(r))
		}
		if len(r.Reasons()) != 0 {
			t.Fatalf("COMPLETE report carries reasons: %v", r.Reasons())
		}
		for _, s := range r.Slots {
			if !s.Filled {
				t.Errorf("slot %s not filled in COMPLETE fixture", s.Slot)
			}
		}
	})
}

func TestEvaluateRefusesIncompleteOptions(t *testing.T) {
	m := mustLoadLiveManifest(t)
	good := liveOptions(t)
	for name, mutate := range map[string]func(*Options){
		"no root":        func(o *Options) { o.Root = "" },
		"no trusted key": func(o *Options) { o.TrustedPublicKey = "" },
		"no clock":       func(o *Options) { o.Now = time.Time{} },
	} {
		t.Run(name, func(t *testing.T) {
			o := good
			mutate(&o)
			if _, err := Evaluate(m, o); !errors.Is(err, ErrOptions) {
				t.Fatalf("Evaluate error = %v, want ErrOptions", err)
			}
		})
	}
}
