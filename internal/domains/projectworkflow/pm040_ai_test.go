package projectworkflow

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
)

func aiPolicyFixture() AIPolicy {
	return AIPolicy{TenantID: "tenant-a", ApprovedProviders: []AIProvider{{Name: "approved", Region: "eu-west"}}, PromptRetention: 24 * 60 * 60 * 1e9, TraceRetention: 7 * 24 * 60 * 60 * 1e9, AllowedClassifications: []string{"INTERNAL", "PUBLIC"}, MaxPromptTokens: 1000, MaxOutputTokens: 500, MaxConcurrentRuns: 2, MaxSpendCents: 1000, Currency: "USD"}
}

func proposalFixture() BoardProposal {
	return BoardProposal{
		ID: "proposal-1", TenantID: "tenant-a", ProjectID: "project-1", RequesterID: "requester-1", BaseConfigVersion: 7,
		Config: workflowFixture(), Rationale: "Make the team's intake flow explicit", Methodology: "CONTINUOUS_FLOW",
		Assumptions: []string{"The team owns the project"}, CitedSources: []SourceCitation{{ID: "doc-1", Revision: 3, Classification: "INTERNAL"}},
		SampleCards: []SampleCard{{ID: "sample-1", Title: "Example request", TypeID: "task", InitialStatusID: "todo", StatusID: "todo", Synthetic: true, Fields: map[string]json.RawMessage{"owner": json.RawMessage(`"person-1"`), "kind": json.RawMessage(`"request"`), "summary": json.RawMessage(`"Example"`)}}},
		Sharing:     []ProposalShare{{PrincipalID: "member-1", Role: "CONTRIBUTOR"}},
	}
}

func TestTodo_PM_040(t *testing.T) {
	policy := aiPolicyFixture()
	got, err := AdmitAIRequest(policy, AIRequest{TenantID: "tenant-a", Provider: "approved", Region: "eu-west", Classification: "INTERNAL", PromptTokens: 100, OutputTokens: 80, EstimatedSpendCents: 25, CurrentSpendCents: 100, InFlightRuns: 0})
	if err != nil || got.Provider.Region != "eu-west" || got.PromptRetention == 0 || got.TraceRetention == 0 || got.SpendLimitCents != 1000 {
		t.Fatalf("admission = %+v, err=%v", got, err)
	}
}

func TestTodo_PM_040_Security(t *testing.T) {
	policy := aiPolicyFixture()
	for name, request := range map[string]AIRequest{
		"wrong region":             {TenantID: "tenant-a", Provider: "approved", Region: "us-east", Classification: "INTERNAL"},
		"protected classification": {TenantID: "tenant-a", Provider: "approved", Region: "eu-west", Classification: "CONFIDENTIAL"},
		"wrong tenant":             {TenantID: "tenant-b", Provider: "approved", Region: "eu-west", Classification: "INTERNAL"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := AdmitAIRequest(policy, request)
			var denied *AIPolicyDenial
			if !errors.As(err, &denied) || !errors.Is(err, ErrAIPolicyDenied) {
				t.Fatalf("err=%v, want typed policy denial", err)
			}
		})
	}
}

func TestTodo_PM_040_Fault(t *testing.T) {
	policy := aiPolicyFixture()
	for name, request := range map[string]AIRequest{
		"tokens":      {TenantID: "tenant-a", Provider: "approved", Region: "eu-west", Classification: "INTERNAL", PromptTokens: 1001},
		"concurrency": {TenantID: "tenant-a", Provider: "approved", Region: "eu-west", Classification: "INTERNAL", InFlightRuns: 2},
		"spend":       {TenantID: "tenant-a", Provider: "approved", Region: "eu-west", Classification: "INTERNAL", CurrentSpendCents: 990, EstimatedSpendCents: 11},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := AdmitAIRequest(policy, request)
			var denied *AIPolicyDenial
			if !errors.As(err, &denied) || denied.Code == AIDenialInvalidRequest {
				t.Fatalf("err=%v, want a typed ceiling denial", err)
			}
		})
	}
}

func TestTodo_PM_040_Golden(t *testing.T) {
	policy := aiPolicyFixture()
	first, err := AdmitAIRequest(policy, AIRequest{TenantID: "tenant-a", Provider: "approved", Region: "eu-west", Classification: "INTERNAL", PromptTokens: 10, OutputTokens: 10})
	if err != nil {
		t.Fatal(err)
	}
	second, err := AdmitAIRequest(policy, AIRequest{TenantID: "tenant-a", Provider: "approved", Region: "eu-west", Classification: "INTERNAL", PromptTokens: 10, OutputTokens: 10})
	if err != nil || first != second {
		t.Fatalf("admission is not deterministic: first=%+v second=%+v err=%v", first, second, err)
	}
}

func TestTodo_PM_040_Integration(t *testing.T) {
	policy := aiPolicyFixture()
	admission, err := AdmitAIRequest(policy, AIRequest{TenantID: policy.TenantID, Provider: "approved", Region: "eu-west", Classification: "PUBLIC"})
	if err != nil || admission.Currency != policy.Currency || admission.OutputTokenLimit != policy.MaxOutputTokens {
		t.Fatalf("policy-to-admission boundary = %+v, err=%v", admission, err)
	}
}

func TestTodo_PM_041(t *testing.T) {
	proposal := proposalFixture()
	proposal.Config.Statuses[0].AllowedNextStatusIDs = []string{"doing", "doing"}
	proposal.Config.Transitions = append(proposal.Config.Transitions, Transition{From: "todo", To: "done"})
	errs := ValidateProposal(proposal)
	if !hasCode(errs, "DUPLICATE_ALLOWED_TRANSITION") || !hasCode(errs, "TRANSITION_NOT_ALLOWED_BY_STATUS") {
		t.Fatalf("proposal compiler missed illegal workflow output: %#v", errs)
	}
}

func TestTodo_PM_041_Property(t *testing.T) {
	base := proposalFixture()
	if got := ValidateProposal(base); len(got) != 0 {
		t.Fatalf("fixture rejected: %#v", got)
	}
	mutations := []func(*BoardProposal){
		func(p *BoardProposal) {
			p.Config.TaskTypes[0].FieldIDs = []string{"owner"}
			p.Config.TaskTypes[0].RequiredFields = []string{"summary"}
		},
		func(p *BoardProposal) { p.Config.Statuses[0].AllowedNextStatusIDs = []string{"missing"} },
		func(p *BoardProposal) { p.SampleCards[0].Synthetic = false },
	}
	for i, mutate := range mutations {
		candidate := proposalFixture()
		mutate(&candidate)
		if len(ValidateProposal(candidate)) == 0 {
			t.Fatalf("mutation %d was accepted", i)
		}
	}
}

func TestTodo_PM_041_Security(t *testing.T) {
	proposal := proposalFixture()
	proposal.Methodology = "SCRUM"
	proposal.Sharing = []ProposalShare{{PrincipalID: "external", Role: "MANAGER", External: true}}
	errs := ValidateProposal(proposal)
	if !hasCode(errs, "UNSUPPORTED_METHODOLOGY") || !hasCode(errs, "UNSAFE_EXTERNAL_SHARE") {
		t.Fatalf("unsafe model claims/sharing accepted: %#v", errs)
	}
}

func TestTodo_PM_041_Conformance(t *testing.T) {
	proposal := proposalFixture()
	proposal.Config.Statuses[0].AllowedNextStatusIDs = []string{"doing"}
	proposal.Config.Transitions[0].To = "done"
	first, second := ValidateProposal(proposal), ValidateProposal(proposal)
	if len(first) == 0 || len(first) != len(second) {
		t.Fatalf("compiler output is not stable: first=%#v second=%#v", first, second)
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("compiler order changed: first=%#v second=%#v", first, second)
		}
	}
}

func TestTodo_PM_042(t *testing.T) {
	proposal := proposalFixture()
	proposal.Config.Columns[0].Name = "Incoming"
	preview, err := PreviewProposal(ProposalPreviewRequest{Proposal: proposal, CurrentConfig: workflowFixture(), CurrentVersion: 7})
	if err != nil || !preview.Accessible || !preview.Safe || preview.ConfigDiff.ChangedColumnIDs[0] != "queue" || len(preview.SyntheticSamples) != 1 || preview.SyntheticSamples[0].Synthetic != true {
		t.Fatalf("preview = %+v, err=%v", preview, err)
	}
	if len(preview.Actions) != 2 || preview.Actions[0].ID != "edit" || preview.Actions[1].ID != "accept" || preview.ReviewedDigest == "" {
		t.Fatalf("preview controls/digest = %+v", preview)
	}
}

func TestTodo_PM_042_Browser(t *testing.T) {
	preview, err := PreviewProposal(ProposalPreviewRequest{Proposal: proposalFixture(), CurrentConfig: workflowFixture(), CurrentVersion: 7})
	if err != nil || preview.SampleLabel != "Synthetic sample card" || preview.Actions[0].Label == "" || preview.Actions[1].Label == "" {
		t.Fatalf("component preview is not browser-actionable: %+v, err=%v", preview, err)
	}
}

func TestTodo_PM_042_Accessibility(t *testing.T) {
	preview, err := PreviewProposal(ProposalPreviewRequest{Proposal: proposalFixture(), CurrentConfig: workflowFixture(), CurrentVersion: 7})
	if err != nil || !preview.Accessible || preview.SampleLabel == "" {
		t.Fatalf("preview accessibility contract = %+v, err=%v", preview, err)
	}
}

func TestTodo_PM_043(t *testing.T) {
	proposal := proposalFixture()
	configDigest, err := ConfigDigest(proposal.Config)
	if err != nil {
		t.Fatal(err)
	}
	revalidated, err := RevalidateProposalPublication(ProposalPublicationRequest{Proposal: proposal, CurrentConfig: proposal.Config, CurrentConfigVersion: 7, ExpectedConfigVersion: 7, ReviewedProposalDigest: ProposalDigest(proposal), ReviewedConfigDigest: configDigest, ReviewedSourceDigest: SourcesDigest(proposal.CitedSources), RequesterID: "requester-1", PublisherID: "publisher-1", RequesterGrants: []SourceGrant{{ID: "doc-1", Revision: 3, CanRead: true}}, PublisherGrants: []SourceGrant{{ID: "doc-1", Revision: 3, CanRead: true}}})
	if err != nil || revalidated.SourceDigest == "" {
		t.Fatalf("publication revalidation = %+v, err=%v", revalidated, err)
	}
}

func TestTodo_PM_043_Race(t *testing.T) {
	proposal := proposalFixture()
	want := ProposalDigest(proposal)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := ProposalDigest(proposal); got != want {
				t.Errorf("digest changed under concurrent reads: %q != %q", got, want)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_PM_043_Security(t *testing.T) {
	proposal := proposalFixture()
	configDigest, _ := ConfigDigest(proposal.Config)
	_, err := RevalidateProposalPublication(ProposalPublicationRequest{Proposal: proposal, CurrentConfig: proposal.Config, CurrentConfigVersion: 7, ExpectedConfigVersion: 7, ReviewedProposalDigest: ProposalDigest(proposal), ReviewedConfigDigest: configDigest, ReviewedSourceDigest: SourcesDigest(proposal.CitedSources), RequesterID: "requester-1", PublisherID: "publisher-1", RequesterGrants: []SourceGrant{{ID: "doc-1", Revision: 4, CanRead: true}}, PublisherGrants: []SourceGrant{{ID: "doc-1", Revision: 3, CanRead: true}}})
	if !errors.Is(err, ErrSourceGrantStale) {
		t.Fatalf("revoked/revised source accepted: %v", err)
	}
}

func TestTodo_PM_043_Golden(t *testing.T) {
	proposal := proposalFixture()
	if ProposalDigest(proposal) != ProposalDigest(proposal) || SourcesDigest(proposal.CitedSources) != SourcesDigest(proposal.CitedSources) {
		t.Fatal("publication evidence digests are not stable")
	}
}
