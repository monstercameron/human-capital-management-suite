package chatui_test

import (
	"html"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// agentux059Page renders the whole Chat page, with the product's own catalog,
// for a conversation with an agent whose answer carries the rating controls.
func agentux059Page(t *testing.T, locale string, change func(*chatui.Model)) string {
	t.Helper()
	ctx := productui.ResolveProductLocale(locale)
	at := time.Date(2026, 10, 2, 9, 30, 0, 0, time.UTC)
	m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), SelectedID: "policy", CurrentUser: "alice", CurrentTenantID: "t",
		Text:          func(key string) string { return ctx.Text(key) },
		Conversations: []chatui.Conversation{{ID: "policy", Name: "Policy Helper", Kind: chatui.DirectMessage, Agent: true, AgentID: "policy-helper", Joined: true}},
		Messages: []chatui.Message{
			{ID: "question", AuthorID: "alice", Author: "Alice", SentAt: at, Body: "How much leave carries over?", Revision: 1},
			{ID: "answer", AuthorID: "policy-helper", Author: "Policy Helper", SentAt: at.Add(time.Second), Body: "Up to 40 hours carry over.", Revision: 1,
				PersonaActor: &chatui.PersonaActor{PersonaID: "policy-helper", AgentID: "policy-helper", Trusted: true}},
		},
		PersonaActivityReady: true,
		PersonaInvocations: []chatui.PersonaThreadInvocation{{PostID: "question", Projection: chatui.PersonaProgressProjection{
			InvocationID: "run", ViewerID: "alice", InvokerID: "alice", AgentName: "Policy Helper", DurablePostID: "answer"}}},
	}
	m.Callbacks.SendMessage = func(string, string) {}
	m.Callbacks.SubmitAgentFeedback = func(string, bool) {}
	m.Callbacks.UndoAgentFeedback = func(string) {}
	change(&m)
	rendered, err := ui.RenderToString(chatui.Build(m))
	if err != nil {
		t.Fatal(err)
	}
	page := html.UnescapeString(rendered)
	if strings.Contains(page, "⟦") {
		t.Errorf("%s: the page prints a copy key: %s", locale, regexp.MustCompile(`.{30}⟦[^⟧]*⟧`).FindString(page))
	}
	return page
}

// agentux059Button finds one rating button on the page by its data-extra.
func agentux059Button(t *testing.T, page, rating string) string {
	t.Helper()
	for _, button := range regexp.MustCompile(`<button[^>]*class="agent-feedback-button"[^>]*>`).FindAllString(page, -1) {
		if strings.Contains(button, `data-extra="`+rating+`"`) {
			return button
		}
	}
	t.Fatalf("the page has no %q rating button", rating)
	return ""
}

// TestTodo_AGENTUX_059_Browser draws the page a person sees around a rating,
// in the three languages: a rating the server holds is shown after a reload and
// pressing it again removes it; a rating the server refused is not shown as
// given, the buttons are back at what the server holds, and the sentence
// "Your rating was not saved. Try again." stands beside them as an alert.
func TestTodo_AGENTUX_059_Browser(t *testing.T) {
	unsaved := regexp.MustCompile(`<span[^>]*class="agent-feedback-unsaved"[^>]*>([^<]*)</span>`)
	for locale, sentence := range map[string]string{
		"en-US": "Your rating was not saved. Try again.",
		"de-DE": "Ihre Bewertung wurde nicht gespeichert. Versuchen Sie es erneut.",
		"ar":    "لم يتم حفظ تقييمك. حاول مرة أخرى.",
	} {
		// A reload: nothing was pressed on this page, the server holds "Helpful".
		stored := agentux059Page(t, locale, func(m *chatui.Model) { m.AgentFeedbackSaved = map[string]string{"run": chatui.AgentFeedbackHelpful} })
		helpful, notRight := agentux059Button(t, stored, "helpful"), agentux059Button(t, stored, "not-right")
		if !strings.Contains(helpful, `aria-pressed="true"`) || !strings.Contains(notRight, `aria-pressed="false"`) {
			t.Errorf("%s: the stored rating is not shown after a reload: %s %s", locale, helpful, notRight)
		}
		if !strings.Contains(helpful, `data-action="agent-feedback-undo"`) || !strings.Contains(helpful, `data-id="run"`) || !strings.Contains(notRight, `data-action="agent-feedback"`) {
			t.Errorf("%s: pressing the stored rating again does not remove it: %s", locale, helpful)
		}
		if unsaved.MatchString(stored) {
			t.Errorf("%s: a saved rating is reported as not saved", locale)
		}

		// A first rating the server refused: no rating shows, and the page says so.
		refused := agentux059Page(t, locale, func(m *chatui.Model) { m.AgentFeedbackRestored = map[string]string{"run": ""} })
		helpful, notRight = agentux059Button(t, refused, "helpful"), agentux059Button(t, refused, "not-right")
		if strings.Contains(helpful, `aria-pressed="true"`) || strings.Contains(notRight, `aria-pressed="true"`) {
			t.Errorf("%s: a rating the server refused is shown as given: %s %s", locale, helpful, notRight)
		}
		note := unsaved.FindStringSubmatch(refused)
		if note == nil || note[1] != sentence || !strings.Contains(note[0], `role="alert"`) {
			t.Errorf("%s: the page does not say %q beside the buttons as an alert: %v", locale, sentence, note)
		}
		group := regexp.MustCompile(`<div[^>]*class="agent-feedback"[^>]*>.*?</div>`).FindString(refused)
		if !strings.Contains(group, "agent-feedback-unsaved") || strings.Count(group, "agent-feedback-button") != 2 {
			t.Errorf("%s: the sentence is not in the same group as the two buttons: %s", locale, group)
		}

		// A change the server refused: the buttons are back at the rating it holds.
		kept := agentux059Page(t, locale, func(m *chatui.Model) {
			m.AgentFeedbackSaved = map[string]string{"run": chatui.AgentFeedbackHelpful}
			m.AgentFeedbackRestored = map[string]string{"run": chatui.AgentFeedbackHelpful}
		})
		if !strings.Contains(agentux059Button(t, kept, "helpful"), `aria-pressed="true"`) || strings.Contains(agentux059Button(t, kept, "not-right"), `aria-pressed="true"`) {
			t.Errorf("%s: after a refused change the buttons do not show what the server holds", locale)
		}
		if note := unsaved.FindStringSubmatch(kept); note == nil || note[1] != sentence {
			t.Errorf("%s: a refused change is not reported: %v", locale, note)
		}

		// With no way to send a rating the buttons are off, not silently dead.
		off := agentux059Page(t, locale, func(m *chatui.Model) { m.Callbacks.SubmitAgentFeedback = nil })
		if !strings.Contains(agentux059Button(t, off, "helpful"), " disabled") {
			t.Errorf("%s: the rating can be pressed where it cannot be sent", locale)
		}
		if locale == "ar" && !strings.Contains(refused, `dir="rtl"`) {
			t.Error("ar: the page is not right-to-left")
		}
	}
}
