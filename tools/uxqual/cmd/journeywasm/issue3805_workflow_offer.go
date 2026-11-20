package main

import (
	"net/url"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/ambientagents"
)

// ambientWorkflowStartTarget is where the page goes after the person pressed
// "Start request" on a workflow offer and the server accepted it: the
// workflow's own start page, taken from the accepted card. It returns "" for any
// other command, a refused one, or a link that is not a path on this site, so a
// card can never send the page elsewhere. Nothing is submitted by going there.
func ambientWorkflowStartTarget(command ambientagents.Command, snapshot ambientagents.Snapshot, accepted bool) string {
	if !accepted || command.Action != "OPEN" {
		return ""
	}
	for _, card := range snapshot.Cards {
		if card.ID != command.ID || card.Kind != "WORKFLOW_START" || card.State != "ACCEPTED" {
			continue
		}
		parsed, err := url.Parse(card.StartHref)
		if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/") || strings.HasPrefix(card.StartHref, "//") || strings.ContainsAny(card.StartHref, "\\\r\n") {
			return ""
		}
		return card.StartHref
	}
	return ""
}
