package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// A waiting card that has outlived its deadline is an interrupted answer: it
// says so, it no longer counts, and a card the client drew on its own offers no
// Try again, because there is no invocation behind it to ask again about.
func TestTodo_AGENTRUN_004_Card(t *testing.T) {
	render := func(progress PersonaProgressProps) string {
		t.Helper()
		projection := PersonaProgressProjection{ViewerID: "reader", InvokerID: "reader", AgentName: "Policy Helper", Progress: &progress}
		markup, err := ui.RenderToString(chat5ProgressFrame(Model{Locale: "en-US"}, projection))
		if err != nil {
			t.Fatal(err)
		}
		return markup
	}
	base := PersonaProgressProps{InvokerID: "reader", ViewerID: "reader", AgentName: "Policy Helper", Visible: true, ElapsedSeconds: 42}

	working := base
	working.Deadline = time.Now().Add(time.Minute)
	if markup := render(working); !strings.Contains(markup, `data-agent-reply-state="working"`) || !strings.Contains(markup, "agent-elapsed-seconds") {
		t.Fatalf("a card inside its deadline is not counting: %s", markup)
	}

	provisional := base
	provisional.Deadline, provisional.Provisional = time.Now().Add(-time.Second), true
	markup := render(provisional)
	if strings.Contains(markup, `data-agent-reply-state="working"`) || strings.Contains(markup, "agent-elapsed-seconds") || strings.Contains(markup, "agent-reply-counter") || strings.Contains(markup, `data-agent-action="retry"`) || !strings.Contains(markup, "interrupted") {
		t.Fatalf("a provisional card past its deadline still counts or offers a retry that cannot work: %s", markup)
	}

	real := base
	real.InvocationID, real.Deadline = "invocation-1", time.Now().Add(-time.Second)
	markup = render(real)
	if strings.Contains(markup, "agent-elapsed-seconds") || strings.Contains(markup, "agent-reply-counter") || !strings.Contains(markup, `data-agent-action="retry"`) || !strings.Contains(markup, "interrupted") {
		t.Fatalf("an expired card with an invocation must end and offer Try again: %s", markup)
	}
}
