package notifyplan

import "testing"

// TestTodo_WF_NOTIFY_001_Plan pins the declaration both the execution composition
// and the workflow editor read: who is told, when, and under which purpose.
func TestTodo_WF_NOTIFY_001_Plan(t *testing.T) {
	for stepType, want := range map[string][]Notice{
		"APPROVAL":   {{AudienceAssignee, MomentRouted, PurposeApproval}, {AudienceRequester, MomentRouted, PurposeUpdate}},
		" approval ": {{AudienceAssignee, MomentRouted, PurposeApproval}, {AudienceRequester, MomentRouted, PurposeUpdate}},
		"TASK":       {{AudienceAssignee, MomentRouted, PurposeTask}, {AudienceRequester, MomentRouted, PurposeUpdate}},
		"END":        {{AudienceRequester, MomentFinished, PurposeUpdate}},
		"DECISION":   nil, "CAPABILITY": nil, "WAIT": nil, "": nil,
	} {
		got := ForStep(stepType)
		if len(got) != len(want) {
			t.Fatalf("%q: %+v, want %+v", stepType, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%q notice %d: %+v, want %+v", stepType, i, got[i], want[i])
			}
		}
	}
	// A caller may not edit the declaration through a returned slice.
	ForStep("END")[0].Purpose = "changed"
	if ForStep("END")[0].Purpose != PurposeUpdate {
		t.Fatal("ForStep returned shared storage")
	}
	if AssigneePurpose(true) != PurposeApproval || AssigneePurpose(false) != PurposeTask {
		t.Fatal("assignee purpose")
	}
}
