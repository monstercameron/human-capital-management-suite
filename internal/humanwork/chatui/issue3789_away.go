package chatui

import (
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ISSUE-3789: away until a date, with a named cover.
//
// A person says in Chat preferences that they are away until a date, may name
// a colleague who covers, may keep the absence private, and may ask for their
// cover to be told about mentions of them. Colleagues then see "Away until ..."
// and who covers on the person's profile card and beside their name in the
// "@" list. This is a chat preference only: it gives the cover no access to
// any conversation and no authority over any approval. Approvals follow the
// company's own delegation, which is said in the row so nobody assumes more.
// The server decides what a colleague may see; this file draws and edits.

// AwayView is the signed-in person's own setting as the page holds it.
type AwayView struct {
	Loaded, Failed bool
	Revision       uint64
	// Active is whether an absence is running; Until is when it ends.
	Active  bool
	Until   time.Time
	CoverID string
	// Hidden keeps the absence from colleagues. NotifyCover offers the cover a
	// notice for mentions of this person.
	Hidden, NotifyCover bool
	// Note is the last save's outcome: "", "saved", "ended", "failed",
	// "conflict", "invalid", "choose_date", "past_date" or "far_date".
	Note string
}

// AwayEdit is a change the person made. A zero Until ends the absence. Problem
// is set instead of a save when the typed date cannot be used: "choose_date",
// "past_date" or "far_date"; the page then says so and sends nothing.
type AwayEdit struct {
	Until               time.Time
	CoverID             string
	Hidden, NotifyCover bool
	Problem             string
}

// AwayPersonView is what a colleague may see of someone else's absence.
type AwayPersonView struct {
	Until   time.Time
	CoverID string
}

const (
	k3789Title       = "chat.issue3789.title"
	k3789Status      = "chat.issue3789.status"
	k3789NotAway     = "chat.issue3789.not_away"
	k3789UntilLabel  = "chat.issue3789.until_label"
	k3789CoverLabel  = "chat.issue3789.cover_label"
	k3789NoCover     = "chat.issue3789.no_cover"
	k3789Save        = "chat.issue3789.save"
	k3789Back        = "chat.issue3789.back"
	k3789Show        = "chat.issue3789.show"
	k3789Notify      = "chat.issue3789.notify"
	k3789NotifyNeeds = "chat.issue3789.notify_needs"
	k3789Authority   = "chat.issue3789.authority"
	k3789Loading     = "chat.issue3789.loading"
	k3789Unavailable = "chat.issue3789.unavailable"
	k3789Saved       = "chat.issue3789.saved"
	k3789Ended       = "chat.issue3789.ended"
	k3789Failed      = "chat.issue3789.failed"
	k3789Conflict    = "chat.issue3789.conflict"
	k3789Invalid     = "chat.issue3789.invalid"
	k3789ChooseDate  = "chat.issue3789.choose_date"
	k3789PastDate    = "chat.issue3789.past_date"
	k3789FarDate     = "chat.issue3789.far_date"
	k3789AwayUntil   = "chat.issue3789.away_until"
	k3789CoveredBy   = "chat.issue3789.covered_by"
	k3789Colleague   = "chat.issue3789.colleague"
	k3789Private     = "chat.issue3789.private"
)

var issue3789Copy = map[string]map[string]string{
	"en-US": {
		k3789Title:       "Away",
		k3789Status:      "You are away until {date}.{cover}",
		k3789NotAway:     "You are not away.",
		k3789UntilLabel:  "Away until",
		k3789CoverLabel:  "Who covers",
		k3789NoCover:     "No one",
		k3789Save:        "Set away",
		k3789Back:        "I am back",
		k3789Show:        "Show my absence and who covers to colleagues",
		k3789Notify:      "Tell my cover when I am mentioned",
		k3789NotifyNeeds: "Choose who covers to turn this on.",
		k3789Authority:   "Your cover does not get new access to conversations or any approval authority from this. Approvals follow your company's own delegation.",
		k3789Loading:     "Loading your away setting…",
		k3789Unavailable: "Your away setting could not be loaded.",
		k3789Saved:       "Away setting saved",
		k3789Ended:       "Welcome back. You are no longer away.",
		k3789Failed:      "Your away setting could not be saved. Try again.",
		k3789Conflict:    "Your away setting was changed somewhere else. It has been reloaded; check it and save again.",
		k3789Invalid:     "That cover is not available. Pick someone else.",
		k3789ChooseDate:  "Choose the date you are away until.",
		k3789PastDate:    "Choose a date after today.",
		k3789FarDate:     "Choose a date within the next year.",
		k3789AwayUntil:   "Away until {date}",
		k3789CoveredBy:   "{name} covers",
		k3789Colleague:   "A colleague",
		k3789Private:     "Only you see this while it is hidden.",
	},
	"de-DE": {
		k3789Title:       "Abwesend",
		k3789Status:      "Sie sind bis {date} abwesend.{cover}",
		k3789NotAway:     "Sie sind nicht abwesend.",
		k3789UntilLabel:  "Abwesend bis",
		k3789CoverLabel:  "Vertretung",
		k3789NoCover:     "Niemand",
		k3789Save:        "Abwesend setzen",
		k3789Back:        "Ich bin zurück",
		k3789Show:        "Meine Abwesenheit und Vertretung für Kolleginnen und Kollegen anzeigen",
		k3789Notify:      "Meine Vertretung benachrichtigen, wenn ich erwähnt werde",
		k3789NotifyNeeds: "Wählen Sie eine Vertretung, um dies einzuschalten.",
		k3789Authority:   "Ihre Vertretung erhält dadurch weder neuen Zugriff auf Unterhaltungen noch Genehmigungsbefugnis. Genehmigungen folgen der Vertretungsregelung Ihres Unternehmens.",
		k3789Loading:     "Ihre Abwesenheit wird geladen…",
		k3789Unavailable: "Ihre Abwesenheit konnte nicht geladen werden.",
		k3789Saved:       "Abwesenheit gespeichert",
		k3789Ended:       "Willkommen zurück. Sie sind nicht mehr abwesend.",
		k3789Failed:      "Ihre Abwesenheit konnte nicht gespeichert werden. Versuchen Sie es erneut.",
		k3789Conflict:    "Ihre Abwesenheit wurde an anderer Stelle geändert und neu geladen. Prüfen Sie sie und speichern Sie erneut.",
		k3789Invalid:     "Diese Vertretung ist nicht verfügbar. Wählen Sie jemand anderen.",
		k3789ChooseDate:  "Wählen Sie das Datum, bis zu dem Sie abwesend sind.",
		k3789PastDate:    "Wählen Sie ein Datum nach heute.",
		k3789FarDate:     "Wählen Sie ein Datum innerhalb des nächsten Jahres.",
		k3789AwayUntil:   "Abwesend bis {date}",
		k3789CoveredBy:   "Vertretung: {name}",
		k3789Colleague:   "Eine Kollegin oder ein Kollege",
		k3789Private:     "Solange sie ausgeblendet ist, sehen nur Sie diese Angabe.",
	},
	"ar": {
		k3789Title:       "خارج المكتب",
		k3789Status:      "أنت خارج المكتب حتى {date}.{cover}",
		k3789NotAway:     "أنت لست خارج المكتب.",
		k3789UntilLabel:  "خارج المكتب حتى",
		k3789CoverLabel:  "من ينوب عنك",
		k3789NoCover:     "لا أحد",
		k3789Save:        "تعيين الغياب",
		k3789Back:        "لقد عدت",
		k3789Show:        "إظهار غيابي ومن ينوب عني للزملاء",
		k3789Notify:      "إبلاغ من ينوب عني عند الإشارة إليّ",
		k3789NotifyNeeds: "اختر من ينوب عنك لتفعيل هذا.",
		k3789Authority:   "لا يمنح هذا من ينوب عنك وصولًا جديدًا إلى المحادثات ولا أي صلاحية موافقة. تتبع الموافقات تفويض شركتك.",
		k3789Loading:     "جارٍ تحميل إعداد الغياب…",
		k3789Unavailable: "تعذر تحميل إعداد الغياب.",
		k3789Saved:       "تم حفظ إعداد الغياب",
		k3789Ended:       "مرحبًا بعودتك. لم تعد خارج المكتب.",
		k3789Failed:      "تعذر حفظ إعداد الغياب. حاول مرة أخرى.",
		k3789Conflict:    "تم تغيير إعداد الغياب في مكان آخر وأُعيد تحميله. راجعه واحفظ مجددًا.",
		k3789Invalid:     "هذا الشخص غير متاح للنيابة. اختر شخصًا آخر.",
		k3789ChooseDate:  "اختر تاريخ انتهاء غيابك.",
		k3789PastDate:    "اختر تاريخًا بعد اليوم.",
		k3789FarDate:     "اختر تاريخًا خلال السنة القادمة.",
		k3789AwayUntil:   "خارج المكتب حتى {date}",
		k3789CoveredBy:   "ينوب عنه {name}",
		k3789Colleague:   "أحد الزملاء",
		k3789Private:     "أنت وحدك ترى هذا أثناء إخفائه.",
	},
}

func issue3789Text(m Model, key string) string { return laneText(m, issue3789Copy, key) }

func issue3789Textf(m Model, key string, vars map[string]string) string {
	return laneTextf(m, issue3789Copy, key, vars)
}

const issue3789DateLayout = "2006-01-02"

// issue3789MaxDays is the furthest day an absence may end, a little inside the
// year the server accepts so a day boundary never tips a valid choice over.
const issue3789MaxDays = 364

// issue3789Date writes the day an absence ends, in the person's zone.
func issue3789Date(m Model, at time.Time) string {
	return formatDay(m.Locale, at.In(issue3920Location(m)), at.In(issue3920Location(m)).Year() != time.Now().In(issue3920Location(m)).Year())
}

// issue3789CoverName is the cover's name as this page knows it, or a plain
// word when it does not; never an identifier.
func issue3789CoverName(m Model, id string) string {
	if name := strings.TrimSpace(chatKnownName(m, id)); name != "" && name != id {
		return name
	}
	return issue3789Text(m, k3789Colleague)
}

// issue3789Line is one person's absence in words: "Away until Tuesday,
// December 1 · Priya covers". It is empty when there is nothing to show. The
// signed-in person sees their own absence even when it is hidden; everyone else
// sees only what the server returned for them.
func issue3789Line(m Model, id string) string {
	var until time.Time
	cover := ""
	switch {
	case id == "":
		return ""
	case id == m.CurrentUser:
		if !m.Away.Loaded || !m.Away.Active || !m.Away.Until.After(time.Now()) {
			return ""
		}
		until, cover = m.Away.Until, m.Away.CoverID
	default:
		p, ok := m.AwayPeople[id]
		if !ok || !p.Until.After(time.Now()) {
			return ""
		}
		until, cover = p.Until, p.CoverID
	}
	line := issue3789Textf(m, k3789AwayUntil, map[string]string{"date": issue3789Date(m, until)})
	if cover != "" {
		line += " · " + issue3789Textf(m, k3789CoveredBy, map[string]string{"name": issue3789CoverName(m, cover)})
	}
	return line
}

// issue3789PersonAway is the line on the profile card.
func issue3789PersonAway(m Model, id string) ui.Node {
	line := issue3789Line(m, id)
	if line == "" {
		return nil
	}
	return html.P(html.Props{Class: "person-away", Role: "status", Dir: "auto", Data: map[string]string{"person-away": "true"}, Text: line})
}

// issue3789Edit is the person's current setting as an edit to change one part of.
func issue3789Edit(m Model) AwayEdit {
	v := m.Away
	e := AwayEdit{Hidden: v.Hidden}
	if v.Active {
		e.Until, e.CoverID, e.NotifyCover = v.Until, v.CoverID, v.NotifyCover
	}
	return e
}

// issue3789Typed turns the typed date and chosen cover into an edit. The date
// is the day the person is back: the absence ends at the start of that day in
// the person's zone. A date that is empty or not after today is a Problem.
func issue3789Typed(m Model, date, cover string, now time.Time) AwayEdit {
	e := issue3789Edit(m)
	e.CoverID = strings.TrimSpace(cover)
	if e.CoverID == "" {
		e.NotifyCover = false
	}
	date = strings.TrimSpace(date)
	if date == "" {
		e.Problem = "choose_date"
		return e
	}
	loc := issue3920Location(m)
	day, err := time.ParseInLocation(issue3789DateLayout, date, loc)
	if err != nil {
		e.Problem = "choose_date"
		return e
	}
	if !day.After(now) {
		e.Problem = "past_date"
		return e
	}
	if day.After(now.AddDate(0, 0, issue3789MaxDays)) {
		e.Problem = "far_date"
		return e
	}
	e.Until = day.UTC()
	return e
}

// issue3789Apply runs one click of the Away row and says whether it was one of
// its own. read gives a field's typed value.
func issue3789Apply(m Model, action string, read func(string) string, now time.Time) bool {
	switch action {
	case "away-save":
		if m.Callbacks.SaveAway != nil {
			m.Callbacks.SaveAway(issue3789Typed(m, read("chat-away-until"), read("chat-away-cover"), now))
		}
		return true
	case "away-clear":
		if m.Callbacks.SaveAway != nil {
			m.Callbacks.SaveAway(AwayEdit{Hidden: m.Away.Hidden})
		}
		return true
	case "away-show":
		if m.Callbacks.SaveAway != nil {
			e := issue3789Edit(m)
			e.Hidden = !e.Hidden
			m.Callbacks.SaveAway(e)
		}
		return true
	case "away-notify":
		if m.Callbacks.SaveAway != nil && m.Away.Active && m.Away.CoverID != "" {
			e := issue3789Edit(m)
			e.NotifyCover = !e.NotifyCover
			m.Callbacks.SaveAway(e)
		}
		return true
	}
	return false
}

// issue3789Click handles a click on the Away row's buttons.
func issue3789Click(e ui.MouseEvent, m Model) bool {
	action, _, _ := eventAction(e)
	if !issue3789Apply(m, action, domValue, time.Now()) {
		return false
	}
	e.PreventDefault()
	return true
}

func issue3789NoteText(m Model) string {
	switch m.Away.Note {
	case "saved":
		return issue3789Text(m, k3789Saved)
	case "ended":
		return issue3789Text(m, k3789Ended)
	case "failed":
		return issue3789Text(m, k3789Failed)
	case "conflict":
		return issue3789Text(m, k3789Conflict)
	case "invalid":
		return issue3789Text(m, k3789Invalid)
	case "choose_date":
		return issue3789Text(m, k3789ChooseDate)
	case "past_date":
		return issue3789Text(m, k3789PastDate)
	case "far_date":
		return issue3789Text(m, k3789FarDate)
	}
	return ""
}

// issue3789Candidates are the people who can be named as cover: the people the
// page knows from the conversations in view, never the person themselves and
// never an agent. Naming one grants nothing, so the list needs no more than that.
func issue3789Candidates(m Model) []mentionCandidate {
	seen := map[string]bool{}
	out := []mentionCandidate{}
	for _, p := range m.Members {
		name := strings.TrimSpace(p.Name)
		if p.Agent || p.ID == "" || p.ID == m.CurrentUser || name == "" || name == p.ID || seen[p.ID] {
			continue
		}
		seen[p.ID] = true
		out = append(out, mentionCandidate{ID: p.ID, Name: name, Member: true})
	}
	// A cover already chosen stays listed even when no conversation in view
	// holds them, so saving again does not silently drop them.
	if id := m.Away.CoverID; id != "" && !seen[id] {
		out = append(out, mentionCandidate{ID: id, Name: issue3789CoverName(m, id)})
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func issue3789Toggle(action, label string, pressed, disabled bool) ui.Node {
	return html.Button(html.Props{Class: "chat-prefs-choice away-toggle", Type: "button", Dir: "auto", Text: label, Disabled: disabled,
		Data: map[string]string{"action": action},
		Aria: map[string]string{"pressed": boolString(pressed)}})
}

// issue3789Row is the Away row of Chat preferences. It is drawn where the page
// can keep the setting; until the server has answered it says so, and when the
// server cannot answer it says that instead of offering controls that would do
// nothing.
func issue3789Row(m Model) ui.Node {
	if m.Callbacks.SaveAway == nil {
		return nil
	}
	v := m.Away
	title := issue3789Text(m, k3789Title)
	switch {
	case v.Failed && !v.Loaded:
		return chatux002Row("away", title, "", nil,
			html.P(html.Props{Class: "field-hint chat-prefs-note", Role: "status", Dir: "auto", Text: issue3789Text(m, k3789Unavailable)}))
	case !v.Loaded:
		return chatux002Row("away", title, "", nil,
			html.P(html.Props{Class: "field-hint chat-prefs-note", Role: "status", Dir: "auto", Text: issue3789Text(m, k3789Loading)}))
	}
	status := issue3789Text(m, k3789NotAway)
	value := ""
	if v.Active {
		cover := ""
		if v.CoverID != "" {
			cover = " " + issue3789Textf(m, k3789CoveredBy, map[string]string{"name": issue3789CoverName(m, v.CoverID)}) + "."
		}
		status = issue3789Textf(m, k3789Status, map[string]string{"date": issue3789Date(m, v.Until), "cover": cover})
		value = issue3789Textf(m, k3789AwayUntil, map[string]string{"date": issue3789Date(m, v.Until)})
	}
	loc := issue3920Location(m)
	tomorrow := time.Now().In(loc).AddDate(0, 0, 1).Format(issue3789DateLayout)
	chosen := ""
	if v.Active {
		chosen = v.Until.In(loc).Format(issue3789DateLayout)
	}
	options := []ui.Node{html.Option(html.Props{Raw: map[string]any{"value": ""}, Selected: v.CoverID == "", Text: issue3789Text(m, k3789NoCover)})}
	for _, c := range issue3789Candidates(m) {
		options = append(options, html.WithKey(html.Option(html.Props{Value: c.ID, Selected: c.ID == v.CoverID, Text: c.Name}), "away-cover:"+c.ID))
	}
	children := []ui.Node{
		html.P(html.Props{Class: "field-hint chat-prefs-note away-status", Role: "status", Dir: "auto", Data: map[string]string{"away-status": "true"}, Text: status}),
		html.Div(html.Props{Class: "away-fields", Role: "group", Aria: map[string]string{"label": title}},
			html.Label(html.Props{For: "chat-away-until", Class: "away-label", Dir: "auto", Text: issue3789Text(m, k3789UntilLabel)}),
			html.Input(html.Props{ID: "chat-away-until", Class: "chat-input", Type: "date", Data: map[string]string{"chat-value": chosen}, Raw: map[string]any{"min": tomorrow, "max": time.Now().In(loc).AddDate(0, 0, issue3789MaxDays).Format(issue3789DateLayout)}}),
			html.Label(html.Props{For: "chat-away-cover", Class: "away-label", Dir: "auto", Text: issue3789Text(m, k3789CoverLabel)}),
			html.Select(html.Props{ID: "chat-away-cover", Class: "chat-input"}, options...)),
		html.Div(html.Props{Class: "chat-prefs-choices", Role: "group", Aria: map[string]string{"label": title}}, awayButtons(m, v.Active)...),
		issue3789Toggle("away-show", issue3789Text(m, k3789Show), !v.Hidden, false),
		issue3789Toggle("away-notify", issue3789Text(m, k3789Notify), v.NotifyCover, !v.Active || v.CoverID == ""),
	}
	if !v.Active || v.CoverID == "" {
		children = append(children, html.P(html.Props{Class: "field-hint chat-prefs-note", Dir: "auto", Text: issue3789Text(m, k3789NotifyNeeds)}))
	}
	if v.Hidden {
		children = append(children, html.P(html.Props{Class: "field-hint chat-prefs-note", Dir: "auto", Text: issue3789Text(m, k3789Private)}))
	}
	if note := issue3789NoteText(m); note != "" {
		role := "status"
		if v.Note != "saved" && v.Note != "ended" {
			role = "alert"
		}
		children = append(children, html.P(html.Props{Class: "field-hint chat-prefs-note away-note", Role: role, Dir: "auto", Data: map[string]string{"away-note": v.Note}, Text: note}))
	}
	children = append(children, html.P(html.Props{Class: "field-hint chat-prefs-note away-authority", Dir: "auto", Text: issue3789Text(m, k3789Authority)}))
	return chatux002Row("away", title, value, nil, children...)
}

func awayButtons(m Model, active bool) []ui.Node {
	out := []ui.Node{html.Button(html.Props{Class: "button secondary small", Type: "button", Dir: "auto", Text: issue3789Text(m, k3789Save), Data: map[string]string{"action": "away-save", "away-save": "true"}})}
	if active {
		out = append(out, html.Button(html.Props{Class: "button secondary small", Type: "button", Dir: "auto", Text: issue3789Text(m, k3789Back), Data: map[string]string{"action": "away-clear", "away-clear": "true"}}))
	}
	return out
}

// Issue3789Styles is the Away row and the profile line: a column that fits the
// 360px panel and a 320px phone, 44px targets and logical properties only so it
// holds in right-to-left.
const Issue3789Styles = `.away-fields{display:grid;gap:6px;padding-block:4px}` +
	`.away-fields .chat-input{box-sizing:border-box;min-block-size:44px;inline-size:100%;min-inline-size:0}` +
	`.away-label{font-weight:600}` +
	`.chat-prefs-section[data-prefs-section="away"] .button,.away-toggle{min-block-size:44px}` +
	`.away-note[data-away-note]:not([data-away-note="saved"]):not([data-away-note="ended"]){color:var(--danger,var(--ink))}` +
	`.person-away{margin:0;padding-inline:12px;color:var(--muted);font-size:.9375rem;text-align:center}`
