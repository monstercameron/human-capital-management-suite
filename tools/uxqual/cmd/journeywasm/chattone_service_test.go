package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func TestTodo_CHATTONE_004_Browser(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("missing session")
		}
		switch r.URL.Path {
		case chattoneHTTPPath + "/suggestion":
			if r.Method != "GET" || r.URL.Query().Get("conversation_id") != "room" {
				t.Error("suggestion identity")
			}
			_, _ = io.WriteString(w, `{"enabled":true,"styles":[{"id":"professional","label":"Professional","register":"professional"}],"suggestion":{"style_id":"professional","reason_key":"neutral"}}`)
		case chattoneHTTPPath + "/rewrite":
			var draft chattoneDraftRequest
			if json.NewDecoder(r.Body).Decode(&draft) != nil || draft.Draft != "  Please retain\n these words.  " || draft.StyleID != "professional" || r.Method != "POST" {
				t.Error("draft changed in transit")
			}
			_, _ = io.WriteString(w, `{"enabled":true,"draft":"Please keep these words."}`)
		default:
			t.Error("wrong route", r.URL.Path)
		}
	}))
	defer server.Close()
	cfg := journeyclient.Config{TunnelURL: server.URL, Bearer: "fixture"}
	reply, err := chattoneRequest(context.Background(), server.Client(), cfg, "suggestion", chattoneDraftRequest{ConversationID: "room"})
	if err != nil || !reply.Enabled || reply.Suggestion.StyleID != "professional" || requests.Load() != 1 {
		t.Fatal(reply, err, requests.Load())
	}
	// Reading the suggestion never transmits a draft or triggers rewriting.
	typed := "  Please retain\n these words.  "
	p := chattonePreview{}
	serial, ok := p.Begin(typed)
	if !ok {
		t.Fatal("begin")
	}
	reply, err = chattoneRequest(context.Background(), server.Client(), cfg, "rewrite", chattoneDraftRequest{"room", typed, "professional"})
	if err != nil || !p.Complete(serial, typed, typed, reply.Draft) || p.Rewritten != "Please keep these words." {
		t.Fatal("preview", p, err)
	}
	p.ShowChanges = true
	restored, ok := p.Undo()
	if !ok || restored != typed || p.ShowChanges || p.Original != "" || requests.Load() != 2 {
		t.Fatal("undo lost typed bytes", restored)
	}
}
func TestTodo_CHATTONE_004_Security(t *testing.T) {
	p := chattonePreview{}
	serial, _ := p.Begin("Please keep my words")
	if p.Complete(serial, "Please keep my words", "New words typed meanwhile", "Unwanted rewrite") || p.Busy || p.Rewritten != "" {
		t.Fatal("late response overwrote draft")
	}
	serial, _ = p.Begin("Please keep my words")
	p.Clear()
	if p.Complete(serial, "Please keep my words", "Please keep my words", "Late rewrite") {
		t.Fatal("cancelled response accepted")
	}
	serial, _ = p.Begin("Please keep my words")
	if _, ok := p.Begin("Another draft text"); ok {
		t.Fatal("duplicate request")
	}
	if _, ok := p.Undo(); ok {
		t.Fatal("undo during pending request")
	}
	if p.Complete(serial, "Please keep my words", "Please keep my words", "") || p.Busy {
		t.Fatal("empty failure preview")
	}
	if _, ok := p.Begin("two words"); ok {
		t.Fatal("too few words")
	}
}
func TestTodo_CHATTONE_004_Fault(t *testing.T) {
	for _, code := range []string{"limit", "preservation", "policy", "invalid", "denied", "disabled", "private provider stack trace"} {
		t.Run(code, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(429)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
			}))
			defer server.Close()
			_, err := chattoneRequest(context.Background(), server.Client(), journeyclient.Config{TunnelURL: server.URL, Bearer: "fixture"}, "rewrite", chattoneDraftRequest{"room", "Please keep this draft", "professional"})
			want := code
			if strings.HasPrefix(code, "private") {
				want = "unavailable"
			}
			if chattoneErrorCode(err) != want {
				t.Fatal(err)
			}
		})
	}
	for _, cfg := range []journeyclient.Config{{TunnelURL: "not-a-url", Bearer: "fixture"}, {TunnelURL: "ftp://example.com", Bearer: "fixture"}, {TunnelURL: "https://example.com"}} {
		if _, err := chattoneRequest(context.Background(), http.DefaultClient, cfg, "suggestion", chattoneDraftRequest{}); chattoneErrorCode(err) != "unavailable" {
			t.Fatal("bad config", err)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{"enabled":true,"draft":""}`) }))
	defer server.Close()
	cfg := journeyclient.Config{TunnelURL: server.URL, Bearer: "fixture"}
	for _, action := range []string{"invalid", "rewrite"} {
		_, err := chattoneRequest(context.Background(), server.Client(), cfg, action, chattoneDraftRequest{"room", "two words", "professional"})
		if chattoneErrorCode(err) != "invalid" {
			t.Fatal(err)
		}
	}
	_, err := chattoneRequest(context.Background(), server.Client(), cfg, "rewrite", chattoneDraftRequest{"room", "Please keep my words", "professional"})
	if chattoneErrorCode(err) != "unavailable" {
		t.Fatal("empty output", err)
	}
	if chattoneErrorCode(errors.New("internal details")) != "unavailable" {
		t.Fatal("raw error displayed")
	}
}

func TestTodo_CHATTONE_004_EditablePreview(t *testing.T) {
	p := chattonePreview{}
	typed := "  Keep my exact\n text  "
	serial, _ := p.Begin(typed)
	if !p.Complete(serial, typed, typed, "Keep my text") {
		t.Fatal("preview")
	}
	p.Edit("Writer edited the preview")
	if p.Rewritten != "Writer edited the preview" || p.Original != typed {
		t.Fatal("editable preview lost original", p)
	}
	if restored, ok := p.Undo(); !ok || restored != typed {
		t.Fatal("undo edited preview", restored)
	}
	serial, _ = p.Begin(typed)
	_ = p.Complete(serial, typed, typed, "Keep my text")
	p.Edit("")
	if _, ok := p.Undo(); ok {
		t.Fatal("sent/cleared draft kept old undo")
	}
}
