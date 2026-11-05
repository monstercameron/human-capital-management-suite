package project

import (
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectactivity"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectlink"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
)

// MaxTaskDetailComments bounds one task-detail discussion page. Older entries
// stay behind the carried cursor; the builder rejects oversized pages instead
// of rendering an unbounded thread.
const MaxTaskDetailComments = 50

var ErrInvalidTaskDetail = errors.New("project: invalid task detail input")

// TaskDetailAccess authorizes the viewer for the task before any field,
// comment, or link is projected.
type TaskDetailAccess interface {
	CanViewTask(ProjectID, TaskID) bool
}

// TaskDetailLink pairs a stored reference with its viewer-scoped resolution.
// Callers resolve through projectlink.Resolver so denial and absence share
// the same neutral restricted state.
type TaskDetailLink struct {
	LinkID     string
	Reference  projectlink.Reference
	Resolution projectlink.Result
}

// TaskDetailLinkView is the renderable link row. Restricted rows carry only
// the stable link ID and state: no kind, target, title, or snippet.
type TaskDetailLinkView struct {
	LinkID         string
	Kind           projectlink.Kind
	TargetID       string
	State          projectlink.State
	Title          string
	Snippet        string
	Version        string
	ScopeID        string
	Status         string
	Freshness      string
	ConversationID string
}

// TaskDetailCommentView renders the current revision only. Tombstoned comments
// keep their placeholder and revision number but no text.
type TaskDetailCommentView struct {
	ID        string
	AuthorID  string
	At        time.Time
	HTML      string
	Mentions  []projectactivity.Mention
	Tombstone bool
	Revision  uint64
}

// TaskDetailActivityView renders one activity row in page order.
type TaskDetailActivityView struct {
	Sequence uint64
	Kind     string
	ActorID  string
	At       time.Time
	Revision uint64
}

// TaskDetail is the viewer-scoped task-detail projection: authorized fields,
// assignment state, project-local due state, one bounded discussion page, and
// neutral restricted references.
type TaskDetail struct {
	TaskID         TaskID
	ProjectID      ProjectID
	Title          string
	Description    string
	Status         string
	TypeID         TypeID
	Priority       Priority
	OwnerID        string
	AssigneeID     string
	Due            TaskDueState
	Comments       []TaskDetailCommentView
	Activity       []TaskDetailActivityView
	CommentsCursor string
	Links          []TaskDetailLinkView
	Revision       uint64
}

// DetailAssigned reports whether the assignment control shows an assignee.
func DetailAssigned(d TaskDetail) bool { return d.AssigneeID != "" }

// DetailHasDueDate reports whether the detail carries a due state.
func DetailHasDueDate(d TaskDetail) bool { return d.Due.DueDate != "" }

// DetailHasMoreComments reports whether another discussion page is available.
func DetailHasMoreComments(d TaskDetail) bool { return d.CommentsCursor != "" }

// DetailCommentTombstoned reports whether a comment row is a tombstone.
func DetailCommentTombstoned(c TaskDetailCommentView) bool { return c.Tombstone }

// BuildTaskDetail projects one authorized task-detail page. It fails closed:
// unauthorized viewers get nothing, invalid references and mismatched
// previews collapse to neutral restricted rows, and oversized or ownerless
// inputs are rejected instead of partially rendered.
func BuildTaskDetail(task Task, project Project, category projectworkflow.StatusCategory, now time.Time, page projectactivity.Page, links []TaskDetailLink, access TaskDetailAccess) (TaskDetail, error) {
	if access == nil || !access.CanViewTask(project.ID, task.ID) {
		return TaskDetail{}, ErrNotAuthorized
	}
	if task.ID == "" || task.ProjectID != project.ID || task.TenantID != project.TenantID || project.TenantID == "" {
		return TaskDetail{}, ErrInvalidIdentity
	}
	if project.OwnerID == "" {
		return TaskDetail{}, ErrInvalidTaskDetail
	}
	due, err := CalculateDueState(task, project, category, now)
	if err != nil {
		return TaskDetail{}, err
	}
	if len(page.Comments) > MaxTaskDetailComments || len(page.Activity) > MaxTaskDetailComments {
		return TaskDetail{}, ErrInvalidTaskDetail
	}
	detail := TaskDetail{
		TaskID: task.ID, ProjectID: project.ID, Title: task.Title, Description: task.Description,
		Status: task.Status, TypeID: task.TypeID, Priority: task.Priority, OwnerID: project.OwnerID,
		AssigneeID: task.AssigneeID, Due: due, CommentsCursor: page.NextCursor, Revision: task.Revision,
	}
	for _, comment := range page.Comments {
		view, err := projectDetailComment(comment, task)
		if err != nil {
			return TaskDetail{}, err
		}
		detail.Comments = append(detail.Comments, view)
	}
	for _, event := range page.Activity {
		if event.TaskID != string(task.ID) {
			return TaskDetail{}, ErrInvalidTaskDetail
		}
		detail.Activity = append(detail.Activity, TaskDetailActivityView{Sequence: event.Sequence, Kind: event.Kind, ActorID: event.ActorID, At: event.At, Revision: event.Revision})
	}
	for _, link := range links {
		view, err := projectDetailLink(link)
		if err != nil {
			return TaskDetail{}, err
		}
		detail.Links = append(detail.Links, view)
	}
	return detail, nil
}

func projectDetailComment(comment projectactivity.Comment, task Task) (TaskDetailCommentView, error) {
	if comment.ID == "" || comment.TaskID != string(task.ID) {
		return TaskDetailCommentView{}, ErrInvalidTaskDetail
	}
	current := comment.Current()
	view := TaskDetailCommentView{ID: comment.ID, AuthorID: current.ActorID, At: current.At, Tombstone: current.Tombstone, Revision: current.Number}
	if !current.Tombstone {
		view.HTML = current.Text.HTML
		view.Mentions = append([]projectactivity.Mention(nil), current.Text.Mentions...)
	}
	return view, nil
}

func projectDetailLink(link TaskDetailLink) (TaskDetailLinkView, error) {
	if link.LinkID == "" || link.Reference.Validate() != nil {
		return TaskDetailLinkView{}, ErrInvalidTaskDetail
	}
	neutral := TaskDetailLinkView{LinkID: link.LinkID, State: projectlink.Restricted}
	if link.Resolution.State != projectlink.Available || link.Resolution.Preview == nil {
		return neutral, nil
	}
	preview := link.Resolution.Preview
	if preview.Kind != link.Reference.Kind || preview.ID != link.Reference.ID {
		return neutral, nil
	}
	return TaskDetailLinkView{
		LinkID: link.LinkID, Kind: preview.Kind, TargetID: preview.ID, State: projectlink.Available,
		Title: preview.Title, Snippet: preview.Snippet, Version: preview.Version, ScopeID: preview.ScopeID,
		Status: preview.Status, Freshness: preview.Freshness, ConversationID: preview.ConversationID,
	}, nil
}
