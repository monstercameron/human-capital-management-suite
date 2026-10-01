package workspace

import "testing"

func TestTodo_UXBLIND_004(t *testing.T) {
	description, ok := DevPersonaDescription("hiring-manager")
	if !ok || description == "" {
		t.Fatal("hiring-manager persona has no promotion-story description")
	}
	if description != "Proposes promotions for workers in the reporting line." {
		t.Fatalf("unexpected hiring-manager description: %q", description)
	}
}

func TestTodo_UXBLIND_004_Browser(t *testing.T) {
	for _, id := range []string{"admin", "hiring-manager", "finance-partner", "individual-contributor"} {
		if description, ok := DevPersonaDescription(id); !ok || description == "" {
			t.Fatalf("persona %q has no visible role description", id)
		}
	}
}

func TestTodo_UXBLIND_004_Security(t *testing.T) {
	if description, ok := DevPersonaDescription("not-a-persona"); ok || description != "" {
		t.Fatalf("unknown persona received a role description: %q", description)
	}
}
