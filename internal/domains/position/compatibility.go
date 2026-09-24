package position

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// FindingCode names one reason a position is not compatible with a proposed
// placement. Codes are stable wire tokens, never free text, so a caller can
// branch on them without parsing prose.
type FindingCode string

// Defined finding codes. The RED clause this package exists to satisfy names
// each of these explicitly: a closed, frozen, wrong-job, wrong-org,
// wrong-legal-entity or stale position must never be reported compatible.
const (
	FindingClosed              FindingCode = "POSITION_CLOSED"
	FindingFrozen              FindingCode = "POSITION_FROZEN"
	FindingJobMismatch         FindingCode = "JOB_MISMATCH"
	FindingOrgMismatch         FindingCode = "ORG_MISMATCH"
	FindingLegalEntityMismatch FindingCode = "LEGAL_ENTITY_MISMATCH"
	FindingStaleRevision       FindingCode = "STALE_REVISION"
	FindingNotEffective        FindingCode = "NOT_EFFECTIVE_AT_AS_OF"
	FindingNotFound            FindingCode = "POSITION_NOT_FOUND"

	// POSITION-002 capacity/vacancy finding codes. FindingOverCapacityHeads and
	// FindingOverCapacityFTE name the RED clause's "over-capacity heads/FTE";
	// FindingOverlappingExclusiveOccupancy names its "overlapping exclusive
	// occupancy"; FindingVacantAfterDate is the typed "vacancy-after-date"
	// finding CalculateCapacity's GREEN clause requires.
	FindingOverCapacityHeads             FindingCode = "OVER_CAPACITY_HEADS"
	FindingOverCapacityFTE               FindingCode = "OVER_CAPACITY_FTE"
	FindingOverlappingExclusiveOccupancy FindingCode = "OVERLAPPING_EXCLUSIVE_OCCUPANCY"
	FindingVacantAfterDate               FindingCode = "VACANT_AFTER_DATE"
)

var findingCodeValid = map[FindingCode]struct{}{
	FindingClosed: {}, FindingFrozen: {}, FindingJobMismatch: {},
	FindingOrgMismatch: {}, FindingLegalEntityMismatch: {},
	FindingStaleRevision: {}, FindingNotEffective: {}, FindingNotFound: {},
	FindingOverCapacityHeads: {}, FindingOverCapacityFTE: {},
	FindingOverlappingExclusiveOccupancy: {}, FindingVacantAfterDate: {},
}

// Valid reports whether c is a defined finding code.
func (c FindingCode) Valid() bool { _, ok := findingCodeValid[c]; return ok }

// Finding is one compatibility problem the read surfaced, with a bounded,
// value-free detail string: a finding names what is wrong, never a protected
// value.
type Finding struct {
	Code   FindingCode
	Detail string
}

// Canonical returns the canonical byte encoding, or nil when the code is
// undefined.
func (f Finding) Canonical() []byte {
	if !f.Code.Valid() {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.position.Finding", positionSchemaVer).
		String("code", string(f.Code)).
		String("detail", f.Detail).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Authorizer is called with the resolved revision before any compatibility
// finding is computed. Returning false fails closed: CheckCompatibility
// returns ErrUnauthorized and discloses nothing about the position, mirroring
// how internal/domains/organization gates its graph reads.
type Authorizer func(rev PositionRevision) bool

// CompatibilityRequest is what CheckCompatibility is asked. Desired* fields
// are optional; a zero value means "this dimension is not being checked",
// which lets a caller ask a narrower question (for example, only whether the
// position is open at all) without a false JOB_MISMATCH on a job it never
// named.
type CompatibilityRequest struct {
	Tenant   values.TenantId
	Position values.EntityRef
	AsOf     AsOf

	DesiredJobCode     string
	DesiredOrgUnit     string
	DesiredLegalEntity string

	// MinRevision, when specified, is the minimum acceptable revision: a
	// caller that read this position earlier and is replaying a decision
	// against it names the revision its decision was based on, and a position
	// revised since then is reported STALE_REVISION rather than compatible.
	// An identical token always passes - opaque content digests are
	// comparable for equality but never for order, so only a different
	// token reaches the CompareInStream ordering, where it must name the
	// same revision stream the reader answers with.
	MinRevision values.RevisionToken

	// Authorize is an optional fail-closed scope gate. A nil Authorize means
	// no additional scope restriction beyond tenant isolation.
	Authorize Authorizer
}

// Validate reports whether the request is well formed on its own terms.
func (r CompatibilityRequest) Validate() error {
	if err := r.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %w", ErrInvalidRequest, err)
	}
	if err := r.Position.Validate(); err != nil {
		return fmt.Errorf("%w: position: %w", ErrInvalidRequest, err)
	}
	if r.Position.Tenant != r.Tenant {
		return fmt.Errorf("%w: position %s is outside tenant %s", ErrInvalidRequest, r.Position, r.Tenant)
	}
	if r.Position.Kind != KindPosition {
		return fmt.Errorf("%w: subject kind is %q, want %q", ErrInvalidRequest, r.Position.Kind, KindPosition)
	}
	return r.AsOf.Validate()
}

// CompatibilityResult is the disclosed revision plus the compatibility
// findings CheckCompatibility computed against it.
type CompatibilityResult struct {
	Position values.EntityRef
	Exists   bool

	Revision  values.RevisionToken
	Effective values.EffectiveInterval
	Lifecycle Lifecycle
	Capacity  CapacityPolicy

	Authority  evidence.SourceAuthority
	Provenance evidence.Provenance

	Compatible bool
	Findings   []Finding
}

// Canonical returns the canonical byte encoding, or nil when incoherent.
func (res CompatibilityResult) Canonical() []byte {
	w := canonicalbytes.New(resultSchema, positionSchemaVer).
		Value("position", res.Position).
		Bool("exists", res.Exists)
	if res.Exists {
		w.Value("revision", res.Revision).
			Value("effective", res.Effective).
			String("lifecycle", res.Lifecycle.String()).
			Field("capacity", res.Capacity.Canonical()).
			Value("authority", res.Authority).
			Value("provenance", res.Provenance)
	}
	w.Bool("compatible", res.Compatible).Count("findings", len(res.Findings))
	for _, f := range res.Findings {
		w.Field("finding", f.Canonical())
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// CheckCompatibility is the POSITION-001 entry point: an authorized,
// as-of/known-at read of one position's governed revision plus the typed
// compatibility findings against a proposed placement.
//
// A position the caller may not see under Authorize is refused outright
// (ErrUnauthorized) rather than answered with an empty finding list, because
// silently returning "no findings" for a position the caller cannot see would
// read as "compatible". A position that does not exist, is closed, frozen,
// mismatched on job/org/legal-entity, or stale relative to MinRevision is
// always reported - never silently treated as compatible.
func CheckCompatibility(ctx context.Context, reader PositionFacts, req CompatibilityRequest) (CompatibilityResult, error) {
	if reader == nil {
		return CompatibilityResult{}, fmt.Errorf("%w: no position facts reader", ErrInvalidRequest)
	}
	if err := req.Validate(); err != nil {
		return CompatibilityResult{}, err
	}

	rev, exists, err := reader.PositionRevisionAt(ctx, PositionQuery{
		Tenant: req.Tenant, Position: req.Position, AsOf: req.AsOf,
	})
	if err != nil {
		return CompatibilityResult{}, fmt.Errorf("%w: %w", ErrReaderFailed, err)
	}
	if !exists {
		return CompatibilityResult{
			Position: req.Position, Exists: false, Compatible: false,
			Findings: []Finding{{Code: FindingNotFound, Detail: "no revision at the requested coordinate"}},
		}, nil
	}
	if err := rev.Validate(); err != nil {
		return CompatibilityResult{}, err
	}
	if rev.Position != req.Position {
		return CompatibilityResult{}, fmt.Errorf("%w: asked %s, answered %s", ErrSubjectMismatch, req.Position, rev.Position)
	}
	if req.Authorize != nil && !req.Authorize(rev) {
		return CompatibilityResult{}, ErrUnauthorized
	}

	var findings []Finding
	switch rev.Lifecycle {
	case LifecycleClosed:
		findings = append(findings, Finding{Code: FindingClosed, Detail: "position is closed"})
	case LifecycleFrozen:
		findings = append(findings, Finding{Code: FindingFrozen, Detail: "position is frozen"})
	}
	if req.DesiredJobCode != "" && req.DesiredJobCode != rev.JobCode {
		findings = append(findings, Finding{Code: FindingJobMismatch, Detail: fmt.Sprintf("position job is %s", rev.JobCode)})
	}
	if req.DesiredOrgUnit != "" && req.DesiredOrgUnit != rev.OrgUnit {
		findings = append(findings, Finding{Code: FindingOrgMismatch, Detail: fmt.Sprintf("position org unit is %s", rev.OrgUnit)})
	}
	if req.DesiredLegalEntity != "" && req.DesiredLegalEntity != rev.LegalEntity {
		findings = append(findings, Finding{Code: FindingLegalEntityMismatch, Detail: fmt.Sprintf("position legal entity is %s", rev.LegalEntity)})
	}
	if ok, cerr := rev.Effective.ContainsDate(req.AsOf.EffectiveOn); cerr == nil && !ok {
		findings = append(findings, Finding{Code: FindingNotEffective, Detail: "position revision does not cover the requested effective date"})
	}
	if req.MinRevision.IsSpecified() && !rev.Revision.Equal(req.MinRevision) {
		// Opaque revision tokens (content digests, as issued by the
		// durable reader) are comparable for equality but never for
		// order: an identical digest names the same revision the caller
		// saw, which the Equal check above already accepted. Only a
		// different token reaches the in-stream ordering, where a
		// genuinely older revision - or a baseline that cannot be
		// compared at all - is still reported stale, never compatible.
		cmp, cerr := rev.Revision.CompareInStream(req.MinRevision)
		if cerr != nil || cmp < 0 {
			findings = append(findings, Finding{Code: FindingStaleRevision, Detail: "position has been revised since the caller's baseline, or the baseline cannot be compared"})
		}
	}

	return CompatibilityResult{
		Position:   rev.Position,
		Exists:     true,
		Revision:   rev.Revision,
		Effective:  rev.Effective,
		Lifecycle:  rev.Lifecycle,
		Capacity:   rev.Capacity,
		Authority:  rev.Authority,
		Provenance: rev.Provenance,
		Compatible: len(findings) == 0,
		Findings:   findings,
	}, nil
}
