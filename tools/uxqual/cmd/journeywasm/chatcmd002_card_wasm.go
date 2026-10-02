//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// chatcmd002Browser holds what the server said each card on the page looks
// like to this reader. It belongs to one person in one conversation (owner) and
// is emptied when either changes, so a view never crosses into another room.
var chatcmd002Browser struct {
	sync.Mutex
	owner    string
	views    map[string]chat.Chatcmd002View
	asked    map[string]uint64
	pending  map[string]bool
	keys     map[string]string
	failures int
}

func chatcmd002Owner(m chatui.Model) string {
	return m.CurrentTenantID + "\x00" + m.CurrentUser + "\x00" + m.SelectedID
}

// chatcmd002Adopt makes owner the store's owner, dropping another room's
// views. The caller holds the lock.
func chatcmd002Adopt(owner string) {
	if chatcmd002Browser.owner == owner && chatcmd002Browser.views != nil {
		return
	}
	chatcmd002Browser.owner = owner
	chatcmd002Browser.views = map[string]chat.Chatcmd002View{}
	chatcmd002Browser.asked = map[string]uint64{}
	chatcmd002Browser.pending = map[string]bool{}
	chatcmd002Browser.failures = 0
}

// chatcmd002Sync reads the view of every card on the page that has not been
// read at the revision it is shown at. The page calls it whenever a card
// appears or changes revision, which is also how another person's vote reaches
// this reader: the conversation's own event moves the message's revision.
func chatcmd002Sync() {
	m := chatBrowser.snapshot()
	if m.SelectedID == "" || m.CurrentUser == "" {
		return
	}
	owner := chatcmd002Owner(m)
	cards := chatcmd002Cards(m)
	chatcmd002Browser.Lock()
	chatcmd002Adopt(owner)
	ids := chatcmd002Stale(cards, chatcmd002Browser.asked)
	for _, id := range ids {
		chatcmd002Browser.asked[id] = cards[id]
	}
	chatcmd002Browser.Unlock()
	if len(ids) == 0 {
		return
	}
	go chatcmd002Read(chatBrowser.config(journeyclient.Config{}), owner, m.SelectedID, ids)
}

func chatcmd002Read(cfg journeyclient.Config, owner, room string, ids []string) {
	failed := false
	views := map[string]chat.Chatcmd002View{}
	for start := 0; start < len(ids); start += 100 {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		var reply chatcmd002Reply
		err := chatcmd002Request(ctx, http.DefaultClient, personaChatHTTPConfig(cfg), "read", chatcmd002Command{HostTenantID: channelTodoHost(room, cfg.Tenant), ConversationID: room, PostIDs: ids[start:min(start+100, len(ids))]}, &reply)
		cancel()
		if err != nil {
			failed = true
			break
		}
		for id, view := range reply.Views {
			views[id] = view
		}
	}
	chatcmd002Browser.Lock()
	if chatcmd002Browser.owner != owner {
		chatcmd002Browser.Unlock()
		return
	}
	for id, view := range views {
		if existing, ok := chatcmd002Browser.views[id]; !ok || view.Revision >= existing.Revision {
			// A notice about the reader's last press outlives the read it caused.
			view.Notice = existing.Notice
			chatcmd002Browser.views[id] = view
		}
	}
	retry := false
	if failed {
		// The cards keep what they showed. They are asked for again a little
		// later, a few times; after that the next change on the page asks.
		for _, id := range ids {
			if _, ok := views[id]; !ok {
				delete(chatcmd002Browser.asked, id)
			}
		}
		chatcmd002Browser.failures++
		retry = chatcmd002Browser.failures <= 4
	} else {
		chatcmd002Browser.failures = 0
	}
	wait := time.Duration(chatcmd002Browser.failures) * 2 * time.Second
	chatcmd002Browser.Unlock()
	if retry {
		time.AfterFunc(wait, chatcmd002Sync)
	}
	chatStreamRender.Schedule()
}

func chatcmd002Post(cfg journeyclient.Config, conversation, parent string, card chat.Chatcmd002Card, done func(error)) {
	active := chatBrowser.config(cfg)
	raw, _ := json.Marshal(card)
	// One key per card as written: pressing Post again after a failure is the
	// same post, never a second one.
	slot := conversation + "\x00" + parent + "\x00" + string(raw)
	chatcmd002Browser.Lock()
	if chatcmd002Browser.keys == nil {
		chatcmd002Browser.keys = map[string]string{}
	}
	key := chatcmd002Browser.keys[slot]
	if key == "" {
		key = fmt.Sprintf("wasm-card-%d", time.Now().UnixNano())
		chatcmd002Browser.keys[slot] = key
	}
	chatcmd002Browser.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var reply chatcmd002Reply
		err := chatcmd002Request(ctx, http.DefaultClient, personaChatHTTPConfig(active), "post", chatcmd002Command{HostTenantID: channelTodoHost(conversation, active.Tenant), ConversationID: conversation, ParentID: parent, IdempotencyKey: key, Card: &card}, &reply)
		if err == nil {
			chatcmd002Browser.Lock()
			delete(chatcmd002Browser.keys, slot)
			chatcmd002Browser.Unlock()
		}
		ui.PostAsync(func() { done(err) })
		if err != nil {
			return
		}
		// The live stream delivers the message; reading forward as well shows
		// it at once when the stream is between connections.
		if parent != "" {
			if client := chatBrowser.conversationClient(); client != nil {
				refreshChatThread(chatRPCContext(ctx, active), client, active, conversation, parent)
				refreshChatThreadRoute(conversation, parent)
			}
		}
		catchUpChatConversation(ctx, conversation, chatBrowser.currentGeneration(), active)
		chatStreamRender.Schedule()
	}()
}

func chatcmd002Mutate(cfg journeyclient.Config, post string, revision uint64, mutation chat.Chatcmd002Mutation) {
	m := chatBrowser.snapshot()
	room, owner := m.SelectedID, chatcmd002Owner(m)
	if room == "" || post == "" {
		return
	}
	chatcmd002Browser.Lock()
	chatcmd002Adopt(owner)
	if chatcmd002Browser.pending[post] {
		// One press at a time per card; the answer to the first is on its way.
		chatcmd002Browser.Unlock()
		return
	}
	chatcmd002Browser.pending[post] = true
	chatcmd002Browser.Unlock()
	active := chatBrowser.config(cfg)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var reply chatcmd002Reply
		err := chatcmd002Request(ctx, http.DefaultClient, personaChatHTTPConfig(active), "mutate", chatcmd002Command{HostTenantID: channelTodoHost(room, active.Tenant), ConversationID: room, PostID: post, ExpectedRevision: revision, Mutation: &mutation}, &reply)
		chatcmd002Browser.Lock()
		if chatcmd002Browser.owner != owner {
			chatcmd002Browser.Unlock()
			return
		}
		delete(chatcmd002Browser.pending, post)
		current := chatcmd002Browser.views[post]
		switch {
		case err != nil:
			current.Notice = chatcmd002Notice(err, mutation, current.Card.Poll != nil && current.Card.Poll.Anonymous)
			chatcmd002Browser.views[post] = current
			// The card may have moved on; read it again.
			delete(chatcmd002Browser.asked, post)
		case reply.View != nil:
			chatcmd002Browser.views[post] = *reply.View
			if reply.View.Revision > chatcmd002Browser.asked[post] {
				chatcmd002Browser.asked[post] = reply.View.Revision
			}
		default:
			current.Notice = ""
			chatcmd002Browser.views[post] = current
			delete(chatcmd002Browser.asked, post)
		}
		chatcmd002Browser.Unlock()
		if err != nil || reply.View == nil {
			chatcmd002Sync()
		}
		chatStreamRender.Schedule()
	}()
}

func chatcmd002Tidy(cfg journeyclient.Config, conversation string, draft chat.Chatcmd003Draft, done func(chat.Chatcmd003Draft, error)) {
	active := chatBrowser.config(cfg)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		tidied := draft
		err := chatcmd002Request(ctx, http.DefaultClient, personaChatHTTPConfig(active), "tidy", struct {
			Conversation string               `json:"conversation_id"`
			Draft        chat.Chatcmd003Draft `json:"draft"`
		}{conversation, draft}, &tidied)
		if err != nil {
			tidied = draft
		}
		ui.PostAsync(func() { done(tidied, err) })
	}()
}

// chatcmd002TimeZone is the zone the browser shows times in, so "Friday" in a
// command and the dates on a card mean the reader's Friday. It is asked for
// once: the page does not change zones while it is open.
var chatcmd002TimeZone = sync.OnceValue(func() string {
	intl := js.Global().Get("Intl")
	if !intl.Truthy() {
		return ""
	}
	zone := intl.Get("DateTimeFormat").Invoke().Call("resolvedOptions").Get("timeZone")
	if zone.Type() != js.TypeString {
		return ""
	}
	return zone.String()
})

// chatcmd002Project gives the page's model its cards: the callbacks that post
// and change one, the views read so far, and the reader's time zone.
func chatcmd002Project(model chatui.Model, cfg journeyclient.Config) chatui.Model {
	owner := chatcmd002Owner(model)
	chatcmd002Browser.Lock()
	if chatcmd002Browser.owner == owner && len(chatcmd002Browser.views) > 0 {
		views := make(map[string]chatui.Chatcmd002View, len(chatcmd002Browser.views))
		for id, view := range chatcmd002Browser.views {
			views[id] = view
		}
		model.Chatcmd002Views = views
	}
	chatcmd002Browser.Unlock()
	model.Chatcmd002TimeZone = chatcmd002TimeZone()
	model.Messages = chatcmd002PlainMessages(model.Messages)
	model.ThreadMessages = chatcmd002PlainMessages(model.ThreadMessages)
	if model.ThreadParent != nil && model.ThreadParent.Edited && chatcmd002IsCard(model.ThreadParent.Body) {
		parent := *model.ThreadParent
		parent.Edited = false
		model.ThreadParent = &parent
	}
	model.Chatcmd002.Post = func(conversation, parent string, card chat.Chatcmd002Card, done func(error)) {
		chatcmd002Post(cfg, conversation, parent, card, done)
	}
	model.Chatcmd002.Mutate = func(post string, revision uint64, mutation chat.Chatcmd002Mutation) {
		chatcmd002Mutate(cfg, post, revision, mutation)
	}
	// The model pass is offered only where an administrator has writing styles
	// on; the preview is already tidied without it.
	if model.ChatFeatures != nil && model.ChatFeatures.WritingStyles {
		model.Chatcmd002.Tidy = func(conversation string, draft chat.Chatcmd003Draft, done func(chat.Chatcmd003Draft, error)) {
			chatcmd002Tidy(cfg, conversation, draft, done)
		}
	}
	// A search result shows a card as its words, not its data.
	if len(model.SearchMessages) > 0 {
		hits := append([]chatui.SearchMessage(nil), model.SearchMessages...)
		for i := range hits {
			hits[i].Message.Body = chatui.Chatcmd002PlainBody(hits[i].Message.Body)
		}
		model.SearchMessages = hits
	}
	// CHATCMD-004: a task of a posted list can be copied to the channel's own
	// list, where that list is loaded and takes changes.
	if add := model.Callbacks.AddChannelTodo; add != nil && model.ChannelTodo.Revision != 0 && !chatui.ChannelWidgetsReadOnly(model) {
		views := model.Chatcmd002Views
		model.Chatcmd002.MoveToChannel = func(post, item string) {
			if view, ok := views[post]; ok && view.Card.Todo != nil {
				for _, task := range view.Card.Todo.Items {
					if task.ID == item {
						add(task.Text, "")
					}
				}
			}
		}
	}
	// CHATBUG-074: the channel's standing poll can be ended.
	model.Chatcmd002.CloseChannelPoll = func() { go mutateChannelPoll(cfg, "CLOSE", "", nil, "") }
	// A card's body is its data, not text to edit in the message box: saving
	// it from there could leave a message that no longer reads as a card.
	if begin := model.Callbacks.BeginEdit; begin != nil {
		timeline, thread := model.Messages, model.ThreadMessages
		model.Callbacks.BeginEdit = func(id string) {
			for _, messages := range [][]chatui.Message{timeline, thread} {
				for _, message := range messages {
					if message.ID == id && chatcmd002IsCard(message.Body) {
						return
					}
				}
			}
			begin(id)
		}
	}
	// CHATBUG-082: an archived or locked channel shows its widgets and offers no
	// change to them.
	return chatui.WithReadOnlyChannelWidgets(model)
}
