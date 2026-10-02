package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// TestTodo_CHATBUG_014_AgentSnapshotOnDemand: a document that deferred the
// agent snapshot costs no read until the Agents page opens; that page then
// reads it once, with the session's credential, and both holders of the agents
// (the page's configuration and the projection views are built with) see it.
func TestTodo_CHATBUG_014_AgentSnapshotOnDemand(t *testing.T) {
	var reads atomic.Int32
	var failing atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		if r.URL.Path != chatperf2AgentSnapshotPath || r.Header.Get("Authorization") != "Bearer session-token" {
			t.Errorf("snapshot read went to %s with authorization %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		if failing.Load() {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"start_available":true,"enabled":true,"viewer_is_admin":true,"service":"available",` +
			`"agents":[{"id":"coach","name":"People Coach","description":"Helps with people questions"}],` +
			`"tasks":[{"id":"task-1","version":3,"title":"Holiday guide","state":"completed","answer_text":"Done","actions":{"confirm_plan":false,"pause":false,"resume":false,"cancel":false}}]}`))
	}))
	defer server.Close()
	fetch := func(ctx context.Context) (journeyclient.Agents, error) {
		return chatperf2FetchAgentSnapshot(ctx, server.Client(), server.URL+"/", "session-token")
	}

	island := &journeyclient.Agents{Enabled: true, ViewerIsAdmin: true, Deferred: true}
	session := projectAgents(island)
	if !session.Enabled || session.Snapshot.Availability != productui.AgentsUnavailable || len(session.Snapshot.Agents) != 0 {
		t.Fatalf("deferred projection = %+v", session)
	}

	// Chat, and every page that does not draw the snapshot, reads nothing.
	for _, page := range []productui.PageID{productui.PageChat, productui.PageHome, productui.PageChatSettings} {
		chatperf2LoadAgentSnapshot(context.Background(), page, island, session, fetch)
	}
	if reads.Load() != 0 || !island.Deferred {
		t.Fatalf("%d snapshot reads before the Agents page opened", reads.Load())
	}

	// A failed read leaves the snapshot deferred, so opening the page again
	// (or its retry) reads again.
	failing.Store(true)
	chatperf2LoadAgentSnapshot(context.Background(), productui.PageAgents, island, session, fetch)
	if reads.Load() != 1 || !island.Deferred || len(session.Snapshot.Agents) != 0 {
		t.Fatalf("after a failed read: %d reads, deferred %v, %d agents", reads.Load(), island.Deferred, len(session.Snapshot.Agents))
	}
	failing.Store(false)

	chatperf2LoadAgentSnapshot(context.Background(), productui.PageAgents, island, session, fetch)
	if reads.Load() != 2 {
		t.Fatalf("the Agents page made %d reads, want one more", reads.Load())
	}
	if island.Deferred || !island.StartAvailable || island.Service != "available" || len(island.Agents) != 1 || len(island.Tasks) != 1 {
		t.Fatalf("configuration after the read = %+v", island)
	}
	if !session.Enabled || !session.ViewerIsAdmin || session.Snapshot.Availability != productui.AgentsAvailable || !session.Snapshot.StartAvailable {
		t.Fatalf("projection after the read = %+v", session)
	}
	if len(session.Snapshot.Agents) != 1 || session.Snapshot.Agents[0].Name != "People Coach" || len(session.Snapshot.Tasks) != 1 || session.Snapshot.Tasks[0].AnswerText != "Done" {
		t.Fatalf("projection snapshot = %+v", session.Snapshot)
	}
	// The task list's first paint is seeded from the same read.
	initialAgentTasks.RLock()
	seeded := len(initialAgentTasks.tasks)
	initialAgentTasks.RUnlock()
	if seeded != 1 {
		t.Fatalf("%d tasks seeded for the first paint, want 1", seeded)
	}

	// Opening the page again reads nothing more.
	chatperf2LoadAgentSnapshot(context.Background(), productui.PageAgents, island, session, fetch)
	if reads.Load() != 2 {
		t.Fatalf("the Agents page read the snapshot again (%d reads)", reads.Load())
	}

	// A document that carried the snapshot (the Agents page's own) never reads.
	carried := &journeyclient.Agents{Enabled: true, Service: "available"}
	chatperf2LoadAgentSnapshot(context.Background(), productui.PageAgents, carried, projectAgents(carried), fetch)
	chatperf2LoadAgentSnapshot(context.Background(), productui.PageAgents, nil, nil, fetch)
	if reads.Load() != 2 {
		t.Fatalf("a snapshot that was not deferred was read (%d reads)", reads.Load())
	}
	if _, err := chatperf2FetchAgentSnapshot(context.Background(), nil, server.URL, "session-token"); err == nil {
		t.Fatal("a read with no transport succeeded")
	}
}
