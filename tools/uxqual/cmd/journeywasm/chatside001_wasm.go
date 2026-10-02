//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// CHATSIDE-001: the person's own sections in the browser. Every change is
// applied to the page at once and written with the rest of the sidebar layout
// (persistChatRecipientSidebar); when the server refuses the write, the layout
// the page held before the change is put back and a plain notice says so.

// sideSnapshot is the part of the model one sidebar change may alter.
type sideSnapshot struct {
	sections           []chatui.SidebarSection
	favoritesCollapsed bool
	starred            map[string]bool
}

func captureSide(m *chatui.Model) sideSnapshot {
	snap := sideSnapshot{favoritesCollapsed: m.FavoritesCollapsed, starred: map[string]bool{}}
	snap.sections = make([]chatui.SidebarSection, len(m.Sections))
	for i, section := range m.Sections {
		section.Chats = append([]chatui.Conversation(nil), section.Chats...)
		snap.sections[i] = section
	}
	for _, c := range m.Conversations {
		if c.Starred {
			snap.starred[c.ID] = true
		}
	}
	return snap
}

func (s sideSnapshot) restore(m *chatui.Model) {
	m.Sections = make([]chatui.SidebarSection, len(s.sections))
	for i, section := range s.sections {
		section.Chats = append([]chatui.Conversation(nil), section.Chats...)
		m.Sections[i] = section
	}
	m.FavoritesCollapsed = s.favoritesCollapsed
	for id, on := range s.starred {
		setStarred(m, id, on)
	}
	for i := range m.Conversations {
		if !s.starred[m.Conversations[i].ID] {
			setStarred(m, m.Conversations[i].ID, false)
		}
	}
}

// setStarred marks a conversation a favorite or not, in the list and in the
// copies the sections hold.
func setStarred(m *chatui.Model, id string, on bool) {
	for i := range m.Conversations {
		if m.Conversations[i].ID == id {
			m.Conversations[i].Starred = on
		}
	}
	for i := range m.Sections {
		for j := range m.Sections[i].Chats {
			if m.Sections[i].Chats[j].ID == id {
				m.Sections[i].Chats[j].Starred = on
			}
		}
	}
}

func isStarred(m *chatui.Model, id string) bool {
	for _, c := range m.Conversations {
		if c.ID == id {
			return c.Starred
		}
	}
	return false
}

var sideUndo struct {
	sync.Mutex
	fn func()
}

func setSideUndo(fn func()) {
	sideUndo.Lock()
	sideUndo.fn = fn
	sideUndo.Unlock()
}

func takeSideUndo() func() {
	sideUndo.Lock()
	fn := sideUndo.fn
	sideUndo.fn = nil
	sideUndo.Unlock()
	return fn
}

// chatside001Confirmed is called when the server has accepted a sidebar write:
// the change cannot be refused any more, so there is nothing to put back.
func chatside001Confirmed() { takeSideUndo() }

// chatside001Failed is called when the server refused a sidebar write. If the
// refused write was one of these changes, the layout is put back, the person is
// told, and the caller has nothing more to report; it returns false for any
// other sidebar write so the generic notice and Try again still apply.
func chatside001Failed() bool {
	undo := takeSideUndo()
	if undo == nil {
		return false
	}
	undo()
	locale := chatBrowser.snapshot().Locale
	chatRecipientBrowser.Lock()
	chatRecipientBrowser.sidebarSaved = chatRecipientBrowser.sidebarEdit
	chatRecipientBrowser.Unlock()
	invalidateChatRecipientProjection()
	noteChatAction(chatui.Chatside001NotSaved(locale))
	return true
}

// changeSide applies one change to the sidebar layout. change reports whether
// it altered anything; an unaltered layout is not written.
func changeSide(cfg journeyclient.Config, refresh func(), change func(*chatui.Model) bool) {
	var before sideSnapshot
	changed := false
	model := chatBrowser.mutate(func(m *chatui.Model) {
		ensureRecipientSections(m)
		before = captureSide(m)
		changed = change(m)
	})
	if !changed {
		return
	}
	setSideUndo(func() { chatBrowser.mutate(func(m *chatui.Model) { before.restore(m) }) })
	persistChatRecipientSidebar(cfg, model)
	refresh()
}

// chatside001KeepStarred drops the stars of conversations no section holds (a
// channel the person has left), so a stale star never makes the save fail.
func chatside001KeepStarred(starred []string, inSection map[string]bool) []string {
	var kept []string
	for _, id := range starred {
		if inSection[id] {
			kept = append(kept, id)
		}
	}
	return kept
}

func sideSectionIndex(m *chatui.Model, id string) int {
	for i := range m.Sections {
		if m.Sections[i].ID == id {
			return i
		}
	}
	return -1
}

// sideDefaultSection is the built-in section a conversation belongs in when it
// is in none of the person's own.
func sideDefaultSection(c chatui.Conversation) string {
	if c.Kind == chatui.DirectMessage || c.Kind == chatui.GroupChat {
		return "direct"
	}
	return "channels"
}

func sideFits(sectionID string, c chatui.Conversation) bool {
	switch sectionID {
	case "channels", "direct":
		return sideDefaultSection(c) == sectionID
	}
	return true
}

func sideNewSection(m *chatui.Model, name string) (string, bool) {
	if msg := chatui.Chatside001NameError(*m, name, ""); msg != "" {
		return "", false
	}
	id := "custom-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	// A new section goes to the top, under Favorites; the person moves it from
	// there.
	m.Sections = chatside001InsertSection(m.Sections, chatui.SidebarSection{ID: id, Name: name})
	return id, true
}

// sideMove puts a conversation in a section: Favorites stars it and leaves the
// section it sits in alone; any other section takes it and ends the favorite.
func sideMove(m *chatui.Model, chatID, sectionID string) bool {
	if sectionID == "favorites" {
		if isStarred(m, chatID) {
			return false
		}
		setStarred(m, chatID, true)
		return true
	}
	target := sideSectionIndex(m, sectionID)
	if target < 0 || len(m.Sections[target].Chats) >= 200 {
		return false
	}
	starred := isStarred(m, chatID)
	for i := range m.Sections {
		for j, c := range m.Sections[i].Chats {
			if c.ID != chatID {
				continue
			}
			if !sideFits(sectionID, c) {
				return false
			}
			if i == target {
				if !starred {
					return false
				}
				setStarred(m, chatID, false)
				return true
			}
			m.Sections[i].Chats = append(m.Sections[i].Chats[:j], m.Sections[i].Chats[j+1:]...)
			c.Starred = false
			chatside001Place(&m.Sections[target], c)
			setStarred(m, chatID, false)
			return true
		}
	}
	return false
}

func withChatside001Callbacks(callbacks chatui.Callbacks, cfg journeyclient.Config, refresh func()) chatui.Callbacks {
	toggle := callbacks.ToggleSection
	callbacks.ToggleSection = func(id string) {
		if id != "favorites" {
			if toggle != nil {
				toggle(id)
			}
			return
		}
		changeSide(cfg, refresh, func(m *chatui.Model) bool {
			m.FavoritesCollapsed = !m.FavoritesCollapsed
			return true
		})
	}
	callbacks.ReorderSection = func(id string, delta int) {
		changeSide(cfg, refresh, func(m *chatui.Model) bool {
			i, j := sideSectionIndex(m, id), chatui.Chatside001Neighbor(m.Sections, id, delta)
			if i < 0 || j < 0 {
				return false
			}
			m.Sections[i], m.Sections[j] = m.Sections[j], m.Sections[i]
			return true
		})
	}
	callbacks.CreateSection = func(name string) {
		changeSide(cfg, refresh, func(m *chatui.Model) bool {
			_, ok := sideNewSection(m, name)
			return ok
		})
	}
	callbacks.CreateSectionFor = func(name, chatID string) {
		changeSide(cfg, refresh, func(m *chatui.Model) bool {
			id, ok := sideNewSection(m, name)
			if !ok {
				return false
			}
			sideMove(m, chatID, id)
			return true
		})
	}
	callbacks.RenameSection = func(id, name string) {
		changeSide(cfg, refresh, func(m *chatui.Model) bool {
			i := sideSectionIndex(m, id)
			if i < 0 || id == "channels" || id == "direct" || chatui.Chatside001NameError(*m, name, id) != "" {
				return false
			}
			m.Sections[i].Name = name
			return true
		})
	}
	callbacks.RemoveSection = func(id string) {
		changeSide(cfg, refresh, func(m *chatui.Model) bool {
			i := sideSectionIndex(m, id)
			if i < 0 || id == "channels" || id == "direct" {
				return false
			}
			// Its conversations go back to their default section: nothing is
			// left out and nothing is archived.
			for _, c := range m.Sections[i].Chats {
				if target := sideSectionIndex(m, sideDefaultSection(c)); target >= 0 {
					chatside001Place(&m.Sections[target], c)
				}
			}
			m.Sections = append(m.Sections[:i], m.Sections[i+1:]...)
			return true
		})
	}
	callbacks.MoveConversationSection = func(chatID, sectionID string) {
		changeSide(cfg, refresh, func(m *chatui.Model) bool { return sideMove(m, chatID, sectionID) })
	}
	callbacks.SetFavorite = func(chatID string, favorite bool) {
		changeSide(cfg, refresh, func(m *chatui.Model) bool {
			if isStarred(m, chatID) == favorite {
				return false
			}
			setStarred(m, chatID, favorite)
			return true
		})
	}
	return callbacks
}

// chatside001SidebarAnnounced is what a layout-changed notice from the event
// stream does in this tab: when another tab saved a newer layout, read it and
// draw it. Only the layout is taken; the drafts and the rest of the sidebar
// document stay as they are. A read that fails changes nothing and the periodic
// read stays the fallback.
func chatside001SidebarAnnounced(cfg journeyclient.Config, revision uint64) {
	chatRecipientBrowser.Lock()
	client, generation := chatRecipientBrowser.client, chatRecipientBrowser.generation
	known := chatRecipientBrowser.sidebarRevision
	pending := chatRecipientBrowser.sidebarEdit != chatRecipientBrowser.sidebarSaved
	chatRecipientBrowser.Unlock()
	if client == nil || !chatside001ShouldReread(known, revision, pending) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		side, err := client.GetSidebar(chatRPCContext(ctx, cfg), &chatv1.GetSidebarRequest{})
		if err != nil || side.GetSidebar() == nil {
			return
		}
		var layout recipientLayout
		if json.Unmarshal([]byte(side.GetSidebar().GetLayoutJson()), &layout) != nil {
			return
		}
		chatRecipientBrowser.Lock()
		if generation != chatRecipientBrowser.generation || chatRecipientBrowser.sidebarEdit != chatRecipientBrowser.sidebarSaved ||
			side.GetSidebar().GetRevision() <= chatRecipientBrowser.sidebarRevision {
			chatRecipientBrowser.Unlock()
			return
		}
		chatRecipientBrowser.sidebarRevision = side.GetSidebar().GetRevision()
		chatRecipientBrowser.layout = layout
		hosts := make(map[string]string, len(chatRecipientBrowser.hosts))
		for id, host := range chatRecipientBrowser.hosts {
			hosts[id] = host
		}
		chatRecipientBrowser.Unlock()
		chatBrowser.mutate(func(m *chatui.Model) { applyRecipientLayout(m, layout, hosts) })
		refreshChatRoute()
	}()
}
