package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/ambientagents"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func TestAgentUXAmbient_Client_Security(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer fixture-session" {
			t.Error("session missing")
		}
		if r.URL.Query().Get("conversation") != "general & private" {
			t.Error("conversation changed")
		}
		if r.Method == http.MethodPost && r.URL.Path != ambientagents.Path+"/control" {
			t.Error("wrong action route")
		}
		_ = json.NewEncoder(w).Encode(ambientagents.Snapshot{Cards: []chatui.AgentUXAmbientCard{{ID: "one", Title: "Existing offer"}}})
	}))
	defer server.Close()
	cfg := journeyclient.Config{TunnelURL: server.URL + "/tunnel?old=1", Bearer: "fixture-session"}
	for _, action := range []string{"", "control"} {
		snapshot, err := agentUXAmbientRequest(context.Background(), server.Client(), cfg, "general & private", action, ambientagents.Command{Action: "ADD", ID: "one", ExpectedRevision: 1})
		if err != nil || len(snapshot.Cards) != 1 {
			t.Fatalf("request %+v %v", snapshot, err)
		}
	}
	if _, err := agentUXAmbientRequest(context.Background(), server.Client(), cfg, "general & private", "publish", nil); err == nil || calls != 2 {
		t.Fatal("unsupported effect reached HTTP")
	}
	cfg.Bearer = ""
	if _, err := agentUXAmbientRequest(context.Background(), server.Client(), cfg, "general", "", nil); err == nil {
		t.Fatal("missing session accepted")
	}
	state := agentUXAmbientClientState{Conversation: "general", Snapshot: ambientagents.Snapshot{Cards: []chatui.AgentUXAmbientCard{{ID: "old"}}}}
	state.apply("general", ambientagents.Snapshot{}, errors.New("slow read"))
	if len(state.Snapshot.Cards) != 1 || !state.Failed {
		t.Fatal("failed read hid server text")
	}
	state.apply("other", ambientagents.Snapshot{}, nil)
	if len(state.Snapshot.Cards) != 1 {
		t.Fatal("stale response replaced current conversation")
	}
	state.apply("general", ambientagents.Snapshot{Cards: []chatui.AgentUXAmbientCard{{ID: "new"}}}, nil)
	if state.Failed || state.Snapshot.Cards[0].ID != "new" {
		t.Fatal("explicit retry did not update snapshot")
	}
	first := state.begin("general")
	second := state.begin("general")
	if state.finish("general", first, ambientagents.Snapshot{}, nil) || state.Snapshot.Cards[0].ID != "new" {
		t.Fatal("an older same-conversation response hid newer server text")
	}
	if !state.finish("general", second, ambientagents.Snapshot{}, errors.New("read failed")) || len(state.Snapshot.Cards) != 1 || !state.Failed {
		t.Fatal("current failed response did not preserve server text")
	}
	state.begin("other")
	if len(state.Snapshot.Cards) != 0 || state.Failed || state.finish("general", second, ambientagents.Snapshot{}, nil) {
		t.Fatal("navigation carried cards from another conversation")
	}
}
