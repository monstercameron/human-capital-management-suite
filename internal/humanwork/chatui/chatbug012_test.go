package chatui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

// TestTodo_CHATBUG_012 pins the reading-settings client half: the typed
// {"available":false} answer is understood as "feature off", and once it is
// seen the client never asks again in that session.
func TestTodo_CHATBUG_012(t *testing.T) {
	var hits atomic.Int32
	off := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"available":false,"reason":"renderings_not_composed"}`))
	}))
	defer off.Close()
	session := &ReadingSession{}
	client := ReadingSettingsClient{Origin: off.URL, Bearer: "token", HTTP: off.Client(), Session: session}
	ctx := context.Background()
	if _, err := client.Load(ctx, "room"); !errors.Is(err, chatrender.ErrUnavailable) {
		t.Fatalf("settings: want ErrUnavailable, got %v", err)
	}
	if !session.Off() || hits.Load() != 1 {
		t.Fatalf("session must be off after one ask: off=%v hits=%d", session.Off(), hits.Load())
	}
	for i := 0; i < 5; i++ {
		if _, err := client.Load(ctx, "room"); !errors.Is(err, chatrender.ErrUnavailable) {
			t.Fatal(err)
		}
		if _, err := client.LoadLanguages(ctx, "room"); !errors.Is(err, chatrender.ErrUnavailable) {
			t.Fatal(err)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("the client asked %d times in one session; once is the limit", hits.Load())
	}

	// Languages seen first is understood the same way, on a fresh session.
	hits.Store(0)
	client.Session = &ReadingSession{}
	if _, err := client.LoadLanguages(ctx, "room"); !errors.Is(err, chatrender.ErrUnavailable) || !client.Session.Off() {
		t.Fatalf("languages: %v off=%v", err, client.Session.Off())
	}
	if _, err := client.Load(ctx, "room"); !errors.Is(err, chatrender.ErrUnavailable) || hits.Load() != 1 {
		t.Fatalf("settings after languages: %v hits=%d", err, hits.Load())
	}

	// A composed service keeps answering normally and never trips the session.
	preference, _ := json.Marshal(chatrender.DefaultPreference("en-US"))
	on := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/chat/renderings/v1/languages" {
			_, _ = w.Write([]byte(`{"de":2}`))
			return
		}
		_, _ = w.Write(preference)
	}))
	defer on.Close()
	composed := ReadingSettingsClient{Origin: on.URL, Bearer: "token", HTTP: on.Client(), Session: &ReadingSession{}}
	if got, err := composed.Load(ctx, "room"); err != nil || got.ReadingLanguage != "en" {
		t.Fatalf("composed settings: %+v %v", got, err)
	}
	if got, err := composed.LoadLanguages(ctx, "room"); err != nil || got["de"] != 2 {
		t.Fatalf("composed languages: %v %v", got, err)
	}
	if composed.Session.Off() {
		t.Fatal("a composed service must not switch the session off")
	}
}

// TestTodo_CHATBUG_012_Browser: while the page model says a feature is off (or
// has not been answered yet, the empty ChatFeatures), nothing that would ask
// the server for that feature is rendered, so the client never asks.
func TestTodo_CHATBUG_012_Browser(t *testing.T) {
	model := Model{Locale: "en-US", SelectedID: "room", ChatFeatures: &ChatFeatures{}}
	if chattoneToolbar(model, "composer", false) != nil {
		t.Fatal("writing style toolbar (which asks for a suggestion) rendered while WritingStyles is off")
	}
	if integrate2ReadingSettings(model) != nil {
		t.Fatal("reading settings (which ask for settings and languages) rendered while Renderings is off")
	}
	model.ChatFeatures = &ChatFeatures{WritingStyles: true, Renderings: true}
	if chattoneToolbar(model, "composer", false) == nil || integrate2ReadingSettings(model) == nil {
		t.Fatal("a composed feature must render")
	}
}
