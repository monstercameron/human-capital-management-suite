package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func chatcmd002TestBody(t *testing.T, card chat.Chatcmd002Card) string {
	t.Helper()
	body, err := card.Body()
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// TestTodo_CHATBUG_057 covers the client half of the defect: a press on a poll
// in the conversation sends a request, with the person's credential, to the
// route the server serves and the page's content security policy admits.
func TestTodo_CHATBUG_057(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization")+" "+r.Header.Get("Content-Type"))
		var command chatcmd002Command
		if err := json.Unmarshal(body, &command); err != nil {
			t.Errorf("request body %q: %v", body, err)
		}
		switch r.URL.Path {
		case "/api/chat/message-card/v1/mutate":
			if command.Mutation == nil || command.Mutation.Operation != "VOTE" || command.PostID != "p1" || command.ExpectedRevision != 3 || command.ConversationID != "room" || command.HostTenantID != "host" {
				t.Errorf("vote request %+v", command)
			}
			if len(command.Mutation.Options) == 1 && command.Mutation.Options[0] == "refused" {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"code":"conflict"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(chatcmd002Reply{PostID: "p1", Revision: 4, View: &chat.Chatcmd002View{MyOptions: []string{"a"}, Voted: true, Revision: 4}})
		case "/api/chat/message-card/v1/read":
			_ = json.NewEncoder(w).Encode(chatcmd002Reply{Views: map[string]chat.Chatcmd002View{command.PostIDs[0]: {Revision: 2}}})
		default:
			w.WriteHeader(http.StatusBadGateway)
		}
	}))
	defer server.Close()
	cfg := journeyclient.Config{TunnelURL: server.URL, Bearer: "token", Tenant: "host", Subject: "alice"}
	ctx := context.Background()

	var reply chatcmd002Reply
	vote := chat.Chatcmd002Mutation{Operation: "VOTE", Options: []string{"a"}}
	if err := chatcmd002Request(ctx, server.Client(), cfg, "mutate", chatcmd002Command{HostTenantID: "host", ConversationID: "room", PostID: "p1", ExpectedRevision: 3, Mutation: &vote}, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Revision != 4 || reply.View == nil || !reply.View.Voted || len(seen) != 1 || seen[0] != "POST /api/chat/message-card/v1/mutate Bearer token application/json" {
		t.Fatalf("vote reply %+v, requests %v", reply, seen)
	}
	reply = chatcmd002Reply{}
	if err := chatcmd002Request(ctx, server.Client(), cfg, "read", chatcmd002Command{ConversationID: "room", PostIDs: []string{"p9"}}, &reply); err != nil || reply.Views["p9"].Revision != 2 {
		t.Fatalf("read %+v %v", reply, err)
	}
	refused := chat.Chatcmd002Mutation{Operation: "VOTE", Options: []string{"refused"}}
	err := chatcmd002Request(ctx, server.Client(), cfg, "mutate", chatcmd002Command{HostTenantID: "host", ConversationID: "room", PostID: "p1", ExpectedRevision: 3, Mutation: &refused}, &reply)
	if err != chatcmd002Error("conflict") {
		t.Fatalf("refusal %v", err)
	}
	if got := chatcmd002Notice(err, refused, true); got != "final" {
		t.Fatalf("an anonymous ballot refused as already cast reads %q", got)
	}
	if got := chatcmd002Notice(err, chat.Chatcmd002Mutation{Operation: "CLOSE"}, false); got != "conflict" {
		t.Fatalf("a stale close reads %q", got)
	}
	if got := chatcmd002Notice(err, chat.Chatcmd002Mutation{Operation: "ADD_OPTION", Text: "x"}, false); got != "failed" {
		t.Fatalf("a repeated option read %q", got)
	}
	if got := chatcmd002Notice(chatcmd002Error("permission_denied"), vote, false); got != "denied" {
		t.Fatalf("a refusal reads %q", got)
	}
	// No answer, a foreign status and no credential are all "unavailable"; the
	// card keeps what it showed and says the press was not saved.
	if err := chatcmd002Request(ctx, server.Client(), cfg, "post", chatcmd002Command{ConversationID: "room"}, &reply); err != chatcmd002Error("unavailable") || chatcmd002Notice(err, vote, false) != "failed" {
		t.Fatalf("gateway failure %v", err)
	}
	before := len(seen)
	if err := chatcmd002Request(ctx, server.Client(), journeyclient.Config{TunnelURL: server.URL}, "read", chatcmd002Command{}, &reply); err != chatcmd002Error("unavailable") || len(seen) != before {
		t.Fatalf("a request without a credential was sent: %v", err)
	}
}

// Which cards are read, and when: each card on the page once per revision, so
// another person's vote (which moves the message's revision) is read and a
// re-render is not.
func TestTodo_CHATBUG_057_Sync(t *testing.T) {
	poll := chatcmd002TestBody(t, chat.Chatcmd002Card{Kind: "poll", Title: "Lunch?", Poll: &chat.Chatcmd002Poll{Results: "always", AddOptions: "author", Options: []chat.ChannelPollOption{{ID: "a", Text: "A"}, {ID: "b", Text: "B"}}}})
	list := chatcmd002TestBody(t, chat.Chatcmd002Card{Kind: "todo", Title: "Launch", Todo: &chat.Chatcmd002Todo{Tick: "anyone", Items: []chat.Chatcmd002Task{{ChannelTodoItem: chat.ChannelTodoItem{ID: "t", Text: "Ship"}}}}})
	m := chatui.Model{CurrentTenantID: "host", CurrentUser: "alice", SelectedID: "room",
		Messages:       []chatui.Message{{ID: "plain", Revision: 2, Body: "hello", Edited: true}, {ID: "poll", Revision: 5, Body: poll, Edited: true}, {ID: "fake", Revision: 1, Body: "x" + chat.Chatcmd002BodyMarker + "{}"}},
		ThreadMessages: []chatui.Message{{ID: "list", Revision: 1, Body: list}},
		ChannelPins:    []chatui.ChannelPin{{PostID: "poll", Revision: 4, Body: poll}},
	}
	cards := chatcmd002Cards(m)
	if !reflect.DeepEqual(cards, map[string]uint64{"poll": 5, "list": 1}) {
		t.Fatalf("cards %v", cards)
	}
	asked := map[string]uint64{}
	first := chatcmd002Stale(cards, asked)
	sort.Strings(first)
	if !reflect.DeepEqual(first, []string{"list", "poll"}) {
		t.Fatalf("first read asks for %v", first)
	}
	for _, id := range first {
		asked[id] = cards[id]
	}
	if again := chatcmd002Stale(cards, asked); len(again) != 0 {
		t.Fatalf("a re-render asked again for %v", again)
	}
	fingerprint := chatcmd002Fingerprint(m)
	m.Messages[0].Revision++
	if chatcmd002Fingerprint(m) != fingerprint {
		t.Fatal("an ordinary message's edit asked for the cards again")
	}
	m.Messages[1].Revision++
	if chatcmd002Fingerprint(m) == fingerprint {
		t.Fatal("a vote did not change the fingerprint")
	}
	if voted := chatcmd002Stale(chatcmd002Cards(m), asked); !reflect.DeepEqual(voted, []string{"poll"}) {
		t.Fatalf("after another person's vote the read asks for %v", voted)
	}
	// A card's revision moves with every vote; that is not "edited".
	plain := chatcmd002PlainMessages(m.Messages)
	if plain[1].Edited || !plain[0].Edited || !m.Messages[1].Edited {
		t.Fatalf("edited marks: plain %v card %v, input changed %v", plain[0].Edited, plain[1].Edited, !m.Messages[1].Edited)
	}
}
