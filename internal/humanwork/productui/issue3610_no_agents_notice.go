package productui

import (
	"net/url"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3610: with no reviewed template and no agent there is nothing to edit
// and nothing to create. The notice says that, and does not send the reader to
// "Edit" on a card that does not exist or to a New agent control that is not
// drawn. Nothing on this page claims to install a template.
func init() {
	personaAdminR5SetupCopy["no_template_no_agents"] = [3]string{
		"There are no agents yet, and none can be created in this workspace right now because no reviewed template is available. Nothing on this page adds one.",
		"Es gibt noch keine Agenten, und in diesem Arbeitsbereich kann derzeit keiner erstellt werden, weil keine geprüfte Vorlage verfügbar ist. Auf dieser Seite lässt sich keine hinzufügen.",
		"لا يوجد وكلاء بعد، ولا يمكن إنشاء وكيل في مساحة العمل هذه الآن لأنه لا يتوفر قالب تمت مراجعته. لا شيء في هذه الصفحة يضيف قالباً.",
	}
}

// personaAdminNoAgentsNotice keeps the one real next step the no-template
// notice has: when another person looks after agents, they are named and can
// be messaged.
func personaAdminNoAgentsNotice(locale LocaleContext, options []PersonaAdminTarget, viewer string) ui.Node {
	children := []ui.Node{ui.Text(personaAdminR5SetupText(locale, "no_template_no_agents"))}
	manager := personaAdminRoleContact(options, "platform_administrator")
	if manager.Label == "" || agentUX074ViewerIs(viewer, manager) {
		manager = personaAdminRoleContact(options, "agent_administrator")
	}
	if manager.Label != "" && !agentUX074ViewerIs(viewer, manager) {
		children = append(children,
			ui.Text(personaAdminSpace+strings.ReplaceAll(personaAdminR5SetupText(locale, "named_template_manager"), "{person}", manager.Label)+personaAdminSpace),
			html.A(html.Props{Class: "button secondary compact", Href: Path(PageChat) + "?person=" + url.QueryEscape(manager.ID)}, ui.Text(strings.ReplaceAll(personaAdminR5SetupText(locale, "message_person"), "{person}", manager.Label))),
		)
	}
	return html.Div(html.Props{ID: "persona-admin-new-agent-note", Class: "persona-admin-new-agent-note", Role: "status"}, children...)
}
