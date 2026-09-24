// Package assessment implements versioned, reviewer-signed privacy risk
// assessments scoped to one tenant and one PRIV-001 processing activity.
// Automated decision processing and RESTRICTED-or-higher field processing
// are denied unless a complete, current assessment verifies against a
// trusted reviewer key.
package assessment

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Activation block reasons. Callers can classify failures with errors.Is.
var (
	ErrAssessmentMissing   = errors.New("privacy: no risk assessment for tenant activity")
	ErrAssessmentUnsigned  = errors.New("privacy: risk assessment lacks a valid reviewer signature")
	ErrAssessmentStale     = errors.New("privacy: risk assessment is outside its review interval")
	ErrAssessmentMismatch  = errors.New("privacy: risk assessment names a different tenant or activity")
	ErrAssessmentInvalid   = errors.New("privacy: risk assessment is malformed")
	ErrActivityUnavailable = errors.New("privacy: processing activity is not an approved inventory release")
	ErrInventoryMismatch   = errors.New("privacy: processing inventory release failed validation")
)

// MaximumReviewInterval bounds the freshness interval an assessment can
// declare. A reviewer cannot make an approval effectively permanent by
// supplying an unbounded interval.
const MaximumReviewInterval = 365 * 24 * time.Hour

// Risk describes a plausible adverse outcome for data subjects. Likelihood
// and Impact are ordinal values: low, medium, or high.
type Risk struct {
	Description      string `json:"description"`
	AffectedSubjects string `json:"affected_subjects"`
	Likelihood       string `json:"likelihood"`
	Impact           string `json:"impact"`
	Severity         string `json:"severity"`
}

// Mitigation identifies a safeguard and the control or policy implementing it.
type Mitigation struct {
	Control   string `json:"control"`
	Reference string `json:"reference"`
}

// PrivacyRiskAssessment is the versioned risk assessment for a tenant processing
// activity. ReviewerSignature is base64url encoded Ed25519 over CanonicalBytes.
// Reviewer public keys must come from a trusted reviewer registry supplied to
// AuthorizeActivation; an assessment cannot introduce its own trust key.
type PrivacyRiskAssessment struct {
	TenantID          string
	ActivityID        string
	ActivityVersion   string
	InventoryDigest   string
	Version           int
	DecidedAt         time.Time
	ReviewInterval    time.Duration
	Necessity         string
	Proportionality   string
	Risks             []Risk
	Mitigations       []Mitigation
	Reviewer          string
	ReviewerSignature string
}

// Assessment is the concise name for PrivacyRiskAssessment.
type Assessment = PrivacyRiskAssessment

type canonicalAssessment struct {
	TenantID        string        `json:"tenant_id"`
	ActivityID      string        `json:"activity_id"`
	ActivityVersion string        `json:"activity_version"`
	InventoryDigest string        `json:"inventory_digest"`
	Version         int           `json:"version"`
	DecidedAt       string        `json:"decided_at"`
	ReviewInterval  time.Duration `json:"review_interval_ns"`
	Necessity       string        `json:"necessity"`
	Proportionality string        `json:"proportionality"`
	Risks           []Risk        `json:"risks"`
	Mitigations     []Mitigation  `json:"mitigations"`
	Reviewer        string        `json:"reviewer"`
}

// CanonicalBytes produces stable JSON for the assessment contents covered by
// the reviewer signature. The signature itself is excluded to avoid a cycle.
func (a Assessment) CanonicalBytes() []byte {
	risks := append([]Risk(nil), a.Risks...)
	sort.Slice(risks, func(i, j int) bool {
		if risks[i].Severity != risks[j].Severity {
			return risks[i].Severity < risks[j].Severity
		}
		if risks[i].Description != risks[j].Description {
			return risks[i].Description < risks[j].Description
		}
		if risks[i].AffectedSubjects != risks[j].AffectedSubjects {
			return risks[i].AffectedSubjects < risks[j].AffectedSubjects
		}
		if risks[i].Likelihood != risks[j].Likelihood {
			return risks[i].Likelihood < risks[j].Likelihood
		}
		return risks[i].Impact < risks[j].Impact
	})
	mitigations := append([]Mitigation(nil), a.Mitigations...)
	sort.Slice(mitigations, func(i, j int) bool {
		if mitigations[i].Control != mitigations[j].Control {
			return mitigations[i].Control < mitigations[j].Control
		}
		return mitigations[i].Reference < mitigations[j].Reference
	})
	encoded, _ := json.Marshal(canonicalAssessment{
		TenantID: a.TenantID, ActivityID: a.ActivityID, ActivityVersion: a.ActivityVersion, InventoryDigest: a.InventoryDigest, Version: a.Version,
		DecidedAt: a.DecidedAt.UTC().Format(time.RFC3339Nano), ReviewInterval: a.ReviewInterval,
		Necessity: a.Necessity, Proportionality: a.Proportionality,
		Risks: risks, Mitigations: mitigations, Reviewer: a.Reviewer,
	})
	return encoded
}

// Digest returns the stable SHA-256 digest of the signed contents.
func (a Assessment) Digest() string {
	digest := sha256.Sum256(a.CanonicalBytes())
	return "sha256:" + hex.EncodeToString(digest[:])
}

// SignAssessment signs the canonical contents with the reviewer's Ed25519
// private key and stores the signature as unpadded base64url.
func SignAssessment(a *Assessment, privateKey ed25519.PrivateKey) error {
	if a == nil || len(privateKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("%w: assessment and Ed25519 private key are required", ErrAssessmentInvalid)
	}
	if err := validate(*a); err != nil {
		return err
	}
	a.ReviewerSignature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, a.CanonicalBytes()))
	return nil
}

// AuthorizeActivation reports whether processing can be enabled for the exact
// tenant and PRIV-001 activity at now. Reviewer keys are trusted public keys
// indexed by reviewer identity, sourced outside of the assessment. A nil key
// map or missing key fails closed for gated processing.
func AuthorizeActivation(a *Assessment, tenantID, activityID string, highRisk, automatedDecision, restrictedOrHigher bool, now time.Time, trustedReviewers map[string]ed25519.PublicKey) error {
	if !highRisk && !automatedDecision && !restrictedOrHigher {
		return nil
	}
	if a == nil {
		return fmt.Errorf("%w: tenant=%s activity=%s", ErrAssessmentMissing, tenantID, activityID)
	}
	if a.TenantID != tenantID || a.ActivityID != activityID {
		return fmt.Errorf("%w: assessment covers tenant=%s activity=%s", ErrAssessmentMismatch, a.TenantID, a.ActivityID)
	}
	if err := validate(*a); err != nil {
		return err
	}
	if now.IsZero() || now.Before(a.DecidedAt) || !now.Before(a.DecidedAt.Add(a.ReviewInterval)) {
		return fmt.Errorf("%w: decided=%s interval=%s", ErrAssessmentStale, a.DecidedAt.UTC().Format(time.RFC3339Nano), a.ReviewInterval)
	}
	publicKey := trustedReviewers[a.Reviewer]
	signature, err := base64.RawURLEncoding.DecodeString(a.ReviewerSignature)
	if len(publicKey) != ed25519.PublicKeySize || err != nil || len(signature) != ed25519.SignatureSize || !ed25519.Verify(publicKey, a.CanonicalBytes(), signature) {
		return fmt.Errorf("%w: reviewer=%s", ErrAssessmentUnsigned, a.Reviewer)
	}
	return nil
}

func validate(a Assessment) error {
	if strings.TrimSpace(a.TenantID) == "" || strings.TrimSpace(a.ActivityID) == "" || strings.TrimSpace(a.ActivityVersion) == "" || strings.TrimSpace(a.InventoryDigest) == "" || a.Version < 1 ||
		a.DecidedAt.IsZero() || a.ReviewInterval <= 0 || a.ReviewInterval > MaximumReviewInterval ||
		strings.TrimSpace(a.Necessity) == "" || strings.TrimSpace(a.Proportionality) == "" || strings.TrimSpace(a.Reviewer) == "" {
		return fmt.Errorf("%w: required identity, version, review, rationale, or reviewer field is absent or invalid", ErrAssessmentInvalid)
	}
	if len(a.Risks) == 0 {
		return fmt.Errorf("%w: at least one data-subject risk is required", ErrAssessmentInvalid)
	}
	for _, risk := range a.Risks {
		if strings.TrimSpace(risk.Description) == "" || strings.TrimSpace(risk.AffectedSubjects) == "" ||
			!validLevel(risk.Likelihood) || !validLevel(risk.Impact) || !validLevel(risk.Severity) {
			return fmt.Errorf("%w: every risk needs a description, affected subjects, likelihood, impact, and severity", ErrAssessmentInvalid)
		}
	}
	if len(a.Mitigations) == 0 {
		return fmt.Errorf("%w: at least one mitigation is required", ErrAssessmentInvalid)
	}
	refs := make(map[string]bool, len(a.Mitigations))
	for _, mitigation := range a.Mitigations {
		if strings.TrimSpace(mitigation.Control) == "" || strings.TrimSpace(mitigation.Reference) == "" {
			return fmt.Errorf("%w: every mitigation needs a control and reference", ErrAssessmentInvalid)
		}
		refs[mitigation.Reference] = true
	}
	for _, required := range []string{"TRUST-010", "TRUST-018", "TRUST-024"} {
		if !refs[required] {
			return fmt.Errorf("%w: mitigation reference %s is required", ErrAssessmentInvalid, required)
		}
	}
	return nil
}

func validLevel(level string) bool {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "low", "medium", "high":
		return true
	default:
		return false
	}
}
