package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func TestTodo_CHATSEARCH_002_ThreadNavigation(t *testing.T) {
	target := chatsearch.Target{ConversationID: "room", ThreadID: "parent", MessageID: "reply", Sequence: 42, ThreadSequence: 5}
	model := chatui.Model{SelectedID: "room", State: chatui.StateReady}
	if chatsearchThreadReady(model, target, false) {
		t.Fatal("thread committed before the parent timeline read")
	}
	model.FocusMessageID = "parent"
	if chatsearchThreadReady(model, target, true) || !chatsearchThreadReady(model, target, false) {
		t.Fatal("thread did not wait for room opening to finish")
	}
	model.SelectedID = "other"
	if chatsearchThreadReady(model, target, false) {
		t.Fatal("thread crossed a room navigation")
	}
	model.SelectedID, model.FocusMessageID = "room", ""
	target.ThreadSequence = 0
	if !chatsearchThreadReady(model, target, false) {
		t.Fatal("unreadable parent prevented an authorized reply window")
	}
	model.State = chatui.StateLoading
	if chatsearchThreadReady(model, target, false) {
		t.Fatal("thread overwrote a loading timeline")
	}
}

func TestTodo_CHATSEARCH_002_Client(t *testing.T) {
	query, err := chatsearchControlQuery(`budget in:old from:agent`, map[string]string{"channel": "General team", "person": "Full Name", "on": "2026-10-01"}, map[string]bool{"file": true, "mentions": true})
	p, parseErr := chatsearch.Parse(query)
	if err != nil || parseErr != nil || p.Filters.Conversation != "General team" || p.Filters.Person != "Full Name" || !p.Filters.Agent || !p.Filters.File || !p.Filters.MentionsMe {
		t.Fatalf("controls %q %+v %v %v", query, p, err, parseErr)
	}
	if _, err = chatsearchControlQuery("budget", map[string]string{"channel": `bad"name`}, nil); !errors.Is(err, chatsearch.ErrInvalid) {
		t.Fatal("unquoted value accepted")
	}
	target := chatsearch.Target{ConversationID: "room", MessageID: "message", Sequence: 42, ThreadID: "parent", ItemID: "item"}
	b, _ := json.Marshal(target)
	got, err := chatsearchDecodeTarget(base64.RawURLEncoding.EncodeToString(b))
	if err != nil || got != target {
		t.Fatalf("lost exact target %+v %v", got, err)
	}
	for _, invalid := range []string{"!", strings.Repeat("x", 4097), base64.RawURLEncoding.EncodeToString([]byte(`{}`))} {
		if _, err = chatsearchDecodeTarget(invalid); !errors.Is(err, chatsearch.ErrInvalid) {
			t.Fatal("invalid target accepted")
		}
	}
	for _, test := range []struct {
		err  error
		want string
	}{{chatsearchClientError{"meaning_unavailable"}, "meaning"}, {chatsearchClientError{"invalid"}, "invalid"}, {errors.New("network"), "error"}} {
		if got := chatsearchErrorCode(test.err); got != test.want {
			t.Fatalf("error %s want %s", got, test.want)
		}
	}
	older, newer, err := chatsearchThreadRequests("tenant", target)
	if err != nil || older.BeforeSequence != 43 || newer.AfterSequence != 41 || older.ConversationId != "room" || newer.ConversationId != "room" {
		t.Fatalf("reply context %v %v %v", older, newer, err)
	}
	target.ThreadID = ""
	if _, _, err := chatsearchThreadRequests("tenant", target); !errors.Is(err, chatsearch.ErrInvalid) {
		t.Fatal("missing thread target accepted")
	}
}

func TestTodo_CHATSEARCH_002_HTTPClient(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer fixture" || r.URL.RawQuery != "" {
			t.Error("authentication or inherited URL query")
		}
		if r.Method == "DELETE" {
			w.WriteHeader(204)
			return
		}
		if r.Method == "GET" {
			w.WriteHeader(503)
			_, _ = w.Write([]byte(`{"code":"meaning_unavailable"}`))
			return
		}
		var q chatsearch.Request
		if json.NewDecoder(r.Body).Decode(&q) != nil || q.Query != "budget" || r.URL.Path != "/api/chat/search" {
			t.Error("wrong endpoint or request")
		}
		_ = json.NewEncoder(w).Encode(chatsearch.Response{Mode: "keyword"})
	}))
	defer server.Close()
	cfg := journeyclient.Config{TunnelURL: server.URL + "/tunnel?secret=discard", Bearer: "fixture"}
	var got chatsearch.Response
	if err := chatsearchRequest(context.Background(), server.Client(), cfg, "/api/chat/search", "POST", chatsearch.Request{Query: "budget"}, &got); err != nil || got.Mode != "keyword" {
		t.Fatalf("request %+v %v", got, err)
	}
	if err := chatsearchRequest(context.Background(), server.Client(), cfg, "/api/chat/search/recent", "GET", nil, &got); chatsearchErrorCode(err) != "meaning" {
		t.Fatal("unavailable not typed")
	}
	if err := chatsearchRequest(context.Background(), server.Client(), cfg, "/api/chat/search/recent", "DELETE", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := chatsearchRequest(context.Background(), server.Client(), cfg, "/elsewhere", "POST", nil, &got); !errors.Is(err, chatsearch.ErrInvalid) || calls != 3 {
		t.Fatal("client followed an arbitrary endpoint")
	}
}
