package chatui

import (
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/localize"
)

// Issue 3954: "Plan next step" on a posted poll. A reader who may see the
// poll's results opens a private panel on the card. It reads the results the
// card already shows (never voters, never a count the card hides), says which
// option leads, whether it is tied and whether voting is still open, and
// prepares meeting details the person can edit and copy. A poll option such as
// "9:00 AM ET" names no date and no time zone rule, so the details state a time
// only after the person types a date, a clock time, a named zone and a length;
// nothing is guessed. No calendar is connected to Chat: the panel says so, and
// creates, posts and sends nothing. The panel lives in the page's local state,
// so it is private to the reader. It checks access again whenever it is
// drawn or used, and warns when the poll changed after it was opened or the
// details were prepared.

const (
	issue3954OpenAction = "chatcmd002-plan"
	issue3954Close      = "chatcmd002-plan-close"
	issue3954Pick       = "chatcmd002-plan-pick"
	issue3954Prepare    = "chatcmd002-plan-prepare"
	issue3954Refresh    = "chatcmd002-plan-refresh"
)

var (
	// issue3954Now and issue3954Fields are swapped by tests; the page reads
	// the real clock and the fields in the document.
	issue3954Now    = time.Now
	issue3954Fields = chatcmd002FieldValues
)

// issue3954State is the panel a card is showing, with what was typed in it.
type issue3954State struct {
	Post     string // poll message that has the panel open, "" when none
	Revision uint64 // the card's revision the person last read
	Option   string // option id the person wants to plan
	Date     string
	Clock    string
	Zone     string
	Minutes  string
	Notice   string // copy key suffix of why the last Prepare did nothing
	Details  string // prepared meeting details, "" until Prepare succeeds
}

var issue3954Copy = map[string][3]string{
	"open":            {"Plan next step", "Nächsten Schritt planen", "تخطيط الخطوة التالية"},
	"close":           {"Close", "Schließen", "إغلاق"},
	"private":         {"Only you can see this. Nothing is posted, sent or scheduled.", "Nur Sie sehen das. Es wird nichts gepostet, gesendet oder terminiert.", "أنت فقط ترى هذا. لن يُنشر أو يُرسل أو يُجدول أي شيء."},
	"source":          {"Source: the poll “{title}”, {n} votes counted, {status}.", "Quelle: die Umfrage „{title}“, {n} Stimmen gezählt, {status}.", "المصدر: الاستطلاع «{title}»، تم احتساب {n} صوتًا، {status}."},
	"status-open":     {"voting still open", "Abstimmung noch offen", "التصويت ما زال مفتوحًا"},
	"status-closed":   {"voting closed", "Abstimmung beendet", "التصويت مغلق"},
	"calc":            {"How this is worked out: the options with the most votes are named. If two or more share the top count, it is a tie.", "So wird gerechnet: Genannt werden die Antworten mit den meisten Stimmen. Haben zwei oder mehr gleich viele, ist es ein Gleichstand.", "طريقة الحساب: يُذكر الخيار الأكثر أصواتًا. إذا تساوى خياران أو أكثر في أعلى عدد فهو تعادل."},
	"sum-none":        {"No votes yet, so there is no result to plan from.", "Noch keine Stimmen, es gibt also kein Ergebnis zum Planen.", "لا أصوات بعد، فلا توجد نتيجة للتخطيط."},
	"sum-leading":     {"{option} has the most votes: {n} of {total}.", "{option} hat die meisten Stimmen: {n} von {total}.", "{option} لديه أكثر الأصوات: {n} من {total}."},
	"sum-tied":        {"Tie: {options} each have {n} of {total} votes. The poll has not picked one.", "Gleichstand: {options} haben je {n} von {total} Stimmen. Die Umfrage hat keine gewählt.", "تعادل: لدى {options} {n} من {total} صوتًا لكل منها. لم يختر الاستطلاع خيارًا."},
	"note-open":       {"Voting is still open, so these results can change.", "Die Abstimmung ist noch offen, das Ergebnis kann sich ändern.", "التصويت ما زال مفتوحًا، لذا قد تتغير هذه النتائج."},
	"note-closed":     {"Voting is closed.", "Die Abstimmung ist beendet.", "التصويت مغلق."},
	"pick":            {"Option to plan", "Zu planende Antwort", "الخيار المراد تخطيطه"},
	"pick-hint":       {"Choose the option to plan. The poll does not choose for you.", "Wählen Sie die Antwort, die Sie planen möchten. Die Umfrage wählt nicht für Sie.", "اختر الخيار الذي تريد تخطيطه. الاستطلاع لا يختار عنك."},
	"when-hint":       {"The option does not give a date or a time zone. Type them yourself; nothing is guessed.", "Die Antwort nennt weder Datum noch Zeitzone. Geben Sie beides selbst ein, es wird nichts geraten.", "لا يحدد الخيار تاريخًا ولا منطقة زمنية. اكتبهما بنفسك، فلا شيء يُخمَّن."},
	"date":            {"Date", "Datum", "التاريخ"},
	"time":            {"Time", "Uhrzeit", "الوقت"},
	"zone":            {"Time zone name", "Name der Zeitzone", "اسم المنطقة الزمنية"},
	"zone-hint":       {"A place name such as Europe/Berlin or America/New_York. Short names like ET are not accepted.", "Ein Ortsname wie Europe/Berlin oder America/New_York. Kürzel wie ET werden nicht akzeptiert.", "اسم مكان مثل Europe/Berlin أو America/New_York. الاختصارات مثل ET غير مقبولة."},
	"minutes":         {"Length in minutes", "Dauer in Minuten", "المدة بالدقائق"},
	"prepare":         {"Prepare details", "Details vorbereiten", "تجهيز التفاصيل"},
	"refresh":         {"Update from current votes", "Mit aktuellen Stimmen aktualisieren", "التحديث بالأصوات الحالية"},
	"stale":           {"The votes changed since you opened this. Update before you use the details.", "Die Stimmen haben sich geändert, seit Sie dies geöffnet haben. Aktualisieren Sie, bevor Sie die Details verwenden.", "تغيّرت الأصوات منذ فتحت هذا. حدّث قبل استخدام التفاصيل."},
	"details":         {"Meeting details (you can edit them)", "Besprechungsdetails (bearbeitbar)", "تفاصيل الاجتماع (يمكنك تعديلها)"},
	"calendar":        {"Calendar is not connected here, so no event is created. Copy these details into your calendar yourself.", "Hier ist kein Kalender verbunden, daher wird kein Termin erstellt. Kopieren Sie die Details selbst in Ihren Kalender.", "التقويم غير متصل هنا، لذا لا يُنشأ أي حدث. انسخ هذه التفاصيل إلى تقويمك بنفسك."},
	"err-option":      {"Choose which option to plan.", "Wählen Sie die Antwort, die geplant werden soll.", "اختر الخيار المراد تخطيطه."},
	"err-date":        {"Enter a date and a time.", "Geben Sie Datum und Uhrzeit ein.", "أدخل تاريخًا ووقتًا."},
	"err-zone":        {"Enter a time zone name such as Europe/Berlin.", "Geben Sie einen Zeitzonennamen wie Europe/Berlin ein.", "أدخل اسم منطقة زمنية مثل Europe/Berlin."},
	"err-length":      {"Enter a length between 5 and 1440 minutes.", "Geben Sie eine Dauer zwischen 5 und 1440 Minuten ein.", "أدخل مدة بين 5 و1440 دقيقة."},
	"err-gap":         {"That time does not exist in that zone because the clock skips it. Pick another time.", "Diese Uhrzeit gibt es in dieser Zeitzone nicht, weil die Uhr sie überspringt. Wählen Sie eine andere.", "هذا الوقت غير موجود في تلك المنطقة لأن الساعة تتخطاه. اختر وقتًا آخر."},
	"err-repeat":      {"That time happens twice in that zone because the clock repeats it. Pick another time.", "Diese Uhrzeit gibt es in dieser Zeitzone zweimal, weil die Uhr sie wiederholt. Wählen Sie eine andere.", "هذا الوقت يحدث مرتين في تلك المنطقة لأن الساعة تكرره. اختر وقتًا آخر."},
	"err-past":        {"That time has already passed.", "Diese Zeit ist bereits vorbei.", "هذا الوقت مضى بالفعل."},
	"d-topic":         {"Meeting: {title}", "Besprechung: {title}", "الاجتماع: {title}"},
	"d-option":        {"Chosen in the poll: {option} ({n} of {total} votes, {status})", "In der Umfrage gewählt: {option} ({n} von {total} Stimmen, {status})", "المختار في الاستطلاع: {option} ({n} من {total} صوتًا، {status})"},
	"d-when":          {"Date and time: {when}", "Datum und Uhrzeit: {when}", "التاريخ والوقت: {when}"},
	"d-unconfirmed":   {"Date and time: still to be confirmed (the poll option gives no date or time zone)", "Datum und Uhrzeit: noch zu bestätigen (die Antwort nennt kein Datum und keine Zeitzone)", "التاريخ والوقت: لم يُؤكَّد بعد (لا يحدد الخيار تاريخًا ولا منطقة زمنية)"},
	"d-length":        {"Length: {n} minutes", "Dauer: {n} Minuten", "المدة: {n} دقيقة"},
	"d-notfinal":      {"Poll status: tied and voting is still open; this is not a final decision.", "Stand der Umfrage: Gleichstand und Abstimmung noch offen; das ist keine endgültige Entscheidung.", "حالة الاستطلاع: تعادل والتصويت ما زال مفتوحًا؛ هذا ليس قرارًا نهائيًا."},
	"d-notfinal-open": {"Poll status: voting is still open; this is not a final decision.", "Stand der Umfrage: Abstimmung noch offen; das ist keine endgültige Entscheidung.", "حالة الاستطلاع: التصويت ما زال مفتوحًا؛ هذا ليس قرارًا نهائيًا."},
	"d-notfinal-tied": {"Poll status: tied; this is not a final decision.", "Stand der Umfrage: Gleichstand; das ist keine endgültige Entscheidung.", "حالة الاستطلاع: تعادل؛ هذا ليس قرارًا نهائيًا."},
}

func issue3954Text(m Model, key string, values ...string) string {
	copy := issue3954Copy[key]
	return chatcmd002Fill(chatbug039Text(key, copy[chatbug039LocaleIndex(m.Locale)], copy[0]), values...)
}

// issue3954Result is what the results the reader may see add up to.
type issue3954Result struct {
	Total   int
	Top     int
	Leaders []chat.ChannelPollOption
}

func issue3954Tally(poll *chat.Chatcmd002Poll) issue3954Result {
	var r issue3954Result
	for _, option := range poll.Options {
		r.Total += option.Count
		if option.Count > r.Top {
			r.Top = option.Count
		}
	}
	if r.Top > 0 {
		for _, option := range poll.Options {
			if option.Count == r.Top {
				r.Leaders = append(r.Leaders, option)
			}
		}
	}
	return r
}

// issue3954CanPlan reports whether the reader is offered Plan next step: a
// posted poll whose view has been read and whose results the reader may see.
func issue3954CanPlan(m Model, post string, view chat.Chatcmd002View, preview bool) bool {
	if preview || post == "" || view.Card.Poll == nil || !view.ResultsVisible {
		return false
	}
	_, loaded := m.Chatcmd002Views[post]
	return loaded
}

// issue3954Plan is the button, or the open panel, drawn under a poll's options.
func issue3954Plan(m Model, post string, view chat.Chatcmd002View, preview bool) []ui.Node {
	if !issue3954CanPlan(m, post, view, preview) {
		return nil
	}
	if m.issue3954Plan.Post != post {
		return []ui.Node{html.Div(html.Props{Class: "chatcmd002-foot"},
			html.Button(html.Props{Type: "button", ID: "issue3954-open-" + post, Class: "chatcmd002-link issue3954-open", Data: map[string]string{"action": issue3954OpenAction, "id": post}, Text: issue3954Text(m, "open")}))}
	}
	return []ui.Node{issue3954Panel(m, post, view)}
}

func issue3954Status(m Model, closed bool) string {
	if closed {
		return issue3954Text(m, "status-closed")
	}
	return issue3954Text(m, "status-open")
}

func issue3954Panel(m Model, post string, view chat.Chatcmd002View) ui.Node {
	state := m.issue3954Plan
	poll := view.Card.Poll
	closed := view.Card.Closed(issue3954Now())
	result := issue3954Tally(poll)
	tied := len(result.Leaders) > 1
	title := view.Card.Title

	var summary string
	switch {
	case result.Top == 0:
		summary = issue3954Text(m, "sum-none")
	case tied:
		names := make([]string, len(result.Leaders))
		for i, leader := range result.Leaders {
			names[i] = leader.Text
		}
		summary = issue3954Text(m, "sum-tied", "n", m.nz(result.Top), "total", m.nz(result.Total), "options", strings.Join(names, "; "))
	default:
		summary = issue3954Text(m, "sum-leading", "n", m.nz(result.Top), "total", m.nz(result.Total), "option", result.Leaders[0].Text)
	}
	note := issue3954Text(m, "note-open")
	if closed {
		note = issue3954Text(m, "note-closed")
	}

	nodes := []ui.Node{
		html.H5(html.Props{ID: "issue3954-plan-" + post, Class: "issue3954-title", TabIndex: -1, Text: issue3954Text(m, "open")}),
		html.P(html.Props{Class: "chatcmd002-note", Text: issue3954Text(m, "private")}),
		html.P(html.Props{Class: "chatcmd002-note", Dir: "auto", Text: issue3954Text(m, "source", "n", m.nz(result.Total), "status", issue3954Status(m, closed), "title", title)}),
		html.P(html.Props{Class: "issue3954-summary", Dir: "auto", Text: summary + " " + note}),
		html.P(html.Props{Class: "chatcmd002-note", Text: issue3954Text(m, "calc")}),
	}
	if state.Revision != view.Revision {
		nodes = append(nodes, html.Div(html.Props{Class: "issue3954-stale", Role: "alert"},
			html.Span(html.Props{Text: issue3954Text(m, "stale")}),
			html.Button(html.Props{Type: "button", Class: "button secondary small", Data: map[string]string{"action": issue3954Refresh, "id": post}, Text: issue3954Text(m, "refresh")})))
	}

	// Which option to plan: a tie or an open poll is never settled for the person.
	chosen := state.Option
	var choices []ui.Node
	for _, option := range poll.Options {
		on := option.ID == chosen
		class := "chatcmd002-vote"
		if on {
			class += " selected"
		}
		choices = append(choices, html.Li(html.Props{Class: "chatcmd002-option"},
			html.Button(html.Props{Type: "button", Class: class, Data: map[string]string{"action": issue3954Pick, "id": post, "extra": option.ID},
				Aria: map[string]string{"pressed": boolString(on)}}, html.Span(html.Props{Class: "chatcmd002-option-text", Dir: "auto", Text: option.Text}))))
	}
	nodes = append(nodes,
		html.P(html.Props{Class: "issue3954-label", Text: issue3954Text(m, "pick")}),
		html.P(html.Props{Class: "chatcmd002-note", Text: issue3954Text(m, "pick-hint")}),
		html.Ul(html.Props{Class: "chatcmd002-options", Role: "group", Aria: map[string]string{"label": issue3954Text(m, "pick")}}, choices...),
		html.P(html.Props{Class: "chatcmd002-note", Text: issue3954Text(m, "when-hint")}))

	field := func(name, label, kind, value, hint string, extra map[string]string) ui.Node {
		id := "issue3954-" + name + "-" + post
		data := map[string]string{"chatcmd002-field": "plan-" + name, "chatcmd002-post": post, "chatcmd002-enter": issue3954Prepare, "chatcmd002-escape": issue3954Close}
		props := html.Props{ID: id, Class: "chat-input", Type: kind, Value: value, AutoComplete: "off", Data: data, Dir: "auto"}
		if kind == "number" {
			props.Raw = map[string]any{"min": 5, "max": 1440, "step": 1, "inputmode": "numeric"}
		}
		if hint != "" {
			props.Aria = map[string]string{"describedby": id + "-hint"}
		}
		kids := []ui.Node{html.Label(html.Props{For: id, Class: "chatcmd002-edit-label", Text: label}), html.Input(props)}
		if hint != "" {
			kids = append(kids, html.P(html.Props{ID: id + "-hint", Class: "chatcmd002-note", Text: hint}))
		}
		return html.Div(html.Props{Class: "chatcmd002-edit-field"}, kids...)
	}
	nodes = append(nodes, html.Div(html.Props{Class: "issue3954-fields"},
		field("date", issue3954Text(m, "date"), "date", state.Date, "", nil),
		field("time", issue3954Text(m, "time"), "time", state.Clock, "", nil),
		field("zone", issue3954Text(m, "zone"), "text", state.Zone, issue3954Text(m, "zone-hint"), nil),
		field("minutes", issue3954Text(m, "minutes"), "number", state.Minutes, "", nil)))
	if state.Notice != "" {
		nodes = append(nodes, html.P(html.Props{Class: "chatcmd002-note issue3954-error", Role: "alert", Text: issue3954Text(m, state.Notice)}))
	}
	details := state.Details
	if details == "" {
		details = issue3954Draft(m, view, result, closed, chosen, "", "")
	}
	nodes = append(nodes,
		html.Div(html.Props{Class: "chatcmd002-foot"},
			html.Button(html.Props{Type: "button", Class: "button small", Data: map[string]string{"action": issue3954Prepare, "id": post}, Text: issue3954Text(m, "prepare")})),
		html.Label(html.Props{For: "issue3954-details-" + post, Class: "chatcmd002-edit-label", Text: issue3954Text(m, "details")}),
		html.Textarea(html.Props{ID: "issue3954-details-" + post, Class: "chat-input issue3954-details", Rows: 7, Dir: "auto", Data: map[string]string{"chatcmd002-field": "plan-details", "chatcmd002-post": post}}, ui.Text(details)),
		html.P(html.Props{Class: "chatcmd002-note", Role: "status", Text: issue3954Text(m, "calendar")}),
		html.Div(html.Props{Class: "chatcmd002-foot"},
			html.Button(html.Props{Type: "button", Class: "chatcmd002-link", Data: map[string]string{"action": issue3954Close, "id": post}, Text: issue3954Text(m, "close")})))
	return html.Section(html.Props{Class: "issue3954-panel", Dir: agentReplyDirection(m.Locale), Role: "region", Aria: map[string]string{"label": issue3954Text(m, "open")}}, nodes...)
}

// issue3954Draft is the meeting description. when and length are empty until
// the person has confirmed them, and then it says so rather than guessing.
func issue3954Draft(m Model, view chat.Chatcmd002View, result issue3954Result, closed bool, chosen, when, length string) string {
	lines := []string{issue3954Text(m, "d-topic", "title", view.Card.Title)}
	for _, option := range view.Card.Poll.Options {
		if option.ID == chosen {
			lines = append(lines, issue3954Text(m, "d-option", "n", m.nz(option.Count), "total", m.nz(result.Total), "status", issue3954Status(m, closed), "option", option.Text))
		}
	}
	if when == "" {
		lines = append(lines, issue3954Text(m, "d-unconfirmed"))
	} else {
		lines = append(lines, issue3954Text(m, "d-when", "when", when), issue3954Text(m, "d-length", "n", length))
	}
	tied := len(result.Leaders) > 1
	switch {
	case tied && !closed:
		lines = append(lines, issue3954Text(m, "d-notfinal"))
	case tied:
		lines = append(lines, issue3954Text(m, "d-notfinal-tied"))
	case !closed:
		lines = append(lines, issue3954Text(m, "d-notfinal-open"))
	}
	return strings.Join(lines, "\n")
}

// issue3954Zone reads a named zone: "UTC" or a place name with a slash. Short
// names (ET, PST, EST5EDT) say nothing about daylight saving and are refused.
func issue3954Zone(name string) (*time.Location, bool) {
	name = strings.TrimSpace(name)
	if name != "UTC" && !strings.Contains(name, "/") {
		return nil, false
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, false
	}
	return loc, true
}

func issue3954Capture(local localStore, post string) {
	values := issue3954Fields(post)
	if values == nil {
		return
	}
	local.update(func(u *localUI) {
		if u.plan.Post != post {
			return
		}
		u.plan.Date, u.plan.Clock = strings.TrimSpace(values["plan-date"]), strings.TrimSpace(values["plan-time"])
		u.plan.Zone, u.plan.Minutes = strings.TrimSpace(values["plan-zone"]), strings.TrimSpace(values["plan-minutes"])
	})
}

// issue3954Action runs the panel's presses. Every one reads the card's view
// again and drops the panel when the reader may no longer see the results.
func issue3954Action(m Model, local localStore, action, post, extra string) bool {
	switch action {
	case issue3954OpenAction, issue3954Close, issue3954Pick, issue3954Prepare, issue3954Refresh:
	default:
		return false
	}
	view, loaded := m.Chatcmd002Views[post]
	if action == issue3954Close || !loaded || !issue3954CanPlan(m, post, view, false) {
		local.update(func(u *localUI) {
			if u.plan.Post == post || action == issue3954Close {
				u.plan = issue3954State{}
			}
		})
		if action == issue3954Close {
			focusChatLayerField("issue3954-open-" + post)
		}
		return true
	}
	if action == issue3954OpenAction {
		state := issue3954State{Post: post, Revision: view.Revision}
		if result := issue3954Tally(view.Card.Poll); len(result.Leaders) == 1 {
			state.Option = result.Leaders[0].ID
		}
		local.update(func(u *localUI) { u.plan = state })
		focusChatLayerField("issue3954-plan-" + post)
		return true
	}
	if local.get().plan.Post != post {
		return true
	}
	issue3954Capture(local, post)
	switch action {
	case issue3954Pick:
		for _, option := range view.Card.Poll.Options {
			if option.ID == extra {
				local.update(func(u *localUI) { u.plan.Option, u.plan.Details, u.plan.Notice = extra, "", "" })
			}
		}
	case issue3954Refresh:
		local.update(func(u *localUI) { u.plan.Revision, u.plan.Details, u.plan.Notice = view.Revision, "", "" })
	case issue3954Prepare:
		issue3954Make(m, local, view)
	}
	return true
}

func issue3954Make(m Model, local localStore, view chat.Chatcmd002View) {
	state := local.get().plan
	fail := func(key string) { local.update(func(u *localUI) { u.plan.Notice, u.plan.Details = key, "" }) }
	if state.Revision != view.Revision {
		// The poll moved on since the person last read it: show the new result first.
		local.update(func(u *localUI) { u.plan.Revision, u.plan.Notice, u.plan.Details = view.Revision, "", "" })
		return
	}
	if state.Option == "" {
		fail("err-option")
		return
	}
	if state.Date == "" || state.Clock == "" {
		fail("err-date")
		return
	}
	loc, ok := issue3954Zone(state.Zone)
	if !ok {
		fail("err-zone")
		return
	}
	minutes, err := strconv.Atoi(state.Minutes)
	if err != nil || minutes < 5 || minutes > 1440 {
		fail("err-length")
		return
	}
	at, err := chat.Chatcmd004ResolveDateTime(state.Date, state.Clock, loc, issue3954Now())
	switch err {
	case nil:
	case chat.ErrDeadlineNotExists:
		fail("err-gap")
		return
	case chat.ErrDeadlineAmbiguous:
		fail("err-repeat")
		return
	case chat.ErrDeadlinePast:
		fail("err-past")
		return
	default:
		fail("err-date")
		return
	}
	when := localize.FormatDay(m.Locale, at, true) + ", " + localize.FormatClock(m.Locale, at) + " " + loc.String() + " (" + chatcmd002Offset(at) + ")"
	closed := view.Card.Closed(issue3954Now())
	details := issue3954Draft(m, view, issue3954Tally(view.Card.Poll), closed, state.Option, when, strconv.Itoa(minutes))
	local.update(func(u *localUI) { u.plan.Details, u.plan.Notice = details, "" })
}

const issue3954Styles = `.chat-workspace .issue3954-panel{display:grid;gap:8px;min-width:0;border-block-start:1px solid var(--line);padding-block-start:8px}` +
	`.chat-workspace .issue3954-title{margin:0;font-size:.875rem}.chat-workspace .issue3954-summary{margin:0;font-weight:600}.chat-workspace .issue3954-label{margin:0;font-size:.75rem;font-weight:600;color:var(--muted)}` +
	`.chat-workspace .issue3954-fields{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,150px),1fr));gap:8px;min-width:0}` +
	`.chat-workspace .issue3954-fields .chat-input,.chat-workspace .issue3954-details{box-sizing:border-box;width:100%;min-width:0;max-width:100%}` +
	`.chat-workspace .issue3954-stale{display:flex;flex-wrap:wrap;align-items:center;gap:8px;padding:6px 8px;border:1px solid var(--warn,var(--line));border-radius:var(--hcm-radius-control)}` +
	`.chat-workspace .issue3954-error{color:var(--danger,inherit)}` +
	`.chat-workspace .issue3954-panel button,.chat-workspace .issue3954-open{min-height:32px}` +
	`@media (pointer:coarse),(max-width:640px){.chat-workspace .issue3954-panel button,.chat-workspace .issue3954-open{min-height:44px}.chat-workspace .issue3954-panel .chat-input{min-height:44px}}`
