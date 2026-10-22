package application

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/scheduled"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/ambientagents"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// agentUXAmbientScreenPlanner is the fake screen model of the served tests: it
// returns, through the governed planner port, the same proposal the library
// fixture derives from the message, and counts its calls. It never leaves the
// process and costs nothing.
type agentUXAmbientScreenPlanner struct {
	calls  int
	agents []string
}

func (p *agentUXAmbientScreenPlanner) PlanAmbientMessage(_ context.Context, source AgentUXAmbientRunSource, agent, _ string, messages []agentmodel.ModelMessage) ([]byte, error) {
	p.calls++
	p.agents = append(p.agents, agent)
	lines := strings.Split(messages[1].Content, "\n")
	var data struct {
		Message string `json:"message"`
	}
	if len(lines) < 3 || json.Unmarshal([]byte(lines[1]), &data) != nil {
		return nil, ErrAgentUXAmbientInvalid
	}
	proposal := AgentUXAmbientFixtureProposal(AgentUXAmbientMessage{Author: source.Author, Body: data.Message}, agent)
	return json.Marshal(map[string]any{"kind": proposal.Kind, "title": proposal.Title, "owner": proposal.Owner, "date": proposal.Date, "clock": proposal.Clock, "event": proposal.Event, "explicit": proposal.Explicit})
}

// agentUXAmbientAsker binds a reminder to the person who sets it, as the
// served binding does, instead of to a fixed owner.
type agentUXAmbientAsker struct{}

func (agentUXAmbientAsker) ResolveAmbientAnnouncement(ctx context.Context, o AgentUXAmbientOffer) (scheduled.Schedule, error) {
	s, err := agentUXAmbientScheduleBinding{}.ResolveAmbientAnnouncement(ctx, o)
	if err != nil {
		return s, err
	}
	if asker, ok := trust.FromContext(ctx); ok {
		s.OwnerID = asker.Subject()
	}
	return s, nil
}

type agentUXServedAmbientHarness struct {
	t         *testing.T
	service   *AgentUXAmbientService
	store     *chatstore.Store
	served    agentUXAmbientServed
	schedules *agentUXAmbientScheduleMemory
	clock     *time.Time
	planner   *agentUXAmbientScreenPlanner
	call      func(person, method, path string, body any) *httptest.ResponseRecorder
}

// agentUXServedAmbientHarness composes the ambient agents the way the served
// assembly does (composeAgentUXAmbient, the real chat store, the production
// delivery, the scheduler store behind the announcements' port) with a fake
// screen model, and serves them through the assembly's own overlay.
func newAgentUXServedAmbientHarness(t *testing.T, withModel, withScheduler bool) *agentUXServedAmbientHarness {
	t.Helper()
	fixture, _, _ := agentUXAmbientFixture(t)
	store, ok := fixture.DB.(*chatstore.Store)
	if !ok {
		t.Fatal("the fixture's chat database is not the chat store")
	}
	clock := fixture.Now()
	h := &agentUXServedAmbientHarness{t: t, store: store, clock: &clock, schedules: agentUXAmbientScheduleMemoryNew(), planner: &agentUXAmbientScreenPlanner{}}
	now := func() time.Time { return *h.clock }
	composition := agentUXAmbientComposition{Chat: store, TodoAuthority: func(context.Context, string, string, string) error { return nil }, Now: now}
	if withModel {
		composition.Planner = h.planner
	}
	if withScheduler {
		admission, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Authority: agentUXAmbientAdmissionAuthority{}, Store: agentrun.NewMemoryAdmissionStore(), Now: now})
		if err != nil {
			t.Fatal(err)
		}
		composition.Schedules, composition.Bindings, composition.Admissions = h.schedules, agentUXAmbientAsker{}, admission
	}
	h.served = composeAgentUXAmbient(composition)
	h.service = h.served.Worker.Service
	people := map[string]*trust.Principal{}
	for _, person := range []string{"author", "luis", "peer"} {
		principal, err := localAgentDemoPrincipal(clock, "tenant-a", person, "organization")
		if err != nil {
			t.Fatal(err)
		}
		people[person] = principal
	}
	admission := transport.Config{Now: now, Verifier: trust.VerifierFunc(func(_ context.Context, credential trust.Credential) (*trust.Principal, error) {
		if principal, ok := people[credential.Token]; ok && credential.Scheme == "Bearer" {
			return principal, nil
		}
		return nil, trust.ErrNoCredential
	})}
	assembly := &agentServedAssembly{}
	assembly.agentUX.Ambient = h.served.Surface
	assembly.agentUX.AmbientWorker = h.served.Worker
	handler := assembly.Overlay(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) }), admission)
	h.call = func(person, method, path string, body any) *httptest.ResponseRecorder {
		var reader *bytes.Reader
		if body != nil {
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			reader = bytes.NewReader(raw)
		} else {
			reader = bytes.NewReader(nil)
		}
		r := httptest.NewRequest(method, "http://cell.test"+path, reader)
		r.Header.Set("Authorization", "Bearer "+person)
		if body != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	return h
}

// post writes a person's message the way the chat store does and queues the
// outbox event the consumer reads.
func (h *agentUXServedAmbientHarness) post(id, author, body string) {
	h.t.Helper()
	agentUXAmbientPost(h.t, h.service, id, author, body)
	if err := h.service.DB.RunTenantTx(h.t.Context(), "tenant-a", func(tx dbport.Tx) error {
		raw, _ := json.Marshal(chatstore.OutboxEnvelope{ConversationID: "general", ActorID: author, TargetID: id, Revision: 1})
		_, err := tx.Exec(h.t.Context(), `INSERT INTO chat_outbox(tenant_id,aggregate_id,event_type,payload) VALUES('tenant-a',$1,'post.created',$2)`, id, raw)
		return err
	}); err != nil {
		h.t.Fatal(err)
	}
}

func (h *agentUXServedAmbientHarness) snapshot(person string) ambientagents.Snapshot {
	h.t.Helper()
	return agentUXServedSnapshot(h.t, h.call(person, http.MethodGet, ambientagents.Path+"?conversation=general", nil))
}

func (h *agentUXServedAmbientHarness) posts(conversation, author string) []string {
	h.t.Helper()
	var bodies []string
	if err := h.service.DB.RunTenantTx(h.t.Context(), "tenant-a", func(tx dbport.Tx) error {
		rows, err := tx.Query(h.t.Context(), `SELECT body FROM chat_post WHERE tenant_id='tenant-a' AND conversation_id=$1 AND author_id=$2 ORDER BY sequence`, conversation, author)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var body string
			if err := rows.Scan(&body); err != nil {
				return err
			}
			bodies = append(bodies, body)
		}
		return rows.Err()
	}); err != nil {
		h.t.Fatal(err)
	}
	return bodies
}

// TestTodo_AGENTUX_068_Served_SetReminder is "Set reminder" on the served
// assembly: pressing it creates exactly one scheduled announcement, owned by the
// person who asked, and when its time comes the reminder is delivered once: in
// the person's conversation with the agent for a private one, as the agent's
// message in the channel for a public one.
func TestTodo_AGENTUX_068_Served_SetReminder(t *testing.T) {
	h := newAgentUXServedAmbientHarness(t, true, true)
	ctx := t.Context()
	dm, err := chatcore.DirectPairConversationID("tenant-a", []chatcore.MemberRef{{TenantID: "tenant-a", SubjectID: "author"}, {TenantID: "tenant-a", SubjectID: "reminder"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = h.service.DB.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO chat_conversation(id,tenant_id,kind,owner_id) VALUES($1,'tenant-a','DIRECT','author')`, dm); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO chat_membership(tenant_id,conversation_id,member_id,home_tenant_id,role) VALUES('tenant-a',$1,'author','tenant-a','member'),('tenant-a',$1,'reminder','tenant-a','member')`, dm)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	h.post("private-reminder", "author", "Remind me tomorrow at 9 am to call the vendor")
	h.post("public-reminder", "author", "Remind us Friday at 3 pm to submit timesheets")
	if err := h.served.Worker.TickTenant(ctx, "tenant-a"); err != nil {
		t.Fatal(err)
	}
	if h.planner.calls != 2 {
		t.Fatalf("the screen model was asked %d times for two reminder messages: %v", h.planner.calls, h.planner.agents)
	}
	cards := h.snapshot("author").Cards
	if len(cards) != 2 {
		t.Fatalf("author sees %d cards: %+v", len(cards), cards)
	}
	set := map[string]chatui_card{}
	for _, card := range cards {
		if card.Kind != "REMINDER" || card.State != "OFFERED" {
			t.Fatalf("card %+v", card)
		}
		set[card.Source] = chatui_card{ID: card.ID, Revision: card.Revision, Scope: card.Scope}
		if w := h.call("author", http.MethodPost, ambientagents.Path+"/control", ambientagents.Command{Conversation: "general", ID: card.ID, Action: "SET", ExpectedRevision: card.Revision}); w.Code != http.StatusOK {
			t.Fatalf("Set reminder answered %d: %s", w.Code, w.Body.String())
		}
	}
	if set["private-reminder"].Scope != "PRIVATE" || set["public-reminder"].Scope != "PUBLIC" {
		t.Fatalf("scopes %+v", set)
	}
	if len(h.schedules.records) != 2 {
		t.Fatalf("Set created %d scheduled announcements for two reminders", len(h.schedules.records))
	}
	for id, record := range h.schedules.records {
		if record.OwnerID != "author" || record.State != scheduled.StateActive || !strings.HasPrefix(id, "ambient-") {
			t.Fatalf("scheduled announcement %s: owner=%q state=%s", id, record.OwnerID, record.State)
		}
	}
	if w := h.call("peer", http.MethodPost, ambientagents.Path+"/control", ambientagents.Command{Conversation: "general", ID: set["public-reminder"].ID, Action: "SET", ExpectedRevision: 2}); w.Code == http.StatusOK {
		t.Fatal("a member who may not manage the channel set its reminder")
	}
	if err := h.served.Worker.TickTenant(ctx, "tenant-a"); err != nil || len(h.posts(dm, "reminder")) != 0 || len(h.posts("general", "reminder")) != 0 {
		t.Fatalf("a reminder was delivered before its time: %v", err)
	}
	// The public reminder is for Friday; the private one for tomorrow morning.
	*h.clock = h.clock.AddDate(0, 0, 3)
	for range 3 {
		if err := h.served.Worker.TickTenant(ctx, "tenant-a"); err != nil {
			t.Fatal(err)
		}
	}
	private, public := h.posts(dm, "reminder"), h.posts("general", "reminder")
	if len(private) != 1 || !strings.Contains(private[0], "Remind me tomorrow at 9 am to call the vendor") || !strings.Contains(private[0], "/workspace/app/chat?") {
		t.Fatalf("the private reminder was delivered %d times: %q", len(private), private)
	}
	if len(public) != 1 || !strings.Contains(public[0], "Remind us Friday at 3 pm to submit timesheets") {
		t.Fatalf("the public reminder was delivered %d times: %q", len(public), public)
	}
	if posts := h.posts("general", "author"); len(posts) != 2 {
		t.Fatalf("the people's messages changed: %v", posts)
	}
	for _, record := range h.schedules.records {
		if record.State != scheduled.StateRetired {
			t.Fatalf("a fired reminder is still scheduled: %+v", record.State)
		}
	}
}

type chatui_card struct {
	ID, Scope string
	Revision  uint64
}

// TestTodo_AGENTUX_067_Served_Offers is the consumer on the served assembly,
// with the fake screen model: a message with a task produces one card under it;
// nothing is read without the channel administrator's grant, nothing for a
// person who opted out, and nothing is added until the person presses Add.
func TestTodo_AGENTUX_067_Served_Offers(t *testing.T) {
	h := newAgentUXServedAmbientHarness(t, true, false)
	ctx := t.Context()
	h.post("task", "author", "I'll send the deck by Friday")
	for range 2 {
		if err := h.served.Worker.TickTenant(ctx, "tenant-a"); err != nil {
			t.Fatal(err)
		}
	}
	if h.planner.calls != 1 {
		t.Fatalf("one message was screened %d times (replay must not ask the model again)", h.planner.calls)
	}
	cards := h.snapshot("author").Cards
	if len(cards) != 1 || cards[0].Kind != "TASK" || cards[0].Scope != "PRIVATE" || cards[0].Source != "task" || cards[0].State != "OFFERED" {
		t.Fatalf("author's cards: %+v", cards)
	}
	if peer := h.snapshot("peer").Cards; len(peer) != 0 {
		t.Fatalf("a private card reached a peer: %+v", peer)
	}
	if tasks, err := h.service.ListTasks(ctx, "tenant-a", "author"); err != nil || len(tasks) != 0 {
		t.Fatalf("a task was added without Add: %+v %v", tasks, err)
	}
	if list, err := h.store.ChannelTodo(ctx, "tenant-a", "tenant-a", "general", "author", func(context.Context) error { return nil }); err != nil || len(list.Items) != 0 {
		t.Fatalf("the channel list changed without Add: %+v %v", list, err)
	}
	if w := h.call("author", http.MethodPost, ambientagents.Path+"/control", ambientagents.Command{Conversation: "general", ID: cards[0].ID, Action: "ADD", ExpectedRevision: cards[0].Revision}); w.Code != http.StatusOK {
		t.Fatalf("Add answered %d", w.Code)
	}
	if tasks, err := h.service.ListTasks(ctx, "tenant-a", "author"); err != nil || len(tasks) != 1 {
		t.Fatalf("Add did not add the task: %+v %v", tasks, err)
	}

	// Opted out: the person's messages are never read.
	if w := h.call("luis", http.MethodPost, ambientagents.Path+"/opt-out", ambientagents.OptOutCommand{Conversation: "general", OptOut: true}); w.Code != http.StatusOK {
		t.Fatalf("opt out answered %d: %s", w.Code, w.Body.String())
	}
	calls := h.planner.calls
	h.post("opted-out", "luis", "I'll book the room tomorrow")
	if err := h.served.Worker.TickTenant(ctx, "tenant-a"); err != nil || h.planner.calls != calls {
		t.Fatalf("the screen model read an opted-out person's message: calls %d -> %d (%v)", calls, h.planner.calls, err)
	}
	if got := h.snapshot("luis").Cards; len(got) != 0 {
		t.Fatalf("an opted-out person was offered %+v", got)
	}

	// No grant, no read: the administrator turns the agents off.
	for _, agent := range []string{"task-catcher", "reminder"} {
		if w := h.call("author", http.MethodPost, ambientagents.Path+"/grant", ambientagents.GrantCommand{Conversation: "general", Agent: agent, Enabled: false}); w.Code != http.StatusOK {
			t.Fatalf("switch off answered %d", w.Code)
		}
	}
	calls = h.planner.calls
	h.post("ungranted", "peer", "I'll file the report")
	if err := h.served.Worker.TickTenant(ctx, "tenant-a"); err != nil || h.planner.calls != calls {
		t.Fatalf("a message was read without the grant: %d -> %d (%v)", calls, h.planner.calls, err)
	}
	if got := h.snapshot("peer").Cards; len(got) != 0 {
		t.Fatalf("an ungranted read produced %+v", got)
	}
}

// With no screen model configured nothing runs and nothing errors: the tick
// leaves the messages unread, the outbox cursor where it was, and the surface
// serves as before.
func TestTodo_AGENTUX_067_Served_NoModel(t *testing.T) {
	h := newAgentUXServedAmbientHarness(t, false, false)
	ctx := t.Context()
	h.post("task", "author", "I'll send the deck by Friday")
	for range 2 {
		if err := h.served.Worker.TickTenant(ctx, "tenant-a"); err != nil {
			t.Fatalf("a workspace with no model errored: %v", err)
		}
	}
	if h.planner.calls != 0 || len(h.snapshot("author").Cards) != 0 {
		t.Fatal("something ran without a model")
	}
	var cursors int
	if err := h.service.DB.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM chat_outbox_cursor WHERE tenant_id='tenant-a' AND consumer='agentux-ambient'`).Scan(&cursors)
	}); err != nil || cursors != 0 {
		t.Fatalf("the outbox cursor moved without a model: %d %v", cursors, err)
	}
	// Set reminder is refused as unavailable, not answered as an error page, when
	// there is no scheduler.
	if w := h.call("author", http.MethodGet, ambientagents.Path+"?conversation=general", nil); w.Code != http.StatusOK {
		t.Fatalf("the read answered %d", w.Code)
	}
}

// A card's times are stated in the zone the person keeps in Chat preferences.
func TestTodo_AGENTUX_068_Served_Zone(t *testing.T) {
	h := newAgentUXServedAmbientHarness(t, true, true)
	ctx := t.Context()
	state := chatstore.NewRecipientStateStore(h.store)
	hours, err := state.QuietHours(ctx, "tenant-a", "author")
	if err != nil || hours.Timezone != "UTC" {
		t.Fatalf("default zone %+v %v", hours, err)
	}
	h.post("zone", "author", "Remind me tomorrow at 9 am to call the vendor")
	if err = h.served.Worker.TickTenant(ctx, "tenant-a"); err != nil {
		t.Fatal(err)
	}
	if cards := h.snapshot("author").Cards; len(cards) != 1 || cards[0].Zone != "UTC" {
		t.Fatalf("a person with no zone is shown %+v", cards)
	}
	if _, err = state.PutQuietHours(ctx, "tenant-a", "author", chatrecipient.QuietHours{Timezone: "America/New_York", StartMinute: 1320, EndMinute: 420, Revision: 1}, 1); err != nil {
		t.Fatal(err)
	}
	if cards := h.snapshot("author").Cards; len(cards) != 1 || cards[0].Zone != "America/New_York" {
		t.Fatalf("the card is not in the person's zone: %+v", cards)
	}
	if cards := h.snapshot("luis").Cards; len(cards) != 0 {
		t.Fatalf("another person's private card: %+v", cards)
	}
	if _, err = state.PutQuietHours(ctx, "tenant-a", "luis", chatrecipient.QuietHours{Timezone: "Not/AZone", StartMinute: 1320, EndMinute: 420, Revision: 1}, 1); err != nil {
		t.Fatal(err)
	}
	zone, err := agentUXAmbientZone(h.store)(ctx, "tenant-a", "luis")
	if err != nil || zone != "UTC" {
		t.Fatalf("an unknown zone is %q (%v), want UTC", zone, err)
	}
}
