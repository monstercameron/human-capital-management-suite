// Package workflowversionstore is the durable [version.Store] over the
// compiled workflow version registry (migration 00292). It keeps publication,
// activation, quarantine and the approval history truthful across restart
// (WF-COMP-006, WF-RUN-035): a version is published once as an immutable
// record whose content digest is re-verified on every read, every lifecycle
// transition appends to workflow_version_transition, and activation through
// [Store.ActivateApproved] requires a durable approval granted against the
// exact compiled-plan digest by an approver who is not the publisher.
package workflowversionstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

var (
	// ErrInvalid reports a missing dependency or malformed input.
	ErrInvalid = errors.New("workflowversionstore: invalid input")
	// ErrImmutable reports a Put that would change a published record's
	// content rather than only its lifecycle status and approval history.
	ErrImmutable = errors.New("workflowversionstore: published version content is immutable")
	// ErrNoApproval reports an activation with no durable approval recorded
	// against the version's compiled-plan digest.
	ErrNoApproval = errors.New("workflowversionstore: no durable approval for this version")
	// ErrSelfApproval reports an approval granted by the version's own
	// publisher.
	ErrSelfApproval = errors.New("workflowversionstore: the publisher cannot approve its own version")
)

// Store is the PostgreSQL-backed version registry. Its methods each run in
// their own transaction; [Store.BindTx] joins a caller's transaction instead.
type Store struct {
	DB dbport.Beginner
}

var _ version.TxBinder = Store{}

// BindTx implements [version.TxBinder].
func (s Store) BindTx(ctx context.Context, ex dbport.Conn) version.Store {
	return bound{ctx: ctx, ex: ex}
}

func (s Store) tx(ctx context.Context, fn func(bound) error) error {
	if s.DB == nil {
		return fmt.Errorf("%w: database is required", ErrInvalid)
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return fmt.Errorf("workflowversionstore: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(bound{ctx: ctx, ex: tx}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("workflowversionstore: commit: %w", err)
	}
	return nil
}

// Put implements [version.Store].
func (s Store) Put(v version.CompiledVersion) error {
	return s.tx(context.Background(), func(b bound) error { return b.Put(v) })
}

// GetByDigest implements [version.Store].
func (s Store) GetByDigest(digest string) (out version.CompiledVersion, found bool, err error) {
	err = s.tx(context.Background(), func(b bound) error {
		out, found, err = b.GetByDigest(digest)
		return err
	})
	return out, found, err
}

// GetActiveForWorkflow implements [version.Store].
func (s Store) GetActiveForWorkflow(workflowID string) (out version.CompiledVersion, found bool, err error) {
	err = s.tx(context.Background(), func(b bound) error {
		out, found, err = b.GetActiveForWorkflow(workflowID)
		return err
	})
	return out, found, err
}

// List implements [version.Store].
func (s Store) List(workflowID string) (out []version.CompiledVersion, err error) {
	err = s.tx(context.Background(), func(b bound) error {
		out, err = b.List(workflowID)
		return err
	})
	return out, err
}

// Approval is one durable activation approval.
type Approval struct {
	ApprovalID         uuid.UUID
	CompiledPlanDigest string
	// ReviewedPlanDigest is the digest the approver reviewed; activation
	// refuses CHANGED_AFTER_REVIEW when it differs from the version's.
	ReviewedPlanDigest string
	ApprovedBy         string
	Authority          string
	Reason             string
	TestsPassed        bool
	FixtureRefs        []string
	ApprovedAt         time.Time
}

// RecordApproval appends a durable approval for a published version. It is
// idempotent on ApprovalID, and refuses an approval by the version's own
// publisher.
func (s Store) RecordApproval(ctx context.Context, a Approval) error {
	return s.tx(ctx, func(b bound) error { return b.recordApproval(a) })
}

// ActivateApproved activates a published version on the evidence of its
// latest durable approval, in one transaction. An already ACTIVE version is
// returned unchanged, so a recomposed server activates nothing twice. With no
// approval -- or none newer than the version's last lifecycle transition -- it
// refuses [ErrNoApproval], and a version under a governed quarantine is
// refused [ErrQuarantined]: only [Store.LiftQuarantine] returns it to service.
// Every other gate (changed after review, failed tests, unresolved
// dependencies, a competing active version unless supersede is set) is
// [version.Activate]'s own typed refusal.
func (s Store) ActivateApproved(ctx context.Context, planDigest string, supersede bool) (out version.CompiledVersion, err error) {
	err = s.tx(ctx, func(b bound) error {
		if err := b.lock(planDigest); err != nil {
			return err
		}
		if _, quarantined, err := b.quarantinePolicy(planDigest); err != nil {
			return err
		} else if quarantined {
			return fmt.Errorf("%w: %s", ErrQuarantined, planDigest)
		}
		out, err = b.activateApproved(planDigest, supersede)
		return err
	})
	return out, err
}

func (b bound) lock(planDigest string) error {
	if _, err := b.ex.Exec(b.ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 22292))`, planDigest); err != nil {
		return fmt.Errorf("workflowversionstore: serialize version transition: %w", err)
	}
	return nil
}

func (b bound) recordApproval(a Approval) error {
	switch {
	case a.ApprovalID == uuid.Nil, a.CompiledPlanDigest == "", strings.TrimSpace(a.ApprovedBy) == "",
		strings.TrimSpace(a.Authority) == "", a.ApprovedAt.IsZero():
		return fmt.Errorf("%w: approval needs an id, digest, approver, authority and instant", ErrInvalid)
	}
	fixtures, err := json.Marshal(append([]string{}, a.FixtureRefs...))
	if err != nil {
		return fmt.Errorf("workflowversionstore: encode fixtures: %w", err)
	}
	v, found, err := b.GetByDigest(a.CompiledPlanDigest)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: no published version carries digest %s", ErrInvalid, a.CompiledPlanDigest)
	}
	if a.ApprovedBy == v.PublishedBy {
		return fmt.Errorf("%w: %s published %s", ErrSelfApproval, a.ApprovedBy, a.CompiledPlanDigest)
	}
	if _, err := b.ex.Exec(b.ctx, `INSERT INTO workflow_version_approval
		(approval_id, compiled_plan_digest, reviewed_plan_digest, approved_by, authority, reason, tests_passed, fixture_refs, approved_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) ON CONFLICT (approval_id) DO NOTHING`,
		a.ApprovalID, a.CompiledPlanDigest, a.ReviewedPlanDigest, a.ApprovedBy, a.Authority, a.Reason,
		a.TestsPassed, fixtures, a.ApprovedAt.UTC()); err != nil {
		return fmt.Errorf("workflowversionstore: record approval: %w", err)
	}
	return nil
}

func (b bound) activateApproved(planDigest string, supersede bool) (version.CompiledVersion, error) {
	v, found, err := b.GetByDigest(planDigest)
	if err != nil {
		return version.CompiledVersion{}, err
	}
	if found && v.Status == version.StatusActive {
		return v, nil
	}
	var ev version.ActivationEvidence
	err = b.ex.QueryRow(b.ctx, `SELECT approved_by, authority, reason, approved_at, reviewed_plan_digest, tests_passed
		FROM workflow_version_approval WHERE compiled_plan_digest = $1 ORDER BY approved_at DESC, approval_id DESC LIMIT 1`,
		planDigest).Scan(&ev.ApprovedBy, &ev.Authority, &ev.Reason, &ev.ApprovedAt, &ev.ReviewedPlanDigest, &ev.TestsPassed)
	if errors.Is(err, dbport.ErrNoRows) {
		return version.CompiledVersion{}, fmt.Errorf("%w: %s", ErrNoApproval, planDigest)
	}
	if err != nil {
		return version.CompiledVersion{}, fmt.Errorf("workflowversionstore: load approval: %w", err)
	}
	if found && len(v.Approvals) > 0 && !ev.ApprovedAt.After(v.Approvals[len(v.Approvals)-1].ApprovedAt) {
		// A version returns to service only on a review made after its last
		// lifecycle transition, never on the approval that first put it there.
		return version.CompiledVersion{}, fmt.Errorf("%w: %s has no approval newer than its last transition", ErrNoApproval, planDigest)
	}
	if found && ev.ApprovedBy == v.PublishedBy {
		return version.CompiledVersion{}, fmt.Errorf("%w: %s published %s", ErrSelfApproval, ev.ApprovedBy, planDigest)
	}
	ev.Authorized, ev.SupersedeActive, ev.ApprovedAt = true, supersede, ev.ApprovedAt.UTC()
	return version.Activate(b, planDigest, ev)
}

// bound is the registry joined to one open transaction or connection.
type bound struct {
	ctx context.Context
	ex  dbport.Conn
}

func (b bound) Put(v version.CompiledVersion) error {
	if v.WorkflowID == "" || v.CompiledPlanDigest == "" || v.Digest() == "" {
		return fmt.Errorf("%w: a version needs a workflow id, compiled-plan digest and record digest", ErrInvalid)
	}
	if err := v.Verify(); err != nil {
		return err
	}
	record, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("workflowversionstore: encode version: %w", err)
	}
	var storedDigest string
	err = b.ex.QueryRow(b.ctx, `SELECT record_digest FROM workflow_compiled_version WHERE compiled_plan_digest = $1 FOR UPDATE`,
		v.CompiledPlanDigest).Scan(&storedDigest)
	switch {
	case errors.Is(err, dbport.ErrNoRows):
		if _, err := b.ex.Exec(b.ctx, `INSERT INTO workflow_compiled_version
			(compiled_plan_digest, workflow_id, semantic_version, record_digest, record, status, published_at, published_by, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $7)`,
			v.CompiledPlanDigest, v.WorkflowID, v.SemanticVersion, v.Digest(), record, string(v.Status),
			v.PublishedAt.UTC(), v.PublishedBy); err != nil {
			return fmt.Errorf("workflowversionstore: insert version: %w", err)
		}
	case err != nil:
		return fmt.Errorf("workflowversionstore: load version: %w", err)
	case storedDigest != v.Digest():
		return fmt.Errorf("%w: %s", ErrImmutable, v.CompiledPlanDigest)
	default:
		updatedAt := v.PublishedAt
		if n := len(v.Approvals); n > 0 {
			updatedAt = v.Approvals[n-1].ApprovedAt
		}
		if _, err := b.ex.Exec(b.ctx, `UPDATE workflow_compiled_version SET status = $2, record = $3, updated_at = $4
			WHERE compiled_plan_digest = $1`, v.CompiledPlanDigest, string(v.Status), record, updatedAt.UTC()); err != nil {
			return fmt.Errorf("workflowversionstore: update version status: %w", err)
		}
	}
	return b.appendTransitions(v)
}

// appendTransitions records every approval in v's history the transition
// table does not hold yet. History is append-only, so the rows already
// written are a prefix of v.Approvals.
func (b bound) appendTransitions(v version.CompiledVersion) error {
	var recorded int
	if err := b.ex.QueryRow(b.ctx, `SELECT count(*) FROM workflow_version_transition WHERE compiled_plan_digest = $1`,
		v.CompiledPlanDigest).Scan(&recorded); err != nil {
		return fmt.Errorf("workflowversionstore: count transitions: %w", err)
	}
	if recorded > len(v.Approvals) {
		return fmt.Errorf("%w: %s would drop recorded lifecycle history", ErrImmutable, v.CompiledPlanDigest)
	}
	for i, a := range v.Approvals[recorded:] {
		if _, err := b.ex.Exec(b.ctx, `INSERT INTO workflow_version_transition
			(compiled_plan_digest, sequence, status, approved_by, authority, reason, approved_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			v.CompiledPlanDigest, recorded+i+1, string(a.Result), a.ApprovedBy, a.Authority, a.Reason, a.ApprovedAt.UTC()); err != nil {
			return fmt.Errorf("workflowversionstore: record transition: %w", err)
		}
	}
	return nil
}

const selectVersion = `SELECT record, record_digest, status FROM workflow_compiled_version`

func (b bound) scan(row dbport.Row) (version.CompiledVersion, error) {
	var record []byte
	var digest, status string
	if err := row.Scan(&record, &digest, &status); err != nil {
		return version.CompiledVersion{}, err
	}
	var v version.CompiledVersion
	if err := json.Unmarshal(record, &v); err != nil {
		return version.CompiledVersion{}, fmt.Errorf("workflowversionstore: decode version: %w", err)
	}
	v.Status = version.ActivationStatus(status)
	return version.Restore(v, digest)
}

func (b bound) GetByDigest(digest string) (version.CompiledVersion, bool, error) {
	v, err := b.scan(b.ex.QueryRow(b.ctx, selectVersion+` WHERE compiled_plan_digest = $1`, digest))
	if errors.Is(err, dbport.ErrNoRows) {
		return version.CompiledVersion{}, false, nil
	}
	if err != nil {
		return version.CompiledVersion{}, false, err
	}
	return v, true, nil
}

func (b bound) GetActiveForWorkflow(workflowID string) (version.CompiledVersion, bool, error) {
	v, err := b.scan(b.ex.QueryRow(b.ctx, selectVersion+` WHERE workflow_id = $1 AND status = 'ACTIVE'`, workflowID))
	if errors.Is(err, dbport.ErrNoRows) {
		return version.CompiledVersion{}, false, nil
	}
	if err != nil {
		return version.CompiledVersion{}, false, err
	}
	return v, true, nil
}

func (b bound) List(workflowID string) ([]version.CompiledVersion, error) {
	rows, err := b.ex.Query(b.ctx, selectVersion+` WHERE workflow_id = $1 ORDER BY published_at, compiled_plan_digest`, workflowID)
	if err != nil {
		return nil, fmt.Errorf("workflowversionstore: list versions: %w", err)
	}
	defer rows.Close()
	var out []version.CompiledVersion
	for rows.Next() {
		v, err := b.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("workflowversionstore: list versions: %w", err)
	}
	return out, nil
}
