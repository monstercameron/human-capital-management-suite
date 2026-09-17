// LEARN-005: validate assessments and issue credentials. Score,
// attempts, proctoring and evidence yield exactly one of PASS, FAIL or
// UNKNOWN; only a PASS against a verified completion issues a credential,
// and every credential binds issuer, scope, dates, evidence and
// revocation status behind a canonical digest.
package learning

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

var ErrInvalidCredential = errors.New("learning: credential cannot be issued")

// Assessment verdicts share the closed vocabulary.
const (
	AssessmentPass    = "PASS"
	AssessmentFail    = "FAIL"
	AssessmentUnknown = "UNKNOWN"
)

// AssessmentOutcome is one assessed attempt.
type AssessmentOutcome struct {
	Score           string
	MaxScore        string
	AttemptsUsed    int
	AttemptsAllowed int
	Proctored       bool
	ProctorRef      string
	EvidenceRef     string
}

// CredentialRequest is one issuance request.
type CredentialRequest struct {
	LearnerID string
	CourseID  string
	Version   uint64
	Tenant    string
	Outcome   AssessmentOutcome
	At        time.Time
}

// Credential is one issued, revocable credential revision.
type Credential struct {
	ID                string
	LearnerID         string
	CourseID          string
	Version           uint64
	Tenant            string
	Issuer            string
	Scope             string
	IssuedAt          time.Time
	ExpiresAt         time.Time
	EvidenceRef       string
	Revoked           bool
	RevocationReason  string
	CredentialVersion uint64
	RenewalOf         string
	Digest            string
}

// compareDecimal compares two non-negative decimal literals.
func compareDecimal(a, b string) (int, error) {
	split := func(s string) (string, string, error) {
		if !isDecimalText(s) || strings.HasPrefix(s, "-") {
			return "", "", fmt.Errorf("not a non-negative decimal: %q", s)
		}
		intPart, fracPart := s, ""
		if i := strings.IndexByte(s, '.'); i >= 0 {
			intPart, fracPart = s[:i], s[i+1:]
		}
		intPart = strings.TrimLeft(intPart, "0")
		if intPart == "" {
			intPart = "0"
		}
		return intPart, fracPart, nil
	}
	ai, af, err := split(a)
	if err != nil {
		return 0, err
	}
	bi, bf, err := split(b)
	if err != nil {
		return 0, err
	}
	for len(af) < len(bf) {
		af += "0"
	}
	for len(bf) < len(af) {
		bf += "0"
	}
	if len(ai) != len(bi) {
		if len(ai) < len(bi) {
			return -1, nil
		}
		return 1, nil
	}
	if ai != bi {
		if ai < bi {
			return -1, nil
		}
		return 1, nil
	}
	switch {
	case af < bf:
		return -1, nil
	case af > bf:
		return 1, nil
	}
	return 0, nil
}

// EvaluateAssessmentStatic grades one outcome against a passing score
// without registry state, so fuzz and unit tests share the oracle.
func EvaluateAssessmentStatic(out AssessmentOutcome, passingScore string) string {
	if strings.TrimSpace(out.EvidenceRef) == "" {
		return AssessmentUnknown
	}
	cmp, err := compareDecimal(out.Score, passingScore)
	if err != nil {
		return AssessmentUnknown
	}
	if out.AttemptsAllowed > 0 && out.AttemptsUsed > out.AttemptsAllowed {
		return AssessmentFail
	}
	if !out.Proctored || strings.TrimSpace(out.ProctorRef) == "" {
		return AssessmentFail
	}
	if cmp < 0 {
		return AssessmentFail
	}
	return AssessmentPass
}

// EvaluateAssessment grades one outcome under one recorded version.
func (r *Registry) EvaluateAssessment(v CourseVersion, out AssessmentOutcome) string {
	return EvaluateAssessmentStatic(out, v.PassingScore)
}

// expiryDays parses "valid-N-days" expiry policies.
func expiryDays(policy string) int {
	var days int
	if _, err := fmt.Sscanf(policy, "valid-%d-days", &days); err != nil || days <= 0 {
		return 365
	}
	return days
}

func credentialDigest(c Credential) (string, error) {
	w := canonicalbytes.New("learning-credential", 1)
	w.String("id", c.ID)
	w.String("learner_id", c.LearnerID)
	w.String("course_id", c.CourseID)
	w.Int("version", int64(c.Version))
	w.String("tenant", c.Tenant)
	w.String("issuer", c.Issuer)
	w.String("scope", c.Scope)
	w.String("issued_at", c.IssuedAt.UTC().Format(time.RFC3339))
	w.String("expires_at", c.ExpiresAt.UTC().Format(time.RFC3339))
	w.String("evidence_ref", c.EvidenceRef)
	w.Bool("revoked", c.Revoked)
	w.Int("credential_version", int64(c.CredentialVersion))
	w.String("renewal_of", c.RenewalOf)
	raw, err := w.Bytes()
	if err != nil {
		return "", err
	}
	return canonicalbytes.Digest(raw), nil
}

// IssueCredential validates one assessment and, on PASS against the
// learner's verified completion, issues the credential.
func (r *Registry) IssueCredential(caller Caller, req CredentialRequest) (Credential, error) {
	if strings.TrimSpace(req.LearnerID) == "" || strings.TrimSpace(req.CourseID) == "" ||
		req.Version == 0 || strings.TrimSpace(req.Tenant) == "" || req.At.IsZero() {
		return Credential{}, fmt.Errorf("%w: learner, course, version, tenant and instant are required", ErrInvalidCredential)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.authorize(caller, req.Tenant); err != nil {
		return Credential{}, err
	}
	v, ok := r.courses.LoadVersion(req.CourseID, req.Version)
	if !ok || v.Tenant != req.Tenant {
		return Credential{}, fmt.Errorf("%w: %s", ErrUnknownCourse, versionKey(req.CourseID, req.Version))
	}
	if _, ok := r.completions.FindVerified(req.LearnerID, req.CourseID, req.Version); !ok {
		return Credential{}, fmt.Errorf("%w: no verified completion for learner", ErrInvalidCredential)
	}
	if verdict := r.evaluateLocked(v, req.Outcome); verdict != AssessmentPass {
		return Credential{}, fmt.Errorf("%w: assessment verdict is %s", ErrInvalidCredential, verdict)
	}
	cred := Credential{
		ID:        fmt.Sprintf("cred-%s-%s-%d", req.LearnerID, req.CourseID, req.Version),
		LearnerID: req.LearnerID, CourseID: req.CourseID, Version: req.Version,
		Tenant: req.Tenant, Issuer: v.ExternalAuthority,
		Scope:    fmt.Sprintf("course:%s", versionKey(req.CourseID, req.Version)),
		IssuedAt: req.At.UTC(), ExpiresAt: req.At.UTC().AddDate(0, 0, expiryDays(v.ExpiryPolicy)),
		EvidenceRef: req.Outcome.EvidenceRef, CredentialVersion: 1,
	}
	digest, err := credentialDigest(cred)
	if err != nil {
		return Credential{}, err
	}
	cred.Digest = digest
	r.credentials[cred.ID] = cred
	r.journal = append(r.journal, JournalEntry{
		Op: "issue-credential", Ref: cred.ID,
		Detail: fmt.Sprintf("learner=%s issuer=%s", cred.LearnerID, cred.Issuer),
	})
	return cred, nil
}

// evaluateLocked grades without locking; the caller must hold r.mu.
func (r *Registry) evaluateLocked(v CourseVersion, out AssessmentOutcome) string {
	return EvaluateAssessmentStatic(out, v.PassingScore)
}

// LookupCredential returns the recorded credential, if any.
func (r *Registry) LookupCredential(id string) (Credential, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.credentials[id]
	return c, ok
}
