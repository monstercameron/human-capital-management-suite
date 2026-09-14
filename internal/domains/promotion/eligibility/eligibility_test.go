package eligibility

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type memoryPositionFacts struct{ revision position.PositionRevision }

func (m memoryPositionFacts) PositionRevisionAt(_ context.Context, q position.PositionQuery) (position.PositionRevision, bool, error) {
	if q.Position != m.revision.Position {
		return position.PositionRevision{}, false, nil
	}
	return m.revision, true, nil
}

func testPositionRevision(t *testing.T) position.PositionRevision {
	t.Helper()
	tenant := values.TenantId("tenant-a")
	pos := values.EntityRef{Tenant: tenant, Kind: position.KindPosition, Id: uuid.NewString()}
	start, _ := values.ParseLocalDate("2026-01-01")
	end, _ := values.ParseLocalDate("2026-12-31")
	interval, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "calendar", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	revision, err := values.NewSequenceRevision("position.revision", 4)
	if err != nil {
		t.Fatal(err)
	}
	fte, err := values.NewDecimal("1.0000", 4, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := values.NewRecordedAt(values.NewInstant(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	return position.PositionRevision{
		Position: pos, Revision: revision, Effective: interval, Lifecycle: position.LifecycleOpen,
		JobCode: "OPS-HRBP3", OrgUnit: "people-ops", LegalEntity: "HarborCare US Inc.",
		Capacity:   position.CapacityPolicy{CapacityFTE: fte, CapacityHeads: 1},
		Authority:  evidence.SourceAuthority{Kind: evidence.AuthorityLocal, System: "position", PolicyRef: "policy/1"},
		Provenance: evidence.Provenance{Source: "position", EvidenceRef: "evidence-1", RecordedAt: recorded},
	}
}

func testAsOf(t *testing.T) position.AsOf {
	t.Helper()
	d, _ := values.ParseLocalDate("2026-06-01")
	k, err := values.NewKnownAt(values.NewInstant(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	return position.AsOf{EffectiveOn: d, KnownAt: k}
}

func candidateRequest(t *testing.T, rev position.PositionRevision, reader position.PositionFacts) CandidateRequest {
	t.Helper()
	fte, _ := values.NewDecimal("1.0000", 4, values.RoundingExactRequired)
	return CandidateRequest{
		Tenant: rev.Position.Tenant, Position: rev.Position, AsOf: testAsOf(t),
		CurrentJobCode: "OPS-HRBP2", CurrentGrade: "P2", TargetJobCode: rev.JobCode, TargetGrade: "P3",
		TargetOrgUnit: rev.OrgUnit, TargetLegalEntity: rev.LegalEntity,
		Paths:     []Path{{Ref: "path-1", Revision: "2026.1", SourceJobCode: "OPS-HRBP2", SourceGrade: "P2", TargetJobCode: "OPS-HRBP3", TargetGrade: "P3"}},
		Authorize: func(position.PositionRevision) bool { return true }, PositionFacts: reader, FTE: fte, Heads: 1,
		ProposalRevisionID: "proposal-1", ProposalDigest: "sha256:proposal", AuthorityDigest: "sha256:authority",
		ReservationKey: "reservation-1", ReservationExpiry: time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC),
	}
}

func TestTodo_PROMOUX_004(t *testing.T) {
	rev := testPositionRevision(t)
	if err := rev.Validate(); err != nil {
		t.Fatalf("test revision: %v", err)
	}
	result, err := Resolve(context.Background(), candidateRequest(t, rev, memoryPositionFacts{revision: rev}))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !result.Eligible || result.Candidate == nil {
		t.Fatalf("result = %+v, want eligible candidate", result)
	}
	if result.Candidate.Path.Ref != "path-1" || result.Candidate.PositionRevision != rev.Revision {
		t.Fatalf("candidate = %+v, want path and authorized position revision", result.Candidate)
	}
	if result.Reservation == nil || result.Reservation.ProposalDigest != "sha256:proposal" {
		t.Fatalf("reservation = %+v, want proposal-bound projection", result.Reservation)
	}
	if result.Reservation.Position != rev.Position || result.Reservation.EffectiveDate.String() != "2026-06-01" {
		t.Fatalf("reservation lost position/date binding: %+v", result.Reservation)
	}
	if result.Reservation.MinRevision != rev.Revision || result.Reservation.Effective != rev.Effective {
		t.Fatalf("reservation lost authorized revision/effective window: %+v", result.Reservation)
	}
}

func TestTodo_PROMOUX_004_Security(t *testing.T) {
	rev := testPositionRevision(t)
	req := candidateRequest(t, rev, memoryPositionFacts{revision: rev})
	req.Authorize = func(position.PositionRevision) bool { return false }
	if _, err := Resolve(context.Background(), req); !errors.Is(err, position.ErrUnauthorized) {
		t.Fatalf("unauthorized Resolve = %v, want position.ErrUnauthorized", err)
	}

	req = candidateRequest(t, rev, memoryPositionFacts{revision: rev})
	req.TargetJobCode = "OPS-HRBP4"
	if _, err := Resolve(context.Background(), req); !errors.Is(err, ErrNoCompatiblePath) {
		t.Fatalf("missing path Resolve = %v, want ErrNoCompatiblePath", err)
	}
}

func TestTodo_PROMOUX_004_Validation(t *testing.T) {
	rev := testPositionRevision(t)
	req := candidateRequest(t, rev, memoryPositionFacts{revision: rev})
	req.TargetGrade = ""
	if _, err := Resolve(context.Background(), req); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("malformed Resolve = %v, want ErrInvalidRequest", err)
	}

	req = candidateRequest(t, rev, memoryPositionFacts{revision: rev})
	req.Paths[0].Revision = ""
	if _, err := Resolve(context.Background(), req); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("malformed path Resolve = %v, want ErrInvalidRequest", err)
	}

	req = candidateRequest(t, rev, memoryPositionFacts{revision: rev})
	req.Authorize = nil
	if _, err := Resolve(context.Background(), req); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unscoped Resolve = %v, want ErrInvalidRequest", err)
	}
}
