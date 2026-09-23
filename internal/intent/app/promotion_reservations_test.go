package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/localcommit"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// REV-006-02 admission tests: the revision implies its seat, the derived
// request keys to the proposal digest, and the candidate path holds the head
// before the commit is attempted.

var rev00602AppTenant = values.TenantId("11111111-1111-4111-8111-111111111111")

const rev00602AppSeatID = "44444444-4444-4444-8444-444444444444"

func rev00602AppSeat(t *testing.T) position.PositionRevision {
	t.Helper()
	interval, err := values.NewLocalDateInterval(
		mustAppDate(t, "2026-01-01"), mustAppDate(t, "2026-12-31"),
		values.CalendarRef{Ref: "position.test.calendar", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	revision, err := values.NewSequenceRevision("position.revision.seat-1", 3)
	if err != nil {
		t.Fatal(err)
	}
	fte, err := values.NewDecimal("1.0000", 4, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := values.NewRecordedAt(values.NewInstant(time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	return position.PositionRevision{
		Position: values.EntityRef{Tenant: rev00602AppTenant, Kind: position.KindPosition, Id: rev00602AppSeatID},
		Revision: revision, Effective: interval, Lifecycle: position.LifecycleOpen,
		JobCode: "ENG-MGR", OrgUnit: "eng-platform", LegalEntity: "HarborCare US Inc.",
		Capacity:   position.CapacityPolicy{CapacityFTE: fte, CapacityHeads: 1},
		Authority:  evidence.SourceAuthority{Kind: evidence.AuthorityLocal, System: "hcmnext.position", PolicyRef: "position.source_authority/2026.1"},
		Provenance: evidence.Provenance{Source: "hcmnext.position", EvidenceRef: "evd_seat_1_r3", RecordedAt: recorded},
	}
}

func mustAppDate(t *testing.T, s string) values.LocalDate {
	t.Helper()
	d, err := values.ParseLocalDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

type rev00602AppReader struct {
	seats map[string]position.PositionRevision
}

func (r rev00602AppReader) PositionRevisionAt(_ context.Context, q position.PositionQuery) (position.PositionRevision, bool, error) {
	rev, ok := r.seats[q.Position.Id]
	return rev, ok, nil
}

func rev00602AppRevision(t *testing.T, revisionID, positionID string, effective time.Time) intent.ProposalRevision {
	t.Helper()
	eff, err := values.NewOpenInstantInterval(values.NewInstant(effective))
	if err != nil {
		t.Fatal(err)
	}
	rev := intent.ProposalRevision{
		ProposalRevisionID: revisionID,
		Tenant:             rev00602AppTenant,
		EffectiveTime:      eff,
		MaterialDigest:     digest.Reference{ProfileID: "PROPOSAL", ProfileVersion: 1, SchemaID: "proposal", SchemaVersion: 1, AlgorithmID: "sha256", Digest: "sha256:" + revisionID},
		CreatedAt:          values.NewInstant(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)),
		ProposedState: []intent.StateAssertion{
			{FieldPath: "assignment.job_code", CanonicalText: "ENG-MGR"},
			{FieldPath: "assignment.org_unit", CanonicalText: "eng-platform"},
		},
	}
	if positionID != "" {
		rev.Subjects = []intent.SubjectReference{
			{Kind: "EMPLOYMENT", SubjectID: "66666666-6666-4666-8666-666666666666", AuthorityDomain: "people"},
			{Kind: "POSITION", SubjectID: positionID, AuthorityDomain: "organization"},
		}
	}
	return rev
}

func TestPromotionPositionTarget(t *testing.T) {
	id, start, ok := promotionPositionTarget(rev00602AppRevision(t, "rev-1", rev00602AppSeatID, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)))
	if !ok || id.String() != rev00602AppSeatID {
		t.Fatalf("target = %v %v %v", id, start, ok)
	}
	if start.Format(time.DateOnly) != "2026-10-01" {
		t.Fatalf("start = %v", start)
	}
	if _, _, ok := promotionPositionTarget(rev00602AppRevision(t, "rev-1", "", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))); ok {
		t.Fatal("position-less revision names a target")
	}
	bad := rev00602AppRevision(t, "rev-1", "not-a-uuid", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	if _, _, ok := promotionPositionTarget(bad); ok {
		t.Fatal("malformed position subject names a target")
	}
}

func TestPositionHoldRequest(t *testing.T) {
	seat := rev00602AppSeat(t)
	rev := rev00602AppRevision(t, "rev-1", rev00602AppSeatID, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	req, ok, err := positionHoldRequest(rev, seat, "sha256:controls")
	if err != nil || !ok {
		t.Fatalf("request = %+v %v %v", req, ok, err)
	}
	if req.ProposalDigest != "sha256:rev-1" || req.IdempotencyKey != "sha256:rev-1" || req.ProposalRevisionID != "rev-1" {
		t.Fatalf("request keys = %+v", req)
	}
	if req.Heads != 1 || !req.FTE.Equal(seat.Capacity.CapacityFTE) {
		t.Fatalf("quantities = %v %+v", req.Heads, req.FTE)
	}
	if req.ExpiresAt.Format(time.DateOnly) != "2026-10-31" {
		t.Fatalf("expiry = %v", req.ExpiresAt)
	}
	if req.Position.Id != rev00602AppSeatID || req.MinRevision != seat.Revision {
		t.Fatalf("seat binding = %+v", req)
	}
	if _, ok, err := positionHoldRequest(rev00602AppRevision(t, "rev-1", "", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)), seat, "sha256:controls"); err != nil || ok {
		t.Fatalf("position-less revision holds: %v %v", ok, err)
	}
	other := seat
	other.Position.Id = "77777777-7777-4777-8777-777777777777"
	if _, _, err := positionHoldRequest(rev, other, "sha256:controls"); err == nil {
		t.Fatal("mismatched seat revision accepted")
	}
}

func TestHoldProposalPosition(t *testing.T) {
	ctx := context.Background()
	seat := rev00602AppSeat(t)
	observed := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	engine := &journeyEngine{
		positionReader: rev00602AppReader{seats: map[string]position.PositionRevision{rev00602AppSeatID: seat}},
		reservations:   localcommit.NewPromotionReservationFence(),
	}
	inst := intent.Instance{Tenant: rev00602AppTenant}
	rev := rev00602AppRevision(t, "rev-1", rev00602AppSeatID, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))

	if err := engine.holdProposalPosition(ctx, inst, rev, observed); err != nil {
		t.Fatalf("hold: %v", err)
	}
	// Re-holding the identical proposal replays instead of conflicting
	// with its own earlier self.
	if err := engine.holdProposalPosition(ctx, inst, rev, observed); err != nil {
		t.Fatalf("identical re-hold: %v", err)
	}

	// A second proposal for the same seat loses with the typed conflict.
	second := rev00602AppRevision(t, "rev-2", rev00602AppSeatID, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	if err := engine.holdProposalPosition(ctx, inst, second, observed); !errors.Is(err, position.ErrCompetingReservation) {
		t.Fatalf("second hold = %v, want %v", err, position.ErrCompetingReservation)
	}

	// A position-less revision implies no hold and no refusal.
	if err := engine.holdProposalPosition(ctx, inst, rev00602AppRevision(t, "rev-3", "", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)), observed); err != nil {
		t.Fatalf("position-less hold: %v", err)
	}

	// No reader is a composition gap, refused closed rather than unfenced.
	bare := &journeyEngine{reservations: localcommit.NewPromotionReservationFence()}
	if err := bare.holdProposalPosition(ctx, inst, rev, observed); !errors.Is(err, workspace.ErrJourneyUnavailable) {
		t.Fatalf("reader-less hold = %v, want %v", err, workspace.ErrJourneyUnavailable)
	}
}
