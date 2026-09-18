// CLOCK-004: offline store-and-forward for time observations.
//
// A device that loses connectivity buffers signed punch evidence in a local
// log keyed by a contiguous sequence starting at 1. Buffering validates the
// sequence and the presence of the signed occurred time and payload digest,
// but never verifies signatures: that belongs to CLOCK-003 at capture time.
// Sync assigns receipt order from the local sequence — it never rewrites it —
// and marks per-entry skew against the injected server clock with a
// confidence grade. The package is kernel-pure: clocks arrive as parameters,
// never from the wall; nothing is persisted or emitted.
package clock

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	// ErrOfflineRejected is the CLOCK-004 seeded-defect sentinel. A test
	// that probes a reordered, replayed, gapped or tampered offline log
	// must see this error with the offending field, state and version.
	ErrOfflineRejected = errors.New("CLOCK_004_REJECTED")
	// ErrOfflineEvidence identifies an invalid sync result that cannot be
	// used as time evidence.
	ErrOfflineEvidence = errors.New("clock: offline sync evidence is invalid")
)

// OfflineRejection is the stable CLOCK-004 failure shape.
type OfflineRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *OfflineRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrOfflineRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the CLOCK_004_REJECTED sentinel to errors.Is.
func (r *OfflineRejection) Unwrap() error { return ErrOfflineRejected }

func offlineReject(field, state, version, reason string) error {
	return &OfflineRejection{Field: field, State: state, Version: version, Reason: reason}
}

// OfflineEntry is one device-buffered punch: its local sequence plus the
// signed occurred time. PayloadDigest is the opaque device signature over
// the observation payload, verified at capture, never recomputed here.
type OfflineEntry struct {
	Sequence      uint64
	EventType     EventType
	DeviceRef     string
	WorkerRef     string
	OccurredAt    time.Time
	PayloadDigest string
}

// OfflineBuffer is the validated device log. Entries are stored in local
// sequence order.
type OfflineBuffer struct {
	DeviceRef string
	Entries   []OfflineEntry
}

// BufferOffline validates a device log: sequences must be contiguous from 1
// in order, and every entry must carry its device, event, signed occurred
// time and payload digest. A reorder, replay, gap or tampered entry fails
// closed with CLOCK_004_REJECTED.
func BufferOffline(deviceRef string, entries []OfflineEntry) (OfflineBuffer, error) {
	const version = "clock-offline/v1"
	if strings.TrimSpace(deviceRef) == "" {
		return OfflineBuffer{}, offlineReject("device_ref", "MISSING", version, "device reference is required")
	}
	if len(entries) == 0 {
		return OfflineBuffer{}, offlineReject("entries", "MISSING", version, "offline log has no entries")
	}
	buffered := make([]OfflineEntry, 0, len(entries))
	for i, e := range entries {
		if e.Sequence != uint64(i+1) {
			return OfflineBuffer{}, offlineReject("entries.sequence", fmt.Sprintf("WANT_%d_GOT_%d", i+1, e.Sequence), version, "offline log must be contiguous from sequence 1")
		}
		if strings.TrimSpace(e.DeviceRef) == "" || e.DeviceRef != deviceRef {
			return OfflineBuffer{}, offlineReject("entries.device_ref", "MISMATCH", version, "entry is not bound to the buffering device")
		}
		if !e.EventType.Valid() {
			return OfflineBuffer{}, offlineReject("entries.event_type", "MISSING", version, "entry event type is not declared")
		}
		if e.OccurredAt.IsZero() {
			return OfflineBuffer{}, offlineReject("entries.occurred_at", "MISSING", version, "signed occurred time is required")
		}
		if strings.TrimSpace(e.PayloadDigest) == "" {
			return OfflineBuffer{}, offlineReject("entries.payload_digest", "MISSING", version, "signed payload digest is required")
		}
		buffered = append(buffered, e)
	}
	return OfflineBuffer{DeviceRef: deviceRef, Entries: buffered}, nil
}

// SyncConfidence grades one synced entry against the server clock.
type SyncConfidence string

// Sync confidence grades.
const (
	SyncConfidenceHigh     SyncConfidence = "HIGH"
	SyncConfidenceDegraded SyncConfidence = "DEGRADED"
)

// SyncedEntry is one buffered entry with its server-side receipt: the
// receipt order mirrors the local sequence, skew is the server clock minus
// the signed occurred time, and confidence degrades when the occurred time
// drifts beyond the skew window.
type SyncedEntry struct {
	OfflineEntry
	ReceiptOrder uint64
	Skew         time.Duration
	Confidence   SyncConfidence
}

// SyncResult is the validated outcome of forwarding one offline log.
type SyncResult struct {
	DeviceRef     string
	Entries       []SyncedEntry
	ReceiptDigest string
}

// SyncOffline forwards a buffered log in local sequence order. Receipt order
// is assigned from that order and never rewritten; the signed occurred time
// is preserved exactly. Entries whose occurred time lies beyond the clock
// skew window sync as DEGRADED, never as rejected: drift is evidence, not
// loss.
func SyncOffline(buf OfflineBuffer, now time.Time) (SyncResult, error) {
	if strings.TrimSpace(buf.DeviceRef) == "" || len(buf.Entries) == 0 {
		return SyncResult{}, fmt.Errorf("%w: sync requires a buffered log", ErrOfflineEvidence)
	}
	if now.IsZero() {
		return SyncResult{}, fmt.Errorf("%w: server clock is required", ErrOfflineEvidence)
	}
	res := SyncResult{DeviceRef: buf.DeviceRef, Entries: make([]SyncedEntry, 0, len(buf.Entries))}
	var body strings.Builder
	fmt.Fprintf(&body, "%s\x00%d\x00", buf.DeviceRef, len(buf.Entries))
	for i, e := range buf.Entries {
		confidence := SyncConfidenceHigh
		if e.OccurredAt.After(now.Add(observationSkew)) {
			confidence = SyncConfidenceDegraded
		}
		res.Entries = append(res.Entries, SyncedEntry{
			OfflineEntry: e,
			ReceiptOrder: uint64(i + 1),
			Skew:         now.Sub(e.OccurredAt),
			Confidence:   confidence,
		})
		fmt.Fprintf(&body, "%d\x00%s\x00%s\x00", e.Sequence, e.OccurredAt.UTC().Format(time.RFC3339Nano), e.PayloadDigest)
	}
	sum := sha256.Sum256([]byte(body.String()))
	res.ReceiptDigest = "sha256:" + hex.EncodeToString(sum[:])
	return res, nil
}
