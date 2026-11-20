package chatui

import (
	"fmt"
	"strconv"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/localize"
)

// The retention preview (#3778, first slice): on the Chat settings page an
// administrator asks what the saved retention policy would remove. The server
// answers with counts only: how many records of each kind are due, how many are
// kept because a legal hold covers them, and how many are kept because every
// copy of them cannot be confirmed. Nothing is deleted by asking.

// ChatRetentionPreviewKind is one kind of record and how many a sweep would remove.
type ChatRetentionPreviewKind struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

// ChatRetentionPreviewData is the server's answer.
type ChatRetentionPreviewData struct {
	Configured     bool                       `json:"configured"`
	Mode           string                     `json:"mode"`
	Scanned        int                        `json:"scanned"`
	WouldRemove    []ChatRetentionPreviewKind `json:"would_remove"`
	Total          int                        `json:"total"`
	HeldSkipped    int                        `json:"held_skipped"`
	UnknownSkipped int                        `json:"unknown_skipped"`
	UsageUnknown   bool                       `json:"usage_unknown"`
}

// ChatRetentionPreviewModel is everything the panel needs to draw. A nil
// handler draws its control disabled.
type ChatRetentionPreviewModel struct {
	Locale                         string
	Data                           ChatRetentionPreviewData
	Loaded, Loading, Failed        bool
	Denied, Unavailable, SignedOut bool
	// Run asks for the preview; it is also what Try again calls.
	Run ui.Handler
}

// ChatRetentionPreviewText returns one string of the panel in the person's language.
func ChatRetentionPreviewText(locale, key string) string {
	values, ok := chatRetentionPreviewCopy[key]
	if !ok {
		return ""
	}
	return chatbug039Text(key, values[chatbug039LocaleIndex(locale)], values[0])
}

var chatRetentionPreviewCopy = map[string][3]string{
	"title":          {"Preview what retention would remove", "Vorschau: Was die Aufbewahrung entfernen würde", "معاينة ما ستزيله سياسة الاحتفاظ"},
	"intro":          {"See what the saved policy would remove if it ran now. This is a preview only: nothing is deleted, and records under a legal hold are always skipped.", "Sehen Sie, was die gespeicherte Richtlinie entfernen würde, wenn sie jetzt liefe. Das ist nur eine Vorschau: Es wird nichts gelöscht, und Datensätze unter rechtlicher Aufbewahrungspflicht werden immer übersprungen.", "اطّلع على ما ستزيله السياسة المحفوظة لو عملت الآن. هذه معاينة فقط: لا يُحذف شيء، ويتم دائمًا تخطي السجلات الخاضعة لحجز قانوني."},
	"run":            {"Preview", "Vorschau anzeigen", "معاينة"},
	"running":        {"Checking what would be removed. Please wait.", "Es wird geprüft, was entfernt würde. Bitte warten.", "جارٍ فحص ما سيُزال. يرجى الانتظار."},
	"error":          {"The preview could not load. Nothing was changed. Try again.", "Die Vorschau konnte nicht geladen werden. Es wurde nichts geändert. Versuchen Sie es erneut.", "تعذر تحميل المعاينة. لم يتغير شيء. حاول مجددًا."},
	"signed_out":     {"You were signed out, so the preview could not load. Reload the page to sign in again.", "Sie wurden abgemeldet, daher konnte die Vorschau nicht geladen werden. Laden Sie die Seite neu und melden Sie sich erneut an.", "تم تسجيل خروجك لذا تعذرت المعاينة. أعد تحميل الصفحة لتسجيل الدخول مجددًا."},
	"retry":          {"Try again", "Erneut versuchen", "حاول مجددًا"},
	"denied":         {"Only a workspace administrator can preview retention.", "Nur Administratoren des Arbeitsbereichs können die Aufbewahrung in der Vorschau ansehen.", "يمكن لمسؤول مساحة العمل فقط معاينة الاحتفاظ."},
	"unavailable":    {"The retention preview is not set up on this server.", "Die Aufbewahrungsvorschau ist auf diesem Server nicht eingerichtet.", "معاينة الاحتفاظ غير مُعدّة على هذا الخادم."},
	"none":           {"No policy is saved, so nothing would be removed.", "Es ist keine Richtlinie gespeichert, daher würde nichts entfernt.", "لا توجد سياسة محفوظة، لذا لن يُزال شيء."},
	"nothing":        {"Nothing is due for removal under the saved policy. %s records were checked.", "Nach der gespeicherten Richtlinie ist nichts zum Entfernen fällig. %s Datensätze wurden geprüft.", "لا شيء مستحق للإزالة وفق السياسة المحفوظة. تم فحص %s سجلًا."},
	"summary":        {"If the saved policy ran now it would remove %s records out of %s checked.", "Wenn die gespeicherte Richtlinie jetzt liefe, würde sie %s von %s geprüften Datensätzen entfernen.", "لو عملت السياسة المحفوظة الآن لأزالت %s سجلًا من أصل %s تم فحصها."},
	"budget":         {"A storage limit cannot be previewed yet. Chat does not measure the space used by files and copies, so a storage limit would remove nothing.", "Ein Speicherlimit kann noch nicht in der Vorschau angezeigt werden. Der Chat misst den von Dateien und Kopien belegten Platz nicht, daher würde ein Speicherlimit nichts entfernen.", "لا يمكن معاينة حد التخزين بعد. لا تقيس الدردشة المساحة التي تشغلها الملفات والنسخ، لذا لن يزيل حد التخزين شيئًا."},
	"held":           {"%s records are under a legal hold and would be skipped.", "%s Datensätze unterliegen einer rechtlichen Aufbewahrungspflicht und würden übersprungen.", "هناك %s سجلًا خاضعًا لحجز قانوني وسيتم تخطيه."},
	"unknown":        {"%s records would be skipped because every copy of them cannot be confirmed yet.", "%s Datensätze würden übersprungen, weil noch nicht bestätigt werden kann, dass alle Kopien erfasst sind.", "سيتم تخطي %s سجلًا لأنه لا يمكن تأكيد جميع نسخها بعد."},
	"kept":           {"Enforcement is not active, so nothing has been or will be deleted by this preview.", "Die Durchsetzung ist nicht aktiv, daher wurde und wird durch diese Vorschau nichts gelöscht.", "التطبيق غير مفعّل، لذا لم يُحذف ولن يُحذف شيء بسبب هذه المعاينة."},
	"caption":        {"Records that would be removed, by kind", "Datensätze, die entfernt würden, nach Art", "السجلات التي ستُزال حسب النوع"},
	"col_kind":       {"Kind of record", "Art des Datensatzes", "نوع السجل"},
	"col_count":      {"Would remove", "Würde entfernen", "ستُزال"},
	"k_POST":         {"Messages", "Nachrichten", "الرسائل"},
	"k_EDIT":         {"Earlier versions of edited messages", "Frühere Fassungen bearbeiteter Nachrichten", "النسخ السابقة من الرسائل المعدّلة"},
	"k_TOMBSTONE":    {"Removal markers", "Löschmarkierungen", "علامات الإزالة"},
	"k_REACTION":     {"Reactions", "Reaktionen", "التفاعلات"},
	"k_FILE":         {"Files", "Dateien", "الملفات"},
	"k_VOICE":        {"Voice messages", "Sprachnachrichten", "الرسائل الصوتية"},
	"k_VIDEO":        {"Video", "Video", "الفيديو"},
	"k_AGENT_OUTPUT": {"Agent replies", "Agentenantworten", "ردود الوكلاء"},
	"k_DERIVED_COPY": {"Copies such as translations and masked versions", "Kopien wie Übersetzungen und maskierte Fassungen", "نسخ مثل الترجمات والنسخ المقنّعة"},
	"k_CONVERSATION": {"Conversation records", "Unterhaltungsdatensätze", "سجلات المحادثات"},
	"k_MEMBERSHIP":   {"Membership records", "Mitgliedschaftsdatensätze", "سجلات العضوية"},
	"k_APP_CHANGE":   {"App changes", "App-Änderungen", "تغييرات التطبيقات"},
	"k_SHARE_LINK":   {"Share links", "Freigabelinks", "روابط المشاركة"},
}

// ChatRetentionPreviewKindName names a kind of record in plain words; a kind the
// panel has no words for is shown as the server named it, never hidden.
func ChatRetentionPreviewKindName(locale, kind string) string {
	if text := ChatRetentionPreviewText(locale, "k_"+kind); text != "" {
		return text
	}
	return kind
}

func chatRetentionPreviewNumber(locale string, n int) string {
	if shown, err := localize.FormatNumber(locale, strconv.Itoa(n), 0); err == nil && shown != "" {
		return shown
	}
	return strconv.Itoa(n)
}

func chatRetentionPreviewShell(m ChatRetentionPreviewModel, body ...ui.Node) ui.Node {
	nodes := append([]ui.Node{html.H2(html.Props{ID: "chatretpreview-title", Text: ChatRetentionPreviewText(m.Locale, "title")})}, body...)
	return html.Section(html.Props{Class: "chatlangadmin chatlangadmin-page chatretpreview", Dir: direction(m.Locale), Data: map[string]string{"chatretpreview": "panel"}, Aria: map[string]string{"labelledby": "chatretpreview-title"}}, nodes...)
}

// ChatRetentionPreviewSettings draws the preview panel for the Chat settings page.
func ChatRetentionPreviewSettings(m ChatRetentionPreviewModel) ui.Node {
	t := func(key string) string { return ChatRetentionPreviewText(m.Locale, key) }
	n := func(v int) string { return chatRetentionPreviewNumber(m.Locale, v) }
	// While a read is in flight the button is aria-disabled, not disabled: a
	// disabled button drops keyboard focus, and the handler ignores a second press.
	run := func(key string) ui.Node {
		return html.Button(html.Props{ID: "chatretpreview-run", Type: "button", Class: "button", Text: t(key), OnClick: m.Run, Disabled: m.Run.Value() == nil, Aria: map[string]string{"describedby": "chatretpreview-intro", "disabled": boolString(m.Loading)}})
	}
	if m.Denied {
		return chatRetentionPreviewShell(m, html.P(html.Props{Role: "alert", Text: t("denied")}))
	}
	if m.Unavailable {
		return chatRetentionPreviewShell(m, html.P(html.Props{Role: "status", Text: t("unavailable")}))
	}
	intro := html.P(html.Props{ID: "chatretpreview-intro", Class: "field-hint", Text: t("intro")})
	status := func(text string) ui.Node {
		return html.P(html.Props{ID: "chatretpreview-status", Role: "status", Aria: map[string]string{"live": "polite"}, Text: text})
	}
	switch {
	case m.Loading:
		return chatRetentionPreviewShell(m, intro, run("run"), status(t("running")))
	case m.Failed:
		key := "error"
		if m.SignedOut {
			key = "signed_out"
		}
		return chatRetentionPreviewShell(m, intro, html.P(html.Props{ID: "chatretpreview-status", Role: "alert", Text: t(key)}), run("retry"))
	case !m.Loaded:
		return chatRetentionPreviewShell(m, intro, run("run"), status(""))
	}
	d := m.Data
	lines := []ui.Node{}
	headline := ""
	switch {
	case !d.Configured:
		headline = t("none")
	case d.UsageUnknown:
		headline = t("budget")
	case d.Total == 0:
		headline = fmt.Sprintf(t("nothing"), n(d.Scanned))
	default:
		headline = fmt.Sprintf(t("summary"), n(d.Total), n(d.Scanned))
	}
	lines = append(lines, html.P(html.Props{Class: "chatretpreview-headline", Text: headline}))
	if d.Configured && !d.UsageUnknown && len(d.WouldRemove) > 0 {
		rows := make([]ui.Node, 0, len(d.WouldRemove))
		for _, k := range d.WouldRemove {
			rows = append(rows, html.Tr(html.Props{},
				html.Th(html.Props{Raw: map[string]any{"scope": "row"}, Text: ChatRetentionPreviewKindName(m.Locale, k.Kind)}),
				html.Td(html.Props{Dir: "ltr", Text: n(k.Count)})))
		}
		head := html.Thead(html.Props{}, html.Tr(html.Props{},
			html.Th(html.Props{Raw: map[string]any{"scope": "col"}, Text: t("col_kind")}),
			html.Th(html.Props{Raw: map[string]any{"scope": "col"}, Text: t("col_count")})))
		lines = append(lines, html.Div(html.Props{Class: "md-table-wrap", Dir: "auto", Raw: map[string]any{"tabindex": "0"}, Role: "region", Aria: map[string]string{"label": t("caption")}},
			html.Table(html.Props{Class: "md-table chatretpreview-table"}, html.Caption(html.Props{Class: "sr-only", Text: t("caption")}), head, html.Tbody(html.Props{}, rows...))))
	}
	if d.Configured && d.HeldSkipped > 0 {
		lines = append(lines, html.P(html.Props{Class: "chatretpreview-held", Text: fmt.Sprintf(t("held"), n(d.HeldSkipped))}))
	}
	if d.Configured && d.UnknownSkipped > 0 && !d.UsageUnknown {
		lines = append(lines, html.P(html.Props{Class: "chatretpreview-unknown", Text: fmt.Sprintf(t("unknown"), n(d.UnknownSkipped))}))
	}
	lines = append(lines, html.P(html.Props{Class: "field-hint", Text: t("kept")}))
	body := []ui.Node{intro, run("run"), html.Div(html.Props{ID: "chatretpreview-status", Role: "status", Aria: map[string]string{"live": "polite"}}, lines...)}
	return chatRetentionPreviewShell(m, body...)
}
