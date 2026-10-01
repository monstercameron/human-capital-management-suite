package clockservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	clockdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timesession"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const maxPunchBatch = 200

// ErrBatchIdempotencyConflict means a device sequence was previously
// acknowledged for different immutable input.
var ErrBatchIdempotencyConflict = errors.New("clockservice: batch idempotency conflict")

// BatchPunch is one buffered device event. WorkerCredentialRef is resolved
// by the server-side worker directory; it is not a worker id assertion.
type BatchPunch struct {
	DeviceSequence        int64
	EventType             timesession.PunchKind
	WorkerCredentialRef   string
	AssignmentRef         string
	JobRef                string
	CostCodeRef           string
	Travel                bool
	DeviceOccurredAt      time.Time
	DeviceClockOffset     time.Duration
	Method                timesession.IdentificationMethod
	Source                string
	PhotoAttestationRef   string
	IdempotencyKey        string
	Scheduled             bool
	resolvedWorkerRef     string
	resolvedAssignmentRef string
}

// BatchRequest is the authenticated device submission. Tenant and device
// identity come from the trusted principal and enrolled-device lookup.
type BatchRequest struct {
	DeviceID string
	Punches  []BatchPunch
}

// BatchReceiptStatus is the per-punch result vocabulary.
type BatchReceiptStatus string

const (
	BatchAccepted  BatchReceiptStatus = "ACCEPTED"
	BatchRejected  BatchReceiptStatus = "REJECTED"
	BatchHeld      BatchReceiptStatus = "HELD"
	BatchDuplicate BatchReceiptStatus = "DUPLICATE"
)

// BatchReceipt is immutable acknowledgement for one device sequence.
type BatchReceipt struct {
	DeviceSequence     int64
	Status             BatchReceiptStatus
	Reason             string
	ObservationID      string
	Original           *ReceiptRecord
	OriginalReceiptRef string
	Workflow           *WorkflowReceiptBinding
}

// BatchResponse reports every row and the highest contiguous durable cursor.
type BatchResponse struct {
	Receipts          []BatchReceipt
	HighestContiguous int64
	SequenceGap       bool
}

// BatchCommitEntry is the atomic unit supplied to a BatchIngestStore. Invalid
// rows have Work nil and are still persisted as immutable receipts.
type BatchCommitEntry struct {
	Sequence int64
	Receipt  ReceiptRecord
	Work     *PunchWork
}

// BatchCommitResult is returned after one transaction commits all entries.
type BatchCommitResult struct {
	Receipts          []ReceiptRecord
	HighestContiguous int64
	Results           []PunchResult
	Duplicates        map[int64]bool
	WorkflowBindings  []WorkflowReceiptBinding
}

// BatchIngestStore commits receipts, observations, session transitions and
// outbox rows in one transaction. Implementations must return the original
// receipt for an exact replay and ErrIdempotencyConflict for changed data.
type BatchIngestStore interface {
	CommitPunchBatch(context.Context, string, string, []BatchCommitEntry) (BatchCommitResult, error)
}

// BatchReceiptLookup returns the immutable receipt for a previously committed
// device sequence, before worker and session resolution is attempted.
type BatchReceiptLookup interface {
	LookupPunchReceipt(context.Context, string, string, int64) (ReceiptRecord, bool, error)
}

// SubmitPunches authenticates the enrolled machine, resolves each worker from
// the authoritative credential directory and prepares an atomic batch.
func (s Service) SubmitPunches(ctx context.Context, p *trust.Principal, req BatchRequest) (BatchResponse, error) {
	if len(req.Punches) == 0 || len(req.Punches) > maxPunchBatch {
		return BatchResponse{}, reject(ErrInvalidRequest, "punches", "BOUNDS", "batch must contain between one and 200 punches")
	}
	device, err := s.authenticatedRosterDevice(ctx, p, req.DeviceID)
	if err != nil {
		return BatchResponse{}, err
	}
	store, ok := s.Work.(BatchIngestStore)
	if !ok || s.Workers == nil || s.Roster == nil || s.IDs == nil || s.Auth == nil || s.Sessions == nil {
		return BatchResponse{}, ErrUnavailable
	}
	var roster map[string]struct{}
	entries := make([]BatchCommitEntry, 0, len(req.Punches))
	pending := make(map[string]timesession.Session, len(req.Punches))
	lookup, _ := store.(BatchReceiptLookup)
	replayBindings := make(map[int64]WorkflowReceiptBinding)
	seen := make(map[int64]struct{}, len(req.Punches))
	last := int64(0)
	for i := range req.Punches {
		in := req.Punches[i]
		if in.DeviceSequence <= last || in.DeviceSequence <= 0 {
			return BatchResponse{}, reject(ErrInvalidRequest, "device_sequence", "ORDER", "device sequences must be strictly increasing")
		}
		last = in.DeviceSequence
		if _, exists := seen[in.DeviceSequence]; exists {
			return BatchResponse{}, reject(ErrInvalidRequest, "device_sequence", "DUPLICATE", "device sequence repeats in batch")
		}
		seen[in.DeviceSequence] = struct{}{}
		if lookup != nil {
			original, found, lookupErr := lookup.LookupPunchReceipt(ctx, tenantOf(p), device.ID, in.DeviceSequence)
			if lookupErr != nil {
				return BatchResponse{}, lookupErr
			}
			if found {
				if receiptInputDigest(original.Payload) != batchInputDigest(tenantOf(p), device.ID, in) {
					return BatchResponse{}, ErrBatchIdempotencyConflict
				}
				if original.Status == string(BatchAccepted) {
					workflowLookup, lookupOK := s.BatchWorkflow.(WorkflowReceiptLookup)
					if !lookupOK {
						return BatchResponse{}, ErrUnavailable
					}
					binding, bindingFound, bindingErr := workflowLookup.LookupWorkflowReceipt(ctx, tenantOf(p), device.ID, in.DeviceSequence)
					if bindingErr != nil {
						return BatchResponse{}, bindingErr
					}
					if !bindingFound || !validWorkflowBinding(binding, tenantOf(p)) || binding.ObservationID != original.ObservationID {
						return BatchResponse{}, ErrUnavailable
					}
					replayBindings[in.DeviceSequence] = binding
				}
				entries = append(entries, BatchCommitEntry{Sequence: in.DeviceSequence, Receipt: original})
				continue
			}
		}
		if roster == nil {
			roster, err = s.batchRoster(ctx, device)
			if err != nil {
				return BatchResponse{}, err
			}
		}
		entry, prepErr := s.prepareBatchEntry(ctx, p, device, roster, pending, in)
		if prepErr != nil {
			entry.Receipt.Status = string(BatchRejected)
			entry.Receipt.Reason = typedBatchReason(prepErr)
		}
		entries = append(entries, entry)
	}
	committed, err := s.commitBatchThroughWorkflow(ctx, store, tenantOf(p), device.ID, entries)
	if err != nil {
		return BatchResponse{}, err
	}
	bySequence := make(map[int64]struct{}, len(committed.Receipts))
	for _, receipt := range committed.Receipts {
		if _, duplicate := bySequence[receipt.DeviceSequence]; duplicate {
			return BatchResponse{}, ErrUnavailable
		}
		bySequence[receipt.DeviceSequence] = struct{}{}
	}
	if len(bySequence) != len(entries) {
		return BatchResponse{}, ErrUnavailable
	}
	for _, binding := range replayBindings {
		committed.WorkflowBindings = append(committed.WorkflowBindings, binding)
	}
	for _, entry := range entries {
		if _, ok := bySequence[entry.Sequence]; !ok {
			return BatchResponse{}, ErrUnavailable
		}
	}
	return batchResponse(committed, entries), nil
}

func (s Service) commitBatchThroughWorkflow(ctx context.Context, store BatchIngestStore, tenant, device string, entries []BatchCommitEntry) (BatchCommitResult, error) {
	hasWork := false
	for _, entry := range entries {
		if entry.Work != nil {
			hasWork = true
			break
		}
	}
	if !hasWork {
		return store.CommitPunchBatch(ctx, tenant, device, entries)
	}
	if s.BatchWorkflow == nil {
		return BatchCommitResult{}, ErrUnavailable
	}
	result, err := s.BatchWorkflow.ExecutePunchBatch(ctx, tenant, device, entries)
	if err != nil {
		return BatchCommitResult{}, err
	}
	if err := validateWorkflowBindings(tenant, entries, result); err != nil {
		return BatchCommitResult{}, err
	}
	return result, nil
}

func validateWorkflowBindings(tenant string, entries []BatchCommitEntry, result BatchCommitResult) error {
	needed := 0
	for _, entry := range entries {
		if entry.Work != nil {
			needed++
		}
	}
	if needed == 0 {
		return nil
	}
	bySequence := make(map[int64]WorkflowReceiptBinding, len(result.WorkflowBindings))
	for _, binding := range result.WorkflowBindings {
		if _, exists := bySequence[binding.DeviceSequence]; exists || !validWorkflowBinding(binding, tenant) {
			return ErrUnavailable
		}
		bySequence[binding.DeviceSequence] = binding
	}
	if len(bySequence) != needed {
		return ErrUnavailable
	}
	for _, entry := range entries {
		if entry.Work == nil {
			continue
		}
		binding, ok := bySequence[entry.Sequence]
		expectedNode := "commit_punch"
		if !entry.Work.SessionIsNew {
			expectedNode = "commit_clock_out"
		}
		if !ok || binding.NodeID != expectedNode || binding.ObservationID != entry.Work.Observation.ID || binding.SessionID != entry.Work.Session.ID {
			return ErrUnavailable
		}
	}
	return nil
}

func (s Service) batchRoster(ctx context.Context, device DeviceRecord) (map[string]struct{}, error) {
	workers := make(map[string]struct{})
	cursor := ""
	for page := 0; page < 100; page++ {
		delta, err := s.Roster.Delta(ctx, device.TenantID, device.SiteID, device.ProfileID, cursor, s.now())
		if err != nil {
			return nil, err
		}
		if err := validateRosterDelta(delta); err != nil {
			return nil, err
		}
		for _, w := range delta.Workers {
			if !w.Terminated {
				workers[w.WorkerRef] = struct{}{}
			}
		}
		if !delta.HasMore {
			return workers, nil
		}
		if delta.NextCursor == "" || delta.NextCursor == cursor {
			return nil, ErrInvalidRequest
		}
		cursor = delta.NextCursor
	}
	return nil, ErrInvalidRequest
}

func (s Service) prepareBatchEntry(ctx context.Context, p *trust.Principal, device DeviceRecord, roster map[string]struct{}, pending map[string]timesession.Session, in BatchPunch) (BatchCommitEntry, error) {
	digest := batchInputDigest(tenantOf(p), device.ID, in)
	receiptPayload, _ := json.Marshal(struct {
		InputDigest string `json:"input_digest"`
	}{digest})
	entry := BatchCommitEntry{Sequence: in.DeviceSequence, Receipt: ReceiptRecord{DeviceSequence: in.DeviceSequence, Status: string(BatchAccepted), Payload: receiptPayload}}
	if len(in.Source) == 0 || len(string(in.Method)) == 0 {
		return entry, reject(ErrInvalidRequest, "source", "MISSING", "source and identification method are required")
	}
	assignmentInput := in.AssignmentRef
	if in.resolvedAssignmentRef != "" {
		assignmentInput = in.resolvedAssignmentRef
	}
	if !in.EventType.Valid() || in.DeviceOccurredAt.IsZero() || !requireNonEmpty(in.WorkerCredentialRef, assignmentInput, in.IdempotencyKey, in.Source, string(in.Method)) {
		if !in.EventType.Valid() {
			return entry, reject(ErrInvalidRequest, "event_type", "INVALID", "event type is not declared")
		}
		return entry, reject(ErrInvalidRequest, "punch", "MISSING", "worker credential, assignment, idempotency key and occurred time are required")
	}
	worker := in.resolvedWorkerRef
	var err error
	if worker == "" {
		var active bool
		worker, active, err = s.Workers.ResolveWorker(ctx, tenantOf(p), in.WorkerCredentialRef)
		if err != nil {
			return entry, err
		}
		if !active || worker == "" {
			return entry, ErrWorkerNotEligible
		}
	}
	assignmentRef := in.AssignmentRef
	if in.resolvedAssignmentRef != "" {
		assignmentRef = in.resolvedAssignmentRef
	}
	if _, ok := roster[worker]; !ok {
		return entry, ErrWorkerNotOnRoster
	}
	project, _, ok, err := s.Workers.ResolveAssignment(ctx, tenantOf(p), worker, assignmentRef)
	if err != nil {
		return entry, err
	}
	if !ok {
		return entry, ErrAssignmentNotFound
	}
	delegated, err := s.Auth.AuthorizePunch(ctx, p, tenantOf(p), worker, assignmentRef)
	if err != nil {
		return entry, err
	}
	now := s.now()
	obsID := s.IDs.Deterministic(tenantOf(p), "observation", device.ID, in.IdempotencyKey)
	clockEvent, ok := observationEvent(in.EventType)
	if !ok {
		return entry, reject(ErrInvalidRequest, "event_type", "INVALID", "event type is not supported by the signed observation contract")
	}
	capture, err := clockdomain.AuthenticateCapture(clockdomain.CaptureRequest{
		Tenant:             values.TenantId(tenantOf(p)),
		CapturedAt:         now,
		DeviceRegistration: batchDeviceRegistration(device),
		DeviceProof: clockdomain.DeviceProof{
			DeviceID: device.ID, RegistrationVersion: fmt.Sprint(device.Revision),
			ProofRef: "key:" + device.ID, Fingerprint: batchDeviceFingerprint(device.PublicKey),
			IssuedAt: p.IssuedAt(), ExpiresAt: p.ExpiresAt(),
		},
		Principal:        p,
		ClaimedWorkerRef: worker,
		Worker: clockdomain.WorkerResolution{
			WorkerRef: worker, Tenant: values.TenantId(tenantOf(p)), Resolved: true, Active: true,
			ResolutionRef: "worker-resolution:" + worker, EvidenceRef: "credential:" + in.WorkerCredentialRef,
		},
		Location: clockdomain.LocationEvidence{
			LocationRef: device.SiteID, PolicyVersion: "device-location/v1", EvidenceRef: "device-location:" + device.ID, Allowed: true,
		},
	})
	if err != nil {
		return entry, err
	}
	observationRequest := clockdomain.ObservationRequest{
		Tenant: values.TenantId(tenantOf(p)), Capture: capture.Evidence, EventType: clockEvent,
		OccurredAt: in.DeviceOccurredAt, Timezone: device.Timezone, Location: capture.Evidence.Location,
		Signature: clockdomain.DeviceSignature{KeyRef: "key:" + device.ID, Algorithm: "ed25519"}, Now: now,
	}
	observationRequest.Signature.PayloadDigest = clockdomain.ObservationPayloadDigest(observationRequest)
	observed, err := clockdomain.CaptureObservation(observationRequest)
	if err != nil {
		return entry, err
	}
	if _, err := clockdomain.ReconcileDeviceBackend(nil, []clockdomain.DevicePunch{{
		Tenant: tenantOf(p), SourceID: device.ID, PunchID: obsID, DeviceSeq: in.DeviceSequence,
		OccurredAt: in.DeviceOccurredAt, PayloadDigest: observed.Digest, SignatureOK: true,
	}}, now); err != nil {
		return entry, err
	}
	source := timesession.PunchSource{Kind: in.Source, DeviceRef: device.ID, Method: in.Method}
	var base timesession.Session
	var expected uint64
	isNew := in.EventType == timesession.PunchIn
	sessionID := s.IDs.Deterministic(tenantOf(p), worker, assignmentRef, "session-in", in.IdempotencyKey)
	if !isNew {
		pendingKey := worker + "\x00" + assignmentRef
		if prior, ok := pending[pendingKey]; ok {
			base = prior
			sessionID = prior.SessionID
		} else {
			current, err := s.Sessions.CurrentSession(ctx, tenantOf(p), worker, assignmentRef)
			if err != nil {
				return entry, err
			}
			base, err = unmarshalSession(current.Payload)
			if err != nil {
				return entry, err
			}
			sessionID = current.ID
		}
		expected = base.Revision
	}
	outSession, outcome, err := timesession.Apply(base, timesession.Punch{Kind: in.EventType, Tenant: tenantOf(p), Worker: worker, Actor: p.Subject(), Delegated: delegated, Assignment: assignmentRef, SessionID: sessionID, IdempotencyKey: in.IdempotencyKey, DeviceTime: in.DeviceOccurredAt, ServerReceiptTime: now, Source: source, JobRef: in.JobRef, CostCodeRef: in.CostCodeRef, Travel: in.Travel, Scheduled: in.Scheduled, ExpectedRevision: expected})
	if err != nil {
		return entry, err
	}
	payload, err := marshalSession(outSession)
	if err != nil {
		return entry, err
	}
	pending[worker+"\x00"+assignmentRef] = outSession
	record := SessionRecord{ID: sessionID, TenantID: tenantOf(p), WorkerRef: worker, AssignmentRef: assignmentRef, Status: string(outSession.State), Source: in.Source, ProjectRef: project, Revision: outSession.Revision, Payload: payload, OpenedAt: outSession.Segments[0].Start}
	if outSession.State == timesession.StateClosed || outSession.State == timesession.StateAutoClosed {
		record.ClosedAt = outSession.Segments[len(outSession.Segments)-1].End
	}
	obsPayload, err := json.Marshal(struct {
		SessionPayload      json.RawMessage `json:"session"`
		DeviceClockOffset   time.Duration   `json:"device_clock_offset"`
		PhotoAttestationRef string          `json:"photo_attestation_ref,omitempty"`
		CaptureDigest       string          `json:"capture_digest"`
		ObservationDigest   string          `json:"observation_digest"`
		RecordedAt          time.Time       `json:"recorded_at"`
		SignatureKeyRef     string          `json:"signature_key_ref"`
	}{payload, in.DeviceClockOffset, in.PhotoAttestationRef, capture.Evidence.Digest, observed.Digest, observed.RecordedAt, observationRequest.Signature.KeyRef})
	if err != nil {
		return entry, err
	}
	obs := ObservationRecord{ID: obsID, TenantID: tenantOf(p), WorkerRef: worker, AssignmentRef: assignmentRef, DeviceRef: device.ID, Source: in.Source, EventType: string(clockEvent), ProjectRef: project, IdempotencyKey: in.IdempotencyKey, Digest: observed.Digest, OccurredAt: observed.OccurredAt, ReceivedAt: observed.RecordedAt, Payload: obsPayload, Timezone: device.Timezone}
	entry.Receipt.ObservationID = obsID
	entry.Work = &PunchWork{Session: record, SessionIsNew: isNew, ExpectedRevision: expected, SessionEvents: []SessionEvent{{Kind: string(outcome.Kind), ActorRef: p.Subject(), IdempotencyKey: in.IdempotencyKey, Digest: digest, Payload: payload}}, Observation: obs}
	return entry, nil
}

func observationEvent(kind timesession.PunchKind) (clockdomain.EventType, bool) {
	switch kind {
	case timesession.PunchIn:
		return clockdomain.EventClockIn, true
	case timesession.PunchOut:
		return clockdomain.EventClockOut, true
	case timesession.PunchBreakStart:
		return clockdomain.EventBreakStart, true
	case timesession.PunchBreakEnd:
		return clockdomain.EventBreakEnd, true
	case timesession.PunchMealStart:
		return clockdomain.EventMealStart, true
	case timesession.PunchMealEnd:
		return clockdomain.EventMealEnd, true
	case timesession.PunchTransfer:
		return clockdomain.EventJobTransfer, true
	default:
		return "", false
	}
}

func batchDeviceRegistration(device DeviceRecord) clockdomain.Registration {
	return clockdomain.Registration{
		ID: device.ID, Version: fmt.Sprint(device.Revision), State: clockdomain.Active,
		OwnerRef: "workforce", LocationRef: device.SiteID,
		ClockTrustPolicy: "clock-trust/v1", OfflinePolicy: "offline:bounded", ReplayPolicy: "replay:sequence",
		SignaturePolicy: "sig:ed25519", FirmwarePolicy: "firmware:current", CertificatePolicy: "cert:device",
		RetentionPolicy: "retention:time-punch",
	}
}

func batchDeviceFingerprint(publicKey []byte) string {
	digest := sha256.Sum256(publicKey)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func batchResponse(result BatchCommitResult, entries []BatchCommitEntry) BatchResponse {
	bySeq := make(map[int64]ReceiptRecord, len(result.Receipts))
	for _, r := range result.Receipts {
		bySeq[r.DeviceSequence] = r
	}
	out := BatchResponse{HighestContiguous: result.HighestContiguous}
	bindings := make(map[int64]WorkflowReceiptBinding, len(result.WorkflowBindings))
	for _, binding := range result.WorkflowBindings {
		bindings[binding.DeviceSequence] = binding
	}
	for _, e := range entries {
		r, ok := bySeq[e.Sequence]
		if !ok {
			r = e.Receipt
		}
		status := BatchReceiptStatus(r.Status)
		if status == "" {
			status = BatchRejected
		}
		duplicate := result.Duplicates != nil && result.Duplicates[e.Sequence]
		if duplicate {
			status = BatchDuplicate
		}
		originalRef := ""
		if duplicate {
			originalRef = r.ObservationID
		}
		var workflow *WorkflowReceiptBinding
		if binding, ok := bindings[e.Sequence]; ok {
			copy := binding
			workflow = &copy
		}
		out.Receipts = append(out.Receipts, BatchReceipt{DeviceSequence: r.DeviceSequence, Status: status, Reason: r.Reason, ObservationID: r.ObservationID, Original: &r, OriginalReceiptRef: originalRef, Workflow: workflow})
	}
	for _, e := range entries {
		if e.Sequence > out.HighestContiguous {
			out.SequenceGap = true
			break
		}
	}
	return out
}

func typedBatchReason(err error) string {
	switch {
	case errors.Is(err, clockdomain.ErrObservationRejected):
		return "CLOCK_003_REJECTED"
	case errors.Is(err, clockdomain.ErrDedupRejected):
		return "CLOCK_005_REJECTED"
	case errors.Is(err, ErrWorkerNotOnRoster):
		return "WORKER_NOT_ON_ROSTER"
	case errors.Is(err, ErrWorkerNotEligible):
		return "WORKER_NOT_ELIGIBLE"
	case errors.Is(err, ErrAssignmentNotFound):
		return "ASSIGNMENT_NOT_FOUND"
	case errors.Is(err, ErrInvalidRequest):
		return "INVALID_REQUEST"
	default:
		return strings.ToUpper(strings.ReplaceAll(fmt.Sprint(err), " ", "_"))
	}
}

type batchDigestInput struct {
	Tenant, DeviceID      string
	DeviceSequence        int64
	EventType             timesession.PunchKind
	WorkerCredentialRef   string
	AssignmentRef, JobRef string
	CostCodeRef           string
	Travel                bool
	DeviceOccurredAt      time.Time
	DeviceClockOffset     time.Duration
	Method                timesession.IdentificationMethod
	Source                string
	PhotoAttestationRef   string
	IdempotencyKey        string
	Scheduled             bool
}

func batchInputDigest(tenant, deviceID string, in BatchPunch) string {
	b, _ := json.Marshal(batchDigestInput{
		Tenant: tenant, DeviceID: deviceID, DeviceSequence: in.DeviceSequence,
		EventType: in.EventType, WorkerCredentialRef: in.WorkerCredentialRef,
		AssignmentRef: in.AssignmentRef, JobRef: in.JobRef, CostCodeRef: in.CostCodeRef,
		Travel: in.Travel, DeviceOccurredAt: in.DeviceOccurredAt.UTC(),
		DeviceClockOffset: in.DeviceClockOffset, Method: in.Method, Source: in.Source,
		PhotoAttestationRef: in.PhotoAttestationRef, IdempotencyKey: in.IdempotencyKey,
		Scheduled: in.Scheduled,
	})
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func receiptInputDigest(payload []byte) string {
	var value struct {
		InputDigest string `json:"input_digest"`
	}
	if json.Unmarshal(payload, &value) != nil {
		return ""
	}
	return value.InputDigest
}
