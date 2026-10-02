//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

var integrate2Browser struct {
	sync.Mutex
	cancel       context.CancelFunc
	room         string
	epoch        uint64
	ctx          context.Context
	config       journeyclient.Config
	readerKey    string
	locationKey  string
	directoryKey string
}

func configureIntegrate2Chat(cfg journeyclient.Config) func() {
	ctx, cancel := context.WithCancel(context.Background())
	integrate2Browser.Lock()
	if integrate2Browser.cancel != nil {
		integrate2Browser.cancel()
	}
	integrate2Browser.cancel = cancel
	integrate2Browser.ctx = ctx
	integrate2Browser.config = cfg
	integrate2Browser.readerKey = ""
	integrate2Browser.locationKey = ""
	integrate2Browser.directoryKey = ""
	integrate2Browser.room = ""
	integrate2Browser.epoch++
	epoch := integrate2Browser.epoch
	integrate2Browser.Unlock()
	chatBrowser.mutate(func(m *chatui.Model) {
		m.ChatFeatures = &chatui.ChatFeatures{}
		m.ReaderPending = true
		m.ReaderPolicyRequired = true
		integrate2PrepareReaderSelections(m)
	})
	// CHATUX-012: a failed features read is retried (1 s, 2 s, 5 s, then every
	// 15 s) rather than leaving every feature off until the next reload.
	chatReadRetries.CancelPrefix(chatux012Integrate2Keys)
	readFeatures := func() bool {
		var features chatui.ChatFeatures
		call, stop := context.WithTimeout(ctx, 15*time.Second)
		defer stop()
		err := personaChatRequest(call, http.DefaultClient, personaChatHTTPConfig(cfg), http.MethodGet, "/api/chat/features/v1", "", &features)
		if ctx.Err() != nil {
			return true
		}
		if err != nil {
			ui.PostAsync(func() {
				integrate2Browser.Lock()
				valid := integrate2Browser.epoch == epoch
				integrate2Browser.Unlock()
				if !valid {
					return
				}
				chatBrowser.mutate(func(m *chatui.Model) {
					m.ReaderPending = false
					m.ReaderSelections = nil
					integrate2PrepareReaderSelections(m)
				})
				chatStreamRender.Schedule()
			})
			return chatux012HTTPFinal(err)
		}
		ui.PostAsync(func() {
			integrate2Browser.Lock()
			valid := integrate2Browser.epoch == epoch
			integrate2Browser.Unlock()
			if !valid {
				return
			}
			chatBrowser.mutate(func(m *chatui.Model) {
				m.ChatFeatures = &features
				m.ReaderPending = features.Renderings
				m.ReaderPolicyRequired = features.Renderings
				if !features.Renderings {
					m.ReaderSelections = nil
				}
				integrate2PrepareReaderSelections(m)
			})
			refreshChatRoute()
			integrate2SyncStatus(ctx, cfg)
		})
		return true
	}
	go func() {
		if !readFeatures() {
			chatux012Retry("integrate2|features", func() bool { return ctx.Err() == nil }, readFeatures)
		}
	}()
	changed := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		integrate2Browser.Lock()
		integrate2Browser.readerKey = ""
		integrate2Browser.Unlock()
		chatlang004ResetAudience()
		chatBrowser.mutate(func(m *chatui.Model) { m.ReaderSelections = nil; integrate2PrepareReaderSelections(m) })
		integrate2SyncProjection()
		chatStreamRender.Schedule()
		return nil
	})
	document := js.Global().Get("document")
	document.Call("addEventListener", "chat-reading-settings-changed", changed)
	return func() {
		cancel()
		chatReadRetries.CancelPrefix(chatux012Integrate2Keys)
		document.Call("removeEventListener", "chat-reading-settings-changed", changed)
		changed.Release()
	}
}

func integrate2SyncStatus(parent context.Context, cfg journeyclient.Config) {
	model := chatBrowser.snapshot()
	if model.ChatFeatures == nil || !model.ChatFeatures.Status {
		return
	}
	room := model.SelectedID
	channel := false
	for _, c := range model.Conversations {
		if c.ID == room {
			channel = c.Kind == chatui.PublicChannel || c.Kind == chatui.PrivateChannel
		}
	}
	if !channel {
		integrate2Browser.Lock()
		integrate2Browser.room = ""
		integrate2Browser.Unlock()
		return
	}
	integrate2Browser.Lock()
	if integrate2Browser.room == room {
		integrate2Browser.Unlock()
		return
	}
	integrate2Browser.room = room
	integrate2Browser.Unlock()
	cfg = chatBrowser.config(cfg)
	client := chatstateClient{HTTP: http.DefaultClient, BaseURL: personaChatHTTPConfig(cfg).TunnelURL, Bearer: cfg.Bearer, Tenant: cfg.Tenant}
	apply := func(v chatui.ChannelStatusView) {
		if parent.Err() != nil {
			return
		}
		ui.PostAsync(func() {
			chatBrowser.mutate(func(m *chatui.Model) {
				if m.CurrentTenantID != cfg.Tenant || m.CurrentUser != cfg.Subject {
					return
				}
				next := map[string]chatui.ChannelStatusView{}
				for k, old := range m.ChannelStatuses {
					next[k] = old
				}
				next[room] = v
				m.ChannelStatuses = next
				if m.SelectedID == room {
					m.ChangeChannelStatus = func(r chat.ChangeChannelStatusRequest) {
						go func() { changed, _ := client.Change(parent, r, v); applyIntegrate2Status(cfg, room, changed) }()
					}
					m.RetryChannelStatus = func() {
						go func() { changed, _ := client.Load(parent, room, v); applyIntegrate2Status(cfg, room, changed) }()
					}
				}
			})
			chatStreamRender.Schedule()
		})
	}
	// CHATUX-012: a poll that fails keeps the last status it read and tries again
	// on the next tick, and a first read that fails is retried with backoff until
	// it lands; neither ends the watch for good.
	poll := func(view chatui.ChannelStatusView) {
		// CHATBUG-075: once a minute, not every five seconds, and at once when
		// the event stream reports a change to the conversation.
		ticker := time.NewTicker(chatstateFallbackInterval)
		defer ticker.Stop()
		changed := chatstateChanged.Listen(room)
		for {
			select {
			case <-parent.Done():
				return
			case <-ticker.C:
			case <-changed:
			}
			if chatBrowser.selectedID() != room {
				return
			}
			next, err := client.Load(parent, room, view)
			if err != nil {
				continue
			}
			view = next
			apply(view)
		}
	}
	go func() {
		view, err := client.Load(parent, room, model.ChannelStatuses[room])
		apply(view)
		if err == nil {
			poll(view)
			return
		}
		if chatux012StatusFinal(err) {
			return
		}
		chatux012Retry(chatux012Integrate2Keys+"status|"+room, func() bool { return parent.Err() == nil && chatBrowser.selectedID() == room }, func() bool {
			next, err := client.Load(parent, room, view)
			apply(next)
			if err != nil {
				return chatux012StatusFinal(err)
			}
			go poll(next)
			return true
		})
	}()
}

func applyIntegrate2Status(cfg journeyclient.Config, room string, v chatui.ChannelStatusView) {
	ui.PostAsync(func() {
		chatBrowser.mutate(func(m *chatui.Model) {
			if m.SelectedID != room || m.CurrentTenantID != cfg.Tenant || m.CurrentUser != cfg.Subject {
				return
			}
			next := map[string]chatui.ChannelStatusView{}
			for k, old := range m.ChannelStatuses {
				next[k] = old
			}
			next[room] = v
			m.ChannelStatuses = next
		})
		chatStreamRender.Schedule()
	})
}

func integrate2SyncProjection() {
	integrate2Browser.Lock()
	ctx, cfg := integrate2Browser.ctx, integrate2Browser.config
	integrate2Browser.Unlock()
	if ctx == nil || ctx.Err() != nil {
		return
	}
	integrate2SyncStatus(ctx, cfg)
	integrate2SyncStatusDirectory(ctx, cfg)
	integrate2SyncLocations(ctx, cfg)
	chatlang004SyncAudience(ctx, cfg)
	chatvoiceSyncAccess(ctx, cfg)
	m := chatBrowser.snapshot()
	if m.ChatFeatures == nil || !m.ChatFeatures.Renderings || m.SelectedID == "" {
		return
	}
	key := integrate2ProjectionKey(m)
	integrate2Browser.Lock()
	if integrate2Browser.readerKey == key {
		integrate2Browser.Unlock()
		return
	}
	integrate2Browser.readerKey = key
	integrate2Browser.Unlock()
	cfg = chatBrowser.config(cfg)
	generation := chatBrowser.currentGeneration()
	// CHATUX-012: a read that does not get an answer is retried (not remembered
	// as asked); the loop ends when the projection moves on or the page is left.
	retryKey := chatux012Integrate2Keys + "reader|" + m.SelectedID
	chatReadRetries.Cancel(retryKey)
	selected := map[string]chatui.ReaderSelection{}
	read := func() bool {
		failed := false
		seen := map[string]bool{}
		messages := integrate2ReaderMessages(m)
		for start := 0; start < len(messages); start += 100 {
			host := cfg.Tenant
			for _, room := range m.Conversations {
				if room.ID == m.SelectedID && room.HostTenantID != "" {
					host = room.HostTenantID
				}
			}
			query := url.Values{"conversation": {m.SelectedID}, "tenant": {host}}
			for _, msg := range messages[start:min(start+100, len(messages))] {
				if msg.ID != "" && !seen[msg.ID] {
					query.Add("message", msg.ID)
					seen[msg.ID] = true
				}
			}
			if len(query["message"]) == 0 {
				continue
			}
			part := map[string]chatui.ReaderSelection{}
			call, cancel := context.WithTimeout(ctx, 20*time.Second)
			err := personaChatRequest(call, http.DefaultClient, personaChatHTTPConfig(cfg), http.MethodGet, "/api/chat/renderings/v1/reader?"+query.Encode(), "", &part)
			cancel()
			if ctx.Err() != nil {
				return true
			}
			if err != nil && !chatux012HTTPFinal(err) {
				failed = true
			}
			// A composed policy read may withhold original text; failures cannot grant that text.
			integrate2ApplyPolicyAnswer(selected, messages, query["message"], part, err)
		}
		ui.PostAsync(func() {
			committed := chatBrowser.commit(generation, func(current *chatui.Model) {
				if current.SelectedID != m.SelectedID || current.CurrentUser != m.CurrentUser || current.CurrentTenantID != m.CurrentTenantID {
					return
				}
				next := map[string]chatui.ReaderSelection{}
				for id, v := range current.ReaderSelections {
					next[id] = v
				}
				for id, v := range selected {
					next[id] = v
				}
				current.ReaderSelections = next
			})
			if committed {
				chatStreamRender.Schedule()
			}
		})
		if !failed && len(chatlang004Unsettled(selected)) > 0 {
			// CHATLANG-004: a translation still on its way replaces the text when it is ready.
			chatlang004Watch(ctx, cfg, m.SelectedID)
		}
		return !failed
	}
	go func() {
		if read() {
			return
		}
		chatux012Retry(retryKey, func() bool {
			integrate2Browser.Lock()
			current := integrate2Browser.readerKey == key
			integrate2Browser.Unlock()
			return ctx.Err() == nil && current
		}, read)
	}()
}

func integrate2SyncLocations(ctx context.Context, cfg journeyclient.Config) {
	m := chatBrowser.snapshot()
	if m.ChatFeatures == nil || !m.ChatFeatures.Locations || m.SelectedID == "" {
		return
	}
	key := integrate2ProjectionKey(m)
	integrate2Browser.Lock()
	if integrate2Browser.locationKey == key {
		integrate2Browser.Unlock()
		return
	}
	integrate2Browser.locationKey = key
	integrate2Browser.Unlock()
	cfg = chatBrowser.config(cfg)
	host := cfg.Tenant
	for _, c := range m.Conversations {
		if c.ID == m.SelectedID && c.HostTenantID != "" {
			host = c.HostTenantID
		}
	}
	generation := chatBrowser.currentGeneration()
	// CHATUX-012: a message whose read did not get an answer is read again, not
	// remembered as having no location; only those messages are asked for.
	retryKey := chatux012Integrate2Keys + "locations|" + m.SelectedID
	chatReadRetries.Cancel(retryKey)
	locations := map[string][]chatui.ChatmapEmbed{}
	answered := map[string]bool{}
	read := func() bool {
		failed := false
		for _, msg := range integrate2ReaderMessages(m) {
			if msg.ID == "" || answered[msg.ID] {
				continue
			}
			call, cancel := context.WithTimeout(ctx, 10*time.Second)
			raw, err := ChatmapRequestHTTP(call, http.DefaultClient, personaChatHTTPConfig(cfg), "message", ChatmapRequest{TenantID: host, ConversationID: m.SelectedID, PostID: msg.ID})
			cancel()
			if ctx.Err() != nil {
				return true
			}
			if err != nil {
				if chatux012LocationFinal(err) {
					answered[msg.ID] = true
				} else {
					failed = true
				}
				continue
			}
			answered[msg.ID] = true
			var shares []chat.LocationShare
			if json.Unmarshal(raw, &shares) != nil {
				continue
			}
			for _, share := range shares {
				if share.PostID == msg.ID {
					locations[msg.ID] = append(locations[msg.ID], chatui.ChatmapEmbed{Share: share, Sharer: msg.Author, MessageRevision: msg.Revision})
				}
			}
		}
		found := make(map[string][]chatui.ChatmapEmbed, len(locations))
		for id, embeds := range locations {
			found[id] = embeds
		}
		ui.PostAsync(func() {
			if ctx.Err() != nil {
				return
			}
			chatBrowser.commit(generation, func(current *chatui.Model) {
				if current.SelectedID == m.SelectedID && current.CurrentUser == m.CurrentUser && current.CurrentTenantID == m.CurrentTenantID {
					current.MessageLocations = found
				}
			})
			chatStreamRender.Schedule()
		})
		return !failed
	}
	go func() {
		if read() {
			return
		}
		chatux012Retry(retryKey, func() bool {
			integrate2Browser.Lock()
			current := integrate2Browser.locationKey == key
			integrate2Browser.Unlock()
			return ctx.Err() == nil && current
		}, read)
	}()
}

func integrate2SyncStatusDirectory(ctx context.Context, cfg journeyclient.Config) {
	m := chatBrowser.snapshot()
	if m.ChatFeatures == nil || !m.ChatFeatures.Status {
		return
	}
	key := m.CurrentTenantID + "\x00" + m.CurrentUser
	for _, room := range append(append([]chatui.Conversation(nil), m.Conversations...), m.SearchChannels...) {
		key += "\x00" + room.ID
	}
	integrate2Browser.Lock()
	if integrate2Browser.directoryKey == key {
		integrate2Browser.Unlock()
		return
	}
	integrate2Browser.directoryKey = key
	integrate2Browser.Unlock()
	cfg = chatBrowser.config(cfg)
	// CHATUX-012: the archived list and each room's status that did not get an
	// answer are asked for again; the key is not kept as "asked" for them.
	retryKey := chatux012Integrate2Keys + "directory"
	chatReadRetries.Cancel(retryKey)
	views := map[string]chatui.ChannelStatusView{}
	archivedRead := false
	var archivedRooms []chatui.Conversation
	read := func() bool {
		failed := false
		client := chatstateClient{HTTP: http.DefaultClient, BaseURL: personaChatHTTPConfig(cfg).TunnelURL, Bearer: cfg.Bearer, Tenant: cfg.Tenant}
		rooms := append(append([]chatui.Conversation(nil), m.Conversations...), m.SearchChannels...)
		if !archivedRead {
			var archived []chat.ChannelStatus
			call, cancel := context.WithTimeout(ctx, 15*time.Second)
			err := personaChatRequest(call, http.DefaultClient, personaChatHTTPConfig(cfg), http.MethodGet, "/api/chat/v1/channel-status/?archived=true", "", &archived)
			cancel()
			if ctx.Err() != nil {
				return true
			}
			switch {
			case err == nil:
				archivedRead = true
				for _, status := range archived {
					archivedRooms = append(archivedRooms, chatui.Conversation{ID: status.ConversationID, Kind: chatui.PublicChannel, Name: status.Name})
				}
			case chatux012HTTPFinal(err):
				archivedRead = true
			default:
				failed = true
			}
		}
		rooms = append(rooms, archivedRooms...)
		// CHATBUG-014: one request for every channel still to read. A channel
		// it did not answer for is read on its own below, as before.
		var batchViews map[string]chatui.ChannelStatusView
		var batchRefused map[string]error
		if wanted := chatperf2StatusRooms(rooms, views, m.SelectedID); len(wanted) > 1 {
			call, cancel := context.WithTimeout(ctx, 15*time.Second)
			batchViews, batchRefused = client.LoadMany(call, wanted, m.ChannelStatuses)
			cancel()
			if ctx.Err() != nil {
				return true
			}
		}
		for _, room := range rooms {
			if room.Kind != chatui.PublicChannel && room.Kind != chatui.PrivateChannel {
				continue
			}
			if _, ok := views[room.ID]; ok || room.ID == m.SelectedID {
				continue
			}
			if view, ok := batchViews[room.ID]; ok {
				views[room.ID] = view
				continue
			}
			if refusal, ok := batchRefused[room.ID]; ok {
				if !chatux012StatusFinal(refusal) {
					failed = true
				}
				continue
			}
			call, cancel := context.WithTimeout(ctx, 10*time.Second)
			view, err := client.Load(call, room.ID, m.ChannelStatuses[room.ID])
			cancel()
			if ctx.Err() != nil {
				return true
			}
			if err == nil {
				views[room.ID] = view
			} else if !chatux012StatusFinal(err) {
				failed = true
			}
		}
		found := make(map[string]chatui.ChannelStatusView, len(views))
		for id, v := range views {
			found[id] = v
		}
		ui.PostAsync(func() {
			if ctx.Err() != nil {
				return
			}
			chatBrowser.mutate(func(current *chatui.Model) {
				if current.CurrentUser != m.CurrentUser || current.CurrentTenantID != m.CurrentTenantID {
					return
				}
				next := map[string]chatui.ChannelStatusView{}
				for id, v := range current.ChannelStatuses {
					next[id] = v
				}
				for id, v := range found {
					next[id] = v
				}
				current.ChannelStatuses = next
			})
			chatStreamRender.Schedule()
		})
		return !failed
	}
	go func() {
		if read() {
			return
		}
		chatux012Retry(retryKey, func() bool {
			integrate2Browser.Lock()
			current := integrate2Browser.directoryKey == key
			integrate2Browser.Unlock()
			return ctx.Err() == nil && current
		}, read)
	}()
}
