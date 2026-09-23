// This file is the chat client's state, and none of it touches the browser.
//
// Everything the chat surface decides without a live connection lives here:
// which idempotency key one send attempt may reuse, whether a stream event is
// new, a duplicate or the far side of a gap, how long a transient notice
// stays on screen, which conversation's draft the composer is showing, and
// how a protobuf post becomes a rendered message. The wasm half
// (chat_wasm.go, chat_stream_wasm.go, chat_recipient_wasm.go) owns the RPCs,
// the goroutines and the route revalidation, and calls into this file for
// every state change.
//
// The split is not tidiness. The bugs this package had were all decisions --
// a key reused after the body changed, an error that never cleared, a stale
// snapshot restored over a newer selection -- and a decision that needs a
// browser to reproduce is a decision nobody tests.
//
// Locking: chatState.mu is the only lock over chat surface state, and it is
// never held across an RPC or across chatRecipientBrowser's lock. Recipient
// code snapshots its own state first, then calls the methods here.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

const (
	// chatMessageWindow bounds the timeline held in memory. A channel with
	// a year of history must not grow the model without limit as the stream
	// delivers; older pages come back through the ListPosts cursor.
	chatMessageWindow = 500
	// chatPageSize is one ListPosts page.
	chatPageSize = 200
	// chatNoticeTTL is how long a transient notice stays on screen before it
	// clears itself. An action's outcome is news for a few seconds; after
	// that it is furniture, and the latched error this replaces was furniture
	// that also hid the timeline.
	chatNoticeTTL = 4 * time.Second
	// chatReactionWindow bounds how many of the newest visible posts get a
	// reaction count read. There is no bulk reaction-count RPC (see the
	// report in chat_wasm.go), so the count is a bounded fan-out, not a
	// per-post call for a thousand-post channel.
	chatReactionWindow = 50
)

// chatSendAttempt is the idempotency key a conversation's pending send owns,
// together with the body that key was minted for.
//
// The body is the whole point. A key cached per conversation and cleared only
// on success replays the first body when a timed-out send is retried with an
// edited one: the server honours the key, returns the original post, and the
// reader's correction is silently discarded. A key belongs to a body, so a
// retry of the same text reuses it (that is what makes the retry safe) and
// any other text gets a new one.
type chatSendAttempt struct {
	Key  string
	Body string
}

// chatQueuedSend is a message typed before its room was ready.
//
// It names its room. A single shared queue was enough for one room opening at a
// time, and the seed opened three in a row: the second room's message was
// queued, a third Create claimed the open, and the second room's drain found
// the queue no longer its own and dropped it. The message never left the
// client -- six posts of seven, and a room with nothing in it.
//
// An empty Conversation means the reader typed before any room was open, which
// the first room to resolve takes.
type chatQueuedSend struct {
	Conversation string
	Body         string
}

// chatCursor is the position for one conversation, in the two different
// sequence spaces the server actually uses.
//
// They are not the same number, and treating them as one is what stopped the
// feed. LastSequence is the post sequence, which orders the timeline and
// addresses a ListPosts catch-up. StreamSequence is the watch position, which
// the store compares against its outbox row id
// (internal/data/chatstore/contracts_adapter.go:1209 builds each event as
// `ConversationEvent{Sequence: uint64(id)}` from `chat_outbox.id`, a counter
// shared by every conversation and every event kind). Seeding the watch from
// the post sequence made the first replayed event of a room with history look
// like a jump of thousands, so the client declared a gap, caught up,
// resubscribed, saw the same jump, and gave up five attempts later. A brand-new
// room replays nothing, which is why only rooms with history went quiet.
type chatCursor struct {
	ConversationID string
	// LastSequence is the highest post sequence already applied.
	LastSequence uint64
	// StreamSequence is the highest watch position already applied. It is only
	// ever learned from the stream itself.
	StreamSequence uint64
	// Baselined says the stream has delivered an ordered event, so the next one
	// can be compared against it. Until then there is nothing to compare: a gap
	// is a hole between two events on the same stream, never a difference
	// between the server's position and a number this client made up.
	Baselined bool
	// Resume is the server's opaque resume cursor from the last event.
	Resume string
	// Seen maps an applied post id to its post sequence, which is what
	// recognises a replayed event whatever watch position it arrives with.
	Seen map[string]uint64
}

// chatEventOutcome is what one stream event means for the cursor.
type chatEventOutcome int

const (
	// chatEventApplied means the event was the next one and was applied.
	chatEventApplied chatEventOutcome = iota
	// chatEventDuplicate means the event was already applied. Dropping it is
	// correct: at-least-once delivery and every resubscribe replay land here.
	chatEventDuplicate
	// chatEventGap means an event was missed. The caller catches up through
	// ListPosts after the last applied sequence and resubscribes.
	chatEventGap
	// chatEventUnordered means the event carries no sequence (membership and
	// conversation changes). Its payload is applied; the cursor does not move.
	chatEventUnordered
)

// chatState is the whole chat surface's state.
type chatState struct {
	mu sync.RWMutex

	client chatv1.ConversationServiceClient
	cfg    journeyclient.Config
	model  chatui.Model

	// callbacks rebuilds the callback table. Every model the UI is handed
	// must carry live callbacks, and the table is rebuilt rather than copied
	// because a stale closure captures a stale client.
	callbacks func() chatui.Callbacks

	// generation is bumped by every action that changes which conversation
	// the reader is looking at. A slower answer for an older generation is
	// discarded rather than allowed to restore the previous selection.
	generation uint64

	sendAttempts map[string]chatSendAttempt
	editDrafts   map[string]string
	drafts       map[string]string
	// draftRevs stamps each conversation's draft with the local edit that
	// produced it. A persisted copy read back from the server carries no
	// revision, so it is adopted only for a conversation this session has not
	// touched -- which is what stops a projection reload from restoring the
	// text a send just cleared.
	draftRevs  map[string]uint64
	draftRev   uint64
	draftEpoch uint64
	// directory maps a subject id to a display name for this session, and
	// directoryRead says the session's own read has happened.
	//
	// It used to be rebuilt from productui.View.People on every projection,
	// which made chat names a side effect of whether the People page had been
	// visited: an Arabic session and every second-persona session saw raw ids
	// ("hc-050-rafael-torres") because nothing had populated that projection.
	// Chat reads the worker directory itself now and keeps it for the session.
	directory           map[string]string
	photos              map[string]string
	workerPhotos        map[string]string
	dmPeerReads         map[string]bool
	directoryRead       bool
	directoryEpoch      uint64
	directoryRetryAfter time.Time
	// browseAll is the whole discoverable listing. Model.Browse is whatever
	// matches the typed query, so filtering costs no RPC.
	browseAll []chatui.Conversation
	// lastActivity is the newest post time this client has seen per
	// conversation, which is all the ordering signal there is: the wire
	// Conversation carries no last-post or updated-at field, so a room this
	// session has never opened has no activity time at all.
	lastActivity map[string]time.Time

	// loadErr is the last *load* failure: the conversation list, or the
	// selected conversation's posts. It is the only error allowed to replace
	// the timeline, and the next successful load clears it. Action failures
	// are notices (see notice) and never latch.
	loadErr string

	notice      string
	noticeToken uint64

	cursor chatCursor
	// olderCursor is the backward page cursor from the last descending
	// ListPosts. The server pages newest-first now, so older history is the
	// next backward page rather than a wider forward re-read.
	olderCursor          string
	newerScanSequence    uint64
	pageLoading          bool
	threadBeforeSequence uint64
	threadAfterSequence  uint64
	threadPaging         bool
	channelFragmentLast  string
	// reactions is the per-post reaction breakdown this session has read or
	// applied: one chip per emoji, with whether the viewer is on it.
	reactions map[string][]chatui.ReactionChip
	// reactionMembers tracks the individual memberships behind each count so
	// replaying an old stream event cannot count a reaction twice.
	reactionMembers map[string]map[chatReactionIdentity]struct{}
	// reactionsReady says the read for reactionsFor has landed.
	reactionsReady      bool
	reactionsFor        string
	reactionPageLoading bool
	reactionPagePending []string
	reactionPageReading map[string]bool
	pinRevisions        map[string]uint64
	window              int

	// cancelStream ends the current conversation's subscription and
	// streamFor names the conversation it belongs to. At most one stream is
	// ever open, and a route revalidation must not tear down and rebuild the
	// one that is already live.
	cancelStream     func()
	streamFor        string
	streamGeneration uint64

	// opening names the conversation whose first read is still in flight, and
	// queuedSends holds anything the reader typed and sent while it was.
	//
	// chatui addresses a send with the SelectedID of the model it last
	// rendered, so a message typed a moment after a room was created carried
	// the previous room's id, or none at all, and Model.Send dropped it. A
	// send is never dropped: it waits for the open and then goes to the room
	// the reader is actually in.
	opening                 string
	queuedSends             []chatQueuedSend
	shareGeneration         uint64
	shareAttemptKey         string
	shareAttemptDestination string
	shareAttemptKeys        map[string]string
	embedEntries            map[string]chatEmbedEntry
	embedTimerRunning       bool
	embedOrder              []string
	embedEpoch              uint64
	embedDraftSignature     string
	openedShareFragment     string
}

type chatReactionIdentity struct {
	homeTenantID string
	subjectID    string
	emoji        string
}

var chatBrowser chatState

// reset returns the state to its configured-but-empty shape. It is the one
// place that clears every latch, which is why nothing else may hold one.
func (s *chatState) reset(client chatv1.ConversationServiceClient, cfg journeyclient.Config, callbacks func() chatui.Callbacks) {
	s.mu.Lock()
	stop := s.cancelStream
	s.client = client
	s.cfg = cfg
	s.callbacks = callbacks
	s.model = chatui.Model{CurrentUser: cfg.Subject, CurrentTenantID: cfg.Tenant, Locale: cfg.Locale, Number: chatNumberFormatter(cfg.Locale)}
	s.sendAttempts = make(map[string]chatSendAttempt)
	s.editDrafts = make(map[string]string)
	s.drafts = make(map[string]string)
	s.draftRevs = make(map[string]uint64)
	s.draftRev = 0
	s.draftEpoch++
	s.directory = make(map[string]string)
	s.photos = make(map[string]string)
	s.workerPhotos = make(map[string]string)
	s.dmPeerReads = make(map[string]bool)
	s.directoryRead = false
	s.directoryEpoch++
	s.directoryRetryAfter = time.Time{}
	s.lastActivity = make(map[string]time.Time)
	s.browseAll = nil
	s.reactions = make(map[string][]chatui.ReactionChip)
	s.reactionMembers = make(map[string]map[chatReactionIdentity]struct{})
	s.reactionsFor = ""
	s.reactionPageLoading = false
	s.reactionPagePending = nil
	s.reactionPageReading = nil
	s.pinRevisions = make(map[string]uint64)
	s.window = chatMessageWindow
	s.cursor = chatCursor{}
	s.olderCursor = ""
	s.newerScanSequence = 0
	s.pageLoading = false
	s.threadBeforeSequence, s.threadAfterSequence = 0, 0
	s.threadPaging = false
	s.channelFragmentLast = ""
	s.loadErr = ""
	s.notice = ""
	s.noticeToken = 0
	s.cancelStream = nil
	s.streamFor = ""
	s.streamGeneration = 0
	s.opening = ""
	s.queuedSends = nil
	s.shareGeneration++
	s.shareAttemptKey, s.shareAttemptDestination = "", ""
	s.shareAttemptKeys = nil
	s.embedEntries, s.embedOrder = nil, nil
	s.embedEpoch++
	s.embedTimerRunning = false
	s.embedDraftSignature = ""
	s.openedShareFragment = ""
	s.generation++
	s.mu.Unlock()
	if stop != nil {
		stop()
	}
}

// config is the admitted config for callbacks that outlive a route loader.
func (s *chatState) config(fallback journeyclient.Config) journeyclient.Config {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()
	if cfg.Bearer == "" {
		return fallback
	}
	return cfg
}

func (s *chatState) conversationClient() chatv1.ConversationServiceClient {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.client
}

func (s *chatState) selectedID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.model.SelectedID
}

// snapshot is the current model with a live callback table.
func (s *chatState) snapshot() chatui.Model {
	s.mu.RLock()
	model, build := s.model, s.callbacks
	s.mu.RUnlock()
	if build != nil {
		model.Callbacks = build()
	}
	return model
}

// mutate applies one change to the model under the lock.
func (s *chatState) mutate(change func(*chatui.Model)) chatui.Model {
	s.mu.Lock()
	if change != nil {
		change(&s.model)
	}
	model, build := s.model, s.callbacks
	s.mu.Unlock()
	if build != nil {
		model.Callbacks = build()
	}
	return model
}

// beginGeneration claims the next generation for an action that changes what
// the reader is looking at.
func (s *chatState) beginGeneration() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generation++
	return s.generation
}

// currentGeneration is the generation without claiming a new one. A route
// revalidation reads it so its answer can be discarded if the reader moved on
// while it was in flight; it must never bump, because a revalidation is not a
// change of what the reader is looking at.
func (s *chatState) currentGeneration() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.generation
}

func (s *chatState) generationActive(generation uint64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.generation == generation
}

// commit applies a change only while generation is still current, so a slower
// answer cannot restore the selection or timeline the reader has left.
func (s *chatState) commit(generation uint64, change func(*chatui.Model)) bool {
	s.mu.Lock()
	if s.generation != generation {
		s.mu.Unlock()
		return false
	}
	if change != nil {
		change(&s.model)
	}
	s.mu.Unlock()
	return true
}

// sendKey is the idempotency key for one send attempt of body.
//
// mint is injected so the policy is testable without a clock.
func (s *chatState) sendKey(conversationID, body string, mint func() string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sendAttempts == nil {
		s.sendAttempts = make(map[string]chatSendAttempt)
	}
	if pending, ok := s.sendAttempts[conversationID]; ok && pending.Body == body && pending.Key != "" {
		return pending.Key
	}
	key := mint()
	s.sendAttempts[conversationID] = chatSendAttempt{Key: key, Body: body}
	return key
}

// clearSendKey forgets a conversation's pending attempt after it committed.
func (s *chatState) clearSendKey(conversationID string) {
	s.mu.Lock()
	delete(s.sendAttempts, conversationID)
	s.mu.Unlock()
}

// setNotice records a transient notice and returns the token that owns it. A
// later clearNotice with a superseded token does nothing, so one action's
// four-second timer cannot wipe a newer action's answer.
//
// retry asks the renderer for a Try again control beside the notice. It is for
// a notice the reader can act on -- a live feed that stopped -- and never for
// one that only reports what happened.
func (s *chatState) setNotice(text string, retry bool) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.noticeToken++
	s.notice = text
	s.model.Notice = text
	s.model.NoticeRetry = retry
	return s.noticeToken
}

// clearNotice clears the notice, and its retry control with it, when token
// still names the current one.
func (s *chatState) clearNotice(token uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if token != 0 && token != s.noticeToken {
		return false
	}
	s.notice = ""
	s.model.Notice = ""
	s.model.NoticeRetry = false
	return true
}

// chatFriendlyError is the reason clause the reader sees.
//
// A gRPC status string ("rpc error: code = PermissionDenied desc = ...") names
// an implementation to someone who wants to know what to do next, so it never
// reaches the model. The code decides the sentence; the detail stays in the
// transport.
func chatFriendlyError(err error) string {
	if err == nil {
		return ""
	}
	// status.Code reports Unknown for anything that is not a gRPC status, so
	// the code is only consulted when the error actually carries one. Without
	// that, a plain local error would be described to the reader as a service
	// that did not answer.
	reported, ok := status.FromError(err)
	if !ok {
		return "Something went wrong. Try again."
	}
	switch reported.Code() {
	case codes.PermissionDenied:
		return "You do not have access to it."
	case codes.Unauthenticated:
		return "Your sign-in has expired. Sign in again."
	case codes.NotFound:
		return "It is no longer there."
	case codes.AlreadyExists, codes.Aborted, codes.FailedPrecondition:
		return "Someone else changed it first. Try again."
	case codes.InvalidArgument:
		return "The details were not accepted."
	case codes.ResourceExhausted:
		return "There were too many requests. Try again in a moment."
	case codes.Unimplemented:
		return "This server does not offer it yet."
	case codes.DeadlineExceeded, codes.Unavailable, codes.Internal, codes.Unknown:
		return "The service did not answer. Try again."
	}
	return "Something went wrong. Try again."
}

// chatLoadFailureMessage is what replaces the timeline when a load fails.
func chatLoadFailureMessage(subject string, err error) string {
	if err == nil {
		return ""
	}
	return "We couldn't load " + subject + ". " + chatFriendlyError(err)
}

// chatCursorRejected reports whether the server refused a resume cursor, which
// is the one failure the client answers by dropping the cursor and resuming
// from the plain sequence instead of by backing off.
func chatCursorRejected(err error) bool {
	return err != nil && status.Code(err) == codes.InvalidArgument
}

func (s *chatState) currentNotice() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.notice
}

func (s *chatState) editDraftSnapshot() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return copyChatDrafts(s.editDrafts)
}

func (s *chatState) setEditDraft(postID, body string) {
	s.mu.Lock()
	if s.editDrafts == nil {
		s.editDrafts = make(map[string]string)
	}
	s.editDrafts[postID] = body
	if s.model.EditDrafts == nil {
		s.model.EditDrafts = make(map[string]string)
	}
	s.model.EditDrafts[postID] = body
	s.mu.Unlock()
}

func (s *chatState) clearEditDraft(postID string) {
	s.mu.Lock()
	delete(s.editDrafts, postID)
	delete(s.model.EditDrafts, postID)
	s.mu.Unlock()
}

func (s *chatState) reactionSnapshot() map[string][]chatui.ReactionChip {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string][]chatui.ReactionChip, len(s.reactions))
	for id, chips := range s.reactions {
		out[id] = append([]chatui.ReactionChip(nil), chips...)
	}
	return out
}

// The model already carries chips for retained posts. Keep the auxiliary
// lookup limited to those posts as the timeline moves through a long room.
// Callers hold s.mu.
func (s *chatState) pruneRetainedReactions() {
	if len(s.reactions) <= chatMessageWindow+chatThreadWindow {
		return
	}
	keep := make(map[string]bool, len(s.model.Messages)+len(s.model.ThreadMessages)+1)
	for _, message := range s.model.Messages {
		keep[message.ID] = true
	}
	for _, message := range s.model.ThreadMessages {
		keep[message.ID] = true
	}
	if s.model.ThreadParent != nil {
		keep[s.model.ThreadParent.ID] = true
	}
	for id := range s.reactions {
		if !keep[id] {
			delete(s.reactions, id)
			delete(s.reactionMembers, id)
		}
	}
}

func (s *chatState) setReactions(chips map[string][]chatui.ReactionChip, members map[string]map[chatReactionIdentity]struct{}) {
	s.mu.Lock()
	if s.reactions == nil {
		s.reactions = make(map[string][]chatui.ReactionChip, len(chips))
	}
	if s.reactionMembers == nil {
		s.reactionMembers = make(map[string]map[chatReactionIdentity]struct{}, len(members))
	}
	for id, list := range chips {
		s.reactions[id] = append([]chatui.ReactionChip(nil), list...)
		s.reactionMembers[id] = cloneChatReactionMembers(members[id])
	}
	s.reactionsReady = true
	s.mu.Unlock()
}

// queuePagedReactionRead keeps at most one reader and a queue drawn only from
// the current 500-post timeline. A quick second page preserves work for posts
// still retained while dropping IDs evicted by the moving window.
func (s *chatState) queuePagedReactionRead(generation uint64, conversation string, messages []chatui.Message) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generation != generation || s.model.SelectedID != conversation {
		return false
	}
	retained := make(map[string]bool, len(s.model.Messages))
	for _, message := range s.model.Messages {
		retained[message.ID] = true
	}
	queued := make(map[string]bool, len(s.reactionPagePending)+len(messages))
	pending := make([]string, 0, len(retained))
	add := func(id string) {
		if id == "" || !retained[id] || queued[id] || s.reactionPageReading[id] {
			return
		}
		if _, loaded := s.reactions[id]; loaded {
			return
		}
		queued[id] = true
		pending = append(pending, id)
	}
	for _, id := range s.reactionPagePending {
		add(id)
	}
	for _, id := range chatPostWindow(messages, chatPageSize) {
		add(id)
	}
	s.reactionPagePending = pending
	if s.reactionPageLoading || len(pending) == 0 {
		return false
	}
	s.reactionPageLoading = true
	return true
}

func (s *chatState) takePagedReactionRead(generation uint64, conversation string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generation != generation || s.model.SelectedID != conversation {
		return nil
	}
	ids := s.reactionPagePending
	if len(ids) > chatReactionWindow {
		ids = s.reactionPagePending[:chatReactionWindow]
		s.reactionPagePending = append([]string(nil), s.reactionPagePending[chatReactionWindow:]...)
	} else {
		s.reactionPagePending = nil
	}
	if len(ids) == 0 {
		s.reactionPageLoading = false
	} else {
		if s.reactionPageReading == nil {
			s.reactionPageReading = make(map[string]bool, chatReactionWindow)
		}
		for _, id := range ids {
			s.reactionPageReading[id] = true
		}
	}
	return ids
}

func (s *chatState) completePagedReactionBatch(generation uint64, ids []string) {
	s.mu.Lock()
	if s.generation == generation {
		for _, id := range ids {
			delete(s.reactionPageReading, id)
		}
	}
	s.mu.Unlock()
}

func (s *chatState) finishPagedReactionRead(generation uint64) {
	s.mu.Lock()
	if s.generation == generation {
		s.reactionPageLoading = false
		s.reactionPagePending = nil
		s.reactionPageReading = nil
	}
	s.mu.Unlock()
}

func (s *chatState) applyPagedReactionChips(generation uint64, conversation string, chips map[string][]chatui.ReactionChip) bool {
	return s.applyPagedReactionSnapshot(generation, conversation, chips, nil)
}

func (s *chatState) applyPagedReactionSnapshot(generation uint64, conversation string, chips map[string][]chatui.ReactionChip, members map[string]map[chatReactionIdentity]struct{}) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generation != generation || s.model.SelectedID != conversation {
		return false
	}
	changed := false
	for i := range s.model.Messages {
		message := &s.model.Messages[i]
		list, ok := chips[message.ID]
		if !ok {
			continue
		}
		message.Chips = append([]chatui.ReactionChip(nil), list...)
		message.Reactions, message.Reacted = reactionTotal(message.Chips)
		if s.reactions == nil {
			s.reactions = make(map[string][]chatui.ReactionChip)
		}
		s.reactions[message.ID] = append([]chatui.ReactionChip(nil), list...)
		if s.reactionMembers == nil {
			s.reactionMembers = make(map[string]map[chatReactionIdentity]struct{})
		}
		s.reactionMembers[message.ID] = cloneChatReactionMembers(members[message.ID])
		changed = true
	}
	s.pruneRetainedReactions()
	return changed
}

func cloneChatReactionMembers(in map[chatReactionIdentity]struct{}) map[chatReactionIdentity]struct{} {
	if in == nil {
		return nil
	}
	out := make(map[chatReactionIdentity]struct{}, len(in))
	for identity := range in {
		out[identity] = struct{}{}
	}
	return out
}

func (s *chatState) applyReactionEventMembers(event *chatv1.ConversationEvent, viewerHomeTenant string) {
	reaction := event.GetReaction()
	if reaction == nil || reaction.GetPostId() == "" || reaction.GetSubjectId() == "" {
		return
	}
	emoji := reaction.GetEmoji()
	if emoji == "" {
		emoji = chatDefaultReaction
	}
	identity := chatReactionIdentity{homeTenantID: reaction.GetHomeTenantId(), subjectID: reaction.GetSubjectId(), emoji: emoji}
	if identity.homeTenantID == "" {
		identity.homeTenantID = viewerHomeTenant
	}
	if s.reactionMembers == nil {
		s.reactionMembers = make(map[string]map[chatReactionIdentity]struct{})
	}
	members := s.reactionMembers[reaction.GetPostId()]
	if members == nil {
		members = make(map[chatReactionIdentity]struct{})
		s.reactionMembers[reaction.GetPostId()] = members
	}
	if event.GetRemoved() {
		delete(members, identity)
	} else {
		members[identity] = struct{}{}
	}
	chips := chatReactionChipsFromMembers(members, s.model.CurrentUser)
	for i := range s.model.Messages {
		if s.model.Messages[i].ID != reaction.GetPostId() {
			continue
		}
		s.model.Messages[i].Chips = append([]chatui.ReactionChip(nil), chips...)
		s.model.Messages[i].Reactions, s.model.Messages[i].Reacted = reactionTotal(chips)
		break
	}
	if s.reactions == nil {
		s.reactions = make(map[string][]chatui.ReactionChip)
	}
	s.reactions[reaction.GetPostId()] = append([]chatui.ReactionChip(nil), chips...)
}

// chatDefaultReaction is the emoji a bare React (no picker) adds and the one
// a legacy event without an emoji is counted under.
const chatDefaultReaction = "👍"

// applyReactionChips places a room's read chips on the messages on screen,
// if that room is still the one open. It reports whether anything changed.
func (s *chatState) applyReactionChips(conversationID string, chips map[string][]chatui.ReactionChip) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.model.SelectedID != conversationID {
		return false
	}
	changed := false
	for i := range s.model.Messages {
		list, ok := chips[s.model.Messages[i].ID]
		if !ok {
			continue
		}
		msg := &s.model.Messages[i]
		msg.Chips = append([]chatui.ReactionChip(nil), list...)
		msg.Reactions, msg.Reacted = reactionTotal(msg.Chips)
		changed = true
	}
	return changed
}

// reactionTotal is the count across every chip.
func reactionTotal(chips []chatui.ReactionChip) (total int, mine bool) {
	for _, chip := range chips {
		total += chip.Count
		mine = mine || chip.Mine
	}
	return total, mine
}

func chatReactionChipsFromMembers(members map[chatReactionIdentity]struct{}, viewer string) []chatui.ReactionChip {
	identities := make([]chatReactionIdentity, 0, len(members))
	for identity := range members {
		identities = append(identities, identity)
	}
	sort.Slice(identities, func(i, j int) bool {
		if identities[i].emoji != identities[j].emoji {
			return identities[i].emoji < identities[j].emoji
		}
		if identities[i].homeTenantID != identities[j].homeTenantID {
			return identities[i].homeTenantID < identities[j].homeTenantID
		}
		return identities[i].subjectID < identities[j].subjectID
	})
	chips := make([]chatui.ReactionChip, 0, len(identities))
	for _, identity := range identities {
		chips = adjustChips(chips, identity.emoji, 1, viewer != "" && identity.subjectID == viewer)
	}
	return chips
}

// adjustChips moves one emoji's count by delta on a chip list, adding the
// chip when it is new and dropping it when it reaches zero. viewer says the
// change is the viewer's own reaction, which is what the chip highlights.
func adjustChips(chips []chatui.ReactionChip, emoji string, delta int, viewer bool) []chatui.ReactionChip {
	for i := range chips {
		if chips[i].Emoji != emoji {
			continue
		}
		chips[i].Count += delta
		if viewer {
			chips[i].Mine = delta > 0
		}
		if chips[i].Count <= 0 {
			return append(chips[:i:i], chips[i+1:]...)
		}
		return chips
	}
	if delta > 0 {
		chips = append(chips, chatui.ReactionChip{Emoji: emoji, Count: delta, Mine: viewer})
	}
	return chips
}

// ownReactionAlreadyAt reports whether the viewer's chip already reflects an
// add or remove. The RPC completion and stream event may arrive in either
// order, but they describe the same change and must count it only once.
func ownReactionAlreadyAt(chips []chatui.ReactionChip, emoji string, added bool) bool {
	for _, chip := range chips {
		if chip.Emoji == emoji {
			return chip.Mine == added
		}
	}
	return !added
}

// setPinRevisions replaces the pin revisions for the loaded conversation.
// UnpinPost refuses a zero expected_revision, and ListPins is the only source
// of that revision, so the client keeps it rather than guessing 1.
func (s *chatState) setPinRevisions(revisions map[string]uint64) {
	s.mu.Lock()
	s.pinRevisions = make(map[string]uint64, len(revisions))
	for id, revision := range revisions {
		s.pinRevisions[id] = revision
	}
	s.mu.Unlock()
}

func (s *chatState) setPinRevision(postID string, revision uint64) {
	s.mu.Lock()
	if s.pinRevisions == nil {
		s.pinRevisions = make(map[string]uint64)
	}
	s.pinRevisions[postID] = revision
	s.mu.Unlock()
}

func (s *chatState) pinRevision(postID string) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pinRevisions[postID]
}

func (s *chatState) clearPinRevision(postID string) {
	s.mu.Lock()
	delete(s.pinRevisions, postID)
	s.mu.Unlock()
}

// setOlderCursor records where an older page starts, and whether one exists.
func (s *chatState) setOlderCursor(cursor string) {
	s.mu.Lock()
	s.olderCursor = cursor
	s.model.HasOlder = cursor != ""
	s.mu.Unlock()
}

func (s *chatState) olderPageCursor() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.olderCursor
}

// prependOlderChatMessages folds an older page into the front of the timeline
// and widens the retained window by what it added, so the bound that keeps a
// busy channel from growing without limit does not immediately discard the
// history the reader just asked for.
func (s *chatState) prependOlderChatMessages(conversationID string, older []chatui.Message, cursor string, fence ...chatTimelinePageFence) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.model.SelectedID != conversationID || !s.timelineFenceCurrent(fence) {
		return false
	}
	before := len(s.model.Messages)
	s.model.Messages = prependChatMessages(s.model.Messages, older)
	sortChatMessagesBySequence(s.model.Messages, s.cursor.Seen)
	added := len(s.model.Messages) - before
	if added <= 0 {
		s.olderCursor = cursor
		s.model.HasOlder = cursor != ""
		return true
	}
	if len(s.model.Messages) > chatMessageWindow {
		s.model.Messages = append([]chatui.Message(nil), s.model.Messages[:chatMessageWindow]...)
		s.model.HasNewer = true
		s.newerScanSequence = s.model.Messages[len(s.model.Messages)-1].Sequence
	}
	s.olderCursor = cursor
	s.model.HasOlder = cursor != ""
	pruneChatSeen(&s.cursor, s.model.Messages, s.model.ThreadMessages)
	s.pruneRetainedReactions()
	return true
}

func (s *chatState) appendNewerChatMessages(conversationID string, newer []chatui.Message, cursor string, scannedMax uint64, fence ...chatTimelinePageFence) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.model.SelectedID != conversationID || !s.timelineFenceCurrent(fence) {
		return false
	}
	for _, message := range newer {
		if !chatHasMessage(s.model.Messages, message.ID) {
			s.model.Messages = append(s.model.Messages, message)
		}
	}
	sortChatMessagesBySequence(s.model.Messages, s.cursor.Seen)
	if len(s.model.Messages) > chatMessageWindow {
		s.model.Messages = append([]chatui.Message(nil), s.model.Messages[len(s.model.Messages)-chatMessageWindow:]...)
		s.model.HasOlder = true
		s.olderCursor = ""
	}
	s.model.HasNewer = cursor != ""
	if scannedMax > s.newerScanSequence {
		s.newerScanSequence = scannedMax
	}
	pruneChatSeen(&s.cursor, s.model.Messages, s.model.ThreadMessages)
	s.pruneRetainedReactions()
	return true
}

func (s *chatState) newestHeldSequence() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var newest uint64
	for _, message := range s.model.Messages {
		if message.Sequence > newest {
			newest = message.Sequence
		}
	}
	if s.newerScanSequence > newest {
		return s.newerScanSequence
	}
	return newest
}

func (s *chatState) replaceLatestChatMessages(conversationID string, messages []chatui.Message, olderCursor string, fence ...chatTimelinePageFence) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.model.SelectedID != conversationID || s.cursor.ConversationID != conversationID || !s.timelineFenceCurrent(fence) {
		return false
	}
	s.model.Messages = append([]chatui.Message(nil), messages...)
	s.model.Messages, s.model.HasOlder = boundChatMessages(s.model.Messages, chatMessageWindow, olderCursor != "")
	s.model.HasNewer = false
	s.newerScanSequence = 0
	s.olderCursor = olderCursor
	s.cursor.Seen = make(map[string]uint64, len(messages))
	for _, message := range s.model.Messages {
		s.cursor.Seen[message.ID] = message.Sequence
		if message.Sequence > s.cursor.LastSequence {
			s.cursor.LastSequence = message.Sequence
		}
	}
	return true
}

// setChatMemberCount records how many people are in a conversation, for the
// header's "Public channel - N members".
func (s *chatState) setChatMemberCount(conversationID string, count int) {
	s.mu.Lock()
	for i := range s.model.Conversations {
		if s.model.Conversations[i].ID == conversationID {
			s.model.Conversations[i].MemberCount = count
			break
		}
	}
	s.mu.Unlock()
}

// retainedWindow is how many timeline messages are kept in memory. It starts
// at chatMessageWindow; older and newer history remains available through
// explicit server pages as the retained slice moves.
func (s *chatState) retainedWindow() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.window <= 0 {
		return chatMessageWindow
	}
	return s.window
}

func (s *chatState) growWindow(by int) int {
	_ = by
	return s.retainedWindow()
}

// actionFailureNotice is what a failed action says. It names the action, so a
// reader who pinned a post is not told "Conversation unavailable".
func actionFailureNotice(action string, err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("We couldn't %s. %s", action, chatFriendlyError(err))
}

// setLoadError records the one failure allowed to replace the timeline.
func (s *chatState) setLoadError(message string) {
	s.mu.Lock()
	s.loadErr = message
	s.mu.Unlock()
}

func (s *chatState) loadError() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadErr
}

// draft is the composer text stored for one conversation.
func (s *chatState) draft(conversationID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.drafts[conversationID]
}

// setDraft records a keystroke. No RPC and no route revalidation: the
// textarea already shows the character, and re-rendering a controlled input
// from a model one keystroke behind is what moved the caret.
func (s *chatState) setDraft(conversationID, value string) {
	s.mu.Lock()
	if s.drafts == nil {
		s.drafts = make(map[string]string)
	}
	if s.draftRevs == nil {
		s.draftRevs = make(map[string]uint64)
	}
	if conversationID != "" {
		s.draftRev++
		s.draftRevs[conversationID] = s.draftRev
	}
	if conversationID != "" {
		if value == "" {
			delete(s.drafts, conversationID)
		} else {
			s.drafts[conversationID] = value
		}
	}
	if s.model.SelectedID == conversationID {
		s.model.Draft = value
	}
	s.model.Preferences.Drafts = copyChatDrafts(s.drafts)
	s.mu.Unlock()
}

// loadDrafts adopts persisted drafts for conversations this session has not
// typed in.
//
// A conversation with a local revision is never overwritten. The persisted copy
// is always at least as old as the last keystroke -- it is a debounced write of
// it -- so adopting it is how a send's cleared draft came back and got merged
// into the next message.
func (s *chatState) loadDrafts(drafts map[string]string) {
	s.mu.Lock()
	if s.drafts == nil {
		s.drafts = make(map[string]string, len(drafts))
	}
	if s.draftRevs == nil {
		s.draftRevs = make(map[string]uint64)
	}
	for id, body := range drafts {
		if s.draftRevs[id] != 0 {
			continue
		}
		if strings.TrimSpace(body) == "" {
			continue
		}
		s.drafts[id] = body
	}
	s.model.Preferences.Drafts = copyChatDrafts(s.drafts)
	s.model.Draft = s.drafts[s.model.SelectedID]
	s.mu.Unlock()
}

// rebaseDrafts takes a newer sidebar copy after a revision conflict. Rooms
// edited in this session keep their text (including a send's empty tombstone);
// untouched rooms follow the server, including drafts another tab cleared.
func (s *chatState) rebaseDrafts(server map[string]string, cfg journeyclient.Config, epoch uint64) (map[string]string, uint64, bool) {
	s.mu.Lock()
	if s.draftEpoch != epoch || s.cfg.Tenant != cfg.Tenant || s.cfg.Subject != cfg.Subject || s.cfg.Bearer != cfg.Bearer {
		s.mu.Unlock()
		return nil, 0, false
	}
	if s.drafts == nil {
		s.drafts = make(map[string]string)
	}
	for id := range s.drafts {
		if s.draftRevs[id] == 0 {
			delete(s.drafts, id)
		}
	}
	for id, body := range server {
		if s.draftRevs[id] == 0 && strings.TrimSpace(body) != "" {
			s.drafts[id] = body
		}
	}
	s.model.Preferences.Drafts = copyChatDrafts(s.drafts)
	s.model.Draft = s.drafts[s.model.SelectedID]
	drafts, revision := copyChatDrafts(s.drafts), s.draftRev
	s.mu.Unlock()
	return drafts, revision, true
}

func (s *chatState) draftsForWrite() (map[string]string, uint64, uint64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return copyChatDrafts(s.drafts), s.draftRev, s.draftEpoch
}

func (s *chatState) markDraftsPersisted(revision uint64, cfg journeyclient.Config, epoch uint64) {
	s.mu.Lock()
	if s.draftEpoch != epoch || s.cfg.Tenant != cfg.Tenant || s.cfg.Subject != cfg.Subject || s.cfg.Bearer != cfg.Bearer {
		s.mu.Unlock()
		return
	}
	for id, localRevision := range s.draftRevs {
		if localRevision <= revision {
			delete(s.draftRevs, id)
		}
	}
	s.mu.Unlock()
}

// draftRevision is the local edit stamp for one conversation, or zero when
// this session has not typed in it.
func (s *chatState) draftRevision(conversationID string) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.draftRevs[conversationID]
}

func (s *chatState) allDrafts() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return copyChatDrafts(s.drafts)
}

func copyChatDrafts(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for id, body := range in {
		out[id] = body
	}
	return out
}

// swapChatDraft moves the composer to another conversation: the text being
// typed is kept under the conversation it was typed in, and the new
// conversation's own draft comes back.
func swapChatDraft(model *chatui.Model, drafts map[string]string, next string) {
	if drafts == nil {
		return
	}
	if current := model.SelectedID; current != "" {
		if strings.TrimSpace(model.Draft) == "" {
			delete(drafts, current)
		} else {
			drafts[current] = model.Draft
		}
	}
	model.SelectedID = next
	model.Draft = drafts[next]
	model.Preferences.Drafts = copyChatDrafts(drafts)
}

// selectChatConversation is the whole of what a conversation switch changes in
// the model: the selection, the composer, the thread pane, the timeline, the
// stream cursor and the transient notice. It claims the new generation in the
// same critical section, so there is no window in which a stream event or a
// slower read can still be counted as current.
//
// It returns the model and that generation; the caller guards its own read
// with it.
func (s *chatState) selectChatConversation(id string) (chatui.Model, uint64) {
	var generation uint64
	model := s.mutate(func(model *chatui.Model) {
		s.generation++
		generation = s.generation
		s.reactionPageLoading = false
		s.reactionPagePending = nil
		swapChatDraft(model, s.drafts, id)
		model.Search = ""
		model.ShowThread = false
		model.ThreadParentID = ""
		model.ThreadParent = nil
		model.ThreadMessages = nil
		model.ThreadLoading = false
		model.ThreadHasOlder, model.ThreadHasNewer = false, false
		s.threadBeforeSequence, s.threadAfterSequence = 0, 0
		s.threadPaging = false
		model.Messages = nil
		model.ChannelPins = nil
		model.Members = nil
		model.ChannelTodo = chatui.ChannelTodoList{}
		model.ChannelTeam, model.ChannelProject = chatui.ChannelTeamWidget{}, chatui.ChannelProjectWidget{}
		model.ChannelPoll = chatui.ChannelPoll{}
		model.ChannelPollLoading, model.ChannelPollPending = false, false
		model.ChannelPollError = ""
		model.ChannelWidgetsLoading, model.ChannelWidgetsPending = false, false
		model.ChannelWidgetsError = ""
		model.ChannelTodoLoading, model.ChannelTodoPending = false, false
		model.ChannelTodoError = ""
		model.ChannelTodoDraft, model.ChannelTodoSourcePin = "", ""
		model.ChannelTodoNewMode, model.ChannelTodoNewSelected = "EVERYONE", nil
		model.CanPinChannelTodo = false
		model.PinReferenceUnavailable = false
		for _, conversation := range model.Conversations {
			if conversation.ID == id && conversation.HostTenantID != "" && conversation.HostTenantID != s.cfg.Tenant {
				model.PinReferenceUnavailable = true
				break
			}
		}
		model.HasOlder = false
		model.HasNewer = false
		s.newerScanSequence = 0
		s.pageLoading = false
		model.UnreadFromID, model.PickerID, model.MenuID = "", "", ""
		s.shareGeneration++
		model.SharePostID, model.ShareSourceRoomID, model.ShareDestinationID, model.ShareQuery, model.ShareError = "", "", "", "", ""
		model.ShareSource, model.ShareDestinations = nil, nil
		model.ShareLoading, model.SharePending = false, false
		model.ShareVersion++
		s.shareAttemptKey, s.shareAttemptDestination = "", ""
		s.shareAttemptKeys = nil
		model.State = chatui.StateLoading
		// A notice belongs to the conversation it was raised in.
		model.Notice = ""
		s.notice = ""
		s.noticeToken++
		s.loadErr = ""
		s.cursor = chatCursor{ConversationID: id, Seen: map[string]uint64{}}
		s.opening = id
	})
	return model, generation
}

func (s *chatState) claimChannelFragment(claim string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if claim == "" {
		s.channelFragmentLast = ""
		return false
	}
	if s.channelFragmentLast == claim {
		return false
	}
	s.channelFragmentLast = claim
	return true
}

// resolveSendTarget decides where one send goes, and queues it when there is
// nowhere to put it yet.
//
// The room on screen wins over the id the caller carried: chatui addresses a
// send from the model it last rendered, which during an open is a render
// behind. queued means the caller does nothing -- finishChatOpen will send it.
func (s *chatState) resolveSendTarget(conversationID, body string) (target string, queued bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.opening != "" {
		// The room being opened is where the reader is, whatever the caller's
		// render still says.
		s.queuedSends = append(s.queuedSends, chatQueuedSend{Conversation: s.opening, Body: body})
		return "", true
	}
	// With nothing opening, the caller's id is the composer it was typed into.
	// The model is the fallback, not the preference: a stale projection commit
	// can leave SelectedID naming the room before this one.
	target = conversationID
	if target == "" {
		target = s.model.SelectedID
	}
	if target == "" {
		s.queuedSends = append(s.queuedSends, chatQueuedSend{Body: body})
		return "", true
	}
	return target, false
}

// finishChatOpen ends this room's open and hands back what was typed for it,
// in the order it was typed.
//
// A later Create may have opened another room, and this room's queued sends
// still belong to it. When the reader has returned to this same room, its
// newer read owns both the opening latch and those sends.
func (s *chatState) finishChatOpen(conversationID string, generation uint64) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.opening == conversationID {
		if s.generation != generation {
			// A newer read of this same room owns the opening and its
			// queued sends. Let that read finish them instead.
			return nil
		}
		s.opening = ""
	}
	kept := make([]chatQueuedSend, 0, len(s.queuedSends))
	bodies := make([]string, 0, len(s.queuedSends))
	for _, queued := range s.queuedSends {
		// An entry with no room was typed before any room was open; the first
		// room to resolve takes it.
		if queued.Conversation == conversationID || queued.Conversation == "" {
			bodies = append(bodies, queued.Body)
			continue
		}
		kept = append(kept, queued)
	}
	s.queuedSends = kept
	if len(bodies) == 0 {
		return nil
	}
	return bodies
}

// discardQueuedChatSends takes back the messages waiting for a room that will
// never open, so a failed Create can put the text in the composer instead of
// swallowing it.
func (s *chatState) discardQueuedChatSends() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queuedSends) == 0 {
		return nil
	}
	bodies := make([]string, 0, len(s.queuedSends))
	for _, queued := range s.queuedSends {
		bodies = append(bodies, queued.Body)
	}
	s.queuedSends = nil
	return bodies
}

// -- subscription ----------------------------------------------------------

// swapSubscription installs cancel as the live subscription and returns the
// cancel it replaced, so the caller ends the old stream outside the lock.
//
// started is false in two cases, and neither is an error. A stale generation
// means the reader has already moved on, and the stream that belongs to the
// newer generation is left alone. An identical conversation already streaming
// means there is nothing to do: a route revalidation happens on every notice
// and every action, and tearing down a healthy stream to rebuild it each time
// would be its own reconnect storm.
func (s *chatState) swapSubscription(generation uint64, conversationID string, cancel func()) (previous func(), started bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generation != generation {
		return nil, false
	}
	if s.cancelStream != nil && s.streamFor == conversationID && s.streamGeneration == generation {
		return nil, false
	}
	previous = s.cancelStream
	s.cancelStream, s.streamFor, s.streamGeneration = cancel, conversationID, generation
	// A new stream has its own position to learn. Keeping the old baseline
	// would measure its first event against a position it never reported.
	if s.cursor.ConversationID == conversationID {
		s.cursor.Baselined = false
	}
	return previous, true
}

// takeSubscription removes and returns the live subscription's cancel.
func (s *chatState) takeSubscription() func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	stop := s.cancelStream
	s.cancelStream, s.streamFor, s.streamGeneration = nil, "", 0
	return stop
}

// releaseSubscription clears the registration only when it still belongs to
// this watch, and returns its cancel.
//
// A watch that exits because its generation was superseded must not clear a
// subscription a newer one has since installed -- and, just as important, must
// not leave its own dead registration behind. A registration outlives its
// goroutine only if nobody releases it, and while one is registered the route
// loader trusts the stream and stops re-reading the timeline, so the
// conversation would go silent with no stream running.
func (s *chatState) releaseSubscription(generation uint64, conversationID string) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.streamFor != conversationID || s.streamGeneration != generation {
		return nil
	}
	stop := s.cancelStream
	s.cancelStream, s.streamFor, s.streamGeneration = nil, "", 0
	return stop
}

// streamLive reports whether a subscription for conversationID is open.
//
// It is what lets a route revalidation skip re-reading the timeline: while the
// stream is delivering, the messages in the model are the current ones, and
// re-reading them on every notice and every action would be a storm of its
// own. A watch that gives up releases the subscription, so reads resume.
func (s *chatState) streamLive(conversationID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return conversationID != "" && s.cancelStream != nil && s.streamFor == conversationID && s.streamGeneration == s.generation
}

// chatWatchStart is the resume position one WatchConversation request carries.
//
// Exactly one of the two is ever sent: the transport refuses a request with
// both after_sequence and resume_cursor set with INVALID_ARGUMENT. The signed
// cursor wins when there is one, because it carries the server's own
// authorization and position; the raw sequence is the fallback for a first
// subscription after a plain ListPosts read.
func chatWatchStart(cursor chatCursor) (afterSequence uint64, resume string) {
	if strings.TrimSpace(cursor.Resume) != "" {
		return 0, cursor.Resume
	}
	// The watch position, never the post sequence. With neither, the server
	// replays from the beginning and the client drops what it already has by
	// post id -- which is what Seen is for.
	return cursor.StreamSequence, ""
}

// applySentChatPost folds the reader's own committed post into the timeline.
//
// The send response is the post, so there is nothing to re-read: applying it
// here is what lets a send cost one RPC instead of a whole projection reload,
// and recording it in the cursor is what stops the stream from delivering it a
// second time a moment later.
func (s *chatState) applySentChatPost(conversationID string, post *chatv1.Post, locale string, directory map[string]string, now time.Time) (applied, gapped bool) {
	if post == nil || post.GetId() == "" {
		return false, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.model.SelectedID != conversationID || s.cursor.ConversationID != conversationID {
		return false, false
	}
	defer pruneChatSeen(&s.cursor, s.model.Messages, s.model.ThreadMessages)
	defer s.pruneRetainedReactions()
	if s.cursor.Seen == nil {
		s.cursor.Seen = map[string]uint64{}
	}
	if seen, ok := s.cursor.Seen[post.GetId()]; ok && seen >= post.GetSequence() {
		return false, false
	}
	s.cursor.Seen[post.GetId()] = post.GetSequence()
	// The cursor only advances when this post is the next one. A post that
	// jumped the sequence means posts from other people landed in between, so
	// the gap is reported and the caller reads them by cursor. Advancing past
	// it would mean never asking for what was missed.
	if post.GetSequence() == s.cursor.LastSequence+1 {
		s.cursor.LastSequence = post.GetSequence()
	} else if post.GetSequence() > s.cursor.LastSequence+1 {
		gapped = true
	}
	if post.GetParentId() != "" {
		if s.model.ThreadParent != nil && s.model.ThreadParent.ID == post.GetParentId() {
			s.model.ThreadParent.Replies++
		}
		for i := range s.model.Messages {
			if s.model.Messages[i].ID == post.GetParentId() {
				s.model.Messages[i].Replies++
				break
			}
		}
		if s.model.ShowThread && s.model.ThreadParentID == post.GetParentId() && liveChatThreadAccepts(&s.model, post.GetSequence()) && !chatHasMessage(s.model.ThreadMessages, post.GetId()) {
			s.model.ThreadMessages = append(s.model.ThreadMessages, chatMessage(post, locale, directory, now))
			s.boundLiveThread()
		}
		return true, gapped
	}
	if chatHasMessage(s.model.Messages, post.GetId()) {
		return false, gapped
	}
	if s.model.HasNewer && len(s.model.Messages) > 0 && post.GetSequence() > s.model.Messages[len(s.model.Messages)-1].Sequence {
		return true, gapped
	}
	s.model.Messages = append(s.model.Messages, chatMessage(post, locale, directory, now))
	sortChatMessagesBySequence(s.model.Messages, s.cursor.Seen)
	trimmed := len(s.model.Messages) > chatMessageWindow
	window := s.window
	if window <= 0 {
		window = chatMessageWindow
	}
	s.model.Messages, s.model.HasOlder = boundChatMessages(s.model.Messages, window, s.model.HasOlder)
	if trimmed {
		s.olderCursor = ""
	}
	pruneChatSeen(&s.cursor, s.model.Messages, s.model.ThreadMessages)
	// A send never takes an open conversation back to loading or empty: the
	// reader's own message is on screen, and a composer disabled by a loading
	// render is how keystrokes typed during it were dropped.
	s.model.State, s.model.Error = chatui.StateReady, ""
	return true, gapped
}

// chatStreamQuietLifetime is how long a stream must have been open for its
// clean end to be a rollover rather than a refusal.
const chatStreamQuietLifetime = 10 * time.Second

// chatStreamRollover reports whether a stream that ended without delivering
// anything ended for a reason that says nothing about the conversation's
// health.
//
// Three of those, and the live run found the third the hard way.
//
// A healthy server closes a quiet watch at its lifetime ceiling, and there is
// no heartbeat, so silence means nothing changed: a clean end after a long
// stream is a ceiling, not an outage.
//
// A cancellation this client asked for -- a conversation switch, a page
// leaving -- is not a failure at all, whatever the lifetime.
//
// And a CANCELED status with our own context still alive is the tunnel's
// WebSocket rotating underneath the stream. The server logged every watch for
// the room as CANCELED with no failure of its own, and because each rotation
// happened inside the ten-second window it was counted as a refusal: five
// rotations, four backoffs, and "no longer updating live" about forty seconds
// after the room opened, on a server that was working the whole time.
//
// A rollover does not reset the backoff. Not counting it toward the give-up is
// what keeps the feed alive; letting it reset the wait as well would put a
// rotating transport back into the reconnect loop this replaced.
func chatStreamRollover(err error, lifetime time.Duration, ownContextAlive bool) bool {
	if !ownContextAlive {
		return true
	}
	if status.Code(err) == codes.Canceled || errors.Is(err, context.Canceled) {
		return true
	}
	if lifetime < chatStreamQuietLifetime {
		return false
	}
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	switch status.Code(err) {
	case codes.OK, codes.DeadlineExceeded, codes.Unavailable:
		return true
	}
	return false
}

// claimReactionRead reports whether the reaction counts must be read for this
// conversation, and records that they were. There is no bulk count RPC, so the
// read is a bounded fan-out; it happens when a conversation is opened, not on
// every revalidation. REACTION_CHANGED events keep the counts current after
// that, and a local reaction releases the claim.
func (s *chatState) claimReactionRead(conversationID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	// A room whose chips are already held is not read again; a room whose
	// read is still in flight may be read by a second load. The page's first
	// projection load is often followed at once by a warm refresh with a
	// newer generation: refusing that one left the adopted timeline with no
	// chips at all, because the first read's result died with its generation.
	if conversationID == "" || (s.reactionsFor == conversationID && s.reactionsReady) {
		return false
	}
	s.reactionsFor, s.reactionsReady = conversationID, false
	return true
}

// releaseReactionRead makes the next load re-read the counts.
func (s *chatState) releaseReactionRead() {
	s.mu.Lock()
	s.reactionsFor, s.reactionsReady = "", false
	s.mu.Unlock()
}

// adjustReactionCount moves one post's reaction count by delta, so the
// reader's own reaction shows before the server's event arrives.
func (s *chatState) adjustReaction(postID, emoji string, delta int, viewer bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.model.Messages {
		if s.model.Messages[i].ID != postID {
			continue
		}
		msg := &s.model.Messages[i]
		if !viewer || !ownReactionAlreadyAt(msg.Chips, emoji, delta > 0) {
			msg.Chips = adjustChips(msg.Chips, emoji, delta, viewer)
		}
		msg.Reactions, msg.Reacted = reactionTotal(msg.Chips)
		if viewer {
			if s.reactionMembers == nil {
				s.reactionMembers = make(map[string]map[chatReactionIdentity]struct{})
			}
			members := s.reactionMembers[postID]
			if members == nil {
				members = make(map[chatReactionIdentity]struct{})
				s.reactionMembers[postID] = members
			}
			identity := chatReactionIdentity{homeTenantID: s.cfg.Tenant, subjectID: s.model.CurrentUser, emoji: emoji}
			if delta > 0 {
				members[identity] = struct{}{}
			} else {
				delete(members, identity)
			}
		}
		if s.reactions == nil {
			s.reactions = map[string][]chatui.ReactionChip{}
		}
		s.reactions[postID] = append([]chatui.ReactionChip(nil), msg.Chips...)
		return
	}
}

// adoptLoadedChatProjection installs a projection read, keeping every field
// the loader does not own.
//
// The read takes a snapshot, spends a round trip in ListConversations, then
// commits. Overwriting the whole model with that snapshot put back whatever
// local state changed during the round trip -- which is how a send cleared the
// composer and a coalesced render two hundred milliseconds later delivered the
// sent text back into Model.Draft. The composer, the drafts, the edit drafts
// and the transient notice are local authority: they are re-read here, at
// commit time, rather than carried in from before the RPC.
func (s *chatState) adoptLoadedChatProjection(generation uint64, loaded chatui.Model, cursor chatCursor, reloaded bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generation != generation {
		return false
	}
	// This listing was built from a snapshot. The governed worker directory
	// may have completed while the RPC was in flight, so the current cache
	// owns this field regardless of which conversation the listing selected.
	loaded.SearchDirectory = s.model.SearchDirectory
	retry := s.model.NoticeRetry
	preservedThreadPage := false
	if loaded.SelectedID == s.model.SelectedID {
		// The read started from a snapshot and spent a round trip in
		// ListConversations. Everything the reader did in the meantime --
		// closed the phone drawer, opened details, a thread, a picker or a
		// menu, typed a browse filter -- lives in the current model and must
		// not be rolled back by a refresh that only re-read the listing.
		cur := s.model
		loaded.SidebarOpen, loaded.ShowDetails, loaded.ShowCreate, loaded.ShowBrowse = cur.SidebarOpen, cur.ShowDetails, cur.ShowCreate, cur.ShowBrowse
		loaded.ShowPerson, loaded.PersonDetails = cur.ShowPerson, cur.PersonDetails
		loaded.SharePostID, loaded.ShareSourceRoomID, loaded.ShareDestinationID, loaded.ShareQuery, loaded.ShareError = cur.SharePostID, cur.ShareSourceRoomID, cur.ShareDestinationID, cur.ShareQuery, cur.ShareError
		loaded.ShareSource, loaded.ShareDestinations, loaded.ShareLoading, loaded.SharePending = cur.ShareSource, cur.ShareDestinations, cur.ShareLoading, cur.SharePending
		loaded.ShareVersion = cur.ShareVersion
		loaded.EmbedOrigin, loaded.Embeds, loaded.EmbedRevision = cur.EmbedOrigin, cur.Embeds, cur.EmbedRevision
		loaded.Browse, loaded.BrowseQuery, loaded.NewKind, loaded.NewName = cur.Browse, cur.BrowseQuery, cur.NewKind, cur.NewName
		loaded.EditingID, loaded.PickerID, loaded.MenuID, loaded.UnreadFromID = cur.EditingID, cur.PickerID, cur.MenuID, cur.UnreadFromID
		loaded.MemberQuery = cur.MemberQuery
		loaded.ShowThread, loaded.ThreadParentID, loaded.ThreadFollowed = cur.ShowThread, cur.ThreadParentID, cur.ThreadFollowed
		loaded.ThreadLoading = cur.ThreadLoading
		loaded.ThreadParent = cur.ThreadParent
		loaded.Members = cur.Members
		if reloaded && !cur.ShowThread {
			loaded.ThreadMessages = nil
			loaded.ThreadHasOlder, loaded.ThreadHasNewer = false, false
		}
		if reloaded && cur.ShowThread {
			// The dedicated thread read owns this projection. A room listing
			// refresh may have started before the pane opened or its page landed.
			loaded.ThreadMessages = cur.ThreadMessages
			loaded.ThreadHasOlder, loaded.ThreadHasNewer = cur.ThreadHasOlder, cur.ThreadHasNewer
			preservedThreadPage = true
		}
		if !reloaded {
			// The streaming shortcut kept the snapshot's timeline too, so
			// the reaction chips, attachment URLs and stream posts the room
			// accrued since would vanish with it. (Measured: the first two
			// rooms after page load opened with no chips; the third, opened
			// once the shell went quiet, had all of them.)
			loaded.Messages, loaded.HasOlder, loaded.HasNewer, loaded.ThreadMessages = cur.Messages, cur.HasOlder, cur.HasNewer, cur.ThreadMessages
			loaded.ThreadHasOlder, loaded.ThreadHasNewer = cur.ThreadHasOlder, cur.ThreadHasNewer
			if cur.State == chatui.StateReady && loaded.State == chatui.StateLoading {
				loaded.State = cur.State
			}
		}
	}
	peerIDs := s.model.PeerIDs
	// The listing and post read began before this commit. Peer membership and
	// directory names can arrive during those RPCs, so resolve direct-room
	// labels from current identity facts before replacing the room values.
	for i := range loaded.Conversations {
		room := &loaded.Conversations[i]
		if room.Kind != chatui.DirectMessage || peerIDs[room.ID] == "" {
			continue
		}
		*room = chatDirectoryNamedDirect(*room, peerIDs, s.directory)
		if room.Name != room.ID {
			continue
		}
		for _, current := range s.model.Conversations {
			if current.ID == room.ID && current.Name != "" && current.Name != current.ID {
				room.Name = current.Name
				break
			}
		}
		if room.Name != room.ID {
			continue
		}
		for _, section := range s.model.Sections {
			for _, current := range section.Chats {
				if current.ID == room.ID && current.Name != "" && current.Name != current.ID {
					room.Name = current.Name
					break
				}
			}
			if room.Name != room.ID {
				break
			}
		}
	}
	// The recipient rail is local authority while this listing RPC is in flight.
	// Rebind its order to fresh conversation values without losing a collapse.
	if len(s.model.Sections) > 0 {
		fresh := make(map[string]chatui.Conversation, len(loaded.Conversations))
		for _, c := range loaded.Conversations {
			fresh[c.ID] = c
		}
		used := make(map[string]bool)
		sections := make([]chatui.SidebarSection, len(s.model.Sections))
		for i, section := range s.model.Sections {
			sections[i] = chatui.SidebarSection{ID: section.ID, Name: section.Name, Collapsed: section.Collapsed}
			for _, c := range section.Chats {
				if next, ok := fresh[c.ID]; ok && !used[c.ID] {
					sections[i].Chats = append(sections[i].Chats, next)
					used[c.ID] = true
				}
			}
		}
		for _, c := range loaded.Conversations {
			if used[c.ID] {
				continue
			}
			target := "channels"
			if c.Kind == chatui.DirectMessage || c.Kind == chatui.GroupChat {
				target = "direct"
			}
			for i := range sections {
				if sections[i].ID == target {
					sections[i].Chats = append(sections[i].Chats, c)
					break
				}
			}
		}
		loaded.Sections = sections
	}
	loaded.RailMenuID = s.model.RailMenuID
	s.model = loaded
	s.model.PeerIDs = peerIDs
	s.model.PhotoURLs = make(map[string]string, len(s.photos))
	for id, url := range s.photos {
		s.model.PhotoURLs[id] = url
	}
	s.applyChatDirectoryLocked(s.directory)
	// Locally owned, as of now rather than as of the snapshot.
	s.model.Draft = s.drafts[s.model.SelectedID]
	s.model.Preferences.Drafts = copyChatDrafts(s.drafts)
	s.model.EditDrafts = copyChatDrafts(s.editDrafts)
	s.model.Notice, s.model.NoticeRetry = s.notice, retry && s.notice != ""
	if cursor.ConversationID != "" {
		s.cursor = cursor
	}
	if reloaded && !preservedThreadPage && len(s.model.ThreadMessages) > 0 {
		s.threadBeforeSequence = s.model.ThreadMessages[0].Sequence
		s.threadAfterSequence = s.model.ThreadMessages[len(s.model.ThreadMessages)-1].Sequence
	}
	sortChatMessagesBySequence(s.model.Messages, s.cursor.Seen)
	return true
}

// restoreDraft puts a body back in the composer after a send that failed.
//
// It is the only path that may do so. The draft is cleared before the send
// leaves, so nothing else needs to write the sent text back, and nothing else
// is allowed to: every other restore was the bug.
func (s *chatState) restoreDraft(conversationID, body string) {
	s.mu.Lock()
	if s.drafts == nil {
		s.drafts = make(map[string]string)
	}
	if s.draftRevs == nil {
		s.draftRevs = make(map[string]uint64)
	}
	if conversationID != "" && body != "" {
		s.draftRev++
		s.draftRevs[conversationID] = s.draftRev
		s.drafts[conversationID] = body
		if s.model.SelectedID == conversationID {
			s.model.Draft = body
		}
		s.model.Preferences.Drafts = copyChatDrafts(s.drafts)
	}
	s.mu.Unlock()
}

// alreadyOpen reports whether id is the open conversation with a live stream
// and nothing to recover. Re-opening it would cancel that stream for nothing.
func (s *chatState) alreadyOpen(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return id != "" &&
		s.model.SelectedID == id &&
		s.opening == "" &&
		s.loadErr == "" &&
		s.cancelStream != nil &&
		s.streamFor == id &&
		s.streamGeneration == s.generation
}

// alreadyOpening keeps a second click on the selected room from cancelling
// its pending read and starting the same load again. A failed read clears
// opening, so selecting the room then remains an explicit retry.
func (s *chatState) alreadyOpening(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return id != "" && s.model.SelectedID == id && s.opening == id
}

// mergeDirectory adds resolved names to the session's directory.
//
// It merges rather than replaces, for two reasons. Two sources contribute (the
// session's own worker read and the product shell's People projection when
// there is one), and a source that comes back empty must never erase names
// already resolved -- replacing was what let an empty People projection leave
// every author as an id.
func (s *chatState) mergeDirectory(directory map[string]string) {
	if len(directory) == 0 {
		return
	}
	s.mu.Lock()
	if s.directory == nil {
		s.directory = make(map[string]string, len(directory))
	}
	for id, name := range directory {
		if name != "" {
			s.directory[id] = name
		}
	}
	s.mu.Unlock()
}

func (s *chatState) mergePhotos(photos map[string]string, workers bool) {
	if len(photos) == 0 {
		return
	}
	s.mu.Lock()
	if s.photos == nil {
		s.photos = make(map[string]string)
	}
	if workers && s.workerPhotos == nil {
		s.workerPhotos = make(map[string]string)
	}
	for id, url := range photos {
		if workers {
			s.workerPhotos[id] = url
		} else if _, known := s.workerPhotos[id]; known {
			continue
		}
		if url == "" {
			delete(s.photos, id)
		} else {
			s.photos[id] = url
		}
	}
	s.model.PhotoURLs = make(map[string]string, len(s.photos))
	for id, url := range s.photos {
		s.model.PhotoURLs[id] = url
	}
	s.mu.Unlock()
}

func (s *chatState) photoSnapshot() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.photos))
	for id, url := range s.photos {
		out[id] = url
	}
	return out
}

func (s *chatState) claimDMPeerRead(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dmPeerReads[id] {
		return false
	}
	s.dmPeerReads[id] = true
	return true
}

func (s *chatState) releaseDMPeerRead(id string) {
	s.mu.Lock()
	delete(s.dmPeerReads, id)
	s.mu.Unlock()
}

func chatPhotosFromWorkers(workers []*journeyv1.Worker) map[string]string {
	photos := make(map[string]string)
	for _, worker := range workers {
		if worker == nil {
			continue
		}
		for _, id := range []string{worker.GetWorkerRef(), worker.GetWorkerId()} {
			if id = strings.TrimSpace(id); id != "" {
				photos[id] = worker.GetProfilePhotoUrl()
			}
		}
	}
	return photos
}

func chatPhotos(people []productui.Person) map[string]string {
	photos := make(map[string]string)
	for _, person := range people {
		if strings.TrimSpace(person.PhotoURL) == "" {
			continue
		}
		for _, id := range []string{person.ID, person.WorkerID} {
			if id = strings.TrimSpace(id); id != "" {
				photos[id] = person.PhotoURL
			}
		}
	}
	return photos
}

// applyChatDirectory re-resolves the names already on screen.
//
// The directory read lands after the first render, so without this the messages
// and members drawn from it would keep their ids until something else reloaded
// them -- and Members is not re-projected by a route read at all.
func (s *chatState) applyChatDirectory(directory map[string]string) bool {
	if len(directory) == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.applyChatDirectoryLocked(directory)
}

func (s *chatState) applyChatDirectoryLocked(directory map[string]string) bool {
	changed := false
	resolve := func(id, current string) (string, bool) {
		name, ok := directory[id]
		if !ok || name == "" || name == current {
			return current, false
		}
		return name, true
	}
	resolveRoom := func(room *chatui.Conversation) {
		if room == nil {
			return
		}
		if room.Kind == chatui.DirectMessage && directory[s.model.PeerIDs[room.ID]] != "" {
			name := directory[s.model.PeerIDs[room.ID]]
			if name != room.Name {
				room.Name, changed = name, true
			}
			return
		}
		if name, moved := resolve(room.ID, room.Name); moved {
			room.Name, changed = name, true
		}
	}
	for i := range s.model.Messages {
		if name, moved := resolve(s.model.Messages[i].AuthorID, s.model.Messages[i].Author); moved {
			s.model.Messages[i].Author, changed = name, true
		}
		if name, moved := resolve(s.model.Messages[i].ForwardedAuthorID, s.model.Messages[i].ForwardedAuthor); moved {
			s.model.Messages[i].ForwardedAuthor, changed = name, true
		}
	}
	for i := range s.model.ThreadMessages {
		if name, moved := resolve(s.model.ThreadMessages[i].AuthorID, s.model.ThreadMessages[i].Author); moved {
			s.model.ThreadMessages[i].Author, changed = name, true
		}
		if name, moved := resolve(s.model.ThreadMessages[i].ForwardedAuthorID, s.model.ThreadMessages[i].ForwardedAuthor); moved {
			s.model.ThreadMessages[i].ForwardedAuthor, changed = name, true
		}
	}
	for i := range s.model.Members {
		if name, moved := resolve(s.model.Members[i].ID, s.model.Members[i].Name); moved {
			s.model.Members[i].Name, changed = name, true
		}
	}
	// A direct or group room is named after the people in it, so its rail and
	// browse rows resolve through the same table as everything else. One
	// resolver, or the same person reads as "Rafael Torres" on a message and
	// "Rafael" in the member list.
	for i := range s.model.Conversations {
		resolveRoom(&s.model.Conversations[i])
	}
	for i := range s.model.Sections {
		for j := range s.model.Sections[i].Chats {
			resolveRoom(&s.model.Sections[i].Chats[j])
		}
	}
	if name, moved := resolve(s.model.CurrentUser, s.model.CurrentUserName); moved {
		s.model.CurrentUserName, changed = name, true
	}
	for i := range s.model.Browse {
		resolveRoom(&s.model.Browse[i])
	}
	for i := range s.browseAll {
		resolveRoom(&s.browseAll[i])
	}
	return changed
}

// directorySnapshot is the names resolved so far. An id missing from it renders
// as itself, which is what the reader sees only while the read is in flight.
func (s *chatState) directorySnapshot() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.directory))
	for id, name := range s.directory {
		out[id] = name
	}
	return out
}

// claimDirectoryRead reports whether this session still owes itself a worker
// directory read, and records that it is being made. One read per session: the
// directory is the same for every conversation in it.
func (s *chatState) claimDirectoryRead() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.directoryRead {
		return false
	}
	s.directoryRead = true
	return true
}

func (s *chatState) claimDirectoryReadFor(cfg journeyclient.Config) (uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.Tenant != cfg.Tenant || s.cfg.Subject != cfg.Subject || s.cfg.Bearer != cfg.Bearer || s.directoryRead || time.Now().Before(s.directoryRetryAfter) {
		return s.directoryEpoch, false
	}
	s.directoryRead = true
	return s.directoryEpoch, true
}

// releaseDirectoryRead lets a failed read be tried again.
func (s *chatState) releaseDirectoryRead() {
	s.mu.Lock()
	s.directoryRead = false
	s.mu.Unlock()
}

func (s *chatState) releaseDirectoryReadAt(epoch uint64, cfg journeyclient.Config) {
	s.mu.Lock()
	if s.directoryEpoch == epoch && s.cfg.Tenant == cfg.Tenant && s.cfg.Subject == cfg.Subject && s.cfg.Bearer == cfg.Bearer {
		s.directoryRead = false
		s.directoryRetryAfter = time.Now().Add(2 * time.Second)
	}
	s.mu.Unlock()
}

func (s *chatState) completeDirectoryRead(epoch uint64, cfg journeyclient.Config, directory, photos map[string]string, people []chatui.SearchPerson) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.directoryEpoch != epoch || s.cfg.Tenant != cfg.Tenant || s.cfg.Subject != cfg.Subject || s.cfg.Bearer != cfg.Bearer {
		return false
	}
	for id, name := range directory {
		if name != "" {
			s.directory[id] = name
		}
	}
	for id, photo := range photos {
		s.workerPhotos[id] = photo
		if photo == "" {
			delete(s.photos, id)
		} else {
			s.photos[id] = photo
		}
	}
	s.model.PhotoURLs = make(map[string]string, len(s.photos))
	for id, photo := range s.photos {
		s.model.PhotoURLs[id] = photo
	}
	s.model.SearchDirectory = people
	s.applyChatDirectoryLocked(directory)
	return true
}

// chatDirectoryFromWorkers indexes the worker directory by both identifiers a
// chat subject id can take. A preferred name wins over a legal one, which is
// the same rule the People page renders by.
func chatDirectoryFromWorkers(workers []*journeyv1.Worker) map[string]string {
	directory := make(map[string]string, len(workers)*2)
	for _, worker := range workers {
		if worker == nil {
			continue
		}
		// The governed read may withhold the legal name (it is a restricted
		// field); the worker reference still carries the surname, so the
		// preferred name is completed from it rather than shown bare.
		legal := worker.GetLegalName()
		if strings.TrimSpace(legal) == "" {
			legal = humanizeChatSubjectID(worker.GetWorkerRef())
		}
		name := chatWorkerDisplayName(worker.GetPreferredName(), legal)
		if name == "" {
			continue
		}
		for _, id := range []string{worker.GetWorkerRef(), worker.GetWorkerId()} {
			if id = strings.TrimSpace(id); id != "" {
				directory[id] = name
			}
		}
	}
	return directory
}

// chatWorkerDisplayName is the name chat shows for a worker: the preferred
// name, completed with the legal surname when the preferred name is a single
// word. "Rafael" + "Rafael Torres" reads "Rafael Torres", "Bob" + "Robert
// Smith" reads "Bob Smith", and a preferred name that is already full stands.
// Half the room on first names and half on full names (the humanized fallback
// and the direct-message titles are full names) made one person look like
// two.
func chatWorkerDisplayName(preferred, legal string) string {
	preferred, legal = strings.TrimSpace(preferred), strings.TrimSpace(legal)
	if preferred == "" {
		return legal
	}
	if legal == "" || strings.Contains(preferred, " ") {
		return preferred
	}
	parts := strings.Fields(legal)
	if len(parts) < 2 || strings.EqualFold(parts[len(parts)-1], preferred) {
		return preferred
	}
	return preferred + " " + parts[len(parts)-1]
}

func (s *chatState) streamCursor() chatCursor {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cursor := s.cursor
	cursor.Seen = make(map[string]uint64, len(s.cursor.Seen))
	for id, sequence := range s.cursor.Seen {
		cursor.Seen[id] = sequence
	}
	return cursor
}

// applyStreamEvent folds one delivered event into the model.
//
// It reports the outcome and whether this subscription is still the live one.
// The two checks together are what stops a late event from the conversation
// the reader left from appearing in the one they are now reading.
func (s *chatState) applyStreamEvent(generation uint64, conversationID string, event *chatv1.ConversationEvent, locale string, directory map[string]string, now time.Time, resume string) (chatEventOutcome, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generation != generation || s.cursor.ConversationID != conversationID || s.model.SelectedID != conversationID {
		return chatEventDuplicate, false
	}
	oldest := uint64(0)
	if len(s.model.Messages) > 0 {
		oldest = s.model.Messages[0].Sequence
	}
	outcome := applyChatEvent(&s.model, &s.cursor, event, locale, directory, now)
	if outcome == chatEventApplied || outcome == chatEventUnordered {
		if event.GetKind() == chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_REACTION_CHANGED {
			s.applyReactionEventMembers(event, s.cfg.Tenant)
		}
	}
	if s.model.ThreadHasOlder && len(s.model.ThreadMessages) > 0 && (s.threadBeforeSequence == 0 || s.model.ThreadMessages[0].Sequence > s.threadBeforeSequence) {
		s.threadBeforeSequence = s.model.ThreadMessages[0].Sequence
	}
	if oldest > 0 && len(s.model.Messages) > 0 && s.model.Messages[0].Sequence > oldest {
		s.olderCursor = ""
	}
	if outcome == chatEventApplied {
		pruneChatSeen(&s.cursor, s.model.Messages, s.model.ThreadMessages)
		s.pruneRetainedReactions()
	}
	if outcome == chatEventApplied && resume != "" {
		s.cursor.Resume = resume
	}
	return outcome, true
}

// applyCatchup folds a forward ListPosts read into the timeline after a gap or
// between two streams. Posts already applied are skipped by id, so a
// replayed page is not a duplicated timeline.
func (s *chatState) applyCatchup(generation uint64, conversationID string, posts []*chatv1.Post, locale string, directory map[string]string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generation != generation || s.cursor.ConversationID != conversationID || s.model.SelectedID != conversationID {
		return false
	}
	if s.cursor.Seen == nil {
		s.cursor.Seen = map[string]uint64{}
	}
	applied := false
	replies := make(map[string]int)
	for _, post := range posts {
		if post == nil || post.GetId() == "" {
			continue
		}
		if seen, ok := s.cursor.Seen[post.GetId()]; ok && seen >= post.GetSequence() {
			continue
		}
		s.cursor.Seen[post.GetId()] = post.GetSequence()
		if post.GetSequence() > s.cursor.LastSequence {
			s.cursor.LastSequence = post.GetSequence()
		}
		applied = true
		switch {
		case post.GetDeleted():
			applyChatPostDeleted(&s.model, post)
		case post.GetParentId() != "":
			replies[post.GetParentId()]++
			if s.model.ThreadParent != nil && s.model.ThreadParent.ID == post.GetParentId() {
				s.model.ThreadParent.Replies++
			}
			if s.model.ShowThread && s.model.ThreadParentID == post.GetParentId() && liveChatThreadAccepts(&s.model, post.GetSequence()) && !chatHasMessage(s.model.ThreadMessages, post.GetId()) {
				s.model.ThreadMessages = append(s.model.ThreadMessages, chatMessage(post, locale, directory, now))
				s.boundLiveThread()
			}
		case chatHasMessage(s.model.Messages, post.GetId()):
			applyChatPostEdited(&s.model, post, locale, directory, now)
		default:
			if !s.model.HasNewer || len(s.model.Messages) == 0 || post.GetSequence() <= s.model.Messages[len(s.model.Messages)-1].Sequence {
				s.model.Messages = append(s.model.Messages, chatMessage(post, locale, directory, now))
			}
		}
	}
	if !applied {
		return false
	}
	for parent, count := range replies {
		for i := range s.model.Messages {
			if s.model.Messages[i].ID == parent {
				s.model.Messages[i].Replies += count
				break
			}
		}
	}
	sortChatMessagesBySequence(s.model.Messages, s.cursor.Seen)
	trimmed := len(s.model.Messages) > chatMessageWindow
	window := s.window
	if window <= 0 {
		window = chatMessageWindow
	}
	s.model.Messages, s.model.HasOlder = boundChatMessages(s.model.Messages, window, s.model.HasOlder)
	if trimmed {
		s.olderCursor = ""
	}
	pruneChatSeen(&s.cursor, s.model.Messages, s.model.ThreadMessages)
	s.pruneRetainedReactions()
	if len(s.model.Messages) > 0 && s.model.State == chatui.StateEmpty {
		s.model.State = chatui.StateReady
	}
	return true
}

// -- projection ------------------------------------------------------------

// chatDirectory indexes the authorized people projection the product shell
// already loaded, so a chat author is a person's name rather than a subject
// id. Chat subject ids and worker refs are the same identifier space (the
// shell binds a persona's worker ref to the verified subject), so this is a
// lookup and not a guess.
func chatDirectory(people []productui.Person) map[string]string {
	directory := make(map[string]string, len(people)*2)
	for _, person := range people {
		// The projection's Name is the preferred (first) name. Chat shows the
		// same composed form the worker read produces, or the People page
		// and the timeline disagree about who "Rafael" is.
		legal := strings.TrimSpace(person.LegalName)
		if legal == "" {
			legal = humanizeChatSubjectID(strings.TrimSpace(person.ID))
		}
		preferred := strings.TrimSpace(person.PreferredName)
		if preferred == "" {
			preferred = strings.TrimSpace(person.Name)
		}
		name := chatWorkerDisplayName(preferred, legal)
		if name == "" {
			continue
		}
		for _, id := range []string{person.ID, person.WorkerID} {
			if id = strings.TrimSpace(id); id != "" {
				directory[id] = name
			}
		}
	}
	return directory
}

// chatDisplayName resolves one subject id.
//
// The directory wins. Failing that the identifier is humanized, because the
// directory is not always complete for the reader: ListWorkers is filtered by
// the principal's organization visibility, so a manager restricted to their own
// unit receives a subset and every author outside it was rendering as the raw
// slug "hc-050-rafael-torres". An identifier that does not read as a name is
// left exactly as it is -- a slug is bad, an invented name is worse.
func chatDisplayName(directory map[string]string, id string) string {
	if name, ok := directory[id]; ok && name != "" {
		return name
	}
	return humanizeChatSubjectID(id)
}

func chatMembershipDisplayName(directory map[string]string, subjectID, homeTenantID, viewerTenantID string) string {
	if homeTenantID != viewerTenantID {
		return ""
	}
	name := chatDisplayName(directory, subjectID)
	if name == subjectID {
		return ""
	}
	return name
}

// chatHumanizedWordLimit bounds how many words a humanized identifier may
// produce. A person has a name, not a sentence; anything longer is an opaque
// identifier that happens to contain letters.
const chatHumanizedWordLimit = 4

// humanizeChatSubjectID reads a name out of a worker reference, or gives the
// reference back unchanged.
//
// "hc-050-rafael-torres" is a tenant prefix, a number and a name. The prefix
// and the number are dropped and the rest is title-cased, so the reader sees
// "Rafael Torres". An identifier whose remaining parts are not words -- a
// UUID, a hash, a bare code -- is returned as it came, because guessing at one
// produces a name that belongs to nobody.
func humanizeChatSubjectID(id string) string {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return id
	}
	// A subject id can be an address. Its local part carries the name.
	if at := strings.IndexByte(trimmed, '@'); at > 0 {
		trimmed = trimmed[:at]
	}
	parts := strings.FieldsFunc(trimmed, func(r rune) bool { return r == '-' || r == '.' || r == '_' })
	// Drop a leading code: a run of digits, or letters immediately followed by
	// digits ("hc" then "050").
	for len(parts) > 1 {
		if chatAllDigits(parts[0]) {
			parts = parts[1:]
			continue
		}
		if chatAllLetters(parts[0]) && chatAllDigits(parts[1]) {
			parts = parts[2:]
			continue
		}
		break
	}
	if len(parts) == 0 || len(parts) > chatHumanizedWordLimit {
		return id
	}
	words := make([]string, 0, len(parts))
	for _, part := range parts {
		if !chatAllLetters(part) {
			return id
		}
		words = append(words, strings.ToUpper(part[:1])+strings.ToLower(part[1:]))
	}
	return strings.Join(words, " ")
}

func chatAllDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func chatAllLetters(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}

func chatConversation(v *chatv1.Conversation) chatui.Conversation {
	kind := chatui.PublicChannel
	switch v.GetKind() {
	case chatv1.ConversationKind_CONVERSATION_KIND_PRIVATE_CHANNEL:
		kind = chatui.PrivateChannel
	case chatv1.ConversationKind_CONVERSATION_KIND_DIRECT:
		kind = chatui.DirectMessage
	case chatv1.ConversationKind_CONVERSATION_KIND_GROUP:
		kind = chatui.GroupChat
	}
	name := v.GetName()
	if strings.TrimSpace(name) == "" {
		name = v.GetId()
	}
	// Joined is true for the rail: these are the rooms the caller is in.
	// chatBrowseConversation is the one that reads the server's flag, because
	// it is only meaningful on a discoverable listing.
	conversation := chatui.Conversation{ID: v.GetId(), Name: name, Kind: kind, Joined: true, MemberCount: int(v.GetMemberCount()), OwnerID: v.GetOwnerId(), HostTenantID: v.GetTenantId()}
	if at := v.GetLastActivityAt(); at != nil && at.IsValid() {
		conversation.LastActivity = at.AsTime()
	}
	return conversation
}

// chatBrowseConversation projects a row from a discoverable listing, where
// joined tells a channel the caller can join from one they are already in.
func chatBrowseConversation(v *chatv1.Conversation) chatui.Conversation {
	conversation := chatConversation(v)
	conversation.Joined = v.GetJoined()
	return conversation
}

func chatKind(v chatui.ConversationKind) chatv1.ConversationKind {
	switch v {
	case chatui.PrivateChannel:
		return chatv1.ConversationKind_CONVERSATION_KIND_PRIVATE_CHANNEL
	case chatui.DirectMessage:
		return chatv1.ConversationKind_CONVERSATION_KIND_DIRECT
	case chatui.GroupChat:
		return chatv1.ConversationKind_CONVERSATION_KIND_GROUP
	default:
		return chatv1.ConversationKind_CONVERSATION_KIND_PUBLIC_CHANNEL
	}
}

// chatMessage projects one post.
//
// The author is never "You". A timeline that renames the reader's own posts
// loses the one fact a reader needs when two people share a screen or when a
// post is quoted: who wrote it. AuthorID carries identity, Author carries the
// resolved name, and the styling of one's own messages is the renderer's job.
func chatMessage(v *chatv1.Post, locale string, directory map[string]string, now time.Time) chatui.Message {
	when := time.Time{}
	if ts := v.GetCreatedAt(); ts != nil {
		when = ts.AsTime()
	}
	forwardedAuthor := ""
	forwardedAuthorID := ""
	if source := v.GetSourceAttribution(); source != nil && source.GetOriginalAuthorId() != "" {
		forwardedAuthorID = source.GetOriginalAuthorId()
		forwardedAuthor = chatDisplayName(directory, source.GetOriginalAuthorId())
	}
	return chatui.Message{
		ID:        v.GetId(),
		AuthorID:  v.GetAuthorId(),
		Author:    chatDisplayName(directory, v.GetAuthorId()),
		Body:      v.GetBody(),
		Revision:  v.GetRevision(),
		Sequence:  v.GetSequence(),
		SentAt:    when,
		TimeLabel: chatTimeLabel(when, now, locale),
		Edited:    v.GetRevision() > 1,
		// URLs are empty here on purpose: the renderer shows a chip until a
		// grant produces one, and the timeline never waits on bytes.
		Attachments:       chatMediaAttachments(v),
		ForwardedAuthor:   forwardedAuthor,
		ForwardedAuthorID: forwardedAuthorID,
	}
}

// chatMessages projects one ListPosts page into the timeline.
//
// Replies is the count of posts in the page whose parent is this post, which
// is the thread count the data already carries; Reactions comes from the
// bounded reaction read (there is no bulk count RPC). Deleted posts leave the
// timeline, and only roots are timeline entries.
func chatMessages(posts []*chatv1.Post, locale string, directory map[string]string, reactions map[string][]chatui.ReactionChip, now time.Time) []chatui.Message {
	replies := make(map[string]int, len(posts))
	for _, post := range posts {
		if post == nil || post.GetDeleted() {
			continue
		}
		if parent := post.GetParentId(); parent != "" {
			replies[parent]++
		}
	}
	ordered := chatInConversationOrder(posts)
	out := make([]chatui.Message, 0, len(ordered))
	for _, post := range ordered {
		if post.GetDeleted() || post.GetParentId() != "" {
			continue
		}
		message := chatMessage(post, locale, directory, now)
		message.Replies = replies[message.ID]
		if chips := reactions[message.ID]; len(chips) > 0 {
			message.Chips = append([]chatui.ReactionChip(nil), chips...)
			message.Reactions, message.Reacted = reactionTotal(message.Chips)
		}
		out = append(out, message)
	}
	return out
}

// chatThreadMessages projects the replies under one root.
func chatThreadMessages(posts []*chatv1.Post, root, locale string, directory map[string]string, now time.Time) []chatui.Message {
	if root == "" {
		return nil
	}
	out := make([]chatui.Message, 0, 8)
	for _, post := range posts {
		if post == nil || post.GetDeleted() || post.GetParentId() != root {
			continue
		}
		out = append(out, chatMessage(post, locale, directory, now))
	}
	return out
}

// chatLastSequence is the highest sequence in a page, which is where a
// subscription starts and where a catch-up resumes.
func chatLastSequence(posts []*chatv1.Post) uint64 {
	var last uint64
	for _, post := range posts {
		if post != nil && post.GetSequence() > last {
			last = post.GetSequence()
		}
	}
	return last
}

// chatPostWindow returns the newest ids in a timeline, bounded, for the
// reaction read.
func chatPostWindow(messages []chatui.Message, limit int) []string {
	if limit <= 0 || len(messages) == 0 {
		return nil
	}
	start := 0
	if len(messages) > limit {
		start = len(messages) - limit
	}
	out := make([]string, 0, len(messages)-start)
	for _, message := range messages[start:] {
		if message.ID != "" {
			out = append(out, message.ID)
		}
	}
	return out
}

// chatTwelveHourLocales are the locales this product renders a message clock
// in twelve-hour form. Everything else, including en-GB and de-DE, gets the
// twenty-four-hour clock its readers expect; the previous code printed
// time.Kitchen ("3:04PM") for every locale in the world.
var chatTwelveHourLocales = map[string]bool{
	"en-us": true, "en-ca": true, "en-au": true, "en-nz": true, "en-ph": true, "en-in": true,
}

// chatTimeLabel renders a message timestamp for one locale. Today's messages
// carry a clock; older ones carry the locale's date as well, because "3:04 PM"
// on a post from March is not a timestamp.
func chatTimeLabel(when, now time.Time, locale string) string {
	if when.IsZero() {
		return ""
	}
	local := when.Local()
	clock := "15:04"
	if chatTwelveHourLocales[strings.ToLower(strings.TrimSpace(locale))] {
		clock = "3:04 PM"
	}
	// The day is the divider's job: every row under "September 8, 2026"
	// repeating the date was noise, and the thread pane keeps the day in
	// its root's context. Only the clock is shown here.
	_ = now
	return localizeDigits(local.Format(clock), productui.ResolveProductLocale(locale))
}

// chatNumberFormatter writes counts in the locale's numerals, through the
// same table the clock uses.
func chatNumberFormatter(locale string) func(int) string {
	resolved := productui.ResolveProductLocale(locale)
	return func(n int) string {
		if n == 0 {
			return ""
		}
		return localizeDigits(strconv.Itoa(n), resolved)
	}
}

// localizeDigits writes a clock in the locale's own numerals. The date half
// of a label already comes from the locale (Arabic-Indic digits under ar), and
// a time in Latin digits beside it read as two systems in one string.
func localizeDigits(clock string, locale productui.LocaleContext) string {
	digits := [10]string{}
	changed := false
	for i := range digits {
		digits[i] = locale.FormatNumber(string(rune('0'+i)), 0)
		if digits[i] != string(rune('0'+i)) {
			changed = true
		}
	}
	if !changed {
		return clock
	}
	var out strings.Builder
	for _, r := range clock {
		if r >= '0' && r <= '9' {
			out.WriteString(digits[r-'0'])
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

func sameCalendarDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// joinableChatConversations is the browse list: the discoverable rows the
// caller has not joined.
//
// The server decides who may see which channel, so this reads its joined flag
// rather than subtracting the rail from the listing: a client-side difference
// would also have to know the discovery policy, and it does not.
func joinableChatConversations(all []*chatv1.Conversation) []chatui.Conversation {
	out := make([]chatui.Conversation, 0, len(all))
	for _, conversation := range all {
		if conversation == nil || conversation.GetArchived() || conversation.GetJoined() {
			continue
		}
		out = append(out, chatBrowseConversation(conversation))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// chatInConversationOrder puts a page in reading order, oldest first.
//
// It sorts by sequence rather than reversing. A descending request is a
// backward *page*, and the store hands that page back oldest-first already
// (internal/data/chatstore/contracts_adapter.go, "it is handed back
// oldest-first so a caller renders it in conversation order without reversing
// it itself"); reversing it was what put the newest message at the top of the
// timeline. Sorting is right whichever way the rows arrive, so the client
// cannot be wrong about the store's convention a second time.
func chatInConversationOrder(posts []*chatv1.Post) []*chatv1.Post {
	out := make([]*chatv1.Post, 0, len(posts))
	for _, post := range posts {
		if post != nil {
			out = append(out, post)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].GetSequence() < out[j].GetSequence() })
	return out
}

// sortChatMessagesBySequence orders a timeline by conversation sequence.
//
// chatui.Message carries no sequence, so the order comes from the cursor's own
// id-to-sequence index. Timestamps are not the authority: posts committed in
// the same moment share a created_at, and sequence is the only total order the
// conversation has.
func sortChatMessagesBySequence(messages []chatui.Message, sequences map[string]uint64) {
	sort.SliceStable(messages, func(i, j int) bool {
		left, right := sequences[messages[i].ID], sequences[messages[j].ID]
		if left == 0 {
			left = messages[i].Sequence
		}
		if right == 0 {
			right = messages[j].Sequence
		}
		if left != 0 && right != 0 {
			return left < right
		}
		// One of them is not in the index -- a search hit, or a post whose
		// event has not landed. Fall back to the clock rather than sorting an
		// unknown sequence to the front as zero.
		return messages[i].SentAt.Before(messages[j].SentAt)
	})
}

// prependChatMessages puts an older page in front of the timeline, skipping
// anything already on screen.
func prependChatMessages(existing, older []chatui.Message) []chatui.Message {
	if len(older) == 0 {
		return existing
	}
	held := make(map[string]bool, len(existing))
	for _, message := range existing {
		held[message.ID] = true
	}
	out := make([]chatui.Message, 0, len(existing)+len(older))
	for _, message := range older {
		if !held[message.ID] {
			out = append(out, message)
		}
	}
	return append(out, existing...)
}

// noteChatActivity records a conversation's newest post time.
func (s *chatState) noteChatActivity(conversationID string, at time.Time) {
	if conversationID == "" || at.IsZero() {
		return
	}
	s.mu.Lock()
	if s.lastActivity == nil {
		s.lastActivity = make(map[string]time.Time)
	}
	if at.After(s.lastActivity[conversationID]) {
		s.lastActivity[conversationID] = at
	}
	s.mu.Unlock()
}

func (s *chatState) activitySnapshot() map[string]time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]time.Time, len(s.lastActivity))
	for id, at := range s.lastActivity {
		out[id] = at
	}
	return out
}

// sortChatRail puts the room with the newest message first.
//
// Only what this client has read can order it: a conversation carries no
// last-post or updated-at field on the wire (see the report), so a room this
// session has never opened has no time and sorts after the ones that do, by
// name. Revision is the tie-break before name because a renamed or re-settinged
// room is at least known to have changed.
func sortChatRail(conversations []chatui.Conversation, activity map[string]time.Time) {
	sort.SliceStable(conversations, func(i, j int) bool {
		left, right := activity[conversations[i].ID], activity[conversations[j].ID]
		switch {
		case !left.IsZero() && !right.IsZero() && !left.Equal(right):
			return left.After(right)
		case !left.IsZero() && right.IsZero():
			return true
		case left.IsZero() && !right.IsZero():
			return false
		}
		return conversations[i].Name < conversations[j].Name
	})
}

// filterChatBrowse is the rows a browse query matches.
//
// Case-insensitive, over the name and the topic, and it returns the whole list
// for an empty query. It is a filter over the listing already in hand, never a
// reason to ask the server again.
func filterChatBrowse(all []chatui.Conversation, query string) []chatui.Conversation {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return append([]chatui.Conversation(nil), all...)
	}
	out := make([]chatui.Conversation, 0, len(all))
	for _, conversation := range all {
		name := strings.ToLower(conversation.Name)
		topic := strings.ToLower(conversation.Topic)
		if strings.Contains(name, needle) || strings.Contains(topic, needle) {
			out = append(out, conversation)
		}
	}
	return out
}

// setChatBrowse records the discoverable listing and shows what the current
// query matches.
func (s *chatState) setChatBrowse(all []chatui.Conversation) chatui.Model {
	return s.mutate(func(model *chatui.Model) {
		s.browseAll = all
		model.Browse = filterChatBrowse(all, model.BrowseQuery)
	})
}

// filterChatBrowseQuery applies a typed query to the listing already held.
func (s *chatState) filterChatBrowseQuery(query string) chatui.Model {
	return s.mutate(func(model *chatui.Model) {
		model.BrowseQuery = query
		model.Browse = filterChatBrowse(s.browseAll, query)
	})
}

// chatJoinedSet indexes the conversations already in the rail.
func chatJoinedSet(conversations []chatui.Conversation) map[string]bool {
	joined := make(map[string]bool, len(conversations))
	for _, conversation := range conversations {
		joined[conversation.ID] = true
	}
	return joined
}

// -- streaming -------------------------------------------------------------

// classifyChatSequence decides what an event's sequence means for a cursor.
//
// The server is asked for events after last, so the next one is last+1.
// Anything at or below last has been applied already (at-least-once delivery
// and every resubscribe replay), and anything above last+1 means an event was
// missed, which is not a render decision but a catch-up.
func classifyChatSequence(last, sequence uint64, baselined bool) chatEventOutcome {
	switch {
	case sequence == 0:
		return chatEventUnordered
	case !baselined:
		// The first ordered event on a stream sets the position rather than
		// being measured against one.
		return chatEventApplied
	case sequence <= last:
		return chatEventDuplicate
	case sequence == last+1:
		return chatEventApplied
	default:
		return chatEventGap
	}
}

// classifyChatEvent adds identity dedupe to the sequence decision: a post id
// already applied is a duplicate whatever sequence it arrives with.
func classifyChatEvent(cursor chatCursor, postID string, postSequence, streamSequence uint64, createsPost bool) chatEventOutcome {
	// Identity dedupe is for a replayed *creation* only. An edit, a delete, a
	// reaction or a pin is always about a post already on screen, so matching
	// on the id would drop every one of them.
	if createsPost && postID != "" && cursor.Seen != nil {
		if seen, ok := cursor.Seen[postID]; ok && postSequence > 0 && seen >= postSequence {
			return chatEventDuplicate
		}
	}
	// A creation at or below the history already loaded is not news. The
	// ListPosts page was the authority for every sequence up to LastSequence:
	// a post there is on screen (caught above), older than the retained
	// window (LoadOlder's job), or gone. An unknown id in that range is the
	// third case -- the outbox keeps events for posts the store no longer has
	// (a hard purge, a seeder reset) -- and a fresh subscribe replays the
	// whole outbox, so without this the purged history came back as a second
	// copy of every message, interleaved with the real one by sequence.
	if createsPost && postID != "" && postSequence > 0 && postSequence <= cursor.LastSequence {
		if _, ok := cursor.Seen[postID]; !ok {
			return chatEventDuplicate
		}
	}
	return classifyChatSequence(cursor.StreamSequence, streamSequence, cursor.Baselined)
}

// chatEventPost is the post an event is about, whatever kind it is.
func chatEventPost(event *chatv1.ConversationEvent) *chatv1.Post {
	if event == nil {
		return nil
	}
	if post := event.GetPost(); post != nil {
		return post
	}
	return nil
}

// applyChatEvent folds one event into the model and moves the cursor.
//
// It returns the outcome so the caller can act on it: applied and unordered
// events are rendered, duplicates are dropped, and a gap sends the caller to
// the ListPosts cursor before it resubscribes.
func applyChatEvent(model *chatui.Model, cursor *chatCursor, event *chatv1.ConversationEvent, locale string, directory map[string]string, now time.Time) chatEventOutcome {
	if model == nil || cursor == nil || event == nil {
		return chatEventDuplicate
	}
	if cursor.Seen == nil {
		cursor.Seen = map[string]uint64{}
	}
	post := chatEventPost(event)
	postID := post.GetId()
	streamSequence := event.GetSequence()
	postSequence := post.GetSequence()
	creates := event.GetKind() == chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_POST_CREATED
	outcome := classifyChatEvent(*cursor, postID, postSequence, streamSequence, creates)
	if outcome == chatEventGap {
		// A gap must not move the position: the catch-up needs to know where
		// the hole started.
		return outcome
	}
	if outcome == chatEventDuplicate {
		// The payload is already on screen, but the server did deliver this
		// event, so the watch position moves past it. Otherwise every
		// reconnect replays the whole backlog again.
		advanceChatWatchPosition(cursor, streamSequence)
		return outcome
	}

	switch event.GetKind() {
	case chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_POST_CREATED:
		applyChatPostCreated(model, post, cursor.Seen, locale, directory, now)
		if postID != "" && postSequence > 0 {
			// The sort index needs this post before the timeline is ordered.
			cursor.Seen[postID] = postSequence
		}
	case chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_POST_EDITED:
		applyChatPostEdited(model, post, locale, directory, now)
	case chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_POST_DELETED:
		applyChatPostDeleted(model, post)
	case chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_REACTION_CHANGED:
		applyChatReactionChanged(model, event, model.CurrentUser)
	case chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_PIN_CHANGED:
		applyChatPinChanged(model, event)
	case chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_CONVERSATION_UPDATED:
		applyChatConversationUpdated(model, event.GetConversation())
	}

	advanceChatWatchPosition(cursor, streamSequence)
	if postSequence > cursor.LastSequence {
		cursor.LastSequence = postSequence
	}
	if postID != "" {
		cursor.Seen[postID] = postSequence
	}
	return outcome
}

// advanceChatWatchPosition moves the stream position and takes the baseline.
func advanceChatWatchPosition(cursor *chatCursor, streamSequence uint64) {
	if streamSequence == 0 {
		return
	}
	cursor.Baselined = true
	if streamSequence > cursor.StreamSequence {
		cursor.StreamSequence = streamSequence
	}
}

func applyChatPostCreated(model *chatui.Model, post *chatv1.Post, sequences map[string]uint64, locale string, directory map[string]string, now time.Time) {
	if post == nil || post.GetId() == "" {
		return
	}
	if parent := post.GetParentId(); parent != "" {
		if model.ThreadParent != nil && model.ThreadParent.ID == parent {
			model.ThreadParent.Replies++
		}
		for i := range model.Messages {
			if model.Messages[i].ID == parent {
				model.Messages[i].Replies++
				break
			}
		}
		if model.ShowThread && model.ThreadParentID == parent && liveChatThreadAccepts(model, post.GetSequence()) && !chatHasMessage(model.ThreadMessages, post.GetId()) {
			model.ThreadMessages = append(model.ThreadMessages, chatMessage(post, locale, directory, now))
			boundLiveChatThread(model)
		}
		return
	}
	if chatHasMessage(model.Messages, post.GetId()) {
		return
	}
	if model.HasNewer && len(model.Messages) > 0 && post.GetSequence() > model.Messages[len(model.Messages)-1].Sequence {
		return
	}
	model.Messages = append(model.Messages, chatMessage(post, locale, directory, now))
	sortChatMessagesBySequence(model.Messages, sequences)
	model.Messages, model.HasOlder = boundChatMessages(model.Messages, chatMessageWindow, model.HasOlder)
	if model.State == chatui.StateEmpty {
		model.State = chatui.StateReady
	}
}

func applyChatPostEdited(model *chatui.Model, post *chatv1.Post, locale string, directory map[string]string, now time.Time) {
	if post == nil {
		return
	}
	next := chatMessage(post, locale, directory, now)
	for i := range model.Messages {
		if model.Messages[i].ID == next.ID {
			model.Messages[i] = carryChatLocalState(model.Messages[i], next)
			break
		}
	}
	for i := range model.ThreadMessages {
		if model.ThreadMessages[i].ID == next.ID {
			model.ThreadMessages[i] = carryChatLocalState(model.ThreadMessages[i], next)
			break
		}
	}
	if model.ThreadParent != nil && model.ThreadParent.ID == next.ID {
		parent := carryChatLocalState(*model.ThreadParent, next)
		model.ThreadParent = &parent
	}
}

// carryChatLocalState rebuilds a message from a newer post while keeping what
// the post does not carry: thread and reaction counts, the chips and the
// viewer's own flag, the pin, and the attachment URLs the grants produced.
// A replayed creation of a post already on screen goes through here too, and
// without this every replay wiped the chips (a room opened with one chip row
// and showed six on the next visit) and blanked every image back to a chip.
func carryChatLocalState(current, next chatui.Message) chatui.Message {
	if next.Revision < current.Revision {
		return current
	}
	next.Replies, next.Reactions, next.Reacted, next.Pinned = current.Replies, current.Reactions, current.Reacted, current.Pinned
	next.Chips = current.Chips
	if len(next.Attachments) > 0 && len(current.Attachments) > 0 {
		urls := make(map[string]string, len(current.Attachments))
		for _, a := range current.Attachments {
			if a.URL != "" {
				urls[a.ID] = a.URL
			}
		}
		for i := range next.Attachments {
			if next.Attachments[i].URL == "" {
				next.Attachments[i].URL = urls[next.Attachments[i].ID]
			}
		}
	}
	return next
}

func applyChatPostDeleted(model *chatui.Model, post *chatv1.Post) {
	id := post.GetId()
	if id == "" {
		return
	}
	model.Messages = chatWithoutMessage(model.Messages, id)
	model.ThreadMessages = chatWithoutMessage(model.ThreadMessages, id)
	if model.ThreadParent != nil && model.ThreadParent.ID == id {
		model.ThreadParent = nil
	}
	if parent := post.GetParentId(); parent != "" {
		if model.ThreadParent != nil && model.ThreadParent.ID == parent && model.ThreadParent.Replies > 0 {
			model.ThreadParent.Replies--
		}
		for i := range model.Messages {
			if model.Messages[i].ID == parent && model.Messages[i].Replies > 0 {
				model.Messages[i].Replies--
				break
			}
		}
	}
	if len(model.Messages) == 0 && model.State == chatui.StateReady {
		model.State = chatui.StateEmpty
	}
}

func applyChatReactionChanged(model *chatui.Model, event *chatv1.ConversationEvent, viewer string) {
	reaction := event.GetReaction()
	if reaction == nil {
		return
	}
	delta := 1
	if event.GetRemoved() {
		delta = -1
	}
	emoji := reaction.GetEmoji()
	if emoji == "" {
		emoji = chatDefaultReaction
	}
	for i := range model.Messages {
		if model.Messages[i].ID == reaction.GetPostId() {
			msg := &model.Messages[i]
			own := viewer != "" && reaction.GetSubjectId() == viewer
			if own && ownReactionAlreadyAt(msg.Chips, emoji, delta > 0) {
				return
			}
			msg.Chips = adjustChips(msg.Chips, emoji, delta, own)
			msg.Reactions, msg.Reacted = reactionTotal(msg.Chips)
			return
		}
	}
}

func applyChatPinChanged(model *chatui.Model, event *chatv1.ConversationEvent) {
	pin := event.GetPin()
	if pin == nil {
		return
	}
	for i := range model.Messages {
		if model.Messages[i].ID == pin.GetPostId() {
			model.Messages[i].Pinned = !event.GetRemoved()
			return
		}
	}
}

func applyChatConversationUpdated(model *chatui.Model, conversation *chatv1.Conversation) {
	if conversation == nil {
		return
	}
	next := chatConversation(conversation)
	for i := range model.Conversations {
		if model.Conversations[i].ID == next.ID {
			// Empty-name DMs use the opaque ID on the wire. A conversation
			// event must not replace the authorized peer label already resolved
			// from membership and the worker directory with that ID.
			if next.Kind == chatui.DirectMessage && model.PeerIDs[next.ID] != "" && next.Name == next.ID && model.Conversations[i].Name != "" && model.Conversations[i].Name != next.ID {
				next.Name = model.Conversations[i].Name
			}
			model.Conversations[i].Name = next.Name
			model.Conversations[i].Kind = next.Kind
			for section := range model.Sections {
				for row := range model.Sections[section].Chats {
					if model.Sections[section].Chats[row].ID == next.ID {
						model.Sections[section].Chats[row].Name = next.Name
						model.Sections[section].Chats[row].Kind = next.Kind
					}
				}
			}
			return
		}
	}
}

func chatHasMessage(messages []chatui.Message, id string) bool {
	for _, message := range messages {
		if message.ID == id {
			return true
		}
	}
	return false
}

func chatWithoutMessage(messages []chatui.Message, id string) []chatui.Message {
	for i := range messages {
		if messages[i].ID == id {
			return append(messages[:i:i], messages[i+1:]...)
		}
	}
	return messages
}

// boundChatMessages keeps the newest max messages. hasOlder stays true once
// anything has been dropped or an older page is known to exist, which is what
// the scroll-up affordance is for.
func boundChatMessages(messages []chatui.Message, max int, hasOlder bool) ([]chatui.Message, bool) {
	if max <= 0 || len(messages) <= max {
		return messages, hasOlder
	}
	retained := make([]chatui.Message, max)
	copy(retained, messages[len(messages)-max:])
	return retained, true
}

// pruneChatSeen keeps the sequence index for retained posts and a short
// recent replay overlap. LastSequence and the opaque stream cursor continue
// to guard the rest of history without retaining every post ID.
func pruneChatSeen(cursor *chatCursor, messages, thread []chatui.Message) {
	if cursor == nil || len(cursor.Seen) <= chatMessageWindow+128 {
		return
	}
	keep := make(map[string]uint64, len(messages)+len(thread)+128)
	for _, message := range messages {
		if message.ID != "" {
			keep[message.ID] = message.Sequence
		}
	}
	for _, message := range thread {
		if message.ID != "" {
			keep[message.ID] = message.Sequence
		}
	}
	type seenPost struct {
		id       string
		sequence uint64
	}
	recent := make([]seenPost, 0, len(cursor.Seen))
	for id, sequence := range cursor.Seen {
		recent = append(recent, seenPost{id, sequence})
	}
	sort.Slice(recent, func(i, j int) bool { return recent[i].sequence > recent[j].sequence })
	for i, post := range recent {
		if i >= 128 {
			break
		}
		keep[post.id] = post.sequence
	}
	cursor.Seen = keep
}

// dropResumeCursor forgets a resume cursor the server refused, so the next
// subscription resumes from the plain sequence instead. after_sequence and
// resume_cursor are never sent together (see chatWatchStart), so dropping one
// is the whole of the recovery.
func (s *chatState) dropResumeCursor(conversationID string) {
	s.mu.Lock()
	if s.cursor.ConversationID == conversationID {
		s.cursor.Resume = ""
	}
	s.mu.Unlock()
}

// oldestChatHeldSequence is the lowest sequence on screen, which is where a
// backward page starts when there is no cursor to follow.
func (s *chatState) oldestHeldSequence() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var oldest uint64
	for _, message := range s.model.Messages {
		sequence := message.Sequence
		if sequence == 0 {
			continue
		}
		if oldest == 0 || sequence < oldest {
			oldest = sequence
		}
	}
	return oldest
}
