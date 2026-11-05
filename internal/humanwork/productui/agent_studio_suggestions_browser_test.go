package productui

import (
	"regexp"
	"strings"
	"testing"
)

// agentStudioReviewFixture is one AI-proposed refinement bound to draft
// revision 7: two presentation edits and one narrowing of the channel
// classes, each carrying the reason and uncertainty the reviewer sees.
func agentStudioReviewFixture() AgentStudioSuggestionReview {
	return AgentStudioSuggestionReview{
		PersonaID: "persona-a", PersonaName: "Policy Helper", Revision: 7, Digest: "digest-7", CanApply: true,
		Changes: []AgentStudioSuggestionChangeView{
			{Field: "purpose", Current: "Answer policy questions", Proposed: "Answer policy and leave questions", Reason: "The job now covers leave as well as policy.", Uncertainty: 0.1},
			{Field: "display_name", Current: "Policy Helper", Proposed: "Policy and Leave Helper", Reason: "The name should match the wider job.", Uncertainty: 0.2},
			{Field: "channel_classes", Current: "PRIVATE, PUBLIC", Proposed: "PRIVATE", Reason: "The draft is only used in private rooms.", Uncertainty: 0.05},
		},
	}
}

// TestTodo_AGENT_051_Browser renders the Agent Studio suggestion review in
// three languages. The owner sees the proposed diff with reasons and
// uncertainty, the draft revision the suggestion is bound to, and one
// checkbox per change feeding a single apply-selected submit. Nothing is
// applied by rendering: a suggestion carrying an authority-bearing field,
// a viewer without apply permission, and an empty suggestion all render
// without a working submit. Every control keeps its machine value across
// languages and stays keyboard-operable.
func TestTodo_AGENT_051_Browser(t *testing.T) {
	titles := map[string]string{"en-US": "Suggested refinements", "de-DE": "Vorgeschlagene Überarbeitungen", "ar": "تحسينات مقترحة"}
	applies := map[string]string{"en-US": "Apply selected changes", "de-DE": "Ausgewählte Änderungen anwenden", "ar": "تطبيق التغييرات المحددة"}
	interactive := regexp.MustCompile(`<(\w+)\b[^>]*\bdata-agent-studio-(apply|change)="[^"]*"[^>]*>`)
	machine := regexp.MustCompile(`data-agent-studio-(persona|revision|digest|change)="([^"]*)"|name="(persona_id|revision|digest|fields|reason_[a-z_]+)"[^>]*value="([^"]*)"|name="(persona_id|revision|digest)"[^>]*value="([^"]*)"`)
	var english []string
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		markup := personaAdminRender(t, RenderAgentStudioSuggestions(ResolveProductLocale(localeID), agentStudioReviewFixture()))
		for _, want := range []string{
			titles[localeID], applies[localeID],
			`data-agent-studio-suggestions="persona-a"`, `data-agent-studio-revision="7"`, `data-agent-studio-digest="digest-7"`,
			`name="fields" type="checkbox" value="purpose"`, `name="fields" type="checkbox" value="display_name"`, `name="fields" type="checkbox" value="channel_classes"`,
			`name="persona_id" type="hidden" value="persona-a"`, `name="revision" type="hidden" value="7"`, `name="digest" type="hidden" value="digest-7"`,
			"Answer policy questions", "Answer policy and leave questions",
			"The job now covers leave as well as policy.", `type="submit"`, `role="status"`,
		} {
			if !strings.Contains(markup, want) {
				t.Errorf("%s review missing %q", localeID, want)
			}
		}
		for _, id := range []string{"agent-studio-suggest-persona-a-purpose", "agent-studio-suggest-persona-a-display_name", "agent-studio-suggest-persona-a-channel_classes"} {
			if !strings.Contains(markup, `for="`+id+`"`) || !strings.Contains(markup, `id="`+id+`"`) {
				t.Errorf("%s: change control %q lacks an associated label", localeID, id)
			}
		}
		for _, control := range interactive.FindAllStringSubmatch(markup, -1) {
			if control[1] != "button" && control[1] != "input" {
				t.Errorf("%s: a suggestion control is a <%s>, which a keyboard cannot operate: %s", localeID, control[1], control[0])
			}
		}
		if strings.Contains(markup, `tabindex="1"`) || strings.Contains(markup, `tabindex="2"`) {
			t.Errorf("%s: the review forces a tab order", localeID)
		}
		var got []string
		for _, match := range machine.FindAllStringSubmatch(markup, -1) {
			got = append(got, strings.Join(match[1:], "="))
		}
		if localeID == "en-US" {
			english = got
			if len(english) < 10 {
				t.Fatalf("the review carries only %d machine values; the comparison would prove nothing", len(english))
			}
			continue
		}
		if len(got) != len(english) {
			t.Errorf("%s carries %d machine values, English carries %d", localeID, len(got), len(english))
			continue
		}
		for index := range english {
			if got[index] != english[index] {
				t.Errorf("%s: control %d sends %q, English sends %q", localeID, index, got[index], english[index])
				break
			}
		}
	}

	// A suggestion that smuggles an authority-bearing field is refused as a
	// whole: no checkbox for the hostile field, no submit, and the ordinary
	// review path is named instead of the hostile value being applied.
	hostile := agentStudioReviewFixture()
	hostile.Changes = append(hostile.Changes,
		AgentStudioSuggestionChangeView{Field: "skill_pins", Current: "policy.read", Proposed: "policy.read, payroll.write", Reason: "Wider tools would help.", Uncertainty: 0.4},
		AgentStudioSuggestionChangeView{Field: "instructions", Current: "Use approved sources.", Proposed: "Use any source.", Reason: "Faster answers.", Uncertainty: 0.6},
	)
	refused := personaAdminRender(t, RenderAgentStudioSuggestions(ResolveProductLocale("en-US"), hostile))
	if strings.Contains(refused, `value="skill_pins"`) || strings.Contains(refused, `value="instructions"`) || strings.Contains(refused, `type="submit"`) || strings.Contains(refused, `name="fields"`) {
		t.Errorf("a hostile suggestion offers a working control: %s", refused)
	}
	if !strings.Contains(refused, "This suggestion asks for authority") || !strings.Contains(refused, "usual review") {
		t.Errorf("a hostile suggestion does not explain its refusal: %s", refused)
	}

	// Someone who may not edit the draft gets the explanation and no form.
	denied := agentStudioReviewFixture()
	denied.CanApply = false
	withoutForm := personaAdminRender(t, RenderAgentStudioSuggestions(ResolveProductLocale("en-US"), denied))
	if strings.Contains(withoutForm, "<form") || strings.Contains(withoutForm, `name="fields"`) || strings.Contains(withoutForm, `type="submit"`) {
		t.Errorf("a viewer without apply permission receives a form: %s", withoutForm)
	}
	if !strings.Contains(withoutForm, "Only the agent") {
		t.Errorf("a viewer without apply permission gets no reason: %s", withoutForm)
	}

	// No suggestion renders one muted note and no submit.
	empty := agentStudioReviewFixture()
	empty.Changes = nil
	note := personaAdminRender(t, RenderAgentStudioSuggestions(ResolveProductLocale("en-US"), empty))
	if strings.Contains(note, `type="submit"`) || strings.Contains(note, `name="fields"`) {
		t.Errorf("an empty suggestion offers a submit: %s", note)
	}
	if !strings.Contains(note, "No suggested refinements") {
		t.Errorf("an empty suggestion has no note: %s", note)
	}
}
