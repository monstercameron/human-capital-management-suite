// Package conformance owns the transport-neutral product-query contract.
//
// A query is authorized once, against the existing repository scope, and the
// resulting envelope is the semantic value every presentation transport
// projects. The package deliberately has no database, renderer, or wire
// dependency: those edges consume the same envelope and may not add authority
// or protected payloads of their own.
package conformance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// ContractVersion is the version of the transport-neutral query contract.
const ContractVersion = 1

// MaxQuerySubjects bounds one product query. A wildcard population is not a
// valid shape; callers must supply an already bounded repository scope.
const MaxQuerySubjects = 200

// MaxInvalidationSubjects bounds one authorized invalidation message.
const MaxInvalidationSubjects = 16

// MaxInvalidationBytes bounds the canonical message form before it reaches a
// streaming transport.
const MaxInvalidationBytes = 8192

// Version returns the contract version.
func Version() int { return ContractVersion }

// Explain returns the safe contract summary used by architecture tooling.
func Explain() string {
	return "transport conformance v1: one authorized product-query envelope, bounded source-aware invalidation, and cross-surface semantic parity"
}

var (
	// ErrInvalidQuery means the request is not a complete canonical query
	// shape and therefore cannot be admitted to a repository.
	ErrInvalidQuery = errors.New("transport conformance: invalid product query")
	// ErrInvalidSurface means a caller selected a surface outside the closed
	// set of transports qualified by this contract.
	ErrInvalidSurface = errors.New("transport conformance: invalid surface")
	// ErrParityMismatch means two transports did not project the same
	// semantic envelope.
	ErrParityMismatch = errors.New("transport conformance: surface semantic parity mismatch")
	// ErrInvalidationRejected means an invalidation could not be bounded or
	// authorized without guessing.
	ErrInvalidationRejected = errors.New("transport conformance: invalidation rejected")
)

// FreshnessState describes the relation between the authoritative source and
// the projection used by a query.
type FreshnessState uint8

const (
	FreshnessUnknown FreshnessState = iota
	FreshnessFresh
	FreshnessStale
)

// String returns the stable wire spelling of a freshness state.
func (s FreshnessState) String() string {
	switch s {
	case FreshnessFresh:
		return "FRESH"
	case FreshnessStale:
		return "STALE"
	default:
		return "UNKNOWN"
	}
}

// Freshness carries the source and projection versions needed to explain a
// result without exposing storage details. A projection is stale when its
// applied source sequence trails the authoritative source sequence.
type Freshness struct {
	SourceVersion      string
	ProjectionVersion  string
	SourceSequence     uint64
	ProjectionSequence uint64
	ObservedAt         values.Instant
}

// Validate reports whether freshness metadata is complete and safe to put on
// a product response.
func (f Freshness) Validate() error {
	if !safeToken(f.SourceVersion, 128) || !safeToken(f.ProjectionVersion, 128) {
		return fmt.Errorf("%w: freshness versions must be non-empty safe tokens", ErrInvalidQuery)
	}
	if err := f.ObservedAt.Validate(); err != nil {
		return fmt.Errorf("%w: freshness observed time: %v", ErrInvalidQuery, err)
	}
	return nil
}

// State returns the deterministic freshness state.
func (f Freshness) State() FreshnessState {
	if f.Validate() != nil {
		return FreshnessUnknown
	}
	if f.ProjectionSequence < f.SourceSequence {
		return FreshnessStale
	}
	return FreshnessFresh
}

// Surface is a qualified semantic consumer of a product-query envelope.
type Surface uint8

const (
	SurfaceSSR Surface = iota + 1
	SurfaceBrowser
	SurfaceRPC
	SurfaceExport
	// SurfaceEnhancedBrowser is the qualified client-hydrated surface
	// (ALIGN-057). It is numbered after the existing surfaces so every
	// previously valid surface keeps its value.
	SurfaceEnhancedBrowser
)

// String returns the stable surface spelling.
func (s Surface) String() string {
	switch s {
	case SurfaceSSR:
		return "SSR"
	case SurfaceBrowser:
		return "BROWSER"
	case SurfaceRPC:
		return "RPC"
	case SurfaceExport:
		return "EXPORT"
	case SurfaceEnhancedBrowser:
		return "ENHANCED_BROWSER"
	default:
		return "UNKNOWN"
	}
}

// QueryRequest is the transport-neutral input to Execute. Scope is an
// evaluated authz.RepositoryScope; it cannot be constructed as an authorized
// value outside the authz planner. Requested is closed and bounded, so a
// query never means "all records".
type QueryRequest struct {
	Tenant            values.TenantId
	Purpose           string
	EffectiveAt       values.Instant
	Scope             authz.RepositoryScope
	Gate              *authz.RepositoryGate
	Requested         []values.EntityRef
	SchemaVersion     string
	DefinitionVersion string
	Freshness         Freshness
}

// Validate reports whether the request binds the logical tenant, purpose,
// instant, evaluated repository scope, projection versions, and freshness.
func (r QueryRequest) Validate() error {
	if err := r.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrInvalidQuery, err)
	}
	if !safeToken(r.Purpose, 128) {
		return fmt.Errorf("%w: purpose must be a non-empty safe token", ErrInvalidQuery)
	}
	if err := r.EffectiveAt.Validate(); err != nil {
		return fmt.Errorf("%w: effective time: %v", ErrInvalidQuery, err)
	}
	if r.Scope.Zero() {
		return fmt.Errorf("%w: repository scope was not evaluated", ErrInvalidQuery)
	}
	if err := r.Scope.Validate(); err != nil {
		return fmt.Errorf("%w: repository scope: %v", ErrInvalidQuery, err)
	}
	if r.Scope.Tenant() != r.Tenant {
		return fmt.Errorf("%w: repository scope tenant does not match query tenant", ErrInvalidQuery)
	}
	if r.Scope.Purpose() != r.Purpose {
		return fmt.Errorf("%w: repository scope purpose does not match query purpose", ErrInvalidQuery)
	}
	if r.Scope.EvaluatedAt().Compare(r.EffectiveAt) != 0 {
		return fmt.Errorf("%w: repository scope instant does not match query instant", ErrInvalidQuery)
	}
	if r.Gate == nil {
		return fmt.Errorf("%w: repository gate is required", ErrInvalidQuery)
	}
	if len(r.Requested) == 0 || len(r.Requested) > MaxQuerySubjects {
		return fmt.Errorf("%w: requested subjects must be between 1 and %d", ErrInvalidQuery, MaxQuerySubjects)
	}
	seen := make(map[values.EntityRef]struct{}, len(r.Requested))
	for _, subject := range r.Requested {
		if err := subject.Validate(); err != nil {
			return fmt.Errorf("%w: requested subject: %v", ErrInvalidQuery, err)
		}
		if subject.Tenant != r.Tenant {
			return fmt.Errorf("%w: requested subject crosses the query tenant", ErrInvalidQuery)
		}
		if _, ok := seen[subject]; ok {
			return fmt.Errorf("%w: requested subjects contain a duplicate", ErrInvalidQuery)
		}
		seen[subject] = struct{}{}
	}
	if !safeToken(r.SchemaVersion, 128) || !safeToken(r.DefinitionVersion, 128) {
		return fmt.Errorf("%w: schema and definition versions must be non-empty safe tokens", ErrInvalidQuery)
	}
	return r.Freshness.Validate()
}

// AuthorizationMetadata is the safe authorization evidence attached to every
// response. It carries no principal, role, relationship, or denied-record
// detail.
type AuthorizationMetadata struct {
	Effect        authz.Effect
	Reason        string
	PolicyVersion string
	InputsDigest  string
	EvidenceID    string
}

// QueryEnvelope is the canonical semantic result shared by SSR, enhanced
// browser, RPC, and export adapters. Its rows are already narrowed by the
// repository gate and contain no unauthorized subject.
type QueryEnvelope struct {
	ContractVersion   int
	Tenant            values.TenantId
	Purpose           string
	EffectiveAt       values.Instant
	SchemaVersion     string
	DefinitionVersion string
	Authorization     AuthorizationMetadata
	Freshness         Freshness
	Rows              []authz.Projection
	SemanticDigest    string
}

// Execute applies the already-evaluated repository scope and builds the one
// canonical query envelope. It never performs a database call; a real store
// is represented by the existing authz.RepositoryGate port at this boundary.
func Execute(ctx context.Context, req QueryRequest) (QueryEnvelope, error) {
	if ctx == nil {
		return QueryEnvelope{}, fmt.Errorf("%w: nil context", ErrInvalidQuery)
	}
	if err := ctx.Err(); err != nil {
		return QueryEnvelope{}, err
	}
	if err := req.Validate(); err != nil {
		return QueryEnvelope{}, err
	}
	rows, err := req.Gate.Query(req.Scope, req.Requested, req.EffectiveAt)
	if err != nil {
		return QueryEnvelope{}, fmt.Errorf("%w: repository projection: %v", ErrInvalidQuery, err)
	}
	slices.SortFunc(rows, func(a, b authz.Projection) int {
		return strings.Compare(a.Subject.String(), b.Subject.String())
	})
	envelope := QueryEnvelope{
		ContractVersion:   Version(),
		Tenant:            req.Tenant,
		Purpose:           req.Purpose,
		EffectiveAt:       req.EffectiveAt,
		SchemaVersion:     req.SchemaVersion,
		DefinitionVersion: req.DefinitionVersion,
		Authorization: AuthorizationMetadata{
			Effect:        req.Scope.Effect(),
			Reason:        req.Scope.Reason(),
			PolicyVersion: req.Scope.PolicyVersion(),
			InputsDigest:  req.Scope.InputsDigest(),
			EvidenceID:    req.Scope.EvidenceID(),
		},
		Freshness: req.Freshness,
		Rows:      cloneProjections(rows),
	}
	if err := envelope.Validate(); err != nil {
		return QueryEnvelope{}, err
	}
	envelope.SemanticDigest = envelope.CanonicalDigest()
	return envelope, nil
}

// Validate reports whether an envelope is complete and contains only
// authorized projection effects.
func (e QueryEnvelope) Validate() error {
	if e.ContractVersion != Version() {
		return fmt.Errorf("%w: envelope contract version %d is unsupported", ErrInvalidQuery, e.ContractVersion)
	}
	if err := e.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: envelope tenant: %v", ErrInvalidQuery, err)
	}
	if !safeToken(e.Purpose, 128) {
		return fmt.Errorf("%w: envelope purpose or effective time is invalid", ErrInvalidQuery)
	}
	if err := e.EffectiveAt.Validate(); err != nil {
		return fmt.Errorf("%w: envelope purpose or effective time is invalid", ErrInvalidQuery)
	}
	if !safeToken(e.SchemaVersion, 128) || !safeToken(e.DefinitionVersion, 128) {
		return fmt.Errorf("%w: envelope versions are invalid", ErrInvalidQuery)
	}
	if !e.Authorization.Effect.Valid() || !safeToken(e.Authorization.PolicyVersion, 160) ||
		!safeToken(e.Authorization.InputsDigest, 160) || !safeToken(e.Authorization.EvidenceID, 160) {
		return fmt.Errorf("%w: authorization metadata is incomplete", ErrInvalidQuery)
	}
	if e.Authorization.Effect == authz.EffectDenied && !safeToken(e.Authorization.Reason, 160) {
		return fmt.Errorf("%w: denied envelope has no safe reason", ErrInvalidQuery)
	}
	if err := e.Freshness.Validate(); err != nil {
		return err
	}
	var previous string
	seen := make(map[values.EntityRef]struct{}, len(e.Rows))
	for _, row := range e.Rows {
		if err := row.Subject.Validate(); err != nil || row.Subject.Tenant != e.Tenant {
			return fmt.Errorf("%w: row subject is invalid or outside the envelope tenant", ErrInvalidQuery)
		}
		if previous != "" && strings.Compare(previous, row.Subject.String()) >= 0 {
			return fmt.Errorf("%w: rows are not in canonical order", ErrInvalidQuery)
		}
		previous = row.Subject.String()
		if _, ok := seen[row.Subject]; ok {
			return fmt.Errorf("%w: rows contain a duplicate subject", ErrInvalidQuery)
		}
		seen[row.Subject] = struct{}{}
		for _, field := range row.Fields {
			if !field.Effect.Valid() || field.Effect == authz.EffectWithheld {
				return fmt.Errorf("%w: row contains a withheld or invalid field effect", ErrInvalidQuery)
			}
			if field.Effect == authz.EffectRedacted && field.Value != authz.RedactedPlaceholder {
				return fmt.Errorf("%w: redacted field carries a non-redacted value", ErrInvalidQuery)
			}
		}
	}
	if e.Authorization.Effect == authz.EffectDenied && len(e.Rows) != 0 {
		return fmt.Errorf("%w: denied envelope contains rows", ErrInvalidQuery)
	}
	if e.SemanticDigest != "" && e.SemanticDigest != e.CanonicalDigest() {
		return fmt.Errorf("%w: semantic digest does not match the envelope", ErrInvalidQuery)
	}
	return nil
}

// CanonicalDigest returns the digest of semantic fields only. Transport labels
// and authorization evidence identifiers are intentionally excluded so that
// changing an unauthorized candidate cannot alter a cache or surface key.
func (e QueryEnvelope) CanonicalDigest() string {
	h := sha256.New()
	write := func(label, value string) { fmt.Fprintf(h, "%s=%d:%s;", label, len(value), value) }
	write("contract", fmt.Sprint(e.ContractVersion))
	write("tenant", e.Tenant.String())
	write("purpose", e.Purpose)
	write("effective_at", e.EffectiveAt.String())
	write("schema", e.SchemaVersion)
	write("definition", e.DefinitionVersion)
	write("authz.effect", e.Authorization.Effect.String())
	write("authz.reason", e.Authorization.Reason)
	write("policy", e.Authorization.PolicyVersion)
	write("freshness.source", e.Freshness.SourceVersion)
	write("freshness.projection", e.Freshness.ProjectionVersion)
	write("freshness.source_sequence", fmt.Sprint(e.Freshness.SourceSequence))
	write("freshness.projection_sequence", fmt.Sprint(e.Freshness.ProjectionSequence))
	write("freshness.observed_at", e.Freshness.ObservedAt.String())
	write("rows", fmt.Sprint(len(e.Rows)))
	for _, row := range e.Rows {
		write("row.subject", row.Subject.String())
		for _, field := range row.Fields {
			write("row.field", string(field.FieldID))
			write("row.effect", field.Effect.String())
			write("row.value", field.Value)
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// SurfaceProjection labels an envelope for one transport without changing
// its semantic content.
type SurfaceProjection struct {
	Surface        Surface
	Envelope       QueryEnvelope
	SemanticDigest string
}

// ProjectSurface returns a transport-labelled copy of e. The surface label is
// metadata only; it is absent from the semantic digest.
func ProjectSurface(surface Surface, e QueryEnvelope) (SurfaceProjection, error) {
	if surface < SurfaceSSR || surface > SurfaceEnhancedBrowser {
		return SurfaceProjection{}, fmt.Errorf("%w: %d", ErrInvalidSurface, surface)
	}
	if err := e.Validate(); err != nil {
		return SurfaceProjection{}, err
	}
	if e.SemanticDigest == "" {
		e.SemanticDigest = e.CanonicalDigest()
	}
	e.Rows = cloneProjections(e.Rows)
	return SurfaceProjection{Surface: surface, Envelope: e, SemanticDigest: e.SemanticDigest}, nil
}

// AssertParity proves that two qualified surfaces carry the same semantic
// result. Different encodings are expected; different semantic digests are
// not.
func AssertParity(left, right SurfaceProjection) error {
	if left.SemanticDigest == "" || right.SemanticDigest == "" || left.SemanticDigest != right.SemanticDigest {
		return fmt.Errorf("%w: %s=%s, %s=%s", ErrParityMismatch, left.Surface, left.SemanticDigest, right.Surface, right.SemanticDigest)
	}
	return nil
}

// CheckNoninterference checks a set of surface projections for parity and
// verifies that hidden subjects do not appear on any surface. The check has no
// count, timing, or denied-subject output channel to compare.
func CheckNoninterference(projections []SurfaceProjection, hidden []values.EntityRef) error {
	if len(projections) < 2 {
		return fmt.Errorf("%w: at least two surfaces are required", ErrParityMismatch)
	}
	for i := 1; i < len(projections); i++ {
		if err := AssertParity(projections[0], projections[i]); err != nil {
			return err
		}
	}
	for _, subject := range hidden {
		if err := subject.Validate(); err != nil {
			return fmt.Errorf("%w: hidden subject: %v", ErrParityMismatch, err)
		}
		for _, projection := range projections {
			for _, row := range projection.Envelope.Rows {
				if row.Subject == subject {
					return fmt.Errorf("%w: hidden subject appeared on %s", ErrParityMismatch, projection.Surface)
				}
			}
		}
	}
	return nil
}

// Invalidation is a bounded, authorization-filtered attention hint. It is
// not a business payload or a source of truth; a client must refetch through
// the query contract after receiving it.
type Invalidation struct {
	ContractVersion         int
	Tenant                  values.TenantId
	ProjectionVersion       string
	SourceSequence          uint64
	PolicyVersion           string
	AuthorizationEvidenceID string
	Subjects                []values.EntityRef
}

// AuthorizeInvalidation filters candidate subject identifiers through the
// evaluated repository scope and constructs a bounded invalidation message.
// Unauthorized same-tenant subjects disappear; malformed or cross-tenant
// identifiers reject the message rather than being guessed about.
func AuthorizeInvalidation(scope authz.RepositoryScope, at values.Instant, projectionVersion string, sourceSequence uint64, candidates []values.EntityRef) (Invalidation, error) {
	if scope.Zero() {
		return Invalidation{}, fmt.Errorf("%w: repository scope was not evaluated", ErrInvalidationRejected)
	}
	if err := scope.Validate(); err != nil {
		return Invalidation{}, fmt.Errorf("%w: repository scope: %v", ErrInvalidationRejected, err)
	}
	if err := at.Validate(); err != nil || at.Compare(scope.EvaluatedAt()) != 0 {
		return Invalidation{}, fmt.Errorf("%w: invalidation instant is not the scope instant", ErrInvalidationRejected)
	}
	if !safeToken(projectionVersion, 128) || sourceSequence == 0 {
		return Invalidation{}, fmt.Errorf("%w: projection version or source sequence is invalid", ErrInvalidationRejected)
	}
	if len(candidates) > MaxInvalidationSubjects {
		return Invalidation{}, fmt.Errorf("%w: candidate count exceeds %d", ErrInvalidationRejected, MaxInvalidationSubjects)
	}
	visible := make([]values.EntityRef, 0, len(candidates))
	for _, subject := range candidates {
		if err := subject.Validate(); err != nil || subject.Tenant != scope.Tenant() {
			return Invalidation{}, fmt.Errorf("%w: candidate is invalid or crosses tenant scope", ErrInvalidationRejected)
		}
		if scope.AuthorizesRecord(subject, at) {
			visible = append(visible, subject)
		}
	}
	slices.SortFunc(visible, func(a, b values.EntityRef) int { return strings.Compare(a.String(), b.String()) })
	visible = slices.Compact(visible)
	message := Invalidation{
		ContractVersion:         Version(),
		Tenant:                  scope.Tenant(),
		ProjectionVersion:       projectionVersion,
		SourceSequence:          sourceSequence,
		PolicyVersion:           scope.PolicyVersion(),
		AuthorizationEvidenceID: scope.EvidenceID(),
		Subjects:                visible,
	}
	if err := message.Validate(); err != nil {
		return Invalidation{}, err
	}
	return message, nil
}

// Validate reports whether an invalidation is bounded and contains only
// tenant-scoped identifiers and safe version/evidence references.
func (i Invalidation) Validate() error {
	if i.ContractVersion != Version() || i.SourceSequence == 0 {
		return fmt.Errorf("%w: invalidation contract or sequence", ErrInvalidationRejected)
	}
	if err := i.Tenant.Validate(); err != nil || !safeToken(i.ProjectionVersion, 128) ||
		!safeToken(i.PolicyVersion, 160) || !safeToken(i.AuthorizationEvidenceID, 160) {
		return fmt.Errorf("%w: invalidation metadata is incomplete", ErrInvalidationRejected)
	}
	if len(i.Subjects) > MaxInvalidationSubjects {
		return fmt.Errorf("%w: invalidation subject count exceeds %d", ErrInvalidationRejected, MaxInvalidationSubjects)
	}
	previous := ""
	for _, subject := range i.Subjects {
		if err := subject.Validate(); err != nil || subject.Tenant != i.Tenant {
			return fmt.Errorf("%w: invalidation subject is invalid or outside tenant", ErrInvalidationRejected)
		}
		if previous != "" && previous >= subject.String() {
			return fmt.Errorf("%w: invalidation subjects are not canonical", ErrInvalidationRejected)
		}
		previous = subject.String()
	}
	if len(i.Canonical()) > MaxInvalidationBytes {
		return fmt.Errorf("%w: canonical invalidation exceeds %d bytes", ErrInvalidationRejected, MaxInvalidationBytes)
	}
	return nil
}

// Canonical returns the bounded, payload-free invalidation representation.
func (i Invalidation) Canonical() []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "v%d|tenant=%s|projection=%s|sequence=%d|policy=%s|evidence=%s|subjects=%d",
		i.ContractVersion, i.Tenant, i.ProjectionVersion, i.SourceSequence, i.PolicyVersion, i.AuthorizationEvidenceID, len(i.Subjects))
	for _, subject := range i.Subjects {
		fmt.Fprintf(&b, "|subject=%s", subject.String())
	}
	return []byte(b.String())
}

// Explain returns a redaction-safe summary without subject identifiers.
func (i Invalidation) Explain() string {
	return fmt.Sprintf("authorized invalidation v%d tenant=%s projection=%s sequence=%d subjects=%d", i.ContractVersion, i.Tenant, i.ProjectionVersion, i.SourceSequence, len(i.Subjects))
}

func safeToken(s string, max int) bool {
	if len(s) == 0 || len(s) > max || strings.TrimSpace(s) != s {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

func cloneProjections(in []authz.Projection) []authz.Projection {
	if in == nil {
		return nil
	}
	out := make([]authz.Projection, len(in))
	for i, row := range in {
		out[i] = row
		out[i].Fields = slices.Clone(row.Fields)
	}
	return out
}
