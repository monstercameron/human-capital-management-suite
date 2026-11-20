package main

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// #3908: a location sheet opened for one conversation is stale in another.
func TestIssue3908_SheetIsStaleInAnotherConversation(t *testing.T) {
	cases := []struct {
		openedFor, selected string
		stale               bool
	}{
		{"people-ops", "benefits", true},
		{"people-ops", "people-ops", false},
		{"people-ops", "", true},
		{"", "benefits", false},
	}
	for _, c := range cases {
		if got := ChatmapSheetStale(c.openedFor, c.selected); got != c.stale {
			t.Errorf("ChatmapSheetStale(%q, %q) = %v, want %v", c.openedFor, c.selected, got, c.stale)
		}
	}
}

// #3862: a typed address is shared only once a lookup has resolved it, and
// editing the address throws the resolved point away.
func TestIssue3862_TypedAddressNeedsResolvedPoint(t *testing.T) {
	if err := chatmapAddressReady(chat.LocationAddress, ""); !errors.Is(err, errChatmapAddressUnresolved) {
		t.Fatalf("unresolved typed address: err = %v, want errChatmapAddressUnresolved", err)
	}
	if err := chatmapAddressReady(chat.LocationAddress, "  "); !errors.Is(err, errChatmapAddressUnresolved) {
		t.Fatalf("blank point: err = %v", err)
	}
	if err := chatmapAddressReady(chat.LocationAddress, "47.606200"); err != nil {
		t.Fatalf("resolved typed address: err = %v", err)
	}
	if err := chatmapAddressReady(chat.LocationDevice, ""); err != nil {
		t.Fatalf("other sources are checked elsewhere: err = %v", err)
	}
	if got := chatmapAddressStatusKey(true); got != "lookup_full" {
		t.Fatalf("lookup unavailable status = %q", got)
	}
	if got := chatmapAddressStatusKey(false); got != "unresolved" {
		t.Fatalf("lookup available status = %q", got)
	}
}
