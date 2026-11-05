// WF-CAP-017: bind recruiting vendors — job boards, background
// screening and employment eligibility verification.
//
// Recruit-to-onboard cannot post jobs, screen candidates or verify
// eligibility by itself: those are DELEGATED contracts over connector
// bindings with rule-pack deadlines. Posting delegates the requisition;
// screening runs the ordered pre-adverse and final adverse action
// sequence; eligibility verification runs the tentative-nonconfirmation
// branch. Screening reports stay in a restricted compartment: only the
// adjudicator's view carries findings.
package recruiting

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Vendor errors. All are matchable with errors.Is.
var (
	// ErrVendorInvalid is returned for a malformed binding, request or
	// viewer role.
	ErrVendorInvalid = errors.New("recruiting: invalid vendor request")
	// ErrVendorOrder is returned for a lifecycle step taken out of order.
	ErrVendorOrder = errors.New("recruiting: vendor step out of order")
	// ErrVendorDeadline is returned for an action past its rule-pack
	// deadline.
	ErrVendorDeadline = errors.New("recruiting: rule-pack deadline passed")
)

// Delegated vendor capabilities.
const (
	// CapabilityPosting posts requisitions to job boards.
	CapabilityPosting = "job-posting"
	// CapabilityScreening runs background screening.
	CapabilityScreening = "background-screening"
	// CapabilityEligibility verifies employment eligibility.
	CapabilityEligibility = "eligibility-verification"
)

// RulePack versions the deadline rules every vendor binding enforces.
type RulePack struct {
	Version                string
	VendorTurnaroundDays   int
	PostingDays            int
	PreAdverseResponseDays int
	FinalAdverseWaitDays   int
	TNCContestDays         int
}

func (p RulePack) valid() bool {
	return strings.TrimSpace(p.Version) != "" && p.VendorTurnaroundDays > 0 && p.PostingDays > 0 &&
		p.PreAdverseResponseDays > 0 && p.FinalAdverseWaitDays > 0 && p.TNCContestDays > 0
}

// VendorBinding is one DELEGATED contract: a capability performed by the
// named connector under a pinned rule pack.
type VendorBinding struct {
	Connector  string
	Capability string
	RulePack   RulePack
}

// BindVendor binds a capability to a connector under a rule pack.
func BindVendor(connector, capability string, pack RulePack) (VendorBinding, error) {
	if strings.TrimSpace(connector) == "" || strings.TrimSpace(connector) != connector {
		return VendorBinding{}, fmt.Errorf("%w: connector is required", ErrVendorInvalid)
	}
	switch capability {
	case CapabilityPosting, CapabilityScreening, CapabilityEligibility:
	default:
		return VendorBinding{}, fmt.Errorf("%w: unknown capability %q", ErrVendorInvalid, capability)
	}
	if !pack.valid() {
		return VendorBinding{}, fmt.Errorf("%w: rule pack %q has no deadlines", ErrVendorInvalid, pack.Version)
	}
	return VendorBinding{Connector: connector, Capability: capability, RulePack: pack}, nil
}

// DelegatedCall is one call across the connector binding. The payload
// carries refs under a per-operation allow-list, never PII; the deadline
// is the rule-pack deadline the call must meet.
type DelegatedCall struct {
	Connector  string
	Capability string
	Operation  string
	Payload    map[string]string
	Deadline   time.Time
}

// VendorResponse is the vendor's acknowledgement.
type VendorResponse struct {
	ReceiptID string
	Result    string
}

// ConnectorPort is the vendor on the far side of the binding.
type ConnectorPort interface {
	Invoke(call DelegatedCall) (VendorResponse, error)
}

func (b VendorBinding) require(capability string) error {
	if b.Capability != capability {
		return fmt.Errorf("%w: binding is %q, need %q", ErrVendorInvalid, b.Capability, capability)
	}
	return nil
}

// PostingRequest posts one requisition.
type PostingRequest struct {
	RequisitionID string
	Title         string
}

// JobPosting is one delegated posting with its rule-pack expiry.
type JobPosting struct {
	PostingID     string
	RequisitionID string
	Connector     string
	ExpiresAt     time.Time
}

// PostJob delegates a requisition posting to the bound job board.
func PostJob(b VendorBinding, req PostingRequest, port ConnectorPort, at time.Time) (JobPosting, error) {
	if err := b.require(CapabilityPosting); err != nil {
		return JobPosting{}, err
	}
	if strings.TrimSpace(req.RequisitionID) == "" || strings.TrimSpace(req.Title) == "" {
		return JobPosting{}, fmt.Errorf("%w: requisition and title are required", ErrVendorInvalid)
	}
	if port == nil {
		return JobPosting{}, fmt.Errorf("%w: no connector port", ErrVendorInvalid)
	}
	expires := at.AddDate(0, 0, b.RulePack.PostingDays)
	res, err := port.Invoke(DelegatedCall{
		Connector: b.Connector, Capability: b.Capability, Operation: "post",
		Payload:  map[string]string{"requisition_id": req.RequisitionID, "title": req.Title},
		Deadline: expires,
	})
	if err != nil {
		return JobPosting{}, err
	}
	return JobPosting{PostingID: res.ReceiptID, RequisitionID: req.RequisitionID, Connector: b.Connector, ExpiresAt: expires}, nil
}

// Screening states run ORDERED -> CLEAR | CONSIDER -> PRE_ADVERSE ->
// RESPONDED | EXPIRED -> FINAL_ADVERSE -> CLOSED_ADVERSE.
type ScreeningState string

// Screening states and results.
const (
	ScreeningOrdered            ScreeningState = "ORDERED"
	ScreeningClear              ScreeningState = "CLEAR"
	ScreeningConsider           ScreeningState = "CONSIDER"
	ScreeningPreAdverse         ScreeningState = "PRE_ADVERSE"
	ScreeningPreAdverseAnswered ScreeningState = "RESPONDED"
	ScreeningPreAdverseExpired  ScreeningState = "EXPIRED"
	ScreeningFinalAdverse       ScreeningState = "FINAL_ADVERSE"
	ScreeningClosedAdverse      ScreeningState = "CLOSED_ADVERSE"
)

// Screening is one background screening with its adverse-action state.
// It retains its rule pack so every deadline derives from the pinned
// pack rather than the caller's memory.
type Screening struct {
	ID           string
	CandidateRef string
	State        ScreeningState
	ReportID     string
	OrderedAt    time.Time
	PreAdverseAt time.Time
	RespondBy    time.Time
	Response     string
	Pack         RulePack
}

// StartScreening delegates a background screening order.
func StartScreening(b VendorBinding, candidateRef string, port ConnectorPort, at time.Time) (Screening, error) {
	if err := b.require(CapabilityScreening); err != nil {
		return Screening{}, err
	}
	if strings.TrimSpace(candidateRef) == "" {
		return Screening{}, fmt.Errorf("%w: candidate ref is required", ErrVendorInvalid)
	}
	if port == nil {
		return Screening{}, fmt.Errorf("%w: no connector port", ErrVendorInvalid)
	}
	res, err := port.Invoke(DelegatedCall{
		Connector: b.Connector, Capability: b.Capability, Operation: "screen",
		Payload:  map[string]string{"candidate_ref": candidateRef, "package": "standard"},
		Deadline: at.AddDate(0, 0, b.RulePack.VendorTurnaroundDays),
	})
	if err != nil {
		return Screening{}, err
	}
	return Screening{ID: res.ReceiptID, CandidateRef: candidateRef, State: ScreeningOrdered, OrderedAt: at.UTC(), Pack: b.RulePack}, nil
}

// RecordScreeningResult records the vendor's clear/consider result.
func RecordScreeningResult(s Screening, result ScreeningState, reportID string) (Screening, error) {
	if s.State != ScreeningOrdered {
		return s, fmt.Errorf("%w: result for screening in %s", ErrVendorOrder, s.State)
	}
	if result != ScreeningClear && result != ScreeningConsider {
		return s, fmt.Errorf("%w: result %q", ErrVendorInvalid, result)
	}
	if strings.TrimSpace(reportID) == "" {
		return s, fmt.Errorf("%w: report id is required", ErrVendorInvalid)
	}
	s.State, s.ReportID = result, reportID
	return s, nil
}

// BeginPreAdverse starts the pre-adverse action notice with its
// rule-pack response deadline.
func BeginPreAdverse(s Screening, at time.Time) (Screening, error) {
	if s.State != ScreeningConsider {
		return s, fmt.Errorf("%w: pre-adverse from %s", ErrVendorOrder, s.State)
	}
	s.State = ScreeningPreAdverse
	s.PreAdverseAt = at.UTC()
	s.RespondBy = at.AddDate(0, 0, s.Pack.PreAdverseResponseDays)
	return s, nil
}

// RecordPreAdverseResponse records the candidate's timely response.
func RecordPreAdverseResponse(s Screening, response string, at time.Time) (Screening, error) {
	if s.State != ScreeningPreAdverse {
		return s, fmt.Errorf("%w: response in %s", ErrVendorOrder, s.State)
	}
	if at.After(s.RespondBy) {
		return s, fmt.Errorf("%w: response due %v", ErrVendorDeadline, s.RespondBy)
	}
	if strings.TrimSpace(response) == "" {
		return s, fmt.Errorf("%w: response is required", ErrVendorInvalid)
	}
	s.State, s.Response = ScreeningPreAdverseAnswered, response
	return s, nil
}

// ExpirePreAdverse closes an unanswered pre-adverse notice after its
// deadline.
func ExpirePreAdverse(s Screening, at time.Time) (Screening, error) {
	if s.State != ScreeningPreAdverse {
		return s, fmt.Errorf("%w: expiry in %s", ErrVendorOrder, s.State)
	}
	if !at.After(s.RespondBy) {
		return s, fmt.Errorf("%w: notice still answerable until %v", ErrVendorOrder, s.RespondBy)
	}
	s.State = ScreeningPreAdverseExpired
	return s, nil
}

// BeginFinalAdverse starts final adverse action after the pre-adverse
// response or expiry and the rule-pack waiting period. The wait runs
// from the pre-adverse notice.
func BeginFinalAdverse(s Screening, at time.Time) (Screening, error) {
	if s.State != ScreeningPreAdverseAnswered && s.State != ScreeningPreAdverseExpired {
		return s, fmt.Errorf("%w: final adverse from %s", ErrVendorOrder, s.State)
	}
	waitUntil := s.PreAdverseAt.AddDate(0, 0, s.Pack.FinalAdverseWaitDays)
	if at.Before(waitUntil) {
		return s, fmt.Errorf("%w: waiting period runs to %v", ErrVendorOrder, waitUntil)
	}
	s.State = ScreeningFinalAdverse
	return s, nil
}

// RecordFinalAdverse records the final adverse action notice.
func RecordFinalAdverse(s Screening, _ time.Time) (Screening, error) {
	if s.State != ScreeningFinalAdverse {
		return s, fmt.Errorf("%w: final record from %s", ErrVendorOrder, s.State)
	}
	s.State = ScreeningClosedAdverse
	return s, nil
}

// Verification states run ORDERED -> AUTHORIZED | TNC -> CONTESTED ->
// AUTHORIZED | FINAL_TNC.
type VerificationState string

// Verification states and results.
const (
	VerifyOrdered      VerificationState = "ORDERED"
	VerifyAuthorized   VerificationState = "AUTHORIZED"
	VerifyTNC          VerificationState = "TENTATIVE_NONCONFIRMATION"
	VerifyTNCContested VerificationState = "TNC_CONTESTED"
	VerifyFinalTNC     VerificationState = "FINAL_NONCONFIRMATION"
)

// Verification is one employment eligibility verification. It retains
// its rule pack so the contest deadline derives from the pinned pack.
type Verification struct {
	ID           string
	CandidateRef string
	State        VerificationState
	OrderedAt    time.Time
	TNCAt        time.Time
	ContestBy    time.Time
	HireBlocked  bool
	Pack         RulePack
}

// StartVerification delegates an eligibility verification.
func StartVerification(b VendorBinding, candidateRef string, port ConnectorPort, at time.Time) (Verification, error) {
	if err := b.require(CapabilityEligibility); err != nil {
		return Verification{}, err
	}
	if strings.TrimSpace(candidateRef) == "" {
		return Verification{}, fmt.Errorf("%w: candidate ref is required", ErrVendorInvalid)
	}
	if port == nil {
		return Verification{}, fmt.Errorf("%w: no connector port", ErrVendorInvalid)
	}
	res, err := port.Invoke(DelegatedCall{
		Connector: b.Connector, Capability: b.Capability, Operation: "verify",
		Payload:  map[string]string{"candidate_ref": candidateRef},
		Deadline: at.AddDate(0, 0, b.RulePack.VendorTurnaroundDays),
	})
	if err != nil {
		return Verification{}, err
	}
	return Verification{ID: res.ReceiptID, CandidateRef: candidateRef, State: VerifyOrdered, OrderedAt: at.UTC(), Pack: b.RulePack}, nil
}

// RecordVerificationResult records the authorized result or opens the
// tentative-nonconfirmation branch with its contest deadline.
func RecordVerificationResult(v Verification, result VerificationState, at time.Time) (Verification, error) {
	if v.State != VerifyOrdered {
		return v, fmt.Errorf("%w: result for verification in %s", ErrVendorOrder, v.State)
	}
	switch result {
	case VerifyAuthorized:
		v.State = VerifyAuthorized
		return v, nil
	case VerifyTNC:
		v.State = VerifyTNC
		v.TNCAt = at.UTC()
		v.ContestBy = at.AddDate(0, 0, v.Pack.TNCContestDays)
		return v, nil
	default:
		return v, fmt.Errorf("%w: result %q", ErrVendorInvalid, result)
	}
}

// ContestTNC contests a tentative nonconfirmation before its deadline.
func ContestTNC(v Verification, at time.Time) (Verification, error) {
	if v.State != VerifyTNC {
		return v, fmt.Errorf("%w: contest in %s", ErrVendorOrder, v.State)
	}
	if at.After(v.ContestBy) {
		return v, fmt.Errorf("%w: contest due %v", ErrVendorDeadline, v.ContestBy)
	}
	v.State = VerifyTNCContested
	return v, nil
}

// ExpireTNC closes an uncontested tentative nonconfirmation after its
// deadline as a final nonconfirmation that blocks the hire: E-Verify
// final-nonconfirmation on failure to contest.
func ExpireTNC(v Verification, at time.Time) (Verification, error) {
	if v.State != VerifyTNC {
		return v, fmt.Errorf("%w: expiry in %s", ErrVendorOrder, v.State)
	}
	if !at.After(v.ContestBy) {
		return v, fmt.Errorf("%w: TNC still contestable until %v", ErrVendorOrder, v.ContestBy)
	}
	v.State = VerifyFinalTNC
	v.HireBlocked = true
	return v, nil
}

// ResolveTNC resolves a contested TNC as authorized or as a final
// nonconfirmation that blocks the hire.
func ResolveTNC(v Verification, resolution VerificationState, _ time.Time) (Verification, error) {
	if v.State != VerifyTNCContested {
		return v, fmt.Errorf("%w: resolve in %s", ErrVendorOrder, v.State)
	}
	switch resolution {
	case VerifyAuthorized:
		v.State = VerifyAuthorized
		return v, nil
	case VerifyFinalTNC:
		v.State = VerifyFinalTNC
		v.HireBlocked = true
		return v, nil
	default:
		return v, fmt.Errorf("%w: resolution %q", ErrVendorInvalid, resolution)
	}
}

// ScreeningReport is the vendor's background report. Findings and
// sources live in the restricted compartment.
type ScreeningReport struct {
	ID           string
	CandidateRef string
	Status       ScreeningState
	Findings     string
	Sources      []string
}

// ReportViewerRole scopes a screening-report view.
type ReportViewerRole string

// Declared report viewer roles.
const (
	// ReportViewerAdjudicator is the restricted HR adjudication role.
	ReportViewerAdjudicator ReportViewerRole = "HR_ADJUDICATOR"
	// ReportViewerRecruiter sees status only.
	ReportViewerRecruiter ReportViewerRole = "RECRUITER"
	// ReportViewerCandidate sees their own status only.
	ReportViewerCandidate ReportViewerRole = "CANDIDATE"
)

// ViewScreeningReport projects a report for a viewer role: only the
// adjudicator's view carries findings and sources.
func ViewScreeningReport(report ScreeningReport, role ReportViewerRole) (ScreeningReport, error) {
	switch role {
	case ReportViewerAdjudicator:
		return report, nil
	case ReportViewerRecruiter, ReportViewerCandidate:
		return ScreeningReport{ID: report.ID, CandidateRef: report.CandidateRef, Status: report.Status}, nil
	default:
		return ScreeningReport{}, fmt.Errorf("%w: role %q is not declared", ErrVendorInvalid, role)
	}
}
