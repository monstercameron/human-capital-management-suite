package chatui

import "strings"

// Issue #3949: after a report dialog was closed with Escape, a second Escape
// closed the message menu behind it and focus fell to the page. The menu's
// More button is drawn only while its message is the active row, so once the
// button was gone there was nothing to focus. The decision of where focus goes
// when a message menu closes is kept here, apart from the browser.

// MenuReturnTarget names what takes focus when a message's menu closes:
// "trigger" is the remembered More button while it is drawn, "button" a More
// button found by the message's id, "row" the message row itself (which draws
// its More button as it takes focus, and the client then moves on to it), and ""
// when the message is no longer on the page.
func MenuReturnTarget(triggerDrawn, buttonDrawn, rowFound bool) string {
	switch {
	case triggerDrawn:
		return "trigger"
	case buttonDrawn:
		return "button"
	case rowFound:
		return "row"
	}
	return ""
}

// MenuRowScope turns the name a menu is opened under into the pane to look in
// and the message to find there: the thread pane's menus are "thread:<id>".
func MenuRowScope(menuID string) (scope, postID string) {
	if id, ok := strings.CutPrefix(menuID, "thread:"); ok {
		return ".thread-pane", id
	}
	return ".message-list", menuID
}
