package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/ambientagents"
)

func TestIssue3805_AcceptedWorkflowOfferNavigatesToItsStartPage(t *testing.T) {
	cards := func(state, href string) ambientagents.Snapshot {
		return ambientagents.Snapshot{Cards: []chatui.AgentUXAmbientCard{
			{ID: "other", Kind: "TASK", State: "ADDED", StartHref: "/workspace/app/elsewhere"},
			{ID: "offer", Kind: "WORKFLOW_START", State: state, StartHref: href},
		}}
	}
	open := ambientagents.Command{ID: "offer", Action: "OPEN"}
	const start = "/workspace/app/workflow-start/start/wf-bank"
	if got := ambientWorkflowStartTarget(open, cards("ACCEPTED", start), true); got != start {
		t.Fatalf("accepted offer went to %q", got)
	}
	for name, got := range map[string]string{
		"refused":       ambientWorkflowStartTarget(open, cards("ACCEPTED", start), false),
		"not accepted":  ambientWorkflowStartTarget(open, cards("UNAVAILABLE", start), true),
		"dismiss":       ambientWorkflowStartTarget(ambientagents.Command{ID: "offer", Action: "DISMISS"}, cards("ACCEPTED", start), true),
		"other card":    ambientWorkflowStartTarget(ambientagents.Command{ID: "other", Action: "OPEN"}, cards("ACCEPTED", start), true),
		"absolute":      ambientWorkflowStartTarget(open, cards("ACCEPTED", "https://evil.example/x"), true),
		"protocol rel.": ambientWorkflowStartTarget(open, cards("ACCEPTED", "//evil.example/x"), true),
		"empty":         ambientWorkflowStartTarget(open, cards("ACCEPTED", ""), true),
	} {
		if got != "" {
			t.Fatalf("%s navigated to %q", name, got)
		}
	}
}
