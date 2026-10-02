package productui

import (
	"strings"
	"testing"
)

type agentDocInstr2ReviewClient struct{ personaAdminTestClient }

func (*agentDocInstr2ReviewClient) ReviewPersona(string, string) error { return nil }

func TestAgentDocInstr2_F1MentionListboxContract(t *testing.T) {
	markup := personaAdminRender(t, agentDocInstructionEditor(ResolveProductLocale("en-US"), "instructions", "Use @Paid", false))
	for _, want := range []string{`data-agentdoc-instructions="true"`, `aria-autocomplete="list"`, `aria-expanded="false"`, `data-agentdoc-mention-menu="true"`, `role="listbox"`, `data-agentdoc-mention-results="true"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("mention editor missing %q: %s", want, markup)
		}
	}
}

func TestAgentDocInstr2_F2PickerRenderedTreeContract(t *testing.T) {
	model := AgentDocumentReferencePickerModel{
		ID: "policy", Available: true, State: AgentDocumentPickerReady,
		References:  []PersonaAdminDocumentReference{{DocumentID: "doc-a", Title: "Policy", Label: "Policy", VersionMode: "PINNED", PinnedVersion: 2, Readable: true}},
		Suggestions: []AgentDocumentSuggestion{{DocumentID: "doc-b", Title: "Handbook", Location: "People", PublishedVersion: 3}},
	}
	markup := personaAdminRender(t, AgentDocumentReferencePicker(ResolveProductLocale("en-US"), model))
	for _, want := range []string{`data-agentdoc-picker="policy"`, `data-agentdoc-results="true"`, `data-agentdoc-option="doc-b"`, `data-agentdoc-selected="true"`, `data-agentdoc-reference="true"`, `data-agentdoc-mode="true"`, `data-agentdoc-remove="true"`, `data-document-location=""`} {
		if !strings.Contains(markup, want) {
			t.Errorf("picker/wasm contract missing %q: %s", want, markup)
		}
	}
}

func TestAgentDocInstr2_F3VisibleStoredBoundary(t *testing.T) {
	references := []PersonaAdminDocumentReference{
		{DocumentID: "doc-people", Title: "Paid time off", Location: "People", Label: "Paid time off", Readable: true},
		{DocumentID: "doc-finance", Title: "Paid time off", Location: "Finance", Label: "Paid time off", Readable: true},
	}
	visible := AgentDocInstructionDisplayText("Follow {{doc:doc-people}}.", references)
	if visible != "Follow @[Paid time off (People)]." || strings.Contains(visible, "doc-people") {
		t.Fatalf("visible instructions = %q", visible)
	}
	stored, kept, unknown := AgentDocInstructionStoredText(visible, references)
	if stored != "Follow {{doc:doc-people}}." || len(kept) != 1 || kept[0].DocumentID != "doc-people" || len(unknown) != 0 {
		t.Fatalf("stored=%q kept=%+v unknown=%v", stored, kept, unknown)
	}
	_, kept, unknown = AgentDocInstructionStoredText("Follow @[Missing policy].", references)
	if len(kept) != 0 || len(unknown) != 1 || unknown[0] != "Missing policy" {
		t.Fatalf("unknown mention kept=%+v unknown=%v", kept, unknown)
	}
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		if text := AgentDocInstructionValidationText(ResolveProductLocale(localeID), "Missing policy"); !strings.Contains(text, "Missing policy") {
			t.Errorf("%s validation does not name mention: %q", localeID, text)
		}
	}
}

func TestAgentDocInstr2_F4PickerStateIsTruthful(t *testing.T) {
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		idle := personaAdminRender(t, AgentDocumentReferencePicker(locale, AgentDocumentReferencePickerModel{ID: "idle", Available: true, State: AgentDocumentPickerIdle}))
		if !strings.Contains(idle, `data-agentdoc-count="true" hidden`) || !strings.Contains(idle, `data-agentdoc-retry="true" hidden`) || strings.Count(idle, agentDocumentPickerText(locale, "idle")) != 2 {
			// One occurrence is visible status and one is the browser data value.
			t.Errorf("%s idle state is misleading: %s", localeID, idle)
		}
		failed := personaAdminRender(t, AgentDocumentReferencePicker(locale, AgentDocumentReferencePickerModel{ID: "failed", Available: true, State: AgentDocumentPickerFailed}))
		if strings.Contains(failed, `data-agentdoc-retry="true" hidden`) {
			t.Errorf("%s failed retry is hidden: %s", localeID, failed)
		}
		withTwo := personaAdminRender(t, AgentDocumentReferencePicker(locale, AgentDocumentReferencePickerModel{ID: "two", Available: true, References: []PersonaAdminDocumentReference{{DocumentID: "a", Label: "A"}, {DocumentID: "b", Label: "B"}}}))
		if !strings.Contains(withTwo, agentDocumentPickerCount(locale, 2, 8)) || strings.Contains(withTwo, `data-agentdoc-count="true" hidden`) {
			t.Errorf("%s nonzero count missing: %s", localeID, withTwo)
		}
	}
}

func TestAgentDocInstr2_F5LiveAndDraftVersions(t *testing.T) {
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		persona := PersonaAdminPersona{ID: "policy", Name: "Policy Helper", Version: "5", LiveVersion: "4", Lifecycle: PersonaDraft}
		markup := personaAdminRender(t, personaAdminCard(locale, &personaAdminTestClient{}, persona, PersonaAdminSnapshot{}))
		want := strings.NewReplacer("{live}", personaAdminLocalizedNumber(locale, "4"), "{draft}", personaAdminLocalizedNumber(locale, "5")).Replace(personaAdminText(locale, "live_and_draft"))
		if !strings.Contains(markup, want) || !strings.Contains(markup, personaAdminText(locale, "request_review")) || !strings.Contains(markup, `data-persona-next-step="next_draft"`) {
			t.Errorf("%s live/draft state incomplete: %s", localeID, markup)
		}
	}
}

func TestAgentDocInstr2_F6LifecycleHasOnePrimaryAndNextStep(t *testing.T) {
	states := []struct {
		name string
		edit func(*PersonaAdminPersona)
		want string
	}{
		{"draft", func(p *PersonaAdminPersona) {}, "request_review"},
		{"in-review", func(p *PersonaAdminPersona) {
			p.Lifecycle = PersonaInReview
			p.Reviewer = "curtis-bell"
			p.ReviewerName = "Curtis Bell"
		}, "approve"},
		{"reviewed", func(p *PersonaAdminPersona) { p.Lifecycle = PersonaInReview; p.ReviewApproved = true }, "run_evaluation"},
		{"running", func(p *PersonaAdminPersona) {
			p.Lifecycle = PersonaInReview
			p.ReviewApproved = true
			p.EvaluationStatus = "RUNNING"
		}, "evaluation_running_action"},
		{"failed", func(p *PersonaAdminPersona) {
			p.Lifecycle = PersonaInReview
			p.ReviewApproved = true
			p.EvaluationStatus = "FAILED"
			p.EvaluationPassed = 3
			p.EvaluationFailed = 1
			p.EvaluationFailureNames = []string{"missing-citation"}
		}, "run_evaluation"},
		{"evaluated", func(p *PersonaAdminPersona) {
			p.Lifecycle = PersonaInReview
			p.ReviewApproved = true
			p.EvaluationRef = "evaluation"
			p.LiveVersion = "4"
		}, "publish"},
		{"published", func(p *PersonaAdminPersona) {
			p.Lifecycle = PersonaPublished
			p.ReviewApproved = true
			p.ReviewerName = "Curtis Bell"
			p.Reviewer = "curtis-bell"
			p.ReviewApprovedAt = "2026-09-30"
			p.EvaluationPassedAt = "2026-10-01"
			p.PublishedAt = "2026-10-01"
		}, "ask_this_agent"},
	}
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		for _, state := range states {
			t.Run(localeID+"/"+state.name, func(t *testing.T) {
				persona := PersonaAdminPersona{ID: "policy", Name: "Policy Helper", Version: "5", Lifecycle: PersonaDraft}
				state.edit(&persona)
				markup := personaAdminRender(t, personaAdminCard(locale, &agentDocInstr2ReviewClient{}, persona, PersonaAdminSnapshot{}))
				// A published agent has no lifecycle step left, so its card has no
				// primary action; the way to ask it is the link beside its name.
				primary := 1
				if state.name == "published" {
					primary = 0
				}
				if got := strings.Count(markup, `class="button primary"`); got != primary {
					t.Fatalf("primary actions=%d, want %d: %s", got, primary, markup)
				}
				want := strings.ReplaceAll(personaAdminText(locale, state.want), "{version}", personaAdminLocalizedNumber(locale, persona.Version))
				if state.name == "published" {
					want = agentUXR7Text(locale, "ask_link")
				}
				if !strings.Contains(markup, want) || strings.Count(markup, `data-persona-next-step=`) != 1 {
					t.Fatalf("missing %s or one next step: %s", state.want, markup)
				}
				if state.name == "published" {
					if strings.Count(markup, `data-step-state="complete"`) != 4 || strings.Count(markup, `<time `) < 3 || !strings.Contains(markup, "Curtis Bell") {
						t.Fatalf("published evidence/progress incomplete: %s", markup)
					}
				}
			})
		}
	}
}
