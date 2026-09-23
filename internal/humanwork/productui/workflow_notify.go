package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/localize"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/notifyplan"
)

// workflowNotifySection tells the author who hears about a step. It reads the
// same declaration the execution composition publishes from, so the editor
// cannot promise a notice a run will not send. It is nil for a step that
// notifies nobody: most steps run on their own, and saying so on each of them
// would be noise.
func workflowNotifySection(i18n I18nProps, stepType string) ui.Node {
	notices := notifyplan.ForStep(stepType)
	if len(notices) == 0 {
		return nil
	}
	rows := make([]ui.Node, 0, len(notices))
	// Saying "the approver" sent authors looking for a field that names one.
	// Who that is comes from the request's routing rule, not from the step,
	// so it leads the section as a sentence rather than sitting among the
	// notices as if it were one.
	var routing ui.Node
	for _, notice := range notices {
		if notice.Audience == notifyplan.AudienceAssignee {
			routing = html.P(html.Props{Key: "routing", Class: "muted workflow-inspector-help workflow-inspector-notify-routing"}, ui.Text(i18n.Text("workflow_editor.notify.routing")))
			break
		}
	}
	for _, notice := range notices {
		key := workflowNotifyKey(notice)
		if key == "" {
			continue
		}
		rows = append(rows, html.Li(html.Props{Key: key}, ui.Text(i18n.Text(key))))
	}
	if len(rows) == 0 {
		return nil
	}
	return html.Section(html.Props{Key: "notify", Class: "workflow-inspector-section workflow-inspector-notify", Aria: map[string]string{"labelledby": "workflow-inspector-notify"}},
		html.H4(html.Props{ID: "workflow-inspector-notify"}, productIcon("notifications", "workflow-inspector-notify-icon"), ui.Text(i18n.Text("workflow_editor.notify_title"))),
		routing,
		html.Ul(html.Props{Key: "list", Class: "workflow-inspector-notify-list"}, rows...),
		html.P(html.Props{Class: "muted workflow-inspector-help"}, ui.Text(i18n.Text("workflow_editor.notify_channel"))),
	)
}

func workflowNotifyKey(notice notifyplan.Notice) string {
	switch {
	case notice.Audience == notifyplan.AudienceAssignee && notice.Purpose == notifyplan.PurposeApproval:
		return "workflow_editor.notify.approver"
	case notice.Audience == notifyplan.AudienceAssignee:
		return "workflow_editor.notify.assignee"
	case notice.Audience == notifyplan.AudienceRequester && notice.Moment == notifyplan.MomentRouted:
		return "workflow_editor.notify.requester_routed"
	case notice.Audience == notifyplan.AudienceRequester && notice.Moment == notifyplan.MomentFinished:
		return "workflow_editor.notify.requester_finished"
	default:
		return ""
	}
}

// workflowUpdateNotificationKeys returns the bell's title and state copy for a
// status notice, and false for every other purpose.
func workflowUpdateNotificationKeys(item WorkflowNotification) (title, state string, ok bool) {
	if item.Purpose != notifyplan.PurposeUpdate {
		return "", "", false
	}
	switch item.Status {
	case notifyplan.StatusSentForReview:
		state = "notifications.sent_for_review"
	case notifyplan.StatusFinishedApproved:
		state = "notifications.finished_approved"
	case notifyplan.StatusFinishedDeclined:
		state = "notifications.finished_declined"
	case notifyplan.StatusFinishedFailed:
		state = "notifications.finished_failed"
	case notifyplan.StatusFinished:
		state = "notifications.finished"
	default:
		state = "notifications.unknown"
	}
	return "notifications.update", state, true
}

func workflowNotifyMessages() map[string]localize.Message {
	return map[string]localize.Message{
		"workflow_editor.notify_title":              {Text: "Who is told"},
		"workflow_editor.notify.approver":           {Text: "The approver gets an approval request when the workflow reaches this step. It shows their decision once they make it."},
		"workflow_editor.notify.assignee":           {Text: "The person assigned gets a task when the workflow reaches this step."},
		"workflow_editor.notify.requester_routed":   {Text: "The person who made the request is told it has been sent for review."},
		"workflow_editor.notify.requester_finished": {Text: "The person who made the request is told the workflow finished and how it ended."},
		"workflow_editor.notify_channel":            {Text: "Notices appear under the bell in the product. They are not emailed."},
		"workflow_editor.notify.routing":            {Text: "The person comes from the request's routing rule, usually the worker's current manager, not from this step."},
		"notifications.update":                      {Text: "Request update"},
		"notifications.sent_for_review":             {Text: "Sent to a reviewer"},
		"notifications.finished_approved":           {Text: "Approved and finished"},
		"notifications.finished_declined":           {Text: "Declined"},
		"notifications.finished_failed":             {Text: "Could not be completed"},
		"notifications.finished":                    {Text: "Finished"},
	}
}

func workflowNotifyTranslations(locale string) map[string]localize.Message {
	text := func(value string) localize.Message { return localize.Message{Text: value} }
	switch locale {
	case "de-DE":
		return map[string]localize.Message{
			"workflow_editor.notify_title":              text("Wer informiert wird"),
			"workflow_editor.notify.approver":           text("Die genehmigende Person erhält eine Genehmigungsanfrage, sobald der Workflow diesen Schritt erreicht. Nach der Entscheidung zeigt die Mitteilung das Ergebnis."),
			"workflow_editor.notify.assignee":           text("Die zugewiesene Person erhält eine Aufgabe, sobald der Workflow diesen Schritt erreicht."),
			"workflow_editor.notify.requester_routed":   text("Die Person, die den Antrag gestellt hat, erfährt, dass er zur Prüfung weitergeleitet wurde."),
			"workflow_editor.notify.requester_finished": text("Die Person, die den Antrag gestellt hat, erfährt, dass der Workflow beendet ist und wie er ausging."),
			"workflow_editor.notify_channel":            text("Mitteilungen erscheinen im Produkt unter der Glocke. Sie werden nicht per E-Mail versendet."),
			"workflow_editor.notify.routing":            text("Die Person ergibt sich aus der Weiterleitungsregel des Antrags, meist die aktuelle Führungskraft, nicht aus diesem Schritt."),
			"notifications.update":                      text("Neues zum Antrag"),
			"notifications.sent_for_review":             text("Zur Prüfung weitergeleitet"),
			"notifications.finished_approved":           text("Genehmigt und abgeschlossen"),
			"notifications.finished_declined":           text("Abgelehnt"),
			"notifications.finished_failed":             text("Konnte nicht abgeschlossen werden"),
			"notifications.finished":                    text("Abgeschlossen"),
		}
	case "ar":
		return map[string]localize.Message{
			"workflow_editor.notify_title":              text("من يتم إبلاغه"),
			"workflow_editor.notify.approver":           text("يتلقى صاحب الموافقة طلب موافقة عندما يصل سير العمل إلى هذه الخطوة، ويعرض الإشعار قراره بعد اتخاذه."),
			"workflow_editor.notify.assignee":           text("يتلقى الشخص المكلَّف مهمة عندما يصل سير العمل إلى هذه الخطوة."),
			"workflow_editor.notify.requester_routed":   text("يتم إبلاغ مقدّم الطلب بأن طلبه أُرسل للمراجعة."),
			"workflow_editor.notify.requester_finished": text("يتم إبلاغ مقدّم الطلب بانتهاء سير العمل وبنتيجته."),
			"workflow_editor.notify_channel":            text("تظهر الإشعارات داخل المنتج تحت أيقونة الجرس، ولا تُرسل بالبريد الإلكتروني."),
			"workflow_editor.notify.routing":            text("يُحدَّد الشخص وفق قاعدة توجيه الطلب، وعادةً يكون المدير الحالي للموظف، لا من هذه الخطوة."),
			"notifications.update":                      text("تحديث على الطلب"),
			"notifications.sent_for_review":             text("أُرسل إلى مراجع"),
			"notifications.finished_approved":           text("تمت الموافقة واكتمل"),
			"notifications.finished_declined":           text("مرفوض"),
			"notifications.finished_failed":             text("تعذّر إكماله"),
			"notifications.finished":                    text("اكتمل"),
		}
	default:
		return nil
	}
}
