package productui

// ActionAvailability is presentation's display vocabulary for the
// semantic availability of one action, mirroring the spec rule that a
// temporarily unavailable action is shown only when its existence is
// safe and the viewer can resolve the condition. Availability arrives
// as server-composed page state; presentation renders the state — it
// never decides what a viewer may do.
type ActionAvailability string

const (
	ActionAvailable   ActionAvailability = "available"
	ActionUnavailable ActionAvailability = "unavailable"
	ActionHidden      ActionAvailability = "hidden"
)

// ActionState is the server's availability verdict on one action: its
// semantic state, the reason rendered while unavailable, and the
// recovery destination rendered when the viewer can resolve the
// condition. A blank availability keeps the legacy rendering — a live
// control — so existing surfaces opt in explicitly.
type ActionState struct {
	Availability ActionAvailability
	Reason       string
	// Recovery is the way back when the viewer can resolve the
	// condition; an empty Href renders no recovery link.
	Recovery ActionLinkProps
}

// LauncherActionProjection is the server-resolved, presentation-safe verdict
// for one semantic action in the global launcher. Page permissions and local
// workflow metadata never imply one of these verdicts. Missing, duplicate, or
// unknown projections are treated as hidden by the launcher.
type LauncherActionProjection struct {
	ID       string
	State    ActionState
	Priority int64
}

// ResolveActionAvailability maps one grant decision plus existence
// safety to its semantic state: granted actions are available;
// denied actions whose existence is safe show as unavailable with
// their reason and recovery; denied actions whose existence is unsafe
// hide entirely.
func ResolveActionAvailability(allowed, existenceSafe bool) ActionAvailability {
	if allowed {
		return ActionAvailable
	}
	if existenceSafe {
		return ActionUnavailable
	}
	return ActionHidden
}
