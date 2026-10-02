package productui

import "github.com/monstercameron/human-capital-management-suite/internal/experience/localize"

func withAgentUXPage5Messages(catalog map[string]map[string]localize.Message) map[string]map[string]localize.Message {
	for locale, messages := range map[string]map[string]localize.Message{
		"en-US": {
			"agents.admin_navigation": {Text: "Agent pages"}, "agents.nav.ask": {Text: "Ask"}, "agents.nav.setup": {Text: "Setup"}, "agents.nav.operations": {Text: "Operations"},
			"agents.filter.active_label": {Text: "Active"}, "agents.filter.completed_label": {Text: "Completed"}, "agents.filter.failed_label": {Text: "Failed"}, "agents.task_count": {Text: "Task count"},
			"agents.documents_empty": {Text: "No documents match. You can only add documents you can read."}, "agents.document_done": {Text: "Done"}, "agents.document_remove_named": {Text: "Remove {title}"},
			"agents.document_owner_you": {Text: "You"}, "agents.document_owner_unknown": {Text: "Unknown owner"}, "agents.document_folder_none": {Text: "No folder"}, "agents.document_updated_unknown": {Text: "date unavailable"}, "agents.document_updated": {Text: "updated {date}"},
			"agents.no_documents_used": {Text: "No documents were used for this answer."}, "agents.used_documents": {Text: "Sources:"}, "agents.documents_used": {Text: "Sources:"}, "agents.read": {Text: "Attached:"}, "agents.retry_not_helpful": {Text: "This request can no longer be retried. Ask it again above."},
			"agents.page_title": {Text: "Agents"}, "agents.composer_placeholder": {Text: "For example: How many vacation days carry over to next year?"},
			"agents.setup":         {Text: "Set up agents"},
			"agents.composer_help": {Text: "Write your question or describe the work you need."}, "agents.general_agent_purpose": {Text: "Answers everyday questions. It does not look anything up in company documents unless you add them below."},
			"agents.only_general_agent": {Text: "Only the General agent is available to you. Ask your administrator if you need another agent."},
			"agents.actions_help":       {Text: "Ask: get an answer here, usually within a minute. Plan a longer task: the agent shows you a plan to approve, then works in the background and lists the result under Tasks."},
			"agents.unavailable_title":  {Text: "Agents could not be loaded"}, "agents.load_failed_detail": {Text: "Try again. If the problem continues, contact your workspace administrator."},
			"agents.chat_history_note": {Text: "Questions you ask in Chat are answered in Chat and are not listed here."},
			"agents.agent_failed":      {Text: "{agent}"}, "agents.agent_active": {Text: "{agent} · {status}"},
			"agents.task_failed_reassurance": {Text: "The agent stopped before it could answer. Nothing was changed."},
			"agents.failure_timeout":         {Text: "It took too long."}, "agents.failure_document_access": {Text: "It was not allowed to read one of the documents."}, "agents.failure_service": {Text: "The agent service was unavailable."}, "agents.failure_unknown": {Text: "Something went wrong on our side."},
			"agents.ask_again": {Text: "Ask again"}, "agents.detail_meta": {Text: "{agent} · {time} · {duration}"}, "agents.detail_took_under_minute": {Text: "took under a minute"}, "agents.detail_took_minutes": {Text: "took {count} minutes"},
			"agents.answer_ready": {Text: "Answer ready. Open it to read the full answer."}, "agents.ask_in_chat": {Text: "Ask in Chat instead"},
			"agents.documents_label": {Text: "Documents to read (optional)"}, "agents.documents_access_help": {Text: "Up to 5. The agent only reads documents you can already read."},
			"agents.documents_count": {Text: "{count} of {limit} added"},
			"agents.time_now":        {Text: "just now"}, "agents.time_yesterday": {Text: "yesterday"},
			"agents.time_minutes": {Plural: map[string]string{"one": "{count} minute ago", "other": "{count} minutes ago"}},
			"agents.time_hours":   {Plural: map[string]string{"one": "{count} hour ago", "other": "{count} hours ago"}},
			"agents.time_days":    {Plural: map[string]string{"one": "{count} day ago", "other": "{count} days ago"}},
			"agents.started":      {Text: "Started {time}"}, "agents.finished": {Text: "Finished {time}"}, "agents.updated": {Text: "Updated {time}"},
			"agents.documents_not_used_count": {Plural: map[string]string{"one": "{count} document was not used because you cannot read it.", "other": "{count} documents were not used because you cannot read them."}},
		},
		"de-DE": {
			"agents.admin_navigation": {Text: "Agentenseiten"}, "agents.nav.ask": {Text: "Fragen"}, "agents.nav.setup": {Text: "Einrichten"}, "agents.nav.operations": {Text: "Betrieb"},
			"agents.filter.active_label": {Text: "Aktiv"}, "agents.filter.completed_label": {Text: "Abgeschlossen"}, "agents.filter.failed_label": {Text: "Fehlgeschlagen"}, "agents.task_count": {Text: "Aufgabenanzahl"},
			"agents.documents_empty": {Text: "Keine passenden Dokumente. Sie können nur Dokumente hinzufügen, die Sie lesen dürfen."}, "agents.document_done": {Text: "Fertig"}, "agents.document_remove_named": {Text: "{title} entfernen"},
			"agents.document_owner_you": {Text: "Sie"}, "agents.document_owner_unknown": {Text: "Unbekannte Person"}, "agents.document_folder_none": {Text: "Kein Ordner"}, "agents.document_updated_unknown": {Text: "Datum nicht verfügbar"}, "agents.document_updated": {Text: "aktualisiert {date}"},
			"agents.no_documents_used": {Text: "Für diese Antwort wurden keine Dokumente verwendet."}, "agents.used_documents": {Text: "Quellen:"}, "agents.documents_used": {Text: "Quellen:"}, "agents.read": {Text: "Angehängt:"}, "agents.retry_not_helpful": {Text: "Diese Anfrage kann nicht erneut versucht werden. Stellen Sie sie oben noch einmal."},
			"agents.answered_by": {Text: "Agent: {agent}"}, "agents.page_title": {Text: "Agenten"}, "agents.composer_placeholder": {Text: "Zum Beispiel: Wie viele Urlaubstage kann ich ins nächste Jahr übertragen?"},
			"agents.setup":         {Text: "Agenten einrichten"},
			"agents.composer_help": {Text: "Schreiben Sie Ihre Frage oder beschreiben Sie die benötigte Arbeit."}, "agents.general_agent_purpose": {Text: "Beantwortet alltägliche Fragen. Er schlägt nichts in Unternehmensdokumenten nach, außer Sie fügen sie unten hinzu."},
			"agents.only_general_agent": {Text: "Nur der Allgemeine Agent ist für Sie verfügbar. Fragen Sie Ihre Administration, wenn Sie einen anderen Agenten benötigen."},
			"agents.actions_help":       {Text: "Fragen: Sie erhalten hier meist innerhalb einer Minute eine Antwort. Längere Aufgabe planen: Der Agent zeigt Ihnen zuerst einen Plan zur Freigabe, arbeitet dann im Hintergrund und listet das Ergebnis unter Aufgaben auf."},
			"agents.unavailable_title":  {Text: "Agenten konnten nicht geladen werden"}, "agents.load_failed_detail": {Text: "Versuchen Sie es erneut. Wenn das Problem bleibt, wenden Sie sich an Ihre Workspace-Administration."},
			"agents.chat_history_note": {Text: "Fragen, die Sie im Chat stellen, werden dort beantwortet und hier nicht aufgeführt."},
			"agents.agent_failed":      {Text: "{agent}"}, "agents.agent_active": {Text: "{agent} · {status}"},
			"agents.task_failed_reassurance": {Text: "Der Agent wurde angehalten, bevor er antworten konnte. Es wurde nichts geändert."},
			"agents.failure_timeout":         {Text: "Die Bearbeitung dauerte zu lange."}, "agents.failure_document_access": {Text: "Der Agent durfte eines der Dokumente nicht lesen."}, "agents.failure_service": {Text: "Der Agentendienst war nicht verfügbar."}, "agents.failure_unknown": {Text: "Auf unserer Seite ist etwas schiefgelaufen."},
			"agents.ask_again": {Text: "Erneut fragen"}, "agents.detail_meta": {Text: "{agent} · {time} · {duration}"}, "agents.detail_took_under_minute": {Text: "dauerte weniger als eine Minute"}, "agents.detail_took_minutes": {Text: "dauerte {count} Minuten"},
			"agents.answer_ready": {Text: "Die Antwort ist fertig. Öffnen Sie sie, um sie vollständig zu lesen."}, "agents.ask_in_chat": {Text: "Stattdessen im Chat fragen"},
			"agents.documents_label": {Text: "Zu lesende Dokumente (optional)"}, "agents.documents_access_help": {Text: "Bis zu 5. Der Agent liest nur Dokumente, die Sie bereits lesen können."},
			"agents.documents_count": {Text: "{count} von {limit} hinzugefügt"},
			"agents.time_now":        {Text: "gerade eben"}, "agents.time_yesterday": {Text: "gestern"},
			"agents.time_minutes": {Plural: map[string]string{"one": "vor {count} Minute", "other": "vor {count} Minuten"}},
			"agents.time_hours":   {Plural: map[string]string{"one": "vor {count} Stunde", "other": "vor {count} Stunden"}},
			"agents.time_days":    {Plural: map[string]string{"one": "vor {count} Tag", "other": "vor {count} Tagen"}},
			"agents.started":      {Text: "Gestartet {time}"}, "agents.finished": {Text: "Beendet {time}"}, "agents.updated": {Text: "Aktualisiert {time}"},
			"agents.documents_not_used_count": {Plural: map[string]string{"one": "{count} Dokument wurde nicht verwendet, weil Sie es nicht lesen können.", "other": "{count} Dokumente wurden nicht verwendet, weil Sie sie nicht lesen können."}},
		},
		"ar": {
			"agents.admin_navigation": {Text: "صفحات الوكلاء"}, "agents.nav.ask": {Text: "اسأل"}, "agents.nav.setup": {Text: "الإعداد"}, "agents.nav.operations": {Text: "العمليات"},
			"agents.filter.active_label": {Text: "نشطة"}, "agents.filter.completed_label": {Text: "مكتملة"}, "agents.filter.failed_label": {Text: "فاشلة"}, "agents.task_count": {Text: "عدد المهام"},
			"agents.documents_empty": {Text: "لا توجد مستندات مطابقة. يمكنك إضافة المستندات التي تستطيع قراءتها فقط."}, "agents.document_done": {Text: "تم"}, "agents.document_remove_named": {Text: "إزالة {title}"},
			"agents.document_owner_you": {Text: "أنت"}, "agents.document_owner_unknown": {Text: "مالك غير معروف"}, "agents.document_folder_none": {Text: "بلا مجلد"}, "agents.document_updated_unknown": {Text: "التاريخ غير متاح"}, "agents.document_updated": {Text: "حُدّث {date}"},
			"agents.no_documents_used": {Text: "لم تُستخدم أي مستندات لهذه الإجابة."}, "agents.used_documents": {Text: "المصادر:"}, "agents.documents_used": {Text: "المصادر:"}, "agents.read": {Text: "المرفقات:"}, "agents.retry_not_helpful": {Text: "لم يعد من الممكن إعادة محاولة هذا الطلب. اطرحه مرة أخرى أعلاه."},
			"agents.page_title": {Text: "الوكلاء"}, "agents.composer_placeholder": {Text: "مثال: كم يومًا من الإجازة يمكن ترحيله إلى العام المقبل؟"},
			"agents.setup":         {Text: "إعداد الوكلاء"},
			"agents.composer_help": {Text: "اكتب سؤالك أو صف العمل الذي تحتاج إليه."}, "agents.general_agent_purpose": {Text: "يجيب عن الأسئلة اليومية. لا يبحث في مستندات الشركة إلا إذا أضفتها أدناه."},
			"agents.only_general_agent": {Text: "الوكيل العام وحده متاح لك. اطلب من المسؤول وكيلاً آخر إذا كنت بحاجة إليه."},
			"agents.actions_help":       {Text: "اسأل: احصل على إجابة هنا، عادةً خلال دقيقة. خطط لمهمة أطول: يعرض الوكيل خطة للموافقة عليها، ثم يعمل في الخلفية ويسرد النتيجة ضمن المهام."},
			"agents.unavailable_title":  {Text: "تعذر تحميل الوكلاء"}, "agents.load_failed_detail": {Text: "حاول مرة أخرى. إذا استمرت المشكلة، فتواصل مع مسؤول مساحة العمل."},
			"agents.chat_history_note": {Text: "تُجاب الأسئلة التي تطرحها في الدردشة هناك ولا تُدرج هنا."},
			"agents.agent_failed":      {Text: "{agent}"}, "agents.agent_active": {Text: "{agent} · {status}"},
			"agents.task_failed_reassurance": {Text: "توقف الوكيل قبل أن يتمكن من الإجابة. لم يتغير شيء."},
			"agents.failure_timeout":         {Text: "استغرق الأمر وقتًا طويلاً."}, "agents.failure_document_access": {Text: "لم يُسمح له بقراءة أحد المستندات."},
			"agents.answer_ready": {Text: "الإجابة جاهزة. افتحها لقراءة الإجابة الكاملة."}, "agents.ask_in_chat": {Text: "اسأل في الدردشة بدلاً من ذلك"},
			"agents.documents_label": {Text: "مستندات للقراءة (اختياري)"}, "agents.documents_access_help": {Text: "حتى ٥ مستندات. لا يقرأ الوكيل إلا المستندات التي يمكنك قراءتها بالفعل."},
			"agents.documents_count": {Text: "تمت إضافة {count} من {limit}"},
			"agents.time_now":        {Text: "الآن"}, "agents.time_yesterday": {Text: "أمس"},
			"agents.time_minutes": {Plural: map[string]string{"one": "قبل دقيقة واحدة", "two": "قبل دقيقتين", "few": "قبل {count} دقائق", "many": "قبل {count} دقيقة", "other": "قبل {count} دقيقة"}},
			"agents.time_hours":   {Plural: map[string]string{"one": "قبل ساعة واحدة", "two": "قبل ساعتين", "few": "قبل {count} ساعات", "many": "قبل {count} ساعة", "other": "قبل {count} ساعة"}},
			"agents.time_days":    {Plural: map[string]string{"one": "قبل يوم واحد", "two": "قبل يومين", "few": "قبل {count} أيام", "many": "قبل {count} يومًا", "other": "قبل {count} يوم"}},
			"agents.started":      {Text: "بدأت {time}"}, "agents.finished": {Text: "انتهت {time}"}, "agents.updated": {Text: "حُدثت {time}"},
			"agents.documents_not_used_count": {Plural: map[string]string{"one": "لم يُستخدم مستند واحد لأنك لا تستطيع قراءته.", "two": "لم يُستخدم مستندان لأنك لا تستطيع قراءتهما.", "few": "لم تُستخدم {count} مستندات لأنك لا تستطيع قراءتها.", "many": "لم يُستخدم {count} مستندًا لأنك لا تستطيع قراءته.", "other": "لم يُستخدم {count} مستند لأنك لا تستطيع قراءته."}},
		},
	} {
		for key, message := range messages {
			catalog[locale][key] = message
		}
	}
	for key, message := range map[string]localize.Message{
		"agents.failure_service":          {Text: "\u062e\u062f\u0645\u0629 \u0627\u0644\u0648\u0643\u064a\u0644 \u063a\u064a\u0631 \u0645\u062a\u0627\u062d\u0629."},
		"agents.failure_unknown":          {Text: "\u062d\u062f\u062b \u062e\u0637\u0623 \u0645\u0646 \u062c\u0627\u0646\u0628\u0646\u0627."},
		"agents.ask_again":                {Text: "\u0627\u0637\u0631\u062d\u0647 \u0645\u0631\u0629 \u0623\u062e\u0631\u0649"},
		"agents.detail_meta":              {Text: "{agent} · {time} · {duration}"},
		"agents.detail_took_under_minute": {Text: "\u0627\u0633\u062a\u063a\u0631\u0642 \u0623\u0642\u0644 \u0645\u0646 \u062f\u0642\u064a\u0642\u0629"},
		"agents.detail_took_minutes":      {Text: "\u0627\u0633\u062a\u063a\u0631\u0642 {count} \u062f\u0642\u0627\u0626\u0642"},
	} {
		catalog["ar"][key] = message
	}
	return catalog
}
