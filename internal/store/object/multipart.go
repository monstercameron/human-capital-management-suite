package object

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// MultipartState is the durable semantic state of a resumable upload.
type MultipartState string

const (
	MultipartUploading MultipartState = "UPLOADING"
	MultipartReceived  MultipartState = "RECEIVED"
	MultipartScanning  MultipartState = "SCANNING"
	MultipartAborted   MultipartState = "ABORTED"
)

var (
	ErrUploadUnauthorized = errors.New("object: multipart upload unauthorized")
	ErrUploadNotFound     = errors.New("object: multipart upload not found")
	ErrUploadConflict     = errors.New("object: multipart upload conflict")
	ErrUploadIncomplete   = errors.New("object: multipart upload is missing parts")
	ErrUploadOversize     = errors.New("object: multipart upload exceeds its size limit")
	ErrUploadAborted      = errors.New("object: multipart upload was aborted")
	ErrUploadExpired      = errors.New("object: multipart upload grant expired")
	ErrUploadChecksum     = errors.New("object: multipart part checksum mismatch")
)

// UploadGrant is an already-authorized, short-lived upload decision. This
// package enforces the snapshot on every operation; it does not decide who
// the principal is or how a purpose is granted.
type UploadGrant struct {
	TenantID  string
	Principal string
	Purpose   string
	ExpiresAt time.Time
}

// MultipartRequest starts one bounded upload. ExpectedParts is a contiguous
// one-based part count; MaxBytes is an explicit per-upload ceiling.
type MultipartRequest struct {
	TenantID       string
	ExpectedParts  int
	MaxBytes       int64
	Grant          UploadGrant
	IdempotencyKey string
}

// MultipartPartRequest appends or replays one part. A replay is accepted only
// when both its checksum and bytes are identical to the first accepted part.
type MultipartPartRequest struct {
	UploadID   string
	TenantID   string
	Grant      UploadGrant
	PartNumber int
	Content    []byte
	Checksum   string
}

// MultipartCompleteRequest closes an upload after checking every part and the
// optional whole-object digest.
type MultipartCompleteRequest struct {
	UploadID  string
	TenantID  string
	Grant     UploadGrant
	ContentID string
}

// MultipartAbortRequest aborts an upload. Aborted ids remain tombstoned so a
// late part or completion can never resurrect a usable artifact.
type MultipartAbortRequest struct {
	UploadID string
	TenantID string
	Grant    UploadGrant
}

// MultipartPartReceipt is safe to persist as a checkpoint: it carries no
// provider locator and no mutable byte slice.
type MultipartPartReceipt struct {
	UploadID   string
	TenantID   string
	PartNumber int
	Size       int64
	Checksum   string
}

// MultipartUpload is an immutable snapshot of upload state. Parts are copied
// and sorted by part number on every snapshot.
type MultipartUpload struct {
	UploadID      string
	TenantID      string
	ExpectedParts int
	MaxBytes      int64
	State         MultipartState
	ContentID     string
	TotalBytes    int64
	Parts         []MultipartPartReceipt
	Transitions   []MultipartState
}

type multipartPart struct {
	receipt MultipartPartReceipt
	content []byte
}

type multipartEntry struct {
	upload    MultipartUpload
	grant     UploadGrant
	parts     map[int]multipartPart
	startedAt time.Time
}

// MultipartManager is a concurrency-safe pure state machine. A persistence
// adapter can commit the returned snapshot and bytes behind its own atomic
// transaction without changing this semantic contract.
type MultipartManager struct {
	mu      sync.Mutex
	next    uint64
	uploads map[string]*multipartEntry
	keys    map[string]string
	now     func() time.Time
}

// NewMultipartManager creates an in-memory multipart state machine.
func NewMultipartManager(now func() time.Time) *MultipartManager {
	return &MultipartManager{uploads: make(map[string]*multipartEntry), keys: make(map[string]string), now: now}
}

func (m *MultipartManager) clock() time.Time {
	if m != nil && m.now != nil {
		return m.now().UTC()
	}
	return time.Now().UTC()
}

func validateGrant(g UploadGrant, tenant string, now time.Time) error {
	if strings.TrimSpace(tenant) == "" || g.TenantID != tenant || strings.TrimSpace(g.Principal) == "" || strings.TrimSpace(g.Purpose) == "" {
		return ErrUploadUnauthorized
	}
	if g.ExpiresAt.IsZero() || !now.Before(g.ExpiresAt) {
		return ErrUploadExpired
	}
	return nil
}

func validateMultipartRequest(r MultipartRequest, now time.Time) error {
	if err := validateGrant(r.Grant, r.TenantID, now); err != nil {
		return err
	}
	if r.ExpectedParts < 1 || r.MaxBytes <= 0 || strings.TrimSpace(r.IdempotencyKey) == "" {
		return ErrInvalidRequest
	}
	return nil
}

// Start begins or idempotently replays a tenant-scoped upload.
func (m *MultipartManager) Start(ctx context.Context, r MultipartRequest) (MultipartUpload, error) {
	if err := ctx.Err(); err != nil {
		return MultipartUpload{}, err
	}
	if m == nil {
		return MultipartUpload{}, ErrInvalidRequest
	}
	if err := validateMultipartRequest(r, m.clock()); err != nil {
		return MultipartUpload{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if id, ok := m.keys[r.TenantID+"\x00"+r.Grant.Principal+"\x00"+r.IdempotencyKey]; ok {
		entry := m.uploads[id]
		if entry.upload.ExpectedParts != r.ExpectedParts || entry.upload.MaxBytes != r.MaxBytes || entry.grant.Purpose != r.Grant.Purpose {
			return MultipartUpload{}, ErrUploadConflict
		}
		return snapshotMultipart(entry), nil
	}
	m.next++
	id := fmt.Sprintf("upload-%d", m.next)
	entry := &multipartEntry{
		upload:    MultipartUpload{UploadID: id, TenantID: r.TenantID, ExpectedParts: r.ExpectedParts, MaxBytes: r.MaxBytes, State: MultipartUploading, Transitions: []MultipartState{MultipartUploading}},
		grant:     r.Grant,
		parts:     make(map[int]multipartPart),
		startedAt: m.clock(),
	}
	m.uploads[id] = entry
	m.keys[r.TenantID+"\x00"+r.Grant.Principal+"\x00"+r.IdempotencyKey] = id
	return snapshotMultipart(entry), nil
}

// PutPart accepts a bounded, checksummed part and returns a stable receipt.
func (m *MultipartManager) PutPart(ctx context.Context, r MultipartPartRequest) (MultipartPartReceipt, error) {
	if err := ctx.Err(); err != nil {
		return MultipartPartReceipt{}, err
	}
	if m == nil {
		return MultipartPartReceipt{}, ErrInvalidRequest
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.uploads[r.UploadID]
	if !ok {
		return MultipartPartReceipt{}, ErrUploadNotFound
	}
	if err := validateGrant(r.Grant, r.TenantID, m.clock()); err != nil || r.TenantID != entry.upload.TenantID || r.Grant.Principal != entry.grant.Principal || r.Grant.Purpose != entry.grant.Purpose {
		return MultipartPartReceipt{}, ErrUploadUnauthorized
	}
	if entry.upload.State == MultipartAborted {
		return MultipartPartReceipt{}, ErrUploadAborted
	}
	if entry.upload.State != MultipartUploading {
		return MultipartPartReceipt{}, ErrUploadConflict
	}
	if r.PartNumber < 1 || r.PartNumber > entry.upload.ExpectedParts || len(r.Content) == 0 {
		return MultipartPartReceipt{}, ErrInvalidRequest
	}
	if int64(len(r.Content))+entry.upload.TotalBytes > entry.upload.MaxBytes {
		if _, exists := entry.parts[r.PartNumber]; !exists {
			return MultipartPartReceipt{}, ErrUploadOversize
		}
	}
	actual := MultipartChecksum(r.Content)
	if r.Checksum != "" && r.Checksum != actual {
		return MultipartPartReceipt{}, ErrUploadChecksum
	}
	if old, exists := entry.parts[r.PartNumber]; exists {
		if old.receipt.Checksum != actual || string(old.content) != string(r.Content) {
			return MultipartPartReceipt{}, ErrUploadConflict
		}
		return old.receipt, nil
	}
	receipt := MultipartPartReceipt{UploadID: r.UploadID, TenantID: r.TenantID, PartNumber: r.PartNumber, Size: int64(len(r.Content)), Checksum: actual}
	entry.parts[r.PartNumber] = multipartPart{receipt: receipt, content: append([]byte(nil), r.Content...)}
	entry.upload.TotalBytes += receipt.Size
	return receipt, nil
}

// Complete verifies all contiguous parts and atomically records the
// RECEIVED-to-SCANNING transition. The returned content is copied for the
// storage adapter that will persist it under ContentID.
func (m *MultipartManager) Complete(ctx context.Context, r MultipartCompleteRequest) (MultipartUpload, []byte, error) {
	if err := ctx.Err(); err != nil {
		return MultipartUpload{}, nil, err
	}
	if m == nil {
		return MultipartUpload{}, nil, ErrInvalidRequest
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.uploads[r.UploadID]
	if !ok {
		return MultipartUpload{}, nil, ErrUploadNotFound
	}
	if err := validateGrant(r.Grant, r.TenantID, m.clock()); err != nil || r.TenantID != entry.upload.TenantID || r.Grant.Principal != entry.grant.Principal || r.Grant.Purpose != entry.grant.Purpose {
		return MultipartUpload{}, nil, ErrUploadUnauthorized
	}
	if entry.upload.State == MultipartAborted {
		return MultipartUpload{}, nil, ErrUploadAborted
	}
	if entry.upload.State != MultipartUploading {
		return MultipartUpload{}, nil, ErrUploadConflict
	}
	if len(entry.parts) != entry.upload.ExpectedParts {
		return MultipartUpload{}, nil, ErrUploadIncomplete
	}
	content := make([]byte, 0, entry.upload.TotalBytes)
	for number := 1; number <= entry.upload.ExpectedParts; number++ {
		part, exists := entry.parts[number]
		if !exists {
			return MultipartUpload{}, nil, ErrUploadIncomplete
		}
		if MultipartChecksum(part.content) != part.receipt.Checksum {
			return MultipartUpload{}, nil, ErrUploadChecksum
		}
		content = append(content, part.content...)
	}
	id := MultipartChecksum(content)
	if r.ContentID != "" && r.ContentID != id {
		return MultipartUpload{}, nil, ErrUploadChecksum
	}
	entry.upload.ContentID = id
	entry.upload.State = MultipartReceived
	entry.upload.Transitions = append(entry.upload.Transitions, MultipartReceived, MultipartScanning)
	entry.upload.State = MultipartScanning
	return snapshotMultipart(entry), append([]byte(nil), content...), nil
}

// Abort tombstones an upload and drops its part bytes.
func (m *MultipartManager) Abort(ctx context.Context, r MultipartAbortRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m == nil {
		return ErrInvalidRequest
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.uploads[r.UploadID]
	if !ok {
		return ErrUploadNotFound
	}
	if err := validateGrant(r.Grant, r.TenantID, m.clock()); err != nil || r.TenantID != entry.upload.TenantID || r.Grant.Principal != entry.grant.Principal {
		return ErrUploadUnauthorized
	}
	if entry.upload.State == MultipartAborted {
		return nil
	}
	if entry.upload.State != MultipartUploading {
		return ErrUploadConflict
	}
	entry.upload.State = MultipartAborted
	entry.upload.Transitions = append(entry.upload.Transitions, MultipartAborted)
	entry.parts = make(map[int]multipartPart)
	return nil
}

// SweepAbandoned aborts UPLOADING uploads started before the cutoff
// and returns their ids. It only touches uploads still uploading:
// completed, received and already-aborted uploads are never swept.
func (m *MultipartManager) SweepAbandoned(cutoff time.Time) []string {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var swept []string
	for id, entry := range m.uploads {
		if entry.upload.State != MultipartUploading || !entry.startedAt.Before(cutoff) {
			continue
		}
		entry.upload.State = MultipartAborted
		entry.upload.Transitions = append(entry.upload.Transitions, MultipartAborted)
		entry.parts = make(map[int]multipartPart)
		swept = append(swept, id)
	}
	return swept
}

// Get returns a defensive state snapshot.
func (m *MultipartManager) Get(ctx context.Context, id, tenant string, grant UploadGrant) (MultipartUpload, error) {
	if err := ctx.Err(); err != nil {
		return MultipartUpload{}, err
	}
	if m == nil {
		return MultipartUpload{}, ErrInvalidRequest
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.uploads[id]
	if !ok {
		return MultipartUpload{}, ErrUploadNotFound
	}
	if err := validateGrant(grant, tenant, m.clock()); err != nil || tenant != entry.upload.TenantID || grant.Principal != entry.grant.Principal {
		return MultipartUpload{}, ErrUploadUnauthorized
	}
	return snapshotMultipart(entry), nil
}

func snapshotMultipart(entry *multipartEntry) MultipartUpload {
	out := entry.upload
	out.Parts = make([]MultipartPartReceipt, 0, len(entry.parts))
	for _, part := range entry.parts {
		out.Parts = append(out.Parts, part.receipt)
	}
	sort.Slice(out.Parts, func(i, j int) bool { return out.Parts[i].PartNumber < out.Parts[j].PartNumber })
	out.Transitions = append([]MultipartState(nil), entry.upload.Transitions...)
	return out
}

// MultipartChecksum returns the canonical content id used by multipart
// checkpoints and completion. It is deliberately the same sha256 identity
// scheme as the object store's immutable Put contract.
func MultipartChecksum(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Explain returns a bounded state summary with no part bytes.
func (u MultipartUpload) Explain() string {
	return fmt.Sprintf("multipart upload id=%s tenant=%s state=%s parts=%d/%d bytes=%d content_id=%s", u.UploadID, u.TenantID, u.State, len(u.Parts), u.ExpectedParts, u.TotalBytes, u.ContentID)
}

// ExplainMultipart is the package-level Explain-shaped entry point.
func ExplainMultipart(u MultipartUpload) string { return u.Explain() }
