package chatpolicy

import "testing"

func TestTodo_CHATSTATE_001(t *testing.T) {
	if NormalizeChannelStatus("ACTIVE") != StatusOpen || NormalizeChannelStatus("") != StatusOpen {
		t.Fatal("legacy status mapping")
	}
	if !CanChangeChannelStatus(StatusOpen, StatusAnnouncements, StatusPermissions{ChannelAdmin: true}) {
		t.Fatal("channel admin cannot select announcements")
	}
	if CanChangeChannelStatus(StatusOpen, StatusLocked, StatusPermissions{ChannelAdmin: true}) {
		t.Fatal("channel admin can lock")
	}
	if !CanChangeChannelStatus(StatusLocked, StatusOpen, StatusPermissions{WorkspaceAdmin: true}) {
		t.Fatal("workspace admin cannot restore")
	}
}

func TestTodo_CHATSTATE_001_Property(t *testing.T) {
	expected := [4][7]bool{{true, true, true, true, true, true, true}, {false, true, true, false, true, true, true}, {false, false, false, false, false, true, true}, {}}
	for i, rule := range StatusRegistry() {
		for action := StatusPost; action <= StatusRename; action++ {
			if got := StatusAllows(rule.Status, action, false, false); got != expected[i][action] {
				t.Fatalf("%s action %d = %v", rule.Status, action, got)
			}
		}
	}
	copy := StatusRegistry()
	copy[0].Allowed[0] = false
	if !StatusAllows(StatusOpen, StatusPost, false, false) {
		t.Fatal("caller mutated registry")
	}
}

func TestTodo_CHATSTATE_001_Security(t *testing.T) {
	for _, status := range []ChannelStatus{StatusLocked, StatusArchived, "forged"} {
		for _, action := range []StatusAction{StatusPost, StatusReply, StatusReact, StatusEdit, StatusPin} {
			if StatusAllows(status, action, true, true) {
				t.Fatalf("privilege bypass of %s", status)
			}
		}
	}
	if StatusAllows(StatusOpen, 99, true, true) {
		t.Fatal("unknown action allowed")
	}
	if CanChangeChannelStatus("forged", StatusOpen, StatusPermissions{WorkspaceAdmin: true}) {
		t.Fatal("unknown source status transitioned")
	}
	if !StatusAllows(StatusAnnouncements, StatusPost, false, true) || !StatusAllows(StatusAnnouncements, StatusPost, true, false) {
		t.Fatal("announcers refused")
	}
	for _, from := range []ChannelStatus{StatusOpen, StatusAnnouncements, StatusLocked, StatusArchived} {
		for _, to := range []ChannelStatus{StatusOpen, StatusAnnouncements, StatusLocked, StatusArchived, "forged"} {
			if CanChangeChannelStatus(from, to, StatusPermissions{}) {
				t.Fatal("unprivileged transition")
			}
		}
	}
}
