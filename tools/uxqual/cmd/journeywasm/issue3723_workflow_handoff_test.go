package main

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func issue3723View(actions []productui.LauncherActionProjection, perms ...productui.RolePagePermission) productui.View {
	view := productui.View{LauncherActions: actions, EffectivePermissions: perms}
	view.Locale = productui.ResolveProductLocale("")
	return view
}

// Issue 3723: the handoff is offered only to a viewer the server resolved as
// able to start a promotion (the launcher's verdict, People view and journey
// create), never merely because People is in their navigation, and it goes
// through the shell's router with the ids.
func TestIssue3723_HandoffIsOfferedOnlyWhereTheViewerCanStartAPromotion(t *testing.T) {
	var went []string
	navigate := func(href string) { went = append(went, href) }
	available := []productui.LauncherActionProjection{{ID: productui.SemanticActionPromoteWorker, State: productui.ActionState{Availability: productui.ActionAvailable}}}
	full := []productui.RolePagePermission{{Page: productui.PagePeople, View: true}, {Page: productui.PageJourneys, View: true, Create: true}}

	state := workflowHandoffState(issue3723View(available, full...), navigate)
	if !state.Enabled || state.Start == nil {
		t.Fatal("handoff not offered to a viewer who may start a promotion")
	}
	state.Start("conv-1", "msg-9")
	if len(went) != 1 || !strings.HasPrefix(went[0], "/workspace/app/people?") || !strings.Contains(went[0], "from_conversation=conv-1") || !strings.Contains(went[0], "from_message=msg-9") {
		t.Fatalf("navigated to %v", went)
	}

	unavailable := []productui.LauncherActionProjection{{ID: productui.SemanticActionPromoteWorker, State: productui.ActionState{Availability: productui.ActionUnavailable}}}
	duplicated := append(append([]productui.LauncherActionProjection{}, available...), available...)
	for name, view := range map[string]productui.View{
		"no launcher projection":      issue3723View(nil, full...),
		"action unavailable":          issue3723View(unavailable, full...),
		"duplicate projections":       issue3723View(duplicated, full...),
		"other action only":           issue3723View([]productui.LauncherActionProjection{{ID: "other", State: productui.ActionState{Availability: productui.ActionAvailable}}}, full...),
		"cannot create a journey":     issue3723View(available, productui.RolePagePermission{Page: productui.PagePeople, View: true}, productui.RolePagePermission{Page: productui.PageJourneys, View: true}),
		"cannot open the people page": issue3723View(available, productui.RolePagePermission{Page: productui.PageJourneys, View: true, Create: true}, productui.RolePagePermission{Page: productui.PagePeople}),
		"navigation reaches people only": func() productui.View {
			v := issue3723View(nil, full...)
			v.Navigation = []productui.NavItem{{Page: productui.PagePeople}}
			return v
		}(),
	} {
		if off := workflowHandoffState(view, navigate); off.Enabled || off.Start != nil {
			t.Errorf("%s: handoff offered", name)
		}
	}
	if off := workflowHandoffState(issue3723View(available, full...), nil); off.Enabled {
		t.Error("handoff offered with no router")
	}
	went = nil
	state.Start("", "m")
	if len(went) != 0 {
		t.Fatalf("navigated without a conversation: %v", went)
	}
}
