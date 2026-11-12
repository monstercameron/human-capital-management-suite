package productui

// On the Chat page the product navigation opens as its icon rail (#3771).
// Chat brings a conversation list of its own, and two full-width lists side by
// side left the conversation a narrow column. Nothing changes on other pages.
//
// The rail is Chat's default, not a choice: it does not set View.NavCollapsed,
// so links that leave Chat do not carry nav=collapsed and the stored
// workspace-wide preference is not rewritten. A person's own choice wins and
// is remembered: nav=expanded or nav=collapsed in the address, and between
// visits the choice stored for Chat (ChatNavigationTable).

const (
	// ChatNavigationTable and ChatNavigationFilter name where the person's
	// choice for Chat is stored among their table preferences, the same store
	// My Work keeps its saved tab in.
	ChatNavigationTable  = "chat"
	ChatNavigationFilter = "nav"

	chatNavigationExpanded  = "expanded"
	chatNavigationCollapsed = "collapsed"
)

// ChatRailDefault reports whether page opens with the navigation as a rail
// unless the person chose otherwise.
func ChatRailDefault(page PageID) bool { return page == PageChat }

// navRailCollapsed is whether the shell draws the navigation as its rail: the
// person's collapse, or Chat's default where they did not ask for the full
// navigation.
func (view View) navRailCollapsed() bool {
	return view.NavCollapsed || (ChatRailDefault(view.Page) && !view.NavExpanded)
}

// ChatNavigationChoice is the choice to store for Chat after a view of it:
// "expanded" or "collapsed" when the person made one, empty when the view
// shows Chat's default or is not Chat.
func ChatNavigationChoice(view View) string {
	switch {
	case !ChatRailDefault(view.Page):
		return ""
	case view.NavExpanded:
		return chatNavigationExpanded
	case view.NavCollapsed:
		return chatNavigationCollapsed
	}
	return ""
}

// ChatNavigationStoredExpanded reports whether a visit to page whose address
// names no navigation state opens expanded because the person chose that for
// Chat before. collapsed is their workspace-wide stored preference, which wins:
// it is the later, wider choice whenever the two disagree.
func ChatNavigationStoredExpanded(page PageID, collapsed bool, filters map[string]string) bool {
	return ChatRailDefault(page) && !collapsed && filters[ChatNavigationFilter] == chatNavigationExpanded
}
