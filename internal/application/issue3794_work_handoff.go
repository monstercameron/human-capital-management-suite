package application

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/project"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3794: hand off a specific work item that a conversation is linked to.
//
// A person picks one item of the work this conversation made (issue 3790),
// names the next action, the person to offer it to and a note, and that person
// accepts or declines. The offer is private to the two of them; the work stays
// in the project service. A follow-up offer asks the person to take the next
// step and changes nothing about who owns the work. A transfer offer asks the
// person to become the owner, and accepting it is the project service's own
// reassignment, made as the person accepting, so it needs their own right to
// edit the work and never widens what they can do.

// ErrChatWorkConflict means the offer or the work moved on since it was shown.
var ErrChatWorkConflict = errors.New("chat work: changed since offered")

const (
	// ChatWorkHandoffFollowUp and ChatWorkHandoffTransfer are the two kinds of offer.
	ChatWorkHandoffFollowUp = chatstore.HandoffFollowUp
	ChatWorkHandoffTransfer = chatstore.HandoffTransfer
)

// ChatWorkHandoffStore is where offers are kept.
type ChatWorkHandoffStore interface {
	OfferWorkHandoff(context.Context, chatstore.WorkHandoff) (chatstore.WorkHandoff, bool, error)
	GetWorkHandoff(context.Context, string, string) (chatstore.WorkHandoff, bool, error)
	ListWorkHandoffs(context.Context, string, string, string, int) ([]chatstore.WorkHandoff, error)
	DecideWorkHandoff(context.Context, string, string, string, string, time.Time) (chatstore.WorkHandoff, bool, error)
}

// ChatWorkHandoffTasks is the owning work service as a hand-off uses it. Every
// call is made as the person acting.
type ChatWorkHandoffTasks interface {
	GetTask(context.Context, *trust.Principal, string, string) (projectservice.TaskRecord, error)
	CanEditTask(context.Context, *trust.Principal, string) error
	PatchTask(context.Context, *trust.Principal, projectservice.PatchTaskRequest) (projectservice.TaskRecord, error)
}

// ChatWorkHandoffOfferRequest is one reviewed offer. IdempotencyKey names the
// attempt, so a retry recovers the same offer.
type ChatWorkHandoffOfferRequest struct {
	ConversationID, ProjectID, TaskID, Mode, RecipientID, NextAction, Note, IdempotencyKey string
}

// ChatWorkHandoff is one offer as a viewer may see it. When the viewer cannot
// open the work, Restricted is true and nothing about it is filled in.
type ChatWorkHandoff struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
	PostID         string `json:"post_id,omitempty"`
	ProjectID      string `json:"project_id,omitempty"`
	TaskID         string `json:"task_id,omitempty"`
	Mode           string `json:"mode"`
	// Role is "sender" or "recipient": which of the two people the viewer is.
	Role       string `json:"role"`
	FromID     string `json:"from_id"`
	ToID       string `json:"to_id"`
	NextAction string `json:"next_action"`
	Note       string `json:"note,omitempty"`
	Status     string `json:"status"`
	OfferedAt  string `json:"offered_at"`
	DecidedAt  string `json:"decided_at,omitempty"`
	DecidedBy  string `json:"decided_by,omitempty"`
	// The work as it stands now, and as it stood when offered.
	Title        string `json:"title,omitempty"`
	WorkStatus   string `json:"work_status,omitempty"`
	OwnerID      string `json:"owner_id,omitempty"`
	DueDate      string `json:"due_date,omitempty"`
	OfferedOwner string `json:"offered_owner_id,omitempty"`
	OfferedDue   string `json:"offered_due_date,omitempty"`
	Restricted   bool   `json:"restricted,omitempty"`
	// Changed is true when the work's owner or deadline is no longer what the
	// offer was made against.
	Changed bool `json:"changed,omitempty"`
}

// ChatWorkHandoffs offers, answers and lists hand-offs.
type ChatWorkHandoffs struct {
	work  *ChatMessageWork
	tasks ChatWorkHandoffTasks
	store ChatWorkHandoffStore
	now   func() time.Time
}

// NewChatWorkHandoffs composes hand-offs over the message-to-work reads, the
// owning work service and the offer store.
func NewChatWorkHandoffs(work *ChatMessageWork, tasks ChatWorkHandoffTasks, store ChatWorkHandoffStore) *ChatWorkHandoffs {
	if work == nil || tasks == nil || store == nil {
		return nil
	}
	return &ChatWorkHandoffs{work: work, tasks: tasks, store: store, now: time.Now}
}

const (
	chatWorkHandoffActionMax = chatstore.WorkHandoffActionMax
	chatWorkHandoffNoteMax   = chatstore.WorkHandoffNoteMax
	chatWorkHandoffListMax   = 200
)

func (h *ChatWorkHandoffs) ready() bool {
	return h != nil && h.work != nil && h.tasks != nil && h.store != nil
}

func chatWorkHandoffTaskError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, project.ErrRevisionConflict):
		return ErrChatWorkConflict
	case errors.Is(err, projectaccess.ErrUnauthorized), errors.Is(err, projectaccess.ErrMemberNotFound), errors.Is(err, projectaccess.ErrTenantMismatch):
		return ErrChatWorkDenied
	}
	return chatWorkProjectError(err)
}

// linkedTask finds the item among the work this conversation made, as this
// viewer may see it. Work the viewer cannot read is not there, so it cannot be
// offered and its existence is not confirmed.
func (h *ChatWorkHandoffs) linkedTask(ctx context.Context, p *trust.Principal, conversationID, projectID, taskID string) (ChatWorkLink, error) {
	links, err := h.work.Linked(ctx, p, conversationID)
	if err != nil {
		return ChatWorkLink{}, err
	}
	for _, link := range links {
		if link.ProjectID == projectID && link.TaskID == taskID {
			return link, nil
		}
	}
	return ChatWorkLink{}, ErrChatWorkNotFound
}

// Offer makes one reviewed offer. The conversation is read first, then the
// work (it must be one of the items the conversation made and the offerer must
// own it or be allowed to edit it), then the recipient; only then is anything
// written.
func (h *ChatWorkHandoffs) Offer(ctx context.Context, p *trust.Principal, in ChatWorkHandoffOfferRequest) (ChatWorkHandoff, error) {
	cp, err := chatWorkPrincipal(p)
	if err != nil {
		return ChatWorkHandoff{}, err
	}
	if !h.ready() {
		return ChatWorkHandoff{}, ErrChatWorkUnavailable
	}
	action, note, to := strings.TrimSpace(in.NextAction), strings.TrimSpace(in.Note), strings.TrimSpace(in.RecipientID)
	if strings.TrimSpace(in.ConversationID) == "" || strings.TrimSpace(in.ProjectID) == "" || strings.TrimSpace(in.TaskID) == "" ||
		(in.Mode != ChatWorkHandoffFollowUp && in.Mode != ChatWorkHandoffTransfer) || to == "" || to == cp.SubjectID ||
		action == "" || utf8.RuneCountInString(action) > chatWorkHandoffActionMax || utf8.RuneCountInString(note) > chatWorkHandoffNoteMax ||
		!chatWorkKeyPattern.MatchString(in.IdempotencyKey) {
		return ChatWorkHandoff{}, ErrChatWorkInvalid
	}
	if _, err := h.work.readMessage(ctx, p, in.ConversationID, ""); err != nil {
		return ChatWorkHandoff{}, err
	}
	link, err := h.linkedTask(ctx, p, in.ConversationID, in.ProjectID, in.TaskID)
	if err != nil {
		return ChatWorkHandoff{}, err
	}
	task, err := h.tasks.GetTask(ctx, p, in.ProjectID, in.TaskID)
	if err != nil {
		return ChatWorkHandoff{}, chatWorkHandoffTaskError(err)
	}
	if task.Archived {
		return ChatWorkHandoff{}, ErrChatWorkInvalid
	}
	if task.AssigneeID != cp.SubjectID {
		if err := h.tasks.CanEditTask(ctx, p, in.ProjectID); err != nil {
			return ChatWorkHandoff{}, chatWorkHandoffTaskError(err)
		}
	}
	// The person offered the work must be able to read this conversation, so the
	// offer never reaches someone who could not have seen where it came from.
	// Their right to the work itself is checked when they act on it.
	recipient := chat.Principal{TenantID: cp.TenantID, SubjectID: to}
	if _, _, err := h.work.chat.ReadAuthorizedReference(ctx, recipient, cp.TenantID, in.ConversationID, ""); err != nil {
		if errors.Is(err, chat.ErrNotFound) || errors.Is(err, chat.ErrPermissionDenied) {
			return ChatWorkHandoff{}, ErrChatWorkInvalid
		}
		return ChatWorkHandoff{}, ErrChatWorkUnavailable
	}
	offered, _, err := h.store.OfferWorkHandoff(ctx, chatstore.WorkHandoff{
		TenantID: cp.TenantID, ID: "wh-" + uuid.NewString(), ConversationID: in.ConversationID, SourcePostID: link.PostID,
		ProjectID: in.ProjectID, TaskID: in.TaskID, Mode: in.Mode, FromID: cp.SubjectID, ToID: to, NextAction: action, Note: note,
		OwnerAtOffer: task.AssigneeID, DueAtOffer: task.DueDate, TaskRevision: int64(task.Revision), IdempotencyKey: in.IdempotencyKey, OfferedAt: h.now().UTC(),
	})
	if err != nil {
		if errors.Is(err, chat.ErrInvalidArgument) {
			return ChatWorkHandoff{}, ErrChatWorkInvalid
		}
		return ChatWorkHandoff{}, ErrChatWorkUnavailable
	}
	// An open offer of the same kind to the same person for this work already
	// stands. When it is someone else's, it is theirs and private to them and the
	// person it was offered to: it is not returned, so its note and next step are
	// not shown to a third person who may merely edit the work.
	if offered.FromID != cp.SubjectID {
		return ChatWorkHandoff{}, ErrChatWorkConflict
	}
	return h.view(ctx, p, offered, map[string]ChatWorkLink{chatWorkHandoffKey(link.ProjectID, link.TaskID): link}), nil
}

func chatWorkHandoffKey(projectID, taskID string) string { return projectID + "\x00" + taskID }

// view shows one offer to the viewer. Work the viewer cannot open is left blank.
func (h *ChatWorkHandoffs) view(_ context.Context, p *trust.Principal, o chatstore.WorkHandoff, visible map[string]ChatWorkLink) ChatWorkHandoff {
	role := "recipient"
	if o.FromID == p.Subject() {
		role = "sender"
	}
	out := ChatWorkHandoff{ID: o.ID, ConversationID: o.ConversationID, Mode: o.Mode, Role: role, FromID: o.FromID, ToID: o.ToID, NextAction: o.NextAction, Note: o.Note,
		Status: o.Status, OfferedAt: o.OfferedAt.UTC().Format(time.RFC3339), DecidedBy: o.DecidedBy, OfferedOwner: o.OwnerAtOffer, OfferedDue: o.DueAtOffer}
	if !o.DecidedAt.IsZero() {
		out.DecidedAt = o.DecidedAt.UTC().Format(time.RFC3339)
	}
	link, ok := visible[chatWorkHandoffKey(o.ProjectID, o.TaskID)]
	if !ok {
		out.Restricted = true
		return out
	}
	out.PostID, out.ProjectID, out.TaskID = link.PostID, link.ProjectID, link.TaskID
	out.Title, out.WorkStatus, out.OwnerID, out.DueDate = link.Title, link.Status, link.OwnerID, link.DueDate
	out.Changed = o.Status == chatstore.HandoffOffered && (link.OwnerID != o.OwnerAtOffer || link.DueDate != o.DueAtOffer)
	return out
}

// List shows the offers of one conversation that the viewer made or was
// offered. Nobody else sees them, and work the viewer cannot open is not named.
func (h *ChatWorkHandoffs) List(ctx context.Context, p *trust.Principal, conversationID string) ([]ChatWorkHandoff, error) {
	if _, err := chatWorkPrincipal(p); err != nil {
		return nil, err
	}
	if !h.ready() {
		return nil, ErrChatWorkUnavailable
	}
	if strings.TrimSpace(conversationID) == "" {
		return nil, ErrChatWorkInvalid
	}
	links, err := h.work.Linked(ctx, p, conversationID)
	if err != nil {
		return nil, err
	}
	offers, err := h.store.ListWorkHandoffs(ctx, p.Tenant().String(), conversationID, p.Subject(), chatWorkHandoffListMax)
	if err != nil {
		return nil, ErrChatWorkUnavailable
	}
	visible := make(map[string]ChatWorkLink, len(links))
	for _, link := range links {
		visible[chatWorkHandoffKey(link.ProjectID, link.TaskID)] = link
	}
	out := make([]ChatWorkHandoff, 0, len(offers))
	for _, offer := range offers {
		out = append(out, h.view(ctx, p, offer, visible))
	}
	return out, nil
}

// load reads an offer for the person it concerns in the given role. Anyone
// else, and an offer that is not there, read the same.
func (h *ChatWorkHandoffs) load(ctx context.Context, p *trust.Principal, id string, asRecipient bool) (chatstore.WorkHandoff, error) {
	if _, err := chatWorkPrincipal(p); err != nil {
		return chatstore.WorkHandoff{}, err
	}
	if !h.ready() {
		return chatstore.WorkHandoff{}, ErrChatWorkUnavailable
	}
	if strings.TrimSpace(id) == "" {
		return chatstore.WorkHandoff{}, ErrChatWorkInvalid
	}
	offer, found, err := h.store.GetWorkHandoff(ctx, p.Tenant().String(), id)
	if err != nil {
		return chatstore.WorkHandoff{}, ErrChatWorkUnavailable
	}
	who := offer.FromID
	if asRecipient {
		who = offer.ToID
	}
	if !found || who != p.Subject() {
		return chatstore.WorkHandoff{}, ErrChatWorkNotFound
	}
	// The conversation is read again: someone who left it can no longer act on
	// what it holds.
	if _, err := h.work.readMessage(ctx, p, offer.ConversationID, ""); err != nil {
		return chatstore.WorkHandoff{}, err
	}
	return offer, nil
}

func (h *ChatWorkHandoffs) shown(ctx context.Context, p *trust.Principal, o chatstore.WorkHandoff) (ChatWorkHandoff, error) {
	links, err := h.work.Linked(ctx, p, o.ConversationID)
	if err != nil {
		return ChatWorkHandoff{}, err
	}
	visible := make(map[string]ChatWorkLink, len(links))
	for _, link := range links {
		visible[chatWorkHandoffKey(link.ProjectID, link.TaskID)] = link
	}
	return h.view(ctx, p, o, visible), nil
}

// Accept is the recipient taking the offer. Their access to the conversation
// and the work is read again now. A follow-up is then recorded; a transfer is
// made by the project service as the recipient, only while the work still has
// the owner and deadline it was offered with, and only if they may edit it.
// If the transfer was already made and only the record is missing, the record is
// completed without a second change.
func (h *ChatWorkHandoffs) Accept(ctx context.Context, p *trust.Principal, id string) (ChatWorkHandoff, error) {
	offer, err := h.load(ctx, p, id, true)
	if err != nil {
		return ChatWorkHandoff{}, err
	}
	switch offer.Status {
	case chatstore.HandoffAccepted:
		return h.shown(ctx, p, offer)
	case chatstore.HandoffOffered:
	default:
		return ChatWorkHandoff{}, ErrChatWorkConflict
	}
	task, err := h.tasks.GetTask(ctx, p, offer.ProjectID, offer.TaskID)
	if err != nil {
		return ChatWorkHandoff{}, chatWorkHandoffTaskError(err)
	}
	if task.Archived {
		return ChatWorkHandoff{}, ErrChatWorkConflict
	}
	if offer.Mode == chatstore.HandoffTransfer && task.AssigneeID != p.Subject() {
		if task.AssigneeID != offer.OwnerAtOffer || task.DueDate != offer.DueAtOffer {
			return ChatWorkHandoff{}, ErrChatWorkConflict
		}
		if err := h.tasks.CanEditTask(ctx, p, offer.ProjectID); err != nil {
			return ChatWorkHandoff{}, chatWorkHandoffTaskError(err)
		}
		assignee := p.Subject()
		if _, err := h.tasks.PatchTask(ctx, p, projectservice.PatchTaskRequest{
			ProjectID: offer.ProjectID, TaskID: offer.TaskID, IdempotencyKey: "handoff:" + offer.ID,
			ExpectedTaskRevision: task.Revision, ExpectedWorkflowRevision: task.WorkflowRevision,
			Patch: project.TaskPatch{AssigneeID: &assignee},
		}); err != nil {
			return ChatWorkHandoff{}, chatWorkHandoffTaskError(err)
		}
	}
	decided, _, err := h.store.DecideWorkHandoff(ctx, offer.TenantID, offer.ID, chatstore.HandoffAccepted, p.Subject(), h.now().UTC())
	if err != nil {
		return ChatWorkHandoff{}, ErrChatWorkUnavailable
	}
	if decided.Status != chatstore.HandoffAccepted {
		return ChatWorkHandoff{}, ErrChatWorkConflict
	}
	return h.shown(ctx, p, decided)
}

// Decline is the recipient turning the offer down. The work is not touched.
func (h *ChatWorkHandoffs) Decline(ctx context.Context, p *trust.Principal, id string) (ChatWorkHandoff, error) {
	return h.close(ctx, p, id, true, chatstore.HandoffDeclined)
}

// Withdraw is the offerer taking an open offer back. The work is not touched.
func (h *ChatWorkHandoffs) Withdraw(ctx context.Context, p *trust.Principal, id string) (ChatWorkHandoff, error) {
	return h.close(ctx, p, id, false, chatstore.HandoffWithdrawn)
}

func (h *ChatWorkHandoffs) close(ctx context.Context, p *trust.Principal, id string, asRecipient bool, status string) (ChatWorkHandoff, error) {
	offer, err := h.load(ctx, p, id, asRecipient)
	if err != nil {
		return ChatWorkHandoff{}, err
	}
	if offer.Status == status {
		return h.shown(ctx, p, offer)
	}
	if offer.Status != chatstore.HandoffOffered {
		return ChatWorkHandoff{}, ErrChatWorkConflict
	}
	decided, _, err := h.store.DecideWorkHandoff(ctx, offer.TenantID, offer.ID, status, p.Subject(), h.now().UTC())
	if err != nil {
		return ChatWorkHandoff{}, ErrChatWorkUnavailable
	}
	if decided.Status != status {
		return ChatWorkHandoff{}, ErrChatWorkConflict
	}
	return h.shown(ctx, p, decided)
}

// chatWorkHandoffTasksAdapter exposes the project service as ChatWorkHandoffTasks.
type chatWorkHandoffTasksAdapter struct{ service projectservice.Service }

func (a chatWorkHandoffTasksAdapter) GetTask(ctx context.Context, p *trust.Principal, projectID, taskID string) (projectservice.TaskRecord, error) {
	return a.service.GetTask(ctx, p, projectID, taskID)
}

func (a chatWorkHandoffTasksAdapter) CanEditTask(ctx context.Context, p *trust.Principal, projectID string) error {
	if a.service.Auth == nil {
		return projectservice.ErrUnavailable
	}
	return a.service.Auth.Authorize(ctx, p, projectID, projectaccess.EditTask)
}

func (a chatWorkHandoffTasksAdapter) PatchTask(ctx context.Context, p *trust.Principal, req projectservice.PatchTaskRequest) (projectservice.TaskRecord, error) {
	return a.service.PatchTask(ctx, p, req)
}

// chatWorkHandoffsFor composes hand-offs over the served runtime, or nil when
// message-to-work or the offer store is not composed.
func chatWorkHandoffsFor(work *ChatMessageWork, projects projectservice.Service, store *chatstore.Store) *ChatWorkHandoffs {
	if work == nil || store == nil || projects.Auth == nil || projects.Reads == nil || projects.Workflows == nil {
		return nil
	}
	return NewChatWorkHandoffs(work, chatWorkHandoffTasksAdapter{service: projects}, store)
}
