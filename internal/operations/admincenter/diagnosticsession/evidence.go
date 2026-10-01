package diagnosticsession

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

const (
	MaxEvidenceBytes = 1 << 20
	MaxEvidenceCPU   = 5 * time.Second
)

// Evidence is the only supported export shape: redacted payload plus enough
// provenance to correlate a case without exposing credentials or raw data.
type Evidence struct {
	ID            string
	SessionCaseID string
	Kind          string
	CapturedAt    time.Time
	Digest        string
	Payload       any
	Redacted      bool
}

type AuditEvent struct {
	SessionCaseID string
	Action        UIAction
	Resource      string
	OccurredAt    time.Time
	EvidenceID    string
	Redacted      bool
}

func PrepareEvidence(e Evidence) Evidence {
	e.Payload = Redact(e.Payload)
	e.Redacted = true
	// A redaction marshal failure or an oversized payload must never leave a
	// caller-supplied digest behind: clear it before attempting to derive a
	// fresh one over the redacted bytes.
	e.Digest = ""
	if b, err := json.Marshal(e.Payload); err == nil && len(b) <= MaxEvidenceBytes {
		e.Digest = fmt.Sprintf("sha256:%x", sha256.Sum256(b))
	}
	return e
}

type RegistryEntry struct {
	ID, Version, Purpose string
	MaxTTL               time.Duration
	Actions              []UIAction
	RedactionRequired    bool
	MaxEvidenceBytes     int
	MaxEvidenceCPU       time.Duration
	AuditRequired        bool
}

func Registry() RegistryEntry {
	return RegistryEntry{ID: RegistryID, Version: ContractVersion, Purpose: PurposeSupport, MaxTTL: MaxTTL, Actions: []UIAction{ActionViewSummary, ActionViewTimeline, ActionViewEvidence, ActionCopyRedactedReference}, RedactionRequired: true, MaxEvidenceBytes: MaxEvidenceBytes, MaxEvidenceCPU: MaxEvidenceCPU, AuditRequired: true}
}
