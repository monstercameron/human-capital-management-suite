package main

import (
	"strings"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// journeyDecisionRequest is the one request a decision from a workflow card
// sends: the card's journey, the answer, the typed reason and the proposal
// revision the approver reviewed. The service refuses it when that revision is
// no longer current (#3724).
func journeyDecisionRequest(intentID string, approve bool, reason, revision string) *journeyv1.DecideJourneyRequest {
	return &journeyv1.DecideJourneyRequest{
		IntentId: intentID, Approve: approve, Reason: strings.TrimSpace(reason),
		ExpectedProposalRevisionId: strings.TrimSpace(revision),
	}
}

// journeyDecisionFailure names the card's refusal for a failed DecideJourney
// call: a changed proposal or stage ("stale"), a refused route ("denied"), or
// a temporary failure ("unavailable").
func journeyDecisionFailure(err error) string {
	switch status.Code(err) {
	case codes.FailedPrecondition, codes.Aborted:
		return "stale"
	case codes.PermissionDenied, codes.Unauthenticated:
		return "denied"
	}
	return "unavailable"
}

// journeyDecisionRevision is the proposal revision a card carries. The
// requester reads it for the help panels; the routed approver reads it so a
// decision is bound to what they reviewed. Nobody else is given it.
func journeyDecisionRevision(initiator, waitsOnViewer bool, revision string) string {
	if !initiator && !waitsOnViewer {
		return ""
	}
	return strings.TrimSpace(revision)
}

// journeyDecisionReceipt is the receipt word for a decision.
func journeyDecisionReceiptFor(approve bool) string {
	if approve {
		return "approved"
	}
	return "rejected"
}
