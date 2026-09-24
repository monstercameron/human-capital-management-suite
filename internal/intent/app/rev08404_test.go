package app

import (
	"errors"
	"testing"
)

// TestTodo_REV_084_04 pins the application routing contract consumed by the
// served correction endpoint: stale revisions rebase and altered known
// content is refused as tampering.
func TestTodo_REV_084_04(t *testing.T) {
	stale := align032Request()
	stale.ProposalRevisionID = "proposal-old"
	decision, err := RouteCorrection(stale, align030Tenant, "proposal-current", "sha256:current", align030Now)
	if err != nil {
		t.Fatalf("RouteCorrection(stale): %v", err)
	}
	if decision.Route != RouteRebase || decision.Allowed || decision.ProposalRevisionID != "proposal-old" {
		t.Fatalf("stale decision = %+v, want denied rebase retaining requested revision", decision)
	}
	if err := decision.VerifyDecision(); err != nil {
		t.Fatalf("rebase decision digest: %v", err)
	}

	tampered := align032Request()
	tampered.ProposalDigest = "sha256:altered"
	if _, err := RouteCorrection(tampered, align030Tenant, "proposal-1", "sha256:proposal", align030Now); !errors.Is(err, ErrCorrectionTampered) {
		t.Fatalf("RouteCorrection(tampered) = %v, want ErrCorrectionTampered", err)
	}
}
