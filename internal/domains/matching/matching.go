// Package matching owns descriptive, deterministic candidate matching.
//
// Matching produces recommendations only. It never assigns a person, changes
// a position, persists facts, or creates an approval or work item.
package matching

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const schemaVersion = 1

// Version reports this package's vocabulary version.
func Version() int { return schemaVersion }

var (
	ErrInvalidRequest       = errors.New("matching: invalid match request")
	ErrInvalidConstraint    = errors.New("matching: invalid constraint")
	ErrInvalidCandidate     = errors.New("matching: invalid candidate facts")
	ErrInvalidResult        = errors.New("matching: invalid candidate match")
	ErrUnknownConstraint    = errors.New("matching: unknown constraint kind")
	ErrFactsReader          = errors.New("matching: candidate facts reader failed")
	ErrDuplicateCandidate   = errors.New("matching: duplicate candidate")
	ErrCrossTenantReference = errors.New("matching: cross-tenant reference")
)

// ConstraintKind is the closed set of facts this conformance matcher can
// evaluate. Future kinds must be added deliberately rather than accepted as
// opaque strings.
type ConstraintKind string

const (
	ConstraintLocation           ConstraintKind = "LOCATION"
	ConstraintAvailabilityWindow ConstraintKind = "AVAILABILITY_WINDOW"
	ConstraintCostCeiling        ConstraintKind = "COST_CEILING"
	ConstraintQualification      ConstraintKind = "QUALIFICATION"
)

func (k ConstraintKind) Valid() bool {
	switch k {
	case ConstraintLocation, ConstraintAvailabilityWindow, ConstraintCostCeiling, ConstraintQualification:
		return true
	default:
		return false
	}
}

// ConstraintMode distinguishes a requirement that gates eligibility from a
// preference that only contributes to the explainable score.
type ConstraintMode string

const (
	ConstraintHard ConstraintMode = "HARD"
	ConstraintSoft ConstraintMode = "SOFT"
)

func (m ConstraintMode) Valid() bool { return m == ConstraintHard || m == ConstraintSoft }

// Constraint is one typed hard or soft request. Only the field belonging to
// Kind is used; Validate rejects a missing value instead of guessing.
type Constraint struct {
	Kind   ConstraintKind
	Mode   ConstraintMode
	Hard   bool // compatibility shorthand; when Mode is empty, true means HARD
	Weight int64

	Location string
	Window   values.EffectiveInterval
	Cost     values.Money
}

// MatchConstraint is the descriptive alias used by callers that want the
// request context in the name.
type MatchConstraint = Constraint

func (c Constraint) effectiveMode() ConstraintMode {
	if c.Mode != "" {
		return c.Mode
	}
	if c.Hard {
		return ConstraintHard
	}
	return ConstraintSoft
}

func (c Constraint) Validate() error {
	if !c.Kind.Valid() {
		return fmt.Errorf("%w: %q", ErrUnknownConstraint, c.Kind)
	}
	mode := c.effectiveMode()
	if !mode.Valid() {
		return fmt.Errorf("%w: mode %q", ErrInvalidConstraint, mode)
	}
	if c.Weight < 0 || (mode == ConstraintSoft && c.Weight == 0) {
		return fmt.Errorf("%w: weight must be positive for SOFT and never negative", ErrInvalidConstraint)
	}
	switch c.Kind {
	case ConstraintLocation:
		if strings.TrimSpace(c.Location) == "" {
			return fmt.Errorf("%w: location is required", ErrInvalidConstraint)
		}
	case ConstraintAvailabilityWindow:
		if err := c.Window.Validate(); err != nil {
			return fmt.Errorf("%w: availability window: %v", ErrInvalidConstraint, err)
		}
		if c.Window.Kind() != values.IntervalKindInstant {
			return fmt.Errorf("%w: availability window must use INSTANT boundaries", ErrInvalidConstraint)
		}
	case ConstraintCostCeiling:
		if err := c.Cost.Validate(); err != nil {
			return fmt.Errorf("%w: cost ceiling: %v", ErrInvalidConstraint, err)
		}
	case ConstraintQualification:
		return fmt.Errorf("%w: qualification constraints are declared through RequiredQualificationRefs", ErrInvalidConstraint)
	}
	return nil
}

func (c Constraint) Canonical() []byte {
	if c.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.matching.Constraint", schemaVersion).
		String("kind", string(c.Kind)).String("mode", string(c.effectiveMode())).Int("weight", c.Weight)
	switch c.Kind {
	case ConstraintLocation:
		w.String("location", c.Location)
	case ConstraintAvailabilityWindow:
		w.Value("window", c.Window)
	case ConstraintCostCeiling:
		w.Value("cost", c.Cost)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

// FairnessPolicy and the two policy records are references, not executable
// policy. Their presence makes the ranking contract explicit without letting
// this package inspect protected attributes or claim a fairness decision.
type FairnessPolicy struct {
	PolicyRef string
	Version   string
}

func (p FairnessPolicy) Validate() error {
	if strings.TrimSpace(p.PolicyRef) == "" || strings.TrimSpace(p.Version) == "" {
		return fmt.Errorf("%w: fairness policy ref and version are required", ErrInvalidRequest)
	}
	return nil
}
func (p FairnessPolicy) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.matching.FairnessPolicy", schemaVersion).
		String("policy_ref", p.PolicyRef).String("version", p.Version).Bytes()
	if err != nil {
		return nil
	}
	return b
}

type TieBreakPolicy string

const TieBreakCandidateRef TieBreakPolicy = "CANDIDATE_REF"

type RankingPolicy struct {
	PolicyRef    string
	Version      string
	ScoreVersion uint64
	TieBreak     TieBreakPolicy
}

func (p RankingPolicy) Validate() error {
	if strings.TrimSpace(p.PolicyRef) == "" || strings.TrimSpace(p.Version) == "" || p.ScoreVersion == 0 {
		return fmt.Errorf("%w: ranking policy ref, version and score version are required", ErrInvalidRequest)
	}
	if p.TieBreak != TieBreakCandidateRef {
		return fmt.Errorf("%w: ranking tie break %q is not declared", ErrInvalidRequest, p.TieBreak)
	}
	return nil
}
func (p RankingPolicy) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.matching.RankingPolicy", schemaVersion).
		String("policy_ref", p.PolicyRef).String("version", p.Version).
		Int("score_version", int64(p.ScoreVersion)).String("tie_break", string(p.TieBreak)).Bytes()
	if err != nil {
		return nil
	}
	return b
}

type ExplanationPolicy struct {
	PolicyRef                string
	Version                  string
	IncludeConstraintReasons bool
}

func (p ExplanationPolicy) Validate() error {
	if strings.TrimSpace(p.PolicyRef) == "" || strings.TrimSpace(p.Version) == "" || !p.IncludeConstraintReasons {
		return fmt.Errorf("%w: explanation policy ref, version and constraint reasons are required", ErrInvalidRequest)
	}
	return nil
}
func (p ExplanationPolicy) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.matching.ExplanationPolicy", schemaVersion).
		String("policy_ref", p.PolicyRef).String("version", p.Version).
		Bool("include_constraint_reasons", p.IncludeConstraintReasons).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// MatchRequest freezes the inputs to one descriptive matching run.
type MatchRequest struct {
	RequestID                 string
	Revision                  uint64
	RequesterScope            values.EntityRef
	TargetRef                 values.EntityRef
	CandidateSourceRef        values.EntityRef
	RequiredQualificationRefs []values.EntityRef
	Constraints               []Constraint
	Fairness                  FairnessPolicy
	SnapshotRef               values.EntityRef
	AsOf                      values.Instant
	Ranking                   RankingPolicy
	Explanation               ExplanationPolicy
	CanonicalDigest           string
}

func sameTenant(tenant values.TenantId, ref values.EntityRef, name string) error {
	if err := ref.Validate(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if ref.Tenant != tenant {
		return fmt.Errorf("%w: %s", ErrCrossTenantReference, name)
	}
	return nil
}

func (r MatchRequest) Validate() error {
	if strings.TrimSpace(r.RequestID) == "" || r.Revision == 0 {
		return fmt.Errorf("%w: request id and non-zero revision are required", ErrInvalidRequest)
	}
	if err := r.RequesterScope.Validate(); err != nil {
		return fmt.Errorf("%w: requester scope: %v", ErrInvalidRequest, err)
	}
	if err := sameTenant(r.RequesterScope.Tenant, r.TargetRef, "target ref"); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	if err := sameTenant(r.RequesterScope.Tenant, r.CandidateSourceRef, "candidate source ref"); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	if err := sameTenant(r.RequesterScope.Tenant, r.SnapshotRef, "snapshot ref"); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	if err := r.AsOf.Validate(); err != nil {
		return fmt.Errorf("%w: as-of: %v", ErrInvalidRequest, err)
	}
	if len(r.Constraints) == 0 {
		return fmt.Errorf("%w: at least one constraint is required", ErrInvalidRequest)
	}
	seenKinds := make(map[ConstraintKind]struct{}, len(r.Constraints))
	for i, c := range r.Constraints {
		if err := c.Validate(); err != nil {
			return fmt.Errorf("%w at index %d: %w", ErrInvalidRequest, i, err)
		}
		if _, ok := seenKinds[c.Kind]; ok {
			return fmt.Errorf("%w: duplicate constraint kind %q", ErrInvalidRequest, c.Kind)
		}
		seenKinds[c.Kind] = struct{}{}
	}
	seenRefs := make(map[string]struct{}, len(r.RequiredQualificationRefs))
	for i, ref := range r.RequiredQualificationRefs {
		if err := sameTenant(r.RequesterScope.Tenant, ref, fmt.Sprintf("qualification ref %d", i)); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidRequest, err)
		}
		key := ref.String()
		if _, ok := seenRefs[key]; ok {
			return fmt.Errorf("%w: duplicate qualification ref %q", ErrInvalidRequest, key)
		}
		seenRefs[key] = struct{}{}
	}
	if err := r.Fairness.Validate(); err != nil {
		return err
	}
	if err := r.Ranking.Validate(); err != nil {
		return err
	}
	if err := r.Explanation.Validate(); err != nil {
		return err
	}
	if r.CanonicalDigest != "" && r.CanonicalDigest != r.computedDigest() {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidRequest)
	}
	return nil
}

func (r MatchRequest) body() []byte {
	constraints := append([]Constraint(nil), r.Constraints...)
	sort.Slice(constraints, func(i, j int) bool { return constraints[i].Kind < constraints[j].Kind })
	quals := make([]string, 0, len(r.RequiredQualificationRefs))
	for _, ref := range r.RequiredQualificationRefs {
		quals = append(quals, ref.String())
	}
	sort.Strings(quals)
	w := canonicalbytes.New("hcmnext.domains.matching.MatchRequest", schemaVersion).
		String("request_id", r.RequestID).Int("revision", int64(r.Revision)).
		Value("requester_scope", r.RequesterScope).Value("target_ref", r.TargetRef).
		Value("candidate_source_ref", r.CandidateSourceRef).SortedStrings("qualification_ref", quals).
		Count("constraints", len(constraints))
	for _, c := range constraints {
		w.Value("constraint", c)
	}
	w.Value("fairness", r.Fairness).Value("snapshot_ref", r.SnapshotRef).
		Value("as_of", r.AsOf).Value("ranking", r.Ranking).Value("explanation", r.Explanation)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (r MatchRequest) computedDigest() string {
	b := r.body()
	if b == nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

// NewMatchRequest copies all slices and computes the immutable digest.
func NewMatchRequest(r MatchRequest) (MatchRequest, error) {
	r.RequiredQualificationRefs = append([]values.EntityRef(nil), r.RequiredQualificationRefs...)
	r.Constraints = append([]Constraint(nil), r.Constraints...)
	r.CanonicalDigest = r.computedDigest()
	if err := r.Validate(); err != nil {
		return MatchRequest{}, err
	}
	return r, nil
}

func (r MatchRequest) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}
func (r MatchRequest) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}

// CandidateFacts is the only data this matcher consumes for a candidate.
// Facts are supplied by a caller-owned read port and are never persisted here.
type CandidateFacts struct {
	CandidateRef      values.EntityRef
	SourceRef         values.EntityRef
	Location          string
	Availability      []values.EffectiveInterval
	Cost              values.Money
	QualificationRefs []values.EntityRef
}

func (f CandidateFacts) Validate() error {
	if err := f.CandidateRef.Validate(); err != nil {
		return fmt.Errorf("%w: candidate ref: %v", ErrInvalidCandidate, err)
	}
	if f.SourceRef != (values.EntityRef{}) {
		if err := sameTenant(f.CandidateRef.Tenant, f.SourceRef, "source ref"); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidCandidate, err)
		}
	}
	if strings.TrimSpace(f.Location) == "" {
		return fmt.Errorf("%w: location is required", ErrInvalidCandidate)
	}
	if err := f.Cost.Validate(); err != nil {
		return fmt.Errorf("%w: cost: %v", ErrInvalidCandidate, err)
	}
	for i, window := range f.Availability {
		if err := window.Validate(); err != nil {
			return fmt.Errorf("%w: availability %d: %v", ErrInvalidCandidate, i, err)
		}
		if window.Kind() != values.IntervalKindInstant {
			return fmt.Errorf("%w: availability %d must use INSTANT boundaries", ErrInvalidCandidate, i)
		}
	}
	seen := make(map[string]struct{}, len(f.QualificationRefs))
	for i, ref := range f.QualificationRefs {
		if err := sameTenant(f.CandidateRef.Tenant, ref, fmt.Sprintf("qualification ref %d", i)); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidCandidate, err)
		}
		if _, ok := seen[ref.String()]; ok {
			return fmt.Errorf("%w: duplicate qualification ref", ErrInvalidCandidate)
		}
		seen[ref.String()] = struct{}{}
	}
	return nil
}

// CandidateFactsPort is a read-only in-memory facts boundary for this pure
// conformance implementation.
type CandidateFactsPort interface {
	CandidateFacts(context.Context, MatchRequest) ([]CandidateFacts, error)
}

// InMemoryCandidateFacts is a detached, deterministic implementation of the
// candidate facts port. It does not grant access beyond the supplied slice.
type InMemoryCandidateFacts struct{ facts []CandidateFacts }

func NewInMemoryCandidateFacts(facts []CandidateFacts) (*InMemoryCandidateFacts, error) {
	out := &InMemoryCandidateFacts{facts: append([]CandidateFacts(nil), facts...)}
	seen := make(map[string]struct{}, len(out.facts))
	for i := range out.facts {
		if err := out.facts[i].Validate(); err != nil {
			return nil, fmt.Errorf("candidate %d: %w", i, err)
		}
		key := out.facts[i].CandidateRef.String()
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateCandidate, key)
		}
		seen[key] = struct{}{}
		out.facts[i].Availability = append([]values.EffectiveInterval(nil), out.facts[i].Availability...)
		out.facts[i].QualificationRefs = append([]values.EntityRef(nil), out.facts[i].QualificationRefs...)
	}
	return out, nil
}

func (p *InMemoryCandidateFacts) CandidateFacts(ctx context.Context, _ MatchRequest) ([]CandidateFacts, error) {
	if p == nil {
		return nil, fmt.Errorf("%w: nil candidate facts port", ErrFactsReader)
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	out := append([]CandidateFacts(nil), p.facts...)
	for i := range out {
		out[i].Availability = append([]values.EffectiveInterval(nil), out[i].Availability...)
		out[i].QualificationRefs = append([]values.EntityRef(nil), out[i].QualificationRefs...)
	}
	return out, nil
}

// Candidates is a convenience alias for callers that do not need the
// interface name in their code.
func (p *InMemoryCandidateFacts) Candidates(ctx context.Context, req MatchRequest) ([]CandidateFacts, error) {
	return p.CandidateFacts(ctx, req)
}

type SatisfactionStatus string

const (
	SatisfactionSatisfied   SatisfactionStatus = "SATISFIED"
	SatisfactionUnsatisfied SatisfactionStatus = "UNSATISFIED"
)

type SatisfactionReason string

const (
	ReasonSatisfied            SatisfactionReason = "SATISFIED"
	ReasonLocationMismatch     SatisfactionReason = "LOCATION_MISMATCH"
	ReasonAvailabilityGap      SatisfactionReason = "AVAILABILITY_GAP"
	ReasonCostOverCeiling      SatisfactionReason = "COST_OVER_CEILING"
	ReasonQualificationMissing SatisfactionReason = "QUALIFICATION_MISSING"
)

func (r SatisfactionReason) Valid() bool {
	switch r {
	case ReasonSatisfied, ReasonLocationMismatch, ReasonAvailabilityGap, ReasonCostOverCeiling, ReasonQualificationMissing:
		return true
	default:
		return false
	}
}

type ConstraintSatisfaction struct {
	Kind   ConstraintKind
	Mode   ConstraintMode
	Status SatisfactionStatus
	Reason SatisfactionReason
	Detail string
}

func (s ConstraintSatisfaction) Validate() error {
	if !s.Kind.Valid() || !s.Mode.Valid() || (s.Status != SatisfactionSatisfied && s.Status != SatisfactionUnsatisfied) || !s.Reason.Valid() {
		return fmt.Errorf("%w: malformed constraint satisfaction", ErrInvalidResult)
	}
	if s.Status == SatisfactionSatisfied && s.Reason != ReasonSatisfied {
		return fmt.Errorf("%w: satisfied constraint has non-satisfied reason", ErrInvalidResult)
	}
	if s.Status == SatisfactionUnsatisfied && s.Reason == ReasonSatisfied {
		return fmt.Errorf("%w: unsatisfied constraint has satisfied reason", ErrInvalidResult)
	}
	return nil
}

func (s ConstraintSatisfaction) Canonical() []byte {
	if s.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.matching.ConstraintSatisfaction", schemaVersion).
		String("kind", string(s.Kind)).String("mode", string(s.Mode)).String("status", string(s.Status)).
		String("reason", string(s.Reason)).String("detail", s.Detail).Bytes()
	if err != nil {
		return nil
	}
	return b
}

type ScoreFactor struct {
	Kind   ConstraintKind
	Weight int64
	Points int64
	Reason string
}

func (f ScoreFactor) Validate() error {
	if !f.Kind.Valid() || f.Weight < 0 || f.Points < 0 || f.Points > f.Weight || strings.TrimSpace(f.Reason) == "" {
		return fmt.Errorf("%w: malformed score factor", ErrInvalidResult)
	}
	return nil
}
func (f ScoreFactor) Canonical() []byte {
	if f.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.matching.ScoreFactor", schemaVersion).
		String("kind", string(f.Kind)).Int("weight", f.Weight).Int("points", f.Points).String("reason", f.Reason).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// Score is never a bare number: every point is traceable to a typed factor.
type Score struct {
	Total   int64
	Factors []ScoreFactor
}

func (s Score) Validate() error {
	if len(s.Factors) == 0 || s.Total < 0 {
		return fmt.Errorf("%w: score needs factors and a non-negative total", ErrInvalidResult)
	}
	var total int64
	for _, f := range s.Factors {
		if err := f.Validate(); err != nil {
			return err
		}
		total += f.Points
	}
	if total != s.Total {
		return fmt.Errorf("%w: score total %d does not equal factor total %d", ErrInvalidResult, s.Total, total)
	}
	return nil
}
func (s Score) Canonical() []byte {
	if s.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.matching.Score", schemaVersion).Int("total", s.Total).Count("factors", len(s.Factors))
	for _, f := range s.Factors {
		w.Value("factor", f)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

type CandidateMatch struct {
	CandidateRef    values.EntityRef
	Eligible        bool
	Satisfactions   []ConstraintSatisfaction
	Score           Score
	Rank            int
	CanonicalDigest string
}

func (m CandidateMatch) Validate() error {
	if err := m.CandidateRef.Validate(); err != nil {
		return fmt.Errorf("%w: candidate ref: %v", ErrInvalidResult, err)
	}
	if len(m.Satisfactions) == 0 {
		return fmt.Errorf("%w: satisfactions are required", ErrInvalidResult)
	}
	for _, s := range m.Satisfactions {
		if err := s.Validate(); err != nil {
			return err
		}
	}
	if err := m.Score.Validate(); err != nil {
		return err
	}
	if m.Rank < 0 {
		return fmt.Errorf("%w: rank must not be negative", ErrInvalidResult)
	}
	if m.CanonicalDigest != "" && m.CanonicalDigest != m.computedDigest() {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidResult)
	}
	return nil
}

func (m CandidateMatch) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.matching.CandidateMatch", schemaVersion).
		Value("candidate_ref", m.CandidateRef).Bool("eligible", m.Eligible).Int("rank", int64(m.Rank)).Count("satisfactions", len(m.Satisfactions))
	for _, s := range m.Satisfactions {
		w.Value("satisfaction", s)
	}
	w.Value("score", m.Score)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (m CandidateMatch) computedDigest() string {
	b := m.body()
	if b == nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}
func (m CandidateMatch) Canonical() []byte {
	if m.Validate() != nil {
		return nil
	}
	return m.body()
}
func (m CandidateMatch) Digest() (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}
	return m.computedDigest(), nil
}

// MatchResult cites the exact ranking contract that produced it:
// the score version and tie-break policy replay with the request
// digest, so components, weights, tie policy and version reproduce
// exactly. MATCH-004.
type MatchResult struct {
	RequestID       string
	RequestDigest   string
	ScoreVersion    uint64
	TieBreak        TieBreakPolicy
	Matches         []CandidateMatch
	CanonicalDigest string
}

func (r MatchResult) Validate() error {
	if strings.TrimSpace(r.RequestID) == "" || strings.TrimSpace(r.RequestDigest) == "" || len(r.Matches) == 0 {
		return fmt.Errorf("%w: request binding and matches are required", ErrInvalidResult)
	}
	if r.ScoreVersion == 0 || r.TieBreak != TieBreakCandidateRef {
		return fmt.Errorf("%w: result cites no declared ranking contract", ErrInvalidResult)
	}
	for i, m := range r.Matches {
		if err := m.Validate(); err != nil {
			return fmt.Errorf("match %d: %w", i, err)
		}
		if m.Rank != i+1 {
			return fmt.Errorf("%w: rank %d at index %d", ErrInvalidResult, m.Rank, i)
		}
	}
	if r.CanonicalDigest != "" && r.CanonicalDigest != r.computedDigest() {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidResult)
	}
	return nil
}
func (r MatchResult) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.matching.MatchResult", schemaVersion).
		String("request_id", r.RequestID).String("request_digest", r.RequestDigest).
		Int("score_version", int64(r.ScoreVersion)).String("tie_break", string(r.TieBreak)).
		Count("matches", len(r.Matches))
	for _, m := range r.Matches {
		w.Value("match", m)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (r MatchResult) computedDigest() string {
	b := r.body()
	if b == nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}
func (r MatchResult) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}

// Match evaluates only the caller-provided facts and returns a deterministic
// recommendation ordered by eligibility first, then descending score, then
// candidate reference. Ineligible candidates stay listed with their hard
// reasons but can never outrank an eligible candidate.
func Match(ctx context.Context, reader CandidateFactsPort, request MatchRequest) (MatchResult, error) {
	if reader == nil {
		return MatchResult{}, fmt.Errorf("%w: nil reader", ErrFactsReader)
	}
	if err := request.Validate(); err != nil {
		return MatchResult{}, err
	}
	facts, err := reader.CandidateFacts(ctx, request)
	if err != nil {
		return MatchResult{}, fmt.Errorf("%w: %v", ErrFactsReader, err)
	}
	result := MatchResult{
		RequestID:     request.RequestID,
		RequestDigest: request.computedDigest(),
		ScoreVersion:  request.Ranking.ScoreVersion,
		TieBreak:      request.Ranking.TieBreak,
	}
	seen := make(map[string]struct{}, len(facts))
	for i, fact := range facts {
		if err := fact.Validate(); err != nil {
			return MatchResult{}, fmt.Errorf("candidate %d: %w", i, err)
		}
		key := fact.CandidateRef.String()
		if _, ok := seen[key]; ok {
			return MatchResult{}, fmt.Errorf("%w: %s", ErrDuplicateCandidate, key)
		}
		seen[key] = struct{}{}
		if fact.SourceRef != (values.EntityRef{}) && fact.SourceRef != request.CandidateSourceRef {
			continue
		}
		result.Matches = append(result.Matches, evaluateCandidate(request, fact))
	}
	// MATCH-003: eligibility dominates score. A hard-constraint
	// failure can never be outranked by soft points: every eligible
	// candidate orders before every ineligible one, and only then do
	// score and the deterministic reference tiebreak apply.
	sort.Slice(result.Matches, func(i, j int) bool {
		if result.Matches[i].Eligible != result.Matches[j].Eligible {
			return result.Matches[i].Eligible
		}
		if result.Matches[i].Score.Total != result.Matches[j].Score.Total {
			return result.Matches[i].Score.Total > result.Matches[j].Score.Total
		}
		return result.Matches[i].CandidateRef.String() < result.Matches[j].CandidateRef.String()
	})
	for i := range result.Matches {
		result.Matches[i].Rank = i + 1
		result.Matches[i].CanonicalDigest = result.Matches[i].computedDigest()
	}
	if len(result.Matches) == 0 {
		return MatchResult{}, fmt.Errorf("%w: no candidates from requested source", ErrInvalidResult)
	}
	result.CanonicalDigest = result.computedDigest()
	return result, nil
}

func evaluateCandidate(request MatchRequest, fact CandidateFacts) CandidateMatch {
	out := CandidateMatch{CandidateRef: fact.CandidateRef, Eligible: true}
	qualifications := make(map[string]struct{}, len(fact.QualificationRefs))
	for _, ref := range fact.QualificationRefs {
		qualifications[ref.String()] = struct{}{}
	}
	for _, ref := range request.RequiredQualificationRefs {
		_, ok := qualifications[ref.String()]
		status, reason := SatisfactionSatisfied, ReasonSatisfied
		if !ok {
			status, reason = SatisfactionUnsatisfied, ReasonQualificationMissing
			out.Eligible = false
		}
		out.Satisfactions = append(out.Satisfactions, ConstraintSatisfaction{Kind: ConstraintQualification, Mode: ConstraintHard, Status: status, Reason: reason, Detail: ref.String()})
		points := int64(0)
		if ok {
			points = 1
		}
		out.Score.Factors = append(out.Score.Factors, ScoreFactor{Kind: ConstraintQualification, Weight: 1, Points: points, Reason: string(reason)})
	}
	for _, c := range request.Constraints {
		status, reason, detail := evaluateConstraint(c, fact)
		if status == SatisfactionUnsatisfied && c.effectiveMode() == ConstraintHard {
			out.Eligible = false
		}
		out.Satisfactions = append(out.Satisfactions, ConstraintSatisfaction{Kind: c.Kind, Mode: c.effectiveMode(), Status: status, Reason: reason, Detail: detail})
		points := int64(0)
		if status == SatisfactionSatisfied {
			points = c.Weight
		}
		out.Score.Factors = append(out.Score.Factors, ScoreFactor{Kind: c.Kind, Weight: c.Weight, Points: points, Reason: string(reason)})
	}
	for _, factor := range out.Score.Factors {
		out.Score.Total += factor.Points
	}
	return out
}

func evaluateConstraint(c Constraint, fact CandidateFacts) (SatisfactionStatus, SatisfactionReason, string) {
	switch c.Kind {
	case ConstraintLocation:
		if fact.Location == c.Location {
			return SatisfactionSatisfied, ReasonSatisfied, "candidate location equals requested location"
		}
		return SatisfactionUnsatisfied, ReasonLocationMismatch, "candidate location differs from requested location"
	case ConstraintAvailabilityWindow:
		for _, candidateWindow := range fact.Availability {
			if intervalCovers(candidateWindow, c.Window) {
				return SatisfactionSatisfied, ReasonSatisfied, "candidate availability covers requested window"
			}
		}
		return SatisfactionUnsatisfied, ReasonAvailabilityGap, "no candidate availability window covers the request"
	case ConstraintCostCeiling:
		cmp, err := fact.Cost.Cmp(c.Cost)
		if err == nil && cmp <= 0 {
			return SatisfactionSatisfied, ReasonSatisfied, "candidate cost is at or below the ceiling"
		}
		return SatisfactionUnsatisfied, ReasonCostOverCeiling, "candidate cost exceeds the ceiling or currencies differ"
	default:
		return SatisfactionUnsatisfied, ReasonLocationMismatch, "unknown constraint"
	}
}

func intervalCovers(container, target values.EffectiveInterval) bool {
	if container.Validate() != nil || target.Validate() != nil || container.Kind() != target.Kind() {
		return false
	}
	if container.Kind() == values.IntervalKindLocalDate {
		start, _ := container.StartDate()
		targetStart, _ := target.StartDate()
		if start.Compare(targetStart) > 0 {
			return false
		}
		end, hasEnd := container.EndDate()
		targetEnd, targetHasEnd := target.EndDate()
		return !targetHasEnd || hasEnd && end.Compare(targetEnd) >= 0
	}
	start, _ := container.StartInstant()
	targetStart, _ := target.StartInstant()
	if start.Compare(targetStart) > 0 {
		return false
	}
	end, hasEnd := container.EndInstant()
	targetEnd, targetHasEnd := target.EndInstant()
	return !targetHasEnd || hasEnd && end.Compare(targetEnd) >= 0
}

// Matcher is a small dependency-injected facade over Match.
type Matcher struct{ Facts CandidateFactsPort }

func (m Matcher) Match(ctx context.Context, request MatchRequest) (MatchResult, error) {
	return Match(ctx, m.Facts, request)
}

type MatchExplanation struct {
	RequestID       string
	RequestDigest   string
	ConstraintKinds []ConstraintKind
	RankingPolicy   string
	Authority       string
}

func (r MatchRequest) Explain() (MatchExplanation, error) {
	if err := r.Validate(); err != nil {
		return MatchExplanation{}, err
	}
	kinds := make([]ConstraintKind, 0, len(r.Constraints))
	for _, c := range r.Constraints {
		kinds = append(kinds, c.Kind)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	return MatchExplanation{RequestID: r.RequestID, RequestDigest: r.computedDigest(), ConstraintKinds: kinds, RankingPolicy: r.Ranking.PolicyRef + "@" + r.Ranking.Version, Authority: "descriptive recommendation only; no assignment authority"}, nil
}

// Explain returns the stable request explanation used by audit and UI layers.
func Explain(r MatchRequest) (MatchExplanation, error) { return r.Explain() }
