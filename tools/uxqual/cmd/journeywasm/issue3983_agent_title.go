package main

import "strings"

// agentTaskTabTitle is the tab title for the Agents page: the open task's
// question in front of the page's own title, replacing the question put there
// for an earlier task. current is the document's title now, last the title this
// code set before, base the page title kept from then. The returned base is
// the page's own title: current unless it is still what this code set last,
// and an empty prefix gives the base back, so closing a task removes its question.
func agentTaskTabTitle(current, last, base, prefix string) (title, keptBase string) {
	keptBase = chatTabTitleBase(current, last, base)
	if prefix == "" {
		return keptBase, keptBase
	}
	return strings.TrimSpace(prefix) + " · " + keptBase, keptBase
}
