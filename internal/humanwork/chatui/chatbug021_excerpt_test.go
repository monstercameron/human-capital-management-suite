package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

// Two stretches of a post as the search service hands them back: an
// announcement envelope cut in the middle of its fields (its line break
// flattened to a space, as an excerpt is), and a reply cut in the middle of two
// Markdown links.
const (
	chatbug021CutEnvelope = `<hcm_agent_announcement> {"AgentName":"Agent","OwnerName":"Walt Brennan","Text":"Upcoming company holidays remaining in 2026:\n- Labor Day — Sep 7\n- Thanksgiving Day — Nov 26\n- Christmas Day — Dec 25\n(2026 holiday guide)","Scheduled":false,"PostedAt":"0001-01-`
	chatbug021CutInText   = `<hcm_agent_announcement>` + "\n" + `{"AgentName":"Agent","OwnerName":"Walt Brennan","Text":"Upcoming company holidays remaining in 2026:\n- Thanksgiving Day — Nov 26\n- Chris`
	chatbug021CutLinks    = "...off policy · Carryover · v1.0.0](/workspace/app/docs?document=doc-64271829&version=docv-1#carryover) · [2026 holiday guide · v1.0.0](/workspace/app/docs?document=doc-10c773e5&version=docv-2)"
	chatbug021CutTail     = "Paid time off policy. [2026 holiday guide · v1.0.0](/workspace/app/do..."
	chatbug021CutOpen     = "See [2026 holiday guide · v1.0.0…"
)

// chatbug021Raw is what no excerpt may show.
var chatbug021Raw = []string{"hcm_agent_announcement", "AgentName", "OwnerName", `"Text"`, "{", "}", "0001-01-", "](", "[", "/workspace/app/", "Scheduled"}

func chatbug021Clean(t *testing.T, where, shown string) {
	t.Helper()
	for _, raw := range chatbug021Raw {
		if strings.Contains(shown, raw) {
			t.Errorf("%s prints %q: %s", where, raw, shown)
		}
	}
}

// TestTodo_CHATBUG_021_Excerpts goes through every place that shows a stretch of
// a message: the search result rows (for a message and for a thread reply), the
// older search snippet, the to-do source and poll lines, the moderation list and
// the quote of a question. None prints the envelope of an announcement, a half
// Markdown link or a link's address.
func TestTodo_CHATBUG_021_Excerpts(t *testing.T) {
	for name, body := range map[string]string{"cut envelope": chatbug021CutEnvelope, "cut inside the text": chatbug021CutInText} {
		sentence, announced := chatAnnouncementSentence(body)
		if !announced || !strings.HasPrefix(sentence, "Upcoming company holidays remaining in 2026:") {
			t.Fatalf("%s: the sentence is %q (announced %v)", name, sentence, announced)
		}
		for surface, shown := range map[string]string{
			"chatDisplayText":  chatDisplayText(body),
			"chatDisplayBody":  chatDisplayBody(body),
			"chatExcerptText":  chatExcerptText(body),
			"excerpt":          excerpt(body, 400),
			"searchSnippet":    searchSnippet(body, "holiday", 400),
			"moderation list":  chatmodExcerpt(body),
			"search row":       chatbug021SearchRow(t, chatsearch.Message, body),
			"thread reply row": chatbug021SearchRow(t, chatsearch.Thread, body),
		} {
			chatbug021Clean(t, name+" / "+surface, shown)
			if !strings.Contains(shown, "Upcoming company") {
				t.Errorf("%s / %s lost the sentence: %s", name, surface, shown)
			}
		}
	}
	if shown := chatDisplayText(chatbug021CutEnvelope); !strings.Contains(shown, "Thanksgiving Day — Nov 26") || !strings.Contains(shown, "(2026 holiday guide)") {
		t.Errorf("the sentence lost its lines: %q", shown)
	}
	if shown := chatDisplayText(chatbug021CutInText); !strings.HasSuffix(shown, "…") {
		t.Errorf("a sentence cut by the service does not say so: %q", shown)
	}

	// Links cut by the excerpt: the words stay, the syntax and the address go.
	for name, body := range map[string]string{"start cut away": chatbug021CutLinks, "end cut away": chatbug021CutTail, "never closed": chatbug021CutOpen} {
		for surface, shown := range map[string]string{
			"chatExcerptText": chatExcerptText(body),
			"excerpt":         excerpt(body, 400),
			"searchSnippet":   searchSnippet(body, "holiday", 400),
			"moderation list": chatmodExcerpt(body),
			"search row":      chatbug021SearchRow(t, chatsearch.Message, body),
			"thread reply":    chatbug021SearchRow(t, chatsearch.Thread, body),
		} {
			if surface == "search row" || surface == "thread reply" {
				// The result row is the message renderer, which draws a complete link as a
				// link: what must be gone is the half of a link the cut left.
				for _, half := range []string{"](", "[2026 holiday guide · v1.0.0](/workspace/app/do", "[2026"} {
					if strings.Contains(chatbug021Visible(shown), half) && !(strings.HasPrefix(body, "...") && half == "[2026") {
						t.Errorf("%s / %s prints the half link %q: %s", name, surface, half, chatbug021Visible(shown))
					}
				}
				continue
			}
			chatbug021Clean(t, name+" / "+surface, shown)
		}
	}
	if shown := chatExcerptText(chatbug021CutLinks); shown != "...off policy · Carryover · v1.0.0 · 2026 holiday guide · v1.0.0" {
		t.Errorf("a stretch cut out of two links reads %q", shown)
	}
	if shown := chatExcerptText(chatbug021CutTail); shown != "Paid time off policy. 2026 holiday guide · v1.0.0" {
		t.Errorf("a link cut off at its end reads %q", shown)
	}
	// A person's own words are not altered by the display function.
	for _, plain := range []string{"Use array[0] and x[1] here.", "Read [the guide](https://example.com) today", "Totals: 4] items", "Nothing special"} {
		if got := chatDisplayBody(plain); got != plain {
			t.Errorf("chatDisplayBody changed a person's words: %q -> %q", plain, got)
		}
	}
	// A question typed with a link is quoted as its words.
	direct := chatbug021DirectFor("Is [the PTO policy](/workspace/app/docs?document=doc-1) current?")
	quote := chatbug021Visible(renderNode(t, chatbug047QuestionQuote(direct, direct.Messages[2], false)))
	if !strings.Contains(quote, "Is the PTO policy current?") || strings.Contains(quote, "](") {
		t.Errorf("the quote of a question reads %q", quote)
	}
}

// chatbug021SearchRow renders one search result row of a kind and returns what
// a reader sees.
func chatbug021SearchRow(t *testing.T, kind chatsearch.Kind, text string) string {
	t.Helper()
	view := ChatSearchView{Query: "holiday", Response: chatsearch.Response{Groups: []chatsearch.Group{{Kind: kind, Count: 1, Rows: []chatsearch.Row{{Kind: kind, ID: "row", Text: text, AuthorID: "assistant", At: time.Now(), Target: chatsearch.Target{ConversationID: "general", MessageID: "row"}}}}}}}
	return chatbug021Visible(renderNode(t, RenderChatSearch("en-US", view)))
}

// chatbug021DirectFor is the Policy Helper conversation where the answer to
// "question" does not directly follow it, so the answer carries a quote.
func chatbug021DirectFor(question string) Model {
	m, _ := agentux051Direct("Up to 40 hours.")
	m.Messages = []Message{
		{ID: "q1", Sequence: 4, AuthorID: "walt", Author: "Walt", Body: question, TimeLabel: "8:05", SentAt: time.Now().Add(-6 * time.Hour)},
		{ID: "q2", AuthorID: "walt", Author: "Walt", Body: "another question", TimeLabel: "11:18", SentAt: time.Now().Add(-3 * time.Hour)},
		{ID: "answer", AuthorID: "policy-helper", Author: "Policy Helper", Body: "Up to 40 hours.", PersonaActor: &PersonaActor{PersonaID: "policy-helper", AgentID: "policy-helper", Trusted: true}},
	}
	m.PersonaInvocations = []PersonaThreadInvocation{{PostID: "q1", ThreadID: "q1", Projection: PersonaProgressProjection{InvocationID: "run", ViewerID: "walt", InvokerID: "walt", AgentName: "Policy Helper", DurablePostID: "answer"}}}
	m.Callbacks.OpenSearchMessage = func(string, string, uint64) {}
	return m
}
