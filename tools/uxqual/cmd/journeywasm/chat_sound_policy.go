package main

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

const chatSoundFreshness = 90 * time.Second

// shouldPlayChatSound applies the recipient's chat preferences to a newly
// applied post event. Sound is limited to DMs and explicit textual mentions.
func shouldPlayChatSound(model chatui.Model, conversationID string, post *chatv1.Post, now time.Time, systemReduced, appReduced bool) bool {
	if post == nil || post.GetId() == "" || post.GetSequence() == 0 || post.GetDeleted() || post.GetAuthorId() == "" || post.GetAuthorId() == model.CurrentUser {
		return false
	}
	created := post.GetCreatedAt()
	if created == nil {
		return false
	}
	createdAt := created.AsTime()
	if createdAt.After(now.Add(5*time.Second)) || now.Sub(createdAt) > chatSoundFreshness {
		return false
	}
	if systemReduced || appReduced || chatQuietHoursActive(model.Preferences, now) {
		return false
	}
	var conversation *chatui.Conversation
	for i := range model.Conversations {
		if model.Conversations[i].ID == conversationID {
			conversation = &model.Conversations[i]
			break
		}
	}
	if conversation == nil || conversation.Muted {
		return false
	}
	mode, preferenceLoaded := model.Preferences.Notifications[conversationID]
	if !preferenceLoaded {
		return false
	}
	if mode == chatui.NotifyMute {
		return false
	}
	mentioned := chatPostMentionsViewer(post.GetBody(), model.CurrentUser, model.CurrentUserName)
	if mode == chatui.NotifyMention {
		return mentioned
	}
	return conversation.Kind == chatui.DirectMessage || mentioned
}

func chatQuietHoursActive(prefs chatui.Preferences, now time.Time) bool {
	if !prefs.QuietHours {
		return false
	}
	if prefs.QuietStartMinute < 0 || prefs.QuietStartMinute >= 24*60 || prefs.QuietEndMinute < 0 || prefs.QuietEndMinute >= 24*60 {
		return true
	}
	location, err := time.LoadLocation(strings.TrimSpace(prefs.QuietTimezone))
	if err != nil {
		return true
	}
	local := now.In(location)
	minute := local.Hour()*60 + local.Minute()
	start, end := prefs.QuietStartMinute, prefs.QuietEndMinute
	if start == end {
		return false
	}
	if start < end {
		return minute >= start && minute < end
	}
	return minute >= start || minute < end
}

func chatPostMentionsViewer(body, subjectID, displayName string) bool {
	return chatBodyHasMention(body, subjectID) || chatBodyHasMention(body, displayName)
}

func chatPostNewerThanLoadedTimeline(model chatui.Model, post *chatv1.Post) bool {
	if post == nil || post.GetSequence() == 0 || model.HasNewer {
		return false
	}
	return post.GetSequence() > chatSoundLatestLoadedSequence(model)
}

func chatSoundLatestLoadedSequence(model chatui.Model) uint64 {
	latest := uint64(0)
	for _, message := range model.Messages {
		if message.Sequence > latest {
			latest = message.Sequence
		}
	}
	for _, message := range model.ThreadMessages {
		if message.Sequence > latest {
			latest = message.Sequence
		}
	}
	if model.ThreadParent != nil && model.ThreadParent.Sequence > latest {
		latest = model.ThreadParent.Sequence
	}
	return latest
}

func chatSoundEventsAfter(posts []*chatv1.Post, after uint64) []*chatv1.Post {
	newPosts := make([]*chatv1.Post, 0, len(posts))
	for _, post := range posts {
		if post != nil && post.GetSequence() > after {
			newPosts = append(newPosts, post)
		}
	}
	return chatInConversationOrder(newPosts)
}

func chatSoundLatestPostSequence(posts []*chatv1.Post) uint64 {
	var latest uint64
	for _, post := range posts {
		if post != nil && post.GetSequence() > latest {
			latest = post.GetSequence()
		}
	}
	return latest
}

// chatSoundPollBatch selects a bounded round-robin slice of rooms. The cursor
// advances across the full input so an empty or slow room cannot starve later
// conversations on subsequent polling cycles.
func chatSoundPollBatch(conversations []chatui.Conversation, cursor, limit int) ([]chatui.Conversation, int) {
	if len(conversations) == 0 || limit <= 0 {
		return nil, 0
	}
	cursor %= len(conversations)
	if cursor < 0 {
		cursor += len(conversations)
	}
	batch := make([]chatui.Conversation, 0, min(limit, len(conversations)))
	seen := make(map[string]bool, min(limit, len(conversations)))
	directBudget := limit * 2 / 3
	appendKind := func(direct bool, capacity int) {
		added := 0
		for scanned := 0; scanned < len(conversations) && len(batch) < limit && added < capacity; scanned++ {
			conversation := conversations[(cursor+scanned)%len(conversations)]
			if conversation.ID == "" || seen[conversation.ID] || (conversation.Kind == chatui.DirectMessage) != direct {
				continue
			}
			seen[conversation.ID] = true
			batch = append(batch, conversation)
			added++
		}
	}
	appendKind(true, directBudget)
	appendKind(false, limit-directBudget)
	// Let either class use spare slots when the other has fewer rooms.
	appendKind(true, limit)
	appendKind(false, limit)
	advance, counted := 0, 0
	for scanned := 0; scanned < len(conversations) && counted < limit; scanned++ {
		advance = scanned + 1
		if conversations[(cursor+scanned)%len(conversations)].ID != "" {
			counted++
		}
	}
	return batch, (cursor + advance) % len(conversations)
}

func chatBodyHasMention(body, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	needle := "@" + target
	lowerBody, lowerNeedle := strings.ToLower(body), strings.ToLower(needle)
	for offset := 0; offset < len(lowerBody); {
		relative := strings.Index(lowerBody[offset:], lowerNeedle)
		if relative < 0 {
			return false
		}
		start := offset + relative
		end := start + len(lowerNeedle)
		startBoundary := start == 0
		if start > 0 {
			previous, _ := utf8.DecodeLastRuneInString(lowerBody[:start])
			startBoundary = !unicode.IsLetter(previous) && !unicode.IsDigit(previous) && previous != '_'
		}
		if startBoundary && end == len(lowerBody) {
			return true
		}
		r, _ := utf8.DecodeRuneInString(lowerBody[end:])
		if startBoundary && !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return true
		}
		offset = end
	}
	return false
}
