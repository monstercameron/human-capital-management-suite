package chatui

import "strings"

// modadminText resolves administrator copy for language and custom filters
// through the shared guard, using the English column as its fallback.
func modadminText(m Model, key string) string {
	values, ok := modadminCopy[key]
	if !ok {
		return ""
	}
	return chatbug039Text(key, values[modadminLocaleIndex(m.Locale)], values[0])
}

// modadminLocaleIndex picks the column of modadminCopy: 0 English, 1 German,
// 2 Arabic.
func modadminLocaleIndex(locale string) int {
	locale = strings.ToLower(locale)
	switch {
	case strings.HasPrefix(locale, "de"):
		return 1
	case strings.HasPrefix(locale, "ar"):
		return 2
	}
	return 0
}

// modadminFormat fills {name} style placeholders in a copy string.
func modadminFormat(text string, pairs ...string) string {
	for i := 0; i+1 < len(pairs); i += 2 {
		text = strings.ReplaceAll(text, "{"+pairs[i]+"}", pairs[i+1])
	}
	return text
}

var modadminCopy = map[string][3]string{
	// Panel frame
	"intro":         {"Filters check each message before it is sent. Direct messages are only checked by workspace filters that also apply in direct messages.", "Filter prüfen jede Nachricht, bevor sie gesendet wird. Direktnachrichten prüfen nur Arbeitsbereichsfilter, die auch in Direktnachrichten gelten.", "تفحص المرشحات كل رسالة قبل إرسالها. أما الرسائل المباشرة فلا تخضع إلا لمرشحات مساحة العمل التي تسري أيضاً في الرسائل المباشرة."},
	"intro_ws":      {"These filters protect the whole workspace. Each channel can still switch a built-in list on or off for itself.", "Diese Filter schützen den ganzen Arbeitsbereich. Jeder Kanal kann eine integrierte Liste weiterhin für sich ein- oder ausschalten.", "تحمي هذه المرشحات مساحة العمل بأكملها. ويمكن لكل قناة أن تشغّل قائمة مدمجة أو تطفئها لنفسها."},
	"loading":       {"Loading filters. Please wait.", "Filter werden geladen. Bitte warten.", "جارٍ تحميل المرشحات. يرجى الانتظار."},
	"load_failed":   {"Filters could not be loaded. Nothing was changed.", "Filter konnten nicht geladen werden. Es wurde nichts geändert.", "تعذّر تحميل المرشحات. لم يتغير شيء."},
	"retry":         {"Try again", "Erneut versuchen", "حاول مجدداً"},
	"stale":         {"The latest settings could not be loaded. You are seeing the last settings that did load.", "Die neuesten Einstellungen konnten nicht geladen werden. Sie sehen die zuletzt geladenen Einstellungen.", "تعذّر تحميل أحدث الإعدادات. ما تراه هو آخر إعدادات تم تحميلها."},
	"no_permission": {"You need permission to manage filters here. Ask a workspace administrator or a channel manager.", "Sie benötigen die Berechtigung, hier Filter zu verwalten. Fragen Sie eine Arbeitsbereichsadministration oder eine Kanalverwaltung.", "تحتاج إلى صلاحية لإدارة المرشحات هنا. اطلب ذلك من مسؤول مساحة العمل أو من مدير القناة."},

	// Built-in lists
	"b_title":         {"Built-in word lists", "Integrierte Wortlisten", "قوائم الكلمات المدمجة"},
	"b_hint":          {"Ready-made lists, one set per language. Every list starts switched off.", "Fertige Listen, ein Satz pro Sprache. Jede Liste ist zunächst ausgeschaltet.", "قوائم جاهزة، مجموعة لكل لغة. تبدأ كل قائمة مطفأة."},
	"b_hint_ch":       {"Switching a list here changes only this channel.", "Eine Änderung hier gilt nur für diesen Kanal.", "التغيير هنا يخص هذه القناة فقط."},
	"b_hint_ws":       {"Switching a list here changes it for every channel that has not chosen its own setting.", "Eine Änderung hier gilt für alle Kanäle, die keine eigene Einstellung gewählt haben.", "التغيير هنا يسري على كل قناة لم تختر إعداداً خاصاً بها."},
	"list_profanity":  {"Profanity", "Flüche", "الألفاظ النابية"},
	"list_slurs":      {"Slurs", "Abwertende Ausdrücke", "الإهانات التمييزية"},
	"list_harassment": {"Harassment", "Belästigung", "المضايقة"},
	"on":              {"On", "Ein", "مفعّل"},
	"off":             {"Off", "Aus", "متوقف"},
	"s_ws_on":         {"On for the whole workspace", "Für den ganzen Arbeitsbereich eingeschaltet", "مفعّلة لمساحة العمل بأكملها"},
	"s_off":           {"Off", "Aus", "متوقفة"},
	"s_chan_only":     {"On in this channel only", "Nur in diesem Kanal eingeschaltet", "مفعّلة في هذه القناة فقط"},
	"s_chan_own":      {"On in this channel with its own choice (the workspace has it on too)", "In diesem Kanal mit eigener Wahl eingeschaltet (im Arbeitsbereich ebenfalls eingeschaltet)", "مفعّلة في هذه القناة باختيارها الخاص (وهي مفعّلة أيضاً لمساحة العمل)"},
	"s_chan_off":      {"Off in this channel (on for the workspace)", "In diesem Kanal ausgeschaltet (im Arbeitsbereich eingeschaltet)", "متوقفة في هذه القناة (ومفعّلة لمساحة العمل)"},
	"s_dry":           {"Recording only: matches are noted but nothing is blocked or hidden", "Nur Aufzeichnung: Treffer werden notiert, aber nichts wird blockiert oder ausgeblendet", "تسجيل فقط: تُسجَّل المطابقات دون حظر أو إخفاء"},
	"what":            {"What happens", "Was passiert", "ماذا يحدث"},
	"act_block":       {"Block and explain", "Blockieren und erklären", "امنع واشرح"},
	"act_mask":        {"Hide the word from readers", "Wort für Leser ausblenden", "أخفِ الكلمة عن القراء"},
	"act_flag":        {"Flag for review", "Zur Prüfung markieren", "علّم للمراجعة"},
	"act_notify":      {"Notify a channel or agent", "Kanal oder Agent benachrichtigen", "أبلغ قناة أو وكيلاً"},
	"reset":           {"Use the workspace setting", "Einstellung des Arbeitsbereichs verwenden", "استخدم إعداد مساحة العمل"},

	// Custom filters
	"c_title_ch":      {"This channel's own filters", "Eigene Filter dieses Kanals", "المرشحات الخاصة بهذه القناة"},
	"c_title_ws":      {"Custom filters", "Eigene Filter", "مرشحات مخصّصة"},
	"c_empty_ch":      {"This channel has no filters of its own yet.", "Dieser Kanal hat noch keine eigenen Filter.", "لا توجد مرشحات خاصة بهذه القناة بعد."},
	"c_empty_ws":      {"No custom filters yet.", "Noch keine eigenen Filter.", "لا توجد مرشحات مخصّصة بعد."},
	"edit":            {"Edit", "Bearbeiten", "تعديل"},
	"see":             {"See what it checks", "Anzeigen, was geprüft wird", "اعرض ما تفحصه"},
	"a_title":         {"Set by your workspace administrators", "Von Ihren Arbeitsbereichsadministratoren festgelegt", "من إعداد مسؤولي مساحة العمل"},
	"a_hint":          {"These apply here too. A channel manager cannot change them.", "Diese gelten auch hier. Eine Kanalverwaltung kann sie nicht ändern.", "تسري هذه المرشحات هنا أيضاً. ولا يستطيع مدير القناة تغييرها."},
	"a_hint_admin":    {"These apply here too. Change them in the workspace filters.", "Diese gelten auch hier. Ändern Sie sie in den Arbeitsbereichsfiltern.", "تسري هذه المرشحات هنا أيضاً. غيّرها من مرشحات مساحة العمل."},
	"dm":              {"Also applies in direct messages", "Gilt auch in Direktnachrichten", "تسري أيضاً في الرسائل المباشرة"},
	"create":          {"Create a filter", "Filter erstellen", "أنشئ مرشحاً"},
	"v_block":         {"Blocks {what}", "Blockiert {what}", "يمنع {what}"},
	"v_mask":          {"Hides {what} from readers", "Blendet {what} für Leser aus", "يخفي {what} عن القراء"},
	"v_flag":          {"Flags {what} for review", "Markiert {what} zur Prüfung", "يعلّم {what} للمراجعة"},
	"v_notify":        {"Tells someone about {what}", "Meldet {what} an jemanden", "يُبلغ جهةً بوجود {what}"},
	"o_words":         {"a word list", "eine Wortliste", "قائمة كلمات"},
	"o_pattern":       {"pattern matches", "Musterübereinstimmungen", "مطابقات النمط"},
	"o_card":          {"card numbers", "Kartennummern", "أرقام البطاقات"},
	"o_national-id":   {"national ID numbers", "nationale Ausweisnummern", "أرقام الهوية الوطنية"},
	"o_access-key":    {"access keys", "Zugangsschlüssel", "مفاتيح الوصول"},
	"o_secret":        {"passwords and secrets", "Passwörter und Geheimnisse", "كلمات المرور والأسرار"},
	"o_external-link": {"links outside the allowed list", "Links außerhalb der erlaubten Liste", "الروابط خارج القائمة المسموح بها"},
	"o_attachment":    {"attachment types", "Anhangtypen", "أنواع المرفقات"},
	"sc_ws":           {"Whole workspace", "Ganzer Arbeitsbereich", "مساحة العمل بأكملها"},
	"sc_n":            {"{n} channels", "{n} Kanäle", "{n} قنوات"},
	"sc_one":          {"One channel", "Ein Kanal", "قناة واحدة"},
	"ws_entry":        {"Workspace filters", "Arbeitsbereichsfilter", "مرشحات مساحة العمل"},
	"ws_title":        {"Workspace filters", "Arbeitsbereichsfilter", "مرشحات مساحة العمل"},

	// Editor
	"e_title_edit":  {"Edit this filter", "Diesen Filter bearbeiten", "عدّل هذا المرشح"},
	"e_back":        {"Back to the filters", "Zurück zu den Filtern", "العودة إلى المرشحات"},
	"e_name":        {"Name", "Name", "الاسم"},
	"e_name_ph":     {"For example: Project code names", "Zum Beispiel: Projekt-Codenamen", "مثال: الأسماء الرمزية للمشاريع"},
	"e_name_hint":   {"A short name so you can recognise this filter later.", "Ein kurzer Name, damit Sie den Filter später wiedererkennen.", "اسم قصير يساعدك على تمييز هذا المرشح لاحقاً."},
	"e_kind":        {"What it checks", "Was geprüft wird", "ما الذي يفحصه"},
	"k_words":       {"Words or phrases", "Wörter oder Ausdrücke", "كلمات أو عبارات"},
	"k_pattern":     {"Pattern", "Muster", "نمط"},
	"k_sensitive":   {"Sensitive data (card numbers, national IDs, access keys, secrets)", "Sensible Daten (Kartennummern, Ausweisnummern, Zugangsschlüssel, Geheimnisse)", "بيانات حساسة (أرقام البطاقات، الهويات الوطنية، مفاتيح الوصول، الأسرار)"},
	"k_links":       {"Links outside an allowed list", "Links außerhalb einer erlaubten Liste", "روابط خارج قائمة مسموح بها"},
	"k_attachment":  {"Attachment type", "Anhangtyp", "نوع المرفق"},
	"kh_words":      {"Type one word or phrase per line. Capital letters and simple spelling tricks are still caught.", "Ein Wort oder Ausdruck pro Zeile. Großbuchstaben und einfache Schreibtricks werden trotzdem erkannt.", "اكتب كلمة أو عبارة في كل سطر. يُكتشف حتى استخدام الأحرف الكبيرة والحيل الإملائية البسيطة."},
	"kh_pattern":    {"One pattern per line. Use plain letters, digits and simple groups. Patterns that could be slow are refused.", "Ein Muster pro Zeile. Verwenden Sie einfache Buchstaben, Ziffern und einfache Gruppen. Muster, die langsam sein könnten, werden abgelehnt.", "نمط واحد في كل سطر. استخدم حروفاً وأرقاماً ومجموعات بسيطة. تُرفض الأنماط التي قد تكون بطيئة."},
	"kh_sensitive":  {"Pick what to look for. Card numbers are checked, so ordinary long numbers are not caught.", "Wählen Sie, wonach gesucht wird. Kartennummern werden geprüft, gewöhnliche lange Zahlen werden nicht erfasst.", "اختر ما تريد البحث عنه. تُفحص أرقام البطاقات، لذلك لا تُلتقط الأرقام الطويلة العادية."},
	"kh_links":      {"Any link to another website is caught unless its site is on your allowed list. Add the allowed sites, one per line.", "Jeder Link zu einer anderen Website wird erfasst, außer die Website steht auf Ihrer Erlaubt-Liste. Tragen Sie erlaubte Websites ein, eine pro Zeile.", "يُلتقط أي رابط إلى موقع آخر ما لم يكن موقعه في قائمتك المسموح بها. أضف المواقع المسموح بها، موقعاً في كل سطر."},
	"kh_attachment": {"One file type per line, for example application/pdf.", "Ein Dateityp pro Zeile, zum Beispiel application/pdf.", "نوع ملف واحد في كل سطر، مثل application/pdf."},
	"ml_words":      {"Words or phrases, one per line", "Wörter oder Ausdrücke, einer pro Zeile", "كلمات أو عبارات، واحدة في كل سطر"},
	"ml_pattern":    {"Patterns, one per line", "Muster, eines pro Zeile", "أنماط، واحد في كل سطر"},
	"ml_sensitive":  {"What to look for", "Wonach gesucht wird", "ما الذي يُبحث عنه"},
	"ml_links":      {"Allowed websites, one per line (optional)", "Erlaubte Websites, eine pro Zeile (optional)", "المواقع المسموح بها، موقع في كل سطر (اختياري)"},
	"ml_attachment": {"File types, one per line", "Dateitypen, einer pro Zeile", "أنواع الملفات، نوع في كل سطر"},
	"ph_words":      {"Project Phoenix\nAcme Corp", "Projekt Phönix\nBeispiel GmbH", "مشروع العنقاء\nشركة المثال"},
	"ph_pattern":    {"PX-[0-9]{4}", "PX-[0-9]{4}", "PX-[0-9]{4}"},
	"ph_links":      {"example.com", "beispiel.de", "example.com"},
	"ph_attachment": {"application/zip", "application/zip", "application/zip"},
	"d_card":        {"Card numbers", "Kartennummern", "أرقام البطاقات"},
	"d_national-id": {"National ID numbers", "Nationale Ausweisnummern", "أرقام الهوية الوطنية"},
	"d_access-key":  {"Access keys", "Zugangsschlüssel", "مفاتيح الوصول"},
	"d_secret":      {"Passwords and secrets", "Passwörter und Geheimnisse", "كلمات المرور والأسرار"},
	"e_action":      {"What happens", "Was passiert", "ماذا يحدث"},
	"ah_block":      {"The message is not sent and the author is told why.", "Die Nachricht wird nicht gesendet und die Person erfährt den Grund.", "لا تُرسل الرسالة ويُبلَّغ كاتبها بالسبب."},
	"ah_mask":       {"The message is sent. Readers see the word replaced by “removed word”.", "Die Nachricht wird gesendet. Leser sehen das Wort ersetzt durch „entferntes Wort“.", "تُرسل الرسالة، ويرى القراء الكلمة مستبدلة بعبارة «كلمة محذوفة»."},
	"ah_flag":       {"The message is sent and recorded for review. Nobody sees a difference.", "Die Nachricht wird gesendet und zur Prüfung festgehalten. Niemand sieht einen Unterschied.", "تُرسل الرسالة وتُسجَّل للمراجعة. لن يلاحظ أحد أي فرق."},
	"ah_notify":     {"The message is sent and the person or channel below is told.", "Die Nachricht wird gesendet und die unten genannte Person oder der Kanal wird informiert.", "تُرسل الرسالة ويُبلَّغ الشخص أو القناة المذكورة أدناه."},
	"e_target":      {"Who to tell", "Wen benachrichtigen", "من يُبلَّغ"},
	"e_target_ph":   {"A channel or agent, for example #security", "Ein Kanal oder Agent, zum Beispiel #sicherheit", "قناة أو وكيل، مثل #security"},
	"e_where":       {"Where it applies", "Wo es gilt", "أين يسري"},
	"w_channel":     {"This channel", "Dieser Kanal", "هذه القناة"},
	"w_ws":          {"Whole workspace", "Ganzer Arbeitsbereich", "مساحة العمل بأكملها"},
	"w_chosen":      {"Chosen channels", "Ausgewählte Kanäle", "قنوات محددة"},
	"w_pick":        {"Pick one or more channels", "Wählen Sie einen oder mehrere Kanäle", "اختر قناة واحدة أو أكثر"},
	"w_none":        {"There are no channels to pick from.", "Es gibt keine Kanäle zur Auswahl.", "لا توجد قنوات للاختيار منها."},
	"w_locked":      {"Where a filter applies cannot be changed after it is saved. Create a new filter to use another place.", "Wo ein Filter gilt, lässt sich nach dem Speichern nicht mehr ändern. Erstellen Sie einen neuen Filter für einen anderen Ort.", "لا يمكن تغيير مكان سريان المرشح بعد حفظه. أنشئ مرشحاً جديداً لاستخدام مكان آخر."},
	"e_exempt":      {"Exemptions", "Ausnahmen", "الاستثناءات"},
	"ex_hint":       {"Optional. People with these roles, and these agents, are never held back by this filter.", "Optional. Personen mit diesen Rollen und diese Agenten werden von diesem Filter nie aufgehalten.", "اختياري. لا يحجب هذا المرشح أصحاب هذه الأدوار ولا هذه الوكلاء أبداً."},
	"ex_roles":      {"Roles, one per line", "Rollen, eine pro Zeile", "الأدوار، دور في كل سطر"},
	"ex_agents":     {"Agents, one per line", "Agenten, einer pro Zeile", "الوكلاء، وكيل في كل سطر"},
	"e_dry":         {"Record only for 7 days before enforcing", "7 Tage nur aufzeichnen, dann durchsetzen", "سجّل فقط لمدة 7 أيام قبل التطبيق"},
	"e_dry_hint":    {"Matches are written down but nothing is blocked or hidden.", "Treffer werden festgehalten, aber nichts wird blockiert oder ausgeblendet.", "تُسجَّل المطابقات دون حظر أو إخفاء أي شيء."},
	"e_hard":        {"Also applies in direct messages", "Gilt auch in Direktnachrichten", "يسري أيضاً في الرسائل المباشرة"},
	"e_hard_hint":   {"Only workspace administrators can turn this on, and it cannot be turned off later.", "Nur Arbeitsbereichsadministratoren können dies einschalten, und es lässt sich später nicht ausschalten.", "لا يستطيع تفعيل هذا الخيار إلا مسؤولو مساحة العمل، ولا يمكن إيقافه لاحقاً."},
	"e_hard_locked": {"This filter also applies in direct messages and that cannot be turned off.", "Dieser Filter gilt auch in Direktnachrichten und das lässt sich nicht ausschalten.", "يسري هذا المرشح أيضاً في الرسائل المباشرة ولا يمكن إيقاف ذلك."},
	"e_save":        {"Save filter", "Filter speichern", "احفظ المرشح"},
	"e_save_hint":   {"Saving switches the filter on.", "Beim Speichern wird der Filter eingeschaltet.", "يؤدي الحفظ إلى تشغيل المرشح."},
	"e_cancel":      {"Cancel", "Abbrechen", "إلغاء"},

	// Try box
	"try_title":     {"Try a message", "Nachricht ausprobieren", "جرّب رسالة"},
	"try_hint":      {"Type a message to see what this filter would do. Nothing is saved or sent.", "Geben Sie eine Nachricht ein, um zu sehen, was dieser Filter tun würde. Es wird nichts gespeichert oder gesendet.", "اكتب رسالة لترى ما الذي سيفعله هذا المرشح. لن يُحفظ شيء ولن يُرسل."},
	"try_label":     {"Message to try", "Nachricht zum Ausprobieren", "الرسالة المراد تجربتها"},
	"try_btn":       {"Try it", "Ausprobieren", "جرّبها"},
	"try_busy":      {"Trying…", "Wird ausprobiert…", "جارٍ التجربة…"},
	"try_empty":     {"Type a message to try first.", "Geben Sie zuerst eine Nachricht ein.", "اكتب رسالة لتجربتها أولاً."},
	"try_att":       {"Attachment filters cannot be tried with text. Save the filter, then add a file to a message to see it work.", "Anhangfilter lassen sich nicht mit Text ausprobieren. Speichern Sie den Filter und fügen Sie dann einer Nachricht eine Datei hinzu.", "لا يمكن تجربة مرشحات المرفقات بالنص. احفظ المرشح ثم أضف ملفاً إلى رسالة لترى عمله."},
	"o_block":       {"This message would be blocked: “{term}” matches {rule}.", "Diese Nachricht würde blockiert: „{term}“ passt zu {rule}.", "ستُحظر هذه الرسالة: «{term}» تطابق {rule}."},
	"o_block_plain": {"This message would be blocked by {rule}.", "Diese Nachricht würde von {rule} blockiert.", "ستُحظر هذه الرسالة بواسطة {rule}."},
	"o_mask":        {"Readers would see:", "Leser würden sehen:", "سيرى القراء:"},
	"o_flag":        {"This message would be flagged for review. Nobody else sees a difference.", "Diese Nachricht würde zur Prüfung markiert. Für alle anderen ändert sich nichts.", "ستُعلَّم هذه الرسالة للمراجعة. لن يلاحظ أحد غيرك أي فرق."},
	"o_notify":      {"This message would be sent, and {target} would be told about it.", "Diese Nachricht würde gesendet, und {target} würde darüber informiert.", "ستُرسل هذه الرسالة، وسيُبلَّغ {target} بها."},
	"o_none":        {"No match: this message would be sent as it is.", "Kein Treffer: Diese Nachricht würde unverändert gesendet.", "لا توجد مطابقة: ستُرسل هذه الرسالة كما هي."},
	"o_dry":         {"Record only is on, so for the first 7 days nothing would actually be blocked or hidden.", "„Nur aufzeichnen“ ist eingeschaltet, daher würde in den ersten 7 Tagen nichts tatsächlich blockiert oder ausgeblendet.", "خيار «التسجيل فقط» مفعّل، لذا لن يُحظر شيء ولن يُخفى فعلياً خلال الأيام السبعة الأولى."},

	// Outcomes of an action
	"st_saved":        {"Saved.", "Gespeichert.", "تم الحفظ."},
	"st_saved_on":     {"Saved and switched on.", "Gespeichert und eingeschaltet.", "تم الحفظ والتشغيل."},
	"st_saved_not_on": {"The filter was saved but could not be switched on. Use its switch to try again.", "Der Filter wurde gespeichert, konnte aber nicht eingeschaltet werden. Versuchen Sie es mit seinem Schalter erneut.", "تم حفظ المرشح لكن تعذّر تشغيله. استخدم مفتاحه للمحاولة مجدداً."},
	"er_perm":         {"You need permission to change this.", "Sie benötigen die Berechtigung, dies zu ändern.", "تحتاج إلى صلاحية لتغيير هذا."},
	"er_unavail":      {"Filters are not available right now. Try again in a moment.", "Filter sind gerade nicht verfügbar. Versuchen Sie es gleich noch einmal.", "المرشحات غير متاحة حالياً. حاول مرة أخرى بعد قليل."},
	"er_conflict":     {"Someone else changed this filter first. Reload and try again.", "Jemand hat diesen Filter zuvor geändert. Laden Sie neu und versuchen Sie es erneut.", "قام شخص آخر بتغيير هذا المرشح أولاً. أعد التحميل وحاول مجدداً."},
	"er_invalid":      {"The server did not accept this filter. Check the fields and try again.", "Der Server hat diesen Filter nicht akzeptiert. Prüfen Sie die Felder und versuchen Sie es erneut.", "لم يقبل الخادم هذا المرشح. تحقق من الحقول وحاول مجدداً."},
	"er_network":      {"The server could not be reached. Check your connection and try again.", "Der Server ist nicht erreichbar. Prüfen Sie Ihre Verbindung und versuchen Sie es erneut.", "تعذّر الوصول إلى الخادم. تحقق من اتصالك وحاول مجدداً."},
	"sf_switch":       {"The switch was left as it was.", "Der Schalter blieb unverändert.", "بقي المفتاح كما كان."},
	"sf_save":         {"The filter was not saved.", "Der Filter wurde nicht gespeichert.", "لم يُحفظ المرشح."},
	"sf_try":          {"Nothing was tried.", "Es wurde nichts ausprobiert.", "لم تُجرَّب أي رسالة."},

	// Validation, one line per field
	"v_name":        {"Give the filter a name.", "Geben Sie dem Filter einen Namen.", "أعطِ المرشح اسماً."},
	"v_name_long":   {"Use a shorter name, up to 100 characters.", "Verwenden Sie einen kürzeren Namen, höchstens 100 Zeichen.", "استخدم اسماً أقصر، بحد أقصى 100 حرف."},
	"v_words":       {"Add at least one word or phrase.", "Fügen Sie mindestens ein Wort oder einen Ausdruck hinzu.", "أضف كلمة أو عبارة واحدة على الأقل."},
	"v_words_bad":   {"Use letters or digits in each word or phrase.", "Verwenden Sie in jedem Wort oder Ausdruck Buchstaben oder Ziffern.", "استخدم حروفاً أو أرقاماً في كل كلمة أو عبارة."},
	"v_pattern":     {"Add at least one pattern.", "Fügen Sie mindestens ein Muster hinzu.", "أضف نمطاً واحداً على الأقل."},
	"v_pattern_bad": {"This pattern is not accepted: use plain letters, digits and simple groups.", "Dieses Muster wird nicht akzeptiert: Verwenden Sie einfache Buchstaben, Ziffern und einfache Gruppen.", "هذا النمط غير مقبول: استخدم حروفاً وأرقاماً ومجموعات بسيطة."},
	"v_sensitive":   {"Pick what to look for.", "Wählen Sie, wonach gesucht wird.", "اختر ما تريد البحث عنه."},
	"v_attachment":  {"Add at least one file type.", "Fügen Sie mindestens einen Dateityp hinzu.", "أضف نوع ملف واحداً على الأقل."},
	"v_target":      {"Say who should be told.", "Geben Sie an, wer informiert werden soll.", "حدد من يجب إبلاغه."},
	"v_channels":    {"Pick at least one channel.", "Wählen Sie mindestens einen Kanal.", "اختر قناة واحدة على الأقل."},
	"v_line_long":   {"One line is too long. Keep each line under 512 characters.", "Eine Zeile ist zu lang. Halten Sie jede Zeile unter 512 Zeichen.", "أحد الأسطر طويل جداً. أبقِ كل سطر دون 512 حرفاً."},
	"v_too_many":    {"There are too many lines. Keep it to 128 or fewer.", "Es sind zu viele Zeilen. Beschränken Sie sich auf höchstens 128.", "عدد الأسطر كبير جداً. اكتفِ بـ 128 سطراً أو أقل."},
	"v_invalid":     {"Check this field and try again.", "Prüfen Sie dieses Feld und versuchen Sie es erneut.", "تحقق من هذا الحقل وحاول مجدداً."},
}
