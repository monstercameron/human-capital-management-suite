package chatui

import "strings"

func chatstateText(m Model, key string) string {
	copy := map[string]map[string]string{
		"en-US": {
			"title": "Status", "open": "Open", "announcements": "Announcements only", "locked": "Locked", "archived": "Archived",
			"open_channel": "Open channel", "archive_empty": "No archived channels. Channels archived later will appear here.", "system_line": "{actor} changed the channel status to {status}: {reason}",
			"permission_composer": "You do not have permission to post here. Ask a channel administrator for access.", "revision_conflict": "The status changed while you were editing. Reload the status and try again.", "channel_held": "This channel is locked or under a record hold. It cannot be archived.", "permission_denied": "Your permission changed. Reload the status before trying again.", "last_reopener": "Keep at least one person able to reopen this channel before removing access.",
			"unknown": "Status unavailable", "change": "Change status", "restore": "Restore", "confirm": "Confirm status change",
			"reason": "Explain the reason", "until": "End the lock at (optional)", "by": "Changed by", "when": "Changed at", "why": "Reason", "ends": "Ends at", "system": "System", "none": "Not set",
			"open_effect":           "Members with posting permission can post, reply, react and edit.",
			"announcements_effect":  "Only announcers and agents with a delivery grant can post. Members can reply in threads and react.",
			"locked_effect":         "Nobody can post, reply, react, edit or pin. Reading and search continue.",
			"archived_effect":       "The channel is read-only and hidden from the usual channel list. Search and restore remain available.",
			"announcement_composer": "Only announcers can post here. You can reply in threads and react.",
			"locked_composer":       "This channel is locked. You can read and search; a workspace administrator can reopen it.",
			"archived_composer":     "This channel is archived. You can read and search; a workspace administrator can restore it.",
			"loading":               "Loading channel status. Please wait before posting.", "error": "Channel status could not be loaded. Retry to continue.", "retry": "Retry status", "updated": "Channel status changed", "invalid": "Choose an allowed status and enter a reason. An end time applies only to a future lock.",
		},
		"de-DE": {
			"title": "Status", "open": "Offen", "announcements": "Nur Ankündigungen", "locked": "Gesperrt", "archived": "Archiviert",
			"open_channel": "Kanal öffnen", "archive_empty": "Keine archivierten Kanäle. Später archivierte Kanäle erscheinen hier.", "system_line": "{actor} hat den Kanalstatus zu {status} geändert: {reason}",
			"permission_composer": "Sie dürfen hier nicht schreiben. Bitten Sie die Kanaladministration um Zugriff.", "revision_conflict": "Der Status wurde während der Bearbeitung geändert. Laden Sie ihn erneut und versuchen Sie es noch einmal.", "channel_held": "Dieser Kanal ist gesperrt oder unter Aufbewahrungsschutz. Er kann nicht archiviert werden.", "permission_denied": "Ihre Berechtigung wurde geändert. Laden Sie den Status erneut, bevor Sie es nochmals versuchen.", "last_reopener": "Mindestens eine Person muss den Kanal wieder öffnen können, bevor Sie Zugriff entfernen.",
			"unknown": "Status nicht verfügbar", "change": "Status ändern", "restore": "Wiederherstellen", "confirm": "Statusänderung bestätigen",
			"reason": "Grund angeben", "until": "Sperre beenden am (optional)", "by": "Geändert von", "when": "Geändert am", "why": "Grund", "ends": "Endet am", "system": "System", "none": "Nicht festgelegt",
			"open_effect":           "Mitglieder mit Schreibberechtigung können schreiben, antworten, reagieren und bearbeiten.",
			"announcements_effect":  "Nur Ankündigende und Agenten mit Veröffentlichungsberechtigung können schreiben. Mitglieder können in Threads antworten und reagieren.",
			"locked_effect":         "Niemand kann schreiben, antworten, reagieren, bearbeiten oder anheften. Lesen und Suchen bleiben möglich.",
			"archived_effect":       "Der Kanal ist schreibgeschützt und in der üblichen Kanalliste ausgeblendet. Suchen und Wiederherstellen bleiben möglich.",
			"announcement_composer": "Nur Ankündigende können hier schreiben. Sie können in Threads antworten und reagieren.",
			"locked_composer":       "Dieser Kanal ist gesperrt. Sie können lesen und suchen; die Arbeitsbereichsadministration kann ihn öffnen.",
			"archived_composer":     "Dieser Kanal ist archiviert. Sie können lesen und suchen; die Arbeitsbereichsadministration kann ihn wiederherstellen.",
			"loading":               "Kanalstatus wird geladen. Bitte vor dem Schreiben warten.", "error": "Kanalstatus konnte nicht geladen werden. Erneut versuchen, um fortzufahren.", "retry": "Status erneut laden", "updated": "Kanalstatus geändert", "invalid": "Wählen Sie einen erlaubten Status und geben Sie einen Grund an. Eine Endzeit gilt nur für eine zukünftige Sperre.",
		},
		"ar": {
			"title": "الحالة", "open": "مفتوحة", "announcements": "الإعلانات فقط", "locked": "مقفلة", "archived": "مؤرشفة",
			"open_channel": "فتح القناة", "archive_empty": "لا توجد قنوات مؤرشفة. ستظهر القنوات هنا عند أرشفتها.", "system_line": "غيّر {actor} حالة القناة إلى {status}: {reason}",
			"permission_composer": "ليس لديك إذن بالنشر هنا. اطلب الوصول من مسؤول القناة.", "revision_conflict": "تغيرت الحالة أثناء التحرير. أعد تحميل الحالة وحاول مجددًا.", "channel_held": "هذه القناة مقفلة أو خاضعة لحفظ السجلات. لا يمكن أرشفتها.", "permission_denied": "تغيرت صلاحياتك. أعد تحميل الحالة قبل المحاولة مجددًا.", "last_reopener": "يجب إبقاء شخص واحد على الأقل قادرًا على إعادة فتح القناة قبل إزالة الوصول.",
			"unknown": "الحالة غير متاحة", "change": "تغيير الحالة", "restore": "استعادة", "confirm": "تأكيد تغيير الحالة",
			"reason": "توضيح السبب", "until": "إنهاء القفل في (اختياري)", "by": "تم التغيير بواسطة", "when": "وقت التغيير", "why": "السبب", "ends": "تنتهي في", "system": "النظام", "none": "غير محدد",
			"open_effect":           "يمكن للأعضاء المصرح لهم النشر والرد والتفاعل والتحرير.",
			"announcements_effect":  "يمكن فقط لناشري الإعلانات والوكلاء المصرح لهم النشر. يمكن للأعضاء الرد في المناقشات والتفاعل.",
			"locked_effect":         "لا يمكن لأحد النشر أو الرد أو التفاعل أو التحرير أو التثبيت. تبقى القراءة والبحث متاحين.",
			"archived_effect":       "القناة للقراءة فقط ومخفية من قائمة القنوات المعتادة. يبقى البحث والاستعادة متاحين.",
			"announcement_composer": "يمكن فقط لناشري الإعلانات النشر هنا. يمكنك الرد في المناقشات والتفاعل.",
			"locked_composer":       "هذه القناة مقفلة. يمكنك القراءة والبحث؛ يمكن لمسؤول مساحة العمل إعادة فتحها.",
			"archived_composer":     "هذه القناة مؤرشفة. يمكنك القراءة والبحث؛ يمكن لمسؤول مساحة العمل استعادتها.",
			"loading":               "جار تحميل حالة القناة. يرجى الانتظار قبل النشر.", "error": "تعذر تحميل حالة القناة. أعد المحاولة للمتابعة.", "retry": "إعادة تحميل الحالة", "updated": "تم تغيير حالة القناة", "invalid": "اختر حالة مسموحة وأدخل السبب. ينطبق وقت الانتهاء على قفل مستقبلي فقط.",
		},
	}
	locale := "en-US"
	if strings.HasPrefix(m.Locale, "de") {
		locale = "de-DE"
	} else if strings.HasPrefix(m.Locale, "ar") {
		locale = "ar"
	}
	return chatbug039Text(key, copy[locale][key], copy["en-US"][key])
}
