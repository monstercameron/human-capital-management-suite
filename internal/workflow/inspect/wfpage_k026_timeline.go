package inspect

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/ledger"
)

var ErrPageTimelineInvalid = errors.New("workflow inspect: invalid page timeline")

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
	EventID         string                  `json:"event_id"`
	ActorRef        Ref                     `json:"actor_ref"`
	Operation       ledger.PageUseOperation `json:"operation"`
	WorkflowVersion uint32                  `json:"workflow_version"`
	PageVersion     int64                   `json:"page_version"`
	RecordedAt      string                  `json:"recorded_at"`
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
	Audit          []ledger.PageUseAuditEvent
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
