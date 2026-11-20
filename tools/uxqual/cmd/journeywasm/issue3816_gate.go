package main

import "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"

// chatsearchOpensGate reports whether a search result is shown in the channel's
// gate form (issue 3816): a gate question, or a person's own answer to one. Such
// a result does not move the page to a conversation; the form opens over the
// results, which stay where they are.
func chatsearchOpensGate(kind chatsearch.Kind) bool {
	return kind == chatsearch.GateQuestion || kind == chatsearch.GateAnswer
}
