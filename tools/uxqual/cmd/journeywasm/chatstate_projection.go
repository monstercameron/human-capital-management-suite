package main

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

var errChatstateEvent = errors.New("channel status event is invalid")

// applyChatstateEvent consumes the canonical outbox value after the watch has
// rechecked membership. Older events cannot roll a newer status back.
func applyChatstateEvent(view chatui.ChannelStatusView, tenant, conversation string, payload []byte, at time.Time) (chatui.ChannelStatusView, error) {
	var status chat.ChannelStatus
	if json.Unmarshal(payload, &status) != nil || status.TenantID != tenant || status.ConversationID != conversation || status.Revision == 0 {
		return view, errChatstateEvent
	}
	valid := false
	for _, rule := range chatpolicy.StatusRegistry() {
		if rule.Status == status.Status {
			valid = true
		}
	}
	if !valid {
		return view, errChatstateEvent
	}
	if status.Revision <= view.Status.Revision {
		return view, nil
	}
	view.Status = status
	view.Status.Status = status.Effective(at)
	view.Updated = true
	view.Loading = false
	view.Unavailable = false
	// Permission must be refreshed from the server after a status change.
	view.CanPost = false
	view.Transitions = nil
	view.ActorName = ""
	return view, nil
}
