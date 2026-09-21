package productui

// homeOperationalTranslations is the German and Arabic copy for UXLIVE-030's
// operational Home: the drill-down fact labels and the exceptions card. The
// English source strings live in the reviewed catalog in i18n.go.
func homeOperationalTranslations(locale string) map[string]string {
	switch locale {
	case "de-DE":
		return map[string]string{
			"home.following": "Von Ihnen gestartete Anträge", "home.exceptions": "Mit Problem angehalten", "home.in_progress": "In Bearbeitung",
			"home.group_your_work": "Ihre Arbeit", "home.group_requests": "Für Sie sichtbare Anträge", "home.group_people": "Für Sie sichtbare Personen",
			"home.recent_requests_title": "Aktuelle Anträge", "home.row_updated": "Aktualisiert {date}", "workflow.promotion_in_progress_title": "Beförderung läuft", "home.recent_requests_description": "Offene Anträge, die Sie sehen können, zuletzt geänderte zuerst.",
			"home.eligible_workers": "Für eine Beförderung berechtigt", "home.exceptions_title": "Anträge mit Problemen",
			"home.exceptions_description": "Diese Anträge wurden angehalten: Der Vorschlag ist blockiert, der Ablauf ist fehlgeschlagen oder eine Korrektur ist erforderlich.",
			"home.exceptions_view_all":    "In Abläufen öffnen",
		}
	case "ar":
		return map[string]string{
			"home.following": "الطلبات التي بدأتها", "home.exceptions": "متوقفة بسبب مشكلة", "home.in_progress": "قيد التنفيذ",
			"home.group_your_work": "عملك", "home.group_requests": "الطلبات التي يمكنك رؤيتها", "home.group_people": "الأشخاص الذين يمكنك رؤيتهم",
			"home.recent_requests_title": "الطلبات الأخيرة", "home.row_updated": "حُدِّث {date}", "work.row_effective_date": "يسري من {date}", "workflow.promotion_in_progress_title": "ترقية قيد التنفيذ", "home.recent_requests_description": "الطلبات المفتوحة التي يمكنك رؤيتها، الأحدث تغييرًا أولًا.",
			"home.eligible_workers": "مؤهلون للترقية", "home.exceptions_title": "طلبات بها مشكلات",
			"home.exceptions_description": "توقفت هذه الطلبات: الاقتراح محظور أو فشل التنفيذ أو يلزم إصلاح.",
			"home.exceptions_view_all":    "فتح في الرحلات",
		}
	default:
		// English is the reviewed catalog in i18n.go.
		return nil
	}
}
