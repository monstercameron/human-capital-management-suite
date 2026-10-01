package ledger

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// PageUseAuditSchema is the stable event shape shared by page submit, view,
// and repair. Keeping the page identity in every event makes an audit reader
// independent of the current workflow or page catalog.
const PageUseAuditSchema = "hcmnext.audit.workflow-page-use/v1"

type PageUseOperation string

const (
	PageUseSubmit PageUseOperation = "SUBMIT"
	PageUseView   PageUseOperation = "VIEW"
	PageUseRepair PageUseOperation = "REPAIR"
)

// PageUseAuditEvent is intentionally plain data so it can be encoded into a
// ledger payload by any transport. ActorRef is an already-authorized audit
// principal reference; it is not a display name.
type PageUseAuditEvent struct {
	Schema          string           `json:"schema"`
	EventID         string           `json:"event_id"`
	TenantID        string           `json:"tenant_id"`
	RunID           string           `json:"run_id"`
	ActorRef        string           `json:"actor_ref"`
	WorkflowID      string           `json:"workflow_id"`
	WorkflowVersion uint32           `json:"workflow_version"`
	PageID          string           `json:"page_id"`
	PageVersion     int64            `json:"page_version"`
	Operation       PageUseOperation `json:"operation"`
	RulesDigest     string           `json:"rules_digest,omitempty"`
	SOPVersions     []string         `json:"sop_versions,omitempty"`
	RecordedAt      time.Time        `json:"recorded_at"`
}

var ErrPageUseAuditInvalid = errors.New("ledger: invalid workflow page-use audit event")

// Validate prevents an event from describing a page different from the run
// it claims to explain. The ledger still supplies tenant isolation; this
// value-level check protects callers that build payloads before appending.
func (event PageUseAuditEvent) Validate() error {
	if strings.TrimSpace(event.Schema) != PageUseAuditSchema {
		return fmt.Errorf("%w: schema must be %s", ErrPageUseAuditInvalid, PageUseAuditSchema)
	}
	for name, value := range map[string]string{
		"event_id": event.EventID, "tenant_id": event.TenantID, "run_id": event.RunID,
		"actor_ref": event.ActorRef, "workflow_id": event.WorkflowID, "page_id": event.PageID,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrPageUseAuditInvalid, name)
		}
	}
	if event.WorkflowVersion == 0 || event.PageVersion < 1 {
		return fmt.Errorf("%w: workflow and page versions must be positive", ErrPageUseAuditInvalid)
	}
	switch event.Operation {
	case PageUseSubmit, PageUseView, PageUseRepair:
	default:
		return fmt.Errorf("%w: unsupported operation %q", ErrPageUseAuditInvalid, event.Operation)
	}
	if event.RecordedAt.IsZero() {
		return fmt.Errorf("%w: recorded_at is required", ErrPageUseAuditInvalid)
	}
	return nil
}

// Canonical returns deterministic payload bytes suitable for a ledger
// assertion. A copy is made before sorting so callers retain ownership of the
// event value they supplied.
func (event PageUseAuditEvent) Canonical() ([]byte, error) {
	copyEvent := event
	copyEvent.Schema = PageUseAuditSchema
	copyEvent.RecordedAt = copyEvent.RecordedAt.UTC()
	copyEvent.SOPVersions = append([]string(nil), event.SOPVersions...)
	for i := range copyEvent.SOPVersions {
		copyEvent.SOPVersions[i] = strings.TrimSpace(copyEvent.SOPVersions[i])
	}
	return json.Marshal(copyEvent)
}

func PageUseAuditStreamKey(tenantID, runID string) (string, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(runID) == "" {
		return "", fmt.Errorf("%w: tenant and run are required for a stream", ErrPageUseAuditInvalid)
	}
	return "workflow-page-use/" + strings.TrimSpace(tenantID) + "/" + strings.TrimSpace(runID), nil
}
