package chatui

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// ModerationState is what the Chat page knows about moderation for the person
// looking at it. It comes from the server's one summary read; the server still
// decides every command when it is run, so this only decides what is offered.
type ModerationState struct {
	// Ready is true once the summary has been read. Until then nothing is offered.
	Ready bool
	// Moderator is true for a person who holds a moderation permission somewhere.
	Moderator bool
	// Open is the number of open items this person can open; NoticeCount is the
	// number of recent notices sent to them.
	Open, NoticeCount                  int
	RemoveEverywhere, ReviewEverywhere bool
	Removable, Reviewable              map[string]bool
	// Notices holds the latest removal notice for each of the person's own
	// removed messages, by post id.
	Notices map[string]ModerationOwnNotice
	// Restored holds the removed messages that were put back while this page
	// was open (chatmod004_restored.go). It is the page's own knowledge, not
	// the summary's, so a new summary keeps it.
	Restored map[string]bool
}

// ModerationOwnNotice is a removal notice about the viewer's own message.
type ModerationOwnNotice struct {
	ConversationID, PostID, Reason, HostTenantID string
	CanAppeal                                    bool
}

// ModerationStateFromSummary turns the server's summary into the page's state.
func ModerationStateFromSummary(s chat.ModerationSummary) ModerationState {
	out := ModerationState{Ready: true, Moderator: s.Moderator, Open: s.Open, NoticeCount: s.NoticeCount, RemoveEverywhere: s.RemoveEverywhere, ReviewEverywhere: s.ReviewEverywhere, Removable: map[string]bool{}, Reviewable: map[string]bool{}, Notices: map[string]ModerationOwnNotice{}}
	for _, id := range s.Removable {
		out.Removable[id] = true
	}
	for _, id := range s.Reviewable {
		out.Reviewable[id] = true
	}
	// Notices arrive newest first: the first one about a post is the current one.
	for _, n := range s.Notices {
		if (n.Outcome != "remove" && n.Outcome != "restore") || strings.HasPrefix(n.ID, "outcome:") {
			continue
		}
		if _, seen := out.Notices[n.PostID]; seen {
			continue
		}
		out.Notices[n.PostID] = ModerationOwnNotice{ConversationID: n.ConversationID, PostID: n.PostID, Reason: n.Reason, HostTenantID: n.TenantID, CanAppeal: n.CanAppeal && n.Outcome == "remove"}
	}
	return out
}

// CanRemoveIn reports whether the Remove command is offered in a conversation.
func (s ModerationState) CanRemoveIn(conversation string) bool {
	return s.Ready && (s.RemoveEverywhere || s.Removable[conversation])
}

// CanRestoreIn reports whether Restore is offered in a conversation.
func (s ModerationState) CanRestoreIn(conversation string) bool {
	return s.Ready && (s.ReviewEverywhere || s.Reviewable[conversation])
}

// Attention is the number the Moderation entry of the sidebar shows: the open
// items for a moderator, the recent notices for everybody else.
func (s ModerationState) Attention() int {
	if s.Moderator {
		return s.Open
	}
	return s.NoticeCount
}

// Visible is true when the sidebar offers the Moderation entry at all.
func (s ModerationState) Visible() bool {
	return s.Ready && (s.Moderator || s.NoticeCount > 0)
}

// chatmod005CanRemove decides whether the message menu offers "Remove for
// everyone" in the selected conversation. Once the server's summary has been
// read it is the summary alone; before that a workspace administrator or the
// channel's owner is offered it, and the server refuses anyone who may not.
func (m Model) chatmod005CanRemove() bool {
	if m.Moderation.Ready {
		return m.Moderation.CanRemoveIn(m.SelectedID)
	}
	return m.IsTenantAdmin || (m.CurrentUser != "" && m.selected().OwnerID == m.CurrentUser)
}
