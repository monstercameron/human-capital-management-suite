// Package timeallocation coordinates approved time between the time, work and
// rewards capabilities. It owns neither punches nor pay rates: it stores
// typed source links, effective work-order allocations, correction deltas and
// the receipt at the payroll boundary.
package timeallocation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

var (
	ErrInvalid             = errors.New("timeallocation: invalid request")
	ErrUnauthorized        = errors.New("timeallocation: work-order authorization refused")
	ErrIdempotencyConflict = errors.New("timeallocation: idempotency conflict")
	ErrRevisionConflict    = errors.New("timeallocation: approved revision conflict")
	ErrStaleRevision       = errors.New("timeallocation: stale approved revision")
	ErrUnbalanced          = errors.New("timeallocation: approved minutes are not conserved")
	ErrAuthorityRequired   = errors.New("timeallocation: payroll authority decision required")
	ErrPayrollRejected     = errors.New("timeallocation: payroll handoff rejected")
	ErrNotFound            = errors.New("timeallocation: allocation not found")
	ErrUnavailable         = errors.New("timeallocation: required verifier unavailable")
)

// AuthorityOutcome is the closed result of the FTIME-001 authority decision.
type AuthorityOutcome string

const (
	AuthorityAccepted AuthorityOutcome = "ACCEPTED"
	AuthorityRejected AuthorityOutcome = "REJECTED"
)

// ReceiptStatus is the explicit outcome carried by every payroll handoff.
type ReceiptStatus string

const (
	ReceiptAccepted ReceiptStatus = "ACCEPTED"
	ReceiptRejected ReceiptStatus = "REJECTED"
)

// AuthorityDecision is server-issued evidence. The caller cannot choose a
// worker, period or revision outside this binding and still pass validation.
type AuthorityDecision struct {
	TenantID         string
	DecisionID       string
	TimecardID       string
	ApprovedRevision uint64
	Outcome          AuthorityOutcome
	DecidedBy        string
	DecisionDigest   string
	Reason           string
}

func (d AuthorityDecision) validate(tenant, timecard string, revision uint64) error {
	if strings.TrimSpace(d.TenantID) == "" || d.TenantID != tenant ||
		strings.TrimSpace(d.DecisionID) == "" || strings.TrimSpace(d.TimecardID) == "" || d.TimecardID != timecard ||
		d.ApprovedRevision == 0 || d.ApprovedRevision != revision || strings.TrimSpace(d.DecidedBy) == "" ||
		strings.TrimSpace(d.DecisionDigest) == "" {
		return ErrAuthorityRequired
	}
	if d.Outcome != AuthorityAccepted && d.Outcome != AuthorityRejected {
		return fmt.Errorf("%w: outcome %q is not declared", ErrAuthorityRequired, d.Outcome)
	}
	if d.Outcome == AuthorityRejected && strings.TrimSpace(d.Reason) == "" {
		return fmt.Errorf("%w: rejected decision needs a reason", ErrAuthorityRequired)
	}
	return nil
}

// ApprovedInterval is an approved, source-referenced interval. Its shares
// are the only place minutes may be assigned to work orders.
type ApprovedInterval struct {
	ID               string
	TimecardID       string
	ApprovedRevision uint64
	WorkerID         string
	SourceRef        string
	Minutes          int64
	Shares           []WorkOrderShare
}

// WorkOrderShare assigns whole approved minutes to one authorized line.
type WorkOrderShare struct {
	ProjectID   string
	WorkOrderID string
	LineID      string
	Minutes     int64
}

// AllocationRequest is a complete snapshot of one approved timecard
// revision. Repeating a source interval in a later revision creates a delta;
// repeating it in the same revision is refused unless the request is an exact
// idempotent replay.
type AllocationRequest struct {
	TenantID         string
	ActorID          string
	TimecardID       string
	ApprovedRevision uint64
	Intervals        []ApprovedInterval
	IdempotencyKey   string
}

// WorkOrderAuthorization is deliberately narrower than a general project
// reader. It authorizes this worker, project and order only; no pay data is
// part of the contract.
type WorkOrderAuthorization struct {
	TenantID    string
	ActorID     string
	WorkerID    string
	ProjectID   string
	WorkOrderID string
	LineID      string
}

// WorkOrderAuthorizer is the application boundary to work-order authority.
type WorkOrderAuthorizer interface {
	AuthorizeWorkOrder(context.Context, WorkOrderAuthorization) error
}

// AllowAllAuthorizer is useful for composition tests that have already
// exercised authorization in the upstream work-order service.
type AllowAllAuthorizer struct{}

func (AllowAllAuthorizer) AuthorizeWorkOrder(context.Context, WorkOrderAuthorization) error {
	return nil
}

// AllocationLine is an effective allocation. It has no rate or cost field;
// labor costing remains owned by the labor capability.
type AllocationLine struct {
	TenantID         string
	TimecardID       string
	ApprovedRevision uint64
	Revision         uint64
	IntervalID       string
	WorkerID         string
	ProjectID        string
	WorkOrderID      string
	LineID           string
	SourceRef        string
	Minutes          int64
}

// AllocationDelta is the immutable change from the previous effective
// revision. Negative values remove minutes from an old work-order line.
type AllocationDelta struct {
	IntervalID  string
	ProjectID   string
	WorkOrderID string
	LineID      string
	WorkerID    string
	SourceRef   string
	Minutes     int64
}

// AllocationRevision is the append-only allocation record and current
// effective projection for one approved timecard revision.
type AllocationRevision struct {
	TenantID         string
	TimecardID       string
	ApprovedRevision uint64
	Revision         uint64
	PreviousRevision uint64
	SourceMinutes    int64
	AllocatedMinutes int64
	Lines            []AllocationLine
	Deltas           []AllocationDelta
	SourceDigest     string
	Digest           string
}

// Validate rechecks the effective projection and its conservation binding.
// It does not infer approval; the approved revision remains a required typed
// link supplied by the timecard authority.
func (r AllocationRevision) Validate() error {
	if strings.TrimSpace(r.TenantID) == "" || strings.TrimSpace(r.TimecardID) == "" || r.ApprovedRevision == 0 || r.Revision == 0 || r.SourceMinutes <= 0 || r.AllocatedMinutes <= 0 {
		return ErrInvalid
	}
	if r.PreviousRevision >= r.Revision && r.PreviousRevision != 0 {
		return ErrRevisionConflict
	}
	if len(r.Lines) == 0 || strings.TrimSpace(r.SourceDigest) == "" || strings.TrimSpace(r.Digest) == "" {
		return ErrInvalid
	}
	var total int64
	seen := make(map[string]struct{}, len(r.Lines))
	for _, line := range r.Lines {
		if line.TenantID != r.TenantID || line.TimecardID != r.TimecardID || line.ApprovedRevision != r.ApprovedRevision || line.Revision != r.Revision || strings.TrimSpace(line.IntervalID) == "" || strings.TrimSpace(line.WorkerID) == "" || strings.TrimSpace(line.ProjectID) == "" || strings.TrimSpace(line.WorkOrderID) == "" || strings.TrimSpace(line.LineID) == "" || strings.TrimSpace(line.SourceRef) == "" || line.Minutes <= 0 {
			return ErrInvalid
		}
		key := allocationLineKey(line)
		if _, ok := seen[key]; ok {
			return ErrInvalid
		}
		seen[key] = struct{}{}
		total += line.Minutes
	}
	if total != r.SourceMinutes || total != r.AllocatedMinutes {
		return ErrUnbalanced
	}
	want, err := digestOf(recordWithoutDigest(r))
	if err != nil || want != r.Digest {
		return ErrInvalid
	}
	return nil
}

// BillingDraftLine is a work-order-facing projection of effective minutes.
// DeltaMinutes lets a downstream draft apply a correction without replaying
// the entire timecard.
type BillingDraftLine struct {
	ProjectID    string
	WorkOrderID  string
	LineID       string
	Minutes      int64
	DeltaMinutes int64
	SourceRefs   []string
}

type BillingDraft struct {
	TenantID           string
	TimecardID         string
	ApprovedRevision   uint64
	AllocationRevision uint64
	Lines              []BillingDraftLine
}

type ReconciliationReport struct {
	TenantID           string
	TimecardID         string
	ApprovedRevision   uint64
	AllocationRevision uint64
	SourceMinutes      int64
	AllocatedMinutes   int64
	Balanced           bool
	Deltas             []AllocationDelta
	BillingDraft       BillingDraft
}

// PayrollLine is the payroll handoff's time-only representation. A payroll
// provider may join its own pay policy later; this package never exposes a
// rate to a project or work-order reader.
type PayrollLine struct {
	TimecardID       string
	ApprovedRevision uint64
	IntervalID       string
	WorkerID         string
	ProjectID        string
	WorkOrderID      string
	LineID           string
	SourceRef        string
	Minutes          int64
}

type PayrollExport struct {
	TenantID           string
	TimecardID         string
	ApprovedRevision   uint64
	AllocationRevision uint64
	Lines              []PayrollLine
	Digest             string
}

type HandoffReceipt struct {
	ReceiptID           string
	TenantID            string
	TimecardID          string
	ApprovedRevision    uint64
	AllocationRevision  uint64
	Status              ReceiptStatus
	AuthorityDecisionID string
	ExportDigest        string
	Reason              string
}

type HandoffResult struct {
	Export  PayrollExport
	Receipt HandoffReceipt
}

// PayrollAuthority verifies the FTIME-001 decision against server-owned
// authority state before any export is emitted.
type PayrollAuthority interface {
	VerifyPayrollAuthority(context.Context, AuthorityDecision) error
}

type handoffKey struct {
	tenant, timecard, actor, idempotency string
}

type allocationKey struct {
	tenant, timecard, actor, idempotency string
}

type storedAllocation struct {
	digest string
	record AllocationRevision
}

type storedHandoff struct {
	digest string
	result HandoffResult
}

// Ledger is a process-local application store. A production composition can
// put the same records behind a durable append-only adapter; the mutex keeps
// the ownership and idempotency guarantees true for native callers and race
// tests.
type Ledger struct {
	mu          sync.Mutex
	current     map[string]AllocationRevision
	allocations map[allocationKey]storedAllocation
	requests    map[handoffKey]storedHandoff
}

func NewLedger() *Ledger {
	return &Ledger{current: make(map[string]AllocationRevision), allocations: make(map[allocationKey]storedAllocation), requests: make(map[handoffKey]storedHandoff)}
}

func (l *Ledger) Allocate(ctx context.Context, auth WorkOrderAuthorizer, req AllocationRequest) (AllocationRevision, error) {
	if l == nil || auth == nil {
		return AllocationRevision{}, ErrUnavailable
	}
	if err := validateRequest(req); err != nil {
		return AllocationRevision{}, err
	}
	normalized := normalizeIntervals(req.Intervals)
	for _, interval := range normalized {
		for _, share := range interval.Shares {
			err := auth.AuthorizeWorkOrder(ctx, WorkOrderAuthorization{TenantID: req.TenantID, ActorID: req.ActorID, WorkerID: interval.WorkerID, ProjectID: share.ProjectID, WorkOrderID: share.WorkOrderID, LineID: share.LineID})
			if err != nil {
				return AllocationRevision{}, errors.Join(ErrUnauthorized, err)
			}
		}
	}
	sourceDigest, err := digestOf(struct {
		TenantID         string
		TimecardID       string
		ApprovedRevision uint64
		Intervals        []ApprovedInterval
	}{req.TenantID, req.TimecardID, req.ApprovedRevision, normalized})
	if err != nil {
		return AllocationRevision{}, err
	}
	key := req.TenantID + "\x00" + req.TimecardID
	l.mu.Lock()
	defer l.mu.Unlock()
	requestKey := allocationKey{tenant: req.TenantID, timecard: req.TimecardID, actor: req.ActorID, idempotency: req.IdempotencyKey}
	if old, ok := l.allocations[requestKey]; ok {
		if old.digest != sourceDigest {
			return AllocationRevision{}, ErrIdempotencyConflict
		}
		return cloneRevision(old.record), nil
	}
	if current, ok := l.current[key]; ok {
		if req.ApprovedRevision < current.ApprovedRevision {
			return AllocationRevision{}, ErrStaleRevision
		}
		if req.ApprovedRevision == current.ApprovedRevision {
			if current.SourceDigest == sourceDigest {
				return cloneRevision(current), nil
			}
			return AllocationRevision{}, ErrRevisionConflict
		}
	}
	previous := l.current[key]
	revision := previous.Revision + 1
	lines := flattenLines(req, normalized, revision)
	if len(lines) == 0 {
		return AllocationRevision{}, fmt.Errorf("%w: no work-order lines", ErrInvalid)
	}
	deltas := calculateDeltas(previous.Lines, lines)
	allocated := int64(0)
	for _, line := range lines {
		allocated += line.Minutes
	}
	record := AllocationRevision{TenantID: req.TenantID, TimecardID: req.TimecardID, ApprovedRevision: req.ApprovedRevision, Revision: revision, PreviousRevision: previous.Revision, SourceMinutes: sourceMinutes(normalized), AllocatedMinutes: allocated, Lines: lines, Deltas: deltas, SourceDigest: sourceDigest}
	record.Digest, err = digestOf(recordWithoutDigest(record))
	if err != nil {
		return AllocationRevision{}, err
	}
	l.current[key] = cloneRevision(record)
	l.allocations[requestKey] = storedAllocation{digest: sourceDigest, record: cloneRevision(record)}
	return cloneRevision(record), nil
}

func (l *Ledger) Current(tenant, timecard string) (AllocationRevision, error) {
	if l == nil || strings.TrimSpace(tenant) == "" || strings.TrimSpace(timecard) == "" {
		return AllocationRevision{}, ErrInvalid
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	record, ok := l.current[tenant+"\x00"+timecard]
	if !ok {
		return AllocationRevision{}, ErrNotFound
	}
	return cloneRevision(record), nil
}

func (l *Ledger) Reconcile(tenant, timecard string) (ReconciliationReport, error) {
	record, err := l.Current(tenant, timecard)
	if err != nil {
		return ReconciliationReport{}, err
	}
	if err := record.Validate(); err != nil {
		return ReconciliationReport{}, err
	}
	draft := buildBillingDraft(record)
	return ReconciliationReport{TenantID: record.TenantID, TimecardID: record.TimecardID, ApprovedRevision: record.ApprovedRevision, AllocationRevision: record.Revision, SourceMinutes: record.SourceMinutes, AllocatedMinutes: record.AllocatedMinutes, Balanced: true, Deltas: append([]AllocationDelta(nil), record.Deltas...), BillingDraft: draft}, nil
}

type HandoffRequest struct {
	TenantID           string
	ActorID            string
	TimecardID         string
	ApprovedRevision   uint64
	AllocationRevision uint64
	IdempotencyKey     string
	Authority          AuthorityDecision
}

func (l *Ledger) ExportPayroll(ctx context.Context, verifier PayrollAuthority, req HandoffRequest) (HandoffResult, error) {
	if l == nil || verifier == nil {
		return HandoffResult{}, ErrUnavailable
	}
	if strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.ActorID) == "" || strings.TrimSpace(req.TimecardID) == "" || strings.TrimSpace(req.IdempotencyKey) == "" || req.ApprovedRevision == 0 || req.AllocationRevision == 0 {
		return HandoffResult{}, ErrInvalid
	}
	if err := req.Authority.validate(req.TenantID, req.TimecardID, req.ApprovedRevision); err != nil {
		return HandoffResult{}, err
	}
	if err := verifier.VerifyPayrollAuthority(ctx, req.Authority); err != nil {
		return HandoffResult{}, errors.Join(ErrAuthorityRequired, err)
	}
	record, err := l.Current(req.TenantID, req.TimecardID)
	if err != nil {
		return HandoffResult{}, err
	}
	if record.ApprovedRevision != req.ApprovedRevision || record.Revision != req.AllocationRevision {
		return HandoffResult{}, ErrRevisionConflict
	}
	if err := record.Validate(); err != nil {
		return HandoffResult{}, err
	}
	key := handoffKey{tenant: req.TenantID, timecard: req.TimecardID, actor: req.ActorID, idempotency: req.IdempotencyKey}
	digest, err := digestOf(struct {
		RecordDigest string
		Authority    AuthorityDecision
	}{record.Digest, req.Authority})
	if err != nil {
		return HandoffResult{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if old, ok := l.requests[key]; ok {
		if old.digest != digest {
			return HandoffResult{}, ErrIdempotencyConflict
		}
		if old.result.Receipt.Status == ReceiptRejected {
			return old.result, ErrPayrollRejected
		}
		return old.result, nil
	}
	receipt := HandoffReceipt{ReceiptID: "payroll-handoff/" + digest, TenantID: req.TenantID, TimecardID: req.TimecardID, ApprovedRevision: req.ApprovedRevision, AllocationRevision: req.AllocationRevision, AuthorityDecisionID: req.Authority.DecisionID}
	if req.Authority.Outcome == AuthorityRejected {
		receipt.Status = ReceiptRejected
		receipt.Reason = req.Authority.Reason
		result := HandoffResult{Receipt: receipt}
		l.requests[key] = storedHandoff{digest: digest, result: result}
		return result, ErrPayrollRejected
	}
	export := buildPayrollExport(record)
	receipt.Status = ReceiptAccepted
	receipt.ExportDigest = export.Digest
	result := HandoffResult{Export: export, Receipt: receipt}
	l.requests[key] = storedHandoff{digest: digest, result: result}
	return result, nil
}

func validateRequest(req AllocationRequest) error {
	if strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.ActorID) == "" || strings.TrimSpace(req.TimecardID) == "" || strings.TrimSpace(req.IdempotencyKey) == "" || req.ApprovedRevision == 0 {
		return ErrInvalid
	}
	if len(req.Intervals) == 0 {
		return fmt.Errorf("%w: at least one approved interval is required", ErrInvalid)
	}
	seen := make(map[string]struct{}, len(req.Intervals))
	for _, interval := range req.Intervals {
		if strings.TrimSpace(interval.ID) == "" || strings.TrimSpace(interval.TimecardID) == "" || interval.TimecardID != req.TimecardID || interval.ApprovedRevision != req.ApprovedRevision || strings.TrimSpace(interval.WorkerID) == "" || strings.TrimSpace(interval.SourceRef) == "" || interval.Minutes <= 0 {
			return ErrInvalid
		}
		if _, ok := seen[interval.ID]; ok {
			return fmt.Errorf("%w: duplicate source interval %q", ErrInvalid, interval.ID)
		}
		seen[interval.ID] = struct{}{}
		if len(interval.Shares) == 0 {
			return fmt.Errorf("%w: interval %q has no work-order shares", ErrInvalid, interval.ID)
		}
		var total int64
		shareSeen := make(map[string]struct{}, len(interval.Shares))
		for _, share := range interval.Shares {
			if strings.TrimSpace(share.ProjectID) == "" || strings.TrimSpace(share.WorkOrderID) == "" || strings.TrimSpace(share.LineID) == "" || share.Minutes <= 0 {
				return ErrInvalid
			}
			shareKey := share.ProjectID + "\x00" + share.WorkOrderID + "\x00" + share.LineID
			if _, ok := shareSeen[shareKey]; ok {
				return fmt.Errorf("%w: duplicate work-order share in interval %q", ErrInvalid, interval.ID)
			}
			shareSeen[shareKey] = struct{}{}
			total += share.Minutes
		}
		if total != interval.Minutes {
			return fmt.Errorf("%w: interval %q has %d shared minutes, want %d", ErrUnbalanced, interval.ID, total, interval.Minutes)
		}
	}
	return nil
}

func normalizeIntervals(in []ApprovedInterval) []ApprovedInterval {
	out := make([]ApprovedInterval, len(in))
	for i := range in {
		out[i] = in[i]
		out[i].Shares = append([]WorkOrderShare(nil), in[i].Shares...)
		sort.Slice(out[i].Shares, func(a, b int) bool { return workOrderShareKey(out[i].Shares[a]) < workOrderShareKey(out[i].Shares[b]) })
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out
}

func flattenLines(req AllocationRequest, intervals []ApprovedInterval, revision uint64) []AllocationLine {
	lines := make([]AllocationLine, 0)
	for _, interval := range intervals {
		for _, share := range interval.Shares {
			lines = append(lines, AllocationLine{TenantID: req.TenantID, TimecardID: req.TimecardID, ApprovedRevision: req.ApprovedRevision, Revision: revision, IntervalID: interval.ID, WorkerID: interval.WorkerID, ProjectID: share.ProjectID, WorkOrderID: share.WorkOrderID, LineID: share.LineID, SourceRef: interval.SourceRef, Minutes: share.Minutes})
		}
	}
	sort.Slice(lines, func(a, b int) bool { return allocationLineKey(lines[a]) < allocationLineKey(lines[b]) })
	return lines
}

func calculateDeltas(previous, current []AllocationLine) []AllocationDelta {
	amounts := make(map[string]AllocationDelta, len(previous)+len(current))
	for _, line := range previous {
		key := allocationLineKey(line)
		amounts[key] = AllocationDelta{IntervalID: line.IntervalID, ProjectID: line.ProjectID, WorkOrderID: line.WorkOrderID, LineID: line.LineID, WorkerID: line.WorkerID, SourceRef: line.SourceRef, Minutes: -line.Minutes}
	}
	for _, line := range current {
		key := allocationLineKey(line)
		delta := amounts[key]
		if delta.IntervalID == "" {
			delta = AllocationDelta{IntervalID: line.IntervalID, ProjectID: line.ProjectID, WorkOrderID: line.WorkOrderID, LineID: line.LineID, WorkerID: line.WorkerID, SourceRef: line.SourceRef}
		}
		delta.Minutes += line.Minutes
		amounts[key] = delta
	}
	out := make([]AllocationDelta, 0, len(amounts))
	for _, delta := range amounts {
		if delta.Minutes != 0 {
			out = append(out, delta)
		}
	}
	sort.Slice(out, func(a, b int) bool { return allocationDeltaKey(out[a]) < allocationDeltaKey(out[b]) })
	return out
}

func sourceMinutes(intervals []ApprovedInterval) int64 {
	var total int64
	for _, interval := range intervals {
		total += interval.Minutes
	}
	return total
}

func buildBillingDraft(record AllocationRevision) BillingDraft {
	type aggregate struct{ line BillingDraftLine }
	byKey := make(map[string]*aggregate)
	for _, line := range record.Lines {
		key := line.ProjectID + "\x00" + line.WorkOrderID + "\x00" + line.LineID
		a := byKey[key]
		if a == nil {
			a = &aggregate{line: BillingDraftLine{ProjectID: line.ProjectID, WorkOrderID: line.WorkOrderID, LineID: line.LineID}}
			byKey[key] = a
		}
		a.line.Minutes += line.Minutes
		if !contains(a.line.SourceRefs, line.SourceRef) {
			a.line.SourceRefs = append(a.line.SourceRefs, line.SourceRef)
		}
	}
	for _, delta := range record.Deltas {
		key := delta.ProjectID + "\x00" + delta.WorkOrderID + "\x00" + delta.LineID
		a := byKey[key]
		if a == nil {
			a = &aggregate{line: BillingDraftLine{ProjectID: delta.ProjectID, WorkOrderID: delta.WorkOrderID, LineID: delta.LineID}}
			byKey[key] = a
		}
		a.line.DeltaMinutes += delta.Minutes
	}
	lines := make([]BillingDraftLine, 0, len(byKey))
	for _, a := range byKey {
		sort.Strings(a.line.SourceRefs)
		lines = append(lines, a.line)
	}
	sort.Slice(lines, func(a, b int) bool { return billingLineKey(lines[a]) < billingLineKey(lines[b]) })
	return BillingDraft{TenantID: record.TenantID, TimecardID: record.TimecardID, ApprovedRevision: record.ApprovedRevision, AllocationRevision: record.Revision, Lines: lines}
}

func buildPayrollExport(record AllocationRevision) PayrollExport {
	lines := make([]PayrollLine, 0, len(record.Lines))
	for _, line := range record.Lines {
		lines = append(lines, PayrollLine{TimecardID: line.TimecardID, ApprovedRevision: line.ApprovedRevision, IntervalID: line.IntervalID, WorkerID: line.WorkerID, ProjectID: line.ProjectID, WorkOrderID: line.WorkOrderID, LineID: line.LineID, SourceRef: line.SourceRef, Minutes: line.Minutes})
	}
	export := PayrollExport{TenantID: record.TenantID, TimecardID: record.TimecardID, ApprovedRevision: record.ApprovedRevision, AllocationRevision: record.Revision, Lines: lines}
	export.Digest, _ = digestOf(exportWithoutDigest(export))
	return export
}

func cloneRevision(in AllocationRevision) AllocationRevision {
	out := in
	out.Lines = append([]AllocationLine(nil), in.Lines...)
	out.Deltas = append([]AllocationDelta(nil), in.Deltas...)
	return out
}

func recordWithoutDigest(in AllocationRevision) any { in.Digest = ""; return in }
func exportWithoutDigest(in PayrollExport) any      { in.Digest = ""; return in }

func digestOf(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func allocationLineKey(line AllocationLine) string {
	return line.IntervalID + "\x00" + line.ProjectID + "\x00" + line.WorkOrderID + "\x00" + line.LineID + "\x00" + line.WorkerID + "\x00" + line.SourceRef
}
func allocationDeltaKey(line AllocationDelta) string {
	return line.IntervalID + "\x00" + line.ProjectID + "\x00" + line.WorkOrderID + "\x00" + line.LineID + "\x00" + line.WorkerID + "\x00" + line.SourceRef
}
func workOrderShareKey(share WorkOrderShare) string {
	return share.ProjectID + "\x00" + share.WorkOrderID + "\x00" + share.LineID
}
func billingLineKey(line BillingDraftLine) string {
	return line.ProjectID + "\x00" + line.WorkOrderID + "\x00" + line.LineID
}
func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
