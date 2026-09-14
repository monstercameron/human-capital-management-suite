package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// delegationSelectorOptions resolves the delegations the identity may
// assume next: valid delegated grants that are not the current context.
// It reuses the switcher's normalization and validity rules, so the two
// controls cannot disagree on what counts as a well-formed grant.
func delegationSelectorOptions(props ContextSwitcherProps) []AuthorityContextOption {
	normalized := normalizeContextSwitcherProps(props)
	selectable := make([]AuthorityContextOption, 0)
	for _, option := range normalized.Options {
		if !option.Delegated {
			continue
		}
		if sameContextSelection(ContextSelection{TenantID: option.TenantID, ActingContextID: option.ActingContextID}, normalized.Current) {
			continue
		}
		selectable = append(selectable, option)
	}
	return selectable
}

// DelegationSelector renders the assumable-delegation control: a labelled
// disclosure listing each grant with its delegator, expiry, and elevation
// through the shared option renderer. Every button closes over its
// inseparable server-projected pair and travels the same exchange contract
// as the switcher. No selectable grant means no control.
func DelegationSelector(props ContextSwitcherProps) ui.Node {
	interaction := ui.UseState(ContextSwitcherReady)
	summaryRef := ui.UseDOMRef()
	props = normalizeContextSwitcherProps(props)
	selectable := delegationSelectorOptions(props)
	if len(selectable) == 0 {
		return html.Fragment()
	}
	phase := props.State
	if interaction.Get() != ContextSwitcherReady {
		phase = interaction.Get()
	}
	busy := phase == ContextSwitcherLoading || phase == ContextSwitcherSwitching
	locale := props.Locale.normalized()
	label := locale.Text("delegation_selector.label")
	buttons := make([]ui.Node, 0, len(selectable))
	for _, option := range selectable {
		option := option
		selection := ContextSelection{TenantID: option.TenantID, ActingContextID: option.ActingContextID}
		disabled := busy || props.Exchange == nil || props.Commit == nil || props.Rollback == nil || props.Controller == nil
		button := ui.CreateElement(contextSwitcherOption, contextSwitcherOptionProps{
			Locale: locale, Option: option, Selection: selection, Current: false, Disabled: disabled,
			Switcher: props, Interaction: interaction, SummaryRef: summaryRef,
		})
		buttons = append(buttons, html.WithKey(button, "delegation\x00"+option.TenantID+"\x00"+option.ActingContextID))
	}
	children := []ui.Node{
		html.Ul(html.Props{Class: "delegation-selector-options"}, buttons...),
		html.P(html.Props{
			Class: "delegation-selector-status",
			Raw:   map[string]any{"role": "status", "aria-live": "polite", "aria-atomic": "true"},
		}, ui.Text(contextSwitcherStatus(locale, phase, len(selectable)))),
	}
	panelRaw := map[string]any{}
	if busy {
		panelRaw["aria-busy"] = true
	}
	return html.Details(html.Props{ID: "delegation-selector", Class: "delegation-selector", Dir: string(locale.Direction), Data: map[string]string{
		"hcm-delegation-selector": "true",
	}},
		html.Summary(html.PropsOf(
			html.Class("delegation-selector-summary"),
			html.Aria("label", label),
		), html.Span(html.Props{Class: "delegation-selector-trigger"},
			html.Span(html.Props{Class: "delegation-selector-current"}, ui.Text(label)),
			productIcon("expand", "delegation-selector-chevron"),
		)),
		ui.CreateElement(PopoverSurface, PopoverSurfaceProps{Class: "delegation-selector-panel", Raw: panelRaw, Children: children}),
	)
}
