package main

import (
	"sync/atomic"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chatBrowseAnnounceQuiet is how long the Browse filter stays unchanged before
// the dialog announces how many channels match (#3922).
const chatBrowseAnnounceQuiet = 600 * time.Millisecond

var chatBrowseAnnounceSeq atomic.Uint64

// startChatBrowseAnnounce is called on every filter change. It silences the
// status at once and returns the ticket the settle step must still hold.
func (s *chatState) startChatBrowseAnnounce() uint64 {
	seq := chatBrowseAnnounceSeq.Add(1)
	s.mutate(func(model *chatui.Model) { model.BrowseAnnounce = false })
	return seq
}

// settleChatBrowseAnnounce turns the status on when the ticket is still the
// latest and the browser is still open. It reports whether the page changed.
func (s *chatState) settleChatBrowseAnnounce(seq uint64) bool {
	if chatBrowseAnnounceSeq.Load() != seq {
		return false
	}
	changed := false
	s.mutate(func(model *chatui.Model) {
		if model.ShowBrowse && !model.BrowseAnnounce {
			model.BrowseAnnounce, changed = true, true
		}
	})
	return changed
}
