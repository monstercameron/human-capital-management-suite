package application

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
)

// TestTodo_CHATGATE_014_Served: the served gate answers GET ?labels=1 with the
// channel's labels and who holds each, to any member and never to a reader the
// authority refuses; it is a read, so a POST of it is refused, and a composition
// with no consumers says it is unavailable instead of listing nothing.
func TestTodo_CHATGATE_014_Served(t *testing.T) {
	app, svc := chatgateServedFixture(t)
	admin, member := chatgateAs(t, "admin"), chatgateAs(t, "member")
	rules := []chatgate.SegmentRule{{ID: "rule-1", Field: "office", Value: "Denver", Label: "Denver crew", Segment: "Denver office"}}
	if _, err := app.GateRequest(admin, ChatgateRequest{Conversation: "room", Action: "segments-save", Key: "rules", ExpectedRevision: chatgateRevision(t, svc), Rules: rules, Reason: "First rules"}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.GateRequest(member, ChatgateRequest{Conversation: "room", Action: "submit", Key: "join", Version: "1.0.0", ExpectedRevision: chatgateRevision(t, svc), Answers: map[string]json.RawMessage{"office": json.RawMessage(`"Denver"`)}}); err != nil {
		t.Fatal(err)
	}
	reply, err := app.GateRequest(chatgateAs(t, "other"), ChatgateRequest{Conversation: "room", Action: chatgateLabelsAction})
	labels, ok := reply.Result.([]chatgate.LabelHolders)
	if err != nil || !ok || len(labels) != 1 || labels[0].Name != "Denver crew" || len(labels[0].People) != 1 || labels[0].People[0] != "member" || reply.View != nil {
		t.Fatalf("labels: %+v %v", reply, err)
	}
	// Over HTTP, as the page asks.
	w := httptest.NewRecorder()
	ChatgateHTTP{Surface: app}.ServeHTTP(w, httptest.NewRequest("GET", ChatgatePath+"?labels=1&conversation=room", nil).WithContext(chatgateAs(t, "other")))
	var wire struct {
		Result []struct {
			Name   string   `json:"name"`
			People []string `json:"people"`
		} `json:"result"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &wire) != nil || len(wire.Result) != 1 || wire.Result[0].Name != "Denver crew" || wire.Result[0].People[0] != "member" {
		t.Fatalf("over HTTP: %d %s", w.Code, w.Body.String())
	}
	// It is a read: GET alone.
	w = httptest.NewRecorder()
	post := httptest.NewRequest("POST", ChatgatePath, strings.NewReader(`{"Conversation":"room","Action":"labels"}`))
	ChatgateHTTP{Surface: app}.ServeHTTP(w, post.WithContext(chatgateAs(t, "other")))
	if w.Code != 400 {
		t.Fatalf("a POST of the labels read: %d %s", w.Code, w.Body.String())
	}
	// No consumers composed: unavailable, never an empty list.
	bare := &ChatgateApplication{Service: svc}
	if _, err = bare.GateRequest(admin, ChatgateRequest{Conversation: "room", Action: chatgateLabelsAction}); !errors.Is(err, chatgate.ErrUnavailable) {
		t.Fatalf("no consumers: %v", err)
	}
}

// TestTodo_CHATGATE_014_Served_Audience: the served gate answers GET ?audience=1
// with what the writer may address; nothing while addressing is off, the writer's
// own segments once it is on, and "restricted" only in a private channel whose
// administrators allowed it.
func TestTodo_CHATGATE_014_Served_Audience(t *testing.T) {
	app, svc := chatgateServedFixture(t)
	admin, member := chatgateAs(t, "admin"), chatgateAs(t, "member")
	rules := []chatgate.SegmentRule{{ID: "rule-1", Field: "office", Value: "Denver", Label: "Denver crew", Segment: "Denver office"}}
	if _, err := app.GateRequest(admin, ChatgateRequest{Conversation: "room", Action: "segments-save", Key: "rules", ExpectedRevision: chatgateRevision(t, svc), Rules: rules, Reason: "First rules"}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.GateRequest(member, ChatgateRequest{Conversation: "room", Action: "submit", Key: "join", Version: "1.0.0", ExpectedRevision: chatgateRevision(t, svc), Answers: map[string]json.RawMessage{"office": json.RawMessage(`"Denver"`)}}); err != nil {
		t.Fatal(err)
	}
	read := func(ctx context.Context) chatgate.AudienceOptions {
		t.Helper()
		w := httptest.NewRecorder()
		ChatgateHTTP{Surface: app}.ServeHTTP(w, httptest.NewRequest("GET", ChatgatePath+"?audience=1&conversation=room", nil).WithContext(ctx))
		var wire struct {
			Result chatgate.AudienceOptions `json:"result"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &wire) != nil {
			t.Fatalf("over HTTP: %d %s", w.Code, w.Body.String())
		}
		return wire.Result
	}
	if got := read(member); len(got.Segments) != 0 || got.Restricted {
		t.Fatalf("with addressing off: %+v", got)
	}
	if _, err := app.GateRequest(admin, ChatgateRequest{Conversation: "room", Action: "levels-save", Key: "levels", ExpectedRevision: chatgateRevision(t, svc), Addressing: true, Restricted: true}); err != nil {
		t.Fatal(err)
	}
	if got := read(member); len(got.Segments) != 1 || got.Segments[0].Name != "Denver office" || got.Segments[0].People != 1 || !got.Restricted {
		t.Fatalf("the member's options: %+v", got)
	}
	// A person in no segment may address none.
	if got := read(chatgateAs(t, "other")); len(got.Segments) != 0 {
		t.Fatalf("a person in no segment: %+v", got)
	}
	if reply, err := app.GateRequest(member, ChatgateRequest{Conversation: "room", Action: chatgateAudienceAction}); err != nil || reply.View != nil {
		t.Fatalf("direct: %+v %v", reply, err)
	}
	bare := &ChatgateApplication{Service: svc}
	if _, err := bare.GateRequest(admin, ChatgateRequest{Conversation: "room", Action: chatgateAudienceAction}); !errors.Is(err, chatgate.ErrUnavailable) {
		t.Fatalf("no consumers: %v", err)
	}
}
