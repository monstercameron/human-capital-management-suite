// Vulnerability patch-SLA clock and emergency-change path (SUPPLY-004).
//
// SUPPLY-002's impact Report names a severity and a remediation owner but
// starts no clock. This file binds each report severity to a
// remediation-SLA deadline, escalates an unresolved deadline, and opens a
// narrow emergency-change lane for critical, actively-exploited findings.
// The emergency lane is distinct from ordinary CICD-004 admission but never
// bypasses it: authorization requires a passing release-admission decision
// record and additionally binds a compensating control, a distinct
// approver and a verified rollback plan.
//
// The package stays kernel-pure: the caller supplies the SUPPLY-002
// report, the detection instant and the admission decision produced by the
// real release.Admit evaluation. AdmissionEvidence mirrors the fields of
// release.AdmissionDecision so the orchestrator can populate it verbatim.
package vulnimpact

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	// ErrInvalidSLA reports a malformed clock, policy, escalation or
	// emergency-change request.
	ErrInvalidSLA = errors.New("vulnimpact: invalid patch SLA")
	// ErrSLANotOverdue reports an escalation requested before the deadline.
	ErrSLANotOverdue = errors.New("vulnimpact: SLA deadline has not passed")
	// ErrEmergencyRefused reports an emergency-change request that does not
	// meet the critical/exploited/admission/control/approver/rollback bar.
	ErrEmergencyRefused = errors.New("vulnimpact: emergency change refused")
	// ErrAuthorizationTampered reports an authorization whose digest no
	// longer matches its contents.
	ErrAuthorizationTampered = errors.New("vulnimpact: emergency authorization is tampered")
)

// SLAPolicy is the remediation deadline per severity. Every duration must
// be positive; StartClock fails closed on a zero policy.
type SLAPolicy struct {
	Critical time.Duration
	High     time.Duration
	Moderate time.Duration
	Low      time.Duration
}

// DefaultSLAPolicy maps severity to remediation deadlines: critical
// findings remediate within a day, high within a week, moderate within a
// month and low within a quarter (CIS Control 7, NIST 800-53 SI-2).
func DefaultSLAPolicy() SLAPolicy {
	return SLAPolicy{
		Critical: 24 * time.Hour,
		High:     7 * 24 * time.Hour,
		Moderate: 30 * 24 * time.Hour,
		Low:      90 * 24 * time.Hour,
	}
}

// DeadlineFor resolves one severity to its remediation duration.
func (p SLAPolicy) DeadlineFor(severity Severity) (time.Duration, error) {
	switch severity {
	case SeverityCritical:
		return p.Critical, nil
	case SeverityHigh:
		return p.High, nil
	case SeverityModerate:
		return p.Moderate, nil
	case SeverityLow:
		return p.Low, nil
	default:
		return 0, fmt.Errorf("%w: severity %q carries no SLA", ErrInvalidSLA, severity)
	}
}

// SLAStatus is the clock state at an evaluation instant.
type SLAStatus string

const (
	// SLAOpen means the deadline has not passed yet.
	SLAOpen SLAStatus = "OPEN"
	// SLAOverdue means the deadline passed with no recorded remediation.
	SLAOverdue SLAStatus = "OVERDUE"
	// SLARemediated means remediation evidence closed the clock.
	SLARemediated SLAStatus = "REMEDIATED"
)

// SLAClock is one started remediation clock bound to a SUPPLY-002 report.
type SLAClock struct {
	VulnerabilityID string    `json:"vulnerability_id"`
	ReportDigest    string    `json:"report_digest"`
	Severity        Severity  `json:"severity"`
	DetectedAt      time.Time `json:"detected_at"`
	Deadline        time.Time `json:"deadline"`
	Remediated      bool      `json:"remediated"`
	Digest          string    `json:"digest"`
}

// StartClock resolves the report severity to a deadline from the
// detection instant. It fails closed on an unscored report, an unknown
// severity, a zero detection time or a non-positive SLA duration.
func StartClock(report Report, detectedAt time.Time, policy SLAPolicy) (SLAClock, error) {
	if strings.TrimSpace(report.Digest) == "" {
		return SLAClock{}, fmt.Errorf("%w: clock requires a scored impact report", ErrInvalidSLA)
	}
	if detectedAt.IsZero() {
		return SLAClock{}, fmt.Errorf("%w: detection time is required", ErrInvalidSLA)
	}
	window, err := policy.DeadlineFor(report.Vulnerability.Severity)
	if err != nil {
		return SLAClock{}, err
	}
	if window <= 0 {
		return SLAClock{}, fmt.Errorf("%w: SLA duration for %s is not positive", ErrInvalidSLA, report.Vulnerability.Severity)
	}
	clock := SLAClock{
		VulnerabilityID: report.Vulnerability.ID,
		ReportDigest:    report.Digest,
		Severity:        report.Vulnerability.Severity,
		DetectedAt:      detectedAt.UTC(),
		Deadline:        detectedAt.UTC().Add(window),
	}
	clock.Digest = hashSLA(struct {
		VulnerabilityID string   `json:"vulnerability_id"`
		ReportDigest    string   `json:"report_digest"`
		Severity        Severity `json:"severity"`
		DetectedAt      string   `json:"detected_at"`
		Deadline        string   `json:"deadline"`
	}{clock.VulnerabilityID, clock.ReportDigest, clock.Severity, clock.DetectedAt.Format(time.RFC3339), clock.Deadline.Format(time.RFC3339)})
	return clock, nil
}

// Status evaluates the clock at now. A remediated clock stays remediated;
// an open clock whose deadline has passed reports overdue.
func (c SLAClock) Status(now time.Time) SLAStatus {
	if c.Remediated {
		return SLARemediated
	}
	if !now.Before(c.Deadline) {
		return SLAOverdue
	}
	return SLAOpen
}

// MarkRemediated closes the clock with remediation evidence. It fails
// closed on an empty evidence reference.
func (c SLAClock) MarkRemediated(evidenceRef string) (SLAClock, error) {
	if strings.TrimSpace(evidenceRef) == "" {
		return SLAClock{}, fmt.Errorf("%w: remediation evidence is required", ErrInvalidSLA)
	}
	c.Remediated = true
	return c, nil
}

// Explain renders a deterministic clock summary.
func (c SLAClock) Explain() string {
	return fmt.Sprintf("patch SLA %s for %s (%s): detected %s, deadline %s, digest %s",
		c.VulnerabilityID, c.ReportDigest, c.Severity,
		c.DetectedAt.Format(time.RFC3339), c.Deadline.Format(time.RFC3339), c.Digest)
}

// Escalation is the visible record for an unresolved deadline.
type Escalation struct {
	ClockDigest string        `json:"clock_digest"`
	Severity    Severity      `json:"severity"`
	OverdueBy   time.Duration `json:"overdue_by"`
	Owner       string        `json:"owner"`
	DecidedAt   string        `json:"decided_at"`
	Digest      string        `json:"digest"`
}

// Escalate records that the clock is unresolved past its deadline. It is
// refused while the clock is still open or already remediated, and it
// requires a named escalation owner.
func Escalate(clock SLAClock, owner string, now time.Time) (Escalation, error) {
	if strings.TrimSpace(owner) == "" {
		return Escalation{}, fmt.Errorf("%w: escalation owner is required", ErrInvalidSLA)
	}
	if now.IsZero() {
		return Escalation{}, fmt.Errorf("%w: evaluation time is required", ErrInvalidSLA)
	}
	if clock.Status(now) != SLAOverdue {
		return Escalation{}, fmt.Errorf("%w: clock is %s", ErrSLANotOverdue, clock.Status(now))
	}
	escalation := Escalation{
		ClockDigest: clock.Digest,
		Severity:    clock.Severity,
		OverdueBy:   now.Sub(clock.Deadline),
		Owner:       owner,
		DecidedAt:   now.UTC().Format(time.RFC3339),
	}
	escalation.Digest = hashSLA(struct {
		ClockDigest string   `json:"clock_digest"`
		Severity    Severity `json:"severity"`
		OverdueBy   string   `json:"overdue_by"`
		Owner       string   `json:"owner"`
		DecidedAt   string   `json:"decided_at"`
	}{escalation.ClockDigest, escalation.Severity, escalation.OverdueBy.String(), escalation.Owner, escalation.DecidedAt})
	return escalation, nil
}

// Verify recomputes the escalation digest from the recorded fields and
// fails closed on tampering.
func (e Escalation) Verify() error {
	if e.Digest == "" {
		return fmt.Errorf("%w: escalation digest is missing", ErrAuthorizationTampered)
	}
	recomputed := Escalation{
		ClockDigest: e.ClockDigest, Severity: e.Severity, OverdueBy: e.OverdueBy,
		Owner: e.Owner, DecidedAt: e.DecidedAt,
	}
	recomputed.Digest = hashSLA(struct {
		ClockDigest string   `json:"clock_digest"`
		Severity    Severity `json:"severity"`
		OverdueBy   string   `json:"overdue_by"`
		Owner       string   `json:"owner"`
		DecidedAt   string   `json:"decided_at"`
	}{recomputed.ClockDigest, recomputed.Severity, recomputed.OverdueBy.String(), recomputed.Owner, recomputed.DecidedAt})
	if recomputed.Digest != e.Digest {
		return fmt.Errorf("%w: escalation digest mismatch", ErrAuthorizationTampered)
	}
	return nil
}

// AdmissionEvidence is the release-admission projection the emergency lane
// consumes. The orchestrator populates it verbatim from a passing
// release.Admit decision: the emergency path still passes admission, it
// only expedites the change around it.
type AdmissionEvidence struct {
	Admitted       bool      `json:"admitted"`
	Status         string    `json:"status"`
	Scope          string    `json:"scope"`
	ManifestDigest string    `json:"manifest_digest"`
	EvaluatedAt    time.Time `json:"evaluated_at"`
	Digest         string    `json:"digest"`
}

// RollbackPlan is the verified way back that must exist before an
// emergency change ships.
type RollbackPlan struct {
	Steps      []string `json:"steps"`
	Verified   bool     `json:"verified"`
	VerifiedBy string   `json:"verified_by"`
}

// EmergencyChangeRequest is one critical, actively-exploited finding
// asking for the emergency lane.
type EmergencyChangeRequest struct {
	VulnerabilityID     string            `json:"vulnerability_id"`
	FindingDigest       string            `json:"finding_digest"`
	Severity            Severity          `json:"severity"`
	ActivelyExploited   bool              `json:"actively_exploited"`
	CompensatingControl string            `json:"compensating_control"`
	Requester           string            `json:"requester"`
	Approver            string            `json:"approver"`
	Rollback            RollbackPlan      `json:"rollback"`
	Admission           AdmissionEvidence `json:"admission"`
}

// EmergencyAuthorization is the granted emergency change.
type EmergencyAuthorization struct {
	VulnerabilityID     string            `json:"vulnerability_id"`
	FindingDigest       string            `json:"finding_digest"`
	Severity            Severity          `json:"severity"`
	ActivelyExploited   bool              `json:"actively_exploited"`
	CompensatingControl string            `json:"compensating_control"`
	Requester           string            `json:"requester"`
	Approver            string            `json:"approver"`
	Rollback            RollbackPlan      `json:"rollback"`
	Admission           AdmissionEvidence `json:"admission"`
	Lane                string            `json:"lane"`
	AuthorizedAt        string            `json:"authorized_at"`
	Digest              string            `json:"digest"`
}

// AuthorizeEmergencyChange grants the emergency lane only when every bar
// holds: critical severity, active exploitation, a passing admission
// decision, a recorded compensating control, a distinct approver and a
// verified rollback plan from a distinct verifier.
func AuthorizeEmergencyChange(req EmergencyChangeRequest, now time.Time) (EmergencyAuthorization, error) {
	if now.IsZero() {
		return EmergencyAuthorization{}, fmt.Errorf("%w: authorization time is required", ErrInvalidSLA)
	}
	switch {
	case req.Severity != SeverityCritical:
		return EmergencyAuthorization{}, fmt.Errorf("%w: severity %s cannot use the emergency lane", ErrEmergencyRefused, req.Severity)
	case !req.ActivelyExploited:
		return EmergencyAuthorization{}, fmt.Errorf("%w: active exploitation is required", ErrEmergencyRefused)
	case strings.TrimSpace(req.VulnerabilityID) == "" || strings.TrimSpace(req.FindingDigest) == "":
		return EmergencyAuthorization{}, fmt.Errorf("%w: vulnerability and finding identity are required", ErrEmergencyRefused)
	case strings.TrimSpace(req.CompensatingControl) == "":
		return EmergencyAuthorization{}, fmt.Errorf("%w: compensating control is required", ErrEmergencyRefused)
	case strings.TrimSpace(req.Requester) == "":
		return EmergencyAuthorization{}, fmt.Errorf("%w: requester is required", ErrEmergencyRefused)
	case strings.TrimSpace(req.Approver) == "":
		return EmergencyAuthorization{}, fmt.Errorf("%w: approver is required", ErrEmergencyRefused)
	case req.Approver == req.Requester:
		return EmergencyAuthorization{}, fmt.Errorf("%w: approver must differ from requester", ErrEmergencyRefused)
	case len(req.Rollback.Steps) == 0 || !req.Rollback.Verified:
		return EmergencyAuthorization{}, fmt.Errorf("%w: rollback plan must list steps and be verified", ErrEmergencyRefused)
	case strings.TrimSpace(req.Rollback.VerifiedBy) == "" || req.Rollback.VerifiedBy == req.Requester:
		return EmergencyAuthorization{}, fmt.Errorf("%w: rollback verifier must be named and differ from requester", ErrEmergencyRefused)
	case !req.Admission.Admitted || req.Admission.Status != "ADMIT":
		return EmergencyAuthorization{}, fmt.Errorf("%w: a passing release-admission decision is required", ErrEmergencyRefused)
	case strings.TrimSpace(req.Admission.Digest) == "" || strings.TrimSpace(req.Admission.ManifestDigest) == "":
		return EmergencyAuthorization{}, fmt.Errorf("%w: admission decision identity is required", ErrEmergencyRefused)
	}
	auth := EmergencyAuthorization{
		VulnerabilityID: req.VulnerabilityID, FindingDigest: req.FindingDigest,
		Severity: req.Severity, ActivelyExploited: req.ActivelyExploited,
		CompensatingControl: req.CompensatingControl,
		Requester:           req.Requester, Approver: req.Approver,
		Rollback: req.Rollback, Admission: req.Admission,
		Lane: "emergency", AuthorizedAt: now.UTC().Format(time.RFC3339),
	}
	auth.Digest = authorizationDigest(auth)
	return auth, nil
}

// Verify recomputes the authorization digest and fails closed when any
// recorded field — control, approver, rollback or admission — changed
// after authorization.
func (a EmergencyAuthorization) Verify() error {
	if a.Lane != "emergency" || a.Digest == "" {
		return fmt.Errorf("%w: authorization is incomplete", ErrAuthorizationTampered)
	}
	if authorizationDigest(a) != a.Digest {
		return fmt.Errorf("%w: authorization digest mismatch", ErrAuthorizationTampered)
	}
	return nil
}

// Explain renders a deterministic authorization summary.
func (a EmergencyAuthorization) Explain() string {
	return fmt.Sprintf("emergency change %s (%s, exploited): control %q, approver %s, admission %s, digest %s",
		a.VulnerabilityID, a.Severity, a.CompensatingControl, a.Approver, a.Admission.Digest, a.Digest)
}

func authorizationDigest(a EmergencyAuthorization) string {
	return hashSLA(struct {
		VulnerabilityID     string            `json:"vulnerability_id"`
		FindingDigest       string            `json:"finding_digest"`
		Severity            Severity          `json:"severity"`
		ActivelyExploited   bool              `json:"actively_exploited"`
		CompensatingControl string            `json:"compensating_control"`
		Requester           string            `json:"requester"`
		Approver            string            `json:"approver"`
		Rollback            RollbackPlan      `json:"rollback"`
		Admission           AdmissionEvidence `json:"admission"`
		Lane                string            `json:"lane"`
		AuthorizedAt        string            `json:"authorized_at"`
	}{a.VulnerabilityID, a.FindingDigest, a.Severity, a.ActivelyExploited, a.CompensatingControl,
		a.Requester, a.Approver, a.Rollback, a.Admission, a.Lane, a.AuthorizedAt})
}

func hashSLA(view any) string {
	b, err := json.Marshal(view)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
