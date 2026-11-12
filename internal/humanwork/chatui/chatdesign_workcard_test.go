//go:build !(js && wasm)

package chatui

import (
	"regexp"
	"strings"
	"testing"
)

// #3777: the journey card and the ticket card are one card. Both carry the
// shared parts, and a card that waits on the reader differs from an
// informational one in its marker, its accessible name and its words.
func TestChatDesign_3777_JourneyAndTicketShareOneWorkCard(t *testing.T) {
	for locale, waiting := range map[string]string{"en-US": "Waiting on you", "de-DE": "Wartet auf Sie", "ar": "بانتظارك"} {
		if got := workCardWaitingText(locale); got != waiting {
			t.Fatalf("%s: the waiting sentence is %q, want %q", locale, got, waiting)
		}
		m := Model{Locale: locale, CurrentUser: "u-me", CurrentUserName: "Dana Reyes"}
		journey := func(approver, tone string) string {
			return chatDesignRender(t, journeyPreviewCard(m, JourneyPreview{IntentID: "intent-1", Readable: true, State: "ready", Workflow: "Promotion", Worker: "Sam Lee", From: "P3", To: "P4", Stage: "JOURNEY_STAGE_MANAGER_APPROVAL", StageTone: tone, Approver: approver}))
		}
		ticket := func(assignee, tone string) string {
			return chatDesignRender(t, projectPreviewCard(m, ProjectTaskPreview{ProjectID: "proj-1", TaskID: "task-1", Title: "Fix the badge reader", Readable: true, State: "ready", Key: "OPS-7", Status: "In progress", StatusTone: tone, Assignee: assignee}))
		}
		cards := map[string]map[string]string{
			"journey": {"informational": journey("Kim Park", "active"), "waiting": journey("Dana Reyes", "active"), "done": journey("Dana Reyes", "done")},
			"ticket":  {"informational": ticket("Kim Park", "doing"), "waiting": ticket("dana reyes", "doing"), "done": ticket("Dana Reyes", "done")},
		}
		for kind, states := range cards {
			for state, markup := range states {
				// One card: the same parts on both kinds, in the same order.
				order := -1
				for _, part := range []string{"chat-work-card", "chat-work-card-type", "chat-work-card-title", "chat-work-card-status", "chat-work-card-waits"} {
					at := regexp.MustCompile(`class="[^"]*\b` + part + `(\s[^"]*)?"`).FindStringIndex(markup)
					if at == nil || at[0] < order {
						t.Fatalf("%s %s %s: the card has no %s part in its place: %s", locale, kind, state, part, markup)
					}
					order = at[0]
				}
				if !strings.HasPrefix(markup, "<a ") || !strings.Contains(markup, `href="`) {
					t.Errorf("%s %s %s: the card is no longer the link that opens the object", locale, kind, state)
				}
				marked := strings.Contains(markup, `data-waits="you"`)
				slot := strings.Contains(markup, `<span class="chat-work-card-action">`+waiting+`</span>`)
				named := regexp.MustCompile(`aria-label="[^"]*, ` + regexp.QuoteMeta(waiting) + `"`).MatchString(markup)
				if want := state == "waiting"; marked != want || slot != want || named != want {
					t.Errorf("%s %s %s: waiting-on-you marker=%v slot=%v name=%v, want all %v: %s", locale, kind, state, marked, slot, named, want, markup)
				}
			}
		}
		// The states that are not ready, and a board, are the same card, informational.
		for name, markup := range map[string]string{
			"loading journey":   chatDesignRender(t, journeyPreviewCard(m, JourneyPreview{IntentID: "intent-1", State: "loading"})),
			"restricted ticket": chatDesignRender(t, projectPreviewCard(m, ProjectTaskPreview{ProjectID: "proj-1", TaskID: "task-1", State: "restricted"})),
			"board":             chatDesignRender(t, projectPreviewCard(m, ProjectTaskPreview{ProjectID: "proj-1", Title: "Operations", Readable: true, State: "ready", Total: 4, Done: 1})),
		} {
			if !regexp.MustCompile(`class="chat-embed [^"]*\bchat-work-card"`).MatchString(markup) || strings.Contains(markup, "data-waits") {
				t.Errorf("%s %s: not the shared informational card: %s", locale, name, markup)
			}
		}
	}
	// A reader without a known name is never told a card waits on them.
	if workCardWaitsOnViewer(Model{}, "", true) || workCardWaitsOnViewer(Model{CurrentUserName: "Dana Reyes"}, "", true) || workCardWaitsOnViewer(Model{}, "Dana Reyes", true) {
		t.Fatal("an unknown reader or an unassigned object is treated as waiting on the reader")
	}
}

// The card's style is declared once, and the two looks are different rules.
func TestChatDesign_3777_WorkCardStyleIsDefinedOnce(t *testing.T) {
	for _, selector := range []string{
		`.chat-workspace .chat-work-card{`, `.chat-workspace .chat-work-card[data-waits="you"]{`, `.chat-workspace .chat-work-card-type{`,
		`.chat-workspace .chat-work-card-title{`, `.chat-workspace .chat-work-card-status{`, `.chat-workspace .chat-work-card-waits{`, `.chat-workspace .chat-work-card-action{`,
	} {
		// The forced-colours rule repeats the waiting selector inside its @media.
		sheet := strings.ReplaceAll(Stylesheet, `@media(forced-colors:active){.chat-workspace .chat-work-card[data-waits="you"]{border-width:2px}}`, "")
		if count := strings.Count(sheet, selector); count != 1 {
			t.Errorf("%q is declared %d times, want once", selector, count)
		}
	}
	// No kind keeps a frame colour of its own beside the shared card's.
	if own := regexp.MustCompile(`\.chat-(journey|project)-embed[^{},]*\{[^}]*border-inline-start-color`).FindString(Stylesheet); own != "" {
		t.Errorf("a kind still colours its own frame: %s", own)
	}
	rule := func(selector string) string {
		start := strings.Index(Stylesheet, selector)
		if start < 0 {
			t.Fatalf("no rule %q", selector)
		}
		body := Stylesheet[start+len(selector):]
		return body[:strings.Index(body, "}")]
	}
	plain, waiting := rule(`.chat-workspace .chat-work-card{`), rule(`.chat-workspace .chat-work-card[data-waits="you"]{`)
	if !strings.Contains(plain, "background:var(--surface)") || !strings.Contains(plain, "border-inline-start:3px solid var(--hcm-color-control-border)") {
		t.Errorf("the informational card is not the neutral surface card: %s", plain)
	}
	if !strings.Contains(waiting, "border-color:var(--accent)") || !strings.Contains(waiting, "border-inline-start-color:var(--accent)") || !strings.Contains(waiting, "background:var(--soft)") {
		t.Errorf("the waiting card does not take the accent frame and tint: %s", waiting)
	}
	if action := rule(`.chat-workspace .chat-work-card-action{`); !strings.Contains(action, "background:var(--accent)") || !strings.Contains(action, "color:var(--hcm-color-on-brand)") {
		t.Errorf("the action slot is not the accent pill: %s", action)
	}
	// RTL: the card is laid out with logical properties only.
	if physical := regexp.MustCompile(`(margin|padding|border)-(left|right)|[;{](left|right):|text-align:(left|right)`).FindString(chatDesignWorkCardStyles); physical != "" {
		t.Errorf("the work card uses a physical side: %s", physical)
	}
}
