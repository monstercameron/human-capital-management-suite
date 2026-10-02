package main

import (
	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chatmod004Removed is true for a post an administrator removed: the server
// keeps it in the conversation with its text replaced, so readers see that
// something was removed and the thread under it stays. A post its author
// deleted is not that, and leaves the timeline as before.
func chatmod004Removed(post *chatv1.Post) bool {
	return post != nil && post.GetDeleted() && post.GetBody() == chat.RemovedByAdministrator
}

// chatmod004Gone is true for a deleted post that leaves the timeline.
func chatmod004Gone(post *chatv1.Post) bool {
	return post != nil && post.GetDeleted() && !chatmod004Removed(post)
}

// chatmod004Apply puts what the server now says about a removed post on every
// copy the page holds: the text, with reactions, files, previews and the pin
// hidden. Replies are kept, so the count is untouched.
func chatmod004Apply(model *chatui.Model, post *chatv1.Post) {
	removed := func(m chatui.Message) chatui.Message {
		m.Body, m.Revision = chat.RemovedByAdministrator, post.GetRevision()
		m.Attachments, m.Chips, m.PersonaReferences = nil, nil, nil
		m.Reactions, m.Reacted, m.Pinned = 0, false, false
		return m
	}
	id := post.GetId()
	for i := range model.Messages {
		if model.Messages[i].ID == id {
			model.Messages[i] = removed(model.Messages[i])
		}
	}
	for i := range model.ThreadMessages {
		if model.ThreadMessages[i].ID == id {
			model.ThreadMessages[i] = removed(model.ThreadMessages[i])
		}
	}
	if model.ThreadParent != nil && model.ThreadParent.ID == id {
		parent := removed(*model.ThreadParent)
		model.ThreadParent = &parent
	}
	// A message removed again is no longer "restored".
	model.Moderation = model.Moderation.WithoutRestored(id)
}

// chatmod004NoteRestore marks a message as restored when an edit brings back
// one the page holds as removed by an administrator: the server sends a restore
// as an ordinary edit, and readers are owed a line saying what happened. It
// runs before the edit is applied, while the removed copy is still held.
func chatmod004NoteRestore(model *chatui.Model, post *chatv1.Post) {
	if post == nil || post.GetDeleted() || post.GetBody() == chat.RemovedByAdministrator {
		return
	}
	id := post.GetId()
	held := func(m chatui.Message) bool { return m.ID == id && m.Body == chat.RemovedByAdministrator }
	found := model.ThreadParent != nil && held(*model.ThreadParent)
	for _, m := range model.Messages {
		found = found || held(m)
	}
	for _, m := range model.ThreadMessages {
		found = found || held(m)
	}
	if found {
		model.Moderation = model.Moderation.WithRestored(id)
	}
}
