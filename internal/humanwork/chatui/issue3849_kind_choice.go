package chatui

// Issue 3849: the New conversation control opens the create dialog with no type
// selected, so nobody starts a company-visible channel by leaving a default in
// place. Each type card already says who can see the conversation; the name,
// people and submit appear once a type is chosen.

// KindUnchosen is the create dialog's kind before the person has chosen one.
const KindUnchosen ConversationKind = "unchosen"

var chatissue3849Copy = map[string]map[string]string{
	"en-US": {"prompt": "Choose a type to continue. Nothing is created until you do."},
	"de-DE": {"prompt": "Wählen Sie einen Typ, um fortzufahren. Bis dahin wird nichts erstellt."},
	"ar":    {"prompt": "اختر نوعاً للمتابعة. لن يتم إنشاء أي شيء قبل ذلك."},
}

func chatissue3849Prompt(m Model) string { return laneText(m, chatissue3849Copy, "prompt") }
