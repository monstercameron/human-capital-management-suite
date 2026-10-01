package timestore

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// isUniqueViolation is defined in sessions.go and shared across this
// package's files.

// LedgerIntervalKind is the kind of one recorded working-time interval.
type LedgerIntervalKind string

const (
	LedgerWorked     LedgerIntervalKind = "WORKED"
	LedgerRest       LedgerIntervalKind = "REST"
	LedgerSchoolWeek LedgerIntervalKind = "SCHOOL_WEEK"
)

// LedgerInterval is one append-only slice of a worker aggregation key's
// working time (WTIME-007). Domain code, not this store, computes rolling
// windows (hours per day, week, 14 days, consecutive days, and so on) from
// the slices this store returns; the store's job is only to keep every
// slice ever recorded and the ledger's current revision.
type LedgerInterval struct {
	TenantID       string
	ID             string
	AggregationKey string
	LedgerRevision int64
	Kind           LedgerIntervalKind
	Start          time.Time
	End            time.Time
	Minutes        int64
	SourceRef      string
	CreatedAt      time.Time
}

// LedgerSlice is a bounded read of a ledger: the intervals a caller asked
// for plus the ledger's revision as of that read, so a DECISION can record
// exactly the revision it observed.
type LedgerSlice struct {
	Revision  int64
	Intervals []LedgerInterval
}

// AppendLedgerInterval records one interval and advances the ledger's
// revision. The ledger row is created lazily on first append. It never
// mutates or removes an interval already recorded -- a correction appends
// a new interval and the caller's revision comparison against the observed
// window notices the change.
func (s *Store) AppendLedgerInterval(ctx context.Context, tenant, aggregationKey string, in LedgerInterval) (int64, error) {
	if tenant == "" || aggregationKey == "" || in.AggregationKey != aggregationKey || !validLedgerKind(in.Kind) ||
		in.Start.IsZero() || in.End.IsZero() || !in.End.After(in.Start) || in.Minutes < 0 {
		return 0, ErrInvalid
	}
	var newRevision int64
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO working_time_ledger(tenant_id,aggregation_key,revision) VALUES($1,$2,0) ON CONFLICT (tenant_id,aggregation_key) DO NOTHING`, tenant, aggregationKey); err != nil {
			return err
		}
		row := tx.QueryRow(ctx, `UPDATE working_time_ledger SET revision=revision+1,updated_at=now() WHERE tenant_id=$1 AND aggregation_key=$2 RETURNING revision`, tenant, aggregationKey)
		if err := row.Scan(&newRevision); err != nil {
			return err
		}
		id := in.ID
		if id == "" {
			id = uuid.NewString()
		}
		_, err := tx.Exec(ctx, `INSERT INTO working_time_ledger_interval(tenant_id,id,aggregation_key,ledger_revision,kind,interval_start,interval_end,minutes,source_ref) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			tenant, id, aggregationKey, newRevision, string(in.Kind), in.Start, in.End, in.Minutes, in.SourceRef)
		return err
	})
	if err != nil {
		return 0, err
	}
	return newRevision, nil
}

func validLedgerKind(k LedgerIntervalKind) bool {
	switch k {
	case LedgerWorked, LedgerRest, LedgerSchoolWeek:
		return true
	}
	return false
}

// LedgerRevision returns the current revision for aggregationKey, or 0 with
// no error when the ledger has never had an interval appended.
func (s *Store) LedgerRevision(ctx context.Context, tenant, aggregationKey string) (int64, error) {
	if tenant == "" || aggregationKey == "" {
		return 0, ErrInvalid
	}
	var revision int64
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		err := tx.QueryRow(ctx, `SELECT revision FROM working_time_ledger WHERE tenant_id=$1 AND aggregation_key=$2`, tenant, aggregationKey).Scan(&revision)
		if errors.Is(err, dbport.ErrNoRows) {
			revision = 0
			return nil
		}
		return err
	})
	if err != nil {
		return 0, err
	}
	return revision, nil
}

// ReadLedgerSlice returns every interval of kind (or every kind when kind is
// empty) whose window overlaps [from, to), pinned to the ledger's revision
// as observed inside the same transaction as the read -- exactly the
// "OBSERVE capability returning a revision" WTIME-007 asks for.
func (s *Store) ReadLedgerSlice(ctx context.Context, tenant, aggregationKey string, kind LedgerIntervalKind, from, to time.Time) (LedgerSlice, error) {
	if tenant == "" || aggregationKey == "" || from.IsZero() || to.IsZero() || !to.After(from) {
		return LedgerSlice{}, ErrInvalid
	}
	var out LedgerSlice
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		err := tx.QueryRow(ctx, `SELECT coalesce((SELECT revision FROM working_time_ledger WHERE tenant_id=$1 AND aggregation_key=$2),0)`, tenant, aggregationKey).Scan(&out.Revision)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT tenant_id,id,aggregation_key,ledger_revision,kind,interval_start,interval_end,minutes,source_ref,created_at
			FROM working_time_ledger_interval WHERE tenant_id=$1 AND aggregation_key=$2 AND interval_start<$3 AND interval_end>$4 AND ($5='' OR kind=$5)
			ORDER BY interval_start`, tenant, aggregationKey, to, from, string(kind))
		if err != nil {
			return err
		}
		defer rows.Close()
		out.Intervals = make([]LedgerInterval, 0)
		for rows.Next() {
			var iv LedgerInterval
			var k string
			if err := rows.Scan(&iv.TenantID, &iv.ID, &iv.AggregationKey, &iv.LedgerRevision, &k, &iv.Start, &iv.End, &iv.Minutes, &iv.SourceRef, &iv.CreatedAt); err != nil {
				return err
			}
			iv.Kind = LedgerIntervalKind(k)
			out.Intervals = append(out.Intervals, iv)
		}
		return rows.Err()
	})
	if err != nil {
		return LedgerSlice{}, err
	}
	return out, nil
}

// RecordLedgerDecision guards a workflow DECISION against the ledger
// revision it observed: expectedRevision must equal the revision recorded
// at the time of the call, and no other decision may already have claimed
// that exact (aggregation key, expected revision) pair. Two concurrent
// decisions presenting the same expected revision can only have one
// winner; the second observes ErrRevisionConflict rather than both
// approving against a since-stale window (WTIME-007 Race).
func (s *Store) RecordLedgerDecision(ctx context.Context, tenant, aggregationKey string, expectedRevision int64, decisionRef string) error {
	if tenant == "" || aggregationKey == "" || expectedRevision < 0 || decisionRef == "" {
		return ErrInvalid
	}
	return s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var current int64
		err := tx.QueryRow(ctx, `SELECT coalesce((SELECT revision FROM working_time_ledger WHERE tenant_id=$1 AND aggregation_key=$2),0)`, tenant, aggregationKey).Scan(&current)
		if err != nil {
			return err
		}
		if current != expectedRevision {
			return ErrRevisionConflict
		}
		_, err = tx.Exec(ctx, `INSERT INTO working_time_ledger_decision(tenant_id,id,aggregation_key,expected_revision,decision_ref) VALUES($1,$2,$3,$4,$5)`,
			tenant, uuid.NewString(), aggregationKey, expectedRevision, decisionRef)
		if err != nil {
			if isUniqueViolation(err) {
				return ErrRevisionConflict
			}
			return err
		}
		return nil
	})
}
