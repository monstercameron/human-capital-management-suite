package intent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// GovernedWriteType is the deliberately small set of external writes. A
// caller cannot select a generic aggregate update through this service.
type GovernedWriteType string

const (
	WriteHire               GovernedWriteType = "hire"
	WriteAssignmentChange   GovernedWriteType = "assignment_change"
	WriteTermination        GovernedWriteType = "termination"
	WriteOrganizationChange GovernedWriteType = "organization_change"
	WriteLeaveRequest       GovernedWriteType = "leave_request"
	WriteAccessGrant        GovernedWriteType = "access_grant"
	WritePromotion          GovernedWriteType = "promotion"
	WriteBasePay            GovernedWriteType = "base_pay"
	WriteBudgetHold         GovernedWriteType = "budget_hold"
)

var (
	ErrInvalidWriteRequest = errors.New("intent: invalid governed write request")
	ErrUnsupportedWrite    = errors.New("intent: governed write type is not exposed")
	ErrBatchLimit          = errors.New("intent: batch exceeds declared maximum")
	ErrWriteNotFound       = errors.New("intent: write intent not found")
	ErrWriteConflict       = errors.New("intent: idempotency key conflicts")
)

// WriteStatus is the resource status returned by the management surface.
type WriteStatus string

const (
	WriteCreated    WriteStatus = "CREATED"
	WriteSubmitted  WriteStatus = "SUBMITTED"
	WriteCancelled  WriteStatus = "CANCELLED"
	WriteSuperseded WriteStatus = "SUPERSEDED"
)

// WriteRequest contains an opaque, already-typed request digest. The service
// records an intent envelope and never mutates a governed fact directly.
type WriteRequest struct {
	TenantID       string
	IntentType     GovernedWriteType
	ResourceID     string
	SubjectID      string
	PayloadDigest  string
	RequestedBy    string
	IdempotencyKey string
}

// WriteAuthorizer is evaluated before an intent is recorded.
type WriteAuthorizer interface {
	AuthorizeWrite(context.Context, WriteRequest) error
}

type IntentWrite struct {
	IntentID       string
	TenantID       string
	IntentType     GovernedWriteType
	ResourceID     string
	SubjectID      string
	PayloadDigest  string
	RequestedBy    string
	IdempotencyKey string
	Status         WriteStatus
	Revision       uint64
	CreatedAt      time.Time
}

type BatchWriteRequest struct {
	Items    []WriteRequest
	MaxItems int
	Submit   bool
}

type BatchWriteResult struct {
	Index  int
	Intent IntentWrite
	Error  string
}

type RescindRequest struct {
	IntentID    string
	RequestedBy string
	Action      WriteStatus
}

// WriteService owns its registry; no package-level mutable state is used.
type WriteService struct {
	mu         sync.RWMutex
	authorizer WriteAuthorizer
	now        func() time.Time
	byID       map[string]IntentWrite
	byKey      map[string]string
}

func NewWriteService(authorizer WriteAuthorizer, now func() time.Time) *WriteService {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &WriteService{authorizer: authorizer, now: now, byID: make(map[string]IntentWrite), byKey: make(map[string]string)}
}

func (s *WriteService) Create(ctx context.Context, request WriteRequest, submit bool) (IntentWrite, error) {
	if err := validateWriteRequest(request); err != nil {
		return IntentWrite{}, err
	}
	if !exposedWriteType(request.IntentType) {
		return IntentWrite{}, fmt.Errorf("%w: %s", ErrUnsupportedWrite, request.IntentType)
	}
	if s == nil {
		return IntentWrite{}, ErrInvalidWriteRequest
	}
	if s.authorizer != nil {
		if err := s.authorizer.AuthorizeWrite(ctx, request); err != nil {
			return IntentWrite{}, err
		}
	}
	key := request.TenantID + "\x00" + request.IdempotencyKey
	digest := writeRequestDigest(request)
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.byKey[key]; ok {
		current := s.byID[id]
		if writeRequestDigest(WriteRequest{TenantID: current.TenantID, IntentType: current.IntentType, ResourceID: current.ResourceID, SubjectID: current.SubjectID, PayloadDigest: current.PayloadDigest, RequestedBy: current.RequestedBy, IdempotencyKey: current.IdempotencyKey}) != digest {
			return IntentWrite{}, ErrWriteConflict
		}
		return current, nil
	}
	now := s.now().UTC()
	status := WriteCreated
	if submit {
		status = WriteSubmitted
	}
	id := "intent-" + digest[:24]
	resource := IntentWrite{IntentID: id, TenantID: request.TenantID, IntentType: request.IntentType, ResourceID: request.ResourceID, SubjectID: request.SubjectID, PayloadDigest: request.PayloadDigest, RequestedBy: request.RequestedBy, IdempotencyKey: request.IdempotencyKey, Status: status, Revision: 1, CreatedAt: now}
	s.byID[id], s.byKey[key] = resource, id
	return resource, nil
}

func (s *WriteService) Submit(ctx context.Context, id, requestedBy string) (IntentWrite, error) {
	return s.transition(ctx, id, requestedBy, WriteSubmitted)
}

func (s *WriteService) Rescind(ctx context.Context, request RescindRequest) (IntentWrite, error) {
	if request.Action != WriteCancelled && request.Action != WriteSuperseded {
		return IntentWrite{}, ErrInvalidWriteRequest
	}
	return s.transition(ctx, request.IntentID, request.RequestedBy, request.Action)
}

func (s *WriteService) SubmitBatch(ctx context.Context, request BatchWriteRequest) []BatchWriteResult {
	results := make([]BatchWriteResult, len(request.Items))
	for i, item := range request.Items {
		results[i].Index = i
		if request.MaxItems <= 0 || len(request.Items) > request.MaxItems {
			results[i].Error = ErrBatchLimit.Error()
			continue
		}
		resource, err := s.Create(ctx, item, request.Submit)
		if err != nil {
			results[i].Error = err.Error()
			continue
		}
		results[i].Intent = resource
	}
	return results
}

func (s *WriteService) Get(id string) (IntentWrite, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	resource, ok := s.byID[id]
	if !ok {
		return IntentWrite{}, ErrWriteNotFound
	}
	return resource, nil
}

func (s *WriteService) transition(ctx context.Context, id, requestedBy string, status WriteStatus) (IntentWrite, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(requestedBy) == "" {
		return IntentWrite{}, ErrInvalidWriteRequest
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	resource, ok := s.byID[id]
	if !ok {
		return IntentWrite{}, ErrWriteNotFound
	}
	if resource.RequestedBy != requestedBy {
		return IntentWrite{}, ErrWriteConflict
	}
	if resource.Status == WriteCancelled || resource.Status == WriteSuperseded {
		return resource, nil
	}
	resource.Status, resource.Revision = status, resource.Revision+1
	s.byID[id] = resource
	_ = ctx
	return resource, nil
}

func validateWriteRequest(request WriteRequest) error {
	for name, value := range map[string]string{"tenant_id": request.TenantID, "resource_id": request.ResourceID, "subject_id": request.SubjectID, "payload_digest": request.PayloadDigest, "requested_by": request.RequestedBy, "idempotency_key": request.IdempotencyKey} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("%w: %s is required and may not be padded", ErrInvalidWriteRequest, name)
		}
	}
	return nil
}

func exposedWriteType(kind GovernedWriteType) bool {
	switch kind {
	case WriteHire, WriteAssignmentChange, WriteTermination, WriteOrganizationChange, WriteLeaveRequest, WriteAccessGrant, WritePromotion, WriteBasePay, WriteBudgetHold:
		return true
	default:
		return false
	}
}

func writeRequestDigest(request WriteRequest) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{string(request.IntentType), request.TenantID, request.ResourceID, request.SubjectID, request.PayloadDigest, request.RequestedBy, request.IdempotencyKey}, "\x00")))
	return hex.EncodeToString(sum[:])
}
