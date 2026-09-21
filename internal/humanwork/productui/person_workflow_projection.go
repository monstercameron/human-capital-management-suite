package productui

// PersonWorkflowPresentation is how a surface shows one person's workflow
// actions. It is chosen from an already-resolved projection, by count and
// kind only: a renderer picks direct versus menu presentation without
// re-deciding eligibility or authority (UXLIVE-033).
type PersonWorkflowPresentation string

const (
	// PersonWorkflowContinue leads with the person's active request as a
	// direct "Open active promotion" action.
	PersonWorkflowContinue PersonWorkflowPresentation = "continue"
	// PersonWorkflowDirect offers the one launchable workflow directly.
	PersonWorkflowDirect PersonWorkflowPresentation = "direct"
	// PersonWorkflowMenu offers several launchable workflows in the shared
	// row menu.
	PersonWorkflowMenu PersonWorkflowPresentation = "menu"
	// PersonWorkflowUnavailable is a quiet explanation: nothing can start.
	PersonWorkflowUnavailable PersonWorkflowPresentation = "unavailable"
)

// PersonWorkflowActionProjection is the one resolved answer to "what can
// this viewer do for this person" that the People row, the person page's
// workflow launcher and the shell's action launcher all read. Authority is
// decided first and in one place; a viewer without it gets the identical
// withheld reason for every person, whatever the person's hidden
// eligibility, and no continuation that would reveal an open request.
type PersonWorkflowActionProjection struct {
	// Authorized is the viewer's authority to start workflows at all.
	Authorized bool
	// Actions are the launchable actions in rank order. A continuation of
	// an active request carries Continuation and comes first.
	Actions []PeopleQuickActionProps
	// Reason explains why a named workflow is not actionable for this
	// person, and ReasonWorkflow names that workflow.
	Reason         string
	ReasonWorkflow string
	// ActiveRequest is true when an open request for this person is in the
	// viewer's authorized work.
	ActiveRequest bool
}

// ResolvePersonWorkflowActions resolves the projection for one person.
func ResolvePersonWorkflowActions(view View, person Person) PersonWorkflowActionProjection {
	return resolvePersonWorkflowActions(view, person, rankedPersonWorkflows(view.PersonWorkflows, view.WorkflowUses))
}

func resolvePersonWorkflowActions(view View, person Person, catalog []PersonWorkflow) PersonWorkflowActionProjection {
	projection := PersonWorkflowActionProjection{Authorized: view.Allows(PageJourneys, "create")}
	if !projection.Authorized {
		// Authority is checked before any per-person business state, so the
		// shape and the words are the same for every person this viewer
		// sees: no action, no continuation, the same withheld reason.
		projection.Reason = PromotionAvailabilityReason(view.Locale, PromotionWithheld)
		if len(catalog) > 0 {
			projection.ReasonWorkflow = catalog[0].Name
		}
		return projection
	}
	actions, reason, reasonWorkflow, active := personWorkflowActionList(view, person, catalog)
	// A continuation of an open request leads: it is the obvious next step
	// for this person, ahead of any other workflow the ranking put first.
	projection.Actions = make([]PeopleQuickActionProps, 0, len(actions))
	for _, action := range actions {
		if action.Continuation {
			projection.Actions = append(projection.Actions, action)
		}
	}
	for _, action := range actions {
		if !action.Continuation {
			projection.Actions = append(projection.Actions, action)
		}
	}
	projection.Reason, projection.ReasonWorkflow = reason, reasonWorkflow
	projection.ActiveRequest = active
	return projection
}

// Presentation chooses how the projection is shown.
func (projection PersonWorkflowActionProjection) Presentation() PersonWorkflowPresentation {
	return PersonWorkflowPresentationFor(projection.Actions)
}

// PersonWorkflowPresentationFor chooses a presentation from resolved
// actions alone: a continuation leads, one action is offered directly,
// several share the menu, none is the quiet unavailable treatment.
func PersonWorkflowPresentationFor(actions []PeopleQuickActionProps) PersonWorkflowPresentation {
	switch {
	case len(actions) == 0:
		return PersonWorkflowUnavailable
	case actions[0].Continuation:
		return PersonWorkflowContinue
	case len(actions) == 1:
		return PersonWorkflowDirect
	default:
		return PersonWorkflowMenu
	}
}

// projectionAction returns the resolved action for one catalogue workflow.
func projectionAction(projection PersonWorkflowActionProjection, workflowID string) (PeopleQuickActionProps, bool) {
	for _, action := range projection.Actions {
		if action.WorkflowID == workflowID {
			return action, true
		}
	}
	return PeopleQuickActionProps{}, false
}
