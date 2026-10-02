package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

func TestTodo_CHATMOD_003(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Fixture-Session") != "current" {
			t.Error("current session absent")
		}
		paths = append(paths, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/try"):
			_, _ = w.Write([]byte(`{"Action":"mask","Masked":"[removed word]"}`))
		case strings.HasSuffix(r.URL.Path, "/hits"):
			if r.URL.Query().Get("before") == "19" && (r.URL.Query().Get("q") != "Project name" || r.URL.Query().Get("channel") != "c") {
				t.Error("hit search cursor lost")
			}
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/enablements"):
			_, _ = w.Write([]byte(`[]`))
		case r.Method == "GET":
			_, _ = w.Write([]byte(`[{"ID":"rule","Version":"1.0.0"}]`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()
	c := FilterAPIClient{BaseURL: server.URL, Headers: func() http.Header { return http.Header{"X-Fixture-Session": []string{"current"}} }}
	ctx := t.Context()
	defs, err := c.List(ctx)
	if err != nil || len(defs) != 1 {
		t.Fatal("list", err)
	}
	if err = c.CreateVersion(ctx, defs[0]); err != nil {
		t.Fatal(err)
	}
	if err = c.Enable(ctx, "rule", "c", true, true); err != nil {
		t.Fatal(err)
	}
	if err = c.Enable(ctx, "rule", "c", false, false); err != nil {
		t.Fatal(err)
	}
	out, err := c.Try(ctx, defs[0], "c", "quartz")
	if err != nil || out.Action != "mask" {
		t.Fatal("try", err)
	}
	hits, err := c.Hits(ctx)
	if err != nil || len(hits) != 0 {
		t.Fatal("hits", err)
	}
	if _, err = c.SearchHits(ctx, "Project name", "c", 19); err != nil {
		t.Fatal("search hits", err)
	}
	settings, err := c.Enablements(ctx)
	if err != nil || len(settings) != 0 {
		t.Fatal("enablements", err)
	}
	if len(paths) != 8 {
		t.Fatal("missing service operation", paths)
	}
}
func TestTodo_CHATMOD_002_Browser(t *testing.T) {
	definition := chatfilter.Definition{ID: "fixture", Name: "Project", Kind: "words", Version: "1.0.0", Action: "mask", Match: []string{"quartz"}, Product: true}
	hint, err := ChatFilterHint([]chatfilter.Definition{definition}, chatfilter.Input{Body: "Q U A R T Z"})
	if err != nil || hint.Masked != "[removed word]" {
		t.Fatal("client and server matcher diverged", err)
	}
	definition.Product = false
	hint, err = ChatFilterHint([]chatfilter.Definition{definition}, chatfilter.Input{Body: "quartz"})
	if err != nil || len(hint.Hits) != 0 {
		t.Fatal("private definitions compiled as product hints", err)
	}
	draft := "hello quartz"
	state, ok := ParseFilterBlockedDraft(draft, &FilterAPIError{Code: "content_blocked", RuleName: "Project", Span: &chatfilter.Span{Start: 6, End: 12}})
	if !ok || state.Draft != draft || state.Span.Start != 6 {
		t.Fatal("blocked draft lost")
	}
	for _, err := range []error{nil, errors.New("unavailable"), &FilterAPIError{Code: "content_blocked", Span: &chatfilter.Span{Start: -1, End: 4}}, &FilterAPIError{Code: "content_blocked", Span: &chatfilter.Span{Start: 0, End: 99}}} {
		if _, ok := ParseFilterBlockedDraft(draft, err); ok {
			t.Fatal("invalid span accepted")
		}
	}
	if _, ok := ParseFilterBlockedDraft("é", &FilterAPIError{Code: "content_blocked", Span: &chatfilter.Span{Start: 0, End: 1}}); ok {
		t.Fatal("split UTF-8 accepted")
	}
	if _, ok := ParseFilterBlockedDraft(draft, errors.New(`rpc: {"code":"content_blocked","rule_name":"Project","span":{"Start":6,"End":12}}`)); !ok {
		t.Fatal("wire error lost")
	}
}
