package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

type chatsaveRoundTrip func(*http.Request) (*http.Response, error)

func (f chatsaveRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTodo_CHATSAVE_001(t *testing.T) {
	cfg := journeyclient.Config{TunnelURL: "wss://fixture.invalid/tunnel", Bearer: "deterministic-fixture", Tenant: "tenant", Subject: "person", Locale: "en-US"}
	calls := 0
	client := &http.Client{Transport: chatsaveRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Scheme != "https" || r.URL.Path != "/api/chat/saved" || r.Header.Get("Authorization") != "Bearer deterministic-fixture" {
			t.Fatal("endpoint or authorization")
		}
		body := `{"Items":[{"HomeTenantID":"tenant","PersonID":"person","PostID":"post"}],"NextCursor":"next"}`
		if r.Method == http.MethodPost {
			var command chatsaveCommand
			if json.NewDecoder(r.Body).Decode(&command) != nil || command.Action != "save" || command.PostID != "post" {
				t.Fatal("command")
			}
			body = `{"PostID":"post"}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}
	page, err := chatsaveRequest(context.Background(), client, cfg, "tenant", "todo", "", "", nil)
	if err != nil || len(page.Items) != 1 || page.NextCursor != "next" {
		t.Fatalf("page=%+v %v", page, err)
	}
	_, err = chatsaveRequest(context.Background(), client, cfg, "tenant", "all", "", "", &chatsaveCommand{Action: "save", ConversationID: "room", PostID: "post"})
	if err != nil || calls != 2 {
		t.Fatal("save request", err)
	}
	client.Transport = chatsaveRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 409, Body: io.NopCloser(strings.NewReader(`{"code":"saved_limit"}`))}, nil
	})
	if _, err = chatsaveRequest(context.Background(), client, cfg, "tenant", "all", "", "", nil); chatsaveErrorCode(err) != "saved_limit" {
		t.Fatal("limit code", err)
	}
	page = chat.SavedPage{Items: []chat.SavedItem{{HomeTenantID: "tenant", PersonID: "person", TenantID: "tenant", ConversationID: "room", PostID: "post", Note: "private", Availability: "readable", Post: &chat.Post{AuthorID: "author", AuthorHomeTenantID: "tenant", Body: "current", Sequence: 4}}, {HomeTenantID: "tenant", PersonID: "other", Note: "SECRET"}, {HomeTenantID: "tenant", PersonID: "person", Availability: "no_access", Post: &chat.Post{Body: "SECRET REVOKED"}}}}
	rows := chatsaveRows(page, cfg, chatui.Model{SelectedID: "room", Members: []chatui.Member{{ID: "author", HomeTenantID: "tenant", Name: "Alex"}}})
	if len(rows) != 2 || rows[0].Author != "Alex" || rows[0].Sequence != 4 || rows[0].Body != "current" || rows[1].Body != "" {
		t.Fatalf("owner projection=%+v", rows)
	}
}

func TestTodo_CHATSAVE_001_Accessibility(t *testing.T) {
	if !chatsaveShortcut("S", true, true, false, false, false, false) {
		t.Fatal("save shortcut")
	}
	for _, flags := range [][6]bool{{false, true, false, false, false, false}, {true, false, false, false, false, false}, {true, true, true, false, false, false}, {true, true, false, true, false, false}, {true, true, false, false, true, false}, {true, true, false, false, false, true}} {
		if chatsaveShortcut("s", flags[0], flags[1], flags[2], flags[3], flags[4], flags[5]) {
			t.Fatal("shortcut interferes with edit, repeat or modifier")
		}
	}
	if chatsaveShortcut("b", true, true, false, false, false, false) {
		t.Fatal("wrong key accepted")
	}
}
