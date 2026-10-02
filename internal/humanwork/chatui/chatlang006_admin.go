package chatui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
)

// TranslationAdminTerm is one glossary entry as the administration page lists it.
type TranslationAdminTerm struct {
	ID string `json:"id"`
	chatlang.Term
}

// TranslationAdminData is what the translation administration answers: the same
// shape the server sends, with no message text in it.
type TranslationAdminData struct {
	Workspace chatlang.Workspace     `json:"workspace"`
	Channel   *chatlang.Channel      `json:"channel,omitempty"`
	Effective chatlang.Reason        `json:"effective"`
	Glossary  []TranslationAdminTerm `json:"glossary"`
	Engine    struct {
		Name     string `json:"name"`
		Ready    bool   `json:"ready"`
		External bool   `json:"external"`
	} `json:"engine"`
	SpentMicros        int64    `json:"spent_micros"`
	BudgetMicros       int64    `json:"budget_micros"`
	Paused             bool     `json:"paused"`
	Supported          []string `json:"supported"`
	CanManageWorkspace bool     `json:"can_manage_workspace"`
	CanManageChannel   bool     `json:"can_manage_channel"`
}

// TranslationAdminModel is everything the form needs to draw. Each handler is
// bound by the client; a nil handler draws the control disabled.
type TranslationAdminModel struct {
	Locale       string
	Conversation string
	Data         TranslationAdminData
	// Loaded is false until the server has answered once.
	Loaded                 bool
	Loading, Failed, Saved bool
	// SavedField is the name of the control whose change was saved last, so the
	// page can say "Saved" beside that setting and not only above the form
	// (CHATLANG-007).
	SavedField string
	// Denied is true when the server refused the caller's administration.
	Denied bool
	// SignedOut is true when the server no longer accepts the page's session
	// (CHATBUG-087): the failure line then says so instead of "try again".
	SignedOut                           bool
	SaveWorkspace, SaveChannel, AddTerm ui.Handler
	RemoveTerm                          ui.Handler
	// Retry reads the settings again after a failure (CHATUX-027).
	Retry ui.Handler
}

// failureKey is the copy key of the line that says why settings failed to load
// or save.
func (m TranslationAdminModel) failureKey() string {
	if m.SignedOut {
		return "error_signed_out"
	}
	return "error"
}

// translationAdminText returns the text of a key in the person's language. The
// product catalog may carry it; when it does not (or answers with a missing-key
// marker) the table here answers, so a key is never shown on the page.
func translationAdminText(locale, key string, text func(string) string) string {
	values, ok := translationAdminCopy[key]
	if !ok {
		return ""
	}
	index := 0
	if strings.HasPrefix(locale, "de") {
		index = 1
	}
	if strings.HasPrefix(locale, "ar") {
		index = 2
	}
	return chatbug039Text(key, values[index], values[0])
}

// translationAdminCopy holds every visible string in en-US, de-DE and ar.
var translationAdminCopy = map[string][3]string{
	"title":                  {"Translation", "Übersetzung", "الترجمة"},
	"loading":                {"Loading translation settings. Please wait.", "Übersetzungseinstellungen werden geladen. Bitte warten.", "جار تحميل إعدادات الترجمة. يرجى الانتظار."},
	"error":                  {"Translation settings could not load or save. Try again.", "Übersetzungseinstellungen konnten nicht geladen oder gespeichert werden. Versuchen Sie es erneut.", "تعذر تحميل إعدادات الترجمة أو حفظها. حاول مجدداً."},
	"error_signed_out":       {"You were signed out, so translation settings could not load or save. Reload the page to sign in again.", "Sie wurden abgemeldet, daher konnten die Übersetzungseinstellungen nicht geladen oder gespeichert werden. Laden Sie die Seite neu und melden Sie sich erneut an.", "تم تسجيل خروجك لذا تعذر تحميل إعدادات الترجمة أو حفظها. أعد تحميل الصفحة لتسجيل الدخول مجددًا."},
	"retry":                  {"Try again", "Erneut versuchen", "حاول مجدداً"},
	"denied":                 {"Only a workspace administrator or this channel's manager can change translation.", "Nur Arbeitsbereichsadministratoren oder die Verwaltung dieses Kanals können die Übersetzung ändern.", "يمكن فقط لمسؤول مساحة العمل أو مدير هذه القناة تغيير الترجمة."},
	"saved":                  {"Saved.", "Gespeichert.", "تم الحفظ."},
	"enabled":                {"Translate messages into each reader's language", "Nachrichten in die Sprache jedes Lesers übersetzen", "ترجم الرسائل إلى لغة كل قارئ"},
	"enabled_hint":           {"Off until you turn it on. People also choose, in their own settings, whether to read translations.", "Aus, bis Sie es einschalten. Jede Person entscheidet außerdem in ihren Einstellungen, ob sie Übersetzungen lesen möchte.", "متوقفة حتى تشغّلها. ويختار كل شخص في إعداداته ما إذا كان يريد قراءة الترجمات."},
	"languages":              {"Languages offered", "Angebotene Sprachen", "اللغات المتاحة"},
	"languages_hint":         {"Leave all unticked to offer every language.", "Lassen Sie alle abgewählt, um jede Sprache anzubieten.", "اترك الكل غير محدد لإتاحة جميع اللغات."},
	"limit":                  {"Monthly limit (USD)", "Monatliches Limit (USD)", "الحد الشهري (دولار أمريكي)"},
	"limit_hint":             {"When the limit is reached, translation stops until next month and messages show in the language they were written in.", "Ist das Limit erreicht, pausiert die Übersetzung bis zum nächsten Monat und Nachrichten erscheinen in ihrer Originalsprache.", "عند بلوغ الحد تتوقف الترجمة حتى الشهر التالي وتظهر الرسائل باللغة التي كُتبت بها."},
	"used":                   {"Used this month: {used} of {limit}", "Diesen Monat verbraucht: {used} von {limit}", "المستخدم هذا الشهر: {used} من {limit}"},
	"paused":                 {"Translation is paused: this month's limit is used. It resumes next month, or when you raise the limit.", "Die Übersetzung pausiert: Das Limit dieses Monats ist aufgebraucht. Sie läuft im nächsten Monat weiter oder sobald Sie das Limit erhöhen.", "الترجمة متوقفة: استُنفد حد هذا الشهر. ستستأنف الشهر القادم أو عند رفع الحد."},
	"external":               {"Allow sending text to an outside translation service", "Senden von Text an einen externen Übersetzungsdienst erlauben", "السماح بإرسال النص إلى خدمة ترجمة خارجية"},
	"external_hint":          {"Turn off to keep every message inside this deployment; nothing is translated while no engine inside it exists.", "Ausschalten, damit jede Nachricht in dieser Installation bleibt; solange es dort keine Übersetzungsmaschine gibt, wird nichts übersetzt.", "أوقفه لإبقاء كل رسالة داخل هذا النشر؛ لا يُترجم شيء ما دام لا يوجد محرك بداخله."},
	"engine":                 {"Translation engine: {name}", "Übersetzungsmaschine: {name}", "محرك الترجمة: {name}"},
	"workspace_off_line":     {"Translation is off for this workspace.", "Die Übersetzung ist für diesen Arbeitsbereich ausgeschaltet.", "الترجمة متوقفة لمساحة العمل هذه."},
	"open_admin":             {"Turn it on in Chat settings", "In den Chat-Einstellungen einschalten", "شغّلها من إعدادات الدردشة"},
	"never_outside":          {"Never use an outside service", "Nie einen externen Dienst verwenden", "عدم استخدام خدمة خارجية أبداً"},
	"page_intro":             {"These settings apply to the whole workspace. Each one saves as soon as you change it.", "Diese Einstellungen gelten für den gesamten Arbeitsbereich. Jede wird gespeichert, sobald Sie sie ändern.", "تنطبق هذه الإعدادات على مساحة العمل كاملة. يُحفظ كل إعداد بمجرد تغييره."},
	"channel_switch":         {"Translation in this channel", "Übersetzung in diesem Kanal", "الترجمة في هذه القناة"},
	"follow":                 {"Follow the workspace", "Wie der Arbeitsbereich", "اتبع مساحة العمل"},
	"on":                     {"On", "An", "مفعّلة"},
	"off":                    {"Off", "Aus", "متوقفة"},
	"barred":                 {"Never send this channel's messages to an outside translation service", "Nachrichten dieses Kanals nie an einen externen Übersetzungsdienst senden", "لا ترسل رسائل هذه القناة إلى خدمة ترجمة خارجية أبداً"},
	"status_":                {"Translation is on in this channel.", "Die Übersetzung ist in diesem Kanal eingeschaltet.", "الترجمة مفعّلة في هذه القناة."},
	"status_workspace_off":   {"Translation is off for this workspace. A workspace administrator can turn it on.", "Die Übersetzung ist für diesen Arbeitsbereich ausgeschaltet. Ein Arbeitsbereichsadministrator kann sie einschalten.", "الترجمة متوقفة لمساحة العمل هذه. يمكن لمسؤول مساحة العمل تشغيلها."},
	"status_channel_off":     {"Translation is off in this channel.", "Die Übersetzung ist in diesem Kanal ausgeschaltet.", "الترجمة متوقفة في هذه القناة."},
	"status_external_barred": {"Messages here are never sent to an outside translation service, so they are not translated.", "Nachrichten hier werden nie an einen externen Übersetzungsdienst gesendet und daher nicht übersetzt.", "لا تُرسل الرسائل هنا إلى خدمة ترجمة خارجية أبداً، لذلك لا تُترجم."},
	"glossary":               {"Glossary", "Glossar", "المسرد"},
	"glossary_hint":          {"Terms that must be translated a set way, or never translated, such as product names.", "Begriffe, die auf eine feste Weise übersetzt oder nie übersetzt werden, etwa Produktnamen.", "مصطلحات يجب ترجمتها بطريقة محددة أو عدم ترجمتها أبداً، مثل أسماء المنتجات."},
	"glossary_empty":         {"No terms yet.", "Noch keine Begriffe.", "لا توجد مصطلحات بعد."},
	"term":                   {"Term", "Begriff", "المصطلح"},
	"mode":                   {"How to translate it", "Wie übersetzen", "كيفية ترجمته"},
	"keep":                   {"Keep as written", "Unverändert lassen", "أبقه كما هو"},
	"translate":              {"Translate as", "Übersetzen als", "ترجمه إلى"},
	"language":               {"Language", "Sprache", "اللغة"},
	"target":                 {"Required translation", "Vorgeschriebene Übersetzung", "الترجمة المطلوبة"},
	"add":                    {"Add term", "Begriff hinzufügen", "أضف مصطلحاً"},
	"remove":                 {"Remove", "Entfernen", "أزل"},
	"keeps":                  {"kept as written", "unverändert", "يبقى كما هو"},
	"en":                     {"English", "English", "English"},
	"de":                     {"Deutsch", "Deutsch", "Deutsch"},
	"fr":                     {"Français", "Français", "Français"},
	"es":                     {"Español", "Español", "Español"},
	"pt":                     {"Português", "Português", "Português"},
	"ar":                     {"العربية", "العربية", "العربية"},
	"ja":                     {"日本語", "日本語", "日本語"},
	"hi":                     {"हिन्दी", "हिन्दी", "हिन्दी"},
}

// TranslationAdminAmount writes millionths of a unit as a two-decimal amount.
func TranslationAdminAmount(micros int64) string {
	if micros < 0 {
		micros = 0
	}
	whole, cents := micros/1_000_000, (micros%1_000_000)/10_000
	return strconv.FormatInt(whole, 10) + "." + strings.Repeat("0", 2-len(strconv.FormatInt(cents, 10))) + strconv.FormatInt(cents, 10)
}

func translationAdminHas(list []string, language string) bool {
	for _, l := range list {
		if l == language {
			return true
		}
	}
	return false
}

// ChatlangAdminStyles are the administration's layout, from the product's own
// tokens only.
const ChatlangAdminStyles = `.chatlangadmin{box-sizing:border-box;min-width:0;max-width:100%;display:grid;gap:16px;padding:12px;color:var(--hcm-color-text);background:var(--hcm-color-surface);border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);overflow-wrap:anywhere}.chatlangadmin *{box-sizing:border-box;min-width:0}.chatlangadmin-form,.chatlangadmin-glossary{display:grid;gap:8px}.chatlangadmin input[type=text],.chatlangadmin input[type=number],.chatlangadmin select{width:100%;min-height:44px;padding:8px;color:var(--hcm-color-text);background:var(--hcm-color-surface);border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control)}.chatlangadmin button{min-height:44px;max-width:100%;white-space:normal}.chatlangadmin-check{display:flex;align-items:center;gap:8px;min-height:44px}.chatlangadmin-check input{flex:none;width:24px;height:24px;accent-color:var(--hcm-color-brand-primary)}.chatlangadmin-languages{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,140px),1fr));gap:4px;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control)}.chatlangadmin-languages legend,.chatlangadmin-languages p{grid-column:1/-1}.chatlangadmin-terms{list-style:none;margin:0;padding:0;display:grid;gap:8px}.chatlangadmin-term{display:flex;flex-wrap:wrap;align-items:center;gap:8px}.chatlangadmin-term-text{flex:1 1 160px}.chatlangadmin-paused{color:var(--hcm-color-danger)}.chatlangadmin-used,.chatlangadmin-effective{color:var(--hcm-color-text-muted)}.chatlangadmin :focus-visible{outline:2px solid var(--hcm-color-brand-primary);outline-offset:2px}@media(max-width:390px){.chatlangadmin{padding:8px}}@media(prefers-reduced-motion:reduce){.chatlangadmin *{animation:none;transition:none}}`
