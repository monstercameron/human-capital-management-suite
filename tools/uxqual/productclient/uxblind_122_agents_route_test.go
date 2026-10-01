package productclient

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestTodo_UXBLIND_122_AgentTaskQueryRoundTrip(t *testing.T) {
	state, err := ParseState("/workspace/app/chat/agents", "locale=de-DE&nav=collapsed&task=task-own")
	if err != nil {
		t.Fatal(err)
	}
	if state.Page != productui.PageAgents || state.Request.AgentTaskID != "task-own" || !state.Provided["task"] {
		t.Fatalf("Agents state = %+v, provided=%v", state.Request, state.Provided)
	}
	want := "/workspace/app/chat/agents?locale=de-DE&nav=collapsed&task=task-own"
	if got := CanonicalHref(state); got != want {
		t.Fatalf("canonical Agents href = %q, want %q", got, want)
	}
}

func TestTodo_UXBLIND_122_AgentTaskQueryIsPageScoped(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		query string
		want  string
	}{
		{name: "Chat route", path: "/workspace/app/chat", query: "task=task-own", want: "/workspace/app/chat"},
		{name: "People route", path: "/workspace/app/people", query: "task=task-own", want: "/workspace/app/people"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state, err := ParseState(test.path, test.query)
			if err != nil {
				t.Fatal(err)
			}
			if state.Request.AgentTaskID != "" {
				t.Fatalf("non-Agents route parsed AgentTaskID %q", state.Request.AgentTaskID)
			}
			if got := CanonicalHref(state); got != test.want {
				t.Fatalf("canonical href = %q, want %q", got, test.want)
			}
		})
	}
}
