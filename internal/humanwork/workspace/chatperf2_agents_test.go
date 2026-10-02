package workspace

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func chatperf2Island(t *testing.T, body string) JourneyConfig {
	t.Helper()
	start := strings.Index(body, `id="`+JourneyConfigElementID+`">`)
	if start < 0 {
		t.Fatal("the document has no configuration island")
	}
	island := body[start+len(`id="`+JourneyConfigElementID+`">`):]
	island = island[:strings.Index(island, "</script>")]
	var config JourneyConfig
	if err := json.Unmarshal([]byte(island), &config); err != nil {
		t.Fatalf("island: %v", err)
	}
	return config
}

// TestTodo_CHATBUG_014_AgentSnapshotDeferred: only the Agents page's document
// reads the agent snapshot. Every other page states the tenant setting, marks
// the snapshot as deferred, and leaves it to the client, which reads it from
// its own address.
func TestTodo_CHATBUG_014_AgentSnapshotDeferred(t *testing.T) {
	h, token := newShellHandler(t, false)
	client := &ownerAgentClient{}
	h.agentSettings = &memoryAgentSettings{enabled: map[values.TenantId]bool{shellTenant: true}}
	h.agents = client

	for _, path := range []string{PathProductHome, productui.Path(productui.PageChat)} {
		response := chatperf2Get(t, h, token, path, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", path, response.Code)
		}
		if len(client.requests) != 0 {
			t.Fatalf("the document for %s read the agent snapshot %d times; only the Agents page draws it", path, len(client.requests))
		}
		agents := chatperf2Island(t, response.Body.String()).Agents
		if agents == nil || !agents.Enabled || !agents.Deferred || agents.Service != "" || len(agents.Agents) != 0 || len(agents.Tasks) != 0 {
			t.Fatalf("%s island agents = %+v, want enabled and deferred with no snapshot", path, agents)
		}
		if !strings.Contains(response.Header().Get("Server-Timing"), "agents;dur=") {
			t.Fatalf("Server-Timing %q lost the agents step", response.Header().Get("Server-Timing"))
		}
	}

	// The Agents page still gets the snapshot with its document.
	agentsPath := productui.Path(productui.PageAgents)
	response := chatperf2Get(t, h, token, agentsPath, nil)
	if response.Code != http.StatusOK || len(client.requests) != 1 {
		t.Fatalf("GET %s = %d after %d snapshot reads, want 200 after one", agentsPath, response.Code, len(client.requests))
	}
	onPage := chatperf2Island(t, response.Body.String()).Agents
	if onPage == nil || onPage.Deferred || onPage.Service != agentServiceAvailable || len(onPage.Agents) != 1 || len(onPage.Tasks) != 1 {
		t.Fatalf("Agents page island agents = %+v", onPage)
	}

	// The client's read returns exactly what the Agents document carries.
	read := chatperf2Get(t, h, token, PathAgentSnapshot, nil)
	if read.Code != http.StatusOK || read.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(read.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("GET %s = %d, cache %q, type %q", PathAgentSnapshot, read.Code, read.Header().Get("Cache-Control"), read.Header().Get("Content-Type"))
	}
	var fetched AgentsConfig
	if err := json.Unmarshal(read.Body.Bytes(), &fetched); err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(onPage)
	got, _ := json.Marshal(fetched)
	if string(got) != string(want) || len(client.requests) != 2 {
		t.Fatalf("snapshot read = %s, want %s (after %d reads)", got, want, len(client.requests))
	}
	for _, request := range client.requests {
		if request.TenantID != string(shellTenant) || request.Principal != shellSubject {
			t.Fatalf("the snapshot was read for %+v, not for the admitted viewer", request)
		}
	}
	// The page policy already admits the address.
	if policy := response.Header().Get("Content-Security-Policy"); !strings.Contains(chatperf2Directive(policy, "connect-src"), "example.com"+PathPersonaAdminData+"/") || !strings.HasPrefix(PathAgentSnapshot, PathPersonaAdminData+"/") {
		t.Fatalf("connect-src does not admit %s: %s", PathAgentSnapshot, policy)
	}
	if anonymous := chatperf2Get(t, h, "", PathAgentSnapshot, nil); anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("an unauthenticated snapshot read = %d, want 401", anonymous.Code)
	}

	// A tenant with agents off defers nothing: there is nothing to read.
	h.agentSettings = &memoryAgentSettings{}
	off := chatperf2Island(t, chatperf2Get(t, h, token, PathProductHome, nil).Body.String()).Agents
	if off == nil || off.Enabled || off.Deferred {
		t.Fatalf("agents-off island = %+v", off)
	}
	// Neither does a workspace with no agent service.
	h.agentSettings = &memoryAgentSettings{enabled: map[values.TenantId]bool{shellTenant: true}}
	h.agents = nil
	none := chatperf2Island(t, chatperf2Get(t, h, token, PathProductHome, nil).Body.String()).Agents
	if none == nil || !none.Enabled || none.Deferred || none.Service != agentServiceUnavailable {
		t.Fatalf("no-service island = %+v", none)
	}
}
