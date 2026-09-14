package records

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Verified deletion (MODEL-028) proves a record is gone everywhere it
// should be: canonical, derived, external, backup and restored copies.
// A deletion request names the copy kinds it covers — by default all
// five — and the certificate completes only when every covered copy
// reaches exactly one outcome: destroyed, anonymized, retained under an
// unexpired retention rule, or excepted under a legal hold. Restored
// backups reapply their tombstones before they serve again; a restored
// copy without its tombstone blocks the certificate.
//
// Deduplication is tenant-keyed: a request touches only its own tenant,
// so one tenant can neither preserve nor delete another tenant's bytes.
// The package is kernel-pure and destroys nothing itself; it certifies
// the plan of record a storage adapter must then execute.

// CopyKind names one copy class a deletion must account for.
type CopyKind string

// Deletable copy classes.
const (
	CopyKindCanonical CopyKind = "CANONICAL"
	CopyKindDerived   CopyKind = "DERIVED"
	CopyKindExternal  CopyKind = "EXTERNAL"
	CopyKindBackup    CopyKind = "BACKUP"
	CopyKindRestored  CopyKind = "RESTORED"
)

// Valid reports whether k is a declared copy class.
func (k CopyKind) Valid() bool {
	switch k {
	case CopyKindCanonical, CopyKindDerived, CopyKindExternal, CopyKindBackup, CopyKindRestored:
		return true
	}
	return false
}

// AllCopyKinds is the default coverage: every class must be accounted for.
func AllCopyKinds() []CopyKind {
	return []CopyKind{CopyKindCanonical, CopyKindDerived, CopyKindExternal, CopyKindBackup, CopyKindRestored}
}

// OutcomeKind names one per-copy deletion outcome.
type OutcomeKind string

// Per-copy outcomes.
const (
	OutcomeUnspecified OutcomeKind = "OUTCOME_UNSPECIFIED"
	OutcomeDestroyed   OutcomeKind = "DESTROYED"
	OutcomeAnonymized  OutcomeKind = "ANONYMIZED"
	OutcomeRetained    OutcomeKind = "RETAINED"
	OutcomeException   OutcomeKind = "EXCEPTION"
)

// DeletableCopy is one copy in scope for deletion.
type DeletableCopy struct {
	ID        string   `json:"id"`
	Kind      CopyKind `json:"kind"`
	Tenant    string   `json:"tenant"`
	Anonymize bool     `json:"anonymize"`
	// RetainUntil keeps the copy under an unexpired retention rule.
	RetainUntil *time.Time `json:"retain_until,omitempty"`
	// ReDeleted with ReDeleteRef proves the backup was deleted again
	// after the primary destruction.
	ReDeleted   bool   `json:"re_deleted,omitempty"`
	ReDeleteRef string `json:"re_delete_ref,omitempty"`
	// TombstoneDigest is the tombstone a restored copy must present.
	TombstoneDigest string `json:"tombstone_digest,omitempty"`
}

// Tombstone is one reapplied deletion marker.
type Tombstone struct {
	CopyID string `json:"copy_id"`
	Digest string `json:"digest"`
}

// DeletionHold is one legal hold excepting a copy from destruction.
type DeletionHold struct {
	ID        string `json:"id"`
	CopyID    string `json:"copy_id"`
	Authority string `json:"authority"`
	Reason    string `json:"reason"`
}

// DeletionRequest is the complete deletion envelope. ExpectKinds defaults
// to every copy class when nil: deletion that does not say what it covers
// covers nothing certifiable.
type DeletionRequest struct {
	DeletionID  string          `json:"deletion_id"`
	Tenant      string          `json:"tenant"`
	RecordID    string          `json:"record_id"`
	RequestedBy string          `json:"requested_by"`
	At          time.Time       `json:"at"`
	ExpectKinds []CopyKind      `json:"expect_kinds,omitempty"`
	Copies      []DeletableCopy `json:"copies"`
	Tombstones  []Tombstone     `json:"tombstones"`
	Holds       []DeletionHold  `json:"holds"`
}

// CopyOutcome is one copy's certified outcome.
type CopyOutcome struct {
	CopyID  string      `json:"copy_id"`
	Kind    CopyKind    `json:"kind"`
	Outcome OutcomeKind `json:"outcome"`
	Detail  string      `json:"detail"`
}

// DeletionCertificate is the signed-by-digest completion record. SignerID
// names the privacy officer or system that executed it; the digest pins
// every outcome so the certificate cannot be edited after the fact.
type DeletionCertificate struct {
	DeletionID string        `json:"deletion_id"`
	Tenant     string        `json:"tenant"`
	RecordID   string        `json:"record_id"`
	SignerID   string        `json:"signer_id"`
	At         time.Time     `json:"at"`
	Outcomes   []CopyOutcome `json:"outcomes"`
	Complete   bool          `json:"complete"`
	Digest     string        `json:"digest"`
}

// Explain returns a bounded summary suitable for an operator log.
func (c DeletionCertificate) Explain() string {
	return fmt.Sprintf("verified deletion v1 id=%s tenant=%s record=%s complete=%t outcomes=%d digest=%s",
		c.DeletionID, c.Tenant, c.RecordID, c.Complete, len(c.Outcomes), c.Digest)
}

// ExecuteDeletion certifies one deletion envelope. Malformed envelopes
// are errors; incomplete coverage is an incomplete certificate, never a
// completion over the copies that happened to be convenient.
func ExecuteDeletion(req DeletionRequest) (DeletionCertificate, error) {
	if err := validateDeletion(req); err != nil {
		return DeletionCertificate{}, err
	}
	expect := req.ExpectKinds
	if expect == nil {
		expect = AllCopyKinds()
	}
	holds := map[string]DeletionHold{}
	for _, h := range req.Holds {
		holds[h.CopyID] = h
	}
	tombs := map[string]string{}
	for _, t := range req.Tombstones {
		tombs[t.CopyID] = t.Digest
	}
	covered := map[CopyKind]bool{}
	var outcomes []CopyOutcome
	complete := true
	for _, c := range req.Copies {
		covered[c.Kind] = true
		outcome := decideCopy(req, c, holds, tombs)
		outcomes = append(outcomes, outcome)
		if outcome.Outcome != OutcomeDestroyed && outcome.Outcome != OutcomeAnonymized &&
			outcome.Outcome != OutcomeRetained && outcome.Outcome != OutcomeException {
			complete = false
		}
		if outcome.Outcome == OutcomeUnspecified {
			complete = false
		}
	}
	for _, kind := range expect {
		if !covered[kind] {
			return DeletionCertificate{}, fmt.Errorf("records: copy class %q has no accounted copy", kind)
		}
	}
	sort.Slice(outcomes, func(i, j int) bool {
		if outcomes[i].CopyID != outcomes[j].CopyID {
			return outcomes[i].CopyID < outcomes[j].CopyID
		}
		return outcomes[i].Kind < outcomes[j].Kind
	})
	cert := DeletionCertificate{
		DeletionID: req.DeletionID, Tenant: req.Tenant, RecordID: req.RecordID,
		SignerID: req.RequestedBy, At: req.At, Outcomes: outcomes, Complete: complete,
	}
	cert.Digest = digestDeletion(cert)
	return cert, nil
}

func decideCopy(req DeletionRequest, c DeletableCopy, holds map[string]DeletionHold, tombs map[string]string) CopyOutcome {
	if h, ok := holds[c.ID]; ok {
		return CopyOutcome{CopyID: c.ID, Kind: c.Kind, Outcome: OutcomeException,
			Detail: fmt.Sprintf("legal hold %s by %s: %s", h.ID, h.Authority, h.Reason)}
	}
	switch c.Kind {
	case CopyKindBackup:
		if !c.ReDeleted || strings.TrimSpace(c.ReDeleteRef) == "" {
			return CopyOutcome{CopyID: c.ID, Kind: c.Kind, Outcome: OutcomeUnspecified,
				Detail: "backup requires re-delete evidence after the primary destruction"}
		}
		return CopyOutcome{CopyID: c.ID, Kind: c.Kind, Outcome: OutcomeDestroyed, Detail: "backup re-deleted under " + c.ReDeleteRef}
	case CopyKindRestored:
		digest, ok := tombs[c.ID]
		if !ok || digest == "" || digest != c.TombstoneDigest {
			return CopyOutcome{CopyID: c.ID, Kind: c.Kind, Outcome: OutcomeUnspecified,
				Detail: "restored copy serves only behind its reapplied tombstone"}
		}
		if c.Anonymize {
			return CopyOutcome{CopyID: c.ID, Kind: c.Kind, Outcome: OutcomeAnonymized, Detail: "restored copy anonymized under tombstone " + digest}
		}
		return CopyOutcome{CopyID: c.ID, Kind: c.Kind, Outcome: OutcomeDestroyed, Detail: "restored copy destroyed under tombstone " + digest}
	}
	if c.RetainUntil != nil && c.RetainUntil.After(req.At) {
		return CopyOutcome{CopyID: c.ID, Kind: c.Kind, Outcome: OutcomeRetained,
			Detail: "retention rule holds the copy past the deletion instant"}
	}
	if c.Anonymize {
		return CopyOutcome{CopyID: c.ID, Kind: c.Kind, Outcome: OutcomeAnonymized, Detail: "copy anonymized"}
	}
	return CopyOutcome{CopyID: c.ID, Kind: c.Kind, Outcome: OutcomeDestroyed, Detail: "copy destroyed"}
}

func validateDeletion(req DeletionRequest) error {
	switch {
	case strings.TrimSpace(req.DeletionID) == "":
		return fmt.Errorf("records: deletion id is required")
	case strings.TrimSpace(req.Tenant) == "":
		return fmt.Errorf("records: tenant is required")
	case strings.TrimSpace(req.RecordID) == "":
		return fmt.Errorf("records: record id is required")
	case strings.TrimSpace(req.RequestedBy) == "":
		return fmt.Errorf("records: requester is required")
	case req.At.IsZero():
		return fmt.Errorf("records: deletion instant is required")
	case len(req.Copies) == 0:
		return fmt.Errorf("records: at least one copy is required")
	}
	seen := map[string]bool{}
	for i, c := range req.Copies {
		switch {
		case strings.TrimSpace(c.ID) == "":
			return fmt.Errorf("records: copy %d has no identity", i)
		case seen[c.ID]:
			return fmt.Errorf("records: copy %q listed twice", c.ID)
		case !c.Kind.Valid():
			return fmt.Errorf("records: copy %q has unknown class", c.ID)
		case c.Tenant != req.Tenant:
			return fmt.Errorf("records: copy %q is outside the deletion tenant", c.ID)
		}
		seen[c.ID] = true
	}
	for _, kind := range req.ExpectKinds {
		if !kind.Valid() {
			return fmt.Errorf("records: expected class %q is unknown", kind)
		}
	}
	seenTomb := map[string]bool{}
	for _, t := range req.Tombstones {
		if strings.TrimSpace(t.CopyID) == "" || strings.TrimSpace(t.Digest) == "" {
			return fmt.Errorf("records: tombstones need copy and digest")
		}
		if seenTomb[t.CopyID] {
			return fmt.Errorf("records: copy %q carries two tombstones", t.CopyID)
		}
		seenTomb[t.CopyID] = true
	}
	for _, h := range req.Holds {
		if strings.TrimSpace(h.ID) == "" || strings.TrimSpace(h.CopyID) == "" ||
			strings.TrimSpace(h.Authority) == "" || strings.TrimSpace(h.Reason) == "" {
			return fmt.Errorf("records: holds need id, copy, authority and reason")
		}
		if !seen[h.CopyID] {
			return fmt.Errorf("records: hold %q references an unaccounted copy", h.ID)
		}
	}
	return nil
}

func digestDeletion(cert DeletionCertificate) string {
	cert.Digest = ""
	raw, _ := json.Marshal(cert)
	sum := sha256.Sum256(append([]byte("hcmnext.records.deletion/v1\x00"), raw...))
	return "sha256:" + hex.EncodeToString(sum[:])
}
