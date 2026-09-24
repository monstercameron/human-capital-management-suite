package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// ListDetailLayout is the responsive layout behind the My
// Work list-detail panes. The browser measures the viewport;
// Go governs which panes the measured layout renders. The
// zero value is unknown and fails closed to the list so
// navigation is never stranded.
type ListDetailLayout int

const (
	// ListDetailWide shows list and detail side by side.
	ListDetailWide ListDetailLayout = iota + 1
	// ListDetailNarrow shows one pane: detail on selection,
	// list otherwise.
	ListDetailNarrow
)

// ResolveListDetailPanes decides which My Work panes a
// layout shows for a selection state. Wide layouts always
// show both; narrow layouts show the detail when something
// is selected and the list otherwise; unknown layouts show
// the list only.
func ResolveListDetailPanes(layout ListDetailLayout, hasSelection bool) (showList, showDetail bool) {
	switch layout {
	case ListDetailWide:
		return true, true
	case ListDetailNarrow:
		return !hasSelection, hasSelection
	}
	return true, false
}

func workLayoutName(layout ListDetailLayout) string {
	if layout == ListDetailNarrow {
		return "narrow"
	}
	if layout == ListDetailWide {
		return "wide"
	}
	return "unknown"
}

func useResponsiveWorkLayout(initial ListDetailLayout) ListDetailLayout {
	state := ui.UseState(initial)
	bindWorkLayoutViewport(state)
	return state.Get()
}
