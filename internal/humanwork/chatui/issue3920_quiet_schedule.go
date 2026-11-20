package chatui

import (
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ISSUE-3920: quiet hours that match a person's weekly work pattern.
//
// Quiet hours keep their daily interval and time zone. Beside them a person may
// switch to "Different on each day": one entry per weekday, each with no quiet
// time, a whole quiet day, or hours (which may run overnight into the next
// day). The row also says, in words, when notifications next pause or resume in
// the chosen zone, and offers a one-off pause or a "notify me anyway" override
// that always has an explicit end and a way back to the schedule. The server
// decides what is quiet; this file only draws and edits the schedule.

// Quiet day modes, as the server names them.
const (
	QuietDayNone     = "none"
	QuietDayAll      = "all"
	QuietDayInterval = "interval"
)

// Pause kinds, as the server names them.
const (
	QuietPausePause  = "pause"
	QuietPauseNotify = "notify"
)

// QuietDayView is one weekday's quiet time. Minutes count from midnight; an
// interval whose end is before its start runs overnight into the next day.
type QuietDayView struct {
	Mode                   string
	StartMinute, EndMinute int
}

// QuietScheduleView is what the page holds of the person's weekly schedule.
// Days are indexed like time.Weekday, Sunday first.
type QuietScheduleView struct {
	Loaded, Failed bool
	Revision       uint64
	WeekActive     bool
	Days           [7]QuietDayView
	PauseKind      string
	PauseUntil     time.Time
	QuietNow       bool
	HasNext        bool
	NextAt         time.Time
	NextQuiet      bool
	// Note is the last save's outcome: "", "saved", "failed", "conflict",
	// "overlap" or "empty_interval". NoteDay is the weekday it is about.
	Note    string
	NoteDay int
}

// QuietScheduleEdit is a change the person made. The page keeps an existing
// pause unless PauseKind or PauseMinutes say otherwise: PauseMinutes above zero
// starts a new pause or override of that length from now, and an empty
// PauseKind with no minutes ends any pause.
type QuietScheduleEdit struct {
	WeekActive   bool
	Days         [7]QuietDayView
	PauseKind    string
	PauseUntil   time.Time
	PauseMinutes int
}

const (
	issue3920Daily  = "daily"
	issue3920Weekly = "weekly"
)

// issue3920Order is the order the days are listed in: Monday first.
var issue3920Order = [7]int{1, 2, 3, 4, 5, 6, 0}

var issue3920DayNames = map[string][7]string{
	"en-US": {"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"},
	"de-DE": {"Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"},
	"ar":    {"الأحد", "الاثنين", "الثلاثاء", "الأربعاء", "الخميس", "الجمعة", "السبت"},
}

const (
	k3920Title         = "chat.issue3920.title"
	k3920Mode          = "chat.issue3920.mode"
	k3920Daily         = "chat.issue3920.daily"
	k3920Weekly        = "chat.issue3920.weekly"
	k3920DayNone       = "chat.issue3920.day_none"
	k3920DayAll        = "chat.issue3920.day_all"
	k3920DayInterval   = "chat.issue3920.day_interval"
	k3920Save          = "chat.issue3920.save"
	k3920Saved         = "chat.issue3920.saved"
	k3920Failed        = "chat.issue3920.failed"
	k3920Conflict      = "chat.issue3920.conflict"
	k3920Overlap       = "chat.issue3920.overlap"
	k3920Empty         = "chat.issue3920.empty"
	k3920ExplainNone   = "chat.issue3920.explain_none"
	k3920ExplainAll    = "chat.issue3920.explain_all"
	k3920ExplainSame   = "chat.issue3920.explain_same"
	k3920ExplainNight  = "chat.issue3920.explain_night"
	k3920NextPause     = "chat.issue3920.next_pause"
	k3920NextResume    = "chat.issue3920.next_resume"
	k3920NoQuiet       = "chat.issue3920.no_quiet"
	k3920AlwaysQuiet   = "chat.issue3920.always_quiet"
	k3920PauseTitle    = "chat.issue3920.pause_title"
	k3920Pause1        = "chat.issue3920.pause_1h"
	k3920Pause4        = "chat.issue3920.pause_4h"
	k3920Pause8        = "chat.issue3920.pause_8h"
	k3920Notify1       = "chat.issue3920.notify_1h"
	k3920Back          = "chat.issue3920.back"
	k3920PausedUntil   = "chat.issue3920.paused_until"
	k3920LiftedUntil   = "chat.issue3920.lifted_until"
	k3920Exceptions    = "chat.issue3920.exceptions"
	k3920ZoneNote      = "chat.issue3920.zone_note"
	k3920Loading       = "chat.issue3920.loading"
	k3920Unavailable   = "chat.issue3920.unavailable"
	k3920NeedsOn       = "chat.issue3920.needs_on"
	k3920Hours         = "chat.issue3920.hours_hint"
	k3920DailyReplaced = "chat.issue3920.daily_replaced"
)

// issue3920WeeklyOn is whether the weekly schedule, not the daily interval,
// decides the person's quiet hours.
func issue3920WeeklyOn(m Model) bool { return m.QuietSchedule.Loaded && m.QuietSchedule.WeekActive }

var issue3920Copy = map[string]map[string]string{
	"en-US": {
		k3920Title:         "Schedule",
		k3920Mode:          "Schedule type",
		k3920Daily:         "Same every day",
		k3920Weekly:        "Different on each day",
		k3920DayNone:       "No quiet time",
		k3920DayAll:        "Quiet all day",
		k3920DayInterval:   "Quiet during these hours",
		k3920Save:          "Save schedule",
		k3920Saved:         "Schedule saved",
		k3920Failed:        "The schedule could not be saved. It is unchanged. Try again.",
		k3920Conflict:      "Your schedule was changed somewhere else, so it was reloaded. Check it and save again.",
		k3920Overlap:       "{day}: quiet time starts before the overnight hours from the day before have ended. Change one of the two.",
		k3920Empty:         "{day}: the start and end are the same. Choose “Quiet all day” for a whole quiet day.",
		k3920ExplainNone:   "{day}: no quiet time",
		k3920ExplainAll:    "{day}: quiet all day",
		k3920ExplainSame:   "{day}: quiet from {from} to {until}",
		k3920ExplainNight:  "{day}: quiet from {from} until {until} on {nextday}",
		k3920NextPause:     "Notifications pause {when}.",
		k3920NextResume:    "Quiet now. Notifications resume {when}.",
		k3920NoQuiet:       "No quiet time is scheduled.",
		k3920AlwaysQuiet:   "Quiet all the time, with no end scheduled.",
		k3920PauseTitle:    "Pause or override",
		k3920Pause1:        "Pause for 1 hour",
		k3920Pause4:        "Pause for 4 hours",
		k3920Pause8:        "Pause for 8 hours",
		k3920Notify1:       "Notify me anyway for 1 hour",
		k3920Back:          "Back to my schedule",
		k3920PausedUntil:   "Paused until {when}. Your schedule applies again after that.",
		k3920LiftedUntil:   "Quiet hours are lifted until {when}. Your schedule applies again after that.",
		k3920Exceptions:    "Messages stay in your conversations. Required HR and company notices are never held back.",
		k3920ZoneNote:      "Times follow the clock in {zone}, including daylight-saving changes.",
		k3920Loading:       "Loading your schedule…",
		k3920Unavailable:   "Your weekly schedule could not be loaded.",
		k3920NeedsOn:       "Turn on Quiet hours to use a schedule.",
		k3920Hours:         "From and until",
		k3920DailyReplaced: "Your schedule for each day sets these hours. Change them under Schedule.",
	},
	"de-DE": {
		k3920Title:         "Zeitplan",
		k3920Mode:          "Art des Zeitplans",
		k3920Daily:         "Jeden Tag gleich",
		k3920Weekly:        "An jedem Tag anders",
		k3920DayNone:       "Keine Ruhezeit",
		k3920DayAll:        "Ganztägig ruhig",
		k3920DayInterval:   "Ruhig in diesen Stunden",
		k3920Save:          "Zeitplan speichern",
		k3920Saved:         "Zeitplan gespeichert",
		k3920Failed:        "Der Zeitplan konnte nicht gespeichert werden. Er ist unverändert. Versuchen Sie es erneut.",
		k3920Conflict:      "Ihr Zeitplan wurde an anderer Stelle geändert und deshalb neu geladen. Prüfen Sie ihn und speichern Sie erneut.",
		k3920Overlap:       "{day}: Die Ruhezeit beginnt, bevor die Nachtstunden vom Vortag enden. Ändern Sie eine der beiden.",
		k3920Empty:         "{day}: Beginn und Ende sind gleich. Wählen Sie „Ganztägig ruhig“ für einen ganzen ruhigen Tag.",
		k3920ExplainNone:   "{day}: keine Ruhezeit",
		k3920ExplainAll:    "{day}: ganztägig ruhig",
		k3920ExplainSame:   "{day}: ruhig von {from} bis {until}",
		k3920ExplainNight:  "{day}: ruhig von {from} bis {until} am {nextday}",
		k3920NextPause:     "Benachrichtigungen pausieren {when}.",
		k3920NextResume:    "Gerade ruhig. Benachrichtigungen kommen wieder {when}.",
		k3920NoQuiet:       "Es ist keine Ruhezeit geplant.",
		k3920AlwaysQuiet:   "Dauerhaft ruhig, ohne geplantes Ende.",
		k3920PauseTitle:    "Pausieren oder aufheben",
		k3920Pause1:        "1 Stunde pausieren",
		k3920Pause4:        "4 Stunden pausieren",
		k3920Pause8:        "8 Stunden pausieren",
		k3920Notify1:       "1 Stunde trotzdem benachrichtigen",
		k3920Back:          "Zurück zu meinem Zeitplan",
		k3920PausedUntil:   "Pausiert bis {when}. Danach gilt wieder Ihr Zeitplan.",
		k3920LiftedUntil:   "Ruhezeiten sind aufgehoben bis {when}. Danach gilt wieder Ihr Zeitplan.",
		k3920Exceptions:    "Nachrichten bleiben in Ihren Unterhaltungen. Verpflichtende HR- und Unternehmensmitteilungen werden nie zurückgehalten.",
		k3920ZoneNote:      "Die Zeiten folgen der Uhr in {zone}, einschließlich Sommerzeitwechsel.",
		k3920Loading:       "Ihr Zeitplan wird geladen…",
		k3920Unavailable:   "Ihr Wochenzeitplan konnte nicht geladen werden.",
		k3920NeedsOn:       "Schalten Sie Ruhezeiten ein, um einen Zeitplan zu nutzen.",
		k3920Hours:         "Von und bis",
		k3920DailyReplaced: "Ihr Zeitplan für die einzelnen Tage legt diese Zeiten fest. Ändern Sie sie unter „Zeitplan“.",
	},
	"ar": {
		k3920Title:         "الجدول",
		k3920Mode:          "نوع الجدول",
		k3920Daily:         "نفسه كل يوم",
		k3920Weekly:        "مختلف في كل يوم",
		k3920DayNone:       "بلا وقت هدوء",
		k3920DayAll:        "هدوء طوال اليوم",
		k3920DayInterval:   "هدوء خلال هذه الساعات",
		k3920Save:          "حفظ الجدول",
		k3920Saved:         "تم حفظ الجدول",
		k3920Failed:        "تعذر حفظ الجدول. لم يتغير. حاول مجددًا.",
		k3920Conflict:      "تم تغيير جدولك في مكان آخر فأُعيد تحميله. راجعه واحفظ مجددًا.",
		k3920Overlap:       "{day}: يبدأ وقت الهدوء قبل انتهاء ساعات الليل الآتية من اليوم السابق. غيّر أحدهما.",
		k3920Empty:         "{day}: البداية والنهاية متطابقتان. اختر «هدوء طوال اليوم» ليوم هادئ كامل.",
		k3920ExplainNone:   "{day}: بلا وقت هدوء",
		k3920ExplainAll:    "{day}: هدوء طوال اليوم",
		k3920ExplainSame:   "{day}: هدوء من {from} إلى {until}",
		k3920ExplainNight:  "{day}: هدوء من {from} حتى {until} يوم {nextday}",
		k3920NextPause:     "تتوقف الإشعارات {when}.",
		k3920NextResume:    "وقت هدوء الآن. تعود الإشعارات {when}.",
		k3920NoQuiet:       "لا يوجد وقت هدوء مجدول.",
		k3920AlwaysQuiet:   "هدوء دائم دون نهاية مجدولة.",
		k3920PauseTitle:    "إيقاف مؤقت أو تجاوز",
		k3920Pause1:        "إيقاف لمدة ساعة",
		k3920Pause4:        "إيقاف لمدة 4 ساعات",
		k3920Pause8:        "إيقاف لمدة 8 ساعات",
		k3920Notify1:       "أبلغني رغم ذلك لمدة ساعة",
		k3920Back:          "العودة إلى جدولي",
		k3920PausedUntil:   "متوقف مؤقتًا حتى {when}. يسري جدولك مجددًا بعد ذلك.",
		k3920LiftedUntil:   "رُفعت ساعات الهدوء حتى {when}. يسري جدولك مجددًا بعد ذلك.",
		k3920Exceptions:    "تبقى الرسائل في محادثاتك. لا تُحجب أبدًا إشعارات الموارد البشرية والشركة الإلزامية.",
		k3920ZoneNote:      "تتبع الأوقات الساعة في {zone}، بما في ذلك تغييرات التوقيت الصيفي.",
		k3920Loading:       "جارٍ تحميل جدولك…",
		k3920Unavailable:   "تعذر تحميل جدولك الأسبوعي.",
		k3920NeedsOn:       "فعّل ساعات الهدوء لاستخدام جدول.",
		k3920Hours:         "من وحتى",
		k3920DailyReplaced: "جدول كل يوم يحدد هذه الساعات. غيّرها ضمن «الجدول».",
	},
}

func issue3920Text(m Model, key string) string { return laneText(m, issue3920Copy, key) }

func issue3920Textf(m Model, key string, vars map[string]string) string {
	return laneTextf(m, issue3920Copy, key, vars)
}

func issue3920DayName(m Model, day int) string {
	names, ok := issue3920DayNames[chatEmojiLocale(m.Locale)]
	if !ok {
		names = issue3920DayNames["en-US"]
	}
	return names[((day%7)+7)%7]
}

// issue3920Location is the zone the schedule runs in, UTC when it is not known.
func issue3920Location(m Model) *time.Location {
	zone := quietZone(m.Preferences.QuietTimezone, localTimeZone())
	if loc, err := time.LoadLocation(zone); err == nil {
		return loc
	}
	return time.UTC
}

// issue3920When writes a moment as a weekday and a clock time in the schedule's zone.
func issue3920When(m Model, at time.Time) string {
	local := at.In(issue3920Location(m))
	return issue3920DayName(m, int(local.Weekday())) + " " + quietClock(m, local.Hour()*60+local.Minute())
}

// issue3920Explain is one day's schedule read back in words, so a person can
// check what they set without reading two clocks: nothing, the whole day, hours
// inside the day, or hours that run overnight and end the next day.
func issue3920Explain(m Model, day int, d QuietDayView) string {
	vars := map[string]string{"day": issue3920DayName(m, day), "nextday": issue3920DayName(m, day+1),
		"from": quietClock(m, d.StartMinute), "until": quietClock(m, d.EndMinute)}
	switch d.Mode {
	case QuietDayAll:
		return issue3920Textf(m, k3920ExplainAll, vars)
	case QuietDayInterval:
		if d.EndMinute < d.StartMinute {
			return issue3920Textf(m, k3920ExplainNight, vars)
		}
		return issue3920Textf(m, k3920ExplainSame, vars)
	}
	return issue3920Textf(m, k3920ExplainNone, vars)
}

// issue3920Status is the line under the row's heading: the pause or override
// in force, else when notifications next pause or resume, with the zone.
func issue3920Status(m Model) string {
	v := m.QuietSchedule
	if issue3920PauseLive(v) {
		key := k3920PausedUntil
		if v.PauseKind == QuietPauseNotify {
			key = k3920LiftedUntil
		}
		return issue3920Textf(m, key, map[string]string{"when": issue3920When(m, v.PauseUntil)})
	}
	if !m.Preferences.QuietHours {
		return ""
	}
	if v.HasNext {
		key := k3920NextPause
		if !v.NextQuiet {
			key = k3920NextResume
		}
		return issue3920Textf(m, key, map[string]string{"when": issue3920When(m, v.NextAt)})
	}
	if v.QuietNow {
		return issue3920Text(m, k3920AlwaysQuiet)
	}
	return issue3920Text(m, k3920NoQuiet)
}

// issue3920PauseLive is whether the pause the server reported is still running.
func issue3920PauseLive(v QuietScheduleView) bool {
	return v.PauseKind != "" && v.PauseUntil.After(time.Now())
}

// issue3920Default is the week to start from when the person switches to
// different hours: the daily interval on every day (a whole quiet day when the
// daily start equals its end), so switching changes nothing until they edit.
func issue3920Default(p Preferences) [7]QuietDayView {
	day := QuietDayView{Mode: QuietDayInterval, StartMinute: p.QuietStartMinute, EndMinute: p.QuietEndMinute}
	if p.QuietStartMinute == p.QuietEndMinute {
		day = QuietDayView{Mode: QuietDayAll}
	}
	var out [7]QuietDayView
	for i := range out {
		out[i] = day
	}
	return out
}

// issue3920Untouched is a week the person has never set: every day is empty.
func issue3920Untouched(v QuietScheduleView) bool {
	if v.WeekActive {
		return false
	}
	for _, d := range v.Days {
		if d.Mode != "" && d.Mode != QuietDayNone {
			return false
		}
	}
	return true
}

func issue3920Edit(m Model) QuietScheduleEdit {
	v := m.QuietSchedule
	e := QuietScheduleEdit{WeekActive: v.WeekActive, Days: v.Days, PauseKind: v.PauseKind, PauseUntil: v.PauseUntil}
	for i := range e.Days {
		if e.Days[i].Mode == "" {
			e.Days[i].Mode = QuietDayNone
		}
	}
	return e
}

// issue3920Apply runs one click of the schedule row and says whether it was
// one of its own. The extra is "daily" or "weekly" for the schedule type, and
// "pause:<minutes>", "notify:<minutes>" or "clear" for the pause controls.
func issue3920Apply(m Model, action, extra string) bool {
	switch action {
	case "quiet-week-mode":
		if m.Callbacks.SaveQuietSchedule == nil || (extra != issue3920Daily && extra != issue3920Weekly) {
			return true
		}
		e := issue3920Edit(m)
		e.WeekActive = extra == issue3920Weekly
		if e.WeekActive && issue3920Untouched(m.QuietSchedule) {
			e.Days = issue3920Default(m.Preferences)
		}
		if e.WeekActive != m.QuietSchedule.WeekActive {
			m.Callbacks.SaveQuietSchedule(e)
		}
		return true
	case "quiet-pause":
		if m.Callbacks.SaveQuietSchedule == nil {
			return true
		}
		e := issue3920Edit(m)
		if extra == "clear" {
			e.PauseKind, e.PauseUntil, e.PauseMinutes = "", time.Time{}, 0
			m.Callbacks.SaveQuietSchedule(e)
			return true
		}
		kind, minutes, ok := strings.Cut(extra, ":")
		n, err := strconv.Atoi(minutes)
		if !ok || err != nil || n <= 0 || n > 24*60 || (kind != QuietPausePause && kind != QuietPauseNotify) {
			return true
		}
		e.PauseKind, e.PauseMinutes = kind, n
		m.Callbacks.SaveQuietSchedule(e)
		return true
	}
	return false
}

// issue3920Click handles a click on the schedule row's buttons.
func issue3920Click(e ui.MouseEvent, m Model) bool {
	action, _, extra := eventAction(e)
	if !issue3920Apply(m, action, extra) {
		return false
	}
	e.PreventDefault()
	return true
}

// issue3920Submit saves the weekly editor, reading each day's fields as typed.
// A field left empty keeps the saved value.
func issue3920Submit(m Model) {
	if m.Callbacks.SaveQuietSchedule == nil {
		return
	}
	e := issue3920Edit(m)
	for day := 0; day < 7; day++ {
		id := "quiet-day-" + strconv.Itoa(day)
		switch mode := domValue(id + "-mode"); mode {
		case QuietDayNone, QuietDayAll, QuietDayInterval:
			e.Days[day].Mode = mode
		}
		if minute, ok := quietClockInput(domValue(id + "-start")); ok {
			e.Days[day].StartMinute = minute
		}
		if minute, ok := quietClockInput(domValue(id + "-end")); ok {
			e.Days[day].EndMinute = minute
		}
	}
	e.WeekActive = true
	m.Callbacks.SaveQuietSchedule(e)
}

func issue3920Choice(m Model, action, extra, label string, pressed, disabled bool) ui.Node {
	return html.Button(html.Props{Class: "chat-prefs-choice", Type: "button", Dir: "auto", Text: label, Disabled: disabled,
		Data: map[string]string{"action": action, "extra": extra, "prefs-quiet-schedule": extra},
		Aria: map[string]string{"pressed": boolString(pressed)}})
}

func issue3920NoteText(m Model) string {
	v := m.QuietSchedule
	vars := map[string]string{"day": issue3920DayName(m, v.NoteDay)}
	switch v.Note {
	case "saved":
		return issue3920Text(m, k3920Saved)
	case "failed":
		return issue3920Text(m, k3920Failed)
	case "conflict":
		return issue3920Text(m, k3920Conflict)
	case "overlap":
		return issue3920Textf(m, k3920Overlap, vars)
	case "empty_interval":
		return issue3920Textf(m, k3920Empty, vars)
	}
	return ""
}

func issue3920DayRow(m Model, day int, d QuietDayView, disabled bool) ui.Node {
	id := "quiet-day-" + strconv.Itoa(day)
	name := issue3920DayName(m, day)
	if d.Mode == "" {
		d.Mode = QuietDayNone
	}
	option := func(mode, key string) ui.Node {
		return html.Option(html.Props{Value: mode, Selected: d.Mode == mode, Text: issue3920Text(m, key)})
	}
	return html.Li(html.Props{Class: "quiet-day", Role: "group", Data: map[string]string{"quiet-day": strconv.Itoa(day), "mode": d.Mode}, Aria: map[string]string{"labelledby": id + "-name"}},
		html.Label(html.Props{ID: id + "-name", For: id + "-mode", Class: "quiet-day-name", Dir: "auto", Text: name}),
		html.Select(html.Props{ID: id + "-mode", Class: "chat-input quiet-day-mode", Disabled: disabled,
			Aria: map[string]string{"label": name + ": " + issue3920Text(m, k3920Mode)}},
			option(QuietDayNone, k3920DayNone), option(QuietDayAll, k3920DayAll), option(QuietDayInterval, k3920DayInterval)),
		html.Div(html.Props{Class: "quiet-day-times", Role: "group", Aria: map[string]string{"label": name + ": " + issue3920Text(m, k3920Hours)}},
			html.Input(html.Props{ID: id + "-start", Class: "chat-input", Type: "time", Disabled: disabled, Data: map[string]string{"chat-value": minuteClock(d.StartMinute)},
				Aria: map[string]string{"label": name + ": " + m.t(KeyQuietStart)}}),
			html.Input(html.Props{ID: id + "-end", Class: "chat-input", Type: "time", Disabled: disabled, Data: map[string]string{"chat-value": minuteClock(d.EndMinute)},
				Aria: map[string]string{"label": name + ": " + m.t(KeyQuietEnd)}})),
		html.P(html.Props{Class: "quiet-day-explain", Dir: "auto", Text: issue3920Explain(m, day, d)}))
}

// issue3920Row is the schedule row of Chat preferences. It is drawn where the
// page can keep a schedule; until the server has answered it says so, and when
// the server cannot answer it says that instead of offering controls that would
// do nothing.
func issue3920Row(m Model, h handlers) ui.Node {
	if m.Callbacks.SaveQuietSchedule == nil {
		return nil
	}
	v := m.QuietSchedule
	title := issue3920Text(m, k3920Title)
	switch {
	case v.Failed && !v.Loaded:
		return chatux002Row("quiet-schedule", title, "", nil,
			html.P(html.Props{Class: "field-hint chat-prefs-note", Role: "status", Dir: "auto", Text: issue3920Text(m, k3920Unavailable)}))
	case !v.Loaded:
		return chatux002Row("quiet-schedule", title, "", nil,
			html.P(html.Props{Class: "field-hint chat-prefs-note", Role: "status", Dir: "auto", Text: issue3920Text(m, k3920Loading)}))
	}
	masterOff := !m.Preferences.QuietHours
	weekly := v.WeekActive
	value := issue3920Text(m, k3920Daily)
	if weekly {
		value = issue3920Text(m, k3920Weekly)
	}
	children := []ui.Node{}
	if status := issue3920Status(m); status != "" {
		children = append(children, html.P(html.Props{Class: "field-hint chat-prefs-note quiet-status", Role: "status", Dir: "auto", Data: map[string]string{"quiet-status": "true"}, Text: status}))
	}
	children = append(children,
		html.Div(html.Props{Class: "chat-prefs-choices", Role: "group", Aria: map[string]string{"label": issue3920Text(m, k3920Mode)}},
			issue3920Choice(m, "quiet-week-mode", issue3920Daily, issue3920Text(m, k3920Daily), !weekly, masterOff),
			issue3920Choice(m, "quiet-week-mode", issue3920Weekly, issue3920Text(m, k3920Weekly), weekly, masterOff)))
	if masterOff {
		children = append(children, html.P(html.Props{Class: "field-hint chat-prefs-note", Dir: "auto", Text: issue3920Text(m, k3920NeedsOn)}))
	}
	if weekly {
		days := make([]ui.Node, 0, 7)
		for _, day := range issue3920Order {
			days = append(days, html.WithKey(issue3920DayRow(m, day, v.Days[day], masterOff), "quiet-day:"+strconv.Itoa(day)))
		}
		children = append(children, html.Form(html.Props{Class: "quiet-week", OnSubmit: h.quietSched, Raw: map[string]any{"noValidate": true}, Aria: map[string]string{"label": title}},
			html.Ul(html.Props{Class: "quiet-days"}, days...),
			html.Button(html.Props{Class: "button secondary small", Type: "submit", Disabled: masterOff, Data: map[string]string{"quiet-schedule-save": "true"}, Text: issue3920Text(m, k3920Save)})))
	}
	if note := issue3920NoteText(m); note != "" {
		role := "status"
		if v.Note != "saved" {
			role = "alert"
		}
		children = append(children, html.P(html.Props{Class: "field-hint chat-prefs-note quiet-note", Role: role, Dir: "auto", Data: map[string]string{"quiet-note": v.Note}, Text: note}))
	}
	// The one-off pause and the override sit apart from the recurring schedule.
	pauseLive := issue3920PauseLive(v)
	pause := []ui.Node{html.H4(html.Props{Class: "quiet-pause-title", Dir: "auto", Text: issue3920Text(m, k3920PauseTitle)})}
	if pauseLive {
		pause = append(pause, issue3920Choice(m, "quiet-pause", "clear", issue3920Text(m, k3920Back), false, false))
	} else {
		pause = append(pause,
			issue3920Choice(m, "quiet-pause", "pause:60", issue3920Text(m, k3920Pause1), false, false),
			issue3920Choice(m, "quiet-pause", "pause:240", issue3920Text(m, k3920Pause4), false, false),
			issue3920Choice(m, "quiet-pause", "pause:480", issue3920Text(m, k3920Pause8), false, false))
		if v.QuietNow {
			pause = append(pause, issue3920Choice(m, "quiet-pause", "notify:60", issue3920Text(m, k3920Notify1), false, false))
		}
	}
	children = append(children, html.Div(html.Props{Class: "quiet-pause", Role: "group", Aria: map[string]string{"label": issue3920Text(m, k3920PauseTitle)}}, pause...))
	zone := s24ZoneLabel(m.Locale, quietZone(m.Preferences.QuietTimezone, localTimeZone()), time.Now())
	children = append(children,
		html.P(html.Props{Class: "field-hint chat-prefs-note", Dir: "auto", Text: issue3920Textf(m, k3920ZoneNote, map[string]string{"zone": zone})}),
		html.P(html.Props{Class: "field-hint chat-prefs-note", Dir: "auto", Text: issue3920Text(m, k3920Exceptions)}))
	return chatux002Row("quiet-schedule", title, value, nil, children...)
}

// Issue3920Styles is the schedule editor: a column that fits the 360px panel
// and a 320px phone, 44px targets, logical properties only so it holds in
// right-to-left, and the times shown only for a day set to hours.
const Issue3920Styles = `.quiet-days{list-style:none;margin:0;padding:0;display:grid;gap:10px}` +
	`.quiet-day{display:grid;gap:6px;padding-block:8px;border-block-end:1px solid var(--line)}` +
	`.quiet-day-name{font-weight:600}` +
	`.quiet-day-mode,.quiet-day-times input{box-sizing:border-box;min-block-size:44px;inline-size:100%;min-inline-size:0}` +
	`.quiet-day-times{display:none;grid-template-columns:repeat(2,minmax(0,1fr));gap:8px}` +
	`.quiet-day[data-mode="interval"] .quiet-day-times{display:grid}` +
	`.quiet-day:has(.quiet-day-mode option[value="interval"]:checked) .quiet-day-times{display:grid}` +
	`.quiet-day-explain{margin:0;color:var(--muted);font-size:.875rem}` +
	`.quiet-week{display:grid;gap:10px}` +
	`.quiet-week .button{min-block-size:44px}` +
	`.quiet-pause{display:flex;flex-wrap:wrap;gap:6px;align-items:center;padding-block:4px}` +
	`.quiet-pause-title{flex:1 0 100%;margin:0;font-size:.9375rem}` +
	`.quiet-note[data-quiet-note]:not([data-quiet-note="saved"]){color:var(--danger,var(--ink))}`
