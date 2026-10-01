package workitem

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// RequestType is the deliberately small set of employee-help routes. A
// confidential HR request is still a request, but its projection is never a
// channel message or a tenant-wide announcement.
type RequestType string

const (
	RequestTypeHR             RequestType = "HR"
	RequestTypeIT             RequestType = "IT"
	RequestTypeTeam           RequestType = "TEAM"
	RequestTypeConfidentialHR RequestType = "CONFIDENTIAL_HR"
)

func (k RequestType) Valid() bool {
	switch k {
	case RequestTypeHR, RequestTypeIT, RequestTypeTeam, RequestTypeConfidentialHR:
		return true
	default:
		return false
	}
}

type RequestStatus string

const (
	RequestStatusOpen       RequestStatus = "OPEN"
	RequestStatusInProgress RequestStatus = "IN_PROGRESS"
	RequestStatusWaiting    RequestStatus = "WAITING"
	RequestStatusResolved   RequestStatus = "RESOLVED"
	RequestStatusClosed     RequestStatus = "CLOSED"
)

func (s RequestStatus) Valid() bool {
	switch s {
	case RequestStatusOpen, RequestStatusInProgress, RequestStatusWaiting, RequestStatusResolved, RequestStatusClosed:
		return true
	default:
		return false
	}
}

type PrivateReply struct {
	ReplyID  uuid.UUID
	AuthorID string
	Body     string
	At       time.Time
}

type AuthorizedDocumentLink struct {
	TenantID            uuid.UUID
	DocumentVersionID   string
	DocumentDigest      string
	AuthorizedViewerIDs []string
}

type EmployeeRequest struct {
	TenantID    uuid.UUID
	RequestID   uuid.UUID
	RequesterID string
	OwnerID     string
	Type        RequestType
	Subject     string
	Details     string
	Status      RequestStatus
	DueAt       time.Time
	CreatedAt   time.Time
	Replies     []PrivateReply
	Documents   []AuthorizedDocumentLink
}

func (r EmployeeRequest) Validate() error {
	switch {
	case r.TenantID == uuid.Nil:
		return refuse(CodeInvalidRecord, r.RequestID.String(), "tenant id is required")
	case r.RequestID == uuid.Nil:
		return refuse(CodeInvalidRecord, r.RequestID.String(), "request id is required")
	case strings.TrimSpace(r.RequesterID) == "" || strings.TrimSpace(r.OwnerID) == "":
		return refuse(CodeInvalidRecord, r.RequestID.String(), "requester and owner are required")
	case !r.Type.Valid():
		return refuse(CodeInvalidRecord, r.RequestID.String(), "request type %q is not declared", r.Type)
	case strings.TrimSpace(r.Subject) == "" || strings.TrimSpace(r.Details) == "":
		return refuse(CodeInvalidRecord, r.RequestID.String(), "request subject and details are required")
	case !r.Status.Valid():
		return refuse(CodeInvalidRecord, r.RequestID.String(), "request status %q is not declared", r.Status)
	case r.DueAt.IsZero() || r.CreatedAt.IsZero():
		return refuse(CodeInvalidRecord, r.RequestID.String(), "request due and created times are required")
	}
	if r.Type == RequestTypeConfidentialHR && r.RequesterID == r.OwnerID {
		return refuse(CodeInvalidRecord, r.RequestID.String(), "confidential HR requests require a separate owner")
	}
	return nil
}

// RequestInbox is a value-owned experience adapter. It deliberately stores
// only request state and delegates durable WorkItem persistence to the caller;
// it does not create a second workflow engine.
type RequestInbox struct {
	mu    sync.RWMutex
	items map[uuid.UUID]EmployeeRequest
	byKey map[string]uuid.UUID
}

func (s *RequestInbox) Create(_ context.Context, request EmployeeRequest, idempotencyKey string) (EmployeeRequest, error) {
	if request.RequestID == uuid.Nil {
		request.RequestID = uuid.New()
	}
	if request.Status == "" {
		request.Status = RequestStatusOpen
	}
	if err := request.Validate(); err != nil {
		return EmployeeRequest{}, err
	}
	key := request.TenantID.String() + "\x00" + strings.TrimSpace(idempotencyKey)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items == nil {
		s.items = make(map[uuid.UUID]EmployeeRequest)
		s.byKey = make(map[string]uuid.UUID)
	}
	if strings.TrimSpace(idempotencyKey) != "" {
		if id, ok := s.byKey[key]; ok {
			prior := s.items[id]
			if !sameRequestIdentity(prior, request) {
				return EmployeeRequest{}, refuse(CodeInvalidRecord, request.RequestID.String(), "idempotency key was reused for another request")
			}
			return cloneEmployeeRequest(prior), nil
		}
	}
	if _, exists := s.items[request.RequestID]; exists {
		return EmployeeRequest{}, refuse(CodeInvalidRecord, request.RequestID.String(), "request id already exists")
	}
	request.Replies = nil
	request.Documents = nil
	s.items[request.RequestID] = cloneEmployeeRequest(request)
	if strings.TrimSpace(idempotencyKey) != "" {
		s.byKey[key] = request.RequestID
	}
	return cloneEmployeeRequest(request), nil
}

func (s *RequestInbox) Reply(_ context.Context, tenantID, requestID uuid.UUID, actorID, body string, at time.Time) (EmployeeRequest, error) {
	if strings.TrimSpace(actorID) == "" || strings.TrimSpace(body) == "" || at.IsZero() {
		return EmployeeRequest{}, refuse(CodeInvalidRecord, requestID.String(), "private reply requires actor, body and time")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	request, ok := s.items[requestID]
	if !ok || request.TenantID != tenantID || (request.RequesterID != actorID && request.OwnerID != actorID) {
		return EmployeeRequest{}, refuse(CodeWorkItemNotFound, requestID.String(), "request is not visible to actor")
	}
	request.Replies = append(request.Replies, PrivateReply{ReplyID: uuid.New(), AuthorID: actorID, Body: body, At: at.UTC()})
	s.items[requestID] = cloneEmployeeRequest(request)
	return cloneEmployeeRequest(request), nil
}

func (s *RequestInbox) UpdateStatus(_ context.Context, tenantID, requestID uuid.UUID, ownerID string, status RequestStatus) (EmployeeRequest, error) {
	if !status.Valid() {
		return EmployeeRequest{}, refuse(CodeInvalidRecord, requestID.String(), "request status %q is not declared", status)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	request, ok := s.items[requestID]
	if !ok || request.TenantID != tenantID || request.OwnerID != ownerID {
		return EmployeeRequest{}, refuse(CodeWorkItemNotFound, requestID.String(), "request is not owned by actor")
	}
	request.Status = status
	s.items[requestID] = cloneEmployeeRequest(request)
	return cloneEmployeeRequest(request), nil
}

func (s *RequestInbox) LinkDocument(_ context.Context, tenantID, requestID uuid.UUID, actorID string, link AuthorizedDocumentLink) (EmployeeRequest, error) {
	if strings.TrimSpace(actorID) == "" || link.TenantID == uuid.Nil || strings.TrimSpace(link.DocumentVersionID) == "" || strings.TrimSpace(link.DocumentDigest) == "" || len(link.AuthorizedViewerIDs) == 0 {
		return EmployeeRequest{}, refuse(CodeInvalidRecord, requestID.String(), "document link is incomplete")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	request, ok := s.items[requestID]
	if !ok || request.TenantID != tenantID || (request.RequesterID != actorID && request.OwnerID != actorID) || link.TenantID != tenantID || !contains(link.AuthorizedViewerIDs, actorID) {
		return EmployeeRequest{}, refuse(CodeWorkItemNotFound, requestID.String(), "document link is not authorized")
	}
	request.Documents = append(request.Documents, cloneDocumentLink(link))
	s.items[requestID] = cloneEmployeeRequest(request)
	return cloneEmployeeRequest(request), nil
}

// View is the Help-hub/chat projection. It returns only the caller's tenant
// and participant requests; document links are independently filtered.
func (s *RequestInbox) View(tenantID uuid.UUID, viewerID string) []EmployeeRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]EmployeeRequest, 0)
	for _, request := range s.items {
		if request.TenantID != tenantID || (request.RequesterID != viewerID && request.OwnerID != viewerID) {
			continue
		}
		copy := cloneEmployeeRequest(request)
		copy.Documents = copy.Documents[:0]
		for _, link := range request.Documents {
			if contains(link.AuthorizedViewerIDs, viewerID) {
				copy.Documents = append(copy.Documents, cloneDocumentLink(link))
			}
		}
		result = append(result, copy)
	}
	return result
}

func cloneEmployeeRequest(in EmployeeRequest) EmployeeRequest {
	in.Replies = append([]PrivateReply(nil), in.Replies...)
	documents := in.Documents
	in.Documents = make([]AuthorizedDocumentLink, len(documents))
	for i, link := range documents {
		in.Documents[i] = cloneDocumentLink(link)
	}
	return in
}

func cloneDocumentLink(in AuthorizedDocumentLink) AuthorizedDocumentLink {
	in.AuthorizedViewerIDs = append([]string(nil), in.AuthorizedViewerIDs...)
	return in
}

func sameRequestIdentity(a, b EmployeeRequest) bool {
	return a.TenantID == b.TenantID && a.RequesterID == b.RequesterID && a.OwnerID == b.OwnerID && a.Type == b.Type && a.Subject == b.Subject && a.Details == b.Details && a.Status == b.Status && a.DueAt.Equal(b.DueAt) && a.CreatedAt.Equal(b.CreatedAt)
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// MessageRef is the source binding used by SMB-004. The conversion never
// accepts caller-supplied message text without this tenant/conversation/id
// binding being authorized first.
type MessageRef struct {
	TenantID       uuid.UUID
	ConversationID string
	MessageID      string
	AuthorID       string
	Body           string
}

type ConversionTargetKind string

const (
	ConversionTargetRequest           ConversionTargetKind = "REQUEST"
	ConversionTargetTask              ConversionTargetKind = "TASK"
	ConversionTargetDocumentCandidate ConversionTargetKind = "DOCUMENT_CANDIDATE"
)

func (k ConversionTargetKind) Valid() bool {
	return k == ConversionTargetRequest || k == ConversionTargetTask || k == ConversionTargetDocumentCandidate
}

type MessageConversionRequest struct {
	Message        MessageRef
	ActorID        string
	Target         ConversionTargetKind
	OwnerID        string
	SourceContext  string
	IdempotencyKey string
}

type MessageConversion struct {
	TenantID       uuid.UUID
	ConversionID   uuid.UUID
	Source         MessageRef
	Target         ConversionTargetKind
	OwnerID        string
	SourceContext  string
	IdempotencyKey string
}

type MessageConversionAuthorizer interface {
	CanReadMessage(context.Context, MessageRef, string) error
	CanCreateTarget(context.Context, uuid.UUID, ConversionTargetKind, string) error
}

type MessageConversionSink interface {
	CreateTarget(context.Context, MessageConversion) (string, error)
}

type MessageConversionCoordinator struct {
	mu           sync.Mutex
	authority    MessageConversionAuthorizer
	sink         MessageConversionSink
	results      map[string]MessageConversionResult
	fingerprints map[string]string
}

type MessageConversionResult struct {
	ConversionID uuid.UUID
	TargetID     string
	Target       ConversionTargetKind
}

func NewMessageConversionCoordinator(authority MessageConversionAuthorizer, sink MessageConversionSink) *MessageConversionCoordinator {
	return &MessageConversionCoordinator{authority: authority, sink: sink, results: make(map[string]MessageConversionResult), fingerprints: make(map[string]string)}
}

func (c *MessageConversionCoordinator) Convert(ctx context.Context, in MessageConversionRequest) (MessageConversionResult, error) {
	if c == nil || c.authority == nil || c.sink == nil || in.Message.TenantID == uuid.Nil || strings.TrimSpace(in.Message.ConversationID) == "" || strings.TrimSpace(in.Message.MessageID) == "" || strings.TrimSpace(in.ActorID) == "" || !in.Target.Valid() || strings.TrimSpace(in.OwnerID) == "" || strings.TrimSpace(in.SourceContext) == "" || strings.TrimSpace(in.IdempotencyKey) == "" {
		return MessageConversionResult{}, refuse(CodeInvalidRecord, "", "message conversion is incomplete")
	}
	if err := c.authority.CanReadMessage(ctx, in.Message, in.ActorID); err != nil {
		return MessageConversionResult{}, err
	}
	if err := c.authority.CanCreateTarget(ctx, in.Message.TenantID, in.Target, in.ActorID); err != nil {
		return MessageConversionResult{}, err
	}
	key := in.Message.TenantID.String() + "\x00" + in.IdempotencyKey
	c.mu.Lock()
	defer c.mu.Unlock()
	if prior, ok := c.results[key]; ok {
		if c.fingerprints[key] != conversionFingerprint(in) {
			return MessageConversionResult{}, refuse(CodeInvalidRecord, "", "idempotency key was reused for another message conversion")
		}
		return prior, nil
	}
	conversion := MessageConversion{TenantID: in.Message.TenantID, ConversionID: uuid.New(), Source: in.Message, Target: in.Target, OwnerID: in.OwnerID, SourceContext: in.SourceContext, IdempotencyKey: in.IdempotencyKey}
	targetID, err := c.sink.CreateTarget(ctx, conversion)
	if err != nil {
		return MessageConversionResult{}, err
	}
	result := MessageConversionResult{ConversionID: conversion.ConversionID, TargetID: targetID, Target: in.Target}
	c.results[key] = result
	c.fingerprints[key] = conversionFingerprint(in)
	return result, nil
}

func conversionFingerprint(in MessageConversionRequest) string {
	return strings.Join([]string{in.Message.ConversationID, in.Message.MessageID, in.Message.AuthorID, in.Message.Body, in.ActorID, string(in.Target), in.OwnerID, in.SourceContext}, "\x00")
}
