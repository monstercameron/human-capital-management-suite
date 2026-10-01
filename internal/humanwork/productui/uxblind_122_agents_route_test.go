package productui

import "testing"

func TestTodo_UXBLIND_122_AgentTaskSelection(t *testing.T) {
	tests := []struct {
		name         string
		enabled      bool
		availability AgentsAvailability
		requested    string
		wantID       string
	}{
		{name: "known task", enabled: true, availability: AgentsAvailable, requested: "task-own", wantID: "task-own"},
		{name: "unknown task", enabled: true, availability: AgentsAvailable, requested: "task-missing"},
		{name: "disabled tenant", enabled: false, availability: AgentsAvailable, requested: "task-own"},
		{name: "unavailable runtime", enabled: true, availability: AgentsUnavailable, requested: "task-own"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			previous := AgentTask{ID: "stale", Title: "Stale detail"}
			view := NewView(PageAgents, "tenant-1", "principal-1", "")
			view.AgentsProjection = &AgentsAvailabilityProjection{
				Enabled: test.enabled,
				Snapshot: AgentSnapshot{
					Availability: test.availability,
					Tasks:        []AgentTask{{ID: "task-own", Title: "Owned task"}},
					SelectedTask: &previous,
				},
			}
			got := ApplyRequest(view, PageRequest{Page: PageAgents, AgentTaskID: test.requested})
			if got.AgentsProjection == nil {
				t.Fatal("Agents projection was dropped")
			}
			selected := got.AgentsProjection.Snapshot.SelectedTask
			if test.wantID == "" {
				if selected != nil {
					t.Fatalf("selected task = %+v, want nil", selected)
				}
				return
			}
			if selected == nil || selected.ID != test.wantID || selected.Title != "Owned task" {
				t.Fatalf("selected task = %+v, want owned task", selected)
			}
			selected.Title = "mutated request copy"
			if got.AgentsProjection.Snapshot.Tasks[0].Title != "Owned task" {
				t.Fatal("selected task aliases the authorized task list")
			}
		})
	}
}

func TestTodo_UXBLIND_122_AgentRouteProfile(t *testing.T) {
	profile, _, ok := PageProfiles(PageAgents)
	if !ok || profile != RouteProfileAgents {
		t.Fatalf("Agents route profile = %q, want %q", profile, RouteProfileAgents)
	}
	keys := profile.QueryKeys()
	if len(keys) != 5 || keys[4] != "task" {
		t.Fatalf("Agents route keys = %v, want shell keys plus task", keys)
	}
	for index, want := range []string{"locale", "nav", "menu_q", "favorites"} {
		if keys[index] != want {
			t.Fatalf("Agents route key %d = %q, want %q", index, keys[index], want)
		}
	}
	if got := profile.CanonicalValues(PageRequest{AgentTaskID: "task-own"}, map[string]bool{"task": true}).Get("task"); got != "task-own" {
		t.Fatalf("canonical task = %q, want task-own", got)
	}
	if got := RouteProfilePeople.CanonicalValues(PageRequest{AgentTaskID: "task-own"}, map[string]bool{"task": true}).Get("task"); got != "" {
		t.Fatalf("People route leaked task selector %q", got)
	}
}
