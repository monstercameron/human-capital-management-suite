package journey_test

import (
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
)

// TestTodo_REV_092_01_RPC proves the personal density travels through the
// existing SaveUserPreferences/GetProductPreferences RPCs, is keyed to the
// trusted principal, never writes the organization theme, and is normalized
// before it reaches the store.
func TestTodo_REV_092_01_RPC(t *testing.T) {
	spy := &preferenceSpy{snapshot: preferences.DefaultSnapshot()}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Preferences: spy}))
	ctx := testContext(t)

	saved, err := client.SaveUserPreferences(ctx, &journeyv1.SaveUserPreferencesRequest{User: &journeyv1.UserPreferences{Density: " Compact "}})
	if err != nil {
		t.Fatalf("save density: %v", err)
	}
	if got := saved.GetUser().GetDensity(); got != "compact" {
		t.Fatalf("saved density = %q, want compact", got)
	}
	spy.mu.Lock()
	principal, themeSaves, stored := spy.principal, spy.themeSaves, spy.snapshot.User.Density
	spy.mu.Unlock()
	if principal == "" || themeSaves != 0 || stored != "compact" {
		t.Fatalf("principal=%q themeSaves=%d stored=%q", principal, themeSaves, stored)
	}

	loaded, err := client.GetProductPreferences(ctx, &journeyv1.GetProductPreferencesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.GetUser().GetDensity() != "compact" || loaded.GetTheme().GetDensity() != "comfortable" {
		t.Fatalf("loaded personal=%q organization=%q", loaded.GetUser().GetDensity(), loaded.GetTheme().GetDensity())
	}

	refused, err := client.SaveUserPreferences(ctx, &journeyv1.SaveUserPreferencesRequest{User: &journeyv1.UserPreferences{Version: saved.GetUser().GetVersion(), Density: "tiny"}})
	if err != nil {
		t.Fatalf("save unadmitted density: %v", err)
	}
	if refused.GetUser().GetDensity() != "" {
		t.Fatalf("unadmitted density stored as %q", refused.GetUser().GetDensity())
	}
}
