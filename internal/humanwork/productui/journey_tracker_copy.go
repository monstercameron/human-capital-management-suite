package productui

// journeyTrackerTranslations supplies the reviewed German and Arabic copy
// for the Journeys tracker's filter (UXLIVE-031), the typed object identity
// header (UXLIVE-032), the People row's workflow state (UXLIVE-033) and the
// work filter strip's overflow control (REV-095-05). The English source
// strings live in the reviewed en-US catalog like every other key. Person
// names, references and statuses stay server-projected data.
func journeyTrackerTranslations(locale string) map[string]string {
	switch locale {
	case "de-DE":
		return map[string]string{
			"identity.reference": "{label} {reference}", "identity.name_reference_status": "{primary}, {reference}, {status}", "identity.name_pair": "{first}, {second}",
			"journey.card_title": "Beförderung für {name}", "journey.card_title_unnamed": "Beförderungsantrag",
			"journey.request_reference_label": "Antrag", "journey.copy_reference": "Antragsreferenz {reference} kopieren", "journey.copy": "Kopieren",
			"journey.filter_region": "Anträge finden", "journey.filter_toggle": "Filter", "journey.filter_toggle_count": "Filter ({count})", "journey.filter_search": "Person oder Antragsreferenz", "journey.filter_search_placeholder": "Name oder Referenz",
			"journey.filter_status": "Status", "journey.filter_status_all": "Alle Status", "journey.filter_status_open": "Offene Anträge",
			"journey.filter_from": "Aktualisiert ab", "journey.filter_to": "Aktualisiert bis",
			"journey.filter_sort": "Reihenfolge", "journey.filter_sort_recent": "Zuletzt aktualisiert zuerst", "journey.filter_sort_oldest": "Am längsten unverändert zuerst",
			"journey.filter_group": "Gruppieren nach", "journey.filter_group_person": "Person", "journey.filter_group_status": "Status", "journey.filter_group_none": "Keine Gruppierung",
			"journey.filter_result": "{shown} von {total} Anträgen", "journey.filter_empty_title": "Keine Anträge passen zu diesen Filtern",
			"journey.filter_empty_detail": "Versuchen Sie einen anderen Namen oder eine andere Referenz, erweitern Sie den Zeitraum oder löschen Sie die Filter.",
			"people.workflows_quiet":      "Kein Ablauf startbar", "people.open_promotion": "Beförderung öffnen", "people.start_promotion": "Beförderung starten",
			"work.more_filters": "Mehr", "work.more_filters_aria": "Weitere Arbeitsfilter",
		}
	case "ar":
		return map[string]string{
			"identity.reference": "{label} {reference}", "identity.name_reference_status": "{primary}، {reference}، {status}", "identity.name_pair": "{first}، {second}",
			"journey.card_title": "ترقية {name}", "journey.card_title_unnamed": "طلب ترقية",
			"journey.request_reference_label": "الطلب", "journey.copy_reference": "نسخ مرجع الطلب {reference}", "journey.copy": "نسخ",
			"journey.filter_region": "البحث عن الطلبات", "journey.filter_toggle": "عوامل التصفية", "journey.filter_toggle_count": "عوامل التصفية ({count})", "journey.filter_search": "الشخص أو مرجع الطلب", "journey.filter_search_placeholder": "الاسم أو المرجع",
			"journey.filter_status": "الحالة", "journey.filter_status_all": "كل الحالات", "journey.filter_status_open": "الطلبات المفتوحة",
			"journey.filter_from": "آخر تحديث من", "journey.filter_to": "آخر تحديث حتى",
			"journey.filter_sort": "الترتيب", "journey.filter_sort_recent": "الأحدث تحديثًا أولًا", "journey.filter_sort_oldest": "الأقدم تحديثًا أولًا",
			"journey.filter_group": "التجميع حسب", "journey.filter_group_person": "الشخص", "journey.filter_group_status": "الحالة", "journey.filter_group_none": "بدون تجميع",
			"journey.filter_result": "{shown} من {total} طلبات", "journey.filter_empty_title": "لا توجد طلبات تطابق عوامل التصفية هذه",
			"journey.filter_empty_detail": "جرّب اسمًا أو مرجعًا آخر، أو وسّع نطاق التاريخ، أو امسح عوامل التصفية.",
			"people.workflows_quiet":      "لا يوجد مسار عمل للبدء", "people.open_promotion": "فتح الترقية", "people.start_promotion": "بدء الترقية",
			"work.more_filters": "المزيد", "work.more_filters_aria": "مرشحات عمل إضافية",
		}
	default:
		return nil
	}
}
