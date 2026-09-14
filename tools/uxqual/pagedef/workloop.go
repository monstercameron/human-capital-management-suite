package pagedef

// WorkLoopState is the presentation state of a bounded work projection. It
// is deliberately separate from workflow truth: the browser may present one
// of these states, but it cannot infer that a request completed from it.
type WorkLoopState string

const (
	WorkLoopLoading  WorkLoopState = "loading"
	WorkLoopEmpty    WorkLoopState = "empty"
	WorkLoopReady    WorkLoopState = "ready"
	WorkLoopStale    WorkLoopState = "stale"
	WorkLoopDenied   WorkLoopState = "denied"
	WorkLoopError    WorkLoopState = "error"
	WorkLoopRetrying WorkLoopState = "retrying"
)

// WorkLoopStates returns the closed state vocabulary in its presentation
// order. Loading and retrying are transient; ready, empty, stale, denied,
// and error are explicit outcomes and must not be collapsed into success.
func WorkLoopStates() []WorkLoopState {
	return []WorkLoopState{
		WorkLoopLoading,
		WorkLoopEmpty,
		WorkLoopReady,
		WorkLoopStale,
		WorkLoopDenied,
		WorkLoopError,
		WorkLoopRetrying,
	}
}

// Valid reports whether s is a member of the governed work-loop state
// vocabulary.
func (s WorkLoopState) Valid() bool {
	for _, known := range WorkLoopStates() {
		if s == known {
			return true
		}
	}
	return false
}

// WorkLoopPageDefinition is the default My Work workbench fixture. Its
// regions describe the discover/orient/track loop; the state of the queried
// projection is supplied by the renderer and never encoded as business truth
// in this definition.
func WorkLoopPageDefinition() PageDefinition {
	return PageDefinition{
		PageID:       "work.my-work",
		Version:      1,
		FloorplanRef: "floorplan.intent_workspace.v1",
		Regions: []Region{
			{ID: "shell", Kind: RegionShell},
			{ID: "page-identity", Kind: RegionPageIdentity, Heading: &Heading{Level: 1, Text: "My Work"}},
			{ID: "steps", Kind: RegionLocalNavigation, Heading: &Heading{Level: 2, Text: "Work views"}},
			{
				ID:       "comparison",
				Kind:     RegionPrimary,
				Heading:  &Heading{Level: 2, Text: "Assigned work"},
				Widgets:  []WidgetSlot{{ID: "work-state", WidgetRef: "widget.work-loop.state.v1"}},
				Bindings: []DataBinding{{ID: "work-items", RPC: RPCRef(IntentServiceName, "ListIntents")}},
				Actions:  []ActionRef{{ID: "refresh-work", RPC: RPCRef(IntentServiceName, "ListIntents"), RequiredRole: "work.viewer"}},
			},
			{
				ID:       "engine",
				Kind:     RegionSupporting,
				Heading:  &Heading{Level: 2, Text: "Source and freshness"},
				Widgets:  []WidgetSlot{{ID: "work-evidence", WidgetRef: "widget.work-loop.evidence.v1"}},
				Bindings: []DataBinding{{ID: "work-timeline", RPC: RPCRef(IntentServiceName, "ListIntentTimeline")}},
			},
			{
				ID:      "decision",
				Kind:    RegionCompletion,
				Heading: &Heading{Level: 2, Text: "Next step"},
				Widgets: []WidgetSlot{{ID: "work-recovery", WidgetRef: "widget.work-loop.recovery.v1"}},
			},
		},
		Accessibility: Accessibility{
			Landmarks:  []string{"banner", "navigation", "main", "contentinfo"},
			LiveRegion: LiveRegionPolite,
		},
		BrandTokens: []string{"brand.color.primary", "brand.color.status", "brand.typography.heading", "brand.spacing.md"},
	}
}

// FailureRecoveryPageDefinition is the safe fallback page for a work-loop
// projection that cannot be shown. It has a primary region and a single
// retry/read action, but no write action and no protected resource details.
func FailureRecoveryPageDefinition() PageDefinition {
	return PageDefinition{
		PageID:       "work.recovery",
		Version:      1,
		FloorplanRef: "floorplan.intent_workspace.v1",
		Regions: []Region{
			{ID: "shell", Kind: RegionShell},
			{ID: "page-identity", Kind: RegionPageIdentity, Heading: &Heading{Level: 1, Text: "Work unavailable"}},
			{ID: "steps", Kind: RegionLocalNavigation, Heading: &Heading{Level: 2, Text: "Recovery"}},
			{
				ID:       "comparison",
				Kind:     RegionPrimary,
				Heading:  &Heading{Level: 2, Text: "What happened"},
				Widgets:  []WidgetSlot{{ID: "failure-state", WidgetRef: "widget.failure-recovery.state.v1"}},
				Bindings: []DataBinding{{ID: "retry-read", RPC: RPCRef(IntentServiceName, "GetIntent")}},
				Actions:  []ActionRef{{ID: "retry-read", RPC: RPCRef(IntentServiceName, "GetIntent"), RequiredRole: "work.viewer"}},
			},
			{
				ID:      "engine",
				Kind:    RegionSupporting,
				Heading: &Heading{Level: 2, Text: "Safe recovery"},
				Widgets: []WidgetSlot{{ID: "failure-recovery", WidgetRef: "widget.failure-recovery.actions.v1"}},
			},
			{ID: "decision", Kind: RegionCompletion, Heading: &Heading{Level: 2, Text: "Finish safely"}},
		},
		Accessibility: Accessibility{
			Landmarks:  []string{"banner", "navigation", "main", "contentinfo"},
			LiveRegion: LiveRegionAssertive,
		},
		BrandTokens: []string{"brand.color.status", "brand.typography.heading", "brand.spacing.md"},
	}
}

// AccessibleFailurePageDefinition is a descriptive alias for callers that
// name the route by its accessibility guarantee.
func AccessibleFailurePageDefinition() PageDefinition { return FailureRecoveryPageDefinition() }
