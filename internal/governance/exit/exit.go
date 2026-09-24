// Package exit models a dry-run tenant exit certification.
//
// The package is deliberately kernel-pure: it consumes inventories and
// evidence supplied by storage, provider and trust adapters, but never calls
// them and never destroys data. Actual irreversible destruction is a later
// lifecycle gate.
package exit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const contractVersion = 1

// Version reports the tenant-exit contract version.
func Version() int { return contractVersion }

// Category identifies a copy or authority-bearing material that must be
// accounted for before an exit can be certified.
type Category string

const (
	CategoryCanonical  Category = "CANONICAL"
	CategoryDerived    Category = "DERIVED"
	CategoryExport     Category = "EXPORT"
	CategoryBackup     Category = "BACKUP"
	CategoryProvider   Category = "PROVIDER"
	CategoryCredential Category = "CREDENTIAL"
	CategoryWebhook    Category = "WEBHOOK"
	CategorySupport    Category = "SUPPORT_GRANT"
	CategoryTelemetry  Category = "TELEMETRY"
	CategorySearch     Category = "SEARCH"
)

// RequiredCategories is the minimum inventory coverage for PRIV-EXIT-001.
var RequiredCategories = []Category{
	CategoryCanonical, CategoryDerived, CategoryExport, CategoryBackup,
	CategoryProvider, CategoryCredential, CategoryWebhook, CategorySupport,
	CategoryTelemetry, CategorySearch,
}

// Copy is a metadata-only inventory entry. A zero-count or empty physical
// store is represented by a valid entry with a stable ID; absence is unknown
// and therefore blocks certification.
type Copy struct {
	ID                    string    `json:"id"`
	Tenant                string    `json:"tenant"`
	Category              Category  `json:"category"`
	Location              string    `json:"location"`
	Owner                 string    `json:"owner"`
	Region                string    `json:"region"`
	KeyRef                string    `json:"key_ref"`
	RetentionPolicy       string    `json:"retention_policy"`
	RestorePolicy         string    `json:"restore_policy"`
	FreshAt               time.Time `json:"fresh_at"`
	Known                 bool      `json:"known"`
	DeletionSupported     bool      `json:"deletion_supported"`
	ActiveAuthority       bool      `json:"active_authority,omitempty"`
	RestoreReDeleteNeeded bool      `json:"restore_redelete_needed,omitempty"`
}

// ExportReceipt proves that the exit export was bounded, addressed to a
// named recipient and verified before any later destruction decision.
type ExportReceipt struct {
	ID             string `json:"id"`
	SchemaVersion  string `json:"schema_version"`
	Checksum       string `json:"checksum"`
	ExpectedDigest string `json:"expected_digest"`
	Recipient      string `json:"recipient"`
	Verified       bool   `json:"verified"`
}

// RevocationReceipt is evidence from a trust or connector adapter that one
// authority-bearing target was shut down. It carries no secret material.
type RevocationReceipt struct {
	ID      string    `json:"id"`
	Target  string    `json:"target"`
	Kind    string    `json:"kind"`
	At      time.Time `json:"at"`
	Success bool      `json:"success"`
}

// PendingWork records the disposition of accepted work that cannot be
// silently dropped during exit.
type PendingWork struct {
	ID          string `json:"id"`
	Disposition string `json:"disposition"`
	Reason      string `json:"reason"`
}

// HoldException records a lawful retention exception without changing the
// original hold or copy inventory.
type HoldException struct {
	ID        string `json:"id"`
	CopyID    string `json:"copy_id"`
	Authority string `json:"authority"`
	Reason    string `json:"reason"`
}

// RestoreReDelete is evidence that restore acceptance will reapply a
// restriction/deletion manifest rather than resurrecting a disposed copy.
type RestoreReDelete struct {
	ID              string `json:"id"`
	CopyID          string `json:"copy_id"`
	TombstoneDigest string `json:"tombstone_digest"`
	Watermark       string `json:"watermark"`
	Reapplied       bool   `json:"reapplied"`
}

// Request is the complete evidence envelope for a dry-run exit.
type Request struct {
	Tenant           string              `json:"tenant"`
	RequestedBy      string              `json:"requested_by"`
	At               time.Time           `json:"at"`
	FreshnessWindow  time.Duration       `json:"freshness_window"`
	Copies           []Copy              `json:"copies"`
	Export           ExportReceipt       `json:"export"`
	ExportPackage    *ExportPackage      `json:"export_package,omitempty"`
	ShutdownComplete bool                `json:"shutdown_complete"`
	Revocations      []RevocationReceipt `json:"revocations"`
	PendingWork      []PendingWork       `json:"pending_work"`
	HoldExceptions   []HoldException     `json:"hold_exceptions"`
	RestoreReDeletes []RestoreReDelete   `json:"restore_redeletes"`
}

// Blocker is a stable, non-sensitive reason certification cannot be issued.
type Blocker struct {
	Code    string `json:"code"`
	Subject string `json:"subject"`
	Detail  string `json:"detail"`
}

// ExitPlan is the immutable result of evaluating a Request.
type ExitPlan struct {
	Tenant           string              `json:"tenant"`
	Status           string              `json:"status"`
	Copies           []Copy              `json:"copies"`
	InventoryDigest  string              `json:"inventory_digest"`
	Export           ExportReceipt       `json:"export"`
	ShutdownComplete bool                `json:"shutdown_complete"`
	Revocations      []RevocationReceipt `json:"revocations"`
	PendingWork      []PendingWork       `json:"pending_work"`
	HoldExceptions   []HoldException     `json:"hold_exceptions"`
	RestoreReDeletes []RestoreReDelete   `json:"restore_redeletes"`
	Blockers         []Blocker           `json:"blockers"`
	Digest           string              `json:"digest"`
}

const (
	StatusCertifiable = "CERTIFIABLE"
	StatusBlocked     = "BLOCKED"
)

var (
	ErrInvalidRequest = errors.New("exit: invalid request")
	ErrIncomplete     = errors.New("exit: incomplete evidence")
)

// Build evaluates a dry-run exit. Missing or stale operational evidence is a
// typed blocker in the returned plan, not an error that can be mistaken for a
// transient execution failure.
func Build(req Request) (ExitPlan, error) {
	if err := validateRequest(req); err != nil {
		return ExitPlan{}, err
	}
	if req.FreshnessWindow <= 0 {
		req.FreshnessWindow = 24 * time.Hour
	}
	entries := cloneCopies(req.Copies)
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	blockers := validateEvidence(req, entries)
	inventoryDigest := digestValue(entries)
	plan := ExitPlan{
		Tenant: req.Tenant, Status: StatusCertifiable, Copies: cloneCopies(entries), InventoryDigest: inventoryDigest,
		Export: req.Export, ShutdownComplete: req.ShutdownComplete,
		Revocations: cloneRevocations(req.Revocations), PendingWork: clonePending(req.PendingWork),
		HoldExceptions: cloneHolds(req.HoldExceptions), RestoreReDeletes: cloneRestore(req.RestoreReDeletes),
		Blockers: blockers,
	}
	if len(blockers) != 0 {
		plan.Status = StatusBlocked
	}
	plan.Digest = digestPlan(plan)
	return plan, nil
}

// Explain returns a bounded summary suitable for an operator log.
func (p ExitPlan) Explain() string {
	return fmt.Sprintf("tenant exit v%d tenant=%s status=%s copies=%d revocations=%d blockers=%d digest=%s", Version(), p.Tenant, p.Status, len(p.Copies), len(p.Revocations), len(p.Blockers), p.Digest)
}

// Explain is the package-level explanation entry point.
func Explain(p ExitPlan) string { return p.Explain() }

func validateRequest(req Request) error {
	if strings.TrimSpace(req.Tenant) == "" || strings.TrimSpace(req.RequestedBy) == "" || req.At.IsZero() {
		return fmt.Errorf("%w: tenant, requester and time are required", ErrInvalidRequest)
	}
	if len(req.Copies) == 0 {
		return fmt.Errorf("%w: copy inventory is required", ErrInvalidRequest)
	}
	return nil
}

func validateEvidence(req Request, entries []Copy) []Blocker {
	var blockers []Blocker
	seen := make(map[string]bool, len(entries))
	covered := make(map[Category]bool)
	for _, c := range entries {
		if c.ID == "" || seen[c.ID] {
			blockers = append(blockers, Blocker{"DUPLICATE_OR_MISSING_COPY", c.ID, "copy identity is not unique"})
			continue
		}
		seen[c.ID] = true
		covered[c.Category] = true
		if c.Tenant != req.Tenant {
			blockers = append(blockers, Blocker{"TENANT_MISMATCH", c.ID, "copy is outside the exit tenant"})
		}
		if c.Location == "" || c.Owner == "" || c.Region == "" || c.KeyRef == "" || c.RetentionPolicy == "" || c.RestorePolicy == "" {
			blockers = append(blockers, Blocker{"COPY_METADATA_INCOMPLETE", c.ID, "owner, location, region, key, retention and restore policy are required"})
		}
		if !c.Known {
			blockers = append(blockers, Blocker{"COPY_UNKNOWN", c.ID, "copy discovery is not verified"})
		}
		if c.FreshAt.IsZero() || req.At.Sub(c.FreshAt) > req.FreshnessWindow || c.FreshAt.After(req.At) {
			blockers = append(blockers, Blocker{"COPY_STALE", c.ID, "copy inventory is outside the declared freshness window"})
		}
		if !c.DeletionSupported {
			blockers = append(blockers, Blocker{"COPY_NO_DELETION_PATH", c.ID, "copy has no verified deletion capability"})
		}
		if c.ActiveAuthority && !hasSuccessfulRevocation(req.Revocations, c.ID) {
			blockers = append(blockers, Blocker{"ACTIVE_AUTHORITY", c.ID, "active credential, webhook, provider or support authority lacks a receipt"})
		}
		if c.RestoreReDeleteNeeded && !hasRestoreEvidence(req.RestoreReDeletes, c.ID) {
			blockers = append(blockers, Blocker{"RESTORE_REDELETE_MISSING", c.ID, "restore re-delete evidence is absent"})
		}
	}
	for _, category := range RequiredCategories {
		if !covered[category] {
			blockers = append(blockers, Blocker{"COPY_CATEGORY_UNKNOWN", string(category), "required copy category is not inventoried"})
		}
	}
	if !req.Export.Verified || req.Export.ID == "" || req.Export.SchemaVersion == "" || req.Export.Checksum == "" || req.Export.ExpectedDigest == "" || req.Export.Checksum != req.Export.ExpectedDigest || req.Export.Recipient == "" {
		blockers = append(blockers, Blocker{"EXPORT_UNVERIFIED", req.Export.ID, "schema, recipient and matching export digest are required"})
	} else if req.ExportPackage == nil || VerifyExportPackage(*req.ExportPackage) != nil || !receiptMatchesPackage(req.Export, *req.ExportPackage) || req.ExportPackage.Manifest.Tenant != req.Tenant || req.ExportPackage.Manifest.InventoryDigest != digestValue(entries) {
		blockers = append(blockers, Blocker{"EXPORT_PACKAGE_UNVERIFIED", req.Export.ID, "export receipt is not bound to a verified package produced from this inventory"})
	}
	if !req.ShutdownComplete {
		blockers = append(blockers, Blocker{"SHUTDOWN_INCOMPLETE", req.Tenant, "connector and tenant shutdown receipts are incomplete"})
	}
	for _, work := range req.PendingWork {
		if work.ID == "" || work.Disposition == "" || work.Reason == "" {
			blockers = append(blockers, Blocker{"PENDING_WORK_UNDISPOSITIONED", work.ID, "pending work needs an explicit disposition and reason"})
		}
	}
	for _, h := range req.HoldExceptions {
		if h.ID == "" || h.CopyID == "" || h.Authority == "" || h.Reason == "" {
			blockers = append(blockers, Blocker{"HOLD_EXCEPTION_UNAUTHORIZED", h.ID, "hold exception needs copy, authority and reason"})
		}
		if !seen[h.CopyID] {
			blockers = append(blockers, Blocker{"HOLD_COPY_UNKNOWN", h.CopyID, "hold exception references an uninventoried copy"})
		}
	}
	for _, rev := range req.Revocations {
		if rev.ID == "" || rev.Target == "" || rev.Kind == "" || rev.At.IsZero() || !rev.Success {
			blockers = append(blockers, Blocker{"REVOCATION_RECEIPT_INVALID", rev.ID, "revocation receipt is incomplete or unsuccessful"})
		}
	}
	for _, evidence := range req.RestoreReDeletes {
		if evidence.ID == "" || evidence.CopyID == "" || evidence.TombstoneDigest == "" || evidence.Watermark == "" || !evidence.Reapplied {
			blockers = append(blockers, Blocker{"RESTORE_REDELETE_INVALID", evidence.ID, "restore re-delete evidence is incomplete"})
		}
	}
	sort.SliceStable(blockers, func(i, j int) bool {
		if blockers[i].Code != blockers[j].Code {
			return blockers[i].Code < blockers[j].Code
		}
		return blockers[i].Subject < blockers[j].Subject
	})
	return blockers
}

func hasSuccessfulRevocation(receipts []RevocationReceipt, target string) bool {
	for _, receipt := range receipts {
		if receipt.Target == target && receipt.Success {
			return true
		}
	}
	return false
}

func hasRestoreEvidence(evidence []RestoreReDelete, copyID string) bool {
	for _, item := range evidence {
		if item.CopyID == copyID && item.Reapplied && item.TombstoneDigest != "" && item.Watermark != "" {
			return true
		}
	}
	return false
}

func digestValue(value any) string {
	b, _ := json.Marshal(value)
	sum := sha256.Sum256(append([]byte("hcmnext.exit/v1\x00"), b...))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func digestPlan(plan ExitPlan) string {
	plan.Digest = ""
	return digestValue(plan)
}

func cloneCopies(in []Copy) []Copy { return append([]Copy(nil), in...) }
func cloneRevocations(in []RevocationReceipt) []RevocationReceipt {
	return append([]RevocationReceipt(nil), in...)
}
func clonePending(in []PendingWork) []PendingWork   { return append([]PendingWork(nil), in...) }
func cloneHolds(in []HoldException) []HoldException { return append([]HoldException(nil), in...) }
func cloneRestore(in []RestoreReDelete) []RestoreReDelete {
	return append([]RestoreReDelete(nil), in...)
}
