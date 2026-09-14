// Package qualification owns the pure vocabulary for qualification
// requirements and their read-only evaluation against held credentials.
// Nothing in this package persists facts, grants authorization, or creates
// work; an evaluation is descriptive evidence for a caller to consider.
package qualification

import (
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
	ErrInvalidRequirement = errors.New("qualification: invalid requirement")
	ErrInvalidCredential  = errors.New("qualification: invalid held credential")
	ErrInvalidEvaluation  = errors.New("qualification: invalid evaluation")
)

// EvidenceKind is the closed set of evidence that may support a requirement.
type EvidenceKind string

const (
	EvidenceVerifiedCredential EvidenceKind = "VERIFIED_CREDENTIAL"
	EvidenceVerifiedSkill      EvidenceKind = "VERIFIED_SKILL"
	EvidenceTrainingRecord     EvidenceKind = "TRAINING_RECORD"
	EvidenceAssessment         EvidenceKind = "ASSESSMENT"
	EvidenceLicense            EvidenceKind = "LICENSE"
)

var evidenceKinds = map[EvidenceKind]struct{}{
	EvidenceVerifiedCredential: {}, EvidenceVerifiedSkill: {},
	EvidenceTrainingRecord: {}, EvidenceAssessment: {}, EvidenceLicense: {},
}

func (k EvidenceKind) Valid() bool    { _, ok := evidenceKinds[k]; return ok }
func (k EvidenceKind) String() string { return string(k) }

// RenewalKind is the closed renewal policy vocabulary.
type RenewalKind string

const (
	RenewalNotRequired  RenewalKind = "NOT_REQUIRED"
	RenewalBeforeExpiry RenewalKind = "BEFORE_EXPIRY"
	RenewalAtExpiry     RenewalKind = "AT_EXPIRY"
	RenewalPeriodic     RenewalKind = "PERIODIC"
)

var renewalKinds = map[RenewalKind]struct{}{
	RenewalNotRequired: {}, RenewalBeforeExpiry: {}, RenewalAtExpiry: {}, RenewalPeriodic: {},
}

// RenewalRule states how a held qualification is expected to be renewed.
type RenewalRule struct {
	Kind       RenewalKind
	PeriodDays int
}

func (r RenewalRule) Validate() error {
	if _, ok := renewalKinds[r.Kind]; !ok {
		return fmt.Errorf("%w: renewal kind %q", ErrInvalidRequirement, r.Kind)
	}
	if r.PeriodDays < 0 || (r.Kind == RenewalPeriodic && r.PeriodDays == 0) {
		return fmt.Errorf("%w: renewal period must be positive for PERIODIC and never negative", ErrInvalidRequirement)
	}
	return nil
}

func (r RenewalRule) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.qualification.RenewalRule", schemaVersion).
		String("kind", string(r.Kind)).Int("period_days", int64(r.PeriodDays)).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// SubjectScope identifies the population to which a requirement applies.
type SubjectScope struct {
	Kind string
	Ref  string
}

func (s SubjectScope) Validate() error {
	if strings.TrimSpace(s.Kind) == "" || strings.TrimSpace(s.Ref) == "" {
		return fmt.Errorf("%w: subject scope kind and ref are required", ErrInvalidRequirement)
	}
	return nil
}

func (s SubjectScope) Canonical() []byte {
	if s.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.qualification.SubjectScope", schemaVersion).
		String("kind", s.Kind).String("ref", s.Ref).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// CredentialRequirement names a credential and its minimum level.
type CredentialRequirement struct {
	Ref   string
	Level int
}

func (r CredentialRequirement) Validate() error {
	if strings.TrimSpace(r.Ref) == "" || r.Level <= 0 {
		return fmt.Errorf("%w: credential ref and positive level are required", ErrInvalidRequirement)
	}
	return nil
}

func (r CredentialRequirement) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.qualification.CredentialRequirement", schemaVersion).
		String("ref", r.Ref).Int("level", int64(r.Level)).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// SkillRequirement names a skill and its minimum level.
type SkillRequirement struct {
	Ref   string
	Level int
}

func (r SkillRequirement) Validate() error {
	if strings.TrimSpace(r.Ref) == "" || r.Level <= 0 {
		return fmt.Errorf("%w: skill ref and positive level are required", ErrInvalidRequirement)
	}
	return nil
}

func (r SkillRequirement) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.qualification.SkillRequirement", schemaVersion).
		String("ref", r.Ref).Int("level", int64(r.Level)).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// QualificationRequirement is one immutable, versioned qualification rule.
// The additional context fields are explicit because a qualification without
// an assignment, authority, availability, experience, equivalency, validity,
// or policy context is not a complete requirement.
type QualificationRequirement struct {
	RequirementID string
	Revision      uint64
	SubjectScope  SubjectScope

	AssignmentContext string
	AuthorizationRef  string
	Experience        string
	EquivalencyRef    string
	PolicyRef         string
	Availability      values.EffectiveInterval
	Validity          values.EffectiveInterval
	Credentials       []CredentialRequirement
	Skills            []SkillRequirement
	AcceptedEvidence  []EvidenceKind
	Renewal           RenewalRule
	CanonicalDigest   string
}

func (r QualificationRequirement) Validate() error {
	if strings.TrimSpace(r.RequirementID) == "" || r.Revision == 0 {
		return fmt.Errorf("%w: requirement id and non-zero revision are required", ErrInvalidRequirement)
	}
	if err := r.SubjectScope.Validate(); err != nil {
		return err
	}
	for name, field := range map[string]string{
		"assignment context": r.AssignmentContext, "authorization ref": r.AuthorizationRef,
		"experience": r.Experience, "equivalency ref": r.EquivalencyRef, "policy ref": r.PolicyRef,
	} {
		if strings.TrimSpace(field) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidRequirement, name)
		}
	}
	if err := r.Availability.Validate(); err != nil {
		return fmt.Errorf("%w: availability: %v", ErrInvalidRequirement, err)
	}
	if err := r.Validity.Validate(); err != nil {
		return fmt.Errorf("%w: validity: %v", ErrInvalidRequirement, err)
	}
	if len(r.Credentials) == 0 && len(r.Skills) == 0 {
		return fmt.Errorf("%w: at least one credential or skill is required", ErrInvalidRequirement)
	}
	for _, c := range r.Credentials {
		if err := c.Validate(); err != nil {
			return err
		}
	}
	for _, s := range r.Skills {
		if err := s.Validate(); err != nil {
			return err
		}
	}
	if len(r.AcceptedEvidence) == 0 {
		return fmt.Errorf("%w: accepted evidence is required", ErrInvalidRequirement)
	}
	seen := make(map[EvidenceKind]struct{}, len(r.AcceptedEvidence))
	for _, k := range r.AcceptedEvidence {
		if !k.Valid() {
			return fmt.Errorf("%w: evidence kind %q is not declared", ErrInvalidRequirement, k)
		}
		if _, ok := seen[k]; ok {
			return fmt.Errorf("%w: duplicate evidence kind %q", ErrInvalidRequirement, k)
		}
		seen[k] = struct{}{}
	}
	if err := r.Renewal.Validate(); err != nil {
		return err
	}
	if r.CanonicalDigest != "" && r.CanonicalDigest != r.computedDigest() {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidRequirement)
	}
	return nil
}

func (r QualificationRequirement) body() []byte {
	credentials := append([]CredentialRequirement(nil), r.Credentials...)
	skills := append([]SkillRequirement(nil), r.Skills...)
	evidence := append([]EvidenceKind(nil), r.AcceptedEvidence...)
	sort.Slice(credentials, func(i, j int) bool {
		if credentials[i].Ref != credentials[j].Ref {
			return credentials[i].Ref < credentials[j].Ref
		}
		return credentials[i].Level < credentials[j].Level
	})
	sort.Slice(skills, func(i, j int) bool {
		if skills[i].Ref != skills[j].Ref {
			return skills[i].Ref < skills[j].Ref
		}
		return skills[i].Level < skills[j].Level
	})
	sort.Slice(evidence, func(i, j int) bool { return evidence[i] < evidence[j] })
	w := canonicalbytes.New("hcmnext.domains.qualification.QualificationRequirement", schemaVersion).
		String("requirement_id", r.RequirementID).Int("revision", int64(r.Revision)).Value("subject_scope", r.SubjectScope).
		String("assignment_context", r.AssignmentContext).String("authorization_ref", r.AuthorizationRef).
		String("experience", r.Experience).String("equivalency_ref", r.EquivalencyRef).String("policy_ref", r.PolicyRef).
		Value("availability", r.Availability).Value("validity", r.Validity).Value("renewal", r.Renewal).
		Count("credentials", len(credentials))
	for _, c := range credentials {
		w.Value("credential", c)
	}
	w.Count("skills", len(skills))
	for _, s := range skills {
		w.Value("skill", s)
	}
	w.Count("accepted_evidence", len(evidence))
	for _, k := range evidence {
		w.String("accepted_evidence", string(k))
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (r QualificationRequirement) computedDigest() string {
	b := r.body()
	if b == nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

// NewQualificationRequirement validates and digests an immutable requirement.
func NewQualificationRequirement(r QualificationRequirement) (QualificationRequirement, error) {
	r.Credentials = append([]CredentialRequirement(nil), r.Credentials...)
	r.Skills = append([]SkillRequirement(nil), r.Skills...)
	r.AcceptedEvidence = append([]EvidenceKind(nil), r.AcceptedEvidence...)
	r.CanonicalDigest = r.computedDigest()
	if err := r.Validate(); err != nil {
		return QualificationRequirement{}, err
	}
	return r, nil
}

func (r QualificationRequirement) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}
func (r QualificationRequirement) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}

// HeldCredential is a credential or skill assertion available to evaluation.
type HeldCredential struct {
	CredentialRef string
	SkillRef      string
	Level         int
	EvidenceKind  EvidenceKind
	EvidenceRef   string
	Validity      values.EffectiveInterval
}

func (h HeldCredential) Validate() error {
	if strings.TrimSpace(h.CredentialRef) == "" && strings.TrimSpace(h.SkillRef) == "" {
		return fmt.Errorf("%w: credential or skill ref is required", ErrInvalidCredential)
	}
	if h.Level <= 0 {
		return fmt.Errorf("%w: level must be positive", ErrInvalidCredential)
	}
	if !h.EvidenceKind.Valid() {
		return fmt.Errorf("%w: evidence kind %q", ErrInvalidCredential, h.EvidenceKind)
	}
	if strings.TrimSpace(h.EvidenceRef) == "" {
		return fmt.Errorf("%w: evidence ref is required", ErrInvalidCredential)
	}
	if err := h.Validity.Validate(); err != nil {
		return fmt.Errorf("%w: validity: %v", ErrInvalidCredential, err)
	}
	return nil
}

// RequirementKind distinguishes credential and skill gaps.
type RequirementKind string

const (
	RequirementCredential RequirementKind = "CREDENTIAL"
	RequirementSkill      RequirementKind = "SKILL"
)

// RequirementResult is the typed outcome for one requirement item.
// Restricted marks a requirement whose evidence the caller may not
// see: QUAL-004 lists its ref without disclosing evidence content.
type RequirementResult struct {
	Kind          RequirementKind
	Ref           string
	RequiredLevel int
	Status        Status
	Gap           string
	EvidenceRef   string
	Restricted    bool
}

// Status is the only qualification outcome vocabulary.
type Status string

const (
	StatusSatisfied   Status = "SATISFIED"
	StatusExpiring    Status = "EXPIRING"
	StatusUnsatisfied Status = "UNSATISFIED"
)

func (s Status) Valid() bool {
	return s == StatusSatisfied || s == StatusExpiring || s == StatusUnsatisfied
}

// Evaluation is a pure, detached qualification result.
type Evaluation struct {
	RequirementID   string
	Revision        uint64
	Results         []RequirementResult
	CanonicalDigest string
}

func (e Evaluation) Validate() error {
	if e.RequirementID == "" || e.Revision == 0 || len(e.Results) == 0 {
		return fmt.Errorf("%w: id, revision and results are required", ErrInvalidEvaluation)
	}
	for _, result := range e.Results {
		if result.Kind != RequirementCredential && result.Kind != RequirementSkill || result.Ref == "" || result.RequiredLevel <= 0 || !result.Status.Valid() {
			return fmt.Errorf("%w: malformed result", ErrInvalidEvaluation)
		}
		if result.Status == StatusSatisfied && result.Gap != "" {
			return fmt.Errorf("%w: satisfied result has a gap", ErrInvalidEvaluation)
		}
		if result.Status != StatusSatisfied && result.Gap == "" {
			return fmt.Errorf("%w: unsatisfied result must name its gap", ErrInvalidEvaluation)
		}
	}
	return nil
}

func (e Evaluation) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.qualification.Evaluation", schemaVersion).
		String("requirement_id", e.RequirementID).Int("revision", int64(e.Revision)).Count("results", len(e.Results))
	for _, r := range e.Results {
		w.String("kind", string(r.Kind)).String("ref", r.Ref).Int("required_level", int64(r.RequiredLevel)).String("status", string(r.Status)).String("gap", r.Gap).String("evidence_ref", r.EvidenceRef)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

// Evaluate compares held credentials to every required item. A valid item
// that ends before the requirement ends is EXPIRING; missing, expired, weak,
// or disallowed evidence is UNSATISFIED with a named gap.
func (r QualificationRequirement) Evaluate(held []HeldCredential) (Evaluation, error) {
	if err := r.Validate(); err != nil {
		return Evaluation{}, err
	}
	for i, h := range held {
		if err := h.Validate(); err != nil {
			return Evaluation{}, fmt.Errorf("held credential %d: %w", i, err)
		}
	}
	allowed := make(map[EvidenceKind]bool, len(r.AcceptedEvidence))
	for _, k := range r.AcceptedEvidence {
		allowed[k] = true
	}
	result := Evaluation{RequirementID: r.RequirementID, Revision: r.Revision}
	for _, req := range r.Credentials {
		result.Results = append(result.Results, evaluateItem(RequirementCredential, req.Ref, req.Level, r.Validity, held, allowed, func(h HeldCredential) bool { return h.CredentialRef == req.Ref }))
	}
	for _, req := range r.Skills {
		result.Results = append(result.Results, evaluateItem(RequirementSkill, req.Ref, req.Level, r.Validity, held, allowed, func(h HeldCredential) bool { return h.SkillRef == req.Ref }))
	}
	result.Results = append([]RequirementResult(nil), result.Results...)
	result.CanonicalDigest = canonicalbytes.Digest(result.body())
	return result, nil
}

func evaluateItem(kind RequirementKind, ref string, level int, required values.EffectiveInterval, held []HeldCredential, allowed map[EvidenceKind]bool, match func(HeldCredential) bool) RequirementResult {
	out := RequirementResult{Kind: kind, Ref: ref, RequiredLevel: level, Status: StatusUnsatisfied, Gap: string(kind) + ":" + ref}
	for _, h := range held {
		if !match(h) || h.Level < level {
			continue
		}
		if !allowed[h.EvidenceKind] {
			out.Gap = string(kind) + ":" + ref + ":evidence=" + string(h.EvidenceKind)
			continue
		}
		overlap, err := required.Overlaps(h.Validity)
		if err != nil || !overlap {
			continue
		}
		if intervalCovers(h.Validity, required) {
			out.Status, out.Gap, out.EvidenceRef = StatusSatisfied, "", h.EvidenceRef
			return out
		}
		if out.Status == StatusUnsatisfied {
			out.Status, out.Gap, out.EvidenceRef = StatusExpiring, string(kind)+":"+ref+":validity", h.EvidenceRef
		}
	}
	return out
}

func intervalCovers(container, target values.EffectiveInterval) bool {
	if container.Kind() != target.Kind() {
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
		if targetHasEnd && (!hasEnd || end.Compare(targetEnd) < 0) {
			return false
		}
		return true
	}
	start, _ := container.StartInstant()
	targetStart, _ := target.StartInstant()
	if start.Compare(targetStart) > 0 {
		return false
	}
	end, hasEnd := container.EndInstant()
	targetEnd, targetHasEnd := target.EndInstant()
	if targetHasEnd && (!hasEnd || end.Compare(targetEnd) < 0) {
		return false
	}
	return true
}

// QualificationExplanation is a compact, read-only summary suitable for an
// audit or UI layer.
type QualificationExplanation struct {
	RequirementID string
	Revision      uint64
	Satisfied     int
	Expiring      int
	Unsatisfied   int
	Digest        string
}

func (r QualificationRequirement) Explain() (QualificationExplanation, error) {
	if err := r.Validate(); err != nil {
		return QualificationExplanation{}, err
	}
	return QualificationExplanation{RequirementID: r.RequirementID, Revision: r.Revision, Digest: r.computedDigest()}, nil
}

// Explain returns the validated requirement's stable summary.
func Explain(r QualificationRequirement) (QualificationExplanation, error) { return r.Explain() }
