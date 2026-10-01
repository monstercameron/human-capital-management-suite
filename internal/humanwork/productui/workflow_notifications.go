package productui

import (
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// WorkflowNotification is a safe, recipient-scoped server projection. It
// deliberately contains neither compensation nor executable decision input.
type WorkflowNotification struct {
	ID, JourneyID, WorkerName, Purpose, Status string
	CreatedAt                                  time.Time
	Read                                       bool
}

// WorkflowNotificationListProps composes the inbox in a popover or page.
type WorkflowNotificationListProps struct {
	I18nProps
	Items         []WorkflowNotification
	Unavailable   bool
	ReadOverrides map[string]bool
	OnRead        func(string, func(error))
	Link          func(WorkflowNotification) ActionLinkProps
}

func WorkflowNotificationList(props WorkflowNotificationListProps) ui.Node {
	// Hooks are positional: the read-state hook runs before either early
	// return, and each row owns its click hook, so the hook count never
	// depends on availability or on how many notifications arrived.
	readIDs := ui.UseState(map[string]bool{})
	readError := ui.UseState(false)
	if props.Unavailable {
		return html.P(html.Props{Class: "muted", Role: "status"}, ui.Text(props.Text("notifications.unavailable")))
	}
	if len(props.Items) == 0 {
		return html.P(html.Props{Class: "muted"}, ui.Text(props.Text("notifications.empty")))
	}
	markRead := func(id string) {
		next := make(map[string]bool, len(readIDs.Get())+1)
		for known, marked := range readIDs.Get() {
			next[known] = marked
		}
		next[id] = true
		readIDs.Set(next)
		if props.OnRead == nil {
			return
		}
		props.OnRead(id, func(err error) {
			if err == nil {
				return
			}
			reverted := make(map[string]bool, len(readIDs.Get()))
			for known, marked := range readIDs.Get() {
				reverted[known] = marked
			}
			delete(reverted, id)
			readIDs.Set(reverted)
			readError.Set(true)
		})
	}
	rows := make([]ui.Node, 0, len(props.Items))
	for _, item := range props.Items {
		notification := item
		purpose := "notifications.task"
		if item.Purpose == "APPROVAL" {
			purpose = "notifications.approval"
		}
		status := "notifications.unknown"
		switch item.Status {
		case "CREATED", "ASSIGNED", "AVAILABLE", "CLAIMED", "IN_PROGRESS", "RETURNED", "ESCALATED":
			status = "notifications.pending"
		case "COMPLETED":
			status = "notifications.completed"
			if item.Purpose == "APPROVAL" {
				status = "notifications.decided"
			}
		case "CANCELLED":
			status = "notifications.cancelled"
		case "EXPIRED":
			status = "notifications.expired"
		}
		if title, state, ok := workflowUpdateNotificationKeys(item); ok {
			purpose, status = title, state
		}
		titleText := props.Text(purpose)
		statusText := props.Text(status)
		actor := strings.TrimSpace(item.WorkerName)
		if actor == "" {
			actor = props.Text("common.not_reported")
		}
		when := props.Text("notifications.time_unavailable")
		whenProps := html.Props{Class: "muted workflow-notification-time"}
		if !item.CreatedAt.IsZero() {
			when = props.Locale.FormatTimestamp(item.CreatedAt)
			whenProps.Raw = map[string]any{"datetime": item.CreatedAt.UTC().Format(time.RFC3339)}
		}
		whenNode := html.Tag("time", whenProps, ui.Text(when))
		read := item.Read || props.ReadOverrides[item.ID] || readIDs.Get()[item.ID]
		stateText := props.Text("notifications.unread")
		if read {
			stateText = props.Text("notifications.read")
		}
		accessibleName := props.Text("notifications.item_aria", map[string]string{
			"title": titleText, "actor": actor, "time": when, "state": statusText + ", " + stateText,
		})
		link := props.Link(item)
		rowClass := "workflow-notification-row"
		if !read {
			rowClass += " is-unread"
		}
		linkProps := html.Props{Class: "workflow-notification", Aria: map[string]string{"label": accessibleName}}
		row := ui.CreateElement(workflowNotificationRow, workflowNotificationRowProps{
			ID: notification.ID, Class: rowClass, OnRead: markRead,
			Content: softwareLink(link.Navigate, linkProps, link.Href,
				html.Strong(html.Props{}, ui.Text(titleText)),
				html.Span(html.Props{}, ui.Text(actor)),
				whenNode,
				html.Small(html.Props{Class: "muted workflow-notification-status"}, ui.Text(statusText)),
				html.Small(html.Props{Class: "muted workflow-notification-read-state"}, ui.Text(stateText)),
			),
		})
		row.Props["key"] = item.ID
		rows = append(rows, row)
	}
	list := html.Ul(html.Props{Class: "workflow-notifications", Aria: map[string]string{"label": props.Text("notifications.heading")}}, rows...)
	if readError.Get() {
		return html.Div(html.Props{}, html.P(html.Props{Role: "alert", Class: "notification-read-error"}, ui.Text(props.Text("notifications.read_failed"))), list)
	}
	return list
}

type workflowNotificationRowProps struct {
	ID, Class string
	OnRead    func(string)
	Content   ui.Node
}

// workflowNotificationRow is its own component so its click hook belongs to
// the row, not to a list whose hook count would follow the item count.
func workflowNotificationRow(props workflowNotificationRowProps) ui.Node {
	onClick := ui.UseEvent(func(ui.MouseEvent) {
		if props.OnRead != nil {
			props.OnRead(props.ID)
		}
	})
	return html.Li(html.Props{Key: props.ID, Class: props.Class, OnClick: onClick}, props.Content)
}

func unreadWorkflowNotifications(items []WorkflowNotification) int {
	count := 0
	for _, item := range items {
		if !item.Read {
			count++
		}
	}
	return count
}

func unreadWorkflowNotificationsWithOverrides(items []WorkflowNotification, overrides map[string]bool) int {
	count := 0
	for _, item := range items {
		if !item.Read && !overrides[item.ID] {
			count++
		}
	}
	return count
}

func workflowNotificationTranslations(locale string) map[string]string {
	switch locale {
	case "de-DE":
		return map[string]string{
			"notifications.heading": "Workflow-Mitteilungen", "notifications.empty": "Noch keine Workflow-Mitteilungen.",
			"notifications.approval": "Genehmigung", "notifications.task": "Aufgabe", "notifications.pending": "Entscheidung oder Aktion ausstehend",
			"notifications.completed": "Abgeschlossen", "notifications.decided": "Ihre Entscheidung wurde erfasst", "notifications.cancelled": "Abgebrochen",
			"notifications.expired": "Abgelaufen", "notifications.unknown": "Status nicht verfügbar",
			"notifications.unavailable": "Mitteilungen sind vorübergehend nicht verfügbar. Öffnen Sie Meine Aufgaben, um Ihre Anfragen zu prüfen.",
		}
	case "ar":
		return map[string]string{
			"notifications.heading": "إشعارات سير العمل", "notifications.empty": "لا توجد إشعارات سير عمل حتى الآن.",
			"notifications.approval": "موافقة", "notifications.task": "مهمة", "notifications.pending": "بانتظار قرار أو إجراء",
			"notifications.completed": "مكتمل", "notifications.decided": "تم تسجيل قرارك", "notifications.cancelled": "ملغى",
			"notifications.expired": "منتهي الصلاحية", "notifications.unknown": "الحالة غير متاحة",
			"notifications.unavailable": "الإشعارات غير متاحة مؤقتًا. افتح مهامي لمراجعة طلباتك.",
		}
	default:
		return map[string]string{
			"notifications.heading": "Workflow notifications", "notifications.empty": "No workflow notifications yet.",
			"notifications.approval": "Approval request", "notifications.task": "Task assigned", "notifications.pending": "Awaiting a decision or action",
			"notifications.completed": "Completed", "notifications.decided": "Your decision is recorded", "notifications.cancelled": "Cancelled",
			"notifications.expired": "Expired", "notifications.unknown": "Status unavailable",
			"notifications.unavailable": "Notifications are temporarily unavailable. Open My Work to review your requests.",
		}
	}
}

func workflowNotificationStylesheet() string {
	return `
.workflow-notifications{list-style:none;padding:0;margin:var(--hcm-space-2) 0;max-height:min(55vh,28rem);overflow:auto;overscroll-behavior:contain}
.workflow-notifications>li+li{border-block-start:1px solid var(--line)}
.workflow-notification-row{list-style:none}
.workflow-notification{display:grid;gap:4px;padding:var(--hcm-space-2);border-radius:var(--hcm-radius-control);color:var(--ink);text-decoration:none;overflow-wrap:anywhere;min-height:44px;box-sizing:border-box}
.workflow-notification-row.is-unread .workflow-notification{font-weight:600}
.workflow-notification:hover{background:var(--soft)}
.workflow-notification:focus-visible{background:var(--soft);outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:calc(var(--hcm-focus-ring-width) * -1)}
.workflow-notification-time{font-size:var(--hcm-font-size-small)}
.workflow-notification-read-state{font-size:var(--hcm-font-size-small)}
.notification-unread-count{position:absolute;inset-block-start:3px;inset-inline-end:4px;min-inline-size:1.1rem;block-size:1.1rem;padding-inline:.18rem;border:2px solid var(--surface);border-radius:999px;background:var(--danger);color:var(--on-brand);font-size:.68rem;line-height:1.1rem;text-align:center;font-variant-numeric:tabular-nums}
`
}
