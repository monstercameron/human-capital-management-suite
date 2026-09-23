package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// WorkflowNotification is a safe, recipient-scoped server projection. It
// deliberately contains neither compensation nor executable decision input.
type WorkflowNotification struct {
	ID, JourneyID, WorkerName, Purpose, Status string
}

// WorkflowNotificationListProps composes the inbox in a popover or page.
type WorkflowNotificationListProps struct {
	I18nProps
	Items       []WorkflowNotification
	Unavailable bool
	Link        func(WorkflowNotification) ActionLinkProps
}

func WorkflowNotificationList(props WorkflowNotificationListProps) ui.Node {
	if props.Unavailable {
		return html.P(html.Props{Class: "muted", Role: "status"}, ui.Text(props.Text("notifications.unavailable")))
	}
	if len(props.Items) == 0 {
		return html.P(html.Props{Class: "muted"}, ui.Text(props.Text("notifications.empty")))
	}
	rows := make([]ui.Node, 0, len(props.Items))
	for _, item := range props.Items {
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
		link := props.Link(item)
		rows = append(rows, html.Li(html.Props{Key: item.ID}, softwareLink(link.Navigate, html.Props{Class: "workflow-notification"}, link.Href,
			html.Strong(html.Props{}, ui.Text(props.Text(purpose))),
			html.Span(html.Props{}, ui.Text(item.WorkerName)),
			html.Small(html.Props{Class: "muted"}, ui.Text(props.Text(status))),
		)))
	}
	return html.Ul(html.Props{Class: "workflow-notifications", Aria: map[string]string{"label": props.Text("notifications.heading")}}, rows...)
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
.workflow-notification{display:grid;gap:4px;padding:var(--hcm-space-2);border-radius:var(--hcm-radius-control);color:var(--ink);text-decoration:none;overflow-wrap:anywhere;min-height:44px;box-sizing:border-box}
.workflow-notification:hover{background:var(--soft)}
.workflow-notification:focus-visible{background:var(--soft);outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:calc(var(--hcm-focus-ring-width) * -1)}
`
}
