package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func TestIssue3929_ClientTalksOnlyToTheSavedSearchRoute(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		if r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("not signed in as the person")
		}
		switch r.Method {
		case "GET":
			_ = json.NewEncoder(w).Encode([]issue3929Wire{{ID: "a", Name: "Leave", Query: "leave"}, {ID: "", Name: "bad"}})
		case "POST":
			var in map[string]string
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in["name"] != "Leave" || in["query"] != "leave" {
				t.Errorf("body = %v", in)
			}
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(issue3929Wire{ID: "a", Name: "Leave", Query: "leave"})
		case "DELETE":
			w.WriteHeader(204)
		default:
			w.WriteHeader(409)
			_, _ = w.Write([]byte(`{"code":"name_taken"}`))
		}
	}))
	defer server.Close()
	cfg := journeyclient.Config{TunnelURL: server.URL + "/tunnel?secret=discard", Bearer: "fixture"}
	ctx, c := context.Background(), server.Client()

	var list []issue3929Wire
	if err := issue3929Request(ctx, c, cfg, "GET", "", nil, &list); err != nil || len(issue3929Items(list)) != 1 {
		t.Fatalf("list %+v %v", list, err)
	}
	var made issue3929Wire
	if err := issue3929Request(ctx, c, cfg, "POST", "", map[string]string{"name": "Leave", "query": "leave"}, &made); err != nil || made.ID != "a" {
		t.Fatalf("create %+v %v", made, err)
	}
	if err := issue3929Request(ctx, c, cfg, "DELETE", "a b", nil, nil); err != nil {
		t.Fatal(err)
	}
	err := issue3929Request(ctx, c, cfg, "PUT", "a", map[string]string{"name": "x", "query": "y"}, nil)
	if issue3929StatusFor(err) != "nameTaken" {
		t.Fatalf("conflict = %v", err)
	}
	want := []string{"GET /api/chat/search/saved?", "POST /api/chat/search/saved?", "DELETE /api/chat/search/saved?id=a+b", "PUT /api/chat/search/saved?id=a"}
	for i, w := range want {
		if i >= len(seen) || seen[i] != w {
			t.Fatalf("requests = %v, want %v", seen, want)
		}
	}
	if err := issue3929Request(ctx, c, journeyclient.Config{TunnelURL: server.URL}, "GET", "", nil, &list); err == nil {
		t.Error("a request with no sign-in was sent")
	}
}

func TestIssue3929_StatusFollowsTheServerAnswer(t *testing.T) {
	for code, want := range map[string]string{"name_taken": "nameTaken", "limit": "limit", "invalid": "invalid", "not_found": "failed", "unavailable": "failed"} {
		if got := issue3929StatusFor(chatsearchClientError{code}); got != want {
			t.Errorf("%s -> %s, want %s", code, got, want)
		}
	}
	if issue3929StatusFor(context.DeadlineExceeded) != "failed" {
		t.Error("a timeout is not a failure")
	}
}

func TestIssue3929_ApplyKeepsTheListWhenAChangeFails(t *testing.T) {
	m := chatui.Model{SearchSaved: []chatui.SavedSearchItem{{ID: "a", Name: "Leave", Query: "leave"}}, SavedSearchEditing: "a"}
	issue3929Apply(&m, nil, "nameTaken", "Leave")
	if len(m.SearchSaved) != 1 || m.SavedSearchEditing != "a" || m.SavedSearchStatus != "nameTaken" {
		t.Fatalf("a failed change lost the list or the editor: %+v", m)
	}
	issue3929Apply(&m, []chatui.SavedSearchItem{}, "deleted", "")
	if len(m.SearchSaved) != 0 || m.SavedSearchEditing != "" || m.SavedSearchStatus != "deleted" {
		t.Fatalf("a finished delete: %+v", m)
	}
}
