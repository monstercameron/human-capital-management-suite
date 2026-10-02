package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// TestTodo_CHATVOICE_006_Timings: the page marks a sentence only from timings the
// server sent; a missing or broken header gives none, and a thread is asked for
// with the names the page shows.
func TestTodo_CHATVOICE_006_Timings(t *testing.T) {
	var body string
	header := chatui.EncodeListenTimings([]chatui.ListenTiming{{Text: "One.", StartMS: 0, EndMS: 900}, {Text: "Two.", StartMS: 900, EndMS: 1800}})
	timings := header
	client := &http.Client{Transport: chatvoiceRoundTripper(func(r *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		h := http.Header{"Content-Type": []string{"audio/wav"}}
		if timings != "" {
			h.Set(chatui.ListenTimingsHeader, timings)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: h, Body: io.NopCloser(strings.NewReader("RIFF audio"))}, nil
	})}
	cfg := journeyclient.Config{TunnelURL: "ws://fixture.invalid/rpc", Bearer: "fixture"}
	input := voiceSpeakInput{TenantID: "t", ConversationID: "c", ThreadID: "root", Names: map[string]string{"ada": "Ada Lovelace"}}
	speech, err := voiceSpeakTimed(context.Background(), client, cfg, input)
	if err != nil || string(speech.Audio) != "RIFF audio" || len(speech.Timings) != 2 || speech.Timings[1].Text != "Two." {
		t.Fatalf("timed speech %+v %v", speech, err)
	}
	var sent map[string]any
	if json.Unmarshal([]byte(body), &sent) != nil || sent["ThreadID"] != "root" || sent["Names"].(map[string]any)["ada"] != "Ada Lovelace" {
		t.Fatalf("thread request body %s", body)
	}
	// A single message sends neither a thread nor names.
	if _, err = voiceSpeakTimed(context.Background(), client, cfg, voiceSpeakInput{TenantID: "t", ConversationID: "c", PostID: "p"}); err != nil || strings.Contains(body, "ThreadID") || strings.Contains(body, "Names") {
		t.Fatalf("message request body %s %v", body, err)
	}
	for name, value := range map[string]string{"absent": "", "garbage": "!!!", "not a list": "e30="} {
		timings = value
		speech, err = voiceSpeakTimed(context.Background(), client, cfg, input)
		if err != nil || len(speech.Timings) != 0 || string(speech.Audio) != "RIFF audio" {
			t.Fatalf("%s: %+v %v", name, speech, err)
		}
	}
	// voiceSpeak still answers audio alone for callers that do not mark sentences.
	timings = header
	if data, contentType, err := voiceSpeak(context.Background(), client, cfg, input); err != nil || string(data) != "RIFF audio" || contentType != "audio/wav" {
		t.Fatalf("voiceSpeak %q %q %v", data, contentType, err)
	}
}

// TestTodo_CHATVOICE_006_ScreenReader: a press that carries pointer detail
// autoplays; a keyboard, switch or assistive activation (detail 0) does not.
func TestTodo_CHATVOICE_006_ScreenReader(t *testing.T) {
	for detail, want := range map[int]bool{0: false, 1: true, 2: true} {
		if got := listenAutoplays(detail); got != want {
			t.Errorf("detail %d: autoplay %v, want %v", detail, got, want)
		}
	}
}

// The settings routes are reachable from the page's own client.
func TestTodo_CHATVOICE_005_Routes(t *testing.T) {
	var path string
	client := &http.Client{Transport: chatvoiceRoundTripper(func(r *http.Request) (*http.Response, error) {
		path = r.URL.Path
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"Allowed":false,"Reason":"channel_off","IsChannel":true}`))}, nil
	})}
	cfg := journeyclient.Config{TunnelURL: "ws://fixture.invalid/rpc", Bearer: "fixture"}
	for _, action := range []string{"policy", "settings", "switch"} {
		var view chatui.VoiceSettingsData
		if err := voiceRequest(context.Background(), client, cfg, action, map[string]string{"TenantID": "t"}, &view); err != nil || path != "/api/chat/voice/"+action || view.Reason != "channel_off" || !view.IsChannel {
			t.Fatalf("%s: path=%s view=%+v err=%v", action, path, view, err)
		}
	}
	if err := voiceRequest(context.Background(), client, cfg, "speak", nil, nil); err == nil {
		t.Fatal("an action that has its own client was sent through the generic one")
	}
}
