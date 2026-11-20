package chatui

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// Issue 3958. Prepare a meeting brief from saved messages.
//
// The person picks saved messages, says what the meeting is for, and gets a
// private, editable agenda: decisions needed, questions to resolve and relevant
// context, every item pointing back to the message it came from. The agenda is
// built by plain rules, not by a model: it sorts messages by what they say on
// their face, never invents an owner, a date or an attendee list, and never
// claims a meeting exists. Message text is untrusted: it is shown as one
// quoted, shortened line and never as markup or as a mention.

// MeetingBriefKind says what a saved message is on the agenda.
type MeetingBriefKind string

const (
	BriefDecision  MeetingBriefKind = "decision"  // a decision still to be made
	BriefConfirmed MeetingBriefKind = "confirmed" // a decision the message says was made
	BriefQuestion  MeetingBriefKind = "question"
	BriefContext   MeetingBriefKind = "context"
)

// BriefMaxSources is how many saved messages one brief takes.
const BriefMaxSources = 25

const briefExcerptRunes = 200

var (
	briefConfirmedCues = []string{
		"we decided", "decided to", "decision:", "decision made", "agreed", "approved", "confirmed", "signed off", "it's settled", "its settled",
		"beschlossen", "entschieden", "vereinbart", "genehmigt", "bestätigt", "freigegeben",
		"تقرر", "تم الاتفاق", "تمت الموافقة", "تم اعتماد", "تم تأكيد", "اتفقنا",
	}
	briefOpenCues = []string{
		"not approved", "not confirmed", "unconfirmed", "not agreed", "disagree", "wasn't approved", "isn't approved", "nicht genehmigt", "nicht bestätigt", "nicht vereinbart", "nicht freigegeben", "لم تتم الموافقة", "غير مؤكد",
		"not yet", "still need", "still waiting", "pending", "tbd", "to be decided", "needs approval", "need approval", "awaiting", "waiting for approval", "unless",
		"noch nicht", "ausstehend", "offen", "wartet auf", "muss noch",
		"لم يتم بعد", "قيد الانتظار", "بانتظار", "لم يُحسم",
	}
	briefDecisionCues = []string{
		"decide", "decision", "approve", "approval", "sign off", "sign-off", "choose", "go with", "pick between", "green light",
		"entscheid", "genehmig", "freigabe", "auswählen", "festlegen",
		"نقرر", "قرار", "الموافقة", "اعتماد", "نختار", "نحسم",
	}
)

func briefHasAny(text string, cues []string) bool {
	for _, cue := range cues {
		if strings.Contains(text, cue) {
			return true
		}
	}
	return false
}

// MeetingBriefKindOf reads one message's words. A question mark makes it a
// question, unless it is plainly asking for a decision; wording that says a
// decision was made, without a question or a "not yet", makes it a confirmed
// decision; anything else is context. The person edits the result.
func MeetingBriefKindOf(body string) MeetingBriefKind {
	text := strings.ToLower(strings.Join(strings.Fields(body), " "))
	if text == "" {
		return BriefContext
	}
	question := strings.ContainsAny(text, "?؟")
	open := briefHasAny(text, briefOpenCues)
	switch {
	case question && briefHasAny(text, briefDecisionCues):
		return BriefDecision
	case question:
		return BriefQuestion
	case briefHasAny(text, briefConfirmedCues) && !open:
		return BriefConfirmed
	case briefHasAny(text, briefDecisionCues):
		return BriefDecision
	}
	return BriefContext
}

// MeetingBriefExcerpt is a message as one safe, quoted line: control characters
// and line breaks collapse to spaces, an @ can no longer notify anyone, and a
// long message is cut at a word.
func MeetingBriefExcerpt(body string) string {
	text := chatDisplayBody(Chatcmd002PlainBody(body))
	var b strings.Builder
	for _, r := range text {
		switch {
		case unicode.IsSpace(r):
			b.WriteRune(' ')
		case unicode.IsControl(r), r == '​', r == '‮', r == '‭', r == '⁦', r == '⁧', r == '⁨', r == '⁩':
		default:
			b.WriteRune(r)
		}
	}
	text = strings.Join(strings.Fields(b.String()), " ")
	text = strings.ReplaceAll(text, "@", "@​")
	runes := []rune(text)
	if len(runes) > briefExcerptRunes {
		cut := string(runes[:briefExcerptRunes])
		if at := strings.LastIndex(cut, " "); at > briefExcerptRunes/2 {
			cut = cut[:at]
		}
		text = strings.TrimRight(cut, " ,;:.") + "…"
	}
	return text
}

// MeetingBriefSkip is a selected saved message the brief could not use, and why.
type MeetingBriefSkip struct {
	Row    SavedMessageRow
	Reason string // no_access, removed, deleted or missing
}

// MeetingBriefInput is what the brief is built from. Rows are the selected
// saved messages as just read from the service, in the order the agenda lists
// them within each section.
type MeetingBriefInput struct {
	Locale, Purpose, When, Origin string
	IncludeNotes                  bool
	Rows                          []SavedMessageRow
	Now                           time.Time
}

// MeetingBrief is the private agenda and what it left out.
type MeetingBrief struct {
	Text string
	// Included are the messages the agenda cites, in citation order: the
	// number in "Source [n]" is the position here plus one.
	Included    []SavedMessageRow
	Left        []MeetingBriefSkip
	NotesInText bool
}

// MeetingBriefSkipReason is empty for a saved message the brief can use, and
// otherwise why it cannot: no_access, removed, deleted or missing.
func MeetingBriefSkipReason(row SavedMessageRow) string { return briefSkipReason(row) }

func briefSkipReason(row SavedMessageRow) string {
	switch row.Availability {
	case "readable":
		return ""
	case "removed", "deleted", "missing":
		return row.Availability
	}
	return "no_access"
}

// MeetingBriefReasonText is the plain sentence for a left-out message.
func MeetingBriefReasonText(locale, reason string) string {
	copy := SavedMessagesCopy(locale)
	switch reason {
	case "removed":
		return copy.Removed
	case "deleted":
		return copy.Deleted
	case "missing":
		return chatsave002Text(locale, "br_why_missing")
	}
	return copy.NoAccess
}

func briefSourceLabel(locale string) string { return chatsave002Text(locale, "br_source") }
func briefNoteLabel(locale string) string   { return chatsave002Text(locale, "br_note") }

// BuildMeetingBrief builds the agenda. A message the person can no longer read
// is left out and listed in Left; its text is never used.
func BuildMeetingBrief(in MeetingBriefInput) MeetingBrief {
	loc := in.Locale
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	brief := MeetingBrief{}
	type entry struct {
		kind MeetingBriefKind
		row  SavedMessageRow
	}
	var entries []entry
	for _, row := range in.Rows {
		if reason := briefSkipReason(row); reason != "" {
			brief.Left = append(brief.Left, MeetingBriefSkip{Row: row, Reason: reason})
			continue
		}
		entries = append(entries, entry{MeetingBriefKindOf(Chatcmd002PlainBody(row.Body)), row})
	}
	sections := []struct {
		key   string
		kinds []MeetingBriefKind
	}{
		{"br_sec_decisions", []MeetingBriefKind{BriefDecision}},
		{"br_sec_questions", []MeetingBriefKind{BriefQuestion}},
		{"br_sec_context", []MeetingBriefKind{BriefConfirmed, BriefContext}},
	}
	// The citation numbers follow the order the agenda prints.
	var ordered []entry
	for _, section := range sections {
		for _, kind := range section.kinds {
			for _, e := range entries {
				if e.kind == kind {
					ordered = append(ordered, e)
				}
			}
		}
	}
	for _, e := range ordered {
		brief.Included = append(brief.Included, e.row)
	}

	var b strings.Builder
	b.WriteString(chatsave002Text(loc, "br_title") + "\n")
	if purpose := briefLine(in.Purpose, 200); purpose != "" {
		b.WriteString(chatsave002Text(loc, "br_purpose", "text", purpose) + "\n")
	} else {
		b.WriteString(chatsave002Text(loc, "br_purpose_none") + "\n")
	}
	if when := briefLine(in.When, 80); when != "" {
		b.WriteString(chatsave002Text(loc, "br_time", "text", when) + "\n")
	} else {
		b.WriteString(chatsave002Text(loc, "br_time_none") + "\n")
	}
	b.WriteString(chatsave002Text(loc, "br_prepared", "n", chatsave002Num(loc, len(ordered))) + "\n")

	number := 0
	for _, section := range sections {
		var rows []entry
		for _, kind := range section.kinds {
			for _, e := range entries {
				if e.kind == kind {
					rows = append(rows, e)
				}
			}
		}
		if len(rows) == 0 {
			continue
		}
		b.WriteString("\n" + chatsave002Text(loc, section.key) + "\n")
		for _, e := range rows {
			number++
			n := chatsave002Num(loc, number)
			fmt.Fprintf(&b, "%s. [%s] “%s”\n", n, chatsave002Text(loc, "br_tag_"+string(e.kind)), MeetingBriefExcerpt(e.row.Body))
			if e.kind != BriefContext {
				b.WriteString("   " + chatsave002Text(loc, "br_owner_date") + "\n")
			}
			b.WriteString("   " + briefSourceLabel(loc) + " [" + n + "]: " + briefWhere(in, e.row, now) + "\n")
			if note := briefLine(e.row.Note, 400); in.IncludeNotes && note != "" {
				brief.NotesInText = true
				b.WriteString("   " + briefNoteLabel(loc) + " [" + n + "]: " + note + "\n")
			}
		}
	}
	brief.Text = strings.TrimRight(b.String(), "\n") + "\n"
	return brief
}

// briefLine is typed or untrusted text as one line, limited to max runes.
func briefLine(text string, max int) string {
	text = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			return -1
		}
		return r
	}, text)), " ")
	text = strings.ReplaceAll(text, "@", "@​")
	if runes := []rune(text); len(runes) > max {
		text = string(runes[:max]) + "…"
	}
	return text
}

// briefWhere is where to find a message again: the conversation, who wrote it,
// when, and the conversation's link. (A message has no link of its own.)
func briefWhere(in MeetingBriefInput, row SavedMessageRow, now time.Time) string {
	parts := []string{}
	if row.Channel != "" {
		channel := row.Channel
		if row.InChannel {
			channel = "#" + strings.TrimPrefix(channel, "#")
		}
		parts = append(parts, briefLine(channel, 80))
	}
	if row.Author != "" {
		parts = append(parts, briefLine(row.Author, 80))
	}
	if !row.SentAt.IsZero() {
		parts = append(parts, SavedTimeLabel(in.Locale, row.SentAt, now))
	}
	link := strings.TrimRight(in.Origin, "/") + ChannelReferenceURL(row.ConversationID)
	if len(parts) == 0 {
		return link
	}
	return strings.Join(parts, ", ") + " - " + link
}

// MeetingBriefProblem is something in a brief that cannot go to the chosen
// conversation as it stands.
type MeetingBriefProblem struct {
	Kind string // source or note
	Row  SavedMessageRow
	N    int
}

// MeetingBriefShareCheck reads the brief as the person has edited it and says
// what cannot be shared with the conversation they chose. A cited message from
// any other conversation may not be readable by that audience, so it must be
// removed; the person's private notes go out only when they say so. Lines the
// person deleted are not cited and cause nothing. gone, when given, says per
// message that the person can no longer open it at all.
func MeetingBriefShareCheck(locale, text string, included []SavedMessageRow, gone []bool, target string, shareNotes bool) []MeetingBriefProblem {
	// A label cites a message whichever language the brief was written in, so
	// switching language between making and sharing the brief hides nothing.
	cited := func(label string) map[int]bool {
		out := map[int]bool{}
		pattern := regexp.MustCompile(`(?m)^[ 	]*` + regexp.QuoteMeta(label) + `[ 	]*\[([^\]]+)\]:`)
		for _, match := range pattern.FindAllStringSubmatch(text, -1) {
			if n := briefParseNumber(match[1]); n >= 1 && n <= len(included) {
				out[n] = true
			}
		}
		return out
	}
	sourceCited, noteCited := map[int]bool{}, map[int]bool{}
	for _, l := range []string{"en-US", "de-DE", "ar"} {
		for n := range cited(briefSourceLabel(l)) {
			sourceCited[n] = true
		}
		for n := range cited(briefNoteLabel(l)) {
			noteCited[n] = true
		}
	}
	lines := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		lines[strings.TrimSpace(line)] = true
	}
	var problems []MeetingBriefProblem
	for i, row := range included {
		n := i + 1
		// Taking out a Source line does not take out the message's words: the
		// quoted text itself counts as a citation too.
		excerpt := MeetingBriefExcerpt(row.Body)
		quoted := excerpt != "" && strings.Contains(text, "“"+excerpt+"”")
		if sourceCited[n] || quoted {
			if i < len(gone) && gone[i] {
				problems = append(problems, MeetingBriefProblem{Kind: "gone", Row: row, N: n})
			} else if row.ConversationID != target {
				problems = append(problems, MeetingBriefProblem{Kind: "source", Row: row, N: n})
			}
		}
		if !shareNotes {
			note := briefLine(row.Note, 400)
			if noteCited[n] || (note != "" && lines[note]) {
				problems = append(problems, MeetingBriefProblem{Kind: "note", Row: row, N: n})
			}
		}
	}
	return problems
}

func briefParseNumber(text string) int {
	n := 0
	for _, r := range strings.TrimSpace(text) {
		switch {
		case r >= '0' && r <= '9':
			n = n*10 + int(r-'0')
		case r >= '٠' && r <= '٩':
			n = n*10 + int(r-'٠')
		default:
			return 0
		}
		if n > 1000 {
			return 0
		}
	}
	return n
}

var chatsave3958Table = map[string][3]string{
	"br_select":         {"Select messages", "Nachrichten auswählen", "تحديد الرسائل"},
	"br_select_done":    {"Stop selecting", "Auswahl beenden", "إنهاء التحديد"},
	"br_selected":       {"{n} selected", "{n} ausgewählt", "{n} محددة"},
	"br_prepare":        {"Prepare a meeting brief", "Besprechungsvorbereitung erstellen", "إعداد ملخص اجتماع"},
	"br_pick_hint":      {"Select the saved messages to bring to the meeting.", "Wählen Sie die gespeicherten Nachrichten für die Besprechung aus.", "حدد الرسائل المحفوظة التي تريد إحضارها إلى الاجتماع."},
	"br_pick_one":       {"Select message from {who}", "Nachricht von {who} auswählen", "تحديد الرسالة من {who}"},
	"br_pick_gone":      {"Select unavailable saved message", "Nicht verfügbare gespeicherte Nachricht auswählen", "تحديد رسالة محفوظة غير متاحة"},
	"br_too_many":       {"A brief can use up to 25 messages. Deselect some to continue.", "Eine Vorbereitung kann bis zu 25 Nachrichten verwenden. Wählen Sie einige ab.", "يمكن أن يستخدم الملخص حتى 25 رسالة. ألغِ تحديد بعضها للمتابعة."},
	"br_page_title":     {"Meeting brief", "Besprechungsvorbereitung", "ملخص الاجتماع"},
	"br_back":           {"Back to Saved", "Zurück zu Gespeichert", "العودة إلى المحفوظات"},
	"br_private":        {"This brief is private. Nothing is posted, sent or scheduled unless you choose to share it.", "Diese Vorbereitung ist privat. Nichts wird veröffentlicht, gesendet oder terminiert, es sei denn, Sie teilen sie.", "هذا الملخص خاص. لا يُنشر أو يُرسل أو يُجدول شيء ما لم تختر مشاركته."},
	"br_purpose_label":  {"Meeting purpose (optional)", "Zweck der Besprechung (optional)", "غرض الاجتماع (اختياري)"},
	"br_when_label":     {"Proposed time (optional)", "Vorgeschlagene Zeit (optional)", "الوقت المقترح (اختياري)"},
	"br_when_hint":      {"Only written into the brief. No meeting is scheduled and nobody is invited.", "Wird nur in die Vorbereitung geschrieben. Es wird kein Termin angelegt und niemand eingeladen.", "يُكتب في الملخص فقط. لا يُجدول اجتماع ولا تُرسل دعوات."},
	"br_notes_label":    {"Include my private notes in the brief", "Meine privaten Notizen in die Vorbereitung aufnehmen", "تضمين ملاحظاتي الخاصة في الملخص"},
	"br_notes_hint":     {"Notes stay in your private brief. They are shared only if you choose to.", "Notizen bleiben in Ihrer privaten Vorbereitung. Sie werden nur geteilt, wenn Sie das wählen.", "تبقى الملاحظات في ملخصك الخاص. لا تُشارك إلا إذا اخترت ذلك."},
	"br_sources":        {"Messages that will be used", "Nachrichten, die verwendet werden", "الرسائل التي ستُستخدم"},
	"br_left":           {"Left out", "Nicht berücksichtigt", "مستبعدة"},
	"br_left_hint":      {"These cannot be used. Their text is not read.", "Diese können nicht verwendet werden. Ihr Text wird nicht gelesen.", "لا يمكن استخدام هذه الرسائل. لا يُقرأ نصها."},
	"br_why_missing":    {"This message is no longer in your Saved list.", "Diese Nachricht ist nicht mehr in Ihrer Gespeichert-Liste.", "لم تعد هذه الرسالة في قائمة المحفوظات."},
	"br_checking":       {"Checking that you can still open your saved messages…", "Es wird geprüft, ob Sie Ihre gespeicherten Nachrichten noch öffnen können …", "جارٍ التحقق من إمكانية فتح رسائلك المحفوظة…"},
	"br_none_usable":    {"None of the selected messages can be used. Go back and choose others.", "Keine der ausgewählten Nachrichten kann verwendet werden. Gehen Sie zurück und wählen Sie andere.", "لا يمكن استخدام أي من الرسائل المحددة. ارجع واختر غيرها."},
	"br_generate":       {"Prepare brief", "Vorbereitung erstellen", "إعداد الملخص"},
	"br_open_source":    {"Open the original message", "Originalnachricht öffnen", "فتح الرسالة الأصلية"},
	"br_open_conv":      {"Open the conversation", "Unterhaltung öffnen", "فتح المحادثة"},
	"br_restart":        {"Change setup and start again", "Einstellungen ändern und neu beginnen", "تغيير الإعدادات والبدء من جديد"},
	"br_has_note":       {"Has your private note", "Hat Ihre private Notiz", "لديها ملاحظتك الخاصة"},
	"br_error":          {"We could not prepare the brief right now. Your selection is kept. You can open the original messages below or try again.", "Die Vorbereitung ist gerade nicht möglich. Ihre Auswahl bleibt erhalten. Sie können die Originalnachrichten unten öffnen oder es erneut versuchen.", "تعذّر إعداد الملخص الآن. تم الاحتفاظ بتحديدك. يمكنك فتح الرسائل الأصلية أدناه أو المحاولة مرة أخرى."},
	"br_retry":          {"Try again", "Erneut versuchen", "إعادة المحاولة"},
	"br_agenda":         {"Agenda (you can edit it)", "Tagesordnung (bearbeitbar)", "جدول الأعمال (يمكنك تعديله)"},
	"br_agenda_hint":    {"Owners and dates are left as “not set” for you to fill in. Check each item against its source.", "Verantwortliche und Termine bleiben „nicht festgelegt“ und können von Ihnen ergänzt werden. Prüfen Sie jeden Punkt anhand seiner Quelle.", "تُترك الجهات المسؤولة والتواريخ «غير محددة» لتكملها أنت. تحقق من كل بند مقابل مصدره."},
	"br_copy":           {"Copy brief", "Vorbereitung kopieren", "نسخ الملخص"},
	"br_copied":         {"Copied. Only you have seen it so far.", "Kopiert. Bisher haben nur Sie sie gesehen.", "تم النسخ. لم يطّلع عليه سواك حتى الآن."},
	"br_copy_failed":    {"Could not copy. Select the text and copy it yourself.", "Kopieren nicht möglich. Markieren Sie den Text und kopieren Sie ihn selbst.", "تعذّر النسخ. حدد النص وانسخه بنفسك."},
	"br_share":          {"Share with a conversation", "In einer Unterhaltung teilen", "المشاركة في محادثة"},
	"br_share_pick":     {"Prepare a draft in", "Entwurf vorbereiten in", "إعداد مسودة في"},
	"br_share_choose":   {"Choose a conversation", "Unterhaltung wählen", "اختر محادثة"},
	"br_share_hint":     {"The text goes into that conversation's message box for you to review. It is not sent.", "Der Text landet im Nachrichtenfeld der Unterhaltung zur Prüfung. Er wird nicht gesendet.", "ينتقل النص إلى مربع رسالة تلك المحادثة لتراجعه. لا يُرسل."},
	"br_share_notes":    {"Share my private notes too", "Meine privaten Notizen ebenfalls teilen", "مشاركة ملاحظاتي الخاصة أيضًا"},
	"br_share_button":   {"Put in a draft", "In Entwurf übernehmen", "وضع في مسودة"},
	"br_share_none":     {"Choose a conversation first.", "Wählen Sie zuerst eine Unterhaltung.", "اختر محادثة أولًا."},
	"br_problem_head":   {"This cannot be shared yet:", "Das lässt sich noch nicht teilen:", "لا يمكن مشاركة هذا بعد:"},
	"br_problem_source": {"Item {n} cites a message from another conversation, which the people here may not be able to open. Remove its Source line and the item, or choose its conversation.", "Punkt {n} verweist auf eine Nachricht aus einer anderen Unterhaltung, die die Beteiligten hier eventuell nicht öffnen können. Entfernen Sie die Quellzeile und den Punkt oder wählen Sie die passende Unterhaltung.", "يشير البند {n} إلى رسالة من محادثة أخرى قد لا يتمكن المشاركون هنا من فتحها. احذف سطر المصدر والبند أو اختر محادثتها."},
	"br_problem_gone":   {"Item {n} cites a message you can no longer open. Remove the item before sharing.", "Punkt {n} verweist auf eine Nachricht, die Sie nicht mehr öffnen können. Entfernen Sie den Punkt vor dem Teilen.", "يشير البند {n} إلى رسالة لم يعد بإمكانك فتحها. احذف البند قبل المشاركة."},
	"br_choose_first":   {"Choose a conversation to prepare the draft in.", "Wählen Sie eine Unterhaltung für den Entwurf.", "اختر محادثة لإعداد المسودة فيها."},
	"br_problem_note":   {"Item {n} includes your private note. Remove the note line or tick “Share my private notes too”.", "Punkt {n} enthält Ihre private Notiz. Entfernen Sie die Notizzeile oder wählen Sie „Meine privaten Notizen ebenfalls teilen“.", "يتضمن البند {n} ملاحظتك الخاصة. احذف سطر الملاحظة أو حدد «مشاركة ملاحظاتي الخاصة أيضًا»."},
	"br_drafted":        {"The brief is in the message box of {name}. Review it and send it yourself.", "Die Vorbereitung steht im Nachrichtenfeld von {name}. Prüfen Sie sie und senden Sie sie selbst.", "الملخص في مربع رسالة {name}. راجعه وأرسله بنفسك."},
	"br_title":          {"Meeting brief", "Besprechungsvorbereitung", "ملخص الاجتماع"},
	"br_purpose":        {"Purpose: {text}", "Zweck: {text}", "الغرض: {text}"},
	"br_purpose_none":   {"Purpose: not stated", "Zweck: nicht angegeben", "الغرض: غير محدد"},
	"br_time":           {"Proposed time: {text} (not scheduled)", "Vorgeschlagene Zeit: {text} (nicht terminiert)", "الوقت المقترح: {text} (غير مجدول)"},
	"br_time_none":      {"Proposed time: not set (nothing is scheduled)", "Vorgeschlagene Zeit: nicht festgelegt (nichts ist terminiert)", "الوقت المقترح: غير محدد (لا شيء مجدول)"},
	"br_prepared":       {"Prepared from {n} saved messages. This is a draft: nothing has been sent or scheduled.", "Erstellt aus {n} gespeicherten Nachrichten. Dies ist ein Entwurf: Nichts wurde gesendet oder terminiert.", "أُعدّ من {n} رسالة محفوظة. هذه مسودة: لم يُرسل أو يُجدول شيء."},
	"br_sec_decisions":  {"Decisions needed", "Erforderliche Entscheidungen", "قرارات مطلوبة"},
	"br_sec_questions":  {"Questions to resolve", "Offene Fragen", "أسئلة تحتاج إلى حل"},
	"br_sec_context":    {"Relevant context", "Relevanter Kontext", "سياق ذو صلة"},
	"br_tag_decision":   {"Decision needed", "Entscheidung nötig", "قرار مطلوب"},
	"br_tag_confirmed":  {"Confirmed decision", "Bestätigte Entscheidung", "قرار مؤكد"},
	"br_tag_question":   {"Open question", "Offene Frage", "سؤال مفتوح"},
	"br_tag_context":    {"Context", "Kontext", "سياق"},
	"br_owner_date":     {"Owner: not set. Date: not set.", "Verantwortlich: nicht festgelegt. Termin: nicht festgelegt.", "المسؤول: غير محدد. التاريخ: غير محدد."},
	"br_source":         {"Source", "Quelle", "المصدر"},
	"br_note":           {"Private note", "Private Notiz", "ملاحظة خاصة"},
}

func init() {
	for key, value := range chatsave3958Table {
		chatsave002Table[key] = value
	}
}
