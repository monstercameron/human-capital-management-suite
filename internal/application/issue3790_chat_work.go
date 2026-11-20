package application

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/application/projectrefs"
	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectlink"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3790: turn a chat message into a task. The conversion goes through the
// workitem coordinator (authorize the message read, then the destination, then
// create once) and lands in the project task store as a task that links the
// message, so the project store's own durable idempotency and project access
// rules decide what is created and who may see it.

var (
	ErrChatWorkInvalid     = errors.New("chat work: invalid request")
	ErrChatWorkNotFound    = errors.New("chat work: message or project not found")
	ErrChatWorkDenied      = errors.New("chat work: not allowed")
	ErrChatWorkUnavailable = errors.New("chat work: unavailable")
)

// ChatWorkProjects is the project service as message conversion uses it.
type ChatWorkProjects interface {
	ListCreatableProjects(context.Context, *trust.Principal) ([]ChatWorkProject, error)
	CanCreateTask(context.Context, *trust.Principal, string) error
	Published(context.Context, *trust.Principal, string) (projectservice.WorkflowConfiguration, error)
	CreateTask(context.Context, *trust.Principal, projectservice.CreateTaskRequest) (projectservice.TaskRecord, error)
	ListTasksLinkedToChatConversation(context.Context, *trust.Principal, string, int) ([]projectservice.ChatLinkedTaskRecord, error)
}

// ChatWorkProject is a project a person may create a task in.
type ChatWorkProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ChatWorkLink is the work that a message became, as one viewer may see it.
type ChatWorkLink struct {
	PostID    string `json:"post_id"`
	ProjectID string `json:"project_id"`
	TaskID    string `json:"task_id"`
	Title     string `json:"title"`
	StatusID  string `json:"status_id"`
	Status    string `json:"status"`
	OwnerID   string `json:"owner_id,omitempty"`
	DueDate   string `json:"due_date,omitempty"`
	// Category is the project's own class for the status (NOT_STARTED, ACTIVE,
	// BLOCKED, DONE, CANCELLED); empty when the project did not say (issue 3791).
	Category string `json:"category,omitempty"`
}

// ChatWorkConvertRequest is one reviewed conversion attempt. IdempotencyKey
// names the attempt, not the message: retrying the same reviewed request
// reuses it, and a second distinct task from the same message uses a new one.
type ChatWorkConvertRequest struct {
	ConversationID, MessageID, Target, ProjectID, Title, OwnerID, DueDate, IdempotencyKey string
}

const (
	chatWorkTitleMax = 200
	chatWorkTaskType = "task_default"
)

var chatWorkKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)

// ChatMessageWork converts messages and reads back what they became.
type ChatMessageWork struct {
	chat        projectrefs.ChatLinks
	projects    ChatWorkProjects
	coordinator *workitem.MessageConversionCoordinator
}

func NewChatMessageWork(chatLinks projectrefs.ChatLinks, projects ChatWorkProjects) *ChatMessageWork {
	w := &ChatMessageWork{chat: chatLinks, projects: projects}
	w.coordinator = workitem.NewMessageConversionCoordinator(chatWorkAuthority{w}, chatWorkSink{w})
	return w
}

type chatWorkCall struct {
	principal *trust.Principal
	projectID string
	title     string
	dueDate   string
}

type chatWorkCallKey struct{}

func chatWorkCallFrom(ctx context.Context) (*chatWorkCall, bool) {
	call, ok := ctx.Value(chatWorkCallKey{}).(*chatWorkCall)
	return call, ok && call != nil && call.principal != nil
}

func chatWorkTenantUUID(tenant string) uuid.UUID {
	if id, err := uuid.Parse(tenant); err == nil {
		return id
	}
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("chat-work-tenant\x00"+tenant))
}

func chatWorkPrincipal(p *trust.Principal) (chat.Principal, error) {
	if p == nil || p.SubjectKind() != trust.SubjectKindHuman {
		return chat.Principal{}, ErrChatWorkDenied
	}
	return chat.Principal{TenantID: p.Tenant().String(), SubjectID: p.Subject(), Roles: p.Roles()}, nil
}

// readMessage is the one place a person's right to read a message is decided.
// A refusal and an absence read the same, so a message the person cannot open
// is never confirmed to exist. With no message id it decides the conversation.
func (w *ChatMessageWork) readMessage(ctx context.Context, p *trust.Principal, conversationID, messageID string) (*chat.Post, error) {
	cp, err := chatWorkPrincipal(p)
	if err != nil {
		return nil, err
	}
	if w == nil || w.chat == nil {
		return nil, ErrChatWorkUnavailable
	}
	_, post, err := w.chat.ReadAuthorizedReference(ctx, cp, cp.TenantID, conversationID, messageID)
	switch {
	case err == nil:
	case errors.Is(err, chat.ErrNotFound), errors.Is(err, chat.ErrPermissionDenied):
		return nil, ErrChatWorkNotFound
	default:
		return nil, ErrChatWorkUnavailable
	}
	if messageID != "" && (post == nil || post.ID != messageID || post.ConversationID != conversationID || post.Deleted) {
		return nil, ErrChatWorkNotFound
	}
	return post, nil
}

type chatWorkAuthority struct{ w *ChatMessageWork }

func (a chatWorkAuthority) CanReadMessage(ctx context.Context, ref workitem.MessageRef, _ string) error {
	call, ok := chatWorkCallFrom(ctx)
	if !ok {
		return ErrChatWorkDenied
	}
	_, err := a.w.readMessage(ctx, call.principal, ref.ConversationID, ref.MessageID)
	return err
}

func (a chatWorkAuthority) CanCreateTarget(ctx context.Context, _ uuid.UUID, kind workitem.ConversionTargetKind, _ string) error {
	call, ok := chatWorkCallFrom(ctx)
	if !ok || kind != workitem.ConversionTargetTask {
		return ErrChatWorkInvalid
	}
	return chatWorkProjectError(a.w.projects.CanCreateTask(ctx, call.principal, call.projectID))
}

type chatWorkSink struct{ w *ChatMessageWork }

func (s chatWorkSink) CreateTarget(ctx context.Context, c workitem.MessageConversion) (string, error) {
	call, ok := chatWorkCallFrom(ctx)
	if !ok {
		return "", ErrChatWorkDenied
	}
	published, err := s.w.projects.Published(ctx, call.principal, call.projectID)
	if err != nil {
		return "", chatWorkProjectError(err)
	}
	initial := ""
	for _, taskType := range published.Config.TaskTypes {
		if taskType.ID == chatWorkTaskType && taskType.InitialStatus != "" {
			initial = taskType.InitialStatus
		}
	}
	for _, status := range published.Config.Statuses {
		if initial == "" && !status.Retired {
			initial = status.ID
		}
	}
	if initial == "" {
		return "", ErrChatWorkUnavailable
	}
	source := projectlink.Reference{Kind: projectlink.ChatPost, ID: c.Source.MessageID, ConversationID: c.Source.ConversationID}
	// The task's title is what the person reviewed. The message text is not
	// copied into the project; the link shows it only to people who can read it.
	task, err := s.w.projects.CreateTask(ctx, call.principal, projectservice.CreateTaskRequest{
		ProjectID: call.projectID, Title: call.title, AssigneeID: c.OwnerID, DueDate: call.dueDate,
		InitialStatusID: initial, TypeID: chatWorkTaskType, IdempotencyKey: c.IdempotencyKey,
		ExpectedWorkflowRevision: published.Version, SourceReference: &source,
	})
	if err != nil {
		return "", chatWorkProjectError(err)
	}
	return task.ID, nil
}

func chatWorkProjectError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrChatWorkNotFound), errors.Is(err, ErrChatWorkDenied), errors.Is(err, ErrChatWorkInvalid), errors.Is(err, ErrChatWorkUnavailable):
		return err
	case errors.Is(err, projectservice.ErrInvalidRequest), errors.Is(err, projectservice.ErrUnsupportedLink):
		return ErrChatWorkInvalid
	case errors.Is(err, projectaccess.ErrUnauthorized), errors.Is(err, projectaccess.ErrTenantMismatch), errors.Is(err, projectaccess.ErrMemberNotFound):
		return ErrChatWorkDenied
	default:
		return ErrChatWorkUnavailable
	}
}

// Projects lists the projects the person may create a task in.
func (w *ChatMessageWork) Projects(ctx context.Context, p *trust.Principal) ([]ChatWorkProject, error) {
	if _, err := chatWorkPrincipal(p); err != nil {
		return nil, err
	}
	if w == nil || w.projects == nil {
		return nil, ErrChatWorkUnavailable
	}
	projects, err := w.projects.ListCreatableProjects(ctx, p)
	return projects, chatWorkProjectError(err)
}

// Linked lists what the messages of a conversation became, limited to what the
// viewer may read. The viewer must be able to read the conversation; a task in
// a project they cannot read is left out, so a channel never learns that
// restricted work exists.
func (w *ChatMessageWork) Linked(ctx context.Context, p *trust.Principal, conversationID string) ([]ChatWorkLink, error) {
	if strings.TrimSpace(conversationID) == "" {
		return nil, ErrChatWorkInvalid
	}
	if _, err := w.readMessage(ctx, p, conversationID, ""); err != nil {
		return nil, err
	}
	if w.projects == nil {
		return nil, ErrChatWorkUnavailable
	}
	records, err := w.projects.ListTasksLinkedToChatConversation(ctx, p, conversationID, 500)
	if err != nil {
		return nil, chatWorkProjectError(err)
	}
	statusNames := map[string]map[string]string{}
	statusCategories := map[string]map[string]string{}
	visiblePosts := map[string]bool{}
	out := make([]ChatWorkLink, 0, len(records))
	for _, record := range records {
		// Reading the room is not reading every message in it: a message sent
		// before the viewer joined, or to a narrower audience, stays unseen, and
		// so does the work it became.
		visible, decided := visiblePosts[record.PostID]
		if !decided {
			_, postErr := w.readMessage(ctx, p, conversationID, record.PostID)
			switch {
			case postErr == nil:
				visible = true
			case errors.Is(postErr, ErrChatWorkNotFound):
			default:
				return nil, postErr
			}
			visiblePosts[record.PostID] = visible
		}
		if !visible {
			continue
		}
		names, seen := statusNames[record.ProjectID]
		if !seen {
			names = map[string]string{}
			categories := map[string]string{}
			if published, err := w.projects.Published(ctx, p, record.ProjectID); err == nil {
				for _, status := range published.Config.Statuses {
					names[status.ID] = status.Name
					categories[status.ID] = string(status.Category)
				}
			}
			statusNames[record.ProjectID] = names
			statusCategories[record.ProjectID] = categories
		}
		status := names[record.StatusID]
		if status == "" {
			status = record.StatusID
		}
		out = append(out, ChatWorkLink{PostID: record.PostID, ProjectID: record.ProjectID, TaskID: record.TaskID, Title: record.Title, StatusID: record.StatusID, Status: status, OwnerID: record.AssigneeID, DueDate: record.DueDate, Category: statusCategories[record.ProjectID][record.StatusID]})
	}
	return out, nil
}

// Convert makes a task from a message the person can read, in a project they
// may create tasks in, and returns it as the message shows it.
func (w *ChatMessageWork) Convert(ctx context.Context, p *trust.Principal, in ChatWorkConvertRequest) (ChatWorkLink, error) {
	cp, err := chatWorkPrincipal(p)
	if err != nil {
		return ChatWorkLink{}, err
	}
	if w == nil || w.projects == nil || w.coordinator == nil {
		return ChatWorkLink{}, ErrChatWorkUnavailable
	}
	title := strings.TrimSpace(in.Title)
	if workitem.ConversionTargetKind(in.Target) != workitem.ConversionTargetTask || strings.TrimSpace(in.ConversationID) == "" || strings.TrimSpace(in.MessageID) == "" ||
		strings.TrimSpace(in.ProjectID) == "" || title == "" || utf8.RuneCountInString(title) > chatWorkTitleMax || !chatWorkKeyPattern.MatchString(in.IdempotencyKey) {
		return ChatWorkLink{}, ErrChatWorkInvalid
	}
	due := strings.TrimSpace(in.DueDate)
	if due != "" {
		if _, err := time.Parse("2006-01-02", due); err != nil {
			return ChatWorkLink{}, ErrChatWorkInvalid
		}
	}
	// The message is read before anything else, so a person who cannot open it
	// learns nothing about the project or the owner they named.
	post, err := w.readMessage(ctx, p, in.ConversationID, in.MessageID)
	if err != nil {
		return ChatWorkLink{}, err
	}
	// Issue 3812: a message of a restricted case conversation does not become
	// work in a project, where other people see it.
	if err := w.caseRestriction(ctx, cp.TenantID, in.ConversationID); err != nil {
		return ChatWorkLink{}, err
	}
	owner := strings.TrimSpace(in.OwnerID)
	if owner == "" {
		owner = cp.SubjectID
	}
	if owner != cp.SubjectID && owner != post.AuthorID {
		return ChatWorkLink{}, ErrChatWorkInvalid
	}
	call := &chatWorkCall{principal: p, projectID: in.ProjectID, title: title, dueDate: due}
	ctx = context.WithValue(ctx, chatWorkCallKey{}, call)
	result, err := w.coordinator.Convert(ctx, workitem.MessageConversionRequest{
		Message: workitem.MessageRef{TenantID: chatWorkTenantUUID(cp.TenantID), ConversationID: in.ConversationID, MessageID: in.MessageID},
		ActorID: cp.SubjectID, Target: workitem.ConversionTargetTask, OwnerID: owner,
		SourceContext:  "chat:" + in.ConversationID,
		IdempotencyKey: in.ProjectID + ":" + in.IdempotencyKey,
	})
	if err != nil {
		if errors.Is(err, ErrChatWorkNotFound) || errors.Is(err, ErrChatWorkDenied) || errors.Is(err, ErrChatWorkInvalid) || errors.Is(err, ErrChatWorkUnavailable) {
			return ChatWorkLink{}, err
		}
		// The coordinator's own refusals: an incomplete or conflicting request.
		return ChatWorkLink{}, ErrChatWorkInvalid
	}
	links, err := w.Linked(ctx, p, in.ConversationID)
	if err == nil {
		for _, link := range links {
			if link.TaskID == result.TargetID && link.PostID == in.MessageID {
				return link, nil
			}
		}
	}
	return ChatWorkLink{PostID: in.MessageID, ProjectID: in.ProjectID, TaskID: result.TargetID, Title: title, OwnerID: owner, DueDate: due}, nil
}

// chatWorkProjectsAdapter exposes the project service as ChatWorkProjects.
type chatWorkProjectsAdapter struct{ service projectservice.Service }

func (a chatWorkProjectsAdapter) ready() bool {
	return a.service.Auth != nil && a.service.Workflows != nil
}

func (a chatWorkProjectsAdapter) ListCreatableProjects(ctx context.Context, p *trust.Principal) ([]ChatWorkProject, error) {
	if !a.ready() {
		return nil, projectservice.ErrUnavailable
	}
	out := []ChatWorkProject{}
	after := ""
	for pages := 0; pages < 5; pages++ {
		page, err := a.service.ListProjects(ctx, p, after, projectservice.MaxListPageSize)
		if err != nil {
			return nil, err
		}
		for _, record := range page.Projects {
			if a.service.Auth.Authorize(ctx, p, record.ID, projectaccess.CreateTask) == nil {
				out = append(out, ChatWorkProject{ID: record.ID, Name: record.Name})
			}
		}
		if page.Next == nil {
			break
		}
		after = page.Next.AfterID
	}
	return out, nil
}

func (a chatWorkProjectsAdapter) CanCreateTask(ctx context.Context, p *trust.Principal, projectID string) error {
	if !a.ready() {
		return projectservice.ErrUnavailable
	}
	return a.service.Auth.Authorize(ctx, p, projectID, projectaccess.CreateTask)
}

func (a chatWorkProjectsAdapter) Published(ctx context.Context, p *trust.Principal, projectID string) (projectservice.WorkflowConfiguration, error) {
	if !a.ready() {
		return projectservice.WorkflowConfiguration{}, projectservice.ErrUnavailable
	}
	if err := a.service.Auth.Authorize(ctx, p, projectID, projectaccess.ReadTask); err != nil {
		return projectservice.WorkflowConfiguration{}, err
	}
	return a.service.Workflows.GetPublished(ctx, p.Tenant().String(), projectID)
}

func (a chatWorkProjectsAdapter) CreateTask(ctx context.Context, p *trust.Principal, req projectservice.CreateTaskRequest) (projectservice.TaskRecord, error) {
	return a.service.CreateTask(ctx, p, req)
}

func (a chatWorkProjectsAdapter) ListTasksLinkedToChatConversation(ctx context.Context, p *trust.Principal, conversationID string, limit int) ([]projectservice.ChatLinkedTaskRecord, error) {
	return a.service.ListTasksLinkedToChatConversation(ctx, p, conversationID, limit)
}

// chatMessageWorkFor composes the conversion over the served runtime, or nil
// when Chat or the project service is not composed.
func chatMessageWorkFor(chatLinks projectrefs.ChatLinks, projects projectservice.Service) *ChatMessageWork {
	if chatLinks == nil || projects.Auth == nil || projects.Workflows == nil {
		return nil
	}
	return NewChatMessageWork(chatLinks, chatWorkProjectsAdapter{service: projects})
}
