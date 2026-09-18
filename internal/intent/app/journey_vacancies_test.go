package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/positionfacts"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// UXLIVE-011: these tests cover the projection that turns rows into the list
// the propose form offers. The decisions themselves belong to
// internal/domains/position and internal/domains/promotion/positionpicker and
// are tested there; what is proved here is that this layer asks them, one
// position at a time with that position's own occupancy, and publishes only
// what came back.

type vacancyDirectory struct {
	rows []positionfacts.DirectoryRow
	err  error
	// asked records the coordinate the caller read at.
	asked position.AsOf
}

func (d *vacancyDirectory) Directory(_ context.Context, _ values.TenantId, asOf position.AsOf) ([]positionfacts.DirectoryRow, error) {
	d.asked = asOf
	return d.rows, d.err
}

// vacancyFacts answers one revision per position and records which positions
// it was asked about, so a test can prove the picker was consulted rather
// than bypassed.
type vacancyFacts struct {
	revisions map[string]position.PositionRevision
	asked     []string
}

func (f *vacancyFacts) PositionRevisionAt(_ context.Context, q position.PositionQuery) (position.PositionRevision, bool, error) {
	f.asked = append(f.asked, q.Position.Id)
	rev, ok := f.revisions[q.Position.Id]
	if !ok {
		return position.PositionRevision{}, false, nil
	}
	return rev, true, nil
}

func vacancyTenant() values.TenantId { return values.TenantId("acme-corp") }

func vacancyRevision(t *testing.T, id, jobCode, orgUnit string, capacity string, lifecycle position.Lifecycle) position.PositionRevision {
	t.Helper()
	from, err := values.ParseLocalDate("2020-01-01")
	if err != nil {
		t.Fatalf("ParseLocalDate: %v", err)
	}
	effective, err := values.NewOpenLocalDateInterval(from,
		values.CalendarRef{Ref: "hcmnext.position.job_position", Version: "1"})
	if err != nil {
		t.Fatalf("NewOpenLocalDateInterval: %v", err)
	}
	fte, err := values.NewDecimal(capacity, 4, values.RoundingExactRequired)
	if err != nil {
		t.Fatalf("NewDecimal: %v", err)
	}
	revision, err := values.NewOpaqueRevision("aggregate.job_position."+id, []byte("digest-"+id))
	if err != nil {
		t.Fatalf("NewOpaqueRevision: %v", err)
	}
	recordedAt, err := values.NewRecordedAt(values.NewInstant(time.Date(2020, 1, 2, 9, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("NewRecordedAt: %v", err)
	}
	heads := int64(0)
	if fte.Sign() > 0 {
		heads = 1
	}
	return position.PositionRevision{
		Position:    values.EntityRef{Tenant: vacancyTenant(), Kind: position.KindPosition, Id: id},
		Revision:    revision,
		Effective:   effective,
		Lifecycle:   lifecycle,
		JobCode:     jobCode,
		OrgUnit:     orgUnit,
		LegalEntity: "ACME US Inc.",
		Capacity:    position.CapacityPolicy{CapacityFTE: fte, CapacityHeads: heads},
		Authority: evidence.SourceAuthority{
			Kind: evidence.AuthorityLocal, System: "hcmnext.position", PolicyRef: "position.source_authority/2026.1",
		},
		Provenance: evidence.Provenance{
			Source: "hcmnext.position", EvidenceRef: "digest-" + id, RecordedAt: recordedAt,
		},
	}
}

func vacancyRow(id, title, jobCode, orgUnit string, occupants ...position.Occupant) positionfacts.DirectoryRow {
	return positionfacts.DirectoryRow{
		Position:     values.EntityRef{Tenant: vacancyTenant(), Kind: position.KindPosition, Id: id},
		Title:        title,
		Organization: "Engineering",
		Location:     "Remote",
		JobCode:      jobCode,
		OrgUnit:      orgUnit,
		Occupants:    occupants,
	}
}

func vacancyOccupant(t *testing.T, worker, fteText string) position.Occupant {
	t.Helper()
	from, err := values.ParseLocalDate("2020-01-01")
	if err != nil {
		t.Fatalf("ParseLocalDate: %v", err)
	}
	effective, err := values.NewOpenLocalDateInterval(from,
		values.CalendarRef{Ref: "hcmnext.position.job_position", Version: "1"})
	if err != nil {
		t.Fatalf("NewOpenLocalDateInterval: %v", err)
	}
	fte, err := values.NewDecimal(fteText, 4, values.RoundingExactRequired)
	if err != nil {
		t.Fatalf("NewDecimal: %v", err)
	}
	return position.Occupant{
		Worker:    values.EntityRef{Tenant: vacancyTenant(), Kind: position.KindWorker, Id: worker},
		FTE:       fte,
		Effective: effective,
		Exclusive: true,
	}
}

func vacancyEngine(directory PositionDirectorySource, facts position.PositionFacts) *journeyEngine {
	return &journeyEngine{
		now:            func() time.Time { return time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC) },
		positions:      directory,
		positionReader: facts,
	}
}

// TestPositionVacanciesPublishesOnlyOpenPositions proves the projection is a
// filter and not a listing: an open position is offered with its
// server-issued reference, and a full one is absent.
func TestPositionVacanciesPublishesOnlyOpenPositions(t *testing.T) {
	facts := &vacancyFacts{revisions: map[string]position.PositionRevision{
		"11111111-1111-4111-8111-000000000001": vacancyRevision(t, "11111111-1111-4111-8111-000000000001", "ENG-MGR", "ENGINEERING", "1.0000", position.LifecycleOpen),
		"11111111-1111-4111-8111-000000000002": vacancyRevision(t, "11111111-1111-4111-8111-000000000002", "CARE-COORD", "CARE", "1.0000", position.LifecycleOpen),
	}}
	directory := &vacancyDirectory{rows: []positionfacts.DirectoryRow{
		vacancyRow("11111111-1111-4111-8111-000000000001", "Engineering Manager", "ENG-MGR", "ENGINEERING"),
		vacancyRow("11111111-1111-4111-8111-000000000002", "Care Coordinator", "CARE-COORD", "CARE",
			vacancyOccupant(t, "22222222-2222-4222-8222-000000000001", "1.0000")),
	}}

	options, err := vacancyEngine(directory, facts).positionVacancies(context.Background(), gatePrincipal(t, "promotion_operator"))
	if err != nil {
		t.Fatalf("positionVacancies: %v", err)
	}
	if len(options) != 1 {
		t.Fatalf("published %d vacancies, want only the open one: %+v", len(options), options)
	}
	if options[0].Title != "Engineering Manager" || options[0].JobCode != "ENG-MGR" || options[0].OrgUnit != "ENGINEERING" {
		t.Fatalf("the published option is %+v, want the open position's own labels and codes", options[0])
	}
	if options[0].Reference == "" || options[0].Reference == options[0].Title {
		t.Fatalf("Reference = %q, want the server-issued revision reference", options[0].Reference)
	}
	if options[0].ReservationState != "AVAILABLE" {
		t.Fatalf("ReservationState = %q, want the domain's disclosed state", options[0].ReservationState)
	}

	// Both positions were put to the domain; the full one was refused there,
	// not skipped here.
	if len(facts.asked) < 2 {
		t.Fatalf("the projection asked about %d positions, want both", len(facts.asked))
	}
	if !directory.asked.EffectiveOn.IsSet() {
		t.Fatalf("the directory was read at an unset coordinate")
	}
}

// TestPositionVacanciesExcludesClosedAndFrozenPositions proves this layer
// publishes only what the domain returned: a position that exists but is not
// open never reaches the form.
func TestPositionVacanciesExcludesClosedAndFrozenPositions(t *testing.T) {
	for name, lifecycle := range map[string]position.Lifecycle{
		"closed": position.LifecycleClosed,
		"frozen": position.LifecycleFrozen,
	} {
		id := "11111111-1111-4111-8111-000000000003"
		facts := &vacancyFacts{revisions: map[string]position.PositionRevision{
			id: vacancyRevision(t, id, "ENG-MGR", "ENGINEERING", "1.0000", lifecycle),
		}}
		directory := &vacancyDirectory{rows: []positionfacts.DirectoryRow{vacancyRow(id, "Engineering Manager", "ENG-MGR", "ENGINEERING")}}

		options, err := vacancyEngine(directory, facts).positionVacancies(context.Background(), gatePrincipal(t, "promotion_operator"))
		if err != nil {
			t.Fatalf("%s: positionVacancies: %v", name, err)
		}
		if len(options) != 0 {
			t.Fatalf("%s: a %s position was offered as a vacancy: %+v", name, name, options)
		}
	}
}

// TestPositionVacanciesIsSilentWhenItCannotRead proves the cell without a
// position read publishes nothing and fails nothing. The form's own
// no-vacancy state is what a reader then sees; the one answer that must
// never come back is a free-text box, and an empty list cannot become one.
func TestPositionVacanciesIsSilentWhenItCannotRead(t *testing.T) {
	principal := gatePrincipal(t, "promotion_operator")
	facts := &vacancyFacts{revisions: map[string]position.PositionRevision{}}
	directory := &vacancyDirectory{}

	for name, engine := range map[string]*journeyEngine{
		"no directory": vacancyEngine(nil, facts),
		"no reader":    vacancyEngine(directory, nil),
		"neither":      vacancyEngine(nil, nil),
	} {
		options, err := engine.positionVacancies(context.Background(), principal)
		if err != nil {
			t.Errorf("%s: a cell that cannot read positions reported a fault: %v", name, err)
		}
		if len(options) != 0 {
			t.Errorf("%s: published %d vacancies with nothing to read them from", name, len(options))
		}
	}

	// No principal is the same: nothing to project for.
	options, err := vacancyEngine(directory, facts).positionVacancies(context.Background(), nil)
	if err != nil || len(options) != 0 {
		t.Fatalf("an unauthenticated read = (%+v, %v), want no options and no fault", options, err)
	}
}

// TestPositionVacanciesReportsAReadFailure proves a broken read is an error
// rather than an empty list. "No vacancy" and "I could not look" are
// different answers, and a form that showed the first for the second would
// be telling a reader something untrue about their organization.
func TestPositionVacanciesReportsAReadFailure(t *testing.T) {
	broken := errors.New("directory unavailable")
	engine := vacancyEngine(&vacancyDirectory{err: broken}, &vacancyFacts{})

	options, err := engine.positionVacancies(context.Background(), gatePrincipal(t, "promotion_operator"))
	if !errors.Is(err, broken) {
		t.Fatalf("err = %v, want the read failure", err)
	}
	if len(options) != 0 {
		t.Fatalf("a failed read still published %d vacancies", len(options))
	}
}
