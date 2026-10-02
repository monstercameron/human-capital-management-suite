package productui

import (
	"strings"
	"testing"
)

// The badge, the "Version N is ..." sentence and the stepper agree at every
// step between draft and published, and the stepper names the step in progress.
func TestTodo_AGENTUX_046_StatusPlacesAgree(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	steps := []struct {
		name     string
		edit     func(*PersonaAdminPersona)
		step     string
		badge    string
		sentence string
	}{
		{"draft", func(p *PersonaAdminPersona) { p.Lifecycle = PersonaDraft }, "Step 1 of 4: Draft", "Version 6 · Draft", "Version 6 is a draft."},
		{"review requested", func(p *PersonaAdminPersona) { p.Lifecycle = PersonaInReview }, "Step 2 of 4: Waiting for review", "Version 6 · Waiting for review", "Version 6 is waiting for review."},
		{"review approved", func(p *PersonaAdminPersona) { p.Lifecycle, p.ReviewApproved = PersonaInReview, true }, "Step 3 of 4: Ready to evaluate", "Version 6 · Ready to evaluate", "Version 6 is waiting for evaluation."},
		{"evaluation running", func(p *PersonaAdminPersona) {
			p.Lifecycle, p.ReviewApproved, p.EvaluationStatus = PersonaInReview, true, "RUNNING"
		}, "Step 3 of 4: Evaluation running", "Version 6 · Evaluation running", "Version 6 is being evaluated."},
		{"evaluation passed", func(p *PersonaAdminPersona) {
			p.Lifecycle, p.ReviewApproved, p.EvaluationRef, p.EvaluationStatus, p.EvaluationPassed = PersonaInReview, true, "evaluation-6", "PASSED", 8
		}, "Step 4 of 4: Ready to publish", "Version 6 · Ready to publish", "Version 6 is ready to publish."},
		{"published", func(p *PersonaAdminPersona) {
			p.Lifecycle, p.ReviewApproved, p.EvaluationRef = PersonaPublished, true, "evaluation-6"
		}, "Step 4 of 4: Published", "Version 6 · Published", "Version 6 is published."},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			persona := agentUXSetup2Persona()
			persona.Version = "6"
			step.edit(&persona)
			card := personaAdminRender(t, personaAdminCard(locale, &agentDocInstr2ReviewClient{}, persona, agentUXSetup2Snapshot(persona)))
			for place, want := range map[string]string{"stepper": step.step, "badge": step.badge, "sentence": step.sentence} {
				if !strings.Contains(card, want) {
					t.Errorf("the %s does not say %q: %s", place, want, agentUX055Text(card))
				}
			}
			// The retired labels named the last step finished, which read as if
			// the step in progress had already happened.
			for _, stale := range []string{"Step 2 of 4: Reviewed", "Step 3 of 4: Evaluated"} {
				if strings.Contains(card, stale) {
					t.Errorf("the stepper still names the last step finished: %q", stale)
				}
			}
			if strings.Count(card, `aria-current="step"`) > 1 {
				t.Error("more than one step is marked current")
			}
		})
	}

	// The requester sees no approve button, only who they are waiting for and a
	// way to message them.
	persona := agentUXSetup2Persona()
	persona.Version = "6"
	requester := agentUXSetup2Snapshot(persona)
	requester.AllowedCommands = []string{"CREATE_VERSION", "REQUEST_REVIEW"}
	waiting := personaAdminRender(t, personaAdminCard(locale, &agentDocInstr2ReviewClient{}, persona, requester))
	if strings.Contains(waiting, `data-persona-review-decision="APPROVE"`) || !strings.Contains(waiting, "Waiting for Curtis Bell to review version 6.") || !strings.Contains(waiting, "Message Curtis Bell") {
		t.Fatalf("the requester's card is wrong: %s", agentUX055Text(waiting))
	}

	// Each command confirms what changed.
	persona.EvaluationPassed = 8
	for action, want := range map[string]string{
		"REQUEST_REVIEW": "Review of version 6 requested from Curtis Bell.",
		"REVIEW":         "Version 6 approved.",
		"RUN_EVALUATION": "Evaluation passed: 8 of 8 cases.",
		"PUBLISH":        "Version 6 published.",
	} {
		if got := personaAdminCommandOutcomeText(locale, action, persona); got != want {
			t.Errorf("%s confirms %q, want %q", action, got, want)
		}
	}
	for _, language := range []string{"de-DE", "ar"} {
		localized := ResolveProductLocale(language)
		for _, action := range []string{"REQUEST_REVIEW", "REVIEW", "RUN_EVALUATION", "PUBLISH"} {
			text := personaAdminCommandOutcomeText(localized, action, persona)
			if text == "" || strings.Contains(text, "{") || text == personaAdminCommandOutcomeText(locale, action, persona) {
				t.Errorf("%s %s outcome is not localized: %q", language, action, text)
			}
		}
	}

	// A live agent with no conversation is not told to publish first.
	live := agentUXSetup2Persona()
	live.Lifecycle = PersonaPublished
	placements := personaAdminRender(t, personaAdminPlacements(locale, live, agentUXSetup2Snapshot(live)))
	if strings.Contains(placements, "Publish it first") || !strings.Contains(placements, "Choose Add to a conversation so people can use it.") {
		t.Fatalf("a published agent's empty placement list contradicts its state: %s", placements)
	}
	draft := agentUXSetup2Persona()
	draft.Lifecycle = PersonaDraft
	if before := personaAdminRender(t, personaAdminPlacements(locale, draft, agentUXSetup2Snapshot(draft))); !strings.Contains(before, "Publish it first, then add it to a conversation here.") {
		t.Fatalf("an unpublished agent is not told what comes first: %s", before)
	}
}
