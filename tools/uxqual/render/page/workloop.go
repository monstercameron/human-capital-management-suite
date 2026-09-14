package page

import (
	"fmt"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/floorplan"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
)

// ProductionWorkLoopRegistry returns the deterministic widgets used by the
// default work-loop and failure/recovery fixtures. state is a projection
// state, not a workflow transition; no constructor renders a successful
// outcome for a non-ready state.
func ProductionWorkLoopRegistry(state pagedef.WorkLoopState) *Registry {
	reg := NewRegistry()
	register := func(ref string, ctor Widget) {
		if err := reg.Register(ref, ctor); err != nil {
			panic(err)
		}
	}
	register("widget.work-loop.state.v1", func(WidgetContext) ui.Node { return workLoopStatePanel(state) })
	register("widget.work-loop.evidence.v1", workLoopEvidenceWidget)
	register("widget.work-loop.recovery.v1", func(WidgetContext) ui.Node { return workLoopRecoveryWidget(state) })
	register("widget.failure-recovery.state.v1", func(WidgetContext) ui.Node { return failureStatePanel(state) })
	register("widget.failure-recovery.actions.v1", func(WidgetContext) ui.Node { return failureRecoveryActions(state) })
	return reg
}

// WorkLoopRegistry is a shorter compatibility spelling for the production
// fixture registry.
func WorkLoopRegistry(state pagedef.WorkLoopState) *Registry {
	return ProductionWorkLoopRegistry(state)
}

// RenderProductionWorkLoop resolves and renders the governed work-loop page
// against the admitted intent-workspace floorplan.
func RenderProductionWorkLoop(state pagedef.WorkLoopState) (ui.Node, error) {
	res, err := floorplan.PromotionRegistry().Resolve(pagedef.WorkLoopPageDefinition())
	if err != nil {
		return nil, fmt.Errorf("page: resolve work-loop fixture: %w", err)
	}
	return Render(res, ProductionWorkLoopRegistry(state))
}

// RenderFailureRecovery resolves and renders the accessible fallback page.
// The state remains explicit so a failure page never implies that retry has
// already succeeded.
func RenderFailureRecovery(state pagedef.WorkLoopState) (ui.Node, error) {
	res, err := floorplan.PromotionRegistry().Resolve(pagedef.FailureRecoveryPageDefinition())
	if err != nil {
		return nil, fmt.Errorf("page: resolve failure fixture: %w", err)
	}
	return Render(res, ProductionWorkLoopRegistry(state))
}

func workLoopStatePanel(state pagedef.WorkLoopState) ui.Node {
	switch state {
	case pagedef.WorkLoopLoading:
		return statusPanel("loading", "Loading your work…", "status", "polite", false)
	case pagedef.WorkLoopEmpty:
		return statusPanel("empty", "No work is assigned right now.", "status", "polite", false)
	case pagedef.WorkLoopReady:
		return html.Ul(html.Props{
			ID:   "assigned-work-list",
			Role: "list",
			Aria: map[string]string{"label": "Assigned work"},
			Data: map[string]string{"work-state": "ready"},
		}, html.Li(html.Props{Data: map[string]string{"work-item": "fixture-review"}},
			html.Strong(html.Props{}, ui.Text("Promotion review")),
			ui.Text(" — Awaiting approval"),
		))
	case pagedef.WorkLoopStale:
		return statusPanel("stale", "This work list may be out of date.", "alert", "assertive", false)
	case pagedef.WorkLoopDenied:
		// Do not include an object id, count, or permission explanation: a
		// denied projection must not disclose whether protected work exists.
		return statusPanel("denied", "You don’t have access to this work.", "alert", "assertive", false)
	case pagedef.WorkLoopError:
		return html.Div(html.Props{
			ID:   "work-load-error",
			Role: "alert",
			Aria: map[string]string{"live": "assertive", "atomic": "true"},
			Data: map[string]string{"work-state": "error"},
		}, html.P(html.Props{}, ui.Text("We couldn’t load your work.")),
			html.P(html.Props{}, ui.Text("No changes were saved.")),
		)
	case pagedef.WorkLoopRetrying:
		return statusPanel("retrying", "Trying again to load your work…", "status", "polite", true)
	default:
		return statusPanel("error", "We couldn’t load your work.", "alert", "assertive", false)
	}
}

func workLoopEvidenceWidget(WidgetContext) ui.Node {
	return html.Div(html.Props{ID: "work-source-evidence", Data: map[string]string{"source": "authorized-work-projection"}},
		html.P(html.Props{}, ui.Text("Source: authorized work projection.")),
		html.P(html.Props{}, ui.Text("Freshness is reported by the server; this page does not infer completion from a counter or cache.")),
	)
}

func workLoopRecoveryWidget(state pagedef.WorkLoopState) ui.Node {
	switch state {
	case pagedef.WorkLoopEmpty, pagedef.WorkLoopStale:
		return retryButton("Refresh work list")
	case pagedef.WorkLoopError:
		return retryButton("Try again")
	case pagedef.WorkLoopRetrying:
		return html.P(html.Props{Data: map[string]string{"work-action": "retry-in-flight"}}, ui.Text("The read is in progress. Keep this page open."))
	case pagedef.WorkLoopDenied:
		return html.P(html.Props{}, ui.Text("If access should have changed, contact your administrator."))
	default:
		return html.P(html.Props{}, ui.Text("No action is required."))
	}
}

func failureStatePanel(state pagedef.WorkLoopState) ui.Node {
	return html.Div(html.Props{ID: "failure-state", Data: map[string]string{"failure-state": string(state)}},
		workLoopStatePanel(state),
	)
}

func failureRecoveryActions(state pagedef.WorkLoopState) ui.Node {
	switch state {
	case pagedef.WorkLoopLoading, pagedef.WorkLoopRetrying:
		return html.P(html.Props{}, ui.Text("Please wait while the read completes."))
	case pagedef.WorkLoopDenied:
		return html.P(html.Props{}, ui.Text("Access is required before this work can be shown."))
	case pagedef.WorkLoopEmpty:
		return html.P(html.Props{}, ui.Text("There is no work to recover."))
	case pagedef.WorkLoopStale:
		return retryButton("Refresh safely")
	default:
		return retryButton("Try again")
	}
}

func statusPanel(state, message, role, live string, busy bool) ui.Node {
	aria := map[string]string{"live": live, "atomic": "true"}
	if busy {
		aria["busy"] = "true"
	}
	return html.Div(html.Props{Role: role, Aria: aria, Data: map[string]string{"work-state": state}}, ui.Text(message))
}

func retryButton(label string) ui.Node {
	return html.Button(html.Props{
		ID:   "retry-work",
		Type: "button",
		Aria: map[string]string{"label": label},
		Data: map[string]string{"work-action": "retry-read"},
	}, ui.Text(label))
}
