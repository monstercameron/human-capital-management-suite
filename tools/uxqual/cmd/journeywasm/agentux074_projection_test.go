package main

import (
	"encoding/json"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// The Agents page drew a made-up glyph because the stored icon was dropped
// between the server's projection and the page: the island did not carry it and
// the client did not read it. This walks the icon through both.
func TestTodo_AGENTUX_074_ProjectionCarriesIcon(t *testing.T) {
	stored := agentIconFixture(agenticon.Input{Name: "Assistant"})
	island := workspace.AgentsConfig{Enabled: true, Service: "available", StartAvailable: true, Agents: []workspace.AgentSummaryConfig{
		{ID: "assistant", Name: "Assistant", Icon: stored, IconRevision: 3},
		{ID: "policy-helper", Name: "Policy Helper"},
	}}
	raw, err := json.Marshal(island)
	if err != nil {
		t.Fatal(err)
	}
	var received journeyclient.Agents
	if err := json.Unmarshal(raw, &received); err != nil {
		t.Fatal(err)
	}
	projection := projectAgents(&received)
	if projection == nil || len(projection.Snapshot.Agents) != 2 {
		t.Fatalf("projection lost agents: %+v", projection)
	}
	if got := projection.Snapshot.Agents[0]; got.Icon != stored || got.IconRevision != 3 {
		t.Fatalf("the stored icon did not reach the page: %+v, want %+v", got, stored)
	}
	if got := projection.Snapshot.Agents[1]; got.Icon.Valid() {
		t.Fatalf("an agent with no stored icon was given one: %+v", got)
	}
	// The server's own first paint reads the island the same way.
	if server := workspace.ProductAgentsAvailability(&island); len(server.Snapshot.Agents) != 2 || server.Snapshot.Agents[0].Icon != stored {
		t.Fatalf("the server render does not carry the stored icon: %+v", server.Snapshot.Agents)
	}
	// A forged icon outside the closed vocabulary is not adopted.
	received.Agents[0].Icon = agenticon.Value{Glyph: "<script>", Shape: "circle", Foreground: "red", Background: "blue"}
	if forged := projectAgents(&received); forged.Snapshot.Agents[0].Icon.Valid() {
		t.Fatal("an icon outside the product's vocabulary was adopted")
	}
}
