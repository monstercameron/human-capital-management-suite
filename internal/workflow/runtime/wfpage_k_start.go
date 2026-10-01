package runtime

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

	"github.com/google/uuid"
)

var (
	ErrPageStartInvalid  = errors.New("workflow runtime: invalid page start")
	ErrPageNotPublished  = errors.New("workflow runtime: page version is not published")
	ErrPageStartConflict = errors.New("workflow runtime: page start idempotency conflict")
)

// PagePublication is the server's authoritative page-version answer. A
// caller supplies a requested version; it never supplies Published=true as
// proof of publication.
type PagePublication struct {
	TenantID    uuid.UUID
	WorkflowID  string
	PageID      string
	PageVersion int64
	Digest      string
	Published   bool
}

// PageStartRequest is the typed-input boundary for a workflow page. Raw JSON
// values retain their declared JSON types while the canonical digest sorts
// field names and rejects malformed values before a start is admitted.
type PageStartRequest struct {
	TenantID        uuid.UUID
	WorkflowID      string
	WorkflowVersion uint32
	PageID          string
	PageVersion     int64
	SubjectRef      string
	IdempotencyKey  string
	Inputs          map[string]json.RawMessage
}

type PageStartBinding struct {
	TenantID        uuid.UUID
	WorkflowID      string
	WorkflowVersion uint32
	PageID          string
	PageVersion     int64
	SubjectRef      string
	IdempotencyKey  string
	InputDigest     string
}

type PageStartReceipt struct {
	InstanceID      uuid.UUID
	WorkflowID      string
	WorkflowVersion uint32
	PageID          string
	PageVersion     int64
	SubjectRef      string
	InputDigest     string
	Replay          bool
}

func PageInputDigest(inputs map[string]json.RawMessage) (string, error) {
	keys := make([]string, 0, len(inputs))
	canonical := make(map[string]json.RawMessage, len(inputs))
	for key, raw := range inputs {
		key = strings.TrimSpace(key)
		if key == "" {
			return "", fmt.Errorf("%w: input field is empty", ErrPageStartInvalid)
		}
		if !json.Valid(raw) {
			return "", fmt.Errorf("%w: input %q is not valid JSON", ErrPageStartInvalid, key)
		}
		keys = append(keys, key)
		canonical[key] = append(json.RawMessage(nil), raw...)
	}
	sort.Strings(keys)
	// A struct with sorted key/value pairs avoids relying on map iteration for
	// the digest while preserving the JSON type of each field.
	entries := make([]struct {
		Field string          `json:"field"`
		Value json.RawMessage `json:"value"`
	}, 0, len(keys))
	for _, key := range keys {
		entries = append(entries, struct {
			Field string          `json:"field"`
			Value json.RawMessage `json:"value"`
		}{Field: key, Value: canonical[key]})
	}
	encoded, err := json.Marshal(entries)
	if err != nil {
		return "", fmt.Errorf("%w: encode input digest: %v", ErrPageStartInvalid, err)
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func BindPageStart(request PageStartRequest, publication PagePublication) (PageStartBinding, error) {
	switch {
	case request.TenantID == uuid.Nil:
		return PageStartBinding{}, fmt.Errorf("%w: tenant is required", ErrPageStartInvalid)
	case strings.TrimSpace(request.WorkflowID) == "", request.WorkflowVersion == 0:
		return PageStartBinding{}, fmt.Errorf("%w: workflow identity is incomplete", ErrPageStartInvalid)
	case strings.TrimSpace(request.PageID) == "", request.PageVersion < 1:
		return PageStartBinding{}, fmt.Errorf("%w: page identity is incomplete", ErrPageStartInvalid)
	case strings.TrimSpace(request.SubjectRef) == "":
		return PageStartBinding{}, fmt.Errorf("%w: subject is required", ErrPageStartInvalid)
	case strings.TrimSpace(request.IdempotencyKey) == "":
		return PageStartBinding{}, fmt.Errorf("%w: idempotency key is required", ErrPageStartInvalid)
	case publication.TenantID != request.TenantID || publication.WorkflowID != request.WorkflowID ||
		publication.PageID != request.PageID || publication.PageVersion != request.PageVersion || !publication.Published:
		return PageStartBinding{}, ErrPageNotPublished
	}
	digest, err := PageInputDigest(request.Inputs)
	if err != nil {
		return PageStartBinding{}, err
	}
	return PageStartBinding{TenantID: request.TenantID, WorkflowID: request.WorkflowID, WorkflowVersion: request.WorkflowVersion,
		PageID: request.PageID, PageVersion: request.PageVersion, SubjectRef: request.SubjectRef,
		IdempotencyKey: request.IdempotencyKey, InputDigest: digest}, nil
}

func pageStartSlot(tenant uuid.UUID, workflowID, subject, key string) string {
	return tenant.String() + "\x00" + workflowID + "\x00" + subject + "\x00" + key
}

type PageStartExecutor func(context.Context, PageStartBinding) (PageStartReceipt, error)

// PageStartRegistry provides a small durable-boundary adapter for the start
// intent. The real executor owns the workflow transaction and must itself use
// runtime.Start's idempotent instance key; this registry prevents concurrent
// page submissions from invoking it twice and replays completed receipts.
type PageStartRegistry struct {
	mu       sync.Mutex
	receipts map[string]PageStartReceipt
}

func NewPageStartRegistry() *PageStartRegistry {
	return &PageStartRegistry{receipts: make(map[string]PageStartReceipt)}
}

func (registry *PageStartRegistry) Submit(ctx context.Context, binding PageStartBinding, execute PageStartExecutor) (PageStartReceipt, error) {
	if registry == nil || execute == nil || binding.TenantID == uuid.Nil || strings.TrimSpace(binding.WorkflowID) == "" || binding.WorkflowVersion == 0 ||
		strings.TrimSpace(binding.PageID) == "" || binding.PageVersion < 1 || strings.TrimSpace(binding.SubjectRef) == "" ||
		strings.TrimSpace(binding.IdempotencyKey) == "" || binding.InputDigest == "" {
		return PageStartReceipt{}, ErrPageStartInvalid
	}
	slot := pageStartSlot(binding.TenantID, binding.WorkflowID, binding.SubjectRef, binding.IdempotencyKey)
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if existing, ok := registry.receipts[slot]; ok {
		if existing.InputDigest != binding.InputDigest || existing.PageID != binding.PageID || existing.PageVersion != binding.PageVersion || existing.WorkflowVersion != binding.WorkflowVersion {
			return PageStartReceipt{}, ErrPageStartConflict
		}
		existing.Replay = true
		return existing, nil
	}
	receipt, err := execute(ctx, binding)
	if err != nil {
		return PageStartReceipt{}, err
	}
	receipt.WorkflowID = binding.WorkflowID
	receipt.WorkflowVersion = binding.WorkflowVersion
	receipt.PageID = binding.PageID
	receipt.PageVersion = binding.PageVersion
	receipt.SubjectRef = binding.SubjectRef
	receipt.InputDigest = binding.InputDigest
	registry.receipts[slot] = receipt
	return receipt, nil
}
