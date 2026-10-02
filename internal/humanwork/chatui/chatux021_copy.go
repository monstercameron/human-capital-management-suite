package chatui

// CHATUX-021 and CHATUX-019 own the strings below, each in en-US, de-DE and ar.
const (
	keyChatux021PurposeAdd    = "chat.ux021.purpose_add"
	keyChatux021PurposeEdit   = "chat.ux021.purpose_edit"
	keyChatux021PurposeHint   = "chat.ux021.purpose_hint"
	keyChatux021Saved         = "chat.ux021.saved"
	keyChatux021StatusChoose  = "chat.ux021.status_choose"
	keyChatux021MemberAdded   = "chat.ux021.member_added"
	keyChatux021SomeoneElse   = "chat.ux021.someone"
	keyChatux021PersonBack    = "chat.ux021.person_back"
	keyChatux019PinnedByWhen  = "chat.ux019.pinned_by_when"
	keyChatux019PinnedBy      = "chat.ux019.pinned_by"
	keyChatux019PinnedWhen    = "chat.ux019.pinned_when"
	keyChatux019PinnedAction  = "chat.ux019.pinned_unpin"
	keyChatux019ReactedOne    = "chat.ux019.reacted_one"
	keyChatux019ReactedMany   = "chat.ux019.reacted_many"
	keyChatux019ReactedCount  = "chat.ux019.reacted_count"
	keyChatux019And           = "chat.ux019.and"
	keyChatux019You           = "chat.ux019.you"
	keyChatux019EmojiThumbsUp = "chat.ux019.emoji.thumbs_up"
	keyChatux019EmojiHeart    = "chat.ux019.emoji.heart"
	keyChatux019EmojiJoy      = "chat.ux019.emoji.joy"
	keyChatux019EmojiParty    = "chat.ux019.emoji.party"
	keyChatux019EmojiEyes     = "chat.ux019.emoji.eyes"
	keyChatux019EmojiFolded   = "chat.ux019.emoji.folded_hands"
	keyChatux019EmojiCheck    = "chat.ux019.emoji.check"
	keyChatux019EmojiFire     = "chat.ux019.emoji.fire"
	keyChatux019EmojiUnnamed  = "chat.ux019.emoji.unnamed"
)

var chatux021Copy = map[string]map[string]string{
	"en-US": {
		keyChatux021PurposeAdd:    "Add a purpose",
		keyChatux021PurposeEdit:   "Edit purpose",
		keyChatux021PurposeHint:   "Enter to save · Esc to cancel",
		keyChatux021Saved:         "Saved",
		keyChatux021StatusChoose:  "Choose a status",
		keyChatux021MemberAdded:   "{actor} added {name}",
		keyChatux021SomeoneElse:   "someone",
		keyChatux021PersonBack:    "Back to conversation details",
		keyChatux019PinnedByWhen:  "Pinned by {name} · {when}",
		keyChatux019PinnedBy:      "Pinned by {name}",
		keyChatux019PinnedWhen:    "Pinned {when}",
		keyChatux019PinnedAction:  "Unpin",
		keyChatux019ReactedOne:    "{names} reacted with {emoji}",
		keyChatux019ReactedMany:   "{names} reacted with {emoji}",
		keyChatux019ReactedCount:  "{n} people reacted with {emoji}",
		keyChatux019And:           "and",
		keyChatux019You:           "You",
		keyChatux019EmojiThumbsUp: "thumbs up",
		keyChatux019EmojiHeart:    "red heart",
		keyChatux019EmojiJoy:      "face with tears of joy",
		keyChatux019EmojiParty:    "party popper",
		keyChatux019EmojiEyes:     "eyes",
		keyChatux019EmojiFolded:   "folded hands",
		keyChatux019EmojiCheck:    "check mark",
		keyChatux019EmojiFire:     "fire",
		keyChatux019EmojiUnnamed:  "an emoji",
	},
	"de-DE": {
		keyChatux021PurposeAdd:    "Zweck hinzufügen",
		keyChatux021PurposeEdit:   "Zweck bearbeiten",
		keyChatux021PurposeHint:   "Enter zum Speichern · Esc zum Abbrechen",
		keyChatux021Saved:         "Gespeichert",
		keyChatux021StatusChoose:  "Status wählen",
		keyChatux021MemberAdded:   "{actor} hat {name} hinzugefügt",
		keyChatux021SomeoneElse:   "jemanden",
		keyChatux021PersonBack:    "Zurück zu den Details zur Unterhaltung",
		keyChatux019PinnedByWhen:  "Angeheftet von {name} · {when}",
		keyChatux019PinnedBy:      "Angeheftet von {name}",
		keyChatux019PinnedWhen:    "Angeheftet {when}",
		keyChatux019PinnedAction:  "Lösen",
		keyChatux019ReactedOne:    "{names} hat mit {emoji} reagiert",
		keyChatux019ReactedMany:   "{names} haben mit {emoji} reagiert",
		keyChatux019ReactedCount:  "{n} Personen haben mit {emoji} reagiert",
		keyChatux019And:           "und",
		keyChatux019You:           "Sie",
		keyChatux019EmojiThumbsUp: "Daumen hoch",
		keyChatux019EmojiHeart:    "rotes Herz",
		keyChatux019EmojiJoy:      "Gesicht mit Freudentränen",
		keyChatux019EmojiParty:    "Konfettibombe",
		keyChatux019EmojiEyes:     "Augen",
		keyChatux019EmojiFolded:   "gefaltete Hände",
		keyChatux019EmojiCheck:    "Häkchen",
		keyChatux019EmojiFire:     "Feuer",
		keyChatux019EmojiUnnamed:  "ein Emoji",
	},
	"ar": {
		keyChatux021PurposeAdd:    "إضافة غرض",
		keyChatux021PurposeEdit:   "تعديل الغرض",
		keyChatux021PurposeHint:   "Enter للحفظ · Esc للإلغاء",
		keyChatux021Saved:         "تم الحفظ",
		keyChatux021StatusChoose:  "اختر حالة",
		keyChatux021MemberAdded:   "أضاف {actor} {name}",
		keyChatux021SomeoneElse:   "شخصًا ما",
		keyChatux021PersonBack:    "العودة إلى تفاصيل المحادثة",
		keyChatux019PinnedByWhen:  "ثبّتها {name} · {when}",
		keyChatux019PinnedBy:      "ثبّتها {name}",
		keyChatux019PinnedWhen:    "ثُبّتت {when}",
		keyChatux019PinnedAction:  "إلغاء التثبيت",
		keyChatux019ReactedOne:    "تفاعل {names} بـ {emoji}",
		keyChatux019ReactedMany:   "تفاعل {names} بـ {emoji}",
		keyChatux019ReactedCount:  "تفاعل {n} أشخاص بـ {emoji}",
		keyChatux019And:           "و",
		keyChatux019You:           "أنت",
		keyChatux019EmojiThumbsUp: "إعجاب",
		keyChatux019EmojiHeart:    "قلب أحمر",
		keyChatux019EmojiJoy:      "وجه يضحك بالدموع",
		keyChatux019EmojiParty:    "مفرقعة الاحتفال",
		keyChatux019EmojiEyes:     "عينان",
		keyChatux019EmojiFolded:   "يدان مطويتان",
		keyChatux019EmojiCheck:    "علامة صح",
		keyChatux019EmojiFire:     "نار",
		keyChatux019EmojiUnnamed:  "رمز تعبيري",
	},
}
