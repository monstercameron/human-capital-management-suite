// Package compfacts is Unit 4's production, database-backed
// internal/domains/rewards.CompensationFacts adapter: the read half of
// "simulate the approved proposal against current compensation facts," over
// the DB-010 compensation_package/compensation_component tables read through
// internal/data/aggregates.
//
// There is no judgment here. A worker with no active package, a tenant this
// reader has no physical mapping for, or a coordinate that matches no row is
// reported as absent, never guessed at or defaulted. The disclosure decision
// (purpose, scopes, field rulings) is supplied by the caller -- the served
// simulation composer in internal/intent/app -- exactly like
// internal/data/positionfacts only turns rows into the typed facts they
// describe.
package compfacts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// compensationAuthority names the source authority of the aggregates
// compensation rows this reader discloses, parallel to the positionfacts
// precedent for aggregates position rows.
const compensationAuthority = "compensation.source_authority/2026.1"

// storedMoneyScale is the exact-decimal scale the aggregates compensation
// tables store amounts at; it matches the payroll observation's own parsing
// of the same column rather than assuming a per-currency default.
const storedMoneyScale = 4

// Reader is the rewards.CompensationFacts adapter.
type Reader struct {
	// DB opens the tenant-scoped, read-only transaction each read runs
	// inside. Every read below opens its own transaction and rolls it back:
	// this port never writes.
	DB dbport.Beginner
	// TenantUUID resolves the domain's logical tenant key to the physical
	// tenant UUID the aggregates tables key on.
	TenantUUID func(values.TenantId) uuid.UUID
	// Worker reads the subject's current placement for the pay band
	// reference the port contract requires. The band code is evaluated
	// through Bands from the same placement the snapshot's own band inputs
	// use, so the two can never disagree.
	Worker people.WorkerFacts
	// Bands is the pay band catalog the band reference is evaluated
	// through.
	Bands rewards.PayBandCatalog
	// PolicyVersion stamps the disclosure policy version on returned fact
	// sets. It names the served simulation capability version that admitted
	// the read; the served composer supplies it, this reader never invents
	// one.
	PolicyVersion string
}

var _ rewards.CompensationFacts = Reader{}

// CompensationFactsAt implements rewards.CompensationFacts over the active
// compensation package and its base-pay component at the query coordinate.
//
// A package whose component frequency names no known pay basis, or a worker
// whose placement resolves to no catalog band, is reported as absent rather
// than disclosed with a guessed basis or band: piece-rate rows cannot
// annualize without declared units, and anything else unrecognized is a
// data-integrity gap, not a caller error.
func (r Reader) CompensationFactsAt(ctx context.Context, q rewards.CompensationFactsQuery) (rewards.CompensationFactSet, error) {
	if r.DB == nil || r.TenantUUID == nil || r.Worker == nil || r.Bands == nil {
		return rewards.CompensationFactSet{}, fmt.Errorf("compfacts: reader has no database, tenant resolver, worker facts and band catalog configured")
	}
	workerID, err := uuid.Parse(q.Worker.Id)
	if err != nil {
		// Not a canonical worker identifier at all -- reported as absent,
		// never as an error. A guessed or malformed id is exactly what
		// this branch exists to refuse without disclosing anything about
		// why.
		return rewards.CompensationFactSet{Worker: q.Worker}, nil
	}
	tenantID := r.TenantUUID(q.Worker.Tenant)
	if tenantID == uuid.Nil {
		return rewards.CompensationFactSet{Worker: q.Worker}, nil
	}
	// The coordinate is a single instant for both axes: the read answers
	// the facts current at that instant as known at that instant.
	businessAt := q.AsOf.Time()
	knownAt := q.AsOf.Time()

	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return rewards.CompensationFactSet{}, fmt.Errorf("compfacts: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return rewards.CompensationFactSet{}, fmt.Errorf("compfacts: scope tenant: %w", err)
	}

	store := aggregates.CompensationStore{}
	active, err := store.ActivePackageForWorker(ctx, tx, tenantID, workerID, businessAt)
	if err != nil {
		if errors.Is(err, aggregates.ErrNotFound) {
			return rewards.CompensationFactSet{Worker: q.Worker}, nil
		}
		return rewards.CompensationFactSet{}, fmt.Errorf("compfacts: active package: %w", err)
	}
	pkg, err := store.KnownAsOfCompensationPackage(ctx, tx, tenantID, active.EntityID, businessAt, knownAt)
	if err != nil {
		if errors.Is(err, aggregates.ErrNotFound) {
			return rewards.CompensationFactSet{Worker: q.Worker}, nil
		}
		return rewards.CompensationFactSet{}, fmt.Errorf("compfacts: known package: %w", err)
	}
	current, err := store.BasePayComponentForPackage(ctx, tx, tenantID, pkg.EntityID, businessAt)
	if err != nil {
		if errors.Is(err, aggregates.ErrNotFound) {
			return rewards.CompensationFactSet{Worker: q.Worker}, nil
		}
		return rewards.CompensationFactSet{}, fmt.Errorf("compfacts: base pay component: %w", err)
	}
	base, err := store.KnownAsOfCompensationComponent(ctx, tx, tenantID, current.EntityID, businessAt, knownAt)
	if err != nil {
		if errors.Is(err, aggregates.ErrNotFound) {
			return rewards.CompensationFactSet{Worker: q.Worker}, nil
		}
		return rewards.CompensationFactSet{}, fmt.Errorf("compfacts: known base pay component: %w", err)
	}

	amount, err := values.NewDecimal(base.Amount, storedMoneyScale, values.RoundingExactRequired)
	if err != nil {
		return rewards.CompensationFactSet{}, fmt.Errorf("compfacts: base pay amount: %w", err)
	}
	pay, err := values.NewMoney(amount.String(), base.Currency, storedMoneyScale, values.RoundingExactRequired)
	if err != nil {
		return rewards.CompensationFactSet{}, fmt.Errorf("compfacts: base pay money: %w", err)
	}
	basis, ok := payBasisForFrequency(base.Frequency)
	if !ok {
		return rewards.CompensationFactSet{Worker: q.Worker}, nil
	}
	bandRef, err := r.bandFor(ctx, q, businessAt, pay)
	if err != nil {
		return rewards.CompensationFactSet{}, err
	}
	if bandRef == "" {
		return rewards.CompensationFactSet{Worker: q.Worker}, nil
	}

	recordedAt, err := values.NewRecordedAt(values.NewInstant(base.RecordedAt.UTC()))
	if err != nil {
		return rewards.CompensationFactSet{}, fmt.Errorf("compfacts: base pay recorded_at: %w", err)
	}
	// Compensation rows carry no separate knowledge timestamp: the fact
	// became known when its row was recorded, so known-at is recorded-at
	// and the knowledge order holds by construction.
	factKnownAt, err := values.NewKnownAt(values.NewInstant(base.RecordedAt.UTC()))
	if err != nil {
		return rewards.CompensationFactSet{}, fmt.Errorf("compfacts: base pay known_at: %w", err)
	}
	revision, err := values.NewOpaqueRevision("aggregate.compensation_component."+base.EntityID.String(), []byte(base.Digest))
	if err != nil {
		return rewards.CompensationFactSet{}, fmt.Errorf("compfacts: base pay revision: %w", err)
	}
	instant, err := values.NewOpenInstantInterval(values.NewInstant(businessAt))
	if err != nil {
		return rewards.CompensationFactSet{}, fmt.Errorf("compfacts: effective instant: %w", err)
	}

	return rewards.CompensationFactSet{
		Worker:        q.Worker,
		Exists:        true,
		Watermark:     revision,
		PolicyVersion: r.PolicyVersion,
		Fact: rewards.CompensationFact{
			Worker:     q.Worker,
			BasePay:    pay,
			PayBandRef: bandRef,
			Currency:   base.Currency,
			PayBasis:   basis,
			Frequency:  base.Frequency,
			Components: []rewards.CompensationComponent{{
				ID:     base.EntityID.String(),
				Kind:   base.ComponentType,
				Amount: pay,
			}},
			Effective: instant,
			KnownAt:   factKnownAt,
			Revision:  revision,
			Authority: evidence.SourceAuthority{
				Kind: evidence.AuthorityLocal, System: "hcmnext.compensation", PolicyRef: compensationAuthority,
			},
			Provenance: evidence.Provenance{
				Source: "hcmnext.compensation", EvidenceRef: base.Digest, RecordedAt: recordedAt,
			},
		},
	}, nil
}

// bandFor evaluates the worker's band for the base pay through the catalog
// the snapshot's own band inputs use, from the worker's current placement,
// and returns its code. An empty code (no band holds this placement) is
// reported, never defaulted: the caller treats it as an absent fact.
func (r Reader) bandFor(ctx context.Context, q rewards.CompensationFactsQuery, businessAt time.Time, pay values.Money) (string, error) {
	effectiveOn, err := values.NewLocalDate(businessAt.Year(), businessAt.Month(), businessAt.Day())
	if err != nil {
		return "", fmt.Errorf("compfacts: coordinate date: %w", err)
	}
	knownAt, err := values.NewKnownAt(q.AsOf)
	if err != nil {
		return "", fmt.Errorf("compfacts: known-at: %w", err)
	}
	set, err := r.Worker.WorkerFactsAt(ctx, people.FactQuery{
		Tenant: q.Worker.Tenant,
		Worker: q.Worker,
		AsOf:   people.AsOf{EffectiveOn: effectiveOn, KnownAt: knownAt},
		Fields: []people.FieldID{people.FieldJobCode, people.FieldGrade, people.FieldPayZone},
	})
	if err != nil {
		return "", fmt.Errorf("compfacts: worker placement: %w", err)
	}
	if !set.Exists {
		return "", nil
	}
	placement := factValues(set.Facts)
	record, err := r.Bands.LookupBand(ctx, rewards.BandQuery{
		Tenant:   q.Worker.Tenant,
		JobCode:  placement[people.FieldJobCode],
		Grade:    placement[people.FieldGrade],
		PayZone:  placement[people.FieldPayZone],
		Currency: pay.Currency(),
		AsOf:     effectiveOn,
	})
	if err != nil {
		if errors.Is(err, rewards.ErrBandNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("compfacts: pay band lookup: %w", err)
	}
	return record.Band.ID, nil
}

// factValues indexes field facts by field id for placement reads.
func factValues(facts []people.Fact) map[people.FieldID]string {
	out := make(map[people.FieldID]string, len(facts))
	for _, f := range facts {
		if v, ok := f.Value.Get(); ok {
			out[f.Field] = v
		}
	}
	return out
}

// payBasisForFrequency maps the stored pay frequency to its pay basis. The
// mapping covers exactly the frequencies the rewards basis vocabulary can
// name: biweekly, weekly and one-time rows have no member there, so they
// report false and the fact is withheld rather than disclosed with a guessed
// basis. That matches the kernel's own closed vocabulary -- extending it is
// a canonical-encoding change, not a read-path decision -- and the snapshot
// records the withhold with its reason instead of simulating on a lie.
func payBasisForFrequency(frequency string) (rewards.PayBasis, bool) {
	switch frequency {
	case "HOURLY":
		return rewards.PayBasisHourly, true
	case "MONTHLY":
		return rewards.PayBasisMonthlySalary, true
	case "ANNUAL":
		return rewards.PayBasisAnnualSalary, true
	default:
		return rewards.PayBasisUnspecified, false
	}
}
