package abuse

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	// ErrInvestigationRejected identifies an investigation transition that
	// would break compartment, evidence, separation of duties or outcome
	// discipline.
	ErrInvestigationRejected = errors.New("abuse: investigation rejected")
	// ErrInvestigationEvidence identifies a corrupted investigation record.
	ErrInvestigationEvidence = errors.New("abuse: investigation evidence is invalid")
)

// InvestigationState is the closed ABUSE-006 lifecycle vocabulary. There is
// deliberately no guilt or accusation state: SUBSTANTIATED describes the
// evidence for the finding, never the person.
type InvestigationState string

const (
	InvestigationOpen            InvestigationState = "OPEN"
	InvestigationUnderReview     InvestigationState = "UNDER_INVESTIGATION"
	InvestigationSubstantiated   InvestigationState = "SUBSTANTIATED"
	InvestigationUnsubstantiated InvestigationState = "UNSUBSTANTIATED"
	InvestigationInconclusive    InvestigationState = "INCONCLUSIVE"
)

func (s InvestigationState) Valid() bool {
	switch s {
	case InvestigationOpen, InvestigationUnderReview, InvestigationSubstantiated, InvestigationUnsubstantiated, InvestigationInconclusive:
		return true
	default:
		return false
	}
}

// DispositionOutcome is the closed result vocabulary.
type DispositionOutcome string

const (
	OutcomeSubstantiated   DispositionOutcome = "SUBSTANTIATED"
	OutcomeUnsubstantiated DispositionOutcome = "UNSUBSTANTIATED"
	OutcomeInconclusive    DispositionOutcome = "INCONCLUSIVE"
)

func (o DispositionOutcome) Valid() bool {
	switch o {
	case OutcomeSubstantiated, OutcomeUnsubstantiated, OutcomeInconclusive:
		return true
	default:
		return false
	}
}

// OpenRequest opens one compartmented investigation over a review signal.
type OpenRequest struct {
	ID           string
	Tenant       string
	Compartment  string
	Finding      RiskFinding
	Investigator string
	EvidenceRefs []string
	OpenedAt     time.Time
}

// DispositionRequest closes the evidence question. DecidedBy must differ
// from the investigator: four eyes before any finding is dispositioned.
type DispositionRequest struct {
	Outcome   DispositionOutcome
	Reason    string
	DecidedBy string
	DecidedAt time.Time
}

// Disposition is the recorded evidence result.
type Disposition struct {
	Outcome   DispositionOutcome
	Reason    string
	DecidedBy string
	DecidedAt time.Time
}

// Correction appends a fix to the record without rewriting history.
type Correction struct {
	Note      string
	Corrected string
	At        time.Time
}

// Transfer records one compartment move with its prior compartment.
type Transfer struct {
	From   string
	To     string
	Reason string
	By     string
	At     time.Time
}

// Investigation is the ABUSE-006 lifecycle: a compartmented, evidence-bound,
// SoD-guarded review of one risk finding. Consequence is never set here: a
// separate human decision owns every employment or access outcome, so no
// alert can equal guilt.
type Investigation struct {
	ID           string
	Tenant       string
	Compartment  string
	Finding      RiskFinding
	State        InvestigationState
	Investigator string
	EvidenceRefs []string
	Result       *Disposition
	Corrections  []Correction
	Trail        []Transfer
	Consequence  string
	Version      int
	Digest       string
}

// RequiresHumanDecision always reports true: investigation never authorizes
// a consequence by itself.
func (i Investigation) RequiresHumanDecision() bool { return true }

func investigationDigest(id, tenant, compartment, investigator string, evidence []string, version int) string {
	sum := sha256.Sum256([]byte(strings.Join(append([]string{id, tenant, compartment, investigator, fmt.Sprintf("%d", version)}, evidence...), "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (i Investigation) refresh() Investigation {
	i.Digest = investigationDigest(i.ID, i.Tenant, i.Compartment, i.Investigator, i.EvidenceRefs, i.Version)
	return i
}

// VerifyDigest proves the evidence binding still holds.
func (i Investigation) VerifyDigest() error {
	if i.Digest != investigationDigest(i.ID, i.Tenant, i.Compartment, i.Investigator, i.EvidenceRefs, i.Version) {
		return fmt.Errorf("%w: evidence binding does not match", ErrInvestigationEvidence)
	}
	return nil
}

// OpenInvestigation opens one investigation. The finding subject can never
// be its investigator.
func OpenInvestigation(req OpenRequest) (Investigation, error) {
	if strings.TrimSpace(req.ID) == "" || strings.TrimSpace(req.Tenant) == "" || strings.TrimSpace(req.Compartment) == "" {
		return Investigation{}, fmt.Errorf("%w: investigation needs id, tenant and compartment", ErrInvestigationRejected)
	}
	if strings.TrimSpace(req.Investigator) == "" || len(req.EvidenceRefs) == 0 || req.OpenedAt.IsZero() {
		return Investigation{}, fmt.Errorf("%w: investigator, evidence and opened time are required", ErrInvestigationRejected)
	}
	if req.Investigator == req.Finding.Evidence.Principal && req.Finding.Evidence.Principal != "" {
		return Investigation{}, fmt.Errorf("%w: the finding subject cannot investigate itself", ErrInvestigationRejected)
	}
	return Investigation{
		ID: req.ID, Tenant: req.Tenant, Compartment: req.Compartment, Finding: req.Finding,
		State: InvestigationOpen, Investigator: req.Investigator,
		EvidenceRefs: append([]string(nil), req.EvidenceRefs...), Version: 1,
	}.refresh(), nil
}

// Begin moves OPEN to UNDER_INVESTIGATION under the assigned investigator.
func (i Investigation) Begin(by string) (Investigation, error) {
	if i.State != InvestigationOpen {
		return Investigation{}, fmt.Errorf("%w: only an open investigation can begin", ErrInvestigationRejected)
	}
	if by != i.Investigator {
		return Investigation{}, fmt.Errorf("%w: only the assigned investigator can begin", ErrInvestigationRejected)
	}
	i.State = InvestigationUnderReview
	i.Version++
	return i.refresh(), nil
}

// Transfer moves the investigation to another compartment, preserving every
// evidence reference and recording the prior compartment.
func (i Investigation) Transfer(to, reason, by string) (Investigation, error) {
	if i.State != InvestigationOpen && i.State != InvestigationUnderReview {
		return Investigation{}, fmt.Errorf("%w: closed investigations do not transfer", ErrInvestigationRejected)
	}
	if strings.TrimSpace(to) == "" || strings.TrimSpace(reason) == "" {
		return Investigation{}, fmt.Errorf("%w: transfer needs a compartment and a reason", ErrInvestigationRejected)
	}
	i.Trail = append(i.Trail, Transfer{From: i.Compartment, To: to, Reason: reason, By: by, At: time.Now().UTC()})
	i.Compartment = to
	i.Version++
	return i.refresh(), nil
}

// Disposition closes the evidence question with one of the three declared
// outcomes. The decider must differ from the investigator.
func (i Investigation) Disposition(req DispositionRequest) (Investigation, error) {
	if i.State != InvestigationUnderReview {
		return Investigation{}, fmt.Errorf("%w: only an investigation under review can be dispositioned", ErrInvestigationRejected)
	}
	if !req.Outcome.Valid() {
		return Investigation{}, fmt.Errorf("%w: outcome %q is not declared", ErrInvestigationRejected, req.Outcome)
	}
	if strings.TrimSpace(req.Reason) == "" || strings.TrimSpace(req.DecidedBy) == "" {
		return Investigation{}, fmt.Errorf("%w: disposition needs a reason and a decider", ErrInvestigationRejected)
	}
	if req.DecidedBy == i.Investigator {
		return Investigation{}, fmt.Errorf("%w: the investigator cannot disposition their own case", ErrInvestigationRejected)
	}
	at := req.DecidedAt
	if at.IsZero() {
		at = time.Now().UTC()
	}
	i.Result = &Disposition{Outcome: req.Outcome, Reason: req.Reason, DecidedBy: req.DecidedBy, DecidedAt: at}
	i.State = InvestigationState(req.Outcome)
	i.Version++
	return i.refresh(), nil
}

// Correct appends a correction to an open or closed investigation without
// rewriting the recorded result.
func (i Investigation) Correct(note, by string) (Investigation, error) {
	if strings.TrimSpace(note) == "" || strings.TrimSpace(by) == "" {
		return Investigation{}, fmt.Errorf("%w: correction needs a note and an author", ErrInvestigationRejected)
	}
	i.Corrections = append(i.Corrections, Correction{Note: note, Corrected: by, At: time.Now().UTC()})
	i.Version++
	return i.refresh(), nil
}
