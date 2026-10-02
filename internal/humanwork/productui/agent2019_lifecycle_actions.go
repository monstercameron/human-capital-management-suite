package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AgentAdminApprovalClient is the second administrator's half of the console.
// The server decides whether the signed-in administrator has passed step-up
// and is a different person from the author; the page only asks.
type AgentAdminApprovalClient interface {
	ApproveRevision(revisionID string) error
}

// AgentAdminRollbackClient republishes an earlier revision as the newest.
type AgentAdminRollbackClient interface {
	RollBack(revisionID string) error
}

// agentAdminLifecycleActions draws the one action a revision card offers next:
// ask for a second administrator, approve as that administrator, publish, or
// (for a replaced revision) roll back to it. A published revision offers none.
func agentAdminLifecycleActions(props AgentAdminAccessPageProps, revision AgentConnectionRevision, needsSecondAdmin bool) []ui.Node {
	locale := props.Locale
	status := strings.ToUpper(strings.TrimSpace(revision.Status))
	button := func(class, key string, run func()) ui.Node {
		return html.Button(html.Props{Class: class, Type: "button", Raw: map[string]any{"data-agent-admin-action": key, "data-revision-id": revision.ID}, OnClick: ui.UseEvent(func(ui.MouseEvent) { run() })}, ui.Text(agentAdminExtraText(locale, key)))
	}
	switch {
	case status == "PUBLISHED":
		return []ui.Node{html.Small(html.Props{Class: "muted"}, ui.Text(agentAdminExtraText(locale, "live_now")))}
	case status == "SUPERSEDED":
		if client, ok := props.Client.(AgentAdminRollbackClient); ok {
			return []ui.Node{button("button secondary", "roll_back", func() { _ = client.RollBack(revision.ID) })}
		}
		return nil
	case status == "AWAITING_APPROVAL":
		nodes := []ui.Node{html.Small(html.Props{Class: "muted", Raw: map[string]any{"data-awaiting-approval": "true"}}, ui.Text(agentAdminExtraText(locale, "waiting_second_admin")))}
		if client, ok := props.Client.(AgentAdminApprovalClient); ok {
			nodes = append(nodes, button("button primary", "approve", func() { _ = client.ApproveRevision(revision.ID) }))
		}
		return nodes
	case needsSecondAdmin && !revision.SecondAdminApproved:
		return []ui.Node{button("button secondary", "request_approval", func() { _ = props.Client.RequestSecondAdminApproval(revision.ID) })}
	}
	return []ui.Node{button("button primary", "publish", func() { _ = props.Client.PublishConnectionRevision(revision.ID) })}
}

var agentAdminExtraCopy = map[string][3]string{
	"live_now":             {"This revision is live.", "Diese Revision ist aktiv.", "هذا الإصدار نشط الآن."},
	"roll_back":            {"Roll back to this revision", "Zu dieser Revision zurückkehren", "الرجوع إلى هذا الإصدار"},
	"waiting_second_admin": {"Waiting for a second administrator to approve.", "Wartet auf die Genehmigung durch eine zweite Administration.", "بانتظار موافقة مسؤول ثانٍ."},
	"approve":              {"Approve as second administrator", "Als zweite Administration genehmigen", "الموافقة بصفتي المسؤول الثاني"},
	"request_approval":     {"Request second-admin approval", "Genehmigung durch zweite Administration anfordern", "طلب موافقة المسؤول الثاني"},
	"publish":              {"Publish revision", "Revision veröffentlichen", "نشر الإصدار"},
}

func agentAdminExtraText(locale LocaleContext, key string) string {
	labels, ok := agentAdminExtraCopy[key]
	if !ok {
		return key
	}
	language := strings.ToLower(locale.Resolved)
	switch {
	case strings.HasPrefix(language, "de"):
		return labels[1]
	case strings.HasPrefix(language, "ar"):
		return labels[2]
	}
	return labels[0]
}
