package artifacts

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/store/object"
)

// ObjectDownloadCatalog is the served artifact-download composition. It
// keeps authorization and lifecycle metadata in this package while delegating
// immutable bytes to the provider-neutral object.Store contract.
//
// DownloadCatalog remains the pure in-memory reference implementation. This
// variant is the boundary used when a running composition has an object
// provider available; it never exposes provider locators or SDK types.
type ObjectDownloadCatalog struct {
	mu      sync.RWMutex
	store   object.Store
	records map[string]DownloadRecord
}

// NewObjectDownloadCatalog binds artifact download policy to an object
// provider. A nil provider is rejected so a served composition cannot
// silently fall back to an ungoverned byte source.
func NewObjectDownloadCatalog(store object.Store) (*ObjectDownloadCatalog, error) {
	if store == nil {
		return nil, ErrDownloadInvalid
	}
	return &ObjectDownloadCatalog{store: store, records: make(map[string]DownloadRecord)}, nil
}

// Register validates metadata and stores the bytes by their content id. A
// byte-identical replay is accepted by the provider contract; any metadata
// disagreement remains an immutable catalog conflict.
func (c *ObjectDownloadCatalog) Register(ctx context.Context, record DownloadRecord) error {
	if c == nil || c.store == nil {
		return ErrDownloadInvalid
	}
	if err := validateObjectDownloadRecord(record); err != nil {
		return err
	}
	info, err := c.store.PutIfAbsent(ctx, object.PutRequest{
		ArtifactID: record.ContentID,
		Content:    record.Bytes,
		Digest:     "sha256:" + record.ContentID,
	})
	if err != nil {
		return err
	}
	if info.ArtifactID != record.ContentID || info.Size != int64(len(record.Bytes)) || info.Digest != "sha256:"+record.ContentID {
		return ErrDownloadTampered
	}

	stored := record
	stored.Bytes = nil
	stored.AllowedPurposes = append([]string(nil), record.AllowedPurposes...)
	c.mu.Lock()
	defer c.mu.Unlock()
	key := downloadKey(record.TenantID, record.ContentID)
	if old, ok := c.records[key]; ok {
		if !sameObjectDownloadRecord(old, stored) {
			return ErrDownloadTampered
		}
		return nil
	}
	c.records[key] = stored
	return nil
}

// SetState changes only policy state and its revision fence. The object-store
// entry is immutable and is never rewritten by a lifecycle transition.
func (c *ObjectDownloadCatalog) SetState(tenant, contentID string, revision uint64, state DownloadState, held bool) error {
	if c == nil || strings.TrimSpace(tenant) == "" || !ValidContentID(contentID) || revision == 0 || !validDownloadState(state) {
		return ErrDownloadInvalid
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	record, ok := c.records[downloadKey(tenant, contentID)]
	if !ok {
		return ErrDownloadNotFound
	}
	if record.Revision != revision {
		return ErrDownloadDenied
	}
	record.Revision++
	record.State = state
	record.Held = held
	c.records[downloadKey(tenant, contentID)] = record
	return nil
}

// Open rechecks the short-lived grant and policy state before reading a range
// from the provider. The receipt is computed from the returned bytes and the
// provider's full-object digest, so a guessed locator or stale grant cannot
// turn into a usable download.
func (c *ObjectDownloadCatalog) Open(ctx context.Context, req DownloadRequest, now time.Time) (io.ReadCloser, DownloadReceipt, error) {
	if err := ctx.Err(); err != nil {
		return nil, DownloadReceipt{}, err
	}
	if c == nil || strings.TrimSpace(req.Grant.TenantID) == "" || strings.TrimSpace(req.Grant.Principal) == "" || strings.TrimSpace(req.Grant.Purpose) == "" || req.Grant.ContentID == "" {
		return nil, DownloadReceipt{}, ErrDownloadInvalid
	}
	if req.Grant.ExpiresAt.IsZero() || !now.Before(req.Grant.ExpiresAt) {
		return nil, DownloadReceipt{}, ErrDownloadDenied
	}
	c.mu.RLock()
	record, ok := c.records[downloadKey(req.Grant.TenantID, req.Grant.ContentID)]
	c.mu.RUnlock()
	if !ok {
		return nil, DownloadReceipt{}, ErrDownloadNotFound
	}
	if req.Grant.ContentID != record.ContentID || req.Grant.Revision != record.Revision || record.State != DownloadAvailable || record.Held || !purposeAllowed(req.Grant.Purpose, record.AllowedPurposes) || !classificationAllowed(req.Grant.AllowedClassifications, record.Classification) {
		return nil, DownloadReceipt{}, ErrDownloadDenied
	}

	info, err := c.store.Stat(ctx, object.Request{ArtifactID: record.ContentID})
	if err != nil {
		return nil, DownloadReceipt{}, mapObjectDownloadError(err)
	}
	if info.ArtifactID != record.ContentID || info.Digest != "sha256:"+record.ContentID {
		return nil, DownloadReceipt{}, ErrDownloadTampered
	}
	end := info.Size
	if req.End != nil {
		end = *req.End
	}
	if req.Start < 0 || end < req.Start || end > info.Size {
		return nil, DownloadReceipt{}, ErrDownloadRange
	}

	reader, got, err := c.store.GetRange(ctx, object.RangeRequest{ArtifactID: record.ContentID, Start: req.Start, End: &end})
	if err != nil {
		return nil, DownloadReceipt{}, mapObjectDownloadError(err)
	}
	data, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil {
		return nil, DownloadReceipt{}, readErr
	}
	if closeErr != nil {
		return nil, DownloadReceipt{}, closeErr
	}
	if got.ArtifactID != record.ContentID || got.Digest != "sha256:"+record.ContentID || int64(len(data)) != end-req.Start {
		return nil, DownloadReceipt{}, ErrDownloadTampered
	}
	receipt := DownloadReceipt{
		TenantID: req.Grant.TenantID, ContentID: record.ContentID, Revision: record.Revision,
		Principal: req.Grant.Principal, Purpose: req.Grant.Purpose,
		RangeStart: req.Start, RangeEnd: end, ByteCount: int64(len(data)),
		FullDigest: record.ContentID, RangeDigest: MultipartDigest(data),
	}
	receipt.Digest = digestDownloadReceipt(receipt)
	return io.NopCloser(bytes.NewReader(data)), receipt, nil
}

func validateObjectDownloadRecord(record DownloadRecord) error {
	if strings.TrimSpace(record.TenantID) == "" || !ValidContentID(record.ContentID) || len(record.Bytes) == 0 || record.Revision == 0 || !record.Classification.Valid() || !validDownloadState(record.State) {
		return ErrDownloadInvalid
	}
	if MultipartDigest(record.Bytes) != record.ContentID {
		return ErrDownloadTampered
	}
	return nil
}

func validDownloadState(state DownloadState) bool {
	switch state {
	case DownloadReceived, DownloadScanning, DownloadAvailable, DownloadQuarantined, DownloadDeleted:
		return true
	default:
		return false
	}
}

func sameObjectDownloadRecord(a, b DownloadRecord) bool {
	return a.TenantID == b.TenantID && a.ContentID == b.ContentID && a.Revision == b.Revision && a.Classification == b.Classification && a.State == b.State && a.Held == b.Held && sameStrings(a.AllowedPurposes, b.AllowedPurposes)
}

func mapObjectDownloadError(err error) error {
	if errors.Is(err, object.ErrNotFound) || errors.Is(err, object.ErrAborted) {
		return ErrDownloadNotFound
	}
	if errors.Is(err, object.ErrRangeUnsupported) {
		return ErrDownloadRange
	}
	return fmt.Errorf("artifacts: object download: %w", err)
}

var _ interface {
	Register(context.Context, DownloadRecord) error
	SetState(string, string, uint64, DownloadState, bool) error
	Open(context.Context, DownloadRequest, time.Time) (io.ReadCloser, DownloadReceipt, error)
} = (*ObjectDownloadCatalog)(nil)
