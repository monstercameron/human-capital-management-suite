package productui

import (
	"strings"

	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// peopleDirectActionLabel is the verb-first visible label of a row's one
// direct action (UXLIVE-033): "Open promotion" for an active request and
// "Start promotion" for the one promotion the viewer may start, never the
// bare workflow noun.
func peopleDirectActionLabel(props PeopleRowProps, action PeopleQuickActionProps) string {
	switch {
	case action.Continuation:
		return props.Text("people.open_promotion")
	case action.WorkflowID == "promotion":
		return props.Text("people.start_promotion")
	case strings.TrimSpace(action.Label) != "":
		return props.Text("workflow.start_named", map[string]string{"name": action.Label})
	default:
		return props.Text("workflow.start")
	}
}

// peopleDirectAction renders one resolved row action as a direct link: the
// continuation of an active request, or the only workflow the viewer can
// start for this person. Its accessible name names the person, so a column
// of identical visible labels still reads as distinct actions.
func peopleDirectAction(props PeopleRowProps, action PeopleQuickActionProps) ui.Node {
	label := peopleDirectActionLabel(props, action)
	accessibleLabel := strings.TrimSpace(action.AccessibleLabel)
	if accessibleLabel == "" {
		accessibleLabel = label
	}
	class := "button secondary people-row-action people-row-direct"
	if action.Continuation {
		class += " people-row-continue"
	}
	return softwareLink(props.Navigate, html.Props{Class: class,
		Aria: map[string]string{"label": accessibleLabel}, Raw: map[string]any{"title": accessibleLabel},
	}, action.Href, ui.Text(label))
}

// peopleUnavailableAction is a row with nothing to start: quiet muted text
// and a single info button whose accessible name carries the server-provided
// reason, opening the same reason as a disclosure for pointer and touch.
func peopleUnavailableAction(props PeopleRowProps, identityLabel string) ui.Node {
	reason := props.WorkflowsUnavailableReason
	ariaKey := "people.workflow_unavailable_reason_aria"
	if reason == "" {
		reason = props.Text("people.no_workflows")
		ariaKey = "people.workflow_unavailable_generic_aria"
	}
	descriptionID := "people-unavailable-" + props.ID
	return html.Span(html.Props{Class: "people-availability"},
		html.Span(html.Props{Class: "people-availability-note"}, ui.Text(props.Text("people.workflows_quiet"))),
		ui.CreateElement(TransientPopover, TransientPopoverProps{
			Kind: "people-workflows", Group: "people-workflows", Class: "people-workflow-menu people-unavailable-menu",
			TriggerClass:  "people-reason-trigger",
			Title:         reason,
			DescriptionID: descriptionID,
			Label:         props.Text(ariaKey, map[string]string{"name": identityLabel, "reason": reason}),
			Trigger:       []ui.Node{productIcon("help", "people-reason-icon")},
			PanelClass:    "people-workflow-options",
			Children:      []ui.Node{html.P(html.Props{ID: descriptionID, Class: "people-workflow-unavailable-reason"}, ui.Text(reason))},
		}),
	)
}

// peopleRowStateStylesheet gives the unavailable state the quiet treatment
// UXLIVE-033 asks for and keeps every row's action cell the height of one
// control, so rows are uniform whatever their state.
func peopleRowStateStylesheet() string {
	return buildTypedSheet(func() {
		declareGlobal(".people-availability",
			gwccss.Display.InlineFlex,
			gwccss.Items.Center,
			gwccss.Gap(gwccss.Px(4)),
			gwccss.MinHeight(gwccss.Px(40)),
		)
		declareGlobal(".people-availability-note",
			gwccss.FontSize(gwccss.Rem(0.8125)),
			gwccss.TextColor(gwccss.Var("muted")),
			gwccss.Raw("white-space", "nowrap"),
		)
		declareGlobal(".people-unavailable-menu>summary.people-reason-trigger",
			gwccss.Display.InlineFlex,
			gwccss.Items.Center,
			gwccss.Raw("justify-content", "center"),
			gwccss.W(gwccss.Px(32)), gwccss.H(gwccss.Px(32)),
			gwccss.MinWidth(gwccss.Px(32)), gwccss.MinHeight(gwccss.Px(32)),
			gwccss.Padding(gwccss.Zero),
			gwccss.Raw("border", "0"),
			gwccss.Raw("border-radius", "50%"),
			gwccss.Raw("background", "transparent"),
			gwccss.Raw("cursor", "pointer"),
			gwccss.TextColor(gwccss.Var("muted")),
		)
		declareGlobal(".people-unavailable-menu>summary.people-reason-trigger:hover,.people-unavailable-menu>summary.people-reason-trigger:focus-visible",
			gwccss.Bg(gwccss.Var("soft")),
			gwccss.TextColor(gwccss.Var("ink")),
		)
		declareGlobal(".people-reason-icon",
			gwccss.W(gwccss.Px(16)), gwccss.H(gwccss.Px(16)),
		)
		declareGlobal(".people-row-direct",
			gwccss.Display.InlineFlex,
			gwccss.Items.Center,
			gwccss.Raw("text-decoration", "none"),
		)
		declareGlobal(".people-row-actions>*",
			gwccss.Raw("flex", "none"),
		)
	})
}
