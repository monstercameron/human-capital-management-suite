package productui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/localize"
)

// clockMessages supplies the English catalog entries for the clock page.
// Registration is performed by the central product catalog composition.
func clockMessages() map[string]localize.Message {
	result := clockCatalog(clockCopyEN)
	for key, text := range timeCopyEN {
		result[key] = localize.Message{Text: text}
	}
	return result
}

// clockTranslations supplies the non-English clock page overlays.
func clockTranslations(locale string) map[string]localize.Message {
	result := map[string]string{}
	switch locale {
	case "de-DE":
		for key, text := range clockCopyDE {
			result[key] = text
		}
		for key, text := range timeCopyDE {
			result[key] = text
		}
	case "ar", "ar-SA":
		for key, text := range clockCopyAR {
			result[key] = text
		}
		for key, text := range timeCopyAR {
			result[key] = text
		}
	default:
		return nil
	}
	return clockCatalog(result)
}

func clockCatalog(values map[string]string) map[string]localize.Message {
	result := make(map[string]localize.Message, len(values))
	for key, text := range values {
		result[key] = localize.Message{Text: text}
	}
	return result
}

// clockCopyFallback returns the built-in copy for a resolved locale code so
// the page stays readable while a catalog is being composed.
func clockCopyFallback(code string) map[string]string {
	switch code {
	case "de-DE":
		return clockCopyDE
	case "ar", "ar-SA":
		return clockCopyAR
	default:
		return clockCopyEN
	}
}

func timeText(locale LocaleContext, key string) string {
	if localized := locale.Text(key); localized != key && !strings.HasPrefix(localized, "⟦") {
		return localized
	}
	values := timeCopyFallback(locale.normalized().Resolved)
	if value, ok := values[key]; ok {
		return value
	}
	return timeCopyEN[key]
}

func timeCountText(locale LocaleContext, key string, count int) string {
	return strings.ReplaceAll(timeText(locale, key), "{count}", locale.FormatNumber(strconv.Itoa(count), 0))
}

func timeCopyFallback(code string) map[string]string {
	switch code {
	case "de-DE":
		return timeCopyDE
	case "ar", "ar-SA":
		return timeCopyAR
	default:
		return timeCopyEN
	}
}

var timeCopyEN = map[string]string{
	"page.timecard.label": "My timecard", "page.timecard.title": "My timecard", "page.timecard.subtitle": "Review and submit your recorded hours.",
	"page.time_schedule.label": "Crew schedule", "page.time_schedule.title": "Crew schedule", "page.time_schedule.subtitle": "Plan shifts and publish changes to your crew.",
	"page.time_devices.label": "Time clock devices", "page.time_devices.title": "Time clock devices", "page.time_devices.subtitle": "Manage enrolled devices that record punches.",
	"timecard.title": "My timecard", "timecard.intro": "Check your hours for the period, fix anything that looks wrong, then submit it for approval.", "timecard.unavailable": "Couldn't load your timecard.", "timecard.unavailable_help": "Try again in a moment. If it keeps happening, ask your supervisor.", "timecard.period_nav": "Change period", "timecard.totals": "Period totals", "timecard.problems": "{count} days need your attention. Fix them before you submit.", "timecard.empty": "There are no days to show for this period.", "timecard.no_punches": "No time recorded",
	"schedule.title": "Crew schedule", "schedule.intro": "Plan who works where. A shift is a plan, not a record; punches show what really happened.", "schedule.unavailable": "Couldn't load the crew schedule.", "schedule.unavailable_help": "Try again in a moment. If it keeps happening, check that you have scheduling access.", "schedule.week_nav": "Change week", "schedule.grid": "Shifts for the week", "schedule.worker": "Worker", "schedule.off": "Off", "schedule.draft": "Draft", "schedule.legend_published": "Published shift", "schedule.legend_draft": "Draft, not yet visible to workers", "schedule.empty": "No shifts this week", "schedule.empty_help": "Add a shift to start planning this week.", "schedule.publish": "Publish this week", "schedule.publishing": "Publishing…", "schedule.publish_yes": "Yes, publish and notify", "schedule.publish_no": "Go back", "schedule.published": "Week published. Workers have been notified.", "schedule.unpublished": "{count} shifts are drafts. Workers cannot see them until you publish.", "schedule.confirm_title": "Publish this week?", "schedule.confirm_body": "Workers with drafts among these {count} shifts are notified right away. Publishing does not record attendance.", "schedule.error": "The week could not be published. Nothing was sent. Try again.",
	"review.title": "Approve timecards", "review.intro": "Approve your crew's hours for the period.", "review.unavailable": "Couldn't load the timecards to review.", "review.unavailable_help": "Try again in a moment. If it keeps happening, check that you have approval access for this crew.", "review.period": "{period} · {count} timecards", "review.empty": "No timecards to review", "review.empty_help": "When your crew's timecards for this period are ready, they appear here.", "review.needs": "Needs your attention ({count})", "review.needs_help": "Open each timecard and fix the problem first. These cannot be approved from this list.", "review.ready": "Ready to approve ({count})", "review.ready_help": "No problems found. Select the timecards you want to approve.", "review.none_ready": "Nothing is ready to approve yet.", "review.select_all": "Select all {count} ready timecards", "review.selected": "{count} selected", "review.approve": "Approve {count} selected", "review.approving": "Approving…", "review.approved": "{count} timecards approved.", "review.pick_hint": "Select timecards to approve them together.", "review.confirm_yes": "Yes, approve {count} timecards", "review.confirm_no": "Go back", "review.confirm_title": "Approve {count} timecards?", "review.confirm_body": "Approved time for {period} goes to payroll, and workers can no longer change these days.", "review.error": "Those timecards could not be approved. Nothing was changed. Try again.",
	"exceptions.title": "Time exceptions", "exceptions.intro": "Punches that do not match the schedule or the rules. Fix each one so timecards can be approved.", "exceptions.unavailable": "Couldn't load time exceptions.", "exceptions.unavailable_help": "Try again in a moment. If it keeps happening, ask your administrator.", "exceptions.filters": "Filter by problem", "exceptions.empty": "No exceptions", "exceptions.empty_help": "Every punch this period matches its schedule and the time rules.",
	"fleet.title": "Time clock devices", "fleet.intro": "Tablets and readers that record punches. Devices needing attention are listed first.", "fleet.unavailable": "Couldn't load the device list.", "fleet.unavailable_help": "Try again in a moment. If it keeps happening, check that you can manage time clock devices.", "fleet.empty": "No devices set up yet", "fleet.empty_help": "Set up a tablet at a jobsite so your crew can clock in with a PIN or badge.", "fleet.last_seen": "Last heard from", "fleet.version": "App version", "fleet.drift": "Clock accuracy", "fleet.revoked": "Turned off", "fleet.revoke": "Turn off this device", "fleet.revoke_yes": "Yes, turn off device", "fleet.revoking": "Turning off…", "fleet.revoke_no": "Keep device on", "fleet.revoke_title": "Turn off {name}?", "fleet.revoke_body": "It stops recording punches right away. Punches already saved on the device are kept and sent if it comes back online.", "fleet.error": "The device could not be turned off. It is still active. Try again.",
	"timecard.problems_one":    "1 day needs your attention. Fix it before you submit.",
	"review.approve_one":       "Approve 1 selected",
	"review.confirm_title_one": "Approve 1 timecard?",
	"review.confirm_yes_one":   "Yes, approve 1 timecard",
	"review.approved_one":      "1 timecard approved.",
	"timecard.go_to":           "Go to {date}", "timecard.today": "Today", "timecard.submit": "Submit timecard for approval",
	"retry":               "Try again",
	"review.approve_none": "Approve selected timecards",
}

var timeCopyDE = map[string]string{
	"page.timecard.label": "Mein Stundenzettel", "page.timecard.title": "Mein Stundenzettel", "page.timecard.subtitle": "Prüfen und senden Sie Ihre erfassten Arbeitsstunden.",
	"page.time_schedule.label": "Teamplan", "page.time_schedule.title": "Teamplan", "page.time_schedule.subtitle": "Planen Sie Schichten und veröffentlichen Sie Änderungen für Ihr Team.",
	"page.time_devices.label": "Stempeluhr-Geräte", "page.time_devices.title": "Stempeluhr-Geräte", "page.time_devices.subtitle": "Verwalten Sie registrierte Geräte, die Buchungen erfassen.",
	"timecard.title": "Mein Stundenzettel", "timecard.intro": "Prüfen Sie Ihre Stunden für den Zeitraum, korrigieren Sie Auffälligkeiten und reichen Sie ihn dann zur Genehmigung ein.", "timecard.unavailable": "Ihr Stundenzettel konnte nicht geladen werden.", "timecard.unavailable_help": "Versuchen Sie es gleich noch einmal. Wenn das Problem bleibt, fragen Sie Ihre Führungskraft.", "timecard.period_nav": "Zeitraum wechseln", "timecard.totals": "Summen des Zeitraums", "timecard.problems": "{count} Tage brauchen Ihre Aufmerksamkeit. Korrigieren Sie sie vor dem Einreichen.", "timecard.empty": "Für diesen Zeitraum gibt es keine Tage.", "timecard.no_punches": "Keine Zeit erfasst",
	"schedule.title": "Teamplan", "schedule.intro": "Planen Sie, wer wo arbeitet. Eine Schicht ist ein Plan, kein Nachweis; die Buchungen zeigen, was wirklich geschah.", "schedule.unavailable": "Der Teamplan konnte nicht geladen werden.", "schedule.unavailable_help": "Versuchen Sie es gleich noch einmal. Wenn das Problem bleibt, prüfen Sie Ihre Berechtigung zur Planung.", "schedule.week_nav": "Woche wechseln", "schedule.grid": "Schichten der Woche", "schedule.worker": "Mitarbeiter", "schedule.off": "Frei", "schedule.draft": "Entwurf", "schedule.legend_published": "Veröffentlichte Schicht", "schedule.legend_draft": "Entwurf, für Mitarbeiter noch nicht sichtbar", "schedule.empty": "Keine Schichten diese Woche", "schedule.empty_help": "Fügen Sie eine Schicht hinzu, um diese Woche zu planen.", "schedule.publish": "Diese Woche veröffentlichen", "schedule.publishing": "Wird veröffentlicht…", "schedule.publish_yes": "Ja, veröffentlichen und benachrichtigen", "schedule.publish_no": "Zurück", "schedule.published": "Woche veröffentlicht. Die Mitarbeiter wurden benachrichtigt.", "schedule.unpublished": "{count} Schichten sind Entwürfe. Mitarbeiter sehen sie erst nach der Veröffentlichung.", "schedule.confirm_title": "Diese Woche veröffentlichen?", "schedule.confirm_body": "Mitarbeiter mit Entwürfen unter diesen {count} Schichten werden sofort benachrichtigt. Die Veröffentlichung erfasst keine Anwesenheit.", "schedule.error": "Die Woche konnte nicht veröffentlicht werden. Es wurde nichts gesendet. Versuchen Sie es erneut.",
	"review.title": "Stundenzettel genehmigen", "review.intro": "Genehmigen Sie die Stunden Ihres Teams für den Zeitraum.", "review.unavailable": "Die Stundenzettel konnten nicht geladen werden.", "review.unavailable_help": "Versuchen Sie es gleich noch einmal. Wenn das Problem bleibt, prüfen Sie Ihre Genehmigungsrechte für dieses Team.", "review.period": "{period} · {count} Stundenzettel", "review.empty": "Keine Stundenzettel zu prüfen", "review.empty_help": "Sobald die Stundenzettel Ihres Teams für diesen Zeitraum bereit sind, erscheinen sie hier.", "review.needs": "Braucht Ihre Aufmerksamkeit ({count})", "review.needs_help": "Öffnen Sie jeden Stundenzettel und beheben Sie zuerst das Problem. Aus dieser Liste können sie nicht genehmigt werden.", "review.ready": "Bereit zur Genehmigung ({count})", "review.ready_help": "Keine Probleme gefunden. Wählen Sie die Stundenzettel aus, die Sie genehmigen möchten.", "review.none_ready": "Noch ist nichts zur Genehmigung bereit.", "review.select_all": "Alle {count} bereiten Stundenzettel auswählen", "review.selected": "{count} ausgewählt", "review.approve": "{count} ausgewählte genehmigen", "review.approving": "Wird genehmigt…", "review.approved": "{count} Stundenzettel genehmigt.", "review.pick_hint": "Wählen Sie Stundenzettel aus, um sie gemeinsam zu genehmigen.", "review.confirm_yes": "Ja, {count} Stundenzettel genehmigen", "review.confirm_no": "Zurück", "review.confirm_title": "{count} Stundenzettel genehmigen?", "review.confirm_body": "Die genehmigte Zeit für {period} geht an die Lohnabrechnung, und Mitarbeiter können diese Tage nicht mehr ändern.", "review.error": "Diese Stundenzettel konnten nicht genehmigt werden. Es wurde nichts geändert. Versuchen Sie es erneut.",
	"exceptions.title": "Zeitausnahmen", "exceptions.intro": "Buchungen, die nicht zum Plan oder zu den Regeln passen. Klären Sie jede, damit Stundenzettel genehmigt werden können.", "exceptions.unavailable": "Die Zeitausnahmen konnten nicht geladen werden.", "exceptions.unavailable_help": "Versuchen Sie es gleich noch einmal. Wenn das Problem bleibt, fragen Sie Ihre Administration.", "exceptions.filters": "Nach Problem filtern", "exceptions.empty": "Keine Ausnahmen", "exceptions.empty_help": "Jede Buchung dieses Zeitraums passt zu ihrem Plan und zu den Zeitregeln.",
	"fleet.title": "Stempeluhr-Geräte", "fleet.intro": "Tablets und Lesegeräte, die Buchungen erfassen. Geräte mit Handlungsbedarf stehen zuerst.", "fleet.unavailable": "Die Geräteliste konnte nicht geladen werden.", "fleet.unavailable_help": "Versuchen Sie es gleich noch einmal. Wenn das Problem bleibt, prüfen Sie, ob Sie Stempeluhr-Geräte verwalten dürfen.", "fleet.empty": "Noch keine Geräte eingerichtet", "fleet.empty_help": "Richten Sie ein Tablet auf der Baustelle ein, damit Ihr Team per PIN oder Ausweis stempeln kann.", "fleet.last_seen": "Zuletzt gemeldet", "fleet.version": "App-Version", "fleet.drift": "Uhrgenauigkeit", "fleet.revoked": "Ausgeschaltet", "fleet.revoke": "Dieses Gerät ausschalten", "fleet.revoke_yes": "Ja, Gerät ausschalten", "fleet.revoking": "Wird ausgeschaltet…", "fleet.revoke_no": "Gerät eingeschaltet lassen", "fleet.revoke_title": "{name} ausschalten?", "fleet.revoke_body": "Es erfasst sofort keine Buchungen mehr. Bereits gespeicherte Buchungen bleiben erhalten und werden gesendet, wenn das Gerät wieder online ist.", "fleet.error": "Das Gerät konnte nicht ausgeschaltet werden. Es ist weiterhin aktiv. Versuchen Sie es erneut.",
	"timecard.problems_one":    "1 Tag braucht Ihre Aufmerksamkeit. Korrigieren Sie ihn vor dem Einreichen.",
	"review.approve_one":       "1 ausgewählten genehmigen",
	"review.confirm_title_one": "1 Stundenzettel genehmigen?",
	"review.confirm_yes_one":   "Ja, 1 Stundenzettel genehmigen",
	"review.approved_one":      "1 Stundenzettel genehmigt.",
	"timecard.go_to":           "Zu {date}", "timecard.today": "Heute", "timecard.submit": "Stundenzettel zur Genehmigung einreichen",
	"retry":               "Erneut versuchen",
	"review.approve_none": "Ausgewählte Stundenzettel genehmigen",
}

var timeCopyAR = map[string]string{
	"page.timecard.label": "بطاقة وقتي", "page.timecard.title": "بطاقة وقتي", "page.timecard.subtitle": "راجع ساعات عملك المسجلة وأرسلها.",
	"page.time_schedule.label": "جدول الفريق", "page.time_schedule.title": "جدول الفريق", "page.time_schedule.subtitle": "خطط للورديات وانشر التغييرات لفريقك.",
	"page.time_devices.label": "أجهزة ساعة الدوام", "page.time_devices.title": "أجهزة ساعة الدوام", "page.time_devices.subtitle": "أدر الأجهزة المسجلة التي تسجل الحضور والانصراف.",
	"timecard.title": "بطاقة وقتي", "timecard.intro": "راجع ساعاتك خلال الفترة، وصحّح ما يبدو خاطئًا، ثم أرسلها للاعتماد.", "timecard.unavailable": "تعذّر تحميل بطاقة وقتك.", "timecard.unavailable_help": "حاول مرة أخرى بعد قليل. إن استمرت المشكلة فاسأل مشرفك.", "timecard.period_nav": "تغيير الفترة", "timecard.totals": "إجماليات الفترة", "timecard.problems": "{count} أيام تحتاج إلى انتباهك. صحّحها قبل الإرسال.", "timecard.empty": "لا توجد أيام لعرضها في هذه الفترة.", "timecard.no_punches": "لا يوجد وقت مسجل",
	"schedule.title": "جدول الفريق", "schedule.intro": "خطّط لمن يعمل وأين. الوردية خطة وليست سجلًا؛ التسجيلات تُظهر ما حدث فعلًا.", "schedule.unavailable": "تعذّر تحميل جدول الفريق.", "schedule.unavailable_help": "حاول مرة أخرى بعد قليل. إن استمرت المشكلة فتأكد من أن لديك صلاحية الجدولة.", "schedule.week_nav": "تغيير الأسبوع", "schedule.grid": "ورديات الأسبوع", "schedule.worker": "الموظف", "schedule.off": "إجازة", "schedule.draft": "مسودة", "schedule.legend_published": "وردية منشورة", "schedule.legend_draft": "مسودة، لا يراها العاملون بعد", "schedule.empty": "لا توجد ورديات هذا الأسبوع", "schedule.empty_help": "أضف وردية لبدء تخطيط هذا الأسبوع.", "schedule.publish": "نشر هذا الأسبوع", "schedule.publishing": "جارٍ النشر…", "schedule.publish_yes": "نعم، انشر وأبلغ", "schedule.publish_no": "رجوع", "schedule.published": "تم نشر الأسبوع. تم إبلاغ العاملين.", "schedule.unpublished": "{count} ورديات مسودات. لا يراها العاملون حتى تنشرها.", "schedule.confirm_title": "هل تريد نشر هذا الأسبوع؟", "schedule.confirm_body": "سيُبلَّغ فورًا العاملون الذين لديهم مسودات ضمن هذه الورديات ({count}). النشر لا يسجّل الحضور.", "schedule.error": "تعذّر نشر الأسبوع. لم يُرسل شيء. حاول مرة أخرى.",
	"review.title": "اعتماد بطاقات الوقت", "review.intro": "اعتمد ساعات فريقك خلال الفترة.", "review.unavailable": "تعذّر تحميل بطاقات الوقت للمراجعة.", "review.unavailable_help": "حاول مرة أخرى بعد قليل. إن استمرت المشكلة فتأكد من أن لديك صلاحية الاعتماد لهذا الفريق.", "review.period": "{period} · {count} بطاقة وقت", "review.empty": "لا توجد بطاقات للمراجعة", "review.empty_help": "عندما تصبح بطاقات فريقك لهذه الفترة جاهزة ستظهر هنا.", "review.needs": "تحتاج إلى انتباهك ({count})", "review.needs_help": "افتح كل بطاقة وصحّح المشكلة أولًا. لا يمكن اعتمادها من هذه القائمة.", "review.ready": "جاهزة للاعتماد ({count})", "review.ready_help": "لم تُكتشف مشكلات. حدّد البطاقات التي تريد اعتمادها.", "review.none_ready": "لا شيء جاهز للاعتماد بعد.", "review.select_all": "تحديد كل البطاقات الجاهزة ({count})", "review.selected": "تم تحديد {count}", "review.approve": "اعتماد المحدد ({count})", "review.approving": "جارٍ الاعتماد…", "review.approved": "تم اعتماد {count} بطاقات.", "review.pick_hint": "حدّد بطاقات لاعتمادها معًا.", "review.confirm_yes": "نعم، اعتمد {count} بطاقات", "review.confirm_no": "رجوع", "review.confirm_title": "هل تريد اعتماد {count} بطاقات؟", "review.confirm_body": "يُرسل الوقت المعتمد للفترة {period} إلى الرواتب، ولن يتمكن العاملون من تغيير هذه الأيام.", "review.error": "تعذّر اعتماد هذه البطاقات. لم يتغير شيء. حاول مرة أخرى.",
	"exceptions.title": "استثناءات الوقت", "exceptions.intro": "تسجيلات لا تطابق الجدول أو القواعد. عالج كل واحد حتى يمكن اعتماد بطاقات الوقت.", "exceptions.unavailable": "تعذّر تحميل استثناءات الوقت.", "exceptions.unavailable_help": "حاول مرة أخرى بعد قليل. إن استمرت المشكلة فاسأل المسؤول.", "exceptions.filters": "التصفية حسب المشكلة", "exceptions.empty": "لا توجد استثناءات", "exceptions.empty_help": "كل تسجيل في هذه الفترة يطابق جدوله وقواعد الوقت.",
	"fleet.title": "أجهزة ساعة الدوام", "fleet.intro": "الأجهزة اللوحية وقارئات الشارات التي تسجّل الأوقات. الأجهزة التي تحتاج إلى انتباه تظهر أولًا.", "fleet.unavailable": "تعذّر تحميل قائمة الأجهزة.", "fleet.unavailable_help": "حاول مرة أخرى بعد قليل. إن استمرت المشكلة فتأكد من أن بإمكانك إدارة أجهزة ساعة الدوام.", "fleet.empty": "لم يُعدّ أي جهاز بعد", "fleet.empty_help": "جهّز جهازًا لوحيًا في موقع العمل ليسجّل فريقك الحضور بالرقم السري أو الشارة.", "fleet.last_seen": "آخر اتصال", "fleet.version": "إصدار التطبيق", "fleet.drift": "دقة الساعة", "fleet.revoked": "متوقف", "fleet.revoke": "إيقاف هذا الجهاز", "fleet.revoke_yes": "نعم، أوقف الجهاز", "fleet.revoking": "جارٍ الإيقاف…", "fleet.revoke_no": "إبقاء الجهاز يعمل", "fleet.revoke_title": "هل تريد إيقاف {name}؟", "fleet.revoke_body": "سيتوقف عن تسجيل الأوقات فورًا. التسجيلات المحفوظة على الجهاز تبقى وتُرسل إن عاد متصلًا.", "fleet.error": "تعذّر إيقاف الجهاز. ما زال نشطًا. حاول مرة أخرى.",
	"timecard.problems_one":    "يوم واحد يحتاج إلى انتباهك. صحّحه قبل الإرسال.",
	"review.approve_one":       "اعتماد بطاقة واحدة محددة",
	"review.confirm_title_one": "هل تريد اعتماد بطاقة واحدة؟",
	"review.confirm_yes_one":   "نعم، اعتمد بطاقة واحدة",
	"review.approved_one":      "تم اعتماد بطاقة واحدة.",
	"timecard.go_to":           "الانتقال إلى {date}", "timecard.today": "اليوم", "timecard.submit": "إرسال بطاقة الوقت للاعتماد",
	"retry":               "حاول مرة أخرى",
	"review.approve_none": "اعتماد بطاقات الوقت المحددة",
}

var clockCopyEN = map[string]string{
	"page.time_missing_punch.label": "Fix a missing punch", "page.time_missing_punch.title": "Fix a missing punch", "page.time_missing_punch.subtitle": "Tell your supervisor what to correct on your timecard.",
	"page.time_clock.label": "Time clock", "page.time_clock.title": "Time clock", "page.time_clock.subtitle": "Clock in, take breaks and clock out.",
	"clock_page.description":      "Record when you start work, when you take a break, and when you finish.",
	"clock_page.service_state":    "Clock information is not available for this workspace yet.",
	"clock_page.unavailable_help": "Try again in a moment. If it keeps happening, ask your system administrator to check the time clock service.",

	// UXBLIND-123: the reason the clock is not offered, and what an
	// administrator can do about it. Nothing here sends anyone to a supervisor
	// for access an administrator alone can give.
	"clock_page.admin_badge":                    "For administrators",
	"clock_page.reason.NOT_ENABLED.title":       "The time clock is not turned on for this workspace.",
	"clock_page.reason.NOT_ENABLED.help":        "Nobody here can clock in or out yet, and nothing has been recorded. Ask your system administrator to turn on time tracking.",
	"clock_page.reason.NOT_ENABLED.admin":       "Time tracking has not been set up for this workspace yet. Turn it on in the workspace's deployment settings, then reload this page.",
	"clock_page.reason.NO_WORKER_RECORD.title":  "Your sign-in is not linked to a worker record.",
	"clock_page.reason.NO_WORKER_RECORD.help":   "The clock records time for workers only. Nothing has been recorded.",
	"clock_page.reason.NO_WORKER_RECORD.admin":  "Link this sign-in to an active worker in the workforce directory, then reload.",
	"clock_page.reason.NO_ASSIGNMENT.title":     "You have no current assignment to record time against.",
	"clock_page.reason.NO_ASSIGNMENT.help":      "Your worker record is active but has no current assignment. Nothing has been recorded.",
	"clock_page.reason.NO_ASSIGNMENT.admin":     "Give the worker a current assignment in the workforce directory, then reload.",
	"clock_page.reason.NO_TIME_PROFILE.title":   "No time profile is set up for your assignment.",
	"clock_page.reason.NO_TIME_PROFILE.help":    "The clock needs a published time profile to know how your time is recorded. Nothing has been recorded.",
	"clock_page.reason.NO_TIME_PROFILE.admin":   "Publish a punch time profile and pin it to this worker's assignment, then reload.",
	"clock_page.reason.EXEMPT.title":            "You are exempt from clocking in and out.",
	"clock_page.reason.EXEMPT.help":             "Your time profile is exempt, so your hours are not recorded by clock. Nothing has been recorded.",
	"clock_page.reason.EXEMPT.admin":            "If this worker should clock in and out, publish a punch time profile for their assignment.",
	"clock_page.reason.CAPTURE_NOT_PUNCH.title": "Your time is not recorded by clock.",
	"clock_page.reason.CAPTURE_NOT_PUNCH.help":  "Your time profile records hours another way, such as hours per day or by exception. Nothing has been recorded here.",
	"clock_page.reason.CAPTURE_NOT_PUNCH.admin": "If this worker should clock in and out, publish a punch time profile for their assignment.",
	"clock_page.since_in":                       "On the clock since {time}",
	"clock_page.last_punch":                     "Last punch {time}",
	"clock_page.status_title":                   "Current clock status",
	"clock_page.worker":                         "Worker",
	"clock_page.schedule":                       "Today's shift",
	"clock_page.status":                         "Status",
	"clock_page.last_event":                     "Last recorded event",
	"clock_page.receipt_trace":                  "Confirmation code",
	"clock_page.action_busy":                    "Recording your punch…",
	"clock_page.action_error":                   "That punch was not recorded. Check your connection, then try again.",
	"clock_page.action_success":                 "Punch recorded",
	"clock_page.clock_in":                       "Clock in",
	"clock_page.clock_out":                      "Clock out",
	"clock_page.start_break":                    "Start break",
	"clock_page.end_break":                      "End break",
	"clock_page.hint_out":                       "Start your day with the button above.",
	"clock_page.hint_in":                        "Press the button above when your shift ends.",
	"clock_page.hint_break":                     "You are on a break. End it to go back on the clock.",
	"clock_page.confirm_title":                  "Clock out now?",
	"clock_page.confirm_body":                   "This stops your work timer. If you clock out by mistake, ask your supervisor to correct it.",
	"clock_page.confirm_yes":                    "Yes, clock out",
	"clock_page.confirm_no":                     "Keep working",
	"clock_page.links_title":                    "Need to change a punch?",
	"clock_page.kiosk_title":                    "Shared tablet clock",
	"clock_page.kiosk_body":                     "Set up a tablet at the site so your crew can clock in with a PIN or badge.",
}

var clockCopyDE = map[string]string{
	"page.time_missing_punch.label": "Fehlenden Zeiteintrag korrigieren", "page.time_missing_punch.title": "Fehlenden Zeiteintrag korrigieren", "page.time_missing_punch.subtitle": "Sagen Sie Ihrer Führungskraft, was auf Ihrem Stundenzettel korrigiert werden soll.",
	"page.time_clock.label": "Zeiterfassung", "page.time_clock.title": "Zeiterfassung", "page.time_clock.subtitle": "Ein- und ausstempeln und Pausen erfassen.",
	"clock_page.description":      "Erfassen Sie, wann Sie mit der Arbeit beginnen, wann Sie Pause machen und wann Sie fertig sind.",
	"clock_page.service_state":    "Zeiterfassungsinformationen sind für diesen Arbeitsbereich noch nicht verfügbar.",
	"clock_page.unavailable_help": "Versuchen Sie es gleich noch einmal. Wenn das Problem bleibt, bitten Sie Ihren Systemadministrator, den Zeiterfassungsdienst zu prüfen.",

	"clock_page.admin_badge":                    "Für Administratoren",
	"clock_page.reason.NOT_ENABLED.title":       "Die Zeiterfassung ist für diesen Arbeitsbereich nicht eingeschaltet.",
	"clock_page.reason.NOT_ENABLED.help":        "Niemand kann hier ein- oder ausstempeln, und es wurde nichts erfasst. Bitten Sie Ihren Systemadministrator, die Zeiterfassung einzuschalten.",
	"clock_page.reason.NOT_ENABLED.admin":       "Die Zeiterfassung wurde für diesen Arbeitsbereich noch nicht eingerichtet. Schalten Sie sie in den Bereitstellungseinstellungen des Arbeitsbereichs ein und laden Sie die Seite neu.",
	"clock_page.reason.NO_WORKER_RECORD.title":  "Ihre Anmeldung ist keinem Mitarbeiterdatensatz zugeordnet.",
	"clock_page.reason.NO_WORKER_RECORD.help":   "Die Stempeluhr erfasst nur Zeiten von Mitarbeitenden. Es wurde nichts erfasst.",
	"clock_page.reason.NO_WORKER_RECORD.admin":  "Verknüpfen Sie diese Anmeldung im Mitarbeiterverzeichnis mit einem aktiven Mitarbeitenden und laden Sie die Seite neu.",
	"clock_page.reason.NO_ASSIGNMENT.title":     "Sie haben keine aktuelle Zuordnung, der Zeit zugeordnet werden kann.",
	"clock_page.reason.NO_ASSIGNMENT.help":      "Ihr Mitarbeiterdatensatz ist aktiv, hat aber keine aktuelle Zuordnung. Es wurde nichts erfasst.",
	"clock_page.reason.NO_ASSIGNMENT.admin":     "Weisen Sie dem Mitarbeitenden im Mitarbeiterverzeichnis eine aktuelle Zuordnung zu und laden Sie die Seite neu.",
	"clock_page.reason.NO_TIME_PROFILE.title":   "Für Ihre Zuordnung ist kein Zeitprofil eingerichtet.",
	"clock_page.reason.NO_TIME_PROFILE.help":    "Die Stempeluhr braucht ein veröffentlichtes Zeitprofil, um zu wissen, wie Ihre Zeit erfasst wird. Es wurde nichts erfasst.",
	"clock_page.reason.NO_TIME_PROFILE.admin":   "Veröffentlichen Sie ein Stempel-Zeitprofil und ordnen Sie es der Zuordnung dieses Mitarbeitenden zu; laden Sie dann die Seite neu.",
	"clock_page.reason.EXEMPT.title":            "Sie sind vom Ein- und Ausstempeln befreit.",
	"clock_page.reason.EXEMPT.help":             "Ihr Zeitprofil ist befreit, daher werden Ihre Stunden nicht per Stempeluhr erfasst. Es wurde nichts erfasst.",
	"clock_page.reason.EXEMPT.admin":            "Wenn diese Person ein- und ausstempeln soll, veröffentlichen Sie ein Stempel-Zeitprofil für ihre Zuordnung.",
	"clock_page.reason.CAPTURE_NOT_PUNCH.title": "Ihre Zeit wird nicht per Stempeluhr erfasst.",
	"clock_page.reason.CAPTURE_NOT_PUNCH.help":  "Ihr Zeitprofil erfasst Stunden auf andere Weise, etwa als Stunden pro Tag oder nur bei Abweichungen. Hier wurde nichts erfasst.",
	"clock_page.reason.CAPTURE_NOT_PUNCH.admin": "Wenn diese Person ein- und ausstempeln soll, veröffentlichen Sie ein Stempel-Zeitprofil für ihre Zuordnung.",
	"clock_page.since_in":                       "Eingestempelt seit {time}",
	"clock_page.last_punch":                     "Letzte Buchung {time}",
	"clock_page.status_title":                   "Aktueller Zeiterfassungsstatus",
	"clock_page.worker":                         "Mitarbeiter",
	"clock_page.schedule":                       "Schicht heute",
	"clock_page.status":                         "Status",
	"clock_page.last_event":                     "Letztes aufgezeichnetes Ereignis",
	"clock_page.receipt_trace":                  "Bestätigungscode",
	"clock_page.action_busy":                    "Ihre Buchung wird erfasst…",
	"clock_page.action_error":                   "Diese Buchung wurde nicht erfasst. Prüfen Sie Ihre Verbindung und versuchen Sie es erneut.",
	"clock_page.action_success":                 "Buchung erfasst",
	"clock_page.clock_in":                       "Einstempeln",
	"clock_page.clock_out":                      "Ausstempeln",
	"clock_page.start_break":                    "Pause starten",
	"clock_page.end_break":                      "Pause beenden",
	"clock_page.hint_out":                       "Beginnen Sie Ihren Tag mit der Schaltfläche oben.",
	"clock_page.hint_in":                        "Drücken Sie oben auf die Schaltfläche, wenn Ihre Schicht endet.",
	"clock_page.hint_break":                     "Sie sind in der Pause. Beenden Sie sie, um weiterzuarbeiten.",
	"clock_page.confirm_title":                  "Jetzt ausstempeln?",
	"clock_page.confirm_body":                   "Damit endet Ihre Arbeitszeit. Bei einem Versehen bitten Sie Ihre Führungskraft um eine Korrektur.",
	"clock_page.confirm_yes":                    "Ja, ausstempeln",
	"clock_page.confirm_no":                     "Weiterarbeiten",
	"clock_page.links_title":                    "Eine Buchung ändern?",
	"clock_page.kiosk_title":                    "Gemeinsame Tablet-Stempeluhr",
	"clock_page.kiosk_body":                     "Richten Sie ein Tablet am Standort ein, damit Ihr Team per PIN oder Ausweis stempeln kann.",
}

var clockCopyAR = map[string]string{
	"page.time_missing_punch.label": "تصحيح تسجيل وقت مفقود", "page.time_missing_punch.title": "تصحيح تسجيل وقت مفقود", "page.time_missing_punch.subtitle": "أخبر مشرفك بما يجب تصحيحه في بطاقة وقتك.",
	"page.time_clock.label": "ساعة الدوام", "page.time_clock.title": "ساعة الدوام", "page.time_clock.subtitle": "سجّل الحضور والاستراحات والانصراف.",
	"clock_page.description":      "سجّل متى تبدأ العمل ومتى تأخذ استراحة ومتى تنتهي.",
	"clock_page.service_state":    "معلومات الساعة غير متاحة لمساحة العمل هذه بعد.",
	"clock_page.unavailable_help": "حاول مرة أخرى بعد قليل. إن استمرت المشكلة فاطلب من مسؤول النظام فحص خدمة ساعة الدوام.",

	"clock_page.admin_badge":                    "للمسؤولين",
	"clock_page.reason.NOT_ENABLED.title":       "ساعة الدوام غير مفعّلة لمساحة العمل هذه.",
	"clock_page.reason.NOT_ENABLED.help":        "لا يستطيع أحد هنا تسجيل الحضور أو الانصراف بعد، ولم يُسجَّل أي شيء. اطلب من مسؤول النظام تفعيل تتبّع الوقت.",
	"clock_page.reason.NOT_ENABLED.admin":       "لم يُعدّ تتبّع الوقت لمساحة العمل هذه بعد. فعّله من إعدادات نشر مساحة العمل، ثم أعد تحميل هذه الصفحة.",
	"clock_page.reason.NO_WORKER_RECORD.title":  "حساب دخولك غير مرتبط بسجل موظف.",
	"clock_page.reason.NO_WORKER_RECORD.help":   "تسجّل ساعة الدوام أوقات الموظفين فقط. لم يُسجَّل أي شيء.",
	"clock_page.reason.NO_WORKER_RECORD.admin":  "اربط هذا الحساب بموظف نشط في دليل القوى العاملة، ثم أعد التحميل.",
	"clock_page.reason.NO_ASSIGNMENT.title":     "ليس لديك تكليف حالي لتسجيل الوقت عليه.",
	"clock_page.reason.NO_ASSIGNMENT.help":      "سجل الموظف الخاص بك نشط لكن ليس له تكليف حالي. لم يُسجَّل أي شيء.",
	"clock_page.reason.NO_ASSIGNMENT.admin":     "امنح الموظف تكليفًا حاليًا في دليل القوى العاملة، ثم أعد التحميل.",
	"clock_page.reason.NO_TIME_PROFILE.title":   "لم يُعدّ ملف وقت لتكليفك.",
	"clock_page.reason.NO_TIME_PROFILE.help":    "تحتاج ساعة الدوام إلى ملف وقت منشور لمعرفة كيفية تسجيل وقتك. لم يُسجَّل أي شيء.",
	"clock_page.reason.NO_TIME_PROFILE.admin":   "انشر ملف وقت للتسجيل بالساعة واربطه بتكليف هذا الموظف، ثم أعد التحميل.",
	"clock_page.reason.EXEMPT.title":            "أنت معفى من تسجيل الحضور والانصراف.",
	"clock_page.reason.EXEMPT.help":             "ملف وقتك معفى، لذلك لا تُسجَّل ساعاتك بالساعة. لم يُسجَّل أي شيء.",
	"clock_page.reason.EXEMPT.admin":            "إذا كان على هذا الموظف تسجيل الحضور والانصراف، فانشر ملف وقت للتسجيل بالساعة لتكليفه.",
	"clock_page.reason.CAPTURE_NOT_PUNCH.title": "لا يُسجَّل وقتك بالساعة.",
	"clock_page.reason.CAPTURE_NOT_PUNCH.help":  "يسجّل ملف وقتك الساعات بطريقة أخرى، مثل الساعات اليومية أو عند الاستثناء فقط. لم يُسجَّل أي شيء هنا.",
	"clock_page.reason.CAPTURE_NOT_PUNCH.admin": "إذا كان على هذا الموظف تسجيل الحضور والانصراف، فانشر ملف وقت للتسجيل بالساعة لتكليفه.",
	"clock_page.since_in":                       "على الساعة منذ {time}",
	"clock_page.last_punch":                     "آخر تسجيل {time}",
	"clock_page.status_title":                   "حالة الساعة الحالية",
	"clock_page.worker":                         "الموظف",
	"clock_page.schedule":                       "وردية اليوم",
	"clock_page.status":                         "الحالة",
	"clock_page.last_event":                     "آخر حدث مسجل",
	"clock_page.receipt_trace":                  "رمز التأكيد",
	"clock_page.action_busy":                    "جارٍ تسجيل الوقت…",
	"clock_page.action_error":                   "لم يُسجَّل هذا الإجراء. تحقق من اتصالك ثم حاول مرة أخرى.",
	"clock_page.action_success":                 "تم تسجيل الوقت",
	"clock_page.clock_in":                       "تسجيل الحضور",
	"clock_page.clock_out":                      "تسجيل الانصراف",
	"clock_page.start_break":                    "بدء الاستراحة",
	"clock_page.end_break":                      "إنهاء الاستراحة",
	"clock_page.hint_out":                       "ابدأ يومك بالزر أعلاه.",
	"clock_page.hint_in":                        "اضغط الزر أعلاه عند انتهاء ورديتك.",
	"clock_page.hint_break":                     "أنت في استراحة. أنهِها للعودة إلى الدوام.",
	"clock_page.confirm_title":                  "هل تريد تسجيل الانصراف الآن؟",
	"clock_page.confirm_body":                   "سيتوقف عداد وقت عملك. إذا سجّلت الانصراف بالخطأ فاطلب من مشرفك تصحيحه.",
	"clock_page.confirm_yes":                    "نعم، سجّل الانصراف",
	"clock_page.confirm_no":                     "متابعة العمل",
	"clock_page.links_title":                    "تحتاج إلى تعديل تسجيل؟",
	"clock_page.kiosk_title":                    "ساعة الدوام المشتركة على جهاز لوحي",
	"clock_page.kiosk_body":                     "جهّز جهازًا لوحيًا في الموقع ليسجّل فريقك الحضور بالرقم السري أو الشارة.",
}
