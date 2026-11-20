package chatui

import (
	"strings"
	"testing"
)

// An unsaved purpose survives a trip to another channel and back (#3909): it
// is kept per channel, not saved, and the other channel does not inherit it.
func TestIssue3909_UnsavedPurposeSurvivesSwitchingChannels(t *testing.T) {
	local := localStore{box: &localUI{}}
	local.forRoom("engineering")
	local.update(func(u *localUI) { u.purposeEditing, u.purposeDraft = true, "Ship the new onboarding" })

	local.forRoom("design")
	if st := local.get(); st.purposeEditing || st.purposeDraft != "" {
		t.Fatalf("another channel inherited the purpose editor: %+v", st)
	}
	local.forRoom("engineering")
	if st := local.get(); !st.purposeEditing || st.purposeDraft != "Ship the new onboarding" {
		t.Fatalf("the unsaved purpose was dropped on return: %+v", st)
	}

	m := chatux021Model()
	got := chatux021Details(t, m, handlers{local: local.get()})
	if !strings.Contains(got, `id="channel-team-purpose"`) || !strings.Contains(got, "Ship the new onboarding") {
		t.Errorf("the editor does not show the kept text: %s", got)
	}
}

// Cancel and Save are still the boundary: a closed editor is not brought back.
func TestIssue3909_CancelledPurposeIsNotRestored(t *testing.T) {
	local := localStore{box: &localUI{}}
	local.forRoom("engineering")
	local.update(func(u *localUI) { u.purposeEditing, u.purposeDraft = true, "draft" })
	local.update(func(u *localUI) { u.purposeEditing = false })
	local.forRoom("design")
	local.forRoom("engineering")
	if st := local.get(); st.purposeEditing {
		t.Fatalf("a cancelled editor came back: %+v", st)
	}
}

// What is typed is recorded as it is typed, so there is text to keep.
func TestIssue3909_TypedPurposeIsRecorded(t *testing.T) {
	local := localStore{box: &localUI{}}
	local.forRoom("a")
	local.box.purposeEditing = true
	issue3909RememberPurpose(local, "purpose", "Hiring plans")
	local.forRoom("a")
	local.forRoom("b")
	local.forRoom("a")
	if st := local.get(); !st.purposeEditing || st.purposeDraft != "Hiring plans" {
		t.Fatalf("typed text lost: %+v", st)
	}
}
