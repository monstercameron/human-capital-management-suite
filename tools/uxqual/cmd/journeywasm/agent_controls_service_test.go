package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func TestTodo_AGENT_030_ControlsTransport(t *testing.T) {
	var received productui.AgentControlsCommand
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer session" || r.URL.RawQuery != "" {
			t.Error("wrong authentication or caller scope")
		}
		if r.Method == http.MethodPost {
			if r.URL.Path != "/api/agent-controls/control" {
				t.Error("wrong control route")
			}
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Error(err)
			}
		} else if r.URL.Path != "/api/agent-controls" {
			t.Error("wrong read route")
		}
		_ = json.NewEncoder(w).Encode(agentControlsReply{Snapshot: productui.AgentControlsSnapshot{Available: true, Schedules: []productui.AgentControlSchedule{{ID: "weekly", Revision: 8, State: "PAUSED"}}}})
	}))
	defer server.Close()
	cfg := journeyclient.Config{TunnelURL: strings.Replace(server.URL, "http:", "ws:", 1) + "/rpc?tenant=forged", Bearer: "session", Tenant: "not-transmitted", Subject: "not-transmitted"}
	command := productui.AgentControlsCommand{Kind: "schedule", ID: "weekly", ExpectedRevision: 7, Action: "pause", IdempotencyKey: "unique", Reason: "incident"}
	reply, err := agentControlsRequest(context.Background(), server.Client(), cfg, "control", command)
	if err != nil || !reply.Snapshot.Available || reply.Snapshot.Schedules[0].Revision != 8 || received != command {
		t.Fatalf("revisioned roundtrip failed: %v %#v", err, received)
	}
	if _, err := agentControlsRequest(context.Background(), server.Client(), cfg, "", nil); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_AGENT_041_ControlsTransportFault(t *testing.T) {
	for _, status := range []int{http.StatusConflict, http.StatusBadRequest, http.StatusForbidden, http.StatusServiceUnavailable} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
		_, err := agentControlsRequest(context.Background(), server.Client(), journeyclient.Config{TunnelURL: server.URL, Bearer: "session"}, "control", productui.AgentControlsCommand{})
		server.Close()
		want := errAgentControls
		if status == http.StatusForbidden {
			want = errAgentControlsDenied
		}
		if status == http.StatusConflict {
			want = errAgentControlsConflict
		}
		if status == http.StatusBadRequest {
			want = errAgentControlsInvalid
		}
		if !errors.Is(err, want) {
			t.Fatalf("status %d: %v", status, err)
		}
	}
	for _, cfg := range []journeyclient.Config{{TunnelURL: "file:///etc/passwd", Bearer: "session"}, {TunnelURL: "http://example.org"}} {
		if _, err := agentControlsRequest(context.Background(), http.DefaultClient, cfg, "", nil); !errors.Is(err, errAgentControls) {
			t.Fatal("invalid boundary accepted")
		}
	}
}
