package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

type chatremoveHTTPTransport func(*http.Request) (*http.Response, error)

func (f chatremoveHTTPTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTodo_CHATMOD_004(t *testing.T) {
	input, err := chatremoveFormInput("preview", map[string]string{"conversation": "room", "reason": "spam", "author": "author", "author_home": "tenant", "from": "2026-10-01T08:00", "until": "2026-10-01T09:00"}, nil, time.FixedZone("local", -4*3600))
	if err != nil || input.Removal.Selection.From.UTC().Hour() != 12 || input.Removal.Action != "remove" {
		t.Fatalf("input=%+v err=%v", input, err)
	}
	_, err = chatremoveFormInput("preview", map[string]string{"author": "author", "from": "invalid"}, nil, nil)
	if !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatal(err)
	}
	input, err = chatremoveFormInput("apply", map[string]string{"confirmation": "digest", "confirmed_count": "2", "reason": "spam"}, []string{"one", "two"}, nil)
	if err != nil || input.Removal.ConfirmedCount != 2 || len(input.Removal.Selection.PostIDs) != 2 {
		t.Fatalf("input=%+v %v", input, err)
	}
}

func TestTodo_CHATMOD_005(t *testing.T) {
	client := &http.Client{Transport: chatremoveHTTPTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/chat/moderation" || r.URL.Query().Get("query") != "spam" || r.Header.Get("Authorization") != "Bearer fixture" {
			t.Errorf("request=%+v", r)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"Items":[],"Count":0}`))}, nil
	})}
	cfg := journeyclient.Config{TunnelURL: "https://fixture.invalid", Bearer: "fixture"}
	b, err := chatremoveRequest(context.Background(), client, cfg, "", "spam", nil)
	if err != nil || !strings.Contains(string(b), `"Count":0`) {
		t.Fatalf("body=%s err=%v", b, err)
	}
	if !chatremoveRoute("/chat/moderation/") || chatremoveRoute("/chat/moderation-other") {
		t.Fatal("route boundary")
	}
	if chatremoveErrorKey(chat.ErrConflict) != "conflict" {
		t.Fatal("conflict copy")
	}
}

func TestTodo_CHATMOD_005_Security(t *testing.T) {
	client := &http.Client{Transport: chatremoveHTTPTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader("private content"))}, nil
	})}
	_, err := chatremoveRequest(context.Background(), client, journeyclient.Config{TunnelURL: "https://fixture.invalid", Bearer: "fixture"}, "review", "", map[string]string{"Note": "reason"})
	if !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal(err)
	}
	_, err = chatremoveRequest(context.Background(), client, journeyclient.Config{TunnelURL: "https://fixture.invalid", Bearer: "fixture"}, "arbitrary", "", nil)
	if !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatal(err)
	}
}
