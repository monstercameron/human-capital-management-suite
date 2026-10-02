package chatui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

// chatbug087Server is the settings route of a server whose credential rotated
// while a tab stayed open: only the current bearer is accepted, and it keeps what
// was last saved.
type chatbug087Server struct {
	mu      sync.Mutex
	current string
	stored  chatrender.Preference
	puts    int
	bodies  []string
	status  map[string]int // forced status by method
}

func (s *chatbug087Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if forced := s.status[r.Method]; forced != 0 && r.Header.Get("Authorization") != "Bearer "+s.current {
		w.WriteHeader(forced)
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+s.current {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodPut {
		raw, _ := io.ReadAll(r.Body)
		s.bodies = append(s.bodies, string(raw))
		var body struct {
			Settings chatrender.Preference `json:"settings"`
		}
		if json.Unmarshal(raw, &body) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		s.stored = body.Settings
		s.puts++
		_, _ = w.Write([]byte(`{"ok":true}`))
		return
	}
	_ = json.NewEncoder(w).Encode(s.stored)
}

func chatbug087Client(server *httptest.Server, bearer string, refresh func(context.Context) (string, error)) ReadingSettingsClient {
	return ReadingSettingsClient{Origin: server.URL, Bearer: bearer, HTTP: server.Client(), Refresh: refresh}
}

// TestTodo_CHATBUG_087: a tab that outlived its credential used to answer
// "Settings could not be saved" on every press. The client now gets a fresh
// credential once and repeats the save with the same body; when none is to be
// had it says the session ended, and it never says "unavailable" for a refusal.
func TestTodo_CHATBUG_087(t *testing.T) {
	pref := chatrender.DefaultPreference("en")
	pref.ReadingLanguage, pref.Translate = "de", true
	ctx := context.Background()

	t.Run("a stale credential is refreshed once and the save repeated", func(t *testing.T) {
		backend := &chatbug087Server{current: "fresh"}
		server := httptest.NewServer(backend)
		defer server.Close()
		refreshes := 0
		client := chatbug087Client(server, "stale", func(context.Context) (string, error) { refreshes++; return "fresh", nil })
		if err := client.Save(ctx, "", pref); err != nil {
			t.Fatalf("save with a stale credential: %v", err)
		}
		if refreshes != 1 || backend.puts != 1 || backend.stored.ReadingLanguage != "de" || !backend.stored.Translate {
			t.Fatalf("refreshes=%d puts=%d stored=%+v", refreshes, backend.puts, backend.stored)
		}
		if !strings.Contains(backend.bodies[0], `"reading_language":"de"`) {
			t.Fatalf("the repeated save lost its body: %q", backend.bodies[0])
		}
	})

	t.Run("a refresh that finds no session says the session ended", func(t *testing.T) {
		server := httptest.NewServer(&chatbug087Server{current: "fresh"})
		defer server.Close()
		client := chatbug087Client(server, "stale", func(context.Context) (string, error) { return "", ErrReadingSignedOut })
		if err := client.Save(ctx, "", pref); !errors.Is(err, ErrReadingSignedOut) {
			t.Fatalf("save = %v, want the signed-out error", err)
		}
		if _, err := client.Load(ctx, ""); !errors.Is(err, ErrReadingSignedOut) {
			t.Fatalf("load = %v, want the signed-out error", err)
		}
		// No refresher at all: still the signed-out error, not "unavailable".
		bare := chatbug087Client(server, "stale", nil)
		if err := bare.Save(ctx, "", pref); !errors.Is(err, ErrReadingSignedOut) || errors.Is(err, chatrender.ErrUnavailable) {
			t.Fatalf("save without a refresher = %v", err)
		}
	})

	t.Run("a refresh that returns the same credential does not loop", func(t *testing.T) {
		backend := &chatbug087Server{current: "fresh"}
		server := httptest.NewServer(backend)
		defer server.Close()
		calls := 0
		client := chatbug087Client(server, "stale", func(context.Context) (string, error) { calls++; return "stale", nil })
		if err := client.Save(ctx, "", pref); !errors.Is(err, ErrReadingSignedOut) || calls != 1 || backend.puts != 0 {
			t.Fatalf("save = %v after %d refreshes and %d puts", err, calls, backend.puts)
		}
	})

	t.Run("a refused read of a role is not a stale credential", func(t *testing.T) {
		backend := &chatbug087Server{current: "fresh", status: map[string]int{http.MethodGet: http.StatusForbidden, http.MethodPut: http.StatusForbidden}}
		server := httptest.NewServer(backend)
		defer server.Close()
		refreshes := 0
		client := chatbug087Client(server, "stale", func(context.Context) (string, error) { refreshes++; return "fresh", nil })
		if _, err := client.Load(ctx, ""); !errors.Is(err, chatrender.ErrDenied) || refreshes != 0 {
			t.Fatalf("a 403 on a read refreshed (%d) or was misnamed: %v", refreshes, err)
		}
		// A 403 on a write may be the browser token of an older page: refresh once.
		if err := client.Save(ctx, "", pref); err != nil || refreshes != 1 || backend.puts != 1 {
			t.Fatalf("a 403 on a write: err=%v refreshes=%d puts=%d", err, refreshes, backend.puts)
		}
	})

	t.Run("a save after another client saved is not refused", func(t *testing.T) {
		backend := &chatbug087Server{current: "token", stored: chatrender.DefaultPreference("en")}
		server := httptest.NewServer(backend)
		defer server.Close()
		first := chatbug087Client(server, "token", nil)
		second := chatbug087Client(server, "token", nil)
		if _, err := first.Load(ctx, ""); err != nil {
			t.Fatal(err)
		}
		other := pref
		other.ReadingLanguage = "fr"
		if err := second.Save(ctx, "", other); err != nil {
			t.Fatal(err)
		}
		if err := first.Save(ctx, "", pref); err != nil {
			t.Fatalf("the first client's later save was refused: %v", err)
		}
		if got, err := second.Load(ctx, ""); err != nil || got.ReadingLanguage != "de" {
			t.Fatalf("the last save did not stand: %+v %v", got, err)
		}
	})

	t.Run("statuses name their cause", func(t *testing.T) {
		for status, want := range map[int]error{401: ErrReadingSignedOut, 403: chatrender.ErrDenied, 400: chatrender.ErrInvalid, 404: chatrender.ErrUnavailable, 503: chatrender.ErrUnavailable} {
			if got := readingStatusError(status); !errors.Is(got, want) {
				t.Errorf("status %d = %v, want %v", status, got, want)
			}
		}
		keys := map[string]error{"error_signed_out": ErrReadingSignedOut, "error_denied": chatrender.ErrDenied, "error": chatrender.ErrInvalid, "error_unreachable": chatrender.ErrUnavailable}
		for key, err := range keys {
			if got := readingFailureKey(err); got != key {
				t.Errorf("failure key for %v = %q, want %q", err, got, key)
			}
			if text := RenderingText("en-US", key); text == "" || strings.Contains(text, "⟦") {
				t.Errorf("key %q prints %q", key, text)
			}
		}
	})
}

// TestTodo_CHATBUG_087_Session: the page shell's island is read for a fresh
// bearer and written back with every other field kept.
func TestTodo_CHATBUG_087_Session(t *testing.T) {
	page := `<!doctype html><html><body><div id="app"></div><script type="application/json" id="journey-config">{"tunnel_url":"/t","bearer":"fresh-token","roles":["a"]}</script></body></html>`
	if got, ok := ParseJourneyBearer(page); !ok || got != "fresh-token" {
		t.Fatalf("bearer = %q %v", got, ok)
	}
	for _, bad := range []string{"", "<html></html>", `<script id="journey-config">{"bearer":""}</script>`, `<script id="journey-config">{"bearer":"x"`, `<script id="journey-config">not json</script>`} {
		if got, ok := ParseJourneyBearer(bad); ok {
			t.Errorf("%q gave %q", bad, got)
		}
	}
	updated, err := withIslandBearer(`{"tunnel_url":"/t","bearer":"old","roles":["a"]}`, `new"</script>`)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(updated), &fields); err != nil || string(fields["tunnel_url"]) != `"/t"` || string(fields["roles"]) != `["a"]` {
		t.Fatalf("other fields lost: %s", updated)
	}
	var bearer string
	if json.Unmarshal(fields["bearer"], &bearer) != nil || bearer != `new"</script>` {
		t.Fatalf("bearer = %s", fields["bearer"])
	}
	if _, err := withIslandBearer("not json", "x"); err == nil {
		t.Fatal("a broken island was rewritten")
	}
	if got, err := chatSessionRefresh(context.Background()); got != "" || !errors.Is(err, ErrReadingSignedOut) {
		t.Fatalf("outside the browser there is nothing to refresh: %q %v", got, err)
	}
}
