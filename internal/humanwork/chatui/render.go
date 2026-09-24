package chatui

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// handlers is the workspace's complete, fixed set of event hooks.
//
// GoWebComponents binds hooks by position: a UseEvent call inside a loop or a
// branch moves every later hook to a different slot whenever the loop count
// changes, and the DOM then fires a wrapper that points at somebody else's
// closure (measured: Enter in the composer selected a different room, and the
// message landed there). So the workspace calls UseEvent exactly this many
// times, unconditionally, before rendering anything, and every row, message
// action and dialog button is a plain element with data-action/data-id that
// the one delegated click hook dispatches.
type handlers struct {
	rootClick, rootKey                                       ui.Handler
	composerSubmit, composerKey, composerInput               ui.Handler
	searchInput                                              ui.Handler
	quietToggle, quietTZ, quietStart, quietEnd               ui.Handler
	notifyMode                                               ui.Handler
	editInput, editSubmit, createSubmit, kindPick            ui.Handler
	threadSubmit, threadKey, threadInput                     ui.Handler
	browseFilter                                             ui.Handler
	shareFilter                                              ui.Handler
	memberFilter                                             ui.Handler
	sectionSubmit                                            ui.Handler
	todoSubmit                                               ui.Handler
	todoDraft, todoSource                                    ui.Handler
	todoPolicyMode, todoPolicyMember                         ui.Handler
	todoNewMode, todoNewMember                               ui.Handler
	teamPurposeSubmit, projectDetailsSubmit, milestoneSubmit ui.Handler
	pollCreate                                               ui.Handler
	pickInput, pickKey                                       ui.Handler
	// mentionView is the "@" suggestion list as of this render.
	mentionView mentionState
	// local is the tray and create-dialog state as of this render.
	local localUI
}

func bindHandlers(m Model, giphy *giphyPickerViews, mention mentionStore, local localStore, drafts *browserDrafts) handlers {
	act := func(action, id string) { m.act(action, id) }
	threadSend := func() {
		if query, ok := giphyCommand(domValue("thread-composer")); ok {
			if strings.TrimSpace(m.GiphyAPIKey) == "" {
				local.update(func(u *localUI) { u.composerNotice = m.t(KeyGiphyUnavailable) })
				return
			}
			setDOMValue("thread-composer", "")
			giphy.openSearch("thread-composer", m.GiphyAPIKey, query)
			return
		}
		replyInThread(m)
	}
	closeDrawer := func() {
		if m.SidebarOpen && m.Callbacks.ToggleSidebar != nil {
			m.Callbacks.ToggleSidebar(false)
		}
	}
	pick := func(id string) {
		st := local.get()
		for _, p := range pickCandidates(m, st.pickQuery, st.picked) {
			if p.ID == id {
				local.update(func(u *localUI) {
					u.picked = append(append([]mentionCandidate(nil), u.picked...), p)
					u.pickQuery, u.pickActive = "", 0
				})
				setDOMValue("new-chat-member-search", "")
				focusField("new-chat-member-search")
				return
			}
		}
	}
	mentionPick := func(state mentionState, index int) {
		people := mentionCandidates(m, state.Query)
		mention.Set(mentionState{})
		if !state.Open || index < 0 || index >= len(people) {
			return
		}
		// Re-read the field: the list was drawn a keystroke ago, and the
		// replacement must land on the "@query" the field holds now.
		value, caret, ok := composerSelection(state.Target)
		if !ok {
			return
		}
		if _, start, found := mentionTokenAt(value, caret); found && start == state.Start {
			updated, next := applyMention(value, start, caret, people[index].Name)
			replaceComposerText(state.Target, updated, next)
		}
	}
	mentionTrack := func(target string) {
		current := mention.Get()
		value, caret, ok := composerSelection(target)
		query, start, found := mentionTokenAt(value, caret)
		if !ok || !found {
			if current.Open {
				mention.Set(mentionState{})
			}
			return
		}
		if !current.Open && len(m.Members) == 0 && m.Callbacks.LoadMembers != nil {
			m.Callbacks.LoadMembers()
		}
		next := mentionState{Target: target, Query: query, Start: start, End: caret, Open: true}
		if current.Open && current.Target == target && current.Query == query {
			next.Active = current.Active
		}
		mention.Set(next)
	}
	mentionKey := func(e ui.KeyboardEvent, target string) bool {
		state := mention.Get()
		if !state.Open || state.Target != target {
			return false
		}
		count := len(mentionCandidates(m, state.Query))
		switch e.GetKey() {
		case "ArrowDown", "ArrowUp":
			if count == 0 {
				return false
			}
			delta := 1
			if e.GetKey() == "ArrowUp" {
				delta = -1
			}
			state.Active = nextMention(state.Active, delta, count)
			mention.Set(state)
		case "Enter", "Tab":
			if count == 0 {
				mention.Set(mentionState{})
				return false
			}
			mentionPick(state, state.Active)
		case "Escape":
			mention.Set(mentionState{})
			e.StopPropagation()
		default:
			return false
		}
		e.PreventDefault()
		return true
	}
	send := func() {
		if !drafts.canSend(m) {
			return
		}
		body := strings.TrimSpace(domValue("chat-composer"))
		if body == "" {
			body = strings.TrimSpace(m.Draft)
		}
		if query, ok := giphyCommand(body); ok {
			// "/giphy <search>" opens the GIF picker; it is never posted.
			if strings.TrimSpace(m.GiphyAPIKey) == "" {
				local.update(func(u *localUI) { u.composerNotice = m.t(KeyGiphyUnavailable) })
				return
			}
			setDOMValue("chat-composer", "")
			drafts.set(m.SelectedID, "")
			if m.Callbacks.DraftChanged != nil {
				m.Callbacks.DraftChanged(m.SelectedID, "")
			}
			giphy.openSearch("chat-composer", m.GiphyAPIKey, query)
			return
		}
		if body == "" || m.SelectedID == "" || m.Callbacks.SendMessage == nil {
			return
		}
		m.Callbacks.SendMessage(m.SelectedID, body)
		setDOMValue("chat-composer", "")
		drafts.set(m.SelectedID, "")
		if m.Callbacks.DraftChanged != nil {
			m.Callbacks.DraftChanged(m.SelectedID, "")
		}
	}
	savePrefs := func(mutate func(*Preferences)) {
		next := m.Preferences
		if next.Drafts != nil {
			next.Drafts = copyMap(next.Drafts)
		}
		if next.Notifications != nil {
			next.Notifications = copyMap(next.Notifications)
		}
		mutate(&next)
		if m.Callbacks.SavePreferences != nil {
			m.Callbacks.SavePreferences(next)
		}
	}
	return handlers{
		mentionView: mention.Get(),
		local:       local.get(),
		rootClick: ui.UseEvent(func(e ui.MouseEvent) {
			switch action, id, _ := eventAction(e); action {
			case "tray-todo", "tray-poll", "open-todo", "open-poll":
				which := "todo"
				if strings.HasSuffix(action, "poll") {
					which = "poll"
				}
				opening := local.get().tray != which
				local.update(func(u *localUI) {
					if opening {
						u.tray = which
					} else {
						u.tray = ""
					}
				})
				if opening && which == "todo" && m.Callbacks.OpenChannelTodo != nil {
					m.Callbacks.OpenChannelTodo()
				}
				if opening && which == "poll" && m.Callbacks.OpenChannelPoll != nil {
					m.Callbacks.OpenChannelPoll(m.SelectedID)
				}
				return
			case "tray-close":
				local.update(func(u *localUI) { u.tray = "" })
				return
			case "create-kind":
				local.update(func(u *localUI) {
					u.createKind = ConversationKind(id)
					if u.createKind == DirectMessage && len(u.picked) > 1 {
						u.picked = u.picked[:1]
					}
				})
				return
			case "create-pick":
				pick(id)
				return
			case "create-unpick":
				local.update(func(u *localUI) {
					kept := make([]mentionCandidate, 0, len(u.picked))
					for _, p := range u.picked {
						if p.ID != id {
							kept = append(kept, p)
						}
					}
					u.picked = kept
				})
				focusField("new-chat-member-search")
				return
			case "browse-open":
				if m.Callbacks.SelectConversation != nil {
					m.Callbacks.SelectConversation(id)
				}
				if m.Callbacks.CloseBrowse != nil {
					m.Callbacks.CloseBrowse()
				}
				return
			case "open-browse", "open-create", "browse-to-create":
				closeDrawer()
				local.resetCreate()
			}
			if action, _, extra := eventAction(e); action == "doc-suggest-pick" {
				if index, err := strconv.Atoi(extra); err == nil {
					docSuggestPick(local, index-1)
				}
				return
			}
			if action, id, extra := eventAction(e); action == "mention-pick" {
				if index, err := strconv.Atoi(extra); err == nil && mention.Get().Target == id {
					mentionPick(mention.Get(), index-1)
				}
				return
			}
			if mention.Get().Open {
				mention.Set(mentionState{})
			}
			if action, id, _ := eventAction(e); action == "open-doc-reference" {
				openDocReference(e, m, id)
				return
			}
			if action, _, _ := eventAction(e); action == "view-image" {
				openImageViewer(e, m.t(KeyImageViewer), m.t(KeyCloseImageViewer), m.t(KeyDownloadAttachment), m.t(KeyImageActualSize), m.t(KeyImageFitToScreen))
				return
			}
			if eventOnBackdrop(e) {
				if m.SharePostID != "" {
					act("close-share", "")
				} else if m.ShowCreate {
					act("close-create", "")
					restoreChatDialogFocus()
				} else if m.ShowBrowse {
					act("close-browse", "")
					restoreChatDialogFocus()
				}
				return
			}
			action, id, extra := eventAction(e)
			if action == "emoji-toggle" {
				toggleEmojiPicker(id)
				return
			}
			if action == "emoji-insert" {
				insertComposerEmoji(id, extra)
				return
			}
			if action == "format" {
				applyComposerFormat(id, extra)
				return
			}
			if action == "search-in-channel" && m.Callbacks.Search != nil {
				next := strings.TrimSpace(m.Search) + " in:#" + extra
				setDOMValue("chat-search", next)
				m.Callbacks.Search(next)
				return
			}
			if action == "reveal-thread-parent" {
				revealThreadParent(m.ShowThread, m.ThreadParentID)
				return
			}
			if action == "giphy-toggle" {
				giphy.toggle(id, m.GiphyAPIKey)
				return
			}
			if action == "giphy-more" {
				giphy.loadMore(id)
				return
			}
			if action == "giphy-select" {
				giphy.selectResult(id, extra)
				return
			}
			if action != "" {
				if action == "select" {
					currentDraft := domValue("chat-composer")
					if currentDraft == "" {
						currentDraft = m.Draft
					}
					drafts.selectConversation(m.SelectedID, currentDraft, id)
					if m.Callbacks.DraftChanged != nil && m.SelectedID != "" {
						m.Callbacks.DraftChanged(m.SelectedID, currentDraft)
					}
				}
				closeEmojiPickers(false)
				openRailMenu := action == "rail-menu" && m.RailMenuID != id
				openMessageMenu := action == "menu" && m.MenuID != id
				focusMenuItem := (openRailMenu || openMessageMenu) && menuTriggerIsFocusVisible(e)
				if action == "open-rail" {
					rememberMobileRailTrigger(e)
				}
				if action == "open-person" {
					rememberPersonTrigger(e)
				}
				if action == "open-share" {
					rememberShareTrigger(e)
				}
				if action == "open-create" || action == "open-browse" {
					rememberChatDialogTrigger(e, action)
				}
				if action == "menu" && m.MenuID != id {
					rememberMessageMenuTrigger(e, id)
				}
				if action == "rail-menu" {
					positionRailMenu(e)
				}
				m.actWith(action, id, extra)
				switch action {
				case "open-create", "open-browse", "browse-to-create":
					focusChatDialog()
				case "close-create", "close-browse":
					restoreChatDialogFocus()
				}
				if openRailMenu && m.Callbacks.OpenRailMenu != nil {
					focusRailMenu(func() { m.Callbacks.OpenRailMenu("") }, focusMenuItem)
				}
				if openMessageMenu && m.Callbacks.OpenMenu != nil {
					focusMessageMenu(id, focusMenuItem)
				}
				return
			}
			closeEmojiPickers(false)
			// A click anywhere else closes an open picker or menu.
			if m.PickerID != "" && m.Callbacks.OpenPicker != nil {
				m.Callbacks.OpenPicker("")
			}
			if m.MenuID != "" && m.Callbacks.OpenMenu != nil {
				m.Callbacks.OpenMenu("")
			}
			if m.RailMenuID != "" && m.Callbacks.OpenRailMenu != nil {
				clearRailMenuDismiss()
				m.Callbacks.OpenRailMenu("")
			}
		}),
		rootKey: ui.UseEvent(func(e ui.KeyboardEvent) {
			if handleEmojiPickerKey(e) {
				e.PreventDefault()
				return
			}
			if (m.ShowCreate || m.ShowBrowse) && trapChatDialogFocus(e) {
				e.PreventDefault()
				return
			}
			if m.SharePostID != "" && trapShareFocus(e) {
				e.PreventDefault()
				return
			}
			if m.SidebarOpen && trapMobileRailFocus(e) {
				e.PreventDefault()
				return
			}
			if m.RailMenuID != "" {
				if moveRailMenuFocus(e) {
					e.PreventDefault()
					return
				}
				if e.GetKey() == "Tab" && m.Callbacks.OpenRailMenu != nil {
					clearRailMenuDismiss()
					m.Callbacks.OpenRailMenu("")
				}
			}
			if m.MenuID != "" {
				if moveMessageMenuFocus(e, m.MenuID) {
					e.PreventDefault()
					return
				}
				if e.GetKey() == "Tab" && m.Callbacks.OpenMenu != nil {
					m.Callbacks.OpenMenu("")
					restoreMessageMenuFocus(m.MenuID)
					e.PreventDefault()
					return
				}
			}
			if pane := eventPane(e); pane != "" {
				if m.resizeFromKey(pane, e.GetKey()) {
					e.PreventDefault()
				}
				return
			}
			if e.GetKey() != "Escape" {
				return
			}
			if sectionCreateOpen() {
				closeSectionCreate(true)
				e.PreventDefault()
				return
			}
			switch {
			case m.SharePostID != "":
				act("close-share", "")
			case m.RailMenuID != "":
				clearRailMenuDismiss()
				act("rail-menu", m.RailMenuID)
				restoreRailMenuFocus(m.RailMenuID)
			case m.PickerID != "":
				act("react-pick", m.PickerID)
			case m.MenuID != "":
				act("menu", m.MenuID)
				restoreMessageMenuFocus(m.MenuID)
				e.PreventDefault()
			case m.ShowCreate:
				act("close-create", "")
				restoreChatDialogFocus()
			case m.ShowBrowse:
				act("close-browse", "")
				restoreChatDialogFocus()
			case m.ShowPerson:
				act("close-person", "")
			case m.Search != "" && m.Callbacks.Search != nil:
				m.Callbacks.Search("")
				e.PreventDefault()
			case m.EditingID != "":
				act("cancel-edit", "")
			case m.SidebarOpen:
				act("close-rail", "")
				e.PreventDefault()
			}
		}),
		composerSubmit: ui.UseEvent(func(e ui.FormEvent) { e.PreventDefault(); send() }),
		composerKey: ui.UseEvent(func(e ui.KeyboardEvent) {
			if docSuggestKey(local, e.GetKey(), "chat-composer") {
				e.PreventDefault()
				return
			}
			if mentionKey(e, "chat-composer") {
				return
			}
			if commandHeld(e) && (e.GetKey() == "b" || e.GetKey() == "i") {
				e.PreventDefault()
				kind := "bold"
				if e.GetKey() == "i" {
					kind = "italic"
				}
				applyComposerFormat("chat-composer", kind)
				return
			}
			if e.GetKey() == "Enter" && !shiftHeld(e) {
				e.PreventDefault()
				send()
			}
		}),
		composerInput: ui.UseEvent(func(e ui.InputEvent) {
			drafts.set(m.SelectedID, e.GetValue())
			if m.Callbacks.DraftChanged != nil {
				m.Callbacks.DraftChanged(m.SelectedID, e.GetValue())
			}
			mentionTrack("chat-composer")
			docSuggestTrack(m, local, "chat-composer")
			if local.get().composerNotice != "" {
				local.update(func(u *localUI) { u.composerNotice = "" })
			}
		}),
		searchInput: ui.UseEvent(func(e ui.InputEvent) {
			if m.Callbacks.Search != nil {
				m.Callbacks.Search(e.GetValue())
			}
		}),
		quietToggle: ui.UseEvent(func(e ui.ChangeEvent) {
			savePrefs(func(p *Preferences) { p.QuietHours = e.IsChecked() })
		}),
		quietTZ: ui.UseEvent(func(e ui.ChangeEvent) {
			savePrefs(func(p *Preferences) { p.QuietTimezone = strings.TrimSpace(e.GetValue()) })
		}),
		quietStart: ui.UseEvent(func(e ui.ChangeEvent) {
			savePrefs(func(p *Preferences) { p.QuietStartMinute = parseClock(e.GetValue()) })
		}),
		quietEnd: ui.UseEvent(func(e ui.ChangeEvent) {
			savePrefs(func(p *Preferences) { p.QuietEndMinute = parseClock(e.GetValue()) })
		}),
		notifyMode: ui.UseEvent(func(e ui.ChangeEvent) {
			mode := NotificationMode(e.GetValue())
			savePrefs(func(p *Preferences) {
				if p.Notifications == nil {
					p.Notifications = map[string]NotificationMode{}
				}
				p.Notifications[m.SelectedID] = mode
			})
		}),
		editInput: ui.UseEvent(func(e ui.InputEvent) {
			if m.Callbacks.SetEditDraft != nil && m.EditingID != "" {
				m.Callbacks.SetEditDraft(m.EditingID, e.GetValue())
			}
		}),
		editSubmit: ui.UseEvent(func(e ui.FormEvent) {
			e.PreventDefault()
			if m.Callbacks.EditMessage == nil || m.EditingID == "" {
				return
			}
			body := strings.TrimSpace(domValue("edit-" + m.EditingID))
			if body == "" {
				body = strings.TrimSpace(m.EditDrafts[m.EditingID])
			}
			if body == "" {
				return
			}
			for _, msg := range m.Messages {
				if msg.ID == m.EditingID {
					m.Callbacks.EditMessage(msg.ID, body, msg.Revision)
					return
				}
			}
		}),
		createSubmit: ui.UseEvent(func(e ui.FormEvent) {
			e.PreventDefault()
			if m.Callbacks.CreateConversation == nil {
				return
			}
			name := strings.TrimSpace(domValue("new-chat-name"))
			if name == "" {
				name = strings.TrimSpace(m.NewName)
			}
			if name == "" && createKindOf(m, local.get()) == DirectMessage {
				for _, p := range local.get().picked {
					name = p.Name
				}
			}
			if name == "" {
				return
			}
			st := local.get()
			kind := createKindOf(m, st)
			members := strings.Split(pickedIDs(st.picked), ",")
			if len(st.picked) == 0 {
				members = splitMembers(st.pickQuery)
			}
			m.Callbacks.CreateConversation(kind, name, members)
		}),
		kindPick: ui.UseEvent(func(ui.ChangeEvent) {}),
		pickInput: ui.UseEvent(func(e ui.InputEvent) {
			value := e.GetValue()
			local.update(func(u *localUI) { u.pickQuery, u.pickActive = value, 0 })
		}),
		pickKey: ui.UseEvent(func(e ui.KeyboardEvent) {
			st := local.get()
			people := pickCandidates(m, st.pickQuery, st.picked)
			switch e.GetKey() {
			case "ArrowDown", "ArrowUp":
				if len(people) == 0 {
					return
				}
				delta := 1
				if e.GetKey() == "ArrowUp" {
					delta = -1
				}
				e.PreventDefault()
				local.update(func(u *localUI) { u.pickActive = nextMention(u.pickActive, delta, len(people)) })
			case "Enter":
				if strings.TrimSpace(st.pickQuery) != "" && len(people) > 0 {
					e.PreventDefault()
					pick(people[min(st.pickActive, len(people)-1)].ID)
				}
			case "Backspace":
				if st.pickQuery == "" && len(st.picked) > 0 {
					local.update(func(u *localUI) { u.picked = u.picked[:len(u.picked)-1] })
				}
			}
		}),
		browseFilter: ui.UseEvent(func(e ui.InputEvent) {
			if m.Callbacks.FilterBrowse != nil {
				m.Callbacks.FilterBrowse(e.GetValue())
			}
		}),
		shareFilter: ui.UseEvent(func(e ui.InputEvent) {
			if m.Callbacks.FilterShare != nil {
				m.Callbacks.FilterShare(e.GetValue())
			}
		}),
		memberFilter: ui.UseEvent(func(e ui.InputEvent) {
			if m.Callbacks.FilterMembers != nil {
				m.Callbacks.FilterMembers(e.GetValue())
			}
		}),
		sectionSubmit: ui.UseEvent(func(e ui.FormEvent) {
			e.PreventDefault()
			if m.Callbacks.CreateSection != nil {
				name := strings.TrimSpace(domValue("chat-new-section"))
				if name != "" && len(m.Sections) < 30 {
					m.Callbacks.CreateSection(name)
					closeSectionCreate(true)
				}
			}
		}),
		todoSubmit: ui.UseEvent(func(e ui.FormEvent) {
			e.PreventDefault()
			if m.Callbacks.AddChannelTodo == nil || m.ChannelTodoPending || m.ChannelTodoError != "" {
				return
			}
			text := strings.TrimSpace(domValue("chat-todo-new"))
			if text == "" {
				return
			}
			m.Callbacks.AddChannelTodo(text, todoAuthorizedSourcePin(m))
		}),
		todoDraft: ui.UseEvent(func(e ui.InputEvent) {
			if m.Callbacks.SetChannelTodoDraft != nil {
				m.Callbacks.SetChannelTodoDraft(e.GetValue())
			}
		}),
		todoSource: ui.UseEvent(func(e ui.ChangeEvent) {
			if m.Callbacks.SetChannelTodoSourcePin != nil {
				id := e.GetValue()
				if id != "" {
					valid := false
					for _, pin := range m.ChannelPins {
						if pin.PostID == id {
							valid = true
							break
						}
					}
					if !valid {
						id = ""
					}
				}
				m.Callbacks.SetChannelTodoSourcePin(id)
			}
		}),
		todoPolicyMode: ui.UseEvent(func(e ui.ChangeEvent) {
			_, id, _ := eventAction(e)
			if m.Callbacks.SetChannelTodoPolicy == nil {
				return
			}
			for _, item := range m.ChannelTodo.Items {
				if item.ID == id && item.CanManageCompletionPolicy && !m.ChannelTodoPending && m.ChannelTodoError == "" {
					selected := item.SelectedCompleters
					if e.GetValue() != "ME_AND_SELECTED" {
						selected = nil
					}
					m.Callbacks.SetChannelTodoPolicy(id, e.GetValue(), selected)
					return
				}
			}
		}),
		todoPolicyMember: ui.UseEvent(func(e ui.ChangeEvent) {
			_, id, _ := eventAction(e)
			index, err := strconv.Atoi(e.GetValue())
			if err != nil || index < 0 || index >= len(m.Members) || m.Callbacks.SetChannelTodoPolicy == nil {
				return
			}
			member := m.Members[index]
			for _, item := range m.ChannelTodo.Items {
				if item.ID != id || !item.CanManageCompletionPolicy || item.CompletionMode != "ME_AND_SELECTED" || m.ChannelTodoPending || m.ChannelTodoError != "" || len(item.SelectedCompleters) >= 20 {
					continue
				}
				for _, selected := range item.SelectedCompleters {
					if selected.HomeTenantID == member.HomeTenantID && selected.SubjectID == member.ID {
						return
					}
				}
				next := append(append([]ChannelTodoSelectedMember(nil), item.SelectedCompleters...), ChannelTodoSelectedMember{HomeTenantID: member.HomeTenantID, SubjectID: member.ID})
				setDOMValue("todo-member-"+id, "")
				m.Callbacks.SetChannelTodoPolicy(id, item.CompletionMode, next)
				return
			}
		}),
		todoNewMode: ui.UseEvent(func(e ui.ChangeEvent) {
			if m.Callbacks.SetChannelTodoNewPolicy == nil || m.ChannelTodoPending || m.ChannelTodoError != "" {
				return
			}
			selected := m.ChannelTodoNewSelected
			if e.GetValue() != "ME_AND_SELECTED" {
				selected = nil
			}
			m.Callbacks.SetChannelTodoNewPolicy(e.GetValue(), selected)
		}),
		todoNewMember: ui.UseEvent(func(e ui.ChangeEvent) {
			index, err := strconv.Atoi(e.GetValue())
			if err != nil || index < 0 || index >= len(m.Members) || m.Callbacks.SetChannelTodoNewPolicy == nil || m.ChannelTodoPending || m.ChannelTodoError != "" || len(m.ChannelTodoNewSelected) >= 20 {
				return
			}
			member := m.Members[index]
			if member.ID == m.CurrentUser && member.HomeTenantID == m.CurrentTenantID {
				return
			}
			for _, selected := range m.ChannelTodoNewSelected {
				if selected.HomeTenantID == member.HomeTenantID && selected.SubjectID == member.ID {
					return
				}
			}
			next := append(append([]ChannelTodoSelectedMember(nil), m.ChannelTodoNewSelected...), ChannelTodoSelectedMember{HomeTenantID: member.HomeTenantID, SubjectID: member.ID})
			setDOMValue("chat-todo-new-member", "")
			m.Callbacks.SetChannelTodoNewPolicy("ME_AND_SELECTED", next)
		}),
		teamPurposeSubmit: ui.UseEvent(func(e ui.FormEvent) {
			e.PreventDefault()
			if m.Callbacks.SetChannelTeamPurpose != nil && !m.ChannelWidgetsPending {
				m.Callbacks.SetChannelTeamPurpose(strings.TrimSpace(domValue("channel-team-purpose")))
			}
		}),
		projectDetailsSubmit: ui.UseEvent(func(e ui.FormEvent) {
			e.PreventDefault()
			if m.Callbacks.SetChannelProjectDetails != nil && !m.ChannelWidgetsPending {
				m.Callbacks.SetChannelProjectDetails(strings.TrimSpace(domValue("channel-project-title")), strings.TrimSpace(domValue("channel-project-summary")))
			}
		}),
		milestoneSubmit: ui.UseEvent(func(e ui.FormEvent) {
			e.PreventDefault()
			if m.Callbacks.AddChannelProjectMilestone != nil && !m.ChannelWidgetsPending {
				ownerHome, ownerSubject := widgetOwnerIdentity(m, domValue("channel-milestone-new-owner"))
				m.Callbacks.AddChannelProjectMilestone(ChannelProjectMilestone{Text: strings.TrimSpace(domValue("channel-milestone-new")), Status: domValue("channel-milestone-new-status"), OwnerHomeTenantID: ownerHome, OwnerSubjectID: ownerSubject, DueDate: domValue("channel-milestone-new-date")})
			}
		}),
		pollCreate: ui.UseEvent(func(e ui.FormEvent) {
			e.PreventDefault()
			if m.Callbacks.CreateChannelPoll == nil || m.ChannelPollPending || m.ChannelPollLoading || m.ChannelPollError != "" {
				return
			}
			question := strings.TrimSpace(domValue("channel-poll-question"))
			options := pollOptions(domValue("channel-poll-options"))
			if question != "" {
				m.Callbacks.CreateChannelPoll(question, options)
			}
		}),
		threadSubmit: ui.UseEvent(func(e ui.FormEvent) { e.PreventDefault(); threadSend() }),
		threadInput:  ui.UseEvent(func(ui.InputEvent) { mentionTrack("thread-composer") }),
		threadKey: ui.UseEvent(func(e ui.KeyboardEvent) {
			if mentionKey(e, "thread-composer") {
				return
			}
			if commandHeld(e) && (e.GetKey() == "b" || e.GetKey() == "i") {
				e.PreventDefault()
				kind := "bold"
				if e.GetKey() == "i" {
					kind = "italic"
				}
				applyComposerFormat("thread-composer", kind)
				return
			}
			if e.GetKey() == "Enter" && !shiftHeld(e) {
				e.PreventDefault()
				threadSend()
			}
		}),
	}
}

func replyInThread(m Model) {
	body := strings.TrimSpace(domValue("thread-composer"))
	if body == "" || m.ThreadParentID == "" || m.Callbacks.ReplyInThread == nil {
		return
	}
	m.Callbacks.ReplyInThread(m.ThreadParentID, body)
	setDOMValue("thread-composer", "")
}

func copyMap[K comparable, V any](in map[K]V) map[K]V {
	out := make(map[K]V, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// actWith is the dispatch for controls that carry an emoji beside the id.
// Anything else falls through to act. An action taken from an open menu
// closes the menu; a picked reaction closes the picker.
func (m Model) actWith(action, id, extra string) {
	cb := m.Callbacks
	switch action {
	case "open-search-message":
		if cb.OpenSearchMessage != nil {
			for _, hit := range m.SearchMessages {
				if hit.ConversationID == id && hit.Message.ID == extra {
					cb.OpenSearchMessage(hit.ConversationID, hit.Message.ID, hit.Message.Sequence)
					return
				}
			}
		}
		return
	case "todo-policy-remove":
		index, err := strconv.Atoi(extra)
		if err != nil || cb.SetChannelTodoPolicy == nil || m.ChannelTodoPending || m.ChannelTodoError != "" {
			return
		}
		for _, item := range m.ChannelTodo.Items {
			if item.ID != id || !item.CanManageCompletionPolicy || item.CompletionMode != "ME_AND_SELECTED" || index < 0 || index >= len(item.SelectedCompleters) {
				continue
			}
			next := append([]ChannelTodoSelectedMember(nil), item.SelectedCompleters[:index]...)
			next = append(next, item.SelectedCompleters[index+1:]...)
			cb.SetChannelTodoPolicy(id, item.CompletionMode, next)
			return
		}
		return
	case "todo-new-policy-remove":
		index, err := strconv.Atoi(extra)
		if err != nil || cb.SetChannelTodoNewPolicy == nil || m.ChannelTodoPending || m.ChannelTodoError != "" || index < 0 || index >= len(m.ChannelTodoNewSelected) {
			return
		}
		next := append([]ChannelTodoSelectedMember(nil), m.ChannelTodoNewSelected[:index]...)
		next = append(next, m.ChannelTodoNewSelected[index+1:]...)
		cb.SetChannelTodoNewPolicy("ME_AND_SELECTED", next)
		return
	case "poll-vote":
		if cb.VoteChannelPoll != nil && !m.ChannelPollPending && !m.ChannelPollLoading && m.ChannelPollError == "" {
			for _, option := range m.ChannelPoll.Options {
				if option.ID == id {
					cb.VoteChannelPoll(id)
					break
				}
			}
		}
		return
	case "download-attachment":
		if cb.DownloadAttachment != nil && id != "" && extra != "" {
			cb.DownloadAttachment(id, extra)
		}
		return
	case "rail-move-section":
		if cb.MoveConversationSection != nil {
			cb.MoveConversationSection(id, extra)
		}
		if m.RailMenuID != "" && cb.OpenRailMenu != nil {
			clearRailMenuDismiss()
			cb.OpenRailMenu("")
		}
		return
	case "react-with":
		if cb.ReactWith != nil && extra != "" {
			cb.ReactWith(id, extra)
		}
		if cb.OpenPicker != nil {
			cb.OpenPicker("")
		}
		return
	case "toggle-reaction":
		mine := false
		for _, msg := range m.Messages {
			if msg.ID != id {
				continue
			}
			for _, chip := range msg.Chips {
				if chip.Emoji == extra {
					mine = chip.Mine
				}
			}
		}
		switch {
		case mine && cb.RemoveReactionWith != nil:
			cb.RemoveReactionWith(id, extra)
		case cb.ReactWith != nil && extra != "":
			cb.ReactWith(id, extra)
		}
		return
	}
	m.act(action, id)
	if action != "rail-menu" && m.RailMenuID != "" && cb.OpenRailMenu != nil {
		clearRailMenuDismiss()
		cb.OpenRailMenu("")
		if action != "rail-details" {
			restoreRailMenuFocus(m.RailMenuID)
		}
	}
	if action != "menu" && m.MenuID != "" && cb.OpenMenu != nil {
		cb.OpenMenu("")
	}
}

// act is the single dispatch point for every delegated control.
func (m Model) act(action, id string) {
	cb := m.Callbacks
	call := func(fn func()) {
		if fn != nil {
			fn()
		}
	}
	switch action {
	case "rail-menu":
		if cb.OpenRailMenu != nil {
			if m.RailMenuID == id {
				clearRailMenuDismiss()
				cb.OpenRailMenu("")
			} else {
				cb.OpenRailMenu(id)
			}
		}
	case "rail-details":
		if cb.OpenConversationDetails != nil {
			cb.OpenConversationDetails(id)
		}
	case "rail-copy-reference":
		if cb.CopyConversationReference != nil {
			for _, conversation := range m.Conversations {
				if conversation.ID == id {
					cb.CopyConversationReference(id, conversationReferenceLabel(m, conversation))
					break
				}
			}
		}
	case "rail-copy-api-curl":
		if cb.CopyConversationAPICurl != nil {
			for _, conversation := range m.Conversations {
				if conversation.ID == id {
					cb.CopyConversationAPICurl(id)
					break
				}
			}
		}
	case "rail-chat-up":
		if cb.MoveConversationOrder != nil {
			cb.MoveConversationOrder(id, -1)
		}
	case "rail-chat-down":
		if cb.MoveConversationOrder != nil {
			cb.MoveConversationOrder(id, 1)
		}
	case "rail-notify-all", "rail-notify-mentions", "rail-notify-mute":
		if cb.SetConversationNotification != nil {
			mode := NotifyAll
			if action == "rail-notify-mentions" {
				mode = NotifyMention
			}
			if action == "rail-notify-mute" {
				mode = NotifyMute
			}
			cb.SetConversationNotification(id, mode)
		}
	case "select":
		if strings.TrimSpace(m.Search) != "" && cb.Search != nil {
			cb.Search("")
		}
		if cb.SelectConversation != nil {
			cb.SelectConversation(id)
		}
		if cb.ToggleSidebar != nil && m.SidebarOpen {
			cb.ToggleSidebar(false)
		}
	case "search-more":
		call(cb.SearchMore)
	case "search-more-channels":
		call(cb.SearchMoreChannels)
	case "browse-search-channel":
		if cb.OpenSearchChannel != nil {
			cb.OpenSearchChannel(id)
		}
	case "open-channel-reference":
		if cb.SelectConversation != nil {
			for _, conversation := range m.Conversations {
				if conversation.ID == id && conversation.Joined {
					cb.SelectConversation(id)
					break
				}
			}
		}
	case "toggle-section":
		if cb.ToggleSection != nil {
			cb.ToggleSection(id)
		}
	case "section-up":
		if cb.ReorderSection != nil {
			cb.ReorderSection(id, -1)
		}
	case "section-down":
		if cb.ReorderSection != nil {
			cb.ReorderSection(id, 1)
		}
	case "section-remove":
		if cb.RemoveSection != nil {
			cb.RemoveSection(id)
		}
	case "open-section-create":
		focusSectionCreate()
	case "cancel-section-create":
		closeSectionCreate(true)
	case "open-create":
		call(cb.OpenCreate)
	case "close-create":
		call(cb.CloseCreate)
	case "open-browse":
		call(cb.OpenBrowse)
	case "close-browse":
		call(cb.CloseBrowse)
	case "browse-to-create":
		call(cb.CloseBrowse)
		call(cb.OpenCreate)
	case "join":
		if cb.RequestJoinConversation != nil {
			cb.RequestJoinConversation(id)
		} else if cb.JoinConversation != nil {
			cb.JoinConversation(id)
		}
	case "join-confirm":
		if cb.JoinConversation != nil {
			cb.JoinConversation(id)
		}
	case "join-dismiss":
		if cb.DismissJoinPrompt != nil {
			cb.DismissJoinPrompt()
		}
	case "open-rail":
		if cb.ToggleSidebar != nil {
			cb.ToggleSidebar(true)
		}
	case "close-rail":
		if cb.ToggleSidebar != nil {
			cb.ToggleSidebar(false)
		}
	case "details":
		if cb.ToggleDetails != nil {
			cb.ToggleDetails(!m.ShowDetails)
		}
	case "open-person":
		if strings.TrimSpace(m.Search) != "" && cb.Search != nil {
			cb.Search("")
		}
		if cb.OpenPerson != nil && id != "" {
			cb.OpenPerson(id)
		}
	case "close-person":
		call(cb.ClosePerson)
	case "start-direct-message":
		if cb.StartDirectMessage != nil && id != "" {
			cb.StartDirectMessage(id)
		}
	case "close-details":
		if cb.ToggleDetails != nil {
			cb.ToggleDetails(false)
		}
	case "open-todo":
		if cb.OpenChannelTodo != nil {
			cb.OpenChannelTodo()
		}
	case "open-poll":
		if cb.OpenChannelPoll != nil {
			cb.OpenChannelPoll(id)
		}
	case "poll-retry":
		call(cb.RetryChannelPoll)
	case "refresh-members":
		call(cb.LoadMembers)
	case "load-older":
		call(cb.LoadOlder)
	case "load-newer":
		call(cb.LoadNewer)
	case "load-older-thread":
		call(cb.LoadOlderThread)
	case "load-newer-thread":
		call(cb.LoadNewerThread)
	case "retry":
		call(cb.Retry)
	case "reply", "stats":
		if cb.OpenThread != nil {
			cb.OpenThread(id)
		}
	case "close-thread":
		call(cb.CloseThread)
	case "follow":
		if cb.SetThreadFollow != nil {
			cb.SetThreadFollow(!m.ThreadFollowed)
		}
	case "react-pick":
		switch {
		case cb.OpenPicker != nil && m.PickerID == id:
			cb.OpenPicker("")
		case cb.OpenPicker != nil:
			cb.OpenPicker(id)
		case cb.React != nil:
			cb.React(id)
		}
	case "menu":
		if cb.OpenMenu != nil {
			if m.MenuID == id {
				cb.OpenMenu("")
			} else {
				cb.OpenMenu(id)
			}
		}
	case "copy-link":
		if cb.CopyLink != nil && !m.PinReferenceUnavailable {
			cb.CopyLink(id)
		}
	case "copy-contents":
		if cb.CopyContents != nil {
			cb.CopyContents(id)
		}
	case "open-share":
		if cb.OpenShare != nil {
			cb.OpenShare(id)
			focusShareDialog()
		}
	case "close-share":
		if !m.SharePending {
			call(cb.CloseShare)
			restoreShareFocus()
		}
	case "share-destination":
		if cb.SelectShareDestination != nil {
			cb.SelectShareDestination(id)
		}
	case "share-submit":
		call(cb.ShareMessage)
	case "open-embed":
		if cb.OpenEmbeddedMessage != nil {
			cb.OpenEmbeddedMessage(id)
		}
	case "jump-newest":
		BeginScrollToNewest()
		call(cb.JumpToNewest)
	case "react":
		if cb.React != nil {
			cb.React(id)
		}
	case "unreact":
		if cb.RemoveReaction != nil {
			cb.RemoveReaction(id)
		} else if cb.React != nil {
			cb.React(id)
		}
	case "pin":
		if cb.Pin != nil {
			cb.Pin(id)
		}
	case "unpin":
		if cb.Unpin != nil {
			cb.Unpin(id)
		} else if cb.Pin != nil {
			cb.Pin(id)
		}
	case "pin-jump":
		if cb.JumpToPin != nil {
			for _, pin := range m.ChannelPins {
				if pin.PostID == id {
					cb.JumpToPin(id, pin.Sequence)
					break
				}
			}
		}
	case "pin-copy":
		if cb.CopyPinReference != nil && !m.PinReferenceUnavailable {
			cb.CopyPinReference(id)
		}
	case "todo-toggle":
		if cb.SetChannelTodoCompleted != nil && !m.ChannelTodoPending && m.ChannelTodoError == "" {
			for _, item := range m.ChannelTodo.Items {
				if item.ID == id && todoMayToggle(item) {
					cb.SetChannelTodoCompleted(id, !item.Completed)
					break
				}
			}
		}
	case "todo-delete":
		if cb.DeleteChannelTodo != nil && !m.ChannelTodoPending && m.ChannelTodoError == "" {
			cb.DeleteChannelTodo(id)
		}
	case "todo-pin":
		if cb.SetChannelTodoPinned != nil && !m.ChannelTodoPending && m.ChannelTodoError == "" {
			cb.SetChannelTodoPinned(!m.ChannelTodo.Pinned)
		}
	case "todo-retry":
		call(cb.RetryChannelTodo)
	case "widget-retry":
		call(cb.RetryChannelWidgets)
	case "team-pin", "project-pin":
		if cb.SetChannelWidgetPinned != nil && !m.ChannelWidgetsPending && m.ChannelWidgetsError == "" {
			if action == "team-pin" && m.ChannelTeam.CanPin {
				cb.SetChannelWidgetPinned("TEAM", !m.ChannelTeam.Pinned)
			}
			if action == "project-pin" && m.ChannelProject.CanPin {
				cb.SetChannelWidgetPinned("PROJECT", !m.ChannelProject.Pinned)
			}
		}
	case "team-role-save":
		if cb.SetChannelTeamRoleLabel != nil && !m.ChannelWidgetsPending {
			if i, err := strconv.Atoi(id); err == nil && i >= 0 && i < len(m.ChannelTeam.Members) {
				member := m.ChannelTeam.Members[i]
				cb.SetChannelTeamRoleLabel(member.HomeTenantID, member.SubjectID, strings.TrimSpace(domValue("channel-team-role-"+id)))
			}
		}
	case "milestone-save":
		if cb.UpdateChannelProjectMilestone != nil && !m.ChannelWidgetsPending {
			for i, milestone := range m.ChannelProject.Milestones {
				if milestone.ID == id {
					ownerHome, ownerSubject := widgetOwnerIdentity(m, domValue("channel-milestone-owner-"+strconv.Itoa(i)))
					cb.UpdateChannelProjectMilestone(ChannelProjectMilestone{ID: id, Text: strings.TrimSpace(domValue("channel-milestone-text-" + strconv.Itoa(i))), Status: domValue("channel-milestone-status-" + strconv.Itoa(i)), OwnerHomeTenantID: ownerHome, OwnerSubjectID: ownerSubject, DueDate: domValue("channel-milestone-date-" + strconv.Itoa(i))})
					break
				}
			}
		}
	case "milestone-delete":
		if cb.DeleteChannelProjectMilestone != nil && !m.ChannelWidgetsPending {
			cb.DeleteChannelProjectMilestone(id)
		}
	case "edit":
		if cb.BeginEdit != nil {
			cb.BeginEdit(id)
		}
	case "cancel-edit":
		call(cb.CancelEdit)
	case "delete":
		if cb.DeleteMessage != nil {
			for _, msg := range m.Messages {
				if msg.ID == id {
					cb.DeleteMessage(id, msg.Revision)
					return
				}
			}
		}
	case "dismiss-notice":
		call(cb.DismissNotice)
	}
}

// actionButton is a plain button routed through the delegated click hook.
func actionButton(class, action, id, label string, disabled bool, children ...ui.Node) ui.Node {
	data := map[string]string{"action": action}
	if id != "" {
		data["id"] = id
	}
	return html.Button(html.Props{Class: class, Type: "button", Disabled: disabled, Data: data, Aria: map[string]string{"label": label}, Title: label}, children...)
}

func personButton(m Model, id, name, class string, children ...ui.Node) ui.Node {
	return html.Button(html.Props{Class: class, Type: "button", Disabled: id == "" || m.Callbacks.OpenPerson == nil,
		Data: map[string]string{"action": "open-person", "id": id},
		Aria: map[string]string{"label": m.tf(KeyViewPerson, map[string]string{"name": name})}}, children...)
}

var paneResizeOwner *uint64

func Workspace(model Model) ui.Node {
	owner := ui.UseRef(new(uint64)).Get()
	giphy := ui.UseRef(newGiphyPickerViews()).Get()
	drafts := ui.UseRef(&browserDrafts{}).Get()
	drafts.prepare(&model)
	mention := mentionStore{box: ui.UseRef(&mentionBox{}).Get(), tick: ui.UseState(uint64(0))}
	local := localStore{box: ui.UseRef(&localUI{}).Get(), tick: ui.UseState(uint64(0))}
	local.forRoom(model.SelectedID)
	paneResizeOwner = owner
	ui.UseEffectOf(func() func() {
		return func() {
			if paneResizeOwner == owner {
				paneResizeCommit = nil
				clearChatScrollMemory()
				clearMobileRailFocus()
				clearMessageMenuGeometry()
				drafts.releaseMemory()
				giphy.closeAll()
			}
		}
	}, struct{}{})
	clearDraftModel := drafts.takeClearModel()
	ui.UseEffectOf(func() func() {
		if clearDraftModel != nil {
			clearDraftModel.ClearDrafts()
		}
		return nil
	}, struct{ pending bool }{clearDraftModel != nil})
	ui.UseEffectOf(func() func() {
		return installDraftLogoutListener(func() {
			drafts.clear()
			model.ClearDrafts()
		})
	}, struct{ tenant, principal string }{model.CurrentTenantID, model.CurrentUser})
	ui.UseLayoutEffect(func() func() { return startChatImageLoading() }, struct{ room, principal string }{model.SelectedID, model.CurrentUser})
	ui.UseEffectOf(func() func() { revealThreadParent(model.ShowThread, model.ThreadParentID); return nil }, struct {
		open bool
		id   string
	}{model.ShowThread, model.ThreadParentID})
	ui.UseEffectOf(func() func() { return startChatGiphyPostEmbeds(model.GiphyAPIKey, model.SelectedID, model.CurrentUser) }, struct{ room, principal, key string }{model.SelectedID, model.CurrentUser, model.GiphyAPIKey})
	installFieldSync()
	installComposerPin()
	personID := ""
	if model.PersonDetails != nil {
		personID = model.PersonDetails.ID
	}
	syncPersonFocusFor(model.ShowPerson, personID)
	syncMobileRailFocus(model.SidebarOpen)
	syncOpenMessageMenu(model.MenuID)
	syncImageViewer(model.SelectedID, model.CurrentUser)
	installPaneResize()
	paneResizeCommit = func(pane string, px int) {
		switch pane {
		case "details":
			if model.Callbacks.ResizeDetails != nil {
				model.Callbacks.ResizeDetails(px)
			}
		default:
			if model.Callbacks.ResizeRail != nil {
				model.Callbacks.ResizeRail(px)
			}
		}
	}
	h := bindHandlers(model, giphy, mention, local, drafts)
	if model.Locale == "" {
		model.Locale = "en-US"
	}
	if model.Direction == "" {
		model.Direction = direction(model.Locale)
	}
	model.mentions, model.mentionsReady = mentionIndex(model), true
	// Search results take the whole main column; a thread or details pane from
	// the room behind them would describe something the reader cannot see.
	sideOpen := (model.ShowPerson || model.ShowThread || model.ShowDetails) && strings.TrimSpace(model.Search) == ""
	data := map[string]string{
		"chat-state": string(model.State), "chat-personal-state": "true",
		"selected-id":  model.SelectedID,
		"sidebar-open": boolString(model.SidebarOpen), "details-open": boolString(sideOpen),
		"thread-open": boolString(model.ShowThread),
	}
	if model.CurrentUser != "" {
		data["principal"] = model.CurrentUser
	}
	children := []ui.Node{
		skipLink(model),
		html.Div(html.Props{Class: "chat-layout", Aria: map[string]string{"hidden": boolString(model.SharePostID != "")}}, rail(model, h), timeline(model, h), sideColumn(model, h)),
	}
	if model.SidebarOpen {
		children = append(children, actionButton("chat-scrim", "close-rail", "", model.t(KeyCloseConversations), model.Callbacks.ToggleSidebar == nil))
	}
	if model.ShowCreate {
		children = append(children, createDialog(model, h))
	}
	if model.ShowBrowse {
		children = append(children, browseDialog(model, h))
	}
	if model.JoinPromptID != "" {
		children = append(children, joinChannelDialog(model))
	}
	if model.SharePostID != "" {
		children = append(children, shareDialog(model, h))
	}
	return html.Div(html.Props{Class: "chat-workspace", Dir: model.Direction, Lang: model.Locale, Data: withPaneData(data, model.Pane), OnClick: h.rootClick, OnKeyDown: h.rootKey}, children...)
}

// withPaneData carries the viewer's pane sizes as data attributes: the
// shell's CSP forbids inline style attributes, so the stylesheet maps a few
// named widths instead of reading a custom property from the element.
func withPaneData(data map[string]string, p PaneSizes) map[string]string {
	if p.Rail > 0 {
		data["rail-width"] = itoa(p.Rail)
	}
	if p.Details > 0 {
		data["details-width"] = itoa(p.Details)
	}
	return data
}

func skipLink(m Model) ui.Node {
	return html.A(html.Props{Class: "chat-skip", Href: "#chat-main"}, ui.Text(m.t(KeySkip)))
}

func notice(m Model) ui.Node {
	children := []ui.Node{html.Span(html.Props{Text: m.Notice})}
	if m.NoticeRetry {
		children = append(children, html.Button(html.Props{Class: "button secondary small", Type: "button", Disabled: m.Callbacks.Retry == nil, Data: map[string]string{"action": "retry"}, Text: m.t(KeyRefresh)}))
	}
	children = append(children, actionButton("icon-button", "dismiss-notice", "", m.t(KeyClose), m.Callbacks.DismissNotice == nil, icon("close")))
	return html.Div(html.Props{Class: "chat-notice", Role: "status", Aria: map[string]string{"live": "polite"}}, children...)
}

// --- rail -----------------------------------------------------------------

func rail(m Model, h handlers) ui.Node {
	sections := m.Sections
	if len(sections) == 0 && len(m.Conversations) > 0 {
		sections = defaultSections(m)
	}
	head := html.Div(html.Props{Class: "rail-head"},
		html.H2(html.Props{Class: "rail-title", Text: m.t(KeyRailTitle)}),
		html.Div(html.Props{Class: "rail-head-actions"},
			actionButton("icon-button", "open-create", "", m.t(KeyNewConversation), m.Callbacks.OpenCreate == nil, icon("compose")),
			actionButton("icon-button mobile-chat-close", "close-rail", "", m.t(KeyCloseConversations), m.Callbacks.ToggleSidebar == nil, icon("close")),
		),
	)
	search := html.Div(html.Props{Class: "rail-search"},
		html.Label(html.Props{Class: "sr-only", For: "chat-search"}, ui.Text(m.t(KeySearch))),
		icon("search"),
		html.Input(html.Props{ID: "chat-search", Class: "chat-search", Type: "search", Placeholder: m.t(KeySearchPlaceholder), Data: map[string]string{"chat-value": m.Search}, AutoComplete: "off", OnInput: h.searchInput, Aria: map[string]string{"controls": "chat-main"}}),
	)
	list := []ui.Node{}
	for _, section := range sections {
		list = append(list, html.WithKey(railSection(m, section), "section:"+section.ID))
	}
	list = append(list, html.WithKey(html.Details(html.Props{Class: "section-create", ID: "chat-section-create"},
		html.Summary(html.Props{Class: "section-create-trigger", Data: map[string]string{"action": "open-section-create"}}, icon("plus"), html.Span(html.Props{Text: m.t(KeyNewSection)})),
		html.Form(html.Props{Class: "section-create-form", OnSubmit: h.sectionSubmit},
			html.Label(html.Props{For: "chat-new-section"}, ui.Text(m.t(KeySectionName))),
			html.Input(html.Props{ID: "chat-new-section", Class: "chat-input", Type: "text", MaxLength: 80, Placeholder: m.t(KeySectionName), Required: true, AutoComplete: "off"}),
			html.Div(html.Props{Class: "section-create-actions"},
				html.Button(html.Props{Class: "button secondary small", Type: "button", Data: map[string]string{"action": "cancel-section-create"}, Text: m.t(KeyCancel)}),
				html.Button(html.Props{Class: "button small", Type: "submit", Disabled: m.Callbacks.CreateSection == nil || len(sections) >= 30, Text: m.t(KeyCreate)})))), "section-create"))
	if len(sections) == 0 {
		list = append(list, html.WithKey(railEmpty(m), "empty"))
	}
	// Browse channels stays pinned under the list: it is how a reader finds
	// the rooms they are not in, and it was the first thing a short window
	// pushed out of reach.
	browse := html.Div(html.Props{Class: "rail-footer"},
		html.Button(html.Props{Class: "rail-link", Type: "button", Disabled: m.Callbacks.OpenBrowse == nil, Data: map[string]string{"action": "open-browse"}}, icon("browse"), html.Span(html.Props{Text: m.t(KeyBrowse)})))
	return html.Nav(html.Props{Class: "chat-rail chat-sidebar", Role: "navigation", Aria: map[string]string{"label": m.t(KeyNav)}},
		head, search,
		html.Div(html.Props{Class: "rail-scroll"}, list...),
		railMenu(m),
		browse,
		railPreferences(m, h),
		paneHandle(m, "rail"),
	)
}

func defaultSections(m Model) []SidebarSection {
	channels := SidebarSection{ID: "channels", Name: m.t(KeySectionChannels)}
	direct := SidebarSection{ID: "direct", Name: m.t(KeySectionDirect)}
	for _, c := range m.Conversations {
		if c.Kind == DirectMessage || c.Kind == GroupChat {
			direct.Chats = append(direct.Chats, c)
		} else {
			channels.Chats = append(channels.Chats, c)
		}
	}
	// Both groups always render: the empty-state copy promises direct
	// messages, so the rail shows where they will appear.
	return []SidebarSection{channels, direct}
}

func railEmpty(m Model) ui.Node {
	if m.State == StateLoading {
		return html.Div(html.Props{Class: "rail-skeleton", Aria: map[string]string{"hidden": "true"}},
			html.Span(html.Props{}), html.Span(html.Props{}), html.Span(html.Props{}), html.Span(html.Props{}))
	}
	return html.P(html.Props{Class: "rail-empty", Text: m.t(KeyRailEmpty)})
}

func railSection(m Model, section SidebarSection) ui.Node {
	name := section.Name
	if section.ID == "channels" {
		name = m.t(KeySectionChannels)
	}
	if section.ID == "direct" {
		name = m.t(KeySectionDirect)
	}
	chevron := "chevron-down"
	if section.Collapsed {
		chevron = "chevron-right"
	}
	controls := []ui.Node{
		html.Button(html.Props{Class: "section-title", Type: "button", Disabled: m.Callbacks.ToggleSection == nil, Data: map[string]string{"action": "toggle-section", "id": section.ID}, Aria: map[string]string{"expanded": boolString(!section.Collapsed)}}, icon(chevron), html.Span(html.Props{Text: name}), html.Span(html.Props{Class: "section-count", Text: m.n(len(section.Chats))})),
		actionButton("section-order", "section-up", section.ID, m.t(KeyMoveSectionUp), m.Callbacks.ReorderSection == nil, icon("arrow-up")),
		actionButton("section-order", "section-down", section.ID, m.t(KeyMoveSectionDown), m.Callbacks.ReorderSection == nil, icon("arrow-down")),
	}
	if section.ID != "channels" && section.ID != "direct" {
		controls = append(controls, actionButton("section-order", "section-remove", section.ID, m.t(KeyRemoveSection), m.Callbacks.RemoveSection == nil, icon("close")))
	}
	items := []ui.Node{html.WithKey(html.Div(html.Props{Class: "section-controls"}, controls...), "controls")}
	if !section.Collapsed {
		for _, c := range section.Chats {
			items = append(items, html.WithKey(railRow(m, c), "conversation:"+c.ID))
		}
		if len(section.Chats) == 0 && section.ID == "direct" {
			items = append(items, html.WithKey(html.P(html.Props{Class: "rail-empty", Text: m.t(KeyDMEmpty)}), "empty"))
		}
		if section.ID == "channels" {
			items = append(items, html.WithKey(html.Button(html.Props{Class: "chat-row rail-add", Type: "button", Disabled: m.Callbacks.OpenBrowse == nil, Data: map[string]string{"action": "open-browse"}},
				html.Span(html.Props{Class: "kind-glyph", Aria: map[string]string{"hidden": "true"}}, icon("plus")), html.Span(html.Props{Class: "chat-row-name", Text: m.t(KeyAddChannels)})), "add-channels"))
		}
	}
	return html.Div(html.Props{Class: "sidebar-section", Data: map[string]string{"section-id": section.ID}}, items...)
}

func railRow(m Model, c Conversation) ui.Node {
	class := "chat-row"
	if c.ID == m.SelectedID {
		class += " selected"
	}
	if c.Unread > 0 {
		class += " unread"
	}
	if c.Muted {
		class += " muted"
	}
	label := displayName(m, c)
	if label == "" {
		label = m.t(KeyConversation)
	}
	children := []ui.Node{kindGlyph(m, c, m.t(kindKey(c.Kind))), html.Span(html.Props{Class: "chat-row-name", Text: label})}
	if c.Mentions > 0 {
		children = append(children, html.Span(html.Props{Class: "chat-badge mention", Text: m.n(c.Mentions), Aria: map[string]string{"label": m.tf(KeyMentionCount, map[string]string{"n": m.n(c.Mentions)})}}))
	} else if c.Unread > 0 {
		children = append(children, html.Span(html.Props{Class: "chat-badge", Text: m.n(c.Unread), Aria: map[string]string{"label": m.tf(KeyUnreadCount, map[string]string{"n": m.n(c.Unread)})}}))
	}
	return html.Div(html.Props{Class: "chat-rail-row", Data: map[string]string{"conversation-id": c.ID}},
		html.Button(html.Props{Class: class, Type: "button", Disabled: m.Callbacks.SelectConversation == nil, Data: map[string]string{"action": "select", "id": c.ID}, Aria: map[string]string{"current": boolString(c.ID == m.SelectedID)}}, children...),
		html.Button(html.Props{Class: "rail-row-more", Type: "button", Disabled: m.Callbacks.OpenRailMenu == nil, Data: map[string]string{"action": "rail-menu", "id": c.ID}, Aria: map[string]string{"label": m.tf(KeyConversationMore, map[string]string{"name": label}), "haspopup": "menu", "expanded": boolString(m.RailMenuID == c.ID)}, Title: m.t(KeyMore)}, icon("more-vertical")),
	)
}

func railMenu(m Model) ui.Node {
	if m.RailMenuID == "" {
		return nil
	}
	mode := m.Preferences.Notifications[m.RailMenuID]
	name := m.t(KeyConversation)
	found := false
	for _, conversation := range m.Conversations {
		if conversation.ID == m.RailMenuID {
			name = displayName(m, conversation)
			found = true
			break
		}
	}
	items := []ui.Node{html.Button(html.Props{Class: "menu-item", Type: "button", Role: "menuitem", Disabled: m.Callbacks.OpenConversationDetails == nil, Data: map[string]string{"action": "rail-details", "id": m.RailMenuID}, Text: m.t(KeyDetails)})}
	items = append(items, html.Button(html.Props{Class: "menu-item", Type: "button", Role: "menuitem", Disabled: m.Callbacks.CopyConversationReference == nil || !found, Data: map[string]string{"action": "rail-copy-reference", "id": m.RailMenuID}}, icon("link"), html.Span(html.Props{Text: m.t(KeyCopyConversationReference)})))
	items = append(items, html.Button(html.Props{Class: "menu-item", Type: "button", Role: "menuitem", Disabled: m.Callbacks.CopyConversationAPICurl == nil || !found, Data: map[string]string{"action": "rail-copy-api-curl", "id": m.RailMenuID}}, icon("copy"), html.Span(html.Props{Text: m.t(KeyCopyConversationAPICurl)})))
	sections := m.Sections
	if len(sections) == 0 {
		sections = defaultSections(m)
	}
	chatPosition, chatCount := -1, 0
	for _, section := range sections {
		for index, conversation := range section.Chats {
			if conversation.ID == m.RailMenuID {
				chatPosition, chatCount = index, len(section.Chats)
				break
			}
		}
		if chatPosition >= 0 {
			break
		}
	}
	items = append(items,
		html.Button(html.Props{Class: "menu-item", Type: "button", Role: "menuitem", Disabled: m.Callbacks.MoveConversationOrder == nil || chatPosition <= 0, Data: map[string]string{"action": "rail-chat-up", "id": m.RailMenuID}, Text: m.t(KeyMoveConversationUp)}),
		html.Button(html.Props{Class: "menu-item", Type: "button", Role: "menuitem", Disabled: m.Callbacks.MoveConversationOrder == nil || chatPosition < 0 || chatPosition >= chatCount-1, Data: map[string]string{"action": "rail-chat-down", "id": m.RailMenuID}, Text: m.t(KeyMoveConversationDown)}),
	)
	for _, item := range []struct {
		action string
		mode   NotificationMode
		key    string
	}{{"rail-notify-all", NotifyAll, KeyNotifyAll}, {"rail-notify-mentions", NotifyMention, KeyNotifyMentions}, {"rail-notify-mute", NotifyMute, KeyNotifyMute}} {
		check := ""
		if mode == item.mode {
			check = "✓"
		}
		items = append(items, html.Button(html.Props{Class: "menu-item", Type: "button", Role: "menuitemradio", Disabled: m.Callbacks.SetConversationNotification == nil, Data: map[string]string{"action": item.action, "id": m.RailMenuID}, Aria: map[string]string{"checked": boolString(mode == item.mode)}}, html.Span(html.Props{Class: "rail-menu-check", Aria: map[string]string{"hidden": "true"}, Text: check}), html.Span(html.Props{Text: m.t(item.key)})))
	}
	for _, section := range m.Sections {
		name := section.Name
		if section.ID == "channels" {
			name = m.t(KeySectionChannels)
		}
		if section.ID == "direct" {
			name = m.t(KeySectionDirect)
		}
		items = append(items, html.Button(html.Props{Class: "menu-item", Type: "button", Role: "menuitem", Disabled: m.Callbacks.MoveConversationSection == nil, Data: map[string]string{"action": "rail-move-section", "id": m.RailMenuID, "extra": section.ID}, Text: m.tf(KeyMoveToSection, map[string]string{"name": name})}))
	}
	return html.Div(html.Props{Class: "rail-row-menu", Role: "menu", Aria: map[string]string{"label": m.tf(KeyConversationMore, map[string]string{"name": name})}}, items...)
}

func conversationReferenceLabel(m Model, conversation Conversation) string {
	name := strings.TrimPrefix(strings.TrimSpace(displayName(m, conversation)), "#")
	return ChannelReferenceLabel(name)
}

func kindGlyph(m Model, c Conversation, kindLabel string) ui.Node {
	switch c.Kind {
	case DirectMessage:
		return personAvatar(m, m.PeerIDs[c.ID], displayName(m, c), "avatar tiny")
	case GroupChat:
		return html.Span(html.Props{Class: "kind-glyph", Aria: map[string]string{"label": kindLabel}}, icon("people"))
	case PrivateChannel:
		return html.Span(html.Props{Class: "kind-glyph", Aria: map[string]string{"label": kindLabel}}, icon("lock"))
	}
	return html.Span(html.Props{Class: "kind-glyph hash", Aria: map[string]string{"label": kindLabel}, Text: "#"})
}

// personAvatar keeps the initials in the fixed-size slot beneath the image.
// A failed image therefore leaves the person's initials visible in place.
func personAvatar(m Model, id, name, class string) ui.Node {
	return personAvatarWithPhoto(name, class, m.PhotoURLs[id])
}

func personAvatarWithPhoto(name, class, photoURL string) ui.Node {
	children := []ui.Node{ui.Text(initials(name))}
	if url := strings.TrimSpace(photoURL); url != "" {
		children = append(children, html.Img(html.Props{Class: "chat-avatar-photo", Src: url, Loading: "lazy", Width: "64", Height: "64", OnError: chatPhotoErrorHandler(), OnLoad: chatPhotoLoadHandler(), Raw: map[string]any{"alt": "", "decoding": "async"}}))
	}
	return html.Span(html.Props{Class: class, Aria: map[string]string{"hidden": "true"}}, children...)
}

func conversationAvatar(m Model, c Conversation) ui.Node {
	if c.Kind == DirectMessage {
		return personAvatar(m, m.PeerIDs[c.ID], displayName(m, c), "large-avatar")
	}
	return html.Div(html.Props{Class: "large-avatar", Aria: map[string]string{"hidden": "true"}, Text: initials(displayName(m, c))})
}

func railPreferences(m Model, h handlers) ui.Node {
	p := m.Preferences
	pill := html.Span(html.Props{Class: "prefs-pill", Text: m.t(KeyOff)})
	value := ui.Node(html.Span(html.Props{Class: "prefs-value"}))
	if p.QuietHours {
		pill = html.Span(html.Props{Class: "prefs-pill on", Text: m.t(KeyOn)})
		value = html.Span(html.Props{Class: "prefs-value", Text: quietClock(m, p.QuietStartMinute) + "–" + quietClock(m, p.QuietEndMinute)})
	}
	zone := strings.TrimSpace(p.QuietTimezone)
	if zone == "" {
		zone = "UTC"
	}
	device := localTimeZone()
	zones := []string{}
	seen := map[string]bool{}
	for _, z := range append([]string{zone, device, "UTC"}, commonTimeZones...) {
		if z != "" && !seen[z] {
			seen[z] = true
			zones = append(zones, z)
		}
	}
	options := make([]ui.Node, 0, len(zones))
	for _, z := range zones {
		label := strings.ReplaceAll(z, "_", " ")
		if z == device {
			label = m.tf(KeyTimezoneDevice, map[string]string{"zone": label})
		}
		options = append(options, html.Option(html.Props{Value: z, Selected: z == zone, Text: label}))
	}
	status := m.t(KeyQuietHelp)
	if p.QuietHours {
		status = m.tf(KeyQuietSummary, map[string]string{"from": quietClock(m, p.QuietStartMinute), "until": quietClock(m, p.QuietEndMinute)}) + " · " + strings.ReplaceAll(zone, "_", " ")
	}
	return html.Details(html.Props{Class: "rail-prefs"},
		html.Summary(html.Props{}, icon("moon"), html.Span(html.Props{Class: "prefs-summary-text", Text: m.t(KeyQuietHours)}), value, pill),
		html.Div(html.Props{Class: "rail-prefs-body", Role: "group", Aria: map[string]string{"label": m.t(KeyQuietHours)}},
			html.Label(html.Props{Class: "prefs-row switch-row", For: "quiet-hours"},
				html.Span(html.Props{Class: "switch-text"}, html.Strong(html.Props{Text: m.t(KeyQuietHoursOn)}), html.Span(html.Props{Class: "field-hint", Text: status})),
				html.Input(html.Props{ID: "quiet-hours", Class: "switch", Type: "checkbox", Role: "switch", Checked: p.QuietHours, OnChange: h.quietToggle}),
			),
			html.Div(html.Props{Class: "prefs-times"},
				html.Label(html.Props{Class: "prefs-field", For: "quiet-start"}, html.Span(html.Props{Text: m.t(KeyQuietStart)}),
					html.Input(html.Props{ID: "quiet-start", Class: "chat-input", Type: "time", Disabled: !p.QuietHours, Data: map[string]string{"chat-value": minuteClock(p.QuietStartMinute)}, OnChange: h.quietStart})),
				html.Label(html.Props{Class: "prefs-field", For: "quiet-end"}, html.Span(html.Props{Text: m.t(KeyQuietEnd)}),
					html.Input(html.Props{ID: "quiet-end", Class: "chat-input", Type: "time", Disabled: !p.QuietHours, Data: map[string]string{"chat-value": minuteClock(p.QuietEndMinute)}, OnChange: h.quietEnd})),
			),
			html.Label(html.Props{Class: "prefs-field", For: "quiet-timezone"}, html.Span(html.Props{Text: m.t(KeyQuietTimezone)}),
				html.Select(html.Props{ID: "quiet-timezone", Class: "chat-input", Disabled: !p.QuietHours, OnChange: h.quietTZ}, options...)),
		),
	)
}
func timeline(m Model, h handlers) ui.Node {
	c := m.selected()
	title := displayName(m, c)
	if title == "" {
		title = m.t(KeyConversation)
	}
	audience := m.t(kindKey(c.Kind))
	if c.MemberCount > 0 {
		audience += " · " + memberCountLabel(m, c.MemberCount)
	}
	hasSelection := m.SelectedID != ""
	titleBlock := html.Div(html.Props{Class: "conversation-title"},
		kindGlyph(m, c, m.t(kindKey(c.Kind))),
		html.Div(html.Props{Class: "conversation-heading"},
			html.H1(html.Props{Text: title}),
			html.P(html.Props{Class: "conversation-topic", Text: strings.TrimSpace(strings.Join([]string{audience, c.Topic}, "  "))}),
		),
	)
	if c.Kind == DirectMessage && m.PeerIDs[c.ID] != "" {
		peer := m.PeerIDs[c.ID]
		titleBlock = html.Div(html.Props{Class: "conversation-title"},
			personButton(m, peer, title, "person-avatar-button", conversationAvatar(m, c)),
			html.Div(html.Props{Class: "conversation-heading"},
				personButton(m, peer, title, "conversation-person-name", ui.Text(title)),
				html.P(html.Props{Class: "conversation-topic", Text: strings.TrimSpace(strings.Join([]string{audience, c.Topic}, "  "))}),
			),
		)
	}
	if !hasSelection {
		// Nothing is open: the header stays visually blank (the empty state
		// below already says what to do); the h1 remains for the document
		// outline and assistive technology.
		titleBlock = html.Div(html.Props{Class: "conversation-title"},
			html.Div(html.Props{Class: "conversation-heading"}, html.H1(html.Props{Class: "sr-only", Text: m.t(KeyNoneTitle)})))
	}
	if strings.TrimSpace(m.Search) != "" {
		titleBlock = html.Div(html.Props{Class: "conversation-title"},
			html.Div(html.Props{Class: "conversation-heading"}, html.H1(html.Props{Text: m.tf(KeySearchResults, map[string]string{"query": strings.TrimSpace(m.Search)})})))
	}
	detailsBtn := actionButton("icon-button", "details", "", m.t(KeyDetails), m.Callbacks.ToggleDetails == nil || !hasSelection, icon("info"))
	headerChildren := []ui.Node{
		html.Button(html.Props{Class: "rail-pill mobile-chat-toggle", Type: "button", Disabled: m.Callbacks.ToggleSidebar == nil, Data: map[string]string{"action": "open-rail"}, Aria: map[string]string{"label": m.t(KeyOpenConversations)}, Title: m.t(KeyOpenConversations)}, icon("panel-left"), icon("arrow-left"), html.Span(html.Props{Text: m.t(KeyRailTitle)}), unreadDot(m)),
		titleBlock,
	}
	if hasSelection && strings.TrimSpace(m.Search) == "" {
		actions := []ui.Node{detailsBtn}
		if c.Kind == PublicChannel || c.Kind == PrivateChannel {
			actions = append(actions, channelTodoTrigger(m))
			actions = append(actions, channelPollTrigger(m))
		}
		headerChildren = append(headerChildren, html.Div(html.Props{Class: "conversation-actions", Aria: map[string]string{"pressed": boolString(m.ShowDetails)}}, actions...))
	}
	headerClass := "conversation-header"
	if !hasSelection {
		headerClass += " empty"
	}
	header := html.Header(html.Props{Class: headerClass}, headerChildren...)
	// Every child is keyed: the notice bar comes and goes between the list
	// and the composer, and an unkeyed sibling shift would make the
	// reconciler re-create the composer — dropping focus mid-sentence and
	// letting a late draft render land in an unfocused box.
	content := []ui.Node{html.WithKey(header, "header")}
	// The notice is a slim bar under the header, in flow: it never covers
	// the newest message or the box people type in, and never moves them.
	if m.Notice != "" {
		content = append(content, html.WithKey(notice(m), "notice"))
	}
	if hasSelection && (c.Kind == PublicChannel || c.Kind == PrivateChannel) && strings.TrimSpace(m.Search) == "" {
		content = append(content, html.WithKey(inlineChannelWidgets(m), "widgets-inline"))
		content = append(content, html.WithKey(channelTray(m, h, h.local.tray), "channel-tray"))
	}
	timeline := html.WithKey(ui.CreateElement(virtualTimelineList, virtualTimelineProps{model: m, handlers: h}), "list")
	if strings.TrimSpace(m.Search) != "" {
		content = append(content, html.WithKey(searchResultsPanel(m), "search-results"))
	} else if hasSelection {
		content = append(content,
			html.WithKey(html.Div(html.Props{Class: "timeline-frame"}, timeline,
				html.WithKey(html.Button(html.Props{Class: "jump-newest", Type: "button", Data: map[string]string{"action": "jump-newest"}, Aria: map[string]string{"label": m.t(KeyJumpNewest)}}, icon("arrow-down"), html.Span(html.Props{Text: m.t(KeyJumpNewest)})), "jump")), "timeline-frame"),
			html.WithKey(composer(m, h), "composer"))
	} else {
		content = append(content, timeline)
	}
	// The product shell already owns the page's <main> landmark; the timeline
	// is a named region inside it, not a second main.
	return html.Section(html.Props{Class: "chat-main", Role: "region", Aria: map[string]string{"label": m.t(KeyMessagesRegion)}}, content...)
}

func searchResultsPanel(m Model) ui.Node {
	query := strings.TrimSpace(m.Search)
	children := []ui.Node{html.H2(html.Props{Text: m.tf(KeySearchResults, map[string]string{"query": query})}), searchFilterBar(m, query)}
	if m.SearchLoading {
		children = append(children, html.P(html.Props{Class: "search-status", Role: "status", Aria: map[string]string{"live": "polite", "busy": "true"}, Text: m.t(KeySearchLoading)}))
	}
	if m.SearchError != "" {
		children = append(children, html.P(html.Props{Class: "search-status search-error", Role: "alert", Text: m.SearchError}))
		return html.Div(html.Props{ID: "chat-search-results", Class: "chat-search-results", Role: "region", Aria: map[string]string{"label": m.tf(KeySearchResults, map[string]string{"query": query})}}, children...)
	}
	total := len(m.SearchChannels) + len(m.SearchPeople) + len(m.SearchMessages)
	if !m.SearchLoading && m.SearchError == "" {
		count := m.tf(KeySearchCount, map[string]string{"n": m.n(total)})
		if total == 1 {
			count = m.t(KeySearchCountOne)
		}
		children = append(children, html.P(html.Props{Class: "search-status search-count", Role: "status", Aria: map[string]string{"live": "polite"}, Text: count}))
	}
	if !m.SearchLoading && total == 0 && !m.SearchHasMore && !m.SearchHasMoreChannels {
		children = append(children, html.P(html.Props{Class: "search-status", Role: "status", Text: m.t(KeySearchNoResults)}))
	}
	if len(m.SearchChannels) > 0 || m.SearchHasMoreChannels || m.SearchLoadingMoreChannels {
		rows := []ui.Node{searchGroupHeading(m, KeySearchChannels, len(m.SearchChannels))}
		for _, channel := range m.SearchChannels {
			name := channel.Name
			if channel.Kind == PublicChannel || channel.Kind == PrivateChannel {
				name = "#" + name
			}
			action, label, disabled := "select", m.t(conversationKindKey(channel.Kind)), m.Callbacks.SelectConversation == nil
			if !channel.Joined {
				action, label, disabled = "browse-search-channel", m.tf(KeySearchBrowseChannel, map[string]string{"channel": name}), m.Callbacks.OpenSearchChannel == nil
			}
			rows = append(rows, html.WithKey(html.Button(html.Props{Class: "search-result", Type: "button", Data: map[string]string{"action": action, "id": channel.ID}, Disabled: disabled}, html.Strong(html.Props{}, highlightText(name, query)...), html.Span(html.Props{Text: label})), "channel:"+channel.ID))
		}
		children = append(children, html.Div(html.Props{Class: "search-result-group"}, rows...))
		if m.SearchMoreChannelsError != "" {
			children = append(children, html.P(html.Props{Class: "search-status search-error", Role: "alert", Text: m.SearchMoreChannelsError}))
		}
		if m.SearchHasMoreChannels {
			label := m.t(KeySearchMoreChannels)
			if m.SearchLoadingMoreChannels {
				label = m.t(KeySearchLoadingMoreChannels)
			}
			children = append(children, html.Button(html.Props{Class: "button secondary search-more", Type: "button", Data: map[string]string{"action": "search-more-channels"}, Disabled: m.SearchLoadingMoreChannels || m.Callbacks.SearchMoreChannels == nil, Text: label}))
		}
	}
	if len(m.SearchPeople) > 0 {
		rows := []ui.Node{searchGroupHeading(m, KeySearchPeople, len(m.SearchPeople))}
		for _, person := range m.SearchPeople {
			name := strings.TrimSpace(person.Name)
			if name == "" {
				continue
			}
			rows = append(rows, html.WithKey(html.Button(html.Props{Class: "search-result search-person-result", Type: "button", Data: map[string]string{"action": "open-person", "id": person.ID}, Disabled: m.Callbacks.OpenPerson == nil}, personAvatar(m, person.ID, name, "avatar small"), html.Strong(html.Props{}, highlightText(name, query)...)), "person:"+person.ID))
		}
		children = append(children, html.Div(html.Props{Class: "search-result-group"}, rows...))
	}
	if len(m.SearchMessages) > 0 || m.SearchHasMore || m.SearchLoadingMore {
		rows := []ui.Node{searchGroupHeading(m, KeySearchMessages, len(m.SearchMessages))}
		for _, hit := range m.SearchMessages {
			channel := strings.TrimSpace(hit.ConversationName)
			if channel == "" {
				channel = hit.ConversationID
			}
			name := strings.TrimSpace(hit.Message.Author)
			if name == "" {
				name = hit.Message.AuthorID
			}
			var glyph ui.Node
			for _, room := range m.Conversations {
				if room.ID != hit.ConversationID {
					continue
				}
				if (room.Kind == PublicChannel || room.Kind == PrivateChannel) && !strings.HasPrefix(channel, "#") {
					channel = "#" + channel
				} else if room.Kind == GroupChat || room.Kind == DirectMessage {
					glyph = kindGlyph(m, room, m.t(kindKey(room.Kind)))
				}
				break
			}
			label := m.tf(KeySearchOpenMessage, map[string]string{"channel": channel, "author": name})
			rows = append(rows, html.WithKey(html.Button(html.Props{Class: "search-result search-message-result", Type: "button", Data: map[string]string{"action": "open-search-message", "id": hit.ConversationID, "extra": hit.Message.ID}, Aria: map[string]string{"label": label}, Disabled: m.Callbacks.OpenSearchMessage == nil},
				personAvatar(m, hit.Message.AuthorID, name, "avatar small"),
				html.Span(html.Props{Class: "search-result-main"},
					html.Span(html.Props{Class: "search-result-meta"}, html.Strong(html.Props{Text: name}), html.Span(html.Props{Class: "search-result-context"}, searchContext(m, glyph, channel)...), html.Span(html.Props{Class: "search-result-time", Text: searchWhen(m, hit.Message)})),
					html.Span(html.Props{Class: "search-result-snippet"}, highlightText(searchSnippet(hit.Message.Body, query, 180), query)...))), "message:"+hit.ConversationID+":"+hit.Message.ID))
		}
		children = append(children, html.Div(html.Props{Class: "search-result-group"}, rows...))
		if m.SearchMoreError != "" {
			children = append(children, html.P(html.Props{Class: "search-status search-error", Role: "alert", Text: m.SearchMoreError}))
		}
		if m.SearchHasMore {
			label := m.t(KeySearchMore)
			if m.SearchLoadingMore {
				label = m.t(KeySearchLoadingMore)
			}
			children = append(children, html.Button(html.Props{Class: "button secondary search-more", Type: "button", Data: map[string]string{"action": "search-more"}, Disabled: m.SearchLoadingMore || m.Callbacks.SearchMore == nil, Text: label}))
		}
	}
	return html.Div(html.Props{ID: "chat-search-results", Class: "chat-search-results", Role: "region", Aria: map[string]string{"label": m.tf(KeySearchResults, map[string]string{"query": query}), "live": "polite"}}, children...)
}

func conversationKindKey(kind ConversationKind) string {
	switch kind {
	case PrivateChannel:
		return KeyKindPrivate
	case DirectMessage:
		return KeyKindDirect
	case GroupChat:
		return KeyKindGroup
	default:
		return KeyKindPublic
	}
}

// listAnchor names what the timeline is showing: the room and its newest
// message. The DOM-side scroll keeper (fieldsync_js.go) reads it off the
// attribute: a new room scrolls to the newest message, a new tail in the same
// room follows only when the reader was already at the bottom.
func listAnchor(m Model) string {
	if m.State != StateReady || m.SelectedID == "" {
		return ""
	}
	newest := ""
	var top time.Time
	for _, msg := range m.Messages {
		if !msg.SentAt.Before(top) {
			top, newest = msg.SentAt, msg.ID
		}
	}
	return m.SelectedID + ":" + newest
}

// listClass marks a timeline whose whole history is on screen: it reads from
// the top (intro first) rather than being pushed to the bottom edge.
func listClass(m Model) string {
	if m.State == StateReady && m.SelectedID != "" && len(m.Messages) > 0 && !m.HasOlder {
		return "message-list from-top"
	}
	return "message-list"
}

func timelineBody(m Model, h handlers) []ui.Node {
	switch m.State {
	case StateLoading:
		return []ui.Node{html.Div(html.Props{Class: "state-panel", Role: "status", Aria: map[string]string{"busy": "true"}},
			html.Div(html.Props{Class: "skeleton-lines", Aria: map[string]string{"hidden": "true"}}, html.Span(html.Props{}), html.Span(html.Props{}), html.Span(html.Props{})),
			html.P(html.Props{Text: m.t(KeyLoading)}))}
	case StateError:
		return []ui.Node{html.Div(html.Props{Class: "state-panel", Role: "alert"}, html.H2(html.Props{Text: m.t(KeyErrorTitle)}), html.P(html.Props{Text: m.Error}), retryButton(m))}
	case StateEmpty:
		return []ui.Node{html.Div(html.Props{Class: "state-panel", Role: "status"}, html.Div(html.Props{Class: "empty-icon", Aria: map[string]string{"hidden": "true"}}, icon("compose")), html.H2(html.Props{Text: m.t(KeyEmptyTitle)}), html.P(html.Props{Text: m.t(KeyEmptyBody)}))}
	}
	if m.SelectedID == "" {
		return []ui.Node{html.Div(html.Props{Class: "state-panel", Role: "status"}, html.Div(html.Props{Class: "empty-icon", Aria: map[string]string{"hidden": "true"}}, icon("chat")), html.H2(html.Props{Text: m.t(KeyNoneTitle)}), html.P(html.Props{Text: m.t(KeyNoneBody)}),
			html.Div(html.Props{Class: "state-actions"},
				html.Button(html.Props{Class: "button", Type: "button", Disabled: m.Callbacks.OpenBrowse == nil, Data: map[string]string{"action": "open-browse"}, Text: m.t(KeyBrowse)}),
				html.Button(html.Props{Class: "button secondary", Type: "button", Disabled: m.Callbacks.OpenCreate == nil, Data: map[string]string{"action": "open-create"}, Text: m.t(KeyNewConversation)}),
			))}
	}
	if len(m.Messages) == 0 {
		return []ui.Node{html.Div(html.Props{Class: "state-panel", Role: "status"}, html.H2(html.Props{Text: m.t(KeyNoMessages)}), html.P(html.Props{Text: m.t(KeyEmptyBody)}))}
	}
	items := make([]ui.Node, 0, len(m.Messages)+4)
	messages := chronological(m.Messages)
	if !m.HasOlder {
		items = append(items, html.WithKey(channelIntro(m), "intro"))
	}
	if m.HasOlder {
		items = append(items, html.WithKey(html.Button(html.Props{Class: "button secondary small load-older", Type: "button", Disabled: m.Callbacks.LoadOlder == nil, Data: map[string]string{"action": "load-older"}, Text: m.t(KeyLoadOlder)}), "load-older"))
	}
	var prev Message
	var prevDay string
	for i, msg := range messages {
		day := dayKey(msg.SentAt)
		if day != "" && day != prevDay {
			items = append(items, html.WithKey(html.Div(html.Props{Class: "day-divider", Role: "separator", Aria: map[string]string{"label": dayLabel(m, msg.SentAt)}}, html.Span(html.Props{Text: dayLabel(m, msg.SentAt)})), "day:"+day))
		}
		unread := m.UnreadFromID != "" && msg.ID == m.UnreadFromID
		if unread {
			items = append(items, html.WithKey(html.Div(html.Props{Class: "unread-divider", Role: "separator", Aria: map[string]string{"label": m.t(KeyNew)}}, html.Span(html.Props{Text: m.t(KeyNew)})), "unread:"+msg.ID))
		}
		continued := !unread && i > 0 && day == prevDay && msg.AuthorID != "" && msg.AuthorID == prev.AuthorID && !msg.SentAt.IsZero() && msg.SentAt.Sub(prev.SentAt) < 5*time.Minute && m.EditingID != msg.ID && m.EditingID != prev.ID
		items = append(items, html.WithKey(message(m, h, msg, continued), "message:"+msg.ID))
		prev, prevDay = msg, day
	}
	if m.HasNewer {
		items = append(items, html.WithKey(html.Button(html.Props{Class: "button secondary small load-newer", Type: "button", Disabled: m.Callbacks.LoadNewer == nil, Data: map[string]string{"action": "load-newer"}, Text: m.t(KeyLoadNewer)}), "load-newer"))
	}
	return items
}

// chronological returns the messages oldest first. The client hands the
// timeline over in whatever order its page arrived (the newest-first page is
// the usual case); reading order is this package's responsibility, and a
// stable sort keeps same-instant messages in their given order.
func chronological(in []Message) []Message {
	sorted := true
	for i := 1; i < len(in); i++ {
		if !in[i-1].SentAt.IsZero() && !in[i].SentAt.IsZero() && in[i].SentAt.Before(in[i-1].SentAt) {
			sorted = false
			break
		}
	}
	if sorted {
		return in
	}
	out := make([]Message, len(in))
	copy(out, in)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SentAt.IsZero() || out[j].SentAt.IsZero() {
			return false
		}
		return out[i].SentAt.Before(out[j].SentAt)
	})
	return out
}

// channelIntro opens a room's history the way a reader expects: the room's
// name, who can see it, and then the first message — instead of a void
// above a lone "Today" divider.
func channelIntro(m Model) ui.Node {
	c := m.selected()
	body := m.t(KeyIntroPublic)
	switch c.Kind {
	case PrivateChannel, GroupChat:
		body = m.t(KeyIntroPrivate)
	case DirectMessage:
		body = m.t(KeyIntroDirect)
		if m.CurrentUser != "" && m.PeerIDs[c.ID] == m.CurrentUser {
			body = m.t(KeyIntroSelf)
		}
	}
	name := displayName(m, c)
	if c.Kind == PublicChannel || c.Kind == PrivateChannel {
		name = "#" + name
	}
	return html.Div(html.Props{Class: "channel-intro"},
		conversationAvatar(m, c),
		html.H2(html.Props{Text: m.tf(KeyIntroTitle, map[string]string{"name": name})}),
		html.P(html.Props{Text: body}),
	)
}

// dayKey groups messages by the reader's calendar day. The timestamps a row
// shows are local, so the divider above them has to be local too: a UTC day
// put a 10:28 PM message under the next day's divider, ahead of that
// morning's 4:24 AM, and the feed looked out of order.
func dayKey(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("2006-01-02")
}

func dayLabel(m Model, t time.Time) string {
	t = t.Local()
	today := time.Now().In(t.Location())
	switch dayKey(t) {
	case dayKey(today):
		return m.t(KeyToday)
	case dayKey(today.AddDate(0, 0, -1)):
		return m.t(KeyYesterday)
	}
	if t.Year() == today.Year() {
		return t.Format("Monday, January 2")
	}
	return t.Format("January 2, 2006")
}

func message(m Model, h handlers, msg Message, continued bool) ui.Node {
	own := msg.AuthorID != "" && msg.AuthorID == m.CurrentUser
	reactAction, reactLabel, reactIcon := "react", m.t(KeyReact), "smile"
	if msg.Reacted {
		reactAction, reactLabel, reactIcon = "unreact", m.t(KeyRemoveReaction), "smile-filled"
	}
	pinAction, pinLabel, pinIcon := "pin", m.t(KeyPin), "pin"
	if msg.Pinned {
		pinAction, pinLabel, pinIcon = "unpin", m.t(KeyUnpin), "pin-filled"
	}
	canReact := m.Callbacks.React != nil || m.Callbacks.ReactWith != nil || m.Callbacks.OpenPicker != nil
	if m.Callbacks.OpenPicker != nil || m.Callbacks.ReactWith != nil {
		// With a picker the toolbar's smile opens it; the chips below carry
		// the viewer's own state per emoji.
		reactAction, reactLabel, reactIcon = "react-pick", m.t(KeyReact), "smile"
	}
	menuOpen := m.MenuID == msg.ID
	actions := []ui.Node{}
	// One-click reactions ahead of the tools, the way Slack's hover bar leads
	// with the emoji people use most.
	for _, emoji := range quickReactions {
		label := m.tf(KeyReactWith, map[string]string{"emoji": emoji})
		actions = append(actions, html.Button(html.Props{Class: "message-action quick-react", Type: "button", Disabled: m.Callbacks.ReactWith == nil,
			Data: map[string]string{"action": "react-with", "id": msg.ID, "emoji": emoji}, Aria: map[string]string{"label": label}, Title: label, Text: emoji}))
	}
	actions = append(actions,
		actionButton("message-action", "reply", msg.ID, m.t(KeyReply), m.Callbacks.OpenThread == nil, icon("reply")),
		html.Button(html.Props{Class: "message-action", Type: "button", Disabled: !canReact, Data: map[string]string{"action": reactAction, "id": msg.ID}, Aria: map[string]string{"label": reactLabel, "pressed": boolString(msg.Reacted && reactAction != "react-pick"), "expanded": boolString(m.PickerID == msg.ID)}, Title: reactLabel}, icon(reactIcon)),
		html.Button(html.Props{Class: "message-action", Type: "button", Disabled: m.Callbacks.Pin == nil, Data: map[string]string{"action": pinAction, "id": msg.ID}, Aria: map[string]string{"label": pinLabel, "pressed": boolString(msg.Pinned)}, Title: pinLabel}, icon(pinIcon)),
		html.Button(html.Props{Class: "message-action", Type: "button", Disabled: m.Callbacks.OpenMenu == nil, Data: map[string]string{"action": "menu", "id": msg.ID}, Aria: map[string]string{"label": m.t(KeyMore), "haspopup": "menu", "expanded": boolString(menuOpen)}, Title: m.t(KeyMore)}, icon("more")),
	)
	var menu ui.Node
	if menuOpen {
		copyLabel := m.t(KeyCopyLink)
		if m.PinReferenceUnavailable {
			copyLabel = m.t(KeyPinCopyGuestUnavailable)
		}
		items := []ui.Node{html.Button(html.Props{Class: "menu-item", Type: "button", Role: "menuitem", Disabled: m.Callbacks.CopyLink == nil || m.PinReferenceUnavailable, Data: map[string]string{"action": "copy-link", "id": msg.ID}, Title: copyLabel}, icon("link"), html.Span(html.Props{Text: copyLabel})), copyContentsMenuItem(m, msg)}
		shareDisabled := m.Callbacks.OpenShare == nil || strings.TrimSpace(msg.Body) == ""
		shareTitle := ""
		if strings.TrimSpace(msg.Body) == "" && len(msg.Attachments) > 0 {
			shareTitle = m.t(KeyShareAttachments)
		}
		shareAria := m.t(KeyShareToChannel)
		if shareTitle != "" {
			shareAria += ". " + shareTitle
		}
		items = append(items, html.Button(html.Props{Class: "menu-item", Type: "button", Role: "menuitem", Disabled: shareDisabled, Title: shareTitle, Aria: map[string]string{"label": shareAria}, Data: map[string]string{"action": "open-share", "id": msg.ID}}, icon("reply"), html.Span(html.Props{Text: m.t(KeyShareToChannel)})))
		if own {
			items = append(items,
				html.Button(html.Props{Class: "menu-item", Type: "button", Role: "menuitem", Disabled: m.Callbacks.BeginEdit == nil, Data: map[string]string{"action": "edit", "id": msg.ID}}, icon("edit"), html.Span(html.Props{Text: m.t(KeyEdit)})),
				html.Button(html.Props{Class: "menu-item danger", Type: "button", Role: "menuitem", Disabled: m.Callbacks.DeleteMessage == nil, Data: map[string]string{"action": "delete", "id": msg.ID}}, icon("trash"), html.Span(html.Props{Text: m.t(KeyDelete)})),
			)
		}
		menu = html.Div(html.Props{Class: "message-menu", Role: "menu", Data: map[string]string{"message-menu": msg.ID}, Aria: map[string]string{"label": m.t(KeyMore)}}, items...)
	}
	body := ui.Node(html.Div(html.Props{Class: "message-body", Dir: "auto"}, markdownMessageBody(m, msg.Body)...))
	if m.EditingID == msg.ID {
		editValue := msg.Body
		if m.EditDrafts != nil && m.EditDrafts[msg.ID] != "" {
			editValue = m.EditDrafts[msg.ID]
		}
		body = html.Form(html.Props{Class: "message-edit", OnSubmit: h.editSubmit},
			html.Label(html.Props{Class: "sr-only", For: "edit-" + msg.ID}, ui.Text(m.t(KeyEdit))),
			html.Textarea(html.Props{ID: "edit-" + msg.ID, Class: "edit-input", Data: map[string]string{"chat-value": editValue}, Rows: 2, OnInput: h.editInput}),
			html.Div(html.Props{Class: "edit-actions"},
				html.Button(html.Props{Class: "button secondary small", Type: "button", Disabled: m.Callbacks.CancelEdit == nil, Data: map[string]string{"action": "cancel-edit"}, Text: m.t(KeyCancel)}),
				html.Button(html.Props{Class: "button small", Type: "submit", Disabled: m.Callbacks.EditMessage == nil, Text: m.t(KeySaveEdit)}),
			))
	}
	class := "message"
	if continued {
		class += " continued"
	}
	if msg.Pinned {
		class += " pinned"
	}
	if own {
		class += " own"
	}
	if m.ShowThread && m.ThreadParentID == msg.ID {
		class += " thread-active"
	}
	meta := []ui.Node{}
	if !continued {
		meta = append(meta, personButton(m, msg.AuthorID, msg.Author, "message-author person-name", ui.Text(msg.Author)))
	}
	if msg.TimeLabel != "" {
		meta = append(meta, html.Time(html.Props{Class: "message-time", Text: msg.TimeLabel}))
	}
	if msg.Edited {
		meta = append(meta, html.Span(html.Props{Class: "message-badge", Text: m.t(KeyEdited)}))
	}
	if msg.Pinned {
		meta = append(meta, html.Span(html.Props{Class: "message-badge pinned-badge"}, icon("pin-filled"), html.Span(html.Props{Text: m.t(KeyPinned)})))
	}
	if msg.ForwardedAuthor != "" {
		meta = append(meta, html.Span(html.Props{Class: "message-badge", Text: m.tf(KeyForwardedFrom, map[string]string{"name": msg.ForwardedAuthor})}))
	}
	lead := ui.Node(personButton(m, msg.AuthorID, msg.Author, "person-avatar-button", personAvatar(m, msg.AuthorID, msg.Author, "avatar")))
	if continued {
		lead = html.Div(html.Props{Class: "gutter-time", Aria: map[string]string{"hidden": "true"}, Text: msg.TimeLabel})
	}
	footer := []ui.Node{}
	if s := stats(m, msg); s != "" {
		footer = append(footer, html.Button(html.Props{Class: "message-stats", Type: "button", Disabled: m.Callbacks.OpenThread == nil, Data: map[string]string{"action": "stats", "id": msg.ID}, Text: s}))
	}
	// Focusable so keyboard users reach the action toolbar and a tap on a
	// phone reveals it (actions are hidden there until the row has focus).
	contentChildren := []ui.Node{html.Div(html.Props{Class: "message-meta"}, meta...), body}
	contentChildren = append(contentChildren, linkEmbeds(m, msg.Body)...)
	contentChildren = append(contentChildren, docPreviewEmbeds(m, msg.Body)...)
	if len(msg.Attachments) > 0 {
		contentChildren = append(contentChildren, attachments(m, msg))
	}
	if chips := reactionRow(m, msg); chips != nil {
		contentChildren = append(contentChildren, chips)
	}
	contentChildren = append(contentChildren, html.Div(html.Props{Class: "message-footer"}, footer...))
	children := []ui.Node{
		lead,
		html.Div(html.Props{Class: "message-content"}, contentChildren...),
		html.Div(html.Props{Class: "message-actions", Role: "toolbar", Aria: map[string]string{"label": m.t(KeyMessageActions)}}, actions...),
	}
	if menu != nil {
		children = append(children, menu)
	}
	if msg.ID == m.FocusMessageID {
		class += " search-target"
	}
	return html.Article(html.Props{Class: class, Data: map[string]string{"message-id": msg.ID}, Raw: map[string]any{"tabindex": "0"}}, children...)
}

func copyContentsMenuItem(m Model, msg Message) ui.Node {
	empty := strings.TrimSpace(msg.Body) == ""
	title := ""
	if empty {
		title = m.t(KeyCopyContentsEmpty)
	}
	return html.Button(html.Props{Class: "menu-item", Type: "button", Role: "menuitem", Disabled: m.Callbacks.CopyContents == nil || empty, Title: title, Data: map[string]string{"action": "copy-contents", "id": msg.ID}}, icon("copy"), html.Span(html.Props{Text: m.t(KeyCopyContents)}))
}

func threadMessageMenu(m Model, msg Message) []ui.Node {
	menuID := "thread:" + msg.ID
	open := m.MenuID == menuID
	items := []ui.Node{html.Button(html.Props{Class: "message-action thread-more", Type: "button", Disabled: m.Callbacks.OpenMenu == nil, Data: map[string]string{"action": "menu", "id": menuID}, Aria: map[string]string{"label": m.t(KeyMore), "haspopup": "menu", "expanded": boolString(open)}, Title: m.t(KeyMore)}, icon("more"))}
	if open {
		items = append(items, html.Div(html.Props{Class: "message-menu", Role: "menu", Data: map[string]string{"message-menu": menuID}, Aria: map[string]string{"label": m.t(KeyMore)}}, copyContentsMenuItem(m, msg)))
	}
	return items
}

// reactionRow is the emoji chips under a message: one per emoji with its
// count, the viewer's own highlighted, an add chip, and the picker when it is
// open on this message. Nothing renders for a message with no reactions and
// no open picker, so the grid stays tight.
func reactionRow(m Model, msg Message) ui.Node {
	pickerOpen := m.PickerID == msg.ID
	if len(msg.Chips) == 0 && !pickerOpen {
		return nil
	}
	canPick := m.Callbacks.OpenPicker != nil
	emptyPicker := len(msg.Chips) == 0
	items := make([]ui.Node, 0, len(msg.Chips)+2)
	for _, chip := range msg.Chips {
		if chip.Count <= 0 {
			continue
		}
		class := "reaction"
		if chip.Mine {
			class += " mine"
		}
		label := m.tf(KeyReactionChip, map[string]string{"n": m.n(chip.Count), "emoji": chip.Emoji})
		items = append(items, html.Button(html.Props{Class: class, Type: "button", Disabled: m.Callbacks.ReactWith == nil && m.Callbacks.RemoveReactionWith == nil, Data: map[string]string{"action": "toggle-reaction", "id": msg.ID, "emoji": chip.Emoji}, Aria: map[string]string{"label": label, "pressed": boolString(chip.Mine)}, Title: label},
			html.Span(html.Props{Class: "reaction-emoji", Text: chip.Emoji}), html.Span(html.Props{Class: "reaction-count", Text: m.n(chip.Count)})))
	}
	if canPick && !emptyPicker {
		items = append(items, html.Button(html.Props{Class: "reaction add", Type: "button", Data: map[string]string{"action": "react-pick", "id": msg.ID}, Aria: map[string]string{"label": m.t(KeyReact), "expanded": boolString(pickerOpen)}, Title: m.t(KeyReact)}, icon("smile"), html.Span(html.Props{Text: "+"})))
	}
	if pickerOpen {
		options := make([]ui.Node, 0, len(reactionPalette))
		for _, emoji := range reactionPalette {
			label := m.tf(KeyReactWith, map[string]string{"emoji": emoji})
			options = append(options, html.Button(html.Props{Class: "picker-emoji", Type: "button", Role: "menuitem", Data: map[string]string{"action": "react-with", "id": msg.ID, "emoji": emoji}, Aria: map[string]string{"label": label}, Title: label, Text: emoji}))
		}
		items = append(items, html.Div(html.Props{Class: "reaction-picker", Role: "menu", Aria: map[string]string{"label": m.t(KeyPickReaction)}}, options...))
	}
	class := "reaction-row"
	if emptyPicker {
		class += " picker-only"
	}
	return html.Div(html.Props{Class: class}, items...)
}

func linkEmbeds(m Model, body string) []ui.Node {
	locators := ShareLocators(body, m.EmbedOrigin)
	if len(locators) == 0 {
		return nil
	}
	out := make([]ui.Node, 0, len(locators))
	for _, locator := range locators {
		embed := m.Embeds[locator.Token]
		if locator.LegacyPost != "" {
			embed = LinkEmbed{State: "unavailable"}
			for _, message := range m.Messages {
				if message.ID == locator.LegacyPost {
					embed = LinkEmbed{State: "ready", SourceRoom: m.SelectedID, SourcePost: message.ID, Author: message.Author, Body: message.Body, TimeLabel: message.TimeLabel, Channel: m.selected().Name}
					break
				}
			}
		}
		state := embed.State
		if state == "" {
			continue
		}
		content := []ui.Node{html.Span(html.Props{Class: "chat-embed-label", Text: m.t(KeyEmbedTitle)})}
		if state == "ready" {
			content = append(content, html.Strong(html.Props{Class: "chat-embed-source", Text: embed.Channel}), html.Span(html.Props{Class: "chat-embed-byline", Text: embed.Author + " · " + embed.TimeLabel}))
			if embed.Body != "" {
				content = append(content, html.P(html.Props{Class: "chat-embed-body", Dir: "auto", Text: embed.Body}))
			}
			if embed.AttachmentCount > 0 {
				content = append(content, html.Span(html.Props{Class: "chat-embed-attachments", Text: m.tf(KeyEmbedAttachments, map[string]string{"count": strconv.Itoa(embed.AttachmentCount)})}))
			}
		} else if state == "unavailable" {
			content = append(content, html.Span(html.Props{Text: m.t(KeyEmbedUnavailable)}))
		} else {
			content = append(content, html.Span(html.Props{Text: m.t(KeyEmbedLoading)}))
		}
		key := locator.Token
		if locator.LegacyPost != "" {
			key = "legacy:" + locator.LegacyPost
		}
		card := html.Div(html.Props{Class: "chat-embed", Data: map[string]string{"embed-key": key, "embed-state": state}}, content...)
		if state == "ready" && m.Callbacks.OpenEmbeddedMessage != nil {
			card = html.Button(html.Props{Class: "chat-embed chat-embed-link", Type: "button", Data: map[string]string{"action": "open-embed", "id": key}, Aria: map[string]string{"label": m.t(KeyEmbedOpen)}}, content...)
		}
		out = append(out, card)
	}
	return out
}

func composer(m Model, h handlers) ui.Node {
	id := "chat-composer"
	c := m.selected()
	target := displayName(m, c)
	if c.Kind == PublicChannel || c.Kind == PrivateChannel {
		target = "#" + target
	}
	placeholder := m.tf(KeyComposePlaceholder, map[string]string{"name": target})
	if c.Name == "" {
		placeholder = m.t(KeyComposeUnselected)
	}
	// A refresh of an open room must not lock the composer: a keystroke or
	// Enter that lands while the box is disabled is silently dropped, and a
	// send used to trigger exactly such a refresh, eating the next message.
	disabled := m.SelectedID == "" || m.State == StateError
	canSend := m.Callbacks.SendMessage != nil && !disabled
	return html.Form(html.Props{Class: "chat-composer", OnSubmit: h.composerSubmit, Aria: map[string]string{"label": m.t(KeyComposeRegion)}},
		html.Label(html.Props{Class: "sr-only", For: id}, ui.Text(m.t(KeyMessage))),
		mentionMenu(m, h.mentionView, id),
		docSuggestMenu(m, h.local.docSuggest, id),
		composerNotice(h.local.composerNotice),
		html.Textarea(html.Props{ID: id, Class: "composer-input", Name: "message", Placeholder: placeholder, Rows: 3, Dir: "auto", Data: map[string]string{"chat-value": m.Draft}, Disabled: disabled,
			OnInput: h.composerInput, OnKeyDown: h.composerKey,
			Aria: docSuggestFieldAria(h.local.docSuggest, id, mentionFieldAria(h.mentionView, id, map[string]string{"describedby": "composer-help"}))}),
		html.Div(html.Props{Class: "composer-embeds"}, append(append(linkEmbeds(m, m.Draft), docPreviewEmbeds(m, m.Draft)...), channelReferenceLinks(m, m.Draft)...)...),
		html.Div(html.Props{Class: "composer-toolbar"},
			html.Div(html.Props{Class: "composer-tools"},
				formatToolbar(m, id, disabled),
				emojiPicker(m, id, disabled),
				giphyPickerControl(m, id, disabled),
				html.Button(html.Props{Class: "tool-button", Type: "button", Disabled: true, Aria: map[string]string{"label": m.t(KeyAttach)}, Title: m.t(KeyAttach)}, icon("attach")),
			),
			html.Span(html.Props{ID: "composer-help", Class: "composer-help", Text: m.t(KeyComposeHint)}),
			html.Button(html.Props{Class: "send-button", Type: "submit", Disabled: !canSend, Aria: map[string]string{"label": m.t(KeySend)}, Title: m.t(KeySend)}, icon("send"), html.Span(html.Props{Class: "send-label", Text: m.t(KeySend)})),
		),
	)
}

func emojiPicker(m Model, targetID string, disabled bool) ui.Node {
	pickerID := targetID + "-emoji-picker"
	emojis := []string{"😀", "😂", "❤️", "👍", "🎉", "🙏", "🔥", "👀"}
	buttons := make([]ui.Node, 0, len(emojis))
	for _, emoji := range emojis {
		buttons = append(buttons, html.Button(html.Props{
			Class: "emoji-choice", Type: "button", Data: map[string]string{"action": "emoji-insert", "id": targetID, "emoji": emoji},
			Aria: map[string]string{"label": m.tf(KeyEmojiItem, map[string]string{"emoji": emoji})},
			Text: emoji,
		}))
	}
	return html.Div(html.Props{Class: "emoji-control"},
		html.Button(html.Props{
			Class: "tool-button emoji-trigger", Type: "button", Disabled: disabled,
			Data:  map[string]string{"action": "emoji-toggle", "id": targetID},
			Aria:  map[string]string{"label": m.t(KeyEmojiPicker), "expanded": "false", "controls": pickerID, "haspopup": "dialog"},
			Title: m.t(KeyEmojiPicker),
		}, icon("smile")),
		html.Div(html.Props{ID: pickerID, Class: "emoji-picker", Role: "dialog", Hidden: true, Aria: map[string]string{"label": m.t(KeyEmojiPickerTitle)}}, buttons...),
	)
}

// memberFilter is the search box over a long member list. It appears once
// the list is long enough to need it, and reads through the same attribute
// sync as every other text field here.
func memberFilter(m Model, h handlers) ui.Node {
	if len(m.Members) < 8 && m.MemberQuery == "" {
		return html.Span(html.Props{Class: "member-filter-slot"})
	}
	return html.Div(html.Props{Class: "member-filter"},
		html.Label(html.Props{Class: "sr-only", For: "member-filter"}, ui.Text(m.t(KeyMemberFilter))),
		icon("search"),
		html.Input(html.Props{ID: "member-filter", Class: "chat-search", Type: "search", Placeholder: m.t(KeyMemberFilter), Data: map[string]string{"chat-value": m.MemberQuery}, AutoComplete: "off", OnInput: h.memberFilter}),
	)
}

// activityLabel says how recently a room was used, coarsely: a browse row
// needs "live or dead", not a timestamp.
func activityLabel(m Model, at time.Time) string {
	if at.IsZero() {
		return m.t(KeyNoActivity)
	}
	age := time.Since(at)
	switch {
	case age < time.Hour:
		return m.t(KeyActiveJustNow)
	case age < 24*time.Hour:
		return m.tf(KeyActiveHoursAgo, map[string]string{"n": itoa(int(age / time.Hour))})
	case age < 14*24*time.Hour:
		return m.tf(KeyActiveDaysAgo, map[string]string{"n": itoa(int(age / (24 * time.Hour)))})
	}
	return m.tf(KeyActiveOn, map[string]string{"date": at.Format("Jan 2")})
}

// paneHandle is the draggable seam on a column's inner edge. The browser
// half (paneresize_js.go) does the dragging; the arrow keys and Home work
// through the root key handler so the seam is reachable without a pointer.
func paneHandle(m Model, pane string) ui.Node {
	label, value, low, high := m.t(KeyResizeRail), m.Pane.Rail, RailMin, RailMax
	if value == 0 {
		value = RailDefault
	}
	if pane == "details" {
		label, value, low, high = m.t(KeyResizeSide), m.Pane.Details, DetailsMin, DetailsMax
		if value == 0 {
			value = DetailsDefault
		}
	}
	return html.Div(html.Props{Class: "pane-handle", Role: "separator", Data: map[string]string{"pane": pane}, Title: label,
		Raw:  map[string]any{"tabindex": "0"},
		Aria: map[string]string{"label": label, "orientation": "vertical", "valuenow": itoa(value), "valuemin": itoa(low), "valuemax": itoa(high)}})
}

// resizeFromKey moves a column by keyboard from its handle: arrows nudge by
// a step, Home restores the defaults. It reports whether the key was taken.
func (m Model) resizeFromKey(pane, key string) bool {
	const step = 16
	grow := key == "ArrowRight"
	shrink := key == "ArrowLeft"
	if m.Direction == "rtl" {
		grow, shrink = shrink, grow
	}
	if pane == "details" {
		grow, shrink = shrink, grow
	}
	switch {
	case key == "Home":
		if m.Callbacks.RestorePanes != nil {
			m.Callbacks.RestorePanes()
		}
		return true
	case !grow && !shrink:
		return false
	}
	delta := step
	if shrink {
		delta = -step
	}
	if pane == "details" {
		if m.Callbacks.ResizeDetails == nil {
			return false
		}
		current := m.Pane.Details
		if current == 0 {
			current = DetailsDefault
		}
		m.Callbacks.ResizeDetails(current + delta)
		return true
	}
	if m.Callbacks.ResizeRail == nil {
		return false
	}
	current := m.Pane.Rail
	if current == 0 {
		current = RailDefault
	}
	m.Callbacks.ResizeRail(current + delta)
	return true
}

// --- side column: details or thread ------------------------------------------

func personField(m Model, label, value string) ui.Node {
	if strings.TrimSpace(value) == "" {
		value = m.t(KeyNotAvailable)
	}
	return html.Div(html.Props{Class: "person-detail-field"},
		html.Span(html.Props{Class: "person-detail-label", Text: m.t(label)}), html.Span(html.Props{Class: "person-detail-value", Text: value}))
}

func personManagerField(m Model, p *PersonDetails) ui.Node {
	value := p.Manager
	if strings.TrimSpace(value) == "" {
		value = m.t(KeyNotAvailable)
	}
	var content ui.Node = html.Span(html.Props{Class: "person-detail-value", Text: value})
	if p.ManagerID != "" && p.Manager != "" && m.Callbacks.OpenPerson != nil {
		content = personButton(m, p.ManagerID, p.Manager, "person-detail-value person-detail-link",
			personAvatarWithPhoto(p.Manager, "avatar small person-detail-avatar", p.ManagerPhotoURL), html.Span(html.Props{Class: "person-detail-name", Text: p.Manager}))
	}
	return html.Div(html.Props{Class: "person-detail-field"},
		html.Span(html.Props{Class: "person-detail-label", Text: m.t(KeyManager)}), content)
}

func personReports(m Model, reports []PersonLink) ui.Node {
	if len(reports) == 0 {
		return nil
	}
	links := make([]ui.Node, 0, len(reports))
	for _, report := range reports {
		if strings.TrimSpace(report.ID) == "" || strings.TrimSpace(report.Name) == "" {
			continue
		}
		links = append(links, html.Li(html.Props{}, personButton(m, report.ID, report.Name, "person-detail-link",
			personAvatarWithPhoto(report.Name, "avatar small person-detail-avatar", report.PhotoURL), html.Span(html.Props{Class: "person-detail-name", Text: report.Name}))))
	}
	if len(links) == 0 {
		return nil
	}
	return html.Div(html.Props{Class: "person-detail-field person-detail-reports"},
		html.Span(html.Props{Class: "person-detail-label", Text: m.t(KeyDirectReports)}),
		html.Ul(html.Props{Class: "person-detail-report-list"}, links...))
}

func personPane(m Model) ui.Node {
	p := m.PersonDetails
	if p == nil {
		p = &PersonDetails{}
	}
	name := p.Name
	if name == "" {
		// The HR directory may not hold everyone chat knows (a demo tenant's
		// served workforce can differ from its chat members); the name chat
		// already shows for this person is better than "Not available".
		name = chatKnownName(m, p.ID)
	}
	if name == "" {
		name = m.t(KeyNotAvailable)
	}
	avatar := personAvatar(m, p.ID, name, "large-avatar")
	if p.PhotoURL != "" {
		avatar = html.Div(html.Props{Class: "large-avatar"}, html.Img(html.Props{Class: "chat-avatar-photo", Src: p.PhotoURL, Loading: "lazy", Width: "96", Height: "96", OnError: chatPhotoErrorHandler(), OnLoad: chatPhotoLoadHandler(), Raw: map[string]any{"alt": "", "decoding": "async"}}))
	}
	fields := []ui.Node{
		personField(m, KeyJobTitle, p.JobTitle),
		personManagerField(m, p),
		personField(m, KeyDepartment, p.Department),
		personField(m, KeyPhone, p.Phone),
		personField(m, KeyEmail, p.Email),
	}
	if p.Location != "" {
		fields = append(fields, personField(m, KeyLocation, p.Location))
	}
	if p.Company != "" {
		fields = append(fields, personField(m, KeyCompany, p.Company))
	}
	if p.BusinessUnit != "" {
		fields = append(fields, personField(m, KeyBusinessUnit, p.BusinessUnit))
	}
	status := ui.Node(nil)
	if !p.Ready && !p.Unavailable {
		status = html.P(html.Props{Class: "person-detail-status", Role: "status", Aria: map[string]string{"live": "polite"}, Text: m.t(KeyPersonLoading)})
	} else if p.Unavailable {
		status = html.P(html.Props{Class: "person-detail-status", Role: "status", Aria: map[string]string{"live": "polite"}, Text: m.t(KeyPersonUnavailable)})
	}
	contents := []ui.Node{
		paneHandle(m, "details"),
		html.Div(html.Props{Class: "side-heading"}, html.H2(html.Props{Class: "person-pane-heading", Raw: map[string]any{"tabindex": "-1"}, Text: m.t(KeyPersonDetails)}), actionButton("icon-button", "close-person", "", m.t(KeyClosePerson), m.Callbacks.ClosePerson == nil, icon("close"))),
		html.Div(html.Props{Class: "details-summary person-summary"}, avatar, html.H3(html.Props{Text: name}),
			html.Button(html.Props{Class: "button", Type: "button", Disabled: !(p.Ready || p.Unavailable) || p.ID == "" || m.Callbacks.StartDirectMessage == nil, Data: map[string]string{"action": "start-direct-message", "id": p.ID}, Text: m.t(KeyStartDirectMessage)})),
	}
	if p.OrgChartHref != "" && p.Ready {
		contents = append(contents, html.A(html.Props{Class: "person-org-chart-link", Href: p.OrgChartHref}, html.Span(html.Props{Text: m.t(KeyViewOrgChart)})))
	}
	if status != nil {
		contents = append(contents, status)
	}
	if reports := personReports(m, p.DirectReports); reports != nil {
		fields = append(fields, reports)
	}
	// Without directory details every field would read "Not available"; the
	// status line above already says so once.
	if !p.Unavailable {
		contents = append(contents, html.Div(html.Props{Class: "person-detail-list"}, fields...))
	}
	return html.Aside(html.Props{Class: "chat-side person-pane", Role: "complementary", Data: map[string]string{"pane": "details"}, Aria: map[string]string{"label": m.t(KeyPersonDetails)}}, contents...)
}

func sideColumn(m Model, h handlers) ui.Node {
	if strings.TrimSpace(m.Search) != "" {
		return nil
	}
	if m.ShowPerson {
		return personPane(m)
	}
	if m.ShowThread {
		return threadPane(m, h)
	}
	return details(m, h)
}

func threadPane(m Model, h handlers) ui.Node {
	root := m.ThreadParent
	if root != nil && root.ID != m.ThreadParentID {
		root = nil
	}
	for i := range m.Messages {
		if m.Messages[i].ID == m.ThreadParentID {
			root = &m.Messages[i]
			break
		}
	}
	roomName := displayName(m, m.selected())
	if k := m.selected().Kind; k == PublicChannel || k == PrivateChannel {
		roomName = "#" + roomName
	}
	// First-strong isolates keep "#general" whole inside a right-to-left
	// sentence; without them the "#" drifts to the far end of the name.
	roomName = "\u2068" + roomName + "\u2069"
	items := []ui.Node{html.Div(html.Props{Class: "side-heading thread-heading"},
		html.H2(html.Props{Text: m.tf(KeyThreadIn, map[string]string{"name": roomName})}),
		html.Div(html.Props{Class: "side-heading-actions"},
			html.Button(html.Props{Class: "button secondary small", Type: "button", Disabled: m.Callbacks.SetThreadFollow == nil, Data: map[string]string{"action": "follow"}, Aria: map[string]string{"pressed": boolString(m.ThreadFollowed), "label": m.t(KeyFollow)}, Text: followLabelFor(m, m.ThreadFollowed)}),
			actionButton("icon-button", "close-thread", "", m.t(KeyCloseThread), m.Callbacks.CloseThread == nil, icon("close")),
		))}
	if root != nil {
		rootChildren := []ui.Node{html.Div(html.Props{Class: "message-meta"}, personButton(m, root.AuthorID, root.Author, "message-author person-name", ui.Text(root.Author)), html.Time(html.Props{Class: "message-time", Text: root.TimeLabel}),
			html.Button(html.Props{Class: "thread-view-in-channel", Type: "button", Data: map[string]string{"action": "reveal-thread-parent"}, Text: m.t(KeyViewInChannel)})), html.Div(html.Props{Class: "message-body", Dir: "auto"}, markdownMessageBody(m, root.Body)...)}
		rootChildren = append(rootChildren, linkEmbeds(m, root.Body)...)
		rootChildren = append(rootChildren, docPreviewEmbeds(m, root.Body)...)
		if len(root.Attachments) > 0 {
			rootChildren = append(rootChildren, attachments(m, *root))
		}
		if chips := reactionRow(m, *root); chips != nil {
			rootChildren = append(rootChildren, chips)
		}
		rootChildren = append(rootChildren, threadMessageMenu(m, *root)...)
		items = append(items, html.Div(html.Props{Class: "thread-root"}, personButton(m, root.AuthorID, root.Author, "person-avatar-button", personAvatar(m, root.AuthorID, root.Author, "avatar small")), html.Div(html.Props{Class: "thread-root-body"}, rootChildren...)))
	}
	if m.ThreadHasOlder {
		items = append(items, html.Button(html.Props{Class: "button secondary small load-older", Type: "button", Disabled: m.Callbacks.LoadOlderThread == nil, Data: map[string]string{"action": "load-older-thread"}, Text: m.t(KeyLoadOlderThread)}))
	}
	if m.ThreadLoading && len(m.ThreadMessages) == 0 {
		items = append(items, html.Div(html.Props{Class: "thread-loading", Role: "status", Aria: map[string]string{"busy": "true"}},
			html.Div(html.Props{Class: "thread-loading-skeleton", Aria: map[string]string{"hidden": "true"}},
				html.Div(html.Props{Class: "thread-loading-row"},
					html.Span(html.Props{Class: "chat-skeleton thread-loading-avatar"}),
					html.Div(html.Props{Class: "thread-loading-lines"}, html.Span(html.Props{Class: "chat-skeleton"}), html.Span(html.Props{Class: "chat-skeleton"}))),
				html.Div(html.Props{Class: "thread-loading-row"},
					html.Span(html.Props{Class: "chat-skeleton thread-loading-avatar"}),
					html.Div(html.Props{Class: "thread-loading-lines"}, html.Span(html.Props{Class: "chat-skeleton"}), html.Span(html.Props{Class: "chat-skeleton"})))),
			html.P(html.Props{Class: "thread-empty", Text: m.t(KeyThreadLoading)})))
	} else if len(m.ThreadMessages) == 0 {
		items = append(items, html.P(html.Props{Class: "thread-empty", Text: m.t(KeyThreadEmpty)}))
	} else {
		replies := []ui.Node{}
		for _, msg := range m.ThreadMessages {
			content := []ui.Node{html.Div(html.Props{Class: "message-meta"}, personButton(m, msg.AuthorID, msg.Author, "message-author person-name", ui.Text(msg.Author)), html.Time(html.Props{Class: "message-time", Text: msg.TimeLabel})), html.Div(html.Props{Class: "message-body", Dir: "auto"}, markdownMessageBody(m, msg.Body)...)}
			content = append(content, linkEmbeds(m, msg.Body)...)
			content = append(content, docPreviewEmbeds(m, msg.Body)...)
			content = append(content, threadMessageMenu(m, msg)...)
			replies = append(replies, html.Div(html.Props{Class: "thread-message", Data: map[string]string{"message-id": msg.ID}}, personButton(m, msg.AuthorID, msg.Author, "person-avatar-button", personAvatar(m, msg.AuthorID, msg.Author, "avatar small")), html.Div(html.Props{Class: "thread-message-body"}, content...)))
		}
		items = append(items, html.Div(html.Props{Class: "thread-count", Role: "separator"}, html.Span(html.Props{Text: stats(m, Message{Replies: len(m.ThreadMessages)})})))
		items = append(items, html.Div(html.Props{Class: "thread-replies"}, replies...))
	}
	if m.ThreadHasNewer {
		items = append(items, html.Button(html.Props{Class: "button secondary small load-newer", Type: "button", Disabled: m.Callbacks.LoadNewerThread == nil, Data: map[string]string{"action": "load-newer-thread"}, Text: m.t(KeyLoadNewerThread)}))
	}
	heading := items[0]
	body := html.Div(html.Props{Class: "thread-scroll"}, items[1:]...)
	canReply := m.Callbacks.ReplyInThread != nil && m.ThreadParentID != ""
	composer := html.Form(html.Props{Class: "thread-composer", OnSubmit: h.threadSubmit, Aria: map[string]string{"label": m.t(KeyReplySend)}},
		html.Label(html.Props{Class: "sr-only", For: "thread-composer"}, ui.Text(m.t(KeyReplySend))),
		mentionMenu(m, h.mentionView, "thread-composer"),
		html.Textarea(html.Props{ID: "thread-composer", Class: "composer-input", Name: "reply", Placeholder: m.t(KeyReplyPlaceholder), Rows: 1, Dir: "auto", Disabled: !canReply, Data: map[string]string{"chat-value": ""}, OnKeyDown: h.threadKey, OnInput: h.threadInput,
			Aria: mentionFieldAria(h.mentionView, "thread-composer", nil)}),
		html.Div(html.Props{Class: "composer-toolbar"},
			formatToolbar(m, "thread-composer", !canReply),
			emojiPicker(m, "thread-composer", !canReply),
			giphyPickerControl(m, "thread-composer", !canReply),
			html.Span(html.Props{Class: "composer-help", Text: m.t(KeyComposeHint)}),
			html.Button(html.Props{Class: "send-button", Type: "submit", Disabled: !canReply, Aria: map[string]string{"label": m.t(KeyReplySend)}, Title: m.t(KeyReplySend)}, icon("send"), html.Span(html.Props{Class: "send-label", Text: m.t(KeyReplySend)})),
		),
	)
	return html.Aside(html.Props{Class: "chat-side thread-pane", Role: "complementary", Aria: map[string]string{"label": m.t(KeyThreadRegion)}}, paneHandle(m, "details"), heading, body, composer)
}

func details(m Model, h handlers) ui.Node {
	if !m.ShowDetails {
		return html.Aside(html.Props{Class: "chat-side chat-details collapsed", Aria: map[string]string{"label": m.t(KeyDetails), "hidden": "true"}})
	}
	c := m.selected()
	memberNodes := []ui.Node{}
	query := strings.ToLower(strings.TrimSpace(m.MemberQuery))
	for _, p := range m.Members {
		memberName := p.Name
		if memberName == "" || memberName == p.ID {
			memberName = m.t(KeyTodoMemberFallback)
		}
		if query != "" && !strings.Contains(strings.ToLower(memberName), query) {
			continue
		}
		avatarClass := "avatar small"
		if p.Online {
			avatarClass += " online"
		}
		name := memberName
		if p.ID != "" && p.ID == m.CurrentUser {
			name += " (" + m.t(KeyYou) + ")"
		}
		row := []ui.Node{personButton(m, p.ID, memberName, "member-person-button", personAvatar(m, p.ID, memberName, avatarClass), html.Span(html.Props{Class: "member-name", Text: name}))}
		if p.Online {
			row = append(row, html.Span(html.Props{Class: "member-status online", Text: m.t(KeyOnline)}))
		} else if p.Subtitle != "" {
			row = append(row, html.Span(html.Props{Class: "member-status", Text: p.Subtitle}))
		}
		memberNodes = append(memberNodes, html.Li(html.Props{Class: "member-row"}, row...))
	}
	mode := m.Preferences.Notifications[m.SelectedID]
	if mode == "" {
		mode = NotifyAll
	}
	return html.Aside(html.Props{Class: "chat-side chat-details", Role: "complementary", Data: map[string]string{"pane": "details"}, Aria: map[string]string{"label": m.t(KeyDetails)}},
		paneHandle(m, "details"),
		html.Div(html.Props{Class: "side-heading"}, html.H2(html.Props{Text: m.t(KeyDetails)}), actionButton("icon-button", "close-details", "", m.t(KeyCloseDetails), m.Callbacks.ToggleDetails == nil, icon("close"))),
		html.Div(html.Props{Class: "details-summary"}, conversationAvatar(m, c), html.H3(html.Props{Text: displayName(m, c)}), html.P(html.Props{Class: "conversation-topic", Text: m.t(kindKey(c.Kind))})),
		html.WithKey(channelWidgetSections(m, h), "channel-widgets-"+m.SelectedID),
		pinnedSection(m),
		html.Section(html.Props{Class: "details-section"},
			html.Label(html.Props{Class: "prefs-field", For: "notify-mode"}, html.Span(html.Props{Text: m.t(KeyNotifications)}),
				html.Select(html.Props{ID: "notify-mode", Class: "chat-input", OnChange: h.notifyMode},
					html.Option(html.Props{Value: string(NotifyAll), Selected: mode == NotifyAll, Text: m.t(KeyNotifyAll)}),
					html.Option(html.Props{Value: string(NotifyMention), Selected: mode == NotifyMention, Text: m.t(KeyNotifyMentions)}),
					html.Option(html.Props{Value: string(NotifyMute), Selected: mode == NotifyMute, Text: m.t(KeyNotifyMute)}),
				))),
		html.Section(html.Props{Class: "details-section"},
			html.Div(html.Props{Class: "details-section-head"},
				html.H3(html.Props{Text: membersHeading(m, c)}),
				actionButton("icon-button", "refresh-members", "", m.t(KeyRefreshMembers), m.Callbacks.LoadMembers == nil, icon("refresh")),
			),
			memberFilter(m, h),
			html.Ul(html.Props{Class: "member-list"}, memberNodes...)),
	)
}

func todoCompletedBy(m Model, item ChannelTodoItem) string {
	if !item.Completed {
		return ""
	}
	name := ""
	if item.CompletedBySubjectID != "" {
		if item.CompletedBySubjectID == m.CurrentUser && item.CompletedByHomeTenantID == m.CurrentTenantID {
			name = m.CurrentUserName
		}
		if name == "" {
			for _, member := range m.Members {
				if member.ID == item.CompletedBySubjectID && member.HomeTenantID == item.CompletedByHomeTenantID {
					name = member.Name
					break
				}
			}
		}
	}
	if name == "" {
		name = m.t(KeyTodoMemberFallback)
	}
	if name == item.CompletedBySubjectID {
		name = m.t(KeyTodoMemberFallback)
	}
	return m.tf(KeyTodoCompletedBy, map[string]string{"name": name})
}

func todoModeKey(mode string) string {
	switch mode {
	case "ME":
		return KeyTodoModeMe
	case "ME_AND_SELECTED":
		return KeyTodoModeSelected
	default:
		return KeyTodoModeEveryone
	}
}

func todoMayToggle(item ChannelTodoItem) bool { return item.CanToggle || item.CompletionMode == "" }

func todoErrorText(m Model) string {
	if m.ChannelTodoError == "save" {
		return m.t(KeyTodoSaveError)
	}
	return m.t(KeyTodoError)
}

func todoToggleReason(m Model, item ChannelTodoItem) string {
	if todoMayToggle(item) {
		return ""
	}
	if item.CompletionMode == "ME" {
		return m.t(KeyTodoOnlyCreator)
	}
	return m.t(KeyTodoOnlySelected)
}

func todoToggleButton(m Model, item ChannelTodoItem) ui.Node {
	label := m.t(KeyTodoComplete)
	if item.Completed {
		label = m.t(KeyTodoReopen)
	}
	if reason := todoToggleReason(m, item); reason != "" {
		label += ": " + reason
	} else {
		label += ": " + item.Text
	}
	return actionButton("channel-todo-check", "todo-toggle", item.ID, label, m.Callbacks.SetChannelTodoCompleted == nil || m.ChannelTodoPending || m.ChannelTodoLoading || m.ChannelTodoError != "" || !todoMayToggle(item), ui.Text(map[bool]string{true: "☑", false: "☐"}[item.Completed]))
}

func todoMemberName(m Model, selected ChannelTodoSelectedMember) string {
	if selected.SubjectID == m.CurrentUser && selected.HomeTenantID == m.CurrentTenantID && m.CurrentUserName != "" {
		return m.CurrentUserName
	}
	for _, member := range m.Members {
		if member.ID == selected.SubjectID && member.HomeTenantID == selected.HomeTenantID {
			if member.Name != "" && member.Name != member.ID {
				return member.Name
			}
			break
		}
	}
	return m.t(KeyTodoMemberFallback)
}

func todoMemberLabel(m Model, member Member) string {
	label := member.Name
	if label == "" || label == member.ID {
		label = m.t(KeyTodoMemberFallback)
	}
	for _, other := range m.Members {
		if other.ID != member.ID && other.Name == member.Name {
			if member.HomeTenantID != "" {
				return label + " · " + member.HomeTenantID
			}
			return label + " · " + member.ID
		}
	}
	return label
}

func todoAuthorizedSourcePin(m Model) string {
	for _, pin := range m.ChannelPins {
		if pin.PostID != "" && pin.PostID == m.ChannelTodoSourcePin {
			return pin.PostID
		}
	}
	return ""
}

func todoPolicyControls(m Model, h handlers, item ChannelTodoItem) ui.Node {
	mode := item.CompletionMode
	if mode == "" {
		mode = "EVERYONE"
	}
	if !item.CanManageCompletionPolicy {
		return html.P(html.Props{Class: "channel-todo-policy-summary", Text: m.t(KeyTodoModeLabel) + ": " + m.t(todoModeKey(mode))})
	}
	options := []ui.Node{
		html.Option(html.Props{Value: "EVERYONE", Text: m.t(KeyTodoModeEveryone), Selected: mode == "EVERYONE"}),
		html.Option(html.Props{Value: "ME", Text: m.t(KeyTodoModeMe), Selected: mode == "ME"}),
		html.Option(html.Props{Value: "ME_AND_SELECTED", Text: m.t(KeyTodoModeSelected), Selected: mode == "ME_AND_SELECTED"}),
	}
	controls := []ui.Node{
		html.Label(html.Props{Class: "prefs-field", For: "todo-mode-" + item.ID}, html.Span(html.Props{Text: m.t(KeyTodoModeLabel)})),
		html.Select(html.Props{ID: "todo-mode-" + item.ID, Class: "chat-input", Value: mode, Data: map[string]string{"action": "todo-policy-mode", "id": item.ID, "chat-select-value": mode, "chat-select-version": strconv.FormatUint(m.ChannelTodo.Revision, 10) + m.ChannelTodoError}, OnChange: h.todoPolicyMode, Disabled: m.ChannelTodoPending || m.ChannelTodoError != ""}, options...),
	}
	if mode == "ME_AND_SELECTED" {
		selectedRows := []ui.Node{}
		for i, selected := range item.SelectedCompleters {
			name := todoMemberName(m, selected)
			selectedRows = append(selectedRows, html.Li(html.Props{}, html.Span(html.Props{Text: name}), html.Button(html.Props{Class: "icon-button", Type: "button", Data: map[string]string{"action": "todo-policy-remove", "id": item.ID, "extra": strconv.Itoa(i)}, Disabled: m.ChannelTodoPending || m.ChannelTodoError != "", Aria: map[string]string{"label": m.tf(KeyTodoRemoveMember, map[string]string{"name": name})}, Title: m.tf(KeyTodoRemoveMember, map[string]string{"name": name})}, icon("close"))))
		}
		memberOptions := []ui.Node{html.Option(html.Props{Raw: map[string]any{"value": ""}, Text: m.t(KeyTodoAddMember), Selected: true})}
		for i, member := range m.Members {
			selected := member.ID == m.CurrentUser && member.HomeTenantID == m.CurrentTenantID
			for _, already := range item.SelectedCompleters {
				if already.SubjectID == member.ID && already.HomeTenantID == member.HomeTenantID {
					selected = true
					break
				}
			}
			if selected {
				continue
			}
			memberOptions = append(memberOptions, html.Option(html.Props{Value: strconv.Itoa(i), Text: todoMemberLabel(m, member)}))
		}
		controls = append(controls, html.Ul(html.Props{Class: "channel-todo-selected"}, selectedRows...), html.Label(html.Props{Class: "sr-only", For: "todo-member-" + item.ID, Text: m.t(KeyTodoAddMember)}), html.Select(html.Props{ID: "todo-member-" + item.ID, Class: "chat-input", Raw: map[string]any{"value": ""}, Data: map[string]string{"action": "todo-policy-member", "id": item.ID, "chat-select-value": "__none__", "chat-select-version": strconv.FormatUint(m.ChannelTodo.Revision, 10) + m.ChannelTodoError}, OnChange: h.todoPolicyMember, Disabled: m.ChannelTodoPending || m.ChannelTodoError != "" || len(memberOptions) == 1}, memberOptions...))
	}
	return html.Div(html.Props{Class: "channel-todo-policy"}, controls...)
}

func todoNewPolicyControls(m Model, h handlers) ui.Node {
	mode := m.ChannelTodoNewMode
	if mode == "" {
		mode = "EVERYONE"
	}
	disabled := m.ChannelTodoPending || m.ChannelTodoError != "" || m.Callbacks.SetChannelTodoNewPolicy == nil
	controls := []ui.Node{
		html.Label(html.Props{For: "chat-todo-new-mode", Text: m.t(KeyTodoModeLabel)}),
		html.Select(html.Props{ID: "chat-todo-new-mode", Class: "chat-input", Value: mode, Data: map[string]string{"chat-select-value": mode}, OnChange: h.todoNewMode, Disabled: disabled},
			html.Option(html.Props{Value: "EVERYONE", Text: m.t(KeyTodoModeEveryone), Selected: mode == "EVERYONE"}),
			html.Option(html.Props{Value: "ME", Text: m.t(KeyTodoModeMe), Selected: mode == "ME"}),
			html.Option(html.Props{Value: "ME_AND_SELECTED", Text: m.t(KeyTodoModeSelected), Selected: mode == "ME_AND_SELECTED"})),
	}
	if mode == "ME_AND_SELECTED" {
		rows := []ui.Node{}
		for i, selected := range m.ChannelTodoNewSelected {
			name := todoMemberName(m, selected)
			rows = append(rows, html.Li(html.Props{}, html.Span(html.Props{Text: name}), html.Button(html.Props{Class: "icon-button", Type: "button", Data: map[string]string{"action": "todo-new-policy-remove", "extra": strconv.Itoa(i)}, Disabled: disabled, Aria: map[string]string{"label": m.tf(KeyTodoRemoveMember, map[string]string{"name": name})}}, icon("close"))))
		}
		options := []ui.Node{html.Option(html.Props{Raw: map[string]any{"value": ""}, Text: m.t(KeyTodoAddMember), Selected: true})}
		if len(m.ChannelTodoNewSelected) < 20 {
			for i, member := range m.Members {
				if member.ID == m.CurrentUser && member.HomeTenantID == m.CurrentTenantID {
					continue
				}
				found := false
				for _, selected := range m.ChannelTodoNewSelected {
					if selected.SubjectID == member.ID && selected.HomeTenantID == member.HomeTenantID {
						found = true
						break
					}
				}
				if !found {
					options = append(options, html.Option(html.Props{Value: strconv.Itoa(i), Text: todoMemberLabel(m, member)}))
				}
			}
		}
		controls = append(controls, html.Ul(html.Props{Class: "channel-todo-selected"}, rows...), html.Label(html.Props{Class: "sr-only", For: "chat-todo-new-member", Text: m.t(KeyTodoAddMember)}), html.Select(html.Props{ID: "chat-todo-new-member", Class: "chat-input", Raw: map[string]any{"value": ""}, Data: map[string]string{"chat-select-value": "__none__", "chat-select-version": mode + strconv.Itoa(len(m.ChannelTodoNewSelected))}, OnChange: h.todoNewMember, Disabled: disabled || len(options) == 1}, options...))
	}
	return html.Div(html.Props{Class: "channel-todo-new-policy"}, controls...)
}

func channelTodoTrigger(m Model) ui.Node {
	remaining := 0
	for _, item := range m.ChannelTodo.Items {
		if !item.Completed {
			remaining++
		}
	}
	remainingLabel := m.t(KeyTodoNoOpen)
	if remaining > 0 {
		remainingLabel = m.tf(KeyTodoRemaining, map[string]string{"n": m.n(remaining)})
	}
	label := m.t(KeyTodoOpen) + ", " + remainingLabel
	count := m.n(remaining)
	if m.ChannelTodoLoading {
		label = m.t(KeyTodoOpen) + ", " + m.t(KeyTodoLoading)
		count = "…"
	}
	return html.Button(html.Props{Class: "channel-todo-trigger", Type: "button", Disabled: m.Callbacks.OpenChannelTodo == nil,
		Data: map[string]string{"action": "open-todo"}, Aria: map[string]string{"label": label, "pressed": boolString(m.ShowDetails), "busy": boolString(m.ChannelTodoLoading)}, Title: label},
		icon("checklist"), html.Span(html.Props{Class: "channel-todo-trigger-label", Text: m.t(KeyTodoTitle)}),
		html.Span(html.Props{Class: "channel-todo-count", Text: count}))
}

func channelPollTrigger(m Model) ui.Node {
	label := m.t(KeyPollTitle)
	class := "channel-poll-trigger"
	children := []ui.Node{icon("poll"), html.Span(html.Props{Class: "channel-poll-trigger-label", Text: m.t(KeyPollTitle)})}
	if m.ChannelPoll.Question != "" {
		label += ": " + m.ChannelPoll.Question
		class += " active"
		children = append(children, html.Span(html.Props{Class: "channel-poll-active-indicator", Aria: map[string]string{"hidden": "true"}, Text: "•"}))
	}
	return html.Button(html.Props{Class: class, Type: "button", Disabled: m.Callbacks.OpenChannelPoll == nil,
		Data: map[string]string{"action": "open-poll", "id": m.SelectedID}, Aria: map[string]string{"label": label}, Title: label}, children...)
}

func channelTodoSection(m Model, h handlers) ui.Node {
	c := m.selected()
	if c.Kind != PublicChannel && c.Kind != PrivateChannel {
		return html.Span(html.Props{Class: "pinned-slot"})
	}
	items := make([]ui.Node, 0, len(m.ChannelTodo.Items))
	for _, item := range m.ChannelTodo.Items {
		parts := []ui.Node{
			todoToggleButton(m, item),
			html.Span(html.Props{Class: "channel-todo-text", Text: item.Text}),
		}
		if item.Completed {
			parts = append(parts, html.Span(html.Props{Class: "channel-todo-completed-by", Text: todoCompletedBy(m, item)}))
		}
		if reason := todoToggleReason(m, item); reason != "" {
			parts = append(parts, html.Span(html.Props{Class: "channel-todo-restriction", Text: reason}))
		}
		parts = append(parts, todoRuleDisclosure(m, h, item))
		if item.SourcePostID != "" {
			for _, pin := range m.ChannelPins {
				if pin.PostID == item.SourcePostID {
					copyLabel := m.t(KeyPinCopy)
					if m.PinReferenceUnavailable {
						copyLabel = m.t(KeyPinCopyGuestUnavailable)
					}
					parts = append(parts, actionButton("channel-todo-source", "pin-jump", pin.PostID, m.t(KeyPinJump), m.Callbacks.JumpToPin == nil || pin.Sequence == 0, icon("pin"), ui.Text(m.t(KeyTodoSource))), actionButton("icon-button channel-todo-source-copy", "pin-copy", pin.PostID, copyLabel, m.Callbacks.CopyPinReference == nil || m.PinReferenceUnavailable, icon("copy")))
					break
				}
			}
		}
		parts = append(parts, actionButton("icon-button channel-todo-delete", "todo-delete", item.ID, m.t(KeyTodoDelete)+": "+item.Text, m.Callbacks.DeleteChannelTodo == nil || m.ChannelTodoPending || m.ChannelTodoError != "", icon("close")))
		items = append(items, html.Li(html.Props{Class: "channel-todo-row"}, parts...))
	}
	content := []ui.Node{}
	if m.ChannelTodoError != "" {
		content = append(content, html.P(html.Props{Role: "alert", Text: todoErrorText(m)}), actionButton("button secondary small", "todo-retry", "", m.t(KeyRetry), m.Callbacks.RetryChannelTodo == nil || m.ChannelTodoLoading, ui.Text(m.t(KeyRetry))))
	}
	if m.ChannelTodoLoading {
		content = append(content, html.P(html.Props{Role: "status", Text: m.t(KeyTodoLoading)}))
	} else if m.ChannelTodo.Revision > 0 {
		if len(items) == 0 {
			content = append(content, html.P(html.Props{Class: "muted", Text: m.t(KeyTodoEmpty)}))
		}
		content = append(content, html.Ul(html.Props{Class: "channel-todo-list"}, items...))
		pinOptions := []ui.Node{html.Option(html.Props{Raw: map[string]any{"value": ""}, Text: m.t(KeyTodoNoPin), Selected: m.ChannelTodoSourcePin == ""})}
		for _, pin := range m.ChannelPins {
			pinOptions = append(pinOptions, html.Option(html.Props{Value: pin.PostID, Text: excerpt(pin.Body, 70), Selected: m.ChannelTodoSourcePin == pin.PostID}))
		}
		// The task field and Add sit on one row; the linked message and the
		// completion rule are options most tasks never change.
		content = append(content, html.Form(html.Props{Class: "channel-todo-form", OnSubmit: h.todoSubmit},
			html.Div(html.Props{Class: "channel-todo-add-row"},
				html.Label(html.Props{Class: "sr-only", For: "chat-todo-new", Text: m.t(KeyTodoNew)}),
				html.Input(html.Props{ID: "chat-todo-new", Class: "chat-input", Type: "text", MaxLength: 500, Placeholder: m.t(KeyTodoNew), Data: map[string]string{"chat-value": m.ChannelTodoDraft}, OnInput: h.todoDraft, Disabled: m.Callbacks.AddChannelTodo == nil || m.ChannelTodoPending}),
				html.Button(html.Props{Class: "button small", Type: "submit", Disabled: m.Callbacks.AddChannelTodo == nil || m.ChannelTodoPending || m.ChannelTodoError != "", Text: m.t(KeyTodoAdd)})),
			html.Details(html.Props{Class: "channel-todo-options"},
				html.Summary(html.Props{Text: m.t(KeyTodoMoreOptions)}),
				html.Label(html.Props{Class: "prefs-field", For: "chat-todo-pin", Text: m.t(KeyTodoAttachPin)}),
				html.Select(html.Props{ID: "chat-todo-pin", Class: "chat-input", Raw: map[string]any{"value": todoAuthorizedSourcePin(m)}, Data: map[string]string{"chat-select-value": todoAuthorizedSourcePin(m), "chat-select-version": m.SelectedID}, OnChange: h.todoSource, Disabled: m.ChannelTodoPending || len(m.ChannelPins) == 0}, pinOptions...),
				todoNewPolicyControls(m, h))))
	}
	return html.Section(html.Props{ID: "chat-todo-section", Class: "details-section channel-todo", TabIndex: -1, Aria: map[string]string{"label": m.t(KeyTodoTitle)}},
		html.Div(html.Props{Class: "details-section-head"}, html.H3(html.Props{Text: m.t(KeyTodoTitle)}),
			actionButton("icon-button", "todo-pin", "", map[bool]string{true: m.t(KeyTodoUnpin), false: m.t(KeyTodoPin)}[m.ChannelTodo.Pinned], !m.CanPinChannelTodo || m.ChannelTodoPending || m.ChannelTodoLoading || m.ChannelTodoError != "", icon(map[bool]string{true: "pin-filled", false: "pin"}[m.ChannelTodo.Pinned]))),
		html.Div(html.Props{Class: "channel-todo-content"}, content...))
}

// pinnedSection uses the authorized room projection, including posts outside
// the retained timeline window.
func pinnedSection(m Model) ui.Node {
	rows := []ui.Node{}
	for _, msg := range m.ChannelPins {
		copyLabel := m.t(KeyPinCopy)
		if m.PinReferenceUnavailable {
			copyLabel = m.t(KeyPinCopyGuestUnavailable)
		}
		rows = append(rows, html.Li(html.Props{Class: "pinned-row"},
			html.Div(html.Props{Class: "pinned-link"},
				html.Strong(html.Props{Text: msg.Author}),
				html.Span(html.Props{Text: excerpt(msg.Body, 96)})),
			html.Div(html.Props{Class: "pin-actions"},
				actionButton("button secondary small", "pin-jump", msg.PostID, m.t(KeyPinJump), m.Callbacks.JumpToPin == nil || msg.Sequence == 0, ui.Text(m.t(KeyPinJump))),
				actionButton("button secondary small", "pin-copy", msg.PostID, copyLabel, m.Callbacks.CopyPinReference == nil || m.PinReferenceUnavailable, ui.Text(copyLabel)))))
	}
	if len(rows) == 0 {
		return html.Span(html.Props{Class: "pinned-slot"})
	}
	return html.Section(html.Props{Class: "details-section"},
		html.H3(html.Props{Text: m.t(KeyPinned) + " · " + m.n(len(rows))}),
		html.Ul(html.Props{Class: "pinned-list"}, rows...))
}

// excerpt is the first line of a body, cut to n runes.
func excerpt(body string, n int) string {
	if i := strings.IndexByte(body, '\n'); i >= 0 {
		body = body[:i]
	}
	r := []rune(strings.TrimSpace(body))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n-1]) + "…"
}

// --- dialogs ----------------------------------------------------------------

func shareDialog(m Model, h handlers) ui.Node {
	rows := []ui.Node{}
	query := strings.ToLower(strings.TrimSpace(m.ShareQuery))
	for _, c := range m.ShareDestinations {
		if query != "" && !strings.Contains(strings.ToLower(c.Name), query) {
			continue
		}
		rows = append(rows, html.Li(html.Props{Class: "share-row"},
			html.Button(html.Props{Class: "share-choice", Type: "button", Disabled: m.SharePending, Data: map[string]string{"action": "share-destination", "id": c.ID}, Aria: map[string]string{"pressed": boolString(m.ShareDestinationID == c.ID)}},
				kindGlyph(m, c, m.t(kindKey(c.Kind))), html.Span(html.Props{Text: c.Name}), html.Span(html.Props{Class: "share-choice-kind", Text: m.t(kindKey(c.Kind))}))))
	}
	var listing ui.Node = html.Ul(html.Props{Class: "share-list", Aria: map[string]string{"label": m.t(KeyShareDestination)}}, rows...)
	if m.ShareLoading {
		listing = html.P(html.Props{Text: m.t(KeyShareLoading)})
	} else if len(rows) == 0 {
		listing = html.P(html.Props{Text: m.t(KeyShareEmpty)})
	}
	preview := []ui.Node{}
	notices := []ui.Node{}
	if msg := m.ShareSource; msg != nil {
		preview = append(preview, html.Strong(html.Props{Text: msg.Author}), html.P(html.Props{Dir: "auto", Text: msg.Body}))
		if len(msg.Attachments) > 0 {
			notices = append(notices, html.P(html.Props{Class: "share-caution", Text: m.t(KeyShareAttachments)}))
		}
	}
	if m.ShareError != "" {
		notices = append(notices, html.P(html.Props{Class: "share-error", Role: "alert", Text: m.ShareError}))
	}
	return html.Div(html.Props{Class: "chat-dialog-backdrop", Role: "presentation"},
		html.Dialog(html.Props{Class: "chat-dialog share-dialog", Open: true, Role: "dialog", TabIndex: -1, Data: map[string]string{"share-version": itoa(int(m.ShareVersion))}, Aria: map[string]string{"label": m.t(KeyShareTitle), "modal": "true"}},
			html.Div(html.Props{Class: "side-heading"}, html.H2(html.Props{Text: m.t(KeyShareTitle)}), actionButton("icon-button", "close-share", "", m.t(KeyClose), m.Callbacks.CloseShare == nil || m.SharePending, icon("close"))),
			html.Div(html.Props{Class: "share-preview"}, preview...),
			html.Div(html.Props{Class: "share-notices"}, notices...),
			html.P(html.Props{Class: "share-disclosure", Text: m.t(KeyShareDisclosure)}),
			html.Label(html.Props{For: "share-filter", Text: m.t(KeyShareDestination)}),
			html.Input(html.Props{ID: "share-filter", Class: "chat-input", Type: "search", Placeholder: m.t(KeyShareFilter), Data: map[string]string{"chat-value": m.ShareQuery}, AutoComplete: "off", AutoFocus: true, Disabled: m.SharePending, OnInput: h.shareFilter}),
			listing,
			html.Div(html.Props{Class: "dialog-actions"},
				html.Button(html.Props{Class: "button secondary", Type: "button", Disabled: m.Callbacks.CloseShare == nil || m.SharePending, Data: map[string]string{"action": "close-share"}, Text: m.t(KeyCancel)}),
				html.Button(html.Props{Class: "button", Type: "button", Disabled: m.Callbacks.ShareMessage == nil || m.ShareDestinationID == "" || m.SharePending || m.ShareLoading, Data: map[string]string{"action": "share-submit"}, Text: map[bool]string{true: m.t(KeySharePending), false: m.t(KeyShareSubmit)}[m.SharePending]}))))
}

func joinChannelDialog(m Model) ui.Node {
	channel := m.selected()
	for _, candidate := range m.Browse {
		if candidate.ID == m.JoinPromptID {
			channel = candidate
			break
		}
	}
	name := channel.Name
	if name == "" {
		name = m.t(KeyConversation)
	}
	return html.Div(html.Props{Class: "chat-dialog-backdrop join-channel-backdrop", Role: "presentation"},
		html.Dialog(html.Props{Class: "chat-dialog join-channel-dialog", Open: true, Role: "dialog", TabIndex: -1, Aria: map[string]string{"label": m.tf(KeyJoinChannelTitle, map[string]string{"name": name}), "modal": "true"}},
			html.Div(html.Props{Class: "side-heading"}, html.H2(html.Props{Text: m.tf(KeyJoinChannelTitle, map[string]string{"name": name})})),
			html.P(html.Props{Class: "join-channel-copy", Text: m.t(KeyJoinChannelBody)}),
			html.Div(html.Props{Class: "dialog-actions"},
				html.Button(html.Props{Class: "button secondary", Type: "button", Disabled: m.JoinPromptPending || m.Callbacks.DismissJoinPrompt == nil, Data: map[string]string{"action": "join-dismiss", "id": m.JoinPromptID}, Text: m.t(KeyJoinChannelDismiss)}),
				html.Button(html.Props{Class: "button", Type: "button", Disabled: m.JoinPromptPending || m.Callbacks.JoinConversation == nil, Data: map[string]string{"action": "join-confirm", "id": m.JoinPromptID}, Text: map[bool]string{true: m.t(KeyJoinPending), false: m.t(KeyJoinChannelConfirm)}[m.JoinPromptPending]}))))
}

// attachments renders a message's media: images and GIFs inline in a tile
// row, everything else as a file chip. A tile whose grant has not arrived
// yet keeps its footprint (width/height when known) so the timeline does
// not jump when the bytes land.
func attachments(m Model, msg Message) ui.Node {
	tiles := []ui.Node{}
	for _, a := range msg.Attachments {
		name := a.Name
		if name == "" {
			name = a.ID
		}
		if a.IsImage() {
			data := map[string]string{"attachment-id": a.ID}
			class := "attachment-image"
			var frame map[string]any
			if a.Width <= 0 || a.Height <= 0 {
				class += " unmeasured"
			} else {
				class += " measured"
				width := min(a.Width, 360)
				if scaled := a.Width * 320 / a.Height; scaled < width {
					width = scaled
				}
				if width < 1 {
					width = 1
				}
				// The product CSP forbids style attributes, so the frame's size
				// travels as data and the stylesheet reads it with typed attr().
				data["frame-width"], data["frame-w"], data["frame-h"] = itoa(width), itoa(a.Width), itoa(a.Height)
			}
			if a.IsGIF() {
				class += " gif"
			}
			children := []ui.Node{}
			if a.ID != "" && !a.PreviewUnavailable {
				raw := map[string]any{"loading": "lazy", "decoding": "async"}
				if a.Width > 0 && a.Height > 0 {
					raw["width"] = itoa(a.Width)
					raw["height"] = itoa(a.Height)
				}
				media := map[string]string{"action": "view-image", "id": a.ID, "media-id": a.ID, "media-thumb": "thumbnail", "media-display": "display", "media-original": "original", "media-width": itoa(a.Width), "media-height": itoa(a.Height), "media-bytes": strconv.FormatInt(a.Bytes, 10), "media-animated": strconv.FormatBool(a.IsGIF()), "media-name": name}
				children = append(children, html.Button(html.Props{Class: "attachment-image-open", Type: "button", Data: media, Aria: map[string]string{"label": m.t(KeyOpenImage) + ": " + name}, Title: m.t(KeyOpenImage)}, html.Img(html.Props{Alt: name, Raw: raw})))
				if m.Callbacks.DownloadAttachment != nil {
					children = append(children, html.Button(html.Props{Class: "attachment-download", Type: "button", Data: map[string]string{"action": "download-attachment", "id": msg.ID, "extra": a.ID}, Aria: map[string]string{"label": m.t(KeyDownloadAttachment) + ": " + name}, Text: m.t(KeyDownloadAttachment)}))
				}
			} else if a.PreviewUnavailable || a.URL != "" {
				children = append(children, html.Div(html.Props{Class: "attachment-pending", Role: "status"},
					html.Span(html.Props{Text: m.t(KeyAttachmentUnavailable)}),
					html.Button(html.Props{Class: "attachment-download", Type: "button", Disabled: m.Callbacks.DownloadAttachment == nil, Data: map[string]string{"action": "download-attachment", "id": msg.ID, "extra": a.ID}, Aria: map[string]string{"label": m.t(KeyDownloadAttachment) + ": " + name}, Text: m.t(KeyDownloadAttachment)})))
			} else {
				children = append(children, html.Div(html.Props{Class: "attachment-pending", Aria: map[string]string{"label": m.t(KeyAttachmentLoading)}}, icon("attach")))
			}
			if a.IsGIF() {
				children = append(children, html.Span(html.Props{Class: "attachment-badge", Text: m.t(KeyGIF)}))
			}
			tiles = append(tiles, html.Figure(html.Props{Class: class, Data: data, Title: name, Raw: frame}, children...))
			continue
		}
		size := ""
		if a.Bytes > 0 {
			size = humanBytes(a.Bytes)
		}
		chip := []ui.Node{icon("attach"), html.Span(html.Props{Class: "attachment-name", Text: name})}
		if size != "" {
			chip = append(chip, html.Span(html.Props{Class: "attachment-size", Text: size}))
		}
		if a.URL != "" {
			tiles = append(tiles, html.A(html.Props{Class: "attachment-chip", Href: a.URL, Target: "_blank", Rel: "noopener", Data: map[string]string{"attachment-id": a.ID}}, chip...))
		} else {
			class := "attachment-chip pending"
			if a.PreviewUnavailable {
				class = "attachment-chip unavailable"
				chip = append(chip, html.Span(html.Props{Text: m.t(KeyAttachmentUnavailable)}))
				tiles = append(tiles, html.Button(html.Props{Class: class, Type: "button", Disabled: m.Callbacks.DownloadAttachment == nil, Data: map[string]string{"action": "download-attachment", "id": msg.ID, "extra": a.ID}, Aria: map[string]string{"label": m.t(KeyDownloadAttachment) + ": " + name}}, append(chip, html.Span(html.Props{Text: m.t(KeyDownloadAttachment)}))...))
				continue
			}
			tiles = append(tiles, html.Span(html.Props{Class: class, Data: map[string]string{"attachment-id": a.ID}}, chip...))
		}
	}
	return html.Div(html.Props{Class: "message-attachments"}, tiles...)
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return itoa(int((n+512*1024)/(1<<20))) + " MB"
	case n >= 1<<10:
		return itoa(int((n+512)/(1<<10))) + " KB"
	}
	return itoa(int(n)) + " B"
}

// memberCountLabel selects singular copy for a one-member direct message.
func memberCountLabel(m Model, count int) string {
	if count == 1 {
		return m.t(KeyMemberCountOne)
	}
	return m.tf(KeyMemberCount, map[string]string{"n": m.n(count)})
}

// membersHeading names the member list with its size when the size is known.
func membersHeading(m Model, c Conversation) string {
	n := c.MemberCount
	if n == 0 {
		n = len(m.Members)
	}
	if n == 0 {
		return m.t(KeyMembers)
	}
	return m.t(KeyMembers) + " · " + m.n(n)
}

func retryButton(m Model) ui.Node {
	return html.Button(html.Props{Class: "button secondary", Type: "button", Disabled: m.Callbacks.Retry == nil, Data: map[string]string{"action": "retry"}}, ui.Text(m.t(KeyRetry)))
}

// displayName is the conversation's name as the viewer should read it. A
// direct message is stored under every participant's name ("Rafael Torres,
// Evelyn Morgan"); the viewer already knows they are in it, so their own name
// is dropped and the row, the header and the composer say who they are
// talking to. A name that is not a participant list is returned as it is.
func displayName(m Model, c Conversation) string {
	// A direct message whose only member is the viewer is their own notes
	// space; naming it after them without a marker reads as a DM to someone.
	if c.Kind == DirectMessage && m.CurrentUser != "" && m.PeerIDs[c.ID] == m.CurrentUser && strings.TrimSpace(c.Name) != "" {
		return m.tf(KeySelfName, map[string]string{"name": strings.TrimSpace(c.Name)})
	}
	if c.Kind != DirectMessage || m.CurrentUserName == "" || !strings.Contains(c.Name, ",") {
		return c.Name
	}
	parts := strings.Split(c.Name, ",")
	kept := parts[:0]
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || strings.EqualFold(part, strings.TrimSpace(m.CurrentUserName)) {
			continue
		}
		kept = append(kept, part)
	}
	if len(kept) == 0 || len(kept) == len(parts) {
		return c.Name
	}
	return strings.Join(kept, ", ")
}
