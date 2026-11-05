package chat

import (
	"strings"
	"testing"
)

// TestTodo_AGENTUX_082 pins the expired-agent conversation sentence: an agent
// whose model route lapsed tells the asker it is unavailable because its
// setup expired, and names who can fix it (by renewing). A route that is
// unavailable for any other reason keeps the generic sentence.
func TestTodo_AGENTUX_082(t *testing.T) {
	expired := map[string]struct{ sentence, next string }{
		"en-US": {"because its setup expired", "to renew its setup"},
		"de-DE": {"weil die Einrichtung abgelaufen ist", "die Einrichtung zu erneuern"},
		"ar":    {"صلاحية", "تجديد"},
	}
	for locale, want := range expired {
		t.Run(locale, func(t *testing.T) {
			got := AgentAnswerFailureFor(locale, "Policy Helper", "MODEL_ROUTE_EXPIRED")
			if got.Class != "expired" {
				t.Fatalf("class for MODEL_ROUTE_EXPIRED = %q, want expired (%+v)", got.Class, got)
			}
			if !strings.Contains(got.Sentence, "Policy Helper") || !strings.Contains(got.Sentence, want.sentence) {
				t.Fatalf("expired sentence = %q, want the agent and %q", got.Sentence, want.sentence)
			}
			if !strings.Contains(got.NextStep, want.next) {
				t.Fatalf("expired next step = %q, want %q", got.NextStep, want.next)
			}
			if got.Retryable {
				t.Fatalf("expired refusal offers retry: %+v", got)
			}
			if strings.Contains(got.Sentence+got.NextStep, "MODEL_ROUTE_EXPIRED") {
				t.Fatalf("expired copy leaks the code: %+v", got)
			}
			generic := AgentAnswerFailureFor(locale, "Policy Helper", "MODEL_ROUTE_UNAVAILABLE")
			if generic.Class != "unavailable" {
				t.Fatalf("class for MODEL_ROUTE_UNAVAILABLE = %q, want the generic unavailable", generic.Class)
			}
			if strings.Contains(generic.Sentence, want.sentence) {
				t.Fatalf("generic sentence claims expiry: %q", generic.Sentence)
			}
		})
	}
}
