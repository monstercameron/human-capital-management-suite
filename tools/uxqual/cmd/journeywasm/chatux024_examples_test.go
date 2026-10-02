package main

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// The example questions of an agent's definition reach the page's model with
// the rest of the agent, as the served directory encodes them (the directory
// with icons wraps the profile; the examples must survive that wrapping).
func TestTodo_CHATUX_024_Browser(t *testing.T) {
	examples := []string{"How many days of leave carry over?", "Which holidays are left this year?", "Who approves my expenses?"}
	served := transport.AgentIconDirectory{Personas: []transport.AgentIconMentionProfile{
		{Profile: personachat.Profile{Reference: personachat.Reference{Kind: "AGENT_MENTION", TenantID: "tenant", ID: "assistant", Display: "Assistant", ConversationID: "dm"}, Purpose: "Answers company questions.", Examples: examples}},
		{Profile: personachat.Profile{Reference: personachat.Reference{Kind: "AGENT_MENTION", TenantID: "tenant", ID: "policy", Display: "Policy Helper", ConversationID: "dm"}, Purpose: "Answers policy questions."}},
	}}
	wire, err := json.Marshal(served)
	if err != nil {
		t.Fatal(err)
	}
	var payload personaChatDirectory
	if err = json.Unmarshal(wire, &payload); err != nil {
		t.Fatal(err)
	}
	profiles := personaChatProfiles(payload.Personas, journeyclient.Config{Tenant: "tenant", Subject: "alice"}, "dm")
	if len(profiles) != 2 || !reflect.DeepEqual(profiles[0].Examples, examples) || len(profiles[1].Examples) != 0 {
		t.Fatalf("examples on the page = %+v", profiles)
	}
}
