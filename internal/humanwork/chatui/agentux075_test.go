package chatui

import (
	"strings"
	"testing"
)

func agentUX075Actor() *PersonaActor {
	return &PersonaActor{PersonaID: "assistant", AgentID: "assistant", Trusted: true}
}

func agentUX075Sources() []agentReplySource {
	return []agentReplySource{
		{Title: "Paid time off policy · Carryover · v1.0.0", Href: "http://x/workspace/app/docs?document=pto", Readable: true},
		{Title: "2026 holiday guide · v1.0.0", Href: "http://x/workspace/app/docs?document=holidays", Readable: true},
	}
}

// An agent's answer is formatted text: every Markdown construct draws as its
// element and no mark of the markup is left on the page.
func TestTodo_AGENTUX_075(t *testing.T) {
	m := Model{Locale: "en-US"}
	t.Run("constructs", func(t *testing.T) {
		body := strings.Join([]string{
			"## Time off", "",
			"You get **20 days**, *prorated*, and `PTO_CARRY` is capped.", "",
			"- Vacation", "  - Nested item", "- Sick leave", "", "1. First", "2. Second", "",
			"| Type | Days |", "|---|--:|", "| Vacation | `20` |", "| Sick | 10 |", "",
			"```", "if x { return }", "```", "",
			"See [the policy](https://example.com/a_b_c) or https://example.com/path_with_under_scores.",
		}, "\n")
		markup := renderMarkdownBody(t, m, agentUX075AnswerBody(agentUX075Actor(), body, nil))
		for _, want := range []string{"<h4>Time off</h4>", "<strong>20 days</strong>", "<em>prorated</em>", "<code>PTO_CARRY</code>",
			"<ul>", "<ol>", "Nested item", "<table", "<thead>", `<th scope="col">Type</th>`, "<code>20</code>", `md-align-end`,
			"<pre><code>", `href="https://example.com/a_b_c"`, "path_with_under_scores"} {
			if !strings.Contains(markup, want) {
				t.Errorf("answer markup lacks %q:\n%s", want, markup)
			}
		}
		for _, raw := range []string{"**", "##", "|---", "```", "| Type"} {
			if strings.Contains(markup, raw) {
				t.Errorf("raw markup %q reached the page:\n%s", raw, markup)
			}
		}
	})
	t.Run("a table with inline code and a pipe in a cell", func(t *testing.T) {
		markup := renderMarkdownBody(t, m, "| Command | Effect |\n|---|---|\n| `a \\| b` | pipes |\n")
		if !strings.Contains(markup, "<table") || !strings.Contains(markup, "pipes") || strings.Contains(markup, "|---|") {
			t.Fatalf("table with code: %s", markup)
		}
	})
	t.Run("emphasis left open", func(t *testing.T) {
		for body, want := range map[string]string{
			"**Important":                  "Important",
			"a **b** c *d":                 "a **b** c d",
			"use `code":                    "use code",
			"snake_case_word stays":        "snake_case_word stays",
			"* a bullet stays":             "* a bullet stays",
			"2 * 3 stays":                  "2 * 3 stays",
			"https://x.com/a_b_c stays":    "https://x.com/a_b_c stays",
			"`**kept**` inside code spans": "`**kept**` inside code spans",
		} {
			if got := agentUX075CleanAnswer(body, nil); got != want {
				t.Errorf("%q cleaned to %q, want %q", body, got, want)
			}
		}
		fenced := "```\n**not closed\n```"
		if got := agentUX075CleanAnswer(fenced, nil); got != fenced {
			t.Errorf("code block was changed: %q", got)
		}
		// A message that is not an agent's is never rewritten.
		if got := agentUX075AnswerBody(nil, "**x", nil); got != "**x" {
			t.Errorf("a person's message was rewritten: %q", got)
		}
	})
	t.Run("citation stays in its sentence", func(t *testing.T) {
		// The answer's parts arrive as paragraphs: the sentence, the citation, the full stop.
		body := "Employees may carry over up to 40 hours of unused PTO into the next calendar year\n\n[Paid time off policy · Carryover · v1.0.0](http://x/workspace/app/docs?document=pto)\n\n."
		cleaned := agentUX075CleanAnswer(body, agentUX075Sources())
		if !strings.Contains(cleaned, `calendar year[\[1\]](http://x/workspace/app/docs?document=pto "Paid time off policy · Carryover · v1.0.0").`) || strings.Contains(cleaned, "\n") {
			t.Fatalf("citation cleaned to %q", cleaned)
		}
		markup := renderMarkdownBody(t, m, cleaned)
		if !strings.Contains(markup, `class="agent-cite"`) || !strings.Contains(markup, `aria-label="Paid time off policy · Carryover · v1.0.0"`) ||
			!strings.Contains(markup, `title="Paid time off policy · Carryover · v1.0.0"`) || !strings.Contains(markup, ">[1]</a>.") || strings.Contains(markup, "<br") || strings.Contains(markup, "<p>.") {
			t.Fatalf("citation marker markup: %s", markup)
		}
		// The same document cited twice is the same number; a second one is the next.
		two := "First point\n[Paid time off policy · Carryover · v1.0.0](http://x/workspace/app/docs?document=pto)\n.\n\nSecond point\n[2026 holiday guide · v1.0.0](http://x/workspace/app/docs?document=holidays)\n.\n\nThird\n[Paid time off policy · Carryover · v1.0.0](http://x/workspace/app/docs?document=pto)."
		cleaned = agentUX075CleanAnswer(two, agentUX075Sources())
		if strings.Count(cleaned, `\[1\]`) != 2 || strings.Count(cleaned, `\[2\]`) != 1 {
			t.Fatalf("numbering: %q", cleaned)
		}
	})
	t.Run("punctuation attaches to the sentence whatever space precedes it", func(t *testing.T) {
		link := "[Paid time off policy · Carryover · v1.0.0](http://x/workspace/app/docs?document=pto)"
		marker := `[\[1\]](http://x/workspace/app/docs?document=pto "Paid time off policy · Carryover · v1.0.0")`
		spaces := []string{"", " ", "\n", "\n\n", " \n ", string(rune(0xa0)), string(rune(0x202f)), string(rune(0x2009)), string(rune(0x200b)) + " "}
		for _, before := range spaces {
			for _, after := range spaces {
				for _, mark := range []string{".", ",", "،", "…"} {
					body := "Employees may carry over 40 hours" + before + link + after + mark
					want := "Employees may carry over 40 hours" + marker + mark
					if got := agentUX075CleanAnswer(body, agentUX075Sources()); got != want {
						t.Errorf("before %q after %q mark %q: got %q, want %q", before, after, mark, got, want)
					}
				}
			}
		}
		markup := renderMarkdownBody(t, m, agentUX075CleanAnswer("Employees may carry over 40 hours"+string(rune(0x202f))+link+string(rune(0xa0))+".", agentUX075Sources()))
		if !strings.Contains(markup, ">[1]</a>.") {
			t.Errorf("a space reached the page between the marker and the full stop: %s", markup)
		}
	})
	t.Run("links that are not citations are left alone", func(t *testing.T) {
		for _, body := range []string{
			"Per [Paid time off policy](http://x/workspace/app/docs?document=pto) you carry over 40 hours.",
			"[Paid time off policy · Carryover · v1.0.0](http://x/workspace/app/docs?document=pto) opens the answer.",
			"Documents:\n- [Paid time off policy](http://x/workspace/app/docs?document=pto)\n- [2026 holiday guide](http://x/workspace/app/docs?document=holidays)",
		} {
			if got := agentUX075CleanAnswer(body, agentUX075Sources()); got != body {
				t.Errorf("%q was rewritten to %q", body, got)
			}
		}
		// With no Sources row the title is the only place it is named: keep it.
		body := "Text\n[Paid time off policy · Carryover · v1.0.0](http://x/workspace/app/docs?document=pto)."
		if got := agentUX075CleanAnswer(body, nil); got != body {
			t.Errorf("citation without a Sources row was rewritten: %q", got)
		}
	})
	t.Run("a source named in brackets at the end is dropped", func(t *testing.T) {
		list := "Upcoming company holidays remaining in 2026:\n- Labor Day — Sep 7\n- Christmas Day — Dec 25\n(2026 holiday guide)"
		if got := agentUX075CleanAnswer(list, agentUX075Sources()); got != "Upcoming company holidays remaining in 2026:\n- Labor Day — Sep 7\n- Christmas Day — Dec 25" {
			t.Errorf("trailing source name kept: %q", got)
		}
		if got := agentUX075CleanAnswer("Christmas Day — Dec 25 (2026 holiday guide).", agentUX075Sources()); got != "Christmas Day — Dec 25" {
			t.Errorf("source name at the end of the last line kept: %q", got)
		}
		// A name with no listed source stays, and an answer that is only a name stays.
		if got := agentUX075CleanAnswer("Christmas Day — Dec 25\n(Employee handbook)", agentUX075Sources()); !strings.Contains(got, "(Employee handbook)") {
			t.Errorf("an unlisted source name was dropped: %q", got)
		}
		if got := agentUX075CleanAnswer("(2026 holiday guide)", agentUX075Sources()); got != "(2026 holiday guide)" {
			t.Errorf("an answer that is only a name was emptied: %q", got)
		}
	})
}

// The working message names what the agent is doing, in the viewer's language,
// with moving dots and the elapsed time, and the formatted answer draws in each.
func TestTodo_AGENTUX_075_Browser(t *testing.T) {
	for _, tc := range []struct {
		locale   string
		searches string
		reading  string
		writing  string
	}{
		{"en-US", "Searching documents…", "Reading " + agentUX075FSI + "Holiday guide 2026" + agentUX075PDI + "…", "Writing the answer…"},
		{"de-DE", "Dokumente werden durchsucht…", agentUX075FSI + "Holiday guide 2026" + agentUX075PDI + " wird gelesen…", "Die Antwort wird geschrieben…"},
		{"ar", "جارٍ البحث في المستندات…", "جارٍ قراءة " + agentUX075FSI + "Holiday guide 2026" + agentUX075PDI + "…", "جارٍ كتابة الإجابة…"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			model := Model{Locale: tc.locale, Callbacks: Callbacks{CancelPersonaInvocation: func(string) {}}}
			for activity, want := range map[string]string{"Preparing answer": tc.searches, "Reading Holiday guide 2026": tc.reading, "Delivering answer": tc.writing} {
				projection := PersonaProgressProjection{ViewerID: "alice", InvokerID: "alice", AgentName: "Assistant",
					Progress: &PersonaProgressProps{InvocationID: "run-1", InvokerID: "alice", AgentName: "Assistant", Activity: activity, Visible: true, ElapsedSeconds: 12}}
				markup := renderAgentUXChat3Node(t, RenderPersonaProgress(model, projection), 390)
				for _, must := range []string{want, "agent-working-dots", "agent-progress-cancel", ">" + chatNumeral(tc.locale, "0:12") + "<", `data-agent-reply-state="working"`, "Assistant"} {
					if !strings.Contains(markup, must) {
						t.Errorf("%s %q: working message lacks %q: %s", tc.locale, activity, must, markup)
					}
				}
				if strings.Contains(markup, "chat.agent.") {
					t.Errorf("%s: an untranslated key reached the page: %s", tc.locale, markup)
				}
				if rtl := strings.Contains(markup, `dir="rtl"`); rtl != (tc.locale == "ar") {
					t.Errorf("%s: direction rtl=%v", tc.locale, rtl)
				}
			}
			// A stage nobody named leaves the generic line, never the server's word.
			projection := PersonaProgressProjection{ViewerID: "alice", InvokerID: "alice", AgentName: "Assistant",
				Progress: &PersonaProgressProps{InvocationID: "run-1", InvokerID: "alice", Activity: "ZXQ-17", Visible: true}}
			if markup := renderAgentUXChat3Node(t, RenderPersonaProgress(model, projection), 390); strings.Contains(markup, "ZXQ-17") {
				t.Errorf("%s: an unknown activity was printed: %s", tc.locale, markup)
			}
			// The answer is formatted text in this language too.
			answer := "## Holidays\n\n- **Labor Day** — Sep 7\n- Christmas Day — Dec 25\n\n| Day | Date |\n|---|---|\n| Labor Day | Sep 7 |\n"
			markup := renderMarkdownBody(t, Model{Locale: tc.locale}, agentUX075AnswerBody(agentUX075Actor(), answer, agentUX075Sources()))
			for _, want := range []string{"<h4>Holidays</h4>", "<strong>Labor Day</strong>", "<ul>", "<table"} {
				if !strings.Contains(markup, want) {
					t.Errorf("%s: answer lacks %q: %s", tc.locale, want, markup)
				}
			}
			if strings.Contains(markup, "**") || strings.Contains(markup, "|---") {
				t.Errorf("%s: raw markup on the page: %s", tc.locale, markup)
			}
		})
	}
}
