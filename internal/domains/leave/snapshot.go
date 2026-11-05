package leave

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/documents/intake"
)

// Required leave inputs: omitting any one fails the snapshot.
var requiredLeaveInputs = []string{
	"worker", "employment", "assignment", "location", "manager",
	"schedule", "calendar", "balances", "benefits",
	"payroll-context", "legal-context", "evidence-usability",
	"source-authority", "freshness", "effective-time", "known-time",
}

// Input statuses: every unsatisfied input keeps its exact status.
const (
	InputReady   = "READY"
	InputPartial = "PARTIAL"
	InputStale   = "STALE"
	InputUnknown = "UNKNOWN"
	InputDenied  = "DENIED"
)

// InputEntry binds one required input to its exact revision, watermark,
// authority and times plus a governed evidence reference. The reference is
// the DOC-INTAKE-001 pipeline's own intake.EvidenceRef, usable only while
// its issuer binding holds and its artifact stays released: a bare string
// cannot satisfy this field. Entries carry references only: evidence
// content stays in its compartment and restricted bytes can never enter
// the snapshot because no entry has a content field.
type InputEntry struct {
	Input       string
	Revision    string
	Watermark   string
	Status      string
	EvidenceRef intake.EvidenceRef
	Authority   string
	Effective   string
	Known       string
}

// LeaveInputSnapshot is the immutable leave intake binding.
type LeaveInputSnapshot struct {
	Entries []InputEntry
	Digest  string
}

func snapshotDigest(entries []InputEntry) string {
	parts := []string{"leave-input-snapshot"}
	for _, entry := range entries {
		ref := entry.EvidenceRef
		parts = append(parts, strings.Join([]string{
			entry.Input, entry.Revision, entry.Watermark, entry.Status,
			ref.ID, ref.ArtifactID, ref.ArtifactDigest, ref.DerivativeDigest, ref.Purpose, ref.IssuedAt.UTC().Format(time.RFC3339Nano),
			entry.Authority, entry.Effective, entry.Known,
		}, "\x01"))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validStatus(status string) bool {
	switch status {
	case InputReady, InputPartial, InputStale, InputUnknown, InputDenied:
		return true
	default:
		return false
	}
}

// BuildSnapshot binds the required inputs into an immutable snapshot.
// Missing inputs, hollow revisions, off-vocabulary statuses and evidence
// references that are not usable governed DOC-INTAKE-001 issues refuse.
func BuildSnapshot(entries []InputEntry) (LeaveInputSnapshot, error) {
	if len(entries) != len(requiredLeaveInputs) {
		return LeaveInputSnapshot{}, fmt.Errorf("leave: snapshot requires exactly %d inputs, got %d", len(requiredLeaveInputs), len(entries))
	}
	seen := make(map[string]bool, len(entries))
	ordered := append([]InputEntry(nil), entries...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Input < ordered[j].Input })
	for _, entry := range ordered {
		if seen[entry.Input] {
			return LeaveInputSnapshot{}, fmt.Errorf("leave: duplicate input %q", entry.Input)
		}
		seen[entry.Input] = true
		if strings.TrimSpace(entry.Revision) == "" || strings.TrimSpace(entry.Watermark) == "" {
			return LeaveInputSnapshot{}, fmt.Errorf("leave: input %q needs an exact revision and watermark", entry.Input)
		}
		if !validStatus(entry.Status) {
			return LeaveInputSnapshot{}, fmt.Errorf("leave: input %q status %q is not input truth", entry.Input, entry.Status)
		}
		if err := entry.EvidenceRef.Check(); err != nil {
			return LeaveInputSnapshot{}, fmt.Errorf("leave: input %q needs a usable governed evidence reference: %w", entry.Input, err)
		}
		if strings.TrimSpace(entry.Authority) == "" {
			return LeaveInputSnapshot{}, fmt.Errorf("leave: input %q needs a source authority", entry.Input)
		}
		if strings.TrimSpace(entry.Effective) == "" || strings.TrimSpace(entry.Known) == "" {
			return LeaveInputSnapshot{}, fmt.Errorf("leave: input %q needs effective and known time", entry.Input)
		}
	}
	for _, required := range requiredLeaveInputs {
		if !seen[required] {
			return LeaveInputSnapshot{}, fmt.Errorf("leave: snapshot omits required input %q", required)
		}
	}
	return LeaveInputSnapshot{Entries: ordered, Digest: snapshotDigest(ordered)}, nil
}

// Satisfied reports whether every required input is READY.
func (s LeaveInputSnapshot) Satisfied() bool {
	if s.Digest == "" {
		return false
	}
	for _, entry := range s.Entries {
		if entry.Status != InputReady {
			return false
		}
	}
	return true
}

// Pending lists every input that remains PARTIAL, STALE, UNKNOWN or DENIED.
func (s LeaveInputSnapshot) Pending() []string {
	var pending []string
	for _, entry := range s.Entries {
		if entry.Status != InputReady {
			pending = append(pending, entry.Input+":"+entry.Status)
		}
	}
	return pending
}

// Verify recomputes the snapshot seal.
func (s LeaveInputSnapshot) Verify() error {
	if s.Digest == "" || snapshotDigest(s.Entries) != s.Digest {
		return fmt.Errorf("leave: input snapshot seal is broken")
	}
	return nil
}
