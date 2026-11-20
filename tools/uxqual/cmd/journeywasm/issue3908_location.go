package main

import (
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// ChatmapSheetStale reports whether a Location sheet opened for one
// conversation is now showing another (#3908). Its choices belong to the
// conversation it was opened in, so the browser code closes and clears it.
func ChatmapSheetStale(openedFor, selected string) bool {
	return openedFor != "" && openedFor != selected
}

// errChatmapAddressUnresolved means a typed address has not been resolved to a
// point the person can check (#3862).
var errChatmapAddressUnresolved = errors.New("address not resolved")

// chatmapAddressReady is nil unless the source is a typed address that has no
// resolved point: such a draft is not sendable, because the person could not
// see what readers would get. Editing the address clears the point, so an
// edited address is unresolved again.
func chatmapAddressReady(source chat.LocationSource, latitude string) error {
	if source == chat.LocationAddress && strings.TrimSpace(latitude) == "" {
		return errChatmapAddressUnresolved
	}
	return nil
}

// chatmapAddressStatusKey is the sheet's status line for an unresolved typed
// address: the lookup failure with its alternative when lookup cannot run,
// otherwise the instruction to find the address.
func chatmapAddressStatusKey(lookupUnavailable bool) string {
	if lookupUnavailable {
		return "lookup_full"
	}
	return "unresolved"
}
