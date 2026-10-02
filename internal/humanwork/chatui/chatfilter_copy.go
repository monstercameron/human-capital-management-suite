package chatui

import "strings"

func FilterBlockedExplanation(locale string) string {
	return chatfilterText(Model{Locale: locale}, "blocked")
}

func chatfilterText(m Model, key string) string {
	copy := map[string][3]string{
		"hard":      {"Enforce in direct messages too (workspace administrators only)", "Auch in Direktnachrichten anwenden (nur Arbeitsbereichsadministratoren)", "طبّق أيضاً في الرسائل المباشرة (لمسؤولي مساحة العمل فقط)"},
		"new":       {"Create another filter", "Weiteren Filter erstellen", "أنشئ مرشحاً آخر"},
		"manage":    {"Manage filters", "Filter verwalten", "أدر المرشحات"},
		"direct":    {"Direct messages follow only the workspace's hard rules.", "Für Direktnachrichten gelten nur verbindliche Arbeitsbereichsregeln.", "تخضع الرسائل المباشرة للقواعد الإلزامية لمساحة العمل فقط."},
		"edit":      {"Edit filter", "Filter bearbeiten", "عدّل المرشح"},
		"profanity": {"Profanity", "Flüche", "الألفاظ النابية"}, "slurs": {"Slurs", "Abwertende Ausdrücke", "الإهانات التمييزية"}, "harassment": {"Harassment", "Belästigung", "المضايقة"},
		"title": {"Filters", "Filter", "المرشحات"}, "hint": {"Built-in lists are off until enabled. Workspace rules still apply in every channel.", "Integrierte Listen sind zunächst ausgeschaltet. Arbeitsbereichsregeln gelten in jedem Kanal.", "القوائم المدمجة معطلة حتى تفعيلها. تسري قواعد مساحة العمل في كل قناة."},
		"empty":   {"No custom filters yet. Create a rule below.", "Noch keine eigenen Filter. Erstellen Sie unten eine Regel.", "لا توجد مرشحات مخصصة بعد. أنشئ قاعدة أدناه."},
		"loading": {"Loading filters. Please wait.", "Filter werden geladen. Bitte warten.", "جار تحميل المرشحات. يرجى الانتظار."}, "error": {"Filters could not load. Try again.", "Filter konnten nicht geladen werden. Versuchen Sie es erneut.", "تعذر تحميل المرشحات. حاول مجدداً."},
		"retry": {"Retry loading", "Erneut laden", "أعد التحميل"}, "save": {"Save new version", "Neue Version speichern", "احفظ إصداراً جديداً"}, "try": {"Try it", "Ausprobieren", "جرّبها"},
		"name": {"Rule name", "Regelname", "اسم القاعدة"}, "version": {"Version, for example 1.0.0", "Version, zum Beispiel 1.0.0", "الإصدار، مثلاً 1.0.0"},
		"kind": {"Match kind", "Filterart", "نوع المطابقة"}, "words": {"Words or phrases", "Wörter oder Ausdrücke", "كلمات أو عبارات"}, "pattern": {"Restricted pattern", "Eingeschränktes Muster", "نمط مقيد"}, "detector": {"Sensitive data detector", "Erkennung sensibler Daten", "كاشف البيانات الحساسة"}, "attachment": {"Attachment type", "Anhangtyp", "نوع المرفق"},
		"match":  {"Terms, patterns or detector name, one per line", "Begriffe, Muster oder Detektorname, einer pro Zeile", "مصطلحات أو أنماط أو اسم الكاشف، واحد في كل سطر"},
		"action": {"Action", "Aktion", "الإجراء"}, "block": {"Block and explain", "Blockieren und erklären", "امنع واشرح"}, "mask": {"Mask for readers", "Für Leser maskieren", "احجب عن القراء"}, "flag": {"Flag for review", "Zur Prüfung markieren", "علّم للمراجعة"}, "notify": {"Notify a channel or agent", "Kanal oder Agent benachrichtigen", "أبلغ قناة أو وكيلاً"},
		"target": {"Notification destination", "Benachrichtigungsziel", "وجهة الإبلاغ"}, "scope": {"Channel scope; leave empty for workspace", "Kanalumfang; leer für Arbeitsbereich", "نطاق القنوات؛ اتركه فارغاً لمساحة العمل"},
		"roles": {"Exempt roles, one per line", "Ausgenommene Rollen, eine pro Zeile", "الأدوار المستثناة، واحد في كل سطر"}, "agents": {"Exempt agents, one per line", "Ausgenommene Agenten, einer pro Zeile", "الوكلاء المستثنون، واحد في كل سطر"},
		"sample": {"Sample message", "Beispielnachricht", "رسالة تجريبية"}, "dry": {"Record only for seven days before enforcing", "Sieben Tage nur protokollieren, dann anwenden", "سجّل فقط لسبعة أيام قبل التطبيق"},
		"enable": {"Enable filter", "Filter aktivieren", "فعّل المرشح"}, "disable": {"Disable filter", "Filter deaktivieren", "عطّل المرشح"}, "removed": {"removed word", "entferntes Wort", "كلمة محذوفة"},
		"blocked": {"This message was not sent: it contains a word this workspace does not allow.", "Diese Nachricht wurde nicht gesendet: Sie enthält ein Wort, das dieser Arbeitsbereich nicht erlaubt.", "لم تُرسل هذه الرسالة: فهي تحتوي على كلمة لا تسمح بها مساحة العمل هذه."},
		"preview": {"Readers see:", "Leser sehen:", "يرى القراء:"}, "allowed": {"This sample can be sent.", "Dieses Beispiel kann gesendet werden.", "يمكن إرسال هذا المثال."},
		"invalid":    {"Check the rule name, version, kind and match terms, then try again.", "Prüfen Sie Namen, Version, Art und Begriffe und versuchen Sie es erneut.", "تحقق من الاسم والإصدار والنوع والمصطلحات ثم حاول مجدداً."},
		"permission": {"Only people with Manage filters permission can change these rules.", "Nur Personen mit der Berechtigung zum Verwalten von Filtern können diese Regeln ändern.", "يمكن فقط لمن لديهم صلاحية إدارة المرشحات تغيير هذه القواعد."},
	}
	values, ok := copy[key]
	if !ok {
		return ""
	}
	index := 0
	if strings.HasPrefix(m.Locale, "de") {
		index = 1
	}
	if strings.HasPrefix(m.Locale, "ar") {
		index = 2
	}
	return chatbug039Text(key, values[index], values[0])
}
