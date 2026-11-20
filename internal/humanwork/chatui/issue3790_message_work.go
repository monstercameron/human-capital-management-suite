package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3790: turn a message into a task. The message menu offers Create task,
// a small form (title from the message, project, owner, due date) makes the
// task, and the message then shows what it became.

// MessageWorkLink is the work a message became, as this viewer may see it.
type MessageWorkLink struct {
	PostID, ProjectID, TaskID, Title, Status, OwnerID, DueDate string
}

// MessageWorkState is the conversion's client state. Enabled is set once the
// server has said it can make tasks; Open starts the form for one message.
type MessageWorkState struct {
	Enabled bool
	// Links holds what each message of the open conversation became, keyed by
	// message id. Anything the viewer may not read is already left out.
	Links map[string][]MessageWorkLink
	Open  func(postID string)
}

// MessageWorkProject is a project the person may create a task in.
type MessageWorkProject struct{ ID, Name string }

// MessageWorkOwner is who a task may be handed to.
type MessageWorkOwner struct{ ID, Label string }

// MessageWorkDialogView is everything the form shows.
type MessageWorkDialogView struct {
	Locale, PostID                    string
	Title, ProjectID, OwnerID, DueDay string
	Projects                          []MessageWorkProject
	Owners                            []MessageWorkOwner
	// Existing is the work this message already became, so a person can avoid
	// filing the same thing twice.
	Existing []MessageWorkLink
	// State is "loading", "ready" or "error"; Error is a code from
	// MessageWorkErrorText; Busy is true while the task is being made.
	State, Error string
	Busy         bool
}

const (
	cwMenu           = "menu"
	cwChipKind       = "chip_kind"
	cwChipsLabel     = "chips_label"
	cwOwner          = "owner"
	cwYou            = "you"
	cwStatus         = "status"
	cwDue            = "due"
	cwOpenTask       = "open_task"
	cwDialogTitle    = "dialog_title"
	cwFieldTitle     = "field_title"
	cwFieldProject   = "field_project"
	cwFieldOwner     = "field_owner"
	cwFieldDue       = "field_due"
	cwSubmit         = "submit"
	cwBusy           = "busy"
	cwCancel         = "cancel"
	cwNotice         = "notice"
	cwExisting       = "existing"
	cwChooseProject  = "choose_project"
	cwLoading        = "loading"
	cwNoProjects     = "no_projects"
	cwErrInvalid     = "err_invalid"
	cwErrNotFound    = "err_not_found"
	cwErrDenied      = "err_denied"
	cwErrUnavailable = "err_unavailable"
	cwErrTitle       = "err_title"
	cwErrProject     = "err_project"
	cwDone           = "done"
	cwOwnerMe        = "owner_me"
	cwOwnerAuthor    = "owner_author"
)

var chatworkCopy = map[string]map[string]string{
	"en-US": {
		cwMenu: "Create task", cwChipKind: "Task created", cwChipsLabel: "Work from this message",
		cwOwner: "Owner", cwYou: "You", cwStatus: "Status", cwDue: "Due", cwOpenTask: "Open task",
		cwDialogTitle: "Create a task from this message", cwFieldTitle: "Task title", cwFieldProject: "Project",
		cwFieldOwner: "Owner", cwFieldDue: "Due date (optional)", cwSubmit: "Create task", cwBusy: "Creating the task", cwCancel: "Cancel",
		cwNotice:   "Only the title you enter and a link back to this message are saved. The message text is not copied into the project.",
		cwExisting: "Already made from this message", cwChooseProject: "Choose a project", cwLoading: "Loading projects",
		cwNoProjects:     "You can't create tasks in any project yet. Ask a project owner to add you.",
		cwErrInvalid:     "Check the title, project, owner and date, then try again.",
		cwErrNotFound:    "This message is no longer available to you.",
		cwErrDenied:      "You can't create tasks in that project.",
		cwErrUnavailable: "The task could not be created right now. Try again; the same request never makes two tasks.",
		cwErrTitle:       "Enter a title for the task.", cwErrProject: "Choose a project for the task.", cwDone: "Task created", cwOwnerMe: "Me", cwOwnerAuthor: "wrote the message",
	},
	"de-DE": {
		cwMenu: "Aufgabe erstellen", cwChipKind: "Aufgabe erstellt", cwChipsLabel: "Arbeit aus dieser Nachricht",
		cwOwner: "Verantwortlich", cwYou: "Sie", cwStatus: "Status", cwDue: "Fällig", cwOpenTask: "Aufgabe öffnen",
		cwDialogTitle: "Aus dieser Nachricht eine Aufgabe erstellen", cwFieldTitle: "Titel der Aufgabe", cwFieldProject: "Projekt",
		cwFieldOwner: "Verantwortlich", cwFieldDue: "Fälligkeitsdatum (optional)", cwSubmit: "Aufgabe erstellen", cwBusy: "Aufgabe wird erstellt", cwCancel: "Abbrechen",
		cwNotice:   "Gespeichert werden nur der eingegebene Titel und ein Link zu dieser Nachricht. Der Nachrichtentext wird nicht ins Projekt kopiert.",
		cwExisting: "Bereits aus dieser Nachricht erstellt", cwChooseProject: "Projekt wählen", cwLoading: "Projekte werden geladen",
		cwNoProjects:     "Sie können noch in keinem Projekt Aufgaben erstellen. Bitten Sie eine Projektleitung, Sie hinzuzufügen.",
		cwErrInvalid:     "Prüfen Sie Titel, Projekt, Verantwortliche und Datum und versuchen Sie es erneut.",
		cwErrNotFound:    "Diese Nachricht ist für Sie nicht mehr verfügbar.",
		cwErrDenied:      "Sie dürfen in diesem Projekt keine Aufgaben erstellen.",
		cwErrUnavailable: "Die Aufgabe konnte gerade nicht erstellt werden. Versuchen Sie es erneut; dieselbe Anfrage erzeugt nie zwei Aufgaben.",
		cwErrTitle:       "Geben Sie einen Titel für die Aufgabe ein.", cwErrProject: "Wählen Sie ein Projekt für die Aufgabe.", cwDone: "Aufgabe erstellt", cwOwnerMe: "Ich", cwOwnerAuthor: "hat die Nachricht geschrieben",
	},
	"ar": {
		cwMenu: "إنشاء مهمة", cwChipKind: "تم إنشاء مهمة", cwChipsLabel: "عمل من هذه الرسالة",
		cwOwner: "المسؤول", cwYou: "أنت", cwStatus: "الحالة", cwDue: "الاستحقاق", cwOpenTask: "فتح المهمة",
		cwDialogTitle: "إنشاء مهمة من هذه الرسالة", cwFieldTitle: "عنوان المهمة", cwFieldProject: "المشروع",
		cwFieldOwner: "المسؤول", cwFieldDue: "تاريخ الاستحقاق (اختياري)", cwSubmit: "إنشاء مهمة", cwBusy: "جارٍ إنشاء المهمة", cwCancel: "إلغاء",
		cwNotice:   "يُحفظ فقط العنوان الذي تدخله ورابط يعود إلى هذه الرسالة. لا يُنسخ نص الرسالة إلى المشروع.",
		cwExisting: "تم إنشاؤه سابقًا من هذه الرسالة", cwChooseProject: "اختر مشروعًا", cwLoading: "جارٍ تحميل المشاريع",
		cwNoProjects:     "لا يمكنك إنشاء مهام في أي مشروع بعد. اطلب من مالك المشروع إضافتك.",
		cwErrInvalid:     "تحقق من العنوان والمشروع والمسؤول والتاريخ ثم حاول مرة أخرى.",
		cwErrNotFound:    "لم تعد هذه الرسالة متاحة لك.",
		cwErrDenied:      "لا يمكنك إنشاء مهام في هذا المشروع.",
		cwErrUnavailable: "تعذر إنشاء المهمة الآن. حاول مرة أخرى؛ لن ينشئ الطلب نفسه مهمتين أبدًا.",
		cwErrTitle:       "أدخل عنوانًا للمهمة.", cwErrProject: "اختر مشروعًا للمهمة.", cwDone: "تم إنشاء المهمة", cwOwnerMe: "أنا", cwOwnerAuthor: "كاتب الرسالة",
	},
}

// MessageWorkText is the copy of the Create task surfaces in the viewer's language.
func MessageWorkText(locale, key string) string {
	return chatbug039Text(key, chatworkCopy[chatEmojiLocale(locale)][key], chatworkCopy["en-US"][key])
}

// MessageWorkOwnerLabels are the words for the two people a task can go to:
// "Me", and the suffix that names the author of the message.
func MessageWorkOwnerLabels(locale string) (me, author string) {
	return MessageWorkText(locale, cwOwnerMe), MessageWorkText(locale, cwOwnerAuthor)
}

// MessageWorkDoneText announces a made task to a screen reader.
func MessageWorkDoneText(locale string) string { return MessageWorkText(locale, cwDone) }

// MessageWorkErrorText names a refusal code from the server in plain words.
func MessageWorkErrorText(locale, code string) string {
	switch code {
	case "invalid_argument":
		return MessageWorkText(locale, cwErrInvalid)
	case "not_found":
		return MessageWorkText(locale, cwErrNotFound)
	case "permission_denied":
		return MessageWorkText(locale, cwErrDenied)
	case "title":
		return MessageWorkText(locale, cwErrTitle)
	case "project":
		return MessageWorkText(locale, cwErrProject)
	case "":
		return ""
	default:
		return MessageWorkText(locale, cwErrUnavailable)
	}
}

// chatworkMenuItem is the Create task row of a message's menu. It is offered
// only where the server can make tasks and the form can open.
func chatworkMenuItem(m Model, msg Message) []ui.Node {
	if !m.MessageWork.Enabled || m.MessageWork.Open == nil || m.CurrentUser == "" || msg.ID == "" || issue3812Restricted(m) {
		return nil
	}
	// A conversation hosted by another company is served by that company.
	if host := m.selected().HostTenantID; host != "" && host != m.CurrentTenantID {
		return nil
	}
	label := MessageWorkText(m.Locale, cwMenu)
	return []ui.Node{chatux022Item("menu-item chatwork-menu-item", "work-open", msg.ID, "checklist", label, false)}
}

// chatworkOpen runs when the Create task row is chosen.
func (m Model) chatworkOpen(postID string) {
	if m.MessageWork.Open == nil || postID == "" {
		return
	}
	// The dispatcher closes the menu after every action.
	m.MessageWork.Open(postID)
}

func chatworkOwnerName(m Model, msg Message, ownerID string) string {
	switch {
	case ownerID == "":
		return ""
	case ownerID == m.CurrentUser:
		return MessageWorkText(m.Locale, cwYou)
	case ownerID == msg.AuthorID && strings.TrimSpace(msg.Author) != "":
		return msg.Author
	}
	for _, person := range m.SearchDirectory {
		if person.ID == ownerID && strings.TrimSpace(person.Name) != "" {
			return person.Name
		}
	}
	return ""
}

// chatworkChips is the "Task created" line under a message: one chip for each
// task the message became, with its owner, status and due date. The chips are
// a projection of what the server returned for this viewer, so work in a
// project the viewer cannot read is simply not here.
func chatworkChips(m Model, msg Message) []ui.Node {
	links := m.MessageWork.Links[msg.ID]
	if len(links) == 0 {
		return nil
	}
	kind := MessageWorkText(m.Locale, cwChipKind)
	chips := make([]ui.Node, 0, len(links))
	for _, link := range links {
		parts := []ui.Node{icon("checklist"), html.Span(html.Props{Class: "chatwork-chip-kind", Text: kind}),
			html.Span(html.Props{Class: "chatwork-chip-title", Dir: "auto", Text: link.Title})}
		summary := []string{kind, link.Title}
		if owner := chatworkOwnerName(m, msg, link.OwnerID); owner != "" {
			parts = append(parts, html.Span(html.Props{Class: "chatwork-chip-meta", Text: MessageWorkText(m.Locale, cwOwner) + ": " + owner}))
			summary = append(summary, MessageWorkText(m.Locale, cwOwner)+": "+owner)
		}
		if link.Status != "" {
			parts = append(parts, html.Span(html.Props{Class: "chatwork-chip-meta", Text: MessageWorkText(m.Locale, cwStatus) + ": " + link.Status}))
			summary = append(summary, MessageWorkText(m.Locale, cwStatus)+": "+link.Status)
		}
		if link.DueDate != "" {
			parts = append(parts, html.Span(html.Props{Class: "chatwork-chip-meta", Text: MessageWorkText(m.Locale, cwDue) + ": " + link.DueDate}))
			summary = append(summary, MessageWorkText(m.Locale, cwDue)+": "+link.DueDate)
		}
		props := html.Props{Class: "chatwork-chip", Title: MessageWorkText(m.Locale, cwOpenTask), Data: map[string]string{"chatwork-task": link.TaskID},
			Aria: map[string]string{"label": MessageWorkText(m.Locale, cwOpenTask) + ". " + strings.Join(summary, ", ")}}
		if href := ProjectTaskReferenceURL(link.ProjectID, link.TaskID); href != "" {
			props.Href = href
			chips = append(chips, html.A(props, parts...))
			continue
		}
		chips = append(chips, html.Span(props, parts...))
	}
	group := html.Div(html.Props{Class: "chatwork-chips", Role: "group", Aria: map[string]string{"label": MessageWorkText(m.Locale, cwChipsLabel)}}, chips...)
	// Issue 3794: the hand-offs of this work sit under its chips.
	return append([]ui.Node{group}, workHandoffBlocks(m, msg)...)
}

// RenderMessageWorkDialog is the Create task form. The browser client draws it
// over the page, fills it, and sends the reviewed values.
func RenderMessageWorkDialog(v MessageWorkDialogView) ui.Node {
	t := func(key string) string { return MessageWorkText(v.Locale, key) }
	dir := "ltr"
	if chatEmojiLocale(v.Locale) == "ar" {
		dir = "rtl"
	}
	var body []ui.Node
	body = append(body, html.H2(html.Props{ID: "chatwork-title", Class: "chatwork-heading", Text: t(cwDialogTitle)}))
	switch {
	case v.State == "loading":
		body = append(body, html.P(html.Props{Class: "chatwork-status", Role: "status", Text: t(cwLoading)}))
	case v.State == "error" && len(v.Projects) == 0:
		body = append(body, html.P(html.Props{Class: "chatwork-error", Role: "alert", Text: MessageWorkErrorText(v.Locale, v.Error)}))
	case len(v.Projects) == 0:
		body = append(body, html.P(html.Props{Class: "chatwork-status", Role: "status", Text: t(cwNoProjects)}))
	default:
		if len(v.Existing) > 0 {
			items := make([]ui.Node, 0, len(v.Existing))
			for _, link := range v.Existing {
				label := link.Title
				if link.Status != "" {
					label += " (" + link.Status + ")"
				}
				items = append(items, html.Li(html.Props{Dir: "auto", Text: label}))
			}
			body = append(body, html.Div(html.Props{Class: "chatwork-existing"}, html.P(html.Props{Class: "chatwork-existing-label", Text: t(cwExisting)}), html.Ul(html.Props{}, items...)))
		}
		options := []ui.Node{html.Option(html.Props{Raw: map[string]any{"value": ""}, Text: t(cwChooseProject), Selected: v.ProjectID == ""})}
		for _, project := range v.Projects {
			options = append(options, html.Option(html.Props{Value: project.ID, Text: project.Name, Selected: project.ID == v.ProjectID}))
		}
		owners := make([]ui.Node, 0, len(v.Owners))
		for _, owner := range v.Owners {
			owners = append(owners, html.Option(html.Props{Value: owner.ID, Text: owner.Label, Selected: owner.ID == v.OwnerID}))
		}
		submit := t(cwSubmit)
		if v.Busy {
			submit = t(cwBusy)
		}
		field := func(id, label string, control ui.Node) ui.Node {
			return html.Div(html.Props{Class: "chatwork-field"}, html.Label(html.Props{For: id, Text: label}), control)
		}
		form := []ui.Node{
			field("chatwork-field-title", t(cwFieldTitle), html.Input(html.Props{ID: "chatwork-field-title", Name: "title", Type: "text", Dir: "auto", Value: v.Title, Disabled: v.Busy, Required: true, Raw: map[string]any{"maxlength": "200", "autocomplete": "off"}})),
			field("chatwork-field-project", t(cwFieldProject), html.Select(html.Props{ID: "chatwork-field-project", Name: "project", Disabled: v.Busy, Required: true}, options...)),
			field("chatwork-field-owner", t(cwFieldOwner), html.Select(html.Props{ID: "chatwork-field-owner", Name: "owner", Disabled: v.Busy}, owners...)),
			field("chatwork-field-due", t(cwFieldDue), html.Input(html.Props{ID: "chatwork-field-due", Name: "due", Type: "date", Value: v.DueDay, Disabled: v.Busy})),
			html.P(html.Props{Class: "chatwork-notice", Text: t(cwNotice)}),
		}
		if v.Error != "" {
			form = append(form, html.P(html.Props{Class: "chatwork-error", ID: "chatwork-error", Role: "alert", Text: MessageWorkErrorText(v.Locale, v.Error)}))
		}
		form = append(form, html.Div(html.Props{Class: "chatwork-actions"},
			html.Button(html.Props{Type: "button", Class: "button secondary", Data: map[string]string{"chatwork-close": "true"}, Disabled: v.Busy, Text: t(cwCancel)}),
			html.Button(html.Props{Type: "submit", Class: "button primary", Disabled: v.Busy, Aria: map[string]string{"busy": boolString(v.Busy)}, Text: submit})))
		body = append(body, html.Form(html.Props{Class: "chatwork-form", Data: map[string]string{"chatwork-form": v.PostID}, Raw: map[string]any{"novalidate": ""}}, form...))
	}
	if len(v.Projects) == 0 {
		body = append(body, html.Div(html.Props{Class: "chatwork-actions"},
			html.Button(html.Props{Type: "button", Class: "button secondary", Data: map[string]string{"chatwork-close": "true"}, Text: t(cwCancel)})))
	}
	return html.Div(html.Props{Class: "chat-dialog-backdrop chatwork-overlay", Dir: dir, Data: map[string]string{"chatwork-overlay": "true"}},
		html.Div(html.Props{Class: "chatwork-dialog", Role: "dialog", Dir: dir, Aria: map[string]string{"modal": "true", "labelledby": "chatwork-title"}}, body...))
}

// ChatWorkStyles is joined into the workspace stylesheet through
// ChatMsgListStyles.
const ChatWorkStyles = `
.chatwork-chips{display:flex;flex-wrap:wrap;gap:6px;margin-block-start:6px}
.chatwork-chip{display:inline-flex;flex-wrap:wrap;align-items:center;gap:4px 8px;max-inline-size:100%;min-block-size:32px;padding:4px 10px;border:1px solid var(--hcm-color-border);border-radius:16px;background:var(--hcm-color-surface-muted,transparent);color:var(--hcm-color-text);font-size:.8125rem;line-height:1.35;text-decoration:none;box-sizing:border-box}
a.chatwork-chip:hover{border-color:var(--hcm-color-accent,currentColor)}
a.chatwork-chip:focus-visible{outline:2px solid var(--hcm-color-focus,currentColor);outline-offset:2px}
.chatwork-chip .chat-icon{inline-size:16px;block-size:16px;flex:none}
.chatwork-chip-kind{font-weight:600}
.chatwork-chip-title{min-inline-size:0;overflow-wrap:anywhere}
.chatwork-chip-meta{color:var(--hcm-color-text-muted,inherit)}
.chatwork-overlay{z-index:60;box-sizing:border-box}
.chatwork-dialog{inline-size:min(100%,480px);max-block-size:100%;overflow:auto;padding:20px;border-radius:12px;background:var(--hcm-color-surface,Canvas);color:var(--hcm-color-text,CanvasText);box-shadow:var(--hcm-shadow-raised);box-sizing:border-box}
.chatwork-heading{margin:0 0 12px;font-size:1.125rem;line-height:1.3}
.chatwork-field{display:flex;flex-direction:column;gap:4px;margin-block-end:12px}
.chatwork-field label{font-size:.875rem;font-weight:600}
.chatwork-field input,.chatwork-field select{min-block-size:44px;padding:8px 10px;border:1px solid var(--hcm-color-border);border-radius:8px;background:transparent;color:inherit;font:inherit;box-sizing:border-box;inline-size:100%}
.chatwork-notice,.chatwork-status{margin:0 0 12px;font-size:.8125rem;line-height:1.45;color:var(--hcm-color-text-muted,inherit)}
.chatwork-error{margin:0 0 12px;font-size:.875rem;line-height:1.45;color:var(--hcm-color-danger,#b00020)}
.chatwork-existing{margin-block-end:12px;font-size:.875rem}
.chatwork-existing-label{margin:0 0 4px;font-weight:600}
.chatwork-existing ul{margin:0;padding-inline-start:20px}
.chatwork-actions{display:flex;flex-wrap:wrap;gap:8px;justify-content:flex-end}
.chatwork-actions .button{min-block-size:44px;min-inline-size:44px}
`
