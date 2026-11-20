//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Shared files (#3917), browser half. The list is read through the Chat search
// endpoint (kind:file in this conversation), a page at a time; this file keeps
// the paging position, drops answers for a conversation or search that has been
// left, and opens a file's message through the paths a search result uses.

var sharedFilesRead struct {
	sync.Mutex
	run          uint64
	conversation string
	name         string
	cursor       string
}

func sharedFilesSet(conversationID string, change func(*chatui.SharedFilesView)) {
	chatBrowser.mutate(func(m *chatui.Model) {
		if m.SelectedID != conversationID {
			return
		}
		view := m.SharedFiles
		if view.ConversationID != conversationID {
			view = chatui.SharedFilesView{ConversationID: conversationID}
		}
		change(&view)
		m.SharedFiles = view
	})
	refreshChatRoute()
}

// loadSharedFiles reads the first page of the open conversation's files,
// narrowed by a name (empty for all).
func loadSharedFiles(cfg journeyclient.Config, name string) {
	conversationID := chatBrowser.selectedID()
	if conversationID == "" {
		return
	}
	query, err := sharedFilesQuery(conversationID, name)
	sharedFilesRead.Lock()
	sharedFilesRead.run++
	run := sharedFilesRead.run
	sharedFilesRead.conversation, sharedFilesRead.name, sharedFilesRead.cursor = conversationID, name, ""
	sharedFilesRead.Unlock()
	if err != nil {
		sharedFilesSet(conversationID, func(v *chatui.SharedFilesView) {
			*v = chatui.SharedFilesView{ConversationID: conversationID, Status: chatui.SharedFilesError, Name: name}
		})
		return
	}
	sharedFilesSet(conversationID, func(v *chatui.SharedFilesView) {
		*v = chatui.SharedFilesView{ConversationID: conversationID, Status: chatui.SharedFilesLoading, Name: name}
	})
	go readSharedFilesPage(cfg, conversationID, name, query, "", run, false)
}

// moreSharedFiles reads the next older page of the list on screen.
func moreSharedFiles(cfg journeyclient.Config) {
	conversationID := chatBrowser.selectedID()
	sharedFilesRead.Lock()
	cursor, name, run := sharedFilesRead.cursor, sharedFilesRead.name, sharedFilesRead.run
	same := sharedFilesRead.conversation == conversationID
	sharedFilesRead.Unlock()
	if conversationID == "" || !same || cursor == "" {
		return
	}
	query, err := sharedFilesQuery(conversationID, name)
	if err != nil {
		return
	}
	sharedFilesSet(conversationID, func(v *chatui.SharedFilesView) { v.LoadingMore = true })
	go readSharedFilesPage(cfg, conversationID, name, query, cursor, run, true)
}

func readSharedFilesPage(cfg journeyclient.Config, conversationID, name, query, cursor string, run uint64, appending bool) {
	active := chatBrowser.config(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var response chatsearch.Response
	// Transient: the list is not one of the person's searches and must not be
	// remembered among their recent ones.
	err := chatsearchRequest(ctx, http.DefaultClient, active, "/api/chat/search", http.MethodPost,
		chatsearch.Request{Query: query, DisplayQuery: query, Mode: "keyword", Cursor: cursor, Limit: sharedFilesPageSize, Transient: true}, &response)
	sharedFilesRead.Lock()
	current := run == sharedFilesRead.run && conversationID == sharedFilesRead.conversation
	sharedFilesRead.Unlock()
	if !current || chatBrowser.selectedID() != conversationID {
		return
	}
	files, more, unavailable := sharedFilesFromResponse(response, conversationID)
	if err == nil && !unavailable {
		sharedFilesRead.Lock()
		if run == sharedFilesRead.run {
			sharedFilesRead.cursor = response.NextCursor
		}
		sharedFilesRead.Unlock()
	}
	switch {
	case err != nil && appending:
		// The files already read stay; only the older page failed.
		sharedFilesSet(conversationID, func(v *chatui.SharedFilesView) { v.LoadingMore = false })
		noteChatAction(chatui.SharedFilesText(active.Locale, "error"))
	case err != nil:
		sharedFilesSet(conversationID, func(v *chatui.SharedFilesView) {
			*v = chatui.SharedFilesView{ConversationID: conversationID, Status: chatui.SharedFilesError, Name: name}
		})
	case unavailable && appending:
		sharedFilesSet(conversationID, func(v *chatui.SharedFilesView) { v.LoadingMore = false })
		noteChatAction(chatui.SharedFilesText(active.Locale, "unavailable"))
	case unavailable:
		sharedFilesSet(conversationID, func(v *chatui.SharedFilesView) {
			*v = chatui.SharedFilesView{ConversationID: conversationID, Status: chatui.SharedFilesUnavailable, Name: name}
		})
	default:
		sharedFilesSet(conversationID, func(v *chatui.SharedFilesView) {
			if appending {
				v.Files = sharedFilesMerge(v.Files, files)
			} else {
				v.Files = files
			}
			v.Status, v.More, v.LoadingMore, v.Name = chatui.SharedFilesReady, more, false, name
		})
	}
}

// openSharedFileSource shows the message a shared file was sent in, replies in
// their thread, the way a search result does: the same window around the exact
// message, and the same note when the message is no longer there. On a narrow
// screen the details panel steps aside, and coming back to it lands on the row.
func openSharedFileSource(cfg journeyclient.Config, id string) {
	model := chatBrowser.snapshot()
	file, ok := sharedFileByID(model, id)
	conversationID := model.SelectedID
	if !ok || conversationID == "" || file.Sequence == 0 || file.PostID == "" {
		return
	}
	sharedFilesSet(conversationID, func(v *chatui.SharedFilesView) { v.ReturnID = id })
	if width := js.Global().Get("innerWidth"); width.Type() == js.TypeNumber {
		closeChatDetailsForPinJump(width.Int())
	}
	if file.ThreadID != "" && file.ThreadSequence > 0 {
		chatBrowser.mutate(func(m *chatui.Model) { m.FocusMessageID = "" })
		openChatConversationAt(cfg, conversationID, file.ThreadSequence, file.ThreadID)
		go chatsearchOpenThread(cfg, chatsearch.Target{ConversationID: conversationID, MessageID: file.PostID, ThreadID: file.ThreadID, Sequence: file.Sequence, ThreadSequence: file.ThreadSequence})
		return
	}
	chatBrowser.mutate(func(m *chatui.Model) { m.FocusMessageID = file.PostID })
	openChatConversationAt(cfg, conversationID, file.Sequence, file.PostID)
}

// restoreSharedFilesPlace puts focus back on the row whose message was opened
// when the details panel is opened again.
func restoreSharedFilesPlace() {
	model := chatBrowser.snapshot()
	if model.SharedFiles.ConversationID != model.SelectedID || model.SharedFiles.ReturnID == "" {
		return
	}
	id := model.SharedFiles.ReturnID
	ui.PostAsync(func() { chatui.FocusSharedFile(id) })
}
