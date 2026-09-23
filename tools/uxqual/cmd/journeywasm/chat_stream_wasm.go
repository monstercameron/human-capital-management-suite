//go:build js && wasm

package main

import (
	"context"
	"sync"
	"time"

	"google.golang.org/grpc/status"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// This file is the chat surface's live half: one WatchConversation
// subscription for the conversation the reader is looking at, and nothing
// else.
//
// The shape it is written against is the one the spec describes
// (planning/specs/company-chat-and-collaboration.md, "WatchConversation
// begins from an authorized snapshot or a signed resumable cursor ... On a
// slow consumer or sequence gap, the server closes with a catch-up cursor;
// the client fetches bounded history and resumes"):
//
//	rpc WatchConversation(WatchConversationRequest) returns (stream WatchConversationResponse)
//
// with after_sequence / resume_cursor on the request and one event plus a
// resume cursor per response. Events are applied in sequence order, deduped
// by post id and sequence, and a gap is never rendered: it sends the client
// to ListPosts after the last applied sequence and then back to a fresh
// subscription.
//
// Re-renders are coalesced. A replay after a reconnect can deliver hundreds
// of events in a few milliseconds, and one route revalidation per event would
// be the same storm the composer used to cause.

const (
	// chatStreamRetryBase is the first wait after a stream fails to open or
	// ends with nothing delivered. It doubles up to chatStreamRetryMax.
	chatStreamRetryBase = 2 * time.Second
	// chatStreamRetryMax bounds the backoff so a page left open against a
	// refusing cell retries at a human pace rather than never again.
	chatStreamRetryMax = 30 * time.Second
	// chatStreamRenderDebounce coalesces the re-renders a burst of events
	// would otherwise cause.
	chatStreamRenderDebounce = 120 * time.Millisecond
	// chatStreamGapBudget bounds how many times in a row a gap may resubscribe
	// without waiting. A gap is normally one catch-up and then a healthy
	// stream; a server that keeps reporting one must not be asked again as
	// fast as the loop can run.
	chatStreamGapBudget = 3
)

// chatStreamRender coalesces stream-driven re-renders.
var chatStreamRender = newRefreshCoalescer(browserDebounceScheduler, chatStreamRenderDebounce, func() error {
	refreshChatRoute()
	return nil
})

// subscribeChatConversation opens the live feed for one conversation and
// cancels whatever was open before it.
func subscribeChatConversation(conversationID string) {
	if conversationID == "" {
		return
	}
	generation := chatBrowser.currentGeneration()
	ctx, cancel := context.WithCancel(context.Background())
	previous, started := chatBrowser.swapSubscription(generation, conversationID, cancel)
	if previous != nil {
		// The one line a live run needs: who ended a stream the server was
		// serving. Every CANCELED watch in the log that had no server cause
		// was this call, and it says so now.
		logChatStream(chatStreamEvent{Conversation: conversationID, Event: "replaced", Cause: "resubscribe"})
		previous()
	}
	if !started {
		cancel()
		logChatStream(chatStreamEvent{Conversation: conversationID, Event: "subscribe-skipped", Cause: "already-live-or-superseded"})
		return
	}
	logChatStream(chatStreamEvent{Conversation: conversationID, Event: "subscribe"})
	go watchChatConversation(ctx, conversationID, generation)
}

// cancelChatSubscription ends the current feed. A conversation switch calls it
// before the new read starts, so at most one stream is ever open and a late
// event from the conversation the reader left cannot reach the model.
func cancelChatSubscription() {
	if stop := chatBrowser.takeSubscription(); stop != nil {
		logChatStream(chatStreamEvent{Event: "cancelled", Cause: "switch"})
		stop()
	}
}

// watchChatConversation runs one conversation's feed until the reader leaves
// it, the page ends, or the cell stops answering.
func watchChatConversation(ctx context.Context, conversationID string, generation uint64) {
	attempts, gaps := 0, 0
	backoff := chatStreamRetryBase
	// firstFailure is when the current run of failures started. The notice is
	// not worth showing for a blip, so it waits on elapsed time as well as on
	// attempts, and any productive round clears it.
	var firstFailure time.Time
	// Whatever ends this watch, its registration goes with it. Leaving one
	// behind would tell the route loader a dead stream is keeping the timeline
	// current.
	defer func() {
		if stop := chatBrowser.releaseSubscription(generation, conversationID); stop != nil {
			stop()
		}
	}()
	for {
		if chatStreamEnded(ctx, generation, conversationID) {
			return
		}
		client := chatBrowser.conversationClient()
		if client == nil {
			return
		}
		cfg := chatBrowser.config(journeyclient.Config{})
		openedAt := time.Now()
		afterSequence, resume := chatWatchStart(chatBrowser.streamCursor())
		logChatStream(chatStreamEvent{Conversation: conversationID, Event: "open", Attempt: attempts, Backoff: backoff})
		stream, err := client.WatchConversation(chatRPCContext(ctx, cfg), &chatv1.WatchConversationRequest{
			TenantId:       cfg.Tenant,
			ConversationId: conversationID,
			AfterSequence:  afterSequence,
			ResumeCursor:   resume,
		})
		if chatStreamEnded(ctx, generation, conversationID) {
			return
		}
		if err != nil {
			logChatStream(chatStreamEvent{Conversation: conversationID, Event: "open-failed", Cause: chatStreamCause(err), Attempt: attempts + 1})
			if resume != "" && chatCursorRejected(err) {
				// The server refused the resume cursor. It is a stale token,
				// not an outage: drop it and resubscribe from the sequence at
				// once rather than spending a retry and a backoff on it.
				chatBrowser.dropResumeCursor(conversationID)
				continue
			}
			attempts++
			if firstFailure.IsZero() {
				firstFailure = time.Now()
			}
			if chatStreamShouldGiveUp(attempts, firstFailure, time.Now()) {
				chatStreamGaveUp(conversationID, attempts)
				return
			}
			if !chatStreamWait(ctx, &backoff) {
				return
			}
			continue
		}

		delivered, reason, termination := receiveChatEvents(ctx, stream, conversationID, generation, cfg)
		gap := reason == chatStreamEndGap
		if chatStreamEnded(ctx, generation, conversationID) {
			return
		}
		// Only a stream that actually worked says the stream is healthy.
		// Resetting the counter because a *catch-up* read succeeded is what
		// turned a refusing cell into 702 WatchConversation opens and 763
		// ListPosts reads in two minutes: every round failed, every round
		// caught up, and the backoff was reset every time.
		//
		// A quiet stream that the server closed at its lifetime ceiling is not
		// a failure either. There is no heartbeat, so a conversation nobody
		// posted in for fifteen minutes delivers nothing and then ends
		// normally; counting that as an outage is what said a healthy server
		// had stopped updating.
		rollover := chatStreamRollover(termination, time.Since(openedAt), ctx.Err() == nil)
		// A gap is work done, not a failure: the catch-up below reads what was
		// missed and the resubscribe carries on. Counting it was half of why a
		// room with history reached the give-up in under a second.
		if delivered || gap {
			attempts, backoff = 0, chatStreamRetryBase
			if delivered {
				gaps = 0
			}
		} else if !rollover {
			attempts++
			if firstFailure.IsZero() {
				firstFailure = time.Now()
			}
		}
		if delivered || gap || rollover {
			firstFailure = time.Time{}
		}
		// A rollover keeps the backoff where it is: not counting it toward the
		// give-up is what keeps a rotating transport live, and resetting the
		// wait as well would put it back into a tight reconnect loop.
		logChatStream(chatStreamEvent{
			Conversation: conversationID, Event: "ended", Cause: reason.String() + "/" + chatStreamCause(termination),
			Attempt: attempts, Backoff: backoff, Rollover: rollover,
		})
		// A catch-up is worth doing only when the stream was working: it
		// reconciles what the gap or the ceiling hid. After a stream that
		// never delivered there is nothing between it and the last load.
		if delivered || gap {
			catchUpChatConversation(ctx, conversationID, generation, cfg)
		}
		if chatStreamShouldGiveUp(attempts, firstFailure, time.Now()) {
			chatStreamGaveUp(conversationID, attempts)
			return
		}
		if gap {
			gaps++
			if gaps <= chatStreamGapBudget {
				// A gap is not an outage: resubscribe at once from the
				// sequence the catch-up established.
				continue
			}
		}
		if !chatStreamWait(ctx, &backoff) {
			return
		}
	}
}

// chatStreamGaveUp tells the reader the conversation has stopped updating.
//
// It only says so; watchChatConversation's deferred release is what clears the
// registration, and that matters as much as the notice: while a stream is
// registered the route loader trusts it and does not re-read the timeline, so a
// dead stream left registered would leave the conversation frozen and silent.
func chatStreamGaveUp(conversationID string, attempts int) {
	logChatStream(chatStreamEvent{Conversation: conversationID, Event: "gave-up", Attempt: attempts})
	noteChatActionWithRetry("This conversation is no longer updating live.")
}

// chatStreamCause names why a stream ended, for the log only. It is the
// transport's own words, which is exactly what reader-facing copy must not be.
func chatStreamCause(err error) string {
	if err == nil {
		return "clean"
	}
	return status.Code(err).String()
}

// receiveChatEvents drains one stream, doing the work each event implies.
//
// The loop itself is drainChatStream (chat_stream_drain.go); this is the half
// that touches the browser.
func receiveChatEvents(ctx context.Context, stream chatv1.ConversationService_WatchConversationClient, conversationID string, generation uint64, cfg journeyclient.Config) (delivered bool, reason chatStreamEnd, termination error) {
	directory := chatDirectorySnapshot()
	var soundModel chatui.Model
	soundCandidate := false
	installChatSoundUnlock()
	return drainChatStream(ctx, stream, chatStreamHooks{
		Apply: func(event *chatv1.ConversationEvent, resume string) (chatEventOutcome, bool) {
			soundModel = chatBrowser.snapshot()
			soundCandidate = event.GetKind() == chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_POST_CREATED && chatPostNewerThanLoadedTimeline(soundModel, event.GetPost())
			outcome, active := chatBrowser.applyStreamEvent(generation, conversationID, event, cfg.Locale, directory, time.Now(), resume)
			soundCandidate = soundCandidate && active && (outcome == chatEventApplied || outcome == chatEventUnordered)
			return outcome, active
		},
		Applied: func(event *chatv1.ConversationEvent) {
			switch event.GetKind() {
			case chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_MEMBERSHIP_CHANGED:
				chatBrowser.invalidateChatEmbeds()
				// Membership changed under the reader: the member list they may
				// be looking at, and their own access, are both now stale.
				go loadChatMembers(cfg)
			case chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_POST_CREATED:
				// The rail's unread and mention counts are the server's, not a
				// number this client increments. Drop the cached counts so the
				// next projection re-reads GetCounts.
				invalidateChatRecipientProjection()
				post := event.GetPost()
				markChatRead(cfg, conversationID, []*chatv1.Post{post})
				claimedSound := soundCandidate && post != nil && claimChatSoundSequence(conversationID, post.GetSequence())
				if post != nil {
					noteChatSoundSequence(conversationID, post.GetSequence())
				}
				if claimedSound {
					playChatSoundIfAllowed(soundModel, conversationID, post, time.Now())
				}
				if created := post.GetCreatedAt(); created != nil {
					// The rail orders by newest message, so a delivered post
					// moves its room to the top.
					chatBrowser.noteChatActivity(conversationID, created.AsTime())
				}
			}
			if len(event.GetPost().GetReferences()) > 0 {
				go resolveChatMedia(cfg, conversationID)
			}
			chatStreamRender.Schedule()
		},
	})
}

// catchUpChatConversation reads forward from the last applied sequence and
// folds the result in. It is the gap recovery and the between-streams
// reconciliation, and it reports whether it applied anything.
func catchUpChatConversation(ctx context.Context, conversationID string, generation uint64, cfg journeyclient.Config) bool {
	client := chatBrowser.conversationClient()
	if client == nil {
		return false
	}
	cursor := chatBrowser.streamCursor()
	if cursor.ConversationID != conversationID {
		return false
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	applied := false
	pageCursor := ""
	for page := 0; page < chatCatchupPages; page++ {
		result, err := client.ListPosts(chatRPCContext(callCtx, cfg), &chatv1.ListPostsRequest{
			TenantId: cfg.Tenant, ConversationId: conversationID, AfterSequence: cursor.LastSequence, PageSize: chatPageSize, Cursor: pageCursor,
		})
		if err != nil {
			return false
		}
		posts := result.GetPosts()
		if len(posts) > 0 && chatBrowser.applyCatchup(generation, conversationID, posts, cfg.Locale, chatDirectorySnapshot(), time.Now()) {
			applied = true
			markChatRead(cfg, conversationID, posts)
		}
		pageCursor = result.GetNextCursor()
		if pageCursor == "" {
			break
		}
	}
	if applied {
		invalidateChatRecipientProjection()
		chatStreamRender.Schedule()
	}
	return applied
}

// chatStreamEnded reports whether this subscription has been superseded.
func chatStreamEnded(ctx context.Context, generation uint64, conversationID string) bool {
	return ctx.Err() != nil || !chatBrowser.generationActive(generation) || chatBrowser.selectedID() != conversationID
}

// chatStreamWait sleeps the current backoff and doubles it, reporting false
// when the subscription ended first.
func chatStreamWait(ctx context.Context, backoff *time.Duration) bool {
	wait := *backoff
	if wait <= 0 {
		wait = chatStreamRetryBase
	}
	if next := wait * 2; next <= chatStreamRetryMax {
		*backoff = next
	} else {
		*backoff = chatStreamRetryMax
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// loadOlderChatMessages moves the fixed retained window toward older posts.
func loadOlderChatMessages(cfg journeyclient.Config) {
	conversationID, cursor, fence, ok := chatBrowser.claimTimelinePage(false)
	if !ok {
		return
	}
	go func() {
		defer chatBrowser.finishTimelinePage(fence)
		client := chatBrowser.conversationClient()
		if client == nil {
			return
		}
		active := chatBrowser.config(cfg)
		request := &chatv1.ListPostsRequest{
			TenantId: active.Tenant, ConversationId: conversationID, PageSize: chatPageSize, Descending: true,
		}
		if cursor != "" {
			request.Cursor = cursor
		} else {
			request.BeforeSequence = fence.edge
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result, err := client.ListPosts(chatRPCContext(ctx, active), request)
		if !chatBrowser.generationActive(fence.generation) || active.Tenant != chatBrowser.config(cfg).Tenant || active.Subject != chatBrowser.config(cfg).Subject {
			return
		}
		if chatActionFailed("load earlier messages", err) {
			return
		}
		posts := chatInConversationOrder(result.GetPosts())
		older := chatMessages(posts, active.Locale, chatDirectorySnapshot(), chatBrowser.reactionSnapshot(), time.Now())
		// A prepend, not a reload: the reader stays where they are and the
		// stream keeps delivering. HasOlder follows the server's cursor.
		if chatBrowser.prependOlderChatMessages(conversationID, older, result.GetNextCursor(), fence) {
			refreshChatRoute()
			queuePagedChatReactions(active, conversationID, fence.generation, older)
		}
	}()
}

func loadNewerChatMessages(cfg journeyclient.Config) {
	conversationID, _, fence, ok := chatBrowser.claimTimelinePage(true)
	if !ok {
		return
	}
	go func() {
		defer chatBrowser.finishTimelinePage(fence)
		client := chatBrowser.conversationClient()
		active := chatBrowser.config(cfg)
		if client == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result, err := client.ListPosts(chatRPCContext(ctx, active), &chatv1.ListPostsRequest{TenantId: active.Tenant, ConversationId: conversationID, AfterSequence: fence.edge, PageSize: chatPageSize})
		if !chatBrowser.generationActive(fence.generation) || active.Tenant != chatBrowser.config(cfg).Tenant || active.Subject != chatBrowser.config(cfg).Subject {
			return
		}
		if chatActionFailed("load newer messages", err) {
			return
		}
		messages := chatMessages(chatInConversationOrder(result.GetPosts()), active.Locale, chatDirectorySnapshot(), chatBrowser.reactionSnapshot(), time.Now())
		scannedMax := chatLastSequence(result.GetPosts())
		if chatBrowser.appendNewerChatMessages(conversationID, messages, result.GetNextCursor(), scannedMax, fence) {
			refreshChatRoute()
			queuePagedChatReactions(active, conversationID, fence.generation, messages)
		}
	}()
}

func queuePagedChatReactions(cfg journeyclient.Config, conversation string, generation uint64, messages []chatui.Message) {
	if !chatBrowser.queuePagedReactionRead(generation, conversation, messages) {
		return
	}
	go func() {
		client := chatBrowser.conversationClient()
		if client == nil {
			chatBrowser.finishPagedReactionRead(generation)
			return
		}
		for {
			ids := chatBrowser.takePagedReactionRead(generation, conversation)
			if len(ids) == 0 {
				return
			}
			if !chatBrowser.generationActive(generation) || cfg.Tenant != chatBrowser.config(cfg).Tenant || cfg.Subject != chatBrowser.config(cfg).Subject {
				chatBrowser.finishPagedReactionRead(generation)
				return
			}
			ctx, cancel := context.WithTimeout(chatRPCContext(context.Background(), cfg), 20*time.Second)
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
					result, err := client.ListReactions(ctx, &chatv1.ListReactionsRequest{TenantId: cfg.Tenant, ConversationId: conversation, PostId: id, PageSize: 100})
					if err != nil {
						return
					}
					mu.Lock()
					chips[id], members[id] = chatReactionSnapshot(result.GetReactions(), cfg.Subject)
					mu.Unlock()
				}()
			}
			wg.Wait()
			cancel()
			chatBrowser.completePagedReactionBatch(generation, ids)
			if !chatBrowser.generationActive(generation) || cfg.Tenant != chatBrowser.config(cfg).Tenant || cfg.Subject != chatBrowser.config(cfg).Subject {
				chatBrowser.finishPagedReactionRead(generation)
				return
			}
			if chatBrowser.applyPagedReactionSnapshot(generation, conversation, chips, members) {
				refreshChatRoute()
			}
		}
	}()
}

func jumpToNewestChatMessages(cfg journeyclient.Config) {
	if !chatBrowser.snapshot().HasNewer {
		chatui.ScrollToNewest()
		return
	}
	conversationID, fence, ok := chatBrowser.claimLatestPage()
	if !ok {
		return
	}
	go func() {
		defer chatBrowser.finishTimelinePage(fence)
		client := chatBrowser.conversationClient()
		active := chatBrowser.config(cfg)
		if client == nil || conversationID == "" {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result, err := client.ListPosts(chatRPCContext(ctx, active), &chatv1.ListPostsRequest{TenantId: active.Tenant, ConversationId: conversationID, PageSize: chatPageSize, Descending: true})
		if !chatBrowser.generationActive(fence.generation) || active.Tenant != chatBrowser.config(cfg).Tenant || active.Subject != chatBrowser.config(cfg).Subject {
			return
		}
		if err != nil {
			chatActionFailed("load newest messages", err)
			return
		}
		messages := chatMessages(chatInConversationOrder(result.GetPosts()), active.Locale, chatDirectorySnapshot(), chatBrowser.reactionSnapshot(), time.Now())
		if chatBrowser.replaceLatestChatMessages(conversationID, messages, result.GetNextCursor(), fence) {
			refreshChatRoute()
			chatui.ScrollToNewest()
			queuePagedChatReactions(active, conversationID, fence.generation, messages)
		}
	}()
}

func loadChatThreadPage(cfg journeyclient.Config, direction chatThreadPageDirection) {
	conversation, root, generation, edge, ok := chatBrowser.threadPageEdge(direction)
	if !ok {
		return
	}
	go func() {
		defer chatBrowser.finishThreadPage(generation)
		client := chatBrowser.conversationClient()
		active := chatBrowser.config(cfg)
		if client == nil {
			return
		}
		request := &chatv1.ListPostsRequest{TenantId: active.Tenant, ConversationId: conversation, PageSize: chatPageSize}
		if direction == chatThreadOlder {
			request.Descending, request.BeforeSequence = true, edge
		} else {
			request.AfterSequence = edge
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result, err := client.ListPosts(chatRPCContext(ctx, active), request)
		if !chatBrowser.generationActive(generation) || active.Tenant != chatBrowser.config(cfg).Tenant || active.Subject != chatBrowser.config(cfg).Subject {
			return
		}
		if chatActionFailed("load thread messages", err) {
			return
		}
		if chatBrowser.applyThreadPage(conversation, root, generation, direction, result.GetPosts(), result.GetNextCursor(), active.Locale, chatDirectorySnapshot(), time.Now(), edge) {
			refreshChatRoute()
		}
	}()
}
