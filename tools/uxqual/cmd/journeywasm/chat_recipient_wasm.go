//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"syscall/js"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type recipientChatRef struct {
	HostTenantID   string `json:"hostTenantId"`
	ConversationID string `json:"conversationId"`
}
type recipientSection struct {
	ID, Name  string
	Collapsed bool
	Chats     []recipientChatRef `json:"chats"`
}
type recipientLayout struct {
	Sections             []recipientSection          `json:"sections"`
	Panes                chatui.PaneSizes            `json:"panes"`
	PanesByDevice        map[string]chatui.PaneSizes `json:"panesByDevice,omitempty"`
	Starred              []string                    `json:"starred,omitempty"`
	Filters              map[string]string           `json:"filters,omitempty"`
	DismissedJoinPrompts []string                    `json:"dismissedJoinPrompts,omitempty"`
	// Drafts is the reader's unsent composer text, keyed by conversation.
	// Chat has no drafts store of its own: NotificationPreferences has no
	// such field and the product preference schema is closed, so the sidebar
	// blob -- the only per-principal free-form store chat publishes -- is
	// where a draft survives a reload. The server validates only the
	// sections shape and persists the raw document, so this key round-trips.
	Drafts map[string]string `json:"drafts,omitempty"`
}

// A draft-only retry starts with the other tab's layout. Keep any room this
// tab newly opened so the draft still names an admitted sidebar conversation.
func keepDraftRooms(server *recipientLayout, local recipientLayout, drafts map[string]string) bool {
	seen := map[string]string{}
	for _, section := range server.Sections {
		for _, room := range section.Chats {
			if host, ok := seen[room.ConversationID]; ok && host != room.HostTenantID {
				seen[room.ConversationID] = ""
			} else if !ok {
				seen[room.ConversationID] = room.HostTenantID
			}
		}
	}
	for _, section := range local.Sections {
		for _, room := range section.Chats {
			if drafts[room.ConversationID] == "" {
				continue
			}
			if host, ok := seen[room.ConversationID]; ok {
				if host != room.HostTenantID {
					return false
				}
				continue
			}
			index := -1
			for i := range server.Sections {
				if server.Sections[i].ID == section.ID {
					index = i
					break
				}
			}
			if index < 0 {
				server.Sections = append(server.Sections, recipientSection{ID: section.ID, Name: section.Name, Collapsed: section.Collapsed})
				index = len(server.Sections) - 1
			}
			server.Sections[index].Chats = append(server.Sections[index].Chats, room)
			seen[room.ConversationID] = room.HostTenantID
		}
	}
	for id := range drafts {
		if seen[id] == "" {
			return false
		}
	}
	return true
}

var chatRecipientBrowser struct {
	sync.Mutex
	sidebarWrite, quietWrite       sync.Mutex
	client                         chatv1.ChatExtensionsServiceClient
	generation                     uint64
	hosts                          map[string]string
	sidebarRevision, quietRevision uint64
	loading                        bool
	loadedAt                       time.Time
	layout                         recipientLayout
	quiet                          *chatv1.QuietHours
	counts                         map[string]*chatv1.ChatCounts
	follows                        map[string]*chatv1.ThreadFollow
	readMarked                     map[string]uint64
	sidebarEdit                    uint64
	sidebarSaved                   uint64
}

func configureChatRecipientBrowser(conn grpc.ClientConnInterface, cfg journeyclient.Config) {
	chatRecipientBrowser.Lock()
	chatRecipientBrowser.client = chatv1.NewChatExtensionsServiceClient(conn)
	chatRecipientBrowser.generation++
	chatRecipientBrowser.hosts = make(map[string]string)
	chatRecipientBrowser.sidebarRevision = 1
	chatRecipientBrowser.quietRevision = 1
	chatRecipientBrowser.loadedAt = time.Time{}
	chatRecipientBrowser.loading = false
	chatRecipientBrowser.layout = recipientLayout{}
	chatRecipientBrowser.sidebarEdit = 0
	chatRecipientBrowser.sidebarSaved = 0
	chatRecipientBrowser.quiet = nil
	chatRecipientBrowser.counts = nil
	chatRecipientBrowser.follows = make(map[string]*chatv1.ThreadFollow)
	chatRecipientBrowser.readMarked = make(map[string]uint64)
	chatRecipientBrowser.Unlock()
}

// startChatRecipientProjection leaves the base chat route responsive while
// personal state is loaded. A short cache prevents a render-refresh loop.
func startChatRecipientProjection(cfg journeyclient.Config, conversations []*chatv1.Conversation) {
	chatRecipientBrowser.Lock()
	if chatRecipientBrowser.client == nil || chatRecipientBrowser.loading || time.Since(chatRecipientBrowser.loadedAt) < 15*time.Second {
		chatRecipientBrowser.Unlock()
		return
	}
	chatRecipientBrowser.loading = true
	client := chatRecipientBrowser.client
	generation := chatRecipientBrowser.generation
	edit := chatRecipientBrowser.sidebarEdit
	wasSaved := edit == chatRecipientBrowser.sidebarSaved
	hosts := make(map[string]string, len(conversations))
	for _, c := range conversations {
		if c != nil {
			hosts[c.GetId()] = c.GetTenantId()
		}
	}
	chatRecipientBrowser.hosts = hosts
	chatRecipientBrowser.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		ctx = chatRPCContext(ctx, cfg)
		side, sideErr := client.GetSidebar(ctx, &chatv1.GetSidebarRequest{})
		quiet, quietErr := client.GetQuietHours(ctx, &chatv1.GetQuietHoursRequest{})
		layout := recipientLayout{}
		if sideErr == nil && side.GetSidebar() != nil {
			_ = json.Unmarshal([]byte(side.GetSidebar().GetLayoutJson()), &layout)
		}
		counts := make(map[string]*chatv1.ChatCounts, len(hosts))
		// Cap concurrent RPCs so a large rail cannot monopolize the browser.
		sem := make(chan struct{}, 8)
		var wg sync.WaitGroup
		var mu sync.Mutex
		for id, host := range hosts {
			id, host := id, host
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				result, err := client.GetCounts(ctx, &chatv1.GetCountsRequest{TenantId: host, ConversationId: id})
				if err == nil && result.GetCounts() != nil {
					mu.Lock()
					counts[id] = result.GetCounts()
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		chatRecipientBrowser.Lock()
		if generation != chatRecipientBrowser.generation {
			chatRecipientBrowser.Unlock()
			return
		}
		chatRecipientBrowser.loading = false
		chatRecipientBrowser.loadedAt = time.Now()
		freshSidebar := wasSaved && edit == chatRecipientBrowser.sidebarEdit && edit == chatRecipientBrowser.sidebarSaved
		if sideErr == nil && side.GetSidebar() != nil && freshSidebar {
			chatRecipientBrowser.sidebarRevision = side.GetSidebar().GetRevision()
			chatRecipientBrowser.layout = layout
		}
		if quietErr == nil && quiet.GetQuietHours() != nil {
			chatRecipientBrowser.quietRevision = quiet.GetQuietHours().GetRevision()
			chatRecipientBrowser.quiet = quiet.GetQuietHours()
		}
		chatRecipientBrowser.counts = counts
		chatRecipientBrowser.Unlock()
		// Recipient state is folded in through chatState methods: this file
		// never takes chatBrowser's lock itself, which is what keeps the two
		// locks in one order (recipient first, chat second, never held
		// together) instead of five ad-hoc reach-ins.
		chatBrowser.mutate(func(model *chatui.Model) {
			for i := range model.Conversations {
				if count := counts[model.Conversations[i].ID]; count != nil {
					model.Conversations[i].Unread = int(count.GetUnreadCount())
					model.Conversations[i].Mentions = int(count.GetMentionCount())
				}
			}
			clearOpenRoomUnread(model)
			if sideErr == nil && freshSidebar {
				applyRecipientLayout(model, layout, hosts)
			}
			if quietErr == nil && quiet.GetQuietHours() != nil {
				model.Preferences.QuietHours = quiet.GetQuietHours().GetEnabled()
				model.Preferences.QuietTimezone = quiet.GetQuietHours().GetTimezone()
				model.Preferences.QuietStartMinute = int(quiet.GetQuietHours().GetStartMinute())
				model.Preferences.QuietEndMinute = int(quiet.GetQuietHours().GetEndMinute())
			}
		})
		if sideErr == nil && freshSidebar {
			// Drafts ride in the same blob, so a reload restores the composer.
			chatBrowser.loadDrafts(layout.Drafts)
		}
		refreshChatRoute()
	}()
}

func applyCachedChatRecipientProjection(model *chatui.Model) {
	chatRecipientBrowser.Lock()
	layout := chatRecipientBrowser.layout
	quiet := chatRecipientBrowser.quiet
	counts := chatRecipientBrowser.counts
	follows := chatRecipientBrowser.follows
	hosts := chatRecipientBrowser.hosts
	pendingSidebar := chatRecipientBrowser.sidebarEdit != chatRecipientBrowser.sidebarSaved
	chatRecipientBrowser.Unlock()
	for i := range model.Conversations {
		if count := counts[model.Conversations[i].ID]; count != nil {
			model.Conversations[i].Unread = int(count.GetUnreadCount())
			model.Conversations[i].Mentions = int(count.GetMentionCount())
		}
	}
	clearOpenRoomUnread(model)
	if !pendingSidebar && (len(layout.Sections) > 0 || layout.Panes.Rail > 0 || layout.Panes.Details > 0) {
		applyRecipientLayout(model, layout, hosts)
	}
	if quiet != nil {
		model.Preferences.QuietHours = quiet.GetEnabled()
		model.Preferences.QuietTimezone = quiet.GetTimezone()
		model.Preferences.QuietStartMinute = int(quiet.GetStartMinute())
		model.Preferences.QuietEndMinute = int(quiet.GetEndMinute())
	}
	if model.ThreadParentID != "" {
		if f := follows[model.SelectedID+"\x00"+model.ThreadParentID]; f != nil {
			model.ThreadFollowed = f.GetFollowed()
		}
	}
}

func applyRecipientLayout(model *chatui.Model, layout recipientLayout, hosts map[string]string) {
	model.JoinPromptSeen = make(map[string]bool, len(layout.DismissedJoinPrompts))
	for _, id := range layout.DismissedJoinPrompts {
		if id != "" {
			model.JoinPromptSeen[id] = true
		}
	}
	if model.JoinPromptID != "" && model.JoinPromptSeen[model.JoinPromptID] {
		model.JoinPromptID, model.JoinPromptPending = "", false
	}
	starred := make(map[string]bool, len(layout.Starred))
	for _, id := range layout.Starred {
		starred[id] = true
	}
	for i := range model.Conversations {
		model.Conversations[i].Starred = starred[model.Conversations[i].ID]
	}
	byID := make(map[string]chatui.Conversation, len(model.Conversations))
	for _, c := range model.Conversations {
		byID[c.ID] = c
	}
	sections := make([]chatui.SidebarSection, 0, len(layout.Sections))
	used := make(map[string]bool)
	for _, s := range layout.Sections {
		if s.ID == "" {
			continue
		}
		section := chatui.SidebarSection{ID: s.ID, Name: s.Name, Collapsed: s.Collapsed}
		if section.Name == "" {
			section.Name = s.ID
		}
		for _, ref := range s.Chats {
			if c, ok := byID[ref.ConversationID]; ok && hosts[c.ID] == ref.HostTenantID && !used[c.ID] {
				section.Chats = append(section.Chats, c)
				used[c.ID] = true
			}
		}
		sections = append(sections, section)
	}
	if len(sections) > 0 {
		model.Sections = sections
		ensureRecipientSections(model)
		sections = model.Sections
		used = make(map[string]bool)
		for _, section := range sections {
			for _, c := range section.Chats {
				used[c.ID] = true
			}
		}
		for _, c := range model.Conversations {
			if !used[c.ID] {
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
		}
		model.Sections = sections
	}
	pane := layout.Panes
	if specific, ok := layout.PanesByDevice[recipientDeviceClass()]; ok {
		pane = specific
	}
	if pane.Rail > 0 || pane.Details > 0 {
		model.Pane = pane
		model.Preferences.Panes = pane
	}
}

func recipientDeviceClass() string {
	width := js.Global().Get("innerWidth").Int()
	if width < 600 {
		return "mobile"
	}
	if width < 1024 {
		return "tablet"
	}
	return "desktop"
}

func withChatRecipientCallbacks(callbacks chatui.Callbacks, cfg journeyclient.Config, refresh func()) chatui.Callbacks {
	callbacks.SetThreadFollow = func(followed bool) {
		model := chatBrowser.snapshot()
		if !model.ShowThread || model.ThreadParentID == "" || model.SelectedID == "" {
			return
		}
		go putChatThreadFollow(cfg, model.SelectedID, model.ThreadParentID, followed)
	}
	callbacks.ToggleSection = func(id string) {
		model := chatBrowser.mutate(func(model *chatui.Model) {
			ensureRecipientSections(model)
			for i := range model.Sections {
				if model.Sections[i].ID == id {
					model.Sections[i].Collapsed = !model.Sections[i].Collapsed
					break
				}
			}
		})
		persistChatRecipientSidebar(cfg, model)
		refresh()
	}
	callbacks.ReorderSection = func(id string, delta int) {
		model := chatBrowser.mutate(func(model *chatui.Model) {
			ensureRecipientSections(model)
			for i := range model.Sections {
				if model.Sections[i].ID == id {
					j := i + delta
					if j >= 0 && j < len(model.Sections) {
						model.Sections[i], model.Sections[j] = model.Sections[j], model.Sections[i]
					}
					break
				}
			}
		})
		persistChatRecipientSidebar(cfg, model)
		refresh()
	}
	callbacks.CreateSection = func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		model := chatBrowser.mutate(func(model *chatui.Model) {
			ensureRecipientSections(model)
			if len(model.Sections) >= 30 {
				return
			}
			model.Sections = append(model.Sections, chatui.SidebarSection{ID: "custom-" + strconv.FormatInt(time.Now().UnixNano(), 36), Name: name})
		})
		persistChatRecipientSidebar(cfg, model)
		refresh()
	}
	callbacks.RemoveSection = func(id string) {
		if id == "channels" || id == "direct" {
			return
		}
		model := chatBrowser.mutate(func(model *chatui.Model) {
			ensureRecipientSections(model)
			for i, section := range model.Sections {
				if section.ID == id {
					for _, c := range section.Chats {
						target := "channels"
						if c.Kind == chatui.DirectMessage || c.Kind == chatui.GroupChat {
							target = "direct"
						}
						for j := range model.Sections {
							if model.Sections[j].ID == target {
								model.Sections[j].Chats = append(model.Sections[j].Chats, c)
								break
							}
						}
					}
					model.Sections = append(model.Sections[:i], model.Sections[i+1:]...)
					break
				}
			}
		})
		persistChatRecipientSidebar(cfg, model)
		refresh()
	}
	callbacks.MoveConversationSection = func(chatID, sectionID string) {
		model := chatBrowser.mutate(func(model *chatui.Model) {
			ensureRecipientSections(model)
			target := -1
			for i := range model.Sections {
				if model.Sections[i].ID == sectionID {
					target = i
				}
			}
			if target < 0 || len(model.Sections[target].Chats) >= 200 {
				return
			}
			for i := range model.Sections {
				for j, c := range model.Sections[i].Chats {
					if c.ID == chatID {
						if i == target {
							return
						}
						model.Sections[i].Chats = append(model.Sections[i].Chats[:j], model.Sections[i].Chats[j+1:]...)
						model.Sections[target].Chats = append(model.Sections[target].Chats, c)
						return
					}
				}
			}
		})
		persistChatRecipientSidebar(cfg, model)
		refresh()
	}
	callbacks.MoveConversationOrder = func(chatID string, delta int) {
		moved := false
		model := chatBrowser.mutate(func(model *chatui.Model) {
			ensureRecipientSections(model)
			for i := range model.Sections {
				for j, conversation := range model.Sections[i].Chats {
					if conversation.ID != chatID {
						continue
					}
					target := j + delta
					if delta == 0 || target < 0 || target >= len(model.Sections[i].Chats) {
						return
					}
					model.Sections[i].Chats[j], model.Sections[i].Chats[target] = model.Sections[i].Chats[target], model.Sections[i].Chats[j]
					moved = true
					return
				}
			}
		})
		if moved {
			persistChatRecipientSidebar(cfg, model)
			refresh()
		}
	}
	callbacks.ResizeRail = func(px int) {
		changeRecipientPane(cfg, refresh, func(p *chatui.PaneSizes) { p.Rail = clampChatPane(px, chatRailMin, chatRailMax) })
	}
	callbacks.ResizeDetails = func(px int) {
		changeRecipientPane(cfg, refresh, func(p *chatui.PaneSizes) { p.Details = clampChatPane(px, chatDetailsMin, chatDetailsMax) })
	}
	callbacks.RestorePanes = func() {
		changeRecipientPane(cfg, refresh, func(p *chatui.PaneSizes) {
			*p = chatui.PaneSizes{Rail: chatRailDefault, Details: chatDetailsDefault}
		})
	}
	previousSave := callbacks.SavePreferences
	callbacks.SavePreferences = func(prefs chatui.Preferences) {
		current := chatBrowser.snapshot()
		selected := current.SelectedID
		modeChanged := prefs.Notifications[selected] != current.Preferences.Notifications[selected]
		quietChanged := prefs.QuietHours != current.Preferences.QuietHours || prefs.QuietTimezone != current.Preferences.QuietTimezone || prefs.QuietStartMinute != current.Preferences.QuietStartMinute || prefs.QuietEndMinute != current.Preferences.QuietEndMinute
		paneChanged := prefs.Panes != current.Pane
		// This delegation is the reachable UpdatePreferences path: the chat
		// half's SavePreferences is called from here and nowhere else.
		if modeChanged && previousSave != nil {
			previousSave(prefs)
		}
		// Drafts belong to the composer, not to this dialog. A preferences
		// save must not drop the text the reader is still typing.
		prefs.Drafts = chatBrowser.allDrafts()
		model := chatBrowser.mutate(func(model *chatui.Model) {
			model.Preferences = prefs
			model.Pane = prefs.Panes
		})
		if paneChanged {
			persistChatRecipientSidebar(cfg, model)
		}
		if quietChanged {
			persistChatQuietHours(cfg, prefs)
		}
		refresh()
	}
	return callbacks
}

// Pane bounds are chatui's: the drag handles, the keyboard nudges and this
// clamp all read the same constants, so a width the reader chose is never
// accepted by one side and refused by the other.
const (
	chatRailMin, chatRailMax, chatRailDefault          = chatui.RailMin, chatui.RailMax, chatui.RailDefault
	chatDetailsMin, chatDetailsMax, chatDetailsDefault = chatui.DetailsMin, chatui.DetailsMax, chatui.DetailsDefault
)

// clampChatPane holds a pane width inside its bounds.
func clampChatPane(px, low, high int) int {
	if px < low {
		return low
	}
	if px > high {
		return high
	}
	return px
}

func loadChatThreadFollow(cfg journeyclient.Config, root string) {
	conversation := chatBrowser.selectedID()
	chatRecipientBrowser.Lock()
	client := chatRecipientBrowser.client
	host := chatRecipientBrowser.hosts[conversation]
	generation := chatRecipientBrowser.generation
	chatRecipientBrowser.Unlock()
	if client == nil || conversation == "" || root == "" {
		return
	}
	if host == "" {
		host = cfg.Tenant
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		result, err := client.GetThreadFollow(chatRPCContext(ctx, cfg), &chatv1.GetThreadFollowRequest{TenantId: host, ConversationId: conversation, RootPostId: root})
		if err != nil {
			return
		}
		chatRecipientBrowser.Lock()
		if generation != chatRecipientBrowser.generation {
			chatRecipientBrowser.Unlock()
			return
		}
		chatRecipientBrowser.follows[conversation+"\x00"+root] = result.GetFollow()
		chatRecipientBrowser.Unlock()
		chatBrowser.mutate(func(model *chatui.Model) {
			if model.SelectedID == conversation && model.ThreadParentID == root {
				model.ThreadFollowed = result.GetFollow().GetFollowed()
			}
		})
		refreshChatRoute()
	}()
}

// markChatRead advances only after a successful visible post fetch. The
// server's current revision and monotonic cursor remain the source of truth.
func markChatRead(cfg journeyclient.Config, conversation string, posts []*chatv1.Post) {
	var last uint64
	host := cfg.Tenant
	for _, post := range posts {
		if post != nil && post.GetSequence() > last {
			last = post.GetSequence()
			if post.GetTenantId() != "" {
				host = post.GetTenantId()
			}
		}
	}
	if conversation == "" || last == 0 {
		return
	}
	key := host + "\x00" + conversation
	chatRecipientBrowser.Lock()
	if last <= chatRecipientBrowser.readMarked[key] {
		chatRecipientBrowser.Unlock()
		return
	}
	generation := chatRecipientBrowser.generation
	chatRecipientBrowser.Unlock()
	go func() {
		client := chatBrowser.conversationClient()
		if client == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		ctx = chatRPCContext(ctx, cfg)
		current, err := client.GetReadState(ctx, &chatv1.GetReadStateRequest{TenantId: host, ConversationId: conversation})
		if err != nil || !recipientGenerationActive(generation) {
			return
		}
		if state := current.GetState(); state != nil && state.GetLastReadSequence() >= last {
			chatRecipientBrowser.Lock()
			if generation == chatRecipientBrowser.generation {
				chatRecipientBrowser.readMarked[key] = last
			}
			chatRecipientBrowser.Unlock()
			return
		}
		revision := uint64(1)
		if current.GetState() != nil {
			revision = current.GetState().GetRevision()
		}
		_, err = client.UpdateReadState(ctx, &chatv1.UpdateReadStateRequest{State: &chatv1.ReadState{TenantId: host, ConversationId: conversation, LastReadSequence: last}, ExpectedRevision: revision})
		if err != nil || !recipientGenerationActive(generation) {
			return
		}
		chatRecipientBrowser.Lock()
		chatRecipientBrowser.readMarked[key] = last
		chatRecipientBrowser.loadedAt = time.Time{}
		chatRecipientBrowser.Unlock()
		refreshChatRoute()
	}()
}
func putChatThreadFollow(cfg journeyclient.Config, conversation, root string, followed bool) {
	chatRecipientBrowser.Lock()
	client := chatRecipientBrowser.client
	host := chatRecipientBrowser.hosts[conversation]
	generation := chatRecipientBrowser.generation
	current := chatRecipientBrowser.follows[conversation+"\x00"+root]
	chatRecipientBrowser.Unlock()
	if client == nil {
		return
	}
	if host == "" {
		host = cfg.Tenant
	}
	revision := uint64(1)
	if current != nil {
		revision = current.GetRevision()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := client.PutThreadFollow(chatRPCContext(ctx, cfg), &chatv1.PutThreadFollowRequest{Follow: &chatv1.ThreadFollow{TenantId: host, ConversationId: conversation, RootPostId: root, Followed: followed}, ExpectedRevision: revision})
	if err != nil {
		if recipientGenerationActive(generation) {
			recipientActionError("follow this thread", err)
			loadChatThreadFollow(cfg, root)
		}
		return
	}
	chatRecipientBrowser.Lock()
	if generation != chatRecipientBrowser.generation {
		chatRecipientBrowser.Unlock()
		return
	}
	chatRecipientBrowser.follows[conversation+"\x00"+root] = result.GetFollow()
	chatRecipientBrowser.Unlock()
	chatBrowser.mutate(func(model *chatui.Model) {
		if model.SelectedID == conversation && model.ThreadParentID == root {
			model.ThreadFollowed = result.GetFollow().GetFollowed()
		}
	})
	refreshChatRoute()
}

func ensureRecipientSections(model *chatui.Model) {
	if len(model.Sections) == 0 || (len(model.Sections) == 1 && model.Sections[0].ID == "all") {
		channels := chatui.SidebarSection{ID: "channels", Name: "Channels"}
		direct := chatui.SidebarSection{ID: "direct", Name: "Direct messages"}
		for _, c := range model.Conversations {
			if c.Kind == chatui.DirectMessage || c.Kind == chatui.GroupChat {
				direct.Chats = append(direct.Chats, c)
			} else {
				channels.Chats = append(channels.Chats, c)
			}
		}
		model.Sections = []chatui.SidebarSection{channels, direct}
		return
	}
	channels, direct := false, false
	for _, section := range model.Sections {
		if section.ID == "channels" {
			channels = true
		}
		if section.ID == "direct" {
			direct = true
		}
	}
	if !channels {
		model.Sections = append([]chatui.SidebarSection{{ID: "channels", Name: "Channels"}}, model.Sections...)
	}
	if !direct {
		model.Sections = append(model.Sections, chatui.SidebarSection{ID: "direct", Name: "Direct messages"})
	}
}
func changeRecipientPane(cfg journeyclient.Config, refresh func(), change func(*chatui.PaneSizes)) {
	model := chatBrowser.mutate(func(model *chatui.Model) {
		change(&model.Pane)
		model.Preferences.Panes = model.Pane
	})
	// The cached layout is what every projection load applies. Updating it
	// only when the write came back let a refresh started in between snap
	// the column back to the old width until the round trip landed, which
	// the reader saw as a flicker at the end of every drag.
	chatRecipientBrowser.Lock()
	chatRecipientBrowser.layout.Panes = model.Pane
	if chatRecipientBrowser.layout.PanesByDevice == nil {
		chatRecipientBrowser.layout.PanesByDevice = map[string]chatui.PaneSizes{}
	}
	chatRecipientBrowser.layout.PanesByDevice[recipientDeviceClass()] = model.Pane
	chatRecipientBrowser.Unlock()
	refresh()
	persistChatRecipientSidebar(cfg, model)
}
func persistChatRecipientSidebar(cfg journeyclient.Config, model chatui.Model, draftOnly ...bool) {
	cfg = chatBrowser.config(cfg)
	chatRecipientBrowser.Lock()
	chatRecipientBrowser.sidebarEdit++
	chatRecipientBrowser.Unlock()
	go func() {
		chatRecipientBrowser.sidebarWrite.Lock()
		defer chatRecipientBrowser.sidebarWrite.Unlock()
		// Queued writers read the newest model only after taking the write lock.
		// A shallow model captured by an older callback must never write last.
		model = chatBrowser.snapshot()
		chatRecipientBrowser.Lock()
		client := chatRecipientBrowser.client
		generation := chatRecipientBrowser.generation
		revision := chatRecipientBrowser.sidebarRevision
		edit := chatRecipientBrowser.sidebarEdit
		hosts := make(map[string]string, len(chatRecipientBrowser.hosts))
		for id, host := range chatRecipientBrowser.hosts {
			hosts[id] = host
		}
		chatRecipientBrowser.Unlock()
		if client == nil {
			return
		}
		ensureRecipientSections(&model)
		layout := recipientLayout{Panes: model.Pane, Sections: make([]recipientSection, 0, len(model.Sections))}
		layout.DismissedJoinPrompts = chatDismissedJoinPromptIDs(model.JoinPromptSeen)
		chatRecipientBrowser.Lock()
		layout.PanesByDevice = make(map[string]chatui.PaneSizes, len(chatRecipientBrowser.layout.PanesByDevice)+1)
		for device, pane := range chatRecipientBrowser.layout.PanesByDevice {
			layout.PanesByDevice[device] = pane
		}
		layout.Filters = make(map[string]string, len(chatRecipientBrowser.layout.Filters))
		for id, filter := range chatRecipientBrowser.layout.Filters {
			layout.Filters[id] = filter
		}
		chatRecipientBrowser.Unlock()
		layout.PanesByDevice[recipientDeviceClass()] = model.Pane
		var draftRevision uint64
		var draftEpoch uint64
		layout.Drafts, draftRevision, draftEpoch = chatBrowser.draftsForWrite()
		for _, c := range model.Conversations {
			if c.Starred {
				layout.Starred = append(layout.Starred, c.ID)
			}
		}
		for _, section := range model.Sections {
			x := recipientSection{ID: section.ID, Name: section.Name, Collapsed: section.Collapsed}
			for _, c := range section.Chats {
				host := hosts[c.ID]
				if host == "" {
					host = cfg.Tenant
				}
				x.Chats = append(x.Chats, recipientChatRef{HostTenantID: host, ConversationID: c.ID})
			}
			layout.Sections = append(layout.Sections, x)
		}
		reportWriteError := func(err error) {
			if recipientGenerationActive(generation) {
				recipientActionError("save your sidebar", err)
				invalidateChatRecipientProjection()
			}
		}
		var result *chatv1.PutSidebarResponse
		for attempt := 0; attempt < 3; attempt++ {
			if !recipientGenerationActive(generation) {
				return
			}
			body, err := json.Marshal(layout)
			if err != nil {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			result, err = client.PutSidebar(chatRPCContext(ctx, cfg), &chatv1.PutSidebarRequest{Sidebar: &chatv1.SidebarState{TenantId: cfg.Tenant, LayoutJson: string(body)}, ExpectedRevision: revision})
			cancel()
			if err == nil {
				break
			}
			if status.Code(err) != codes.Aborted || attempt == 2 {
				reportWriteError(err)
				return
			}
			ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
			fresh, readErr := client.GetSidebar(chatRPCContext(ctx, cfg), &chatv1.GetSidebarRequest{})
			cancel()
			if readErr != nil || fresh.GetSidebar() == nil || !recipientGenerationActive(generation) {
				if recipientGenerationActive(generation) {
					if readErr == nil {
						readErr = status.Error(codes.Unavailable, "sidebar state unavailable")
					}
					reportWriteError(readErr)
				}
				return
			}
			var server recipientLayout
			if json.Unmarshal([]byte(fresh.GetSidebar().GetLayoutJson()), &server) != nil || fresh.GetSidebar().GetRevision() == 0 {
				reportWriteError(status.Error(codes.Unavailable, "sidebar state unavailable"))
				return
			}
			revision = fresh.GetSidebar().GetRevision()
			chatRecipientBrowser.Lock()
			if generation != chatRecipientBrowser.generation {
				chatRecipientBrowser.Unlock()
				return
			}
			chatRecipientBrowser.sidebarRevision = revision
			chatRecipientBrowser.Unlock()
			drafts, stamp, current := chatBrowser.rebaseDrafts(server.Drafts, cfg, draftEpoch)
			if !current {
				return
			}
			draftRevision = stamp
			if len(draftOnly) > 0 && draftOnly[0] {
				if !keepDraftRooms(&server, layout, drafts) {
					reportWriteError(status.Error(codes.FailedPrecondition, "sidebar conversation changed"))
					return
				}
				layout = server
			}
			layout.Drafts = drafts
		}
		if result == nil || result.GetSidebar() == nil {
			return
		}
		if !recipientGenerationActive(generation) {
			return
		}
		chatBrowser.markDraftsPersisted(draftRevision, cfg, draftEpoch)
		chatRecipientBrowser.Lock()
		if generation != chatRecipientBrowser.generation {
			chatRecipientBrowser.Unlock()
			return
		}
		chatRecipientBrowser.sidebarRevision = result.GetSidebar().GetRevision()
		chatRecipientBrowser.sidebarSaved = edit
		if edit == chatRecipientBrowser.sidebarEdit {
			chatRecipientBrowser.layout = layout
		}
		chatRecipientBrowser.Unlock()
	}()
}
func persistChatQuietHours(cfg journeyclient.Config, prefs chatui.Preferences) {
	go func() {
		chatRecipientBrowser.quietWrite.Lock()
		defer chatRecipientBrowser.quietWrite.Unlock()
		chatRecipientBrowser.Lock()
		client := chatRecipientBrowser.client
		generation := chatRecipientBrowser.generation
		revision := chatRecipientBrowser.quietRevision
		current := chatRecipientBrowser.quiet
		chatRecipientBrowser.Unlock()
		if client == nil {
			return
		}
		next := &chatv1.QuietHours{TenantId: cfg.Tenant, Enabled: prefs.QuietHours, Timezone: prefs.QuietTimezone, StartMinute: uint32(prefs.QuietStartMinute), EndMinute: uint32(prefs.QuietEndMinute)}
		if next.Timezone == "" {
			next.Timezone = "UTC"
			if current != nil {
				next.Timezone = current.GetTimezone()
			}
		}
		chatRecipientBrowser.Lock()
		active := generation == chatRecipientBrowser.generation
		chatRecipientBrowser.Unlock()
		if !active {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		result, err := client.PutQuietHours(chatRPCContext(ctx, cfg), &chatv1.PutQuietHoursRequest{QuietHours: next, ExpectedRevision: revision})
		if err != nil {
			if recipientGenerationActive(generation) {
				recipientActionError("save your quiet hours", err)
				invalidateChatRecipientProjection()
			}
			return
		}
		chatRecipientBrowser.Lock()
		if generation != chatRecipientBrowser.generation {
			chatRecipientBrowser.Unlock()
			return
		}
		chatRecipientBrowser.quietRevision = result.GetQuietHours().GetRevision()
		chatRecipientBrowser.quiet = result.GetQuietHours()
		chatRecipientBrowser.Unlock()
	}()
}

// clearOpenRoomUnread drops the badge on the room that is open: opening it
// marks it read, and a count that stayed until the next projection read
// taught readers that the badge means nothing.
func clearOpenRoomUnread(model *chatui.Model) {
	for i := range model.Conversations {
		if model.Conversations[i].ID == model.SelectedID {
			model.Conversations[i].Unread, model.Conversations[i].Mentions = 0, 0
		}
	}
}

func invalidateChatRecipientProjection() {
	chatRecipientBrowser.Lock()
	chatRecipientBrowser.loadedAt = time.Time{}
	chatRecipientBrowser.Unlock()
}
func recipientGenerationActive(generation uint64) bool {
	chatRecipientBrowser.Lock()
	active := chatRecipientBrowser.generation == generation
	chatRecipientBrowser.Unlock()
	return active
}

// recipientActionError reports a failed personal-state write as a transient
// notice. It used to put the whole surface into StateError, so a sidebar save
// that failed replaced the timeline with "Conversation unavailable" and left
// it replaced until the next configure.
func recipientActionError(action string, err error) {
	if err == nil {
		return
	}
	noteChatAction(actionFailureNotice(action, err))
}
