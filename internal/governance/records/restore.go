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

// Restore acceptance (PRIV-004) reapplies deletion and restriction
// manifests while a backup returns to service. The service stays FENCED
// until the manifest epoch matches the backup epoch: a restore that
// cannot prove its tombstones and holds are current must not resurrect
// deleted or restricted data. Acceptance is idempotent — the same
// envelope always yields the same receipt — and tenant-keyed, so one
// tenant's manifests never gate or open another tenant's restore.
//
// The gate is kernel-pure: it certifies the acceptance a recovery
// operator must then execute, and destroys or serves nothing itself.

// RestoreStatus names the service posture after acceptance.
type RestoreStatus string

// Restore postures.
const (
	RestoreFenced RestoreStatus = "FENCED"
	RestoreOpen   RestoreStatus = "OPEN"
)

// RestrictionManifest bounds one subject to stated purposes until the
// manifest epoch advances past it.
type RestrictionManifest struct {
	SubjectID string   `json:"subject_id"`
	Purposes  []string `json:"purposes"`
	Digest    string   `json:"digest"`
}

// RestoreManifests is the privacy evidence a restore must reapply.
type RestoreManifests struct {
	Epoch        uint64                `json:"epoch"`
	Tombstones   []Tombstone           `json:"tombstones"`
	Restrictions []RestrictionManifest `json:"restrictions"`
	Holds        []DeletionHold        `json:"holds"`
}

// RestoredCopy is one copy the backup returns.
type RestoredCopy struct {
	ID              string   `json:"id"`
	Kind            CopyKind `json:"kind"`
	Tenant          string   `json:"tenant"`
	TombstoneDigest string   `json:"tombstone_digest,omitempty"`
}

// RestoreRequest is the complete restore acceptance envelope.
type RestoreRequest struct {
	RestoreID   string           `json:"restore_id"`
	Tenant      string           `json:"tenant"`
	At          time.Time        `json:"at"`
	BackupEpoch uint64           `json:"backup_epoch"`
	Manifests   RestoreManifests `json:"manifests"`
	Copies      []RestoredCopy   `json:"copies"`
}

// RestoreFinding names one acceptance defect.
type RestoreFinding struct {
	Code   string `json:"code"`
	CopyID string `json:"copy_id"`
	Detail string `json:"detail"`
}

// RestoreReceipt reports the acceptance: posture, reapplied counts, the
// watermark the service may serve from, and the defects keeping it fenced.
type RestoreReceipt struct {
	RestoreID             string           `json:"restore_id"`
	Tenant                string           `json:"tenant"`
	Status                RestoreStatus    `json:"status"`
	ReappliedTombstones   int              `json:"reapplied_tombstones"`
	ReappliedRestrictions int              `json:"reapplied_restrictions"`
	ReappliedHolds        int              `json:"reapplied_holds"`
	Watermark             uint64           `json:"watermark"`
	Findings              []RestoreFinding `json:"findings"`
	Digest                string           `json:"digest"`
}

// Explain returns a bounded summary suitable for an operator log.
func (r RestoreReceipt) Explain() string {
	return fmt.Sprintf("restore acceptance v1 id=%s tenant=%s status=%s tombstones=%d restrictions=%d holds=%d watermark=%d digest=%s",
		r.RestoreID, r.Tenant, r.Status, r.ReappliedTombstones, r.ReappliedRestrictions, r.ReappliedHolds, r.Watermark, r.Digest)
}

// AcceptRestore gates one restore on its privacy manifests. Malformed
// envelopes are errors; epoch drift or unverified copies fence the
// service with findings, never with silent service.
func AcceptRestore(req RestoreRequest) (RestoreReceipt, error) {
	if err := validateRestore(req); err != nil {
		return RestoreReceipt{}, err
	}
	rec := RestoreReceipt{RestoreID: req.RestoreID, Tenant: req.Tenant, Status: RestoreOpen, Watermark: req.BackupEpoch}
	if req.Manifests.Epoch != req.BackupEpoch {
		rec.Status = RestoreFenced
		rec.Findings = append(rec.Findings, RestoreFinding{
			Code: "MANIFEST_EPOCH_DRIFT", Detail: "manifest epoch does not match the backup epoch; tombstones and holds are not current"})
	}
	tombs := map[string]string{}
	for _, t := range req.Manifests.Tombstones {
		tombs[t.CopyID] = t.Digest
	}
	holds := map[string]bool{}
	for _, h := range req.Manifests.Holds {
		holds[h.CopyID] = true
	}
	for _, c := range req.Copies {
		switch {
		case c.Kind == CopyKindRestored:
			digest, ok := tombs[c.ID]
			if !ok || digest == "" || digest != c.TombstoneDigest {
				rec.Status = RestoreFenced
				rec.Findings = append(rec.Findings, RestoreFinding{
					Code: "TOMBSTONE_UNVERIFIED", CopyID: c.ID,
					Detail: "restored copy presents no matching reapplied tombstone"})
				continue
			}
			rec.ReappliedTombstones++
		case holds[c.ID]:
			rec.ReappliedHolds++
		}
	}
	for _, r := range req.Manifests.Restrictions {
		if strings.TrimSpace(r.SubjectID) == "" || len(r.Purposes) == 0 || strings.TrimSpace(r.Digest) == "" {
			rec.Status = RestoreFenced
			rec.Findings = append(rec.Findings, RestoreFinding{
				Code: "RESTRICTION_UNVERIFIABLE", Detail: "restriction manifest lacks subject, purposes or digest"})
			continue
		}
		rec.ReappliedRestrictions++
	}
	sort.Slice(rec.Findings, func(i, j int) bool {
		if rec.Findings[i].Code != rec.Findings[j].Code {
			return rec.Findings[i].Code < rec.Findings[j].Code
		}
		return rec.Findings[i].CopyID < rec.Findings[j].CopyID
	})
	rec.Digest = digestRestore(rec)
	return rec, nil
}

func validateRestore(req RestoreRequest) error {
	switch {
	case strings.TrimSpace(req.RestoreID) == "":
		return fmt.Errorf("records: restore id is required")
	case strings.TrimSpace(req.Tenant) == "":
		return fmt.Errorf("records: tenant is required")
	case req.At.IsZero():
		return fmt.Errorf("records: restore instant is required")
	case req.BackupEpoch == 0:
		return fmt.Errorf("records: backup epoch is required")
	case len(req.Copies) == 0:
		return fmt.Errorf("records: at least one restored copy is required")
	}
	seen := map[string]bool{}
	for i, c := range req.Copies {
		switch {
		case strings.TrimSpace(c.ID) == "":
			return fmt.Errorf("records: restored copy %d has no identity", i)
		case seen[c.ID]:
			return fmt.Errorf("records: restored copy %q listed twice", c.ID)
		case !c.Kind.Valid():
			return fmt.Errorf("records: restored copy %q has unknown class", c.ID)
		case c.Tenant != req.Tenant:
			return fmt.Errorf("records: restored copy %q is outside the restore tenant", c.ID)
		}
		seen[c.ID] = true
	}
	for _, h := range req.Manifests.Holds {
		if strings.TrimSpace(h.ID) == "" || strings.TrimSpace(h.CopyID) == "" ||
			strings.TrimSpace(h.Authority) == "" || strings.TrimSpace(h.Reason) == "" {
			return fmt.Errorf("records: holds need id, copy, authority and reason")
		}
	}
	return nil
}

func digestRestore(rec RestoreReceipt) string {
	rec.Digest = ""
	raw, _ := json.Marshal(rec)
	sum := sha256.Sum256(append([]byte("hcmnext.records.restore/v1\x00"), raw...))
	return "sha256:" + hex.EncodeToString(sum[:])
}
