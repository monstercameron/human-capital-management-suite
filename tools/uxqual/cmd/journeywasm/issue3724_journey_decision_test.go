package main

import (
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestIssue3724_DecisionRequestCarriesTheReviewedRevision(t *testing.T) {
	req := journeyDecisionRequest("j1", false, "  Band exceeds budget \n", " rev-7f3a ")
	if req.GetIntentId() != "j1" || req.GetApprove() || req.GetReason() != "Band exceeds budget" || req.GetExpectedProposalRevisionId() != "rev-7f3a" {
		t.Fatalf("request = %+v", req)
	}
	if !journeyDecisionRequest("j1", true, "", "r").GetApprove() {
		t.Fatal("approve was lost")
	}
}

func TestIssue3724_DecisionFailuresMapToCardRefusals(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{status.Error(codes.FailedPrecondition, "stage"), "stale"},
		{status.Error(codes.PermissionDenied, "no"), "denied"},
		{status.Error(codes.Unauthenticated, "no"), "denied"},
		{status.Error(codes.Unavailable, "down"), "unavailable"},
		{errors.New("network"), "unavailable"},
	} {
		if got := journeyDecisionFailure(tc.err); got != tc.want {
			t.Errorf("%v -> %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestIssue3724_OnlyTheRequesterAndTheRoutedApproverCarryARevision_Security(t *testing.T) {
	if got := journeyDecisionRevision(false, true, " rev-1 "); got != "rev-1" {
		t.Errorf("routed approver revision = %q", got)
	}
	if got := journeyDecisionRevision(true, false, "rev-1"); got != "rev-1" {
		t.Errorf("requester revision = %q", got)
	}
	if got := journeyDecisionRevision(false, false, "rev-1"); got != "" {
		t.Errorf("an unrelated viewer was given the revision %q", got)
	}
	if journeyDecisionReceiptFor(true) != "approved" || journeyDecisionReceiptFor(false) != "rejected" {
		t.Error("receipt words")
	}
}
