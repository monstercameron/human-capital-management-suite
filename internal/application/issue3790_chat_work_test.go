package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectlink"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type issue3790Chat struct {
	readable map[string]string // conversation/post -> author
	reads    int
}

func (c *issue3790Chat) ReadAuthorizedReference(_ context.Context, p chat.Principal, tenant, conversation, post string) (chat.Conversation, *chat.Post, error) {
	c.reads++
	if p.TenantID != tenant {
		return chat.Conversation{}, nil, chat.ErrPermissionDenied
	}
	author, ok := c.readable[conversation+"/"+post]
	if !ok {
		return chat.Conversation{}, nil, chat.ErrPermissionDenied
	}
	room := chat.Conversation{ID: conversation, TenantID: tenant}
	if post == "" {
		return room, nil, nil
	}
	return room, &chat.Post{ID: post, TenantID: tenant, ConversationID: conversation, AuthorID: author}, nil
}

type issue3790Projects struct {
	creatable map[string]bool
	created   []projectservice.CreateTaskRequest
	byKey     map[string]projectservice.TaskRecord
	linked    []projectservice.ChatLinkedTaskRecord
}

func (p *issue3790Projects) ListCreatableProjects(context.Context, *trust.Principal) ([]ChatWorkProject, error) {
	return []ChatWorkProject{{ID: "ops", Name: "IT operations"}}, nil
}
func (p *issue3790Projects) CanCreateTask(_ context.Context, _ *trust.Principal, id string) error {
	if !p.creatable[id] {
		return projectaccess.ErrUnauthorized
	}
	return nil
}
func (p *issue3790Projects) Published(context.Context, *trust.Principal, string) (projectservice.WorkflowConfiguration, error) {
	return projectservice.WorkflowConfiguration{Version: 4, Config: projectworkflow.Config{
		TaskTypes: []projectworkflow.TaskType{{ID: "task_default", InitialStatus: "todo"}},
		Statuses:  []projectworkflow.Status{{ID: "todo", Name: "To do"}},
	}}, nil
}
func (p *issue3790Projects) CreateTask(_ context.Context, _ *trust.Principal, r projectservice.CreateTaskRequest) (projectservice.TaskRecord, error) {
	if prior, ok := p.byKey[r.IdempotencyKey]; ok {
		return prior, nil
	}
	p.created = append(p.created, r)
	task := projectservice.TaskRecord{ID: "task-" + r.IdempotencyKey, ProjectID: r.ProjectID, Title: r.Title}
	p.byKey[r.IdempotencyKey] = task
	p.linked = append(p.linked, projectservice.ChatLinkedTaskRecord{PostID: r.SourceReference.ID, ProjectID: r.ProjectID, TaskID: task.ID, Title: r.Title, StatusID: r.InitialStatusID, AssigneeID: r.AssigneeID, DueDate: r.DueDate})
	return task, nil
}
func (p *issue3790Projects) ListTasksLinkedToChatConversation(context.Context, *trust.Principal, string, int) ([]projectservice.ChatLinkedTaskRecord, error) {
	return p.linked, nil
}

func issue3790Principal(t *testing.T, subject string) *trust.Principal {
	t.Helper()
	now := time.Now()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant-a"), Subject: subject, SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceLow, SessionRef: "session", IssuedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CredentialDigest: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func issue3790Work() (*ChatMessageWork, *issue3790Chat, *issue3790Projects) {
	room := &issue3790Chat{readable: map[string]string{"general/": "", "general/m1": "dana", "private/m2": "sam"}}
	projects := &issue3790Projects{creatable: map[string]bool{"ops": true}, byKey: map[string]projectservice.TaskRecord{}}
	return NewChatMessageWork(room, projects), room, projects
}

func issue3790Request(key string) ChatWorkConvertRequest {
	return ChatWorkConvertRequest{ConversationID: "general", MessageID: "m1", Target: "TASK", ProjectID: "ops", Title: "Swap Dana's laptop", OwnerID: "manager", DueDate: "2026-12-04", IdempotencyKey: key}
}

// Issue 3790: a message becomes an owned, due-dated task that links the message.
func TestIssue3790_ConvertMakesAnOwnedDueTaskLinkedToTheMessage_Integration(t *testing.T) {
	work, _, projects := issue3790Work()
	link, err := work.Convert(context.Background(), issue3790Principal(t, "manager"), issue3790Request("attempt-0001"))
	if err != nil {
		t.Fatal(err)
	}
	if len(projects.created) != 1 {
		t.Fatalf("created %d tasks", len(projects.created))
	}
	got := projects.created[0]
	if got.Title != "Swap Dana's laptop" || got.AssigneeID != "manager" || got.DueDate != "2026-12-04" || got.InitialStatusID != "todo" || got.ExpectedWorkflowRevision != 4 {
		t.Fatalf("task request=%+v", got)
	}
	if got.SourceReference == nil || *got.SourceReference != (projectlink.Reference{Kind: projectlink.ChatPost, ID: "m1", ConversationID: "general"}) {
		t.Fatalf("source=%+v", got.SourceReference)
	}
	if strings.Contains(got.Description, "laptop") {
		t.Fatal("message text was copied into the project")
	}
	if link.PostID != "m1" || link.Status != "To do" || link.OwnerID != "manager" || link.DueDate != "2026-12-04" || link.TaskID == "" {
		t.Fatalf("link=%+v", link)
	}
}

// A retry of the same reviewed request recovers the same task; a second,
// separately reviewed request from the same message is a second task.
func TestIssue3790_RetryReusesTheTaskAndAnotherAttemptMakesAnother(t *testing.T) {
	work, _, projects := issue3790Work()
	p := issue3790Principal(t, "manager")
	first, err := work.Convert(context.Background(), p, issue3790Request("attempt-0001"))
	if err != nil {
		t.Fatal(err)
	}
	again, err := work.Convert(context.Background(), p, issue3790Request("attempt-0001"))
	if err != nil || again.TaskID != first.TaskID || len(projects.created) != 1 {
		t.Fatalf("retry=%+v err=%v created=%d", again, err, len(projects.created))
	}
	other := issue3790Request("attempt-0002")
	other.Title = "Order a docking station"
	second, err := work.Convert(context.Background(), p, other)
	if err != nil || second.TaskID == first.TaskID || len(projects.created) != 2 {
		t.Fatalf("second=%+v err=%v created=%d", second, err, len(projects.created))
	}
	// The same key with another owner is a conflict, not a silent second task.
	changed := issue3790Request("attempt-0001")
	changed.OwnerID = "dana"
	if _, err := work.Convert(context.Background(), p, changed); !errors.Is(err, ErrChatWorkInvalid) || len(projects.created) != 2 {
		t.Fatalf("changed owner err=%v created=%d", err, len(projects.created))
	}
}

// Authorization comes before any side effect: a person who cannot read the
// message, or cannot create in the project, creates nothing, and the two
// refusals do not reveal whether the message or project exists.
func TestIssue3790_ConvertRefusesBeforeAnySideEffect_Security(t *testing.T) {
	work, _, projects := issue3790Work()
	p := issue3790Principal(t, "manager")
	unreadable := issue3790Request("attempt-0003")
	unreadable.ConversationID, unreadable.MessageID = "hr-private", "secret"
	if _, err := work.Convert(context.Background(), p, unreadable); !errors.Is(err, ErrChatWorkNotFound) {
		t.Fatalf("unreadable message err=%v", err)
	}
	noProject := issue3790Request("attempt-0004")
	noProject.ProjectID = "finance"
	if _, err := work.Convert(context.Background(), p, noProject); !errors.Is(err, ErrChatWorkDenied) {
		t.Fatalf("project without create access err=%v", err)
	}
	badOwner := issue3790Request("attempt-0005")
	badOwner.OwnerID = "someone-else"
	if _, err := work.Convert(context.Background(), p, badOwner); !errors.Is(err, ErrChatWorkInvalid) {
		t.Fatalf("owner outside the conversation err=%v", err)
	}
	for name, mutate := range map[string]func(*ChatWorkConvertRequest){
		"no title":     func(r *ChatWorkConvertRequest) { r.Title = "  " },
		"bad due date": func(r *ChatWorkConvertRequest) { r.DueDate = "Friday" },
		"short key":    func(r *ChatWorkConvertRequest) { r.IdempotencyKey = "x" },
		"request type": func(r *ChatWorkConvertRequest) { r.Target = "REQUEST" },
	} {
		r := issue3790Request("attempt-0006")
		mutate(&r)
		if _, err := work.Convert(context.Background(), p, r); !errors.Is(err, ErrChatWorkInvalid) {
			t.Fatalf("%s err=%v", name, err)
		}
	}
	if len(projects.created) != 0 {
		t.Fatalf("a refused conversion created %d tasks", len(projects.created))
	}
}

// The message shows what it became only to someone who can read the room.
func TestIssue3790_LinkedWorkRequiresReadingTheConversation_Security(t *testing.T) {
	work, _, projects := issue3790Work()
	if _, err := work.Convert(context.Background(), issue3790Principal(t, "manager"), issue3790Request("attempt-0001")); err != nil {
		t.Fatal(err)
	}
	links, err := work.Linked(context.Background(), issue3790Principal(t, "manager"), "general")
	if err != nil || len(links) != 1 || links[0].Status != "To do" || len(projects.linked) != 1 {
		t.Fatalf("links=%+v err=%v", links, err)
	}
	if _, err := work.Linked(context.Background(), issue3790Principal(t, "manager"), "hr-private"); !errors.Is(err, ErrChatWorkNotFound) {
		t.Fatalf("unreadable conversation err=%v", err)
	}
}

// Reading a room is not reading each of its messages: work made from a message
// the viewer cannot open (sent before they joined, or to a narrower audience)
// is not listed to them, and no placeholder stands in for it.
func TestIssue3790_LinkedWorkOmitsTasksFromMessagesTheViewerCannotRead_Security(t *testing.T) {
	work, _, projects := issue3790Work()
	if _, err := work.Convert(context.Background(), issue3790Principal(t, "manager"), issue3790Request("attempt-0001")); err != nil {
		t.Fatal(err)
	}
	projects.linked = append(projects.linked, projectservice.ChatLinkedTaskRecord{PostID: "m-before-join", ProjectID: "ops", TaskID: "task-hidden", Title: "Quiet reorganisation", StatusID: "todo"})
	links, err := work.Linked(context.Background(), issue3790Principal(t, "manager"), "general")
	if err != nil || len(links) != 1 || links[0].PostID != "m1" {
		t.Fatalf("links=%+v err=%v", links, err)
	}
	for _, link := range links {
		if link.TaskID == "task-hidden" || strings.Contains(link.Title, "reorganisation") {
			t.Fatalf("work from an unreadable message was listed: %+v", link)
		}
	}
}
