package productui

import "github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"

// chatbug059Translations are the Chat keys whose German and Arabic copy was
// missing from chatTranslations, so the English sentence was shown on those
// pages (CHATBUG-059): the join dialog, moving a conversation, the person card's
// own states and links, the image viewer's sizes, and the first line of a direct
// message in Arabic.
func chatbug059Translations(locale string) map[string]string {
	switch locale {
	case "de-DE":
		return map[string]string{
			chatui.KeyDirectReports:        "Direkt unterstellte Personen",
			chatui.KeyViewOrgChart:         "Im Organigramm anzeigen",
			chatui.KeyPersonLoading:        "Personendetails werden geladen …",
			chatui.KeyPersonUnavailable:    "Personendetails sind nicht verfügbar.",
			chatui.KeyMoveConversationUp:   "Unterhaltung nach oben",
			chatui.KeyMoveConversationDown: "Unterhaltung nach unten",
			chatui.KeyJoinChannelTitle:     "#{name} beitreten?",
			chatui.KeyJoinChannelBody:      "Treten Sie diesem Kanal bei, um ihn in Ihrer Seitenleiste zu behalten. Sie können ihn jederzeit verlassen.",
			chatui.KeyJoinChannelConfirm:   "Kanal beitreten",
			chatui.KeyJoinChannelDismiss:   "Nicht jetzt",
			chatui.KeyJoinPending:          "Beitritt läuft …",
			chatui.KeyImageActualSize:      "Bild in Originalgröße anzeigen",
			chatui.KeyImageFitToScreen:     "Bild an den Bildschirm anpassen",
		}
	case "ar":
		return map[string]string{
			chatui.KeyDirectReports:        "المرؤوسون المباشرون",
			chatui.KeyViewOrgChart:         "عرض في الهيكل التنظيمي",
			chatui.KeyPersonLoading:        "جارٍ تحميل تفاصيل الشخص…",
			chatui.KeyPersonUnavailable:    "تفاصيل الشخص غير متاحة.",
			chatui.KeyMoveConversationUp:   "نقل المحادثة للأعلى",
			chatui.KeyMoveConversationDown: "نقل المحادثة للأسفل",
			chatui.KeyJoinChannelTitle:     "هل تريد الانضمام إلى #{name}؟",
			chatui.KeyJoinChannelBody:      "انضم إلى هذه القناة لتبقى في شريطك الجانبي. يمكنك المغادرة في أي وقت.",
			chatui.KeyJoinChannelConfirm:   "الانضمام إلى القناة",
			chatui.KeyJoinChannelDismiss:   "ليس الآن",
			chatui.KeyJoinPending:          "جارٍ الانضمام…",
			chatui.KeyImageActualSize:      "عرض الصورة بحجمها الفعلي",
			chatui.KeyImageFitToScreen:     "ملاءمة الصورة للشاشة",
			chatui.KeyIntroDirect:          "هذه المحادثة خاصة بالأشخاص المشاركين فيها.",
		}
	}
	return nil
}
