//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Event timeline (#3964), browser half. The messages are read through the Chat
// search endpoint, a page at a time and one kind at a time; this file keeps the
// newest request, drops answers for a conversation or request that has been left,
// reads access again before a draft, and opens an entry's message through the
// paths a search result uses. Nothing it reads is stored.

var timelineReading struct {
	sync.Mutex
	run uint64
}

func timelineSet(conversationID string, change func(*chatui.TimelineView)) {
	chatBrowser.mutate(func(m *chatui.Model) {
		if m.SelectedID != conversationID {
			return
		}
		view := m.Timeline
		if view.ConversationID != conversationID {
			view = chatui.TimelineView{ConversationID: conversationID}
		}
		change(&view)
		m.Timeline = view
	})
	refreshChatRoute()
}

// timelineRead reads the whole range of one conversation: the kinds in turn, each
// page by page up to the limit. status is "" when it succeeded.
func timelineRead(cfg journeyclient.Config, conversationID string, start, end time.Time, replies bool) (entries []chatui.TimelineEntry, truncated bool, status string) {
	kinds := []chatsearch.Kind{chatsearch.Message}
	if replies {
		kinds = append(kinds, chatsearch.Thread)
	}
	reads := make([]timelineKindRead, 0, len(kinds))
	for _, kind := range kinds {
		query, err := timelineQuery(conversationID, kind, start, end)
		if err != nil {
			return nil, false, chatui.TimelineError
		}
		read := timelineKindRead{}
		cursor := ""
		inRange := 0
		for page := 1; ; page++ {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			var response chatsearch.Response
			// Transient: a timeline is not one of the person's searches and must
			// not be remembered among their recent ones.
			err := chatsearchRequest(ctx, http.DefaultClient, cfg, "/api/chat/search", http.MethodPost,
				chatsearch.Request{Query: query, DisplayQuery: query, Mode: "keyword", Cursor: cursor, Limit: timelinePageSize, Transient: true}, &response)
			cancel()
			if err != nil {
				return nil, false, chatui.TimelineError
			}
			rows, more, unavailable := timelineFromResponse(response, conversationID)
			if unavailable {
				return nil, false, chatui.TimelineUnavailable
			}
			read.entries = append(read.entries, rows...)
			cursor = response.NextCursor
			pastEnd := len(rows) > 0 && !rows[len(rows)-1].At.Before(end)
			read.more = more && !pastEnd
			var keep bool
			keep, inRange = timelineKeepReading(more, rows, inRange, page, start, end)
			if !keep {
				break
			}
		}
		reads = append(reads, read)
	}
	entries, truncated = timelineMerge(reads, start, end)
	return entries, truncated, ""
}

// loadTimeline reads the timeline of the open conversation for a range of days.
func loadTimeline(cfg journeyclient.Config, from, to string, replies bool) {
	conversationID := chatBrowser.selectedID()
	if conversationID == "" {
		return
	}
	start, end, problem := chatui.TimelineRange(from, to)
	timelineReading.Lock()
	timelineReading.run++
	run := timelineReading.run
	timelineReading.Unlock()
	if problem != "" {
		timelineSet(conversationID, func(v *chatui.TimelineView) {
			*v = chatui.TimelineView{ConversationID: conversationID, Status: chatui.TimelineError, From: from, To: to, Replies: replies}
		})
		return
	}
	timelineSet(conversationID, func(v *chatui.TimelineView) {
		*v = chatui.TimelineView{ConversationID: conversationID, Status: chatui.TimelineLoading, From: from, To: to, Replies: replies}
	})
	go func() {
		active := chatBrowser.config(cfg)
		entries, truncated, status := timelineRead(active, conversationID, start, end, replies)
		timelineReading.Lock()
		current := run == timelineReading.run
		timelineReading.Unlock()
		if !current || chatBrowser.selectedID() != conversationID {
			return
		}
		timelineSet(conversationID, func(v *chatui.TimelineView) {
			if status != "" {
				*v = chatui.TimelineView{ConversationID: conversationID, Status: status, From: from, To: to, Replies: replies}
				return
			}
			*v = chatui.TimelineView{ConversationID: conversationID, Status: chatui.TimelineReady, From: from, To: to, Replies: replies, Entries: entries, Truncated: truncated}
		})
	}()
}

// checkTimelineEntries reads the range again and reports how many of the entries
// asked about can no longer be shared as they were shown. The read is a fresh
// one: nothing the timeline holds in memory counts as access.
func checkTimelineEntries(cfg journeyclient.Config, ids []string, done func(chatui.TimelineCheck)) {
	model := chatBrowser.snapshot()
	conversationID, view := model.SelectedID, model.Timeline
	start, end, problem := chatui.TimelineRange(view.From, view.To)
	if conversationID == "" || view.ConversationID != conversationID || problem != "" {
		done(chatui.TimelineCheck{Failed: true})
		return
	}
	go func() {
		active := chatBrowser.config(cfg)
		fresh, _, status := timelineRead(active, conversationID, start, end, view.Replies)
		if status != "" || chatBrowser.selectedID() != conversationID {
			done(chatui.TimelineCheck{Failed: true})
			return
		}
		done(chatui.TimelineCheck{Changed: timelineChanged(ids, view.Entries, fresh)})
	}()
}

// openTimelineSource shows the message of one entry, replies in their thread, the
// way a search result does: the same window around the exact message, and the
// same note when the message is no longer there. On a narrow screen the details
// panel steps aside.
func openTimelineSource(cfg journeyclient.Config, id string) {
	model := chatBrowser.snapshot()
	entry, ok := timelineEntryByID(model, id)
	conversationID := model.SelectedID
	if !ok || conversationID == "" || entry.Sequence == 0 {
		return
	}
	if width := js.Global().Get("innerWidth"); width.Type() == js.TypeNumber {
		closeChatDetailsForPinJump(width.Int())
	}
	if entry.ThreadID != "" && entry.ThreadSequence > 0 {
		chatBrowser.mutate(func(m *chatui.Model) { m.FocusMessageID = "" })
		openChatConversationAt(cfg, conversationID, entry.ThreadSequence, entry.ThreadID)
		go chatsearchOpenThread(cfg, chatsearch.Target{ConversationID: conversationID, MessageID: entry.ID, ThreadID: entry.ThreadID, Sequence: entry.Sequence, ThreadSequence: entry.ThreadSequence})
		return
	}
	chatBrowser.mutate(func(m *chatui.Model) { m.FocusMessageID = entry.ID })
	openChatConversationAt(cfg, conversationID, entry.Sequence, entry.ID)
}
