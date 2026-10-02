package chatui

// CHATSIDE-001 owns the strings of the sidebar's own sections: Favorites, the
// section menu, the name field and its refusals, in en-US, de-DE and ar.
const (
	keyChatside001Favorites      = "chat.side001.favorites"
	keyChatside001AddFavorite    = "chat.side001.add_favorite"
	keyChatside001RemoveFavorite = "chat.side001.remove_favorite"
	keyChatside001MoveNew        = "chat.side001.move_new"
	keyChatside001SectionMore    = "chat.side001.section_more"
	keyChatside001Rename         = "chat.side001.rename"
	keyChatside001Delete         = "chat.side001.delete"
	keyChatside001ErrEmpty       = "chat.side001.err_empty"
	keyChatside001ErrLong        = "chat.side001.err_long"
	keyChatside001ErrDuplicate   = "chat.side001.err_duplicate"
	keyChatside001ErrReserved    = "chat.side001.err_reserved"
	keyChatside001ErrLimit       = "chat.side001.err_limit"
	keyChatside001UnreadIn       = "chat.side001.unread_in"
	keyChatside001NotSaved       = "chat.side001.not_saved"
)

var chatside001Copy = map[string]map[string]string{
	"en-US": {
		keyChatside001Favorites:      "Favorites",
		keyChatside001AddFavorite:    "Add to favorites",
		keyChatside001RemoveFavorite: "Remove from favorites",
		keyChatside001MoveNew:        "Move to a new section…",
		keyChatside001SectionMore:    "Options for section {name}",
		keyChatside001Rename:         "Rename section",
		keyChatside001Delete:         "Delete section",
		keyChatside001ErrEmpty:       "Give the section a name.",
		keyChatside001ErrLong:        "Use 40 characters or fewer.",
		keyChatside001ErrDuplicate:   "You already have a section with that name.",
		keyChatside001ErrReserved:    "Favorites is already a section.",
		keyChatside001ErrLimit:       "You can have up to 20 sections of your own.",
		keyChatside001UnreadIn:       "{n} unread in {name}",
		keyChatside001NotSaved:       "That change was not saved. Your sidebar is back as it was.",
	},
	"de-DE": {
		keyChatside001Favorites:      "Favoriten",
		keyChatside001AddFavorite:    "Zu Favoriten hinzufügen",
		keyChatside001RemoveFavorite: "Aus Favoriten entfernen",
		keyChatside001MoveNew:        "In einen neuen Bereich verschieben…",
		keyChatside001SectionMore:    "Optionen für Bereich {name}",
		keyChatside001Rename:         "Bereich umbenennen",
		keyChatside001Delete:         "Bereich löschen",
		keyChatside001ErrEmpty:       "Gib dem Bereich einen Namen.",
		keyChatside001ErrLong:        "Höchstens 40 Zeichen.",
		keyChatside001ErrDuplicate:   "Du hast schon einen Bereich mit diesem Namen.",
		keyChatside001ErrReserved:    "Favoriten ist bereits ein Bereich.",
		keyChatside001ErrLimit:       "Du kannst bis zu 20 eigene Bereiche haben.",
		keyChatside001UnreadIn:       "{n} ungelesen in {name}",
		keyChatside001NotSaved:       "Die Änderung wurde nicht gespeichert. Deine Seitenleiste ist wieder wie vorher.",
	},
	"ar": {
		keyChatside001Favorites:      "المفضلة",
		keyChatside001AddFavorite:    "إضافة إلى المفضلة",
		keyChatside001RemoveFavorite: "إزالة من المفضلة",
		keyChatside001MoveNew:        "نقل إلى قسم جديد…",
		keyChatside001SectionMore:    "خيارات القسم {name}",
		keyChatside001Rename:         "إعادة تسمية القسم",
		keyChatside001Delete:         "حذف القسم",
		keyChatside001ErrEmpty:       "أعطِ القسم اسماً.",
		keyChatside001ErrLong:        "استخدم 40 حرفاً أو أقل.",
		keyChatside001ErrDuplicate:   "لديك قسم بهذا الاسم بالفعل.",
		keyChatside001ErrReserved:    "المفضلة قسم موجود بالفعل.",
		keyChatside001ErrLimit:       "يمكنك امتلاك ما يصل إلى 20 قسماً خاصاً بك.",
		keyChatside001UnreadIn:       "{n} غير مقروءة في {name}",
		keyChatside001NotSaved:       "لم يُحفظ هذا التغيير. عاد الشريط الجانبي كما كان.",
	},
}

// Chatside001NotSaved is the notice shown when a sidebar change was refused and
// has been put back; the browser build raises it.
func Chatside001NotSaved(locale string) string {
	return chatbug039Text(keyChatside001NotSaved, chatside001Copy[chatEmojiLocale(locale)][keyChatside001NotSaved], chatside001Copy["en-US"][keyChatside001NotSaved])
}

// Chatside001NameError is the plain message for a section name the sidebar
// refuses ("" when the name is fine), so the browser build, which validates
// before it sends, says the same words as the form.
func Chatside001NameError(m Model, name, exceptID string) string {
	return chatside001NameError(m, name, exceptID)
}
