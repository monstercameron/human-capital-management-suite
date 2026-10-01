package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func TestTodo_UXAUDIT_003_ClientProjection(t *testing.T) {
	values := []journeyclient.LauncherAction{{
		ID: productui.SemanticActionPromoteWorker, Availability: string(productui.ActionAvailable), Priority: 7,
	}}
	got := projectLauncherActions(values)
	if len(got) != 1 || got[0].ID != productui.SemanticActionPromoteWorker || got[0].State.Availability != productui.ActionAvailable || got[0].Priority != 7 {
		t.Fatalf("available projection = %+v", got)
	}

	for name, candidate := range map[string][]journeyclient.LauncherAction{
		"blank state": {{ID: productui.SemanticActionPromoteWorker}},
		"unknown":     {{ID: productui.SemanticActionPromoteWorker, Availability: "future"}},
		"hidden":      {{ID: productui.SemanticActionPromoteWorker, Availability: string(productui.ActionHidden)}},
		"negative":    {{ID: productui.SemanticActionPromoteWorker, Availability: string(productui.ActionAvailable), Priority: -1}},
		"duplicate":   {values[0], values[0]},
		"bad denial":  {{ID: productui.SemanticActionPromoteWorker, Availability: string(productui.ActionUnavailable), Reason: "Ask for access"}},
	} {
		if projected := projectLauncherActions(candidate); len(projected) != 0 {
			t.Fatalf("%s failed open: %+v", name, projected)
		}
	}

	unavailable := projectLauncherActions([]journeyclient.LauncherAction{{
		ID: productui.SemanticActionPromoteWorker, Availability: string(productui.ActionUnavailable),
		Reason: "Ask for access", RecoveryLabel: "Learn about access", RecoveryHref: "/workspace/app/help",
	}})
	if len(unavailable) != 1 || unavailable[0].State.Recovery.Href != "/workspace/app/help" {
		t.Fatalf("safe unavailable projection = %+v", unavailable)
	}
}

func TestTodo_WFPAGE_002_ClientWorkflowStarts(t *testing.T) {
	got := projectWorkflowStarts([]journeyclient.WorkflowStart{
		{WorkflowID: " hire ", Name: "New hire", Availability: "available", Keywords: []string{"onboarding"}},
		{WorkflowID: "hire", Name: "duplicate", Availability: "available"},
		{WorkflowID: "", Name: "no id", Availability: "available"},
		{WorkflowID: "odd", Name: "Odd", Availability: "granted-by-server-maybe"},
	})
	if len(got) != 2 || got[0].WorkflowID != "hire" || got[0].Availability != productui.WorkflowStartAvailable {
		t.Fatalf("projection = %+v", got)
	}
	if got[1].Availability != productui.WorkflowStartMissingAuthority {
		t.Fatalf("unknown availability = %q, want fail-closed missing_authority", got[1].Availability)
	}
}
