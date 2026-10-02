package application

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/edge"
)

// chatbug057Served is the card route as serve.go mounts it: the card overlay on
// the edge handler and the served assembly, with its browser guard, in front of
// it. Chat behind it is really composed over the test PostgreSQL.
type chatbug057Served struct {
	t       *testing.T
	rig     *chatlangRig
	handler http.Handler
	bearer  map[string]string
}

func newChatbug057Served(t *testing.T) *chatbug057Served {
	t.Helper()
	rig := newChatlangRig(t)
	served := &chatbug057Served{t: t, rig: rig, bearer: map[string]string{}}
	admission, bearer := integrate1Admission(t, "host", "alice", time.Now)
	served.bearer["alice"] = bearer
	for _, subject := range []string{"bruno", "stranger"} {
		_, served.bearer[subject] = integrate1Admission(t, "host", subject, time.Now)
	}
	cards := chatcmd002CardPort(rig.chat)
	if cards == nil {
		t.Fatal("the composed Chat gives the card route no service")
	}
	served.handler = (&agentServedAssembly{}).Overlay(OverlayChatcmd002Cards(http.NotFoundHandler(), cards, admission), admission)
	return served
}

func (s *chatbug057Served) call(subject, action string, body any) (int, Chatcmd002CardReply, string) {
	s.t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		s.t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, Chatcmd002CardPath+"/"+action, strings.NewReader(string(raw)))
	request.Header.Set("Content-Type", "application/json")
	if bearer := s.bearer[subject]; bearer != "" {
		request.Header.Set("Authorization", bearer)
	}
	response := httptest.NewRecorder()
	s.handler.ServeHTTP(response, request)
	var reply Chatcmd002CardReply
	var refusal chatcmd002CardError
	if response.Code == http.StatusOK {
		if err := json.Unmarshal(response.Body.Bytes(), &reply); err != nil {
			s.t.Fatalf("%s: %v: %s", action, err, response.Body.String())
		}
	} else {
		_ = json.Unmarshal(response.Body.Bytes(), &refusal)
	}
	return response.Code, reply, refusal.Code
}

// cards is the conversation as a member reads it: the cards among its messages.
func (s *chatbug057Served) cards(subject string) map[string]chatcore.Chatcmd002Card {
	s.t.Helper()
	page, err := s.rig.chat.service.ListPosts(s.rig.as(subject), chatcore.ListPostsRequest{Principal: s.rig.person(subject), TenantID: "host", ConversationID: s.rig.room, Page: chatcore.Page{PageSize: 50}})
	if err != nil {
		s.t.Fatal(err)
	}
	cards := map[string]chatcore.Chatcmd002Card{}
	for _, post := range page.Posts {
		if card, ok := chatcore.Chatcmd002Decode(post.Body); ok {
			cards[post.ID] = card
		}
	}
	return cards
}

// TestTodo_CHATBUG_057_Served is page finding 3 of 2026-10-02: Post on the
// preview was pressed and no card appeared. The route the page calls is driven
// here from what a person types to a card message in the conversation, through
// the served assembly, admission, the card service, the routed Chat service and
// the store; and a post the server refuses is answered with a code the page can
// put into words.
func TestTodo_CHATBUG_057_Served(t *testing.T) {
	served := newChatbug057Served(t)
	room := served.rig.room
	// The served assembly routes the card actions itself, as it routes the tidy
	// action and the other Chat surfaces, so the browser guard and the session
	// cookie apply to them as well.
	for _, action := range []string{"post", "read", "mutate", "tidy"} {
		if !agentServedPath(Chatcmd002CardPath + "/" + action) {
			t.Errorf("the served assembly does not route %s/%s", Chatcmd002CardPath, action)
		}
	}
	if agentServedPath(Chatcmd002CardPath+"x/post") || agentServedPath(Chatcmd002CardPath) {
		t.Error("an address that only starts like the card address is routed")
	}

	// What the composer holds, read and tidied the way the page does it.
	now := time.Now()
	poll, err := chatcore.Chatcmd003ParsePoll("where for lunch? tacos, pho or pizza", now, chatcore.Chatcmd004ResolveDate)
	if err != nil {
		t.Fatal(err)
	}
	pollCard := chatcore.Chatcmd003Tidy(poll).Card
	code, reply, refusal := served.call("alice", "post", Chatcmd002CardCommand{ConversationID: room, IdempotencyKey: "served-poll", Card: &pollCard})
	if code != http.StatusOK || reply.PostID == "" {
		t.Fatalf("posting the poll: %d %q", code, refusal)
	}
	pollID := reply.PostID
	members := []chatcore.Chatcmd004Member{{HomeTenantID: "host", ID: "alice", Name: "Alice"}, {HomeTenantID: "host", ID: "bruno", Name: "Bruno Diaz"}}
	todo, err := chatcore.Chatcmd004ParseTodo("Order pizza @Bruno Diaz friday; Book room", members, "alice", now, chatcore.Chatcmd004ResolveDate)
	if err != nil {
		t.Fatal(err)
	}
	todoCard := chatcore.Chatcmd003Tidy(todo).Card
	todoCard.Title = "To-do list"
	code, reply, refusal = served.call("alice", "post", Chatcmd002CardCommand{ConversationID: room, IdempotencyKey: "served-todo", Card: &todoCard})
	if code != http.StatusOK || reply.PostID == "" {
		t.Fatalf("posting the list: %d %q", code, refusal)
	}
	todoID := reply.PostID

	// Both are messages in the conversation, for another member as well.
	cards := served.cards("bruno")
	if len(cards) != 2 {
		t.Fatalf("the conversation holds %d cards, want the poll and the list", len(cards))
	}
	if got := cards[pollID]; got.Kind != "poll" || got.Title != "Where for lunch?" || len(got.Poll.Options) != 3 || got.Poll.Options[0].Text != "Tacos" || got.Poll.Options[0].ID == "" {
		t.Fatalf("the poll in the conversation: %+v", got)
	}
	got := cards[todoID]
	if got.Kind != "todo" || len(got.Todo.Items) != 2 || got.Todo.Items[0].Text != "Order pizza" || got.Todo.Items[0].AssigneeID != "bruno" || got.Todo.Items[0].DueAt == nil || got.Todo.Items[0].DueAt.Weekday() != time.Friday || got.Todo.Items[1].Text != "Book room" || got.Todo.Items[1].AssigneeID != "" || got.Todo.Items[1].DueAt != nil {
		t.Fatalf("the list in the conversation: %+v", got.Todo)
	}

	// Pressing Post again with the same key is the same post, not a second card.
	if code, again, _ := served.call("alice", "post", Chatcmd002CardCommand{ConversationID: room, IdempotencyKey: "served-poll", Card: &pollCard}); code != http.StatusOK || again.PostID != pollID || len(served.cards("alice")) != 2 {
		t.Fatalf("a repeated post: %d %q, %d cards", code, again.PostID, len(served.cards("alice")))
	}

	// The card can be read and voted on through the same route.
	code, reply, refusal = served.call("bruno", "read", Chatcmd002CardCommand{ConversationID: room, PostIDs: []string{pollID, todoID}})
	if code != http.StatusOK || len(reply.Views) != 2 || !reply.Views[todoID].CanTick[got.Todo.Items[0].ID] {
		t.Fatalf("reading the cards: %d %q %+v", code, refusal, reply.Views)
	}
	view := reply.Views[pollID]
	code, reply, refusal = served.call("bruno", "mutate", Chatcmd002CardCommand{ConversationID: room, PostID: pollID, ExpectedRevision: view.Revision,
		Mutation: &chatcore.Chatcmd002Mutation{Operation: "VOTE", Options: []string{view.Card.Poll.Options[1].ID}}})
	if code != http.StatusOK || reply.View == nil || reply.View.Card.Poll.Options[1].Count != 1 || len(reply.View.MyOptions) != 1 {
		t.Fatalf("voting: %d %q %+v", code, refusal, reply.View)
	}

	// A refused post says which refusal it is, and posts nothing.
	outsider := todoCard
	outsider.Todo = &chatcore.Chatcmd002Todo{Tick: "anyone", Items: []chatcore.Chatcmd002Task{{ChannelTodoItem: chatcore.ChannelTodoItem{Text: "Ship"}, AssigneeHomeTenantID: "host", AssigneeID: "stranger", AssigneeName: "Stranger"}}}
	empty := pollCard
	empty.Poll = &chatcore.Chatcmd002Poll{Results: "always", AddOptions: "author", Options: []chatcore.ChannelPollOption{{Text: "Only one"}}}
	for name, tc := range map[string]struct {
		subject string
		command Chatcmd002CardCommand
		status  int
		code    string
	}{
		"no credential":                 {"", Chatcmd002CardCommand{ConversationID: room, IdempotencyKey: "r1", Card: &pollCard}, http.StatusUnauthorized, "request_denied"},
		"not a member":                  {"stranger", Chatcmd002CardCommand{ConversationID: room, IdempotencyKey: "r2", Card: &pollCard}, http.StatusForbidden, "permission_denied"},
		"a task names a non-member":     {"alice", Chatcmd002CardCommand{ConversationID: room, IdempotencyKey: "r3", Card: &outsider}, http.StatusForbidden, "permission_denied"},
		"a poll with one option":        {"alice", Chatcmd002CardCommand{ConversationID: room, IdempotencyKey: "r4", Card: &empty}, http.StatusBadRequest, "invalid_argument"},
		"no key to make the post again": {"alice", Chatcmd002CardCommand{ConversationID: room, Card: &pollCard}, http.StatusBadRequest, "invalid_argument"},
		"a conversation that is gone":   {"alice", Chatcmd002CardCommand{ConversationID: "no-such-room", IdempotencyKey: "r6", Card: &pollCard}, http.StatusNotFound, "not_found"},
	} {
		if code, _, refusal := served.call(tc.subject, "post", tc.command); code != tc.status || refusal != tc.code {
			t.Errorf("%s: %d %q, want %d %q", name, code, refusal, tc.status, tc.code)
		}
	}
	if count := len(served.cards("alice")); count != 2 {
		t.Fatalf("a refused post left a card: %d in the conversation", count)
	}

	// A browser session posts with its cookie and the edge's browser token; the
	// same press from another site, or without the token, is refused before it
	// reaches Chat.
	raw, _ := json.Marshal(Chatcmd002CardCommand{ConversationID: room, IdempotencyKey: "served-browser", Card: &pollCard})
	browser := func(origin string, token bool) int {
		request := httptest.NewRequest(http.MethodPost, "http://cell.test"+Chatcmd002CardPath+"/post", strings.NewReader(string(raw)))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", served.bearer["alice"])
		request.Header.Set("Origin", origin)
		if token {
			request.AddCookie(&http.Cookie{Name: edge.BrowserCSRFCookieName, Value: "browser-token"})
		}
		response := httptest.NewRecorder()
		served.handler.ServeHTTP(response, request)
		return response.Code
	}
	if code := browser("https://elsewhere.test", true); code != http.StatusForbidden {
		t.Errorf("a post from another site: %d", code)
	}
	if code := browser("http://cell.test", false); code != http.StatusForbidden {
		t.Errorf("a browser post without the edge's token: %d", code)
	}
	if count := len(served.cards("alice")); count != 2 {
		t.Fatalf("a refused browser post left a card: %d", count)
	}
	if code := browser("http://cell.test", true); code != http.StatusOK || len(served.cards("alice")) != 3 {
		t.Errorf("a post from the page itself: %d, %d cards", code, len(served.cards("alice")))
	}
}
