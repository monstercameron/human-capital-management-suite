package productui

// peoplePersonTranslations supplies reviewed directory and profile language to
// the shared product catalog. Employee names, job titles, teams, and locations
// remain server-projected data; these messages cover only product-owned copy.
func peoplePersonTranslations(locale string) map[string]string {
	switch locale {
	case "de-DE":
		return map[string]string{
			"people.scope":         "Es werden nur Mitarbeitende angezeigt, die Sie sehen dürfen.",
			"page.people.subtitle": "Finden Sie Mitarbeitende und starten Sie Aufgaben, für die Sie Zugriff haben.",
			"people.table_aria":    "Für Sie sichtbare Mitarbeitende",
			"people.column.person": "Person", "people.column.role": "Position", "people.column.team": "Team", "people.column.manager": "Führungskraft", "people.column.location": "Standort",
			"people.pages": "Seiten des Mitarbeiterverzeichnisses", "people.page_count": "Seite {page} von {pages}",
			"people.empty_title": "Keine Mitarbeitenden gefunden", "people.empty_detail": "Suchen Sie nach Name, Position, Team, Standort, Personalnummer oder Stellenkennung innerhalb Ihres Zugriffs.", "people.clear_filter": "Filter löschen",
			"person.unavailable": "Person nicht verfügbar", "person.unavailable_detail": "Diese Person ist im Mitarbeiterverzeichnis, auf das Sie Zugriff haben, nicht enthalten.", "person.return_directory": "Zurück zum Mitarbeiterverzeichnis",
			"person.summary": "Personenübersicht", "person.worker_profile": "Beschäftigtenprofil", "person.source": "Quelle · {source}",
			"person.employment_details": "Beschäftigungsdaten", "person.sensitive": "Vertraulich", "person.private_data": "Private Daten", "person.private_detail": "Es werden nur Daten angezeigt, die Sie sehen dürfen. Kontaktangaben, Ausweisdaten, Bankdaten und Privatadresse bleiben geschützt.",
			"person.visible_scope": "Für Sie sichtbar", "person.employment_overview": "Beschäftigung im Überblick", "person.employment_overview_detail": "Aktuelle Angaben zu Beschäftigung und Stelle.",
			"person.worker_number": "Personalnummer", "person.job_code": "Stellenkennung", "person.job_level": "Stufe", "person.hire_date": "Eintrittsdatum", "person.employment_type": "Beschäftigungsart", "person.time_type": "Arbeitszeitmodell", "person.record_source": "Datensatzquelle", "person.record_created": "Datensatz angelegt",
			"person.organization": "Organisation", "person.organization_detail": "Führungskraft, Zuordnung und organisatorischer Zusammenhang.", "person.organization_unit": "Organisationseinheit", "person.manager": "Führungskraft", "person.position_id": "Positionsnummer", "person.work_location": "Arbeitsort", "person.company": "Unternehmen", "person.business_unit": "Geschäftsbereich", "person.cost_center": "Kostenstelle", "person.work_arrangement": "Arbeitsmodell",
			"person.compensation": "Vergütung", "person.compensation_detail": "Vergütungsangaben, die Sie für diese Aufgabe sehen dürfen.", "person.base_pay": "Grundgehalt", "person.bonus_target": "Bonusziel", "person.pay_zone": "Vergütungszone", "person.pay_frequency": "Auszahlungsrhythmus",
			"person.personal_information": "Persönliche Daten", "person.personal_hidden": "Persönliche Kennungen · standardmäßig verborgen", "person.restricted": "Eingeschränkt", "person.legal_name": "Amtlicher Name", "person.preferred_name": "Bevorzugter Name", "person.worker_id": "Interne Personalnummer", "person.worker_ref": "Stabile Beschäftigtenreferenz", "person.history_detail": "Abgeschlossene, abgelehnte und fehlgeschlagene Abläufe für {name}.",
			"person.fact_status.present": "Verfügbar", "person.fact_status.missing": "Nicht angegeben", "person.fact_status.unknown": "Unbekannt", "person.fact_status.withheld": "Eingeschränkt",
			"workflow.available_aria": "Verfügbare Abläufe", "workflow.filter_placeholder": "Abläufe filtern", "workflow.filter_aria": "Verfügbare Abläufe filtern", "workflow.find": "Ablauf finden", "workflow.filter": "Filtern", "workflow.start_named": "{name} starten",
			"workflow.none": "Keine passenden Abläufe", "workflow.none_detail": "Suchen Sie nach einem Ablauf, einer Kategorie oder einem Ergebnis.", "workflow.unavailable": "Keine Abläufe verfügbar", "workflow.unavailable_detail": "Mit Ihrem aktuellen Zugriff können Sie für diese Person keinen Ablauf starten.", "workflow.start": "Ablauf starten",
			"history.none": "Keine Abläufe gefunden", "history.search_aria": "Ablaufhistorie durchsuchen", "history.outcome_aria": "Ablaufhistorie nach Ergebnis filtern", "history.none_detail": "Keine vergangenen Abläufe passen zu dieser Ansicht.", "history.any_year": "Beliebiges Wirksamkeitsjahr", "history.year_aria": "Ablaufhistorie nach Wirksamkeitsjahr filtern", "history.all_outcomes": "Alle Ergebnisse", "history.completed": "Abgeschlossen", "history.rejected": "Abgelehnt", "history.failed": "Fehlgeschlagen", "history.apply": "Filter anwenden", "history.find": "Vergangene Abläufe suchen", "history.clear": "Filter löschen", "history.empty_terminal": "Abgeschlossene, abgelehnte und fehlgeschlagene Abläufe erscheinen hier, sobald ein Ergebnis vorliegt.",
			"history.column_employee": "Mitarbeitende", "history.column_workflow": "Ablauf", "history.column_change": "Änderung", "history.column_closed": "Geschlossen", "history.column_outcome": "Ergebnis",
		}
	case "ar":
		return map[string]string{
			"page.history.subtitle": "راجع مسارات العمل المكتملة والمرفوضة والفاشلة.",
			"people.scope":          "يظهر هنا الموظفون الذين يُسمح لك برؤيتهم فقط.",
			"people.range":          "{first}–{last} من {total}",
			"work.past":             "مسارات العمل السابقة",
			"nav.support":           "المساعدة والإعدادات", "nav.favorite_add": "إضافة {label} إلى المفضلة", "nav.favorite_remove": "إزالة {label} من المفضلة", "nav.admin_overview": "نظرة عامة على الإدارة", "nav.work_queue": "قائمة المهام",
			"page.people.subtitle": "ابحث عن الموظفين وابدأ المهام التي تملك صلاحية إدارتها.",
			"people.table_aria":    "الموظفون الظاهرون لك",
			"people.column.person": "الشخص", "people.column.role": "الوظيفة", "people.column.team": "الفريق", "people.column.manager": "المدير", "people.column.location": "الموقع",
			"people.pages": "صفحات دليل الموظفين", "people.page_count": "الصفحة {page} من {pages}",
			"people.empty_title": "لم يُعثر على موظفين", "people.empty_detail": "ابحث بالاسم أو الوظيفة أو الفريق أو الموقع أو رقم الموظف أو رمز الوظيفة ضمن نطاق وصولك.", "people.clear_filter": "مسح التصفية",
			"person.unavailable": "الشخص غير متاح", "person.unavailable_detail": "هذا الشخص غير موجود في سجلات الموظفين الظاهرة لك.", "person.return_directory": "العودة إلى دليل الموظفين",
			"person.summary": "ملخص الموظف", "person.worker_profile": "ملف الموظف", "person.source": "المصدر · {source}",
			"person.employment_details": "بيانات التوظيف", "person.sensitive": "حساس", "person.private_data": "بيانات خاصة", "person.private_detail": "تُعرض المعلومات التي يُسمح لك بالاطلاع عليها فقط. وتبقى بيانات الاتصال والهوية الحكومية والبنك وعنوان المنزل محمية.",
			"person.visible_scope": "ظاهر لك", "person.employment_overview": "نظرة عامة على التوظيف", "person.employment_overview_detail": "بيانات التوظيف والوظيفة الحالية.",
			"person.worker_number": "رقم الموظف", "person.job_code": "رمز الوظيفة", "person.job_level": "المستوى الوظيفي", "person.hire_date": "تاريخ التعيين", "person.employment_type": "نوع التوظيف", "person.time_type": "نظام الدوام", "person.record_source": "مصدر السجل", "person.record_created": "تاريخ إنشاء السجل",
			"person.organization": "المؤسسة", "person.organization_detail": "خط الإشراف والتعيين والسياق التنظيمي.", "person.organization_unit": "الوحدة التنظيمية", "person.manager": "المدير", "person.position_id": "رقم المنصب", "person.work_location": "موقع العمل", "person.company": "الشركة", "person.business_unit": "وحدة الأعمال", "person.cost_center": "مركز التكلفة", "person.work_arrangement": "نظام العمل",
			"person.compensation": "التعويضات", "person.compensation_detail": "بيانات التعويضات المسموح لك برؤيتها لهذه المهمة.", "person.base_pay": "الراتب الأساسي", "person.bonus_target": "المكافأة المستهدفة", "person.pay_zone": "نطاق الأجور", "person.pay_frequency": "وتيرة صرف الراتب",
			"person.personal_information": "البيانات الشخصية", "person.personal_hidden": "المعرّفات الشخصية · مخفية افتراضيًا", "person.restricted": "مقيّد", "person.legal_name": "الاسم القانوني", "person.preferred_name": "الاسم المفضل", "person.worker_id": "معرّف الموظف الداخلي", "person.worker_ref": "مرجع الموظف الثابت", "person.history_detail": "مسارات العمل المكتملة والمرفوضة والفاشلة المسجلة لـ {name}.",
			"person.fact_status.present": "متاح", "person.fact_status.missing": "غير مقدّم", "person.fact_status.unknown": "غير معروف", "person.fact_status.withheld": "مقيّد",
			"workflow.available_aria": "مسارات العمل المتاحة", "workflow.filter_placeholder": "تصفية مسارات العمل", "workflow.filter_aria": "تصفية مسارات العمل المتاحة", "workflow.find": "العثور على مسار عمل", "workflow.filter": "تصفية", "workflow.start_named": "بدء {name}",
			"workflow.none": "لا توجد مسارات عمل مطابقة", "workflow.none_detail": "جرّب اسم مسار العمل أو فئته أو نتيجته.", "workflow.unavailable": "لا توجد مسارات عمل متاحة", "workflow.unavailable_detail": "لا يسمح وصولك الحالي ببدء مسار عمل لهذا الموظف.", "workflow.start": "بدء مسار عمل",
			"history.none": "لم يُعثر على سجلات لمسارات العمل", "history.search_aria": "البحث في سجل مسارات العمل", "history.outcome_aria": "تصفية سجل مسارات العمل حسب النتيجة", "history.none_detail": "لا تطابق مسارات العمل السابقة هذا العرض.", "history.any_year": "أي سنة لسريان القرار", "history.year_aria": "تصفية سجل مسارات العمل حسب سنة السريان", "history.all_outcomes": "كل النتائج", "history.completed": "مكتمل", "history.rejected": "مرفوض", "history.failed": "فشل", "history.apply": "تطبيق عوامل التصفية", "history.find": "البحث في مسارات العمل السابقة", "history.clear": "مسح عوامل التصفية", "history.empty_terminal": "ستظهر هنا مسارات العمل المكتملة والمرفوضة والفاشلة بعد تسجيل نتيجتها.",
			"history.global_title": "سجل مسارات العمل", "history.global_detail": "راجع نتائج مسارات العمل المنتهية للموظفين الظاهرين لك.", "history.promotion": "ترقية", "history.filtered_count": "{filtered} من أصل {total}",
			"history.all_people": "كل الموظفين", "history.person_aria": "تصفية سجل مسارات العمل حسب الموظف", "history.search_placeholder": "ابحث عن موظف أو مسار عمل أو تغيير", "history.search_person_placeholder": "ابحث عن مسار عمل أو تغيير أو نتيجة",
			"history.record": "السجل", "history.open": "فتح السجل", "history.closed": "أُغلق · {value}", "history.effective": "يسري · {value}", "history.pages": "صفحات سجل مسارات العمل",
			"history.column_employee": "الموظف", "history.column_workflow": "مسار العمل", "history.column_change": "التغيير", "history.column_closed": "أُغلق", "history.column_outcome": "النتيجة",
		}
	default:
		return map[string]string{
			"people.scope":                      "Only employees you can access are shown.",
			"person.employment_overview_detail": "Current employment and job details.",
			"person.compensation_detail":        "Compensation details available for this task.",
			"history.empty_terminal":            "Completed, rejected, and failed workflows will appear here after an outcome is recorded.",
		}
	}
}
