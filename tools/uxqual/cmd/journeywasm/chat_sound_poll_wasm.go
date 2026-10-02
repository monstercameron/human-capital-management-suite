//go:build js && wasm

package main

import (
	"context"
	"sync"
	"syscall/js"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

const (
	chatSoundPollInterval     = 20 * time.Second
	chatSoundPollPageSize     = 50
	chatSoundPollFanout       = 4
	chatSoundPollRoomsPerTick = 24
	chatSoundPollBudget       = 8 * time.Second
)

// startChatSoundPolling checks inactive conversations while the Chat page is
// mounted. Each room is baselined silently, then only posts newer than its
// stored sequence are considered. A bounded round-robin batch, worker pool and
// per-cycle deadline keep polling work and retained goroutines finite.
func startChatSoundPolling() func() {
	ctx, cancel := context.WithCancel(context.Background())
	go chatSoundPollLoop(ctx)
	return cancel
}

func chatSoundPollLoop(ctx context.Context) {
	ticker := time.NewTicker(chatSoundPollInterval)
	defer ticker.Stop()
	for {
		pollChatSoundConversations(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func pollChatSoundConversations(ctx context.Context) {
	model := chatBrowser.snapshot()
	cfg := chatBrowser.config(journeyclient.Config{})
	if cfg.Tenant == "" || cfg.Subject == "" || cfg.Bearer == "" || len(model.Conversations) == 0 {
		return
	}
	client := chatBrowser.conversationClient()
	if client == nil {
		return
	}
	batch, nextCursor := chatSoundPollBatch(model.Conversations, chatSoundPollCursor(), chatSoundPollRoomsPerTick)
	setChatSoundPollCursor(nextCursor)
	pollCtx, cancel := context.WithTimeout(ctx, chatSoundPollBudget)
	defer cancel()
	// CHATBUG-014: one read of the list says which conversations have a newer
	// message than the one last read; the others are not read (chatperf_sound.go).
	newest, listed := chatperfSoundListing(pollCtx, client, cfg)
	jobs := make(chan chatui.Conversation, chatSoundPollFanout)
	var wg sync.WaitGroup
	for range chatSoundPollFanout {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for conversation := range jobs {
				if pollCtx.Err() != nil {
					return
				}
				roomCtx, roomCancel := context.WithTimeout(pollCtx, 15*time.Second)
				read := pollChatSoundConversation(roomCtx, client, cfg, conversation, model.SelectedID)
				roomCancel()
				if at, known := newest[conversation.ID]; read && listed && known {
					chatperfSoundSeen.settled(conversation.ID, at)
				}
			}
		}()
	}
	for _, conversation := range batch {
		at, known := newest[conversation.ID]
		if chatperfSoundSkip(&chatperfSoundSeen, conversation.ID, model.SelectedID, at, listed && known, chatSoundHasBaseline(conversation.ID)) {
			continue
		}
		select {
		case jobs <- conversation:
		case <-pollCtx.Done():
			close(jobs)
			wg.Wait()
			return
		}
	}
	close(jobs)
	wg.Wait()
}

func chatSoundPollCursor() int {
	defer func() { _ = recover() }()
	state, _, ok := chatSoundDocumentState()
	if !ok {
		return 0
	}
	cursor := state.Get("pollCursor")
	if cursor.Type() != js.TypeNumber {
		return 0
	}
	return cursor.Int()
}

func setChatSoundPollCursor(cursor int) {
	defer func() { _ = recover() }()
	state, _, ok := chatSoundDocumentState()
	if ok {
		state.Set("pollCursor", cursor)
	}
}

// pollChatSoundConversation reads one conversation for new messages. It reports
// whether the read got its answer; a conversation whose read did not is read
// again on the next tick whatever the list says.
func pollChatSoundConversation(ctx context.Context, client chatv1.ConversationServiceClient, cfg journeyclient.Config, conversation chatui.Conversation, selectedID string) bool {
	host := conversation.HostTenantID
	if host == "" {
		host = cfg.Tenant
	}
	model := chatBrowser.snapshot()
	if _, ok := model.Preferences.Notifications[conversation.ID]; !ok {
		preferences, err := client.GetPreferences(chatRPCContext(ctx, cfg), &chatv1.GetPreferencesRequest{TenantId: host, ConversationId: conversation.ID})
		if err != nil || preferences.GetPreferences() == nil || !chatSoundPollConfigActive(cfg) {
			return false
		}
		mode := chatui.NotifyAll
		if preferences.GetPreferences().GetMuted() {
			mode = chatui.NotifyMute
		} else if preferences.GetPreferences().GetMentionsOnly() {
			mode = chatui.NotifyMention
		}
		setChatSoundNotificationMode(conversation.ID, mode)
		model = chatBrowser.snapshot()
	}
	if conversation.ID == selectedID {
		if !model.HasNewer {
			chatSoundSetBaseline(conversation.ID, chatSoundLatestLoadedSequence(model))
		}
		return true
	}
	if !chatSoundHasBaseline(conversation.ID) {
		result, err := client.ListPosts(chatRPCContext(ctx, cfg), &chatv1.ListPostsRequest{
			TenantId: host, ConversationId: conversation.ID, PageSize: 1, Descending: true,
		})
		if err != nil || !chatSoundPollConfigActive(cfg) {
			return false
		}
		chatSoundSetBaseline(conversation.ID, chatSoundLatestPostSequence(result.GetPosts()))
		return true
	}

	after := chatSoundCurrentSequence(conversation.ID)
	result, err := client.ListPosts(chatRPCContext(ctx, cfg), &chatv1.ListPostsRequest{
		TenantId: host, ConversationId: conversation.ID, AfterSequence: after, PageSize: chatSoundPollPageSize,
	})
	if err != nil || !chatSoundPollConfigActive(cfg) {
		return false
	}
	for _, post := range chatSoundEventsAfter(result.GetPosts(), after) {
		if !claimChatSoundSequence(conversation.ID, post.GetSequence()) {
			continue
		}
		current := chatBrowser.snapshot()
		if shouldPlayChatSound(current, conversation.ID, post, time.Now(), chatSystemReducedInterruption(), chatAppReducedInterruption()) {
			playChatSoundIfAllowed(current, conversation.ID, post, time.Now())
		}
	}
	if latest := chatSoundLatestPostSequence(result.GetPosts()); latest > after {
		noteChatSoundSequence(conversation.ID, latest)
	}
	// A full page may not be the end of what is new: read again next tick.
	return len(result.GetPosts()) < chatSoundPollPageSize
}

// chatperfSoundSeen is what the sound poll has read to the end.
var chatperfSoundSeen chatperfSoundActivity

// chatperfSoundListing reads when each of the reader's conversations last had a
// message. listed is false when the list could not say for all of them (the
// read failed, or there are more conversations than one page), in which case
// every conversation is read as before.
func chatperfSoundListing(ctx context.Context, client chatv1.ConversationServiceClient, cfg journeyclient.Config) (newest map[string]time.Time, listed bool) {
	list, err := client.ListConversations(chatRPCContext(ctx, cfg), &chatv1.ListConversationsRequest{TenantId: cfg.Tenant, PageSize: 100})
	if err != nil || list.GetNextCursor() != "" || !chatSoundPollConfigActive(cfg) {
		return nil, false
	}
	newest = make(map[string]time.Time, len(list.GetConversations()))
	for _, conversation := range list.GetConversations() {
		if conversation == nil || conversation.GetId() == "" {
			continue
		}
		var at time.Time
		if stamp := conversation.GetLastActivityAt(); stamp != nil {
			at = stamp.AsTime()
		}
		newest[conversation.GetId()] = at
	}
	return newest, true
}

func chatSoundPollConfigActive(cfg journeyclient.Config) bool {
	current := chatBrowser.config(cfg)
	return current.Tenant == cfg.Tenant && current.Subject == cfg.Subject && current.Bearer == cfg.Bearer
}

func setChatSoundNotificationMode(conversationID string, mode chatui.NotificationMode) {
	chatBrowser.mutate(func(model *chatui.Model) {
		if model.Preferences.Notifications == nil {
			model.Preferences.Notifications = map[string]chatui.NotificationMode{}
		}
		model.Preferences.Notifications[conversationID] = mode
		for i := range model.Conversations {
			if model.Conversations[i].ID == conversationID {
				model.Conversations[i].Muted = mode == chatui.NotifyMute
			}
		}
		for i := range model.Sections {
			for j := range model.Sections[i].Chats {
				if model.Sections[i].Chats[j].ID == conversationID {
					model.Sections[i].Chats[j].Muted = mode == chatui.NotifyMute
				}
			}
		}
	})
}
