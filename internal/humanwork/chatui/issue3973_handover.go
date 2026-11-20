package chatui

import (
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3973. Prepare a work handover from project-task cards.
//
// The person asks for it from a conversation that holds project-task cards,
// ticks the tasks to hand over, names a colleague and an optional window, and
// gets a private, editable brief. The browser re-reads every ticked task from
// Projects as the author (so a card that has gone stale is not trusted), asks
// Projects whether the colleague is a member of each task's project, and
// builds the brief by plain rules, not by a model. The brief keeps facts read
// from Projects apart from suggestions to fill in, never states why anyone is
// away, never reassigns anything, and leaves out every task the colleague
// cannot open or whose access could not be confirmed (those are listed
// without their content). Nothing is sent: the brief goes into the message
// box for the author to review and send.

// HandoverMaxTasks is how many tasks one handover takes.
const HandoverMaxTasks = 25

// Outcomes of one task in a handover.
const (
	HandoverIncluded    = "included"
	HandoverWithheld    = "withheld"    // the colleague cannot open it, or that could not be confirmed
	HandoverUnavailable = "unavailable" // the author cannot read it from Projects now
)

// HandoverKeySeparator joins a project id and a task id into the one string a
// checkbox carries. Ids cannot contain it.
const HandoverKeySeparator = "|"

// HandoverKey is the string identifying one task in the panel.
func HandoverKey(projectID, taskID string) string { return projectID + HandoverKeySeparator + taskID }

// HandoverSplitKey is the inverse of HandoverKey.
func HandoverSplitKey(key string) (projectID, taskID string, ok bool) {
	projectID, taskID, ok = strings.Cut(key, HandoverKeySeparator)
	if !ok || ProjectTaskPreviewKey(projectID, taskID) == "" || taskID == "" {
		return "", "", false
	}
	return projectID, taskID, true
}

// HandoverCandidate is a project task the conversation shows a card for.
type HandoverCandidate struct {
	Key, ProjectID, TaskID, Title, TaskKey string
	// Readable is false while the card is loading or the person cannot open
	// the task; such a task is listed without a name and cannot be ticked.
	Readable bool
}

// HandoverCandidates are the distinct project tasks in the messages on screen,
// in the order they appear, up to HandoverMaxTasks.
func HandoverCandidates(m Model) []HandoverCandidate {
	var out []HandoverCandidate
	seen := map[string]bool{}
	for _, msg := range m.Messages {
		for _, ref := range ProjectTaskReferencesFor(msg.Body, m.EmbedOrigin, msg.WorkReferences) {
			if ref.Unresolved || ref.ProjectID == "" || ref.TaskID == "" {
				continue
			}
			key := HandoverKey(ref.ProjectID, ref.TaskID)
			previewKey := ProjectTaskPreviewKey(ref.ProjectID, ref.TaskID)
			if previewKey == "" || seen[key] {
				continue
			}
			seen[key] = true
			candidate := HandoverCandidate{Key: key, ProjectID: ref.ProjectID, TaskID: ref.TaskID}
			if p, ok := m.ProjectTaskPreviews[previewKey]; ok && p.State == "ready" && p.Readable && strings.TrimSpace(p.Title) != "" {
				candidate.Readable, candidate.Title, candidate.TaskKey = true, briefLine(p.Title, 120), p.Key
			}
			out = append(out, candidate)
			if len(out) >= HandoverMaxTasks {
				return out
			}
		}
	}
	return out
}

// HandoverRecipients are the colleagues of the open conversation the handover
// can be addressed to: people (not agents) from the viewer's own organisation,
// other than the viewer.
func HandoverRecipients(m Model) []Member {
	var out []Member
	for _, member := range m.Members {
		if member.ID == "" || member.Agent || member.ID == m.CurrentUser || strings.TrimSpace(member.Name) == "" {
			continue
		}
		if member.HomeTenantID != "" && member.HomeTenantID != m.CurrentTenantID {
			continue
		}
		out = append(out, member)
	}
	return out
}

// HandoverCommand is what the panel asks the browser to do.
type HandoverCommand struct {
	// Kind is open, close, toggle, prepare, back or drafted.
	Kind string
	// Key is the task a toggle is about.
	Key string
	// Recipient, From and To are the selection form as the person left it;
	// Text is the brief as the person has edited it.
	Recipient, From, To, Text string
}

// HandoverRow is one selected task as the draft stage lists it.
type HandoverRow struct {
	Key, Title string
	Outcome    string // included, withheld or unavailable
	// Reason says why a task is not in the brief: noaccess, missing, archived,
	// failed, recipient_no or recipient_unknown.
	Reason string
	// Changed is true when Projects now differs from the card in the chat.
	Changed bool
}

// HandoverView is the panel's state in one conversation.
type HandoverView struct {
	// ConversationID is the conversation the state belongs to.
	ConversationID string
	Open           bool
	// Stage is pick, preparing, draft or error.
	Stage    string
	Selected map[string]bool
	// Recipient is a member id; From and To are YYYY-MM-DD or empty.
	Recipient, From, To string
	Text                string
	Rows                []HandoverRow
	// Status names the last notice: window, none, recipient, drafted.
	Status string
	// RecipientName is who the brief was prepared for.
	RecipientName string
}

// HandoverItem is one selected task with what the browser learned about it.
type HandoverItem struct {
	ProjectID, TaskID string
	// Fresh is the task as just read from Projects by the author; Shown is the
	// preview the chat card held before.
	Fresh, Shown ProjectTaskPreview
	// Recipient is whether the colleague belongs to the task's project: "yes",
	// "no" or "unknown".
	Recipient string
}

// HandoverInput is what the brief is built from.
type HandoverInput struct {
	Locale, Origin, RecipientName, From, To string
	Now                                     time.Time
	Items                                   []HandoverItem
}

// HandoverResult is the brief and how each task fared.
type HandoverResult struct {
	Text string
	Rows []HandoverRow
}

func handoverReadable(p ProjectTaskPreview) bool {
	return p.State == "ready" && p.Readable && strings.TrimSpace(p.Title) != "" && !p.IsBoard()
}

// handoverChanges lists, in words, how Projects now differs from the card the
// conversation showed. A card that was never ready shows nothing to compare.
func handoverChanges(locale string, shown, fresh ProjectTaskPreview) []string {
	if !handoverReadable(shown) {
		return nil
	}
	var out []string
	was := func(value string) string {
		if value = briefLine(value, 120); value != "" {
			return value
		}
		return issue3973T(locale, "not_set_word")
	}
	if strings.TrimSpace(shown.Title) != strings.TrimSpace(fresh.Title) {
		out = append(out, issue3973T(locale, "chg_title", "was", was(shown.Title)))
	}
	if shown.Status != fresh.Status {
		out = append(out, issue3973T(locale, "chg_status", "was", was(shown.Status)))
	}
	if shown.Assignee != fresh.Assignee {
		out = append(out, issue3973T(locale, "chg_owner", "was", was(shown.Assignee)))
	}
	if shown.Due != fresh.Due {
		out = append(out, issue3973T(locale, "chg_due", "was", was(handoverDue(locale, shown.Due))))
	}
	return out
}

func handoverDue(locale, due string) string {
	if day, err := time.Parse("2006-01-02", due); err == nil {
		return formatShortDate(locale, day)
	}
	return ""
}

func handoverReason(item HandoverItem) (outcome, reason string) {
	if !handoverReadable(item.Fresh) {
		switch item.Fresh.State {
		case "missing", "archived", "failed":
			return HandoverUnavailable, item.Fresh.State
		}
		return HandoverUnavailable, "noaccess"
	}
	switch item.Recipient {
	case "yes":
		return HandoverIncluded, ""
	case "no":
		return HandoverWithheld, "recipient_no"
	}
	return HandoverWithheld, "recipient_unknown"
}

// BuildHandover builds the brief. A task the colleague cannot open, or whose
// access could not be confirmed, or that the author cannot read now, is named
// only by its position in the "not included" list; its title, status, owner
// and link are never written into the text.
func BuildHandover(in HandoverInput) HandoverResult {
	loc := in.Locale
	result := HandoverResult{}
	var included, left []int
	for i, item := range in.Items {
		outcome, reason := handoverReason(item)
		row := HandoverRow{Key: HandoverKey(item.ProjectID, item.TaskID), Outcome: outcome, Reason: reason}
		if handoverReadable(item.Fresh) {
			row.Title = briefLine(item.Fresh.Title, 120)
		}
		if outcome == HandoverIncluded {
			row.Changed = len(handoverChanges(loc, item.Shown, item.Fresh)) > 0
			included = append(included, i)
		} else {
			left = append(left, i)
		}
		result.Rows = append(result.Rows, row)
	}

	var b strings.Builder
	b.WriteString(issue3973T(loc, "title") + "\n")
	if name := briefLine(in.RecipientName, 80); name != "" {
		b.WriteString(issue3973T(loc, "for", "name", name) + "\n")
	}
	b.WriteString(issue3973T(loc, "window", "text", handoverWindowText(loc, in.From, in.To)) + "\n")
	b.WriteString(issue3973T(loc, "prepared", "n", chatsave002Num(loc, len(included))) + "\n")
	b.WriteString(issue3973T(loc, "no_reassign") + "\n")

	for n, i := range included {
		item := in.Items[i]
		p := item.Fresh
		head := briefLine(p.Title, 160)
		if key := briefLine(p.Key, 40); key != "" {
			head += " (" + key + ")"
		}
		b.WriteString("\n" + chatsave002Num(loc, n+1) + ". " + head + "\n")
		b.WriteString("   " + issue3973T(loc, "facts") + "\n")
		b.WriteString("   - " + issue3973T(loc, "status", "text", handoverOr(loc, briefLine(p.Status, 80))) + "\n")
		b.WriteString("   - " + issue3973T(loc, "owner", "text", handoverOr(loc, briefLine(p.Assignee, 80))) + "\n")
		b.WriteString("   - " + issue3973T(loc, "due", "text", handoverOr(loc, handoverDue(loc, p.Due))) + "\n")
		b.WriteString("   - " + issue3973T(loc, "link", "text", strings.TrimRight(in.Origin, "/")+ProjectTaskReferenceURL(item.ProjectID, item.TaskID)) + "\n")
		for _, change := range handoverChanges(loc, item.Shown, p) {
			b.WriteString("   - " + change + "\n")
		}
		b.WriteString("   " + issue3973T(loc, "suggestions") + "\n")
		b.WriteString("   - " + issue3973T(loc, "next_step") + "\n")
		b.WriteString("   - " + issue3973T(loc, "questions") + "\n")
	}
	if len(left) > 0 {
		b.WriteString("\n" + issue3973T(loc, "left_head", "n", chatsave002Num(loc, len(left))) + "\n")
		for n, i := range left {
			b.WriteString("- " + issue3973T(loc, "left_task", "n", chatsave002Num(loc, n+1)) + " " + HandoverReasonText(loc, result.Rows[i].Reason) + "\n")
		}
	}
	result.Text = b.String()
	return result
}

func handoverOr(locale, text string) string {
	if text == "" {
		return issue3973T(locale, "not_set_word")
	}
	return text
}

func handoverWindowText(locale, from, to string) string {
	f, t := handoverDue(locale, from), handoverDue(locale, to)
	switch {
	case f != "" && t != "":
		return issue3973T(locale, "window_range", "from", f, "to", t)
	case f != "":
		return issue3973T(locale, "window_from", "from", f)
	case t != "":
		return issue3973T(locale, "window_to", "to", t)
	}
	return issue3973T(locale, "not_set_word")
}

// HandoverReasonText is the plain sentence for a task left out of the brief.
func HandoverReasonText(locale, reason string) string {
	switch reason {
	case "missing":
		return issue3973T(locale, "why_missing")
	case "archived":
		return issue3973T(locale, "why_archived")
	case "failed":
		return issue3973T(locale, "why_failed")
	case "recipient_no":
		return issue3973T(locale, "why_recipient_no")
	case "recipient_unknown":
		return issue3973T(locale, "why_recipient_unknown")
	}
	return issue3973T(locale, "why_noaccess")
}

// HandoverWindowValid says whether the two optional days make a window: each
// is empty or a day, and the end is not before the start.
func HandoverWindowValid(from, to string) bool {
	var start, end time.Time
	var err error
	if from != "" {
		if start, err = time.Parse("2006-01-02", from); err != nil {
			return false
		}
	}
	if to != "" {
		if end, err = time.Parse("2006-01-02", to); err != nil {
			return false
		}
	}
	return from == "" || to == "" || !end.Before(start)
}

// HandoverDraftText is the message box after the brief is put in it: whatever
// was already typed stays, and the brief follows.
func HandoverDraftText(existing, brief string) string {
	brief = strings.TrimRight(brief, "\n\r\t ")
	if strings.TrimSpace(existing) == "" {
		return brief
	}
	return strings.TrimRight(existing, "\n\r\t ") + "\n\n" + brief
}

var issue3973Copy = map[string][3]string{
	"open":        {"Prepare a work handover", "Übergabe vorbereiten", "إعداد تسليم عمل"},
	"heading":     {"Work handover", "Arbeitsübergabe", "تسليم العمل"},
	"close":       {"Close", "Schließen", "إغلاق"},
	"private":     {"The handover is a private draft. Nothing is sent, shared or reassigned unless you choose to.", "Die Übergabe ist ein privater Entwurf. Nichts wird gesendet, geteilt oder neu zugewiesen, es sei denn, Sie entscheiden das.", "التسليم مسودة خاصة. لا يُرسل أو يُشارك أو يُعاد إسناد أي شيء ما لم تختر ذلك."},
	"tasks":       {"Tasks to hand over", "Aufgaben für die Übergabe", "المهام المراد تسليمها"},
	"task_closed": {"A task you cannot open", "Eine Aufgabe, die Sie nicht öffnen können", "مهمة لا يمكنك فتحها"},
	"recipient":   {"Hand over to", "Übergabe an", "التسليم إلى"},
	"choose":      {"Choose a colleague", "Kollegin oder Kollegen wählen", "اختر زميلًا"},
	"no_people":   {"There is nobody in this conversation to hand over to.", "In dieser Unterhaltung gibt es niemanden für die Übergabe.", "لا يوجد في هذه المحادثة من يمكن التسليم إليه."},
	"from":        {"From (optional)", "Von (optional)", "من (اختياري)"},
	"to":          {"Until (optional)", "Bis (optional)", "حتى (اختياري)"},
	"window_hint": {"Only written into the handover as dates. Nothing is recorded about why.", "Wird nur als Datum in die Übergabe geschrieben. Ein Grund wird nicht festgehalten.", "تُكتب كتواريخ فقط في التسليم. لا يُسجَّل أي سبب."},
	"prepare":     {"Prepare handover", "Übergabe erstellen", "إعداد التسليم"},
	"reading":     {"Reading the latest from Projects…", "Der aktuelle Stand wird aus Projekten gelesen …", "جارٍ قراءة آخر المستجدات من المشاريع…"},
	"error":       {"The handover could not be prepared right now. Your selection is kept. Try again.", "Die Übergabe konnte gerade nicht erstellt werden. Ihre Auswahl bleibt erhalten. Versuchen Sie es erneut.", "تعذّر إعداد التسليم الآن. تم الاحتفاظ بتحديدك. حاول مرة أخرى."},
	"retry":       {"Try again", "Erneut versuchen", "إعادة المحاولة"},
	"s_none":      {"Tick at least one task.", "Wählen Sie mindestens eine Aufgabe.", "حدد مهمة واحدة على الأقل."},
	"s_recipient": {"Choose who to hand over to.", "Wählen Sie, an wen übergeben wird.", "اختر من سيُسلَّم إليه."},
	"s_window":    {"The end day is before the start day.", "Der letzte Tag liegt vor dem ersten Tag.", "يوم النهاية قبل يوم البداية."},
	"s_drafted":   {"The handover is in your message box. Review it and send it yourself.", "Die Übergabe steht in Ihrem Nachrichtenfeld. Prüfen Sie sie und senden Sie sie selbst.", "التسليم في مربع رسالتك. راجعه وأرسله بنفسك."},
	"s_ready":     {"Handover prepared from the latest in Projects. Check it and edit it.", "Die Übergabe wurde mit dem aktuellen Stand aus Projekten erstellt. Prüfen und bearbeiten Sie sie.", "أُعدّ التسليم من آخر المستجدات في المشاريع. راجعه وعدّله."},
	"s_empty":     {"None of the tasks can go in the handover. Change the selection or ask for access.", "Keine der Aufgaben kann in die Übergabe aufgenommen werden. Ändern Sie die Auswahl oder bitten Sie um Zugriff.", "لا يمكن إدراج أي من المهام في التسليم. غيّر التحديد أو اطلب الوصول."},
	"rows":        {"How each task fared", "Was aus jeder Aufgabe wurde", "حال كل مهمة"},
	"row_in":      {"In the handover", "In der Übergabe", "ضمن التسليم"},
	"row_changed": {"Changed since the card in this chat", "Seit der Karte in diesem Chat geändert", "تغيّرت منذ البطاقة في هذه المحادثة"},
	"row_out":     {"Not included", "Nicht enthalten", "غير مضمّنة"},
	"text_label":  {"Handover (you can edit it)", "Übergabe (bearbeitbar)", "التسليم (يمكنك تعديله)"},
	"text_hint":   {"Facts come from Projects just now. Suggestions are blanks for you to fill in. Everyone in this conversation will see it once you send it; it was checked against the colleague you chose only.", "Die Fakten stammen soeben aus Projekten. Vorschläge sind Lücken, die Sie ausfüllen. Alle in dieser Unterhaltung sehen die Übergabe, sobald Sie sie senden; geprüft wurde nur für die gewählte Person.", "الحقائق من المشاريع الآن. الاقتراحات فراغات تملؤها أنت. سيراها الجميع في هذه المحادثة عند إرسالها؛ وقد جرى التحقق من الزميل الذي اخترته فقط."},
	"put":         {"Put in message box", "In Nachrichtenfeld übernehmen", "وضع في مربع الرسالة"},
	"back":        {"Change selection", "Auswahl ändern", "تغيير التحديد"},
	"ownership":   {"To change who owns a task, ask through the project. This handover does not reassign anything.", "Um die Zuständigkeit für eine Aufgabe zu ändern, fragen Sie im Projekt an. Diese Übergabe weist nichts neu zu.", "لتغيير مالك مهمة، اطلب ذلك عبر المشروع. هذا التسليم لا يعيد إسناد أي شيء."},

	"title":                 {"Work handover", "Arbeitsübergabe", "تسليم العمل"},
	"for":                   {"For: {name}", "Für: {name}", "إلى: {name}"},
	"window":                {"Handover window: {text}", "Übergabezeitraum: {text}", "فترة التسليم: {text}"},
	"window_range":          {"{from} to {to}", "{from} bis {to}", "من {from} إلى {to}"},
	"window_from":           {"from {from}", "ab {from}", "من {from}"},
	"window_to":             {"until {to}", "bis {to}", "حتى {to}"},
	"prepared":              {"{n} tasks, read from Projects just now. This is a draft: nothing has been sent.", "{n} Aufgaben, soeben aus Projekten gelesen. Dies ist ein Entwurf: Nichts wurde gesendet.", "{n} مهام، قُرئت من المشاريع الآن. هذه مسودة: لم يُرسل شيء."},
	"no_reassign":           {"Nothing has been reassigned.", "Es wurde nichts neu zugewiesen.", "لم يُعاد إسناد أي شيء."},
	"facts":                 {"Facts from Projects:", "Fakten aus Projekten:", "حقائق من المشاريع:"},
	"status":                {"Status: {text}", "Status: {text}", "الحالة: {text}"},
	"owner":                 {"Owner: {text}", "Verantwortlich: {text}", "المسؤول: {text}"},
	"due":                   {"Due: {text}", "Fällig: {text}", "الاستحقاق: {text}"},
	"link":                  {"Link: {text}", "Link: {text}", "الرابط: {text}"},
	"suggestions":           {"Suggestions (check and edit):", "Vorschläge (prüfen und bearbeiten):", "اقتراحات (راجعها وعدّلها):"},
	"next_step":             {"Next step: not set. Add it here.", "Nächster Schritt: nicht festgelegt. Hier ergänzen.", "الخطوة التالية: غير محددة. أضفها هنا."},
	"questions":             {"Open questions: none known. Add any here.", "Offene Fragen: keine bekannt. Hier ergänzen.", "أسئلة مفتوحة: لا شيء معروف. أضف ما لديك هنا."},
	"not_set_word":          {"not set", "nicht festgelegt", "غير محدد"},
	"chg_title":             {"Changed since the card in this chat: the title was “{was}”.", "Seit der Karte in diesem Chat geändert: Der Titel war „{was}“.", "تغيّر منذ البطاقة في هذه المحادثة: كان العنوان «{was}»."},
	"chg_status":            {"Changed since the card in this chat: the status was “{was}”.", "Seit der Karte in diesem Chat geändert: Der Status war „{was}“.", "تغيّر منذ البطاقة في هذه المحادثة: كانت الحالة «{was}»."},
	"chg_owner":             {"Changed since the card in this chat: the owner was “{was}”.", "Seit der Karte in diesem Chat geändert: Verantwortlich war „{was}“.", "تغيّر منذ البطاقة في هذه المحادثة: كان المسؤول «{was}»."},
	"chg_due":               {"Changed since the card in this chat: the due date was “{was}”.", "Seit der Karte in diesem Chat geändert: Fällig war „{was}“.", "تغيّر منذ البطاقة في هذه المحادثة: كان الاستحقاق «{was}»."},
	"left_head":             {"Not included ({n})", "Nicht enthalten ({n})", "غير مضمّنة ({n})"},
	"left_task":             {"Task {n}:", "Aufgabe {n}:", "المهمة {n}:"},
	"why_noaccess":          {"You can no longer open it in Projects.", "Sie können sie in Projekten nicht mehr öffnen.", "لم يعد بإمكانك فتحها في المشاريع."},
	"why_missing":           {"It no longer exists.", "Sie existiert nicht mehr.", "لم تعد موجودة."},
	"why_archived":          {"It has been archived.", "Sie wurde archiviert.", "تمت أرشفتها."},
	"why_failed":            {"It could not be read right now. Try again.", "Sie konnte gerade nicht gelesen werden. Versuchen Sie es erneut.", "تعذّرت قراءتها الآن. حاول مرة أخرى."},
	"why_recipient_no":      {"The colleague cannot open it. Remove it, or ask the project owner for access.", "Die Person kann sie nicht öffnen. Entfernen Sie sie oder bitten Sie die Projektverantwortlichen um Zugriff.", "لا يستطيع الزميل فتحها. أزلها أو اطلب الوصول من مالك المشروع."},
	"why_recipient_unknown": {"Whether the colleague can open it could not be confirmed, so it is left out.", "Ob die Person sie öffnen kann, ließ sich nicht bestätigen. Sie wird daher weggelassen.", "تعذّر التأكد من قدرة الزميل على فتحها، لذا استُبعدت."},
}

func issue3973T(locale, key string, pairs ...string) string {
	c := issue3973Copy[key]
	idx := chatbug039LocaleIndex(locale)
	return chatcmd002Fill(chatbug039Text("chat.issue3973."+key, c[idx], c[0]), pairs...)
}

// issue3973Button is the header's entry. It is drawn only when the open
// conversation shows a project task, or while the panel is open.
func issue3973Button(m Model) ui.Node {
	open := m.Handover.Open && m.Handover.ConversationID == m.SelectedID
	if m.SelectedID == "" || (!open && len(HandoverCandidates(m)) == 0) {
		return nil
	}
	label := issue3973T(m.Locale, "open")
	return html.Button(html.Props{Class: "icon-button chatux001-handover", Type: "button", Title: label, Disabled: m.Callbacks.Handover == nil,
		Data: map[string]string{"action": "handover-toggle"},
		Aria: map[string]string{"label": label, "expanded": boolString(open), "controls": "chat-handover-panel"}}, icon("checklist"))
}

func issue3973StatusText(m Model, v HandoverView) (text, role string) {
	loc := m.Locale
	switch {
	case v.Stage == "preparing":
		return issue3973T(loc, "reading"), "status"
	case v.Stage == "error":
		return issue3973T(loc, "error"), "alert"
	}
	switch v.Status {
	case "none":
		return issue3973T(loc, "s_none"), "alert"
	case "recipient":
		return issue3973T(loc, "s_recipient"), "alert"
	case "window":
		return issue3973T(loc, "s_window"), "alert"
	case "drafted":
		return issue3973T(loc, "s_drafted"), "status"
	}
	if v.Stage == "draft" {
		for _, row := range v.Rows {
			if row.Outcome == HandoverIncluded {
				return issue3973T(loc, "s_ready"), "status"
			}
		}
		return issue3973T(loc, "s_empty"), "status"
	}
	return "", "status"
}

// issue3973Panel is the handover panel under the header; nil while closed.
func issue3973Panel(m Model) ui.Node {
	v := m.Handover
	if !v.Open || v.ConversationID != m.SelectedID || m.SelectedID == "" {
		return nil
	}
	loc := m.Locale
	busy := v.Stage == "preparing"
	status, role := issue3973StatusText(m, v)
	live := "polite"
	if role == "alert" {
		live = "assertive"
	}
	nodes := []ui.Node{
		html.Div(html.Props{Class: "handover-top"},
			html.H2(html.Props{ID: "chat-handover-heading", Class: "handover-heading", TabIndex: -1, Text: issue3973T(loc, "heading")}),
			html.Button(html.Props{Class: "icon-button", Type: "button", Data: map[string]string{"action": "handover-close"}, Title: issue3973T(loc, "close"),
				Aria: map[string]string{"label": issue3973T(loc, "close")}}, icon("close"))),
		html.P(html.Props{Class: "handover-private", Text: issue3973T(loc, "private")}),
		html.P(html.Props{ID: "chat-handover-status", Class: "handover-status", Role: role, Aria: map[string]string{"live": live}, Text: status}),
	}
	if v.Stage == "error" {
		nodes = append(nodes, html.Button(html.Props{Class: "button secondary small", Type: "button", Data: map[string]string{"action": "handover-prepare"}, Text: issue3973T(loc, "retry")}))
	}
	if v.Stage == "draft" {
		nodes = append(nodes, issue3973Draft(m, v)...)
	} else {
		nodes = append(nodes, issue3973Pick(m, v, busy)...)
	}
	return html.Div(html.Props{ID: "chat-handover-panel", Class: "handover-panel", Role: "region", Dir: direction(loc),
		Data: map[string]string{"handover-stage": v.Stage}, Aria: map[string]string{"labelledby": "chat-handover-heading", "busy": boolString(busy)}}, nodes...)
}

func issue3973Pick(m Model, v HandoverView, busy bool) []ui.Node {
	loc := m.Locale
	boxes := []ui.Node{}
	for _, c := range HandoverCandidates(m) {
		id := "chat-handover-task-" + c.TaskID
		label := issue3973T(loc, "task_closed")
		if c.Readable {
			label = c.Title
			if c.TaskKey != "" {
				label = c.TaskKey + " " + label
			}
		}
		boxes = append(boxes, html.Li(html.Props{Class: "handover-task"},
			html.Input(html.Props{ID: id, Class: "handover-box", Type: "checkbox", Checked: v.Selected[c.Key] && c.Readable, Disabled: !c.Readable || busy,
				Data: map[string]string{"action": "handover-task", "id": c.Key}}),
			html.Label(html.Props{For: id, Dir: "auto", Text: label})))
	}
	options := []ui.Node{html.Option(html.Props{Raw: map[string]any{"value": ""}, Text: issue3973T(loc, "choose")})}
	people := HandoverRecipients(m)
	for _, member := range people {
		options = append(options, html.Option(html.Props{Value: member.ID, Text: member.Name, Selected: member.ID == v.Recipient}))
	}
	recipient := []ui.Node{
		html.Label(html.Props{For: "chat-handover-recipient", Class: "handover-label", Text: issue3973T(loc, "recipient")}),
		html.Select(html.Props{ID: "chat-handover-recipient", Class: "handover-select", Name: "recipient", Value: v.Recipient, Disabled: busy || len(people) == 0}, options...),
	}
	if len(people) == 0 {
		recipient = append(recipient, html.P(html.Props{Class: "handover-hint", Text: issue3973T(loc, "no_people")}))
	}
	dateField := func(id, key, value string) ui.Node {
		return html.Div(html.Props{Class: "handover-date"},
			html.Label(html.Props{For: id, Class: "handover-label", Text: issue3973T(loc, key)}),
			html.Input(html.Props{ID: id, Class: "chat-input handover-field", Type: "date", Name: key, Disabled: busy, Data: map[string]string{"chat-value": value},
				Aria: map[string]string{"describedby": "chat-handover-window-hint"}}))
	}
	return []ui.Node{
		html.Fieldset(html.Props{Class: "handover-tasks"}, html.Legend(html.Props{Class: "handover-label", Text: issue3973T(loc, "tasks")}),
			html.Ul(html.Props{Class: "handover-list"}, boxes...)),
		html.Div(html.Props{Class: "handover-recipient"}, recipient...),
		html.Div(html.Props{Class: "handover-window"}, dateField("chat-handover-from", "from", v.From), dateField("chat-handover-to", "to", v.To)),
		html.P(html.Props{ID: "chat-handover-window-hint", Class: "handover-hint", Text: issue3973T(loc, "window_hint")}),
		html.Button(html.Props{ID: "chat-handover-prepare", Class: "button small", Type: "button", Disabled: busy || m.Callbacks.Handover == nil,
			Data: map[string]string{"action": "handover-prepare"}, Text: issue3973T(loc, "prepare")}),
	}
}

func issue3973Draft(m Model, v HandoverView) []ui.Node {
	loc := m.Locale
	rows := []ui.Node{}
	for n, row := range v.Rows {
		chip, label := issue3973T(loc, "row_in"), row.Title
		switch {
		case row.Outcome != HandoverIncluded:
			chip = issue3973T(loc, "row_out") + ": " + HandoverReasonText(loc, row.Reason)
			label = issue3973T(loc, "left_task", "n", chatsave002Num(loc, n+1))
			if row.Outcome == HandoverWithheld && row.Title != "" {
				label += " " + row.Title
			}
		case row.Changed:
			chip = issue3973T(loc, "row_changed")
		}
		rows = append(rows, html.Li(html.Props{Class: "handover-row", Data: map[string]string{"outcome": row.Outcome}},
			html.Span(html.Props{Class: "handover-row-title", Dir: "auto", Text: label}),
			html.Span(html.Props{Class: "handover-row-chip", Text: chip})))
	}
	return []ui.Node{
		html.H3(html.Props{Class: "handover-subheading", Text: issue3973T(loc, "rows")}),
		html.Ul(html.Props{Class: "handover-list handover-rows"}, rows...),
		html.Label(html.Props{For: "chat-handover-text", Class: "handover-label", Text: issue3973T(loc, "text_label")}),
		html.Textarea(html.Props{ID: "chat-handover-text", Class: "handover-text", Name: "handover", Rows: 16, Dir: "auto", Text: v.Text,
			Data: map[string]string{"chat-value": v.Text}, Aria: map[string]string{"describedby": "chat-handover-text-hint"}}),
		html.P(html.Props{ID: "chat-handover-text-hint", Class: "handover-hint", Text: issue3973T(loc, "text_hint")}),
		html.P(html.Props{Class: "handover-hint", Text: issue3973T(loc, "ownership")}),
		html.Div(html.Props{Class: "handover-buttons"},
			html.Button(html.Props{ID: "chat-handover-put", Class: "button small", Type: "button", Disabled: m.Callbacks.Handover == nil,
				Data: map[string]string{"action": "handover-draft"}, Text: issue3973T(loc, "put")}),
			html.Button(html.Props{Class: "button secondary small", Type: "button", Data: map[string]string{"action": "handover-back"}, Text: issue3973T(loc, "back")})),
	}
}

// issue3973Form reads what the person has typed in the open stage.
func issue3973Form(v HandoverView, kind string) HandoverCommand {
	cmd := HandoverCommand{Kind: kind}
	if v.Stage == "draft" {
		cmd.Text = domValue("chat-handover-text")
		if cmd.Text == "" {
			cmd.Text = v.Text
		}
		return cmd
	}
	cmd.Recipient, cmd.From, cmd.To = domValue("chat-handover-recipient"), domValue("chat-handover-from"), domValue("chat-handover-to")
	return cmd
}

// issue3973Click handles the panel's buttons and reports whether the press was
// one of them. Putting the brief in the message box happens here, in the page
// that owns the composer; everything else is the browser's.
func issue3973Click(e ui.MouseEvent, m Model, local localStore, drafts *browserDrafts) bool {
	action, id, _ := eventAction(e)
	if !strings.HasPrefix(action, "handover-") || m.Callbacks.Handover == nil {
		return strings.HasPrefix(action, "handover-")
	}
	v := m.Handover
	switch action {
	case "handover-toggle":
		if v.Open && v.ConversationID == m.SelectedID {
			m.Callbacks.Handover(issue3973Form(v, "close"))
			focusHandoverTrigger()
		} else {
			m.Callbacks.Handover(HandoverCommand{Kind: "open"})
			focusHandoverTarget("chat-handover-heading")
		}
	case "handover-close":
		m.Callbacks.Handover(issue3973Form(v, "close"))
		focusHandoverTrigger()
	case "handover-task":
		cmd := issue3973Form(v, "toggle")
		cmd.Key = id
		m.Callbacks.Handover(cmd)
	case "handover-prepare":
		m.Callbacks.Handover(issue3973Form(v, "prepare"))
	case "handover-back":
		cmd := issue3973Form(v, "back")
		m.Callbacks.Handover(cmd)
		focusHandoverTarget("chat-handover-prepare")
	case "handover-draft":
		cmd := issue3973Form(v, "drafted")
		issue3973PutInDraft(m, local, drafts, cmd.Text)
		m.Callbacks.Handover(cmd)
	default:
		return false
	}
	return true
}

// issue3973PutInDraft adds the brief to the open conversation's message box
// without sending it.
func issue3973PutInDraft(m Model, local localStore, drafts *browserDrafts, brief string) {
	if strings.TrimSpace(brief) == "" || m.SelectedID == "" {
		return
	}
	existing := domDraftValue("chat-composer", m.SelectedID)
	if existing == "" {
		existing = m.Draft
	}
	text := HandoverDraftText(existing, brief)
	if drafts != nil {
		drafts.set(m.SelectedID, text)
	}
	setDOMValue("chat-composer", text)
	if m.Callbacks.DraftChanged != nil {
		m.Callbacks.DraftChanged(m.SelectedID, text)
	}
	local.update(func(*localUI) {})
	FocusComposerFor(m.SelectedID)
}

// issue3973Key closes the panel on Escape from inside it.
func issue3973Key(e ui.KeyboardEvent, m Model) bool {
	if e.GetKey() != "Escape" || !m.Handover.Open || m.Handover.ConversationID != m.SelectedID || m.Callbacks.Handover == nil || !handoverFocusInside() {
		return false
	}
	m.Callbacks.Handover(issue3973Form(m.Handover, "close"))
	focusHandoverTrigger()
	return true
}

const issue3973Styles = `.chat-workspace .handover-panel{display:flex;flex-direction:column;gap:8px;margin:0;padding:12px 16px;border-block-end:1px solid var(--line);background:var(--soft);min-width:0;max-height:60vh;overflow:auto}` +
	`.chat-workspace .handover-top{display:flex;align-items:center;justify-content:space-between;gap:8px}` +
	`.chat-workspace .handover-heading{margin:0;font-size:1rem}.chat-workspace .handover-subheading{margin:8px 0 0;font-size:.875rem}` +
	`.chat-workspace .handover-private,.chat-workspace .handover-hint,.chat-workspace .handover-status{margin:0;color:var(--muted);font-size:.8125rem;overflow-wrap:anywhere}.chat-workspace .handover-status:empty{display:none}` +
	`.chat-workspace .handover-label{color:var(--ink);font-size:.8125rem;font-weight:600}` +
	`.chat-workspace .handover-tasks{margin:0;padding:0;border:0;min-width:0}` +
	`.chat-workspace .handover-list{list-style:none;margin:4px 0 0;padding:0;display:flex;flex-direction:column;gap:4px}` +
	`.chat-workspace .handover-task{display:flex;align-items:center;gap:8px;min-height:32px;min-width:0}.chat-workspace .handover-task label{min-width:0;overflow-wrap:anywhere}` +
	`.chat-workspace .handover-box{flex:none;inline-size:18px;block-size:18px;margin:0;accent-color:var(--accent)}` +
	`.chat-workspace .handover-recipient,.chat-workspace .handover-date{display:flex;flex-direction:column;gap:4px;min-width:0}` +
	`.chat-workspace .handover-window{display:flex;flex-wrap:wrap;gap:8px 16px}` +
	`.chat-workspace .handover-select,.chat-workspace .handover-field{max-inline-size:100%;min-block-size:36px;box-sizing:border-box;padding:4px 8px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font:inherit}` +
	`.chat-workspace .handover-row{display:flex;flex-direction:column;gap:2px;padding:6px 10px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);min-width:0}` +
	`.chat-workspace .handover-row-chip{color:var(--muted);font-size:.75rem}.chat-workspace .handover-row[data-outcome="included"] .handover-row-chip{color:var(--ink)}` +
	`.chat-workspace .handover-text{inline-size:100%;box-sizing:border-box;min-block-size:220px;padding:10px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font:inherit;font-size:.875rem;line-height:1.45;resize:vertical}` +
	`.chat-workspace .handover-buttons{display:flex;flex-wrap:wrap;gap:8px}` +
	`@media(max-width:760px),(pointer:coarse){.chat-workspace .handover-panel{padding-inline:12px}.chat-workspace .handover-box{inline-size:24px;block-size:24px}.chat-workspace .handover-task{min-height:44px}.chat-workspace .handover-panel .button,.chat-workspace .handover-panel .icon-button,.chat-workspace .handover-select,.chat-workspace .handover-field{min-height:44px;min-width:44px}}` +
	`@media(prefers-reduced-motion:reduce){.chat-workspace .handover-panel *{transition:none!important;animation:none!important}}`
