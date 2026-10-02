package main

import (
	"strconv"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chatlang004Unsettled names the messages whose translation is on its way or
// did not arrive yet: the server said "pending", or said it fell back to the
// original although this reader asked for a translation. They are asked about
// again, a few times, so a translation replaces the text when it is ready
// without anyone reloading. A message whose original the server withholds, one
// the reader did not ask to have translated, and one already translated are
// settled.
func chatlang004Unsettled(selections map[string]chatui.ReaderSelection) []string {
	var ids []string
	for id, selection := range selections {
		asked := false
		for _, kind := range selection.Mark.Wanted {
			asked = asked || kind == chatrender.Translate
		}
		if asked && (selection.Mark.State == "pending" || selection.Mark.State == "fallback") {
			ids = append(ids, id)
		}
	}
	return ids
}

// chatlang004AudienceKey identifies what the composer's "who reads this in
// which language" line was read for; the line is read again when it changes.
func chatlang004AudienceKey(m chatui.Model, epoch uint64) string {
	members := 0
	for _, room := range m.Conversations {
		if room.ID == m.SelectedID {
			members = room.MemberCount
		}
	}
	return m.CurrentTenantID + "\x00" + m.CurrentUser + "\x00" + m.SelectedID + "\x00" + strconv.Itoa(members) + "\x00" + strconv.Itoa(len(m.Members)) + "\x00" + strconv.FormatUint(epoch, 10)
}
