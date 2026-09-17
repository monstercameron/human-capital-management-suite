// CLOCK-005: deduplicate and reconcile device/backend observations.
//
// ReconcileDeviceBackend compares signed device punches against backend
// records. An identical replay returns the original receipt; a changed
// replay is a conflict; a punch from a revoked source or a duplicate
// signed punch with a changed payload is refused with CLOCK_005_REJECTED.
// Missing, extra or reordered device observations create exceptions — the
// reconciler never guesses. The function is kernel-pure: it keeps no
// state and posts nothing.
package clock

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// DedupVersion is the rejection version carried by DedupRejection.
const DedupVersion = "clock-dedup/v1"

var (
	// ErrDedupRejected is the CLOCK-005 sentinel. A revoked source or a
	// duplicate signed punch posts with this error carrying the offending
	// field, state and version, and nothing is posted.
	ErrDedupRejected = errors.New("CLOCK_005_REJECTED")
)

// DedupRejection is the stable CLOCK-005 failure shape.
type DedupRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *DedupRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrDedupRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the CLOCK_005_REJECTED sentinel to errors.Is.
func (r *DedupRejection) Unwrap() error { return ErrDedupRejected }

func dedupReject(field, state, reason string) error {
	return &DedupRejection{Field: field, State: state, Version: DedupVersion, Reason: reason}
}

// DevicePunch is one signed device observation presented for posting.
// PayloadDigest seals the signed content; SignatureOK reports device
// signature verification done at the trust boundary.
type DevicePunch struct {
	Tenant        string
	SourceID      string
	SourceRevoked bool
	PunchID       string
	DeviceSeq     int64
	OccurredAt    time.Time
	PayloadDigest string
	SignatureOK   bool
}

// BackendRecord is one already-posted backend observation.
type BackendRecord struct {
	PunchID       string
	SourceID      string
	PayloadDigest string
	Receipt       string
	RecordedAt    time.Time
}

// PunchConflict is a changed replay of an already-posted punch.
type PunchConflict struct {
	PunchID         string
	PostedDigest    string
	PresentedDigest string
}

// PunchException is a missing, extra or reordered observation that needs
// human review. The reconciler reports it without guessing a resolution.
type PunchException struct {
	Kind    string
	PunchID string
	Detail  string
}

// DedupResult is the deterministic reconciliation outcome. Receipts maps
// every accepted punch to its receipt: identical replays return the
// original receipt, and only first-seen punches mint new ones.
type DedupResult struct {
	Receipts   map[string]string
	Conflicts  []PunchConflict
	Exceptions []PunchException
	Posted     int
	Replayed   int
	Digest     string
}

func (r DedupResult) computedDigest() string {
	receipts := make([]string, 0, len(r.Receipts))
	for punch, receipt := range r.Receipts {
		receipts = append(receipts, punch+"\x00"+receipt)
	}
	sort.Strings(receipts)
	conflicts := make([]string, 0, len(r.Conflicts))
	for _, c := range r.Conflicts {
		conflicts = append(conflicts, c.PunchID+"\x00"+c.PostedDigest+"\x00"+c.PresentedDigest)
	}
	sort.Strings(conflicts)
	exceptions := make([]string, 0, len(r.Exceptions))
	for _, e := range r.Exceptions {
		exceptions = append(exceptions, e.Kind+"\x00"+e.PunchID+"\x00"+e.Detail)
	}
	sort.Strings(exceptions)
	w := canonicalbytes.New("hcmnext.domains.clock.DedupResult", 1).
		SortedStrings("receipts", receipts).
		SortedStrings("conflicts", conflicts).
		SortedStrings("exceptions", exceptions)
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

func receiptFor(punchID, digest string) string {
	return "receipt:" + punchID + ":" + digest
}

// ReconcileDeviceBackend posts device punches against backend records.
// backend is the posted truth; device is the presented batch. The backend
// slice is never mutated; the result carries the merged receipt view.
func ReconcileDeviceBackend(backend []BackendRecord, device []DevicePunch, now time.Time) (DedupResult, error) {
	if now.IsZero() {
		return DedupResult{}, dedupReject("dedup.now", "MISSING", "reconciliation instant is required")
	}
	posted := make(map[string]BackendRecord, len(backend))
	receipts := make(map[string]string, len(backend)+len(device))
	for _, rec := range backend {
		if strings.TrimSpace(rec.PunchID) == "" || strings.TrimSpace(rec.Receipt) == "" {
			return DedupResult{}, dedupReject("dedup.backend", "INVALID", "backend records carry punch id and receipt")
		}
		if prior, dup := posted[rec.PunchID]; dup && prior.PayloadDigest != rec.PayloadDigest {
			return DedupResult{}, dedupReject("dedup.backend", "DIVERGED", fmt.Sprintf("backend holds two payloads for %s", rec.PunchID))
		}
		posted[rec.PunchID] = rec
		receipts[rec.PunchID] = rec.Receipt
	}
	res := DedupResult{Receipts: receipts}
	seen := make(map[string]string, len(device))
	var lastSeq int64 = -1
	sequenced := true
	for _, p := range device {
		if strings.TrimSpace(p.Tenant) == "" {
			return DedupResult{}, dedupReject("dedup.tenant", "MISSING", "tenant is required")
		}
		if strings.TrimSpace(p.PunchID) == "" {
			return DedupResult{}, dedupReject("dedup.punch_id", "MISSING", "punch id is required")
		}
		if strings.TrimSpace(p.PayloadDigest) == "" {
			return DedupResult{}, dedupReject("dedup.payload_digest", "MISSING", "payload digest is required")
		}
		if p.SourceRevoked {
			return DedupResult{}, dedupReject("dedup.source", "REVOKED", fmt.Sprintf("source %s is revoked", p.SourceID))
		}
		if !p.SignatureOK {
			return DedupResult{}, dedupReject("dedup.signature", "INVALID", fmt.Sprintf("punch %s lacks a valid device signature", p.PunchID))
		}
		if p.OccurredAt.IsZero() {
			return DedupResult{}, dedupReject("dedup.occurred_at", "MISSING", "occurred time is required")
		}
		if prior, dup := seen[p.PunchID]; dup {
			if prior != p.PayloadDigest {
				return DedupResult{}, dedupReject("dedup.punch", "DUPLICATE_CONFLICT", fmt.Sprintf("punch %s repeats with a changed payload", p.PunchID))
			}
			res.Replayed++
			continue
		}
		seen[p.PunchID] = p.PayloadDigest
		if posted, ok := posted[p.PunchID]; ok {
			if posted.PayloadDigest != p.PayloadDigest {
				res.Conflicts = append(res.Conflicts, PunchConflict{PunchID: p.PunchID, PostedDigest: posted.PayloadDigest, PresentedDigest: p.PayloadDigest})
				continue
			}
			res.Replayed++
			continue
		}
		receipt := receiptFor(p.PunchID, p.PayloadDigest)
		res.Receipts[p.PunchID] = receipt
		res.Posted++
		if lastSeq >= 0 && p.DeviceSeq < lastSeq {
			sequenced = false
		}
		lastSeq = p.DeviceSeq
	}
	for punch := range posted {
		if _, ok := seen[punch]; !ok {
			hasPresented := false
			for _, p := range device {
				if p.PunchID == punch {
					hasPresented = true
					break
				}
			}
			if !hasPresented && len(device) > 0 {
				res.Exceptions = append(res.Exceptions, PunchException{Kind: "MISSING_DEVICE_OBSERVATION", PunchID: punch, Detail: "backend holds a punch the device batch omits"})
			}
		}
	}
	if !sequenced {
		res.Exceptions = append(res.Exceptions, PunchException{Kind: "REORDERED_DEVICE_OBSERVATION", PunchID: "", Detail: "device sequence regressed; receipt order preserved"})
	}
	sort.Slice(res.Conflicts, func(i, j int) bool { return res.Conflicts[i].PunchID < res.Conflicts[j].PunchID })
	sort.Slice(res.Exceptions, func(i, j int) bool {
		if res.Exceptions[i].Kind != res.Exceptions[j].Kind {
			return res.Exceptions[i].Kind < res.Exceptions[j].Kind
		}
		return res.Exceptions[i].PunchID < res.Exceptions[j].PunchID
	})
	res.Digest = res.computedDigest()
	return res, nil
}
