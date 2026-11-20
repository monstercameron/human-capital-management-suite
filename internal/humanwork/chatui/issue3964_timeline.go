package chatui

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3964: Draft an event timeline.
//
// Conversation details offer an Event timeline entry. A person picks a first and
// a last day and the open conversation (replies in threads optional); the page
// lists what people posted in that range, oldest first, each entry with when it
// was posted, who posted it and a way to open it. The list is private to the
// person and built by plain rules, not by a model:
//
//   - Every entry is a report. The time is when the message was posted, never a
//     claim about when the event happened; a message that names a time of its
//     own says so. Nothing invents a cause, an owner or an outcome, and no
//     outcome record is mixed in.
//   - It is read through the Chat search, so membership, history, deletion and
//     message access are the server's, per row, at the time of the read. A message
//     that was edited says so and shows its current wording only.
//   - Long silences between entries, a range cut short at the entry limit and a
//     source that cannot be read are each said in words.
//   - The person removes entries they do not want. Nothing is published,
//     exported or changed: "Put in message box" checks access again, leaves out
//     anything addressed to part of the channel, and writes an unsent draft.

// The states of the timeline, as the browser reports them.
const (
	TimelineLoading     = "loading"
	TimelineReady       = "ready"
	TimelineError       = "error"
	TimelineUnavailable = "unavailable"
)

// TimelineMaxEntries is how many messages one timeline reads. A longer range is
// cut there, oldest first, and says so.
const TimelineMaxEntries = 200

// TimelineMaxDays is the longest range one timeline covers.
const TimelineMaxDays = 92

// timelineGapAfter is how long a silence has to be to be pointed out.
const timelineGapAfter = 4 * time.Hour

// TimelineEntry is one message as the timeline shows it.
type TimelineEntry struct {
	// ID is the message's id.
	ID       string
	AuthorID string
	// Text is the message as it reads now.
	Text string
	At   time.Time
	// Href is the address that opens the message (ChatSearchRowHref).
	Href string
	// Edited is true when the message was changed after it was posted.
	Edited bool
	// Restricted is true when the message was addressed to part of the channel.
	Restricted     bool
	InThread       bool
	Sequence       uint64
	ThreadID       string
	ThreadSequence uint64
}

// TimelineView is the timeline of one conversation. Another conversation's view
// draws nothing.
type TimelineView struct {
	ConversationID string
	Status         string
	// From and To are the first and last day asked for (YYYY-MM-DD).
	From, To string
	// Replies is true when replies in threads were read too.
	Replies bool
	Entries []TimelineEntry
	// Truncated is true when messages later than the last entry were not read.
	Truncated bool
}

// TimelineCheck is the answer of the access check made before a draft is made.
type TimelineCheck struct {
	// Changed is how many of the entries asked about can no longer be read as
	// shown: removed, no longer readable, or worded differently.
	Changed int
	// Failed is true when access could not be checked at all.
	Failed bool
}

var issue3964Copy = map[string][3]string{
	"title":         {"Event timeline", "Ereignis-Zeitleiste", "الجدول الزمني للأحداث"},
	"show":          {"Show event timeline", "Ereignis-Zeitleiste anzeigen", "عرض الجدول الزمني للأحداث"},
	"hide":          {"Hide event timeline", "Ereignis-Zeitleiste ausblenden", "إخفاء الجدول الزمني للأحداث"},
	"intro":         {"Draft a timeline of what people reported in this conversation, from the messages you can read. It stays private to you. Nothing is posted, exported or changed unless you choose to.", "Erstellen Sie eine Zeitleiste dessen, was in dieser Unterhaltung berichtet wurde, aus den Nachrichten, die Sie lesen können. Sie bleibt privat. Nichts wird gepostet, exportiert oder geändert, es sei denn, Sie entscheiden das.", "أنشئ جدولًا زمنيًا لما أبلغ عنه الناس في هذه المحادثة من الرسائل التي يمكنك قراءتها. يبقى خاصًا بك. لا يُنشر أو يُصدَّر أو يتغيّر أي شيء ما لم تختر ذلك."},
	"source":        {"Reads: {name}", "Gelesen wird: {name}", "المصدر: {name}"},
	"first":         {"First day", "Erster Tag", "أول يوم"},
	"last":          {"Last day", "Letzter Tag", "آخر يوم"},
	"replies":       {"Include replies in threads", "Antworten in Threads einbeziehen", "تضمين الردود في السلاسل"},
	"make":          {"Make timeline", "Zeitleiste erstellen", "إنشاء الجدول الزمني"},
	"refresh":       {"Read again", "Erneut lesen", "القراءة مرة أخرى"},
	"retry":         {"Try again", "Erneut versuchen", "أعد المحاولة"},
	"loading":       {"Reading messages…", "Nachrichten werden gelesen …", "جارٍ قراءة الرسائل…"},
	"reports_note":  {"Each entry shows when someone posted a report in chat. That is not proof of when the event happened, or that it happened. Records of what was decided or done are not included; check them separately.", "Jeder Eintrag zeigt, wann jemand im Chat etwas gemeldet hat. Das belegt weder, wann das Ereignis stattfand, noch dass es stattfand. Aufzeichnungen darüber, was entschieden oder getan wurde, sind nicht enthalten; prüfen Sie sie getrennt.", "يعرض كل إدخال وقت نشر أحدهم تقريرًا في الدردشة. وهذا ليس دليلًا على وقت حدوث الواقعة أو على حدوثها. لا تتضمن القائمة سجلات ما تقرر أو أُنجز؛ تحقق منها بشكل منفصل."},
	"count":         {"{count} messages read from {from} to {to}. Showing {shown}.", "{count} Nachrichten von {from} bis {to} gelesen. Angezeigt: {shown}.", "تمت قراءة {count} رسالة من {from} إلى {to}. المعروض: {shown}."},
	"empty":         {"No messages you can read were posted in this conversation from {from} to {to}.", "Von {from} bis {to} wurden in dieser Unterhaltung keine Nachrichten gepostet, die Sie lesen können.", "لم تُنشر في هذه المحادثة أي رسائل يمكنك قراءتها من {from} إلى {to}."},
	"all_removed":   {"Every entry has been removed. Restore them to see the timeline again.", "Alle Einträge wurden entfernt. Stellen Sie sie wieder her, um die Zeitleiste erneut zu sehen.", "تمت إزالة كل الإدخالات. استعدها لرؤية الجدول الزمني مرة أخرى."},
	"truncated":     {"Only the first {count} messages in this range were read. Later messages are not shown. Choose a shorter range to see them.", "Nur die ersten {count} Nachrichten dieses Zeitraums wurden gelesen. Spätere Nachrichten werden nicht angezeigt. Wählen Sie einen kürzeren Zeitraum, um sie zu sehen.", "تمت قراءة أول {count} رسالة فقط في هذا النطاق. لا تُعرض الرسائل الأحدث. اختر نطاقًا أقصر لرؤيتها."},
	"error":         {"The timeline could not be made. Your dates are kept. Check your connection and try again.", "Die Zeitleiste konnte nicht erstellt werden. Ihre Daten bleiben erhalten. Prüfen Sie die Verbindung und versuchen Sie es erneut.", "تعذّر إنشاء الجدول الزمني. تم الاحتفاظ بتواريخك. تحقق من اتصالك وحاول مرة أخرى."},
	"unavailable":   {"Messages cannot be read right now, so no timeline was made. Your dates are kept. Try again later, or open the conversation and look at those days there.", "Nachrichten können derzeit nicht gelesen werden, daher wurde keine Zeitleiste erstellt. Ihre Daten bleiben erhalten. Versuchen Sie es später erneut oder öffnen Sie die Unterhaltung und sehen Sie sich diese Tage dort an.", "لا يمكن قراءة الرسائل حاليًا، لذلك لم يُنشأ جدول زمني. تم الاحتفاظ بتواريخك. حاول لاحقًا، أو افتح المحادثة وراجع تلك الأيام فيها."},
	"range_missing": {"Choose both a first and a last day.", "Wählen Sie einen ersten und einen letzten Tag.", "اختر أول يوم وآخر يوم."},
	"range_order":   {"The first day cannot be after the last day.", "Der erste Tag darf nicht nach dem letzten Tag liegen.", "لا يمكن أن يكون أول يوم بعد آخر يوم."},
	"range_long":    {"Choose a range of at most {days} days.", "Wählen Sie einen Zeitraum von höchstens {days} Tagen.", "اختر نطاقًا لا يزيد عن {days} يومًا."},
	"reported":      {"{author} reported", "{author} berichtete", "أبلغ {author}"},
	"posted":        {"Posted {time}", "Gepostet {time}", "نُشرت {time}"},
	"flag_edited":   {"Edited after it was posted. This is its current wording.", "Nach dem Posten bearbeitet. Dies ist der aktuelle Wortlaut.", "عُدّلت بعد نشرها. هذه صياغتها الحالية."},
	"flag_thread":   {"Reply in a thread", "Antwort in einem Thread", "رد في سلسلة"},
	"flag_part":     {"Addressed to part of this channel. Left out of the draft.", "An einen Teil dieses Kanals gerichtet. Im Entwurf nicht enthalten.", "موجّهة إلى جزء من هذه القناة. لا تدخل في المسودة."},
	"flag_time":     {"Names a time of its own. The posted time may not be when it happened.", "Nennt eine eigene Zeit. Die Postzeit ist möglicherweise nicht der Zeitpunkt des Ereignisses.", "تذكر وقتًا خاصًا بها. قد لا يكون وقت النشر هو وقت الحدوث."},
	"open":          {"Open message", "Nachricht öffnen", "فتح الرسالة"},
	"open_label":    {"Open the message from {author}, {time}", "Nachricht von {author}, {time}, öffnen", "فتح رسالة {author}، {time}"},
	"remove":        {"Remove", "Entfernen", "إزالة"},
	"remove_label":  {"Remove from the timeline: {author}, {time}", "Aus der Zeitleiste entfernen: {author}, {time}", "إزالة من الجدول الزمني: {author}، {time}"},
	"removed":       {"Removed from the timeline: {author}, {time}", "Aus der Zeitleiste entfernt: {author}, {time}", "أُزيلت من الجدول الزمني: {author}، {time}"},
	"restore":       {"Restore removed entries ({count})", "Entfernte Einträge wiederherstellen ({count})", "استعادة الإدخالات المُزالة ({count})"},
	"gap":           {"No messages for {span} before the next one.", "{span} lang keine Nachrichten bis zur nächsten.", "لا رسائل لمدة {span} قبل التالية."},
	"hours":         {"{n} hours", "{n} Stunden", "{n} ساعة"},
	"days":          {"{n} days", "{n} Tage", "{n} يومًا"},
	"draft":         {"Put in message box", "In Nachrichtenfeld einfügen", "وضع في مربع الرسالة"},
	"draft_hint":    {"Access is checked again first. Entries addressed to part of the channel are left out. Nothing is sent.", "Zuerst wird der Zugriff erneut geprüft. An einen Teil des Kanals gerichtete Einträge bleiben draußen. Nichts wird gesendet.", "يُتحقق من الوصول مرة أخرى أولًا. تُستبعد الإدخالات الموجّهة إلى جزء من القناة. لا يُرسل أي شيء."},
	"checking":      {"Checking access…", "Zugriff wird geprüft …", "جارٍ التحقق من الوصول…"},
	"drafted":       {"The timeline is in your message box. It has not been sent.", "Die Zeitleiste steht in Ihrem Nachrichtenfeld. Sie wurde nicht gesendet.", "الجدول الزمني في مربع رسالتك. لم يُرسل."},
	"nothing":       {"Nothing to put in the message box: every entry is removed or left out.", "Nichts einzufügen: Alle Einträge sind entfernt oder ausgelassen.", "لا شيء لوضعه في مربع الرسالة: كل الإدخالات أُزيلت أو استُبعدت."},
	"changed":       {"{count} entries were changed or can no longer be read since this timeline was made, so nothing was put in the message box. Read again, then try once more.", "{count} Einträge wurden seit der Erstellung geändert oder sind nicht mehr lesbar, daher wurde nichts eingefügt. Lesen Sie erneut und versuchen Sie es noch einmal.", "تغيّر {count} من الإدخالات أو لم يعد قابلًا للقراءة منذ إنشاء هذا الجدول، لذلك لم يوضع شيء في مربع الرسالة. اقرأ مرة أخرى ثم حاول ثانية."},
	"check_failed":  {"Access could not be checked, so nothing was put in the message box. Try again.", "Der Zugriff konnte nicht geprüft werden, daher wurde nichts eingefügt. Versuchen Sie es erneut.", "تعذّر التحقق من الوصول، لذلك لم يوضع شيء في مربع الرسالة. حاول مرة أخرى."},
	"list_label":    {"Reports in time order", "Berichte in zeitlicher Reihenfolge", "التقارير بترتيب زمني"},
	"this":          {"this conversation", "diese Unterhaltung", "هذه المحادثة"},
	"unknown":       {"Someone", "Jemand", "شخص ما"},

	// The words of the draft itself.
	"d_title":     {"Event timeline for {name}, {from} to {to}", "Ereignis-Zeitleiste für {name}, {from} bis {to}", "الجدول الزمني للأحداث لـ {name}، من {from} إلى {to}"},
	"d_note":      {"Draft for review. Each line is when someone posted a report in chat. It does not confirm when anything happened.", "Entwurf zur Prüfung. Jede Zeile zeigt, wann jemand im Chat etwas gemeldet hat. Sie bestätigt nicht, wann etwas geschehen ist.", "مسودة للمراجعة. كل سطر هو وقت نشر أحدهم تقريرًا في الدردشة، ولا يؤكد وقت حدوث أي شيء."},
	"d_reported":  {"{author} reported", "{author} berichtete", "أبلغ {author}"},
	"d_source":    {"Source: {link}", "Quelle: {link}", "المصدر: {link}"},
	"d_gap":       {"(No messages for {span}.)", "(Keine Nachrichten für {span}.)", "(لا رسائل لمدة {span}.)"},
	"d_truncated": {"Later messages in this range were not read.", "Spätere Nachrichten in diesem Zeitraum wurden nicht gelesen.", "لم تُقرأ الرسائل الأحدث في هذا النطاق."},
	"d_left":      {"Left out: {count} addressed to part of the channel.", "Ausgelassen: {count} an einen Teil des Kanals gerichtet.", "مستبعد: {count} موجّه إلى جزء من القناة."},
	"d_edited":    {"edited after posting", "nach dem Posten bearbeitet", "عُدّلت بعد النشر"},
	"d_thread":    {"reply in a thread", "Antwort in einem Thread", "رد في سلسلة"},
	"d_time":      {"names a time of its own", "nennt eine eigene Zeit", "تذكر وقتًا خاصًا بها"},
}

func issue3964Text(m Model, key string) string { return issue3964Word(m.Locale, key) }

func issue3964Word(locale, key string) string {
	copy := issue3964Copy[key]
	return chatbug039Text("chat.issue3964."+key, copy[chatbug039LocaleIndex(locale)], copy[0])
}

// TimelineText is a word of the timeline in the given locale, for the notices
// the browser raises outside the list itself.
func TimelineText(locale, key string) string { return issue3964Word(locale, key) }

// TimelineRange reads the two days a person typed. Days are calendar days in the
// person's own time zone; end is the start of the day after the last day, so a
// message posted any time on the last day is inside. problem is "", or the copy
// key of what is wrong: range_missing, range_order or range_long.
func TimelineRange(from, to string) (start, end time.Time, problem string) {
	first, e1 := time.ParseInLocation("2006-01-02", strings.TrimSpace(from), time.Local)
	last, e2 := time.ParseInLocation("2006-01-02", strings.TrimSpace(to), time.Local)
	if e1 != nil || e2 != nil {
		return time.Time{}, time.Time{}, "range_missing"
	}
	if last.Before(first) {
		return time.Time{}, time.Time{}, "range_order"
	}
	end = last.AddDate(0, 0, 1)
	if end.Sub(first) > time.Duration(TimelineMaxDays)*24*time.Hour+2*time.Hour {
		return time.Time{}, time.Time{}, "range_long"
	}
	return first, end, ""
}

// TimelineSearchDays are the two day words the Chat search needs to cover a
// range. The search compares calendar days in UTC and excludes both ends, so
// they sit one UTC day outside the first and last instant of the range and no
// further: a wider window would spend the page budget on messages that are then
// dropped. Entries are kept by exact time afterwards.
func TimelineSearchDays(start, end time.Time) (after, before string) {
	return start.UTC().AddDate(0, 0, -1).Format("2006-01-02"), end.UTC().AddDate(0, 0, 1).Format("2006-01-02")
}

var timelineClock = regexp.MustCompile(`(?i)(?:^|[^0-9])(?:[01]?[0-9]|2[0-3])[:.][0-5][0-9](?:[^0-9]|$)|(?:^|[^0-9])(?:1[0-2]|0?[1-9])\s?(?:am|pm)\b|\b\d{1,2}\s?uhr\b`)

var timelineWords = []string{
	"yesterday", "this morning", "this afternoon", "last night", "tonight", "earlier today", "minutes ago", "hours ago", "days ago", "last week", "just now",
	"gestern", "heute morgen", "heute nacht", "vorhin", "letzte nacht", "vor kurzem", "letzte woche", "stunden her", "minuten her",
	"أمس", "هذا الصباح", "الليلة الماضية", "الليلة", "منذ ", "قبل قليل", "الأسبوع الماضي",
}

// TimelineMentionsOwnTime is true when a message names a time or a relative day
// of its own ("around 10:00", "yesterday"), so that the time it was posted may
// not be the time of what it reports.
func TimelineMentionsOwnTime(text string) bool {
	if timelineClock.MatchString(text) {
		return true
	}
	lower := strings.ToLower(text)
	for _, word := range timelineWords {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

// timelineInRange keeps the entries posted inside [start, end), oldest first.
func timelineInRange(entries []TimelineEntry, start, end time.Time) []TimelineEntry {
	out := make([]TimelineEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.ID == "" || entry.At.IsZero() {
			continue
		}
		if !start.IsZero() && entry.At.Before(start) || !end.IsZero() && !entry.At.Before(end) {
			continue
		}
		out = append(out, entry)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// timelineGaps is, per entry, how long it was since the entry before it, for the
// entries that came after a silence worth pointing out.
func timelineGaps(entries []TimelineEntry) map[string]time.Duration {
	gaps := map[string]time.Duration{}
	for i := 1; i < len(entries); i++ {
		if d := entries[i].At.Sub(entries[i-1].At); d >= timelineGapAfter {
			gaps[entries[i].ID] = d
		}
	}
	return gaps
}

func timelineSpan(locale string, d time.Duration) string {
	if d >= 48*time.Hour {
		return chatcmd002Fill(issue3964Word(locale, "days"), "n", chatNumeral(locale, strconv.Itoa(int(d.Hours()/24))))
	}
	hours := int(d.Hours())
	if hours < 1 {
		hours = 1
	}
	return chatcmd002Fill(issue3964Word(locale, "hours"), "n", chatNumeral(locale, strconv.Itoa(hours)))
}

func timelineStamp(locale string, at time.Time) string {
	local := at.Local()
	return formatShortDate(locale, local) + ", " + chat5Clock(locale, local)
}

func timelineDay(locale, day string) string {
	t, err := time.ParseInLocation("2006-01-02", day, time.Local)
	if err != nil {
		return ""
	}
	return formatShortDate(locale, t)
}

func timelineAuthor(m Model, id string) string {
	if name := chatKnownName(m, id); name != "" {
		return name
	}
	return issue3964Text(m, "unknown")
}

func timelineConversationName(m Model) string {
	c := m.selected()
	name := displayName(m, c)
	if name == "" {
		return issue3964Text(m, "this")
	}
	if c.Kind == PublicChannel || c.Kind == PrivateChannel {
		return "#" + strings.TrimPrefix(name, "#")
	}
	return name
}

// timelineKept is the entries that are in the view and not removed, and how many
// of the view's entries are removed.
func timelineKept(entries []TimelineEntry, removed map[string]bool) (kept []TimelineEntry, gone int) {
	for _, entry := range entries {
		if removed[entry.ID] {
			gone++
			continue
		}
		kept = append(kept, entry)
	}
	return kept, gone
}

// timelineDraftable is the kept entries a draft may carry: everything except what
// was addressed to part of the channel, and how many were left out for that.
func timelineDraftable(kept []TimelineEntry) (use []TimelineEntry, left int) {
	for _, entry := range kept {
		if entry.Restricted {
			left++
			continue
		}
		use = append(use, entry)
	}
	return use, left
}

// timelineDraftText writes the timeline as text for the message box. all is every
// entry read (it decides where the silences are); use is the entries to write.
// Message text is untrusted: it is one quoted, shortened line, and an @ cannot
// notify anyone.
func timelineDraftText(m Model, view TimelineView, all, use []TimelineEntry, left int, origin string) string {
	locale := m.Locale
	gaps := timelineGaps(all)
	var b strings.Builder
	b.WriteString(chatcmd002Fill(issue3964Word(locale, "d_title"), "name", briefLine(timelineConversationName(m), 80), "from", timelineDay(locale, view.From), "to", timelineDay(locale, view.To)))
	b.WriteString("\n")
	b.WriteString(issue3964Word(locale, "d_note"))
	b.WriteString("\n")
	for i, entry := range use {
		b.WriteString("\n")
		if gap, ok := gaps[entry.ID]; ok {
			b.WriteString(chatcmd002Fill(issue3964Word(locale, "d_gap"), "span", timelineSpan(locale, gap)))
			b.WriteString("\n")
		}
		line := chatNumeral(locale, strconv.Itoa(i+1)) + ". " + timelineStamp(locale, entry.At) + " - " +
			chatcmd002Fill(issue3964Word(locale, "d_reported"), "author", briefLine(timelineAuthor(m, entry.AuthorID), 80)) + ": “" + MeetingBriefExcerpt(entry.Text) + "”"
		var flags []string
		if entry.Edited {
			flags = append(flags, issue3964Word(locale, "d_edited"))
		}
		if entry.InThread {
			flags = append(flags, issue3964Word(locale, "d_thread"))
		}
		if TimelineMentionsOwnTime(entry.Text) {
			flags = append(flags, issue3964Word(locale, "d_time"))
		}
		if len(flags) > 0 {
			line += " (" + strings.Join(flags, "; ") + ")"
		}
		b.WriteString(line)
		b.WriteString("\n")
		if entry.Href != "" {
			b.WriteString("   " + chatcmd002Fill(issue3964Word(locale, "d_source"), "link", strings.TrimRight(origin, "/")+entry.Href))
			b.WriteString("\n")
		}
	}
	if left > 0 {
		b.WriteString("\n" + chatcmd002Fill(issue3964Word(locale, "d_left"), "count", chatNumeral(locale, strconv.Itoa(left))) + "\n")
	}
	if view.Truncated {
		b.WriteString("\n" + issue3964Word(locale, "d_truncated") + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func timelineDefaultDays(now time.Time) (from, to string) {
	return now.AddDate(0, 0, -6).Format("2006-01-02"), now.Format("2006-01-02")
}

// issue3964Section is the Event timeline entry of Conversation details. It is nil
// when the client cannot read a timeline. Collapsed it shows nothing of the
// messages.
func issue3964Section(m Model, h handlers) ui.Node {
	if m.Callbacks.LoadTimeline == nil || m.SelectedID == "" {
		return nil
	}
	view := m.Timeline
	if view.ConversationID != m.SelectedID {
		view = TimelineView{}
	}
	open := h.local.timelineOpen
	toggleLabel := issue3964Text(m, "show")
	if open {
		toggleLabel = issue3964Text(m, "hide")
	}
	children := []ui.Node{html.Div(html.Props{Class: "details-section-head"},
		html.H3(html.Props{Text: issue3964Text(m, "title")}),
		html.Div(html.Props{Class: "details-section-actions"},
			html.Button(html.Props{Class: "button secondary small", Type: "button", Data: map[string]string{"action": "timeline-toggle"},
				Aria: map[string]string{"expanded": boolString(open), "controls": "chat-details-timeline-body"}, Text: toggleLabel})))}
	if open {
		children = append(children, issue3964Body(m, h, view))
	}
	return html.Section(html.Props{ID: "chat-details-timeline", Class: "details-section details-timeline", Dir: direction(m.Locale), Aria: map[string]string{"label": issue3964Text(m, "title")}}, children...)
}

func issue3964Body(m Model, h handlers, view TimelineView) ui.Node {
	from, to := view.From, view.To
	if from == "" || to == "" {
		from, to = timelineDefaultDays(time.Now())
	}
	replies := true
	if view.Status != "" {
		replies = view.Replies
	}
	busy := view.Status == TimelineLoading
	makeLabel := issue3964Text(m, "make")
	if view.Status == TimelineReady || view.Status == TimelineError || view.Status == TimelineUnavailable {
		makeLabel = issue3964Text(m, "refresh")
	}
	form := html.Div(html.Props{Class: "timeline-form"},
		html.P(html.Props{Class: "field-hint timeline-intro", Text: issue3964Text(m, "intro")}),
		html.P(html.Props{Class: "timeline-source", Dir: "auto", Text: chatcmd002Fill(issue3964Text(m, "source"), "name", timelineConversationName(m))}),
		html.Div(html.Props{Class: "timeline-dates"},
			html.Div(html.Props{Class: "timeline-date"},
				html.Label(html.Props{For: "chat-timeline-from", Text: issue3964Text(m, "first")}),
				html.Input(html.Props{ID: "chat-timeline-from", Class: "chat-input", Type: "date", Name: "from", Data: map[string]string{"chat-value": from}, Aria: map[string]string{"describedby": "chat-timeline-status"}})),
			html.Div(html.Props{Class: "timeline-date"},
				html.Label(html.Props{For: "chat-timeline-to", Text: issue3964Text(m, "last")}),
				html.Input(html.Props{ID: "chat-timeline-to", Class: "chat-input", Type: "date", Name: "to", Data: map[string]string{"chat-value": to}, Aria: map[string]string{"describedby": "chat-timeline-status"}}))),
		html.Label(html.Props{Class: "timeline-replies"},
			html.Input(html.Props{ID: "chat-timeline-replies", Type: "checkbox", Checked: replies}),
			html.Span(html.Props{Text: issue3964Text(m, "replies")})),
		html.Button(html.Props{Class: "button small", Type: "button", Disabled: busy || m.Callbacks.LoadTimeline == nil, Data: map[string]string{"action": "timeline-make"}, Text: makeLabel}))
	nodes := []ui.Node{form}

	status, role := "", "status"
	if h.local.timelineNote != "" {
		status = timelineNoteText(m, h.local.timelineNote, h.local.timelineChanged)
		if h.local.timelineNote == "changed" || h.local.timelineNote == "check_failed" || strings.HasPrefix(h.local.timelineNote, "range_") {
			role = "alert"
		}
	}
	var list ui.Node
	var after []ui.Node
	switch {
	case view.Status == TimelineLoading:
		status = issue3964Text(m, "loading")
	case view.Status == TimelineError || view.Status == TimelineUnavailable:
		status, role = issue3964Text(m, view.Status), "alert"
		after = append(after, html.Button(html.Props{Class: "button secondary small", Type: "button", Data: map[string]string{"action": "timeline-make"}, Text: issue3964Text(m, "retry")}))
	case view.Status == TimelineReady:
		list, status, role, after = issue3964Ready(m, h, view, status, role)
	}
	nodes = append(nodes, html.P(html.Props{ID: "chat-timeline-status", Class: "timeline-status", TabIndex: -1, Role: role, Aria: map[string]string{"live": "polite"}, Text: status}))
	nodes = append(nodes, after...)
	if list != nil {
		nodes = append(nodes, list)
	}
	return html.Div(html.Props{ID: "chat-details-timeline-body", Class: "timeline-body"}, nodes...)
}

func timelineNoteText(m Model, key string, changed int) string {
	text := issue3964Text(m, key)
	switch key {
	case "range_long":
		text = chatcmd002Fill(text, "days", m.n(TimelineMaxDays))
	case "changed":
		text = chatcmd002Fill(text, "count", m.n(changed))
	}
	return text
}

func issue3964Ready(m Model, h handlers, view TimelineView, status, role string) (ui.Node, string, string, []ui.Node) {
	var after []ui.Node
	entries := timelineInRange(view.Entries, mustDay(view.From), mustEnd(view.To))
	if len(entries) == 0 {
		status = chatcmd002Fill(issue3964Text(m, "empty"), "from", timelineDay(m.Locale, view.From), "to", timelineDay(m.Locale, view.To))
		return nil, status, role, after
	}
	kept, gone := timelineKept(entries, h.local.timelineRemoved)
	gaps := timelineGaps(entries)
	rows := make([]ui.Node, 0, len(kept)*2)
	for _, entry := range kept {
		if gap, ok := gaps[entry.ID]; ok {
			rows = append(rows, html.Li(html.Props{Class: "timeline-gap", Role: "note", Text: chatcmd002Fill(issue3964Text(m, "gap"), "span", timelineSpan(m.Locale, gap))}))
		}
		rows = append(rows, issue3964Row(m, entry))
	}
	var list ui.Node
	if len(rows) > 0 {
		list = html.Ol(html.Props{Class: "timeline-list", Aria: map[string]string{"label": issue3964Text(m, "list_label")}}, rows...)
	}
	if status == "" {
		status = chatcmd002Fill(issue3964Text(m, "count"), "count", m.n(len(entries)), "from", timelineDay(m.Locale, view.From), "to", timelineDay(m.Locale, view.To), "shown", m.n(len(kept)))
		if len(kept) == 0 {
			status = issue3964Text(m, "all_removed")
		}
	}
	after = append(after, html.P(html.Props{Class: "field-hint timeline-note", Text: issue3964Text(m, "reports_note")}))
	if view.Truncated {
		after = append(after, html.P(html.Props{Class: "timeline-truncated", Role: "alert", Text: chatcmd002Fill(issue3964Text(m, "truncated"), "count", m.n(TimelineMaxEntries))}))
	}
	actions := []ui.Node{}
	use, _ := timelineDraftable(kept)
	draftLabel := issue3964Text(m, "draft")
	if h.local.timelineChecking {
		draftLabel = issue3964Text(m, "checking")
	}
	actions = append(actions, html.Button(html.Props{Class: "button small", Type: "button", Disabled: h.local.timelineChecking || len(use) == 0 || m.Callbacks.CheckTimelineEntries == nil,
		Data: map[string]string{"action": "timeline-draft"}, Aria: map[string]string{"describedby": "chat-timeline-draft-hint"}, Text: draftLabel}))
	if gone > 0 {
		actions = append(actions, html.Button(html.Props{Class: "button secondary small", Type: "button", Data: map[string]string{"action": "timeline-restore"},
			Text: chatcmd002Fill(issue3964Text(m, "restore"), "count", m.n(gone))}))
	}
	after = append(after, html.Div(html.Props{Class: "timeline-actions"}, actions...),
		html.P(html.Props{ID: "chat-timeline-draft-hint", Class: "field-hint timeline-hint", Text: issue3964Text(m, "draft_hint")}))
	return list, status, role, after
}

func mustDay(day string) time.Time {
	start, _, problem := TimelineRange(day, day)
	if problem != "" {
		return time.Time{}
	}
	return start
}

func mustEnd(day string) time.Time {
	_, end, problem := TimelineRange(day, day)
	if problem != "" {
		return time.Time{}
	}
	return end
}

func issue3964Row(m Model, entry TimelineEntry) ui.Node {
	author := timelineAuthor(m, entry.AuthorID)
	stamp := timelineStamp(m.Locale, entry.At)
	head := html.Div(html.Props{Class: "timeline-entry-head"},
		html.Strong(html.Props{Class: "timeline-author", Dir: "auto", Text: chatcmd002Fill(issue3964Text(m, "reported"), "author", author)}),
		html.Time(html.Props{Class: "timeline-time", Raw: map[string]any{"datetime": entry.At.UTC().Format(time.RFC3339)}, Text: chatcmd002Fill(issue3964Text(m, "posted"), "time", stamp)}))
	flags := []ui.Node{}
	if entry.Edited {
		flags = append(flags, html.Li(html.Props{Class: "timeline-flag", Text: issue3964Text(m, "flag_edited")}))
	}
	if entry.InThread {
		flags = append(flags, html.Li(html.Props{Class: "timeline-flag", Text: issue3964Text(m, "flag_thread")}))
	}
	if entry.Restricted {
		flags = append(flags, html.Li(html.Props{Class: "timeline-flag", Text: issue3964Text(m, "flag_part")}))
	}
	if TimelineMentionsOwnTime(entry.Text) {
		flags = append(flags, html.Li(html.Props{Class: "timeline-flag", Text: issue3964Text(m, "flag_time")}))
	}
	children := []ui.Node{head, html.P(html.Props{Class: "timeline-text", Dir: "auto", Text: "“" + MeetingBriefExcerpt(entry.Text) + "”"})}
	if len(flags) > 0 {
		children = append(children, html.Ul(html.Props{Class: "timeline-flags"}, flags...))
	}
	open := issue3964Text(m, "open")
	remove := issue3964Text(m, "remove")
	openLabel := chatcmd002Fill(issue3964Text(m, "open_label"), "author", author, "time", stamp)
	removeLabel := chatcmd002Fill(issue3964Text(m, "remove_label"), "author", author, "time", stamp)
	children = append(children, html.Div(html.Props{Class: "timeline-entry-actions"},
		html.Button(html.Props{Class: "button secondary small", Type: "button", Disabled: m.Callbacks.OpenTimelineSource == nil, Data: map[string]string{"action": "timeline-open", "id": entry.ID}, Aria: map[string]string{"label": openLabel}, Title: openLabel, Text: open}),
		html.Button(html.Props{Class: "button secondary small", Type: "button", Data: map[string]string{"action": "timeline-remove", "id": entry.ID}, Aria: map[string]string{"label": removeLabel}, Title: removeLabel, Text: remove})))
	return html.Li(html.Props{Class: "timeline-entry", Data: map[string]string{"timeline-id": entry.ID}}, children...)
}

// issue3964Click handles the entry's buttons. It reports whether the press was
// one of them.
func issue3964Click(e ui.MouseEvent, m Model, local localStore, drafts *browserDrafts) bool {
	action, id, _ := eventAction(e)
	if !strings.HasPrefix(action, "timeline-") {
		return false
	}
	switch action {
	case "timeline-toggle":
		open := !local.get().timelineOpen
		local.update(func(u *localUI) { u.timelineOpen = open; u.timelineNote = "" })
	case "timeline-make":
		issue3964Make(m, local)
	case "timeline-remove":
		if id != "" {
			local.update(func(u *localUI) {
				next := map[string]bool{id: true}
				for k, v := range u.timelineRemoved {
					next[k] = v
				}
				u.timelineRemoved, u.timelineNote = next, ""
			})
			focusField("chat-timeline-status")
		}
	case "timeline-restore":
		local.update(func(u *localUI) { u.timelineRemoved, u.timelineNote = nil, "" })
	case "timeline-open":
		if m.Callbacks.OpenTimelineSource != nil && id != "" {
			m.Callbacks.OpenTimelineSource(id)
		}
	case "timeline-draft":
		issue3964Draft(m, local, drafts)
	default:
		return false
	}
	return true
}

// issue3964Make reads the dates and asks the browser for the timeline. A range
// that cannot be read is said in words and nothing is asked.
func issue3964Make(m Model, local localStore) {
	if m.Callbacks.LoadTimeline == nil {
		return
	}
	from, to := strings.TrimSpace(domValue("chat-timeline-from")), strings.TrimSpace(domValue("chat-timeline-to"))
	if from == "" && to == "" && m.Timeline.ConversationID == m.SelectedID {
		from, to = m.Timeline.From, m.Timeline.To
	}
	replies := domChecked("chat-timeline-replies")
	if from == "" || to == "" {
		from, to = timelineDefaultDays(time.Now())
		replies = true
	}
	if _, _, problem := TimelineRange(from, to); problem != "" {
		local.update(func(u *localUI) { u.timelineNote = problem })
		focusField("chat-timeline-status")
		return
	}
	local.update(func(u *localUI) { u.timelineRemoved, u.timelineNote = nil, "" })
	m.Callbacks.LoadTimeline(from, to, replies)
}

// issue3964Draft checks access to every entry the draft would carry, then writes
// the draft into the open conversation's message box. It never sends.
func issue3964Draft(m Model, local localStore, drafts *browserDrafts) {
	view := m.Timeline
	if view.ConversationID != m.SelectedID || m.Callbacks.CheckTimelineEntries == nil || local.get().timelineChecking {
		return
	}
	all := timelineInRange(view.Entries, mustDay(view.From), mustEnd(view.To))
	kept, _ := timelineKept(all, local.get().timelineRemoved)
	use, left := timelineDraftable(kept)
	if len(use) == 0 {
		local.update(func(u *localUI) { u.timelineNote = "nothing" })
		return
	}
	ids := make([]string, len(use))
	for i, entry := range use {
		ids[i] = entry.ID
	}
	room := m.SelectedID
	local.update(func(u *localUI) { u.timelineChecking, u.timelineNote = true, "" })
	m.Callbacks.CheckTimelineEntries(ids, func(check TimelineCheck) {
		ui.PostAsync(func() {
			if local.get().room != room {
				local.update(func(u *localUI) { u.timelineChecking = false })
				return
			}
			switch {
			case check.Failed:
				local.update(func(u *localUI) { u.timelineChecking, u.timelineNote = false, "check_failed" })
			case check.Changed > 0:
				local.update(func(u *localUI) {
					u.timelineChecking, u.timelineNote, u.timelineChanged = false, "changed", check.Changed
				})
			default:
				text := timelineDraftText(m, view, all, use, left, chatmapOrigin())
				issue3964PutInDraft(m, local, drafts, text)
				local.update(func(u *localUI) { u.timelineChecking, u.timelineNote = false, "drafted" })
			}
		})
	})
}

// issue3964PutInDraft adds the timeline to the open conversation's message box
// without sending it.
func issue3964PutInDraft(m Model, local localStore, drafts *browserDrafts, text string) {
	if strings.TrimSpace(text) == "" || m.SelectedID == "" {
		return
	}
	existing := domDraftValue("chat-composer", m.SelectedID)
	if existing == "" {
		existing = m.Draft
	}
	merged := HandoverDraftText(existing, text)
	if drafts != nil {
		drafts.set(m.SelectedID, merged)
	}
	setDOMValue("chat-composer", merged)
	if m.Callbacks.DraftChanged != nil {
		m.Callbacks.DraftChanged(m.SelectedID, merged)
	}
	FocusComposerFor(m.SelectedID)
}

const issue3964Styles = `.chat-workspace .timeline-body{display:flex;flex-direction:column;gap:8px;min-width:0}` +
	`.chat-workspace .timeline-form{display:flex;flex-direction:column;gap:8px;min-width:0}` +
	`.chat-workspace .timeline-intro,.chat-workspace .timeline-source,.chat-workspace .timeline-note,.chat-workspace .timeline-hint{margin:0;overflow-wrap:anywhere}` +
	`.chat-workspace .timeline-dates{display:flex;flex-wrap:wrap;gap:8px}.chat-workspace .timeline-date{display:flex;flex-direction:column;gap:4px;flex:1 1 9rem;min-width:0}` +
	`.chat-workspace .timeline-date .chat-input{min-width:0;box-sizing:border-box}` +
	`.chat-workspace .timeline-replies{display:flex;align-items:center;gap:8px;min-height:32px}` +
	`.chat-workspace .timeline-status{margin:0;color:var(--muted);font-size:.8125rem;overflow-wrap:anywhere}.chat-workspace .timeline-status:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}` +
	`.chat-workspace .timeline-truncated{margin:0;padding:8px;border:1px solid var(--line);border-inline-start-width:4px;border-radius:var(--hcm-radius-control);overflow-wrap:anywhere}` +
	`.chat-workspace .timeline-list{list-style:none;margin:0;padding:0;display:flex;flex-direction:column;gap:6px}` +
	`.chat-workspace .timeline-entry{display:flex;flex-direction:column;gap:4px;padding:8px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);min-width:0}` +
	`.chat-workspace .timeline-entry-head{display:flex;flex-wrap:wrap;justify-content:space-between;gap:2px 8px}.chat-workspace .timeline-time{color:var(--muted);font-size:.8125rem}` +
	`.chat-workspace .timeline-text{margin:0;overflow-wrap:anywhere}` +
	`.chat-workspace .timeline-flags{list-style:none;margin:0;padding:0;display:flex;flex-direction:column;gap:2px}.chat-workspace .timeline-flag{color:var(--muted);font-size:.8125rem;overflow-wrap:anywhere}` +
	`.chat-workspace .timeline-gap{padding:4px 8px;border-inline-start:2px dashed var(--line);color:var(--muted);font-size:.8125rem;list-style:none}` +
	`.chat-workspace .timeline-entry-actions,.chat-workspace .timeline-actions{display:flex;flex-wrap:wrap;gap:6px}` +
	`.chat-workspace .timeline-body .button:focus-visible,.chat-workspace .timeline-body .chat-input:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}` +
	`@media(max-width:560px){.chat-workspace .timeline-body .button,.chat-workspace .timeline-body .chat-input{min-height:44px}.chat-workspace .timeline-replies{min-height:44px}.chat-workspace .timeline-replies input{inline-size:24px;block-size:24px}}`

// TimelineWithinRange keeps the entries posted inside [start, end), oldest first.
func TimelineWithinRange(entries []TimelineEntry, start, end time.Time) []TimelineEntry {
	return timelineInRange(entries, start, end)
}
