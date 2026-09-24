package intentcontrol

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAcceptedActionDigestMatchesContentDigestDomain(t *testing.T) {
	action := AcceptedAction{
		TenantID:   uuid.MustParse("4b2c6fa8-0174-447b-a8c0-b614ce31d00a"),
		DecisionID: uuid.MustParse("64b6bfc5-8c4e-43e2-a665-8a4f05b45a0e"),
		IntentID:   uuid.MustParse("a808a466-cf4a-4ecf-8df3-f1a6c823a6d8"),
		ActionID:   "intent.execute", ProposalRevisionID: "proposal-17",
		ProposalDigest: strings.Repeat("a", 64), AcceptedBy: "principal:manager",
		AcceptedAt:     time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC),
		IdempotencyKey: "execute:intent-17:proposal-17",
	}

	digest := action.computeDigest()
	if !digestPattern.MatchString(digest) {
		t.Fatalf("acceptance digest %q does not match content_digest's bare 64-character lowercase hex form", digest)
	}
	action.AcceptanceDigest = digest
	if got := action.computeDigest(); got != action.AcceptanceDigest {
		t.Fatalf("recomputed acceptance digest = %q, want persisted digest %q", got, action.AcceptanceDigest)
	}

	changed := action
	changed.AcceptedBy = "principal:other-manager"
	if changed.computeDigest() == action.AcceptanceDigest {
		t.Fatal("changed acceptance facts retained the original digest")
	}
}
