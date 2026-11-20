package chatui

import (
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/localize"
)

// Issue 3791: "Work from this conversation". Conversation details lists what
// the conversation has spawned: actions an installed agent proposed from it and
// the tasks its messages became. Every row is read from the work's own owner
// for this reader, so what is listed is only what this person may open. Each
// row says where its state came from and when it was read, and a source that
// could not answer is said so plainly: a short list is never passed off as a
// complete one, and an unknown state is never shown as Done.

// LinkedWorkItem is one thing the conversation spawned, as the server told it
// to this reader.
type LinkedWorkItem struct {
	// Kind is "intent" (a proposed action) or "task".
	Kind, ID, Title string
	// State is one of the server's state words: proposed, awaiting_approval,
	// approved, scheduled, executing, executed, done, failed, closed, blocked,
	// open or unknown.
	State string
	// Source is the owner that answered: "approvals" or "tasks".
	Source     string
	ObservedAt time.Time
	Revision   uint64
	// Href is a workspace path for a proposed action; a task's link is made from
	// ProjectID and TaskID.
	Href, Detail, OwnerID, DueDate, ProjectID, TaskID string
}

// LinkedWorkState is the panel as the page offers it. Transient client state.
type LinkedWorkState struct {
	// Enabled is true while the server offers the panel for this conversation.
	Enabled bool
	// State is "loading", "ready" or "error".
	State string
	Items []LinkedWorkItem
	// Incomplete names the sources ("approvals", "tasks") that could not answer.
	Incomplete []string
	// AsOf is when the server read the owners.
	AsOf time.Time
	// Error is a short code of a failed read.
	Error string
}

const (
	lwTitle        = "lw_title"
	lwHint         = "lw_hint"
	lwListLabel    = "lw_list_label"
	lwLoading      = "lw_loading"
	lwEmpty        = "lw_empty"
	lwRefresh      = "lw_refresh"
	lwRetry        = "lw_retry"
	lwErrUnavail   = "lw_err_unavailable"
	lwIncomplete   = "lw_incomplete"
	lwSrcApprovals = "lw_src_approvals"
	lwSrcTasks     = "lw_src_tasks"
	lwFromApprovs  = "lw_from_approvals"
	lwFromTasks    = "lw_from_tasks"
	lwOpen         = "lw_open"
	lwOwner        = "lw_owner"
	lwDue          = "lw_due"
	lwAsOf         = "lw_as_of"
	lwReadAt       = "lw_read_at"
	lwYou          = "lw_you"
	lwKindIntent   = "lw_kind_intent"
	lwKindTask     = "lw_kind_task"
	lwStatePrefix  = "lw_state_"
)

var linkedWorkCopy = map[string]map[string]string{
	"en-US": {
		lwTitle: "Work from this conversation", lwHint: "Actions proposed here and tasks made from messages, with where each one stands now.",
		lwListLabel: "Work from this conversation", lwLoading: "Loading work from this conversation",
		lwEmpty:   "Nothing has been proposed or created from this conversation yet.",
		lwRefresh: "Refresh", lwRetry: "Try again",
		lwErrUnavail:   "The work from this conversation could not be loaded right now. Nothing here means it is unknown, not that there is none.",
		lwIncomplete:   "Some work could not be checked right now, so this list may be missing items ({sources}).",
		lwSrcApprovals: "proposed actions", lwSrcTasks: "tasks",
		lwFromApprovs: "From approvals", lwFromTasks: "From tasks",
		lwOpen: "Open", lwOwner: "Owner", lwDue: "Due", lwAsOf: "as of", lwReadAt: "Read at", lwYou: "You",
		lwKindIntent: "Proposed action", lwKindTask: "Task",
		lwStatePrefix + "proposed": "Proposed", lwStatePrefix + "awaiting_approval": "Awaiting approval", lwStatePrefix + "approved": "Approved, not applied yet",
		lwStatePrefix + "scheduled": "Scheduled", lwStatePrefix + "executing": "In progress", lwStatePrefix + "executed": "Applied, checking results",
		lwStatePrefix + "done": "Done", lwStatePrefix + "failed": "Failed", lwStatePrefix + "closed": "Closed", lwStatePrefix + "blocked": "Blocked",
		lwStatePrefix + "open": "Open", lwStatePrefix + "unknown": "Status unknown",
	},
	"de-DE": {
		lwTitle: "Arbeit aus dieser Unterhaltung", lwHint: "Hier vorgeschlagene Aktionen und aus Nachrichten erstellte Aufgaben, jeweils mit aktuellem Stand.",
		lwListLabel: "Arbeit aus dieser Unterhaltung", lwLoading: "Arbeit aus dieser Unterhaltung wird geladen",
		lwEmpty:   "Aus dieser Unterhaltung wurde noch nichts vorgeschlagen oder erstellt.",
		lwRefresh: "Aktualisieren", lwRetry: "Erneut versuchen",
		lwErrUnavail:   "Die Arbeit aus dieser Unterhaltung konnte gerade nicht geladen werden. Eine leere Anzeige bedeutet hier unbekannt, nicht dass es nichts gibt.",
		lwIncomplete:   "Manche Arbeit konnte gerade nicht geprüft werden, daher fehlen in dieser Liste möglicherweise Einträge ({sources}).",
		lwSrcApprovals: "vorgeschlagene Aktionen", lwSrcTasks: "Aufgaben",
		lwFromApprovs: "Aus Genehmigungen", lwFromTasks: "Aus Aufgaben",
		lwOpen: "Öffnen", lwOwner: "Verantwortlich", lwDue: "Fällig", lwAsOf: "Stand", lwReadAt: "Gelesen um", lwYou: "Sie",
		lwKindIntent: "Vorgeschlagene Aktion", lwKindTask: "Aufgabe",
		lwStatePrefix + "proposed": "Vorgeschlagen", lwStatePrefix + "awaiting_approval": "Wartet auf Genehmigung", lwStatePrefix + "approved": "Genehmigt, noch nicht umgesetzt",
		lwStatePrefix + "scheduled": "Geplant", lwStatePrefix + "executing": "In Bearbeitung", lwStatePrefix + "executed": "Umgesetzt, Ergebnis wird geprüft",
		lwStatePrefix + "done": "Erledigt", lwStatePrefix + "failed": "Fehlgeschlagen", lwStatePrefix + "closed": "Abgeschlossen", lwStatePrefix + "blocked": "Blockiert",
		lwStatePrefix + "open": "Offen", lwStatePrefix + "unknown": "Stand unbekannt",
	},
	"ar": {
		lwTitle: "العمل الناتج عن هذه المحادثة", lwHint: "الإجراءات المقترحة هنا والمهام المنشأة من الرسائل، مع وضع كل منها الآن.",
		lwListLabel: "العمل الناتج عن هذه المحادثة", lwLoading: "جارٍ تحميل العمل الناتج عن هذه المحادثة",
		lwEmpty:   "لم يُقترح أو يُنشأ أي شيء من هذه المحادثة بعد.",
		lwRefresh: "تحديث", lwRetry: "إعادة المحاولة",
		lwErrUnavail:   "تعذّر تحميل العمل الناتج عن هذه المحادثة الآن. خلوّ القائمة هنا يعني أن الحالة غير معروفة، لا أنه لا يوجد عمل.",
		lwIncomplete:   "تعذّر فحص بعض الأعمال الآن، لذا قد تنقص هذه القائمة عناصر ({sources}).",
		lwSrcApprovals: "الإجراءات المقترحة", lwSrcTasks: "المهام",
		lwFromApprovs: "من الموافقات", lwFromTasks: "من المهام",
		lwOpen: "فتح", lwOwner: "المسؤول", lwDue: "الاستحقاق", lwAsOf: "اعتبارًا من", lwReadAt: "قُرئت في", lwYou: "أنت",
		lwKindIntent: "إجراء مقترح", lwKindTask: "مهمة",
		lwStatePrefix + "proposed": "مقترح", lwStatePrefix + "awaiting_approval": "بانتظار الموافقة", lwStatePrefix + "approved": "تمت الموافقة ولم يُطبَّق بعد",
		lwStatePrefix + "scheduled": "مجدول", lwStatePrefix + "executing": "قيد التنفيذ", lwStatePrefix + "executed": "طُبّق، جارٍ فحص النتائج",
		lwStatePrefix + "done": "مكتمل", lwStatePrefix + "failed": "فشل", lwStatePrefix + "closed": "مغلق", lwStatePrefix + "blocked": "معطّل",
		lwStatePrefix + "open": "مفتوح", lwStatePrefix + "unknown": "الحالة غير معروفة",
	},
}

// LinkedWorkText is the copy of the work panel in the viewer's language.
func LinkedWorkText(locale, key string) string {
	return chatbug039Text(key, linkedWorkCopy[chatEmojiLocale(locale)][key], linkedWorkCopy["en-US"][key])
}

// LinkedWorkStateText names a state in plain words. A state this page does not
// know reads as unknown; it is never shown as the raw word the server sent.
func LinkedWorkStateText(locale, state string) string {
	if _, known := linkedWorkCopy["en-US"][lwStatePrefix+state]; !known {
		state = "unknown"
	}
	return LinkedWorkText(locale, lwStatePrefix+state)
}

func linkedWorkAvailable(m Model) bool {
	if !m.LinkedWork.Enabled || m.SelectedID == "" || m.selected().Agent {
		return false
	}
	// A conversation hosted by another company is served by that company.
	if host := m.selected().HostTenantID; host != "" && host != m.CurrentTenantID {
		return false
	}
	return true
}

func linkedWorkOwnerName(m Model, ownerID string) string {
	switch {
	case ownerID == "":
		return ""
	case ownerID == m.CurrentUser:
		return LinkedWorkText(m.Locale, lwYou)
	}
	for _, person := range m.SearchDirectory {
		if person.ID == ownerID && strings.TrimSpace(person.Name) != "" {
			return person.Name
		}
	}
	return ""
}

// linkedWorkHref is the link of a row, or "" when it has none that is safe: only
// a path inside this workspace is ever linked.
func linkedWorkHref(item LinkedWorkItem) string {
	if item.Kind == "task" {
		return ProjectTaskReferenceURL(item.ProjectID, item.TaskID)
	}
	if strings.HasPrefix(item.Href, "/workspace/") && !strings.HasPrefix(item.Href, "//") && !strings.ContainsAny(item.Href, "\\\r\n\t ") {
		return item.Href
	}
	return ""
}

func linkedWorkSourceText(locale, source string) string {
	switch source {
	case "approvals":
		return LinkedWorkText(locale, lwFromApprovs)
	case "tasks":
		return LinkedWorkText(locale, lwFromTasks)
	}
	return ""
}

func linkedWorkClock(m Model, at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return localize.FormatClock(m.Locale, at.Local())
}

func linkedWorkItemNode(m Model, item LinkedWorkItem, readAt time.Time) ui.Node {
	t := func(key string) string { return LinkedWorkText(m.Locale, key) }
	state := LinkedWorkStateText(m.Locale, item.State)
	kind := t(lwKindIntent)
	if item.Kind == "task" {
		kind = t(lwKindTask)
	}
	stateClass := item.State
	if _, known := linkedWorkCopy["en-US"][lwStatePrefix+stateClass]; !known {
		stateClass = "unknown"
	}
	meta := []string{}
	if source := linkedWorkSourceText(m.Locale, item.Source); source != "" {
		when := item.ObservedAt
		if when.IsZero() {
			when = readAt
		}
		if clock := linkedWorkClock(m, when); clock != "" {
			source += " · " + t(lwAsOf) + " " + clock
		}
		meta = append(meta, source)
	}
	if item.Detail != "" {
		meta = append(meta, item.Detail)
	}
	if owner := linkedWorkOwnerName(m, item.OwnerID); owner != "" {
		meta = append(meta, t(lwOwner)+": "+owner)
	}
	if item.DueDate != "" {
		meta = append(meta, t(lwDue)+": "+item.DueDate)
	}
	children := []ui.Node{
		html.Div(html.Props{Class: "linked-work-head"},
			html.Span(html.Props{Class: "linked-work-chip linked-work-chip-" + stateClass, Data: map[string]string{"state": item.State}, Text: state}),
			html.Span(html.Props{Class: "linked-work-kind", Text: kind})),
		html.P(html.Props{Class: "linked-work-title", Dir: "auto", Text: item.Title}),
		html.P(html.Props{Class: "linked-work-meta", Text: strings.Join(meta, " · ")}),
	}
	if href := linkedWorkHref(item); href != "" {
		children = append(children, html.A(html.Props{Class: "button secondary linked-work-open", Href: href,
			Aria: map[string]string{"label": t(lwOpen) + ": " + item.Title + ". " + state}, Text: t(lwOpen)}))
	}
	return html.Li(html.Props{Class: "linked-work-item", Data: map[string]string{"linked-work": item.ID, "linked-work-kind": item.Kind}}, children...)
}

// linkedWorkDetailsSection is the Work row of Conversation details. The
// browser client reads the work when the conversation opens, reads it again
// when the person presses Refresh, and listens for data-linked-work-refresh.
func linkedWorkDetailsSection(m Model) ui.Node {
	if !linkedWorkAvailable(m) {
		return nil
	}
	t := func(key string) string { return LinkedWorkText(m.Locale, key) }
	w := m.LinkedWork
	body := []ui.Node{html.H3(html.Props{Text: t(lwTitle)}), html.P(html.Props{Class: "field-hint", Text: t(lwHint)})}
	busy := w.State == "loading"
	refresh := func(label string) ui.Node {
		return html.Button(html.Props{Type: "button", Class: "button secondary linked-work-refresh", Disabled: busy, Data: map[string]string{"linked-work-refresh": "true"},
			Text: label})
	}
	switch {
	case busy:
		body = append(body, html.P(html.Props{Class: "linked-work-status", Role: "status", Text: t(lwLoading)}))
	case w.State == "error":
		body = append(body, html.P(html.Props{Class: "linked-work-error", Role: "alert", Text: t(lwErrUnavail)}), refresh(t(lwRetry)))
	default:
		status := ""
		if clock := linkedWorkClock(m, w.AsOf); clock != "" {
			status = t(lwReadAt) + " " + clock
		}
		if len(w.Incomplete) > 0 {
			names := make([]string, 0, len(w.Incomplete))
			for _, source := range w.Incomplete {
				switch source {
				case "approvals":
					names = append(names, t(lwSrcApprovals))
				case "tasks":
					names = append(names, t(lwSrcTasks))
				}
			}
			if len(names) == 0 {
				names = append(names, t(lwSrcTasks))
			}
			notice := strings.ReplaceAll(t(lwIncomplete), "{sources}", strings.Join(names, ", "))
			body = append(body, html.P(html.Props{Class: "linked-work-incomplete", Role: "status", Text: notice}))
		}
		body = append(body, html.P(html.Props{Class: "linked-work-status", Role: "status", Aria: map[string]string{"live": "polite"}, Text: status}))
		if len(w.Items) == 0 && len(w.Incomplete) == 0 {
			body = append(body, html.P(html.Props{Class: "linked-work-empty", Text: t(lwEmpty)}))
		}
		if len(w.Items) > 0 {
			items := make([]ui.Node, 0, len(w.Items))
			for _, item := range w.Items {
				items = append(items, linkedWorkItemNode(m, item, w.AsOf))
			}
			body = append(body, html.Ul(html.Props{Class: "linked-work-list", Aria: map[string]string{"label": t(lwListLabel)}}, items...))
		}
		body = append(body, refresh(t(lwRefresh)))
	}
	return html.Section(html.Props{ID: "chat-details-linked-work", Class: "details-section details-linked-work", Aria: map[string]string{"label": t(lwTitle), "busy": boolString(busy)}}, body...)
}

// ChatLinkedWorkStyles is joined into the workspace stylesheet through
// ChatMsgListStyles.
const ChatLinkedWorkStyles = `
.linked-work-list{list-style:none;margin:0 0 12px;padding:0;display:flex;flex-direction:column;gap:8px}
.linked-work-item{padding:10px 12px;border:1px solid var(--hcm-color-border);border-radius:10px;min-inline-size:0}
.linked-work-head{display:flex;flex-wrap:wrap;align-items:center;gap:6px 8px;margin-block-end:4px}
.linked-work-chip{display:inline-block;padding:2px 8px;border:1px solid var(--hcm-color-border);border-radius:10px;font-size:.75rem;font-weight:600;line-height:1.4}
.linked-work-chip-awaiting_approval,.linked-work-chip-failed,.linked-work-chip-blocked{border-color:var(--hcm-color-warning,currentColor)}
.linked-work-chip-unknown{border-style:dashed}
.linked-work-kind,.linked-work-meta,.linked-work-status,.linked-work-empty{font-size:.8125rem;line-height:1.45;color:var(--hcm-color-text-muted,inherit)}
.linked-work-title{margin:0 0 4px;font-size:.9375rem;line-height:1.4;overflow-wrap:anywhere}
.linked-work-meta,.linked-work-status,.linked-work-empty{margin:0 0 6px;overflow-wrap:anywhere}
.linked-work-meta:empty,.linked-work-status:empty{display:none}
.linked-work-incomplete{margin:0 0 8px;padding:8px 10px;border:1px dashed var(--hcm-color-border);border-radius:8px;font-size:.8125rem;line-height:1.45}
.linked-work-error{margin:0 0 8px;font-size:.875rem;line-height:1.45;color:var(--hcm-color-danger,#b00020)}
.linked-work-open,.linked-work-refresh{display:inline-flex;align-items:center;justify-content:center;min-block-size:44px;min-inline-size:44px;text-decoration:none;box-sizing:border-box}
.linked-work-open:focus-visible,.linked-work-refresh:focus-visible{outline:2px solid var(--hcm-color-focus,currentColor);outline-offset:2px}
`
