package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type AdminPageProps struct {
	Hero         AdminHeroProps
	Capabilities []CapabilityCardProps
}

type AdminHeroProps struct {
	Eyebrow     string
	Title       string
	Description string
	Action      ActionLinkProps
}

type CapabilityCardProps struct {
	Title       string
	Description string
	State       string
	Tone        string
	Action      ActionLinkProps
	// Availability carries the semantic action state. A blank state
	// keeps the legacy rendering — a live action link. Unavailable
	// renders the reason (and recovery link when set) with no live
	// link to the action; hidden drops the card from the page.
	Availability ActionState
}

func AdminPage(props AdminPageProps) ui.Node {
	children := []ui.Node{ui.CreateElement(AdminHero, props.Hero)}
	for _, capability := range props.Capabilities {
		if capability.Availability.Availability == ActionHidden {
			continue
		}
		children = append(children, ui.CreateElement(CapabilityCard, capability))
	}
	return html.Div(html.Props{Class: "admin-grid", Raw: map[string]any{"role": "list", "aria-labelledby": "admin-page-title"}}, children...)
}

func AdminHero(props AdminHeroProps) ui.Node {
	var action ui.Node
	if props.Action.Href != "" {
		action = ui.CreateElement(ActionLink, props.Action)
	}
	return html.Section(html.Props{Class: "surface admin-hero"},
		html.Div(html.Props{}, html.Small(html.Props{}, ui.Text(props.Eyebrow)), html.H2(html.Props{ID: "admin-page-title"}, ui.Text(props.Title)), html.P(html.Props{Class: "muted"}, ui.Text(props.Description))),
		action,
	)
}

func CapabilityCard(props CapabilityCardProps) ui.Node {
	tone := props.Tone
	if tone == "" {
		tone = "positive"
	}
	if props.Availability.Availability == ActionUnavailable {
		children := []ui.Node{
			html.Div(html.Props{}, html.H3(html.Props{}, ui.Text(props.Title)), html.P(html.Props{Class: "muted"}, ui.Text(props.Description))),
			html.Strong(html.Props{Class: tone}, ui.Text(props.State)),
			html.P(html.Props{Class: "muted"}, ui.Text(props.Availability.Reason)),
		}
		if props.Availability.Recovery.Href != "" {
			children = append(children, ui.CreateElement(ActionLink, props.Availability.Recovery))
		}
		return html.Section(html.Props{Class: "surface admin-card", Raw: map[string]any{"role": "listitem", "data-action-state": "unavailable"}}, children...)
	}
	// An available card carries no state badge. The badge exists to mark a
	// card that is not simply available; printing it on every card made it
	// read as decoration and left the one card it applied to competing with
	// five copies of itself (UXLIVE-014).
	children := []ui.Node{
		html.Div(html.Props{}, html.H3(html.Props{}, ui.Text(props.Title)), html.P(html.Props{Class: "muted"}, ui.Text(props.Description))),
	}
	if strings.TrimSpace(props.State) != "" {
		children = append(children, html.Strong(html.Props{Class: tone}, ui.Text(props.State)))
	}
	return html.Section(html.Props{Class: "surface admin-card", Raw: map[string]any{"role": "listitem"}},
		append(children, ui.CreateElement(ActionLink, props.Action))...,
	)
}
