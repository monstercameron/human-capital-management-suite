package chatui

import (
	"net/url"
	"strings"
	"unicode"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATUX-017: an agent's details in a conversation read like a system record:
// an owner identifier, a version number, the technical descriptions of its
// skills. A person deciding whether to ask the agent needs five plain things:
// what it is for, who looks after it, what it can read, what it will never do,
// and where its answers go. The version and the skills are on the agent's page
// in Agent setup, linked as "Details" for the people who may open it.

// chatux017AgentSummary is that summary. It is the body of the agent's row in
// Conversation details and the small card opened from an agent's name.
func chatux017AgentSummary(m Model, persona ResolvedPersonaMention) ui.Node {
	return chatux017AgentSummaryWith(m, persona, true)
}

// chatux017AgentSummaryWith is the summary with or without the sentence about
// where answers are posted. The member list says it once, in its first agent
// block, instead of under every agent (CHATUX-030).
func chatux017AgentSummaryWith(m Model, persona ResolvedPersonaMention, withAnswers bool) ui.Node {
	text := func(key string) string { return chatux017Text(m.Locale, key) }
	rows := []ui.Node{}
	if purpose := strings.TrimSpace(persona.Purpose); purpose != "" {
		rows = append(rows, html.P(html.Props{Class: "agent-summary-purpose", Dir: "auto", Text: purpose}))
	}
	// Who looks after it: a person's name, as a link to that person. The
	// definition may name a team in words instead ("People Operations"), which
	// is shown as written. An identifier is never shown in a name's place.
	if owner := strings.TrimSpace(persona.Owner); owner != "" {
		if name := chatux003PersonName(m, owner); name != "" {
			rows = append(rows, html.P(html.Props{Class: "agent-summary-owner"}, html.Span(html.Props{Text: text("looked_after") + " "}),
				personButton(m, owner, name, "agent-summary-person person-name", ui.Text(name))))
		} else if chatux017WrittenName(owner) {
			rows = append(rows, html.P(html.Props{Class: "agent-summary-owner", Dir: "auto", Text: text("looked_after") + " " + owner}))
		}
	}
	// AGENTP-019: the version, what the agent does and the access it acts with
	// are on this card as they are on the mention menu's.
	if version := agentp019Version(m.Locale, persona, false); version != nil {
		rows = append(rows, version)
	}
	rows = append(rows, agentp019Access(m.Locale))
	if len(persona.Skills) > 0 {
		rows = append(rows, agentp019Skills(m.Locale, persona))
	}
	if reads := chatux017Reads(m.Locale, persona.DataClasses); len(reads) > 0 {
		rows = append(rows, chatux017List(text("reads"), "agent-summary-reads", reads))
	}
	if never := chatux017Never(m.Locale, persona.CannotDo); len(never) > 0 {
		rows = append(rows, chatux017List(text("never"), "agent-summary-never", never))
	}
	if answers := chatux017Answers(m.Locale, persona.ReplyPlacement); answers != "" && withAnswers {
		rows = append(rows, html.P(html.Props{Class: "agent-summary-answers", Dir: "auto", Text: answers}))
	}
	if m.IsTenantAdmin || (m.CurrentUser != "" && m.selected().OwnerID == m.CurrentUser) {
		rows = append(rows, html.A(html.Props{Class: "agent-summary-details", Href: "/workspace/app/admin/personas?locale=" + url.QueryEscape(m.Locale), Text: text("details"),
			Aria: map[string]string{"label": text("details") + ": " + persona.Reference.Display}}))
	}
	return html.Article(html.Props{Class: "agent-summary", Role: "region", Dir: agentReplyDirection(m.Locale), Aria: map[string]string{"label": personaMentionText(m.Locale, "profile")}}, rows...)
}

// chatux017WrittenName reports whether an owner the definition gives is a name
// written out in words rather than an identifier: it has a space between
// words and no digit. "People Operations" is; "ir-001-walt-brennan" is not.
func chatux017WrittenName(owner string) bool {
	if !strings.Contains(owner, " ") {
		return false
	}
	for _, r := range owner {
		if unicode.IsDigit(r) || r == '_' || r == ':' || r == '@' {
			return false
		}
	}
	return true
}

func chatux017List(heading, class string, lines []string) ui.Node {
	items := make([]ui.Node, 0, len(lines))
	for _, line := range lines {
		items = append(items, html.Li(html.Props{Dir: "auto", Text: line}))
	}
	return html.Section(html.Props{Class: "agent-summary-section " + class, Aria: map[string]string{"label": heading}},
		html.H4(html.Props{Text: heading}), html.Ul(html.Props{}, items...))
}

// chatux017Reads says what the agent can read, in the reader's terms: it reads
// with the reader's own access, so it reads what they can open.
func chatux017Reads(locale string, classes []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, class := range classes {
		key := ""
		switch {
		case class == "POLICY_DOCUMENT":
			key = "reads_policy"
		case strings.Contains(strings.ToUpper(class), "DOCUMENT"):
			key = "reads_documents"
		}
		if key != "" && !seen[key] {
			seen[key] = true
			out = append(out, chatux017Text(locale, key))
		}
	}
	return out
}

// chatux017Never is what the agent will never do, in at most three short
// lines. The server names each limit; a limit this page has no plain words for
// is left out rather than shown in the server's.
func chatux017Never(locale string, limits []string) []string {
	keys := map[string]string{
		"act beyond your current access":            "never_access",
		"use skills outside this published version": "never_setup",
		"change governed records":                   "never_records",
		"write to external systems":                 "never_external",
	}
	var out []string
	for _, limit := range limits {
		if key, known := keys[strings.ToLower(strings.TrimSpace(limit))]; known && len(out) < 3 {
			out = append(out, chatux017Text(locale, key))
		}
	}
	return out
}

// chatux017Answers says where the agent's answers go.
func chatux017Answers(locale string, placement PersonaReplyPlacement) string {
	switch placement {
	case PersonaReplyInThread:
		return chatux017Text(locale, "answers_thread")
	case PersonaReplyPrivateAlways:
		return chatux017Text(locale, "answers_private")
	case PersonaReplyPrivateAudience:
		return chatux017Text(locale, "answers_audience")
	}
	return ""
}

// chatux017Identity is an agent's icon and name on a card. Pressing them opens
// the agent's summary; when the agent cannot be established by id they stay
// plain.
func chatux017Identity(m Model, agentID, name string, avatar ui.Node) []ui.Node {
	if agentID == "" {
		return []ui.Node{avatar, html.Strong(html.Props{Class: "agent-reply-name", Text: name})}
	}
	return []ui.Node{html.Button(html.Props{Class: "agent-summary-open", Type: "button", Data: map[string]string{"action": "agent-profile-open", "id": agentID},
		Aria: map[string]string{"label": name + ", " + personaMentionText(m.Locale, "profile"), "haspopup": "dialog"}}, avatar, html.Strong(html.Props{Class: "agent-reply-name", Text: name}))}
}

func chatux017Text(locale, key string) string {
	copy := map[string][3]string{
		"looked_after":     {"Owner:", "Verantwortlich:", "المسؤول:"},
		"reads":            {"What it can read", "Was er lesen kann", "ما يمكنه قراءته"},
		"reads_policy":     {"Policy documents you can open", "Richtliniendokumente, die Sie öffnen können", "مستندات السياسات التي يمكنك فتحها"},
		"reads_documents":  {"Documents you can open", "Dokumente, die Sie öffnen können", "المستندات التي يمكنك فتحها"},
		"never":            {"What it will never do", "Was er niemals tut", "ما لن يفعله أبدًا"},
		"never_access":     {"Read anything you cannot open yourself", "Etwas lesen, das Sie selbst nicht öffnen können", "قراءة ما لا يمكنك فتحه بنفسك"},
		"never_setup":      {"Do anything it was not set up to do", "Etwas tun, wofür er nicht eingerichtet wurde", "القيام بما لم يُعدّ للقيام به"},
		"never_records":    {"Change any record", "Datensätze ändern", "تغيير أي سجل"},
		"never_external":   {"Send anything outside this workspace", "Etwas außerhalb dieses Arbeitsbereichs senden", "إرسال أي شيء خارج مساحة العمل هذه"},
		"answers_thread":   {"Its answers are posted under your question.", "Antworten erscheinen unter Ihrer Frage.", "تُنشر إجاباته تحت سؤالك."},
		"answers_private":  {"Its answers are shown only to you.", "Antworten sehen nur Sie.", "تظهر إجاباته لك وحدك."},
		"answers_audience": {"Its answers are posted under your question when everyone here can open the sources; otherwise only you see them.", "Antworten erscheinen unter Ihrer Frage, wenn alle hier die Quellen öffnen können; sonst sehen nur Sie sie.", "تُنشر إجاباته تحت سؤالك عندما يستطيع الجميع هنا فتح المصادر؛ وإلا تراها أنت وحدك."},
		"details":          {"Details", "Details", "التفاصيل"},
	}
	return chatbug039Text(key, copy[key][chatbug039LocaleIndex(locale)], copy[key][0])
}

// chatUX017Styles sets the summary in the panel's 12px text, keeps Ask on the
// agent's header row while the row is open, and shows the agent's description
// once: in the summary when it is open, under the name when it is not.
const chatUX017Styles = `.agent-summary{display:grid;gap:6px;padding:6px 0 2px;font-size:.75rem;line-height:1.45;overflow-wrap:anywhere;max-width:100%}` +
	`.agent-summary p{margin:0}` +
	`.agent-summary-section h4{margin:0 0 2px;font-size:.75rem;font-weight:600;color:var(--muted)}` +
	`.agent-summary-section ul{margin:0;padding-inline-start:16px}` +
	`.agent-summary-person{padding:0;border:0;background:transparent;color:var(--accent);font:inherit;cursor:pointer;text-decoration:underline}` +
	`.agent-summary-details{color:var(--accent);justify-self:start}` +
	`.member-row.persona-member-row{align-items:flex-start}` +
	`.persona-member-row:has([data-chat-disclosure-body]:not([hidden]))>.persona-member-purpose{display:none}` +
	`.agent-profile-dialog .agent-summary{padding:8px 12px 12px;font-size:.8125rem}` +
	`.agent-summary-open{display:inline-flex;align-items:center;gap:8px;flex:0 1 auto;min-width:0;padding:0;border:0;background:transparent;color:inherit;font:inherit;cursor:pointer;text-align:start}` +
	`.agent-summary-open:hover strong,.agent-summary-open:focus-visible strong{text-decoration:underline}`
