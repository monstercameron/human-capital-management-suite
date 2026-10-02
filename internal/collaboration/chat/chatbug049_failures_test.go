package chat

import (
	"strings"
	"testing"
)

// An answer the reply checks refused is not an agent that is unavailable. The
// card says the agent answered and that the answer is not shown; a reply that
// was only a document's name says exactly that.
func TestTodo_CHATBUG_049(t *testing.T) {
	unavailable := map[string]string{}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		unavailable[locale] = AgentAnswerFailureFor(locale, "Policy Helper", "PERSONA_SUSPENDED").Sentence
		if unavailable[locale] == "" {
			t.Fatalf("%s: no sentence for an unavailable agent", locale)
		}
	}
	if unavailable["en-US"] != "Policy Helper is not available in this conversation right now." {
		t.Fatalf("the unavailable sentence changed: %q", unavailable["en-US"])
	}

	for _, code := range []string{"OUTPUT_REJECTED", "OUTPUT_BINDING_INVALID", "output_grounding", "output_schema"} {
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			failure := AgentAnswerFailureFor(locale, "Policy Helper", code)
			if failure.Class != "rejected" || failure.Sentence == unavailable[locale] || !strings.Contains(failure.Sentence, "Policy Helper") || failure.NextStep == "" || failure.Retryable {
				t.Fatalf("%s/%s = %+v, want the refused-answer sentence", locale, code, failure)
			}
		}
	}
	rejected := AgentAnswerFailureFor("en-US", "Policy Helper", "OUTPUT_REJECTED")
	if rejected.Sentence != "Policy Helper wrote an answer that did not pass its checks, so it is not shown." || strings.Contains(rejected.Sentence, "not available") {
		t.Fatalf("OUTPUT_REJECTED reads %q", rejected.Sentence)
	}

	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		failure := AgentAnswerFailureFor(locale, "Policy Helper", AgentAnswerTitleOnlyCode)
		if failure.Class != "title_only" || failure.Sentence == unavailable[locale] || failure.Sentence == AgentAnswerFailureFor(locale, "Policy Helper", "OUTPUT_REJECTED").Sentence || !strings.Contains(failure.Sentence, "Policy Helper") || failure.NextStep == "" || failure.Retryable {
			t.Fatalf("%s title-only = %+v", locale, failure)
		}
		if strings.Contains(failure.Sentence+failure.NextStep, AgentAnswerTitleOnlyCode) {
			t.Fatalf("%s shows the code: %+v", locale, failure)
		}
	}
	titleOnly := AgentAnswerFailureFor("en-US", "Policy Helper", AgentAnswerTitleOnlyCode)
	if titleOnly.Sentence != "Policy Helper answered with only the name of a document, so the answer is not shown." || titleOnly.NextStep != "Ask again, or ask what the document says." {
		t.Fatalf("title-only reads %+v", titleOnly)
	}
	// The code is matched whatever its case, like every other code.
	if AgentAnswerFailureFor("en-US", "Policy Helper", strings.ToLower(AgentAnswerTitleOnlyCode)).Class != "title_only" {
		t.Fatal("the title-only code is matched by case")
	}
	// What is still an unavailable agent stays one.
	for _, code := range []string{"PERSONA_NOT_INSTALLED", "MODEL_NOT_CONFIGURED", "EXECUTION_UNAVAILABLE", "something-unknown"} {
		if got := AgentAnswerFailureFor("en-US", "Policy Helper", code); got.Class != "unavailable" {
			t.Fatalf("%s = %+v, want unavailable", code, got)
		}
	}
}
