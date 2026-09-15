package replay

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func TestRulesDecisionCandidateRefusesWhatItCannotRecompute(t *testing.T) {
	plan := promotionPlan(t)
	rec := promotionRecord(t, plan)
	art, _ := rec.NodeInput(workflow.PromotionNodeRaiseThreshold, 1)
	decision, _ := plan.Node(workflow.PromotionNodeRaiseThreshold)
	transform, _ := plan.Node(workflow.PromotionNodeBuildProposal)
	cand := RulesDecisionCandidate{Bindings: []RuleTableBinding{PromotionThresholdBinding()}}

	if _, err := cand.Recompute(context.Background(), CandidateRequest{Node: transform, Attempt: 1, Inputs: art}); CodeOf(err) != CodeCandidateFailed {
		t.Fatalf("a TRANSFORM: code = %q (%v), want %s", CodeOf(err), err, CodeCandidateFailed)
	}
	if _, err := (RulesDecisionCandidate{}).Recompute(context.Background(), CandidateRequest{Node: decision, Attempt: 1, Inputs: art}); CodeOf(err) != CodeArtifactUnavailable {
		t.Fatalf("no bindings: code = %q (%v), want %s", CodeOf(err), err, CodeArtifactUnavailable)
	}
	otherRule := decision
	otherDecision := *decision.Decision
	otherDecision.RuleRef = "rules.somewhere.else/v1"
	otherRule.Decision = &otherDecision
	if _, err := cand.Recompute(context.Background(), CandidateRequest{Node: otherRule, Attempt: 1, Inputs: art}); CodeOf(err) != CodeArtifactUnavailable {
		t.Fatalf("a different rule ref: code = %q (%v), want %s", CodeOf(err), err, CodeArtifactUnavailable)
	}

	// A node that declares none of the mapped routes gets UNKNOWN rather than
	// an invented route key.
	undeclared := decision
	undeclared.Routes = []string{"WITHIN_THRESHOLD"}
	out, err := cand.Recompute(context.Background(), CandidateRequest{Node: undeclared, Attempt: 1, Inputs: art})
	if err != nil || out.RouteKey != string(workflow.OutcomeUnknown) {
		t.Fatalf("undeclared route: %+v, %v", out, err)
	}

	// An explicitly unknown budget authority is the table's UNKNOWN_BLOCKED
	// tier, routed UNKNOWN.
	blocked := art.Clone()
	for i := range blocked.Inputs {
		if blocked.Inputs[i].Path == rules.ColumnBudgetAuthority {
			blocked.Inputs[i].Value = string(rules.BudgetAuthorityUnknown)
		}
	}
	out, err = cand.Recompute(context.Background(), CandidateRequest{Node: decision, Attempt: 1, Inputs: blocked})
	if err != nil || out.RouteKey != string(workflow.OutcomeUnknown) {
		t.Fatalf("unknown budget: %+v, %v", out, err)
	}

	// A table with no matching row is UNKNOWN too, never a default route.
	gap := PromotionThresholdBinding()
	gap.Table.Rows = gap.Table.Rows[:1] // only the unknown-budget row
	digest, err := gap.Table.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	gapped := art.Clone()
	gapped.Versions = []runtime.PinnedArtifactVersion{{Kind: runtime.PinnedVersionRuleTable, Ref: gap.Table.ID, Version: gap.Table.Version, Digest: digest}}
	out, err = (RulesDecisionCandidate{Bindings: []RuleTableBinding{gap}}).Recompute(context.Background(),
		CandidateRequest{Node: decision, Attempt: 1, Inputs: gapped})
	if err != nil || out.RouteKey != string(workflow.OutcomeUnknown) {
		t.Fatalf("no matching row: %+v, %v", out, err)
	}

	// A binding whose table does not validate cannot be cited.
	broken := PromotionThresholdBinding()
	broken.Table.HitPolicy = rules.HitPolicyUnspecified
	if _, err := (RulesDecisionCandidate{Bindings: []RuleTableBinding{broken}}).Recompute(context.Background(),
		CandidateRequest{Node: decision, Attempt: 1, Inputs: art}); CodeOf(err) != CodeCandidateFailed {
		t.Fatalf("invalid table: code = %q (%v), want %s", CodeOf(err), err, CodeCandidateFailed)
	}
}

func TestParseRuleValueIsExact(t *testing.T) {
	for _, tc := range []struct {
		kind rules.Kind
		text string
		ok   bool
		want string
	}{
		{rules.KindDecimal, "10.5000", true, "10.5000"},
		{rules.KindDecimal, "10.50001", false, ""},
		{rules.KindDecimal, "ten", false, ""},
		{rules.KindBool, "true", true, "true"},
		{rules.KindBool, "false", true, "false"},
		{rules.KindBool, "1", false, ""},
		{rules.KindBool, "maybe", false, ""},
		{rules.KindInt, "42", true, "42"},
		{rules.KindInt, "4.2", false, ""},
		{rules.KindString, "IN_BAND", true, "IN_BAND"},
	} {
		v, err := parseRuleValue(tc.kind, tc.text, rules.IncreasePercentScale)
		if (err == nil) != tc.ok {
			t.Errorf("%s %q: err = %v, want ok=%v", tc.kind, tc.text, err, tc.ok)
			continue
		}
		if tc.ok && v.String() != tc.want {
			t.Errorf("%s %q parsed to %q, want %q", tc.kind, tc.text, v.String(), tc.want)
		}
	}
}
