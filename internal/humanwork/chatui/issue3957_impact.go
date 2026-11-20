package chatui

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/localize"
)

// Issue 3957: Check downstream impact. In a thread, a message that proposes a
// change offers a private, bounded check. The person confirms the proposed date
// (and, if they like, who is affected) and the check reads the one source this
// product can query for them today: the work linked to this conversation, as
// this person is allowed to see it. It separates explicit links (a task made
// from a message in this conversation) that are due on or near the date from
// everything else, says plainly what was not checked, and never says the
// organization is clear. The person can dismiss findings and draft a short
// question into the thread's reply box; they edit it and send it themselves.
// Nothing is rescheduled, changed or sent for them. The state lives only in the
// page, so it is private, and the draft names no task titles or owners, so it
// cannot show anyone work they may not open.

// ImpactNearDays is how many days either side of the date still count as near.
const ImpactNearDays = 3

// ImpactFinding is one open, linked task due on or near the proposed date.
type ImpactFinding struct {
	Item LinkedWorkItem
	// Days is the due date minus the proposed date, in days.
	Days int
}

// ImpactResult is what one check found, with what it could and could not read.
type ImpactResult struct {
	Date     string
	Findings []ImpactFinding
	// Compared counts open linked tasks with a due date that were compared.
	Compared int
	// NoDate counts open linked tasks without a due date: they cannot be compared.
	NoDate int
	// Finished counts done or closed tasks, left out.
	Finished int
	// Actions counts proposed actions: they have no due date to compare.
	Actions int
	// Incomplete names the sources ("approvals", "tasks") that could not answer.
	Incomplete []string
	AsOf       time.Time
}

// ImpactState is the page's transient state of the check. The page sets Enabled
// only where the work of the conversation can be read for this person.
type ImpactState struct {
	Enabled bool
	// PostID is the message whose check is open ("" when none is).
	PostID     string
	Date       string
	Population string
	// Stage is "", "loading", "ready" or "error".
	Stage string
	// Error is "invalid_date" or a short code of a failed read.
	Error     string
	Result    ImpactResult
	Dismissed map[string]bool
	// Notice is "draft_ready", "nothing_to_ask" or "draft_failed".
	Notice string

	Open    func(postID, suggestedDate string)
	Close   func()
	Run     func(postID, date, population string)
	Dismiss func(id string)
	Restore func(id string)
	// Draft reads the work again (access and freshness are rechecked), then
	// calls deliver with the draft, or with "" when nothing is left to ask.
	Draft func(postID, date, population string, deliver func(text string))
}

var impactDatePattern = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)

// ImpactValidDate reports whether s is a calendar date written YYYY-MM-DD.
func ImpactValidDate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// ImpactDateIn returns the first ISO date a message writes, as a suggestion the
// person still confirms, or "".
func ImpactDateIn(body string) string {
	for _, candidate := range impactDatePattern.FindAllString(body, -1) {
		if ImpactValidDate(candidate) {
			return candidate
		}
	}
	return ""
}

func impactDay(s string) (time.Time, bool) {
	if len(s) >= 10 {
		s = s[:10]
	}
	at, err := time.Parse("2006-01-02", s)
	return at, err == nil
}

// ImpactAssess compares the linked work with the proposed date. Only open tasks
// with a due date are compared; it asks nothing of any owner and decides no
// access: items are what the server already showed this person.
func ImpactAssess(items []LinkedWorkItem, incomplete []string, asOf time.Time, date string) ImpactResult {
	out := ImpactResult{Date: date, Incomplete: append([]string(nil), incomplete...), AsOf: asOf}
	target, ok := impactDay(date)
	if !ok {
		return out
	}
	for _, item := range items {
		if item.Kind != "task" {
			out.Actions++
			continue
		}
		if item.State == "done" || item.State == "closed" {
			out.Finished++
			continue
		}
		due, ok := impactDay(item.DueDate)
		if !ok {
			out.NoDate++
			continue
		}
		out.Compared++
		days := int(due.Sub(target).Hours() / 24)
		if days >= -ImpactNearDays && days <= ImpactNearDays {
			out.Findings = append(out.Findings, ImpactFinding{Item: item, Days: days})
		}
	}
	sort.SliceStable(out.Findings, func(i, j int) bool {
		a, b := out.Findings[i].Days, out.Findings[j].Days
		if a < 0 {
			a = -a
		}
		if b < 0 {
			b = -b
		}
		return a < b
	})
	return out
}

// ImpactKept is how many findings the person has not dismissed.
func ImpactKept(r ImpactResult, dismissed map[string]bool) int {
	n := 0
	for _, f := range r.Findings {
		if !dismissed[f.Item.ID] {
			n++
		}
	}
	return n
}

const (
	keyIssue3957Menu         = "chat.issue3957.menu"
	keyIssue3957Title        = "chat.issue3957.title"
	keyIssue3957Private      = "chat.issue3957.private"
	keyIssue3957Scope        = "chat.issue3957.scope"
	keyIssue3957From         = "chat.issue3957.from"
	keyIssue3957DateLabel    = "chat.issue3957.date"
	keyIssue3957DateHint     = "chat.issue3957.date_hint"
	keyIssue3957PopLabel     = "chat.issue3957.population"
	keyIssue3957PopHint      = "chat.issue3957.population_hint"
	keyIssue3957Run          = "chat.issue3957.run"
	keyIssue3957Rerun        = "chat.issue3957.rerun"
	keyIssue3957Close        = "chat.issue3957.close"
	keyIssue3957Loading      = "chat.issue3957.loading"
	keyIssue3957InvalidDate  = "chat.issue3957.invalid_date"
	keyIssue3957Failed       = "chat.issue3957.failed"
	keyIssue3957Found        = "chat.issue3957.found"
	keyIssue3957None         = "chat.issue3957.none"
	keyIssue3957Checked      = "chat.issue3957.checked"
	keyIssue3957CheckedTasks = "chat.issue3957.checked_tasks"
	keyIssue3957NoDate       = "chat.issue3957.no_date"
	keyIssue3957Finished     = "chat.issue3957.finished"
	keyIssue3957Actions      = "chat.issue3957.actions"
	keyIssue3957NotChecked   = "chat.issue3957.not_checked"
	keyIssue3957NotCalendar  = "chat.issue3957.not_calendar"
	keyIssue3957NotOther     = "chat.issue3957.not_other"
	keyIssue3957NotInferred  = "chat.issue3957.not_inferred"
	keyIssue3957Partial      = "chat.issue3957.partial"
	keyIssue3957AsOf         = "chat.issue3957.as_of"
	keyIssue3957Explicit     = "chat.issue3957.explicit"
	keyIssue3957SameDay      = "chat.issue3957.same_day"
	keyIssue3957DaysBefore   = "chat.issue3957.days_before"
	keyIssue3957DaysAfter    = "chat.issue3957.days_after"
	keyIssue3957Owner        = "chat.issue3957.owner"
	keyIssue3957Due          = "chat.issue3957.due"
	keyIssue3957Open         = "chat.issue3957.open_task"
	keyIssue3957Dismiss      = "chat.issue3957.dismiss"
	keyIssue3957Restore      = "chat.issue3957.restore"
	keyIssue3957Dismissed    = "chat.issue3957.dismissed"
	keyIssue3957Draft        = "chat.issue3957.draft"
	keyIssue3957DraftHint    = "chat.issue3957.draft_hint"
	keyIssue3957DraftReady   = "chat.issue3957.draft_ready"
	keyIssue3957DraftNone    = "chat.issue3957.draft_none"
	keyIssue3957DraftFailed  = "chat.issue3957.draft_failed"
	keyIssue3957You          = "chat.issue3957.you"
	keyIssue3957DraftText    = "chat.issue3957.draft_text"
	keyIssue3957DraftPop     = "chat.issue3957.draft_text_pop"
)

var issue3957Copy = map[string]map[string]string{
	"en-US": {
		keyIssue3957Menu:         "Check downstream impact",
		keyIssue3957Title:        "Check downstream impact",
		keyIssue3957Private:      "Only you see this check. Nothing is rescheduled, changed or sent, and nobody is notified.",
		keyIssue3957Scope:        "This looks only at the work linked to this conversation that you may open. It does not look at calendars, other conversations or other projects, and it does not guess at relationships.",
		keyIssue3957From:         "Proposed change",
		keyIssue3957DateLabel:    "Proposed date",
		keyIssue3957DateHint:     "Confirm the date this change would move to.",
		keyIssue3957PopLabel:     "Who is affected (optional)",
		keyIssue3957PopHint:      "Used only in the question you may draft. The check cannot filter work by group.",
		keyIssue3957Run:          "Check linked work",
		keyIssue3957Rerun:        "Check again",
		keyIssue3957Close:        "Close",
		keyIssue3957Loading:      "Checking the work linked to this conversation",
		keyIssue3957InvalidDate:  "Choose a valid date to check.",
		keyIssue3957Failed:       "The linked work could not be read right now, so nothing is known yet. Try again.",
		keyIssue3957Found:        "{n} open task(s) linked to this conversation are due on or within {days} days of {date}.",
		keyIssue3957None:         "No linked task is due on or within {days} days of {date} in the sources checked. This does not mean nothing else is affected.",
		keyIssue3957Checked:      "Checked",
		keyIssue3957CheckedTasks: "{n} open task(s) with a due date, linked to this conversation",
		keyIssue3957NoDate:       "{n} open task(s) have no due date, so they could not be compared",
		keyIssue3957Finished:     "{n} finished or closed task(s) left out",
		keyIssue3957Actions:      "{n} proposed action(s) have no due date, so they were not compared",
		keyIssue3957NotChecked:   "Not checked",
		keyIssue3957NotCalendar:  "Calendars: not connected here",
		keyIssue3957NotOther:     "Other conversations and projects: not part of this check",
		keyIssue3957NotInferred:  "Relationships inferred from wording: not available",
		keyIssue3957Partial:      "Some work could not be read right now ({sources}), so this may be missing items.",
		keyIssue3957AsOf:         "Read at {time}",
		keyIssue3957Explicit:     "Explicit link: a task made from a message in this conversation",
		keyIssue3957SameDay:      "Due on the proposed date",
		keyIssue3957DaysBefore:   "Due {n} day(s) before the proposed date",
		keyIssue3957DaysAfter:    "Due {n} day(s) after the proposed date",
		keyIssue3957Owner:        "Owner",
		keyIssue3957Due:          "Due",
		keyIssue3957Open:         "Open",
		keyIssue3957Dismiss:      "Dismiss",
		keyIssue3957Restore:      "Restore",
		keyIssue3957Dismissed:    "Dismissed",
		keyIssue3957Draft:        "Draft a question for this thread",
		keyIssue3957DraftHint:    "The draft goes into the reply box for you to edit. It names no tasks or owners, and nothing is sent until you send it.",
		keyIssue3957DraftReady:   "A draft is in the reply box. Edit it, then send it when you are ready.",
		keyIssue3957DraftNone:    "Nothing is left to ask about now, so no draft was made.",
		keyIssue3957DraftFailed:  "The draft could not be prepared because the linked work could not be read again.",
		keyIssue3957You:          "You",
		keyIssue3957DraftText:    "Before we move this to {date}: I checked the work linked to this conversation and some of it is due on or near that date. Could the owners confirm the date still works, or say what would need to move?",
		keyIssue3957DraftPop:     "Before we move this to {date} for {population}: I checked the work linked to this conversation and some of it is due on or near that date. Could the owners confirm the date still works, or say what would need to move?",
	},
	"de-DE": {
		keyIssue3957Menu:         "Folgen prüfen",
		keyIssue3957Title:        "Folgen für abhängige Arbeit prüfen",
		keyIssue3957Private:      "Nur Sie sehen diese Prüfung. Es wird nichts verschoben, geändert oder gesendet, und niemand wird benachrichtigt.",
		keyIssue3957Scope:        "Geprüft wird nur die mit dieser Unterhaltung verknüpfte Arbeit, die Sie öffnen dürfen. Kalender, andere Unterhaltungen und andere Projekte werden nicht geprüft, und Zusammenhänge werden nicht geraten.",
		keyIssue3957From:         "Vorgeschlagene Änderung",
		keyIssue3957DateLabel:    "Vorgeschlagenes Datum",
		keyIssue3957DateHint:     "Bestätigen Sie das Datum, auf das die Änderung verschoben würde.",
		keyIssue3957PopLabel:     "Wer ist betroffen (optional)",
		keyIssue3957PopHint:      "Wird nur in der Frage verwendet, die Sie entwerfen können. Die Prüfung kann Arbeit nicht nach Gruppen filtern.",
		keyIssue3957Run:          "Verknüpfte Arbeit prüfen",
		keyIssue3957Rerun:        "Erneut prüfen",
		keyIssue3957Close:        "Schließen",
		keyIssue3957Loading:      "Mit dieser Unterhaltung verknüpfte Arbeit wird geprüft",
		keyIssue3957InvalidDate:  "Wählen Sie ein gültiges Datum zum Prüfen.",
		keyIssue3957Failed:       "Die verknüpfte Arbeit konnte gerade nicht gelesen werden, daher ist noch nichts bekannt. Versuchen Sie es erneut.",
		keyIssue3957Found:        "{n} offene Aufgabe(n) dieser Unterhaltung sind am {date} oder innerhalb von {days} Tagen davon fällig.",
		keyIssue3957None:         "In den geprüften Quellen ist keine verknüpfte Aufgabe am {date} oder innerhalb von {days} Tagen davon fällig. Das heißt nicht, dass sonst nichts betroffen ist.",
		keyIssue3957Checked:      "Geprüft",
		keyIssue3957CheckedTasks: "{n} offene Aufgabe(n) mit Fälligkeitsdatum, mit dieser Unterhaltung verknüpft",
		keyIssue3957NoDate:       "{n} offene Aufgabe(n) haben kein Fälligkeitsdatum und konnten nicht verglichen werden",
		keyIssue3957Finished:     "{n} erledigte oder geschlossene Aufgabe(n) ausgelassen",
		keyIssue3957Actions:      "{n} vorgeschlagene Aktion(en) haben kein Fälligkeitsdatum und wurden nicht verglichen",
		keyIssue3957NotChecked:   "Nicht geprüft",
		keyIssue3957NotCalendar:  "Kalender: hier nicht verbunden",
		keyIssue3957NotOther:     "Andere Unterhaltungen und Projekte: nicht Teil dieser Prüfung",
		keyIssue3957NotInferred:  "Aus dem Wortlaut abgeleitete Zusammenhänge: nicht verfügbar",
		keyIssue3957Partial:      "Manche Arbeit konnte gerade nicht gelesen werden ({sources}), daher fehlen möglicherweise Einträge.",
		keyIssue3957AsOf:         "Gelesen um {time}",
		keyIssue3957Explicit:     "Ausdrückliche Verknüpfung: eine aus einer Nachricht dieser Unterhaltung erstellte Aufgabe",
		keyIssue3957SameDay:      "Am vorgeschlagenen Datum fällig",
		keyIssue3957DaysBefore:   "{n} Tag(e) vor dem vorgeschlagenen Datum fällig",
		keyIssue3957DaysAfter:    "{n} Tag(e) nach dem vorgeschlagenen Datum fällig",
		keyIssue3957Owner:        "Verantwortlich",
		keyIssue3957Due:          "Fällig",
		keyIssue3957Open:         "Öffnen",
		keyIssue3957Dismiss:      "Verwerfen",
		keyIssue3957Restore:      "Wiederherstellen",
		keyIssue3957Dismissed:    "Verworfen",
		keyIssue3957Draft:        "Frage für diesen Thread entwerfen",
		keyIssue3957DraftHint:    "Der Entwurf kommt zum Bearbeiten ins Antwortfeld. Er nennt keine Aufgaben oder Verantwortlichen, und gesendet wird erst, wenn Sie senden.",
		keyIssue3957DraftReady:   "Ein Entwurf steht im Antwortfeld. Bearbeiten Sie ihn und senden Sie ihn, wenn Sie bereit sind.",
		keyIssue3957DraftNone:    "Es gibt jetzt nichts mehr zu fragen, daher wurde kein Entwurf erstellt.",
		keyIssue3957DraftFailed:  "Der Entwurf konnte nicht erstellt werden, weil die verknüpfte Arbeit nicht erneut gelesen werden konnte.",
		keyIssue3957You:          "Sie",
		keyIssue3957DraftText:    "Bevor wir dies auf den {date} verschieben: Ich habe die mit dieser Unterhaltung verknüpfte Arbeit geprüft, und einiges ist an oder um dieses Datum fällig. Können die Verantwortlichen bestätigen, dass das Datum passt, oder sagen, was verschoben werden müsste?",
		keyIssue3957DraftPop:     "Bevor wir dies auf den {date} für {population} verschieben: Ich habe die mit dieser Unterhaltung verknüpfte Arbeit geprüft, und einiges ist an oder um dieses Datum fällig. Können die Verantwortlichen bestätigen, dass das Datum passt, oder sagen, was verschoben werden müsste?",
	},
	"ar": {
		keyIssue3957Menu:         "فحص الأثر على العمل التابع",
		keyIssue3957Title:        "فحص الأثر على العمل التابع",
		keyIssue3957Private:      "أنت وحدك ترى هذا الفحص. لا يُعاد جدولة أي شيء ولا يُغيَّر ولا يُرسل، ولا يُبلَّغ أحد.",
		keyIssue3957Scope:        "يفحص هذا فقط العمل المرتبط بهذه المحادثة الذي يحق لك فتحه. لا يفحص التقويمات ولا المحادثات الأخرى ولا المشاريع الأخرى، ولا يخمّن العلاقات.",
		keyIssue3957From:         "التغيير المقترح",
		keyIssue3957DateLabel:    "التاريخ المقترح",
		keyIssue3957DateHint:     "أكّد التاريخ الذي سينتقل إليه هذا التغيير.",
		keyIssue3957PopLabel:     "من المتأثرون (اختياري)",
		keyIssue3957PopHint:      "يُستخدم فقط في السؤال الذي قد تصيغه. لا يستطيع الفحص تصفية العمل حسب المجموعة.",
		keyIssue3957Run:          "فحص العمل المرتبط",
		keyIssue3957Rerun:        "فحص مرة أخرى",
		keyIssue3957Close:        "إغلاق",
		keyIssue3957Loading:      "جارٍ فحص العمل المرتبط بهذه المحادثة",
		keyIssue3957InvalidDate:  "اختر تاريخاً صالحاً للفحص.",
		keyIssue3957Failed:       "تعذّرت قراءة العمل المرتبط الآن، لذا لا شيء معروف بعد. حاول مرة أخرى.",
		keyIssue3957Found:        "{n} من المهام المفتوحة المرتبطة بهذه المحادثة مستحقة في {date} أو خلال {days} أيام منه.",
		keyIssue3957None:         "لا توجد مهمة مرتبطة مستحقة في {date} أو خلال {days} أيام منه في المصادر التي فُحصت. هذا لا يعني أن لا شيء آخر يتأثر.",
		keyIssue3957Checked:      "ما فُحص",
		keyIssue3957CheckedTasks: "{n} من المهام المفتوحة ذات تاريخ استحقاق والمرتبطة بهذه المحادثة",
		keyIssue3957NoDate:       "{n} من المهام المفتوحة بلا تاريخ استحقاق، فلم تمكن مقارنتها",
		keyIssue3957Finished:     "{n} من المهام المنتهية أو المغلقة مستبعدة",
		keyIssue3957Actions:      "{n} من الإجراءات المقترحة بلا تاريخ استحقاق، فلم تُقارن",
		keyIssue3957NotChecked:   "ما لم يُفحص",
		keyIssue3957NotCalendar:  "التقويمات: غير متصلة هنا",
		keyIssue3957NotOther:     "المحادثات والمشاريع الأخرى: ليست جزءاً من هذا الفحص",
		keyIssue3957NotInferred:  "العلاقات المستنتجة من الصياغة: غير متاحة",
		keyIssue3957Partial:      "تعذّرت قراءة بعض العمل الآن ({sources})، لذا قد تنقص عناصر.",
		keyIssue3957AsOf:         "قُرئ في {time}",
		keyIssue3957Explicit:     "ارتباط صريح: مهمة أُنشئت من رسالة في هذه المحادثة",
		keyIssue3957SameDay:      "مستحقة في التاريخ المقترح",
		keyIssue3957DaysBefore:   "مستحقة قبل التاريخ المقترح بـ {n} يوم",
		keyIssue3957DaysAfter:    "مستحقة بعد التاريخ المقترح بـ {n} يوم",
		keyIssue3957Owner:        "المسؤول",
		keyIssue3957Due:          "الاستحقاق",
		keyIssue3957Open:         "فتح",
		keyIssue3957Dismiss:      "تجاهل",
		keyIssue3957Restore:      "استعادة",
		keyIssue3957Dismissed:    "المتجاهَلة",
		keyIssue3957Draft:        "صياغة سؤال لهذا النقاش",
		keyIssue3957DraftHint:    "تُوضع المسودة في خانة الرد لتعدّلها. لا تذكر مهام ولا مسؤولين، ولا يُرسل شيء حتى ترسله أنت.",
		keyIssue3957DraftReady:   "توجد مسودة في خانة الرد. عدّلها ثم أرسلها عندما تكون جاهزاً.",
		keyIssue3957DraftNone:    "لم يعد هناك ما يُسأل عنه، لذا لم تُنشأ مسودة.",
		keyIssue3957DraftFailed:  "تعذّر إعداد المسودة لأن العمل المرتبط لم يمكن قراءته مجدداً.",
		keyIssue3957You:          "أنت",
		keyIssue3957DraftText:    "قبل أن ننقل هذا إلى {date}: راجعت العمل المرتبط بهذه المحادثة وبعضه مستحق في هذا التاريخ أو قربه. هل يمكن للمسؤولين تأكيد أن التاريخ مناسب أو بيان ما يجب نقله؟",
		keyIssue3957DraftPop:     "قبل أن ننقل هذا إلى {date} لـ {population}: راجعت العمل المرتبط بهذه المحادثة وبعضه مستحق في هذا التاريخ أو قربه. هل يمكن للمسؤولين تأكيد أن التاريخ مناسب أو بيان ما يجب نقله؟",
	},
}

// Issue3957Text is the copy of the check in a locale.
func Issue3957Text(locale, key string) string {
	return chatbug039Text(key, issue3957Copy[chatEmojiLocale(locale)][key], issue3957Copy["en-US"][key])
}

// impactPopulationMax bounds the optional "who is affected" text.
const impactPopulationMax = 120

// ImpactDraft is the question drafted for the thread. It carries the date and
// the person's own words for who is affected, and nothing from any source: no
// title, owner, count or status, so it cannot expose work the thread's readers
// may not open. It is "" when no finding is left to ask about.
func ImpactDraft(locale, date, population string, kept int) string {
	if kept <= 0 || !ImpactValidDate(date) {
		return ""
	}
	population = strings.Join(strings.Fields(population), " ")
	if runes := []rune(population); len(runes) > impactPopulationMax {
		population = string(runes[:impactPopulationMax])
	}
	if population == "" {
		return strings.ReplaceAll(Issue3957Text(locale, keyIssue3957DraftText), "{date}", date)
	}
	text := strings.ReplaceAll(Issue3957Text(locale, keyIssue3957DraftPop), "{date}", date)
	return strings.ReplaceAll(text, "{population}", population)
}

func impactDateID(post string) string { return "impact-date-" + post }
func impactPopID(post string) string  { return "impact-population-" + post }

// impactAvailable says whether the thread pane may offer the check: the page can
// read the conversation's work, the viewer is signed in, the room is served by
// this company and it is not a restricted case conversation.
func impactAvailable(m Model) bool {
	if !m.Impact.Enabled || m.CurrentUser == "" || m.SelectedID == "" || m.ThreadParentID == "" {
		return false
	}
	c := m.selected()
	if c.Agent || issue3812Restricted(m) {
		return false
	}
	return c.HostTenantID == "" || c.HostTenantID == m.CurrentTenantID
}

// impactMenuItem is the row of a thread message's menu.
func impactMenuItem(m Model, msg Message) []ui.Node {
	if !impactAvailable(m) || m.Impact.Open == nil || msg.ID == "" || msg.Body == chat.RemovedByAdministrator || strings.TrimSpace(msg.Body) == "" {
		return nil
	}
	return []ui.Node{chatux022Item("menu-item impact-menu-item", "impact-open", msg.ID, "link", laneText(m, issue3957Copy, keyIssue3957Menu), false)}
}

func impactFill(text string, vars map[string]string) string {
	for name, value := range vars {
		text = strings.ReplaceAll(text, "{"+name+"}", value)
	}
	return text
}

func impactSourceName(locale, source string) string {
	switch source {
	case "approvals":
		return LinkedWorkText(locale, lwSrcApprovals)
	case "tasks":
		return LinkedWorkText(locale, lwSrcTasks)
	}
	return source
}

func impactRelation(m Model, days int) string {
	t := func(key string) string { return laneText(m, issue3957Copy, key) }
	switch {
	case days == 0:
		return t(keyIssue3957SameDay)
	case days < 0:
		return impactFill(t(keyIssue3957DaysBefore), map[string]string{"n": strconv.Itoa(-days)})
	}
	return impactFill(t(keyIssue3957DaysAfter), map[string]string{"n": strconv.Itoa(days)})
}

func impactFindingNode(m Model, f ImpactFinding, dismissed bool) ui.Node {
	t := func(key string) string { return laneText(m, issue3957Copy, key) }
	item := f.Item
	state := LinkedWorkStateText(m.Locale, item.State)
	meta := []string{impactRelation(m, f.Days)}
	if item.Detail != "" {
		meta = append(meta, item.Detail)
	}
	if owner := linkedWorkOwnerName(m, item.OwnerID); owner != "" {
		meta = append(meta, t(keyIssue3957Owner)+": "+owner)
	}
	if item.DueDate != "" {
		meta = append(meta, t(keyIssue3957Due)+": "+item.DueDate)
	}
	children := []ui.Node{
		html.P(html.Props{Class: "impact-finding-title", Dir: "auto", Text: item.Title}),
		html.P(html.Props{Class: "impact-finding-meta", Text: state + " · " + strings.Join(meta, " · ")}),
	}
	if !dismissed {
		children = append(children, html.P(html.Props{Class: "impact-finding-kind", Text: t(keyIssue3957Explicit)}))
	}
	actions := []ui.Node{}
	if href := linkedWorkHref(item); href != "" && !dismissed {
		actions = append(actions, html.A(html.Props{Class: "button secondary small impact-open", Href: href,
			Aria: map[string]string{"label": t(keyIssue3957Open) + ": " + item.Title}, Text: t(keyIssue3957Open)}))
	}
	action, label := "impact-dismiss", t(keyIssue3957Dismiss)
	if dismissed {
		action, label = "impact-restore", t(keyIssue3957Restore)
	}
	actions = append(actions, html.Button(html.Props{Class: "button secondary small impact-toggle", Type: "button", Title: label,
		Data: map[string]string{"action": action, "id": item.ID}, Aria: map[string]string{"label": label + ": " + item.Title}}, ui.Text(label)))
	children = append(children, html.Div(html.Props{Class: "impact-finding-actions"}, actions...))
	return html.Li(html.Props{Class: "impact-finding", Data: map[string]string{"impact-finding": item.ID}}, children...)
}

func impactResultNodes(m Model, msgID string) []ui.Node {
	s := m.Impact
	t := func(key string) string { return laneText(m, issue3957Copy, key) }
	r := s.Result
	kept := ImpactKept(r, s.Dismissed)
	vars := map[string]string{"date": r.Date, "days": strconv.Itoa(ImpactNearDays), "n": strconv.Itoa(len(r.Findings))}
	summary := impactFill(t(keyIssue3957None), vars)
	if len(r.Findings) > 0 {
		summary = impactFill(t(keyIssue3957Found), vars)
	}
	nodes := []ui.Node{html.P(html.Props{Class: "impact-summary", Text: summary})}
	if len(r.Incomplete) > 0 {
		names := make([]string, 0, len(r.Incomplete))
		for _, source := range r.Incomplete {
			names = append(names, impactSourceName(m.Locale, source))
		}
		nodes = append(nodes, html.P(html.Props{Class: "impact-partial", Text: impactFill(t(keyIssue3957Partial), map[string]string{"sources": strings.Join(names, ", ")})}))
	}
	var live, gone []ui.Node
	for _, f := range r.Findings {
		if s.Dismissed[f.Item.ID] {
			gone = append(gone, impactFindingNode(m, f, true))
		} else {
			live = append(live, impactFindingNode(m, f, false))
		}
	}
	if len(live) > 0 {
		nodes = append(nodes, html.Ul(html.Props{Class: "impact-findings", Aria: map[string]string{"label": t(keyIssue3957Title)}}, live...))
	}
	if len(gone) > 0 {
		nodes = append(nodes, html.Strong(html.Props{Class: "impact-heading", Text: t(keyIssue3957Dismissed)}),
			html.Ul(html.Props{Class: "impact-findings impact-dismissed", Aria: map[string]string{"label": t(keyIssue3957Dismissed)}}, gone...))
	}
	checked := []ui.Node{html.Li(html.Props{Text: impactFill(t(keyIssue3957CheckedTasks), map[string]string{"n": strconv.Itoa(r.Compared)})})}
	if r.NoDate > 0 {
		checked = append(checked, html.Li(html.Props{Text: impactFill(t(keyIssue3957NoDate), map[string]string{"n": strconv.Itoa(r.NoDate)})}))
	}
	if r.Finished > 0 {
		checked = append(checked, html.Li(html.Props{Text: impactFill(t(keyIssue3957Finished), map[string]string{"n": strconv.Itoa(r.Finished)})}))
	}
	if r.Actions > 0 {
		checked = append(checked, html.Li(html.Props{Text: impactFill(t(keyIssue3957Actions), map[string]string{"n": strconv.Itoa(r.Actions)})}))
	}
	if !r.AsOf.IsZero() {
		checked = append(checked, html.Li(html.Props{Text: impactFill(t(keyIssue3957AsOf), map[string]string{"time": localize.FormatClock(m.Locale, r.AsOf.Local())})}))
	}
	nodes = append(nodes,
		html.Strong(html.Props{Class: "impact-heading", Text: t(keyIssue3957Checked)}),
		html.Ul(html.Props{Class: "impact-sources"}, checked...),
		html.Strong(html.Props{Class: "impact-heading", Text: t(keyIssue3957NotChecked)}),
		html.Ul(html.Props{Class: "impact-sources impact-unavailable"},
			html.Li(html.Props{Text: t(keyIssue3957NotCalendar)}),
			html.Li(html.Props{Text: t(keyIssue3957NotOther)}),
			html.Li(html.Props{Text: t(keyIssue3957NotInferred)})),
		html.P(html.Props{Class: "impact-note", Text: t(keyIssue3957DraftHint)}),
		actionButton("button primary small", "impact-draft", msgID, t(keyIssue3957Draft), s.Draft == nil || kept == 0, ui.Text(t(keyIssue3957Draft))))
	return nodes
}

// impactBlock is the open check under its message in the thread pane, or nil.
func impactBlock(m Model, msg Message) []ui.Node {
	s := m.Impact
	if !impactAvailable(m) || s.PostID == "" || s.PostID != msg.ID {
		return nil
	}
	t := func(key string) string { return laneText(m, issue3957Copy, key) }
	busy := s.Stage == "loading"
	run := t(keyIssue3957Run)
	if s.Stage == "ready" || s.Stage == "error" {
		run = t(keyIssue3957Rerun)
	}
	dateID, popID := impactDateID(msg.ID), impactPopID(msg.ID)
	children := []ui.Node{
		html.Strong(html.Props{Class: "impact-title", Text: t(keyIssue3957Title)}),
		html.P(html.Props{Class: "impact-private", Text: t(keyIssue3957Private)}),
		html.P(html.Props{Class: "impact-scope", Text: t(keyIssue3957Scope)}),
		html.Div(html.Props{Class: "impact-source"},
			html.Strong(html.Props{Class: "impact-heading", Text: t(keyIssue3957From)}),
			html.P(html.Props{Class: "impact-quote", Dir: "auto", Text: analysisQuote(msg.Body)})),
		html.Div(html.Props{Class: "impact-field"},
			html.Label(html.Props{For: dateID, Text: t(keyIssue3957DateLabel)}),
			html.Input(html.Props{ID: dateID, Class: "chat-input", Type: "date", Required: true, Disabled: busy, Data: map[string]string{"chat-value": s.Date}, Aria: map[string]string{"describedby": dateID + "-hint"}}),
			html.P(html.Props{ID: dateID + "-hint", Class: "impact-note", Text: t(keyIssue3957DateHint)})),
		html.Div(html.Props{Class: "impact-field"},
			html.Label(html.Props{For: popID, Text: t(keyIssue3957PopLabel)}),
			html.Input(html.Props{ID: popID, Class: "chat-input", Type: "text", MaxLength: impactPopulationMax, Dir: "auto", Disabled: busy, Data: map[string]string{"chat-value": s.Population}, Aria: map[string]string{"describedby": popID + "-hint"}}),
			html.P(html.Props{ID: popID + "-hint", Class: "impact-note", Text: t(keyIssue3957PopHint)})),
		html.Div(html.Props{Class: "impact-actions"},
			actionButton("button primary small", "impact-run", msg.ID, run, busy || s.Run == nil, ui.Text(run)),
			actionButton("button secondary small", "impact-close", msg.ID, t(keyIssue3957Close), s.Close == nil, ui.Text(t(keyIssue3957Close)))),
	}
	status := []ui.Node{}
	switch {
	case s.Stage == "loading":
		status = append(status, html.P(html.Props{Class: "impact-status-text", Text: t(keyIssue3957Loading)}))
	case s.Error == "invalid_date":
		status = append(status, html.P(html.Props{Class: "impact-status-text impact-error", Text: t(keyIssue3957InvalidDate)}))
	case s.Stage == "error":
		status = append(status, html.P(html.Props{Class: "impact-status-text impact-error", Text: t(keyIssue3957Failed)}))
	}
	switch s.Notice {
	case "draft_ready":
		status = append(status, html.P(html.Props{Class: "impact-status-text", Text: t(keyIssue3957DraftReady)}))
	case "nothing_to_ask":
		status = append(status, html.P(html.Props{Class: "impact-status-text", Text: t(keyIssue3957DraftNone)}))
	case "draft_failed":
		status = append(status, html.P(html.Props{Class: "impact-status-text impact-error", Text: t(keyIssue3957DraftFailed)}))
	}
	children = append(children, html.Div(html.Props{Class: "impact-status", Role: "status", Aria: map[string]string{"live": "polite", "busy": boolString(busy)}}, status...))
	if s.Stage == "ready" {
		children = append(children, impactResultNodes(m, msg.ID)...)
	}
	return []ui.Node{html.Div(html.Props{ID: "impact-" + msg.ID, Class: "impact-check", Role: "region", TabIndex: -1, Aria: map[string]string{"label": t(keyIssue3957Title)}}, children...)}
}

// impactMessageBody finds the text of a message of the open thread.
func impactMessageBody(m Model, id string) string {
	for _, msg := range m.ThreadMessages {
		if msg.ID == id {
			return msg.Body
		}
	}
	for _, msg := range m.Messages {
		if msg.ID == id {
			return msg.Body
		}
	}
	return ""
}

// impactDeliver puts a draft in the thread's reply box: after what is already
// there, never over it, and never sent.
func impactDeliver(m Model) func(text string) {
	parent := m.ThreadParentID
	return func(text string) {
		if strings.TrimSpace(text) == "" || parent == "" {
			return
		}
		existing := domValue("thread-composer")
		if existing == "" {
			existing = m.ThreadDrafts[parent]
		}
		if strings.TrimSpace(existing) != "" {
			text = strings.TrimRight(existing, "\n ") + "\n\n" + text
		}
		replaceComposerText("thread-composer", text, len(utf16.Encode([]rune(text))))
		setDOMValue("thread-composer", text)
		if m.Callbacks.ThreadDraftChanged != nil {
			m.Callbacks.ThreadDraftChanged(parent, text)
		}
		focusField("thread-composer")
	}
}

// impactAction runs a control of the check.
func (m Model) impactAction(action, id string) {
	s := m.Impact
	switch action {
	case "impact-open":
		if !impactAvailable(m) || s.Open == nil || id == "" {
			return
		}
		if s.PostID == id && s.Close != nil {
			s.Close()
			return
		}
		s.Open(id, ImpactDateIn(impactMessageBody(m, id)))
	case "impact-close":
		if s.Close != nil {
			s.Close()
		}
	case "impact-run", "impact-draft":
		if !impactAvailable(m) || s.PostID == "" || id != s.PostID {
			return
		}
		date, population := domValue(impactDateID(id)), domValue(impactPopID(id))
		if date == "" {
			date = s.Date
		}
		if population == "" {
			population = s.Population
		}
		if action == "impact-run" && s.Run != nil {
			s.Run(id, date, population)
		}
		if action == "impact-draft" && s.Draft != nil {
			s.Draft(id, date, population, impactDeliver(m))
		}
	case "impact-dismiss":
		if s.Dismiss != nil && s.PostID != "" && id != "" {
			s.Dismiss(id)
		}
	case "impact-restore":
		if s.Restore != nil && s.PostID != "" && id != "" {
			s.Restore(id)
		}
	}
}

// Issue3957Styles is the check's layout. Logical properties only, so it holds
// in right-to-left; targets are 44px on a phone.
const Issue3957Styles = `
.impact-check{margin-block-start:8px;padding:10px;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface);min-inline-size:0;display:grid;gap:8px}
.impact-title{font-size:.875rem}
.impact-private,.impact-scope,.impact-note,.impact-quote,.impact-summary,.impact-partial,.impact-status-text,.impact-finding-title,.impact-finding-meta,.impact-finding-kind{margin:0;font-size:.8125rem;overflow-wrap:anywhere}
.impact-scope,.impact-note,.impact-finding-meta,.impact-finding-kind{color:var(--hcm-color-text-muted)}
.impact-quote{padding-inline-start:8px;border-inline-start:3px solid var(--hcm-color-border);white-space:pre-wrap}
.impact-heading{font-size:.8125rem;display:block}
.impact-field{display:grid;gap:4px;min-inline-size:0}
.impact-field label{font-size:.8125rem;font-weight:600}
.impact-actions,.impact-finding-actions{display:flex;flex-wrap:wrap;gap:8px}
.impact-findings,.impact-sources{margin:0;padding:0;list-style:none;display:grid;gap:6px}
.impact-sources{padding-inline-start:20px;list-style:disc;font-size:.8125rem}
.impact-finding{padding:8px;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);display:grid;gap:4px;min-inline-size:0}
.impact-dismissed .impact-finding{opacity:.75}
.impact-error{color:var(--hcm-color-danger,inherit);font-weight:600}
@media(max-width:767px){.impact-check button,.impact-check .button,.impact-check input{min-block-size:44px;min-inline-size:44px}}
`
