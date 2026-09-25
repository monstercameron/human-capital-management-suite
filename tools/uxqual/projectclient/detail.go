package projectclient

import (
	"sort"
	"strings"
	"time"
	"unicode"

	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/projectui"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// DetailInputs are the authorized reads that turn a task detail into the
// editable ticket modal and task page. Names and Photos resolve subject IDs
// from the governed directory; a subject missing from it is shown by a name
// derived from the subject itself, never as the raw ID.
type DetailInputs struct {
	Task             *projectv1.ProjectTask
	ProjectID        string
	ProjectName      string
	WorkflowRevision uint64
	Statuses         []*projectv1.ProjectStatus
	TaskTypes        []*projectv1.ProjectTaskType
	Comments         []*projectv1.ProjectTaskComment
	Activity         []*projectv1.ProjectTaskActivity
	Members          []*projectv1.ProjectMember
	Names            map[string]string
	Photos           map[string]string
	Viewer           string
	CanEdit          bool
}

// PriorityOptions are the task priorities in ascending order; labels are
// English source text the page localizes.
var PriorityOptions = []projectui.Status{
	{ID: projectv1.TaskPriority_TASK_PRIORITY_LOW.String(), Label: "Low"},
	{ID: projectv1.TaskPriority_TASK_PRIORITY_NORMAL.String(), Label: "Normal"},
	{ID: projectv1.TaskPriority_TASK_PRIORITY_HIGH.String(), Label: "High"},
	{ID: projectv1.TaskPriority_TASK_PRIORITY_URGENT.String(), Label: "Urgent"},
}

// EnrichDetail adds the editing identity, choices, comments and activity to
// a projected task detail.
func EnrichDetail(detail projectui.DetailModel, in DetailInputs) projectui.DetailModel {
	task := in.Task
	if task == nil {
		return detail
	}
	detail.ProjectID, detail.ProjectName, detail.TaskID = in.ProjectID, in.ProjectName, task.GetTaskId()
	detail.TaskRevision, detail.WorkflowRevision = task.GetRevision(), in.WorkflowRevision
	if detail.WorkflowRevision == 0 {
		detail.WorkflowRevision = task.GetWorkflowRevision()
	}
	detail.StatusID = task.GetStatusId()
	allowed := map[string]bool{task.GetStatusId(): true}
	for _, status := range in.Statuses {
		if status != nil && status.GetStatusId() == task.GetStatusId() {
			for _, next := range status.GetAllowedNextStatusIds() {
				allowed[next] = true
			}
		}
	}
	for _, status := range in.Statuses {
		if status == nil || !allowed[status.GetStatusId()] {
			continue
		}
		label := status.GetName()
		if label == "" {
			label = status.GetStatusId()
		}
		detail.StatusOptions = append(detail.StatusOptions, projectui.Status{ID: status.GetStatusId(), Label: label})
	}
	detail.CanEdit, detail.CanComment = in.CanEdit, in.CanEdit
	detail.CanMoveStatus = in.CanEdit && len(detail.StatusOptions) > 1

	detail.AssigneeID = task.GetAssigneeId()
	if detail.AssigneeID != "" {
		detail.Assignee = PersonName(detail.AssigneeID, in.Names)
		detail.AssigneePhoto = in.Photos[detail.AssigneeID]
	}
	for _, member := range in.Members {
		if member == nil || member.GetUserId() == "" {
			continue
		}
		if state := strings.ToUpper(member.GetState()); state != "" && state != "ACTIVE" {
			continue
		}
		detail.Members = append(detail.Members, projectui.Person{ID: member.GetUserId(), Name: PersonName(member.GetUserId(), in.Names), PhotoURL: in.Photos[member.GetUserId()]})
	}
	sort.SliceStable(detail.Members, func(i, j int) bool { return detail.Members[i].Name < detail.Members[j].Name })

	detail.StartDate, detail.StoryPoints = task.GetStartDate(), int(task.GetStoryPoints())
	detail.Labels = append([]string(nil), task.GetLabels()...)
	if reporter := task.GetCreatedBy(); reporter != "" {
		detail.Reporter, detail.ReporterPhoto = PersonName(reporter, in.Names), in.Photos[reporter]
	}
	if created := task.GetCreatedAt(); created != nil && created.IsValid() && created.AsTime().Unix() > 0 {
		detail.CreatedAt = created.AsTime().UTC().Format(time.RFC3339)
	}
	if updated := task.GetUpdatedAt(); updated != nil && updated.IsValid() && updated.AsTime().Unix() > 0 {
		detail.UpdatedAt = updated.AsTime().UTC().Format(time.RFC3339)
	}
	detail.PriorityID = task.GetPriority().String()
	detail.Priority = priorityLabel(task.GetPriority())
	detail.PriorityOptions = append([]projectui.Status(nil), PriorityOptions...)
	detail.Type = task.GetTaskTypeId()
	for _, taskType := range in.TaskTypes {
		if taskType != nil && taskType.GetTaskTypeId() == task.GetTaskTypeId() && taskType.GetName() != "" {
			detail.Type = taskType.GetName()
		}
	}

	// ListTaskComments is an append-only feed: one entry per revision of a
	// comment. Fold it to the latest revision of each comment, which is the
	// revision an edit or delete must expect; a tombstoned latest revision
	// means the comment is gone.
	latest := map[string]*projectv1.ProjectTaskComment{}
	first := map[string]*timestamppb.Timestamp{}
	order := []string{}
	for _, comment := range in.Comments {
		if comment == nil || comment.GetCommentId() == "" {
			continue
		}
		id := comment.GetCommentId()
		if _, seen := latest[id]; !seen {
			order = append(order, id)
			first[id] = comment.GetCreatedAt()
		}
		if current := latest[id]; current == nil || comment.GetCurrentRevision() >= current.GetCurrentRevision() {
			latest[id] = comment
		}
		if created := comment.GetCreatedAt(); created != nil && (first[id] == nil || created.AsTime().Before(first[id].AsTime())) {
			first[id] = created
		}
	}
	comments := make([]*projectv1.ProjectTaskComment, 0, len(order))
	for _, id := range order {
		folded := proto.Clone(latest[id]).(*projectv1.ProjectTaskComment)
		folded.CreatedAt = first[id]
		comments = append(comments, folded)
	}
	sort.SliceStable(comments, func(i, j int) bool {
		return comments[i].GetCreatedAt().AsTime().Before(comments[j].GetCreatedAt().AsTime())
	})
	detail.Comments = nil
	for _, comment := range comments {
		if comment == nil || comment.GetTombstone() || comment.GetCommentId() == "" {
			continue
		}
		entry := projectui.Comment{
			ID: comment.GetCommentId(), Revision: comment.GetCurrentRevision(), Body: comment.GetSourceText(),
			Author: PersonName(comment.GetActorId(), in.Names), AuthorPhoto: in.Photos[comment.GetActorId()],
			Own: in.Viewer != "" && comment.GetActorId() == in.Viewer, Edited: comment.GetCurrentRevision() > 1,
		}
		if comment.GetCreatedAt() != nil {
			entry.DateTime = comment.GetCreatedAt().AsTime().UTC().Format(time.RFC3339)
		}
		detail.Comments = append(detail.Comments, entry)
	}
	detail.CommentTotal = len(detail.Comments)

	activity := append([]*projectv1.ProjectTaskActivity(nil), in.Activity...)
	sort.SliceStable(activity, func(i, j int) bool { return activity[i].GetSequence() > activity[j].GetSequence() })
	detail.Activity = nil
	for _, entry := range activity {
		if entry == nil {
			continue
		}
		item := projectui.Activity{Label: entry.GetKind(), Actor: PersonName(entry.GetActorId(), in.Names)}
		if entry.GetOccurredAt() != nil {
			item.Time = entry.GetOccurredAt().AsTime().UTC().Format(time.RFC3339)
		}
		detail.Activity = append(detail.Activity, item)
	}
	return detail
}

// PersonName resolves a subject ID through the directory, falling back to a
// readable name derived from the subject ("hc-003-evelyn-morgan" reads
// "Evelyn Morgan") so a raw identifier never reaches the page.
func PersonName(subjectID string, names map[string]string) string {
	subjectID = strings.TrimSpace(subjectID)
	if name := strings.TrimSpace(names[subjectID]); name != "" {
		return name
	}
	parts := strings.FieldsFunc(subjectID, func(r rune) bool { return r == '-' || r == '_' || r == '.' || r == ':' })
	words := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || strings.EqualFold(part, "hc") || strings.EqualFold(part, "employee") || strings.IndexFunc(part, unicode.IsLetter) < 0 {
			continue
		}
		runes := []rune(strings.ToLower(part))
		runes[0] = unicode.ToUpper(runes[0])
		words = append(words, string(runes))
	}
	if len(words) == 0 {
		return "Former member"
	}
	return strings.Join(words, " ")
}
