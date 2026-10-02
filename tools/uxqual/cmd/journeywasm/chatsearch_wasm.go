//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

var chatsearchBrowser struct {
	sync.Mutex
	bound         bool
	click, submit js.Func
	view          chatui.ChatSearchView
	flow          chatsearchFlow
	cancel        context.CancelFunc
	config        journeyclient.Config
}

// chatsearchClear ends the search: any request on its way is cancelled and its
// answer dropped, and the model returns to the conversation that was open.
func chatsearchClear() {
	chatsearchBrowser.Lock()
	chatsearchBrowser.flow.clear()
	chatsearchBrowser.view = chatui.ChatSearchView{}
	if chatsearchBrowser.cancel != nil {
		chatsearchBrowser.cancel()
		chatsearchBrowser.cancel = nil
	}
	chatsearchBrowser.Unlock()
	chatBrowser.mutate(chatsearchModelClear)
}

// chatsearchBegin is the one additive search callback hook. The original
// clear-search behaviour remains in the sibling callback for an empty query.
// Typed words wait a moment for the next letter; a button asks at once.
func chatsearchBegin(cfg journeyclient.Config, query string) bool {
	return chatsearchStart(cfg, query, chatsearchDelay, false)
}

func chatsearchStart(cfg journeyclient.Config, query string, delayMillis int, retry bool) bool {
	if query == "" {
		return false
	}
	chatsearchBrowser.Lock()
	bind := !chatsearchBrowser.bound
	chatsearchBrowser.bound = true
	chatsearchBrowser.config = chatBrowser.config(cfg)
	chatsearchBrowser.Unlock()
	if bind {
		chatsearchBrowser.click = js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				chatsearchClick(args[0])
			}
			return nil
		})
		chatsearchBrowser.submit = js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				chatsearchSubmit(args[0])
			}
			return nil
		})
		d := js.Global().Get("document")
		d.Call("addEventListener", "click", chatsearchBrowser.click)
		d.Call("addEventListener", "submit", chatsearchBrowser.submit)
	}
	chatsearchLoad(query, "", delayMillis, retry)
	return true
}

// chatsearchLoad asks for query (and the page after cursor) once. The words go
// onto the model at once, so the results area opens and says it is searching;
// the same search already running or already answered is not asked again.
func chatsearchLoad(query, cursor string, delayMillis int, retry bool) {
	model := chatBrowser.snapshot()
	resolved, resolveErr := chatui.ResolveChatSearchQuery(model, query)
	key := resolved + "\x00" + cursor
	chatsearchBrowser.Lock()
	cfg := chatsearchBrowser.config
	generation, send := chatsearchBrowser.flow.begin(query, key, strings.TrimSpace(model.Search) == query, retry)
	if !send {
		view := chatsearchBrowser.view
		chatsearchBrowser.Unlock()
		chatBrowser.mutate(func(m *chatui.Model) { chatsearchModelBegin(m, view) })
		refreshChatRoute()
		return
	}
	previous := chatsearchBrowser.view
	// The fallback conversation is a plain snapshot; it must not carry the
	// results it replaces or the snapshots would nest one inside the next.
	conversation := model
	conversation.ChatSearch, conversation.SearchOpened = nil, false
	view := chatui.ChatSearchView{Query: query, Loading: true}
	chatsearchBrowser.view = view
	if chatsearchBrowser.cancel != nil {
		chatsearchBrowser.cancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	chatsearchBrowser.cancel = cancel
	chatsearchBrowser.Unlock()
	chatBrowser.mutate(func(m *chatui.Model) { chatsearchModelBegin(m, view) })
	refreshChatRoute()
	go func() {
		defer cancel()
		if delayMillis > 0 {
			select {
			case <-time.After(time.Duration(delayMillis) * time.Millisecond):
			case <-ctx.Done():
				return
			}
			chatsearchBrowser.Lock()
			current := generation == chatsearchBrowser.flow.Generation
			chatsearchBrowser.Unlock()
			if !current {
				return
			}
		}
		var response chatsearch.Response
		e := resolveErr
		if e == nil {
			e = chatsearchRequest(ctx, http.DefaultClient, cfg, "/api/chat/search", http.MethodPost, chatsearch.Request{Query: resolved, DisplayQuery: query, Mode: "keyword", Cursor: cursor, Limit: 20}, &response)
		}
		recent := []string{}
		if e == nil {
			_ = chatsearchRequest(ctx, http.DefaultClient, cfg, "/api/chat/search/recent", http.MethodGet, nil, &recent)
		}
		// Only a newer search or a cleared box overtakes this one. A request that
		// timed out is this search's own failure and is shown as one.
		chatsearchBrowser.Lock()
		current := chatsearchBrowser.flow.current(generation)
		chatsearchBrowser.Unlock()
		if !current {
			return
		}
		code := ""
		if e != nil {
			code = chatsearchErrorCode(e)
		}
		outcome := chatPolishSearchOutcome(previous, conversation, query, response, recent, code)
		ui.PostAsync(func() {
			// Success or failure, the outcome is drawn in the results area:
			// the matches, or the one quiet line. A box cleared meanwhile
			// stays cleared.
			shown := false
			chatsearchBrowser.Lock()
			chatBrowser.mutate(func(m *chatui.Model) {
				shown = chatsearchComplete(&chatsearchBrowser.flow, &chatsearchBrowser.view, m, generation, outcome)
			})
			chatsearchBrowser.Unlock()
			if shown {
				refreshChatRoute()
			}
		})
	}()
}
func chatsearchClick(event js.Value) {
	button := event.Get("target").Call("closest", "[data-chatsearch-action]")
	if !button.Truthy() {
		return
	}
	event.Call("preventDefault")
	chatsearchBrowser.Lock()
	view, cfg := chatsearchBrowser.view, chatsearchBrowser.config
	chatsearchBrowser.Unlock()
	action := button.Call("getAttribute", "data-chatsearch-action").String()
	extra := button.Call("getAttribute", "data-extra").String()
	switch action {
	case "retry":
		chatsearchLoad(view.Query, "", 0, true)
	case "more":
		chatsearchLoad(view.Query, extra, 0, false)
	case "recent":
		chatsearchStart(cfg, extra, 0, false)
	case "widen":
		if extra == "" {
			extra = "*"
		}
		chatsearchStart(cfg, extra, 0, false)
	case "remove":
		query, e := chatsearch.RemoveChip(view.Query, extra)
		if e == nil {
			if query == "" {
				query = "*"
			}
			chatsearchStart(cfg, query, 0, false)
		}
	case "clear-recent":
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			e := chatsearchRequest(ctx, http.DefaultClient, chatBrowser.config(cfg), "/api/chat/search/recent", http.MethodDelete, nil, nil)
			chatsearchBrowser.Lock()
			if e == nil {
				chatsearchBrowser.view.Recent = nil
			} else {
				chatsearchBrowser.view.Error = chatsearchErrorCode(e)
			}
			view := chatsearchBrowser.view
			chatsearchBrowser.Unlock()
			chatBrowser.mutate(func(m *chatui.Model) { chatsearchModelShow(m, view) })
			refreshChatRoute()
		}()
	case "open":
		target, e := chatsearchDecodeTarget(button.Call("getAttribute", "data-target").String())
		if e != nil {
			return
		}
		// Keep the private query on this controller; the existing navigation
		// loads a bounded two-sided message window around the exact sequence.
		model := chatBrowser.snapshot()
		kind := chatsearch.Kind(button.Call("getAttribute", "data-kind").String())
		leavesChat := kind == chatsearch.FilterDefinition && model.FilterSettings != nil
		if kind != chatsearch.Person && !leavesChat {
			// The address names the conversation the result is in, not the
			// one the search began from.
			chatHistory.pushState(chatsearchOpenNavigation(model, target))
		}
		if leavesChat {
			model.FilterSettings()
		} else if kind == chatsearch.Person && model.Callbacks.OpenPerson != nil {
			model.Callbacks.OpenPerson(target.ItemID)
		} else if target.ThreadID != "" && target.Sequence > 0 {
			chatBrowser.mutate(func(m *chatui.Model) { m.FocusMessageID = "" })
			openChatConversationAt(cfg, target.ConversationID, target.ThreadSequence, target.ThreadID)
			go chatsearchOpenThread(cfg, target)
		} else if target.ConversationID != "" && target.MessageID != "" && target.Sequence > 0 {
			chatBrowser.mutate(func(m *chatui.Model) { m.FocusMessageID = target.MessageID })
			openChatConversationAt(cfg, target.ConversationID, target.Sequence, target.MessageID)
		} else if target.ConversationID != "" {
			openChatConversation(cfg, target.ConversationID)
		}
		if !leavesChat {
			// The conversation shows; the query stays in the box and the
			// results wait behind the "Return to results" bar.
			chatsearchBrowser.Lock()
			opened := chatsearchBrowser.flow.open()
			chatsearchBrowser.Unlock()
			if opened {
				chatBrowser.mutate(func(m *chatui.Model) { chatsearchModelOpened(m, view) })
				refreshChatRoute()
			}
		}
		if kind == chatsearch.Todo || kind == chatsearch.Poll {
			go chatsearchOpenList(target, kind)
		}
	case "back":
		// The results are still held: showing them again asks for nothing.
		chatsearchBrowser.Lock()
		returned := chatsearchBrowser.flow.back()
		chatsearchBrowser.Unlock()
		if returned {
			chatBrowser.mutate(chatsearchModelBack)
			refreshChatRoute()
		}
	}
}

func chatsearchOpenThread(cfg journeyclient.Config, target chatsearch.Target) {
	active := chatBrowser.config(cfg)
	olderRequest, newerRequest, err := chatsearchThreadRequests(active.Tenant, target)
	client := chatBrowser.conversationClient()
	if err != nil || client == nil {
		return
	}
	generation := chatBrowser.currentGeneration()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	older, err := client.ListPosts(chatRPCContext(ctx, active), olderRequest)
	if err != nil {
		chatsearchThreadFailure(target.ConversationID, generation)
		return
	}
	newer, err := client.ListPosts(chatRPCContext(ctx, active), newerRequest)
	if err != nil {
		chatsearchThreadFailure(target.ConversationID, generation)
		return
	}
	posts := mergeChatSearchAnchorPosts(older.GetPosts(), newer.GetPosts())
	replies := chatThreadMessages(posts, target.ThreadID, active.Locale, chatDirectorySnapshot(), time.Now())
	found := false
	for _, reply := range replies {
		found = found || reply.ID == target.MessageID
	}
	if !found {
		chatsearchThreadFailure(target.ConversationID, generation)
		return
	}
	// The room read owns the timeline and can replace thread state. Wait for
	// its anchor to commit before installing this reply's two-sided window.
	for !chatsearchThreadReady(chatBrowser.snapshot(), target, chatBrowser.alreadyOpening(target.ConversationID)) {
		if !chatBrowser.generationActive(generation) || chatBrowser.selectedID() != target.ConversationID {
			return
		}
		select {
		case <-ctx.Done():
			chatsearchThreadFailure(target.ConversationID, generation)
			return
		case <-time.After(50 * time.Millisecond):
		}
	}
	ui.PostAsync(func() {
		committed := chatBrowser.commit(generation, func(m *chatui.Model) {
			if m.SelectedID != target.ConversationID {
				return
			}
			m.ShowThread, m.ThreadParentID = true, target.ThreadID
			m.ThreadLoading = false
			m.ThreadParent = nil
			m.ThreadMessages = replies
			m.ThreadHasOlder, m.ThreadHasNewer = older.GetNextCursor() != "", newer.GetNextCursor() != ""
			m.FocusMessageID = target.MessageID
			for _, parent := range m.Messages {
				if parent.ID == target.ThreadID {
					copy := parent
					m.ThreadParent = &copy
				}
			}
			if len(posts) > 0 {
				chatBrowser.threadBeforeSequence = posts[0].GetSequence()
				chatBrowser.threadAfterSequence = posts[len(posts)-1].GetSequence()
			}
		})
		if committed {
			refreshChatRoute()
			ui.PostAsync(func() { chatui.FocusSearchMessage(target.MessageID) })
		}
	})
}

func chatsearchThreadFailure(room string, generation uint64) {
	if !chatBrowser.generationActive(generation) || chatBrowser.selectedID() != room {
		return
	}
	chatsearchBrowser.Lock()
	chatsearchBrowser.view.Error = "error"
	view := chatsearchBrowser.view
	chatsearchBrowser.flow.back()
	chatsearchBrowser.Unlock()
	// The thread could not be opened: show the results again with the one
	// failure line instead of leaving the person on a half-opened room.
	chatBrowser.mutate(func(m *chatui.Model) {
		m.Search, m.ChatSearch, m.SearchOpened = view.Query, &view, false
	})
	refreshChatRoute()
}

func chatsearchOpenList(target chatsearch.Target, kind chatsearch.Kind) {
	deadline := time.Now().Add(15 * time.Second)
	opened := false
	for time.Now().Before(deadline) {
		model := chatBrowser.snapshot()
		if model.SelectedID != target.ConversationID {
			return
		}
		if chatBrowser.alreadyOpening(target.ConversationID) || model.State == chatui.StateLoading || model.ChannelTodoLoading || model.ChannelPollLoading {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		doc := js.Global().Get("document")
		action := "tray-todo"
		if kind == chatsearch.Poll {
			action = "tray-poll"
		}
		button := doc.Call("querySelector", "[data-action='"+action+"']")
		if !opened && button.Truthy() {
			button.Call("click")
			opened = true
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if kind == chatsearch.Todo {
			buttons := doc.Call("querySelectorAll", "[data-action='todo-toggle']")
			for i := 0; i < buttons.Get("length").Int(); i++ {
				item := buttons.Index(i)
				if item.Call("getAttribute", "data-id").String() == target.ItemID {
					item.Call("focus")
					item.Call("scrollIntoView", map[string]any{"block": "nearest", "behavior": "auto"})
					return
				}
			}
		} else {
			question := doc.Call("querySelector", ".channel-poll-question")
			if question.Truthy() {
				question.Call("setAttribute", "tabindex", "-1")
				question.Call("focus")
				question.Call("scrollIntoView", map[string]any{"block": "nearest", "behavior": "auto"})
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
}
func chatsearchSubmit(event js.Value) {
	form := event.Get("target")
	if !form.Call("hasAttribute", "data-chatsearch-form").Bool() {
		return
	}
	event.Call("preventDefault")
	values := map[string]string{}
	checks := map[string]bool{}
	for _, name := range []string{"channel", "person", "kind", "before", "after", "on"} {
		values[name] = form.Call("querySelector", "[name='"+name+"']").Get("value").String()
	}
	for _, name := range []string{"file", "link", "reactions", "threads", "mentions", "agent", "mine", "voice"} {
		checks[name] = form.Call("querySelector", "[name='"+name+"']").Get("checked").Bool()
	}
	chatsearchBrowser.Lock()
	v, cfg := chatsearchBrowser.view, chatsearchBrowser.config
	chatsearchBrowser.Unlock()
	query, e := chatsearchControlQuery(v.Query, values, checks)
	if e != nil {
		chatsearchBrowser.Lock()
		chatsearchBrowser.view.Error = "invalid"
		view := chatsearchBrowser.view
		chatsearchBrowser.Unlock()
		chatBrowser.mutate(func(m *chatui.Model) { chatsearchModelShow(m, view) })
		refreshChatRoute()
		return
	}
	chatsearchBegin(cfg, query)
}
