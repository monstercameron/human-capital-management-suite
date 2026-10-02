package chatui

import "strings"

type SavedCopy struct {
	Save, Saved, Todo, Done, All, Remove, Note, Remind, Reopen, SaveNote, SaveDue, ClearDue                             string
	Title, EmptyTodo, EmptyDone, EmptyAll, Loading, Failed, Retry, Limit, NoAccess, Removed, Deleted, AuthorUnavailable string
	Search, More, Close, Updated, Invalid, Private                                                                      string
	// Unsave labels the message menu entry and the hover-bar button of an
	// already saved message; Remove stays the label of the list's own button.
	Unsave string
}

func SavedMessagesCopy(locale string) SavedCopy {
	switch {
	case strings.HasPrefix(locale, "de"):
		return chatbug039SavedCopy(SavedCopy{"Für später speichern", "Gespeichert", "Zu erledigen", "Erledigt", "Alle", "Entfernen", "Notiz", "Erinnern", "Wieder öffnen", "Notiz speichern", "Erinnerung speichern", "Erinnerung entfernen", "Gespeicherte Nachrichten", "Fahren Sie über eine Nachricht und wählen Sie das Lesezeichen, um sie hier zu behalten.", "Was Sie abhaken, erscheint hier.", "Fahren Sie über eine Nachricht und wählen Sie das Lesezeichen, um sie hier zu behalten.", "Gespeicherte Nachrichten werden geladen.", "Die Liste konnte nicht geladen werden. Versuchen Sie es erneut.", "Erneut versuchen", "Sie haben 5.000 Nachrichten gespeichert. Entfernen Sie eine, um eine weitere zu speichern.", "Sie haben keinen Zugriff mehr auf diese Nachricht.", "Diese Nachricht wurde entfernt.", "Diese Nachricht wurde gelöscht.", "Autor nicht verfügbar", "Gespeicherte Nachrichten durchsuchen", "Mehr laden", "Schließen", "Gespeicherte Liste aktualisiert.", "Prüfen Sie die Notiz und wählen Sie einen Zeitpunkt in der Zukunft.", "Ihre private Liste", "Aus Gespeichert entfernen"}, SavedMessagesCopy("en-US"))
	case strings.HasPrefix(locale, "ar"):
		return chatbug039SavedCopy(SavedCopy{"حفظ لوقت لاحق", "المحفوظات", "للتنفيذ", "تم", "الكل", "إزالة", "ملاحظة", "ذكّرني", "إعادة فتح", "حفظ الملاحظة", "حفظ التذكير", "إزالة التذكير", "الرسائل المحفوظة", "مرّر المؤشر فوق رسالة واضغط على العلامة المرجعية لإبقائها هنا.", "ما تضع عليه علامة تم يظهر هنا.", "مرّر المؤشر فوق رسالة واضغط على العلامة المرجعية لإبقائها هنا.", "جارٍ تحميل الرسائل المحفوظة.", "تعذّر تحميل القائمة. حاول مرة أخرى.", "إعادة المحاولة", "لقد حفظت ٥٬٠٠٠ رسالة. أزل واحدة لحفظ رسالة أخرى.", "لم يعد لديك حق الوصول إلى هذه الرسالة.", "أُزيلت هذه الرسالة.", "حُذفت هذه الرسالة.", "الكاتب غير متاح", "البحث في الرسائل المحفوظة", "تحميل المزيد", "إغلاق", "تم تحديث قائمة المحفوظات.", "تحقق من الملاحظة واختر وقتًا في المستقبل.", "قائمتك الخاصة", "إزالة من المحفوظات"}, SavedMessagesCopy("en-US"))
	default:
		return chatbug039SavedCopy(SavedCopy{"Save for later", "Saved", "To do", "Done", "All", "Remove", "Note", "Remind me", "Reopen", "Save note", "Save reminder", "Remove reminder", "Saved messages", "Hover a message and press the bookmark to keep it here.", "Things you tick off appear here.", "Hover a message and press the bookmark to keep it here.", "Loading saved messages.", "We could not load your list. Try again.", "Try again", "You have saved 5,000 messages. Remove one to save another.", "You no longer have access to this message", "This message was removed.", "This message was deleted.", "Author unavailable", "Search saved messages", "Load more", "Close", "Saved list updated.", "Check your note and choose a time in the future.", "Your private list", "Remove from Saved"}, SavedCopy{})
	}
}
