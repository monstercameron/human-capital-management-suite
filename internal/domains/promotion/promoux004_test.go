package promotion

// PROMOUX-004: "Replace free-text target positions with authorized vacancy
// selection and reservation evidence."
//
// TestTodo_PROMOUX_004 is the PRIMARY: it drives evaluateTargetPositionSelection
// directly -- no UI, no transport, no database -- and proves each of the
// five grounds RED names refuses a promotion for that ground specifically,
// that the zero value fails closed, and that the ground/finding mappings
// are exhaustive with no permissive default.
//
// TestTodo_PROMOUX_004_Security proves no-enumeration: an unauthorized
// position is both absent from the picker
// (internal/domains/promotion/positionpicker) and refused when guessed
// directly here, with the identical refusal in both cases.

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/positionpicker"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// -----------------------------------------------------------------------
// Fixtures
// -----------------------------------------------------------------------

var promoux004Calendar = values.CalendarRef{Ref: "promoux004.test.calendar", Version: "1"}

func promoux004Date(t testing.TB, s string) values.LocalDate {
	t.Helper()
	d, err := values.ParseLocalDate(s)
	if err != nil {
		t.Fatalf("date %q: %v", s, err)
	}
	return d
}

func promoux004KnownAt(t testing.TB, s string) values.KnownAt {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("known at %q: %v", s, err)
	}
	k, err := values.NewKnownAt(values.NewInstant(parsed))
	if err != nil {
		t.Fatalf("known at %q: %v", s, err)
	}
	return k
}

func promoux004AsOf(t testing.TB) position.AsOf {
	t.Helper()
	return position.AsOf{EffectiveOn: promoux004Date(t, "2027-02-01"), KnownAt: promoux004KnownAt(t, "2027-01-15T00:00:00Z")}
}

func promoux004Tenant() values.TenantId { return values.TenantId("promoux004-tenant") }

func promoux004Position(t testing.TB, id string) values.EntityRef {
	t.Helper()
	ref := values.EntityRef{Tenant: promoux004Tenant(), Kind: position.KindPosition, Id: id}
	if err := ref.Validate(); err != nil {
		t.Fatalf("position ref: %v", err)
	}
	return ref
}

const promoux004PositionID = "44444444-4444-4444-8444-444444444444"
const promoux004UnauthorizedPositionID = "55555555-5555-4555-8555-555555555555"

// promoux004Revision builds a well-formed OPEN revision at sequence seq,
// compatible with the desired job/org this file's requests all ask for,
// covering promoux004AsOf's effective date.
func promoux004Revision(t testing.TB, pos values.EntityRef, seq uint64) position.PositionRevision {
	t.Helper()
	interval, err := values.NewLocalDateInterval(promoux004Date(t, "2026-01-01"), promoux004Date(t, "2028-12-31"), promoux004Calendar)
	if err != nil {
		t.Fatalf("interval: %v", err)
	}
	rev, err := values.NewSequenceRevision("promoux004.position.revision", seq)
	if err != nil {
		t.Fatalf("revision: %v", err)
	}
	capacityFTE, err := values.NewDecimal("1.0000", 4, values.RoundingExactRequired)
	if err != nil {
		t.Fatalf("capacity fte: %v", err)
	}
	return position.PositionRevision{
		Position: pos, Revision: rev, Effective: interval, Lifecycle: position.LifecycleOpen,
		JobCode: "ENG-MGR", OrgUnit: "ENGINEERING", LegalEntity: "ACME US Inc.",
		Capacity: position.CapacityPolicy{CapacityFTE: capacityFTE, CapacityHeads: 1},
		Authority: evidence.SourceAuthority{
			Kind: evidence.AuthorityLocal, System: "hcmnext.position", PolicyRef: "position.source_authority/2026.1",
		},
		Provenance: evidence.Provenance{
			Source: "hcmnext.position", EvidenceRef: "evd_promoux004", RecordedAt: promoux004RecordedAt(t),
		},
	}
}

func promoux004RecordedAt(t testing.TB) values.RecordedAt {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, "2026-01-02T09:00:00Z")
	if err != nil {
		t.Fatalf("instant: %v", err)
	}
	r, err := values.NewRecordedAt(values.NewInstant(parsed))
	if err != nil {
		t.Fatalf("recorded at: %v", err)
	}
	return r
}

// fakePositionReader is an in-memory position.PositionFacts fixture that
// answers a single position by id. It performs no authorization itself:
// Authorize is a parameter CheckCompatibility/CalculateCapacity call
// themselves, after this reader discloses the revision, exactly like a real
// PositionFacts adapter.
type fakePositionReader struct {
	byID map[string]position.PositionRevision
}

func (f fakePositionReader) PositionRevisionAt(_ context.Context, q position.PositionQuery) (position.PositionRevision, bool, error) {
	rev, ok := f.byID[q.Position.Id]
	if !ok {
		return position.PositionRevision{}, false, nil
	}
	return rev, true, nil
}

// fakeAdmitter is a PositionReservationAdmitter test double that returns a
// fixed decision and captures the request it was called with.
type fakeAdmitter struct {
	admitted bool
	err      error
	called   bool
	captured PositionSlotAdmitRequest
}

func (f *fakeAdmitter) AdmitPositionSlot(_ context.Context, req PositionSlotAdmitRequest) (bool, error) {
	f.called, f.captured = true, req
	return f.admitted, f.err
}

func promoux004Selection(t testing.TB, pos values.EntityRef, rev values.RevisionToken) *PositionSelection {
	t.Helper()
	ref, err := position.EncodeRevisionRef(pos, rev)
	if err != nil {
		t.Fatalf("EncodeRevisionRef: %v", err)
	}
	return &PositionSelection{
		Reference: ref, AsOf: promoux004AsOf(t),
		ProposalRef: "proposal-promoux004", IdempotencyKey: "req-promoux004",
	}
}

func promoux004Request(t testing.TB, reader position.PositionFacts, admitter PositionReservationAdmitter, sel *PositionSelection) PreflightRequest {
	t.Helper()
	return PreflightRequest{
		Tenant: promoux004Tenant(), Target: TargetPlacement{JobCode: "ENG-MGR", OrgUnit: "ENGINEERING"},
		PositionReader: reader, Reservations: admitter, TargetPositionSelection: sel,
	}
}

// -----------------------------------------------------------------------
// PRIMARY
// -----------------------------------------------------------------------

func TestTodo_PROMOUX_004(t *testing.T) {
	ctx := context.Background()
	pos := promoux004Position(t, promoux004PositionID)

	t.Run("every ground satisfied admits with no findings", func(t *testing.T) {
		reader := fakePositionReader{byID: map[string]position.PositionRevision{
			pos.Id: promoux004Revision(t, pos, 5),
		}}
		admitter := &fakeAdmitter{admitted: true}
		sel := promoux004Selection(t, pos, mustRevision(t, 5))
		findings, err := evaluateTargetPositionSelection(ctx, promoux004Request(t, reader, admitter, sel))
		if err != nil {
			t.Fatalf("evaluateTargetPositionSelection: %v", err)
		}
		if len(findings) != 0 {
			t.Fatalf("findings = %+v, want none", findings)
		}
		if !admitter.called {
			t.Fatal("the reservation admitter must be consulted once every prior ground passes")
		}
	})

	t.Run("nil selection performs no check at all", func(t *testing.T) {
		findings, err := evaluateTargetPositionSelection(ctx, PreflightRequest{Tenant: promoux004Tenant()})
		if err != nil || len(findings) != 0 {
			t.Fatalf("evaluateTargetPositionSelection(nil selection) = %+v, %v, want no findings and no error", findings, err)
		}
	})

	t.Run("ground one: existence -- the position does not exist", func(t *testing.T) {
		reader := fakePositionReader{byID: map[string]position.PositionRevision{}}
		sel := promoux004Selection(t, pos, mustRevision(t, 5))
		findings, err := evaluateTargetPositionSelection(ctx, promoux004Request(t, reader, &fakeAdmitter{admitted: true}, sel))
		requireSoleFinding(t, findings, err, CodeTargetPositionNotFound)
	})

	t.Run("ground one: existence -- a guessed reference never decodes", func(t *testing.T) {
		sel := &PositionSelection{Reference: position.RevisionRef("POS-ENG-MGR-101"), AsOf: promoux004AsOf(t), ProposalRef: "p", IdempotencyKey: "k"}
		reader := fakePositionReader{byID: map[string]position.PositionRevision{pos.Id: promoux004Revision(t, pos, 5)}}
		findings, err := evaluateTargetPositionSelection(ctx, promoux004Request(t, reader, &fakeAdmitter{admitted: true}, sel))
		requireSoleFinding(t, findings, err, CodeTargetPositionNotFound)
	})

	t.Run("ground two: vacancy -- the position is closed", func(t *testing.T) {
		rev := promoux004Revision(t, pos, 5)
		rev.Lifecycle = position.LifecycleClosed
		reader := fakePositionReader{byID: map[string]position.PositionRevision{pos.Id: rev}}
		sel := promoux004Selection(t, pos, mustRevision(t, 5))
		findings, err := evaluateTargetPositionSelection(ctx, promoux004Request(t, reader, &fakeAdmitter{admitted: true}, sel))
		requireSoleFinding(t, findings, err, CodeTargetPositionAtCapacity)
	})

	t.Run("ground two: vacancy -- capacity is fully consumed by an incumbent", func(t *testing.T) {
		reader := fakePositionReader{byID: map[string]position.PositionRevision{pos.Id: promoux004Revision(t, pos, 5)}}
		sel := promoux004Selection(t, pos, mustRevision(t, 5))
		fte, err := values.NewDecimal("1.0000", 4, values.RoundingExactRequired)
		if err != nil {
			t.Fatalf("decimal: %v", err)
		}
		worker := values.EntityRef{Tenant: promoux004Tenant(), Kind: position.KindWorker, Id: "66666666-6666-4666-8666-666666666666"}
		sel.Occupants = []position.Occupant{{Worker: worker, FTE: fte, Exclusive: true, Effective: openInterval(t, "2020-01-01")}}
		findings, err2 := evaluateTargetPositionSelection(ctx, promoux004Request(t, reader, &fakeAdmitter{admitted: true}, sel))
		requireSoleFinding(t, findings, err2, CodeTargetPositionAtCapacity)
	})

	t.Run("ground three: compatibility -- job and organization do not match", func(t *testing.T) {
		rev := promoux004Revision(t, pos, 5)
		rev.JobCode, rev.OrgUnit = "SALES-REP", "SALES"
		reader := fakePositionReader{byID: map[string]position.PositionRevision{pos.Id: rev}}
		sel := promoux004Selection(t, pos, mustRevision(t, 5))
		findings, err := evaluateTargetPositionSelection(ctx, promoux004Request(t, reader, &fakeAdmitter{admitted: true}, sel))
		requireSoleFinding(t, findings, err, CodeTargetPositionIncompatible)
	})

	t.Run("ground four: effective-date capacity -- the selected revision is stale", func(t *testing.T) {
		// The picker disclosed revision 5; the position has since moved to
		// revision 6. A promotion bound to the stale disclosure must be
		// refused, not silently re-validated against whatever is current.
		reader := fakePositionReader{byID: map[string]position.PositionRevision{pos.Id: promoux004Revision(t, pos, 6)}}
		sel := promoux004Selection(t, pos, mustRevision(t, 5))
		findings, err := evaluateTargetPositionSelection(ctx, promoux004Request(t, reader, &fakeAdmitter{admitted: true}, sel))
		requireSoleFinding(t, findings, err, CodeTargetPositionNotEffective)
	})

	t.Run("ground four: effective-date capacity -- the revision does not cover the proposed date", func(t *testing.T) {
		rev := promoux004Revision(t, pos, 5)
		notYetEffective, err := values.NewLocalDateInterval(promoux004Date(t, "2030-01-01"), promoux004Date(t, "2031-01-01"), promoux004Calendar)
		if err != nil {
			t.Fatalf("interval: %v", err)
		}
		rev.Effective = notYetEffective
		reader := fakePositionReader{byID: map[string]position.PositionRevision{pos.Id: rev}}
		sel := promoux004Selection(t, pos, mustRevision(t, 5))
		findings, err2 := evaluateTargetPositionSelection(ctx, promoux004Request(t, reader, &fakeAdmitter{admitted: true}, sel))
		requireSoleFinding(t, findings, err2, CodeTargetPositionNotEffective)
	})

	t.Run("ground five: reservation ownership -- another proposal already holds the slot", func(t *testing.T) {
		reader := fakePositionReader{byID: map[string]position.PositionRevision{pos.Id: promoux004Revision(t, pos, 5)}}
		sel := promoux004Selection(t, pos, mustRevision(t, 5))
		findings, err := evaluateTargetPositionSelection(ctx, promoux004Request(t, reader, &fakeAdmitter{admitted: false}, sel))
		requireSoleFinding(t, findings, err, CodeTargetPositionReservationConflict)
	})

	t.Run("the zero value selection fails closed", func(t *testing.T) {
		reader := fakePositionReader{byID: map[string]position.PositionRevision{pos.Id: promoux004Revision(t, pos, 5)}}
		sel := &PositionSelection{}
		findings, err := evaluateTargetPositionSelection(ctx, promoux004Request(t, reader, &fakeAdmitter{admitted: true}, sel))
		requireSoleFinding(t, findings, err, CodeTargetPositionNotFound)
	})

	t.Run("exhaustive ground mapping: every Position finding code known today classifies, and an unknown one is refused", func(t *testing.T) {
		known := []position.FindingCode{
			position.FindingNotFound, position.FindingClosed, position.FindingFrozen,
			position.FindingJobMismatch, position.FindingOrgMismatch, position.FindingLegalEntityMismatch,
			position.FindingStaleRevision, position.FindingNotEffective,
			position.FindingOverCapacityHeads, position.FindingOverCapacityFTE,
			position.FindingOverlappingExclusiveOccupancy, position.FindingVacantAfterDate,
		}
		for _, code := range known {
			ground, ok := groundForPositionFinding(code)
			if !ok {
				t.Fatalf("groundForPositionFinding(%s) reported unrecognized, want a classification", code)
			}
			if _, err := findingForGround(ground); err != nil {
				t.Fatalf("findingForGround(%s) for code %s: %v", ground, code, err)
			}
		}
		if _, ok := groundForPositionFinding(position.FindingCode("BOGUS_FUTURE_CODE")); ok {
			t.Fatal("an unrecognized position finding code must not classify to any ground")
		}
		if _, err := mapPositionFindings([]position.Finding{{Code: position.FindingCode("BOGUS_FUTURE_CODE")}}); !errors.Is(err, ErrRequestInvalid) {
			t.Fatalf("mapPositionFindings(unrecognized code) = %v, want ErrRequestInvalid (fail closed, never silently dropped)", err)
		}
		if _, err := findingForGround(PositionSelectionGround("QUANTUM_GROUND")); !errors.Is(err, ErrRequestInvalid) {
			t.Fatalf("findingForGround(unrecognized ground) = %v, want ErrRequestInvalid", err)
		}
	})

	t.Run("a misconfigured caller with no reservation admitter fails closed rather than skipping ground five", func(t *testing.T) {
		reader := fakePositionReader{byID: map[string]position.PositionRevision{pos.Id: promoux004Revision(t, pos, 5)}}
		sel := promoux004Selection(t, pos, mustRevision(t, 5))
		_, err := evaluateTargetPositionSelection(ctx, promoux004Request(t, reader, nil, sel))
		if !errors.Is(err, ErrRequestInvalid) {
			t.Fatalf("evaluateTargetPositionSelection(no admitter) = %v, want ErrRequestInvalid", err)
		}
	})
}

// -----------------------------------------------------------------------
// SECURITY
// -----------------------------------------------------------------------

// TestTodo_PROMOUX_004_Security proves no-enumeration: the exact same
// Authorize hook that hides an unauthorized position from the picker also
// refuses it when guessed directly against evaluateTargetPositionSelection,
// and the refusal is byte-for-byte the same Finding a genuinely
// nonexistent position produces -- so a caller learns nothing about which
// guessed identifiers are real.
func TestTodo_PROMOUX_004_Security(t *testing.T) {
	ctx := context.Background()
	authorizedPos := promoux004Position(t, promoux004PositionID)
	unauthorizedPos := promoux004Position(t, promoux004UnauthorizedPositionID)

	authorize := func(rev position.PositionRevision) bool { return rev.Position.Id != unauthorizedPos.Id }
	reader := fakePositionReader{
		byID: map[string]position.PositionRevision{
			authorizedPos.Id:   promoux004Revision(t, authorizedPos, 5),
			unauthorizedPos.Id: promoux004Revision(t, unauthorizedPos, 5),
		},
	}

	// Half one: the picker never discloses the unauthorized position.
	candidates, err := positionpicker.ResolveCandidates(ctx, reader, positionpicker.Request{
		Tenant: promoux004Tenant(), AsOf: promoux004AsOf(t),
		DesiredJobCode: "ENG-MGR", DesiredOrgUnit: "ENGINEERING", Authorize: authorize,
		Directory: []positionpicker.DirectoryEntry{
			{Position: authorizedPos, Title: "Engineering Manager", Manager: "Jane Smith", Location: "Remote"},
			{Position: unauthorizedPos, Title: "Should Never Appear", Manager: "Nobody", Location: "Nowhere"},
		},
	})
	if err != nil {
		t.Fatalf("ResolveCandidates: %v", err)
	}
	if len(candidates) != 1 || candidates[0].Position != authorizedPos {
		t.Fatalf("candidates = %+v, want exactly the one authorized position", candidates)
	}
	for _, c := range candidates {
		if c.Position == unauthorizedPos {
			t.Fatal("the unauthorized position must never appear in the picker's candidate list")
		}
	}

	// Half two: guessing the unauthorized position's real id directly is
	// refused with the identical Finding a nonexistent position produces.
	selUnauthorized := promoux004Selection(t, unauthorizedPos, mustRevision(t, 5))
	selUnauthorized.Authorize = authorize
	findingsUnauthorized, err := evaluateTargetPositionSelection(ctx, PreflightRequest{
		Tenant: promoux004Tenant(), Target: TargetPlacement{JobCode: "ENG-MGR", OrgUnit: "ENGINEERING"},
		PositionReader: reader, Reservations: &fakeAdmitter{admitted: true}, TargetPositionSelection: selUnauthorized,
	})
	if err != nil {
		t.Fatalf("evaluateTargetPositionSelection(unauthorized): %v", err)
	}

	nonexistentPos := promoux004Position(t, "77777777-7777-4777-8777-777777777777")
	selNonexistent := promoux004Selection(t, nonexistentPos, mustRevision(t, 1))
	findingsNonexistent, err := evaluateTargetPositionSelection(ctx, PreflightRequest{
		Tenant: promoux004Tenant(), Target: TargetPlacement{JobCode: "ENG-MGR", OrgUnit: "ENGINEERING"},
		PositionReader: reader, Reservations: &fakeAdmitter{admitted: true}, TargetPositionSelection: selNonexistent,
	})
	if err != nil {
		t.Fatalf("evaluateTargetPositionSelection(nonexistent): %v", err)
	}

	requireSoleFinding(t, findingsUnauthorized, nil, CodeTargetPositionNotFound)
	requireSoleFinding(t, findingsNonexistent, nil, CodeTargetPositionNotFound)
	if len(findingsUnauthorized) != 1 || len(findingsNonexistent) != 1 || !reflect.DeepEqual(findingsUnauthorized[0], findingsNonexistent[0]) {
		t.Fatalf("unauthorized refusal %+v must be byte-for-byte identical to the nonexistent refusal %+v -- otherwise the message itself discloses which guessed ids are real",
			findingsUnauthorized, findingsNonexistent)
	}
}

// -----------------------------------------------------------------------
// shared helpers
// -----------------------------------------------------------------------

func mustRevision(t testing.TB, seq uint64) values.RevisionToken {
	t.Helper()
	rev, err := values.NewSequenceRevision("promoux004.position.revision", seq)
	if err != nil {
		t.Fatalf("revision: %v", err)
	}
	return rev
}

func openInterval(t testing.TB, from string) values.EffectiveInterval {
	t.Helper()
	interval, err := values.NewOpenLocalDateInterval(promoux004Date(t, from), promoux004Calendar)
	if err != nil {
		t.Fatalf("open interval: %v", err)
	}
	return interval
}

func requireSoleFinding(t testing.TB, findings []Finding, err error, code string) {
	t.Helper()
	if err != nil {
		t.Fatalf("evaluateTargetPositionSelection: unexpected error %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %+v, want exactly one", findings)
	}
	if findings[0].Code != code {
		t.Fatalf("finding code = %q, want %q", findings[0].Code, code)
	}
	if findings[0].Severity != SeverityBlocking {
		t.Fatalf("finding severity = %v, want SeverityBlocking", findings[0].Severity)
	}
}
