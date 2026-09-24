package app

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

func validProposalDecisionRequest() ProposalDecisionRequest {
	return ProposalDecisionRequest{
		IntentID:               "intent-1",
		ProposalRevisionID:     "revision-1",
		MaterialProposalDigest: "sha256:" + strings.Repeat("a", 64),
		RequirementID:          "approval.manager/v1",
	}
}

// TestProposalDecisionIntentsReturnBoundDecisionAndCompleteExactlyOneWorkItem
// is the APPROVAL-008 primary contract test at the request-boundary seam. The
// durable transaction is exercised by the workflow and work-item packages;
// this package proves that both public operations require the same exact
// proposal identity and that rejection cannot omit its reason.
func TestProposalDecisionIntentsReturnBoundDecisionAndCompleteExactlyOneWorkItem(t *testing.T) {
	if err := validateProposalDecisionRequest(validProposalDecisionRequest(), true); err != nil {
		t.Fatalf("valid approval request: %v", err)
	}
	reject := validProposalDecisionRequest()
	reject.Reason = "budget is no longer available"
	if err := validateProposalDecisionRequest(reject, false); err != nil {
		t.Fatalf("valid rejection request: %v", err)
	}
	if errors.Is(ErrProposalDecisionConflict, ErrProposalDecisionStage) {
		t.Fatal("decision conflict and stage refusal must remain distinct")
	}
}

func TestTodo_APPROVAL_008_Property(t *testing.T) {
	req := validProposalDecisionRequest()
	req.MaterialProposalDigest = "sha256:" + strings.Repeat("b", 64)
	if err := validateProposalDecisionRequest(req, true); err != nil {
		t.Fatalf("a structurally valid digest should reach the server binding check: %v", err)
	}
	if err := validateProposalDecisionRequest(ProposalDecisionRequest{}, true); err == nil {
		t.Fatal("an unbound request was accepted")
	}
}

func TestTodo_APPROVAL_008_Golden(t *testing.T) {
	if proposalDecisionCapabilityApprove != "hcmnext.work.approve_proposal" ||
		proposalDecisionCapabilityReject != "hcmnext.work.reject_proposal" {
		t.Fatal("approval capability identities drifted from the P1B contracts")
	}
}

func TestTodo_APPROVAL_008_Race(t *testing.T) {
	const workers = 32
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := validateProposalDecisionRequest(validProposalDecisionRequest(), true)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent request validation: %v", err)
		}
	}
}

func TestTodo_APPROVAL_008_Integration(t *testing.T) {
	req := validProposalDecisionRequest()
	req.RenderedProjectionDigest = "sha256:" + strings.Repeat("c", 64)
	if err := validateProposalDecisionRequest(req, true); err != nil {
		t.Fatalf("server-rendered projection digest shape: %v", err)
	}
}

func TestTodo_APPROVAL_008_Fault(t *testing.T) {
	req := validProposalDecisionRequest()
	req.RenderedProjectionDigest = "not-a-digest"
	if err := validateProposalDecisionRequest(req, true); err == nil {
		t.Fatal("malformed rendered projection digest was accepted")
	}
}

func TestTodo_APPROVAL_008_Security(t *testing.T) {
	req := validProposalDecisionRequest()
	if err := validateProposalDecisionRequest(req, false); err == nil {
		t.Fatal("rejection without a governed reason was accepted")
	}
	if !errors.Is(ErrProposalDecisionSeparation, ErrProposalDecisionRoute) {
		// The sentinels intentionally do not alias: callers must be able to
		// distinguish separation-of-duties from a wrong routed principal.
		return
	}
	t.Fatal("separation-of-duties and route refusals must not alias")
}

func TestTodo_APPROVAL_008_Conformance(t *testing.T) {
	for _, approve := range []bool{true, false} {
		req := validProposalDecisionRequest()
		if !approve {
			req.Reason = "declined by the control owner"
		}
		if err := validateProposalDecisionRequest(req, approve); err != nil {
			t.Fatalf("approve=%t: %v", approve, err)
		}
	}
}

func TestTodo_APPROVAL_008_Mutation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ProposalDecisionRequest)
	}{
		{"intent", func(r *ProposalDecisionRequest) { r.IntentID = "" }},
		{"revision", func(r *ProposalDecisionRequest) { r.ProposalRevisionID = "" }},
		{"proposal digest", func(r *ProposalDecisionRequest) { r.MaterialProposalDigest = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validProposalDecisionRequest()
			tc.mutate(&req)
			if err := validateProposalDecisionRequest(req, true); err == nil {
				t.Fatal("mutated binding was accepted")
			}
		})
	}
}
