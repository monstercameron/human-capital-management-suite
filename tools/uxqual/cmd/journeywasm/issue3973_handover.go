package main

import (
	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// Issue 3973. The work handover panel's state machine and the recipient access
// rule. The panel lives only in the page's model: nothing about it is stored,
// and it is dropped when the conversation changes.

// handoverRecipientAccess says whether the colleague belongs to a project:
// "yes" only for an active member listed by the project, "no" for anyone the
// complete list does not show as active. Whether the list was complete is the
// caller's to know; an incomplete or refused read is "unknown", never "no".
func handoverRecipientAccess(members []*projectv1.ProjectMember, recipient string) string {
	if recipient == "" {
		return "unknown"
	}
	for _, member := range members {
		if member.GetUserId() == recipient && member.GetState() == "ACTIVE" {
			return "yes"
		}
	}
	return "no"
}

func handoverCopySet(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for key, on := range in {
		if on {
			out[key] = true
		}
	}
	return out
}

// handoverApply applies one command to the panel's state. It reports whether
// the browser should now read the selected tasks and build the brief. The
// selection is always limited to tasks the conversation shows a readable card
// for, and the colleague to the conversation's own people, whatever the
// command carries.
func handoverApply(view chatui.HandoverView, conversationID string, cmd chatui.HandoverCommand, candidates []chatui.HandoverCandidate, people []chatui.Member) (chatui.HandoverView, bool) {
	if view.ConversationID != conversationID {
		view = chatui.HandoverView{ConversationID: conversationID}
	}
	view.Selected = handoverCopySet(view.Selected)
	readable := map[string]bool{}
	for _, candidate := range candidates {
		if candidate.Readable {
			readable[candidate.Key] = true
		}
	}
	for key := range view.Selected {
		if !readable[key] {
			delete(view.Selected, key)
		}
	}
	if view.Stage == "draft" && cmd.Text != "" {
		view.Text = cmd.Text
	} else if view.Stage != "draft" && view.Stage != "preparing" && cmd.Kind != "open" {
		view.Recipient, view.From, view.To = cmd.Recipient, cmd.From, cmd.To
	}
	switch cmd.Kind {
	case "open":
		view.Open = true
		if view.Stage == "" || view.Stage == "preparing" || view.Stage == "error" {
			view.Stage = "pick"
		}
		view.Status = ""
	case "close":
		view.Open = false
	case "toggle":
		if readable[cmd.Key] {
			if view.Selected[cmd.Key] {
				delete(view.Selected, cmd.Key)
			} else if len(view.Selected) < chatui.HandoverMaxTasks {
				view.Selected[cmd.Key] = true
			}
		}
		view.Status = ""
	case "back":
		view.Stage, view.Status = "pick", ""
	case "drafted":
		view.Status = "drafted"
	case "prepare":
		known := false
		for _, person := range people {
			if person.ID == view.Recipient {
				known, view.RecipientName = true, person.Name
			}
		}
		switch {
		case len(view.Selected) == 0:
			view.Status = "none"
		case !known:
			view.Status = "recipient"
		case !chatui.HandoverWindowValid(view.From, view.To):
			view.Status = "window"
		default:
			view.Stage, view.Status = "preparing", ""
			return view, true
		}
		view.Stage = "pick"
	}
	return view, false
}
