package chatstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func w8Members(owner string, others ...string) []Membership {
	out := []Membership{{MemberID: owner, Role: "manager", State: "active"}}
	for _, m := range others {
		out = append(out, Membership{MemberID: m, Role: "member", State: "active"})
	}
	return out
}

func w8Poll(t *testing.T, s *Store, conversation, key string) (chat.Post, chat.Chatcmd002Card) {
	t.Helper()
	card := chat.Chatcmd002Card{Kind: "poll", Title: "Swap the night shift?", Audience: []string{"Night shift"}, Poll: &chat.Chatcmd002Poll{Anonymous: true, Results: "always", AddOptions: "author", Options: []chat.ChannelPollOption{{ID: "a", Text: "Yes"}, {ID: "b", Text: "No"}}}}
	body, err := card.Body()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := s.sendPostRaw(context.Background(), SendRequest{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: conversation, AuthorID: "owner", ClientKey: key, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	post, err := chatPost(raw)
	if err != nil {
		t.Fatal(err)
	}
	return post, card
}

// TestTodo_CHATGATE_010_Integration: in the chat database, a poll addressed to a
// segment takes votes only from people in it, however the service is called; its
// tally is kept by cell and names nobody; the result by segment is reported only
// for a group of five or more.
func TestTodo_CHATGATE_010_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	night := map[string]bool{}
	people := []string{}
	for i := 1; i <= 8; i++ {
		id := fmt.Sprintf("n%d", i)
		people = append(people, id)
		night[id] = i != 8
	}
	people = append(people, "x")
	seedConversationRow(t, s, Conversation{ID: "polls", TenantID: "tenant-a", Kind: "PRIVATE_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, w8Members("owner", people...))
	post, _ := w8Poll(t, s, "polls", "poll-1")

	resolver := func(person string) func(context.Context, []string) ([]string, error) {
		return func(context.Context, []string) ([]string, error) {
			if night[person] {
				return []string{"Night shift"}, nil
			}
			return nil, nil
		}
	}
	request := func(person, option string, segments func(context.Context, []string) ([]string, error)) chat.Chatcmd002Request {
		return chat.Chatcmd002Request{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: person}, TenantID: "tenant-a", ConversationID: "polls", PostID: post.ID, ExpectedRevision: post.Revision, Segments: segments,
			Mutation: chat.Chatcmd002Mutation{Operation: "VOTE", Options: []string{option}}}
	}
	// Outside the segment, and with no way to ask the gate: refused.
	if _, err := s.Chatcmd002Mutate(ctx, request("x", "a", resolver("x")), todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("a member outside the segment voted: %v", err)
	}
	if _, err := s.Chatcmd002Mutate(ctx, request("n1", "a", nil), todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("a vote with no way to ask the gate: %v", err)
	}
	// A gate that fails is not a yes.
	broken := func(context.Context, []string) ([]string, error) { return nil, errors.New("gate down") }
	if _, err := s.Chatcmd002Mutate(ctx, request("n1", "a", broken), todoAllow); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("a vote while the gate is down: %v", err)
	}
	// Six people in the segment vote: four yes, two no.
	for i, option := range []string{"a", "a", "a", "a", "b", "b"} {
		person := fmt.Sprintf("n%d", i+1)
		if _, err := s.Chatcmd002Mutate(ctx, request(person, option, resolver(person)), todoAllow); err != nil {
			t.Fatalf("%s: %v", person, err)
		}
	}
	read := func(person string) chat.Chatcmd002View {
		r := request(person, "a", resolver(person))
		view, err := s.Chatcmd002Read(ctx, r, todoAllow)
		if err != nil {
			t.Fatal(err)
		}
		return view
	}
	view := read("n7")
	if view.BySegment == nil || view.BySegment.K != chatgate.MinGroup || len(view.BySegment.Segments) != 1 || view.BySegment.Total != 6 {
		t.Fatalf("the breakdown: %+v", view.BySegment)
	}
	if g := view.BySegment.Segments[0]; g.Segment != "Night shift" || g.Voters != 6 || g.Options["a"] != 4 || g.Options["b"] != 2 {
		t.Fatalf("the group: %+v", g)
	}
	if view.NotInAudience || !read("x").NotInAudience {
		t.Fatalf("who the card is for: n7 %v, x %v", view.NotInAudience, read("x").NotInAudience)
	}
	// Nothing in the ballots says who chose what.
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		var named, cells int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM chat_post_card_vote WHERE tenant_id='tenant-a' AND post_id=$1 AND option_id<>''`, post.ID).Scan(&named); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM chat_post_card_segment_tally WHERE tenant_id='tenant-a' AND post_id=$1`, post.ID).Scan(&cells); err != nil {
			return err
		}
		if named != 0 || cells != 3 { // the voters row and one row per option
			t.Errorf("%d named ballots, %d tally rows", named, cells)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A second poll with three voters reports nobody by group.
	small, _ := w8Poll(t, s, "polls", "poll-2")
	for _, person := range []string{"n1", "n2", "n3"} {
		r := request(person, "a", resolver(person))
		r.PostID, r.ExpectedRevision = small.ID, small.Revision
		if _, err := s.Chatcmd002Mutate(ctx, r, todoAllow); err != nil {
			t.Fatal(err)
		}
	}
	r := request("n4", "a", resolver("n4"))
	r.PostID = small.ID
	few, err := s.Chatcmd002Read(ctx, r, todoAllow)
	if err != nil || few.BySegment == nil || len(few.BySegment.Segments) != 0 || few.BySegment.Withheld != 1 || few.BySegment.Total != 3 {
		t.Fatalf("three voters: %+v %v", few.BySegment, err)
	}
	// Who a card is for is not changed by an edit before anyone has voted.
	third, card := w8Poll(t, s, "polls", "poll-3")
	edited := card
	edited.Audience = []string{"Everyone"}
	edit := request("owner", "a", nil)
	edit.PostID, edit.ExpectedRevision = third.ID, third.Revision
	edit.Mutation = chat.Chatcmd002Mutation{Operation: "EDIT", Card: &edited}
	updated, err := s.Chatcmd002Mutate(ctx, edit, todoAllow)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := chat.Chatcmd002Decode(updated.Body); !ok || len(got.Audience) != 1 || got.Audience[0] != "Night shift" {
		t.Fatalf("an edit moved the audience: %+v %v", got.Audience, ok)
	}
}

// TestTodo_CHATGATE_011_Integration: in the chat database a restricted post is
// read only by the people frozen into it and its author, on every path that
// lists or searches posts and on the check that fetches one, and stays so when
// the people around it change; an administrator counts such posts.
func TestTodo_CHATGATE_011_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	adapter := NewAdapter(s)
	ctx := context.Background()
	seedConversationRow(t, s, Conversation{ID: "secret", TenantID: "tenant-a", Kind: "PRIVATE_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, w8Members("owner", "ana", "ben", "cy"))
	send := func(ctx context.Context, key, body string) Post {
		raw, err := s.sendPostRaw(ctx, SendRequest{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "secret", AuthorID: "owner", ClientKey: key, Body: body})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	restricted := send(chat.WithPostAudience(ctx, chat.PostAudience{Segments: []string{"Night shift"}, Restricted: true, Members: []string{"ana", "owner"}}), "r1", "Night shift only: swap on Friday, blue door")
	addressed := send(chat.WithPostAudience(ctx, chat.PostAudience{Segments: []string{"Night shift"}}), "a1", "Night shift, everyone may read: blue door")
	open := send(ctx, "o1", "For everyone: blue door")
	principal := func(id string) chat.Principal { return chat.Principal{TenantID: "tenant-a", SubjectID: id} }

	// The one-post check.
	for person, want := range map[string]bool{"ana": true, "owner": true, "ben": false, "cy": false, "stranger": false} {
		got, err := adapter.PostReadableBy(ctx, "tenant-a", "secret", restricted.ID, person)
		if err != nil || got != want {
			t.Errorf("restricted post read by %s: %v %v, want %v", person, got, err, want)
		}
	}
	for _, id := range []string{addressed.ID, open.ID} {
		if got, err := adapter.PostReadableBy(ctx, "tenant-a", "secret", id, "ben"); err != nil || !got {
			t.Errorf("an unrestricted post is hidden from ben: %v %v", got, err)
		}
	}
	// History.
	history := func(person string) int {
		got, err := adapter.ListPosts(ctx, principal(person), "tenant-a", "secret", 0, chat.Page{PageSize: 50}, chat.PostWindow{})
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range got.Posts {
			if strings.Contains(p.Body, "Night shift only") && person != "ana" && person != "owner" {
				t.Errorf("history shows the restricted post to %s", person)
			}
		}
		return len(got.Posts)
	}
	if history("ana") != 3 || history("owner") != 3 || history("ben") != 2 || history("cy") != 2 {
		t.Fatalf("history: ana %d ben %d", history("ana"), history("ben"))
	}
	// Search.
	found := func(person string) int {
		got, err := adapter.Search(ctx, chat.SearchRequest{Principal: principal(person), TenantID: "tenant-a", Query: "blue"})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range got.Results {
			if strings.Contains(r.Post.Body, "Night shift only") && person != "ana" && person != "owner" {
				t.Errorf("search shows the restricted post to %s", person)
			}
		}
		return len(got.Results)
	}
	if found("ana") != 3 || found("ben") != 2 {
		t.Fatalf("search: ana %d ben %d", found("ana"), found("ben"))
	}
	// The conversation's live events carry a restricted post to its audience only.
	events := func(person string) string {
		page, err := adapter.ReadConversationEvents(ctx, chat.WatchConversationRequest{TenantID: "tenant-a", ConversationID: "secret", Principal: principal(person)}, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(page.Events)
		return string(raw)
	}
	if got := events("ana"); !strings.Contains(got, "swap on Friday") || !strings.Contains(got, "For everyone") {
		t.Fatalf("the audience's stream misses a post: %s", got)
	}
	if got := events("ben"); strings.Contains(got, "swap on Friday") || !strings.Contains(got, "For everyone") {
		t.Fatalf("ben's stream: %s", got)
	}
	// What an agent reads of a thread: ben asks, the owner answers the night shift
	// only, and the agent working for ben is given ben's question and not the
	// answer.
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_conversation SET audience_revision=1 WHERE tenant_id='tenant-a' AND id='secret'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	question, err := s.sendPostRaw(ctx, SendRequest{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "secret", AuthorID: "ben", ClientKey: "q1", Body: "Which door is blue?"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.sendPostRaw(chat.WithPostAudience(ctx, chat.PostAudience{Segments: []string{"Night shift"}, Restricted: true, Members: []string{"ana", "owner"}}), SendRequest{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "secret", AuthorID: "owner", ClientKey: "q1-answer", ParentID: question.ID, Body: "The side door, for nights only"}); err != nil {
		t.Fatal(err)
	}
	thread, err := adapter.CaptureBackgroundThreadSnapshot(ctx, chat.BackgroundThreadSnapshotRequest{TenantID: "tenant-a", ReaderID: "ben", ConversationID: "secret", ThreadID: question.ID, InvokingPostID: question.ID, Limit: 20})
	if err != nil || len(thread.Posts) != 1 || thread.Posts[0].ID != question.ID {
		t.Fatalf("the thread an agent working for ben is given: %+v %v", thread.Posts, err)
	}
	// The audience is frozen: a person who joins afterwards, or whose segment
	// changes, is not added.
	seedConversationRow(t, s, Conversation{ID: "secret", TenantID: "tenant-a", Kind: "PRIVATE_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, w8Members("owner", "late"))
	if got, _ := adapter.PostReadableBy(ctx, "tenant-a", "secret", restricted.ID, "late"); got {
		t.Fatal("a person who joined later reads the restricted post")
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_post_audience SET member_ids='{ben}' WHERE tenant_id='tenant-a' AND post_id=$1`, restricted.ID)
		return err
	}); err == nil {
		t.Fatal("a post's audience was changed after it was sent")
	}
	// Administrators count restricted messages and nothing else of them.
	if n, err := s.RestrictedCount(ctx, "tenant-a", "secret"); err != nil || n != 2 {
		t.Fatalf("restricted count %d %v", n, err)
	}
}

// TestTodo_CHATGATE_012_Integration: an agent's grant, its reads and its runs go
// through the chat database: the read is in the person's list after a restart of
// the service, the run is an outbox event with the submission as its source, and
// the answers the agent was not granted never reached the model.
func TestTodo_CHATGATE_012_Integration(t *testing.T) {
	store, service, c := chatgateFixture(t)
	visible := chatgate.Visibility{Administrators: true, Members: true, Consumers: []string{chatgate.ConsumerAgent, chatgate.ConsumerSegments}}
	c.Key, c.ExpectedRevision = "redefine", 2
	d := chatgate.Definition{Mode: "automatic", Purpose: "Join discussion", Fields: []chatgate.Field{
		{ID: "intro", Kind: "short_text", KindVersion: "1.0.0", Label: "Introduction", Purpose: "Welcome", DataClass: "INTERNAL", Required: true, Visibility: visible, RetentionDays: 30},
		{ID: "private", Kind: "short_text", KindVersion: "1.0.0", Label: "Private note", Purpose: "For the administrator", DataClass: "INTERNAL", Visibility: chatgate.Visibility{Administrators: true}, RetentionDays: 30},
	}}
	if err := service.Define(t.Context(), c, d); err != nil {
		t.Fatal(err)
	}
	c.Key, c.ExpectedRevision = "republish", 3
	if _, err := service.Publish(t.Context(), c, chatgate.Version{Major: 2}); err != nil {
		t.Fatal(err)
	}
	c.Key, c.ExpectedRevision = "grant", 4
	if err := service.GrantAgent(t.Context(), c, "welcomer", []string{"intro"}, []string{chatgate.PurposeWelcome}); err != nil {
		t.Fatal(err)
	}
	applicant := c
	applicant.Actor.Person, applicant.Key, applicant.ExpectedRevision = "worker", "join", 5
	sub, err := service.Submit(t.Context(), applicant, "2.0.0", map[string]json.RawMessage{"intro": json.RawMessage(`"I run payroll in Denver"`), "private": json.RawMessage(`"keep this from the model"`)})
	if err != nil || sub.Status != "admitted" {
		t.Fatalf("join: %+v %v", sub, err)
	}
	model := &w8Model{reply: `{"welcome":"Welcome. Denver payroll is glad you are here."}`}
	out, err := service.RunAgent(t.Context(), c.Actor, c.Scope, model, chatgate.AgentTask{Agent: "welcomer", Person: "worker", SubmissionID: sub.ID, Purpose: chatgate.PurposeWelcome})
	if err != nil || out.Welcome == "" || out.Source != chatgate.RunSourceGateSubmission {
		t.Fatalf("run: %+v %v", out, err)
	}
	if len(model.prompts) != 1 || !strings.Contains(model.prompts[0].Untrusted, "payroll in Denver") || strings.Contains(model.prompts[0].Untrusted, "keep this from the model") {
		t.Fatalf("what the model was given: %+v", model.prompts)
	}
	// The read is in the person's list, read back from the database.
	reads, err := service.Reads(t.Context(), applicant.Actor, applicant.Scope)
	if err != nil {
		t.Fatal(err)
	}
	agentReads := 0
	for _, r := range reads {
		if r.Consumer == "agent:welcomer" && len(r.Fields) == 1 && r.Fields[0] == "intro" {
			agentReads++
		}
	}
	if agentReads != 1 {
		t.Fatalf("the person's list: %+v", reads)
	}
	// The run is an event with the submission as its source.
	if err = store.RunTenantTx(t.Context(), "tenant-a", func(tx dbport.Tx) error {
		var payload []byte
		if err := tx.QueryRow(t.Context(), `SELECT payload FROM chat_outbox WHERE tenant_id='tenant-a' AND event_type='gate.agent_run'`).Scan(&payload); err != nil {
			return err
		}
		var event chatgate.Event
		if err := json.Unmarshal(payload, &event); err != nil || event.Source != chatgate.RunSourceGateSubmission || event.SubmissionID != sub.ID {
			t.Errorf("the run event: %s %v", payload, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// No grant after revoking: the model is not called.
	c.Key, c.ExpectedRevision = "revoke", 5
	g, _ := service.Get(t.Context(), c.Actor, c.Scope, true)
	c.ExpectedRevision = g.Revision
	if err = service.RevokeAgent(t.Context(), c, "welcomer"); err != nil {
		t.Fatal(err)
	}
	model.prompts = nil
	if _, err = service.RunAgent(t.Context(), c.Actor, c.Scope, model, chatgate.AgentTask{Agent: "welcomer", Person: "worker", SubmissionID: sub.ID, Purpose: chatgate.PurposeWelcome}); !errors.Is(err, chatgate.ErrDenied) || len(model.prompts) != 0 {
		t.Fatalf("a revoked agent: %v, %d prompts", err, len(model.prompts))
	}
}

type w8Model struct {
	reply   string
	prompts []chatgate.Prompt
}

func (m *w8Model) Complete(_ context.Context, p chatgate.Prompt) (string, error) {
	m.prompts = append(m.prompts, p)
	return m.reply, nil
}

// TestTodo_CHATGATE_013_Integration: adding a gate to a channel with members locks
// nobody out in the database; "ask" gives them until a date and shows them the
// question on their next visit; only a confirmed list is removed; the list of
// submissions reads without the lock.
func TestTodo_CHATGATE_013_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversationRow(t, s, Conversation{ID: "gated", TenantID: "tenant-a", Kind: "PUBLIC_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, w8Members("owner", "old1", "old2"))
	repo := &GateRepository{Store: s, Membership: func(ctx context.Context, tx dbport.Tx, scope chatgate.Scope, a chatgate.Actor, _ string, admit bool) error {
		if admit {
			_, e := tx.Exec(ctx, `INSERT INTO chat_membership(tenant_id,conversation_id,home_tenant_id,member_id) VALUES($1,$2,$1,$3) ON CONFLICT(tenant_id,conversation_id,home_tenant_id,member_id) DO UPDATE SET state='active',left_at=NULL,revision=chat_membership.revision+1`, scope.Tenant, scope.Conversation, a.Person)
			return e
		}
		_, e := tx.Exec(ctx, `UPDATE chat_membership SET state='left',left_at=now(),revision=revision+1 WHERE tenant_id=$1 AND conversation_id=$2 AND member_id=$3`, scope.Tenant, scope.Conversation, a.Person)
		return e
	}}
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	service := &chatgate.Service{Repository: repo, Registry: chatgate.NewRegistry(), Authority: chatgateAuthority{}, Clock: func() time.Time { return now }}
	scope := chatgate.Scope{Tenant: "tenant-a", Conversation: "gated"}
	owner := chatgate.Command{Scope: scope, Actor: chatgate.Actor{Tenant: "tenant-a", Person: "owner"}, Key: "define"}
	d := chatgate.Definition{Mode: "automatic", Purpose: "Join discussion", Fields: []chatgate.Field{{ID: "intro", Kind: "short_text", KindVersion: "1.0.0", Label: "Introduction", Purpose: "Welcome", DataClass: "INTERNAL", Required: true, Visibility: chatgate.Visibility{Administrators: true}, RetentionDays: 30}}}
	if err := service.Define(t.Context(), owner, d); err != nil {
		t.Fatal(err)
	}
	owner.Key, owner.ExpectedRevision = "publish", 1
	if _, err := service.Publish(t.Context(), owner, chatgate.Version{Major: 1}); err != nil {
		t.Fatal(err)
	}
	rev := func() uint64 {
		g, err := service.Get(t.Context(), owner.Actor, scope, true)
		if err != nil {
			t.Fatal(err)
		}
		return g.Revision
	}
	// People already in the channel are never locked out; a newcomer is asked.
	if err := service.CanJoin(t.Context(), chatgate.Actor{Tenant: "tenant-a", Person: "old1"}, scope); err != nil {
		t.Fatalf("an existing member was locked out: %v", err)
	}
	if err := service.CanJoin(t.Context(), chatgate.Actor{Tenant: "tenant-a", Person: "newcomer"}, scope); !errors.Is(err, chatgate.ErrRequired) {
		t.Fatalf("a newcomer was not asked: %v", err)
	}
	// A change to an existing membership passes the database's fence.
	if err := s.RunTenantTx(t.Context(), "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE chat_membership SET role='manager' WHERE tenant_id='tenant-a' AND conversation_id='gated' AND member_id='old1'`)
		return err
	}); err != nil {
		t.Fatalf("the fence stopped a change to an existing member: %v", err)
	}
	// Ask them, by a date.
	owner.Key, owner.ExpectedRevision = "ask", rev()
	by := now.AddDate(0, 0, 14)
	if err := service.SetExistingMembers(t.Context(), owner, chatgate.ExistingAsk, by); err != nil {
		t.Fatal(err)
	}
	list, err := service.Unanswered(t.Context(), owner.Actor, scope)
	if err != nil || strings.Join(list, ",") != "old1,old2,owner" {
		t.Fatalf("unanswered: %v %v", list, err)
	}
	summaries, err := repo.GateSummaries(t.Context(), "tenant-a", "old2")
	if err != nil || len(summaries) != 1 || !summaries[0].Member || !summaries[0].AnswerAgain || !summaries[0].AnswerBy.Equal(by) {
		t.Fatalf("a current member's summary: %+v %v", summaries, err)
	}
	// Before the date, no removal; after it, only the confirmed list.
	owner.Key, owner.ExpectedRevision = "early", rev()
	if err = service.RemoveUnanswered(t.Context(), owner, []string{"old2"}); !errors.Is(err, chatgate.ErrConflict) {
		t.Fatalf("removal before the date: %v", err)
	}
	now = by.Add(time.Hour)
	owner.Key, owner.ExpectedRevision = "remove", rev()
	if err = service.RemoveUnanswered(t.Context(), owner, []string{"old2"}); err != nil {
		t.Fatal(err)
	}
	if err = service.CanJoin(t.Context(), chatgate.Actor{Tenant: "tenant-a", Person: "old1"}, scope); err != nil {
		t.Fatalf("old1 was removed with old2: %v", err)
	}
	var active int
	if err = s.RunTenantTx(t.Context(), "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT count(*) FROM chat_membership WHERE tenant_id='tenant-a' AND conversation_id='gated' AND state='active' AND member_id IN ('old1','old2')`).Scan(&active)
	}); err != nil || active != 1 {
		t.Fatalf("%d of the two are still in: %v", active, err)
	}
	// The list reads from the state without the lock.
	subs, err := service.ListSubmissions(t.Context(), owner.Actor, scope, "")
	if err != nil || len(subs) != 0 {
		t.Fatalf("submissions: %v %v", subs, err)
	}
}

// TestTodo_CHATGATE_009_Integration: labels and segments are persisted with the
// gate, read back without a lock for the paths that address content, and removed
// with the person.
func TestTodo_CHATGATE_009_Integration(t *testing.T) {
	store, service, c := chatgateFixture(t)
	_ = store
	visible := chatgate.Visibility{Administrators: true, Members: true, Consumers: []string{chatgate.ConsumerSegments}}
	c.Key, c.ExpectedRevision = "redefine", 2
	d := chatgate.Definition{Mode: "automatic", Purpose: "Join discussion", Fields: []chatgate.Field{{ID: "office", Kind: "single_choice", KindVersion: "1.0.0", Label: "Office", Purpose: "Place you", DataClass: "INTERNAL", Required: true, Options: []string{"Denver", "Austin"}, Visibility: visible, RetentionDays: 30}}}
	if err := service.Define(t.Context(), c, d); err != nil {
		t.Fatal(err)
	}
	c.Key, c.ExpectedRevision = "republish", 3
	if _, err := service.Publish(t.Context(), c, chatgate.Version{Major: 2}); err != nil {
		t.Fatal(err)
	}
	c.Key, c.ExpectedRevision = "rules", 4
	if err := service.SetSegments(t.Context(), c, []chatgate.SegmentRule{{ID: "r1", Field: "office", Value: "Denver", Label: "Denver crew", Segment: "Denver office"}}, "First rules"); err != nil {
		t.Fatal(err)
	}
	worker := c
	worker.Actor.Person, worker.Key, worker.ExpectedRevision = "worker", "join", 5
	if _, err := service.Submit(t.Context(), worker, "2.0.0", map[string]json.RawMessage{"office": json.RawMessage(`"Denver"`)}); err != nil {
		t.Fatal(err)
	}
	repo := service.Repository.(*GateRepository)
	state, err := repo.SegmentState(t.Context(), c.Scope)
	if err != nil || strings.Join(state.Members([]string{"Denver office"}), ",") != "worker" || !state.InAny("worker", []string{"Denver office"}) {
		t.Fatalf("the persisted segments: %+v %v", state, err)
	}
	mine, err := service.MySegments(t.Context(), worker.Actor, worker.Scope)
	if err != nil || len(mine) != 2 || mine[0].Value != "Denver" {
		t.Fatalf("my segments: %+v %v", mine, err)
	}
	// A person removed from the channel is in no segment for the paths that address
	// content, even before the gate is told.
	if err = store.RunTenantTx(t.Context(), "tenant-a", func(tx dbport.Tx) error {
		_, e := tx.Exec(t.Context(), `UPDATE chat_membership SET state='left',left_at=now(),revision=revision+1 WHERE tenant_id='tenant-a' AND conversation_id='gated' AND member_id='worker'`)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if state, err = repo.SegmentState(t.Context(), c.Scope); err != nil || state.InAny("worker", []string{"Denver office"}) || len(state.Members([]string{"Denver office"})) != 0 {
		t.Fatalf("a person who left is still in a segment: %+v %v", state, err)
	}
	// Leaving removes them, in the database too.
	c.Key = "erase"
	g, _ := service.Get(t.Context(), c.Actor, c.Scope, true)
	c.ExpectedRevision = g.Revision
	if err = service.Erase(t.Context(), c, "worker", false); err != nil {
		t.Fatal(err)
	}
	if state, err = repo.SegmentState(t.Context(), c.Scope); err != nil || state.InAny("worker", []string{"Denver office"}) {
		t.Fatalf("after erasing: %+v %v", state, err)
	}
}

// TestTodo_CHATGATE_013_Performance_Postgres lists and filters a gate with 10,000
// submissions, each carrying the three questions it was answered against, through
// the repository's read path in the chat database. The budget is 300 ms.
func TestTodo_CHATGATE_013_Performance_Postgres(t *testing.T) {
	store, service, c := chatgateFixture(t)
	fields := make([]chatgate.Field, 3)
	for i := range fields {
		fields[i] = chatgate.Field{ID: fmt.Sprintf("q%d", i), Kind: "single_choice", KindVersion: "1.0.0", Label: "A question people are asked when they join", Purpose: "Place you in the right group", DataClass: "INTERNAL", Options: []string{"One", "Two", "Three"}, Visibility: chatgate.Visibility{Administrators: true, Members: true}, RetentionDays: 90}
	}
	state := chatgate.State{Gate: chatgate.Gate{Revision: 2, Current: "1.0.0", State: "active", Versions: []chatgate.Definition{{Version: chatgate.Version{Major: 1}, Mode: "review", Purpose: "Join", Fields: fields}}}}
	statuses := []string{"review", "admitted", "declined"}
	for i := 0; i < 10000; i++ {
		state.Submissions = append(state.Submissions, chatgate.Submission{ID: fmt.Sprintf("sha256:%064d", i), Person: fmt.Sprintf("person-%05d", i), Version: "1.0.0", Status: statuses[i%3], Revision: 1, SubmittedAt: time.Now(), Fields: fields, Digest: fmt.Sprintf("sha256:%064d", i)})
	}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.RunTenantTx(t.Context(), "tenant-a", func(tx dbport.Tx) error {
		_, e := tx.Exec(t.Context(), `UPDATE chat_gate_state SET state_json=$3 WHERE tenant_id=$1 AND conversation_id=$2`, "tenant-a", "gated", payload)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	// The submission log holds each submission as the repository writes it.
	if err = store.RunTenantTx(t.Context(), "tenant-a", func(tx dbport.Tx) error {
		_, e := tx.Exec(t.Context(), `INSERT INTO chat_gate_submission_revision(tenant_id,conversation_id,submission_id,revision,record_json) SELECT $1,$2,s->>'ID',(s->>'Revision')::bigint,s FROM jsonb_array_elements($3::jsonb->'Submissions') s`, "tenant-a", "gated", payload)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	// One warm-up read, then the measured pair.
	if _, err = service.ListSubmissions(t.Context(), c.Actor, c.Scope, "review"); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	all, err := service.ListSubmissions(t.Context(), c.Actor, c.Scope, "")
	listed := time.Since(started)
	started = time.Now()
	waiting, err2 := service.ListSubmissions(t.Context(), c.Actor, c.Scope, "review")
	filtered := time.Since(started)
	if err != nil || err2 != nil || len(all) != 10000 || len(waiting) != 3334 {
		t.Fatalf("%d %d %v %v", len(all), len(waiting), err, err2)
	}
	t.Logf("10,000 submissions through the chat database: list %v, filter %v (%d bytes of state)", listed, filtered, len(payload))
	if listed > 300*time.Millisecond || filtered > 300*time.Millisecond {
		t.Fatalf("listing took %v and filtering %v, each must take under 300 ms", listed, filtered)
	}
}
