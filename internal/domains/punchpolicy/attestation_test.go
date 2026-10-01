package punchpolicy

import (
	"errors"
	"fmt"
	"testing"
)

func fixtureQuestionSet() QuestionSet {
	return QuestionSet{
		ID:           "clockout-ca-v1",
		Version:      1,
		Jurisdiction: "US-CA",
		Questions: []Question{
			{ID: "q.break", Kind: QuestionBreakProvided, TextKey: KeyBreakProvided, Required: true},
			{ID: "q.reason", Kind: QuestionMissedBreakReason, TextKey: KeyMissedBreakReason, Required: false},
			{ID: "q.injury", Kind: QuestionInjury, TextKey: KeyInjury, Required: true},
			{ID: "q.custom", Kind: QuestionCustom, TextKey: KeyCustom, Required: false, Options: []string{"A", "B"}},
		},
	}
}

// TestTodo_TCLOCK_010 proves the pure part of the contract: a
// "meal not provided" answer produces the REV-045-01 premium input request,
// an injury answer opens a case task request, and a missing required
// answer fails closed instead of silently skipping the question.
func TestTodo_TCLOCK_010(t *testing.T) {
	set := fixtureQuestionSet()

	consequences, err := Consequences(set, []Answer{
		{QuestionID: "q.break", Value: "false"},
		{QuestionID: "q.injury", Value: "true"},
	})
	if err != nil {
		t.Fatalf("Consequences: %v", err)
	}
	var sawPremium, sawCase bool
	for _, c := range consequences {
		switch c.Kind {
		case ConsequencePremiumInput:
			sawPremium = true
			if c.Premium == nil || c.Premium.RuleRef != "REV-045-01" {
				t.Fatalf("premium consequence must name REV-045-01, got %+v", c.Premium)
			}
		case ConsequenceCaseTask:
			sawCase = true
			if c.CaseTask == nil {
				t.Fatalf("case task consequence must carry a request")
			}
		}
	}
	if !sawPremium {
		t.Fatalf("a 'meal not provided' answer must produce a premium input consequence")
	}
	if !sawCase {
		t.Fatalf("an injury answer must produce a case task consequence")
	}

	// A break provided as true produces no premium consequence.
	compliant, err := Consequences(set, []Answer{
		{QuestionID: "q.break", Value: "true"},
		{QuestionID: "q.injury", Value: "false"},
	})
	if err != nil {
		t.Fatalf("Consequences: %v", err)
	}
	if len(compliant) != 0 {
		t.Fatalf("no exception answers should produce no consequences, got %+v", compliant)
	}

	// A missing required answer fails closed.
	_, err = Consequences(set, []Answer{{QuestionID: "q.break", Value: "false"}})
	if !errors.Is(err, ErrUnansweredRequired) {
		t.Fatalf("expected ErrUnansweredRequired for the missing injury answer, got %v", err)
	}
}

// TestTodo_TCLOCK_010_Golden pins the exact consequence set for one fixed
// question set and answer batch.
func TestTodo_TCLOCK_010_Golden(t *testing.T) {
	set := fixtureQuestionSet()
	consequences, err := Consequences(set, []Answer{
		{QuestionID: "q.break", Value: "false"},
		{QuestionID: "q.reason", Value: "no coverage available"},
		{QuestionID: "q.injury", Value: "true"},
	})
	if err != nil {
		t.Fatalf("Consequences: %v", err)
	}
	var rendered string
	rendered += fmt.Sprintf("question set: %s v%d (%s)\n", set.ID, set.Version, set.Jurisdiction)
	rendered += "consequences:\n"
	for _, c := range consequences {
		rendered += fmt.Sprintf("- question=%s kind=%s\n", c.QuestionID, c.Kind)
		if c.Premium != nil {
			rendered += fmt.Sprintf("    premium: rule_ref=%s reason=%q\n", c.Premium.RuleRef, c.Premium.Reason)
		}
		if c.CaseTask != nil {
			rendered += fmt.Sprintf("    case_task: severity=%s reason=%q\n", c.CaseTask.Severity, c.CaseTask.Reason)
		}
	}
	assertGolden(t, "tclock_010_consequences.txt", rendered)
}

// TestTodo_TCLOCK_010_I18n proves every declared question localization key
// resolves to non-empty text in every supported locale, at the key/lookup
// level. INTEGRATION (rendering the resolved text on a real device
// surface) belongs to the transport/client layer, not this pure package.
func TestTodo_TCLOCK_010_I18n(t *testing.T) {
	keys := []string{KeyBreakProvided, KeyMissedBreakReason, KeyInjury}
	for _, key := range keys {
		for _, locale := range SupportedLocales() {
			text, err := Localize(key, locale)
			if err != nil {
				t.Fatalf("Localize(%q, %q): %v", key, locale, err)
			}
			if text == "" {
				t.Fatalf("Localize(%q, %q) returned empty text", key, locale)
			}
		}
	}

	// KeyCustom is declared but deliberately carries no fixed text: a
	// tenant's custom question text is not this package's data.
	for _, locale := range SupportedLocales() {
		text, err := Localize(KeyCustom, locale)
		if err != nil {
			t.Fatalf("Localize(%q, %q): %v", KeyCustom, locale, err)
		}
		if text != "" {
			t.Fatalf("KeyCustom should resolve to empty text for %q, got %q", locale, text)
		}
	}

	if _, err := Localize("not.a.declared.key", LocaleEnUS); err == nil {
		t.Fatalf("an undeclared key must fail to resolve")
	}
	if _, err := Localize(KeyBreakProvided, "fr-FR"); err == nil {
		t.Fatalf("an unsupported locale must fail to resolve")
	}
}

func TestQuestionSet_Validate(t *testing.T) {
	set := fixtureQuestionSet()
	if err := set.Validate(); err != nil {
		t.Fatalf("fixture set should validate: %v", err)
	}

	noID := set
	noID.ID = ""
	if _, err := Consequences(noID, nil); !errors.Is(err, ErrInvalidQuestionSet) {
		t.Fatalf("empty id should fail with ErrInvalidQuestionSet, got %v", err)
	}

	badKind := set
	badKind.Questions = append([]Question{}, set.Questions...)
	badKind.Questions[0].Kind = "NOT_DECLARED"
	if _, err := Consequences(badKind, nil); !errors.Is(err, ErrInvalidQuestionSet) {
		t.Fatalf("undeclared kind should fail with ErrInvalidQuestionSet, got %v", err)
	}

	dup := set
	dup.Questions = append(dup.Questions, dup.Questions[0])
	if _, err := Consequences(dup, nil); !errors.Is(err, ErrInvalidQuestionSet) {
		t.Fatalf("duplicate question id should fail with ErrInvalidQuestionSet, got %v", err)
	}

	if _, err := Consequences(set, []Answer{{QuestionID: "not-a-question", Value: "x"}}); !errors.Is(err, ErrInvalidQuestionSet) {
		t.Fatalf("an answer to an unknown question should fail with ErrInvalidQuestionSet, got %v", err)
	}
}
