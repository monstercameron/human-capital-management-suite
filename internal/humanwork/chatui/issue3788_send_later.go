package chatui

import (
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Issue 3788: Send later.
//
// The composer's + menu has a Send later item. It opens a small panel above
// the message box with a few times to choose (in an hour, this evening,
// tomorrow morning, next Monday) and a date and time picker. Choosing one keeps
// the message as a draft only its author can see and empties the box. The
// Scheduled row above the conversation list opens a tray of those messages,
// where each can be changed or cancelled; one that could not be sent stays
// there with a plain reason. Nothing is sent from the browser: the server
// sends the message as its author at the chosen time, and decides again then
// whether the author may still post, whether the message still passes the
// workspace's filters, and whether the channel is still open.

// ScheduledRow is one scheduled message as the tray draws it.
type ScheduledRow struct {
	ID, ConversationID string
	// Channel is the conversation's name as the rail shows it; InChannel says to
	// write it as "#name".
	Channel   string
	InChannel bool
	Body      string
	SendAt    time.Time
	// State is "pending", "sending" or "failed"; Refusal is the short reason a
	// failed one was not sent ("no_access", "channel_closed", "content_blocked"
	// or "unavailable").
	State, Refusal string
	Revision       uint64
	// QuietPeople is how many people the message is written to whose quiet hours
	// include its time.
	QuietPeople int
}

// SendLaterState is everything the Send later controls and the tray read.
type SendLaterState struct {
	Rows                  []ScheduledRow
	Loading, Loaded, Fail bool
	// Note is set when a change to a row could not be saved.
	Note string
}

// SendLaterResult is what the page reports after it asked the server to keep or
// change a message.
type SendLaterResult struct {
	// Outcome is "saved", "limit", "time" or "failed".
	Outcome     string
	SendAt      time.Time
	QuietPeople int
}

const (
	issue3788Toggle      = "scheduled-toggle"
	issue3788Retry       = "scheduled-retry"
	issue3788Edit        = "scheduled-edit"
	issue3788Cancel      = "scheduled-cancel"
	issue3788Save        = "scheduled-save"
	issue3788CloseEdit   = "scheduled-edit-close"
	issue3788Pick        = "sendlater-pick"
	issue3788Custom      = "sendlater-custom"
	issue3788Close       = "sendlater-close"
	issue3788ToggleID    = "issue3788-toggle"
	issue3788HeadID      = "issue3788-heading"
	issue3788PanelID     = "issue3788-panel"
	issue3788LaterHeadID = "issue3788-later-heading"
	issue3788DateID      = "issue3788-date"
	issue3788TimeID      = "issue3788-time"
	issue3788EditBodyID  = "issue3788-edit-body"
	issue3788EditDateID  = "issue3788-edit-date"
	issue3788EditTimeID  = "issue3788-edit-time"
	issue3788FormNone    = "issue3788-no-form"
	// issue3788ClockLayout is the machine form of a time field's value (HH:MM), not display text.
	issue3788ClockLayout = "15:04"
	// issue3788MinLead is the shortest wait the server accepts, with a margin.
	issue3788MinLead = 2 * time.Minute
	issue3788MaxLead = 360 * 24 * time.Hour

	keyIssue3788Label      = "chat.issue3788.label"
	keyIssue3788Hint       = "chat.issue3788.hint"
	keyIssue3788Count      = "chat.issue3788.count"
	keyIssue3788Heading    = "chat.issue3788.heading"
	keyIssue3788Intro      = "chat.issue3788.intro"
	keyIssue3788Loading    = "chat.issue3788.loading"
	keyIssue3788Empty      = "chat.issue3788.empty"
	keyIssue3788Error      = "chat.issue3788.error"
	keyIssue3788Retry      = "chat.issue3788.retry"
	keyIssue3788Shown      = "chat.issue3788.shown"
	keyIssue3788Failed     = "chat.issue3788.failed"
	keyIssue3788Where      = "chat.issue3788.where"
	keyIssue3788Somewhere  = "chat.issue3788.somewhere"
	keyIssue3788Sends      = "chat.issue3788.sends"
	keyIssue3788Quiet      = "chat.issue3788.quiet"
	keyIssue3788NotSent    = "chat.issue3788.not_sent"
	keyIssue3788Sending    = "chat.issue3788.sending"
	keyIssue3788Access     = "chat.issue3788.refused_access"
	keyIssue3788Channel    = "chat.issue3788.refused_channel"
	keyIssue3788Content    = "chat.issue3788.refused_content"
	keyIssue3788Unavail    = "chat.issue3788.refused_unavailable"
	keyIssue3788EditBtn    = "chat.issue3788.edit"
	keyIssue3788CancelBtn  = "chat.issue3788.cancel"
	keyIssue3788DiscardBtn = "chat.issue3788.discard"
	keyIssue3788SaveBtn    = "chat.issue3788.save"
	keyIssue3788CloseBtn   = "chat.issue3788.close"
	keyIssue3788MsgLabel   = "chat.issue3788.message"
	keyIssue3788DateLabel  = "chat.issue3788.date"
	keyIssue3788TimeLabel  = "chat.issue3788.time"
	keyIssue3788TimeBad    = "chat.issue3788.time_bad"

	keyIssue3788MenuName  = "chat.issue3788.menu_name"
	keyIssue3788MenuNote  = "chat.issue3788.menu_note"
	keyIssue3788LaterHead = "chat.issue3788.later_heading"
	keyIssue3788LaterInfo = "chat.issue3788.later_intro"
	keyIssue3788InHour    = "chat.issue3788.preset_hour"
	keyIssue3788Evening   = "chat.issue3788.preset_evening"
	keyIssue3788Morning   = "chat.issue3788.preset_morning"
	keyIssue3788NextWeek  = "chat.issue3788.preset_week"
	keyIssue3788Pick      = "chat.issue3788.pick"
	keyIssue3788Schedule  = "chat.issue3788.schedule"
	keyIssue3788Zone      = "chat.issue3788.zone"
	keyIssue3788NoteEmpty = "chat.issue3788.note_empty"
	keyIssue3788NoteCmd   = "chat.issue3788.note_command"
	keyIssue3788NoteFiles = "chat.issue3788.note_files"
	keyIssue3788NoteLimit = "chat.issue3788.note_limit"
	keyIssue3788NoteTime  = "chat.issue3788.note_time"
	keyIssue3788NoteFail  = "chat.issue3788.note_failed"
	keyIssue3788NoteSaved = "chat.issue3788.note_saved"
	keyIssue3788NoteQuiet = "chat.issue3788.note_quiet"
)

var issue3788Copy = map[string]map[string]string{
	"en-US": {
		keyIssue3788Label:      "Scheduled",
		keyIssue3788Hint:       "See the messages you chose to send later",
		keyIssue3788Count:      "{n} scheduled",
		keyIssue3788Heading:    "Scheduled messages",
		keyIssue3788Intro:      "Only you can see these. Each one is sent as you at its time, and only if you can still post there and it still passes your workspace's rules.",
		keyIssue3788Loading:    "Loading your scheduled messages",
		keyIssue3788Empty:      "Nothing is scheduled. To write now and send later, choose Send later in the + menu of the message box.",
		keyIssue3788Error:      "Your scheduled messages could not be loaded.",
		keyIssue3788Retry:      "Try again",
		keyIssue3788Shown:      "Scheduled messages: {n}",
		keyIssue3788Failed:     "That change could not be saved. Try again.",
		keyIssue3788Where:      "To {where}",
		keyIssue3788Somewhere:  "a conversation",
		keyIssue3788Sends:      "Sends {when}",
		keyIssue3788Quiet:      "At least one person you are writing to has quiet hours then.",
		keyIssue3788NotSent:    "Not sent",
		keyIssue3788Sending:    "Sending now",
		keyIssue3788Access:     "You can no longer post in this conversation. Nothing was sent.",
		keyIssue3788Channel:    "This channel is not accepting messages right now. Nothing was sent.",
		keyIssue3788Content:    "A workspace rule held this message back. Nothing was sent.",
		keyIssue3788Unavail:    "It could not be sent just now. Nothing was sent.",
		keyIssue3788EditBtn:    "Edit",
		keyIssue3788CancelBtn:  "Cancel and delete",
		keyIssue3788DiscardBtn: "Discard",
		keyIssue3788SaveBtn:    "Save changes",
		keyIssue3788CloseBtn:   "Close",
		keyIssue3788MsgLabel:   "Message",
		keyIssue3788DateLabel:  "Date",
		keyIssue3788TimeLabel:  "Time",
		keyIssue3788TimeBad:    "Choose a date and time between two minutes and a year from now.",
		keyIssue3788MenuName:   "Send later",
		keyIssue3788MenuNote:   "Choose when this message goes out",
		keyIssue3788LaterHead:  "Send later",
		keyIssue3788LaterInfo:  "Your message is kept as a draft only you can see, then sent as you at the time you choose, if you can still post here.",
		keyIssue3788InHour:     "In one hour",
		keyIssue3788Evening:    "This evening",
		keyIssue3788Morning:    "Tomorrow morning",
		keyIssue3788NextWeek:   "Monday morning",
		keyIssue3788Pick:       "Pick a date and time",
		keyIssue3788Schedule:   "Schedule",
		keyIssue3788Zone:       "Times are in {zone}.",
		keyIssue3788NoteEmpty:  "Write a message first, then choose when to send it.",
		keyIssue3788NoteCmd:    "A slash command can't be scheduled. Send it now or remove it.",
		keyIssue3788NoteFiles:  "A message with files can't be scheduled yet. Remove the files or send it now.",
		keyIssue3788NoteLimit:  "You have the most scheduled messages allowed. Cancel one first.",
		keyIssue3788NoteTime:   "Choose a time between two minutes and a year from now.",
		keyIssue3788NoteFail:   "The message could not be scheduled. It is still in the box. Try again.",
		keyIssue3788NoteSaved:  "Scheduled for {when}. You can change or cancel it under Scheduled.",
		keyIssue3788NoteQuiet:  "At least one person you are writing to has quiet hours then.",
	},
	"de-DE": {
		keyIssue3788Label:      "Geplant",
		keyIssue3788Hint:       "Nachrichten ansehen, die Sie später senden wollten",
		keyIssue3788Count:      "{n} geplant",
		keyIssue3788Heading:    "Geplante Nachrichten",
		keyIssue3788Intro:      "Nur Sie sehen diese Nachrichten. Jede wird zur gewählten Zeit in Ihrem Namen gesendet, aber nur, wenn Sie dort noch schreiben dürfen und sie die Regeln Ihres Arbeitsbereichs erfüllt.",
		keyIssue3788Loading:    "Ihre geplanten Nachrichten werden geladen",
		keyIssue3788Empty:      "Nichts ist geplant. Um jetzt zu schreiben und später zu senden, wählen Sie im +-Menü des Nachrichtenfelds „Später senden“.",
		keyIssue3788Error:      "Ihre geplanten Nachrichten konnten nicht geladen werden.",
		keyIssue3788Retry:      "Erneut versuchen",
		keyIssue3788Shown:      "Geplante Nachrichten: {n}",
		keyIssue3788Failed:     "Die Änderung konnte nicht gespeichert werden. Versuchen Sie es erneut.",
		keyIssue3788Where:      "An {where}",
		keyIssue3788Somewhere:  "eine Unterhaltung",
		keyIssue3788Sends:      "Wird gesendet: {when}",
		keyIssue3788Quiet:      "Mindestens eine Person, an die Sie schreiben, hat dann Ruhezeiten.",
		keyIssue3788NotSent:    "Nicht gesendet",
		keyIssue3788Sending:    "Wird gerade gesendet",
		keyIssue3788Access:     "Sie dürfen in dieser Unterhaltung nicht mehr schreiben. Es wurde nichts gesendet.",
		keyIssue3788Channel:    "Dieser Kanal nimmt im Moment keine Nachrichten an. Es wurde nichts gesendet.",
		keyIssue3788Content:    "Eine Regel Ihres Arbeitsbereichs hat diese Nachricht zurückgehalten. Es wurde nichts gesendet.",
		keyIssue3788Unavail:    "Die Nachricht konnte gerade nicht gesendet werden. Es wurde nichts gesendet.",
		keyIssue3788EditBtn:    "Bearbeiten",
		keyIssue3788CancelBtn:  "Abbrechen und löschen",
		keyIssue3788DiscardBtn: "Verwerfen",
		keyIssue3788SaveBtn:    "Änderungen speichern",
		keyIssue3788CloseBtn:   "Schließen",
		keyIssue3788MsgLabel:   "Nachricht",
		keyIssue3788DateLabel:  "Datum",
		keyIssue3788TimeLabel:  "Uhrzeit",
		keyIssue3788TimeBad:    "Wählen Sie Datum und Uhrzeit zwischen zwei Minuten und einem Jahr ab jetzt.",
		keyIssue3788MenuName:   "Später senden",
		keyIssue3788MenuNote:   "Zeitpunkt für diese Nachricht wählen",
		keyIssue3788LaterHead:  "Später senden",
		keyIssue3788LaterInfo:  "Ihre Nachricht bleibt ein Entwurf, den nur Sie sehen, und wird zur gewählten Zeit in Ihrem Namen gesendet, wenn Sie hier dann noch schreiben dürfen.",
		keyIssue3788InHour:     "In einer Stunde",
		keyIssue3788Evening:    "Heute Abend",
		keyIssue3788Morning:    "Morgen früh",
		keyIssue3788NextWeek:   "Montagmorgen",
		keyIssue3788Pick:       "Datum und Uhrzeit wählen",
		keyIssue3788Schedule:   "Planen",
		keyIssue3788Zone:       "Die Zeiten gelten in {zone}.",
		keyIssue3788NoteEmpty:  "Schreiben Sie zuerst eine Nachricht und wählen Sie dann den Zeitpunkt.",
		keyIssue3788NoteCmd:    "Ein Schrägstrich-Befehl kann nicht geplant werden. Senden Sie ihn jetzt oder entfernen Sie ihn.",
		keyIssue3788NoteFiles:  "Eine Nachricht mit Dateien kann noch nicht geplant werden. Entfernen Sie die Dateien oder senden Sie sie jetzt.",
		keyIssue3788NoteLimit:  "Sie haben die höchste Zahl geplanter Nachrichten erreicht. Brechen Sie zuerst eine ab.",
		keyIssue3788NoteTime:   "Wählen Sie eine Zeit zwischen zwei Minuten und einem Jahr ab jetzt.",
		keyIssue3788NoteFail:   "Die Nachricht konnte nicht geplant werden. Sie steht noch im Feld. Versuchen Sie es erneut.",
		keyIssue3788NoteSaved:  "Geplant für {when}. Unter „Geplant“ können Sie sie ändern oder abbrechen.",
		keyIssue3788NoteQuiet:  "Mindestens eine Person, an die Sie schreiben, hat dann Ruhezeiten.",
	},
	"ar": {
		keyIssue3788Label:      "المجدولة",
		keyIssue3788Hint:       "اطّلع على الرسائل التي اخترت إرسالها لاحقاً",
		keyIssue3788Count:      "{n} مجدولة",
		keyIssue3788Heading:    "الرسائل المجدولة",
		keyIssue3788Intro:      "أنت وحدك ترى هذه الرسائل. تُرسَل كل رسالة باسمك في وقتها، بشرط أن تظل قادراً على الكتابة هناك وأن تستوفي قواعد مساحة العمل.",
		keyIssue3788Loading:    "جارٍ تحميل رسائلك المجدولة",
		keyIssue3788Empty:      "لا توجد رسائل مجدولة. لكتابة رسالة الآن وإرسالها لاحقاً، اختر «إرسال لاحقاً» من قائمة + في مربع الرسالة.",
		keyIssue3788Error:      "تعذر تحميل رسائلك المجدولة.",
		keyIssue3788Retry:      "حاول مرة أخرى",
		keyIssue3788Shown:      "الرسائل المجدولة: {n}",
		keyIssue3788Failed:     "تعذر حفظ هذا التغيير. حاول مرة أخرى.",
		keyIssue3788Where:      "إلى {where}",
		keyIssue3788Somewhere:  "محادثة",
		keyIssue3788Sends:      "تُرسَل {when}",
		keyIssue3788Quiet:      "لدى شخص واحد على الأقل ممن تكتب إليهم ساعات هدوء في ذلك الوقت.",
		keyIssue3788NotSent:    "لم تُرسَل",
		keyIssue3788Sending:    "جارٍ الإرسال الآن",
		keyIssue3788Access:     "لم يعد بإمكانك الكتابة في هذه المحادثة. لم يُرسَل شيء.",
		keyIssue3788Channel:    "هذه القناة لا تقبل رسائل في الوقت الحالي. لم يُرسَل شيء.",
		keyIssue3788Content:    "قاعدة في مساحة العمل منعت هذه الرسالة. لم يُرسَل شيء.",
		keyIssue3788Unavail:    "تعذر إرسالها الآن. لم يُرسَل شيء.",
		keyIssue3788EditBtn:    "تعديل",
		keyIssue3788CancelBtn:  "إلغاء وحذف",
		keyIssue3788DiscardBtn: "تجاهل",
		keyIssue3788SaveBtn:    "حفظ التغييرات",
		keyIssue3788CloseBtn:   "إغلاق",
		keyIssue3788MsgLabel:   "الرسالة",
		keyIssue3788DateLabel:  "التاريخ",
		keyIssue3788TimeLabel:  "الوقت",
		keyIssue3788TimeBad:    "اختر تاريخاً ووقتاً بين دقيقتين وسنة من الآن.",
		keyIssue3788MenuName:   "إرسال لاحقاً",
		keyIssue3788MenuNote:   "اختر موعد إرسال هذه الرسالة",
		keyIssue3788LaterHead:  "إرسال لاحقاً",
		keyIssue3788LaterInfo:  "تُحفَظ رسالتك مسودةً لا يراها غيرك، ثم تُرسَل باسمك في الوقت الذي تختاره إذا ظللت قادراً على الكتابة هنا.",
		keyIssue3788InHour:     "بعد ساعة",
		keyIssue3788Evening:    "هذا المساء",
		keyIssue3788Morning:    "صباح الغد",
		keyIssue3788NextWeek:   "صباح الاثنين",
		keyIssue3788Pick:       "اختر تاريخاً ووقتاً",
		keyIssue3788Schedule:   "جدولة",
		keyIssue3788Zone:       "الأوقات بتوقيت {zone}.",
		keyIssue3788NoteEmpty:  "اكتب رسالة أولاً ثم اختر وقت إرسالها.",
		keyIssue3788NoteCmd:    "لا يمكن جدولة أمر يبدأ بشرطة مائلة. أرسله الآن أو احذفه.",
		keyIssue3788NoteFiles:  "لا يمكن جدولة رسالة تحتوي على ملفات بعد. احذف الملفات أو أرسلها الآن.",
		keyIssue3788NoteLimit:  "وصلت إلى أقصى عدد من الرسائل المجدولة. ألغِ واحدة أولاً.",
		keyIssue3788NoteTime:   "اختر وقتاً بين دقيقتين وسنة من الآن.",
		keyIssue3788NoteFail:   "تعذرت جدولة الرسالة. ما زالت في المربع. حاول مرة أخرى.",
		keyIssue3788NoteSaved:  "جُدولت في {when}. يمكنك تغييرها أو إلغاءها من «المجدولة».",
		keyIssue3788NoteQuiet:  "لدى شخص واحد على الأقل ممن تكتب إليهم ساعات هدوء في ذلك الوقت.",
	},
}

// ---- times -----------------------------------------------------------------

type issue3788Preset struct {
	Key string
	At  time.Time
}

// issue3788Presets are the times offered in the person's own zone: in an hour,
// this evening (when that is two hours or more away), tomorrow at 08:30, and
// Monday at 08:30 (when that is not already tomorrow). A wall time that does
// not exist or happens twice that day (a clock change) is left out.
func issue3788Presets(m Model, now time.Time) []issue3788Preset {
	loc := chatcmd003Location(m)
	local := now.In(loc)
	at := func(days, hour, minute int) (time.Time, bool) {
		day := local.AddDate(0, 0, days)
		t, err := chat.Chatcmd004ResolveDateTime(day.Format("2006-01-02"), fmt.Sprintf("%02d:%02d", hour, minute), loc, now.Add(issue3788MinLead))
		return t, err == nil
	}
	out := []issue3788Preset{{keyIssue3788InHour, now.Add(time.Hour).Truncate(time.Minute)}}
	if t, ok := at(0, 18, 0); ok && t.Sub(now) >= 2*time.Hour {
		out = append(out, issue3788Preset{keyIssue3788Evening, t})
	}
	if t, ok := at(1, 8, 30); ok {
		out = append(out, issue3788Preset{keyIssue3788Morning, t})
	}
	if days := (8 - int(local.Weekday())) % 7; days != 1 {
		if days == 0 {
			days = 7
		}
		if t, ok := at(days, 8, 30); ok {
			out = append(out, issue3788Preset{keyIssue3788NextWeek, t})
		}
	}
	return out
}

// issue3788Zone names the zone times are read in, or "" when the page has none.
func issue3788Zone(m Model, at time.Time) string {
	zone := chatcmd003Location(m).String()
	if zone == "Local" {
		zone = ""
	}
	offset := chatcmd002Offset(at.In(chatcmd003Location(m)))
	if zone == "" {
		return offset
	}
	return zone + " (" + offset + ")"
}

// issue3788Window says whether at is a time the server will accept.
func issue3788Window(at, now time.Time) bool {
	return !at.Before(now.Add(issue3788MinLead)) && !at.After(now.Add(issue3788MaxLead))
}

// ---- Send later panel ------------------------------------------------------

// issue3788Available is whether Send later is offered in this conversation:
// not in an agent's conversation, and only where the page can reach the server.
func issue3788Available(m Model) bool {
	return m.SelectedID != "" && m.Callbacks.ScheduleSend != nil && !m.selected().Agent
}

func issue3788NoteText(m Model, local localUI) string {
	switch local.sendLaterNote {
	case "empty":
		return laneText(m, issue3788Copy, keyIssue3788NoteEmpty)
	case "command":
		return laneText(m, issue3788Copy, keyIssue3788NoteCmd)
	case "files":
		return laneText(m, issue3788Copy, keyIssue3788NoteFiles)
	case "limit":
		return laneText(m, issue3788Copy, keyIssue3788NoteLimit)
	case "time":
		return laneText(m, issue3788Copy, keyIssue3788NoteTime)
	case "failed":
		return laneText(m, issue3788Copy, keyIssue3788NoteFail)
	}
	return ""
}

// issue3788Panel is the Send later panel above the message box, drawn only
// while it is open. Its date and time fields belong to no form, so Enter in
// them can never send the message that is in the box.
func issue3788Panel(m Model, local localUI) ui.Node {
	if !local.sendLaterOpen || !issue3788Available(m) {
		return nil
	}
	now := time.Now()
	buttons := []ui.Node{}
	for _, preset := range issue3788Presets(m, now) {
		name := laneText(m, issue3788Copy, preset.Key)
		stamp := chatcmd002Stamp(m, preset.At)
		buttons = append(buttons, html.Button(html.Props{Class: "button secondary issue3788-preset", Type: "button", Disabled: local.sendLaterBusy,
			Data: map[string]string{"action": issue3788Pick, "id": preset.At.UTC().Format(time.RFC3339)},
			Aria: map[string]string{"label": name + ", " + stamp}},
			html.Span(html.Props{Class: "issue3788-preset-name", Dir: "auto", Text: name}),
			html.Span(html.Props{Class: "issue3788-preset-time", Text: stamp})))
	}
	none := map[string]any{"form": issue3788FormNone}
	status := issue3788NoteText(m, local)
	children := []ui.Node{
		html.H3(html.Props{ID: issue3788LaterHeadID, Class: "issue3788-heading", Text: laneText(m, issue3788Copy, keyIssue3788LaterHead), Raw: map[string]any{"tabindex": "-1"}}),
		html.P(html.Props{Class: "issue3788-intro", Text: laneText(m, issue3788Copy, keyIssue3788LaterInfo)}),
		html.Div(html.Props{Class: "issue3788-presets"}, buttons...),
		html.Div(html.Props{Class: "issue3788-custom", Role: "group", Aria: map[string]string{"label": laneText(m, issue3788Copy, keyIssue3788Pick)}},
			html.Span(html.Props{Class: "issue3788-custom-label", Text: laneText(m, issue3788Copy, keyIssue3788Pick)}),
			html.Div(html.Props{Class: "issue3788-field"}, html.Label(html.Props{For: issue3788DateID, Text: laneText(m, issue3788Copy, keyIssue3788DateLabel)}),
				html.Input(html.Props{ID: issue3788DateID, Class: "chat-input", Type: "date", Min: now.In(chatcmd003Location(m)).Format("2006-01-02"), Disabled: local.sendLaterBusy, Raw: none, Data: map[string]string{"chat-value": local.sendLaterDate}})),
			html.Div(html.Props{Class: "issue3788-field"}, html.Label(html.Props{For: issue3788TimeID, Text: laneText(m, issue3788Copy, keyIssue3788TimeLabel)}),
				html.Input(html.Props{ID: issue3788TimeID, Class: "chat-input", Type: "time", Disabled: local.sendLaterBusy, Raw: none, Data: map[string]string{"chat-value": local.sendLaterClock}})),
			html.Button(html.Props{Class: "button small issue3788-schedule", Type: "button", Disabled: local.sendLaterBusy, Data: map[string]string{"action": issue3788Custom}, Text: laneText(m, issue3788Copy, keyIssue3788Schedule)})),
		html.P(html.Props{Class: "issue3788-zone", Dir: "auto", Text: laneTextf(m, issue3788Copy, keyIssue3788Zone, map[string]string{"zone": issue3788Zone(m, now)})}),
	}
	if status != "" {
		children = append(children, html.P(html.Props{Class: "issue3788-note", Role: "alert", Dir: "auto", Text: status}))
	}
	children = append(children, html.Button(html.Props{Class: "button secondary small issue3788-close", Type: "button", Data: map[string]string{"action": issue3788Close}, Text: laneText(m, issue3788Copy, keyIssue3788CloseBtn)}))
	return html.Section(html.Props{ID: issue3788PanelID, Class: "issue3788-later", Aria: map[string]string{"labelledby": issue3788LaterHeadID}}, children...)
}

// issue3788Confirmation is the polite line under the message box after a
// message was kept: when it will go, and the quiet-hours warning if one applies.
func issue3788Confirmation(m Model, local localUI) ui.Node {
	if local.sendLaterSaved.IsZero() {
		return html.Span(html.Props{ID: "issue3788-saved", Class: "sr-only", Role: "status", Aria: map[string]string{"live": "polite"}})
	}
	text := laneTextf(m, issue3788Copy, keyIssue3788NoteSaved, map[string]string{"when": chatcmd002Stamp(m, local.sendLaterSaved)})
	children := []ui.Node{html.Span(html.Props{Dir: "auto", Text: text})}
	if local.sendLaterQuiet > 0 {
		children = append(children, html.Span(html.Props{Class: "issue3788-quiet", Dir: "auto", Text: " " + laneText(m, issue3788Copy, keyIssue3788NoteQuiet)}))
	}
	return html.P(html.Props{ID: "issue3788-saved", Class: "issue3788-saved", Role: "status", Aria: map[string]string{"live": "polite"}}, children...)
}

// ---- Scheduled tray --------------------------------------------------------

func issue3788Control(m Model, h handlers) ui.Node {
	if m.Callbacks.LoadScheduled == nil {
		return nil
	}
	on := h.local.scheduledOpen
	label := laneText(m, issue3788Copy, keyIssue3788Label)
	name := label
	children := []ui.Node{icon("calendar"), html.Span(html.Props{Class: "chat-row-name", Text: label})}
	if m.SendLater.Loaded && !m.SendLater.Fail {
		count := laneTextf(m, issue3788Copy, keyIssue3788Count, map[string]string{"n": m.n(len(m.SendLater.Rows))})
		name += ", " + count
		if len(m.SendLater.Rows) > 0 {
			children = append(children, html.Span(html.Props{Class: "chat-count issue3788-count", Aria: map[string]string{"hidden": "true"}, Text: m.n(len(m.SendLater.Rows))}))
		}
	}
	class := "chat-row issue3788-row"
	if on {
		class += " on"
	}
	return html.Div(html.Props{Class: "issue3788-control"},
		html.Button(html.Props{ID: issue3788ToggleID, Class: class, Type: "button", Title: laneText(m, issue3788Copy, keyIssue3788Hint),
			Data: map[string]string{"action": issue3788Toggle},
			Aria: map[string]string{"expanded": boolString(on), "controls": issue3788PanelID + "-tray", "label": name}}, children...))
}

func issue3788Where(m Model, row ScheduledRow) string {
	name := strings.TrimSpace(row.Channel)
	switch {
	case name == "":
		return laneText(m, issue3788Copy, keyIssue3788Somewhere)
	case row.InChannel:
		return "#" + name
	}
	return name
}

func issue3788Refusal(m Model, row ScheduledRow) string {
	switch row.Refusal {
	case "no_access":
		return laneText(m, issue3788Copy, keyIssue3788Access)
	case "channel_closed":
		return laneText(m, issue3788Copy, keyIssue3788Channel)
	case "content_blocked":
		return laneText(m, issue3788Copy, keyIssue3788Content)
	}
	return laneText(m, issue3788Copy, keyIssue3788Unavail)
}

// issue3788Tray is the open list: a labelled region with a heading, a polite
// status line, and the loading, empty, unavailable or populated body.
func issue3788Tray(m Model, local localUI) ui.Node {
	heading := html.H2(html.Props{ID: issue3788HeadID, Class: "issue3788-heading", Text: laneText(m, issue3788Copy, keyIssue3788Heading), Raw: map[string]any{"tabindex": "-1"}})
	state := m.SendLater
	status := ""
	var body ui.Node
	switch {
	case state.Loading && !state.Loaded:
		body = html.P(html.Props{Class: "rail-empty issue3788-empty", Role: "status", Text: laneText(m, issue3788Copy, keyIssue3788Loading)})
	case state.Fail && !state.Loaded:
		body = html.Div(html.Props{Class: "issue3788-empty", Role: "alert"},
			html.P(html.Props{Class: "rail-empty", Text: laneText(m, issue3788Copy, keyIssue3788Error)}),
			html.Button(html.Props{Class: "button secondary small issue3788-retry", Type: "button", Data: map[string]string{"action": issue3788Retry}, Text: laneText(m, issue3788Copy, keyIssue3788Retry)}))
	case len(state.Rows) == 0:
		body = html.P(html.Props{Class: "rail-empty issue3788-empty", Role: "status", Text: laneText(m, issue3788Copy, keyIssue3788Empty)})
	default:
		status = laneTextf(m, issue3788Copy, keyIssue3788Shown, map[string]string{"n": m.n(len(state.Rows))})
		items := make([]ui.Node, 0, len(state.Rows))
		for _, row := range state.Rows {
			items = append(items, html.WithKey(issue3788Item(m, local, row), "scheduled:"+row.ID))
		}
		body = html.Ul(html.Props{Class: "issue3788-list"}, items...)
	}
	children := []ui.Node{heading,
		html.P(html.Props{Class: "issue3788-intro", Text: laneText(m, issue3788Copy, keyIssue3788Intro)}),
		html.Span(html.Props{Class: "sr-only", Role: "status", Aria: map[string]string{"live": "polite"}, Text: status}), body}
	if state.Note != "" {
		children = append(children, html.P(html.Props{Class: "issue3788-failed", Role: "alert", Text: laneText(m, issue3788Copy, keyIssue3788Failed)}))
	}
	return html.Section(html.Props{ID: issue3788PanelID + "-tray", Class: "issue3788-tray", Aria: map[string]string{"labelledby": issue3788HeadID}}, children...)
}

func issue3788Item(m Model, local localUI, row ScheduledRow) ui.Node {
	if local.scheduledEdit == row.ID {
		return issue3788Editor(m, local, row)
	}
	preview := strings.Join(strings.Fields(row.Body), " ")
	action := func(name, key string) ui.Node {
		text := laneText(m, issue3788Copy, key)
		return html.Button(html.Props{Class: "button secondary small issue3788-act", Type: "button", Disabled: row.State == "sending",
			Data: map[string]string{"action": name, "id": row.ID}, Aria: map[string]string{"label": text + ": " + preview}, Text: text})
	}
	head := []ui.Node{
		html.Span(html.Props{Class: "issue3788-where", Dir: "auto", Text: laneTextf(m, issue3788Copy, keyIssue3788Where, map[string]string{"where": issue3788Where(m, row)})}),
		html.Span(html.Props{Class: "issue3788-preview", Dir: "auto", Text: preview}),
	}
	discard := keyIssue3788CancelBtn
	switch row.State {
	case "failed":
		head = append(head,
			html.Span(html.Props{Class: "issue3788-when"}, html.Span(html.Props{Class: "issue3788-chip", Text: laneText(m, issue3788Copy, keyIssue3788NotSent)})),
			html.Span(html.Props{Class: "issue3788-reason", Role: "note", Dir: "auto", Text: issue3788Refusal(m, row)}))
		discard = keyIssue3788DiscardBtn
	case "sending":
		head = append(head, html.Span(html.Props{Class: "issue3788-when", Text: laneText(m, issue3788Copy, keyIssue3788Sending)}))
	default:
		head = append(head, html.Span(html.Props{Class: "issue3788-when", Dir: "auto", Text: laneTextf(m, issue3788Copy, keyIssue3788Sends, map[string]string{"when": chatcmd002Stamp(m, row.SendAt)})}))
		if row.QuietPeople > 0 {
			head = append(head, html.Span(html.Props{Class: "issue3788-quiet", Dir: "auto", Text: laneText(m, issue3788Copy, keyIssue3788Quiet)}))
		}
	}
	return html.Li(html.Props{Class: "issue3788-item", Data: map[string]string{"scheduled-id": row.ID}},
		html.Div(html.Props{Class: "issue3788-body"}, head...),
		html.Div(html.Props{Class: "issue3788-actions"}, action(issue3788Edit, keyIssue3788EditBtn), action(issue3788Cancel, discard)))
}

// issue3788Editor changes one message in place: its text, date and time, shown
// in the person's own zone. The fields belong to no form.
func issue3788Editor(m Model, local localUI, row ScheduledRow) ui.Node {
	loc := chatcmd003Location(m)
	date, clock := local.scheduledDate, local.scheduledClock
	if date == "" && clock == "" {
		at := row.SendAt.In(loc)
		date, clock = at.Format("2006-01-02"), at.Format(issue3788ClockLayout)
	}
	none := map[string]any{"form": issue3788FormNone}
	children := []ui.Node{
		html.Div(html.Props{Class: "issue3788-field"}, html.Label(html.Props{For: issue3788EditBodyID, Text: laneText(m, issue3788Copy, keyIssue3788MsgLabel)}),
			html.Textarea(html.Props{ID: issue3788EditBodyID, Class: "chat-input", Rows: 3, Dir: "auto", Raw: none, Data: map[string]string{"chat-value": local.scheduledBody}})),
		html.Div(html.Props{Class: "issue3788-field"}, html.Label(html.Props{For: issue3788EditDateID, Text: laneText(m, issue3788Copy, keyIssue3788DateLabel)}),
			html.Input(html.Props{ID: issue3788EditDateID, Class: "chat-input", Type: "date", Raw: none, Data: map[string]string{"chat-value": date}})),
		html.Div(html.Props{Class: "issue3788-field"}, html.Label(html.Props{For: issue3788EditTimeID, Text: laneText(m, issue3788Copy, keyIssue3788TimeLabel)}),
			html.Input(html.Props{ID: issue3788EditTimeID, Class: "chat-input", Type: "time", Raw: none, Data: map[string]string{"chat-value": clock}})),
		html.P(html.Props{Class: "issue3788-zone", Dir: "auto", Text: laneTextf(m, issue3788Copy, keyIssue3788Zone, map[string]string{"zone": issue3788Zone(m, row.SendAt)})}),
	}
	if local.scheduledEditNote != "" {
		reason := keyIssue3788TimeBad
		if local.scheduledEditNote == "failed" {
			reason = keyIssue3788Failed
		}
		children = append(children, html.P(html.Props{Class: "issue3788-note", Role: "alert", Text: laneText(m, issue3788Copy, reason)}))
	}
	children = append(children, html.Div(html.Props{Class: "issue3788-actions"},
		html.Button(html.Props{Class: "button small issue3788-act", Type: "button", Data: map[string]string{"action": issue3788Save, "id": row.ID}, Text: laneText(m, issue3788Copy, keyIssue3788SaveBtn)}),
		html.Button(html.Props{Class: "button secondary small issue3788-act", Type: "button", Data: map[string]string{"action": issue3788CloseEdit, "id": row.ID}, Text: laneText(m, issue3788Copy, keyIssue3788CloseBtn)})))
	return html.Li(html.Props{Class: "issue3788-item issue3788-editing", Data: map[string]string{"scheduled-id": row.ID}},
		html.Div(html.Props{Class: "issue3788-edit", Role: "group", Aria: map[string]string{"label": laneText(m, issue3788Copy, keyIssue3788EditBtn)}}, children...))
}

// ---- actions ---------------------------------------------------------------

// issue3788Body is the text of the message box ready to be kept for later, or
// the reason it cannot be: "empty", "command" (a slash command) or "files".
func issue3788Body(m Model) (string, string) {
	raw := strings.TrimSpace(domValue("chat-composer"))
	if raw == "" {
		raw = strings.TrimSpace(m.Draft)
	}
	switch {
	case m.Chatattach001 != nil && len(m.Chatattach001.Files) > 0:
		return "", "files"
	case raw == "":
		return "", "empty"
	case strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//"):
		return "", "command"
	}
	return strings.TrimSpace(emojiShortcodesInText(raw)), ""
}

// issue3788Click handles the Send later panel and the Scheduled tray. clear
// empties the message box after a message was kept. It reports whether it
// consumed the click.
func issue3788Click(e ui.MouseEvent, m Model, local localStore, clear func(conversation string)) bool {
	action, id, _ := eventAction(e)
	if !strings.HasPrefix(action, "scheduled-") && !strings.HasPrefix(action, "sendlater-") {
		return false
	}
	switch action {
	case issue3788Toggle:
		if local.get().scheduledOpen {
			local.update(func(u *localUI) { u.scheduledOpen, u.scheduledEdit = false, "" })
			return true
		}
		local.update(func(u *localUI) { issue900002Exclusive(u, issue900002Scheduled) })
		// Opening always asks again, so the list is never older than the click.
		if m.Callbacks.LoadScheduled != nil {
			m.Callbacks.LoadScheduled()
		}
		focusField(issue3788HeadID)
		return true
	case issue3788Retry:
		if m.Callbacks.LoadScheduled != nil {
			m.Callbacks.LoadScheduled()
		}
		return true
	case issue3788Close:
		local.update(func(u *localUI) { u.sendLaterOpen, u.sendLaterNote = false, "" })
		focusField("chat-composer")
		return true
	case issue3788Pick, issue3788Custom:
		at, err := time.Parse(time.RFC3339, id)
		if action == issue3788Custom {
			date, clock := domValue(issue3788DateID), domValue(issue3788TimeID)
			local.update(func(u *localUI) { u.sendLaterDate, u.sendLaterClock = date, clock })
			at, err = chat.Chatcmd004ResolveDateTime(date, clock, chatcmd003Location(m), time.Now())
		}
		issue3788Schedule(m, local, clear, at, err)
		return true
	case issue3788Edit:
		for _, row := range m.SendLater.Rows {
			if row.ID == id {
				local.update(func(u *localUI) {
					u.scheduledEdit, u.scheduledBody, u.scheduledEditNote = id, row.Body, ""
					at := row.SendAt.In(chatcmd003Location(m))
					u.scheduledDate, u.scheduledClock = at.Format("2006-01-02"), at.Format(issue3788ClockLayout)
				})
				focusField(issue3788EditBodyID)
			}
		}
		return true
	case issue3788CloseEdit:
		local.update(func(u *localUI) { u.scheduledEdit, u.scheduledEditNote = "", "" })
		focusField(issue3788HeadID)
		return true
	case issue3788Save:
		body := strings.TrimSpace(domValue(issue3788EditBodyID))
		date, clock := domValue(issue3788EditDateID), domValue(issue3788EditTimeID)
		at, err := chat.Chatcmd004ResolveDateTime(date, clock, chatcmd003Location(m), time.Now())
		if body == "" || err != nil || !issue3788Window(at, time.Now()) || m.Callbacks.EditScheduled == nil {
			local.update(func(u *localUI) {
				u.scheduledBody, u.scheduledDate, u.scheduledClock, u.scheduledEditNote = body, date, clock, "time"
			})
			focusField(issue3788EditDateID)
			return true
		}
		var revision uint64
		for _, row := range m.SendLater.Rows {
			if row.ID == id {
				revision = row.Revision
			}
		}
		m.Callbacks.EditScheduled(id, body, at, revision, func(result SendLaterResult) {
			if result.Outcome == "saved" {
				local.update(func(u *localUI) { u.scheduledEdit, u.scheduledEditNote = "", "" })
				focusField(issue3788HeadID)
				return
			}
			note := "failed"
			if result.Outcome == "time" {
				note = "time"
			}
			local.update(func(u *localUI) { u.scheduledEditNote = note })
		})
		return true
	case issue3788Cancel:
		if m.Callbacks.CancelScheduled != nil {
			m.Callbacks.CancelScheduled(id)
			// The row goes away; focus moves to the heading rather than falling to the page.
			focusField(issue3788HeadID)
		}
		return true
	}
	return false
}

// issue3788Schedule keeps the message in the box for the chosen time.
func issue3788Schedule(m Model, local localStore, clear func(string), at time.Time, err error) {
	body, why := issue3788Body(m)
	fail := func(note string) {
		local.update(func(u *localUI) { u.sendLaterNote = note })
	}
	switch {
	case why != "":
		fail(why)
		return
	case err != nil || !issue3788Window(at, time.Now()):
		fail("time")
		return
	case m.Callbacks.ScheduleSend == nil || m.SelectedID == "":
		fail("failed")
		return
	}
	conversation := m.SelectedID
	local.update(func(u *localUI) { u.sendLaterBusy, u.sendLaterNote = true, "" })
	m.Callbacks.ScheduleSend(conversation, body, at, func(result SendLaterResult) {
		switch result.Outcome {
		case "saved":
			if clear != nil {
				clear(conversation)
			}
			local.update(func(u *localUI) {
				u.sendLaterBusy, u.sendLaterOpen, u.sendLaterNote = false, false, ""
				u.sendLaterSaved, u.sendLaterQuiet = result.SendAt, result.QuietPeople
			})
			focusField("chat-composer")
		case "limit", "time":
			local.update(func(u *localUI) { u.sendLaterBusy, u.sendLaterNote = false, result.Outcome })
		default:
			local.update(func(u *localUI) { u.sendLaterBusy, u.sendLaterNote = false, "failed" })
		}
	})
}

// issue3788Key closes the Send later panel on Escape from inside it, with focus
// going back to the message box. It reports whether the key was used.
func issue3788Key(e ui.KeyboardEvent, local localStore) bool {
	if e.GetKey() != "Escape" || !local.get().sendLaterOpen || !sendLaterFocusInside() {
		return false
	}
	local.update(func(u *localUI) { u.sendLaterOpen, u.sendLaterNote = false, "" })
	focusField("chat-composer")
	return true
}

// issue3788Choose runs the + menu's Send later item: the menu closes with focus
// going back to the + button, the panel opens, and focus moves to its heading.
func issue3788Choose(m Model, local localStore) {
	if !issue3788Available(m) {
		return
	}
	if local.get().sendLaterOpen {
		return
	}
	local.update(func(u *localUI) {
		u.sendLaterOpen, u.sendLaterNote, u.sendLaterSaved = true, "", time.Time{}
	})
	focusField(issue3788LaterHeadID)
}

// Issue3788Styles lays the panel and the tray out with logical properties, so
// they hold in right-to-left, and makes every control a 44px target on phones.
const Issue3788Styles = `.issue3788-control{padding:0 .75rem}` +
	`.issue3788-row{width:100%}.issue3788-row.on{font-weight:600}` +
	`.issue3788-tray{margin:.25rem .75rem .5rem;padding:.5rem;border:1px solid var(--line,rgba(0,0,0,.15));border-radius:.5rem;max-height:60vh;overflow-y:auto}` +
	`.issue3788-heading{margin:0 0 .25rem;font-size:.875rem;font-weight:600}.issue3788-heading:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}` +
	`.issue3788-intro{margin:0 0 .5rem;font-size:.75rem;opacity:.8}` +
	`.issue3788-empty{margin:.25rem 0;display:grid;gap:.5rem;justify-items:start}` +
	`.issue3788-list{list-style:none;margin:0;padding:0;display:grid;gap:.5rem}` +
	`.issue3788-item{display:grid;gap:.375rem;padding:.375rem .5rem;border-radius:.375rem;background:var(--soft)}` +
	`.issue3788-body{display:grid;gap:.125rem;min-width:0}` +
	`.issue3788-where{font-weight:600;font-size:.8125rem;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}` +
	`.issue3788-preview{overflow:hidden;display:-webkit-box;-webkit-line-clamp:3;-webkit-box-orient:vertical;overflow-wrap:anywhere}` +
	`.issue3788-when,.issue3788-reason,.issue3788-quiet{font-size:.75rem;opacity:.9}` +
	`.issue3788-chip{display:inline-block;padding:0 .375rem;border:1px solid currentColor;border-radius:999px;font-weight:600}` +
	`.issue3788-actions{display:flex;flex-wrap:wrap;gap:.25rem}` +
	`.issue3788-edit{display:grid;gap:.5rem}.issue3788-field{display:grid;gap:.125rem}.issue3788-field label{font-size:.75rem}` +
	`.issue3788-later{margin:0 0 .5rem;padding:.5rem;border:1px solid var(--line,rgba(0,0,0,.15));border-radius:.5rem;display:grid;gap:.5rem}` +
	`.issue3788-presets{display:flex;flex-wrap:wrap;gap:.25rem}` +
	`.issue3788-preset{display:grid;gap:0;justify-items:start;text-align:start}.issue3788-preset-time{font-size:.6875rem;opacity:.8}` +
	`.issue3788-custom{display:flex;flex-wrap:wrap;gap:.5rem;align-items:end}.issue3788-custom-label{flex-basis:100%;font-size:.75rem;font-weight:600}` +
	`.issue3788-zone{margin:0;font-size:.6875rem;opacity:.8}.issue3788-saved{margin:.25rem 0 0;font-size:.75rem}` +
	`.issue3788-act:focus-visible,.issue3788-row:focus-visible,.issue3788-retry:focus-visible,.issue3788-preset:focus-visible,.issue3788-schedule:focus-visible,.issue3788-close:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}` +
	`.issue3788-failed,.issue3788-note{margin:.25rem 0 0;font-size:.8125rem}` +
	`@media (max-width:640px){.issue3788-row,.issue3788-act,.issue3788-retry,.issue3788-preset,.issue3788-schedule,.issue3788-close,.issue3788-later .chat-input{min-height:44px}.issue3788-presets{display:grid}}` +
	`@media (prefers-reduced-motion:reduce){.issue3788-tray{scroll-behavior:auto}}`
