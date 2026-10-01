package productui

import (
	"sort"
	"strings"
)

// PageWorkflowHistory is the canonical run-history route. PageHistory remains
// as a compatibility entry point for old bookmarks.
const PageWorkflowHistory PageID = "workflow-history"

const (
	workflowHistoryRelationshipStarted      = "started"
	workflowHistoryRelationshipParticipated = "participated"
	workflowHistoryRelationshipVisible      = "visible"
)

// WorkflowHistoryRun is the display-safe run projection used by the page and
// export. It deliberately contains no raw principal or tenant identifiers.
type WorkflowHistoryRun struct {
	ID           string
	Workflow     string
	Version      string
	Subject      string
	Requester    string
	Participants string
	Relationship string
	Stage        string
	StatusGroup  string
	StartedAt    string
	UpdatedAt    string
	Href         string
}

// WorkflowRunHistoryFilter is transport-neutral state for list and export. The
// live route currently receives the established history query keys; callers
// such as the report exporter can populate the fuller filter directly.
type WorkflowRunHistoryFilter struct {
	WorkflowType string
	StatusGroup  string
	Query        string
	Person       string
	Requester    string
	From         string
	To           string
	Relationship string
	Sort         string
	Direction    string
	Page         int
	PageSize     int
}

type workflowHistoryWindow struct {
	Items      []WorkflowHistoryRun
	Page       int
	PageSize   int
	Total      int
	TotalPages int
}

func workflowHistoryRuns(view View) []WorkflowHistoryRun {
	items := admittedWork(view)
	result := make([]WorkflowHistoryRun, 0, len(items))
	for _, item := range items {
		stage := workflowHistoryStage(item)
		taxonomy := JourneyStatusForStage(stage)
		workflow := strings.TrimSpace(localizedWorkTitle(view.Locale, item))
		if workflow == "" {
			workflow = strings.TrimSpace(item.Title)
		}
		if workflow == "" {
			workflow = view.Locale.Text("history.promotion")
		}
		requester := strings.TrimSpace(item.Requester)
		if workflowHistoryHasRelationship(item, "INITIATOR") {
			if requester == "" {
				requester = "You"
			}
		}
		participants := strings.TrimSpace(item.Participants)
		if participants == "" {
			participants = workflowHistoryParticipants(item)
		}
		result = append(result, WorkflowHistoryRun{
			ID: item.ID, Workflow: workflow, Version: workflowHistoryVersion(item.InstanceVersion),
			Subject: strings.TrimSpace(item.Person), Requester: requester, Participants: participants,
			Relationship: workflowHistoryRelationshipValue(item),
			Stage:        taxonomy.Stage, StatusGroup: taxonomy.HomeGroup,
			StartedAt: strings.TrimSpace(item.EffectiveDate), UpdatedAt: strings.TrimSpace(item.CompletedAt), Href: item.Href,
		})
	}
	return result
}

func workflowHistoryStage(item WorkItem) string {
	stage := strings.TrimSpace(item.StatusKey)
	if stage == "" {
		stage = strings.TrimSpace(item.Status)
	}
	return strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(stage, "-", "_"), " ", "_"))
}

func workflowHistoryVersion(version int64) string {
	if version <= 0 {
		return "—"
	}
	return "v" + itoa64(version)
}

func workflowHistoryHasRelationship(item WorkItem, relationship string) bool {
	for _, value := range item.ViewerRelationships {
		if strings.EqualFold(strings.TrimSpace(value), relationship) {
			return true
		}
	}
	return false
}

func workflowHistoryParticipants(item WorkItem) string {
	if item.WorkSummary && strings.TrimSpace(item.AssigneeName) != "" {
		return strings.TrimSpace(item.AssigneeName)
	}
	roles := make([]string, 0, len(item.ViewerRelationships))
	for _, relationship := range item.ViewerRelationships {
		switch strings.ToUpper(strings.TrimSpace(relationship)) {
		case "INITIATOR":
			roles = append(roles, "initiator")
		case "ASSIGNEE":
			roles = append(roles, "assignee")
		case "CANDIDATE":
			roles = append(roles, "candidate")
		}
	}
	sort.Strings(roles)
	return strings.Join(roles, ", ")
}

func workflowHistoryRelationshipValue(item WorkItem) string {
	for _, relationship := range []string{"INITIATOR", "ASSIGNEE", "CANDIDATE"} {
		if workflowHistoryHasRelationship(item, relationship) {
			return relationship
		}
	}
	return ""
}

func workflowHistoryRelationshipMatch(item WorkflowHistoryRun, relationship string) bool {
	switch strings.ToLower(strings.TrimSpace(relationship)) {
	case workflowHistoryRelationshipStarted:
		return strings.EqualFold(item.Relationship, "INITIATOR")
	case workflowHistoryRelationshipParticipated:
		return item.Relationship == "INITIATOR" || item.Relationship == "ASSIGNEE" || item.Relationship == "CANDIDATE"
	case workflowHistoryRelationshipVisible, "":
		return true
	default:
		return false
	}
}

// FilterWorkflowHistoryRuns applies the same deterministic projection rules
// used by the page and export. Inputs are copied before sorting.
func FilterWorkflowHistoryRuns(runs []WorkflowHistoryRun, filter WorkflowRunHistoryFilter) []WorkflowHistoryRun {
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	wantWorkflow := strings.ToLower(strings.TrimSpace(filter.WorkflowType))
	wantStatus := strings.ToLower(strings.TrimSpace(filter.StatusGroup))
	wantPerson := strings.ToLower(strings.TrimSpace(filter.Person))
	wantRequester := strings.ToLower(strings.TrimSpace(filter.Requester))
	result := make([]WorkflowHistoryRun, 0, len(runs))
	for _, run := range runs {
		if !workflowHistoryRelationshipMatch(run, filter.Relationship) {
			continue
		}
		if wantWorkflow != "" && !strings.Contains(strings.ToLower(run.Workflow), wantWorkflow) {
			continue
		}
		if wantStatus != "" && !workflowHistoryStatusMatches(run.StatusGroup, run.Stage, wantStatus) {
			continue
		}
		if wantPerson != "" && !strings.Contains(strings.ToLower(run.Subject), wantPerson) {
			continue
		}
		if wantRequester != "" && !strings.Contains(strings.ToLower(run.Requester), wantRequester) {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(strings.Join([]string{run.Workflow, run.Subject, run.Requester, run.Stage}, " ")), query) {
			continue
		}
		if filter.From != "" && run.StartedAt < filter.From {
			continue
		}
		if filter.To != "" && run.StartedAt > filter.To {
			continue
		}
		result = append(result, run)
	}
	sort.SliceStable(result, func(i, j int) bool {
		left, right := workflowHistorySortValue(result[i], filter.Sort), workflowHistorySortValue(result[j], filter.Sort)
		if left == right {
			return result[i].ID < result[j].ID
		}
		if strings.EqualFold(filter.Direction, "desc") {
			return left > right
		}
		return left < right
	})
	return result
}

func workflowHistoryStatusMatches(group, stage, want string) bool {
	if strings.EqualFold(group, want) {
		return true
	}
	return strings.EqualFold(JourneyStatusForStage(stage).Filter, want) || (want == "open" && !strings.EqualFold(group, JourneyHomeGroupClosed))
}

func workflowHistorySortValue(run WorkflowHistoryRun, sortKey string) string {
	switch strings.ToLower(strings.TrimSpace(sortKey)) {
	case "status":
		return run.StatusGroup + "\x00" + run.Stage
	case "updated", "change", "closed":
		return run.UpdatedAt
	default:
		return run.StartedAt
	}
}

func workflowHistoryWindowFor(runs []WorkflowHistoryRun, page, pageSize int) workflowHistoryWindow {
	if pageSize < 1 {
		pageSize = defaultPageSize
	}
	if pageSize > 100 {
		pageSize = 100
	}
	if page < 1 {
		page = 1
	}
	totalPages := (len(runs) + pageSize - 1) / pageSize
	if totalPages > 0 && page > totalPages {
		page = totalPages
	}
	start := (page - 1) * pageSize
	if start > len(runs) {
		start = len(runs)
	}
	end := start + pageSize
	if end > len(runs) {
		end = len(runs)
	}
	return workflowHistoryWindow{Items: runs[start:end], Page: page, PageSize: pageSize, Total: len(runs), TotalPages: totalPages}
}

func itoa64(value int64) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		index--
		digits[index] = '-'
	}
	return string(digits[index:])
}
