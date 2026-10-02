package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// CHATBUG-014: a workspace document no longer reads the viewer's agent
// snapshot unless it is the Agents page's own document; that read cost every
// page 0.4 to 0.7 s for something one page draws. The document marks the
// snapshot as deferred instead, and the Agents page reads it here when it is
// opened from another page.

// chatperf2AgentSnapshotPath mirrors workspace.PathAgentSnapshot.
const chatperf2AgentSnapshotPath = "/workspace/persona-admin/viewer-agents"

// chatperf2AgentSnapshotLimit bounds the answer; the snapshot of a viewer with
// fifteen tasks is under 10 KB.
const chatperf2AgentSnapshotLimit = 1 << 20

var errChatperf2AgentSnapshot = errors.New("agent snapshot unavailable")

// chatperf2AgentSnapshotMu makes two openings of the Agents page share one read.
var chatperf2AgentSnapshotMu sync.Mutex

// chatperf2FetchAgentSnapshot reads the viewer's agent snapshot from the
// workspace with the session's own credential.
func chatperf2FetchAgentSnapshot(ctx context.Context, client *http.Client, baseURL, bearer string) (journeyclient.Agents, error) {
	if client == nil {
		return journeyclient.Agents{}, errChatperf2AgentSnapshot
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+chatperf2AgentSnapshotPath, nil)
	if err != nil {
		return journeyclient.Agents{}, err
	}
	request.Header.Set("Authorization", "Bearer "+bearer)
	response, err := client.Do(request)
	if err != nil {
		return journeyclient.Agents{}, errChatperf2AgentSnapshot
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, chatperf2AgentSnapshotLimit+1))
	if err != nil || len(payload) > chatperf2AgentSnapshotLimit || response.StatusCode != http.StatusOK {
		return journeyclient.Agents{}, errChatperf2AgentSnapshot
	}
	var agents journeyclient.Agents
	if json.Unmarshal(payload, &agents) != nil {
		return journeyclient.Agents{}, errChatperf2AgentSnapshot
	}
	return agents, nil
}

// chatperf2LoadAgentSnapshot fills a deferred snapshot before the Agents page
// is built. island is the configuration the page's own code reads the agents
// from, and session is the projection every view is built with; both are
// shared by pointer, so filling them in place reaches every holder.
//
// A read that fails leaves the snapshot deferred. The page then says the
// agents could not be loaded and offers its retry, which runs this again.
func chatperf2LoadAgentSnapshot(ctx context.Context, page productui.PageID, island *journeyclient.Agents, session *productui.AgentsAvailabilityProjection, fetch func(context.Context) (journeyclient.Agents, error)) {
	if island == nil || session == nil || fetch == nil || !chatperf2AgentSnapshotPage(page) {
		return
	}
	chatperf2AgentSnapshotMu.Lock()
	defer chatperf2AgentSnapshotMu.Unlock()
	if !island.Deferred {
		return
	}
	agents, err := fetch(ctx)
	if err != nil {
		return
	}
	agents.Deferred = false
	*island = agents
	*session = *projectAgents(island)
}

// chatperf2AgentSnapshotPage reports whether page draws the agent snapshot.
func chatperf2AgentSnapshotPage(page productui.PageID) bool {
	profile, _, ok := productui.PageProfiles(page)
	return ok && profile == productui.RouteProfileAgents
}
