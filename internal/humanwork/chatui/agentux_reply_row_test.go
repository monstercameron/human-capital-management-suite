package chatui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func renderAgentUXReplyRows(t *testing.T, model Model, postID string, width int) string {
	t.Helper()
	markup, err := ui.RenderToString(html.Div(html.Props{Data: map[string]string{"test-viewport": fmt.Sprint(width)}}, personaReplyRowsForPost(model, localUI{}, postID, time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC))...))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func agentUXReplyModel(locale string) Model {
	return Model{
		Locale: locale, CurrentUser: "alice",
		Messages:                []Message{{ID: "question", AuthorID: "alice", Author: "Alice", Body: "@Policy Helper what is the policy?"}},
		ResolvedPersonaMentions: []ResolvedPersonaMention{{Reference: ChatReference{Kind: "AGENT_MENTION", ID: "agent:policy", Display: "Policy Helper"}, Initials: "PH"}},
		PersonaInvocations: []PersonaThreadInvocation{{PostID: "question", Projection: PersonaProgressProjection{
			InvocationID: "invocation", ViewerID: "alice", InvokerID: "alice", AgentName: "Policy Helper",
			Progress: &PersonaProgressProps{InvocationID: "invocation", InvokerID: "alice", AgentName: "Policy Helper", ElapsedSeconds: 12, Visible: true},
		}}},
	}
}

func TestTodo_AGENTUX_025_WorkingRowLocalesAndWidths(t *testing.T) {
	for _, tc := range []struct{ locale, working string }{
		{"en-US", "Finding an answer in your policy documents…"},
		{"de-DE", "Eine Antwort wird in Ihren Richtliniendokumenten gesucht…"},
		{"ar", "جارٍ البحث عن إجابة في مستندات السياسات…"},
	} {
		for _, width := range []int{320, 1440} {
			model := agentUXReplyModel(tc.locale)
			if tc.locale == "ar" {
				model.Number = func(int) string { return "١٢" }
			}
			markup := renderAgentUXReplyRows(t, model, "question", width)
			for _, want := range []string{"Policy Helper", tc.working, "0:12", `data-agent-reply-state="working"`, `aria-live="polite"`, "agent-badge", "agent-working-dots"} {
				if !strings.Contains(markup, want) {
					t.Fatalf("%s at %dpx missing %q: %s", tc.locale, width, want, markup)
				}
			}
			if strings.Count(markup, `aria-live="polite"`) != 1 || strings.Contains(strings.ToLower(markup), ">persona<") {
				t.Fatalf("%s at %dpx did not announce exactly one agent state: %s", tc.locale, width, markup)
			}
		}
	}
}

func TestTodo_AGENTUX_026_PrivateAnswerReplacesWorkingRow(t *testing.T) {
	wants := map[string][]string{
		"en-US": {"Only visible to you", "Saved in your conversation with Policy Helper"},
		"de-DE": {"Nur für Sie sichtbar", "Gespeichert in Ihrer Unterhaltung mit Policy Helper"},
		"ar":    {"مرئي لك فقط", "محفوظ في محادثتك مع Policy Helper"},
	}
	for locale, localized := range wants {
		for _, width := range []int{320, 1440} {
			model := agentUXReplyModel(locale)
			model.EphemeralMessages = []EphemeralMessage{{ID: "answer", ThreadID: "question", Body: "Carry over up to five days.", OnlyVisibleToYou: true, CreatedAt: time.Date(2026, 10, 1, 11, 59, 0, 0, time.UTC), ExpiresAt: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)}}
			model.PersonaInvocations[0].Projection.PrivateReplyHref = "/workspace/app/chat#agent-dm"
			// CHATUX-003: the link to the saved copy is an item in the card's more menu.
			model.MenuID = "agent-card:answer"
			model.PersonaPostActors = map[string]PersonaPostActor{"answer": {Display: "Policy Helper", Actor: PersonaActor{PersonaID: "policy", AgentID: "agent:policy", Trusted: true}}}
			markup := renderAgentUXReplyRows(t, model, "question", width)
			for _, want := range append(localized, "Carry over up to five days.", `href="/workspace/app/chat#agent-dm"`, `data-agent-reply-state="answered-private"`, "class=\"agent-icon\"") {
				if !strings.Contains(markup, want) {
					t.Fatalf("%s private answer at %dpx missing %q: %s", locale, width, want, markup)
				}
			}
			if strings.Contains(markup, "is working on this") || strings.Count(markup, "agent-reply-row") != 1 || strings.Count(markup, `aria-live="polite"`) != 1 {
				t.Fatalf("%s private answer at %dpx did not replace and announce the working row: %s", locale, width, markup)
			}
		}
	}
}

func TestTodo_AGENTUX_026_ThreadAnswerStaysUnderInvokingReply(t *testing.T) {
	model := agentUXReplyModel("en-US")
	model.Messages = []Message{{ID: "root"}, {ID: "question"}}
	model.PersonaInvocations[0].ThreadID = "root"
	model.EphemeralMessages = []EphemeralMessage{{ID: "answer", ThreadID: "root", Body: "Private answer", OnlyVisibleToYou: true, CreatedAt: time.Date(2026, 10, 1, 11, 59, 0, 0, time.UTC), ExpiresAt: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)}}
	if root := renderAgentUXReplyRows(t, model, "root", 1440); strings.Contains(root, "Private answer") {
		t.Fatalf("thread answer was attached to the thread root instead of the invoking reply: %s", root)
	}
	if reply := renderAgentUXReplyRows(t, model, "question", 1440); !strings.Contains(reply, "Private answer") {
		t.Fatalf("thread answer was not attached to its invoking reply: %s", reply)
	}
}

func TestTodo_AGENTUX_027_FailureReasonsAreSanitizedAndRetryable(t *testing.T) {
	cases := []struct{ code, want string }{
		{"MODEL_UNAVAILABLE", "Policy Helper could not answer because the service had a problem. Try again."},
		{"ADMISSION_REFUSED", "cannot answer this request here"},
		{"CONTEXT_UNAVAILABLE", "cannot answer this request here"},
		{"TIMED_OUT", "took too long"},
		{"ADMIN_STOPPED", "answer was stopped"},
		{"INTERNAL_DATABASE_DETAIL", "is not available in this conversation right now"},
	}
	for _, tc := range cases {
		model := agentUXReplyModel("en-US")
		model.PersonaInvocations[0].Projection.Progress = nil
		model.PersonaInvocations[0].Projection.Failure = &PersonaProgressFailure{InvocationID: "invocation", InvokerID: "alice", Code: tc.code, Message: "secret provider trace", Retryable: tc.code == "MODEL_UNAVAILABLE"}
		markup := renderAgentUXReplyRows(t, model, "question", 1440)
		if !strings.Contains(markup, tc.want) || strings.Contains(markup, tc.code) || strings.Contains(markup, "secret provider trace") {
			t.Fatalf("%s failure was not sanitized: %s", tc.code, markup)
		}
		if (tc.code == "MODEL_UNAVAILABLE") != strings.Contains(markup, ">Try again<") {
			t.Fatalf("%s retry policy mismatch: %s", tc.code, markup)
		}
	}
	localizedFailures := map[string]string{
		"en-US": "Policy Helper could not answer because the service had a problem. Try again.",
		"de-DE": "Policy Helper konnte wegen eines Dienstproblems nicht antworten. Versuchen Sie es erneut.",
		"ar":    "تعذر على Policy Helper الإجابة بسبب مشكلة في الخدمة. حاول مرة أخرى.",
	}
	for locale, want := range localizedFailures {
		for _, width := range []int{320, 1440} {
			model := agentUXReplyModel(locale)
			model.PersonaInvocations[0].Projection.Progress = nil
			model.PersonaInvocations[0].Projection.Failure = &PersonaProgressFailure{InvocationID: "invocation", InvokerID: "alice", Code: "MODEL_UNAVAILABLE", Retryable: true}
			markup := renderAgentUXReplyRows(t, model, "question", width)
			if !strings.Contains(markup, want) || strings.Contains(markup, "chat.agent.") || strings.Contains(markup, ">Persona<") || strings.Count(markup, `aria-live="polite"`) != 1 {
				t.Fatalf("%s failure at %dpx exposed an untranslated or unannounced state: %s", locale, width, markup)
			}
		}
	}
}

func TestTodo_AGENTUX_028_OtherMemberProjectionHasNoPrivateTrace(t *testing.T) {
	model := agentUXReplyModel("en-US")
	model.CurrentUser = "bob"
	model.PersonaInvocations[0].Projection.ViewerID = "bob"
	model.EphemeralMessages = nil
	markup := renderAgentUXReplyRows(t, model, "question", 1440)
	for _, secret := range []string{"Carry over", "Only visible", "Policy Helper", "agent-reply-state"} {
		if strings.Contains(markup, secret) {
			t.Fatalf("second member projection disclosed %q: %s", secret, markup)
		}
	}
}

func TestTodo_AGENTUX_029_ReplyRowsAreResponsiveRTLAndPolitelyAnnounced(t *testing.T) {
	for _, width := range []int{320, 1440} {
		markup := renderAgentUXReplyRows(t, agentUXReplyModel("ar"), "question", width)
		if !strings.Contains(markup, `dir="rtl"`) || strings.Count(markup, `aria-live="polite"`) != 1 {
			t.Fatalf("reply row at %dpx lost RTL or polite announcement: %s", width, markup)
		}
	}
	for _, want := range []string{"@media(max-width:390px)", "min-width:0", "max-width:100%", "prefers-reduced-motion:reduce", "var(--hcm-color-border)", "var(--hcm-color-brand-soft)", "var(--hcm-color-text-muted)"} {
		if !strings.Contains(PersonaProgressStyles, want) {
			t.Fatalf("responsive reply styles missing %q", want)
		}
	}
}
