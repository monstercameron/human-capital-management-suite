package application

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type issue3791Links struct {
	links []chatstore.WorkLink
	err   error
	asked []string
}

func (l *issue3791Links) ListWorkLinks(_ context.Context, tenant, conversation string, _ int) ([]chatstore.WorkLink, error) {
	l.asked = append(l.asked, tenant+"/"+conversation)
	return l.links, l.err
}

type issue3791Intents struct {
	byID map[string]*intentsv1.IntentInstance
	errs map[string]error
	read []string
}

func (i *issue3791Intents) ReadIntent(_ context.Context, _ *trust.Principal, id string) (*intentsv1.IntentInstance, error) {
	i.read = append(i.read, id)
	if err := i.errs[id]; err != nil {
		return nil, err
	}
	if inst, ok := i.byID[id]; ok {
		return inst, nil
	}
	return nil, ErrChatWorkNotFound
}

type issue3791Tasks struct {
	links []ChatWorkLink
	err   error
}

func (t issue3791Tasks) Linked(context.Context, *trust.Principal, string) ([]ChatWorkLink, error) {
	return t.links, t.err
}

func issue3791Instance(id string, request intentsv1.RequestState, execution intentsv1.ExecutionState, business intentsv1.BusinessState, consistency intentsv1.ConsistencyState) *intentsv1.IntentInstance {
	return &intentsv1.IntentInstance{IntentId: id, InstanceVersion: 3,
		Definition: &intentsv1.DefinitionReference{IntentTypeId: "promotion.request"},
		Lifecycle:  &intentsv1.LifecycleDimensions{Request: request, Execution: execution, Business: business, Consistency: consistency}}
}

func issue3791Panel(links *issue3791Links, intents *issue3791Intents, tasks ChatWorkTasks) *ChatLinkedWork {
	room := &issue3790Chat{readable: map[string]string{"general/": "", "quiet/": ""}}
	return &ChatLinkedWork{Chat: room, Links: links, Intents: intents, Tasks: tasks, Now: func() time.Time { return time.Date(2026, 11, 19, 9, 30, 0, 0, time.UTC) }}
}

func issue3791Link(id string) chatstore.WorkLink {
	return chatstore.WorkLink{TenantID: "tenant-a", ConversationID: "general", Kind: chatstore.WorkLinkIntentProposal, SourceID: id, ProposedByID: "agent-user", IntentType: "promotion.request"}
}

func TestIssue3791_IntentStatesAreTold_Honestly(t *testing.T) {
	r, e, b, c := intentsv1.RequestState_REQUEST_STATE_SUBMITTED, intentsv1.ExecutionState_EXECUTION_STATE_NOT_PLANNED, intentsv1.BusinessState_BUSINESS_STATE_NOT_STARTED, intentsv1.ConsistencyState_CONSISTENCY_STATE_NOT_APPLICABLE
	approved := intentsv1.RequestState_REQUEST_STATE_APPROVED
	cases := []struct {
		name string
		inst *intentsv1.IntentInstance
		want string
	}{
		{"draft", issue3791Instance("i", intentsv1.RequestState_REQUEST_STATE_DRAFT, e, b, c), "proposed"},
		{"submitted waits for approval", issue3791Instance("i", r, e, b, c), "awaiting_approval"},
		{"approved is not executed", issue3791Instance("i", approved, e, b, c), "approved"},
		{"rejected", issue3791Instance("i", intentsv1.RequestState_REQUEST_STATE_REJECTED, e, b, c), "closed"},
		{"scheduled", issue3791Instance("i", approved, intentsv1.ExecutionState_EXECUTION_STATE_SCHEDULED, b, c), "scheduled"},
		{"executing", issue3791Instance("i", approved, intentsv1.ExecutionState_EXECUTION_STATE_EXECUTING, b, c), "executing"},
		{"committed but effects still pending is not done", issue3791Instance("i", approved, intentsv1.ExecutionState_EXECUTION_STATE_COMMITTED, intentsv1.BusinessState_BUSINESS_STATE_IN_PROGRESS, intentsv1.ConsistencyState_CONSISTENCY_STATE_PENDING_OBSERVATION), "executed"},
		{"committed and reconciled", issue3791Instance("i", approved, intentsv1.ExecutionState_EXECUTION_STATE_COMMITTED, intentsv1.BusinessState_BUSINESS_STATE_COMPLETED, intentsv1.ConsistencyState_CONSISTENCY_STATE_CONSISTENT), "done"},
		{"committed but consistency unknown", issue3791Instance("i", approved, intentsv1.ExecutionState_EXECUTION_STATE_COMMITTED, intentsv1.BusinessState_BUSINESS_STATE_COMPLETED, intentsv1.ConsistencyState_CONSISTENCY_STATE_UNKNOWN), "unknown"},
		{"blocked", issue3791Instance("i", approved, intentsv1.ExecutionState_EXECUTION_STATE_BLOCKED, b, c), "failed"},
		{"repair", issue3791Instance("i", approved, intentsv1.ExecutionState_EXECUTION_STATE_REPAIR_REQUIRED, b, c), "failed"},
		{"business goal not achieved", issue3791Instance("i", approved, intentsv1.ExecutionState_EXECUTION_STATE_COMMITTED, intentsv1.BusinessState_BUSINESS_STATE_NOT_ACHIEVED, c), "failed"},
		{"nothing said", issue3791Instance("i", intentsv1.RequestState_REQUEST_STATE_UNSPECIFIED, intentsv1.ExecutionState_EXECUTION_STATE_UNSPECIFIED, b, c), "unknown"},
		{"no lifecycle at all", &intentsv1.IntentInstance{IntentId: "i"}, "unknown"},
	}
	for _, tc := range cases {
		if got := chatWorkIntentState(tc.inst); got != tc.want {
			t.Errorf("%s: state %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The panel lists what the conversation proposed with an honest state, the
// source it came from and when it was read, the most pressing work first.
func TestIssue3791_PanelListsProposalsAndTasksWithStateSourceAndFreshness_Integration(t *testing.T) {
	links := &issue3791Links{links: []chatstore.WorkLink{issue3791Link("done-1"), issue3791Link("wait-1"), issue3791Link("new-1")}}
	approved := intentsv1.RequestState_REQUEST_STATE_APPROVED
	intents := &issue3791Intents{byID: map[string]*intentsv1.IntentInstance{
		"done-1": issue3791Instance("done-1", approved, intentsv1.ExecutionState_EXECUTION_STATE_COMMITTED, intentsv1.BusinessState_BUSINESS_STATE_COMPLETED, intentsv1.ConsistencyState_CONSISTENCY_STATE_CONSISTENT),
		"wait-1": issue3791Instance("wait-1", intentsv1.RequestState_REQUEST_STATE_SUBMITTED, intentsv1.ExecutionState_EXECUTION_STATE_NOT_PLANNED, intentsv1.BusinessState_BUSINESS_STATE_NOT_STARTED, intentsv1.ConsistencyState_CONSISTENCY_STATE_NOT_APPLICABLE),
		"new-1":  issue3791Instance("new-1", intentsv1.RequestState_REQUEST_STATE_DRAFT, intentsv1.ExecutionState_EXECUTION_STATE_NOT_PLANNED, intentsv1.BusinessState_BUSINESS_STATE_NOT_STARTED, intentsv1.ConsistencyState_CONSISTENCY_STATE_NOT_APPLICABLE),
	}}
	tasks := issue3791Tasks{links: []ChatWorkLink{{PostID: "m1", ProjectID: "ops", TaskID: "t1", Title: "Swap laptop", Status: "To do", StatusID: "todo", Category: "ACTIVE", OwnerID: "dana", DueDate: "2026-12-04"}}}
	got, err := issue3791Panel(links, intents, tasks).List(context.Background(), issue3790Principal(t, "dana"), "general")
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, item := range got.Items {
		order = append(order, item.Kind+":"+item.ID+":"+item.State)
	}
	want := []string{"intent:wait-1:awaiting_approval", "intent:new-1:proposed", "task:t1:open", "intent:done-1:done"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("items %v, want %v", order, want)
	}
	first := got.Items[0]
	if first.Source != "approvals" || first.ObservedAt != "2026-11-19T09:30:00Z" || first.Revision != 3 || first.Title != "Promotion request" || !strings.Contains(first.Href, "journey=wait-1") {
		t.Fatalf("first %+v", first)
	}
	task := got.Items[2]
	if task.Source != "tasks" || task.Detail != "To do" || task.OwnerID != "dana" || task.DueDate != "2026-12-04" || task.ProjectID != "ops" || task.TaskID != "t1" {
		t.Fatalf("task %+v", task)
	}
	if got.AsOf != "2026-11-19T09:30:00Z" || len(got.Incomplete) != 0 {
		t.Fatalf("result %+v", got)
	}
	if !reflect.DeepEqual(links.asked, []string{"tenant-a/general"}) {
		t.Fatalf("links read for %v", links.asked)
	}
}

// Work the viewer may not open is left out entirely: no row, no count, no
// placeholder, and nothing in the answer that says it exists.
func TestIssue3791_PanelOmitsWorkTheViewerCannotOpen_Security(t *testing.T) {
	links := &issue3791Links{links: []chatstore.WorkLink{issue3791Link("mine"), issue3791Link("secret-promotion")}}
	intents := &issue3791Intents{byID: map[string]*intentsv1.IntentInstance{
		"mine": issue3791Instance("mine", intentsv1.RequestState_REQUEST_STATE_SUBMITTED, intentsv1.ExecutionState_EXECUTION_STATE_NOT_PLANNED, 0, 0),
	}}
	got, err := issue3791Panel(links, intents, nil).List(context.Background(), issue3790Principal(t, "dana"), "general")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].ID != "mine" || len(got.Incomplete) != 0 {
		t.Fatalf("items %+v incomplete %v", got.Items, got.Incomplete)
	}
	if !reflect.DeepEqual(intents.read, []string{"mine", "secret-promotion"}) {
		t.Fatalf("each proposal must be read under the viewer's authority: %v", intents.read)
	}
	// A denial reads the same as an absence.
	intents.errs = map[string]error{"secret-promotion": ErrChatWorkDenied}
	again, err := issue3791Panel(links, intents, nil).List(context.Background(), issue3790Principal(t, "dana"), "general")
	if err != nil || len(again.Items) != 1 || len(again.Incomplete) != 0 {
		t.Fatalf("denial %+v %v", again, err)
	}
}

// A source that cannot answer is never read as an empty list or as done: the
// answer says it is incomplete, and no row for it is invented.
func TestIssue3791_PanelSaysIncompleteWhenASourceCannotAnswer_Security(t *testing.T) {
	links := &issue3791Links{links: []chatstore.WorkLink{issue3791Link("ok"), issue3791Link("flaky")}}
	intents := &issue3791Intents{byID: map[string]*intentsv1.IntentInstance{
		"ok": issue3791Instance("ok", intentsv1.RequestState_REQUEST_STATE_SUBMITTED, 0, 0, 0),
	}, errs: map[string]error{"flaky": ErrChatWorkUnavailable}}
	got, err := issue3791Panel(links, intents, issue3791Tasks{err: ErrChatWorkUnavailable}).List(context.Background(), issue3790Principal(t, "dana"), "general")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].ID != "ok" {
		t.Fatalf("items %+v", got.Items)
	}
	if !reflect.DeepEqual(got.Incomplete, []string{"approvals", "tasks"}) {
		t.Fatalf("incomplete %v", got.Incomplete)
	}
	broken := &issue3791Links{err: errors.New("db down")}
	got, err = issue3791Panel(broken, intents, nil).List(context.Background(), issue3790Principal(t, "dana"), "general")
	if err != nil || len(got.Items) != 0 || !reflect.DeepEqual(got.Incomplete, []string{"approvals"}) {
		t.Fatalf("store down %+v %v", got, err)
	}
}

// A person who cannot read the conversation gets nothing about its work, not
// even that there is any, and the proposals are never read for them.
func TestIssue3791_PanelRefusesBeforeReadingWork_Security(t *testing.T) {
	links := &issue3791Links{links: []chatstore.WorkLink{issue3791Link("mine")}}
	intents := &issue3791Intents{byID: map[string]*intentsv1.IntentInstance{}}
	panel := issue3791Panel(links, intents, issue3791Tasks{})
	if _, err := panel.List(context.Background(), issue3790Principal(t, "dana"), "private"); !errors.Is(err, ErrChatWorkNotFound) {
		t.Fatalf("unreadable conversation: %v", err)
	}
	if _, err := panel.List(context.Background(), issue3790Principal(t, "dana"), " "); !errors.Is(err, ErrChatWorkInvalid) {
		t.Fatalf("blank conversation: %v", err)
	}
	now := time.Now()
	agent, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant-a"), Subject: "bot", SubjectKind: trust.SubjectKindAgent, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceLow, SessionRef: "s", IssuedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CredentialDigest: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := panel.List(context.Background(), agent, "general"); !errors.Is(err, ErrChatWorkDenied) {
		t.Fatalf("agent: %v", err)
	}
	if len(links.asked) != 0 || len(intents.read) != 0 {
		t.Fatalf("work was read before the refusal: links %v intents %v", links.asked, intents.read)
	}
}

// Without a task source the panel still answers for proposals, and it does not
// claim tasks were checked.
func TestIssue3791_PanelWorksWithoutATaskSource(t *testing.T) {
	links := &issue3791Links{}
	got, err := issue3791Panel(links, &issue3791Intents{}, nil).List(context.Background(), issue3790Principal(t, "dana"), "general")
	if err != nil || got.Items == nil || len(got.Items) != 0 || len(got.Incomplete) != 0 {
		t.Fatalf("%+v %v", got, err)
	}
}
