package productui

// kioskCopy is deliberately local to the standalone device surface. The
// kiosk has no workspace shell and therefore cannot assume that a workspace
// catalog has been mounted before its first paint; the same LocaleContext and
// reviewed product locale selection still determine the language and
// direction.
func kioskCopy(locale LocaleContext, key string) string {
	values := map[string]map[string]string{
		"en-US": {
			"title": "Time clock", "online": "Online", "offline": "Offline", "queue_empty": "All punches sent", "queue": "{n} waiting to send",
			"enroll_title": "Set up this time clock", "enroll_help": "Enter the one-time enrollment code from your administrator.", "enroll_code": "Enrollment code", "enroll": "Enroll device",
			"start": "Clock in or out", "start_help": "Enter your PIN or scan your badge.", "start_action": "Identify worker", "identify_title": "Identify worker", "credential": "PIN, badge, or QR code", "identify": "Continue",
			"worker": "Worker", "shift": "Current shift", "status": "Current status", "punch_options": "Punch details", "attestation": "Your answer to a required question", "tips": "Tips you received", "job": "Job or project", "cost_code": "Cost code",
			"clock_in": "Clock in", "clock_out": "Clock out", "attest": "Send my answer", "declare_tips": "Record my tips", "transfer": "Switch to this job", "cancel": "Cancel", "done": "Done",
			"receipt": "Punch result", "receipt_sequence": "Punch number {n} on this clock", "recovery": "Stored kiosk data needs administrator recovery.", "revoked": "This time clock is no longer active.",
			"error": "The time clock could not complete that action. Try again.", "no_admin": "This device only provides the shared time clock.",
			"identify_help": "Enter your PIN on the keypad, or scan your badge.", "offline_help": "This clock is offline. PINs cannot be checked, so scan your badge. Punches are saved and sent later.", "credential_placeholder": "Enter PIN", "receipt_in": "{name}, you clocked in at {time}.", "receipt_out": "{name}, you clocked out at {time}.", "keypad": "PIN keypad", "key_clear": "Clear", "key_backspace": "Delete last digit", "checking": "Checking…", "status_in": "Clocked in", "status_break": "On break", "status_out": "Clocked out", "status_unknown": "Status unavailable", "receipt_queued": "Saved on this clock; waiting for connection", "receipt_recorded": "Punch recorded", "receipt_duplicate": "Punch already recorded", "receipt_held": "Punch recorded for review", "receipt_rejected": "Punch was not recorded", "unavailable": "The kiosk is unavailable.",
			"hello": "Hello", "hello_name": "Hello, {name}", "which": "We could not check your status. Choose what you are doing.", "not_you": "Not you? Start over", "details": "Answer a question, add tips or switch job", "return_soon": "Back to the start screen in {s} seconds", "recovery_help": "Ask your supervisor to reset this device. Nothing was lost.", "revoked_help": "Ask your supervisor to set it up again. Punches saved on this clock are kept.",
		},
		"de-DE": {
			"title": "Stempeluhr", "online": "Online", "offline": "Offline", "queue_empty": "Alle Buchungen gesendet", "queue": "{n} warten auf Versand",
			"enroll_title": "Diese Stempeluhr einrichten", "enroll_help": "Geben Sie den einmaligen Registrierungscode Ihrer Verwaltung ein.", "enroll_code": "Registrierungscode", "enroll": "Gerät registrieren",
			"start": "Ein- oder ausstempeln", "start_help": "Geben Sie Ihre PIN ein oder scannen Sie Ihren Ausweis.", "start_action": "Mitarbeiter identifizieren", "identify_title": "Mitarbeiter identifizieren", "credential": "PIN, Ausweis oder QR-Code", "identify": "Weiter",
			"worker": "Mitarbeiter", "shift": "Aktuelle Schicht", "status": "Aktueller Status", "punch_options": "Buchungsdetails", "attestation": "Ihre Antwort auf eine Pflichtfrage", "tips": "Erhaltenes Trinkgeld", "job": "Auftrag oder Projekt", "cost_code": "Kostenstelle",
			"clock_in": "Einstempeln", "clock_out": "Ausstempeln", "attest": "Antwort senden", "declare_tips": "Mein Trinkgeld erfassen", "transfer": "Zu diesem Auftrag wechseln", "cancel": "Abbrechen", "done": "Fertig",
			"receipt": "Ergebnis der Buchung", "receipt_sequence": "Buchung Nr. {n} auf dieser Uhr", "recovery": "Gespeicherte Kioskdaten müssen durch die Verwaltung wiederhergestellt werden.", "revoked": "Diese Stempeluhr ist nicht mehr aktiv.",
			"error": "Die Stempeluhr konnte die Aktion nicht abschließen. Versuchen Sie es erneut.", "no_admin": "Dieses Gerät stellt nur die gemeinsame Stempeluhr bereit.",
			"identify_help": "Geben Sie Ihre PIN über das Tastenfeld ein oder scannen Sie Ihren Ausweis.", "offline_help": "Diese Uhr ist offline. PINs können nicht geprüft werden, scannen Sie daher Ihren Ausweis. Buchungen werden gespeichert und später gesendet.", "credential_placeholder": "PIN eingeben", "receipt_in": "{name}, Sie haben um {time} eingestempelt.", "receipt_out": "{name}, Sie haben um {time} ausgestempelt.", "keypad": "PIN-Tastenfeld", "key_clear": "Leeren", "key_backspace": "Letzte Ziffer löschen", "checking": "Wird geprüft…", "status_in": "Eingestempelt", "status_break": "In Pause", "status_out": "Ausgestempelt", "status_unknown": "Status nicht verfügbar", "receipt_queued": "Auf dieser Uhr gespeichert; wartet auf Verbindung", "receipt_recorded": "Buchung erfasst", "receipt_duplicate": "Buchung bereits erfasst", "receipt_held": "Buchung zur Prüfung erfasst", "receipt_rejected": "Buchung nicht erfasst", "unavailable": "Die Stempeluhr ist nicht verfügbar.",
			"hello": "Hallo", "hello_name": "Hallo, {name}", "which": "Wir konnten Ihren Status nicht prüfen. Wählen Sie, was Sie tun.", "not_you": "Nicht Sie? Neu beginnen", "details": "Frage beantworten, Trinkgeld erfassen oder Auftrag wechseln", "return_soon": "Zurück zum Startbildschirm in {s} Sekunden", "recovery_help": "Bitten Sie Ihre Führungskraft, dieses Gerät zurückzusetzen. Es ist nichts verloren gegangen.", "revoked_help": "Bitten Sie Ihre Führungskraft, es neu einzurichten. Auf dieser Uhr gespeicherte Buchungen bleiben erhalten.",
		},
		"ar": {
			"title": "ساعة الدوام", "online": "متصل", "offline": "غير متصل", "queue_empty": "أُرسلت جميع التسجيلات", "queue": "{n} بانتظار الإرسال",
			"enroll_title": "إعداد ساعة الدوام هذه", "enroll_help": "أدخل رمز التسجيل لمرة واحدة من المسؤول.", "enroll_code": "رمز التسجيل", "enroll": "تسجيل الجهاز",
			"start": "تسجيل الحضور أو الانصراف", "start_help": "أدخل رقمك السري أو امسح بطاقتك.", "start_action": "تحديد الموظف", "identify_title": "تحديد الموظف", "credential": "الرقم السري أو البطاقة أو رمز QR", "identify": "متابعة",
			"worker": "الموظف", "shift": "المناوبة الحالية", "status": "الحالة الحالية", "punch_options": "تفاصيل التسجيل", "attestation": "إجابتك عن سؤال مطلوب", "tips": "الإكراميات التي تلقيتها", "job": "المهمة أو المشروع", "cost_code": "رمز التكلفة",
			"clock_in": "تسجيل الحضور", "clock_out": "تسجيل الانصراف", "attest": "إرسال إجابتي", "declare_tips": "تسجيل إكرامياتي", "transfer": "التبديل إلى هذه المهمة", "cancel": "إلغاء", "done": "تم",
			"receipt": "نتيجة التسجيل", "receipt_sequence": "التسجيل رقم {n} على هذه الساعة", "recovery": "تحتاج بيانات الكشك المحفوظة إلى استعادة من المسؤول.", "revoked": "لم تعد ساعة الدوام هذه نشطة.",
			"error": "تعذر على ساعة الدوام إكمال الإجراء. حاول مرة أخرى.", "no_admin": "يوفر هذا الجهاز ساعة الدوام المشتركة فقط.",
			"identify_help": "أدخل رقمك السري على لوحة الأرقام، أو امسح شارتك.", "offline_help": "هذه الساعة غير متصلة. لا يمكن التحقق من الرقم السري، لذا امسح شارتك. تُحفظ التسجيلات وتُرسل لاحقًا.", "credential_placeholder": "أدخل الرقم السري", "receipt_in": "{name}، سجّلت حضورك عند {time}.", "receipt_out": "{name}، سجّلت انصرافك عند {time}.", "keypad": "لوحة أرقام الرقم السري", "key_clear": "مسح", "key_backspace": "حذف آخر رقم", "checking": "جارٍ التحقق…", "status_in": "تم تسجيل الحضور", "status_break": "في استراحة", "status_out": "تم تسجيل الانصراف", "status_unknown": "الحالة غير متاحة", "receipt_queued": "حُفظ على هذه الساعة؛ بانتظار الاتصال", "receipt_recorded": "تم تسجيل الوقت", "receipt_duplicate": "تم تسجيل الوقت من قبل", "receipt_held": "تم تسجيل الوقت للمراجعة", "receipt_rejected": "لم يتم تسجيل الوقت", "unavailable": "ساعة الدوام غير متاحة.",
			"hello": "مرحبًا", "hello_name": "مرحبًا، {name}", "which": "تعذّر التحقق من حالتك. اختر ما تفعله.", "not_you": "لست أنت؟ ابدأ من جديد", "details": "أجب عن سؤال أو أضف إكراميات أو بدّل المهمة", "return_soon": "العودة إلى شاشة البدء خلال {s} ثوانٍ", "recovery_help": "اطلب من مشرفك إعادة ضبط هذا الجهاز. لم يضع شيء.", "revoked_help": "اطلب من مشرفك إعداده من جديد. التسجيلات المحفوظة على هذه الساعة تبقى.",
		},
	}

	resolved := locale.normalized().Resolved
	if _, ok := values[resolved]; !ok {
		resolved = "en-US"
	}
	if text, ok := values[resolved][key]; ok {
		return text
	}
	return values["en-US"][key]
}

// KioskCopy exposes the reviewed standalone-device catalog to the native
// kiosk host. Dynamic device state (such as worker status and receipt result)
// is projected by the host, but it must use the same copy as the page.
func KioskCopy(locale LocaleContext, key string) string { return kioskCopy(locale, key) }
