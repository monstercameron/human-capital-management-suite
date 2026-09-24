//go:build js && wasm

package main

import (
	"context"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// This file is the second half of the reader's own actions: browsing and
// joining public channels, undoing a pin or a reaction, asking for older
// history, and persisting composer drafts.
//
// Discovery and self-join both go through the server's own policy now.
// ListConversations{include_discoverable: true} returns the channels the
// caller may see as well as the ones they are in, each row saying which it is
// through Conversation.joined, and AddMembership admits the self-join. The
// client reads that flag rather than subtracting the rail from the listing,
// because the difference would also have to know the discovery policy.
//
// Drafts persist through the sidebar layout blob, which is the only
// per-principal free-form store chat has (SidebarState.layout_json). It is
// per principal rather than per conversation and shares the sidebar's single
// revision, so the write is debounced and single-flighted with the sidebar's
// own writer.

const (
	// chatDraftPersistDebounce is how long the client waits after the last
	// keystroke before it writes drafts. Typing is local; persistence is a
	// bounded, coalesced write so a reload restores what was being typed.
	chatDraftPersistDebounce = 1500 * time.Millisecond
)

// withChatDiscoveryCallbacks adds the actions this file owns.
func withChatDiscoveryCallbacks(callbacks chatui.Callbacks, cfg journeyclient.Config) chatui.Callbacks {
	callbacks.OpenBrowse = func() {
		chatBrowser.mutate(func(model *chatui.Model) { model.ShowBrowse = true })
		refreshChatRoute()
		go loadChatBrowse(cfg)
	}
	callbacks.CloseBrowse = func() {
		chatBrowser.mutate(func(model *chatui.Model) {
			model.ShowBrowse, model.Browse, model.BrowseQuery = false, nil, ""
		})
		refreshChatRoute()
	}
	callbacks.OpenSearchChannel = func(id string) {
		model := chatBrowser.snapshot()
		name := ""
		for _, channel := range model.SearchChannels {
			if channel.ID == id {
				name = channel.Name
				break
			}
		}
		if id == "" || name == "" {
			return
		}
		for _, joined := range model.Conversations {
			if joined.ID == id && joined.Joined {
				chatBrowser.mutate(func(current *chatui.Model) {
					current.Search, current.SearchLoading, current.SearchError = "", false, ""
					current.SearchChannels, current.SearchPeople, current.SearchMessages = nil, nil, nil
					current.SearchHasMore, current.SearchNextCursor = false, ""
				})
				openChatConversation(cfg, id)
				return
			}
		}
		chatBrowser.mutate(func(current *chatui.Model) {
			current.Search, current.SearchLoading, current.SearchError = "", false, ""
			current.SearchChannels, current.SearchPeople, current.SearchMessages = nil, nil, nil
			current.SearchHasMore, current.SearchNextCursor = false, ""
			current.ShowBrowse, current.BrowseQuery = true, name
		})
		refreshChatRoute()
		go loadChatBrowse(cfg, id)
	}
	callbacks.FilterBrowse = func(query string) {
		// Local only: the listing is already in hand, so typing costs nothing.
		chatBrowser.filterChatBrowseQuery(query)
		refreshChatRoute()
	}
	callbacks.RequestJoinConversation = func(id string) {
		current := chatBrowser.snapshot()
		if current.JoinPromptSeen[id] {
			go joinChatConversation(cfg, id)
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			dismissed := chatChannelJoinPromptDismissed(ctx, chatBrowser.config(cfg), id)
			if dismissed {
				chatBrowser.mutate(func(model *chatui.Model) {
					if model.JoinPromptSeen == nil {
						model.JoinPromptSeen = make(map[string]bool)
					}
					model.JoinPromptSeen[id] = true
				})
				go joinChatConversation(cfg, id)
				return
			}
			chatBrowser.mutate(func(model *chatui.Model) { requestChatJoinPrompt(model, id) })
			refreshChatRoute()
		}()
	}
	callbacks.JoinConversation = func(id string) { go joinChatConversation(cfg, id) }
	callbacks.DismissJoinPrompt = func() {
		updated := chatBrowser.mutate(func(model *chatui.Model) {
			if id := model.JoinPromptID; id != "" {
				if model.JoinPromptSeen == nil {
					model.JoinPromptSeen = make(map[string]bool)
				}
				model.JoinPromptSeen[id] = true
			}
			model.JoinPromptID, model.JoinPromptPending = "", false
		})
		persistChatRecipientSidebar(cfg, updated)
		refreshChatRoute()
	}
	callbacks.Unpin = func(postID string) { go unpinChatPost(cfg, postID) }
	callbacks.RemoveReaction = func(postID string) { go removeChatReaction(cfg, postID, chatDefaultReaction) }
	callbacks.RemoveReactionWith = func(postID, emoji string) { go removeChatReaction(cfg, postID, emoji) }
	callbacks.LoadOlder = func() { loadOlderChatMessages(cfg) }
	callbacks.LoadNewer = func() { loadNewerChatMessages(cfg) }
	callbacks.VisibleMessageIDs = func(ids []string) { syncVisibleChatMedia(cfg, ids) }
	callbacks.LoadOlderThread = func() { loadChatThreadPage(cfg, chatThreadOlder) }
	callbacks.LoadNewerThread = func() { loadChatThreadPage(cfg, chatThreadNewer) }
	return callbacks
}

// loadChatBrowse fills the browse list with the joinable public channels the
// server will admit to.
func loadChatBrowse(cfg journeyclient.Config, targetIDs ...string) {
	client := chatBrowser.conversationClient()
	if client == nil {
		return
	}
	active := chatBrowser.config(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := client.ListConversations(chatRPCContext(ctx, active), &chatv1.ListConversationsRequest{
		TenantId: active.Tenant, PageSize: 100, IncludeDiscoverable: true,
	})
	if chatActionFailed("list channels you can join", err) {
		return
	}
	browse := joinableChatConversations(result.GetConversations())
	if len(targetIDs) > 0 && targetIDs[0] != "" {
		targetID := targetIDs[0]
		found := false
		for _, conversation := range result.GetConversations() {
			if conversation == nil || conversation.GetId() != targetID || !conversation.GetJoined() {
				continue
			}
			room := chatConversation(conversation)
			chatBrowser.mutate(func(model *chatui.Model) {
				model.ShowBrowse, model.Browse, model.BrowseQuery = false, nil, ""
				model.Search, model.SearchLoading, model.SearchError = "", false, ""
				model.SearchChannels, model.SearchPeople, model.SearchMessages = nil, nil, nil
				model.SearchHasMore, model.SearchNextCursor = false, ""
				model.SearchHasMoreChannels, model.SearchChannelNextCursor = false, ""
				model.Conversations = append(model.Conversations, room)
			})
			openChatConversation(active, targetID)
			return
		}
		for _, channel := range browse {
			if channel.ID == targetID {
				found = true
				break
			}
		}
		if !found {
			conversation, getErr := client.GetConversation(chatRPCContext(ctx, active), &chatv1.GetConversationRequest{TenantId: active.Tenant, ConversationId: targetID})
			if getErr != nil || conversation == nil || conversation.GetConversation() == nil || conversation.GetConversation().GetId() != targetID || conversation.GetConversation().GetTenantId() != active.Tenant || conversation.GetConversation().GetKind() != chatv1.ConversationKind_CONVERSATION_KIND_PUBLIC_CHANNEL {
				if getErr != nil {
					chatActionFailed("open this channel in browse", getErr)
				} else {
					noteChatAction(productui.ResolveProductLocale(active.Locale).Text(chatui.KeySearchChannelUnavailable))
				}
				return
			}
			room := chatConversation(conversation.GetConversation())
			room.Joined = false
			browse = append(browse, room)
		}
	}
	// A browse row named after a subject id resolves through the directory,
	// and ONLY the directory: the humanizing fallback is for people, and run
	// over a channel slug it turned "people-ops" into "People Ops" here while
	// the rail still said "#people-ops" -- one room with two names.
	directory := chatDirectorySnapshot()
	for i := range browse {
		if name, ok := directory[browse[i].Name]; ok && name != "" {
			browse[i].Name = name
		}
	}
	chatBrowser.setChatBrowse(browse)
	// No toast when the list is empty: the dialog says "No channels to join
	// yet." itself, and a toast repeating the panel the reader is looking at
	// is noise over the top of its own answer.
	refreshChatRoute()
}

func requestChatJoinPrompt(model *chatui.Model, id string) {
	if model == nil || id == "" {
		return
	}
	var channel *chatui.Conversation
	for i := range model.Browse {
		if model.Browse[i].ID == id {
			channel = &model.Browse[i]
			break
		}
	}
	if channel == nil && model.PreviewConversation != nil && model.PreviewConversation.ID == id {
		channel = model.PreviewConversation
	}
	if channel == nil {
		for i := range model.Conversations {
			if model.Conversations[i].ID == id {
				channel = &model.Conversations[i]
				break
			}
		}
	}
	if channel == nil || channel.Kind != chatui.PublicChannel || channel.Joined {
		return
	}
	copy := *channel
	model.PreviewConversation = &copy
	if model.JoinPromptSeen == nil || !model.JoinPromptSeen[id] {
		model.JoinPromptID = id
	}
}

// joinChatConversation adds the reader to a public channel and selects it.
func joinChatConversation(cfg journeyclient.Config, id string) {
	client := chatBrowser.conversationClient()
	if client == nil || id == "" {
		return
	}
	active := chatBrowser.config(cfg)
	chatBrowser.mutate(func(model *chatui.Model) {
		if model.JoinPromptID == id {
			model.JoinPromptPending = true
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// HomeTenantID is the only tenant field the wire Membership carries; the
	// transport maps it onto both the conversation tenant and the member's
	// home tenant (internal/transport/chat/chat.go:626). History visibility
	// is explicit because the unspecified enum reads as no history at all.
	_, err := client.AddMembership(chatRPCContext(ctx, active), &chatv1.AddMembershipRequest{
		Membership: &chatv1.Membership{
			ConversationId:    id,
			HomeTenantId:      active.Tenant,
			SubjectId:         active.Subject,
			Role:              chatv1.MembershipRole_MEMBERSHIP_ROLE_MEMBER,
			HistoryVisibility: chatv1.ReadHistoryFrom_READ_HISTORY_FROM_FULL,
		},
	})
	if chatActionFailed("join this channel", err) {
		chatBrowser.mutate(func(model *chatui.Model) { model.JoinPromptPending = false })
		refreshChatRoute()
		return
	}
	// The joined room goes into the rail and opens, the same way a created one
	// does, so the reader lands in what they just joined rather than wherever
	// they were.
	joined := chatui.Conversation{ID: id, Kind: chatui.PublicChannel, Joined: true}
	activity := chatBrowser.activitySnapshot()
	chatBrowser.mutate(func(model *chatui.Model) {
		for _, row := range model.Browse {
			if row.ID == id {
				joined = row
				break
			}
		}
		if model.PreviewConversation != nil && model.PreviewConversation.ID == id {
			joined = *model.PreviewConversation
		}
		model.ShowBrowse, model.Browse, model.BrowseQuery = false, nil, ""
		if model.JoinPromptSeen == nil {
			model.JoinPromptSeen = make(map[string]bool)
		}
		model.JoinPromptSeen[id] = true
		if model.JoinPromptID == id {
			model.JoinPromptID, model.JoinPromptPending = "", false
		}
		if model.PreviewConversation != nil && model.PreviewConversation.ID == id {
			model.PreviewConversation = nil
		}
		joined.Joined = true
		for i := range model.Conversations {
			if model.Conversations[i].ID == id {
				return
			}
		}
		model.Conversations = append(model.Conversations, joined)
		sortChatRail(model.Conversations, activity)
	})
	chatActionSucceeded("Joined the channel")
	invalidateChatRecipientProjection()
	openChatConversation(active, id)
}

// unpinChatPost removes a pin. UnpinPost refuses a zero expected_revision and
// only ListPins publishes that revision, so the client uses the one it kept
// and re-reads the pins when it has none.
func unpinChatPost(cfg journeyclient.Config, postID string) {
	client := chatBrowser.conversationClient()
	if client == nil || postID == "" {
		return
	}
	active := chatBrowser.config(cfg)
	conversation := chatBrowser.selectedID()
	revision := chatBrowser.pinRevision(postID)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	callCtx := chatRPCContext(ctx, active)
	if revision == 0 {
		pins, err := client.ListPins(callCtx, &chatv1.ListPinsRequest{TenantId: active.Tenant, ConversationId: conversation})
		if chatActionFailed("read the pinned messages", err) {
			return
		}
		for _, pin := range pins.GetPins() {
			if pin != nil && pin.GetPostId() == postID {
				revision = pin.GetRevision()
				break
			}
		}
	}
	if revision == 0 {
		chatActionSucceeded("That message is not pinned any more")
		chatBrowser.mutate(func(model *chatui.Model) { setChatPinned(model, postID, false) })
		refreshChatRoute()
		return
	}
	_, err := client.UnpinPost(callCtx, &chatv1.UnpinPostRequest{
		TenantId: active.Tenant, ConversationId: conversation, PostId: postID, ExpectedRevision: revision,
	})
	if chatActionFailed("unpin this message", err) {
		return
	}
	chatBrowser.clearPinRevision(postID)
	chatBrowser.mutate(func(model *chatui.Model) { setChatPinned(model, postID, false) })
	chatActionSucceeded("Message unpinned")
	refreshChatRoute()
}

// removeChatReaction takes the reader's own reaction off a post.
//
// AddReaction is an idempotent insert, not a toggle
// (internal/data/chatstore/contracts_adapter.go:792, ON CONFLICT DO NOTHING),
// so adding and removing are two calls rather than one, and this is the second.
func removeChatReaction(cfg journeyclient.Config, postID, emoji string) {
	client := chatBrowser.conversationClient()
	if client == nil || postID == "" || emoji == "" {
		return
	}
	active := chatBrowser.config(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, err := client.RemoveReaction(chatRPCContext(ctx, active), &chatv1.RemoveReactionRequest{
		TenantId: active.Tenant, ConversationId: chatBrowser.selectedID(), PostId: postID, Emoji: emoji,
	})
	if chatActionFailed("remove your reaction", err) {
		return
	}
	chatBrowser.adjustReaction(postID, emoji, -1, true)
	chatActionSucceeded("Reaction removed")
	refreshChatRoute()
}

// chatDraftPersist debounces the drafts write so typing costs nothing and a
// reload still restores the composer. A send flushes it rather than leaving a
// write of the sent text pending.
var chatDraftPersist *chatDraftWriter

// persistChatDrafts writes the drafts through the sidebar blob, which is the
// one store that carries them (persistChatRecipientSidebar fills
// layout.Drafts from chatState and single-flights the write against the
// sidebar's own revision).
func persistChatDrafts(cfg journeyclient.Config) {
	persistChatRecipientSidebar(cfg, chatBrowser.snapshot(), true)
}

func chatDraftWriterFor(cfg journeyclient.Config) *chatDraftWriter {
	if chatDraftPersist == nil {
		chatDraftPersist = newChatDraftWriter(browserDebounceScheduler, chatDraftPersistDebounce, func() {
			persistChatDrafts(chatBrowser.config(cfg))
		})
	}
	return chatDraftPersist
}

// scheduleChatDraftPersist joins the pending drafts write.
func scheduleChatDraftPersist(cfg journeyclient.Config) { chatDraftWriterFor(cfg).Schedule() }

// flushChatDraftPersist cancels the pending write and persists now.
func flushChatDraftPersist(cfg journeyclient.Config) { chatDraftWriterFor(cfg).Flush() }
