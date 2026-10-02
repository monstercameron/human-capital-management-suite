package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// TestTodo_CHATVOICE_006 is the client's side of Listen: one authenticated POST
// that names a message, audio back, and a stable word for each refusal.
func TestTodo_CHATVOICE_006(t *testing.T) {
	var path, body string
	status := http.StatusOK
	client := &http.Client{Transport: chatvoiceRoundTripper(func(r *http.Request) (*http.Response, error) {
		path = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		if r.Header.Get("Authorization") != "Bearer fixture" || r.Method != http.MethodPost || r.URL.RawQuery != "" {
			t.Error("session, method or query is wrong")
		}
		header := http.Header{"Content-Type": []string{"audio/mpeg"}}
		return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader("ID3 audio"))}, nil
	})}
	cfg := journeyclient.Config{TunnelURL: "ws://fixture.invalid/rpc?spoofed=1", Bearer: "fixture"}
	data, contentType, err := voiceSpeak(context.Background(), client, cfg, voiceSpeakInput{TenantID: "t", ConversationID: "c", PostID: "p"})
	if err != nil || string(data) != "ID3 audio" || contentType != "audio/mpeg" || path != "/api/chat/voice/speak" {
		t.Fatalf("speech %q %q path=%s err=%v", data, contentType, path, err)
	}
	var sent voiceSpeakInput
	if json.Unmarshal([]byte(body), &sent) != nil || sent.PostID != "p" || strings.Contains(body, "Bearer") {
		t.Fatalf("request body %s", body)
	}
	for code, want := range map[int]string{http.StatusConflict: "barred", http.StatusRequestEntityTooLarge: "toolong", http.StatusServiceUnavailable: "unavailable", http.StatusForbidden: "failed", http.StatusBadRequest: "failed"} {
		status = code
		_, _, err = voiceSpeak(context.Background(), client, cfg, voiceSpeakInput{PostID: "p"})
		if err == nil || listenStatusKey(err) != want {
			t.Fatalf("status %d: err=%v key=%s want %s", code, err, listenStatusKey(err), want)
		}
	}
	if !errors.Is(func() error {
		status = http.StatusConflict
		_, _, e := voiceSpeak(context.Background(), client, cfg, voiceSpeakInput{})
		return e
	}(), errListenOutsideBarred) {
		t.Fatal("outside-service refusal lost")
	}
	if _, _, err = voiceSpeak(context.Background(), client, journeyclient.Config{TunnelURL: "ftp://fixture.invalid", Bearer: "fixture"}, voiceSpeakInput{}); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("unsafe scheme: %v", err)
	}
	if _, _, err = voiceSpeak(context.Background(), client, journeyclient.Config{TunnelURL: "http://fixture.invalid"}, voiceSpeakInput{}); !errors.Is(err, chat.ErrVoiceUnavailable) {
		t.Fatalf("no session: %v", err)
	}
}
