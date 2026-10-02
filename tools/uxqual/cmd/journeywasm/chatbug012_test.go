package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// TestTodo_CHATBUG_012 pins the writing-style client half: a 200
// {"available":false} answer is a typed "not available" error, and after the
// first one no other room asks again in the same session.
func TestTodo_CHATBUG_012(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, `{"available":false,"reason":"writing_style_not_composed"}`)
	}))
	defer server.Close()
	cfg := journeyclient.Config{TunnelURL: server.URL, Bearer: "fixture"}
	session := chattoneSession{}
	for _, room := range []string{"room-a", "room-b", "room-c", "room-a", "room-b"} {
		if !session.Allow() {
			continue
		}
		reply, err := chattoneRequest(context.Background(), server.Client(), cfg, "suggestion", chattoneDraftRequest{ConversationID: room})
		if code := chattoneErrorCode(err); code != chattoneNotAvailable || reply.Enabled {
			t.Fatalf("room %s: want typed not-available, got %q %+v", room, code, reply)
		}
		session.Record(err)
	}
	if hits.Load() != 1 {
		t.Fatalf("asked %d times; the session must ask once", hits.Load())
	}
	// An ordinary failure does not switch the feature off for every room.
	other := chattoneSession{}
	other.Record(chattoneRequestError{"unavailable"})
	if !other.Allow() {
		t.Fatal("a transient failure must not switch the feature off")
	}
	// A normal enabled answer has no "available" field and still parses.
	normal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"enabled":true,"styles":[]}`)
	}))
	defer normal.Close()
	cfg.TunnelURL = normal.URL
	if reply, err := chattoneRequest(context.Background(), normal.Client(), cfg, "suggestion", chattoneDraftRequest{ConversationID: "room"}); err != nil || !reply.Enabled {
		t.Fatal(reply, err)
	}
}
