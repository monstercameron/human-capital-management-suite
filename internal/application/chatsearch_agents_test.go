package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// chatsearchAgentsAvailable answers with the agents one person may use and
// records who asked, so a test can see that nobody else's list was read.
type chatsearchAgentsAvailable struct {
	versions []agentpersona.PersonaVersion
	asked    []string
	err      error
}

func (f *chatsearchAgentsAvailable) ListAvailable(_ context.Context, p *trust.Principal) ([]agentpersona.PersonaVersion, error) {
	f.asked = append(f.asked, p.Subject())
	return f.versions, f.err
}

type chatsearchAgentTaskList []agentrun.AgentTask

func (f chatsearchAgentTaskList) ListAgentTasks(context.Context, *trust.Principal) ([]agentrun.AgentTask, error) {
	return f, nil
}

type chatsearchAnnouncementList []agentstore.Announcement

func (f chatsearchAnnouncementList) SearchableAnnouncements(context.Context) ([]agentstore.Announcement, error) {
	return f, nil
}

func chatsearchAgentsRegistry(t *testing.T) (*chatsearch.Registry, *chatsearchAgentsAvailable, context.Context, chatsearch.Request) {
	t.Helper()
	at := time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)
	agents := &chatsearchAgentsAvailable{versions: []agentpersona.PersonaVersion{
		agentUserCatalogPersona(t, "holiday", "Holiday Helper", "Answers questions about the holiday guide.", agentpersonaSkillPin()),
		agentUserCatalogPersona(t, "coach", "People Coach", "Helps with policy questions.", agentpersonaSkillPin()),
	}}
	tasks := chatsearchAgentTaskList{
		{ID: "task-mine", TenantID: "tenant-a", UserID: "user-a", Goal: "Summarise the holiday guide", State: agentrun.StateCompleted, CreatedAt: at, ExpiresAt: at.Add(-time.Hour)},
		// A reader that hands over somebody else's task, or another workspace's,
		// must not make it findable.
		{ID: "task-theirs", TenantID: "tenant-a", UserID: "user-b", Goal: "Holiday pay for user-b", CreatedAt: at},
		{ID: "task-foreign", TenantID: "tenant-b", UserID: "user-a", Goal: "Holiday in another workspace", CreatedAt: at},
	}
	announcements := chatsearchAnnouncementList{
		{ID: "announce-mine", OwnerID: "user-a", ConversationID: "channel-a", Instruction: "Post the holiday calendar every Monday", State: agentstore.AnnouncementActive, UpdatedAt: at},
		{ID: "announce-deleted", OwnerID: "user-a", ConversationID: "channel-a", Instruction: "Old holiday notice", State: agentstore.AnnouncementDeleted, UpdatedAt: at},
		{ID: "announce-theirs", OwnerID: "user-b", ConversationID: "channel-b", Instruction: "Holiday rota for the other team", State: agentstore.AnnouncementActive, UpdatedAt: at},
	}
	registry := chatsearch.NewRegistry()
	if err := RegisterChatSearchAgents(registry, agents, tasks, announcements); err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), agentUserCatalogPrincipal(t))
	q := chatsearch.Request{Actor: chatsearch.Actor{TenantID: "tenant-a", HomeTenantID: "tenant-a", PersonID: "user-a"}, Query: "holiday", At: at}
	return registry, agents, ctx, q
}

func chatsearchAgentsFound(t *testing.T, registry *chatsearch.Registry, ctx context.Context, q chatsearch.Request) map[chatsearch.Kind][]chatsearch.Row {
	t.Helper()
	response, err := registry.Search(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Unavailable) != 0 {
		t.Fatalf("sources unavailable: %v %v", response.Unavailable, response.Failures)
	}
	found := map[chatsearch.Kind][]chatsearch.Row{}
	for _, group := range response.Groups {
		found[group.Kind] = group.Rows
	}
	return found
}

// TestTodo_CHATSEARCH_003: agents, a person's own tasks and their own
// announcements are found from Chat search, each under its own kind, by the
// words a person would use, and each names the record its page opens.
func TestTodo_CHATSEARCH_003(t *testing.T) {
	registry, _, ctx, q := chatsearchAgentsRegistry(t)
	response, err := registry.Search(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	served := map[chatsearch.Kind]bool{}
	for _, kind := range response.Kinds {
		served[kind] = true
	}
	for _, d := range chatsearch.AgentDeclarations() {
		if !served[d.Kind] {
			t.Fatalf("the answer does not name %s among the kinds it can find: %v", d.Kind, response.Kinds)
		}
	}
	found := chatsearchAgentsFound(t, registry, ctx, q)
	agent := found[chatsearch.Agent]
	if len(agent) != 1 || agent[0].ID != "persona.holiday" || agent[0].Text != "Holiday Helper\nAnswers questions about the holiday guide." || agent[0].Target.ItemID != "persona.holiday" || agent[0].Private {
		t.Fatalf("agents=%+v", agent)
	}
	task := found[chatsearch.AgentTask]
	if len(task) != 1 || task[0].ID != "task-mine" || task[0].Text != "Summarise the holiday guide" || !task[0].Private || task[0].OwnerID != "user-a" || task[0].Target.ItemID != "task-mine" || !task[0].ExpiresAt.IsZero() {
		t.Fatalf("tasks=%+v", task)
	}
	announcement := found[chatsearch.AgentAnnouncement]
	if len(announcement) != 1 || announcement[0].ID != "announce-mine" || !announcement[0].Private || announcement[0].Target != (chatsearch.Target{ConversationID: "channel-a", ItemID: "announce-mine"}) {
		t.Fatalf("announcements=%+v", announcement)
	}
	// The kind filter narrows to one of them, and the purpose is searched too.
	q.Query, q.Filters.Kind = "policy", chatsearch.Agent
	if found = chatsearchAgentsFound(t, registry, ctx, q); len(found) != 1 || len(found[chatsearch.Agent]) != 1 || found[chatsearch.Agent][0].ID != "persona.coach" {
		t.Fatalf("kind:agent policy = %+v", found)
	}
	// A reader that is not composed leaves its kind out of the registry.
	bare := chatsearch.NewRegistry()
	if err := RegisterChatSearchAgents(bare, nil, chatsearchAgentTaskList{}, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := bare.Declaration(chatsearch.Agent); ok {
		t.Fatal("agents are declared with no reader behind them")
	}
	if _, ok := bare.Declaration(chatsearch.AgentTask); !ok {
		t.Fatal("tasks are not declared although their reader is composed")
	}
	// A search with no registry behind it is left alone, and a second
	// registration of the same kinds is refused.
	if err := RegisterChatSearchAgents(&chatsearchHTTPFixture{}, &chatsearchAgentsAvailable{}, nil, nil); err != nil {
		t.Fatalf("a port that is not a registry: %v", err)
	}
	if err := RegisterChatSearchAgents(registry, &chatsearchAgentsAvailable{}, nil, nil); !errors.Is(err, chatsearch.ErrRegistry) {
		t.Fatalf("a second source for agents: %v", err)
	}
}

// TestTodo_CHATSEARCH_003_Security: a person finds only the agents they may
// use, only their own tasks and only their own announcements; an agent's
// instructions are never searched or returned; and a search made for anybody
// but the verified person of the request finds nothing.
func TestTodo_CHATSEARCH_003_Security(t *testing.T) {
	registry, agents, ctx, q := chatsearchAgentsRegistry(t)
	found := chatsearchAgentsFound(t, registry, ctx, q)
	raw, _ := json.Marshal(found)
	for _, hidden := range []string{"task-theirs", "task-foreign", "announce-theirs", "announce-deleted", "user-b", "Answer within the declared skill set", "instructions"} {
		if strings.Contains(string(raw), hidden) {
			t.Fatalf("the results carry %q: %s", hidden, raw)
		}
	}
	// The instructions are not matched either.
	q.Query = "declared skill"
	if found = chatsearchAgentsFound(t, registry, ctx, q); len(found) != 0 {
		t.Fatalf("an agent was found by its instructions: %+v", found)
	}
	q.Query = "holiday"
	// The same request without a verified person, for another person, for
	// another workspace, or across workspaces, finds nothing and reads nothing.
	agents.asked = nil
	other := q
	other.Actor.PersonID = "user-b"
	foreign := q
	foreign.Actor.TenantID, foreign.Actor.HomeTenantID = "tenant-b", "tenant-b"
	guest := q
	guest.Actor.HomeTenantID = "tenant-b"
	for name, attempt := range map[string]struct {
		ctx context.Context
		q   chatsearch.Request
	}{"no verified person": {context.Background(), q}, "another person": {ctx, other}, "another workspace": {ctx, foreign}, "a guest": {ctx, guest}} {
		if found := chatsearchAgentsFound(t, registry, attempt.ctx, attempt.q); len(found) != 0 {
			t.Fatalf("%s found %+v", name, found)
		}
	}
	if len(agents.asked) != 0 {
		t.Fatalf("the agent list was read for %v", agents.asked)
	}
	// An agent the person may no longer use is gone from the next search, and a
	// result held from before is refused at the recheck.
	held := chatsearchAgentsFound(t, registry, ctx, q)[chatsearch.Agent]
	if len(held) != 1 {
		t.Fatalf("held=%+v", held)
	}
	agents.versions = agents.versions[1:]
	if found = chatsearchAgentsFound(t, registry, ctx, q); len(found[chatsearch.Agent]) != 0 {
		t.Fatalf("a withdrawn agent is still found: %+v", found)
	}
	source := chatsearchAgentSource(chatsearch.Agent, chatsearchAgentRows(agents))
	if open, err := source.CanOpen(ctx, q.Actor, held[0]); err != nil || open {
		t.Fatalf("a withdrawn agent's result still opens: %v %v", open, err)
	}
	// A reader that fails makes its own kind unavailable and no other.
	agents.err = errors.New("agent store detail")
	response, err := registry.Search(ctx, q)
	if err != nil || len(response.Unavailable) != 1 || response.Unavailable[0] != chatsearch.Agent {
		t.Fatalf("unavailable=%v err=%v", response.Unavailable, err)
	}
	body, _ := json.Marshal(response)
	if strings.Contains(string(body), "agent store detail") {
		t.Fatalf("the cause left the server: %s", body)
	}
	if len(response.Groups) != 2 {
		t.Fatalf("the other kinds did not answer: %+v", response.Groups)
	}
}

// TestTodo_CHATSEARCH_002_Transient: a search the workspace box sends while a
// person types is answered and is not remembered as a recent Chat search.
func TestTodo_CHATSEARCH_002_Transient(t *testing.T) {
	f := &chatsearchHTTPFixture{}
	history := chatsearch.NewRecent()
	h := chatsearchTestHTTP(t, f, history)
	w := chatsearchHTTPCall(h, "POST", ChatSearchPath, `{"Query":"budg","Limit":16,"Transient":true}`, "fixture")
	if w.Code != 200 || f.calls != 1 || !f.q.Transient {
		t.Fatalf("search %d %s %+v", w.Code, w.Body, f.q)
	}
	if recent := history.List(f.q.Actor); len(recent) != 0 {
		t.Fatalf("a typed workspace search was remembered: %v", recent)
	}
	if w = chatsearchHTTPCall(h, "POST", ChatSearchPath, `{"Query":"budget"}`, "fixture"); w.Code != 200 || len(history.List(f.q.Actor)) != 1 {
		t.Fatalf("a Chat search was not remembered: %d %v", w.Code, history.List(f.q.Actor))
	}
}
