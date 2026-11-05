package productui

import "strings"

// agent012StudioCopy is the Agent Studio (AGENT-012) vocabulary in English,
// German, and Arabic. It follows the AGENT2-019 feature-local copy precedent:
// a plain table resolved by the view locale, so Studio copy never depends on
// the shared catalog file owned by other lanes.
var agent012StudioCopy = map[string][3]string{
	"title":                   {"Agent Studio", "Agenten-Studio", "استوديو الوكلاء"},
	"subtitle":                {"Draft, validation, evaluation, and publication for one governed agent.", "Entwurf, Prüfung, Evaluierung und Veröffentlichung für einen geregelten Agenten.", "المسودة والتحقق والتقييم والنشر لوكيل واحد خاضع للحوكمة."},
	"steps_label":             {"Creation steps", "Erstellungsschritte", "خطوات الإنشاء"},
	"step.job":                {"Job", "Aufgabe", "المهمة"},
	"step.charter":            {"Charter", "Leitbild", "الميثاق"},
	"step.context":            {"Context", "Kontext", "السياق"},
	"step.tools":              {"Tools", "Werkzeuge", "الأدوات"},
	"step.triggers":           {"Triggers", "Auslöser", "المشغلات"},
	"step.budgets":            {"Budgets", "Budgets", "الميزانيات"},
	"step.authority":          {"Authority preview", "Berechtigungsvorschau", "معاينة الصلاحيات"},
	"step.sandbox":            {"Sandbox evidence", "Sandbox-Nachweis", "أدلة الاختبار"},
	"step.review":             {"Review and publish", "Prüfung und Veröffentlichung", "المراجعة والنشر"},
	"step.done":               {"done", "fertig", "مكتمل"},
	"step.current":            {"current", "aktuell", "الحالية"},
	"step.todo":               {"to do", "offen", "متبقية"},
	"state.draft":             {"Draft", "Entwurf", "مسودة"},
	"state.validated":         {"Validated", "Geprüft", "تم التحقق"},
	"state.review":            {"In review", "In Prüfung", "قيد المراجعة"},
	"state.published":         {"Published", "Veröffentlicht", "منشور"},
	"revision":                {"Revision", "Revision", "الإصدار"},
	"owner":                   {"Owner", "Verantwortliche Person", "المالك"},
	"section.charter":         {"Charter", "Leitbild", "الميثاق"},
	"section.context":         {"Context", "Kontext", "السياق"},
	"section.tools":           {"Tools", "Werkzeuge", "الأدوات"},
	"section.triggers":        {"Triggers", "Auslöser", "المشغلات"},
	"section.budgets":         {"Budgets", "Budgets", "الميزانيات"},
	"section.authority":       {"Authority preview", "Berechtigungsvorschau", "معاينة الصلاحيات"},
	"section.sandbox":         {"Sandbox evidence", "Sandbox-Nachweis", "أدلة الاختبار"},
	"section.review":          {"Review", "Prüfung", "المراجعة"},
	"section.publish":         {"Publication status", "Veröffentlichungsstatus", "حالة النشر"},
	"section.transfer":        {"Owner transfer", "Übertragung der Verantwortung", "نقل الملكية"},
	"charter.orgs":            {"Organizations", "Organisationen", "المنظمات"},
	"charter.juris":           {"Jurisdictions", "Rechtsräume", "الولايات القضائية"},
	"charter.data":            {"Data categories", "Datenkategorien", "فئات البيانات"},
	"charter.owner":           {"Escalation owner", "Eskalationsverantwortung", "مسؤول التصعيد"},
	"charter.success":         {"Success measures", "Erfolgskriterien", "مقاييس النجاح"},
	"context.sources":         {"Sources", "Quellen", "المصادر"},
	"context.chats":           {"Chat spaces", "Chatbereiche", "مساحات الدردشة"},
	"context.audience":        {"Audience", "Zielgruppe", "الجمهور"},
	"context.purpose":         {"Purpose", "Zweck", "الغرض"},
	"tools.requested":         {"Requested capabilities", "Angeforderte Fähigkeiten", "القدرات المطلوبة"},
	"triggers.modes":          {"Modes", "Modi", "الأوضاع"},
	"triggers.channels":       {"Channels", "Kanäle", "القنوات"},
	"triggers.reply":          {"Replies are", "Antworten sind", "الردود"},
	"budgets.cost":            {"Maximum cost (micro currency units)", "Maximale Kosten (Mikro-Währungseinheiten)", "الحد الأقصى للتكلفة (وحدات العملة الدقيقة)"},
	"budgets.input":           {"Maximum input tokens", "Maximale Eingabe-Token", "الحد الأقصى لرموز الإدخال"},
	"budgets.output":          {"Maximum output tokens", "Maximale Ausgabe-Token", "الحد الأقصى لرموز الإخراج"},
	"budgets.runs":            {"Maximum concurrent runs", "Maximale gleichzeitige Ausführungen", "الحد الأقصى للتشغيل المتزامن"},
	"budgets.autonomy":        {"Autonomy ceiling", "Autonomieobergrenze", "الحد الأقصى للاستقلالية"},
	"budgets.model":           {"Model policy", "Modellrichtlinie", "سياسة النموذج"},
	"authority.allowed":       {"Allowed", "Erlaubt", "مسموح"},
	"authority.denied":        {"Denied", "Verweigert", "مرفوض"},
	"authority.tools":         {"Tools", "Werkzeuge", "الأدوات"},
	"authority.sources":       {"Sources", "Quellen", "المصادر"},
	"authority.none_denied":   {"Nothing requested is denied.", "Nichts Angefordertes wird verweigert.", "لا شيء مطلوب مرفوض."},
	"authority.none":          {"No tools or sources requested yet.", "Noch keine Werkzeuge oder Quellen angefordert.", "لم تُطلب أي أدوات أو مصادر بعد."},
	"sandbox.digest":          {"Evaluation digest", "Evaluierungsdigest", "بصمة التقييم"},
	"sandbox.passed":          {"Passed and fresh", "Bestanden und aktuell", "ناجح وحديث"},
	"sandbox.not_passing":     {"Not passing or stale", "Nicht bestanden oder veraltet", "غير ناجح أو قديم"},
	"sandbox.absent":          {"No sandbox evaluation is bound to this draft yet.", "Diesem Entwurf ist noch keine Sandbox-Evaluierung zugeordnet.", "لا يوجد تقييم تجريبي مرتبط بهذه المسودة بعد."},
	"review.digest":           {"Review digest", "Prüfungsdigest", "بصمة المراجعة"},
	"review.reviewer":         {"Reviewer", "Prüfende Person", "المراجع"},
	"review.absent":           {"No review recorded yet. The author cannot review their own draft.", "Noch keine Prüfung erfasst. Die Autorin oder der Autor kann den eigenen Entwurf nicht prüfen.", "لم تُسجل أي مراجعة بعد. لا يمكن للمؤلف مراجعة مسودته بنفسه."},
	"review.label":            {"Reviewer sign-in name", "Anmeldename der prüfenden Person", "اسم تسجيل دخول المراجع"},
	"review.button":           {"Record review", "Prüfung erfassen", "تسجيل المراجعة"},
	"validate.button":         {"Validate draft", "Entwurf prüfen", "التحقق من المسودة"},
	"submit.button":           {"Request review", "Prüfung anfordern", "طلب المراجعة"},
	"publish.button":          {"Publish exact version", "Exakte Version veröffentlichen", "نشر النسخة المحددة"},
	"publish.help":            {"Publishes only the exact reviewed manifest, evaluation, and review digests shown here.", "Veröffentlicht nur genau die hier gezeigten geprüften Manifest-, Evaluierungs- und Prüfungsdigests.", "ينشر فقط البصمات المحددة المعروضة هنا للبيان والتقييم والمراجعة."},
	"publish.generation":      {"Publication generation", "Veröffentlichungsgeneration", "جيل النشر"},
	"publish.version":         {"Published manifest version", "Veröffentlichte Manifestversion", "إصدار البيان المنشور"},
	"publish.absent":          {"Not published yet.", "Noch nicht veröffentlicht.", "لم يُنشر بعد."},
	"transfer.label":          {"New owner sign-in name", "Anmeldename der neuen verantwortlichen Person", "اسم تسجيل دخول المالك الجديد"},
	"transfer.button":         {"Transfer ownership", "Verantwortung übertragen", "نقل الملكية"},
	"transfer.help":           {"Only the current owner can transfer. A pending review is cleared so the new owner requests a fresh review.", "Nur die aktuelle verantwortliche Person kann übertragen. Eine ausstehende Prüfung wird gelöscht, damit die neue Verantwortung eine neue Prüfung anfordert.", "يمكن للمالك الحالي فقط النقل. تُمسح أي مراجعة معلقة ليطلب المالك الجديد مراجعة جديدة."},
	"unavailable.badge":       {"Not available", "Nicht verfügbar", "غير متاح"},
	"unavailable.title":       {"Agent draft could not be loaded", "Agentenentwurf konnte nicht geladen werden", "تعذر تحميل مسودة الوكيل"},
	"unavailable.description": {"Try again. Nothing is published from this page until validation, review, and publication complete.", "Versuchen Sie es erneut. Von dieser Seite wird nichts veröffentlicht, bis Prüfung, Evaluierung und Veröffentlichung abgeschlossen sind.", "حاول مرة أخرى. لا يُنشر أي شيء من هذه الصفحة حتى تكتمل مراحل التحقق والمراجعة والنشر."},
}

func agent012StudioText(locale LocaleContext, key string) string {
	labels, ok := agent012StudioCopy[key]
	if !ok {
		return "⟦" + key + "⟧"
	}
	switch language := strings.ToLower(locale.Resolved); {
	case strings.HasPrefix(language, "de"):
		return labels[1]
	case strings.HasPrefix(language, "ar"):
		return labels[2]
	default:
		return labels[0]
	}
}
