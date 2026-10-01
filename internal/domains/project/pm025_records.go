package project

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/records"
)

// RecordSeries is the project-owned records inventory vocabulary. The
// project module inventories these records, while records management remains
// authoritative for retention and verified disposal.
type RecordSeries string

const (
	RecordSeriesConfiguration RecordSeries = "project.configuration"
	RecordSeriesTasks         RecordSeries = "project.tasks"
	RecordSeriesMembership    RecordSeries = "project.membership"
	RecordSeriesComments      RecordSeries = "project.comments"
	RecordSeriesAIEvidence    RecordSeries = "project.ai_evidence"
	RecordSeriesAttachments   RecordSeries = "project.attachments"
	RecordSeriesExports       RecordSeries = "project.exports"
)

var requiredRecordSeries = []RecordSeries{
	RecordSeriesConfiguration,
	RecordSeriesTasks,
	RecordSeriesMembership,
	RecordSeriesComments,
	RecordSeriesAIEvidence,
	RecordSeriesAttachments,
	RecordSeriesExports,
}

var (
	ErrInvalidRecordsInventory = errors.New("project: invalid records inventory")
	ErrRecordNotFound          = errors.New("project: record not found")
	ErrRecordUnauthorized      = errors.New("project: record requester is not authorized")
)

// RecordVersion identifies a retained project version. A comment tombstone
// is a version, not permission to erase the preceding held version.
type RecordVersion struct {
	ID        string
	Revision  uint64
	Tombstone bool
}

// Record is metadata only; payload bytes remain in their owning stores. The
// Readers list is the current project authorization projection used by
// exports, so an inaccessible task cannot leak through a records export.
type Record struct {
	ID        string
	TenantID  string
	ProjectID string
	Series    RecordSeries
	Readers   []string
	Versions  []RecordVersion
	Copies    []records.DeletableCopy
}

// Hold keeps all versions of a project record under records authority until
// the hold is released. Version IDs are explicit to make an export or
// disposal receipt auditable without reading protected payloads.
type Hold struct {
	ID         string
	TenantID   string
	ProjectID  string
	RecordID   string
	VersionIDs []string
	Authority  string
	Reason     string
	Active     bool
}

// Inventory is the project module's records projection. CoveredSeries must
// include every required series, including an empty series, so absence is
// distinguishable from an inventory that forgot a category.
type Inventory struct {
	TenantID      string
	ProjectID     string
	CoveredSeries []RecordSeries
	Records       []Record
	Holds         []Hold
}

// ExportRequest describes the current authorization context. This contract
// intentionally returns metadata only; owning stores provide authorized
// fields and bytes separately.
type ExportRequest struct {
	TenantID  string
	ProjectID string
	Requester string
}

type ExportRecord struct {
	ID       string
	Series   RecordSeries
	Versions []RecordVersion
}

type Export struct {
	TenantID  string
	ProjectID string
	Records   []ExportRecord
	Digest    string
}

type DispositionStatus string

const (
	DispositionVerified DispositionStatus = "VERIFIED_DISPOSAL"
	DispositionHeld     DispositionStatus = "HELD"
)

type DispositionResult struct {
	RecordID        string
	Status          DispositionStatus
	RetainedVersion []string
	Certificate     records.DeletionCertificate
}

func RequiredRecordSeries() []RecordSeries {
	return append([]RecordSeries(nil), requiredRecordSeries...)
}

func (i Inventory) Validate() error {
	if strings.TrimSpace(i.TenantID) == "" || strings.TrimSpace(i.ProjectID) == "" {
		return fmt.Errorf("%w: tenant and project are required", ErrInvalidRecordsInventory)
	}
	covered := make(map[RecordSeries]bool, len(i.CoveredSeries))
	for _, series := range i.CoveredSeries {
		if !validRecordSeries(series) || covered[series] {
			return fmt.Errorf("%w: duplicate or unknown series %q", ErrInvalidRecordsInventory, series)
		}
		covered[series] = true
	}
	for _, series := range requiredRecordSeries {
		if !covered[series] {
			return fmt.Errorf("%w: uncovered series %q", ErrInvalidRecordsInventory, series)
		}
	}
	recordsByID := make(map[string]Record, len(i.Records))
	for _, record := range i.Records {
		if strings.TrimSpace(record.ID) == "" || record.TenantID != i.TenantID || record.ProjectID != i.ProjectID || !validRecordSeries(record.Series) {
			return fmt.Errorf("%w: record %q is outside the project inventory", ErrInvalidRecordsInventory, record.ID)
		}
		if _, exists := recordsByID[record.ID]; exists {
			return fmt.Errorf("%w: duplicate record %q", ErrInvalidRecordsInventory, record.ID)
		}
		recordsByID[record.ID] = record
		versions := make(map[string]bool, len(record.Versions))
		for _, version := range record.Versions {
			if strings.TrimSpace(version.ID) == "" || version.Revision == 0 || versions[version.ID] {
				return fmt.Errorf("%w: invalid version for record %q", ErrInvalidRecordsInventory, record.ID)
			}
			versions[version.ID] = true
		}
		for _, copy := range record.Copies {
			if copy.Tenant != i.TenantID || strings.TrimSpace(copy.ID) == "" || !copy.Kind.Valid() {
				return fmt.Errorf("%w: invalid copy for record %q", ErrInvalidRecordsInventory, record.ID)
			}
		}
	}
	for _, hold := range i.Holds {
		if strings.TrimSpace(hold.ID) == "" || hold.TenantID != i.TenantID || hold.ProjectID != i.ProjectID || strings.TrimSpace(hold.Authority) == "" || strings.TrimSpace(hold.Reason) == "" {
			return fmt.Errorf("%w: incomplete hold %q", ErrInvalidRecordsInventory, hold.ID)
		}
		record, ok := recordsByID[hold.RecordID]
		if !ok {
			return fmt.Errorf("%w: hold %q references unknown record", ErrInvalidRecordsInventory, hold.ID)
		}
		for _, versionID := range hold.VersionIDs {
			if !slices.ContainsFunc(record.Versions, func(version RecordVersion) bool { return version.ID == versionID }) {
				return fmt.Errorf("%w: hold %q references unknown version", ErrInvalidRecordsInventory, hold.ID)
			}
		}
	}
	return nil
}

// Export returns only records currently readable by the requester. It is
// deterministic and does not include hidden task IDs, counts, or payloads.
func (i Inventory) Export(req ExportRequest) (Export, error) {
	if err := i.Validate(); err != nil {
		return Export{}, err
	}
	if req.TenantID != i.TenantID || req.ProjectID != i.ProjectID || strings.TrimSpace(req.Requester) == "" {
		return Export{}, ErrRecordUnauthorized
	}
	out := Export{TenantID: i.TenantID, ProjectID: i.ProjectID}
	for _, record := range i.Records {
		if !slices.Contains(record.Readers, req.Requester) {
			continue
		}
		out.Records = append(out.Records, ExportRecord{ID: record.ID, Series: record.Series, Versions: append([]RecordVersion(nil), record.Versions...)})
	}
	sort.Slice(out.Records, func(a, b int) bool { return out.Records[a].ID < out.Records[b].ID })
	out.Digest = digestExport(out)
	return out, nil
}

// VerifyDisposition delegates copy-level deletion proof to the existing
// records authority. A held record receives only legal-hold exceptions; no
// held version is reported as destroyed.
func (i Inventory) VerifyDisposition(recordID, requestedBy string, at time.Time) (DispositionResult, error) {
	if err := i.Validate(); err != nil {
		return DispositionResult{}, err
	}
	if strings.TrimSpace(requestedBy) == "" || at.IsZero() {
		return DispositionResult{}, fmt.Errorf("%w: requester and disposition time are required", ErrInvalidRecordsInventory)
	}
	var record Record
	found := false
	for _, candidate := range i.Records {
		if candidate.ID == recordID {
			record, found = candidate, true
			break
		}
	}
	if !found {
		return DispositionResult{}, ErrRecordNotFound
	}
	activeHold := false
	retained := []string{}
	deletionHolds := make([]records.DeletionHold, 0)
	tombstones := make([]records.Tombstone, 0)
	for _, copy := range record.Copies {
		if copy.Kind == records.CopyKindRestored && copy.TombstoneDigest != "" {
			tombstones = append(tombstones, records.Tombstone{CopyID: copy.ID, Digest: copy.TombstoneDigest})
		}
	}
	for _, hold := range i.Holds {
		if hold.RecordID != recordID || !hold.Active {
			continue
		}
		activeHold = true
		retained = append(retained, hold.VersionIDs...)
		for _, copy := range record.Copies {
			deletionHolds = append(deletionHolds, records.DeletionHold{ID: hold.ID, CopyID: copy.ID, Authority: hold.Authority, Reason: hold.Reason})
		}
	}
	certificate, err := records.ExecuteDeletion(records.DeletionRequest{
		DeletionID:  "project-disposition:" + recordID,
		Tenant:      i.TenantID,
		RecordID:    recordID,
		RequestedBy: requestedBy,
		At:          at,
		Copies:      record.Copies,
		Tombstones:  tombstones,
		Holds:       deletionHolds,
	})
	if err != nil {
		return DispositionResult{}, err
	}
	status := DispositionVerified
	if activeHold {
		status = DispositionHeld
		sort.Strings(retained)
	}
	return DispositionResult{RecordID: recordID, Status: status, RetainedVersion: retained, Certificate: certificate}, nil
}

func validRecordSeries(series RecordSeries) bool {
	return slices.Contains(requiredRecordSeries, series)
}

func digestExport(value Export) string {
	value.Digest = ""
	raw, _ := json.Marshal(value)
	digest := sha256.Sum256(append([]byte("hcmnext.project.export/v1\x00"), raw...))
	return "sha256:" + hex.EncodeToString(digest[:])
}
