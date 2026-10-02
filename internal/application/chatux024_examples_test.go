package application

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
)

// The agents of a conversation are sent with the example questions of their
// published definition, for the empty conversation to offer. An agent whose
// definition holds none sends none.
func TestTodo_CHATUX_024_Integration(t *testing.T) {
	surface, ctx, _, _, _ := personaSurfaceFixture(t)
	directory, err := surface.Directory(ctx, "channel-a")
	if err != nil || len(directory.Personas) != 1 || len(directory.Personas[0].Examples) != 0 {
		t.Fatalf("an agent with no examples: %+v %v", directory, err)
	}
	if encoded, _ := json.Marshal(directory); strings.Contains(string(encoded), `"examples"`) {
		t.Fatalf("an agent with no examples sends the field: %s", encoded)
	}

	personas := surface.Personas.(agentUserCatalogPersonas)
	profile := personas[0].Profile
	profile.ExampleQuestions = []string{"How many days of leave carry over?", "Which holidays are left this year?", "Who approves my expenses?"}
	published, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	surface.Personas = agentUserCatalogPersonas{published, personas[1]}
	directory, err = surface.Directory(ctx, "channel-a")
	if err != nil || len(directory.Personas) != 1 || !reflect.DeepEqual(directory.Personas[0].Examples, profile.ExampleQuestions) {
		t.Fatalf("the agent's examples were not sent: %+v %v", directory, err)
	}
	encoded, _ := json.Marshal(directory)
	if !strings.Contains(string(encoded), `"examples":["How many days of leave carry over?","Which holidays are left this year?","Who approves my expenses?"]`) {
		t.Fatalf("the page does not receive the examples: %s", encoded)
	}
	// What is sent is a copy: the published profile cannot be changed through it.
	directory.Personas[0].Examples[0] = "Changed?"
	if published.Profile.ExampleQuestions[0] != "How many days of leave carry over?" {
		t.Fatal("the directory shares the published profile's examples")
	}
	// The hidden agent's examples are never sent, like the rest of its profile.
	hidden := personas[1].Profile
	hidden.ExampleQuestions = []string{"What is the secret compensation plan?"}
	sealed, err := agentpersona.Seal(hidden)
	if err != nil {
		t.Fatal(err)
	}
	surface.Personas = agentUserCatalogPersonas{published, sealed}
	directory, err = surface.Directory(ctx, "channel-a")
	if encoded, _ = json.Marshal(directory); err != nil || strings.Contains(string(encoded), "secret compensation") {
		t.Fatalf("a hidden agent's example was sent: %s %v", encoded, err)
	}
}
