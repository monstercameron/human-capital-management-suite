package clockservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
)

// PeriodInputKind identifies the capture workflow that produced one period
// obligation. The period runner folds these through a registered reducer; it
// never switches on a vendor's payload shape.
type PeriodInputKind string

const (
	PeriodInputSession   PeriodInputKind = "SESSION"
	PeriodInputDuration  PeriodInputKind = "DURATION"
	PeriodInputException PeriodInputKind = "EXCEPTION"
)

func (k PeriodInputKind) valid() bool {
	return k == PeriodInputSession || k == PeriodInputDuration || k == PeriodInputException
}

// PeriodObligation is the normalized evidence collected from a session,
// duration sheet or exception-only run. Open obligations are retained for
// review but never contribute minutes to the approved aggregate.
type PeriodObligation struct {
	ID            string
	TenantID      string
	WorkerRef     string
	AssignmentRef string
	Kind          PeriodInputKind
	Minutes       int
	Open          bool
	Start         time.Time
	End           time.Time
	SourceRefs    []string
}

func (o PeriodObligation) validate(req PeriodTimecardRequest) error {
	if strings.TrimSpace(o.ID) == "" || !o.Kind.valid() || o.Minutes < 0 || (o.Open && o.Minutes != 0) {
		return fmt.Errorf("%w: invalid period obligation %q", ErrInvalidRequest, o.ID)
	}
	if o.TenantID != "" && o.TenantID != req.Trigger.TenantID {
		return fmt.Errorf("%w: obligation %q crosses tenant scope", ErrInvalidRequest, o.ID)
	}
	if o.WorkerRef != "" && o.WorkerRef != req.WorkerRef {
		return fmt.Errorf("%w: obligation %q names another worker", ErrInvalidRequest, o.ID)
	}
	if o.AssignmentRef != "" && o.AssignmentRef != req.AssignmentRef {
		return fmt.Errorf("%w: obligation %q names another assignment", ErrInvalidRequest, o.ID)
	}
	if !o.Start.IsZero() && o.Start.Before(req.Trigger.PeriodStart) || !o.End.IsZero() && o.End.After(req.Trigger.PeriodEnd) {
		return fmt.Errorf("%w: obligation %q falls outside the period", ErrInvalidRequest, o.ID)
	}
	if len(o.SourceRefs) == 0 {
		return fmt.Errorf("%w: obligation %q has no source evidence", ErrInvalidRequest, o.ID)
	}
	return nil
}

// PeriodTrigger is the schedule-owned identity of one period run. Group is
// required so a schedule cannot accidentally create one global run for a
// tenant's unrelated assignment groups.
type PeriodTrigger struct {
	TenantID        string
	PeriodID        string
	AssignmentGroup string
	PeriodStart     time.Time
	PeriodEnd       time.Time
	At              time.Time
}

// PeriodTimecardRequest starts one pinned period run. Profile is resolved and
// pinned by the caller before this request; it is not re-resolved while a run
// is open or during a typed reopen.
type PeriodTimecardRequest struct {
	Trigger        PeriodTrigger
	WorkerRef      string
	AssignmentRef  string
	Profile        timeprofile.TimeProfile
	Inputs         []PeriodObligation
	RulesRef       string
	WorkerActor    string
	ApproverActor  string
	IdempotencyKey string
	Revision       uint64
}

// PeriodAggregate is the reducer output that all later approvals and
// dispatches pin. TotalMinutes is recomputable from Inputs and is checked by
// the service before the aggregate can be approved.
type PeriodAggregate struct {
	TenantID      string
	PeriodID      string
	WorkerRef     string
	AssignmentRef string
	InputCount    int
	TotalMinutes  int
	Digest        string
}

type PeriodFoldRequest struct {
	Trigger       PeriodTrigger
	WorkerRef     string
	AssignmentRef string
	Profile       timeprofile.TimeProfile
	Inputs        []PeriodObligation
}

// PeriodReducer is the closed application seam for registered capture
// reducers. A reducer may normalize duration or exception semantics, but it
// must return the same aggregate total represented by its source obligations.
type PeriodReducer interface {
	Reduce(context.Context, PeriodFoldRequest) (PeriodAggregate, error)
}

type PeriodReducerFunc func(context.Context, PeriodFoldRequest) (PeriodAggregate, error)

func (f PeriodReducerFunc) Reduce(ctx context.Context, req PeriodFoldRequest) (PeriodAggregate, error) {
	return f(ctx, req)
}

// PeriodReducerRegistry resolves reducers by the pinned profile, keeping the
// period template independent of payroll, AP and VMS formats.
type PeriodReducerRegistry interface {
	Reducer(context.Context, timeprofile.TimeProfile) (PeriodReducer, error)
}

type PeriodReducers map[timeprofile.CaptureMode]PeriodReducer

func (r PeriodReducers) Reducer(_ context.Context, p timeprofile.TimeProfile) (PeriodReducer, error) {
	if reducer := r[p.Capture]; reducer != nil {
		return reducer, nil
	}
	return nil, fmt.Errorf("%w: no reducer for capture mode %s", ErrUnavailable, p.Capture)
}

// DefaultPeriodReducers registers the three period capture reducers. Their
// common fold is intentional: source-specific validation is done at the
// registration boundary while totals are reduced once in one place.
func DefaultPeriodReducers() PeriodReducers {
	fold := PeriodReducerFunc(func(_ context.Context, req PeriodFoldRequest) (PeriodAggregate, error) {
		seen := make(map[string]struct{}, len(req.Inputs))
		total := 0
		for _, input := range req.Inputs {
			if _, ok := seen[input.ID]; ok {
				return PeriodAggregate{}, fmt.Errorf("%w: duplicate obligation %s", ErrInvalidRequest, input.ID)
			}
			seen[input.ID] = struct{}{}
			if !input.Open {
				total += input.Minutes
			}
		}
		digest := periodInputDigest(req.Inputs)
		return PeriodAggregate{TenantID: req.Trigger.TenantID, PeriodID: req.Trigger.PeriodID, WorkerRef: req.WorkerRef, AssignmentRef: req.AssignmentRef, InputCount: len(req.Inputs), TotalMinutes: total, Digest: digest}, nil
	})
	return PeriodReducers{timeprofile.CapturePunch: fold, timeprofile.CaptureDuration: fold, timeprofile.CaptureException: fold}
}

type PeriodPremium struct {
	AggregateDigest string
	RegularMinutes  int
	PremiumMinutes  int
	Digest          string
}

type PeriodPremiumCalculator interface {
	Compute(context.Context, timeprofile.TimeProfile, PeriodAggregate) (PeriodPremium, error)
}

type PeriodWorkerAttestation struct {
	TenantID, PeriodID, WorkerRef, AggregateDigest, By string
	At                                                 time.Time
}

type PeriodWorkerAttestor interface {
	Attest(context.Context, string, string, string, string, string) (PeriodWorkerAttestation, error)
}

type PeriodApproval struct {
	TenantID, PeriodID, WorkerRef, AggregateDigest, By string
	At                                                 time.Time
}

type PeriodApprover interface {
	Approve(context.Context, string, string, string, string, string, string) (PeriodApproval, error)
}

// PeriodDestinationPayload is the connector-neutral round-trip fragment.
// Connectors map this to payroll, AP, agency/VMS or costing schemas outside
// this package.
type PeriodDestinationPayload struct {
	TenantID       string
	PeriodID       string
	WorkerRef      string
	AssignmentRef  string
	Destination    timeprofile.Destination
	Aggregate      PeriodAggregate
	Premium        PeriodPremium
	IdempotencyKey string
}

type PeriodDestinationAcceptance struct {
	TenantID, PeriodID, AggregateDigest, ReceiptRef, Reason string
	Accepted                                                bool
	At                                                      time.Time
}

type PeriodDestination interface {
	Dispatch(context.Context, PeriodDestinationPayload) (PeriodDestinationAcceptance, error)
}

type PeriodDestinationBindings interface {
	Bind(context.Context, string, timeprofile.Destination) (PeriodDestination, error)
}

type PeriodRunState string

const (
	PeriodRunLocked   PeriodRunState = "LOCKED"
	PeriodRunClosed   PeriodRunState = "CLOSED"
	PeriodRunReopened PeriodRunState = "REOPENED"
)

type PeriodRun struct {
	Trigger       PeriodTrigger
	WorkerRef     string
	AssignmentRef string
	Profile       timeprofile.TimeProfile
	Inputs        []PeriodObligation
	Aggregate     PeriodAggregate
	Premium       PeriodPremium
	Attestation   PeriodWorkerAttestation
	Approval      PeriodApproval
	Acceptance    PeriodDestinationAcceptance
	RulesRef      string
	Revision      uint64
	State         PeriodRunState
	ReopenRef     string
}

type PeriodRunStore interface {
	Get(context.Context, string, string) (PeriodRun, error)
	Put(context.Context, PeriodRun) error
}

// PeriodReopenRequest is the typed reopen path for a late session or a
// correction that lands after approval. Reopening without one of these
// reasons is refused before the run is changed.
type PeriodReopenRequest struct {
	TenantID       string
	PeriodID       string
	Reason         string
	LateInput      PeriodObligation
	ActorRef       string
	WorkerActor    string
	ApproverActor  string
	IdempotencyKey string
}

const (
	PeriodReopenLateSession = "LATE_SESSION"
	PeriodReopenCorrection  = "CORRECTION"
)

var (
	ErrPeriodNotEligible = errors.New("clockservice: profile is not eligible for a period timecard")
	ErrPeriodNotAccepted = errors.New("clockservice: destination did not accept the period timecard")
)

// PeriodTimecardService owns the application half of the period workflow.
// The workflow runtime supplies the trigger and task/approval execution; the
// service supplies the typed aggregate and connector round-trip.
type PeriodTimecardService struct {
	Reducers     PeriodReducerRegistry
	Premiums     PeriodPremiumCalculator
	Attestations PeriodWorkerAttestor
	Approvals    PeriodApprover
	Destinations PeriodDestinationBindings
	Runs         PeriodRunStore
	Clock        func() time.Time
}

func (s PeriodTimecardService) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func validatePeriodRequest(req PeriodTimecardRequest) error {
	if strings.TrimSpace(req.Trigger.TenantID) == "" || strings.TrimSpace(req.Trigger.PeriodID) == "" || strings.TrimSpace(req.Trigger.AssignmentGroup) == "" || strings.TrimSpace(req.WorkerRef) == "" || strings.TrimSpace(req.AssignmentRef) == "" || strings.TrimSpace(req.RulesRef) == "" || strings.TrimSpace(req.WorkerActor) == "" || strings.TrimSpace(req.ApproverActor) == "" || strings.TrimSpace(req.IdempotencyKey) == "" {
		return ErrInvalidRequest
	}
	if req.WorkerActor == req.ApproverActor {
		return fmt.Errorf("%w: worker cannot approve the same aggregate", ErrSelfApproval)
	}
	if req.Trigger.PeriodStart.IsZero() || req.Trigger.PeriodEnd.IsZero() || !req.Trigger.PeriodEnd.After(req.Trigger.PeriodStart) || req.Trigger.At.IsZero() {
		return fmt.Errorf("%w: period trigger has invalid bounds", ErrInvalidRequest)
	}
	if err := req.Profile.Validate(); err != nil {
		return err
	}
	if string(req.Profile.TenantRef) != req.Trigger.TenantID {
		return fmt.Errorf("%w: pinned profile belongs to another tenant", ErrInvalidRequest)
	}
	if req.Profile.Capture == timeprofile.CaptureNone {
		return ErrPeriodNotEligible
	}
	return nil
}

func (s PeriodTimecardService) validateWiring() error {
	if s.Reducers == nil || s.Premiums == nil || s.Attestations == nil || s.Approvals == nil || s.Destinations == nil || s.Runs == nil {
		return ErrUnavailable
	}
	return nil
}

func periodInputDigest(inputs []PeriodObligation) string {
	ordered := append([]PeriodObligation(nil), inputs...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	h := sha256.New()
	for _, input := range ordered {
		fmt.Fprintf(h, "%s\x00%s\x00%d\x00%t\x00%s\x00%s\x00", input.ID, input.Kind, input.Minutes, input.Open, input.Start.UTC().Format(time.RFC3339Nano), input.End.UTC().Format(time.RFC3339Nano))
		for _, source := range input.SourceRefs {
			fmt.Fprintf(h, "%s\x00", source)
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func validateInputs(req PeriodTimecardRequest) error {
	if len(req.Inputs) == 0 {
		return fmt.Errorf("%w: a period needs at least one obligation", ErrInvalidRequest)
	}
	seen := make(map[string]struct{}, len(req.Inputs))
	wantKind := PeriodInputKind("")
	switch req.Profile.Capture {
	case timeprofile.CapturePunch:
		wantKind = PeriodInputSession
	case timeprofile.CaptureDuration:
		wantKind = PeriodInputDuration
	case timeprofile.CaptureException:
		wantKind = PeriodInputException
	}
	for _, input := range req.Inputs {
		if err := input.validate(req); err != nil {
			return err
		}
		if input.Kind != wantKind {
			return fmt.Errorf("%w: %s profile cannot fold %s obligations", ErrInvalidRequest, req.Profile.Capture, input.Kind)
		}
		if _, ok := seen[input.ID]; ok {
			return fmt.Errorf("%w: duplicate obligation %s", ErrInvalidRequest, input.ID)
		}
		seen[input.ID] = struct{}{}
	}
	return nil
}

func (s PeriodTimecardService) execute(ctx context.Context, req PeriodTimecardRequest, reopenRef string) (PeriodRun, error) {
	if err := validatePeriodRequest(req); err != nil {
		return PeriodRun{}, err
	}
	if err := validateInputs(req); err != nil {
		return PeriodRun{}, err
	}
	reducer, err := s.Reducers.Reducer(ctx, req.Profile)
	if err != nil {
		return PeriodRun{}, err
	}
	aggregate, err := reducer.Reduce(ctx, PeriodFoldRequest{Trigger: req.Trigger, WorkerRef: req.WorkerRef, AssignmentRef: req.AssignmentRef, Profile: req.Profile, Inputs: append([]PeriodObligation(nil), req.Inputs...)})
	if err != nil {
		return PeriodRun{}, err
	}
	wantTotal := 0
	for _, input := range req.Inputs {
		if !input.Open {
			wantTotal += input.Minutes
		}
	}
	if aggregate.TenantID != req.Trigger.TenantID || aggregate.PeriodID != req.Trigger.PeriodID || aggregate.WorkerRef != req.WorkerRef || aggregate.AssignmentRef != req.AssignmentRef || aggregate.TotalMinutes != wantTotal || aggregate.Digest != periodInputDigest(req.Inputs) {
		return PeriodRun{}, fmt.Errorf("%w: reducer aggregate does not equal the session sum", ErrInvalidRequest)
	}
	premium, err := s.Premiums.Compute(ctx, req.Profile, aggregate)
	if err != nil {
		return PeriodRun{}, err
	}
	if premium.AggregateDigest != aggregate.Digest {
		return PeriodRun{}, fmt.Errorf("%w: premium result is not pinned to the aggregate", ErrInvalidRequest)
	}
	attestation, err := s.Attestations.Attest(ctx, req.Trigger.TenantID, req.Trigger.PeriodID, req.WorkerRef, req.WorkerActor, aggregate.Digest)
	if err != nil {
		return PeriodRun{}, err
	}
	if attestation.TenantID != req.Trigger.TenantID || attestation.PeriodID != req.Trigger.PeriodID || attestation.WorkerRef != req.WorkerRef || attestation.AggregateDigest != aggregate.Digest || attestation.By != req.WorkerActor || attestation.At.IsZero() {
		return PeriodRun{}, fmt.Errorf("%w: invalid worker attestation", ErrInvalidRequest)
	}
	approval, err := s.Approvals.Approve(ctx, req.Trigger.TenantID, req.Trigger.PeriodID, req.WorkerRef, req.ApproverActor, aggregate.Digest, req.RulesRef)
	if err != nil {
		return PeriodRun{}, err
	}
	if approval.TenantID != req.Trigger.TenantID || approval.PeriodID != req.Trigger.PeriodID || approval.WorkerRef != req.WorkerRef || approval.AggregateDigest != aggregate.Digest || approval.By != req.ApproverActor || approval.By == attestation.By || approval.At.IsZero() {
		return PeriodRun{}, fmt.Errorf("%w: invalid approval", ErrInvalidRequest)
	}
	revision := req.Revision
	if revision == 0 {
		revision = 1
	}
	run := PeriodRun{Trigger: req.Trigger, WorkerRef: req.WorkerRef, AssignmentRef: req.AssignmentRef, Profile: req.Profile, Inputs: append([]PeriodObligation(nil), req.Inputs...), Aggregate: aggregate, Premium: premium, Attestation: attestation, Approval: approval, RulesRef: req.RulesRef, Revision: revision, State: PeriodRunLocked, ReopenRef: reopenRef}
	if err := s.Runs.Put(ctx, run); err != nil {
		return PeriodRun{}, err
	}
	connector, err := s.Destinations.Bind(ctx, req.Trigger.TenantID, req.Profile.Destination)
	if err != nil {
		return PeriodRun{}, err
	}
	payload := PeriodDestinationPayload{TenantID: req.Trigger.TenantID, PeriodID: req.Trigger.PeriodID, WorkerRef: req.WorkerRef, AssignmentRef: req.AssignmentRef, Destination: req.Profile.Destination, Aggregate: aggregate, Premium: premium, IdempotencyKey: req.IdempotencyKey}
	acceptance, err := connector.Dispatch(ctx, payload)
	if err != nil {
		return PeriodRun{}, err
	}
	if !acceptance.Accepted || acceptance.TenantID != req.Trigger.TenantID || acceptance.PeriodID != req.Trigger.PeriodID || acceptance.AggregateDigest != aggregate.Digest || acceptance.ReceiptRef == "" || acceptance.At.IsZero() {
		return PeriodRun{}, fmt.Errorf("%w: %s", ErrPeriodNotAccepted, acceptance.Reason)
	}
	run.Acceptance = acceptance
	run.State = PeriodRunClosed
	if err := s.Runs.Put(ctx, run); err != nil {
		return PeriodRun{}, err
	}
	return run, nil
}

// RunPeriodTimecard executes the period fold through approval and waits for
// connector acceptance. The run is persisted locked before dispatch, so a
// connector retry cannot cause an unapproved aggregate to leave the tenant.
func (s PeriodTimecardService) RunPeriodTimecard(ctx context.Context, req PeriodTimecardRequest) (PeriodRun, error) {
	if err := s.validateWiring(); err != nil {
		return PeriodRun{}, err
	}
	return s.execute(ctx, req, "")
}

// ReopenPeriodTimecard is the typed late-session/correction route. It appends
// the new obligation, advances the period revision, and runs attestation,
// approval and destination acceptance again over the complete aggregate.
func (s PeriodTimecardService) ReopenPeriodTimecard(ctx context.Context, req PeriodReopenRequest) (PeriodRun, error) {
	if err := s.validateWiring(); err != nil {
		return PeriodRun{}, err
	}
	if req.TenantID == "" || req.PeriodID == "" || req.ActorRef == "" || req.IdempotencyKey == "" || !((req.Reason == PeriodReopenLateSession) || (req.Reason == PeriodReopenCorrection)) {
		return PeriodRun{}, ErrInvalidRequest
	}
	current, err := s.Runs.Get(ctx, req.TenantID, req.PeriodID)
	if err != nil {
		return PeriodRun{}, err
	}
	if current.Trigger.TenantID != req.TenantID || (current.State != PeriodRunClosed && current.State != PeriodRunLocked) {
		return PeriodRun{}, ErrInvalidRequest
	}
	reopen := current
	reopen.State = PeriodRunReopened
	reopen.Revision++
	reopen.ReopenRef = req.Reason + ":" + req.ActorRef
	reopen.Inputs = append(append([]PeriodObligation(nil), current.Inputs...), req.LateInput)
	baseReq := PeriodTimecardRequest{Trigger: current.Trigger, WorkerRef: current.WorkerRef, AssignmentRef: current.AssignmentRef, Profile: current.Profile, Inputs: reopen.Inputs, RulesRef: current.RulesRef, WorkerActor: req.WorkerActor, ApproverActor: req.ApproverActor, IdempotencyKey: req.IdempotencyKey, Revision: reopen.Revision}
	if err := req.LateInput.validate(baseReq); err != nil {
		return PeriodRun{}, err
	}
	if err := s.Runs.Put(ctx, reopen); err != nil {
		return PeriodRun{}, err
	}
	return s.execute(ctx, baseReq, reopen.ReopenRef)
}
