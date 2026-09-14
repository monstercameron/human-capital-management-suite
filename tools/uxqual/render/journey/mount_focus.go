package journey

import "github.com/monstercameron/GoWebComponents/v5/ui"

// focusOnMountProps names the one element a mount-only focus effect should
// move focus to.
type focusOnMountProps struct {
	TargetID string
}

// focusOnMount exists only so useFocusOnMount is called through
// ui.CreateElement rather than as a plain nested function call.
//
// This is not a stylistic preference: live.go's own doc comment on
// LiveComponent explains why it matters. LiveComponent is the one
// persistent fiber a live client mounts, and it keeps its own hook
// sequence fixed at exactly two calls "because GWC hooks are positional...
// a conditional or data-dependent hook would corrupt the fiber's hook list
// on the next render" -- and Build's own tree, reached by calling
// resolvedPageBody's switch over List/Proposal/Detail, is exactly that
// kind of data-dependent, conditionally-shaped structure. A hook called
// directly from a plain function inside that tree (proposalView, before
// this fix) does not get its own fiber: it lands in whatever hook slot
// LiveComponent's OWN flat sequence happens to reach that render, and the
// number and order of hooks that sequence contains is different on every
// page-type transition. The first version of useFocusOnMount was wired
// exactly that way -- called directly inside proposalView -- and the live
// measurement that prompted this fix could not tell whether the effect ran
// at all: activeElement stayed BODY even once the target element it was
// mistakenly aimed at (the page heading) existed in the DOM with the right
// id.
//
// Routing it through ui.CreateElement instead gives it the same isolation
// reviewSurface's own ui.UseState already relies on (and that Cam
// separately verified live: the review surface's open/closed state and its
// Escape handling both behave correctly): a real child element in the
// returned tree gets reconciled and given its own fiber at its own tree
// position, independent of whatever else LiveComponent's single render
// pass happens to be building alongside it. It renders nil -- it exists
// for its effect, not for markup.
func focusOnMount(props focusOnMountProps) ui.Node {
	focusOnMountHook(props.TargetID)
	return nil
}

// focusOnMountHook is useFocusOnMount by default. It exists as a seam
// (rather than calling useFocusOnMount directly) because useFocusOnMount's
// own effect is a no-op on the native/SSR path this package's tests run on
// (see proposal_view_focus_native.go) -- WHICH target id a caller wired
// this to is otherwise invisible to a RenderToString-based test, since
// focusOnMount renders nil either way. A test swaps this var for a spy to
// prove the wiring rather than the rendered markup; production code never
// touches it.
var focusOnMountHook = useFocusOnMount
