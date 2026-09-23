package productui

func columnChooserTranslations(locale string) map[string]string {
	switch locale {
	case "de-DE":
		return map[string]string{
			"table.columns": "Spalten auswählen", "table.columns_help": "Person bleibt sichtbar. Ihre Auswahl wird für Ihr Konto gespeichert. Die Suche umfasst auch ausgeblendete Spalten.",
			"table.columns_apply": "Spalten anwenden", "table.columns_reset": "Standardspalten",
		}
	case "ar":
		return map[string]string{
			"table.columns": "اختيار الأعمدة", "table.columns_help": "يبقى الشخص ظاهرًا. يُحفظ اختيارك لحسابك. يشمل البحث الأعمدة المخفية أيضًا.",
			"table.columns_apply": "تطبيق الأعمدة", "table.columns_reset": "الأعمدة الافتراضية",
		}
	default:
		return map[string]string{
			"table.columns": "Choose columns", "table.columns_help": "Person always stays visible. Your choices are saved to your account. Search includes hidden columns too.",
			"table.columns_apply": "Apply columns", "table.columns_reset": "Reset to defaults",
		}
	}
}
