// Package succession owns the evidence-gated vocabulary for critical roles
// and succession slates. Values are immutable revisions; this package has no
// persistence or employment-decision side effects.
package succession

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const schemaVersion = 1

// Version reports this domain vocabulary's contract version.
func Version() int { return schemaVersion }

var (
	ErrInvalidCriticalRole        = errors.New("succession: invalid critical role")
	ErrInvalidReadiness           = errors.New("succession: invalid successor readiness")
	ErrInvalidSlate               = errors.New("succession: invalid succession slate")
	ErrSlateMembershipWithheld    = errors.New("succession: slate membership is withheld")
	ErrUnauthorizedNomination     = errors.New("succession: nomination is not authorized")
	ErrConflictingCurrentRevision = errors.New("succession: conflicting current revision")
	ErrInvalidRevisionLineage     = errors.New("succession: invalid revision lineage")
)

// ReadinessBand is deliberately closed. UNKNOWN is an explicit conclusion,
// not a missing label.
type ReadinessBand string

const (
	ReadinessReadyNow   ReadinessBand = "READY_NOW"
	ReadinessReadyLater ReadinessBand = "READY_LATER"
	ReadinessNotReady   ReadinessBand = "NOT_READY"
	ReadinessUnknown    ReadinessBand = "UNKNOWN"
)

func (r ReadinessBand) Valid() bool {
	switch r {
	case ReadinessReadyNow, ReadinessReadyLater, ReadinessNotReady, ReadinessUnknown:
		return true
	default:
		return false
	}
}

// VacancyRisk is a closed, evidence-bearing risk band.
type VacancyRisk string

const (
	VacancyRiskLow      VacancyRisk = "LOW"
	VacancyRiskMedium   VacancyRisk = "MEDIUM"
	VacancyRiskHigh     VacancyRisk = "HIGH"
	VacancyRiskCritical VacancyRisk = "CRITICAL"
	VacancyRiskUnknown  VacancyRisk = "UNKNOWN"
)

func (r VacancyRisk) Valid() bool {
	switch r {
	case VacancyRiskLow, VacancyRiskMedium, VacancyRiskHigh, VacancyRiskCritical, VacancyRiskUnknown:
		return true
	default:
		return false
	}
}

// DisclosureScope controls whether a caller may receive slate membership.
// The scope is an opaque capability name, not a candidate identifier.
type DisclosureScope string

const (
	DisclosureWithheld DisclosureScope = "WITHHELD"
	DisclosureScoped   DisclosureScope = "SCOPED"
)

func (d DisclosureScope) Valid() bool { return d == DisclosureWithheld || d == DisclosureScoped }

// CriticalRole is one immutable revision of a role whose vacancy has been
// classified as business-critical.
type CriticalRole struct {
	RoleID          string
	Revision        uint64
	ParentRevision  uint64
	ParentDigest    string
	PositionRef     string
	JobRevisionRef  string
	OwnerRef        string
	AuthorityRef    string
	EffectiveAt     values.Instant
	KnownAt         values.Instant
	EvidenceRefs    []string
	CanonicalDigest string
}

func (r CriticalRole) Validate() error {
	if strings.TrimSpace(r.RoleID) == "" {
		return fmt.Errorf("%w: role_id is required", ErrInvalidCriticalRole)
	}
	if r.Revision == 0 {
		return fmt.Errorf("%w: revision is required", ErrInvalidCriticalRole)
	}
	if r.Revision == 1 && (r.ParentRevision != 0 || r.ParentDigest != "") {
		return fmt.Errorf("%w: first revision cannot have a parent", ErrInvalidRevisionLineage)
	}
	if r.Revision > 1 && (r.ParentRevision == 0 || r.ParentRevision >= r.Revision || strings.TrimSpace(r.ParentDigest) == "") {
		return fmt.Errorf("%w: successor requires an earlier parent revision and digest", ErrInvalidRevisionLineage)
	}
	for name, value := range map[string]string{
		"position_ref": r.PositionRef, "job_revision_ref": r.JobRevisionRef,
		"owner_ref": r.OwnerRef, "authority_ref": r.AuthorityRef,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidCriticalRole, name)
		}
	}
	if err := r.EffectiveAt.Validate(); err != nil {
		return fmt.Errorf("%w: effective_at: %v", ErrInvalidCriticalRole, err)
	}
	if err := r.KnownAt.Validate(); err != nil {
		return fmt.Errorf("%w: known_at: %v", ErrInvalidCriticalRole, err)
	}
	if err := validateRefs(r.EvidenceRefs, ErrInvalidCriticalRole, "evidence_refs"); err != nil {
		return err
	}
	if r.CanonicalDigest != "" && r.CanonicalDigest != r.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidCriticalRole)
	}
	return nil
}

func (r CriticalRole) body() []byte {
	refs := sortedStrings(r.EvidenceRefs)
	w := canonicalbytes.New("hcmnext.domains.succession.CriticalRole", schemaVersion).
		String("role_id", r.RoleID).Int("revision", int64(r.Revision)).Int("parent_revision", int64(r.ParentRevision)).
		String("parent_digest", r.ParentDigest).String("position_ref", r.PositionRef).String("job_revision_ref", r.JobRevisionRef).
		String("owner_ref", r.OwnerRef).String("authority_ref", r.AuthorityRef).Value("effective_at", r.EffectiveAt).
		Value("known_at", r.KnownAt).SortedStrings("evidence_ref", refs)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (r CriticalRole) computedDigest() string { return canonicalbytes.Digest(r.body()) }
func (r CriticalRole) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}

// NewCriticalRole validates, detaches, and digests revision one or a supplied
// revision. It never stores the value.
func NewCriticalRole(r CriticalRole) (CriticalRole, error) {
	r.EvidenceRefs = append([]string(nil), r.EvidenceRefs...)
	if err := validateCriticalRoleWithoutDigest(r); err != nil {
		return CriticalRole{}, err
	}
	if r.CanonicalDigest == "" {
		r.CanonicalDigest = r.computedDigest()
	}
	if err := r.Validate(); err != nil {
		return CriticalRole{}, err
	}
	return r, nil
}

func validateCriticalRoleWithoutDigest(r CriticalRole) error {
	c := r
	c.CanonicalDigest = ""
	return c.Validate()
}

// Digest returns the canonical digest after validation.
func (r CriticalRole) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.CanonicalDigest, nil
}

// Revise returns a successor role revision and leaves the parent unchanged.
func (r CriticalRole) Revise(next CriticalRole) (CriticalRole, error) {
	if err := r.Validate(); err != nil {
		return CriticalRole{}, err
	}
	next.RoleID, next.Revision, next.ParentRevision, next.ParentDigest = r.RoleID, r.Revision+1, r.Revision, r.CanonicalDigest
	return NewCriticalRole(next)
}

// SuccessorReadinessRevision is an immutable, dated, evidence-backed
// assessment. Readiness and risk are labels; this record never carries an
// unlabeled managerial opinion.
type SuccessorReadinessRevision struct {
	SuccessorID      string
	Revision         uint64
	ParentRevision   uint64
	ParentDigest     string
	Readiness        ReadinessBand
	VacancyRisk      VacancyRisk
	AssessedBy       string
	AssessmentSource string
	EvidenceRefs     []string
	EffectiveAt      values.Instant
	KnownAt          values.Instant
	CanonicalDigest  string
}

func (a SuccessorReadinessRevision) Validate() error {
	if strings.TrimSpace(a.SuccessorID) == "" {
		return fmt.Errorf("%w: successor_id is required", ErrInvalidReadiness)
	}
	if a.Revision == 0 {
		return fmt.Errorf("%w: revision is required", ErrInvalidReadiness)
	}
	if a.Revision == 1 && (a.ParentRevision != 0 || a.ParentDigest != "") {
		return fmt.Errorf("%w: first revision cannot have a parent", ErrInvalidRevisionLineage)
	}
	if a.Revision > 1 && (a.ParentRevision == 0 || a.ParentRevision >= a.Revision || strings.TrimSpace(a.ParentDigest) == "") {
		return fmt.Errorf("%w: successor requires an earlier parent revision and digest", ErrInvalidRevisionLineage)
	}
	if !a.Readiness.Valid() {
		return fmt.Errorf("%w: readiness must be one of READY_NOW, READY_LATER, NOT_READY, UNKNOWN", ErrInvalidReadiness)
	}
	if !a.VacancyRisk.Valid() {
		return fmt.Errorf("%w: vacancy_risk is not declared", ErrInvalidReadiness)
	}
	for name, value := range map[string]string{"assessed_by": a.AssessedBy, "assessment_source": a.AssessmentSource} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidReadiness, name)
		}
	}
	if err := validateRefs(a.EvidenceRefs, ErrInvalidReadiness, "evidence_refs"); err != nil {
		return err
	}
	if err := a.EffectiveAt.Validate(); err != nil {
		return fmt.Errorf("%w: effective_at: %v", ErrInvalidReadiness, err)
	}
	if err := a.KnownAt.Validate(); err != nil {
		return fmt.Errorf("%w: known_at: %v", ErrInvalidReadiness, err)
	}
	if a.CanonicalDigest != "" && a.CanonicalDigest != a.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidReadiness)
	}
	return nil
}

func (a SuccessorReadinessRevision) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.succession.SuccessorReadinessRevision", schemaVersion).
		String("successor_id", a.SuccessorID).Int("revision", int64(a.Revision)).Int("parent_revision", int64(a.ParentRevision)).
		String("parent_digest", a.ParentDigest).String("readiness", string(a.Readiness)).String("vacancy_risk", string(a.VacancyRisk)).
		String("assessed_by", a.AssessedBy).String("assessment_source", a.AssessmentSource).SortedStrings("evidence_ref", sortedStrings(a.EvidenceRefs)).
		Value("effective_at", a.EffectiveAt).Value("known_at", a.KnownAt)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (a SuccessorReadinessRevision) computedDigest() string { return canonicalbytes.Digest(a.body()) }
func (a SuccessorReadinessRevision) Canonical() []byte {
	if a.Validate() != nil {
		return nil
	}
	return a.body()
}
func NewSuccessorReadinessRevision(a SuccessorReadinessRevision) (SuccessorReadinessRevision, error) {
	a.EvidenceRefs = append([]string(nil), a.EvidenceRefs...)
	if err := validateReadinessWithoutDigest(a); err != nil {
		return SuccessorReadinessRevision{}, err
	}
	if a.CanonicalDigest == "" {
		a.CanonicalDigest = a.computedDigest()
	}
	if err := a.Validate(); err != nil {
		return SuccessorReadinessRevision{}, err
	}
	return a, nil
}

func validateReadinessWithoutDigest(a SuccessorReadinessRevision) error {
	c := a
	c.CanonicalDigest = ""
	return c.Validate()
}

func (a SuccessorReadinessRevision) Digest() (string, error) {
	if err := a.Validate(); err != nil {
		return "", err
	}
	return a.CanonicalDigest, nil
}

// SuccessionSlate is a role-bound immutable revision. Candidate membership is
// intentionally absent from Explain and is returned only through a declared
// disclosure scope.
type SuccessionSlate struct {
	SlateID                   string
	Revision                  uint64
	ParentRevision            uint64
	ParentDigest              string
	CriticalRoleID            string
	CriticalRoleRevision      uint64
	CriticalRoleDigest        string
	PositionRef               string
	JobRevisionRef            string
	NominatorID               string
	NominatorRole             string
	NominatorAuthorizationRef string
	NominatorAuthorized       bool
	Visibility                DisclosureScope
	DeclaredScopes            []string
	Candidates                []SuccessorReadinessRevision
	EffectiveAt               values.Instant
	KnownAt                   values.Instant
	CanonicalDigest           string
}

func (s SuccessionSlate) Validate() error {
	if strings.TrimSpace(s.SlateID) == "" {
		return fmt.Errorf("%w: slate_id is required", ErrInvalidSlate)
	}
	if s.Revision == 0 {
		return fmt.Errorf("%w: revision is required", ErrInvalidSlate)
	}
	if s.Revision == 1 && (s.ParentRevision != 0 || s.ParentDigest != "") {
		return fmt.Errorf("%w: first revision cannot have a parent", ErrInvalidRevisionLineage)
	}
	if s.Revision > 1 && (s.ParentRevision == 0 || s.ParentRevision >= s.Revision || strings.TrimSpace(s.ParentDigest) == "") {
		return fmt.Errorf("%w: successor requires an earlier parent revision and digest", ErrInvalidRevisionLineage)
	}
	for name, value := range map[string]string{"critical_role_id": s.CriticalRoleID, "position_ref": s.PositionRef, "job_revision_ref": s.JobRevisionRef, "nominator_id": s.NominatorID, "nominator_role": s.NominatorRole, "nominator_authorization_ref": s.NominatorAuthorizationRef} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidSlate, name)
		}
	}
	if s.CriticalRoleRevision == 0 || strings.TrimSpace(s.CriticalRoleDigest) == "" {
		return fmt.Errorf("%w: critical_role revision and digest are required", ErrInvalidSlate)
	}
	if !s.NominatorAuthorized {
		return ErrUnauthorizedNomination
	}
	if !s.Visibility.Valid() {
		return fmt.Errorf("%w: visibility is not declared", ErrInvalidSlate)
	}
	if s.Visibility == DisclosureScoped && len(s.DeclaredScopes) == 0 {
		return fmt.Errorf("%w: declared_scopes is required for SCOPED visibility", ErrInvalidSlate)
	}
	if s.Visibility == DisclosureWithheld && len(s.DeclaredScopes) != 0 {
		return fmt.Errorf("%w: withheld visibility cannot declare scopes", ErrInvalidSlate)
	}
	if err := validateRefs(s.DeclaredScopes, ErrInvalidSlate, "declared_scopes"); err != nil && s.Visibility == DisclosureScoped {
		return err
	}
	if len(s.Candidates) == 0 {
		return fmt.Errorf("%w: candidates are required", ErrInvalidSlate)
	}
	seen := make(map[string]struct{}, len(s.Candidates))
	for _, candidate := range s.Candidates {
		if err := candidate.Validate(); err != nil {
			return fmt.Errorf("%w: candidate: %v", ErrInvalidSlate, err)
		}
		if _, ok := seen[candidate.SuccessorID]; ok {
			return fmt.Errorf("%w: duplicate successor_id %q", ErrInvalidSlate, candidate.SuccessorID)
		}
		seen[candidate.SuccessorID] = struct{}{}
	}
	if err := s.EffectiveAt.Validate(); err != nil {
		return fmt.Errorf("%w: effective_at: %v", ErrInvalidSlate, err)
	}
	if err := s.KnownAt.Validate(); err != nil {
		return fmt.Errorf("%w: known_at: %v", ErrInvalidSlate, err)
	}
	if s.CanonicalDigest != "" && s.CanonicalDigest != s.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidSlate)
	}
	return nil
}

func (s SuccessionSlate) body() []byte {
	candidates := append([]SuccessorReadinessRevision(nil), s.Candidates...)
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].SuccessorID < candidates[j].SuccessorID })
	w := canonicalbytes.New("hcmnext.domains.succession.SuccessionSlate", schemaVersion).
		String("slate_id", s.SlateID).Int("revision", int64(s.Revision)).Int("parent_revision", int64(s.ParentRevision)).String("parent_digest", s.ParentDigest).
		String("critical_role_id", s.CriticalRoleID).Int("critical_role_revision", int64(s.CriticalRoleRevision)).String("critical_role_digest", s.CriticalRoleDigest).
		String("position_ref", s.PositionRef).String("job_revision_ref", s.JobRevisionRef).String("nominator_id", s.NominatorID).String("nominator_role", s.NominatorRole).
		String("nominator_authorization_ref", s.NominatorAuthorizationRef).Bool("nominator_authorized", s.NominatorAuthorized).
		String("visibility", string(s.Visibility)).SortedStrings("declared_scope", sortedStrings(s.DeclaredScopes)).
		Value("effective_at", s.EffectiveAt).Value("known_at", s.KnownAt).Count("candidates", len(candidates))
	for _, candidate := range candidates {
		w.String("candidate", candidate.CanonicalDigest)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (s SuccessionSlate) computedDigest() string { return canonicalbytes.Digest(s.body()) }
func (s SuccessionSlate) Canonical() []byte {
	if s.Validate() != nil {
		return nil
	}
	return s.body()
}

func NewSuccessionSlate(s SuccessionSlate) (SuccessionSlate, error) {
	s.DeclaredScopes = append([]string(nil), s.DeclaredScopes...)
	s.Candidates = append([]SuccessorReadinessRevision(nil), s.Candidates...)
	for i := range s.Candidates {
		candidate, err := NewSuccessorReadinessRevision(s.Candidates[i])
		if err != nil {
			return SuccessionSlate{}, fmt.Errorf("%w: candidate: %v", ErrInvalidSlate, err)
		}
		s.Candidates[i] = candidate
	}
	if err := validateSlateWithoutDigest(s); err != nil {
		return SuccessionSlate{}, err
	}
	if s.CanonicalDigest == "" {
		s.CanonicalDigest = s.computedDigest()
	}
	if err := s.Validate(); err != nil {
		return SuccessionSlate{}, err
	}
	return s, nil
}

// NewSuccessionSlateRevision is an explicit revision spelling.
func NewSuccessionSlateRevision(s SuccessionSlate) (SuccessionSlate, error) {
	return NewSuccessionSlate(s)
}

// NewSuccessorReadiness is a concise constructor alias.
func NewSuccessorReadiness(a SuccessorReadinessRevision) (SuccessorReadinessRevision, error) {
	return NewSuccessorReadinessRevision(a)
}

func validateSlateWithoutDigest(s SuccessionSlate) error {
	c := s
	c.CanonicalDigest = ""
	return c.Validate()
}

func (s SuccessionSlate) Digest() (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	return s.CanonicalDigest, nil
}

// Revise appends a slate revision, retaining the parent digest and history.
func (s SuccessionSlate) Revise(candidates []SuccessorReadinessRevision, nominatorID, authorizationRef string) (SuccessionSlate, error) {
	if err := s.Validate(); err != nil {
		return SuccessionSlate{}, err
	}
	next := s
	next.Revision, next.ParentRevision, next.ParentDigest = s.Revision+1, s.Revision, s.CanonicalDigest
	next.Candidates = append([]SuccessorReadinessRevision(nil), candidates...)
	next.NominatorID, next.NominatorAuthorizationRef, next.CanonicalDigest = nominatorID, authorizationRef, ""
	return NewSuccessionSlate(next)
}

// CandidatesFor returns membership only when the requested capability is
// declared. Returned values are detached from the slate.
func (s SuccessionSlate) CandidatesFor(scope string) ([]SuccessorReadinessRevision, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if s.Visibility != DisclosureScoped || !contains(s.DeclaredScopes, scope) {
		return nil, ErrSlateMembershipWithheld
	}
	out := append([]SuccessorReadinessRevision(nil), s.Candidates...)
	for i := range out {
		out[i].EvidenceRefs = append([]string(nil), out[i].EvidenceRefs...)
	}
	return out, nil
}

// SuccessionSlateExplanation is audit-safe: it does not enumerate candidate
// IDs, assessment sources, or evidence references.
type SuccessionSlateExplanation struct {
	SlateID              string
	Revision             uint64
	CriticalRoleID       string
	CriticalRoleRevision uint64
	Visibility           DisclosureScope
	CandidateCount       int
	Digest               string
	NonGuaranteeNotice   string
}

func (s SuccessionSlate) Explain() (SuccessionSlateExplanation, error) {
	if err := s.Validate(); err != nil {
		return SuccessionSlateExplanation{}, err
	}
	candidateCount := 0
	if s.Visibility == DisclosureScoped {
		candidateCount = len(s.Candidates)
	}
	return SuccessionSlateExplanation{SlateID: s.SlateID, Revision: s.Revision, CriticalRoleID: s.CriticalRoleID, CriticalRoleRevision: s.CriticalRoleRevision, Visibility: s.Visibility, CandidateCount: candidateCount, Digest: s.CanonicalDigest, NonGuaranteeNotice: NonGuaranteeNotice}, nil
}

// SlateStore is the small in-memory port used by pure callers and tests.
type SlateStore interface {
	Save(SuccessionSlate) error
	Current(roleID string) (SuccessionSlate, error)
}

// MemorySlateStore keeps only the latest revision per role and rejects forks
// or conflicting current revisions. It is a fake port, not domain storage.
type MemorySlateStore struct {
	mu        sync.RWMutex
	current   map[string]SuccessionSlate
	critical  map[string]map[string]map[uint64]CriticalRole
	readiness map[string]map[string]map[uint64]SuccessorReadinessRevision
	slates    map[string]map[string]map[uint64]SuccessionSlate
}

func NewMemorySlateStore() *MemorySlateStore {
	return &MemorySlateStore{
		current:   make(map[string]SuccessionSlate),
		critical:  make(map[string]map[string]map[uint64]CriticalRole),
		readiness: make(map[string]map[string]map[uint64]SuccessorReadinessRevision),
		slates:    make(map[string]map[string]map[uint64]SuccessionSlate),
	}
}
func (m *MemorySlateStore) Save(s SuccessionSlate) error {
	if err := s.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	prior, ok := m.current[s.CriticalRoleID]
	if !ok {
		if s.Revision != 1 {
			return ErrConflictingCurrentRevision
		}
		m.current[s.CriticalRoleID] = cloneSlate(s)
		return nil
	}
	if s.Revision != prior.Revision+1 || s.ParentDigest != prior.CanonicalDigest {
		return ErrConflictingCurrentRevision
	}
	m.current[s.CriticalRoleID] = cloneSlate(s)
	return nil
}
func (m *MemorySlateStore) Current(roleID string) (SuccessionSlate, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.current[roleID]
	if !ok {
		return SuccessionSlate{}, fmt.Errorf("%w: role_id %q", ErrInvalidSlate, roleID)
	}
	return cloneSlate(s), nil
}

func validateRefs(refs []string, base error, field string) error {
	if len(refs) == 0 {
		return fmt.Errorf("%w: %s is required", base, field)
	}
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if strings.TrimSpace(ref) == "" {
			return fmt.Errorf("%w: %s contains an empty reference", base, field)
		}
		if _, ok := seen[ref]; ok {
			return fmt.Errorf("%w: %s contains duplicate reference", base, field)
		}
		seen[ref] = struct{}{}
	}
	return nil
}
func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
func contains(items []string, wanted string) bool {
	for _, item := range items {
		if item == wanted {
			return true
		}
	}
	return false
}
