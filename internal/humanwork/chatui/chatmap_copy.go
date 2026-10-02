package chatui

func ChatmapText(locale, key string) string {
	en := map[string]string{
		"schematic": "Schematic · no map data", "agent_supplied": "Supplied by an agent", "integration_supplied": "Supplied by an integration", "you": "You", "loading": "Loading locations…", "empty": "No locations are shared here. Share a point below.", "find": "Find address", "load_sites": "Load job sites", "close": "Close map", "location": "Location", "where": "Where I am", "address": "An address", "site": "A job site",
		"explain": "Share a point with everyone who can read this conversation, for the duration you choose. It is never used for time or pay.",
		"device":  "Read my position", "pin": "Drop the pin", "label": "Place label", "readable": "Readable address", "note": "Optional note",
		"precision": "Precision", "exact": "Exact", "approximate": "Approximate (500 m grid; label and address omitted)",
		"duration": "Share for", "hour": "1 hour", "day": "Until end of day", "keep": "Keep with the message",
		"send": "Send location", "preview": "Preview for readers", "unavailable": "Map detail unavailable",
		"lookup":            "Address lookup is not available here.",
		"sites_unavailable": "Job sites are not available here.",
		"refused":           "Position permission refused. Share an address or try again.",
		"no_fix":            "No position fix. Try again or drop the pin.",
		"rough":             "Rough position. Review the accuracy circle or try again.",
		"offline":           "Offline. Your share is queued until you reconnect; expired shares are discarded.",
		"discarded":         "The queued location expired and was discarded. Choose a new location.",
		"sent":              "Location shared.", "retry": "Try again", "expired": "Location no longer shared",
		"open": "Open larger", "copy": "Copy address", "directions": "Directions (leaves the product)",
		"stop": "Stop sharing", "within": "Within %s m", "age": "Shared %s minutes ago", "by": "Shared by %s",
		"remaining": "Sharing for %s more minutes", "now": "Sharing now", "lat": "Pin latitude", "lon": "Pin longitude",
		"accuracy": "Accuracy radius in metres", "north": "Move pin north", "south": "Move pin south", "east": "Move pin east", "west": "Move pin west",
		"zoom_in": "Zoom in", "zoom_out": "Zoom out", "failed": "Could not share. Review the location and try again.",
	}
	de := map[string]string{
		"schematic": "Schema · keine Kartendaten", "agent_supplied": "Von einem Agenten bereitgestellt", "integration_supplied": "Von einer Integration bereitgestellt", "you": "Sie", "loading": "Standorte werden geladen…", "empty": "Hier sind keine Standorte geteilt. Teilen Sie unten einen Punkt.", "find": "Adresse suchen", "load_sites": "Einsatzorte laden", "close": "Karte schließen", "location": "Standort", "where": "Wo ich bin", "address": "Eine Adresse", "site": "Ein Einsatzort",
		"explain": "Teilen Sie einen Punkt mit allen, die diese Unterhaltung lesen können, für die gewählte Dauer. Er wird nie für Zeit oder Entgelt verwendet.",
		"device":  "Meine Position abrufen", "pin": "Markierung setzen", "label": "Ortsbezeichnung", "readable": "Lesbare Adresse", "note": "Optionale Notiz",
		"precision": "Genauigkeit", "exact": "Genau", "approximate": "Ungefähr (500-m-Raster; ohne Bezeichnung und Adresse)", "duration": "Teilen für", "hour": "1 Stunde", "day": "Bis Tagesende", "keep": "Mit der Nachricht aufbewahren",
		"send": "Standort senden", "preview": "Vorschau für Leser", "unavailable": "Kartendetails nicht verfügbar",
		"lookup":            "Die Adresssuche ist hier nicht verfügbar.",
		"sites_unavailable": "Einsatzorte sind hier nicht verfügbar.",
		"refused":           "Positionszugriff verweigert. Teilen Sie eine Adresse oder versuchen Sie es erneut.",
		"no_fix":            "Keine Position gefunden. Erneut versuchen oder eine Markierung setzen.", "rough": "Ungenaue Position. Prüfen Sie den Genauigkeitskreis oder versuchen Sie es erneut.",
		"offline":   "Offline. Der Standort wird nach Wiederverbindung gesendet; abgelaufene Freigaben werden verworfen.",
		"discarded": "Der vorgemerkte Standort ist abgelaufen und wurde verworfen. Wählen Sie einen neuen Standort.", "sent": "Standort geteilt.", "retry": "Erneut versuchen", "expired": "Standort nicht mehr geteilt",
		"open": "Größer öffnen", "copy": "Adresse kopieren", "directions": "Wegbeschreibung (verlässt das Produkt)", "stop": "Teilen beenden", "within": "Im Umkreis von %s m", "age": "Vor %s Minuten geteilt", "by": "Geteilt von %s", "remaining": "Noch %s Minuten geteilt", "now": "Jetzt geteilt",
		"lat": "Breitengrad der Markierung", "lon": "Längengrad der Markierung", "accuracy": "Genauigkeitsradius in Metern", "north": "Markierung nach Norden bewegen", "south": "Markierung nach Süden bewegen", "east": "Markierung nach Osten bewegen", "west": "Markierung nach Westen bewegen", "zoom_in": "Vergrößern", "zoom_out": "Verkleinern", "failed": "Teilen fehlgeschlagen. Prüfen Sie den Standort und versuchen Sie es erneut.",
	}
	ar := map[string]string{
		"schematic": "رسم تخطيطي · لا توجد بيانات خريطة", "agent_supplied": "مقدم بواسطة وكيل", "integration_supplied": "مقدم بواسطة تكامل", "you": "أنت", "loading": "جارٍ تحميل المواقع…", "empty": "لا توجد مواقع مشاركة هنا. شارك نقطة أدناه.", "find": "البحث عن عنوان", "load_sites": "تحميل مواقع العمل", "close": "إغلاق الخريطة", "location": "الموقع", "where": "مكاني الحالي", "address": "عنوان", "site": "موقع عمل",
		"explain": "شارك نقطة مع كل من يستطيع قراءة هذه المحادثة للمدة التي تختارها. لا تُستخدم أبداً لحساب الوقت أو الأجر.",
		"device":  "قراءة موقعي", "pin": "وضع الدبوس", "label": "اسم المكان", "readable": "العنوان المقروء", "note": "ملاحظة اختيارية",
		"precision": "الدقة", "exact": "دقيق", "approximate": "تقريبي (شبكة 500 م؛ دون تسمية أو عنوان)", "duration": "مدة المشاركة", "hour": "ساعة واحدة", "day": "حتى نهاية اليوم", "keep": "الاحتفاظ مع الرسالة",
		"send": "إرسال الموقع", "preview": "معاينة للقراء", "unavailable": "تفاصيل الخريطة غير متاحة",
		"lookup": "البحث عن العنوان غير متاح هنا.", "sites_unavailable": "مواقع العمل غير متاحة هنا.",
		"refused": "تم رفض إذن الموقع. شارك عنواناً أو حاول مجدداً.", "no_fix": "لم يتم تحديد الموقع. حاول مجدداً أو ضع الدبوس.", "rough": "الموقع غير دقيق. راجع دائرة الدقة أو حاول مجدداً.",
		"offline": "غير متصل. ستُرسل المشاركة عند الاتصال؛ تُحذف المشاركات المنتهية.", "discarded": "انتهت صلاحية الموقع المنتظر وتم حذفه. اختر موقعاً جديداً.", "sent": "تمت مشاركة الموقع.", "retry": "المحاولة مجدداً", "expired": "لم يعد الموقع مشاركاً",
		"open": "فتح بحجم أكبر", "copy": "نسخ العنوان", "directions": "الاتجاهات (تغادر المنتج)", "stop": "إيقاف المشاركة", "within": "ضمن %s م", "age": "تمت المشاركة منذ %s دقيقة", "by": "شارك بواسطة %s", "remaining": "المشاركة مستمرة لمدة %s دقيقة", "now": "المشاركة الآن",
		"lat": "خط عرض الدبوس", "lon": "خط طول الدبوس", "accuracy": "نصف قطر الدقة بالأمتار", "north": "تحريك الدبوس شمالاً", "south": "تحريك الدبوس جنوباً", "east": "تحريك الدبوس شرقاً", "west": "تحريك الدبوس غرباً", "zoom_in": "تكبير", "zoom_out": "تصغير", "failed": "تعذرت المشاركة. راجع الموقع وحاول مجدداً.",
	}
	if locale == "de-DE" {
		return chatbug039Text(key, de[key], en[key])
	}
	if locale == "ar" {
		return chatbug039Text(key, ar[key], en[key])
	}
	return chatbug039Text(key, en[key], "")
}
