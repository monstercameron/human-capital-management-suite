package inspect

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var ErrPageTimelineInvalid = errors.New("workflow inspect: invalid page timeline")

// PageUseAuditSchema mirrors ledger.PageUseAuditSchema: the stable wire value
// identifying workflow page-use audit events. This package must not import the
// business ledger (WF-RUN-000 NN1 keeps the ledger outside the engine's own
// history), so the audit vocabulary the timeline view reads is declared here.
// The canonical event definition and its ledger-side validation remain the
// ledger package's; this mirror carries only the fields the view reads and
// checks, with identical wire values.
const PageUseAuditSchema = "hcmnext.audit.workflow-page-use/v1"

// PageUseOperation mirrors PageUseOperation.
type PageUseOperation string

const (
	PageUseSubmit PageUseOperation = "SUBMIT"
	PageUseView   PageUseOperation = "VIEW"
	PageUseRepair PageUseOperation = "REPAIR"
)

// PageUseAuditEvent mirrors the ledger page-use audit event fields this view
// consumes: identity, page binding, operation and record time. A caller holding
// ledger events maps them onto this shape before building the timeline.
type PageUseAuditEvent struct {
	Schema          string
	EventID         string
	TenantID        string
	RunID           string
	ActorRef        string
	WorkflowID      string
	WorkflowVersion uint32
	PageID          string
	PageVersion     int64
	Operation       PageUseOperation
	RecordedAt      time.Time
}

var ErrPageUseAuditInvalid = errors.New("workflow inspect: invalid workflow page-use audit event")

// Validate mirrors the ledger event's value-level checks so a malformed
// audit entry is rejected before it can shape the timeline.
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

// PageVersionRef is the exact identity used when a history row is reopened.
// There is deliberately no latest-version fallback.
type PageVersionRef struct {
	WorkflowID      string `json:"workflow_id"`
	WorkflowVersion uint32 `json:"workflow_version"`
	PageID          string `json:"page_id"`
	PageVersion     int64  `json:"page_version"`
}

type PageFieldView struct {
	ID    string `json:"id"`
	Value string `json:"value,omitempty"`
}

type PageTimelineAuditView struct {
	EventID         string           `json:"event_id"`
	ActorRef        Ref              `json:"actor_ref"`
	Operation       PageUseOperation `json:"operation"`
	WorkflowVersion uint32           `json:"workflow_version"`
	PageVersion     int64            `json:"page_version"`
	RecordedAt      string           `json:"recorded_at"`
}

type PageUseTimeline struct {
	RunID       string                  `json:"run_id"`
	Page        PageVersionRef          `json:"page"`
	Fields      []PageFieldView         `json:"fields"`
	Rules       []string                `json:"rules"`
	SOPVersions []string                `json:"sop_versions"`
	Audit       []PageTimelineAuditView `json:"audit"`
}

type PageUseTimelineRequest struct {
	RunID          string
	Page           PageVersionRef
	Fields         map[string]string
	ReadableFields map[string]bool
	Rules          []string
	SOPVersions    []string
	Audit          []PageUseAuditEvent
}

// BuildPageUseTimeline composes the exact page projection used by a run. The
// caller supplies already-authorized field visibility; denied fields are
// omitted rather than represented by their protected value.
func BuildPageUseTimeline(request PageUseTimelineRequest) (PageUseTimeline, error) {
	if strings.TrimSpace(request.RunID) == "" || strings.TrimSpace(request.Page.WorkflowID) == "" || request.Page.WorkflowVersion == 0 || strings.TrimSpace(request.Page.PageID) == "" || request.Page.PageVersion < 1 {
		return PageUseTimeline{}, fmt.Errorf("%w: run and page identity are required", ErrPageTimelineInvalid)
	}
	result := PageUseTimeline{RunID: request.RunID, Page: request.Page, Rules: sortedNonEmpty(request.Rules), SOPVersions: sortedNonEmpty(request.SOPVersions)}
	for id, value := range request.Fields {
		if !request.ReadableFields[id] || strings.TrimSpace(id) == "" {
			continue
		}
		result.Fields = append(result.Fields, PageFieldView{ID: id, Value: value})
	}
	sort.Slice(result.Fields, func(i, j int) bool { return result.Fields[i].ID < result.Fields[j].ID })
	for _, event := range request.Audit {
		if err := event.Validate(); err != nil {
			return PageUseTimeline{}, err
		}
		if event.RunID != request.RunID || event.WorkflowID != request.Page.WorkflowID || event.WorkflowVersion != request.Page.WorkflowVersion || event.PageID != request.Page.PageID || event.PageVersion != request.Page.PageVersion {
			return PageUseTimeline{}, fmt.Errorf("%w: audit event %s is not for the recorded page", ErrPageTimelineInvalid, event.EventID)
		}
		result.Audit = append(result.Audit, PageTimelineAuditView{EventID: event.EventID, ActorRef: RefValue(event.ActorRef), Operation: event.Operation, WorkflowVersion: event.WorkflowVersion, PageVersion: event.PageVersion, RecordedAt: event.RecordedAt.UTC().Format("2006-01-02T15:04:05Z07:00")})
	}
	sort.SliceStable(result.Audit, func(i, j int) bool { return result.Audit[i].RecordedAt < result.Audit[j].RecordedAt })
	return result, nil
}

func sortedNonEmpty(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}
