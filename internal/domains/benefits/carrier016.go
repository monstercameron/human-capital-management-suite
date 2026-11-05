package benefits

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/roundtrip"
)

// WF-CAP-016 binds benefits administration to carriers and vendors as
// DELEGATED contracts over connector bindings: continuation coverage
// events, insurability evidence round trips and outbound enrollment files.
// The domain rules below are kernel-pure; the bindings resolve them through
// the vendor round-trip fragment (dispatch CAPABILITY, correlated SIGNAL,
// bounded OBSERVE), never a fabricated result.

const (
	ContinuationBindingID = "benefits.continuation.carrier"
	InsurabilityBindingID = "benefits.insurability.vendor"
	EnrollmentBindingID   = "benefits.enrollment.carrier"

	ContinuationDispatchCapability = "hcmnext.benefits.dispatch_continuation_notice"
	InsurabilityDispatchCapability = "hcmnext.benefits.request_insurability_evidence"
	EnrollmentDispatchCapability   = "hcmnext.benefits.send_enrollment_file"
)

// ContinuationBinding declares the continuation-coverage round trip: the
// cell dispatches the election notice to the carrier, the member's election
// returns as a correlated callback, and silence falls back to bounded
// polling before quarantine.
func ContinuationBinding() roundtrip.Binding {
	return roundtrip.Binding{
		ID: ContinuationBindingID, Version: 1, Vendor: "vendor:benefits/continuation-carrier", DisplayName: "Continuation coverage carrier",
		Dispatch:   roundtrip.DispatchSpec{CapabilityID: ContinuationDispatchCapability, CapabilityVersion: 1, Operation: "dispatch", IdempotencyKeyField: "notice_id", AuthorityScopes: []string{"scope:benefits.continuation.dispatch"}},
		Callback:   roundtrip.CallbackSpec{EventType: "benefits.continuation.elected", CorrelationKey: "continuation_notice_id", SchemaRef: "benefits.continuation-election/v1", AcceptedSources: []string{"vendor:benefits/continuation-carrier"}},
		Poll:       roundtrip.PollSpec{MaxPolls: 6, IntervalSeconds: 86400, ReadCapabilityID: "hcmnext.benefits.read_continuation_election", ReadCapabilityVersion: 1, ReadOperation: "read"},
		SLA:        roundtrip.SLASpec{CallbackDueSeconds: 86400 * 65, HardDeadlineSeconds: 86400 * 75},
		Quarantine: roundtrip.QuarantineSpec{Route: "quarantine_review", ReasonCode: "CONTINUATION_ELECTION_OVERDUE"},
	}
}

// InsurabilityBinding declares the evidence-of-insurability round trip: the
// cell requests a vendor decision on excess coverage, and the vendor's
// approval, denial or evidence request returns correlated.
func InsurabilityBinding() roundtrip.Binding {
	return roundtrip.Binding{
		ID: InsurabilityBindingID, Version: 1, Vendor: "vendor:insurability.eoi", DisplayName: "Insurability evidence vendor",
		Dispatch:   roundtrip.DispatchSpec{CapabilityID: InsurabilityDispatchCapability, CapabilityVersion: 1, Operation: "dispatch", IdempotencyKeyField: "case_id", AuthorityScopes: []string{"scope:benefits.insurability.dispatch"}},
		Callback:   roundtrip.CallbackSpec{EventType: "benefits.insurability.decided", CorrelationKey: "insurability_case_id", SchemaRef: "benefits.insurability-decision/v1", AcceptedSources: []string{"vendor:insurability.eoi"}},
		Poll:       roundtrip.PollSpec{MaxPolls: 8, IntervalSeconds: 43200, ReadCapabilityID: "hcmnext.benefits.read_insurability_case", ReadCapabilityVersion: 1, ReadOperation: "read"},
		SLA:        roundtrip.SLASpec{CallbackDueSeconds: 86400 * 14, HardDeadlineSeconds: 86400 * 21},
		Quarantine: roundtrip.QuarantineSpec{Route: "quarantine_review", ReasonCode: "INSURABILITY_DECISION_OVERDUE"},
	}
}

// EnrollmentBinding declares the outbound enrollment feed: the cell sends
// the generated enrollment file to the carrier and the carrier's
// acknowledgement returns correlated.
func EnrollmentBinding() roundtrip.Binding {
	return roundtrip.Binding{
		ID: EnrollmentBindingID, Version: 1, Vendor: "vendor:benefits/enrollment-carrier", DisplayName: "Enrollment file carrier",
		Dispatch:   roundtrip.DispatchSpec{CapabilityID: EnrollmentDispatchCapability, CapabilityVersion: 1, Operation: "dispatch", IdempotencyKeyField: "file_digest", AuthorityScopes: []string{"scope:benefits.enrollment.dispatch"}},
		Callback:   roundtrip.CallbackSpec{EventType: "benefits.enrollment.acknowledged", CorrelationKey: "enrollment_file_digest", SchemaRef: "benefits.enrollment-ack/v1", AcceptedSources: []string{"vendor:benefits/enrollment-carrier"}},
		Poll:       roundtrip.PollSpec{MaxPolls: 5, IntervalSeconds: 21600, ReadCapabilityID: "hcmnext.benefits.read_enrollment_ack", ReadCapabilityVersion: 1, ReadOperation: "read"},
		SLA:        roundtrip.SLASpec{CallbackDueSeconds: 86400 * 3, HardDeadlineSeconds: 86400 * 5},
		Quarantine: roundtrip.QuarantineSpec{Route: "quarantine_review", ReasonCode: "ENROLLMENT_ACK_OVERDUE"},
	}
}

var ErrContinuationInvalid = errors.New("benefits: continuation offer request is invalid")

// ContinuationEvent is the closed qualifying-event vocabulary. Anything
// outside it is not a continuation trigger.
type ContinuationEvent string

const (
	ContinuationEventTermination           ContinuationEvent = "TERMINATION"
	ContinuationEventHoursReduction        ContinuationEvent = "HOURS_REDUCTION"
	ContinuationEventDivorce               ContinuationEvent = "DIVORCE"
	ContinuationEventDeath                 ContinuationEvent = "DEATH"
	ContinuationEventMedicare              ContinuationEvent = "MEDICARE_ENTITLEMENT"
	ContinuationEventLossOfDependentStatus ContinuationEvent = "LOSS_OF_DEPENDENT_STATUS"
)

// continuationMonths is the coverage duration each qualifying event opens.
var continuationMonths = map[ContinuationEvent]int{
	ContinuationEventTermination:           18,
	ContinuationEventHoursReduction:        18,
	ContinuationEventDivorce:               36,
	ContinuationEventDeath:                 36,
	ContinuationEventMedicare:              36,
	ContinuationEventLossOfDependentStatus: 36,
}

// ContinuationRequest stages one continuation offer. Dates are UTC midnights.
type ContinuationRequest struct {
	Event      ContinuationEvent
	EventDate  time.Time
	NotifiedOn time.Time
	PlanRef    string
	MemberRef  string
}

// ContinuationOffer is the computed election window and coverage span. The
// member elects within 60 days of notice; coverage runs retroactive to the
// event date for the event's duration.
type ContinuationOffer struct {
	ElectBy         time.Time
	CoverageFrom    time.Time
	CoverageThrough time.Time
	Months          int
}

// ContinuationOfferFor computes the offer for one qualifying event.
func ContinuationOfferFor(req ContinuationRequest) (ContinuationOffer, error) {
	months, ok := continuationMonths[req.Event]
	if !ok {
		return ContinuationOffer{}, fmt.Errorf("%w: unknown event %q", ErrContinuationInvalid, req.Event)
	}
	if strings.TrimSpace(req.PlanRef) == "" || strings.TrimSpace(req.MemberRef) == "" {
		return ContinuationOffer{}, fmt.Errorf("%w: plan and member refs are required", ErrContinuationInvalid)
	}
	if req.EventDate.IsZero() || req.NotifiedOn.IsZero() {
		return ContinuationOffer{}, fmt.Errorf("%w: event and notice dates are required", ErrContinuationInvalid)
	}
	if req.NotifiedOn.Before(req.EventDate) {
		return ContinuationOffer{}, fmt.Errorf("%w: notice predates the event", ErrContinuationInvalid)
	}
	return ContinuationOffer{
		ElectBy:         req.NotifiedOn.AddDate(0, 0, 60),
		CoverageFrom:    req.EventDate,
		CoverageThrough: req.EventDate.AddDate(0, months, 0),
		Months:          months,
	}, nil
}

var ErrInsurabilityInvalid = errors.New("benefits: insurability case or decision is invalid")

// The closed vendor-decision vocabulary for evidence-of-insurability.
const (
	InsurabilityApproved     = "APPROVED"
	InsurabilityDenied       = "DENIED"
	InsurabilityMoreEvidence = "MORE_EVIDENCE"
)

// InsurabilityCase stages one excess-coverage evidence request. Amounts are
// canonical decimal text; the guaranteed-issue amount always enrolls.
type InsurabilityCase struct {
	MemberRef       string
	CoverageAmount  string
	GuaranteedIssue string
}

// InsurabilityOutcome is the applied decision: what enrolls, whether the
// case stays pending, and what evidence the vendor still needs.
type InsurabilityOutcome struct {
	EnrolledAmount string
	Pending        bool
	NeededEvidence []string
}

// ApplyInsurabilityDecision applies one vendor decision to its case.
// APPROVED enrolls the full amount; DENIED caps at guaranteed issue;
// MORE_EVIDENCE keeps the case pending on the named evidence.
func ApplyInsurabilityDecision(stage InsurabilityCase, decision string, needed []string) (InsurabilityOutcome, error) {
	if strings.TrimSpace(stage.MemberRef) == "" || strings.TrimSpace(stage.CoverageAmount) == "" || strings.TrimSpace(stage.GuaranteedIssue) == "" {
		return InsurabilityOutcome{}, fmt.Errorf("%w: member, coverage and guaranteed-issue amounts are required", ErrInsurabilityInvalid)
	}
	switch decision {
	case InsurabilityApproved:
		return InsurabilityOutcome{EnrolledAmount: stage.CoverageAmount}, nil
	case InsurabilityDenied:
		return InsurabilityOutcome{EnrolledAmount: stage.GuaranteedIssue}, nil
	case InsurabilityMoreEvidence:
		return InsurabilityOutcome{Pending: true, NeededEvidence: append([]string(nil), needed...)}, nil
	default:
		return InsurabilityOutcome{}, fmt.Errorf("%w: unknown decision %q", ErrInsurabilityInvalid, decision)
	}
}

var ErrEnrollmentInvalid = errors.New("benefits: enrollment file input is invalid")

// The closed per-member action vocabulary of the enrollment feed.
const (
	EnrollmentAdd       = "ADD"
	EnrollmentChange    = "CHANGE"
	EnrollmentTerminate = "TERMINATE"
)

// EnrollmentMember is one member row of the outbound feed.
type EnrollmentMember struct {
	MemberID    string
	LastName    string
	FirstName   string
	DateOfBirth string // YYYY-MM-DD.
	PlanCode    string
	Tier        string
	Action      string
	Effective   string // YYYY-MM-DD.
}

// EnrollmentFile is the generated feed: deterministic bytes, member count
// and the sha256 digest the trailer carries (also the dispatch idempotency
// key, so a resend is the same file).
type EnrollmentFile struct {
	Bytes   []byte
	Digest  string
	Members int
}

// BuildEnrollmentFile renders the carrier feed. Members sort by member id;
// the header names the carrier, instant and roster size; the trailer totals
// each action and carries the sha256 of the header and member lines.
func BuildEnrollmentFile(carrier string, asOf time.Time, members []EnrollmentMember) (EnrollmentFile, error) {
	if strings.TrimSpace(carrier) == "" {
		return EnrollmentFile{}, fmt.Errorf("%w: carrier ref is required", ErrEnrollmentInvalid)
	}
	if len(members) == 0 {
		return EnrollmentFile{}, fmt.Errorf("%w: roster is empty", ErrEnrollmentInvalid)
	}
	ordered := append([]EnrollmentMember(nil), members...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].MemberID < ordered[j].MemberID })
	var adds, changes, terminates int
	lines := make([]string, 0, len(ordered)+2)
	lines = append(lines, "HDR|"+carrier+"|"+asOf.UTC().Format(time.RFC3339)+"|"+strconv.Itoa(len(ordered)))
	for _, member := range ordered {
		switch member.Action {
		case EnrollmentAdd:
			adds++
		case EnrollmentChange:
			changes++
		case EnrollmentTerminate:
			terminates++
		default:
			return EnrollmentFile{}, fmt.Errorf("%w: member %q has unknown action %q", ErrEnrollmentInvalid, member.MemberID, member.Action)
		}
		if strings.TrimSpace(member.MemberID) == "" || strings.TrimSpace(member.PlanCode) == "" {
			return EnrollmentFile{}, fmt.Errorf("%w: member id and plan code are required", ErrEnrollmentInvalid)
		}
		lines = append(lines, strings.Join([]string{"MEM", member.MemberID, member.LastName, member.FirstName,
			member.DateOfBirth, member.PlanCode, member.Tier, member.Action, member.Effective}, "|"))
	}
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	digest := "sha256:" + hex.EncodeToString(sum[:])
	lines = append(lines, "TRL|"+strconv.Itoa(len(ordered))+"|"+strconv.Itoa(adds)+"|"+strconv.Itoa(changes)+"|"+strconv.Itoa(terminates)+"|"+digest)
	return EnrollmentFile{Bytes: []byte(strings.Join(lines, "\n")), Digest: digest, Members: len(ordered)}, nil
}
