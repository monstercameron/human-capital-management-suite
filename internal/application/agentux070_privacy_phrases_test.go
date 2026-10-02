package application

import (
	"context"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

// TestTodo_AGENTUX_070_PrivacyPhrases asks, in each language, for the phrase
// the product owner named and its translations: every phrase in the table is
// found inside a real question, found again with different case and spacing,
// and removed from the text that goes to the model.
func TestTodo_AGENTUX_070_PrivacyPhrases(t *testing.T) {
	questions := map[string][]string{
		"en": {
			"@Policy Helper how many PTO days carry over? Keep this private.",
			"Tell me, in private: how many PTO days carry over?",
			"How many PTO days carry over? Please answer privately",
			"how many PTO days carry over, just for me",
			"Only me - how many PTO days carry over?",
		},
		"de": {
			"@Policy Helper Wie viele Urlaubstage verfallen nicht? Bitte privat halten.",
			"Wie viele Urlaubstage verfallen nicht? Nur für mich.",
			"Antworte privat: wie viele Urlaubstage verfallen nicht?",
			"Wie viele Urlaubstage verfallen nicht, unter vier Augen",
		},
		"ar": {
			"كم يوما يمكن ترحيله من الإجازة؟ اجعل هذا خاصا",
			"كم يوما يمكن ترحيله من الإجازة؟ بشكل خاص من فضلك",
			"كم يوما يمكن ترحيله من الإجازة؟ لي وحدي",
			"اجعلها خاصة: كم يوماً يمكن ترحيله من الإجازة؟",
			"أجب بِشَكلٍ خاصّ عن عدد الأيام",
		},
	}
	for language, list := range questions {
		for _, question := range list {
			if !AskedForPrivacy(question) {
				t.Errorf("%s: privacy request not found in %q", language, question)
			}
			if !AskedForPrivacy(strings.ToUpper(question)) {
				t.Errorf("%s: privacy request not found in upper case %q", language, question)
			}
			stripped := WithoutPrivacyRequest(question)
			if AskedForPrivacy(stripped) || stripped == "" || stripped == question {
				t.Errorf("%s: %q was not cleaned: %q", language, question, stripped)
			}
		}
	}
	// Every phrase in the table matches on its own and in a sentence.
	for language, phrases := range agentPrivacyPhrases {
		for _, phrase := range phrases {
			if !AskedForPrivacy(phrase.Words) || !AskedForPrivacy("Hello, "+phrase.Words+". Thanks") {
				t.Errorf("%s: table phrase %q does not match", language, phrase.Words)
			}
		}
	}
	// The question that remains is the question that was asked.
	for question, want := range map[string]string{
		"How many PTO days carry over? Keep this private.":                 "How many PTO days carry over?",
		"Tell me, in private: how many PTO days carry over?":               "Tell me: how many PTO days carry over?",
		"Wie viele Urlaubstage verfallen nicht? Bitte privat halten.":      "Wie viele Urlaubstage verfallen nicht?",
		"how many PTO days carry over, just for me":                        "how many PTO days carry over",
		"Keep this private. How many PTO days carry over? Only me please.": "How many PTO days carry over? please.",
	} {
		if got := WithoutPrivacyRequest(question); got != want {
			t.Errorf("WithoutPrivacyRequest(%q) = %q, want %q", question, got, want)
		}
	}
	// A question that is only the phrase is left alone: nothing remains to ask.
	if got := WithoutPrivacyRequest("keep this private"); got != "keep this private" {
		t.Errorf("a question of only the phrase became %q", got)
	}
}

// TestTodo_AGENTUX_070_PrivacyPhrasesWordBoundaries keeps the list from
// matching inside other words or in another sense.
func TestTodo_AGENTUX_070_PrivacyPhrasesWordBoundaries(t *testing.T) {
	for _, question := range []string{
		"What is our policy on private equity holdings?",
		"Do we invest in private equity?",
		"Is the privately held subsidiary covered by the policy?",
		"What are the privateers rules for the pirate-day party?",
		"How do I make a private channel?",
		"Does onlymeasure count here?",
		"Wie viele Privatkunden hat die Abteilung?",
		"Was ist das Privat-Konto Limit?",
		"كم يوما من الإجازة الخاصة بالموظفين؟",
		"What does the handbook say about time off?",
		"",
	} {
		if AskedForPrivacy(question) {
			t.Errorf("%q was taken as a privacy request", question)
		}
		if got := WithoutPrivacyRequest(question); got != question {
			t.Errorf("%q was changed to %q", question, got)
		}
	}
}

// TestTodo_AGENTUX_070_ModelIsNotToldWhereTheAnswerGoes reads the thread the
// way a run does and checks what the model is given: the question, never the
// request for privacy, in the invoking message and in the asker's earlier turns;
// the digest references still describe the posts as they were written.
func TestTodo_AGENTUX_070_ModelIsNotToldWhereTheAnswerGoes(t *testing.T) {
	posts := []agentinvoke.ThreadPost{
		{TenantID: "tenant-a", ConversationID: "room-a", ThreadID: "thread-a", ID: "prior", AuthorID: "alice", Body: "Use the handbook context, just for me."},
		{TenantID: "tenant-a", ConversationID: "room-a", ThreadID: "thread-a", ID: "invoke", AuthorID: "alice", Body: "Wie viele Urlaubstage verfallen nicht? Bitte privat halten."},
	}
	source := &DatabasePersonaRunModelWorkSource{threads: personaRunModelThreadFake{posts: posts}}
	admission := agentrun.Record{Request: agentrun.Request{
		Source:   agentrun.SourceIdentity{TenantID: "tenant-a", Ref: "invoke"},
		Audience: agentrun.AudienceScope{ID: "room-a"}, Context: agentrun.ContextScope{ID: "thread-a"},
		Principal: agentrun.PrincipalChain{InvokerID: "alice"},
	}}
	goal, history, refs, err := source.readThreadContext(context.Background(), admission, agentpersona.PersonaProfile{Instructions: "Answer policy questions", InstructionsDigest: "sha256:valid"})
	if err != nil {
		t.Fatal(err)
	}
	if goal != "Wie viele Urlaubstage verfallen nicht?" || len(history) != 1 || history[0] != "Use the handbook context." {
		t.Fatalf("model was given goal=%q history=%q", goal, history)
	}
	if len(refs) != 2 || refs[1].Digest != digestPersonaThreadPost(posts[1]) {
		t.Fatalf("thread references no longer describe the posts as written: %+v", refs)
	}
}
