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
