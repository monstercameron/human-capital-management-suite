package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestTodo_CHATMOD_003_ClientLogic(t *testing.T) {
	t.Run("the service address", func(t *testing.T) {
		for in, want := range map[string]string{
			"wss://hcm.example/tunnel?x=1#y": "https://hcm.example",
			"ws://localhost:8290/path":       "http://localhost:8290",
			"https://hcm.example/a/b":        "https://hcm.example",
		} {
			if got := filterBaseURL(in); got != want {
				t.Errorf("filterBaseURL(%q) = %q, want %q", in, got, want)
			}
		}
	})

	t.Run("an error reaches the page only as plain words", func(t *testing.T) {
		wrapped := fmt.Errorf("saving: %w", &FilterAPIError{Code: "permission_denied"})
		for err, want := range map[error]string{
			&FilterAPIError{Code: "version_conflict"}: "er_conflict",
			wrapped: "er_perm",
			&FilterAPIError{Code: "filters_unavailable"}: "er_unavail",
			&FilterAPIError{Code: "invalid_filter"}:      "er_invalid",
			&FilterAPIError{Code: "something_new"}:       "er_network",
			context.Canceled:                             "er_network",
			context.DeadlineExceeded:                     "er_network",
			errors.New("Failed to fetch"):                "er_network",
			&FilterAPIError{}:                            "er_network",
		} {
			if got := chatui.ModErrorKey(filterErrorCode(err)); got != want {
				t.Errorf("%v -> %s, want %s", err, got, want)
			}
		}
	})

	var requests []string
	var bodies []map[string]any
	var failEnable bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			bodies = append(bodies, body)
			if failEnable && strings.HasSuffix(r.URL.Path, "/enable") {
				w.WriteHeader(403)
				_, _ = w.Write([]byte(`{"code":"permission_denied"}`))
				return
			}
			_, _ = w.Write([]byte(`{}`))
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/enablements"):
			_, _ = w.Write([]byte(`[{"RuleID":"builtin-en-slurs","Channel":"room","Enabled":false,"Action":"block"},{"RuleID":"builtin-en-slurs","Channel":"","Enabled":true,"Action":"block"}]`))
		default:
			_, _ = w.Write([]byte(`[{"ID":"builtin-en-slurs","Name":"slurs","Version":"1.0.0","Language":"en","Product":true}]`))
		}
	}))
	defer server.Close()
	client := FilterAPIClient{BaseURL: server.URL}
	ctx := t.Context()

	t.Run("one reading carries the definitions and both levels' rows", func(t *testing.T) {
		snapshot, err := client.Snapshot(ctx, "room")
		if err != nil || len(snapshot.Definitions) != 1 || len(snapshot.Enablements) != 2 {
			t.Fatalf("snapshot %+v %v", snapshot, err)
		}
		// whichever order the rows come in, the channel's own row wins
		state := chatui.ModResolve(snapshot.Definitions[0], snapshot.Enablements, "room", time.Now())
		if state.On || !state.HasOverride || !state.WorkspaceOn {
			t.Fatalf("resolved %+v", state)
		}
		if got := requests[len(requests)-2:]; got[0] != "GET /api/chat/filters/v1?channel=room" || got[1] != "GET /api/chat/filters/v1/enablements?channel=room" {
			t.Fatalf("requests %v", got)
		}
	})

	t.Run("a failed read gives nothing half-read", func(t *testing.T) {
		broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/enablements") {
				w.WriteHeader(503)
				_, _ = w.Write([]byte(`{"code":"filters_unavailable"}`))
				return
			}
			_, _ = w.Write([]byte(`[{"ID":"a"}]`))
		}))
		defer broken.Close()
		snapshot, err := FilterAPIClient{BaseURL: broken.URL}.Snapshot(ctx, "room")
		if err == nil || len(snapshot.Definitions) != 0 || filterErrorCode(err) != "filters_unavailable" {
			t.Fatalf("snapshot %+v %v", snapshot, err)
		}
	})

	t.Run("a switch is written at the level the panel chose", func(t *testing.T) {
		bodies = nil
		requests = nil
		if err := client.Apply(ctx, chatui.ModSwitch{RuleID: "builtin-en-slurs", Channel: "room", On: false, Action: "mask"}); err != nil {
			t.Fatal(err)
		}
		if err := client.Apply(ctx, chatui.ModSwitch{RuleID: "builtin-en-slurs", Channel: "", On: true, Action: "flag"}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(requests[0], "POST /api/chat/filters/v1/disable") || !strings.Contains(requests[1], "POST /api/chat/filters/v1/enable") {
			t.Fatalf("requests %v", requests)
		}
		first, _ := bodies[0]["Enablement"].(map[string]any)
		second, _ := bodies[1]["Enablement"].(map[string]any)
		if first["Channel"] != "room" || first["Action"] != "mask" || first["RuleID"] != "builtin-en-slurs" || second["Channel"] != "" || second["Action"] != "flag" {
			t.Fatalf("bodies %v", bodies)
		}
	})

	t.Run("saving creates the version and switches it on where it applies", func(t *testing.T) {
		bodies, requests = nil, nil
		created, err := client.SaveFilter(ctx, chatfilter.Definition{ID: "p-1", Name: "P", Version: "1.0.0", Kind: "words", Action: "block", Match: []string{"x"}, Channels: []string{"room"}}, true)
		if err != nil || !created || !strings.Contains(requests[0], "/versions") || !strings.Contains(requests[1], "/enable") {
			t.Fatalf("save %v %v %v", created, err, requests)
		}
		enablement, _ := bodies[1]["Enablement"].(map[string]any)
		if enablement["Channel"] != "room" || bodies[1]["DryRun"] != true || enablement["RuleID"] != "p-1" {
			t.Fatalf("enable body %v", bodies[1])
		}
		bodies, requests = nil, nil
		if created, err = client.SaveFilter(ctx, chatfilter.Definition{ID: "w-1", Name: "W", Version: "1.0.0", Kind: "words", Action: "block", Match: []string{"x"}}, false); err != nil || !created {
			t.Fatalf("workspace save %v %v", created, err)
		}
		if enablement, _ = bodies[1]["Enablement"].(map[string]any); enablement["Channel"] != "" || bodies[1]["DryRun"] != false {
			t.Fatalf("a workspace filter was switched on in a channel: %v", bodies[1])
		}
	})

	t.Run("a filter saved but not switched on says so", func(t *testing.T) {
		failEnable = true
		defer func() { failEnable = false }()
		created, err := client.SaveFilter(ctx, chatfilter.Definition{ID: "q-1", Name: "Q", Version: "1.0.0", Kind: "words", Action: "block", Match: []string{"x"}}, false)
		if !created || err == nil || filterErrorCode(err) != "permission_denied" {
			t.Fatalf("created=%v err=%v", created, err)
		}
	})

	t.Run("a refused version is not created", func(t *testing.T) {
		refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(409)
			_, _ = w.Write([]byte(`{"code":"version_conflict"}`))
		}))
		defer refusing.Close()
		created, err := FilterAPIClient{BaseURL: refusing.URL}.SaveFilter(ctx, chatfilter.Definition{ID: "q-1"}, false)
		if created || filterErrorCode(err) != "version_conflict" {
			t.Fatalf("created=%v err=%v", created, err)
		}
	})
}
