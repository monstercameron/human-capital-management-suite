package chatui

import (
	"sort"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/localize"
)

// Issue 3986: "What needs my attention?" in the person's own conversation with
// an agent. On request, and only then, the page reads the work the person is
// responsible for from its owning service, and draws a private brief:
// what asks for the person's action, what waits on others, and what the service
// cannot place. Dates and statuses are the work item's own; the order is only a
// suggestion. Reading changes nothing: the brief has links, never a decision.
// Nothing leaves the person's screen unless they put the brief in their message
// box and send it themselves, in this private conversation.

const (
	// AttentionAction is work that asks something of the viewer now.
	AttentionAction = "action"
	// AttentionWaiting is work the viewer follows that waits on someone else.
	AttentionWaiting = "waiting"
	// AttentionUnknown is work whose responsibility the owning service did not state.
	AttentionUnknown = "unknown"

	// AttentionLoading, AttentionReady and AttentionFailed are the read phases.
	AttentionLoading = "loading"
	AttentionReady   = "ready"
	AttentionFailed  = "failed"

	// attentionStaleAfter is how long a read is trusted before it says it may be old.
	attentionStaleAfter = 5 * time.Minute

	issue3986OpenAction   = "attention-open"
	issue3986CloseAction  = "attention-close"
	issue3986ReadAction   = "attention-read"
	issue3986FilterAction = "attention-filter"
	issue3986DraftAction  = "attention-draft"
)

// AttentionItem is one piece of the person's work as its owner reports it.
type AttentionItem struct {
	ID, Title, Source, Status string
	// Href is the work item's own page. Only same-site paths are drawn as links.
	Href string
	// Due is the owner's deadline; zero means the owner set none.
	Due time.Time
	// Responsibility is AttentionAction, AttentionWaiting or AttentionUnknown.
	Responsibility string
	// Changed names what moved since the previous read: "responsibility", "due",
	// "status", or "" when nothing did.
	Changed string
}

// AttentionBrief is the last read of the person's work.
type AttentionBrief struct {
	Phase    string
	Items    []AttentionItem
	Gone     []AttentionItem
	LoadedAt time.Time
	// Total is how many items the owner says the person may see; when it is more
	// than Items, the read was partial.
	Total int
}

// issue3986UI is the panel's own state: open, and the filter that leaves
// waiting items out.
type issue3986UI struct {
	Open, NoWaiting bool
}

// AttentionMerge compares a fresh read with the previous one. Each item says
// what moved, and items that are no longer in the person's work are returned
// separately so a change of authority is shown rather than silently dropped.
func AttentionMerge(prev AttentionBrief, next []AttentionItem) (items, gone []AttentionItem) {
	before := map[string]AttentionItem{}
	for _, item := range prev.Items {
		before[item.ID] = item
	}
	items = make([]AttentionItem, 0, len(next))
	seen := map[string]bool{}
	for _, item := range next {
		seen[item.ID] = true
		item.Changed = ""
		if old, ok := before[item.ID]; ok {
			switch {
			case old.Responsibility != item.Responsibility:
				item.Changed = "responsibility"
			case !old.Due.Equal(item.Due):
				item.Changed = "due"
			case old.Status != item.Status:
				item.Changed = "status"
			}
		}
		items = append(items, item)
	}
	for _, item := range prev.Items {
		if !seen[item.ID] {
			item.Changed = ""
			gone = append(gone, item)
		}
	}
	return items, gone
}

// issue3986Where reports whether the open conversation is the person's own,
// joined conversation with an agent in this tenant, with a way to read.
func issue3986Where(m Model) bool {
	if m.Callbacks.RequestAttention == nil {
		return false
	}
	c := m.selected()
	if c.ID == "" || c.Kind != DirectMessage || !c.Agent || !c.Joined {
		return false
	}
	return c.HostTenantID == "" || c.HostTenantID == m.CurrentTenantID
}

// issue3986Step decides what one press does. effect is "open", "close", "read"
// or "" when nothing further happens.
func issue3986Step(m Model, state issue3986UI, action string) (issue3986UI, string) {
	switch action {
	case issue3986OpenAction:
		state.Open = true
		return state, "open"
	case issue3986CloseAction:
		state.Open = false
		return state, "close"
	case issue3986FilterAction:
		state.NoWaiting = !state.NoWaiting
		return state, ""
	case issue3986ReadAction:
		if m.Callbacks.RequestAttention == nil {
			return state, ""
		}
		return state, "read"
	}
	return state, ""
}

// issue3986Click handles the panel's presses and reports whether it was one.
func issue3986Click(e ui.Event, m Model, local localStore) bool {
	action, _, _ := eventAction(e)
	if !strings.HasPrefix(action, "attention-") {
		return false
	}
	if action == issue3986DraftAction {
		e.PreventDefault()
		issue3986ToDraft(m, local.get().attention)
		return true
	}
	next, effect := issue3986Step(m, local.get().attention, action)
	if action != issue3986FilterAction {
		e.PreventDefault()
	}
	local.update(func(u *localUI) { u.attention = next })
	switch effect {
	case "open":
		chatux026Focus(`.attention-brief [data-action="` + issue3986ReadAction + `"]`)
	case "close":
		chatux026Focus(`.attention-brief [data-action="` + issue3986OpenAction + `"]`)
	case "read":
		chatux026Focus(`.attention-brief[data-attention="open"]`)
		m.Callbacks.RequestAttention()
	}
	return true
}

// issue3986ToDraft puts the brief in the message box after anything already
// typed. It sends nothing.
func issue3986ToDraft(m Model, state issue3986UI) {
	text := issue3986DraftText(m, state)
	if text == "" || m.SelectedID == "" {
		return
	}
	current := domDraftValue("chat-composer", m.SelectedID)
	if current == "" {
		current = m.Draft
	}
	text = issue3977Merge(current, text)
	if m.Callbacks.DraftChanged != nil {
		m.Callbacks.DraftChanged(m.SelectedID, text)
	}
	replaceComposerText("chat-composer", text, len(utf16.Encode([]rune(text))))
}

// issue3986Groups splits a read into the three sections, due-first inside each.
func issue3986Groups(m Model, state issue3986UI) (action, waiting, unknown []AttentionItem, leftOut int) {
	for _, item := range m.Attention.Items {
		switch item.Responsibility {
		case AttentionAction:
			action = append(action, item)
		case AttentionWaiting:
			if state.NoWaiting {
				leftOut++
				continue
			}
			waiting = append(waiting, item)
		default:
			unknown = append(unknown, item)
		}
	}
	for _, group := range [][]AttentionItem{action, waiting, unknown} {
		sort.SliceStable(group, func(i, j int) bool {
			a, b := group[i].Due, group[j].Due
			if a.IsZero() != b.IsZero() {
				return !a.IsZero()
			}
			return !a.IsZero() && a.Before(b)
		})
	}
	return action, waiting, unknown, leftOut
}

func issue3986Dues(m Model, item AttentionItem) string {
	if item.Due.IsZero() {
		return issue3986Text(m, "no_due")
	}
	key := "due"
	if item.Due.Before(time.Now()) {
		key = "overdue"
	}
	return issue3986Format(m, key, map[string]string{"date": formatShortDate(m.Locale, item.Due.Local())})
}

// issue3986DraftText is the brief as plain text, or "" before any read.
func issue3986DraftText(m Model, state issue3986UI) string {
	if m.Attention.Phase != AttentionReady {
		return ""
	}
	action, waiting, unknown, _ := issue3986Groups(m, state)
	var out []string
	out = append(out, issue3986Text(m, "open")+" ("+issue3986Format(m, "read_at", map[string]string{"time": localize.FormatClock(m.Locale, m.Attention.LoadedAt.Local())})+")")
	section := func(key string, items []AttentionItem) {
		if len(items) == 0 {
			return
		}
		out = append(out, "", issue3986Text(m, "draft_"+key))
		for _, item := range items {
			line := "- " + item.Title
			if item.Status != "" {
				line += " | " + item.Status
			}
			line += " | " + issue3986Dues(m, item)
			if issue3986SafeHref(item.Href) {
				line += " | " + item.Href
			}
			out = append(out, line)
		}
	}
	section("action", action)
	section("waiting", waiting)
	section("unknown", unknown)
	if len(out) == 1 {
		out = append(out, "", issue3986Text(m, "empty"))
	}
	return strings.Join(out, "\n")
}

// issue3986SafeHref allows only a path on this site.
func issue3986SafeHref(href string) bool {
	return strings.HasPrefix(href, "/") && !strings.HasPrefix(href, "//") && !strings.Contains(href, "\\")
}

func issue3986Button(m Model, action, key string, primary bool) ui.Node {
	class := "button small"
	if !primary {
		class = "button secondary small"
	}
	return html.Button(html.Props{Class: class, Type: "button", Data: map[string]string{"action": action}, Text: issue3986Text(m, key)})
}

// issue3986Panel is the offer, the scope review and the brief, drawn above the
// message box of the person's own agent conversation.
func issue3986Panel(m Model, local localUI) ui.Node {
	if !issue3986Where(m) {
		return nil
	}
	state := local.attention
	title := issue3986Text(m, "open")
	if !state.Open {
		return html.Div(html.Props{Class: "attention-brief attention-closed", Data: map[string]string{"attention": "closed"}},
			html.Button(html.Props{Class: "attention-offer", Type: "button", Aria: map[string]string{"expanded": "false"}, Data: map[string]string{"action": issue3986OpenAction}, Text: title}))
	}
	brief := m.Attention
	children := []ui.Node{html.H4(html.Props{Class: "attention-title", Text: title})}
	phase := brief.Phase
	switch phase {
	case AttentionLoading:
		children = append(children, html.P(html.Props{Class: "attention-status", Role: "status", Text: issue3986Text(m, "loading")}))
	case AttentionFailed:
		children = append(children,
			html.P(html.Props{Class: "attention-failed", Role: "alert", Dir: "auto", Text: issue3986Text(m, "failed")}),
			html.Div(html.Props{Class: "attention-actions"}, issue3986Button(m, issue3986ReadAction, "retry", true), issue3986Button(m, issue3986CloseAction, "close", false)))
	case AttentionReady:
		children = append(children, issue3986Result(m, state)...)
	default:
		children = append(children, issue3986Scope(m, state)...)
		phase = ""
	}
	if phase == AttentionLoading {
		children = append(children, html.Div(html.Props{Class: "attention-actions"}, issue3986Button(m, issue3986CloseAction, "close", false)))
	}
	return html.Div(html.Props{Class: "attention-brief", Role: "region", TabIndex: -1, Aria: map[string]string{"label": title}, Data: map[string]string{"attention": "open", "phase": phase}}, children...)
}

// issue3986Scope says what a read will use, before anything is read.
func issue3986Scope(m Model, state issue3986UI) []ui.Node {
	return []ui.Node{
		html.P(html.Props{Class: "attention-note", Dir: "auto", Text: issue3986Text(m, "scope_intro")}),
		html.Ul(html.Props{Class: "attention-scope"},
			html.Li(html.Props{Dir: "auto", Text: issue3986Text(m, "scope_source")}),
			html.Li(html.Props{Dir: "auto", Text: issue3986Text(m, "scope_range")})),
		issue3986Filter(m, state),
		html.P(html.Props{Class: "attention-note", Dir: "auto", Text: issue3986Text(m, "private")}),
		html.Div(html.Props{Class: "attention-actions"}, issue3986Button(m, issue3986ReadAction, "read", true), issue3986Button(m, issue3986CloseAction, "close", false)),
	}
}

func issue3986Filter(m Model, state issue3986UI) ui.Node {
	return html.Label(html.Props{Class: "attention-filter", For: "attention-filter"},
		html.Input(html.Props{ID: "attention-filter", Type: "checkbox", Checked: !state.NoWaiting, Data: map[string]string{"action": issue3986FilterAction}}),
		html.Span(html.Props{Text: issue3986Text(m, "filter")}))
}

func issue3986Result(m Model, state issue3986UI) []ui.Node {
	brief := m.Attention
	action, waiting, unknown, leftOut := issue3986Groups(m, state)
	nodes := []ui.Node{html.P(html.Props{Class: "attention-status", Role: "status", Dir: "auto",
		Text: issue3986Format(m, "read_at", map[string]string{"time": localize.FormatClock(m.Locale, brief.LoadedAt.Local())})})}
	if !brief.LoadedAt.IsZero() && time.Since(brief.LoadedAt) > attentionStaleAfter {
		nodes = append(nodes, html.P(html.Props{Class: "attention-stale", Role: "status", Text: issue3986Text(m, "stale")}))
	}
	if brief.Total > len(brief.Items) {
		nodes = append(nodes, html.P(html.Props{Class: "attention-partial", Role: "status", Text: issue3986Format(m, "partial", map[string]string{"shown": itoa(len(brief.Items)), "total": itoa(brief.Total)})}))
	}
	nodes = append(nodes, issue3986Filter(m, state))
	if leftOut > 0 {
		nodes = append(nodes, html.P(html.Props{Class: "attention-note", Text: issue3986Format(m, "left_out", map[string]string{"n": itoa(leftOut)})}))
	}
	if len(action)+len(waiting)+len(unknown) == 0 {
		nodes = append(nodes, html.P(html.Props{Class: "attention-empty", Text: issue3986Text(m, "empty")}))
	} else {
		nodes = append(nodes, html.P(html.Props{Class: "attention-note", Dir: "auto", Text: issue3986Text(m, "order")}))
	}
	for _, group := range []struct {
		key   string
		items []AttentionItem
	}{{"action", action}, {"waiting", waiting}, {"unknown", unknown}} {
		if len(group.items) == 0 {
			continue
		}
		rows := make([]ui.Node, 0, len(group.items))
		for _, item := range group.items {
			rows = append(rows, issue3986Row(m, item))
		}
		nodes = append(nodes, html.Div(html.Props{Class: "attention-group", Data: map[string]string{"group": group.key}},
			html.H5(html.Props{Text: issue3986Text(m, "group_"+group.key)}), html.Ul(html.Props{Class: "attention-list"}, rows...)))
	}
	if len(brief.Gone) > 0 {
		rows := make([]ui.Node, 0, len(brief.Gone))
		for _, item := range brief.Gone {
			rows = append(rows, html.Li(html.Props{Dir: "auto", Text: item.Title}))
		}
		nodes = append(nodes, html.Div(html.Props{Class: "attention-group attention-gone"},
			html.P(html.Props{Class: "attention-note", Text: issue3986Text(m, "gone")}), html.Ul(html.Props{Class: "attention-list"}, rows...)))
	}
	actions := []ui.Node{issue3986Button(m, issue3986ReadAction, "refresh", false)}
	if issue3986DraftText(m, state) != "" {
		actions = append(actions, issue3986Button(m, issue3986DraftAction, "draft", false))
	}
	actions = append(actions, issue3986Button(m, issue3986CloseAction, "close", false))
	nodes = append(nodes, html.P(html.Props{Class: "attention-note", Dir: "auto", Text: issue3986Text(m, "private")}),
		html.Div(html.Props{Class: "attention-actions"}, actions...))
	return nodes
}

func issue3986Row(m Model, item AttentionItem) ui.Node {
	facts := []string{}
	for _, part := range []string{item.Source, item.Status} {
		if strings.TrimSpace(part) != "" {
			facts = append(facts, strings.TrimSpace(part))
		}
	}
	facts = append(facts, issue3986Dues(m, item))
	children := []ui.Node{html.Strong(html.Props{Dir: "auto", Text: item.Title}),
		html.Span(html.Props{Class: "attention-facts", Dir: "auto", Text: strings.Join(facts, " · ")})}
	if item.Responsibility == AttentionUnknown {
		children = append(children, html.Span(html.Props{Class: "attention-facts", Text: issue3986Text(m, "unknown_hint")}))
	}
	if item.Changed != "" {
		children = append(children, html.Span(html.Props{Class: "attention-changed", Role: "status", Text: issue3986Text(m, "changed_"+item.Changed)}))
	}
	if issue3986SafeHref(item.Href) {
		children = append(children, html.A(html.Props{Class: "attention-open", Href: item.Href, Aria: map[string]string{"label": issue3986Format(m, "open_item", map[string]string{"title": item.Title})}, Text: issue3986Text(m, "open_link")}))
	} else {
		children = append(children, html.Span(html.Props{Class: "attention-facts", Text: issue3986Text(m, "no_link")}))
	}
	return html.Li(html.Props{Class: "attention-item", Data: map[string]string{"item": item.ID}}, children...)
}

func issue3986Text(m Model, key string) string {
	table := map[string][3]string{
		"open":                   {"What needs my attention?", "Was braucht meine Aufmerksamkeit?", "ما الذي يحتاج إلى اهتمامي؟"},
		"scope_intro":            {"Before anything is read, this is what will be used:", "Bevor etwas gelesen wird, wird Folgendes verwendet:", "قبل قراءة أي شيء، هذا ما سيُستخدم:"},
		"scope_source":           {"Source: your My Work queue, the approvals and tasks assigned to you or started by you. Only items you may open are read.", "Quelle: Ihre Warteschlange in Meine Arbeit, also Genehmigungen und Aufgaben, die Ihnen zugewiesen sind oder die Sie gestartet haben. Gelesen werden nur Einträge, die Sie öffnen dürfen.", "المصدر: قائمة عملي، أي الموافقات والمهام المسندة إليك أو التي بدأتها. تُقرأ فقط العناصر التي يحق لك فتحها."},
		"scope_range":            {"Range: all open items, with no date limit.", "Zeitraum: alle offenen Einträge, ohne Datumsgrenze.", "النطاق: جميع العناصر المفتوحة، دون حد زمني."},
		"filter":                 {"Include items waiting on others", "Einträge einbeziehen, die auf andere warten", "تضمين العناصر التي تنتظر آخرين"},
		"private":                {"Only you can see this. Reading it changes nothing: nothing is approved, declined, reassigned, snoozed or sent.", "Nur Sie sehen dies. Das Lesen ändert nichts: Es wird nichts genehmigt, abgelehnt, neu zugewiesen, zurückgestellt oder gesendet.", "أنت وحدك من يرى هذا. القراءة لا تغيّر شيئًا: لا يُوافَق على شيء ولا يُرفض ولا يُعاد إسناده ولا يُؤجَّل ولا يُرسل."},
		"read":                   {"Read my work", "Meine Arbeit lesen", "قراءة عملي"},
		"retry":                  {"Try again", "Erneut versuchen", "حاول مرة أخرى"},
		"refresh":                {"Read again", "Erneut lesen", "القراءة مرة أخرى"},
		"close":                  {"Close", "Schließen", "إغلاق"},
		"draft":                  {"Add to my message box", "In mein Nachrichtenfeld einfügen", "إضافة إلى مربع رسالتي"},
		"loading":                {"Reading your work…", "Ihre Arbeit wird gelesen …", "جارٍ قراءة عملك…"},
		"failed":                 {"Your work could not be read, so this is not a list of what is left. Nothing is known about what needs you right now. Try again.", "Ihre Arbeit konnte nicht gelesen werden. Dies ist also keine Liste dessen, was noch offen ist. Derzeit ist nichts darüber bekannt, was Sie erledigen müssen. Versuchen Sie es erneut.", "تعذّرت قراءة عملك، لذا فهذه ليست قائمة بما تبقّى. لا يُعرف شيء الآن عمّا يحتاج إليك. حاول مرة أخرى."},
		"read_at":                {"Read at {time}", "Gelesen um {time}", "قُرئ في {time}"},
		"stale":                  {"This may be out of date. Read again before you act on it.", "Dies ist möglicherweise veraltet. Lesen Sie erneut, bevor Sie handeln.", "قد يكون هذا قديمًا. اقرأ مرة أخرى قبل أن تتصرف بناءً عليه."},
		"partial":                {"Showing {shown} of {total}. This is not everything.", "{shown} von {total} werden angezeigt. Das ist nicht alles.", "يُعرض {shown} من {total}. هذا ليس كل شيء."},
		"left_out":               {"{n} left out by your filter.", "{n} durch Ihren Filter ausgeblendet.", "تم استبعاد {n} بواسطة عامل التصفية لديك."},
		"empty":                  {"Nothing needs you in the work that was read.", "In der gelesenen Arbeit wartet nichts auf Sie.", "لا شيء يحتاج إليك في العمل الذي قُرئ."},
		"order":                  {"Ordered by due date. The order is a suggestion; statuses and dates come from each work item.", "Nach Fälligkeit sortiert. Die Reihenfolge ist ein Vorschlag; Status und Termine stammen vom jeweiligen Arbeitselement.", "مرتبة حسب تاريخ الاستحقاق. الترتيب مجرد اقتراح؛ الحالات والتواريخ تأتي من كل عنصر عمل."},
		"group_action":           {"Needs your action", "Erfordert Ihre Aktion", "يتطلب إجراءً منك"},
		"group_waiting":          {"Waiting on others", "Wartet auf andere", "ينتظر آخرين"},
		"group_unknown":          {"Role not clear", "Rolle unklar", "الدور غير واضح"},
		"draft_action":           {"Needs my action", "Erfordert meine Aktion", "يتطلب إجراءً مني"},
		"draft_waiting":          {"Waiting on others", "Wartet auf andere", "ينتظر آخرين"},
		"draft_unknown":          {"Role not clear", "Rolle unklar", "الدور غير واضح"},
		"unknown_hint":           {"Your role in this is not clear. Open it to see what it asks of you.", "Ihre Rolle dabei ist unklar. Öffnen Sie den Eintrag, um zu sehen, was er von Ihnen verlangt.", "دورك في هذا غير واضح. افتحه لترى ما يطلبه منك."},
		"no_due":                 {"No due date set", "Kein Fälligkeitsdatum festgelegt", "لا يوجد تاريخ استحقاق"},
		"due":                    {"Due {date}", "Fällig am {date}", "الاستحقاق {date}"},
		"overdue":                {"Overdue since {date}", "Überfällig seit {date}", "متأخر منذ {date}"},
		"open_link":              {"Open", "Öffnen", "فتح"},
		"open_item":              {"Open {title}", "{title} öffnen", "فتح {title}"},
		"no_link":                {"No link is available for this item.", "Für diesen Eintrag ist kein Link verfügbar.", "لا يتوفر رابط لهذا العنصر."},
		"changed_due":            {"The due date changed since your last read.", "Das Fälligkeitsdatum hat sich seit Ihrem letzten Lesen geändert.", "تغيّر تاريخ الاستحقاق منذ قراءتك الأخيرة."},
		"changed_responsibility": {"Your role in this changed since your last read.", "Ihre Rolle dabei hat sich seit Ihrem letzten Lesen geändert.", "تغيّر دورك في هذا منذ قراءتك الأخيرة."},
		"changed_status":         {"The status changed since your last read.", "Der Status hat sich seit Ihrem letzten Lesen geändert.", "تغيّرت الحالة منذ قراءتك الأخيرة."},
		"gone":                   {"These are no longer in your work since your last read, so they are no longer yours to act on here:", "Diese sind seit Ihrem letzten Lesen nicht mehr in Ihrer Arbeit und können hier nicht mehr von Ihnen bearbeitet werden:", "لم تعد هذه ضمن عملك منذ قراءتك الأخيرة، فلم يعد بإمكانك التصرف بشأنها هنا:"},
	}
	return chatbug039Text("issue3986."+key, table[key][chatbug039LocaleIndex(m.Locale)], table[key][0])
}

func issue3986Format(m Model, key string, values map[string]string) string {
	text := issue3986Text(m, key)
	for name, value := range values {
		text = strings.ReplaceAll(text, "{"+name+"}", value)
	}
	return text
}

const issue3986Styles = `.attention-brief{display:grid;gap:8px;margin-block:8px;min-width:0}` +
	`.attention-brief:not(.attention-closed){padding:12px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface)}` +
	`.attention-offer{justify-self:start;min-height:36px;padding:6px 12px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font:inherit;font-size:.875rem;cursor:pointer}` +
	`.attention-offer:hover,.attention-offer:focus-visible{border-color:var(--accent);color:var(--accent)}` +
	`.attention-title,.attention-brief h5{margin:0;font-size:.9375rem}.attention-brief p{margin:0;font-size:.8125rem;line-height:1.4;overflow-wrap:anywhere}` +
	`.attention-note,.attention-facts{color:var(--muted)}.attention-facts{font-size:.8125rem;overflow-wrap:anywhere}` +
	`.attention-failed{color:var(--danger,var(--ink))}.attention-stale,.attention-partial,.attention-changed{font-weight:600}` +
	`.attention-list,.attention-scope{display:grid;gap:8px;margin:0;padding:0;list-style:none}.attention-scope{font-size:.8125rem}` +
	`.attention-item{display:grid;gap:2px;padding:8px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);min-width:0}` +
	`.attention-open{justify-self:start;display:inline-flex;align-items:center;min-height:32px;font-size:.8125rem}` +
	`.attention-filter{display:flex;align-items:center;gap:8px;min-height:36px;cursor:pointer;font-size:.8125rem}` +
	`.attention-actions{display:flex;flex-wrap:wrap;gap:8px}` +
	`@media (pointer:coarse),(max-width:480px){.attention-offer,.attention-open,.attention-filter,.attention-actions .button{min-height:44px}.attention-actions{flex-direction:column}.attention-actions .button{width:100%}}`
