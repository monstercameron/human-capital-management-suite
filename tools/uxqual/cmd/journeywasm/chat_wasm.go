//go:build js && wasm

package main

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"strings"
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// This file is the chat surface's RPC half: it turns the reader's actions into
// calls on ConversationService and folds the answers back through chatState
// (chat_state.go), which owns every decision worth testing.
//
// Two rules run through all of it.
//
// A failed *action* is a notice, not a page. Pinning a post that the server
// refuses must not replace the timeline with "Conversation unavailable" and
// must not survive the next successful action; only a failed *load* -- the
// conversation list, or the selected conversation's posts -- may take the
// timeline, and the next successful load clears it.
//
// A keystroke is not an RPC. Composer and edit drafts are local state; they
// update chatState and stop there. The textarea already shows the character
// the reader typed, so there is nothing to re-render, and re-rendering a
// controlled input from a model one keystroke behind is what moved the caret.
//
// Requests carry no Principal. Every chat request message marks that field
// deprecated (schema/proto/hcmnext/chat/v1/chat.proto) because the server
// derives the acting principal from the bearer the tunnel admitted and
// refuses a caller-supplied one rather than ignoring it.

// chatCatchupPages bounds how far a gap recovery walks forward. A conversation
// that moved a long way while the stream was down is read by cursor rather than
// in one unbounded request, and ten pages is far enough that the bound is only
// ever a backstop against a cursor that does not advance.
//
// Opening a room does not use it: that is one backward page (see loadChatPosts).
const chatCatchupPages = 10

// chatReactionFanout caps concurrent per-post reaction reads. There is no
// bulk reaction-count RPC (see the gap report), so the count is a bounded
// fan-out over the newest visible posts.
const chatReactionFanout = 8

// chatWorkers reads the worker directory chat resolves names from. It is the
// same RPC the People page uses, called by chat for itself: a name must not
// depend on which pages this session happened to visit.
var chatWorkers journeyv1.JourneyServiceClient

func configureChatBrowser(conn grpc.ClientConnInterface, cfg journeyclient.Config) {
	clearChatMediaCache()
	resetChatPersonActions()
	client := chatv1.NewConversationServiceClient(conn)
	chatWorkers = journeyv1.NewJourneyServiceClient(conn)
	chatBrowser.reset(client, cfg, func() chatui.Callbacks { return chatCallbacks(cfg) })
	if origin := js.Global().Get("location").Get("origin"); origin.Truthy() {
		chatBrowser.mutate(func(m *chatui.Model) { m.EmbedOrigin = origin.String() })
	}
	configureChatRecipientBrowser(conn, cfg)
	configureChatDocuments(conn)
}

// loadChatDirectory reads the worker directory once per session.
func loadChatDirectory(cfg journeyclient.Config) {
	workers := chatWorkers
	if workers == nil {
		return
	}
	epoch, claimed := chatBrowser.claimDirectoryReadFor(cfg)
	if !claimed {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := workers.ListWorkers(chatRPCContext(ctx, cfg), &journeyv1.ListWorkersRequest{})
	if err != nil {
		// A directory failure is never a page failure. Chat is fully usable
		// without names -- every author falls back to a humanized reference --
		// so this sets no LoadError, raises no notice, and is retried on the
		// next open rather than leaving the session on identifiers for good.
		chatBrowser.releaseDirectoryReadAt(epoch, cfg)
		return
	}
	directory := chatDirectoryFromWorkers(result.GetWorkers())
	if len(directory) == 0 {
		chatBrowser.releaseDirectoryReadAt(epoch, cfg)
		return
	}
	people := chatSearchDirectoryFromWorkers(result.GetWorkers())
	if !chatBrowser.completeDirectoryRead(epoch, cfg, directory, chatPhotosFromWorkers(result.GetWorkers()), people) {
		return
	}
	refreshChatRoute()
	current := chatBrowser.snapshot()
	if strings.TrimSpace(current.Search) != "" && current.Callbacks.Search != nil {
		current.Callbacks.Search(current.Search)
	}
}

func chatRPCContext(ctx context.Context, cfg journeyclient.Config) context.Context {
	return metadata.AppendToOutgoingContext(ctx, journeyclient.AuthorizationHeader, journeyclient.BearerScheme+cfg.Bearer)
}

func chatSearchCopy(model *chatui.Model, key string) string {
	if model != nil && model.Text != nil {
		if value := model.Text(key); value != "" {
			return value
		}
	}
	return chatui.EnglishCopy()[key]
}

func chatSearchConversation(result *chatv1.ChannelSearchResult) chatui.Conversation {
	if result == nil {
		return chatui.Conversation{}
	}
	kind := chatui.ConversationKind(strings.ToLower(strings.TrimSpace(result.GetKind())))
	switch kind {
	case "public", "public_channel", "public-channel":
		kind = chatui.PublicChannel
	case "private", "private_channel", "private-channel":
		kind = chatui.PrivateChannel
	case "direct", "direct_message", "direct-message":
		kind = chatui.DirectMessage
	case "group", "group_chat", "group-chat":
		kind = chatui.GroupChat
	default:
		kind = chatui.ConversationKind(result.GetKind())
	}
	return chatui.Conversation{ID: result.GetConversationId(), Name: result.GetName(), Kind: kind, Joined: result.GetJoined()}
}

const chatSearchMessageWindow = 200

func loadMoreChatSearch(cfg journeyclient.Config) {
	model := chatBrowser.snapshot()
	query, cursor := strings.TrimSpace(model.Search), model.SearchNextCursor
	if query == "" || cursor == "" || model.SearchLoading || model.SearchLoadingMore {
		return
	}
	generation := chatBrowser.currentGeneration()
	chatBrowser.mutate(func(current *chatui.Model) { current.SearchLoadingMore, current.SearchMoreError = true, "" })
	refreshChatRoute()
	go func() {
		client := chatBrowser.conversationClient()
		if client == nil {
			chatBrowser.commit(generation, func(current *chatui.Model) {
				current.SearchLoadingMore, current.SearchMoreError = false, chatSearchCopy(current, chatui.KeySearchMoreError)
			})
			refreshChatRoute()
			return
		}
		active := chatBrowser.config(cfg)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		searchText, searchIn, searchFrom := chatui.SearchFilters(chatBrowser.snapshot(), query)
		result, err := client.Search(chatRPCContext(ctx, active), &chatv1.SearchRequest{TenantId: active.Tenant, Query: searchText, ConversationId: searchIn, AuthorId: searchFrom, Cursor: cursor, PageSize: 50})
		if !chatBrowser.generationActive(generation) {
			return
		}
		directory := chatDirectorySnapshot()
		now := time.Now()
		committed := chatBrowser.commit(generation, func(current *chatui.Model) {
			if current.Search != query {
				return
			}
			current.SearchLoadingMore = false
			if err != nil {
				current.SearchMoreError = chatSearchCopy(current, chatui.KeySearchMoreError)
				return
			}
			seen := make(map[string]bool, len(current.SearchMessages))
			for _, hit := range current.SearchMessages {
				seen[hit.ConversationID+"\x00"+hit.Message.ID] = true
			}
			for _, hit := range result.GetResults() {
				if hit == nil || hit.GetPost() == nil || len(current.SearchMessages) >= chatSearchMessageWindow {
					continue
				}
				post := hit.GetPost()
				key := post.GetConversationId() + "\x00" + post.GetId()
				if key == "\x00" || seen[key] {
					continue
				}
				seen[key] = true
				current.SearchMessages = append(current.SearchMessages, chatui.SearchMessage{
					ConversationID: post.GetConversationId(), ConversationName: hit.GetConversationName(),
					Message: chatMessage(post, active.Locale, directory, now),
				})
			}
			current.SearchNextCursor = result.GetNextCursor()
			current.SearchHasMore = result.GetNextCursor() != "" && len(current.SearchMessages) < chatSearchMessageWindow
			current.SearchMoreError = ""
		})
		if committed {
			refreshChatRoute()
		}
	}()
}

const chatSearchChannelWindow = 100

func loadMoreChatSearchChannels(cfg journeyclient.Config) {
	model := chatBrowser.snapshot()
	query, cursor := strings.TrimSpace(model.Search), model.SearchChannelNextCursor
	if query == "" || cursor == "" || model.SearchLoading || model.SearchLoadingMoreChannels {
		return
	}
	generation := chatBrowser.currentGeneration()
	chatBrowser.mutate(func(current *chatui.Model) {
		current.SearchLoadingMoreChannels, current.SearchMoreChannelsError = true, ""
	})
	refreshChatRoute()
	go func() {
		client := chatBrowser.conversationClient()
		if client == nil {
			chatBrowser.commit(generation, func(current *chatui.Model) {
				current.SearchLoadingMoreChannels, current.SearchMoreChannelsError = false, chatSearchCopy(current, chatui.KeySearchMoreChannelsError)
			})
			refreshChatRoute()
			return
		}
		active := chatBrowser.config(cfg)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		searchText, searchIn, searchFrom := chatui.SearchFilters(chatBrowser.snapshot(), query)
		result, err := client.Search(chatRPCContext(ctx, active), &chatv1.SearchRequest{TenantId: active.Tenant, Query: searchText, ConversationId: searchIn, AuthorId: searchFrom, ChannelCursor: cursor, PageSize: 50})
		if !chatBrowser.generationActive(generation) {
			return
		}
		committed := chatBrowser.commit(generation, func(current *chatui.Model) {
			if current.Search != query {
				return
			}
			current.SearchLoadingMoreChannels = false
			if err != nil {
				current.SearchMoreChannelsError = chatSearchCopy(current, chatui.KeySearchMoreChannelsError)
				return
			}
			seen := make(map[string]bool, len(current.SearchChannels))
			for _, channel := range current.SearchChannels {
				seen[channel.ID] = true
			}
			for _, channel := range result.GetChannels() {
				if channel == nil || channel.GetConversationId() == "" || seen[channel.GetConversationId()] || len(current.SearchChannels) >= chatSearchChannelWindow {
					continue
				}
				seen[channel.GetConversationId()] = true
				current.SearchChannels = append(current.SearchChannels, chatSearchConversation(channel))
			}
			current.SearchChannelNextCursor = result.GetChannelNextCursor()
			current.SearchHasMoreChannels = result.GetChannelNextCursor() != "" && len(current.SearchChannels) < chatSearchChannelWindow
			current.SearchMoreChannelsError = ""
		})
		if committed {
			refreshChatRoute()
		}
	}()
}

// refreshChatRoute re-runs the current route's read so the resolved tree
// re-renders. It is the only way this package reaches the renderer: chatui is
// handed a Model by the route loader, so a state change the reader must see
// is a revalidation.
// chatRerender repaints the chat page from the live model without touching
// the route. It is set by the chat page component while it is mounted
// (chat_page_wasm.go) and nil otherwise, when the full revalidate is the only
// way to draw.
var chatRerender func()

func refreshChatRoute() {
	if chatRerender != nil {
		chatRerender()
		return
	}
	if productRouteRetry != nil {
		// Chat has already applied the change to its local model. Keep the
		// mounted conversation still while the route re-reads its projection.
		productQuietRefreshPending = true
		productRouteRetry()
	}
}

// refreshChatThreadRoute posts the repaint through the UI runtime's supported
// async ingress. ListPosts completes on a Go task; mutating a UseState handle
// directly there bypasses the framework's event loop and left its owned-fiber
// update pending until the reader's next click. PostAsync applies the tick at
// the runtime's next drain and batches it into a normal render pass.
func refreshChatThreadRoute(conversation, root string) {
	if chatRerender == nil {
		return
	}
	epoch := chatRerenderEpoch
	generation := chatBrowser.currentGeneration()
	ui.PostAsync(func() {
		model := chatBrowser.snapshot()
		if chatRerender != nil && chatRerenderEpoch == epoch && chatBrowser.generationActive(generation) && model.SelectedID == conversation && model.ShowThread && model.ThreadParentID == root {
			chatRerender()
		}
	})
}

// productPeopleDirectory is the authorized people projection the product
// shell already read for this route. Chat subject ids and worker refs are the
// same identifier space, so this is the client-side directory: no extra RPC,
// and no invented names when it is empty.
func productPeopleDirectory() []productui.Person {
	if lastResolvedProductView == nil {
		return nil
	}
	return lastResolvedProductView.People
}

// chatDirectorySnapshot is the names resolved so far.
//
// The product shell's People projection is folded in when this session has one,
// but it is never the only source and never erases what is already there: it is
// populated only after the People page has loaded, which is why an Arabic
// session and every second-persona session used to render raw subject ids.
func chatDirectorySnapshot() map[string]string {
	chatBrowser.mergeDirectory(chatDirectory(productPeopleDirectory()))
	chatBrowser.mergePhotos(chatPhotos(productPeopleDirectory()), false)
	return chatBrowser.directorySnapshot()
}

// noteChatAction reports one action's outcome as a transient notice and
// schedules its own expiry. An error clears on the same timer as a success,
// so nothing latches.
func noteChatAction(text string) { noteChatActionRetry(text, false) }

// noteChatActionWithRetry posts a notice the reader can act on. Callbacks.Retry
// is what the control calls, and with a conversation open that resubscribes
// the stream rather than reloading the page.
func noteChatActionWithRetry(text string) { noteChatActionRetry(text, true) }

func noteChatActionRetry(text string, retry bool) {
	if text == "" {
		return
	}
	token := chatBrowser.setNotice(text, retry)
	refreshChatRoute()
	time.AfterFunc(chatNoticeTTL, func() {
		if chatBrowser.clearNotice(token) {
			refreshChatRoute()
		}
	})
}

// chatActionFailed is the single funnel for a failed action.
func chatActionFailed(action string, err error) bool {
	if err == nil {
		return false
	}
	noteChatAction(actionFailureNotice(action, err))
	return true
}

// chatActionSucceeded clears whatever the last action said and, when there is
// something to say, says it. A successful action clearing the previous
// failure is the whole of finding 1: the error is per-action and transient.
func chatActionSucceeded(text string) {
	if text == "" {
		chatBrowser.clearNotice(0)
		refreshChatRoute()
		return
	}
	noteChatAction(text)
}

// loadChatProjection reads the conversation list and the selected
// conversation, and commits the result only while it is still the answer to
// the reader's current selection.
func loadChatProjection(ctx context.Context) (chatui.Model, error) {
	client := chatBrowser.conversationClient()
	cfg := chatBrowser.config(journeyclient.Config{})
	if client == nil {
		return chatui.Model{State: chatui.StateError, Error: "chat client is not configured"}, fmt.Errorf("chat client is not configured")
	}
	// The generation is read, never bumped: a route revalidation is not a
	// change of what the reader is looking at. Bumping here is what let a
	// revalidation cancel an in-flight selection, and committing without
	// this check is what let its stale snapshot restore the old SelectedID
	// and Messages over the newer one.
	generation := chatBrowser.currentGeneration()
	callCtx := chatRPCContext(ctx, cfg)
	list, err := client.ListConversations(callCtx, &chatv1.ListConversationsRequest{TenantId: cfg.Tenant, PageSize: 100, IncludeDiscoverable: true})
	if err != nil {
		message := chatLoadFailureMessage("your conversations", err)
		chatBrowser.setLoadError(message)
		model := chatBrowser.mutate(func(model *chatui.Model) {
			model.State, model.Error = chatui.StateError, message
			model.CurrentUser, model.Locale = cfg.Subject, cfg.Locale
		})
		return model, err
	}
	// The directory read is chat's own and happens on open, in the background:
	// the first render may show an id, and the read that lands a moment later
	// replaces it with a name.
	go loadChatDirectory(cfg)
	directory := chatDirectorySnapshot()

	local := chatBrowser.snapshot()
	conversations := make([]chatui.Conversation, 0, len(list.GetConversations()))
	discoverable := make(map[string]chatui.Conversation)
	for _, conversation := range list.GetConversations() {
		if conversation == nil {
			continue
		}
		room := chatBrowseConversation(conversation)
		room = chatDirectoryNamedDirect(room, local.PeerIDs, directory)
		if room.Joined {
			conversations = append(conversations, room)
		} else if room.Kind == chatui.PublicChannel {
			discoverable[room.ID] = room
		}
	}

	model := chatBrowser.snapshot()
	model.State, model.Error = chatui.StateReady, ""
	model.CurrentUser, model.Locale = cfg.Subject, cfg.Locale
	model.PhotoURLs = chatBrowser.photoSnapshot()
	// The viewer's own name, so a direct message can be titled by the other
	// people in it. It arrives with the directory, which may land later.
	model.CurrentUserName = chatDisplayName(directory, cfg.Subject)
	model.Number = chatNumberFormatter(cfg.Locale)
	sortChatRail(conversations, chatBrowser.activitySnapshot())
	model.Conversations = conversations
	model.EditDrafts = chatBrowser.editDraftSnapshot()
	previous := model.SelectedID
	model.SelectedID = ""
	var previewAccess *chatui.Conversation
	for _, conversation := range conversations {
		if conversation.ID == previous {
			model.SelectedID = previous
			break
		}
	}
	model.PreviewConversation = nil
	if model.SelectedID == "" {
		if preview, ok := discoverable[previous]; ok {
			model.SelectedID = previous
			model.PreviewConversation = &preview
			previewCopy := preview
			previewAccess = &previewCopy
		}
	}
	if model.SelectedID == "" && len(conversations) > 0 {
		model.SelectedID = conversations[0].ID
	}
	if model.SelectedID != previous {
		model.Messages, model.HasOlder = nil, false
		model.Draft = chatBrowser.draft(model.SelectedID)
	}

	var cursor chatCursor
	// While a stream is delivering this conversation, its timeline is already
	// current and re-reading it here would make every transient notice cost a
	// ListPosts walk, a ListPins read and a reaction fan-out.
	streaming := chatBrowser.streamLive(model.SelectedID) && model.SelectedID == previous && len(model.Messages) > 0
	reloaded := false
	if model.SelectedID != "" && model.Search == "" && !streaming {
		reloaded = true
		loaded, postErr := loadChatPosts(callCtx, client, cfg, &model, directory)
		if postErr != nil {
			message := chatLoadFailureMessage("this conversation", postErr)
			chatBrowser.setLoadError(message)
			model.State, model.Error = chatui.StateError, message
		} else {
			chatBrowser.setLoadError("")
			cursor = loaded
		}
	} else {
		chatBrowser.setLoadError("")
	}
	// A notice raised by the last action survives a load; a load error does
	// not survive a successful one.
	model.Notice = chatBrowser.currentNotice()
	applyCachedChatRecipientProjection(&model)

	// The composer, the drafts and the notice are re-read at commit time
	// rather than carried in from the snapshot this read started with: a send
	// that cleared the draft during the round trip must not be undone by it.
	if !chatBrowser.adoptLoadedChatProjection(generation, model, cursor, reloaded) {
		// The reader moved on while this read was in flight. Their newer
		// state is the answer, not this one.
		return chatBrowser.snapshot(), nil
	}
	if cursor.ConversationID != "" {
		subscribeChatConversation(cursor.ConversationID)
	}
	if previewAccess != nil {
		promptChatChannelAccess(cfg, *previewAccess)
	}
	startChatRecipientProjection(cfg, chatMembershipRows(list.GetConversations()))
	startChatDMPeers(cfg, conversations)
	openChatShareFragment(cfg)
	openChatChannelFragment(cfg)
	openChatPersonFragment(cfg)
	return chatBrowser.snapshot(), nil
}

// startChatDMPeers resolves room peers through admitted memberships. Room IDs
// are opaque and never treated as worker IDs or photo paths.
func startChatDMPeers(cfg journeyclient.Config, rooms []chatui.Conversation) {
	semaphore := make(chan struct{}, 4)
	for _, room := range rooms {
		if room.Kind != chatui.DirectMessage || !chatBrowser.claimDMPeerRead(room.ID) {
			continue
		}
		go func(roomID string) {
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			client := chatBrowser.conversationClient()
			if client == nil {
				chatBrowser.releaseDMPeerRead(roomID)
				return
			}
			result, err := client.ListMemberships(chatRPCContext(ctx, cfg), &chatv1.ListMembershipsRequest{TenantId: cfg.Tenant, ConversationId: roomID, PageSize: 100})
			if err != nil {
				chatBrowser.releaseDMPeerRead(roomID)
				return
			}
			peer := chatDMPeerFromMemberships(result.GetMemberships(), cfg.Subject)
			if peer == "" {
				return
			}
			current := chatBrowser.config(journeyclient.Config{})
			if current.Tenant != cfg.Tenant || current.Subject != cfg.Subject || current.Bearer != cfg.Bearer {
				return
			}
			chatBrowser.mutate(func(model *chatui.Model) {
				if model.PeerIDs == nil {
					model.PeerIDs = make(map[string]string)
				}
				model.PeerIDs[roomID] = peer
			})
			chatBrowser.applyChatDirectory(chatDirectorySnapshot())
			ui.PostAsync(func() {
				if chatRerender != nil {
					refreshChatRoute()
				}
			})
		}(room.ID)
	}
}

func chatMembershipRows(rows []*chatv1.Conversation) []*chatv1.Conversation {
	joined := make([]*chatv1.Conversation, 0, len(rows))
	for _, row := range rows {
		if row != nil && row.GetJoined() {
			joined = append(joined, row)
		}
	}
	return joined
}

// A direct conversation with only the viewer as an active member is a DM to
// self. Keep that stable subject as its peer so a nameless server row can be
// labeled from the governed worker directory after a reload as well as on
// creation. Other shapes must not acquire a guessed peer.
func chatDMPeerFromMemberships(memberships []*chatv1.Membership, viewer string) string {
	if viewer == "" {
		return ""
	}
	seen := make(map[string]bool, len(memberships))
	for _, membership := range memberships {
		if membership == nil || membership.GetLeftAt() != nil {
			continue
		}
		subject := membership.GetSubjectId()
		if subject == "" || seen[subject] {
			return ""
		}
		seen[subject] = true
	}
	if !seen[viewer] {
		return ""
	}
	if len(seen) == 1 {
		return viewer
	}
	if len(seen) == 2 {
		for subject := range seen {
			if subject != viewer {
				return subject
			}
		}
	}
	return ""
}

// loadChatPosts fills the timeline for the selected conversation and returns
// the stream cursor the subscription starts from.
//
// The server pages newest-first, so this is one backward page shown in reading
// order. HasOlder follows the server's next cursor, and Callbacks.LoadOlder
// prepends the page after it rather than re-reading the conversation.
type chatSearchAnchor struct {
	sequence uint64
	postID   string
}

func loadChatPosts(ctx context.Context, client chatv1.ConversationServiceClient, cfg journeyclient.Config, model *chatui.Model, directory map[string]string, anchors ...chatSearchAnchor) (chatCursor, error) {
	cursor := chatCursor{ConversationID: model.SelectedID, Seen: map[string]uint64{}}
	if model.SelectedID == "" {
		return cursor, nil
	}
	// Ordinary opens show the newest page. Search opens read equal context on
	// both sides of the hit so paging newer cannot immediately move it out of
	// view or make it look like the newest message.
	model.HasNewer = false
	var posts []*chatv1.Post
	var olderCursor, newerCursor string
	if len(anchors) > 0 && anchors[0].sequence > 0 {
		olderRequest, newerRequest := chatSearchAnchorRequests(cfg.Tenant, model.SelectedID, anchors[0].sequence)
		olderResult, err := client.ListPosts(ctx, olderRequest)
		if err != nil {
			return cursor, err
		}
		newerResult, err := client.ListPosts(ctx, newerRequest)
		if err != nil {
			return cursor, err
		}
		posts = mergeChatSearchAnchorPosts(olderResult.GetPosts(), newerResult.GetPosts())
		olderCursor, newerCursor = olderResult.GetNextCursor(), newerResult.GetNextCursor()
		model.HasNewer = newerCursor != ""
	} else {
		result, err := client.ListPosts(ctx, &chatv1.ListPostsRequest{
			TenantId: cfg.Tenant, ConversationId: model.SelectedID, PageSize: chatPageSize, Descending: true,
		})
		if err != nil {
			return cursor, err
		}
		posts = chatInConversationOrder(result.GetPosts())
		olderCursor = result.GetNextCursor()
	}
	chatBrowser.setOlderCursor(olderCursor)
	now := time.Now()
	reactions := chatBrowser.reactionSnapshot()
	model.Messages = chatMessages(posts, cfg.Locale, directory, reactions, now)
	if len(anchors) > 0 && anchors[0].postID != "" {
		found := false
		for _, message := range model.Messages {
			if message.ID == anchors[0].postID {
				found = true
				break
			}
		}
		if !found {
			return cursor, fmt.Errorf("the message is no longer available")
		}
		model.FocusMessageID = anchors[0].postID
	}
	window := chatBrowser.retainedWindow()
	model.Messages, model.HasOlder = boundChatMessages(model.Messages, window, olderCursor != "")
	if model.ShowThread && model.ThreadParentID != "" {
		for i := range model.Messages {
			if model.Messages[i].ID == model.ThreadParentID {
				parent := model.Messages[i]
				model.ThreadParent = &parent
				break
			}
		}
		model.ThreadMessages = chatThreadMessages(posts, model.ThreadParentID, cfg.Locale, directory, now)
		if boundLiveChatThread(model) || olderCursor != "" {
			model.ThreadHasOlder = true
		}
	}
	// The post sequence, for ordering and for a ListPosts catch-up. The watch
	// position is deliberately left at zero: it lives in the server's outbox
	// space and is only ever learned from the stream.
	cursor.LastSequence = chatLastSequence(posts)
	if len(model.Messages) > 0 {
		chatBrowser.noteChatActivity(model.SelectedID, model.Messages[len(model.Messages)-1].SentAt)
	}
	model.UnreadFromID = chatUnreadFrom(model.Conversations, model.SelectedID, model.Messages)
	for _, post := range posts {
		if post != nil && post.GetId() != "" {
			cursor.Seen[post.GetId()] = post.GetSequence()
		}
	}
	applyChatPins(ctx, client, cfg, model)
	if len(model.Messages) == 0 {
		model.State = chatui.StateEmpty
	}
	go loadChatReactionsLater(cfg, model.SelectedID, chatPostWindow(model.Messages, chatReactionWindow))
	markChatRead(cfg, model.SelectedID, posts)
	// After the timeline, never before it: the attachments render as chips and
	// their URLs swap in when the grants land.
	go resolveChatMedia(cfg, model.SelectedID)
	return cursor, nil
}

// applyChatPins merges the pin projection and keeps each pin's revision:
// UnpinPost refuses a zero expected_revision and ListPins is the only place
// that revision comes from.
func applyChatPins(ctx context.Context, client chatv1.ConversationServiceClient, cfg journeyclient.Config, model *chatui.Model) {
	pins, err := client.ListPins(ctx, &chatv1.ListPinsRequest{TenantId: cfg.Tenant, ConversationId: model.SelectedID})
	if err != nil {
		return
	}
	revisions := make(map[string]uint64, len(pins.GetPins()))
	for _, pin := range pins.GetPins() {
		if pin != nil {
			revisions[pin.GetPostId()] = pin.GetRevision()
		}
	}
	chatBrowser.setPinRevisions(revisions)
	model.ChannelPins = make([]chatui.ChannelPin, 0, len(pins.GetPins()))
	directory := chatDirectorySnapshot()
	for _, pin := range pins.GetPins() {
		if pin == nil || pin.GetPost() == nil || pin.GetPostId() == "" {
			continue
		}
		post := pin.GetPost()
		model.ChannelPins = append(model.ChannelPins, chatui.ChannelPin{
			PostID: pin.GetPostId(), Author: chatDisplayName(directory, post.GetAuthorId()),
			Body: post.GetBody(), Sequence: post.GetSequence(),
		})
	}
	if model.ChannelTodoSourcePin != "" {
		found := false
		for _, pin := range model.ChannelPins {
			if pin.PostID == model.ChannelTodoSourcePin {
				found = true
				break
			}
		}
		if !found {
			model.ChannelTodoSourcePin = ""
		}
	}
	for i := range model.Messages {
		_, pinned := revisions[model.Messages[i].ID]
		model.Messages[i].Pinned = pinned
	}
}

// loadChatReactionCounts fills Message.Reactions for the newest visible posts.
//
// loadChatReactionsLater reads the reactions for the newest posts of a room
// after the room is on screen, and folds them in when they land. The read
// used to happen inside the open, before the commit, so a room switch waited
// on up to fifty round trips and the skeleton stayed up for over a second.
func loadChatReactionsLater(cfg journeyclient.Config, conversationID string, ids []string) {
	if len(ids) == 0 || !chatBrowser.claimReactionRead(conversationID) {
		return
	}
	client := chatBrowser.conversationClient()
	if client == nil {
		chatBrowser.releaseReactionRead()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ctx = chatRPCContext(ctx, cfg)
	chips := make(map[string][]chatui.ReactionChip, len(ids))
	members := make(map[string]map[chatReactionIdentity]struct{}, len(ids))
	sem := make(chan struct{}, chatReactionFanout)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, id := range ids {
		id := id
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			result, err := client.ListReactions(ctx, &chatv1.ListReactionsRequest{TenantId: cfg.Tenant, ConversationId: conversationID, PostId: id, PageSize: 100})
			if err != nil {
				return
			}
			mu.Lock()
			chips[id], members[id] = chatReactionSnapshot(result.GetReactions(), cfg.Subject)
			mu.Unlock()
		}()
	}
	wg.Wait()
	chatBrowser.setReactions(chips, members)
	if chatBrowser.applyReactionChips(conversationID, chips) {
		refreshChatRoute()
	}
}

// chatReactionChips folds one post's reactions into chips: one per emoji in
// first-seen order, counted, with the viewer's own marked.
func chatReactionChips(reactions []*chatv1.Reaction, viewer string) []chatui.ReactionChip {
	chips, _ := chatReactionSnapshot(reactions, viewer)
	return chips
}

func chatReactionSnapshot(reactions []*chatv1.Reaction, viewer string) ([]chatui.ReactionChip, map[chatReactionIdentity]struct{}) {
	chips := make([]chatui.ReactionChip, 0, 4)
	members := make(map[chatReactionIdentity]struct{}, len(reactions))
	for _, reaction := range reactions {
		if reaction == nil {
			continue
		}
		emoji := reaction.GetEmoji()
		if emoji == "" {
			emoji = chatDefaultReaction
		}
		identity := chatReactionIdentity{homeTenantID: reaction.GetHomeTenantId(), subjectID: reaction.GetSubjectId(), emoji: emoji}
		if identity.subjectID == "" {
			continue
		}
		if _, exists := members[identity]; exists {
			continue
		}
		members[identity] = struct{}{}
		chips = adjustChips(chips, emoji, 1, viewer != "" && reaction.GetSubjectId() == viewer)
	}
	return chips, members
}

func chatCallbacks(cfg journeyclient.Config) chatui.Callbacks {
	callbacks := chatui.Callbacks{
		ToggleSidebar: func(open bool) {
			chatBrowser.mutate(func(model *chatui.Model) {
				model.Pane.RailCollapsed = !open
				model.SidebarOpen = open
			})
			refreshChatRoute()
		},
		OpenCreate: func() {
			chatBrowser.mutate(func(model *chatui.Model) {
				model.ShowCreate = true
				model.NewKind = chatui.PublicChannel
				model.NewName = ""
			})
			refreshChatRoute()
			// The member picker searches the governed directory; read it now
			// if the session has not needed it yet.
			if len(chatBrowser.snapshot().SearchDirectory) == 0 {
				go loadChatDirectory(cfg)
			}
		},
		CloseCreate: func() {
			chatBrowser.mutate(func(model *chatui.Model) { model.ShowCreate = false })
			refreshChatRoute()
		},
		SelectConversation: func(id string) {
			navigation := chatBrowser.snapshot()
			navigation.SelectedID = id
			navigation.Search = ""
			navigation.ShowThread, navigation.ThreadParentID = false, ""
			navigation.FocusMessageID = ""
			chatHistory.push(navigation)
			openChatConversation(cfg, id)
		},
		OpenSearchMessage: func(conversationID, postID string, sequence uint64) {
			navigation := chatBrowser.snapshot()
			navigation.SelectedID = conversationID
			navigation.Search = ""
			navigation.ShowThread, navigation.ThreadParentID = false, ""
			navigation.FocusMessageID = postID
			state := chatNavigationStateFromModel(navigation)
			state.FocusSequence = sequence
			chatHistory.pushState(state)
			chatBrowser.beginGeneration()
			chatBrowser.mutate(func(model *chatui.Model) {
				model.Search, model.SearchLoading, model.SearchError = "", false, ""
				model.SearchChannels, model.SearchPeople, model.SearchMessages = nil, nil, nil
				model.FocusMessageID = postID
			})
			refreshChatRoute()
			openChatConversationAt(cfg, conversationID, sequence, postID)
		},
		OpenConversationDetails: func(id string) {
			navigation := chatBrowser.snapshot()
			navigation.SelectedID, navigation.ShowDetails = id, true
			navigation.ShowThread, navigation.ThreadParentID = false, ""
			navigation.FocusMessageID = ""
			chatHistory.push(navigation)
			openChatConversation(cfg, id)
			chatBrowser.mutate(func(model *chatui.Model) { model.ShowDetails = true; model.RailMenuID = ""; model.SidebarOpen = false })
			refreshChatRoute()
			go loadChatMembers(cfg)
			go loadChannelTodo(cfg, id)
			go loadChannelWidgets(cfg, id)
			go loadChannelPoll(cfg, id)
		},
		OpenRailMenu: func(id string) {
			chatBrowser.mutate(func(model *chatui.Model) { model.RailMenuID = id })
			refreshChatRoute()
			if id != "" {
				go loadChatNotificationMode(cfg, id)
			}
		},
		SetConversationNotification: func(id string, mode chatui.NotificationMode) {
			saveChatNotificationMode(cfg, id, mode)
		},
		CreateConversation: func(kind chatui.ConversationKind, name string, members []string) {
			go func() {
				client := chatBrowser.conversationClient()
				if client == nil {
					return
				}
				active := chatBrowser.config(cfg)
				memberRefs := make([]*chatv1.MemberRef, 0, len(members))
				for _, member := range members {
					if member = strings.TrimSpace(member); member != "" {
						memberRefs = append(memberRefs, &chatv1.MemberRef{TenantId: active.Tenant, SubjectId: member})
					}
				}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				created, err := client.CreateConversation(chatRPCContext(ctx, active), &chatv1.CreateConversationRequest{
					TenantId: active.Tenant, Kind: chatKind(kind), Name: strings.TrimSpace(name), OwnerId: active.Subject,
					Members: memberRefs, IdempotencyKey: fmt.Sprintf("wasm-create-%d", time.Now().UnixNano()),
				})
				if err != nil {
					// Nothing will open, so anything typed while this Create was
					// in flight goes back to the composer rather than waiting
					// for a room that will never exist.
					if waiting := chatBrowser.discardQueuedChatSends(); len(waiting) > 0 {
						chatBrowser.restoreDraft(chatBrowser.selectedID(), strings.Join(waiting, "\n"))
						flushChatDraftPersist(active)
					}
					chatActionFailed("create this conversation", err)
					refreshChatRoute()
					return
				}
				conversation := created.GetConversation()
				// The new room is put in the rail and selected before anything
				// renders. Reloading the projection and leaving the previous
				// selection alone is how messages meant for a channel someone
				// had just created went to whichever conversation happened to
				// be first in the list.
				chatBrowser.mutate(func(model *chatui.Model) {
					model.ShowCreate = false
					if conversation.GetId() != "" && !chatJoinedSet(model.Conversations)[conversation.GetId()] {
						model.Conversations = append(model.Conversations, chatConversation(conversation))
					}
				})
				chatActionSucceeded("Conversation created")
				invalidateChatRecipientProjection()
				if id := conversation.GetId(); id != "" {
					openChatConversation(active, id)
					return
				}
				refreshChatRoute()
			}()
		},
		DraftChanged: func(conversationID, value string) {
			// Local only. No RPC, no revalidation: see the file comment.
			chatBrowser.setDraft(conversationID, value)
			scheduleChatDraftEmbeds(cfg, conversationID, value)
			scheduleChatDraftPersist(cfg)
		},
		SendMessage: func(conversationID, body string) {
			// The room on screen decides where this goes, and a room still
			// opening holds the message until it is ready. chatui addresses a
			// send from the model it last rendered, so a message typed a second
			// after a room was created carried the previous room's id -- or
			// none, and was dropped before it ever reached an RPC.
			target, queued := chatBrowser.resolveSendTarget(conversationID, body)
			if queued {
				return
			}
			go sendChatMessage(cfg, target, body)
		},
		ReplyInThread: func(parentID, body string) {
			go func() {
				client := chatBrowser.conversationClient()
				if client == nil || parentID == "" {
					return
				}
				active := chatBrowser.config(cfg)
				conversationID := chatBrowser.selectedID()
				if conversationID == "" {
					return
				}
				// A reply keys its idempotency on the thread, not the room, so
				// the same text in the conversation and under a message are two
				// different attempts.
				key := chatBrowser.sendKey(conversationID+"\x00"+parentID, body, func() string {
					return fmt.Sprintf("wasm-reply-%d", time.Now().UnixNano())
				})
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				callCtx := chatRPCContext(ctx, active)
				_, err := client.SendPost(callCtx, &chatv1.SendPostRequest{
					TenantId: active.Tenant, ConversationId: conversationID, Body: body, ParentId: parentID, IdempotencyKey: key,
				})
				if chatActionFailed("post this reply", err) {
					return
				}
				chatBrowser.clearSendKey(conversationID + "\x00" + parentID)
				refreshChatThread(callCtx, client, active, conversationID, parentID)
				invalidateChatRecipientProjection()
				chatBrowser.clearNotice(0)
				chatStreamRender.Schedule()
			}()
		},
		Search: func(query string) {
			query = strings.TrimSpace(query)
			generation := chatBrowser.beginGeneration()
			if query == "" {
				chatBrowser.commit(generation, func(model *chatui.Model) {
					model.Search, model.SearchLoading, model.SearchError = "", false, ""
					model.SearchMoreError, model.SearchNextCursor = "", ""
					model.SearchMoreChannelsError, model.SearchChannelNextCursor = "", ""
					model.SearchHasMore, model.SearchLoadingMore = false, false
					model.SearchHasMoreChannels, model.SearchLoadingMoreChannels = false, false
					model.SearchChannels, model.SearchPeople, model.SearchMessages = nil, nil, nil
				})
				refreshChatRoute()
				return
			}
			// Directory reads can fail transiently (or finish after this query).
			// Ensure an unavailable/empty session cache gets another governed
			// read; the completion path reruns the active query with the names.
			if len(chatBrowser.snapshot().SearchDirectory) == 0 {
				go loadChatDirectory(cfg)
			}
			chatBrowser.mutate(func(model *chatui.Model) {
				model.Search, model.SearchLoading, model.SearchError = query, true, ""
				model.SearchMoreError, model.SearchNextCursor = "", ""
				model.SearchMoreChannelsError, model.SearchChannelNextCursor = "", ""
				model.SearchHasMore, model.SearchLoadingMore = false, false
				model.SearchHasMoreChannels, model.SearchLoadingMoreChannels = false, false
				model.FocusMessageID = ""
				model.SearchChannels, model.SearchPeople, model.SearchMessages = nil, nil, nil
			})
			refreshChatRoute()
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				timer := time.NewTimer(180 * time.Millisecond)
				defer timer.Stop()
				select {
				case <-timer.C:
				case <-ctx.Done():
					return
				}
				if !chatBrowser.generationActive(generation) {
					return
				}
				client := chatBrowser.conversationClient()
				if client == nil {
					chatBrowser.commit(generation, func(model *chatui.Model) {
						model.SearchLoading, model.SearchError = false, chatSearchCopy(model, chatui.KeySearchError)
					})
					refreshChatRoute()
					return
				}
				active := chatBrowser.config(cfg)
				// Slack-style "in:#channel" and "from:@name" filters narrow the
				// server search; the remaining words are the query.
				searchText, searchIn, searchFrom := chatui.SearchFilters(chatBrowser.snapshot(), query)
				result, err := client.Search(chatRPCContext(ctx, active), &chatv1.SearchRequest{TenantId: active.Tenant, Query: searchText, ConversationId: searchIn, AuthorId: searchFrom, PageSize: 50})
				directory := chatDirectorySnapshot()
				people := chatSearchVisibleWorkers(chatBrowser.snapshot().SearchDirectory, query, 20)
				now := time.Now()
				committed := chatBrowser.commit(generation, func(model *chatui.Model) {
					model.SearchLoading = false
					if err != nil {
						model.SearchError = chatSearchCopy(model, chatui.KeySearchError)
						return
					}
					model.SearchChannels = nil
					for _, channel := range result.GetChannels() {
						if channel == nil || channel.GetConversationId() == "" {
							continue
						}
						model.SearchChannels = append(model.SearchChannels, chatSearchConversation(channel))
					}
					model.SearchPeople = people
					model.SearchMessages = nil
					model.SearchNextCursor = result.GetNextCursor()
					model.SearchHasMore = result.GetNextCursor() != ""
					model.SearchChannelNextCursor = result.GetChannelNextCursor()
					model.SearchHasMoreChannels = result.GetChannelNextCursor() != ""
					model.SearchMoreError, model.SearchLoadingMore = "", false
					model.SearchMoreChannelsError, model.SearchLoadingMoreChannels = "", false
					for _, hit := range result.GetResults() {
						if hit != nil && hit.GetPost() != nil {
							post := hit.GetPost()
							model.SearchMessages = append(model.SearchMessages, chatui.SearchMessage{
								ConversationID: post.GetConversationId(), ConversationName: hit.GetConversationName(),
								Message: chatMessage(post, active.Locale, directory, now),
							})
						}
					}
				})
				if committed {
					refreshChatRoute()
				}
			}()
		},
		SearchMore:         func() { loadMoreChatSearch(cfg) },
		SearchMoreChannels: func() { loadMoreChatSearchChannels(cfg) },
		OpenThread: func(postID string) {
			chatBrowser.mutate(func(model *chatui.Model) {
				model.ShowThread, model.ThreadParentID, model.ThreadMessages = true, postID, nil
				model.ThreadLoading = true
				model.ThreadHasOlder, model.ThreadHasNewer = false, false
				model.ThreadParent = nil
				for i := range model.Messages {
					if model.Messages[i].ID == postID {
						parent := model.Messages[i]
						model.ThreadParent = &parent
						break
					}
				}
			})
			chatHistory.push(chatBrowser.snapshot())
			refreshChatRoute()
			go func() {
				client := chatBrowser.conversationClient()
				if client == nil {
					chatBrowser.mutate(func(model *chatui.Model) {
						if model.ShowThread && model.ThreadParentID == postID {
							model.ThreadLoading = false
						}
					})
					refreshChatRoute()
					return
				}
				active := chatBrowser.config(cfg)
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				conversation := chatBrowser.selectedID()
				refreshChatThread(chatRPCContext(ctx, active), client, active, conversation, postID)
				loadChatThreadFollow(active, postID)
				refreshChatThreadRoute(conversation, postID)
			}()
		},
		CloseThread: func() {
			chatBrowser.mutate(func(model *chatui.Model) {
				model.ShowThread, model.ThreadParentID, model.ThreadMessages = false, "", nil
				model.ThreadLoading = false
				model.ThreadParent = nil
				model.ThreadFollowed = false
			})
			chatHistory.push(chatBrowser.snapshot())
			refreshChatRoute()
		},
		ToggleDetails: func(open bool) {
			chatBrowser.mutate(func(model *chatui.Model) { model.ShowDetails = open })
			chatHistory.push(chatBrowser.snapshot())
			refreshChatRoute()
			if open {
				// Members load because the pane opened, not because the
				// reader found the "Refresh members" button.
				go loadChatMembers(cfg)
			}
		},
		// The to-do list and poll open inline in the chat (chatui's channel
		// tray), not in the details pane; these only load and focus them.
		OpenChannelTodo: func() {
			chatui.FocusChannelTodo()
		},
		OpenChannelPoll: func(conversationID string) {
			current := chatBrowser.snapshot()
			startPollLoad := !current.ChannelPollLoading && current.ChannelPoll.Revision == 0 && current.ChannelPollError == ""
			if startPollLoad {
				chatBrowser.mutate(func(model *chatui.Model) {
					if model.SelectedID == conversationID {
						model.ChannelPollLoading = true
					}
				})
				refreshChatRoute()
			}
			if startPollLoad {
				go loadChannelPoll(cfg, conversationID)
			}
			chatui.FocusChannelPoll(conversationID)
		},
		LoadMembers: func() { go loadChatMembers(cfg) },
		BeginEdit: func(postID string) {
			chatBrowser.mutate(func(model *chatui.Model) { model.EditingID = postID })
			refreshChatRoute()
		},
		SetEditDraft: func(postID, body string) {
			// Local only, for the same reason as DraftChanged.
			chatBrowser.setEditDraft(postID, body)
		},
		EditMessage: func(postID, body string, revision uint64) {
			go func() {
				client := chatBrowser.conversationClient()
				if client == nil {
					return
				}
				active := chatBrowser.config(cfg)
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				_, err := client.EditPost(chatRPCContext(ctx, active), &chatv1.EditPostRequest{
					Post: &chatv1.Post{TenantId: active.Tenant, ConversationId: chatBrowser.selectedID(), Id: postID}, Body: body, ExpectedRevision: revision,
				})
				if chatActionFailed("save this edit", err) {
					return
				}
				chatBrowser.clearEditDraft(postID)
				chatBrowser.mutate(func(model *chatui.Model) { model.EditingID = "" })
				chatActionSucceeded("")
				invalidateChatRecipientProjection()
				refreshChatRoute()
			}()
		},
		DeleteMessage: func(postID string, revision uint64) {
			go func() {
				client := chatBrowser.conversationClient()
				if client == nil {
					return
				}
				active := chatBrowser.config(cfg)
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				_, err := client.DeletePost(chatRPCContext(ctx, active), &chatv1.DeletePostRequest{
					TenantId: active.Tenant, ConversationId: chatBrowser.selectedID(), PostId: postID, ExpectedRevision: revision,
				})
				if chatActionFailed("delete this message", err) {
					return
				}
				chatActionSucceeded("Message deleted")
				invalidateChatRecipientProjection()
				refreshChatRoute()
			}()
		},
		React:     func(postID string) { go addChatReaction(cfg, postID, chatDefaultReaction) },
		ReactWith: func(postID, emoji string) { go addChatReaction(cfg, postID, emoji) },
		OpenPicker: func(postID string) {
			chatBrowser.mutate(func(model *chatui.Model) { model.PickerID, model.MenuID = postID, "" })
			refreshChatRoute()
		},
		OpenMenu: func(postID string) {
			chatBrowser.mutate(func(model *chatui.Model) { model.MenuID, model.PickerID = postID, "" })
			refreshChatRoute()
		},
		CopyLink:                  func(postID string) { copyChatMessageLink(postID) },
		CopyContents:              func(postID string) { copyChatMessageContents(postID) },
		CopyConversationReference: func(id, label string) { copyChatConversationReference(id, label) },
		CopyConversationAPICurl:   func(id string) { copyChatConversationAPICurl(id) },
		OpenEmbeddedMessage:       func(key string) { openChatEmbeddedMessage(cfg, key) },
		DownloadAttachment:        func(postID, attachmentID string) { go downloadChatAttachment(cfg, postID, attachmentID) },
		OpenShare:                 func(postID string) { openChatShare(cfg, postID) },
		CloseShare:                func() { chatBrowser.closeShare(); refreshChatShareRoute() },
		FilterShare:               func(query string) { chatBrowser.shareFilter(query); refreshChatShareRoute() },
		SelectShareDestination: func(id string) {
			if chatBrowser.shareSelect(id) {
				refreshChatShareRoute()
			}
		},
		ShareMessage: func() { shareChatMessage(cfg) },
		FilterMembers: func(query string) {
			chatBrowser.mutate(func(model *chatui.Model) { model.MemberQuery = query })
			refreshChatRoute()
		},
		JumpToNewest: func() { jumpToNewestChatMessages(cfg) },
		JumpToPin: func(postID string, sequence uint64) {
			if width := js.Global().Get("innerWidth"); width.Type() == js.TypeNumber {
				closeChatDetailsForPinJump(width.Int())
			}
			openChatConversationAt(cfg, chatBrowser.selectedID(), sequence, postID)
		},
		CopyPinReference: func(postID string) { copyChatMessageLink(postID) },
		Pin: func(postID string) {
			go func() {
				client := chatBrowser.conversationClient()
				if client == nil {
					return
				}
				active := chatBrowser.config(cfg)
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				result, err := client.PinPost(chatRPCContext(ctx, active), &chatv1.PinPostRequest{
					Pin: &chatv1.Pin{TenantId: active.Tenant, ConversationId: chatBrowser.selectedID(), PostId: postID, PinnedBy: active.Subject},
				})
				if chatActionFailed("pin this message", err) {
					return
				}
				chatBrowser.setPinRevision(postID, result.GetPin().GetRevision())
				chatBrowser.mutate(func(model *chatui.Model) { setChatPinned(model, postID, true) })
				chatActionSucceeded("Message pinned")
				refreshChatRoute()
			}()
		},
		SavePreferences: func(prefs chatui.Preferences) { saveChatNotificationPreference(cfg, prefs) },
		Retry: func() {
			go func() {
				// With a room open, Try again means "make this live again":
				// read what was missed by cursor and resubscribe. Reloading the
				// whole projection would put the open conversation through a
				// loading render for a problem that is only the stream's.
				if id := chatBrowser.selectedID(); id != "" && chatBrowser.loadError() == "" {
					chatBrowser.clearNotice(0)
					active := chatBrowser.config(cfg)
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					catchUpChatConversation(ctx, id, chatBrowser.currentGeneration(), active)
					subscribeChatConversation(id)
					refreshChatRoute()
					return
				}
				_, _ = loadChatProjection(context.Background())
				refreshChatRoute()
			}()
		},
	}
	return withChatDocCallbacks(withChannelPollCallbacks(withChannelWidgetCallbacks(withChannelTodoCallbacks(withChatPersonCallbacks(withChatRecipientCallbacks(withChatDiscoveryCallbacks(callbacks, cfg), cfg, refreshChatRoute), cfg), cfg), cfg), cfg), cfg)
}

func closeChatDetailsForPinJump(viewportWidth int) {
	if viewportWidth <= 0 || viewportWidth > 1350 {
		return
	}
	chatBrowser.mutate(func(model *chatui.Model) { model.ShowDetails = false })
	refreshChatRoute()
}

// sendChatMessage posts to the conversation the reader is in.
func sendChatMessage(cfg journeyclient.Config, conversationID, body string) {
	client := chatBrowser.conversationClient()
	if client == nil || conversationID == "" {
		return
	}
	active := chatBrowser.config(cfg)
	if !chatDraftIdentityMatches(cfg.Tenant, cfg.Subject, active.Tenant, active.Subject) {
		return
	}
	// No parent, ever. This is the main composer; a reply goes through
	// ReplyInThread with the root the reader is looking at, so an open thread
	// pane can no longer swallow a message meant for the conversation.
	//
	// The key belongs to this body. An identical retry reuses it (that is what
	// makes the retry safe); an edited retry after a timeout gets a new one
	// instead of replaying the first post.
	key := chatBrowser.sendKey(conversationID, body, func() string { return fmt.Sprintf("wasm-send-%d", time.Now().UnixNano()) })
	// The composer is empty from this moment, before the RPC rather than after
	// it. The reader has pressed Enter; the text is no longer a draft, and any
	// render between here and the response must show an empty composer. This
	// also cancels the pending debounced write of the text being sent, which
	// would otherwise land after it.
	chatBrowser.setDraft(conversationID, "")
	flushChatDraftPersist(active)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sent, err := client.SendPost(chatRPCContext(ctx, active), &chatv1.SendPostRequest{
		TenantId: active.Tenant, ConversationId: conversationID, Body: body, IdempotencyKey: key,
	})
	if err != nil {
		// A failed send is the one thing allowed to put the text back, so the
		// reader can retry it rather than retype it. The idempotency key is
		// kept: the same body retried reuses it.
		chatBrowser.restoreDraft(conversationID, body)
		flushChatDraftPersist(active)
		chatActionFailed("send this message", err)
		chatStreamRender.Schedule()
		return
	}
	chatBrowser.clearSendKey(conversationID)
	// The response is the committed post, so the reader's own message appears
	// from it. No projection reload: a send used to cost a full route
	// revalidation -- ListConversations, a ListPosts walk, ListPins -- for a
	// post the client was already holding, and the stream delivers the rest.
	_, gapped := chatBrowser.applySentChatPost(conversationID, sent.GetPost(), active.Locale, chatDirectorySnapshot(), time.Now())
	if gapped {
		// Other people posted while this send was in flight. Read those by
		// cursor; a full route reload would put the open conversation through a
		// loading render, and a loading render is what dropped the keystrokes
		// typed during it.
		catchUpChatConversation(ctx, conversationID, chatBrowser.currentGeneration(), active)
	}
	invalidateChatRecipientProjection()
	chatBrowser.clearNotice(0)
	chatStreamRender.Schedule()
}

// openChatConversation is the one path that makes a conversation the open one:
// the rail highlights, the composer swaps to that conversation's draft, the old
// stream is cancelled, the posts are read, and a new stream is opened. Both
// SelectConversation and a just-created room go through it, so a new room is
// never left unselected with its stream unopened.
func openChatConversation(cfg journeyclient.Config, id string) {
	openChatConversationAt(cfg, id, 0, "")
}

func openChatConversationAt(cfg journeyclient.Config, id string, sequence uint64, postID string) {
	if id == "" {
		return
	}
	// Re-opening the room already open is not a switch, and treating it as one
	// is what made the feed give up. Every re-open cancelled its own healthy
	// stream, claimed a new generation, and started again: the server saw a
	// run of CANCELED watches for a room it was serving perfectly well, and
	// five of them in forty seconds reached the give-up.
	if chatBrowser.alreadyOpen(id) || chatBrowser.alreadyOpening(id) {
		if chatBrowser.alreadyOpen(id) && sequence > 0 && postID != "" {
			go openChatSearchHitInCurrentConversation(cfg, id, sequence, postID)
		}
		return
	}
	// Count an actual navigation immediately, including search-driven opens.
	// The page-selection observer covers route entry and history restoration;
	// the visit state de-duplicates the same open when both paths fire.
	visitModel := chatBrowser.snapshot()
	recordSelectedChatVisit(visitModel.CurrentTenantID, visitModel.CurrentUser, id)
	cancelChatSubscription()
	clearChatMediaCache()
	chatBrowser.releaseReactionRead()
	_, generation := chatBrowser.selectChatConversation(id)
	refreshChatRoute()
	go loadChannelTodo(cfg, id)
	go loadChannelWidgets(cfg, id)
	go loadChannelPoll(cfg, id)
	go func() {
		// However this read ends, the open ends with it, and anything typed
		// while it was in flight goes to the room that is now open -- in the
		// order it was typed. A return that left the open marked would queue
		// every later send in silence.
		defer func() {
			for _, body := range chatBrowser.finishChatOpen(id, generation) {
				sendChatMessage(chatBrowser.config(cfg), id, body)
			}
		}()
		client := chatBrowser.conversationClient()
		if client == nil {
			return
		}
		active := chatBrowser.config(cfg)
		callCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		model := chatBrowser.snapshot()
		model.SelectedID = id
		var cursor chatCursor
		var err error
		if sequence > 0 && postID != "" {
			cursor, err = loadChatPosts(chatRPCContext(callCtx, active), client, active, &model, chatDirectorySnapshot(), chatSearchAnchor{sequence: sequence, postID: postID})
		} else {
			cursor, err = loadChatPosts(chatRPCContext(callCtx, active), client, active, &model, chatDirectorySnapshot())
		}
		if err != nil {
			chatBrowser.setLoadError(chatLoadFailureMessage("this conversation", err))
		} else {
			chatBrowser.setLoadError("")
			model.State = chatui.StateReady
			if len(model.Messages) == 0 {
				model.State = chatui.StateEmpty
			}
		}
		committed := chatBrowser.commit(generation, func(current *chatui.Model) {
			if current.SelectedID != id {
				return
			}
			current.Messages, current.HasOlder, current.HasNewer = model.Messages, model.HasOlder, model.HasNewer
			current.ChannelPins = model.ChannelPins
			current.FocusMessageID = model.FocusMessageID
			current.ThreadMessages = model.ThreadMessages
			// The "New" line is decided by the load, before the open marked
			// the room read; the commit has to carry it or the timeline never
			// shows where the reader left off.
			current.UnreadFromID = model.UnreadFromID
			clearOpenRoomUnread(current)
			if err != nil {
				current.State, current.Error = chatui.StateError, chatLoadFailureMessage("this conversation", err)
				return
			}
			current.State, current.Error = model.State, ""
			chatBrowser.cursor = cursor
		})
		if committed && err == nil {
			subscribeChatConversation(id)
			go loadChatMembers(cfg)
			if postID != "" {
				ui.PostAsync(func() {
					current := chatBrowser.snapshot()
					if current.SelectedID == id && current.FocusMessageID == postID {
						chatui.FocusSearchMessage(postID)
					}
				})
			}
			for _, room := range chatBrowser.snapshot().Conversations {
				if room.ID == id && room.Kind == chatui.DirectMessage {
					go loadChatMembers(cfg)
					break
				}
			}
		}
		refreshChatRoute()
	}()
}

func openChatSearchHitInCurrentConversation(cfg journeyclient.Config, conversationID string, sequence uint64, postID string) {
	model := chatBrowser.snapshot()
	if model.SelectedID != conversationID || sequence == 0 || postID == "" {
		return
	}
	generation := chatBrowser.currentGeneration()
	client := chatBrowser.conversationClient()
	if client == nil {
		return
	}
	active := chatBrowser.config(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	model.SelectedID = conversationID
	_, err := loadChatPosts(chatRPCContext(ctx, active), client, active, &model, chatDirectorySnapshot(), chatSearchAnchor{sequence: sequence, postID: postID})
	if err != nil {
		if chatBrowser.generationActive(generation) && chatBrowser.selectedID() == conversationID {
			noteChatAction(productui.ResolveProductLocale(active.Locale).Text(chatui.KeySearchMessageUnavailable))
		}
		return
	}
	committed := chatBrowser.commit(generation, func(current *chatui.Model) {
		if current.SelectedID != conversationID {
			return
		}
		current.Messages, current.HasOlder, current.HasNewer = model.Messages, model.HasOlder, true
		current.FocusMessageID = postID
		current.State, current.Error = chatui.StateReady, ""
	})
	if committed {
		refreshChatRoute()
		ui.PostAsync(func() {
			current := chatBrowser.snapshot()
			if current.SelectedID == conversationID && current.FocusMessageID == postID {
				chatui.FocusSearchMessage(postID)
			}
		})
	}
}

// addChatReaction puts one emoji on a post for the viewer and reflects it on
// the chip row without waiting for the stream to echo it.
func addChatReaction(cfg journeyclient.Config, postID, emoji string) {
	client := chatBrowser.conversationClient()
	if client == nil || postID == "" || emoji == "" {
		return
	}
	active := chatBrowser.config(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := client.AddReaction(chatRPCContext(ctx, active), &chatv1.AddReactionRequest{
		Reaction: &chatv1.Reaction{TenantId: active.Tenant, ConversationId: chatBrowser.selectedID(), PostId: postID, SubjectId: active.Subject, Emoji: emoji},
	})
	if chatActionFailed("add your reaction", err) {
		return
	}
	chatBrowser.adjustReaction(postID, emoji, 1, true)
	refreshChatRoute()
}

// copyChatMessageLink asks the server for a locator, then copies an app URL
// that can be opened directly. The source room is captured at click time.
func copyChatMessageLink(postID string) {
	if postID == "" {
		return
	}
	source := chatBrowser.snapshot()
	if source.SelectedID == "" {
		return
	}
	found := false
	for _, message := range source.Messages {
		if message.ID == postID {
			found = true
			break
		}
	}
	for _, pin := range source.ChannelPins {
		if pin.PostID == postID {
			found = true
			break
		}
	}
	if !found {
		return
	}
	active := chatBrowser.config(journeyclient.Config{})
	if source.PinReferenceUnavailable {
		return
	}
	client := chatBrowser.conversationClient()
	if client == nil || active.Bearer == "" {
		chatActionFailed("copy the link", errChatClipboard)
		return
	}
	location := js.Global().Get("location")
	navigator := js.Global().Get("navigator")
	if !location.Truthy() || !navigator.Truthy() || !navigator.Get("clipboard").Truthy() {
		chatActionFailed("copy the link", errChatClipboard)
		return
	}
	origin := location.Get("origin").String()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		result, err := client.CreateShareLink(chatRPCContext(ctx, active), &chatv1.CreateShareLinkRequest{TenantId: active.Tenant, ConversationId: source.SelectedID, PostId: postID})
		if err != nil {
			chatActionFailed("copy the link", err)
			return
		}
		if active.Tenant != chatBrowser.config(journeyclient.Config{}).Tenant || active.Subject != chatBrowser.config(journeyclient.Config{}).Subject {
			return
		}
		locators := chatui.ShareLocators(result.GetLink().GetUrl(), origin)
		if len(locators) != 1 || locators[0].Token == "" {
			chatActionFailed("copy the link", errChatClipboard)
			return
		}
		link := origin + "/workspace/app/chat#share=" + locators[0].Token
		copyChatLinkToClipboard(navigator, link)
	}()
}

func copyChatLinkToClipboard(navigator js.Value, link string) {
	promise := navigator.Get("clipboard").Call("writeText", link)
	var onDone, onFail js.Func
	onDone = js.FuncOf(func(js.Value, []js.Value) any {
		onDone.Release()
		onFail.Release()
		chatActionSucceeded("Link copied")
		refreshChatRoute()
		return nil
	})
	onFail = js.FuncOf(func(js.Value, []js.Value) any {
		onDone.Release()
		onFail.Release()
		chatActionFailed("copy the link", errChatClipboard)
		return nil
	})
	promise.Call("then", onDone, onFail)
}

func chatCopyableBody(model chatui.Model, postID string) (string, bool) {
	if model.SelectedID == "" || postID == "" {
		return "", false
	}
	for _, message := range model.Messages {
		if message.ID == postID && strings.TrimSpace(message.Body) != "" {
			return message.Body, true
		}
	}
	if model.ShowThread {
		if model.ThreadParent != nil && model.ThreadParentID == postID && model.ThreadParent.ID == postID && strings.TrimSpace(model.ThreadParent.Body) != "" {
			return model.ThreadParent.Body, true
		}
		for _, message := range model.ThreadMessages {
			if message.ID == postID && strings.TrimSpace(message.Body) != "" {
				return message.Body, true
			}
		}
	}
	return "", false
}

func copyChatMessageContents(postID string) {
	model := chatBrowser.snapshot()
	body, ok := chatCopyableBody(model, postID)
	if !ok {
		return
	}
	active := chatBrowser.config(journeyclient.Config{})
	if active.Tenant == "" || active.Subject == "" || active.Bearer == "" {
		return
	}
	copyChatContentsToClipboard(js.Global().Get("navigator"), body, active, model.SelectedID)
}

func copyChatContentsToClipboard(navigator js.Value, body string, active journeyclient.Config, room string) {
	locale := productui.ResolveProductLocale(active.Locale)
	valid := func() bool {
		current := chatBrowser.config(journeyclient.Config{})
		return current.Tenant == active.Tenant && current.Subject == active.Subject && chatBrowser.snapshot().SelectedID == room
	}
	failure := func() {
		if valid() {
			noteChatAction(locale.Text(chatui.KeyCopyContentsFailure))
		}
	}
	defer func() {
		if recover() != nil {
			failure()
		}
	}()
	if !navigator.Truthy() || !navigator.Get("clipboard").Truthy() || navigator.Get("clipboard").Get("writeText").Type() != js.TypeFunction {
		failure()
		return
	}
	// Write during the click's user activation; a later goroutine or RPC would
	// lose clipboard permission in browsers that require a direct gesture.
	promise := navigator.Get("clipboard").Call("writeText", body)
	var onDone, onFail js.Func
	onDone = js.FuncOf(func(js.Value, []js.Value) any {
		onDone.Release()
		onFail.Release()
		if valid() {
			chatActionSucceeded(locale.Text(chatui.KeyCopyContentsSuccess))
		}
		return nil
	})
	onFail = js.FuncOf(func(js.Value, []js.Value) any {
		onDone.Release()
		onFail.Release()
		failure()
		return nil
	})
	promise.Call("then", onDone, onFail)
}

func chatConversationReferenceFormats(origin, id, label string) (string, string, bool) {
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil || id == "" || !strings.HasPrefix(label, "#") {
		return "", "", false
	}
	href := origin + chatui.ChannelReferenceURL(id)
	return label + " " + href, `<a href="` + html.EscapeString(href) + `">` + html.EscapeString(label) + `</a>`, true
}

func copyChatConversationReference(id, label string) {
	model := chatBrowser.snapshot()
	known := false
	for _, conversation := range model.Conversations {
		if conversation.ID == id {
			known = true
			break
		}
	}
	if !known {
		return
	}
	active := chatBrowser.config(journeyclient.Config{})
	if active.Tenant == "" || active.Subject == "" || active.Bearer == "" {
		return
	}
	location := js.Global().Get("location")
	if !location.Truthy() {
		return
	}
	plain, rich, ok := chatConversationReferenceFormats(location.Get("origin").String(), id, label)
	if !ok {
		return
	}
	copyChatConversationReferenceToClipboard(js.Global().Get("navigator"), plain, rich, active)
}

func copyChatConversationReferenceToClipboard(navigator js.Value, plain, rich string, active journeyclient.Config) {
	locale := productui.ResolveProductLocale(active.Locale)
	valid := func() bool {
		current := chatBrowser.config(journeyclient.Config{})
		return current.Tenant == active.Tenant && current.Subject == active.Subject
	}
	failure := func() {
		if valid() {
			noteChatAction(locale.Text(chatui.KeyCopyConversationFailure))
		}
	}
	defer func() {
		if recover() != nil {
			failure()
		}
	}()
	if !navigator.Truthy() || !navigator.Get("clipboard").Truthy() {
		failure()
		return
	}
	clipboard := navigator.Get("clipboard")
	var promise js.Value
	itemClass, blobClass := js.Global().Get("ClipboardItem"), js.Global().Get("Blob")
	if clipboard.Get("write").Type() == js.TypeFunction && itemClass.Type() == js.TypeFunction && blobClass.Type() == js.TypeFunction {
		plainBlob := blobClass.New([]any{plain}, map[string]any{"type": "text/plain"})
		richBlob := blobClass.New([]any{rich}, map[string]any{"type": "text/html"})
		item := itemClass.New(map[string]any{"text/plain": plainBlob, "text/html": richBlob})
		promise = clipboard.Call("write", []any{item})
	} else if clipboard.Get("writeText").Type() == js.TypeFunction {
		promise = clipboard.Call("writeText", plain)
	} else {
		failure()
		return
	}
	var onDone, onFail js.Func
	onDone = js.FuncOf(func(js.Value, []js.Value) any {
		onDone.Release()
		onFail.Release()
		if valid() {
			chatActionSucceeded(locale.Text(chatui.KeyCopyConversationSuccess))
		}
		return nil
	})
	onFail = js.FuncOf(func(js.Value, []js.Value) any {
		onDone.Release()
		onFail.Release()
		failure()
		return nil
	})
	promise.Call("then", onDone, onFail)
}

// errChatClipboard is the one failure copying a link can have in a browser.
var errChatClipboard = chatMediaError("the clipboard is not available")

// chatUnreadFrom is the first message the rail said was unread when the room
// opened: the room's unread count, walked back from the newest message. It is
// computed before the open marks the room read, so the "New" line survives
// the read receipt and goes away only when the reader leaves.
func chatUnreadFrom(conversations []chatui.Conversation, selected string, messages []chatui.Message) string {
	unread := 0
	for _, conversation := range conversations {
		if conversation.ID == selected {
			unread = conversation.Unread
			break
		}
	}
	if unread <= 0 || unread >= len(messages) {
		return ""
	}
	return messages[len(messages)-unread].ID
}

func setChatPinned(model *chatui.Model, postID string, pinned bool) {
	if !pinned && model.ChannelTodoSourcePin == postID {
		model.ChannelTodoSourcePin = ""
	}
	for i := range model.ChannelPins {
		if model.ChannelPins[i].PostID == postID {
			if !pinned {
				model.ChannelPins = append(model.ChannelPins[:i], model.ChannelPins[i+1:]...)
			}
			break
		}
	}
	for i := range model.Messages {
		if model.Messages[i].ID == postID {
			model.Messages[i].Pinned = pinned
			if pinned {
				found := false
				for _, pin := range model.ChannelPins {
					if pin.PostID == postID {
						found = true
						break
					}
				}
				if !found {
					model.ChannelPins = append(model.ChannelPins, chatui.ChannelPin{PostID: postID, Author: model.Messages[i].Author, Body: model.Messages[i].Body, Sequence: model.Messages[i].Sequence})
				}
			}
			break
		}
	}
}

// refreshChatThread re-reads the replies under one root.
func refreshChatThread(ctx context.Context, client chatv1.ConversationServiceClient, cfg journeyclient.Config, conversation, root string) {
	if conversation == "" || root == "" {
		return
	}
	generation := chatBrowser.currentGeneration()
	result, err := client.ListPosts(ctx, &chatv1.ListPostsRequest{TenantId: cfg.Tenant, ConversationId: conversation, PageSize: chatPageSize, Descending: true})
	if err != nil {
		chatBrowser.commit(generation, func(model *chatui.Model) {
			if model.SelectedID == conversation && model.ShowThread && model.ThreadParentID == root {
				model.ThreadLoading = false
			}
		})
		chatActionFailed("load this thread", err)
		return
	}
	chatBrowser.applyThreadPage(conversation, root, generation, chatThreadLatest, result.GetPosts(), result.GetNextCursor(), cfg.Locale, chatDirectorySnapshot(), time.Now())
}

// loadChatMembers reads the membership projection and resolves display names.
//
// Membership carries no presence, and there is no presence signal anywhere in
// the chat protos or server. Online therefore stays false and Subtitle stays
// empty: a member list that prints "Offline" next to the reader's own name is
// stating a fact nobody measured.
func loadChatMembers(cfg journeyclient.Config) {
	client := chatBrowser.conversationClient()
	if client == nil {
		return
	}
	active := chatBrowser.config(cfg)
	conversation := chatBrowser.selectedID()
	if conversation == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := client.ListMemberships(chatRPCContext(ctx, active), &chatv1.ListMembershipsRequest{TenantId: channelTodoHost(conversation, active.Tenant), ConversationId: conversation, PageSize: 100})
	if chatActionFailed("load the member list", err) {
		return
	}
	current := chatBrowser.config(journeyclient.Config{})
	if current.Tenant != active.Tenant || current.Subject != active.Subject || current.Bearer != active.Bearer {
		return
	}
	directory := chatDirectorySnapshot()
	peer := chatDMPeerFromMemberships(result.GetMemberships(), active.Subject)
	members := make([]chatui.Member, 0, len(result.GetMemberships()))
	canPinTodo := false
	for _, room := range chatBrowser.snapshot().Conversations {
		if room.ID == conversation && room.OwnerID == active.Subject {
			canPinTodo = true
			break
		}
	}
	for _, membership := range result.GetMemberships() {
		if membership == nil || membership.GetLeftAt() != nil {
			continue
		}
		homeTenantID := membership.GetHomeTenantId()
		if homeTenantID == "" {
			homeTenantID = channelTodoHost(conversation, active.Tenant)
		}
		members = append(members, chatui.Member{ID: membership.GetSubjectId(), HomeTenantID: homeTenantID, Name: chatMembershipDisplayName(directory, membership.GetSubjectId(), homeTenantID, active.Tenant)})
		if membership.GetSubjectId() == active.Subject && membership.GetRole() == chatv1.MembershipRole_MEMBERSHIP_ROLE_MANAGER {
			canPinTodo = true
		}
	}
	chatBrowser.mutate(func(model *chatui.Model) {
		if model.SelectedID == conversation {
			model.Members = members
			model.CanPinChannelTodo = canPinTodo
		}
		for i := range model.Conversations {
			if model.Conversations[i].ID == conversation {
				if model.Conversations[i].Kind == chatui.DirectMessage {
					if model.PeerIDs == nil {
						model.PeerIDs = make(map[string]string)
					}
					delete(model.PeerIDs, conversation)
					if peer != "" {
						model.PeerIDs[conversation] = peer
					}
				}
				// The conversation messages carry no member count, so the
				// header's "N members" comes from the list the details pane
				// just read rather than from a number nobody published.
				model.Conversations[i].MemberCount = len(members)
				break
			}
		}
	})
	chatBrowser.applyChatDirectory(directory)
	ui.PostAsync(func() {
		if chatRerender != nil {
			refreshChatRoute()
		}
	})
}

// saveChatNotificationPreference is the reachable UpdatePreferences path.
//
// UpdatePreferences refuses a zero expected_revision, and the revision only
// comes from GetPreferences, so the read is part of the write. A conversation
// with no stored row reads back revision 1, which is the insert branch.
func saveChatNotificationPreference(cfg journeyclient.Config, prefs chatui.Preferences) {
	conversation := chatBrowser.selectedID()
	if conversation == "" {
		return
	}
	saveChatNotificationMode(cfg, conversation, prefs.Notifications[conversation])
}

func saveChatNotificationMode(cfg journeyclient.Config, conversation string, mode chatui.NotificationMode) {
	if conversation == "" || (mode != chatui.NotifyAll && mode != chatui.NotifyMention && mode != chatui.NotifyMute) {
		return
	}
	go func() {
		client := chatBrowser.conversationClient()
		if client == nil {
			return
		}
		active := chatBrowser.config(cfg)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		callCtx := chatRPCContext(ctx, active)
		current, err := client.GetPreferences(callCtx, &chatv1.GetPreferencesRequest{TenantId: active.Tenant, ConversationId: conversation})
		if chatActionFailed("read your notification settings", err) {
			return
		}
		revision := current.GetPreferences().GetRevision()
		if revision == 0 {
			revision = 1
		}
		updated, err := client.UpdatePreferences(callCtx, &chatv1.UpdatePreferencesRequest{
			Preferences: &chatv1.NotificationPreferences{
				TenantId: active.Tenant, ConversationId: conversation, SubjectId: active.Subject,
				Muted: mode == chatui.NotifyMute, MentionsOnly: mode == chatui.NotifyMention,
				HomeTenantId: current.GetPreferences().GetHomeTenantId(),
			},
			ExpectedRevision: revision,
		})
		if chatActionFailed("save your notification settings", err) {
			return
		}
		_ = updated
		chatBrowser.mutate(func(model *chatui.Model) {
			if model.Preferences.Notifications == nil {
				model.Preferences.Notifications = map[string]chatui.NotificationMode{}
			}
			model.Preferences.Notifications[conversation] = mode
			for i := range model.Conversations {
				if model.Conversations[i].ID == conversation {
					model.Conversations[i].Muted = mode == chatui.NotifyMute
				}
			}
			for i := range model.Sections {
				for j := range model.Sections[i].Chats {
					if model.Sections[i].Chats[j].ID == conversation {
						model.Sections[i].Chats[j].Muted = mode == chatui.NotifyMute
					}
				}
			}
			if model.RailMenuID == conversation {
				model.RailMenuID = ""
			}
		})
		chatActionSucceeded("Notification settings saved")
		refreshChatRoute()
	}()
}

func loadChatNotificationMode(cfg journeyclient.Config, conversation string) {
	client := chatBrowser.conversationClient()
	if client == nil {
		return
	}
	active := chatBrowser.config(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	current, err := client.GetPreferences(chatRPCContext(ctx, active), &chatv1.GetPreferencesRequest{TenantId: active.Tenant, ConversationId: conversation})
	if err != nil {
		return
	}
	mode := chatui.NotifyAll
	if current.GetPreferences().GetMuted() {
		mode = chatui.NotifyMute
	} else if current.GetPreferences().GetMentionsOnly() {
		mode = chatui.NotifyMention
	}
	chatBrowser.mutate(func(model *chatui.Model) {
		if model.RailMenuID != conversation {
			return
		}
		if model.Preferences.Notifications == nil {
			model.Preferences.Notifications = map[string]chatui.NotificationMode{}
		}
		model.Preferences.Notifications[conversation] = mode
	})
	refreshChatRoute()
}
