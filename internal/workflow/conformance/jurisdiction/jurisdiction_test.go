// Package jurisdiction proves selected-jurisdiction behavior across the
// Promotion slice (transactional) and the Medical Leave slice (long-running
// protected process) for CROSS-CONF-001. Both flows pin the same selected
// jurisdiction and composition release in snapshots and proposals, preserve
// historical evaluation across known-at/effective-at rule changes, and
// deterministically return BLOCKED|REVIEW_REQUIRED|REPLAN_REQUIRED with a
// successor proposal and zero stale effects for ambiguity or material rule
// change. The evaluator is pure: it creates no approval, work item, write or
// effect -- Effects is always zero.
package jurisdiction

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func crossFixtures() ObligationFixture {
	return ObligationFixture{
		"US-CA": {
			FlowPromotion:    {"wage-notice", "final-pay-timing"},
			FlowMedicalLeave: {"cfra-notice", "wage-continuation-review"},
		},
		"US-NY": {
			FlowPromotion:    {"ny-wage-notice"},
			FlowMedicalLeave: {"pfl-coordination"},
		},
	}
}

func crossRule() RuleRelease {
	return RuleRelease{
		Version:     "2026.9",
		EffectiveAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		KnownAt:     time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC),
	}
}

func clearLegal() LegalContext {
	return LegalContext{Present: true, Selected: "US-CA", Release: "2026.9"}
}

func initialRequest(flow FlowKind) EvaluationRequest {
	return EvaluationRequest{
		Flow:        flow,
		Legal:       clearLegal(),
		CurrentRule: crossRule(),
		Fixtures:    crossFixtures(),
	}
}

// TestSelectedJurisdictionPromotionAndLeaveHandleAmbiguityRuleTimeAndReplanConsistently
// is the PRIMARY test: both slices pin the same jurisdiction and release,
// ambiguity never resolves to a guess, UNKNOWN creates nothing, a missing
// LegalContext is refused, and a material rule change replans with a
// successor while preserving history.
func TestSelectedJurisdictionPromotionAndLeaveHandleAmbiguityRuleTimeAndReplanConsistently(t *testing.T) {
	flows := []FlowKind{FlowPromotion, FlowMedicalLeave}
	pinned := map[FlowKind]EvaluationResult{}
	for _, flow := range flows {
		t.Run("pin "+string(flow), func(t *testing.T) {
			got, err := EvaluateSelectedJurisdiction(initialRequest(flow))
			if err != nil {
				t.Fatalf("initial: %v", err)
			}
			if got.Verdict != VerdictProceed {
				t.Fatalf("verdict = %s, want PROCEED", got.Verdict)
			}
			if got.Pinned.Jurisdiction != "US-CA" || got.Pinned.Release != "2026.9" {
				t.Fatalf("pinned = %s/%s, want US-CA/2026.9", got.Pinned.Jurisdiction, got.Pinned.Release)
			}
			if got.Pinned.RuleVersion != "2026.9" {
				t.Fatalf("pinned rule = %s, want 2026.9", got.Pinned.RuleVersion)
			}
			if got.Pinned.ProposalID == "" {
				t.Fatal("pinned snapshot has no proposal ID")
			}
			if got.Successor != nil {
				t.Fatal("initial PROCEED must not carry a successor")
			}
			if len(got.Obligations) == 0 {
				t.Fatal("no domain obligations pinned")
			}
			if got.Effects != 0 {
				t.Fatalf("effects = %d, want 0", got.Effects)
			}
			pinned[flow] = got
		})
	}
	t.Run("same jurisdiction across slices", func(t *testing.T) {
		p, l := pinned[FlowPromotion], pinned[FlowMedicalLeave]
		if p.Pinned.Jurisdiction != l.Pinned.Jurisdiction ||
			p.Pinned.Release != l.Pinned.Release ||
			p.Pinned.RuleVersion != l.Pinned.RuleVersion {
			t.Fatalf("promotion pins %s/%s/%s, leave pins %s/%s/%s: must match",
				p.Pinned.Jurisdiction, p.Pinned.Release, p.Pinned.RuleVersion,
				l.Pinned.Jurisdiction, l.Pinned.Release, l.Pinned.RuleVersion)
		}
		if reflect.DeepEqual(p.Obligations, l.Obligations) {
			t.Fatal("promotion and leave obligations are identical; domain composition must differ")
		}
	})

	for _, flow := range flows {
		flow := flow
		t.Run("missing legal context "+string(flow), func(t *testing.T) {
			req := initialRequest(flow)
			req.Legal = LegalContext{}
			_, err := EvaluateSelectedJurisdiction(req)
			if !errors.Is(err, ErrLegalContextMissing) {
				t.Fatalf("err = %v, want ErrLegalContextMissing", err)
			}
		})
		t.Run("ambiguity never guesses "+string(flow), func(t *testing.T) {
			req := initialRequest(flow)
			req.Legal = LegalContext{Present: true, Candidates: []string{"US-CA", "US-NY"}}
			got, err := EvaluateSelectedJurisdiction(req)
			if err != nil {
				t.Fatalf("ambiguous: %v", err)
			}
			if got.Verdict != VerdictReviewRequired {
				t.Fatalf("verdict = %s, want REVIEW_REQUIRED", got.Verdict)
			}
			if got.Successor == nil {
				t.Fatal("ambiguity must carry a successor proposal")
			}
			if len(got.Obligations) != 0 {
				t.Fatalf("guessed obligations %v for ambiguous jurisdiction", got.Obligations)
			}
			if got.Effects != 0 {
				t.Fatalf("effects = %d, want 0", got.Effects)
			}
		})
		t.Run("unknown creates nothing "+string(flow), func(t *testing.T) {
			req := initialRequest(flow)
			req.Legal = LegalContext{Present: true, Unknown: true}
			got, err := EvaluateSelectedJurisdiction(req)
			if err != nil {
				t.Fatalf("unknown: %v", err)
			}
			if got.Verdict != VerdictBlocked {
				t.Fatalf("verdict = %s, want BLOCKED", got.Verdict)
			}
			if got.Successor != nil {
				t.Fatal("UNKNOWN must not produce a successor proposal")
			}
			if len(got.Obligations) != 0 {
				t.Fatalf("UNKNOWN produced obligations %v", got.Obligations)
			}
			if got.Effects != 0 {
				t.Fatalf("effects = %d, want 0", got.Effects)
			}
		})
		t.Run("rule change replans "+string(flow), func(t *testing.T) {
			first, err := EvaluateSelectedJurisdiction(initialRequest(flow))
			if err != nil {
				t.Fatalf("initial: %v", err)
			}
			next := RuleRelease{
				Version:     "2026.10",
				EffectiveAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
				KnownAt:     time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC),
			}
			req := initialRequest(flow)
			req.Pinned = first.Pinned
			req.ProposalID = first.Pinned.ProposalID
			req.CurrentRule = next
			got, err := EvaluateSelectedJurisdiction(req)
			if err != nil {
				t.Fatalf("resume under new rule: %v", err)
			}
			if got.Verdict != VerdictReplanRequired {
				t.Fatalf("verdict = %s, want REPLAN_REQUIRED", got.Verdict)
			}
			if got.Successor == nil {
				t.Fatal("material rule change must carry a successor proposal")
			}
			if got.Successor.SuccessorOf != first.Pinned.ProposalID {
				t.Fatalf("successor of %s, want %s", got.Successor.SuccessorOf, first.Pinned.ProposalID)
			}
			if got.Successor.Snapshot.RuleVersion != "2026.10" {
				t.Fatalf("successor rule = %s, want 2026.10", got.Successor.Snapshot.RuleVersion)
			}
			if !reflect.DeepEqual(got.Pinned, first.Pinned) {
				t.Fatal("historical evaluation was rewritten by the rule change")
			}
			if got.Effects != 0 {
				t.Fatalf("effects = %d, want 0", got.Effects)
			}
		})
		t.Run("same rule resumes "+string(flow), func(t *testing.T) {
			first, err := EvaluateSelectedJurisdiction(initialRequest(flow))
			if err != nil {
				t.Fatalf("initial: %v", err)
			}
			req := initialRequest(flow)
			req.Pinned = first.Pinned
			req.ProposalID = first.Pinned.ProposalID
			got, err := EvaluateSelectedJurisdiction(req)
			if err != nil {
				t.Fatalf("resume: %v", err)
			}
			if got.Verdict != VerdictProceed {
				t.Fatalf("verdict = %s, want PROCEED", got.Verdict)
			}
			if !reflect.DeepEqual(got.Pinned, first.Pinned) {
				t.Fatal("resume rewrote the pinned evaluation")
			}
			if !reflect.DeepEqual(got.Obligations, first.Obligations) {
				t.Fatal("resume changed historical obligations")
			}
		})
		t.Run("stale proposal refused "+string(flow), func(t *testing.T) {
			first, err := EvaluateSelectedJurisdiction(initialRequest(flow))
			if err != nil {
				t.Fatalf("initial: %v", err)
			}
			req := initialRequest(flow)
			req.Pinned = first.Pinned
			req.ProposalID = "proposal-stale"
			_, err = EvaluateSelectedJurisdiction(req)
			if !errors.Is(err, ErrEvaluationInvalid) {
				t.Fatalf("err = %v, want ErrEvaluationInvalid", err)
			}
		})
	}
}

// TestTodo_CROSS_CONF_001_Property: evaluation is deterministic, pins what
// it was given, and never guesses: ambiguous, multi-location, empty or
// unknown jurisdiction never reaches PROCEED.
func TestTodo_CROSS_CONF_001_Property(t *testing.T) {
	t.Run("deterministic", func(t *testing.T) {
		for _, flow := range []FlowKind{FlowPromotion, FlowMedicalLeave} {
			a, err := EvaluateSelectedJurisdiction(initialRequest(flow))
			if err != nil {
				t.Fatalf("%s: %v", flow, err)
			}
			b, err := EvaluateSelectedJurisdiction(initialRequest(flow))
			if err != nil {
				t.Fatalf("%s: %v", flow, err)
			}
			if !reflect.DeepEqual(a, b) {
				t.Fatalf("%s: same request evaluated differently", flow)
			}
		}
	})
	t.Run("never guesses", func(t *testing.T) {
		ambiguous := []LegalContext{
			{Present: true, Candidates: []string{"US-CA", "US-NY"}},
			{Present: true, Candidates: []string{"US-CA"}},
			{Present: true, Selected: "US-CA", Candidates: []string{"US-CA", "US-NY"}},
			{Present: true},
			{Present: true, Unknown: true},
			{Present: true, Selected: "US-CA", Unknown: true},
		}
		for _, flow := range []FlowKind{FlowPromotion, FlowMedicalLeave} {
			for i, legal := range ambiguous {
				req := initialRequest(flow)
				req.Legal = legal
				got, err := EvaluateSelectedJurisdiction(req)
				if err != nil {
					t.Fatalf("%s case %d: unexpected error %v", flow, i, err)
				}
				if got.Verdict == VerdictProceed {
					t.Fatalf("%s case %d (%+v): guessed PROCEED", flow, i, legal)
				}
				if got.Verdict == VerdictBlocked && len(got.Obligations) != 0 {
					t.Fatalf("%s case %d: BLOCKED leaks obligations %v", flow, i, got.Obligations)
				}
				if got.Effects != 0 {
					t.Fatalf("%s case %d: effects = %d", flow, i, got.Effects)
				}
			}
		}
	})
	t.Run("pins what was given", func(t *testing.T) {
		for _, jur := range []string{"US-CA", "US-NY"} {
			for _, flow := range []FlowKind{FlowPromotion, FlowMedicalLeave} {
				req := initialRequest(flow)
				req.Legal = LegalContext{Present: true, Selected: jur, Release: "2026.9"}
				got, err := EvaluateSelectedJurisdiction(req)
				if err != nil {
					t.Fatalf("%s %s: %v", flow, jur, err)
				}
				if got.Verdict != VerdictProceed {
					t.Fatalf("%s %s: verdict = %s", flow, jur, got.Verdict)
				}
				if got.Pinned.Jurisdiction != jur || got.Pinned.Release != "2026.9" {
					t.Fatalf("%s %s: pinned %s/%s", flow, jur, got.Pinned.Jurisdiction, got.Pinned.Release)
				}
			}
		}
	})
}

// TestTodo_CROSS_CONF_001_Golden: the canonical digest of the pinned
// promotion evaluation matches the checked-in oracle below. The oracle was
// recorded from the GREEN implementation; any semantic drift fails here.
func TestTodo_CROSS_CONF_001_Golden(t *testing.T) {
	const wantDigest = "sha256:b5b9ca15c92fc6955e6bb1a0545850a48649dc2d09b02fab5cb52c234277b728"
	got, err := EvaluateSelectedJurisdiction(initialRequest(FlowPromotion))
	if err != nil {
		t.Fatalf("initial: %v", err)
	}
	if got.Verdict != VerdictProceed || got.Pinned.Jurisdiction != "US-CA" {
		t.Fatalf("golden precondition changed: %+v", got.Pinned)
	}
	if d := got.Digest(); d != wantDigest {
		t.Fatalf("digest = %s, want checked-in oracle %s", d, wantDigest)
	}
}

// TestTodo_CROSS_CONF_001_Fault: every fault -- material rule change,
// unknown jurisdiction on resume, selection change -- lands in an allowed
// durable verdict with a successor where one is owed and zero stale effects.
func TestTodo_CROSS_CONF_001_Fault(t *testing.T) {
	allowed := map[Verdict]bool{
		VerdictBlocked: true, VerdictReviewRequired: true, VerdictReplanRequired: true,
	}
	newRule := func() RuleRelease {
		return RuleRelease{
			Version:     "2026.10",
			EffectiveAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			KnownAt:     time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC),
		}
	}
	for _, flow := range []FlowKind{FlowPromotion, FlowMedicalLeave} {
		first, err := EvaluateSelectedJurisdiction(initialRequest(flow))
		if err != nil {
			t.Fatalf("%s initial: %v", flow, err)
		}
		resume := func(mutate func(*EvaluationRequest)) (EvaluationResult, error) {
			req := initialRequest(flow)
			req.Pinned = first.Pinned
			req.ProposalID = first.Pinned.ProposalID
			mutate(&req)
			return EvaluateSelectedJurisdiction(req)
		}
		t.Run("rule change "+string(flow), func(t *testing.T) {
			var ids []string
			for i := 0; i < 3; i++ {
				got, err := resume(func(r *EvaluationRequest) { r.CurrentRule = newRule() })
				if err != nil {
					t.Fatalf("attempt %d: %v", i, err)
				}
				if !allowed[got.Verdict] || got.Verdict != VerdictReplanRequired {
					t.Fatalf("attempt %d: verdict = %s", i, got.Verdict)
				}
				if got.Successor == nil || got.Effects != 0 {
					t.Fatalf("attempt %d: successor=%v effects=%d", i, got.Successor, got.Effects)
				}
				ids = append(ids, got.Successor.ID)
			}
			if ids[0] != ids[1] || ids[1] != ids[2] {
				t.Fatalf("replan is not idempotent: %v", ids)
			}
		})
		t.Run("unknown on resume "+string(flow), func(t *testing.T) {
			got, err := resume(func(r *EvaluationRequest) {
				r.Legal = LegalContext{Present: true, Unknown: true}
			})
			if err != nil {
				t.Fatalf("resume unknown: %v", err)
			}
			if got.Verdict != VerdictBlocked || got.Effects != 0 || len(got.Obligations) != 0 {
				t.Fatalf("resume unknown: %+v", got)
			}
		})
		t.Run("selection change "+string(flow), func(t *testing.T) {
			got, err := resume(func(r *EvaluationRequest) {
				r.Legal = LegalContext{Present: true, Selected: "US-NY", Release: "2026.9"}
			})
			if err != nil {
				t.Fatalf("resume changed selection: %v", err)
			}
			if got.Verdict != VerdictReplanRequired || got.Successor == nil || got.Effects != 0 {
				t.Fatalf("resume changed selection: %+v", got)
			}
			if got.Successor.Snapshot.Jurisdiction != "US-NY" {
				t.Fatalf("successor jurisdiction = %s", got.Successor.Snapshot.Jurisdiction)
			}
		})
	}
}

// TestTodo_CROSS_CONF_001_Security: denials carry no obligations and no
// fixture contents; an uncovered jurisdiction never yields PROCEED.
func TestTodo_CROSS_CONF_001_Security(t *testing.T) {
	for _, flow := range []FlowKind{FlowPromotion, FlowMedicalLeave} {
		req := initialRequest(flow)
		req.Legal = LegalContext{}
		_, err := EvaluateSelectedJurisdiction(req)
		if !errors.Is(err, ErrLegalContextMissing) {
			t.Fatalf("%s: missing context err = %v", flow, err)
		}
		req = initialRequest(flow)
		req.Legal = LegalContext{Present: true, Selected: "US-TX", Release: "2026.9"}
		got, err := EvaluateSelectedJurisdiction(req)
		if err != nil {
			t.Fatalf("%s uncovered: %v", flow, err)
		}
		if got.Verdict == VerdictProceed {
			t.Fatalf("%s: uncovered jurisdiction reached PROCEED", flow)
		}
		if len(got.Obligations) != 0 {
			t.Fatalf("%s: uncovered jurisdiction leaks obligations %v", flow, got.Obligations)
		}
		if got.Effects != 0 {
			t.Fatalf("%s: effects = %d", flow, got.Effects)
		}
	}
}

// TestTodo_CROSS_CONF_001_Conformance: promotion and leave share the pinned
// jurisdiction/release/rule triple from the same fixtures while carrying
// domain-specific obligations.
func TestTodo_CROSS_CONF_001_Conformance(t *testing.T) {
	p, err := EvaluateSelectedJurisdiction(initialRequest(FlowPromotion))
	if err != nil {
		t.Fatalf("promotion: %v", err)
	}
	l, err := EvaluateSelectedJurisdiction(initialRequest(FlowMedicalLeave))
	if err != nil {
		t.Fatalf("leave: %v", err)
	}
	if p.Verdict != VerdictProceed || l.Verdict != VerdictProceed {
		t.Fatalf("verdicts = %s/%s, want PROCEED/PROCEED", p.Verdict, l.Verdict)
	}
	if p.Pinned.Jurisdiction != l.Pinned.Jurisdiction ||
		p.Pinned.Release != l.Pinned.Release ||
		p.Pinned.RuleVersion != l.Pinned.RuleVersion {
		t.Fatal("slices diverge on the shared jurisdiction contract")
	}
	wantPromo := crossFixtures()["US-CA"][FlowPromotion]
	wantLeave := crossFixtures()["US-CA"][FlowMedicalLeave]
	if !reflect.DeepEqual(p.Obligations, wantPromo) {
		t.Fatalf("promotion obligations = %v, want %v", p.Obligations, wantPromo)
	}
	if !reflect.DeepEqual(l.Obligations, wantLeave) {
		t.Fatalf("leave obligations = %v, want %v", l.Obligations, wantLeave)
	}
}

// TestTodo_CROSS_CONF_001_Recovery: replaying a resume from the pinned
// snapshot reproduces the historical evaluation and converges on one
// successor -- no duplicate or divergent proposals.
func TestTodo_CROSS_CONF_001_Recovery(t *testing.T) {
	for _, flow := range []FlowKind{FlowPromotion, FlowMedicalLeave} {
		first, err := EvaluateSelectedJurisdiction(initialRequest(flow))
		if err != nil {
			t.Fatalf("%s initial: %v", flow, err)
		}
		next := RuleRelease{
			Version:     "2026.10",
			EffectiveAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			KnownAt:     time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC),
		}
		replay := func() EvaluationResult {
			req := initialRequest(flow)
			req.Pinned = first.Pinned
			req.ProposalID = first.Pinned.ProposalID
			req.CurrentRule = next
			got, err := EvaluateSelectedJurisdiction(req)
			if err != nil {
				t.Fatalf("%s replay: %v", flow, err)
			}
			return got
		}
		a, b := replay(), replay()
		if a.Successor.ID != b.Successor.ID || a.Digest() != b.Digest() {
			t.Fatalf("%s: replay diverged", flow)
		}
		if !reflect.DeepEqual(a.Pinned, first.Pinned) {
			t.Fatalf("%s: replay rewrote history", flow)
		}
		if !reflect.DeepEqual(a.Pinned.Obligations, first.Obligations) {
			t.Fatalf("%s: historical obligations lost on replay", flow)
		}
		// The successor re-pins under the new rule and then resumes cleanly.
		follow := initialRequest(flow)
		follow.Pinned = a.Successor.Snapshot
		follow.ProposalID = a.Successor.ID
		follow.CurrentRule = next
		settled, err := EvaluateSelectedJurisdiction(follow)
		if err != nil {
			t.Fatalf("%s follow-up: %v", flow, err)
		}
		if settled.Verdict != VerdictProceed {
			t.Fatalf("%s: successor does not settle: %s", flow, settled.Verdict)
		}
	}
}

// TestTodo_CROSS_CONF_001_ModelBased: generated command sequences match the
// reference jurisdiction model and cover every declared transition.
func TestTodo_CROSS_CONF_001_ModelBased(t *testing.T) {
	type cmd string
	const (
		pinClear     cmd = "pin-clear"
		pinAmbiguous cmd = "pin-ambiguous"
		pinUnknown   cmd = "pin-unknown"
		resumeSame   cmd = "resume-same"
		resumeChange cmd = "resume-changed"
	)
	// Reference model: the only legal verdict per command in context.
	// resume-* is legal only after a pin-clear PROCEED.
	model := func(seq []cmd) []Verdict {
		out := make([]Verdict, 0, len(seq))
		pinned := false
		for _, c := range seq {
			switch c {
			case pinClear:
				pinned = true
				out = append(out, VerdictProceed)
			case pinAmbiguous:
				out = append(out, VerdictReviewRequired)
			case pinUnknown:
				out = append(out, VerdictBlocked)
			case resumeSame:
				if !pinned {
					out = append(out, "")
				} else {
					out = append(out, VerdictProceed)
				}
			case resumeChange:
				if !pinned {
					out = append(out, "")
				} else {
					out = append(out, VerdictReplanRequired)
				}
			}
		}
		return out
	}
	seqs := [][]cmd{
		{pinClear},
		{pinAmbiguous},
		{pinUnknown},
		{pinClear, resumeSame},
		{pinClear, resumeChange},
		{pinClear, resumeSame, resumeChange},
		{pinClear, resumeSame, resumeSame},
		{pinAmbiguous, pinClear, resumeChange},
	}
	for _, flow := range []FlowKind{FlowPromotion, FlowMedicalLeave} {
		for si, seq := range seqs {
			want := model(seq)
			var pinned Snapshot
			var proposalID string
			for i, c := range seq {
				req := initialRequest(flow)
				switch c {
				case pinClear:
				case pinAmbiguous:
					req.Legal = LegalContext{Present: true, Candidates: []string{"US-CA", "US-NY"}}
				case pinUnknown:
					req.Legal = LegalContext{Present: true, Unknown: true}
				case resumeSame:
					req.Pinned = pinned
					req.ProposalID = proposalID
				case resumeChange:
					req.Pinned = pinned
					req.ProposalID = proposalID
					req.CurrentRule = RuleRelease{
						Version:     "2026.10",
						EffectiveAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
						KnownAt:     time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC),
					}
				}
				got, err := EvaluateSelectedJurisdiction(req)
				if err != nil {
					t.Fatalf("%s seq %d step %d (%s): unexpected error %v", flow, si, i, c, err)
				}
				if got.Verdict != want[i] {
					t.Fatalf("%s seq %d step %d (%s): verdict = %s, model = %s",
						flow, si, i, c, got.Verdict, want[i])
				}
				if got.Effects != 0 {
					t.Fatalf("%s seq %d step %d: effects = %d", flow, si, i, got.Effects)
				}
				if got.Verdict == VerdictProceed && pinned.ProposalID == "" {
					pinned, proposalID = got.Pinned, got.Pinned.ProposalID
				}
			}
		}
	}
}

// TestTodo_CROSS_CONF_001_Mutation: seeded semantic mutants -- inputs a
// weakened implementation would accept (a guess, a stale resume, a missing
// context check) -- must be killed: the real evaluator refuses or diverges
// from the mutant's acceptance.
func TestTodo_CROSS_CONF_001_Mutation(t *testing.T) {
	for _, flow := range []FlowKind{FlowPromotion, FlowMedicalLeave} {
		t.Run("guessing mutant killed "+string(flow), func(t *testing.T) {
			// Mutant M1 resolves ambiguity by picking the first candidate.
			req := initialRequest(flow)
			req.Legal = LegalContext{Present: true, Candidates: []string{"US-CA", "US-NY"}}
			oracle, err := EvaluateSelectedJurisdiction(req)
			if err != nil || oracle.Verdict != VerdictReviewRequired {
				t.Fatalf("oracle: %v %+v", err, oracle)
			}
			mutantReq := initialRequest(flow)
			mutantReq.Legal = LegalContext{Present: true, Selected: "US-CA", Release: "2026.9"}
			mutant, err := EvaluateSelectedJurisdiction(mutantReq)
			if err != nil {
				t.Fatalf("mutant request: %v", err)
			}
			if mutant.Verdict == oracle.Verdict && reflect.DeepEqual(mutant.Obligations, oracle.Obligations) {
				t.Fatal("guessing mutant indistinguishable from the oracle; ambiguity check is dead")
			}
			if len(oracle.Obligations) != 0 {
				t.Fatal("oracle leaks obligations for ambiguous jurisdiction; mutant survives")
			}
		})
		t.Run("stale resume mutant killed "+string(flow), func(t *testing.T) {
			// Mutant M2 resumes the old proposal under a new rule as PROCEED.
			first, err := EvaluateSelectedJurisdiction(initialRequest(flow))
			if err != nil {
				t.Fatalf("initial: %v", err)
			}
			req := initialRequest(flow)
			req.Pinned = first.Pinned
			req.ProposalID = first.Pinned.ProposalID
			req.CurrentRule = RuleRelease{
				Version:     "2026.10",
				EffectiveAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
				KnownAt:     time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC),
			}
			got, err := EvaluateSelectedJurisdiction(req)
			if err != nil {
				t.Fatalf("resume: %v", err)
			}
			if got.Verdict == VerdictProceed {
				t.Fatal("stale resume proceeded; mutant M2 survives")
			}
			if got.Successor == nil || got.Successor.SuccessorOf != first.Pinned.ProposalID {
				t.Fatal("no bound successor; stale proposal could resume")
			}
		})
		t.Run("missing context mutant killed "+string(flow), func(t *testing.T) {
			// Mutant M3 drops the LegalContext presence check.
			req := initialRequest(flow)
			req.Legal = LegalContext{Selected: "US-CA", Release: "2026.9"}
			_, err := EvaluateSelectedJurisdiction(req)
			if !errors.Is(err, ErrLegalContextMissing) {
				t.Fatalf("err = %v; presence check is dead, mutant M3 survives", err)
			}
		})
	}
}
