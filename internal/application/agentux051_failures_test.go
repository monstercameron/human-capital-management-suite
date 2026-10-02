package application

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// AGENTUX-051, the server half of the failure clause. A run ends with a code;
// the card says one plain sentence for it and, only where asking again cannot
// help, what would. Every code a run in this package can end with is injected
// here and the sentence, the retry rule and the absence of any internal word
// are asserted.
func TestTodo_AGENTUX_051_Fault(t *testing.T) {
	agent := "Policy Helper"
	for _, test := range []struct {
		code      string
		class     string
		retryable bool
		contains  string
	}{
		{"MODEL_UNAVAILABLE", "service", true, "could not answer because the service had a problem"},
		{"MODEL_REFUSED_OR_INCOMPLETE", "service", true, "could not answer because the service had a problem"},
		{"DELIVERY_FAILED", "service", true, "could not answer because the service had a problem"},
		{"MODEL_TIMEOUT", "timeout", true, "took too long"},
		{"ANSWER_INTERRUPTED", "interrupted", true, "was interrupted"},
		{"NO_RESULTS", "nothing", false, "found nothing about this in the documents it can read here"},
		{"CONTEXT_UNAVAILABLE", "permission", false, "cannot answer this request here"},
		{"TOOL_EXECUTION_FAILED", "unavailable", false, "is not available in this conversation right now"},
		{"OUTPUT_REJECTED", "rejected", false, "did not pass its checks"},
		{chat.AgentAnswerTitleOnlyCode, "title_only", false, "answered with only the name of a document"},
		{"LIMIT_REACHED", "limit", false, "has reached the limit"},
		{"CANCELLED", "stopped", false, "was stopped"},
		{"MODEL_NOT_CONFIGURED", "unavailable", false, "is not available in this conversation right now"},
		{"A_CODE_NOBODY_WROTE_DOWN", "unavailable", false, "is not available in this conversation right now"},
	} {
		english := chat.AgentAnswerFailureFor("en-US", agent, test.code)
		if english.Class != test.class || english.Retryable != test.retryable || !strings.Contains(english.Sentence, test.contains) || !strings.HasPrefix(english.Sentence, agent) && !strings.Contains(english.Sentence, agent+"'s") {
			t.Errorf("%s = %+v, want class %s, retryable %v, containing %q", test.code, english, test.class, test.retryable, test.contains)
		}
		// Where asking again cannot help the card says what would; where it can it says nothing more.
		if english.Retryable && english.NextStep != "Try again." {
			t.Errorf("%s: a retryable failure carries a next step of its own: %+v", test.code, english)
		}
		if !english.Retryable && strings.TrimSpace(english.NextStep) == "" {
			t.Errorf("%s: a failure that retrying cannot mend gives no next step", test.code)
		}
		for _, text := range []string{english.Sentence, english.NextStep} {
			for _, internal := range []string{test.code, "{agent}", "persona", "Persona", "invocation", "provider", "503", ".go"} {
				if strings.Contains(text, internal) {
					t.Errorf("%s: the card text %q contains %q", test.code, text, internal)
				}
			}
		}
		for _, locale := range []string{"de-DE", "ar"} {
			localized := chat.AgentAnswerFailureFor(locale, agent, test.code)
			if localized.Class != english.Class || localized.Retryable != english.Retryable || localized.Sentence == english.Sentence || strings.Contains(localized.Sentence, "{agent}") || !strings.Contains(localized.Sentence, agent) {
				t.Errorf("%s/%s = %+v against %+v", test.code, locale, localized, english)
			}
		}
	}
}

// The stored outcome of a run is the code the card draws a sentence from: the
// causes the surface classifies, and a search that found nothing, each reach
// their own sentence; nothing the person reads is a code.
func TestTodo_AGENTUX_051_Integration(t *testing.T) {
	// A search that returned no document ends the run with the code for "found nothing".
	if got := personaQualitySearchFailure([]byte(`{"Hits":[]}`)); got != "NO_RESULTS" {
		t.Fatalf("an empty search ends the run with %q, want NO_RESULTS", got)
	}
	if got := personaQualitySearchFailure([]byte(`{"Hits":[],"Unavailable":"index"}`)); got != "CONTEXT_UNAVAILABLE" {
		t.Fatalf("a search that could not run ends the run with %q, want CONTEXT_UNAVAILABLE", got)
	}
	if got := personaQualitySearchFailure([]byte(`{"Hits":[{"DocumentID":"pto","Title":"Paid time off policy"}]}`)); got != "" {
		t.Fatalf("a search with a hit ends the run with %q", got)
	}
	for _, test := range []struct {
		cause     error
		code      string
		class     string
		retryable bool
	}{
		{ErrPersonaRunModelFailure, "MODEL_UNAVAILABLE", "service", true},
		{ErrPersonaRunOutputRejected, "OUTPUT_REJECTED", "rejected", false},
		{ErrPersonaRunDeliveryFailure, "DELIVERY_FAILED", "service", false},
		{agentinvoke.ErrDenied, "ADMISSION_REFUSED", "permission", false},
		{ErrPersonaRunExecutorUnavailable, "EXECUTION_UNAVAILABLE", "unavailable", true},
		{ErrAgentDirectoryUnavailable, "ADMISSION_UNAVAILABLE", "unavailable", true},
		{errors.New("a database detail nobody should read"), "INVOCATION_FAILED", "unavailable", false},
	} {
		code, retryable := personaPostFailureClassification(test.cause)
		if code != test.code || retryable != test.retryable {
			t.Errorf("%v classified as %s/%v, want %s/%v", test.cause, code, retryable, test.code, test.retryable)
		}
		card := chat.AgentAnswerFailureFor("en-US", "Policy Helper", code)
		if card.Class != test.class || strings.Contains(card.Sentence, "database detail") || strings.Contains(card.Sentence, code) {
			t.Errorf("%s reaches the card as %+v", code, card)
		}
	}
}
