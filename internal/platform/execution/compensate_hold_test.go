package execution

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// Unit 1 (spec-compliance plan): compensate_budget_hold is a write, so its
// served port must fail closed at every layer instead of fabricating a
// release. These tests pin the fail-closed contract with hermetic inputs;
// the happy path (a real hold released in the advance transaction) runs
// through the served promotion suite once the graph drives the node there.

func compensateHoldRequest(intentID, proposalDigest string) execute.StepRequest {
	return execute.StepRequest{
		TenantID:   uuid.New(),
		InstanceID: uuid.New(),
		Proposal: runtime.ProposalBinding{
			Revision: intent.ProposalRevision{
				IntentID:           intentID,
				ProposalRevisionID: uuid.NewString(),
				MaterialDigest:     digest.Reference{Digest: proposalDigest},
			},
		},
		RecordedAt: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
	}
}

func TestCompensateHoldRefusesToRunOutsideTheAdvanceTransaction(t *testing.T) {
	ports := &promotionStepPorts{}
	req := compensateHoldRequest(uuid.NewString(), strings.Repeat("c", 64))
	if _, err := ports.ReleaseHold(context.Background(), req); err == nil ||
		!strings.Contains(err.Error(), "advance transaction") {
		t.Fatalf("ReleaseHold without advance tx = %v, want the advance-transaction refusal", err)
	}
}

func TestCompensateHoldRefusesAnUndigestedProposal(t *testing.T) {
	database := pgtest.New(t)
	conn := database.NewConn(t)
	defer conn.Close(context.Background())
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	ports := &promotionStepPorts{}
	req := compensateHoldRequest(uuid.NewString(), "")
	if _, err := ports.ReleaseHold(withStepTx(context.Background(), tx), req); err == nil ||
		!strings.Contains(err.Error(), "material digest") {
		t.Fatalf("ReleaseHold without proposal digest = %v, want the digest refusal", err)
	}
}

func TestCompensateHoldRefusesANonUUIDIntentIdentity(t *testing.T) {
	database := pgtest.New(t)
	conn := database.NewConn(t)
	defer conn.Close(context.Background())
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	ports := &promotionStepPorts{}
	req := compensateHoldRequest("intent:not-a-uuid", strings.Repeat("c", 64))
	if _, err := ports.ReleaseHold(withStepTx(context.Background(), tx), req); err == nil ||
		!strings.Contains(err.Error(), "UUID intent identity") {
		t.Fatalf("ReleaseHold with non-UUID intent = %v, want the identity refusal", err)
	}
}

func TestCompensateHoldRefusesWithoutBoundStepServices(t *testing.T) {
	database := pgtest.New(t)
	conn := database.NewConn(t)
	defer conn.Close(context.Background())
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	ports := &promotionStepPorts{}
	req := compensateHoldRequest(uuid.NewString(), strings.Repeat("c", 64))
	if _, err := ports.ReleaseHold(withStepTx(context.Background(), tx), req); err == nil {
		t.Fatalf("ReleaseHold with unbound services succeeded; an unwired correction must not run")
	}
}
