// Package eligibility owns the promotion-specific projection that sits
// between a promotion request and the position domain.  A position code in a
// request is an intention, not evidence that the position exists, is open, or
// belongs to the requested job and organisation.  Resolve therefore performs
// the authorized position read first and only emits a candidate/reservation
// projection when that read is compatible.
package eligibility

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrInvalidRequest reports a malformed eligibility question.
	ErrInvalidRequest = errors.New("promotion/eligibility: invalid request")
	// ErrNoCompatiblePath reports a target that is not a published next step.
	ErrNoCompatiblePath = errors.New("promotion/eligibility: no compatible promotion path")
	// ErrPositionUnavailable reports a target whose authorized compatibility read
	// did not produce a usable candidate.  The wrapped result is deliberately
	// absent: callers must not use this error to infer protected position facts.
	ErrPositionUnavailable = errors.New("promotion/eligibility: position is unavailable")
)

// Path is the immutable, server-published career edge a candidate must match.
// The type intentionally carries only the fields needed by this projection;
// the job-architecture domain remains the owner of the full revision.
type Path struct {
	Ref, Revision              string
	SourceJobCode, SourceGrade string
	TargetJobCode, TargetGrade string
	TargetProfileRef           string
}

// Validate rejects incomplete path projections before any position facts are
// read.  Published-path loading is owned by job architecture; this boundary
// still validates the immutable fields it relies on so malformed data cannot
// accidentally authorize a candidate.
func (p Path) Validate() error {
	for _, field := range []struct {
		name, value string
	}{
		{"ref", p.Ref}, {"revision", p.Revision},
		{"source_job_code", p.SourceJobCode}, {"source_grade", p.SourceGrade},
		{"target_job_code", p.TargetJobCode}, {"target_grade", p.TargetGrade},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%w: path %s is required", ErrInvalidRequest, field.name)
		}
	}
	return nil
}

// CandidateRequest is the caller's desired promotion plus the authorized
// position reader used to verify it.  No current position fact is accepted as
// input: current job/org/legal-entity and the position revision come from
// PositionFacts.
type CandidateRequest struct {
	Tenant   values.TenantId
	Position values.EntityRef
	AsOf     position.AsOf

	CurrentJobCode, CurrentGrade     string
	TargetJobCode, TargetGrade       string
	TargetOrgUnit, TargetLegalEntity string
	Paths                            []Path

	MinPositionRevision values.RevisionToken
	Authorize           position.Authorizer
	PositionFacts       position.PositionFacts

	// Reservation fields are copied only into a projection; Resolve never
	// acquires a hold or mutates a store.
	ProposalRevisionID string
	ProposalDigest     string
	AuthorityDigest    string
	ReservationKey     string
	ReservationExpiry  time.Time
	FTE                values.Decimal
	Heads              int64
}

// Candidate is the server-derived compatible target. PositionRevision is
// retained so an eventual reservation can bind to the revision baseline the
// authorization read returned, while PositionCompatibility keeps the typed
// findings available to callers that need to explain a refusal.
type Candidate struct {
	Path                  Path
	Position              values.EntityRef
	PositionRevision      values.RevisionToken
	PositionCompatibility position.CompatibilityResult
}

// Result is the zero-effect eligibility answer. Reservation is nil unless the
// candidate is compatible and every reservation binding field was supplied.
// It is a request projection, never an acquired reservation.
type Result struct {
	Eligible      bool
	Candidate     *Candidate
	Compatibility position.CompatibilityResult
	Reservation   *position.PositionReservationRequest
}

// Resolve performs one authorized compatibility read and projects one
// compatible position candidate. It never treats a missing reader, denied
// authorization, closed/frozen position, mismatched placement, stale
// revision, or missing path as eligible.
func Resolve(ctx context.Context, req CandidateRequest) (Result, error) {
	if err := req.validate(); err != nil {
		return Result{}, err
	}
	path, ok := compatiblePath(req.Paths, req.CurrentJobCode, req.CurrentGrade, req.TargetJobCode, req.TargetGrade)
	if !ok {
		return Result{}, ErrNoCompatiblePath
	}
	compat, err := position.CheckCompatibility(ctx, req.PositionFacts, position.CompatibilityRequest{
		Tenant: req.Tenant, Position: req.Position, AsOf: req.AsOf,
		DesiredJobCode: req.TargetJobCode, DesiredOrgUnit: req.TargetOrgUnit,
		DesiredLegalEntity: req.TargetLegalEntity, MinRevision: req.MinPositionRevision,
		Authorize: req.Authorize,
	})
	if err != nil {
		if errors.Is(err, position.ErrUnauthorized) {
			return Result{}, err
		}
		return Result{}, fmt.Errorf("%w: %w", ErrPositionUnavailable, err)
	}
	if !compat.Exists || !compat.Compatible {
		return Result{Compatibility: compat}, ErrPositionUnavailable
	}

	candidate := &Candidate{Path: path, Position: compat.Position,
		PositionRevision: compat.Revision, PositionCompatibility: compat}
	result := Result{Eligible: true, Candidate: candidate, Compatibility: compat}
	if reservationFieldsPresent(req) {
		// Bind the projection to the exact revision the authorized read
		// returned when the caller did not supply an older baseline.  The
		// reservation store re-reads and fences against this token before it
		// acquires a hold; leaving it empty would turn a selected revision into
		// an unbound position identifier.
		minRevision := req.MinPositionRevision
		if !minRevision.IsSpecified() {
			minRevision = compat.Revision
		}
		reservation := &position.PositionReservationRequest{
			Tenant: req.Tenant, Position: req.Position, AsOf: req.AsOf,
			ProposalRevisionID: req.ProposalRevisionID, ProposalDigest: req.ProposalDigest,
			EffectiveDate: req.AsOf.EffectiveOn, Effective: compat.Effective,
			FTE: req.FTE, Heads: req.Heads,
			DesiredJobCode: req.TargetJobCode, DesiredOrgUnit: req.TargetOrgUnit,
			DesiredLegalEntity: req.TargetLegalEntity, MinRevision: minRevision,
			AuthorityDigest: req.AuthorityDigest, IdempotencyKey: req.ReservationKey,
			ExpiresAt: req.ReservationExpiry,
		}
		result.Reservation = reservation
	}
	return result, nil
}

func (r CandidateRequest) validate() error {
	if err := r.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrInvalidRequest, err)
	}
	if err := r.Position.Validate(); err != nil || r.Position.Kind != position.KindPosition || r.Position.Tenant != r.Tenant {
		return fmt.Errorf("%w: position is invalid or outside tenant", ErrInvalidRequest)
	}
	if err := r.AsOf.Validate(); err != nil {
		return fmt.Errorf("%w: as-of: %v", ErrInvalidRequest, err)
	}
	for _, field := range []struct {
		name, value string
	}{
		{"current_job_code", r.CurrentJobCode}, {"current_grade", r.CurrentGrade},
		{"target_job_code", r.TargetJobCode}, {"target_grade", r.TargetGrade},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidRequest, field.name)
		}
	}
	for _, path := range r.Paths {
		if err := path.Validate(); err != nil {
			return err
		}
	}
	if r.Authorize == nil {
		return fmt.Errorf("%w: no position authorization policy", ErrInvalidRequest)
	}
	if r.PositionFacts == nil {
		return fmt.Errorf("%w: no authorized position facts reader", ErrInvalidRequest)
	}
	return nil
}

func compatiblePath(paths []Path, currentJob, currentGrade, targetJob, targetGrade string) (Path, bool) {
	for _, path := range paths {
		if path.SourceJobCode == currentJob && path.SourceGrade == currentGrade &&
			path.TargetJobCode == targetJob && path.TargetGrade == targetGrade {
			return path, true
		}
	}
	return Path{}, false
}

func reservationFieldsPresent(req CandidateRequest) bool {
	return req.ProposalRevisionID != "" && req.ProposalDigest != "" &&
		req.AuthorityDigest != "" && req.ReservationKey != "" &&
		!req.ReservationExpiry.IsZero() && req.Heads > 0 && req.FTE.Validate() == nil && req.FTE.Sign() > 0
}
