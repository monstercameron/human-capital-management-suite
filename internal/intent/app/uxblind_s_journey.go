package app

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	intentapproval "github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	stepsapproval "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/approval"
)

const (
	uxblindSAssignedTo = "assigned_to:"
	uxblindSReason     = "decision_reason:"
)

// uxblindSLoadJourneyDecisions reads the immutable work-item decision records
// that explain completed approvals. A transition says that an item changed
// state; the decision record says who decided, which outcome they chose, and
// why. Keeping this read beside the durable workflow read prevents history
// from reconstructing an approval from a step transition.
func uxblindSLoadJourneyDecisions(ctx context.Context, tx workitem.Executor, tenantID, instanceID uuid.UUID) ([]workitem.DecisionRecord, error) {
	return workitem.LoadDecisionsForInstance(ctx, tx, tenantID, instanceID)
}

// uxblindSApplyJourneyDecisionHistory enriches already-composed business
// history. The marker values are private server-to-client presentation
// protocol; the journey client localizes them and resolves the principal name.
func uxblindSApplyJourneyDecisionHistory(detail *workspace.JourneyDetail, items []workitem.WorkItem, records []workitem.DecisionRecord) {
	if detail == nil {
		return
	}
	itemsByID := make(map[string]workitem.WorkItem, len(items))
	for _, item := range items {
		itemsByID[item.WorkItemID.String()] = item
	}
	decisions := make(map[string]workitem.DecisionRecord, len(records))
	for _, record := range records {
		decisions[record.WorkItemID.String()] = record
	}
	for i := range detail.Timeline {
		event := &detail.Timeline[i]
		if event.Kind != JourneyEventWorkItem {
			continue
		}
		item, ok := itemsByID[event.Ref]
		if !ok {
			continue
		}
		if strings.HasSuffix(event.Title, " assigned") && item.Assignment.ChosenOwner != "" {
			event.Detail = uxblindSAssignedTo + item.Assignment.ChosenOwner
		}
		if !strings.HasSuffix(event.Title, " completed") {
			continue
		}
		record, ok := decisions[item.WorkItemID.String()]
		if !ok {
			continue
		}
		decision, err := stepsapproval.DecisionFromRecord(record)
		if err != nil {
			continue
		}
		uxblindSApplyDecision(event, decision, record.DecidedBy)
	}
}

func uxblindSApplyDecision(event *workspace.JourneyEvent, decision intentapproval.ApprovalDecision, fallbackActor string) {
	if event == nil {
		return
	}
	base := strings.TrimSuffix(event.Title, " completed")
	if decision.Outcome == "APPROVED" {
		event.Title = base + " approved"
	} else if decision.Outcome == "REJECTED" {
		event.Title = base + " rejected"
	}
	if decision.Approver.PrincipalID != "" {
		event.Actor = decision.Approver.PrincipalID
	} else if fallbackActor != "" {
		event.Actor = fallbackActor
	}
	if strings.TrimSpace(decision.Reason) != "" {
		event.Detail = uxblindSReason + decision.Reason
	}
}

// uxblindSResolveHistoryMarkers replaces the private history markers with a
// display-safe principal reference. The client still owns localized labels;
// the app owns whether an authorized directory name is available.
func uxblindSResolveHistoryMarkers(detail *workspace.JourneyDetail, resolveName func(string) string) {
	if detail == nil {
		return
	}
	for i := range detail.Timeline {
		event := &detail.Timeline[i]
		if !strings.HasPrefix(event.Detail, uxblindSAssignedTo) {
			continue
		}
		principal := strings.TrimPrefix(event.Detail, uxblindSAssignedTo)
		name := "reviewer"
		if resolveName != nil {
			if resolved := strings.TrimSpace(resolveName(principal)); resolved != "" {
				name = resolved
			}
		}
		event.Detail = uxblindSAssignedTo + name
	}
}

// uxblindSOrderJourneyNodes is the diagnostics projection's stable execution
// order. The storage reader is intentionally allowed to use an indexed node
// id order; support readers need the run order, so the projection orders by
// the node's own start instant and uses recorded time and id as tie-breakers.
func uxblindSOrderJourneyNodes(nodes []workspace.JourneyNode) []workspace.JourneyNode {
	ordered := append([]workspace.JourneyNode(nil), nodes...)
	start := func(node workspace.JourneyNode) time.Time {
		if node.StartedAt != nil {
			return node.StartedAt.UTC()
		}
		return node.RecordedAt.UTC()
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		left, right := start(ordered[i]), start(ordered[j])
		if left.Equal(right) {
			if ordered[i].NodeID == ordered[j].NodeID {
				return ordered[i].Attempt < ordered[j].Attempt
			}
			return ordered[i].NodeID < ordered[j].NodeID
		}
		return left.Before(right)
	})
	return ordered
}

// uxblindSNodeStatusLabel is the human status vocabulary for node diagnostics.
// It intentionally returns the raw value only for an unknown future enum so a
// schema drift remains visible to support staff.
func uxblindSNodeStatusLabel(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "RUNNING":
		return "In progress"
	case "WAITING", "PENDING":
		return "Waiting"
	case "SUCCEEDED", "COMPLETED", "DONE":
		return "Completed"
	case "FAILED", "ERROR":
		return "Failed"
	case "CANCELLED", "CANCELED":
		return "Cancelled"
	case "SKIPPED":
		return "Skipped"
	default:
		return status
	}
}
