package chatui

import (
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Issue 3804: what the agents of a channel remember. A person chooses
// "Remember for this channel" on a message; that proposes a note bound to the
// message. A channel manager approves it, and only then do the channel's agents
// use it. Conversation details lists every note with who added it, whether it
// is waiting or approved, when it stops, and the message it came from, and
// lets the people who may Forget it do so. A note whose message was deleted or
// changed is shown as retired, with no words.

// MemoryItem is one note as the server told it to this reader.
type MemoryItem struct {
	ID, Text, Proposer string
	// Status is "proposed" or "approved".
	Status string
	// Source is "live", "removed" or "edited".
	Source         string
	SourcePostID   string
	SourceSequence uint64
	ExpiresAt      time.Time
	TTLDays        int
	CanApprove     bool
	CanForget      bool
}

// MemoryState is the memory feature as the page offers it. Enabled is set once
// the server has answered a read of this channel's notes for this person.
// Transient client state.
type MemoryState struct {
	Enabled     bool
	CanRemember bool
	IsManager   bool
	// State is "loading", "ready" or "error"; Error is a code from MemoryErrorText.
	State, Error string
	Items        []MemoryItem
	// Busy is the id of the note being changed ("" when none) or "remember".
	Busy string
	// Notice is a short confirmation announced politely.
	Notice string
	// Remember proposes the message; Approve and Forget act on a note.
	Remember func(postID string)
	Approve  func(id string)
	Forget   func(id string)
	// Refresh reads the notes again (Try again).
	Refresh func()
}

const (
	mmTitle        = "title"
	mmHint         = "hint"
	mmListLabel    = "list_label"
	mmLoading      = "loading"
	mmEmpty        = "empty"
	mmRetry        = "retry"
	mmRemember     = "remember"
	mmProposed     = "proposed"
	mmApproved     = "approved"
	mmAddedBy      = "added_by"
	mmWaitsFor     = "waits_for"
	mmExpires      = "expires"
	mmWaitsApprove = "waits_approve"
	mmRetiredGone  = "retired_removed"
	mmRetiredEdit  = "retired_edited"
	mmGoto         = "goto"
	mmApprove      = "approve"
	mmForget       = "forget"
	mmDoneProposed = "done_proposed"
	mmDoneApproved = "done_approved"
	mmDoneForgot   = "done_forgot"
	mmErrInvalid   = "err_invalid"
	mmErrNotFound  = "err_not_found"
	mmErrDenied    = "err_denied"
	mmErrConflict  = "err_conflict"
	mmErrUnavail   = "err_unavailable"
	mmYou          = "owner_you"
	mmUnknown      = "owner_unknown"
	mmDays         = "unit_days"
)

var memoryCopy = map[string]map[string]string{
	"en-US": {
		mmTitle: "What agents remember here", mmHint: "Notes this channel asked its agents to keep. Agents use a note only after a channel manager approves it, only in this channel, and only while the message it came from stays as it was.",
		mmListLabel: "Notes the agents remember", mmLoading: "Loading what agents remember", mmEmpty: "Nothing is remembered here yet. Choose Remember for this channel on a message to propose a note.",
		mmRetry: "Try again", mmRemember: "Remember for this channel", mmProposed: "Waiting for approval", mmApproved: "Approved",
		mmAddedBy: "Added by", mmWaitsFor: "Keeps for", mmExpires: "Stops being used on", mmWaitsApprove: "Not used by any agent until a channel manager approves it.",
		mmRetiredGone: "The message this note came from was deleted, so nothing is remembered. Forget it to clear it from the list.",
		mmRetiredEdit: "The message this note came from was changed, so it is no longer used. Forget it, then remember the new wording.",
		mmGoto:        "Go to message", mmApprove: "Approve", mmForget: "Forget", mmDays: "days",
		mmDoneProposed: "Proposed for this channel. A channel manager approves it before agents use it.", mmDoneApproved: "Approved. Agents in this channel can use it now.", mmDoneForgot: "Forgotten",
		mmErrInvalid:  "This message can't be remembered. It needs words, a short length (up to 500 characters) and a channel, not a direct message.",
		mmErrNotFound: "That message or note is no longer available.",
		mmErrDenied:   "Only the person who wrote the message or a channel manager can do that.",
		mmErrConflict: "That note changed meanwhile. Check the list and try again.",
		mmErrUnavail:  "This could not be saved right now. Try again.",
		mmYou:         "you", mmUnknown: "a former member",
	},
	"de-DE": {
		mmTitle: "Was Agenten hier behalten", mmHint: "Notizen, die dieser Kanal seinen Agenten zu merken aufgetragen hat. Agenten nutzen eine Notiz erst nach der Freigabe durch eine Kanalleitung, nur in diesem Kanal und nur solange die Nachricht unverändert ist.",
		mmListLabel: "Notizen, die Agenten behalten", mmLoading: "Gemerktes wird geladen", mmEmpty: "Hier wird noch nichts gemerkt. Wählen Sie bei einer Nachricht „Für diesen Kanal merken“, um eine Notiz vorzuschlagen.",
		mmRetry: "Erneut versuchen", mmRemember: "Für diesen Kanal merken", mmProposed: "Wartet auf Freigabe", mmApproved: "Freigegeben",
		mmAddedBy: "Hinzugefügt von", mmWaitsFor: "Gilt für", mmExpires: "Wird nicht mehr genutzt ab", mmWaitsApprove: "Kein Agent nutzt sie, bis eine Kanalleitung sie freigibt.",
		mmRetiredGone: "Die Nachricht, aus der diese Notiz stammt, wurde gelöscht. Es wird nichts gemerkt. Vergessen Sie die Notiz, um sie aus der Liste zu entfernen.",
		mmRetiredEdit: "Die Nachricht, aus der diese Notiz stammt, wurde geändert und wird nicht mehr genutzt. Vergessen Sie die Notiz und merken Sie sich die neue Fassung.",
		mmGoto:        "Zur Nachricht", mmApprove: "Freigeben", mmForget: "Vergessen", mmDays: "Tage",
		mmDoneProposed: "Für diesen Kanal vorgeschlagen. Eine Kanalleitung gibt sie frei, bevor Agenten sie nutzen.", mmDoneApproved: "Freigegeben. Agenten in diesem Kanal können sie jetzt nutzen.", mmDoneForgot: "Vergessen",
		mmErrInvalid:  "Diese Nachricht kann nicht gemerkt werden. Sie braucht Text, höchstens 500 Zeichen und einen Kanal, keine Direktnachricht.",
		mmErrNotFound: "Diese Nachricht oder Notiz ist nicht mehr verfügbar.",
		mmErrDenied:   "Das können nur die Person, die die Nachricht geschrieben hat, oder eine Kanalleitung.",
		mmErrConflict: "Die Notiz hat sich inzwischen geändert. Prüfen Sie die Liste und versuchen Sie es erneut.",
		mmErrUnavail:  "Das konnte gerade nicht gespeichert werden. Versuchen Sie es erneut.",
		mmYou:         "Sie", mmUnknown: "ein ehemaliges Mitglied",
	},
	"ar": {
		mmTitle: "ما يتذكره الوكلاء هنا", mmHint: "ملاحظات طلبت هذه القناة من وكلائها الاحتفاظ بها. لا يستخدم الوكلاء الملاحظة إلا بعد موافقة مدير القناة، وفي هذه القناة فقط، وما دامت الرسالة التي جاءت منها كما هي.",
		mmListLabel: "الملاحظات التي يتذكرها الوكلاء", mmLoading: "جارٍ تحميل ما يتذكره الوكلاء", mmEmpty: "لا شيء متذكَّر هنا بعد. اختر «تذكّر لهذه القناة» في رسالة لاقتراح ملاحظة.",
		mmRetry: "حاول مرة أخرى", mmRemember: "تذكّر لهذه القناة", mmProposed: "بانتظار الموافقة", mmApproved: "تمت الموافقة",
		mmAddedBy: "أضافها", mmWaitsFor: "تُحفظ لمدة", mmExpires: "يتوقف استخدامها في", mmWaitsApprove: "لا يستخدمها أي وكيل حتى يوافق عليها مدير القناة.",
		mmRetiredGone: "حُذفت الرسالة التي جاءت منها هذه الملاحظة، لذا لا يُتذكَّر شيء. انسَها لإزالتها من القائمة.",
		mmRetiredEdit: "تغيّرت الرسالة التي جاءت منها هذه الملاحظة، لذا لم تعد مستخدمة. انسَها ثم تذكّر الصياغة الجديدة.",
		mmGoto:        "الانتقال إلى الرسالة", mmApprove: "موافقة", mmForget: "نسيان", mmDays: "يومًا",
		mmDoneProposed: "تم اقتراحها لهذه القناة. يوافق عليها مدير القناة قبل أن يستخدمها الوكلاء.", mmDoneApproved: "تمت الموافقة. يمكن للوكلاء في هذه القناة استخدامها الآن.", mmDoneForgot: "تم النسيان",
		mmErrInvalid:  "لا يمكن تذكّر هذه الرسالة. يجب أن تحتوي على نص لا يتجاوز 500 حرف وأن تكون في قناة لا في رسالة مباشرة.",
		mmErrNotFound: "لم تعد هذه الرسالة أو الملاحظة متاحة.",
		mmErrDenied:   "يستطيع ذلك كاتب الرسالة أو مدير القناة فقط.",
		mmErrConflict: "تغيّرت الملاحظة في هذه الأثناء. تحقق من القائمة وحاول مرة أخرى.",
		mmErrUnavail:  "تعذّر الحفظ الآن. حاول مرة أخرى.",
		mmYou:         "أنت", mmUnknown: "عضو سابق",
	},
}

// MemoryText is the copy of the memory surfaces in the viewer's language.
func MemoryText(locale, key string) string {
	return chatbug039Text(key, memoryCopy[chatEmojiLocale(locale)][key], memoryCopy["en-US"][key])
}

// MemoryErrorText names a refusal code in plain words.
func MemoryErrorText(locale, code string) string {
	switch code {
	case "":
		return ""
	case "invalid_argument":
		return MemoryText(locale, mmErrInvalid)
	case "not_found":
		return MemoryText(locale, mmErrNotFound)
	case "permission_denied":
		return MemoryText(locale, mmErrDenied)
	case "conflict":
		return MemoryText(locale, mmErrConflict)
	default:
		return MemoryText(locale, mmErrUnavail)
	}
}

// MemoryDoneText is the confirmation announced after an action: "proposed",
// "approved" or "forgot".
func MemoryDoneText(locale, what string) string {
	switch what {
	case "proposed":
		return MemoryText(locale, mmDoneProposed)
	case "approved":
		return MemoryText(locale, mmDoneApproved)
	case "forgot":
		return MemoryText(locale, mmDoneForgot)
	}
	return ""
}

// MemoryOwnerLabels are the words used for who added a note.
func MemoryOwnerLabels(locale string) (you, unknown string) {
	return MemoryText(locale, mmYou), MemoryText(locale, mmUnknown)
}

// memoryAvailable says the notes can be offered here: the server answered for
// this conversation and it is one of ours.
func memoryAvailable(m Model) bool {
	if !m.Memory.Enabled || m.SelectedID == "" || m.selected().Agent {
		return false
	}
	// A conversation hosted by another company is served by that company.
	if host := m.selected().HostTenantID; host != "" && host != m.CurrentTenantID {
		return false
	}
	return true
}

func memoryHas(m Model, postID string) bool {
	for _, item := range m.Memory.Items {
		if item.SourcePostID == postID && item.Source == "live" {
			return true
		}
	}
	return false
}

// memoryMenuItem is the Remember for this channel row of a message's menu. It
// is offered to the person who wrote the message and to channel managers, for a
// message with words that is not already remembered.
func memoryMenuItem(m Model, msg Message) []ui.Node {
	if !memoryAvailable(m) || !m.Memory.CanRemember || m.Memory.Remember == nil || msg.ID == "" || strings.TrimSpace(msg.Body) == "" || chatcmd002IsCard(msg.Body) || strings.Contains(msg.Body, chat.Chatcmd002BodyMarker) {
		return nil
	}
	if !m.Memory.IsManager && msg.AuthorID != m.CurrentUser {
		return nil
	}
	if memoryHas(m, msg.ID) {
		return nil
	}
	return []ui.Node{chatux022Item("menu-item memory-menu-item", "memory-remember", msg.ID, "sparkles", MemoryText(m.Locale, mmRemember), m.Memory.Busy != "")}
}

// memoryAction runs when a Remember, Approve, Forget or Go to message control
// is pressed.
func (m Model) memoryAction(action, id string) {
	switch action {
	case "memory-remember":
		if m.Memory.Remember != nil && id != "" {
			m.Memory.Remember(id)
		}
	case "memory-approve":
		if m.Memory.Approve != nil && id != "" {
			m.Memory.Approve(id)
		}
	case "memory-forget":
		if m.Memory.Forget != nil && id != "" {
			m.Memory.Forget(id)
		}
	case "memory-goto":
		if m.Callbacks.OpenSearchMessage == nil {
			return
		}
		for _, item := range m.Memory.Items {
			if item.SourcePostID == id {
				m.Callbacks.OpenSearchMessage(m.SelectedID, id, item.SourceSequence)
				return
			}
		}
	case "memory-retry":
		if m.Memory.Refresh != nil {
			m.Memory.Refresh()
		}
	}
}

func memoryItem(m Model, item MemoryItem) ui.Node {
	t := func(key string) string { return MemoryText(m.Locale, key) }
	retired := item.Source != "live"
	status := t(mmProposed)
	if item.Status == "approved" {
		status = t(mmApproved)
	}
	who := item.Proposer
	if who == "" {
		who = t(mmUnknown)
	}
	children := []ui.Node{}
	switch {
	case item.Source == "removed":
		children = append(children, html.P(html.Props{Class: "memory-retired", Text: t(mmRetiredGone)}))
	case item.Source == "edited":
		children = append(children, html.P(html.Props{Class: "memory-retired", Text: t(mmRetiredEdit)}))
	default:
		children = append(children, html.P(html.Props{Class: "memory-text", Dir: "auto", Text: item.Text}))
	}
	badge := html.Span(html.Props{Class: "memory-badge memory-badge-" + item.Status, Text: status})
	meta := t(mmAddedBy) + " " + who
	if !retired {
		switch {
		case item.Status == "approved" && !item.ExpiresAt.IsZero():
			meta += " · " + t(mmExpires) + " " + formatDay(m.Locale, item.ExpiresAt.Local(), true)
		case item.Status == "proposed" && item.TTLDays > 0:
			meta += " · " + t(mmWaitsFor) + " " + m.n(item.TTLDays) + " " + t(mmDays)
		}
	}
	children = append(children, html.Div(html.Props{Class: "memory-badges"}, badge), html.P(html.Props{Class: "memory-meta", Text: meta}))
	if !retired && item.Status == "proposed" {
		children = append(children, html.P(html.Props{Class: "memory-note", Text: t(mmWaitsApprove)}))
	}
	label := item.Text
	if retired {
		label = status
	}
	var actions []ui.Node
	busy := m.Memory.Busy != ""
	if !retired && item.SourcePostID != "" && m.Callbacks.OpenSearchMessage != nil {
		actions = append(actions, html.Button(html.Props{Type: "button", Class: "button secondary memory-action", Data: map[string]string{"action": "memory-goto", "id": item.SourcePostID},
			Aria: map[string]string{"label": t(mmGoto) + ": " + label}, Text: t(mmGoto)}))
	}
	if item.CanApprove && m.Memory.Approve != nil {
		actions = append(actions, html.Button(html.Props{Type: "button", Class: "button primary memory-action", Disabled: busy, Data: map[string]string{"action": "memory-approve", "id": item.ID},
			Aria: map[string]string{"label": t(mmApprove) + ": " + label, "busy": boolString(m.Memory.Busy == item.ID)}, Text: t(mmApprove)}))
	}
	if item.CanForget && m.Memory.Forget != nil {
		actions = append(actions, html.Button(html.Props{Type: "button", Class: "button secondary memory-action", Disabled: busy, Data: map[string]string{"action": "memory-forget", "id": item.ID},
			Aria: map[string]string{"label": t(mmForget) + ": " + label, "busy": boolString(m.Memory.Busy == item.ID)}, Text: t(mmForget)}))
	}
	if len(actions) > 0 {
		children = append(children, html.Div(html.Props{Class: "memory-actions"}, actions...))
	}
	class := "memory-item"
	if retired {
		class += " memory-item-retired"
	}
	return html.Li(html.Props{Class: class, Data: map[string]string{"memory": item.ID, "sequence": strconv.FormatUint(item.SourceSequence, 10)}}, children...)
}

// memoryDetailsSection is the "What agents remember here" section of
// Conversation details. It is absent where the server does not offer notes
// (a direct message, a conversation hosted elsewhere, a failed first read).
func memoryDetailsSection(m Model) ui.Node {
	if !memoryAvailable(m) {
		return nil
	}
	t := func(key string) string { return MemoryText(m.Locale, key) }
	body := []ui.Node{
		html.H3(html.Props{ID: "chat-details-memory-title", Text: t(mmTitle), Raw: map[string]any{"tabindex": "-1"}}),
		html.P(html.Props{Class: "field-hint", Text: t(mmHint)}),
		html.Div(html.Props{ID: "chat-memory-live", Class: "memory-live", Role: "status", Aria: map[string]string{"live": "polite"}, Text: m.Memory.Notice}),
	}
	switch {
	case m.Memory.State == "loading" && len(m.Memory.Items) == 0:
		body = append(body, html.P(html.Props{Class: "memory-status", Text: t(mmLoading)}))
	case m.Memory.State == "error" && len(m.Memory.Items) == 0:
		body = append(body, html.P(html.Props{Class: "memory-error", Role: "alert", Text: MemoryErrorText(m.Locale, m.Memory.Error)}),
			html.Button(html.Props{Type: "button", Class: "button secondary memory-action", Data: map[string]string{"action": "memory-retry"}, Text: t(mmRetry)}))
	case len(m.Memory.Items) == 0:
		body = append(body, html.P(html.Props{Class: "memory-status", Text: t(mmEmpty)}))
	default:
		items := make([]ui.Node, 0, len(m.Memory.Items))
		for _, item := range m.Memory.Items {
			items = append(items, memoryItem(m, item))
		}
		body = append(body, html.Ul(html.Props{Class: "memory-list", Aria: map[string]string{"label": t(mmListLabel)}}, items...))
	}
	if m.Memory.Error != "" && m.Memory.State != "error" {
		body = append(body, html.P(html.Props{Class: "memory-error", Role: "alert", Text: MemoryErrorText(m.Locale, m.Memory.Error)}))
	}
	return html.Section(html.Props{ID: "chat-details-memory", Class: "details-section details-memory", Aria: map[string]string{"labelledby": "chat-details-memory-title"}}, body...)
}

// ChatMemoryStyles is joined into the workspace stylesheet through
// ChatMsgListStyles.
const ChatMemoryStyles = `
.details-memory .memory-list{list-style:none;margin:0 0 8px;padding:0;display:flex;flex-direction:column;gap:8px}
.memory-item{padding:10px;border:1px solid var(--hcm-color-border);border-radius:10px;min-inline-size:0}
.memory-item-retired{opacity:.85;border-style:dashed}
.memory-text{margin:0 0 6px;font-size:.9375rem;line-height:1.45;overflow-wrap:anywhere}
.memory-retired,.memory-note,.memory-status{margin:0 0 6px;font-size:.8125rem;line-height:1.45;color:var(--hcm-color-text-muted,inherit)}
.memory-meta{margin:0 0 6px;font-size:.8125rem;line-height:1.45;overflow-wrap:anywhere;color:var(--hcm-color-text-muted,inherit)}
.memory-badges{display:flex;flex-wrap:wrap;gap:6px;margin-block-end:6px}
.memory-badge{display:inline-block;padding:2px 8px;border:1px solid var(--hcm-color-border);border-radius:10px;font-size:.75rem;font-weight:600}
.memory-actions{display:flex;flex-wrap:wrap;gap:8px}
.memory-actions .button,.details-memory .memory-action{min-block-size:44px;min-inline-size:44px}
.memory-live:empty{display:none}
.memory-live{margin:0 0 8px;font-size:.875rem;font-weight:600}
.memory-error{margin:0 0 8px;font-size:.875rem;line-height:1.45;color:var(--hcm-color-danger,#b00020)}
`
