// Package assessment implements versioned privacy/data-protection risk
// assessments (REV-005-02, the DPIA equivalent): a reviewer-signed record
// per tenant and per PRIV-001 processing activity that documents
// necessity and proportionality, names risks to data subjects and the
// mitigations that answer them, and gates activation of automated-decision
// scoring or RESTRICTED-or-higher field processing. A missing, unsigned or
// stale assessment blocks activation; assessments never cross tenants.
package assessment

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Activation block reasons. Callers match with errors.Is.
var (
	ErrAssessmentMissing  = errors.New("privacy: no risk assessment for tenant activity")
	ErrAssessmentUnsigned = errors.New("privacy: risk assessment lacks a reviewer signature")
	ErrAssessmentStale    = errors.New("privacy: risk assessment is past its review interval")
	ErrAssessmentMismatch = errors.New("privacy: risk assessment names a different tenant or activity")
	ErrAssessmentInvalid  = errors.New("privacy: risk assessment is malformed")
)

// Risk is one identified risk to data subjects.
type Risk struct {
	Description string
	Severity    string
}

// Mitigation names one control answering the risks and the backlog item or
// policy that provides it (for example TRUST-010 field masks, TRUST-024
// consent, TRUST-018 DLP).
type Mitigation struct {
	Control   string
	Reference string
}

// Assessment is one versioned risk assessment for a tenant activity.
type Assessment struct {
	TenantID          string
	ActivityID        string
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

// Canonical renders the assessment deterministically for digests and golden
// pins. Risks and mitigations sort by their fields so logically identical
// assessments always produce identical bytes.
func (a Assessment) Canonical() string {
	risks := append([]Risk(nil), a.Risks...)
	sort.Slice(risks, func(i, j int) bool {
		if risks[i].Severity != risks[j].Severity {
			return risks[i].Severity < risks[j].Severity
		}
		return risks[i].Description < risks[j].Description
	})
	mitigations := append([]Mitigation(nil), a.Mitigations...)
	sort.Slice(mitigations, func(i, j int) bool {
		if mitigations[i].Control != mitigations[j].Control {
			return mitigations[i].Control < mitigations[j].Control
		}
		return mitigations[i].Reference < mitigations[j].Reference
	})
	var b strings.Builder
	fmt.Fprintf(&b, "tenant: %s\nactivity: %s\nversion: %d\ndecided_at: %s\nreview_interval: %s\n",
		a.TenantID, a.ActivityID, a.Version,
		a.DecidedAt.UTC().Format(time.RFC3339), a.ReviewInterval)
	fmt.Fprintf(&b, "necessity: %s\nproportionality: %s\n", a.Necessity, a.Proportionality)
	for _, r := range risks {
		fmt.Fprintf(&b, "risk: %s|%s\n", r.Severity, r.Description)
	}
	for _, m := range mitigations {
		fmt.Fprintf(&b, "mitigation: %s|%s\n", m.Control, m.Reference)
	}
	fmt.Fprintf(&b, "reviewer: %s\nsignature: %s\n", a.Reviewer, a.ReviewerSignature)
	return b.String()
}

// AuthorizeActivation reports whether high-risk processing may be enabled
// for (tenantID, activityID) at now. Automated-decision scoring and
// RESTRICTED-or-higher field processing require a signed, fresh assessment
// naming exactly this tenant and activity; anything else needs no
// assessment and returns nil.
func AuthorizeActivation(a *Assessment, tenantID, activityID string, automatedDecision, restrictedOrHigher bool, now time.Time) error {
	if !automatedDecision && !restrictedOrHigher {
		return nil
	}
	if a == nil {
		return fmt.Errorf("%w: tenant=%s activity=%s", ErrAssessmentMissing, tenantID, activityID)
	}
	if a.Version < 1 || a.TenantID == "" || a.ActivityID == "" {
		return fmt.Errorf("%w: tenant/activity/version must be set", ErrAssessmentInvalid)
	}
	if a.TenantID != tenantID || a.ActivityID != activityID {
		return fmt.Errorf("%w: assessment covers tenant=%s activity=%s", ErrAssessmentMismatch, a.TenantID, a.ActivityID)
	}
	if strings.TrimSpace(a.Reviewer) == "" || strings.TrimSpace(a.ReviewerSignature) == "" {
		return fmt.Errorf("%w: tenant=%s activity=%s version=%d", ErrAssessmentUnsigned, a.TenantID, a.ActivityID, a.Version)
	}
	if a.ReviewInterval <= 0 || !now.Before(a.DecidedAt.Add(a.ReviewInterval)) {
		return fmt.Errorf("%w: decided=%s interval=%s", ErrAssessmentStale,
			a.DecidedAt.UTC().Format(time.RFC3339), a.ReviewInterval)
	}
	return nil
}
