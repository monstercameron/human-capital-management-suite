package chatui

import "strings"

func personaActivityLabel(locale, activity string) string {
	key := strings.ToLower(strings.TrimSpace(activity))
	copy := map[string][3]string{
		"request unavailable": {"Request unavailable", "Anfrage nicht verfügbar", "الطلب غير متاح"},
		"published profile is not currently available with your access.": {"Published profile is not currently available with your access.", "Das veröffentlichte Profil ist mit Ihrem aktuellen Zugriff nicht verfügbar.", "الملف المنشور غير متاح حالياً بصلاحياتك الحالية."},
		"act beyond your current access":                                 {"Act beyond your current access", "Über Ihren aktuellen Zugriff hinaus handeln", "التصرف خارج نطاق صلاحياتك الحالية"},
		"use skills outside this published version":                      {"Use skills outside this published version", "Fähigkeiten außerhalb dieser veröffentlichten Version verwenden", "استخدام مهارات خارج هذا الإصدار المنشور"},
		"change governed records":                                        {"Change governed records", "Governancepflichtige Datensätze ändern", "تغيير السجلات الخاضعة للحوكمة"},
		"write to external systems":                                      {"Write to external systems", "In externe Systeme schreiben", "الكتابة إلى أنظمة خارجية"},
		"open private reply":                                             {"Open private reply", "Private Antwort öffnen", "فتح الرد الخاص"},
		"admission":                                                      {"Checking your access", "Ihr Zugriff wird geprüft", "جارٍ التحقق من صلاحياتك"},
		"preparing context":                                              {"Preparing context", "Kontext wird vorbereitet", "جارٍ إعداد السياق"},
		"preparing answer":                                               {"Preparing answer", "Antwort wird vorbereitet", "جارٍ إعداد الإجابة"},
		"validating answer":                                              {"Checking the answer", "Antwort wird geprüft", "جارٍ التحقق من الإجابة"},
		"reading sources":                                                {"Reading sources", "Quellen werden gelesen", "جارٍ قراءة المصادر"},
		"delivering answer":                                              {"Delivering answer", "Antwort wird zugestellt", "جارٍ إرسال الإجابة"},
		"delivered":                                                      {"Delivered", "Zugestellt", "تم الإرسال"},
		"the persona could not finish this request.":                     {"The agent could not finish this request.", "Der Agent konnte diese Anfrage nicht abschließen.", "تعذر على الوكيل إكمال هذا الطلب."},
		"this persona is unavailable for this request with your current access.": {"This request is unavailable with your current access.", "Diese Anfrage ist mit Ihrem aktuellen Zugriff nicht verfügbar.", "هذا الطلب غير متاح بصلاحياتك الحالية."},
	}
	labels, ok := copy[key]
	if !ok {
		return activity
	}
	switch {
	case strings.HasPrefix(strings.ToLower(locale), "de"):
		return labels[1]
	case strings.HasPrefix(strings.ToLower(locale), "ar"):
		return labels[2]
	default:
		return labels[0]
	}
}
