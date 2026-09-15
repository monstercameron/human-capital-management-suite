package workflowversionstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// LivePolicy is the disposition every live instance of a quarantined version
// takes at its next advancement (WF-RUN-009).
type LivePolicy string

const (
	// LivePause pauses a live instance at its next safe point.
	LivePause LivePolicy = "PAUSE"
	// LiveContinue lets a live instance run to completion.
	LiveContinue LivePolicy = "CONTINUE"
	// LiveBlock refuses every further advancement until the quarantine is
	// lifted.
	LiveBlock LivePolicy = "BLOCK"
)

func (p LivePolicy) valid() bool { return p == LivePause || p == LiveContinue || p == LiveBlock }

var (
	// ErrQuarantined reports an activation of a version under a governed
	// quarantine; only [Store.LiftQuarantine] returns it to service.
	ErrQuarantined = errors.New("workflowversionstore: version is under a governed quarantine")
	// ErrNotQuarantined reports a lift of a version with no standing
	// quarantine declaration.
	ErrNotQuarantined = errors.New("workflowversionstore: version has no standing quarantine")
	// ErrSameReviewer reports a quarantine approved, or lifted, by the
	// principal who declared it.
	ErrSameReviewer = errors.New("workflowversionstore: the declarer cannot also approve or lift the quarantine")
)

// QuarantineDeclaration pulls an ACTIVE version from service.
type QuarantineDeclaration struct {
	DeclarationID      uuid.UUID
	CompiledPlanDigest string
	Reason             string
	// EvidenceRef names the incident evidence the quarantine rests on.
	EvidenceRef string
	DeclaredBy  string
	// ApprovedBy is the second principal who approved the declaration. It
	// must differ from DeclaredBy.
	ApprovedBy string
	Authority  string
	LivePolicy LivePolicy
	RecordedAt time.Time
}

// QuarantineLift returns a quarantined version to service.
type QuarantineLift struct {
	DeclarationID      uuid.UUID
	CompiledPlanDigest string
	// ReviewedBy must differ from the principal who declared the quarantine.
	ReviewedBy string
	// ValidationEvidenceRef names the validation that justifies the release.
	ValidationEvidenceRef string
	Reason                string
	Authority             string
	TestsPassed           bool
	RecordedAt            time.Time
}

// Quarantine records a governed quarantine and moves the version out of
// service in one transaction. New starts need an ACTIVE version, so they are
// refused from the moment it commits; live instances follow LivePolicy through
// [Store.LiveInstancePolicy]. A replayed DeclarationID returns the current
// version without writing.
func (s Store) Quarantine(ctx context.Context, d QuarantineDeclaration) (out version.CompiledVersion, err error) {
	switch {
	case d.DeclarationID == uuid.Nil, d.CompiledPlanDigest == "", strings.TrimSpace(d.Reason) == "",
		strings.TrimSpace(d.EvidenceRef) == "", strings.TrimSpace(d.DeclaredBy) == "", strings.TrimSpace(d.ApprovedBy) == "",
		strings.TrimSpace(d.Authority) == "", d.RecordedAt.IsZero():
		return version.CompiledVersion{}, fmt.Errorf("%w: a quarantine needs an id, digest, reason, evidence, declarer, approver, authority and instant", ErrInvalid)
	case !d.LivePolicy.valid():
		return version.CompiledVersion{}, fmt.Errorf("%w: live-instance policy %q is not PAUSE, CONTINUE or BLOCK", ErrInvalid, d.LivePolicy)
	case d.DeclaredBy == d.ApprovedBy:
		return version.CompiledVersion{}, fmt.Errorf("%w: %s", ErrSameReviewer, d.DeclaredBy)
	}
	err = s.tx(ctx, func(b bound) error {
		if err := b.lock(d.CompiledPlanDigest); err != nil {
			return err
		}
		if replayed, err := b.declared(d.DeclarationID); err != nil || replayed {
			if err == nil {
				out, _, err = b.GetByDigest(d.CompiledPlanDigest)
			}
			return err
		}
		out, err = version.Quarantine(b, d.CompiledPlanDigest, d.Reason, d.DeclaredBy, d.Authority,
			version.ActivationEvidence{ApprovedAt: d.RecordedAt.UTC()})
		if err != nil {
			return err
		}
		return b.insertDeclaration(d.DeclarationID, d.CompiledPlanDigest, "QUARANTINE", string(d.LivePolicy),
			d.Reason, d.EvidenceRef, d.DeclaredBy, d.ApprovedBy, d.Authority, d.RecordedAt)
	})
	return out, err
}

// LiftQuarantine returns a quarantined version to service on a fresh review by
// a principal who did not declare the quarantine, in one transaction: it
// records the lift, the reviewer's approval against the exact digest, and the
// activation that approval grants. A replayed DeclarationID returns the
// current version without writing.
func (s Store) LiftQuarantine(ctx context.Context, l QuarantineLift) (out version.CompiledVersion, err error) {
	switch {
	case l.DeclarationID == uuid.Nil, l.CompiledPlanDigest == "", strings.TrimSpace(l.ReviewedBy) == "",
		strings.TrimSpace(l.ValidationEvidenceRef) == "", strings.TrimSpace(l.Reason) == "",
		strings.TrimSpace(l.Authority) == "", l.RecordedAt.IsZero():
		return version.CompiledVersion{}, fmt.Errorf("%w: a lift needs an id, digest, reviewer, validation evidence, reason, authority and instant", ErrInvalid)
	}
	err = s.tx(ctx, func(b bound) error {
		if err := b.lock(l.CompiledPlanDigest); err != nil {
			return err
		}
		if replayed, err := b.declared(l.DeclarationID); err != nil || replayed {
			if err == nil {
				out, _, err = b.GetByDigest(l.CompiledPlanDigest)
			}
			return err
		}
		_, quarantined, err := b.quarantinePolicy(l.CompiledPlanDigest)
		if err != nil {
			return err
		}
		if !quarantined {
			return fmt.Errorf("%w: %s", ErrNotQuarantined, l.CompiledPlanDigest)
		}
		var declaredBy string
		if err := b.ex.QueryRow(b.ctx, `SELECT declared_by FROM workflow_version_quarantine
			WHERE compiled_plan_digest = $1 AND action = 'QUARANTINE' ORDER BY recorded_at DESC, declaration_id DESC LIMIT 1`,
			l.CompiledPlanDigest).Scan(&declaredBy); err != nil {
			return fmt.Errorf("workflowversionstore: load quarantine declaration: %w", err)
		}
		if declaredBy == l.ReviewedBy {
			return fmt.Errorf("%w: %s", ErrSameReviewer, l.ReviewedBy)
		}
		if err := b.insertDeclaration(l.DeclarationID, l.CompiledPlanDigest, "LIFT", "", l.Reason,
			l.ValidationEvidenceRef, declaredBy, l.ReviewedBy, l.Authority, l.RecordedAt); err != nil {
			return err
		}
		if err := b.recordApproval(Approval{
			ApprovalID: l.DeclarationID, CompiledPlanDigest: l.CompiledPlanDigest, ReviewedPlanDigest: l.CompiledPlanDigest,
			ApprovedBy: l.ReviewedBy, Authority: l.Authority, Reason: l.Reason, TestsPassed: l.TestsPassed,
			FixtureRefs: []string{l.ValidationEvidenceRef}, ApprovedAt: l.RecordedAt,
		}); err != nil {
			return err
		}
		out, err = b.activateApproved(l.CompiledPlanDigest, false)
		return err
	})
	return out, err
}

// LiveInstancePolicy reports, inside the caller's transaction, whether the
// version a live instance is pinned to stands under a governed quarantine and
// which disposition the instance must take. It is the execute driver's
// quarantine port.
func (s Store) LiveInstancePolicy(ctx context.Context, ex dbport.Conn, compiledPlanDigest string) (string, bool, error) {
	policy, quarantined, err := bound{ctx: ctx, ex: ex}.quarantinePolicy(compiledPlanDigest)
	return string(policy), quarantined, err
}

// quarantinePolicy is the standing policy: the latest declaration for the
// digest is a QUARANTINE and the version is still QUARANTINED.
func (b bound) quarantinePolicy(digest string) (LivePolicy, bool, error) {
	var action, status string
	var policy *string
	err := b.ex.QueryRow(b.ctx, `SELECT q.action, q.live_instance_policy, v.status
		FROM workflow_version_quarantine q JOIN workflow_compiled_version v USING (compiled_plan_digest)
		WHERE q.compiled_plan_digest = $1 ORDER BY q.recorded_at DESC, q.declaration_id DESC LIMIT 1`, digest).Scan(&action, &policy, &status)
	if errors.Is(err, dbport.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("workflowversionstore: load quarantine policy: %w", err)
	}
	if action != "QUARANTINE" || status != string(version.StatusQuarantined) || policy == nil {
		return "", false, nil
	}
	return LivePolicy(*policy), true, nil
}

func (b bound) declared(id uuid.UUID) (bool, error) {
	var n int
	if err := b.ex.QueryRow(b.ctx, `SELECT count(*) FROM workflow_version_quarantine WHERE declaration_id = $1`, id).Scan(&n); err != nil {
		return false, fmt.Errorf("workflowversionstore: load quarantine declaration: %w", err)
	}
	return n > 0, nil
}

func (b bound) insertDeclaration(id uuid.UUID, digest, action, policy, reason, evidence, declaredBy, approvedBy, authority string, at time.Time) error {
	var livePolicy any
	if policy != "" {
		livePolicy = policy
	}
	if _, err := b.ex.Exec(b.ctx, `INSERT INTO workflow_version_quarantine
		(declaration_id, compiled_plan_digest, action, live_instance_policy, reason, evidence_ref, declared_by, approved_by, authority, recorded_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		id, digest, action, livePolicy, reason, evidence, declaredBy, approvedBy, authority, at.UTC()); err != nil {
		return fmt.Errorf("workflowversionstore: record quarantine declaration: %w", err)
	}
	return nil
}
