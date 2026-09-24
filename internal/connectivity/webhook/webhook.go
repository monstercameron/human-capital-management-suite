// Package webhook authenticates and quarantines external event receipts.
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/idempotency"
)

// Version is the contract version of the receipt and replay protocol.
func Version() int { return 1 }

var (
	ErrInvalidEndpoint     = errors.New("webhook: invalid endpoint")
	ErrInvalidRequest      = errors.New("webhook: invalid request")
	ErrPayloadTooLarge     = errors.New("webhook: payload exceeds endpoint limit")
	ErrOutsideReplayWindow = errors.New("webhook: timestamp outside replay window")
	ErrInvalidSignature    = errors.New("webhook: signature is invalid")
	ErrUnknownSchema       = errors.New("webhook: schema is not allowed")
	ErrCrossTenant         = errors.New("webhook: tenant does not match endpoint")
	ErrDuplicateDifferent  = errors.New("webhook: replay key has different bytes")
	ErrReceiptNotFound     = errors.New("webhook: receipt not found")
	ErrReplayNotAuthorized = errors.New("webhook: replay requires approved actor and purpose")
)

// Endpoint is the immutable validation policy for one connection endpoint.
type Endpoint struct {
	ID              string
	TenantID        string
	ConnectionID    string
	Secret          []byte
	AllowedSchemas  []string
	MaxPayloadBytes int
	ReplayWindow    time.Duration
}

// Request is the externally supplied webhook envelope. Signature covers the
// timestamp, event identity, schema, and exact payload bytes.
type Request struct {
	EndpointID  string
	TenantID    string
	EventID     string
	EventType   string
	Schema      string
	Timestamp   time.Time
	Signature   string
	Payload     []byte
	Correlation string
}

// Receipt is immutable and intentionally excludes raw payload bytes.
type Receipt struct {
	ID             string    `json:"receipt_id"`
	EndpointID     string    `json:"endpoint_id"`
	TenantID       string    `json:"tenant_id"`
	ConnectionID   string    `json:"connection_id"`
	EventID        string    `json:"event_id"`
	EventType      string    `json:"event_type"`
	Schema         string    `json:"schema"`
	ReceivedAt     time.Time `json:"received_at"`
	SignatureState string    `json:"signature_state"`
	ReplayKey      string    `json:"replay_key"`
	PayloadHash    string    `json:"payload_hash"`
	PayloadRef     string    `json:"payload_ref"`
	Correlation    string    `json:"correlation,omitempty"`
	Disposition    string    `json:"disposition"`
}

// Envelope is the only form in which protected bytes can be redelivered to a
// trusted internal consumer. Its event identity never changes on replay.
type Envelope struct {
	EventID      string
	EventType    string
	Schema       string
	TenantID     string
	ConnectionID string
	Payload      []byte
	PayloadHash  string
	ReplayOf     string
	Synthetic    bool
}

// ReplayApproval is explicit operator/application authority for a redelivery.
type ReplayApproval struct {
	Actor    string
	Purpose  string
	Approved bool
}

// Store is a concurrency-safe in-memory receipt/quarantine port. Production
// persistence can implement the same immutable semantics without changing the
// validation contract.
type Store struct {
	mu        sync.RWMutex
	endpoints map[string]Endpoint
	receipts  map[string]Receipt
	byReplay  map[string]string
	payloads  map[string][]byte
	lifecycle *idempotency.Registry
}

func NewStore() *Store {
	return NewStoreWithRegistry(idempotency.NewRegistry())
}

// NewStoreWithRegistry composes webhook admission with a shared lifecycle.
// A production receiver can pass a registry backed by a durable idempotency
// store so its event key remains reserved across process restarts. Receipt
// payload quarantine still belongs to the receiver's separate receipt store.
func NewStoreWithRegistry(lifecycle *idempotency.Registry) *Store {
	if lifecycle == nil {
		lifecycle = idempotency.NewRegistry()
	}
	return &Store{endpoints: make(map[string]Endpoint), receipts: make(map[string]Receipt), byReplay: make(map[string]string), payloads: make(map[string][]byte), lifecycle: lifecycle}
}

// RegisterEndpoint publishes a validation policy. Re-registering an endpoint
// is refused so receipt meaning cannot drift under an existing identity.
func (s *Store) RegisterEndpoint(ep Endpoint) error {
	if s == nil || strings.TrimSpace(ep.ID) == "" || strings.TrimSpace(ep.TenantID) == "" || len(ep.Secret) == 0 || ep.MaxPayloadBytes <= 0 || ep.ReplayWindow <= 0 {
		return ErrInvalidEndpoint
	}
	ep.AllowedSchemas = uniqueSorted(ep.AllowedSchemas)
	if len(ep.AllowedSchemas) == 0 {
		return fmt.Errorf("%w: schema allowlist is required", ErrInvalidEndpoint)
	}
	ep.Secret = append([]byte(nil), ep.Secret...)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.endpoints[ep.ID]; exists {
		return fmt.Errorf("%w: endpoint already exists", ErrInvalidEndpoint)
	}
	s.endpoints[ep.ID] = ep
	return nil
}

// Sign creates the required hex HMAC-SHA256 signature for a request.
func Sign(secret []byte, req Request) string {
	h := hmac.New(sha256.New, secret)
	h.Write(signingBytes(req))
	return hex.EncodeToString(h.Sum(nil))
}

// Receive validates and records one immutable receipt. Duplicate identical
// deliveries return the original receipt; duplicate event identity with other
// bytes is rejected without mutating the store.
func (s *Store) Receive(req Request, now time.Time) (Receipt, error) {
	if s == nil || strings.TrimSpace(req.EndpointID) == "" || strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.EventID) == "" || strings.TrimSpace(req.EventType) == "" || req.Schema == "" || req.Timestamp.IsZero() || now.IsZero() {
		return Receipt{}, ErrInvalidRequest
	}
	s.mu.RLock()
	ep, ok := s.endpoints[req.EndpointID]
	s.mu.RUnlock()
	if !ok {
		return Receipt{}, ErrInvalidEndpoint
	}
	if req.TenantID != ep.TenantID {
		return Receipt{}, ErrCrossTenant
	}
	if len(req.Payload) > ep.MaxPayloadBytes {
		return Receipt{}, ErrPayloadTooLarge
	}
	if absDuration(now.Sub(req.Timestamp)) > ep.ReplayWindow {
		return Receipt{}, ErrOutsideReplayWindow
	}
	if !contains(ep.AllowedSchemas, req.Schema) {
		return Receipt{}, ErrUnknownSchema
	}
	want := Sign(ep.Secret, req)
	got, err := hex.DecodeString(strings.TrimSpace(req.Signature))
	wantBytes, _ := hex.DecodeString(want)
	if err != nil || !hmac.Equal(got, wantBytes) {
		return Receipt{}, ErrInvalidSignature
	}
	payloadHash := sha256.Sum256(req.Payload)
	hashText := hex.EncodeToString(payloadHash[:])
	replayKey := req.EndpointID + ":" + req.EventID
	receiptID := digestText(replayKey + "|" + hashText)
	payloadRef := "quarantine://" + receiptID + "/payload"
	identity := idempotency.Identity{Tenant: req.TenantID, Capability: "webhook.receive", EffectScope: "connection:" + ep.ConnectionID, Key: req.EventID}
	reservation, err := s.lifecycle.Reserve(idempotency.Request{Identity: identity, Layer: "webhook", Canonical: canonicalRequest(req), Retention: idempotency.RetentionPolicy{ExpiresAt: now.Add(ep.ReplayWindow), Mode: idempotency.RejectReuse, Tombstone: true}, Now: now})
	if err != nil {
		if errors.Is(err, idempotency.ErrConflict) {
			return Receipt{}, ErrDuplicateDifferent
		}
		return Receipt{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existingID, exists := s.byReplay[replayKey]; exists {
		existing := s.receipts[existingID]
		if existing.PayloadHash != hashText || existing.EventType != req.EventType || existing.Schema != req.Schema {
			return Receipt{}, ErrDuplicateDifferent
		}
		return existing, nil
	}
	r := Receipt{ID: receiptID, EndpointID: req.EndpointID, TenantID: req.TenantID, ConnectionID: ep.ConnectionID, EventID: req.EventID, EventType: req.EventType, Schema: req.Schema, ReceivedAt: now.UTC(), SignatureState: "VALID", ReplayKey: replayKey, PayloadHash: hashText, PayloadRef: payloadRef, Correlation: req.Correlation, Disposition: "RECEIVED"}
	s.receipts[r.ID] = r
	s.byReplay[replayKey] = r.ID
	s.payloads[r.ID] = append([]byte(nil), req.Payload...)
	if reservation.Decision == idempotency.Reserved {
		if _, completeErr := s.lifecycle.Complete(identity, reservation.Record.RequestDigest, r.ID, r.ID, now); completeErr != nil {
			return Receipt{}, completeErr
		}
	}
	return r, nil
}

// Get returns metadata only; protected bytes stay in quarantine.
func (s *Store) Get(receiptID string) (Receipt, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.receipts[receiptID]
	if !ok {
		return Receipt{}, ErrReceiptNotFound
	}
	return r, nil
}

// Replay redelivers the exact existing event identity after explicit
// authorization. A variadic approval keeps the call concise while refusing
// the empty form.
func (s *Store) Replay(receiptID string, approvals ...ReplayApproval) (Envelope, error) {
	if len(approvals) != 1 || !approvals[0].Approved || strings.TrimSpace(approvals[0].Actor) == "" || strings.TrimSpace(approvals[0].Purpose) == "" {
		return Envelope{}, ErrReplayNotAuthorized
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.receipts[receiptID]
	if !ok {
		return Envelope{}, ErrReceiptNotFound
	}
	payload, ok := s.payloads[receiptID]
	if !ok {
		return Envelope{}, ErrReceiptNotFound
	}
	return Envelope{EventID: r.EventID, EventType: r.EventType, Schema: r.Schema, TenantID: r.TenantID, ConnectionID: r.ConnectionID, Payload: append([]byte(nil), payload...), PayloadHash: r.PayloadHash, ReplayOf: r.ID, Synthetic: false}, nil
}

// Explain returns a stable operational summary without exposing payload bytes.
func Explain(r Receipt) string {
	return fmt.Sprintf("receipt=%s event=%s signature=%s disposition=%s replay_key=%s", r.ID, r.EventID, r.SignatureState, r.Disposition, r.ReplayKey)
}

func signingBytes(req Request) []byte {
	return []byte(fmt.Sprintf("%d\n%s\n%s\n%s\n%s", req.Timestamp.UnixNano(), req.EventID, req.EventType, req.Schema, req.Payload))
}

func canonicalRequest(req Request) []byte {
	return []byte(fmt.Sprintf("%s\n%s\n%s\n%s", req.EventID, req.EventType, req.Schema, req.Payload))
}

func digestText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
