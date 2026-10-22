package chatui

import (
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// CHATUX-048. A message an administrator removed keeps its place in the text
// column, as any message does, and says what the reader is allowed to know:
//
//   - everyone reads "Removed by an administrator";
//   - its author also reads when, and why, from their own removal notice;
//   - a person who may review removed messages reads who removed it, when and
//     why, from the moderation record (the same record the Moderation page
//     lists, so nothing is shown here that the queue would not show them).
//
// Restore, for a person who may, and "Ask for a review", for the author, are the
// page's standard secondary buttons under the notice.

// ModerationRemoval is what the moderation record says about one removal.
type ModerationRemoval struct {
	// By is the subject id of whoever removed the message.
	By string
	// At is when it was removed.
	At time.Time
	// Reason is the reason the remover gave, as the queue shows it.
	Reason string
}

// ModerationRemovalsFromItems picks, from a read of the moderation queue, the
// current removal of each post: the latest one that has not been restored.
func ModerationRemovalsFromItems(items []chat.ModerationItem) map[string]ModerationRemoval {
	out := map[string]ModerationRemoval{}
	for _, item := range items {
		if item.Kind != "removal" || item.PostID == "" || !item.RestoredAt.IsZero() || item.RestoredBy != "" {
			continue
		}
		if held, ok := out[item.PostID]; ok && held.At.After(item.At) {
			continue
		}
		by := item.RemovedBy
		if by == "" && item.Decision == "remove" {
			by = item.DecidedBy
		}
		out[item.PostID] = ModerationRemoval{By: by, At: item.At, Reason: item.Reason}
	}
	return out
}

var chatux048RemovedCopy = map[string][3]string{
	"removed_when": {"Removed by an administrator · {when}", "Von einem Administrator entfernt · {when}", "أزالها مسؤول · {when}"},
}

func chatux048RemovedText(locale, key string) string {
	row, ok := chatux048RemovedCopy[key]
	if !ok {
		return ""
	}
	return chatbug039Text(key, row[chatbug039LocaleIndex(locale)], row[0])
}

// chatux048MemberName is the name of a member of the open conversation, "" when
// the page does not hold it.
func chatux048MemberName(m Model, id string) string {
	if id == "" {
		return ""
	}
	for _, member := range m.Members {
		if member.ID == id && strings.TrimSpace(member.Name) != "" {
			return member.Name
		}
	}
	return ""
}

// chatux048RemovedLines is what the notice says: its first line (who and when,
// as far as this reader may know) and the reason line, "" when there is none.
func chatux048RemovedLines(m Model, msg Message, byAdministrator bool) (title, why string) {
	if !byAdministrator {
		return chatremoveText(m.Locale, "deleted"), ""
	}
	title = chatremoveText(m.Locale, "removed")
	var at time.Time
	var reason, by string
	own := false
	if cid := m.SelectedID; cid != "" && m.Moderation.CanRestoreIn(cid) {
		if record, ok := m.Moderation.Removals[msg.ID]; ok {
			at, reason, by = record.At, record.Reason, record.By
		}
	}
	if reason == "" && by == "" {
		// The author's own notice: the reason and when, never who.
		if notice, ok := m.Moderation.Notices[msg.ID]; ok && msg.AuthorID != "" && msg.AuthorID == m.CurrentUser {
			at, reason, own = notice.At, notice.Reason, true
		}
	}
	when := moderationWhen(m.Locale, at, time.Local, time.Now())
	name := chatux048MemberName(m, by)
	switch {
	case !own && name != "":
		title = strings.TrimSpace(moderationFill(chatremoveText(m.Locale, "removal_line"), "{name}", name, "{when}", when))
		if when == "" {
			title = strings.TrimSpace(strings.TrimSuffix(title, "·"))
		}
	case when != "":
		title = moderationFill(chatux048RemovedText(m.Locale, "removed_when"), "{when}", when)
	}
	if reason != "" {
		why = strings.ReplaceAll(chatremoveText(m.Locale, "own_reason"), "{reason}", chatremoveReason(m.Locale, reason))
	}
	return title, why
}

// chatux048Tombstone draws a removed (or author-deleted) message. It replaces
// the older notice that sat between the avatar and text columns: the row is the
// message grid, and everything it says is in the text column.
func chatux048Tombstone(m Model, msg Message, byAdministrator bool) ui.Node {
	title, why := chatux048RemovedLines(m, msg, byAdministrator)
	body := []ui.Node{html.P(html.Props{Class: "message-body chatux048-removed-title", Dir: "auto", Text: title})}
	if why != "" {
		body = append(body, html.P(html.Props{Class: "chatremove-own-reason chatux048-removed-why", Dir: "auto", Text: why}))
	}
	if msg.Replies > 0 {
		body = append(body, html.Button(html.Props{Type: "button", Class: "thread-link", Disabled: m.Callbacks.OpenThread == nil, Data: map[string]string{"action": "thread", "id": msg.ID}, Text: m.t(KeyThread)}))
	}
	if byAdministrator {
		if actions := chatmodTombstoneActions(m, msg); len(actions) > 0 {
			body = append(body, html.Div(html.Props{Class: "chatremove-actions chatux048-removed-actions"}, actions...))
		}
	}
	return html.Div(html.Props{Class: "message-row chatremove-tombstone chatux048-removed", Data: map[string]string{"message-id": msg.ID}},
		html.Div(html.Props{Class: "chatux048-removed-body"}, body...))
}
