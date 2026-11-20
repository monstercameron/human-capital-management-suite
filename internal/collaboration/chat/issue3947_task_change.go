package chat

import "time"

// Issue 3947: the owner of a posted to-do list changes who has one task, or
// when it is due, without replacing the list. One CHANGE_TASK mutation carries
// both, so a person who changes both presses Save once. The rules live here so
// every store applies them the same way:
//
//   - only an open list changes, and only the task named;
//   - completion is never touched: a finished task stays finished, by whoever
//     finished it, and an open one stays open;
//   - a list that lets only the assignee tick keeps an assignee on every task
//     (#3923), so such a task can be handed to someone else, never to nobody;
//     the right to tick moves to the new assignee with the task;
//   - the task records who changed it and when, beside the revision history the
//     store keeps for the whole post.
//
// Whether the new assignee may be given the task (a current member who can read
// the list) is the store's question; this function only checks the shape.

// Chatcmd002OperationChangeTask is the operation name of the mutation.
const Chatcmd002OperationChangeTask = "CHANGE_TASK"

// Chatcmd002ChangeKinds name what a task's last change touched.
const (
	Chatcmd002ChangedAssignee    = "assignee"
	Chatcmd002ChangedDue         = "due"
	Chatcmd002ChangedAssigneeDue = "assignee-due"
)

// Chatcmd002TaskChange is what CHANGE_TASK asks for on one task. SetAssignee
// with an empty person takes the task from its assignee; SetDue with no time
// clears the date.
type Chatcmd002TaskChange struct {
	ItemID               string
	SetAssignee          bool
	AssigneeHomeTenantID string
	AssigneeID           string
	SetDue               bool
	DueAt                *time.Time
	DueTimed             bool
}

// TaskChange reads the task change out of a mutation.
func (m Chatcmd002Mutation) TaskChange() Chatcmd002TaskChange {
	return Chatcmd002TaskChange{ItemID: m.ItemID, SetAssignee: m.SetAssignee, AssigneeHomeTenantID: m.AssigneeHomeTenantID, AssigneeID: m.AssigneeID, SetDue: m.SetDue, DueAt: m.DueAt, DueTimed: m.DueTimed}
}

// ChangeTask is the card with one task's assignee and due date changed, who
// changed it and when. changed is false when the request leaves the task as it
// is (the same person, the same date), so nothing is written. It refuses a card
// that is not an open list (ErrPermissionDenied), a task that is not there
// (ErrNotFound), and a change that asks for nothing, names half a person, takes
// the assignee off an assignee-only list, or gives a date that is not a date
// (ErrInvalidArgument).
func (c Chatcmd002Card) ChangeTask(change Chatcmd002TaskChange, by Principal, now time.Time) (next Chatcmd002Card, changed bool, err error) {
	if c.Todo == nil || c.Closed(now) {
		return c, false, ErrPermissionDenied
	}
	if !change.SetAssignee && !change.SetDue {
		return c, false, ErrInvalidArgument
	}
	if (change.AssigneeID == "") != (change.AssigneeHomeTenantID == "") || (!change.SetAssignee && change.AssigneeID != "") || (!change.SetDue && (change.DueAt != nil || change.DueTimed)) {
		return c, false, ErrInvalidArgument
	}
	if change.SetDue && change.DueAt != nil && (change.DueAt.IsZero() || change.DueAt.Year() < 2000 || change.DueAt.Year() > 2200) {
		return c, false, ErrInvalidArgument
	}
	if change.SetDue && change.DueAt == nil && change.DueTimed {
		return c, false, ErrInvalidArgument
	}
	todo := *c.Todo
	todo.Items = append([]Chatcmd002Task(nil), c.Todo.Items...)
	for i := range todo.Items {
		task := &todo.Items[i]
		if task.ID != change.ItemID {
			continue
		}
		var kinds int
		if change.SetAssignee && (task.AssigneeID != change.AssigneeID || task.AssigneeHomeTenantID != change.AssigneeHomeTenantID) {
			if change.AssigneeID == "" && todo.Tick == "assignee" {
				return c, false, ErrInvalidArgument
			}
			task.AssigneeID, task.AssigneeHomeTenantID = change.AssigneeID, change.AssigneeHomeTenantID
			// The name a card was posted with is the author's reading at the time; the
			// page names people from the conversation's members, so it is dropped.
			task.AssigneeName = ""
			kinds |= 1
		}
		if change.SetDue && !sameDue(task.DueAt, task.DueTimed, change.DueAt, change.DueTimed) {
			if change.DueAt == nil {
				task.DueAt, task.DueTimed = nil, false
			} else {
				at := change.DueAt.UTC()
				task.DueAt, task.DueTimed = &at, change.DueTimed
			}
			kinds |= 2
		}
		if kinds == 0 {
			return c, false, nil
		}
		task.ChangedBySubjectID, task.ChangedByHomeTenantID, task.ChangedAtUnix = by.SubjectID, by.TenantID, now.Unix()
		task.Changed = [...]string{"", Chatcmd002ChangedAssignee, Chatcmd002ChangedDue, Chatcmd002ChangedAssigneeDue}[kinds]
		c.Todo = &todo
		return c, true, nil
	}
	return c, false, ErrNotFound
}

func sameDue(a *time.Time, aTimed bool, b *time.Time, bTimed bool) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b) && aTimed == bTimed
}
