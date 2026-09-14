package lineageconformance

import (
	"slices"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/closurewitness"
)

func TestRequiredLinksFollowDefinitionDeclarations(t *testing.T) {
	mutating := intent.Definition{SideEffect: intent.SideEffectInternalMutation, ProposalBindingRule: "exact/v1", CompensationRule: "repair/v1"}
	req, na := RequiredLinks(mutating)
	if !slices.Equal(req, ChainLinks()) || len(na) != 0 {
		t.Fatalf("mutating definition requires %v, not applicable %v", req, na)
	}

	read := intent.Definition{SideEffect: intent.SideEffectReadOnly, ProposalBindingRule: "NOT_APPLICABLE", CompensationRule: "NOT_APPLICABLE"}
	req, na = RequiredLinks(read)
	if !slices.Equal(req, []Link{LinkIntent, LinkWorkflow, LinkEvent, LinkProjection}) {
		t.Fatalf("read-only definition requires %v", req)
	}
	if len(na) != 8 || na[0].Link != LinkProposal || na[0].Reason != "proposal_binding_rule=NOT_APPLICABLE" {
		t.Fatalf("read-only not applicable = %+v", na)
	}

	// A pure calculation that still names a proposal binding and declares a
	// correction rule keeps PROPOSAL and CORRECTION.
	pure := intent.Definition{SideEffect: intent.SideEffectPure, ProposalBindingRule: "result_digest/v1", CompensationRule: "NOT_APPLICABLE", CorrectionRule: "append/v1"}
	req, _ = RequiredLinks(pure)
	if !slices.Contains(req, LinkProposal) || !slices.Contains(req, LinkCorrection) || slices.Contains(req, LinkRepair) || slices.Contains(req, LinkOutbox) {
		t.Fatalf("pure definition requires %v", req)
	}
}

func TestGenerateCasesSortsAndIdentifiesPaths(t *testing.T) {
	parent := intent.Definition{Ref: intent.Ref{TypeID: "z.parent", Version: 1}, SideEffect: intent.SideEffectInternalMutation,
		AllowedInitiators: []intent.Initiator{intent.InitiatorRule, intent.InitiatorIntegration}}
	child := intent.Definition{Ref: intent.Ref{TypeID: "a.child", Version: 1}, SideEffect: intent.SideEffectInternalMutation}
	in := CaseInput{
		Witnesses: closurewitness.Report{Witnesses: []closurewitness.Witness{
			{Definition: "z.parent/v1", Digest: "w1"}, {Definition: "a.child/v1", Digest: "w2"},
		}},
		Definitions: []intent.Definition{parent, child},
		Bindings: []intent.Binding{
			{Definition: parent.Ref, ChildDefinitions: []intent.Ref{child.Ref}},
			{Definition: intent.Ref{TypeID: "not.accepted", Version: 1}, ChildDefinitions: []intent.Ref{child.Ref}},
		},
	}
	cases, findings := GenerateCases(in)
	var ids []string
	for _, c := range cases {
		ids = append(ids, c.ID)
	}
	want := []string{"a.child/v1", "z.parent/v1", ChildCaseID("z.parent/v1", "a.child/v1"), TriggerCaseID("z.parent/v1", "INTEGRATION"), TriggerCaseID("z.parent/v1", "RULE")}
	slices.Sort(want)
	if !slices.Equal(ids, want) || len(findings) != 0 {
		t.Fatalf("cases = %v findings %v, want %v", ids, findings, want)
	}
	childCase := cases[slices.Index(ids, ChildCaseID("z.parent/v1", "a.child/v1"))]
	if childCase.Parent != "z.parent/v1" || childCase.Definition != "a.child/v1" || childCase.WitnessDigest != "w2" || !childCase.requires(LinkCausation) {
		t.Fatalf("child case = %+v", childCase)
	}
}

func TestSortFindingsOrdersByCaseLinkCodeRecord(t *testing.T) {
	f := []Finding{
		{Case: "b", Link: LinkIntent, Code: "X"},
		{Case: "a", Link: LinkCorrection, Code: "X"},
		{Case: "a", Link: LinkCausation, Code: "Z"},
		{Case: "a", Link: LinkCausation, Code: "Y", Record: "r2"},
		{Case: "a", Link: LinkCausation, Code: "Y", Record: "r1"},
	}
	sortFindings(f)
	if f[0].Record != "r1" || f[1].Record != "r2" || f[2].Code != "Z" || f[3].Link != LinkCorrection || f[4].Case != "b" {
		t.Fatalf("sorted = %+v", f)
	}
}
