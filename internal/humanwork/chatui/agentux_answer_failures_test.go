package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func TestAgentUXQuality_FailureTree(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, code := range []string{"ADMISSION_REFUSED", "NOTHING_FOUND", "PERSONA_NOT_INSTALLED", "MODEL_TIMEOUT", "MODEL_LIMIT", "MODEL_UNAVAILABLE", "ANSWER_INTERRUPTED", "STOPPED"} {
			t.Run(locale+"/"+code, func(t *testing.T) {
				projection := PersonaProgressProjection{ViewerID: "reader", InvokerID: "reader", AgentName: "Policy Helper", Failure: &PersonaProgressFailure{InvokerID: "reader", InvocationID: "invoke", Code: code, Retryable: true, Message: "technical secret"}}
				markup := renderPersonaProgressTest(t, Model{Locale: locale}, projection)
				copy := chat.AgentAnswerFailureFor(locale, "Policy Helper", code)
				if !strings.Contains(markup, "Policy Helper") || !strings.Contains(markup, copy.NextStep) || strings.Contains(markup, code) || strings.Contains(markup, "technical secret") || strings.Contains(markup, `data-agent-action="retry"`) != copy.Retryable {
					t.Fatalf("failure copy/retry contract: %s", markup)
				}
			})
		}
	}
}

func TestAgentUXQuality_WorkingDeadline_Fault(t *testing.T) {
	projection := PersonaProgressProjection{ViewerID: "reader", InvokerID: "reader", Progress: &PersonaProgressProps{InvokerID: "reader", ViewerID: "reader", AgentName: "Policy Helper", Visible: true, Deadline: time.Now().Add(-time.Second)}}
	markup := renderPersonaProgressTest(t, Model{Locale: "en-US"}, projection)
	if strings.Contains(markup, `data-agent-reply-state="working"`) || !strings.Contains(markup, "answer was interrupted") || !strings.Contains(markup, `data-agent-action="retry"`) {
		t.Fatalf("expired status left working: %s", markup)
	}
	projection.Progress.Deadline = time.Now().Add(time.Minute)
	projection.Progress.ElapsedSeconds = 5
	markup = renderPersonaProgressTest(t, Model{Locale: "en-US"}, projection)
	if !strings.Contains(markup, ">Stop</button>") || !strings.Contains(markup, "0:05") {
		t.Fatalf("working status lacks stop at five seconds: %s", markup)
	}
}
