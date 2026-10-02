package chatui

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
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
	editInput, editSubmit, createSubmit, kindPick            ui.Handler
	addMembersSubmit                                         ui.Handler
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
	pollCreate, pollInput                                    ui.Handler
	pickInput, pickKey                                       ui.Handler
	// mentionView is the "@" suggestion list as of this render.
	mentionView                       mentionState
	mentionReplyHint, threadReplyHint string
	composerAgentName                 string
	// local is the tray and create-dialog state as of this render.
	local localUI
}

func bindHandlers(m Model, giphy *giphyPickerViews, mention mentionStore, local localStore, drafts *browserDrafts) handlers {
	act := func(action, id string) { m.act(action, id) }
	threadSend := func() {
		if chatcmd003Send(m, local, "thread-composer", domValue("thread-composer")) {
			return
		}
		if query, ok := giphyCommand(domValue("thread-composer")); ok {
			if strings.TrimSpace(m.GiphyAPIKey) == "" {
				local.update(func(u *localUI) { u.composerNotice = m.t(KeyGiphyUnavailable) })
				return
			}
			setDOMValue("thread-composer", "")
			giphy.openSearch("thread-composer", m.GiphyAPIKey, query)
			return
		}
		replyInThread(m, mention)
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
		options, _ := mentionOptionsForState(m, state.Query, state.Target, state.ShowAllPeople)
		mention.Set(mentionState{})
		if !state.Open || index < 0 || index >= len(options) {
			return
		}
		// Re-read the field: the list was drawn a keystroke ago, and the
		// replacement must land on the "@query" the field holds now.
		value, caret, ok := composerSelection(state.Target)
		if !ok {
			return
		}
		if _, start, found := mentionTokenAt(value, caret); found && start == state.Start {
			option := options[index]
			if option.person != nil {
				updated, next := applyMention(value, start, caret, option.person.Name)
				replaceComposerText(state.Target, updated, next)
				mention.AddPersona(state.Target, m.SelectedID, start, next-1, ChatReference{Kind: "PERSON_MENTION", TenantID: option.person.HomeTenantID, ID: option.person.ID, Display: option.person.Name, ConversationID: m.SelectedID})
				return
			}
			if option.persona != nil {
				_, _, reference, ok := applyPersonaMention(value, start, caret, *option.persona, m.SelectedID)
				if !ok {
					return
				}
				updated, next := insertEmojiAtUTF16(value, "", start, caret)
				replaceComposerText(state.Target, updated, next)
				mention.AddPersonaToken(state.Target, m.SelectedID, reference)
				mention.Set(mentionState{})
			}
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
		// C-19: reposition every keystroke -- the caret, and so the anchor,
		// moves as the reader types. The menu is not in the DOM yet on the
		// render that opens it, so this also runs from a layout effect below.
		positionMentionMenu(target)
	}
	mentionKey := func(e ui.KeyboardEvent, target string) bool {
		state := mention.Get()
		if !state.Open || state.Target != target {
			return false
		}
		options, _ := mentionOptionsForState(m, state.Query, target, state.ShowAllPeople)
		count := len(options)
		switch mentionActionForKey(e.GetKey()) {
		case mentionKeyNext, mentionKeyPrevious:
			if count == 0 {
				return false
			}
			delta := 1
			if mentionActionForKey(e.GetKey()) == mentionKeyPrevious {
				delta = -1
			}
			state.Active = nextMention(state.Active, delta, count)
			state.Details = false
			mention.Set(state)
		case mentionKeyDetails:
			if count == 0 || options[state.Active].persona == nil {
				return false
			}
			state.Details = true
			mention.Set(state)
		case mentionKeyBack:
			if !state.Details {
				return false
			}
			state.Details = false
			mention.Set(state)
		case mentionKeySelect:
			if count == 0 {
				mention.Set(mentionState{})
				return false
			}
			mentionPick(state, state.Active)
		case mentionKeyClose:
			if state.Details {
				state.Details = false
				mention.Set(state)
			} else {
				mention.Set(mentionState{})
			}
			e.StopPropagation()
		default:
			return false
		}
		e.PreventDefault()
		return true
	}
	send := func() {
		if !drafts.canSend(m) || !m.Chatattach001.Ready() {
			return
		}
		raw := domValue("chat-composer")
		refs := mention.PersonaReferences("chat-composer", m.SelectedID, raw)
		body, refs, ok := composerSendPayload(raw, m.Draft, refs)
		if !ok {
			return
		}
		// The command registry reads the line first (composer_commands.go): a
		// command runs and is never posted, a "/word" it does not hold says so and
		// is not posted either, and a line starting with "//" posts one slash.
		if chatcmd003Send(m, local, "chat-composer", body) {
			return
		}
		text, consumed := composerSendCommand(newComposerCommandRuntime(m, local, giphy, drafts, "chat-composer"), body)
		if consumed {
			return
		}
		body = emojiShortcodesInText(text)
		if body == "" || m.SelectedID == "" {
			return
		}
		if len(refs) == 0 {
			if ref, ok := selectedAgentReference(m); ok {
				refs = append(refs, ref)
			}
		}
		if !m.selected().Agent {
			body = bodyWithAgentMentions(body, refs)
		}
		if m.Chatattach001 != nil && len(m.Chatattach001.Files) > 0 && m.Chatattach001.Send != nil {
			m.Chatattach001.Send(body, refs)
		} else if len(refs) > 0 {
			if m.Callbacks.SendMessageWithReferences == nil {
				return
			}
			m.Callbacks.SendMessageWithReferences(m.SelectedID, body, refs)
		} else if m.Callbacks.SendMessage != nil {
			m.Callbacks.SendMessage(m.SelectedID, body)
		} else {
			return
		}
		pinAgentChatAfterSend("chat-composer")
		local.update(func(u *localUI) {
			u.emojiCompletion = emojiCompletion{Active: -1}
			u.commandMenu = composerCommandMenu{}
			u.sentCount++
		})
		mention.RemovePersonas("chat-composer", m.SelectedID)
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
	composerKey := func(e ui.KeyboardEvent, target string, submit func()) {
		// The open "/" command list owns Enter, Tab, the arrows and Escape.
		if composerCommandKey(e, m, local, target) {
			return
		}
		value := domValue(target)
		_, caret, collapsed := composerSelection(target)
		state := liveComposerMention(mention.Get(), target, value, caret)
		if !collapsed {
			state = mentionState{}
		}
		if state != mention.Get() {
			mention.Set(state)
		}
		options, _ := mentionOptionsForState(m, state.Query, target, state.ShowAllPeople)
		emoji := local.get().emojiCompletion
		doc := local.get().docSuggest
		action := composerKeyAction(composerKeyState{
			Draft: value, Shift: shiftHeld(e), Composing: composerIsComposing(e),
			MentionOpen: state.Open, MentionHighlighted: state.Active >= 0 && state.Active < len(options),
			EmojiOpen: emoji.Open && emoji.Target == target, EmojiHighlighted: emoji.Active >= 0 && emoji.Active < len(emojiCompletionItems(emoji.Query)),
			DocumentOpen: doc.Open && doc.Target == target, DocumentHighlighted: doc.Active >= 0 && doc.Active < len(doc.Items),
		}, e.GetKey())
		switch action {
		case composerPickMention:
			e.PreventDefault()
			e.StopPropagation()
			mentionPick(state, state.Active)
			return
		case composerPickEmoji:
			e.PreventDefault()
			e.StopPropagation()
			emojiCompletionPick(local, emoji.Active)
			return
		case composerPickDocument:
			e.PreventDefault()
			e.StopPropagation()
			docSuggestPick(local, doc.Active)
			return
		case composerSend, composerEmpty:
			e.PreventDefault()
			e.StopPropagation()
			mention.Set(mentionState{})
			local.update(func(u *localUI) { u.emojiCompletion = emojiCompletion{Active: -1}; u.docSuggest = docSuggestState{} })
			if action == composerSend {
				submit()
			}
			return
		}
		if composerIsComposing(e) || shiftHeld(e) {
			return
		}
		if emojiCompletionKey(local, e.GetKey(), target) || docSuggestKey(local, e.GetKey(), target) {
			e.PreventDefault()
			e.StopPropagation()
			return
		}
		if mentionKey(e, target) {
			e.StopPropagation()
			return
		}
		if commandHeld(e) && (e.GetKey() == "b" || e.GetKey() == "i") {
			e.PreventDefault()
			kind := "bold"
			if e.GetKey() == "i" {
				kind = "italic"
			}
			applyComposerFormat(target, kind)
		}
	}
	return handlers{
		mentionView:       mention.Get(),
		mentionReplyHint:  mentionReplyHint(m, mention, "chat-composer"),
		threadReplyHint:   mentionReplyHint(m, mention, "thread-composer"),
		composerAgentName: mention.PersonaDisplay("chat-composer", m.SelectedID),
		local:             local.get(),
		rootClick: ui.UseEvent(func(e ui.MouseEvent) {
			rememberChatLayerOpener(e)
			// The composer's tool row (Add menu, @, Aa) and the "/" list's rows.
			if action, id, extra := eventAction(e); chatcmd003Action(m, local, action, id, extra) {
				e.PreventDefault()
				return
			}
			if composerToolsClick(e, m, local) || chatux005Click(e, local) || chatux008Click(e, m) {
				return
			}
			if action, _, _ := eventAction(e); chatux001Click(m, action) {
				return
			}
			if action, _, _ := eventAction(e); action == "chat-search-open" {
				// The header icon does not open a second box: it moves the caret
				// into the sidebar's own search box.
				focusChatSearchBox(m)
				return
			} else if action == "chat-search-close" {
				local.update(func(u *localUI) { u.searchOpen = false })
				restoreChatLayerFocus("search")
				return
			}
			if action, id, _ := eventAction(e); action == "agents-here" {
				if m.Callbacks.ToggleDetails != nil {
					m.Callbacks.ToggleDetails(true)
					focusChatAgentsSection()
				}
				return
			} else if action == "agent-ask-here" {
				selectChatAgent(m, mention, id)
				return
			}
			if action, _, extra := eventAction(e); action == "emoji-completion-pick" {
				if index, err := strconv.Atoi(extra); err == nil {
					emojiCompletionPick(local, index)
				}
				return
			}
			if action, id, _ := eventAction(e); action == "agent-profile-open" || action == "agent-profile-close" {
				e.PreventDefault()
				if action == "agent-profile-close" {
					id = ""
				} else {
					rememberChatDialogTrigger(e, action)
				}
				local.update(func(u *localUI) { u.agentProfileID = id })
				if id != "" {
					focusChatDialog()
				} else {
					restoreChatDialogFocus()
				}
				return
			}
			if action, id, postID := eventAction(e); action == "agent-suggest-mention" {
				e.PreventDefault()
				selectUnresolvedAgent(m, mention, id, postID)
				return
			}
			if action, _, _ := eventAction(e); action == "agent-request-access" {
				e.PreventDefault()
				requestAgentDocumentAccess(m)
				return
			}
			if chatux003HandleAction(e, m, local.get(), mention) {
				return
			}
			switch action, id, _ := eventAction(e); action {
			case "tray-todo", "tray-poll", "open-todo", "open-poll":
				which := "todo"
				if strings.HasSuffix(action, "poll") {
					which = "poll"
				}
				next, opening := chatTrayToggle(local.get().tray, action)
				local.update(func(u *localUI) { u.tray = next })
				if opening && which == "todo" && m.Callbacks.OpenChannelTodo != nil {
					m.Callbacks.OpenChannelTodo()
				}
				if opening && which == "poll" && m.Callbacks.OpenChannelPoll != nil {
					m.Callbacks.OpenChannelPoll(m.SelectedID)
				}
				return
			case "tray-close":
				kind := local.get().tray
				local.update(func(u *localUI) { u.tray = "" })
				restoreChatLayerFocus(kind)
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
			case "open-browse", "open-create", "browse-to-create", "open-add-members":
				closeDrawer()
				local.resetCreate()
			}
			if action, _, extra := eventAction(e); action == "doc-suggest-pick" {
				if index, err := strconv.Atoi(extra); err == nil {
					docSuggestPick(local, index-1)
				}
				return
			}
			if action, _, _ := eventAction(e); action == "persona-mention-retry" {
				e.PreventDefault()
				if m.Callbacks.RetryPersonaMentions != nil {
					m.Callbacks.RetryPersonaMentions()
				}
				return
			}
			if action, id, _ := eventAction(e); action == "mention-show-more" {
				state := mention.Get()
				if state.Open && state.Target == id {
					state.ShowAllPeople = true
					mention.Set(state)
				}
				return
			}
			if action, id, extra := eventAction(e); action == "mention-pick" {
				if index, err := strconv.Atoi(extra); err == nil && mention.Get().Target == id {
					mentionPick(mention.Get(), index-1)
				}
				return
			}
			if action, id, extra := eventAction(e); action == "mention-details" {
				state := mention.Get()
				if index, err := strconv.Atoi(extra); err == nil && state.Target == id && index > 0 {
					state.Active, state.Details = index-1, !(state.Active == index-1 && state.Details)
					mention.Set(state)
				}
				return
			}
			if action, id, extra := eventAction(e); action == "agent-feedback" {
				if m.Callbacks.SubmitAgentFeedback != nil && id != "" {
					state := "not-right"
					if extra == "helpful" {
						state = "helpful"
					}
					m.Callbacks.SubmitAgentFeedback(id, state == "helpful")
					local.update(func(u *localUI) {
						if u.agentFeedback == nil {
							u.agentFeedback = map[string]string{}
						}
						u.agentFeedback[id] = state
					})
				}
				return
			}
			if action, id, _ := eventAction(e); action == "agent-feedback-undo" {
				if m.Callbacks.UndoAgentFeedback != nil && id != "" {
					m.Callbacks.UndoAgentFeedback(id)
					local.update(func(u *localUI) { delete(u.agentFeedback, id) })
				}
				return
			}
			if action, id, extra := eventAction(e); strings.HasPrefix(action, "chatlang-") {
				chatlangClick(m, local, action, id, extra)
				return
			}
			if action, id, _ := eventAction(e); action == "agent-invocation-cancel" {
				if m.Callbacks.CancelPersonaInvocation != nil && id != "" {
					m.Callbacks.CancelPersonaInvocation(id)
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
			if action, id, _ := eventAction(e); action == "open-journey-reference" {
				if journeyReferenceNavigate(m, id, eventPlainClick(e)) {
					e.PreventDefault()
				}
				return
			}
			if action, projectID, taskID := eventAction(e); action == "open-project-task-reference" {
				if projectReferenceNavigate(m, projectID, taskID, eventPlainClick(e)) {
					e.PreventDefault()
				}
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
				} else if m.ShowAddMembers {
					act("close-add-members", "")
				}
				return
			}
			action, id, extra := eventAction(e)
			if chatEmojiClick(e, action, id, extra) {
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
					currentDraft := domDraftValue("chat-composer", m.SelectedID)
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
			if chatLayerContainsEvent(e) {
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
			// Focus inside the composer's Add menu: arrows, Home, End and Escape.
			if composerAddMenuKey(e) {
				e.PreventDefault()
				e.StopPropagation()
				return
			}
			if local.get().agentProfileID != "" {
				if e.GetKey() == "Escape" {
					e.PreventDefault()
					local.update(func(u *localUI) { u.agentProfileID = "" })
					restoreChatDialogFocus()
					return
				}
				if trapChatDialogFocus(e) {
					e.PreventDefault()
					return
				}
			}
			if (e.GetKey() != "Escape" || chatTopLayerKind() == "" || chatTopLayerKind() == "emoji") && handleEmojiPickerKey(e) {
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
			e.PreventDefault()
			e.StopPropagation()
			// Escape closes the one topmost thing; chatEscapeStep owns the order.
			localNow := local.get()
			switch chatEscapeStep(chatEscapeState{
				TopLayer: chatTopLayerKind(), Tray: localNow.tray, SectionCreate: sectionCreateOpen(), Search: localNow.searchOpen,
				Share: m.SharePostID != "", RailMenu: m.RailMenuID != "", Picker: m.PickerID != "", Menu: m.MenuID != "",
				Create: m.ShowCreate, Browse: m.ShowBrowse, AddMembers: m.ShowAddMembers, Person: m.ShowPerson && !m.searching(), Thread: m.ShowThread && !m.searching(), Details: m.ShowDetails && !m.searching(),
				SearchText: m.Search != "" && m.Callbacks.Search != nil, Editing: m.EditingID != "", Sidebar: m.SidebarOpen,
			}) {
			case "section-create":
				closeSectionCreate(true)
			case "search":
				local.update(func(u *localUI) { u.searchOpen = false })
				restoreChatLayerFocus("search")
			case "share":
				act("close-share", "")
			case "rail-menu":
				clearRailMenuDismiss()
				act("rail-menu", m.RailMenuID)
				restoreRailMenuFocus(m.RailMenuID)
			case "reaction":
				local.update(func(u *localUI) { u.focusRow = m.PickerID })
				act("react-pick", m.PickerID)
				restoreChatLayerFocus("reaction")
			case "menu":
				local.update(func(u *localUI) { u.focusRow = strings.TrimPrefix(m.MenuID, "thread:") })
				act("menu", m.MenuID)
				restoreMessageMenuFocus(m.MenuID)
			case "tray":
				kind := localNow.tray
				local.update(func(u *localUI) { u.tray = "" })
				restoreChatLayerFocus(kind)
			case "create":
				act("close-create", "")
				restoreChatDialogFocus()
			case "browse":
				act("close-browse", "")
				restoreChatDialogFocus()
			case "add-members":
				act("close-add-members", "")
			case "person":
				act("close-person", "")
			case "thread":
				act("close-thread", "")
				restoreChatLayerFocus("thread")
			case "details":
				act("close-details", "")
				restoreChatLayerFocus("details")
			case "search-text":
				m.Callbacks.Search("")
			case "edit":
				act("cancel-edit", "")
			case "rail":
				act("close-rail", "")
			}
		}),
		composerSubmit: ui.UseEvent(func(e ui.FormEvent) { e.PreventDefault(); send() }),
		composerKey:    ui.UseEvent(func(e ui.KeyboardEvent) { composerKey(e, "chat-composer", send) }),
		composerInput: ui.UseEvent(func(e ui.InputEvent) {
			setComposerSendReady("chat-composer", e.GetValue())
			mention.ReconcilePersonas("chat-composer", e.GetValue())
			drafts.set(m.SelectedID, e.GetValue())
			if m.Callbacks.DraftChanged != nil {
				m.Callbacks.DraftChanged(m.SelectedID, e.GetValue())
			}
			mentionTrack("chat-composer")
			docSuggestTrack(m, local, "chat-composer")
			emojiCompletionTrack(local, "chat-composer")
			commandMenuTrack(m, local, "chat-composer")
			modAuthorTyped(m, local, ModAuthorKeyComposer(m.SelectedID), e.GetValue())
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
		editInput: ui.UseEvent(func(e ui.InputEvent) {
			if m.Callbacks.SetEditDraft != nil && m.EditingID != "" {
				m.Callbacks.SetEditDraft(m.EditingID, e.GetValue())
			}
			modAuthorTyped(m, local, ModAuthorKeyEdit(m.EditingID), e.GetValue())
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
		// ACCESS-01: shares the create dialog's member picker plumbing
		// (local.picked, the same create-pick/create-unpick actions) --
		// only one of the two dialogs is ever open at a time, and
		// "open-add-members" resets local.picked the same way opening the
		// create dialog does.
		addMembersSubmit: ui.UseEvent(func(e ui.FormEvent) {
			e.PreventDefault()
			if m.Callbacks.AddMembers == nil {
				return
			}
			st := local.get()
			if len(st.picked) == 0 {
				return
			}
			ids := make([]string, 0, len(st.picked))
			for _, p := range st.picked {
				ids = append(ids, p.ID)
			}
			m.Callbacks.AddMembers(ids)
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
				if (strings.TrimSpace(st.pickQuery) != "" || m.ShowAddMembers) && len(people) > 0 {
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
					chatux002CloseSidebarPanels()
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
			if question != "" && len(options) >= 2 {
				m.Callbacks.CreateChannelPoll(question, options)
				local.update(func(u *localUI) { u.pollReady = false })
			}
		}),
		pollInput: ui.UseEvent(func(ui.InputEvent) {
			ready := pollFormReady(domValue("channel-poll-question"), domValue("channel-poll-options"))
			if ready != local.get().pollReady {
				local.update(func(u *localUI) { u.pollReady = ready })
			}
		}),
		threadSubmit: ui.UseEvent(func(e ui.FormEvent) { e.PreventDefault(); threadSend() }),
		threadInput: ui.UseEvent(func(e ui.InputEvent) {
			setComposerSendReady("thread-composer", e.GetValue())
			mention.ReconcilePersonas("thread-composer", e.GetValue())
			mentionTrack("thread-composer")
			emojiCompletionTrack(local, "thread-composer")
			modAuthorTyped(m, local, ModAuthorKeyReply(m.ThreadParentID), e.GetValue())
			if m.Callbacks.ThreadDraftChanged != nil {
				m.Callbacks.ThreadDraftChanged(m.ThreadParentID, e.GetValue())
			}
		}),
		threadKey: ui.UseEvent(func(e ui.KeyboardEvent) { composerKey(e, "thread-composer", threadSend) }),
	}
}

func replyInThread(m Model, mention mentionStore) {
	body := emojiShortcodesInText(strings.TrimSpace(domValue("thread-composer")))
	if body == "" || m.ThreadParentID == "" {
		return
	}
	raw := domValue("thread-composer")
	refs := mention.PersonaReferences("thread-composer", m.SelectedID, raw)
	if len(refs) > 0 {
		if m.Callbacks.ReplyInThreadWithReferences == nil {
			return
		}
		m.Callbacks.ReplyInThreadWithReferences(m.ThreadParentID, body, refs)
	} else if m.Callbacks.ReplyInThread != nil {
		m.Callbacks.ReplyInThread(m.ThreadParentID, body)
	} else {
		return
	}
	mention.RemovePersonas("thread-composer", m.SelectedID)
	pinAgentChatAfterSend("thread-composer")
	setDOMValue("thread-composer", "")
	if m.Callbacks.ThreadDraftChanged != nil {
		m.Callbacks.ThreadDraftChanged(m.ThreadParentID, "")
	}
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
	case "rail-copy-api-curl", "copy-conversation-api-curl":
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
	case "jump-unread":
		chatux007JumpTo(id)
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
	case "open-add-members":
		call(cb.OpenAddMembers)
	case "close-add-members":
		call(cb.CloseAddMembers)
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
	case "clear-search":
		// C-5: the phone results' back control ends the search, which drops
		// the results overlay and leaves the conversation drawer showing.
		if cb.Search != nil {
			cb.Search("")
		}
	case "close-rail":
		if cb.ToggleSidebar != nil {
			cb.ToggleSidebar(false)
		}
	case "details":
		// C-4: the side column shows the open thread over Details
		// (sideColumn), so opening Details while a thread is open must close
		// the thread -- otherwise the button flips ShowDetails and nothing
		// on screen changes.
		if m.ShowThread {
			call(cb.CloseThread)
		}
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
	// A mention typed in one conversation never carries into another.
	mention.ForConversation(model.SelectedID)
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
	ui.UseEffectOf(func() func() { revealSelectedRailRow(model.SelectedID); return nil }, model.SelectedID)
	ui.UseEffectOf(func() func() { syncEditFocus(model.EditingID); return nil }, model.EditingID)
	// The workspace's delegated listeners (long press, disclosure toggles and
	// Escape, and CHATBUG-030's row guard: a hover bar belongs to the row under
	// the pointer or holding focus) follow the workspace element. They are
	// checked after every commit because the element can be replaced while the
	// conversation stays the same; see rebindChatRootListeners.
	ui.UseEffectOf(func() func() { rebindChatRootListeners(local, model); return nil }, nextChatRenderTick())
	ui.UseEffectOf(func() func() { return releaseChatRootListeners }, struct{}{})
	ui.UseEffectOf(func() func() { reconcileChatRowActions(local); return nil }, struct {
		open bool
		id   string
	}{model.ShowThread, model.ThreadParentID})
	ui.UseEffectOf(func() func() {
		if model.MenuID == "" || model.Callbacks.OpenMenu == nil {
			return nil
		}
		return bindMessageMenuSurfaceGuard(func() { model.Callbacks.OpenMenu("") })
	}, model.MenuID != "")
	ui.UseLayoutEffect(func() func() { syncChatAnchoredLayers(); return nil }, struct {
		seq                uint64
		tray, picker, menu string
		// Opening or closing a pane or a dialog re-lays the page out and can replace
		// the elements the layers were drawn on.
		thread, details, person, dialog bool
	}{local.get().seq, local.get().tray, model.PickerID, model.MenuID, model.ShowThread, model.ShowDetails, model.ShowPerson,
		model.ShowCreate || model.ShowBrowse || model.ShowAddMembers || model.JoinPromptID != "" || model.SharePostID != "" || local.get().agentProfileID != ""})
	useChatEmoji(model, local)
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
	// C-19: the menu the first keystroke opens is not in the DOM until this
	// render commits, so mentionTrack's own positionMentionMenu call (fired
	// before that commit) cannot find it yet; this catches that first open
	// and every later reposition once the caret index or query changes.
	ui.UseLayoutEffect(func() func() {
		if h.mentionView.Open {
			positionMentionMenu(h.mentionView.Target)
		}
		return nil
	}, struct {
		open           bool
		target, query  string
		start, caretAt int
	}{h.mentionView.Open, h.mentionView.Target, h.mentionView.Query, h.mentionView.Start, h.mentionView.End})
	if model.Locale == "" {
		model.Locale = "en-US"
	}
	if model.Direction == "" {
		model.Direction = direction(model.Locale)
	}
	model.mentions, model.mentionsReady = mentionIndex(model), true
	// Search results take the whole main column; a thread or details pane from
	// the room behind them would describe something the reader cannot see.
	sideOpen := (model.ShowPerson || model.ShowThread || model.ShowDetails) && !model.searching()
	data := map[string]string{
		"chat-state": string(model.State), "chat-personal-state": "true",
		"selected-id": model.SelectedID, "has-selection": boolString(model.SelectedID != ""),
		"sidebar-open": boolString(model.SidebarOpen), "details-open": boolString(sideOpen),
		"thread-open": boolString(model.ShowThread),
	}
	if model.CurrentUser != "" {
		data["principal"] = model.CurrentUser
	}
	children := []ui.Node{
		chatsavePanel(model),
		ui.CreateElement(chatEmojiLayers, emojiLayersProps{Model: model}),
		chatSearchLayer(model, h),
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
	if model.ShowAddMembers {
		children = append(children, addMembersDialog(model, h))
	}
	if model.JoinPromptID != "" {
		children = append(children, joinChannelDialog(model))
	}
	if model.SharePostID != "" {
		children = append(children, shareDialog(model, h))
	}
	if h.local.agentProfileID != "" {
		children = append(children, agentProfileDialog(model, h.local.agentProfileID))
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
		html.Div(html.Props{Class: "rail-title-group"},
			html.H2(html.Props{Class: "rail-title", Text: m.t(KeyRailTitle)}), chatux002QuietMoon(m)),
		html.Div(html.Props{Class: "rail-head-actions"},
			chatux002PrefsControl(m, h),
			actionButton("icon-button", "open-create", "", m.t(KeyNewConversation), m.Callbacks.OpenCreate == nil, icon("compose")),
			actionButton("icon-button mobile-chat-close", "close-rail", "", m.t(KeyCloseConversations), m.Callbacks.ToggleSidebar == nil, icon("close")),
		),
	)
	searchField := ui.Node(html.Input(html.Props{ID: "chat-search", Class: "chat-search", Type: "search", Placeholder: chatux001Text(m, "chat.ux001.search"), Data: map[string]string{"chat-value": m.Search}, AutoComplete: "off", OnInput: h.searchInput, Aria: map[string]string{"controls": "chat-main", "keyshortcuts": chatux010KeyShortcuts(chatPlatform())}}))
	if h.local.searchOpen {
		searchField = actionButton("chat-search", "chat-search-open", "", chatux001Text(m, "chat.ux001.search"), false)
	}
	search := html.Div(html.Props{Class: "rail-search"},
		html.Label(html.Props{Class: "sr-only", For: "chat-search"}, ui.Text(chatux001Text(m, "chat.ux001.search"))), icon("search"), searchField, chatux010ShortcutHint(m))
	// CHATUX-007: Jump to unread rides the top and bottom edges of the list.
	list := []ui.Node{chatux007Jump(m, chatux007Up)}
	list = append(list, chatsaveSidebar(m), chatmod005Sidebar(m))
	// CHATUX-002: Add channels, Browse channels and New section are one menu on
	// the Channels heading (the first section when there is no Channels one), and
	// the foot of the list holds nothing but conversations.
	menuHost := chatux002MenuHost(sections)
	sectionCreate := chatPolishDisclosure(html.Props{Class: "section-create", ID: "chat-section-create"},
		chatPolishDisclosureLabel(html.Props{Class: "section-create-trigger", Data: map[string]string{"action": "open-section-create"}},
			html.Span(html.Props{Class: "chatux002-item-icon", Aria: map[string]string{"hidden": "true"}}, icon("plus")),
			html.Span(html.Props{Class: "chatux002-item-text"},
				html.Span(html.Props{Class: "chatux002-item-name", Dir: "auto", Text: m.t(KeyNewSection)}),
				html.Span(html.Props{Class: "chatux002-item-note", Dir: "auto", Text: chatux002Text(m, keyChatux002SectionNote)}))),
		html.Form(html.Props{Class: "section-create-form", OnSubmit: h.sectionSubmit},
			html.Label(html.Props{For: "chat-new-section"}, ui.Text(m.t(KeySectionName))),
			html.Input(html.Props{ID: "chat-new-section", Class: "chat-input", Type: "text", MaxLength: 80, Placeholder: m.t(KeySectionName), Required: true, AutoComplete: "off"}),
			html.Div(html.Props{Class: "section-create-actions"},
				html.Button(html.Props{Class: "button secondary small", Type: "button", Data: map[string]string{"action": "cancel-section-create"}, Text: m.t(KeyCancel)}),
				html.Button(html.Props{Class: "button small", Type: "submit", Disabled: m.Callbacks.CreateSection == nil || len(sections) >= 30, Text: m.t(KeyCreate)}))))
	for _, section := range sections {
		var menu ui.Node
		if section.ID == menuHost {
			menu = chatux002ChannelsMenu(m, sectionCreate)
		}
		list = append(list, html.WithKey(railSection(m, section, menu), "section:"+section.ID))
	}
	if len(sections) == 0 {
		list = append(list, html.WithKey(railEmpty(m), "empty"))
	}
	list = append(list, chatux007Jump(m, chatux007Down))
	return html.Nav(html.Props{Class: "chat-rail chat-sidebar", Role: "navigation", Aria: map[string]string{"label": m.t(KeyNav)}},
		head, search,
		html.Div(html.Props{Class: "rail-scroll"}, list...),
		railMenu(m),
		integrate2ArchivedChannels(m),
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

func railSection(m Model, section SidebarSection, menu ui.Node) ui.Node {
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
	if menu != nil {
		controls = append(controls, menu)
	}
	items := []ui.Node{html.WithKey(html.Div(html.Props{Class: "section-controls"}, controls...), "controls")}
	if !section.Collapsed {
		for _, c := range section.Chats {
			items = append(items, html.WithKey(railRow(m, c), "conversation:"+c.ID))
		}
		if len(section.Chats) == 0 && section.ID == "direct" {
			items = append(items, html.WithKey(html.P(html.Props{Class: "rail-empty", Text: m.t(KeyDMEmpty)}), "empty"))
		}
	}
	return html.Div(html.Props{Class: "sidebar-section", Data: map[string]string{"section-id": section.ID}}, items...)
}

func railRow(m Model, c Conversation) ui.Node {
	class := "chat-row"
	if c.ID == m.SelectedID {
		class += " selected"
	}
	if c.Unread > 0 || c.Mentions > 0 {
		class += " unread"
	}
	if c.Muted {
		class += " muted"
	}
	label := displayName(m, c)
	if label == "" && c.Agent {
		label = agentConversationName(m, c)
	}
	// A direct row whose name has not arrived yet while the agent list is being
	// read waits with an empty name: it is never called "Conversation" for a
	// moment and then renamed, and an agent is never shown as an unnamed room.
	holding := label == "" && c.Kind == DirectMessage && m.AgentRailPending
	if label == "" && !holding {
		label = m.t(KeyConversation)
	}
	// C-13: a title attribute lets a reader see the full name of a row this
	// column's width still truncates.
	nameClass := "chat-row-name"
	if holding {
		nameClass += " agent-name-pending"
	}
	children := []ui.Node{kindGlyph(m, c, m.t(kindKey(c.Kind))), html.Span(html.Props{Class: nameClass, Text: label, Title: label})}
	if c.Agent {
		children = append(children, AgentBadgeLabel(m.Locale))
	}
	if view, ok := m.ChannelStatuses[c.ID]; ok {
		if chip := ChannelStatusChip(m, view); chip != nil {
			children = append(children, chip)
		}
	}
	if c.Mentions > 0 {
		children = append(children, html.Span(html.Props{Class: "chat-badge mention", Text: m.n(c.Mentions), Aria: map[string]string{"label": m.tf(KeyMentionCount, map[string]string{"n": m.n(c.Mentions)})}}))
	} else if c.Unread > 0 {
		children = append(children, html.Span(html.Props{Class: "chat-badge", Text: m.n(c.Unread), Aria: map[string]string{"label": m.tf(KeyUnreadCount, map[string]string{"n": m.n(c.Unread)})}}))
	}
	rowAria := map[string]string{"current": boolString(c.ID == m.SelectedID)}
	moreName := label
	if holding {
		rowAria["label"] = m.t(KeyConversation)
		moreName = m.t(KeyConversation)
	}
	return html.Div(html.Props{Class: "chat-rail-row", Data: map[string]string{"conversation-id": c.ID}},
		html.Button(html.Props{Class: class, Type: "button", Disabled: m.Callbacks.SelectConversation == nil, Data: map[string]string{"action": "select", "id": c.ID}, Aria: rowAria}, children...),
		html.Button(html.Props{Class: "rail-row-more", Type: "button", Disabled: m.Callbacks.OpenRailMenu == nil, Data: map[string]string{"action": "rail-menu", "id": c.ID}, Aria: map[string]string{"label": m.tf(KeyConversationMore, map[string]string{"name": moreName}), "haspopup": "menu", "expanded": boolString(m.RailMenuID == c.ID)}, Title: m.t(KeyMore)}, icon("more-vertical")),
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
	// CHAT-08 (retest): "Copy API curl" no longer lives in this everyday
	// menu at all -- it moved to Details' Integrations section, which is
	// itself gated to admins/owners. Every member reaches this menu.
	items := []ui.Node{html.Button(html.Props{Class: "menu-item", Type: "button", Role: "menuitem", Disabled: m.Callbacks.OpenConversationDetails == nil, Data: map[string]string{"action": "rail-details", "id": m.RailMenuID}, Text: m.t(KeyDetails)})}
	items = append(items, html.Button(html.Props{Class: "menu-item", Type: "button", Role: "menuitem", Disabled: m.Callbacks.CopyConversationReference == nil || !found, Data: map[string]string{"action": "rail-copy-reference", "id": m.RailMenuID}}, icon("link"), html.Span(html.Props{Text: m.t(KeyCopyConversationReference)})))
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
		if c.Agent {
			return agentDMAvatar(displayName(m, c), "avatar tiny agent-dm-avatar", conversationAgentIcon(m, c))
		}
		if m.AgentRailPending {
			// Whether this is a person or an agent is not known yet; a letter
			// would be wrong for an agent, so the slot waits.
			return agentDMAvatar(displayName(m, c), "avatar tiny agent-dm-avatar")
		}
		return personAvatar(m, m.PeerIDs[c.ID], displayName(m, c), "avatar tiny")
	case GroupChat:
		return html.Span(html.Props{Class: "kind-glyph", Aria: map[string]string{"label": kindLabel}}, icon("people"))
	case PrivateChannel:
		return html.Span(html.Props{Class: "kind-glyph", Aria: map[string]string{"label": kindLabel}}, icon("lock"))
	}
	return html.Span(html.Props{Class: "kind-glyph hash", Aria: map[string]string{"label": kindLabel}, Text: "#"})
}

func agentDMAvatar(name, class string, identity ...agenticon.Value) ui.Node {
	value := agenticon.Value{}
	if len(identity) > 0 {
		value = identity[0]
	}
	if !value.Valid() {
		// The agent's icon is not known yet. The slot keeps its size and stays
		// empty: the drawing agenticon makes for an unknown value is one shape for
		// every agent, and an agent is never shown wearing another's picture.
		return html.Span(html.Props{Class: class + " agent-icon-pending", Aria: map[string]string{"hidden": "true"}})
	}
	return html.Span(html.Props{Class: class, Aria: map[string]string{"hidden": "true"}}, agenticon.Node(value))
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
		if c.Agent {
			return agentDMAvatar(displayName(m, c), "large-avatar agent-dm-avatar", conversationAgentIcon(m, c))
		}
		return personAvatar(m, m.PeerIDs[c.ID], displayName(m, c), "large-avatar")
	}
	return html.Div(html.Props{Class: "large-avatar", Aria: map[string]string{"hidden": "true"}, Text: initials(displayName(m, c))})
}

// conversationHeaderAvatar is the small header-scoped variant of
// conversationAvatar: the header is 52px tall, so the avatar must fit at
// 28px rather than the 64px intro/details size (C-1).
func conversationHeaderAvatar(m Model, c Conversation) ui.Node {
	if c.Kind == DirectMessage {
		if c.Agent {
			return agentDMAvatar(displayName(m, c), "large-avatar header-avatar agent-dm-avatar", conversationAgentIcon(m, c))
		}
		if m.AgentRailPending {
			return agentDMAvatar(displayName(m, c), "large-avatar header-avatar agent-dm-avatar")
		}
		return personAvatar(m, m.PeerIDs[c.ID], displayName(m, c), "large-avatar header-avatar")
	}
	return html.Div(html.Props{Class: "large-avatar header-avatar", Aria: map[string]string{"hidden": "true"}, Text: initials(displayName(m, c))})
}

func railPreferences(m Model, h handlers) ui.Node {
	p := m.Preferences
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
	unavailable := ""
	if m.Callbacks.SavePreferences == nil {
		status, unavailable = chatPolishUnavailable(m.Locale), chatPolishUnavailable(m.Locale)
	}
	// CHATUX-002: Quiet hours is a section of the Chat preferences panel, drawn
	// under its own heading with the current value beside it.
	quietState := "off"
	if p.QuietHours {
		quietState = "on"
	}
	return chatux002Row("quiet-hours", m.t(KeyQuietHours), chatux002QuietValue(m),
		html.Input(html.Props{ID: "quiet-hours", Class: "switch", Type: "checkbox", Role: "switch", Disabled: m.Callbacks.SavePreferences == nil, Title: unavailable, Aria: map[string]string{"label": m.t(KeyQuietHoursOn)}, Checked: p.QuietHours, OnChange: h.quietToggle}),
		html.Div(html.Props{Class: "rail-prefs-body", Role: "group", Data: map[string]string{"quiet": quietState}, Aria: map[string]string{"label": m.t(KeyQuietHours)}},
			html.P(html.Props{Class: "field-hint chat-prefs-note", Text: status}),
			html.Div(html.Props{Class: "prefs-times"},
				html.Label(html.Props{Class: "prefs-field", For: "quiet-start"}, html.Span(html.Props{Text: m.t(KeyQuietStart)}),
					html.Input(html.Props{ID: "quiet-start", Aria: map[string]string{"label": m.t(KeyQuietStart)}, Class: "chat-input", Type: "time", Disabled: !p.QuietHours, Data: map[string]string{"chat-value": minuteClock(p.QuietStartMinute)}, OnChange: h.quietStart})),
				html.Label(html.Props{Class: "prefs-field", For: "quiet-end"}, html.Span(html.Props{Text: m.t(KeyQuietEnd)}),
					html.Input(html.Props{ID: "quiet-end", Aria: map[string]string{"label": m.t(KeyQuietEnd)}, Class: "chat-input", Type: "time", Disabled: !p.QuietHours, Data: map[string]string{"chat-value": minuteClock(p.QuietEndMinute)}, OnChange: h.quietEnd})),
			),
			html.Label(html.Props{Class: "prefs-field", For: "quiet-timezone"}, html.Span(html.Props{Text: m.t(KeyQuietTimezone)}),
				html.Select(html.Props{ID: "quiet-timezone", Aria: map[string]string{"label": m.t(KeyQuietTimezone)}, Class: "chat-input", Disabled: !p.QuietHours, OnChange: h.quietTZ}, options...)),
		),
	)
}
func timeline(m Model, h handlers) ui.Node {
	c := m.selected()
	title := displayName(m, c)
	if title == "" && c.Agent {
		title = agentConversationName(m, c)
	}
	if title == "" {
		title = m.t(KeyConversation)
	}
	kindLabel := m.t(kindKey(c.Kind))
	if c.Kind == PublicChannel {
		kindLabel = agentUXChat4Text(m, "chat.channel.public_short")
	}
	topicParts := []ui.Node{html.Span(html.Props{Class: "topic-kind", Text: kindLabel})}
	if c.MemberCount > 0 {
		topicParts = append(topicParts, html.Span(html.Props{Class: "topic-count"}, html.Span(html.Props{Class: "topic-sep", Text: " · "}), ui.Text(memberCountLabel(m, c.MemberCount))))
	}
	if agents := chatConversationAgents(m); len(agents) > 0 {
		// CHATBUG-032: the agent count is a third part of the line, set apart by
		// the same separator as the member count at every width.
		topicParts = append(topicParts, html.Span(html.Props{Class: "topic-agents"}, html.Span(html.Props{Class: "topic-sep", Text: " · "}), html.Button(html.Props{Class: "conversation-agent-count", Type: "button", Data: map[string]string{"action": "agents-here"}, Text: agentCountLabel(m, len(agents))})))
	}
	if purpose := chatux001Purpose(m, c); purpose != "" {
		// CHATUX-001: what the channel is for replaces the type and counts.
		topicParts = chatux001PurposeLine(purpose)
	}
	hasSelection := m.SelectedID != ""
	titleBlock := html.Div(html.Props{Class: "conversation-title"},
		kindGlyph(m, c, m.t(kindKey(c.Kind))),
		html.Div(html.Props{Class: "conversation-heading"},
			chatux001Heading(m, title, hasSelection),
			html.P(html.Props{Class: "conversation-topic"}, topicParts...),
		),
	)
	if c.Kind == DirectMessage && c.Agent {
		titleBlock = html.Div(html.Props{Class: "conversation-title"},
			conversationHeaderAvatar(m, c),
			html.Div(html.Props{Class: "conversation-heading"},
				html.Div(html.Props{Class: "agent-identity-line"}, html.H1(html.Props{Text: title}), AgentBadgeLabel(m.Locale)),
				html.P(html.Props{Class: "conversation-topic conversation-topic-long", Text: agentDirectHeaderLine(m, c)}),
				html.P(html.Props{Class: "conversation-topic-short", Text: agentUXChat4Text(m, "chat.agent.private_short")}),
			),
		)
	} else if c.Kind == DirectMessage && m.PeerIDs[c.ID] != "" {
		peer := m.PeerIDs[c.ID]
		// C-7: a DM header names the person, not the kind of conversation or
		// its member count ("Direct message · 2 members" reads as noise), and
		// the name belongs in the document heading, not a plain span.
		titleBlock = html.Div(html.Props{Class: "conversation-title"},
			personButton(m, peer, title, "person-avatar-button", conversationHeaderAvatar(m, c)),
			html.Div(html.Props{Class: "conversation-heading"},
				html.H1(html.Props{Class: "conversation-person-heading"}, personButton(m, peer, title, "conversation-person-name", ui.Text(title))),
				html.P(html.Props{Class: "conversation-topic", Text: directMessageSubtitle(m, c, peer)}),
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
	if m.searching() {
		// Round 3 C-7: the result count sits under the title the way a
		// channel's "Public channel · 19 members" does. The panel's own
		// live status still announces it; this copy is visual only.
		var countLine ui.Node
		if count := searchCountLabel(m); count != "" {
			countLine = html.P(html.Props{Class: "conversation-topic search-head-count", Aria: map[string]string{"hidden": "true"}, Text: count})
		}
		titleBlock = html.Div(html.Props{Class: "conversation-title"},
			html.Div(html.Props{Class: "conversation-heading"}, html.H1(html.Props{Text: m.tf(KeySearchResults, map[string]string{"query": strings.TrimSpace(m.Search)})}), countLine))
	}
	detailsBtn := html.Button(html.Props{Class: "icon-button", Type: "button", Title: m.t(KeyDetails), Disabled: m.Callbacks.ToggleDetails == nil || !hasSelection, Data: map[string]string{"action": "details"}, Aria: map[string]string{"label": m.t(KeyDetails), "pressed": boolString(m.ShowDetails && !m.ShowThread && !m.ShowPerson)}}, icon("info"))
	headerChildren := []ui.Node{
		html.Button(html.Props{Class: "rail-pill mobile-chat-toggle", Type: "button", Disabled: m.Callbacks.ToggleSidebar == nil, Data: map[string]string{"action": "open-rail"}, Aria: map[string]string{"label": m.t(KeyOpenConversations)}, Title: m.t(KeyOpenConversations)}, icon("panel-left"), icon("arrow-left"), html.Span(html.Props{Text: m.t(KeyRailTitle)}), unreadDot(m)),
		titleBlock,
	}
	if view, ok := m.ChannelStatuses[m.SelectedID]; ok {
		if chip := ChannelStatusChip(m, view); chip != nil {
			headerChildren = append(headerChildren, chip)
		}
	}
	if hasSelection && !m.searching() {
		actions := chatux001HeaderActions(m, h, c, detailsBtn)
		headerChildren = append(headerChildren, html.Div(html.Props{Class: "conversation-actions"}, actions...))
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
	if hasSelection && (c.Kind == PublicChannel || c.Kind == PrivateChannel) && !m.searching() {
		content = append(content, html.WithKey(integrate1InlineWidgets(m), "widgets-inline"))
		content = append(content, html.WithKey(channelTray(m, h, h.local.tray), "channel-tray"))
	}
	timeline := html.WithKey(ui.CreateElement(virtualTimelineList, virtualTimelineProps{model: m, handlers: h}), "list")
	if m.searching() {
		content = append(content, html.WithKey(searchResultsPanel(m), "search-results"))
	} else if hasSelection {
		if strings.TrimSpace(m.Search) != "" {
			// A result's conversation is open: the query stays in the box and
			// this bar, in flow above the messages, leads back to the results.
			content = append(content, html.WithKey(searchReturnBar(m), "search-return"))
		}
		if bar := chatlangBar(m, h); bar != nil {
			content = append(content, html.WithKey(bar, "chatlang-bar"))
		}
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

// searchCountLabel is "2 results" once a search has answered, or "" while
// it is loading or failed.
func searchCountLabel(m Model) string {
	if m.ChatSearch != nil {
		// The one search outcome in the results area is the count to state.
		n, ok := chatSearchCount(m.ChatSearch)
		if !ok {
			return ""
		}
		if n == 1 {
			return m.t(KeySearchCountOne)
		}
		return m.tf(KeySearchCount, map[string]string{"n": m.n(n)})
	}
	if m.SearchLoading || m.SearchError != "" {
		return ""
	}
	total := len(m.SearchChannels) + len(m.SearchPeople) + len(m.SearchMessages)
	if total == 1 {
		return m.t(KeySearchCountOne)
	}
	return m.tf(KeySearchCount, map[string]string{"n": m.n(total)})
}

func searchResultsPanel(m Model) ui.Node {
	query := strings.TrimSpace(m.Search)
	// Round 3 C-5: on a phone the results are lifted over the conversation
	// drawer (see .chat-workspace[data-sidebar-open] .chat-search-results),
	// which hid every way back and the query itself. The head carries a
	// back control to the conversation list and makes the title visible
	// there; on wider layouts the conversation header already shows it.
	head := html.Div(html.Props{Class: "search-results-head"},
		html.Button(html.Props{Class: "rail-pill search-results-back", Type: "button", Disabled: m.Callbacks.Search == nil, Data: map[string]string{"action": "clear-search"}, Aria: map[string]string{"label": m.t(KeyOpenConversations)}, Title: m.t(KeyOpenConversations)}, icon("arrow-left"), html.Span(html.Props{Text: m.t(KeyRailTitle)})),
		html.H2(html.Props{Text: m.tf(KeySearchResults, map[string]string{"query": query})}),
		html.Span(html.Props{Class: "search-head-count", Aria: map[string]string{"hidden": "true"}, Text: searchCountLabel(m)}))
	children := []ui.Node{head, searchFilterBar(m, query)}
	if m.SearchLoading {
		children = append(children, html.P(html.Props{Class: "search-status", Role: "status", Aria: map[string]string{"live": "polite", "busy": "true"}, Text: m.t(KeySearchLoading)}))
	}
	if m.SearchError != "" {
		children = append(children, html.P(html.Props{Class: "search-status search-error", Role: "alert", Text: m.SearchError}))
		return html.Div(html.Props{ID: "chat-search-results", Class: "chat-search-results", Role: "region", Aria: map[string]string{"label": m.tf(KeySearchResults, map[string]string{"query": query})}}, children...)
	}
	total := len(m.SearchChannels) + len(m.SearchPeople) + len(m.SearchMessages)
	if count := searchCountLabel(m); count != "" {
		children = append(children, html.P(html.Props{Class: "search-status search-count", Role: "status", Aria: map[string]string{"live": "polite"}, Text: count}))
	}
	if !m.SearchLoading && total == 0 && !m.SearchHasMore && !m.SearchHasMoreChannels {
		children = append(children, html.P(html.Props{Class: "search-status", Role: "status", Text: m.t(KeySearchNoResults)}))
	}
	if len(m.SearchChannels) > 0 || m.SearchHasMoreChannels || m.SearchLoadingMoreChannels {
		rows := []ui.Node{searchGroupHeading(m, KeySearchChannels, len(m.SearchChannels))}
		for _, channel := range m.SearchChannels {
			name := channel.Name
			if channel.Kind == PublicChannel {
				name = "#" + name
			}
			action, label, disabled := "select", m.t(conversationKindKey(channel.Kind)), m.Callbacks.SelectConversation == nil
			if !channel.Joined {
				action, label, disabled = "browse-search-channel", m.tf(KeySearchBrowseChannel, map[string]string{"channel": name}), m.Callbacks.OpenSearchChannel == nil
			}
			// Round 3 C-7: the rail's own glyph (# or lock) leads the name, so
			// a private channel no longer reads "#payroll-close" here while
			// the rail shows it locked.
			// The name is one span: as direct flex children, the highlight
			// and the rest of the name were split by the flex gap
			// ("payroll -close"). Round 4 C-6: the span carries a class so
			// the reconciler cannot elide a prop-less wrapper and hand its
			// parts back to the flex container.
			titleChildren := []ui.Node{kindGlyph(m, channel, m.t(kindKey(channel.Kind))), html.Span(html.Props{Class: "search-channel-label"}, highlightText(channel.Name, query)...)}
			if channel.Agent {
				titleChildren = append(titleChildren, AgentBadgeLabel(m.Locale))
			}
			if view, ok := m.ChannelStatuses[channel.ID]; ok {
				if chip := ChannelStatusChip(m, view); chip != nil {
					titleChildren = append(titleChildren, chip)
				}
			}
			title := html.Strong(html.Props{Class: "search-channel-name"}, titleChildren...)
			rows = append(rows, html.WithKey(html.Button(html.Props{Class: "search-result", Type: "button", Data: map[string]string{"action": action, "id": channel.ID}, Disabled: disabled}, title, html.Span(html.Props{Text: label})), "channel:"+channel.ID))
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
				if room.Kind == PublicChannel && !strings.HasPrefix(channel, "#") {
					channel = "#" + channel
				} else if room.Kind == PrivateChannel || room.Kind == GroupChat || room.Kind == DirectMessage {
					glyph = kindGlyph(m, room, m.t(kindKey(room.Kind)))
				}
				break
			}
			label := m.tf(KeySearchOpenMessage, map[string]string{"channel": channel, "author": name})
			searchActor := ui.Node(nil)
			if hit.Message.PersonaActor != nil {
				searchActor = PersonaBadgeLocalized(m.Locale, hit.Message.PersonaActor)
			}
			rows = append(rows, html.WithKey(html.Button(html.Props{Class: "search-result search-message-result", Type: "button", Data: map[string]string{"action": "open-search-message", "id": hit.ConversationID, "extra": hit.Message.ID}, Aria: map[string]string{"label": label}, Disabled: m.Callbacks.OpenSearchMessage == nil},
				integrate1MessageAvatar(m, hit.Message, "avatar small"),
				html.Span(html.Props{Class: "search-result-main"},
					html.Span(html.Props{Class: "search-result-meta"}, html.Strong(html.Props{Text: name}), searchActor, html.Span(html.Props{Class: "search-result-context"}, searchContext(m, glyph, channel)...), html.Span(html.Props{Class: "search-result-time", Text: searchWhen(m, hit.Message)})),
					html.Span(html.Props{Class: "search-result-snippet"}, highlightText(searchSnippet(resolveJourneyReferencesForSnippet(m, resolveProjectTaskReferencesForSnippet(m, resolveDocTokensForSnippet(m, ReaderMessageBody(m, hit.Message)))), query, 180), query)...))), "message:"+hit.ConversationID+":"+hit.Message.ID))
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
		// CHATUX-009: a skeleton of message rows; one line after five seconds.
		return chatux009LoadingBody(m)
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
	ephemeral := VisibleEphemeralMessages(m.EphemeralMessages, time.Now())
	if len(m.Messages) == 0 && len(ephemeral) == 0 {
		return []ui.Node{html.Div(html.Props{Class: "state-panel", Role: "status"}, html.H2(html.Props{Text: m.t(KeyNoMessages)}), html.P(html.Props{Text: m.t(KeyEmptyBody)}))}
	}
	items := make([]ui.Node, 0, len(m.Messages)+len(ephemeral)+4)
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
		continued := !unread && i > 0 && !messageIsAgent(m, msg) && !messageIsAgent(m, prev) && day == prevDay && msg.AuthorID != "" && msg.AuthorID == prev.AuthorID && !msg.SentAt.IsZero() && msg.SentAt.Sub(prev.SentAt) < 5*time.Minute && m.EditingID != msg.ID && m.EditingID != prev.ID
		items = append(items, html.WithKey(message(m, h, msg, continued), "message:"+msg.ID))
		if m.selected().Agent {
			items = append(items, personaReplyRowsForPost(m, h.local, msg.ID, time.Now())...)
		}
		prev, prevDay = msg, day
	}
	for _, privateMessage := range ephemeral {
		matched := false
		for _, msg := range messages {
			matched = matched || msg.ID == privateMessage.ThreadID
		}
		if !matched {
			items = append(items, html.WithKey(RenderEphemeralMessage(m, privateMessage, time.Now()), "ephemeral:"+privateMessage.ID))
		}
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
	// C-8: a DM (and a private group, which is just as personal a
	// conversation as a DM) reads as a conversation with someone, not a
	// "channel" -- the private-channel copy was grammatically wrong for a
	// person's name and misdescribed a group as a channel.
	titleKey, body := KeyIntroTitle, m.t(KeyIntroPublic)
	switch c.Kind {
	case PrivateChannel:
		body = m.t(KeyIntroPrivate)
	case GroupChat:
		titleKey, body = KeyIntroTitleDirect, m.t(KeyIntroDirect)
	case DirectMessage:
		titleKey, body = KeyIntroTitleDirect, m.t(KeyIntroDirect)
		if m.CurrentUser != "" && m.PeerIDs[c.ID] == m.CurrentUser {
			body = m.t(KeyIntroSelf)
		}
	}
	if c.Agent {
		body = ""
	}
	name := displayName(m, c)
	if c.Kind == PublicChannel || c.Kind == PrivateChannel {
		name = "#" + name
	}
	return html.Div(html.Props{Class: "channel-intro"},
		conversationAvatar(m, c),
		html.H2(html.Props{Text: m.tf(titleKey, map[string]string{"name": name})}),
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
		return formatDay(m.Locale, t, false)
	}
	return formatDay(m.Locale, t, true)
}

func message(m Model, h handlers, msg Message, continued bool) ui.Node {
	if chatremoveIsTombstone(msg) {
		return chatremoveTombstone(m, msg)
	}
	msg = agentAnnouncementProjectedIdentity(m, personaTrustedMessage(m, msg))
	if !msg.SentAt.IsZero() {
		msg.TimeLabel = chat5Clock(m.Locale, msg.SentAt)
	}
	if legacy, ok := legacyPrivateAnswerReceipt(m, msg); ok {
		return legacy
	}
	own := msg.PersonaActor == nil && msg.AuthorID != "" && msg.AuthorID == m.CurrentUser
	envelope := agentReplyEnvelope{}
	if msg.PersonaActor != nil && msg.PersonaActor.valid() {
		envelope = parseAgentReplyEnvelope(msg.Body)
		// The reader's text can be the stored body, with the token and the
		// model's own Sources list: show the answer in the one cleaned shape.
		envelope.Body = agentAnswerPresentBody(readerReplyBody(m, msg, envelope.Body), envelope.Sources)
	} else {
		envelope.Body = ReaderMessageBody(m, msg)
	}
	m.renderReferences = msg.PersonaReferences
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
	for _, emoji := range chatEmojiQuickReactions() {
		label := m.tf(KeyReactWith, map[string]string{"emoji": emoji})
		actions = append(actions, html.Button(html.Props{Class: "message-action quick-react", Type: "button", Disabled: m.Callbacks.ReactWith == nil,
			Data: map[string]string{"action": "react-with", "id": msg.ID, "emoji": emoji}, Aria: map[string]string{"label": label}, Title: label, Text: emoji}))
	}
	threadAvailable := m.Callbacks.OpenThread != nil && !m.selected().Agent
	actions = append(actions, chatsaveAction(m, msg, false))
	actions = append(actions,
		actionButton("message-action", "reply", msg.ID, m.t(KeyReply), !threadAvailable, icon("reply")),
		html.Button(html.Props{Class: "message-action", Type: "button", Disabled: !canReact, Data: map[string]string{"action": reactAction, "id": msg.ID}, Aria: map[string]string{"label": reactLabel, "pressed": boolString(msg.Reacted && reactAction != "react-pick"), "expanded": boolString(m.PickerID == msg.ID)}, Title: reactLabel}, icon(reactIcon)),
		html.Button(html.Props{Class: "message-action", Type: "button", Disabled: m.Callbacks.Pin == nil, Data: map[string]string{"action": pinAction, "id": msg.ID}, Aria: map[string]string{"label": pinLabel, "pressed": boolString(msg.Pinned)}, Title: pinLabel}, icon(pinIcon)),
		html.Button(html.Props{Class: "message-action", Type: "button", Hidden: m.Callbacks.OpenMenu == nil, Disabled: m.Callbacks.OpenMenu == nil, Data: map[string]string{"action": "menu", "id": msg.ID}, Aria: map[string]string{"label": m.t(KeyMore), "haspopup": "menu", "expanded": boolString(menuOpen)}, Title: m.t(KeyMore)}, icon("more")),
	)
	var menu ui.Node
	if menuOpen {
		items := chatMessageMenuItems(m, msg)
		menu = anchoredChatLayer(html.Props{Class: "message-menu", Role: "menu", Data: map[string]string{"message-menu": msg.ID}, Aria: map[string]string{"label": m.t(KeyMore)}}, "menu", items...)
	}
	body := ui.Node(html.Div(chatlangBodyProps(m, msg, html.Props{Class: "message-body", Dir: "auto"}), markdownMessageBody(m, envelope.Body)...))
	body = agentAnnouncementProjectedBody(m, readerAnnouncementMessage(m, msg), body)
	body = chatcmd002ProjectedBody(m, msg, body)
	if _, announcement := DecodeAnnouncementMessageBody(msg.Body); announcement && msg.PersonaActor.valid() {
		envelope = agentReplyEnvelope{} // Sources and attribution belong to the announcement hook.
	}
	if m.EditingID == msg.ID {
		editValue := msg.Body
		if m.EditDrafts != nil && m.EditDrafts[msg.ID] != "" {
			editValue = m.EditDrafts[msg.ID]
		}
		body = html.Form(html.Props{Class: "message-edit", OnSubmit: h.editSubmit},
			html.Label(html.Props{Class: "sr-only", For: "edit-" + msg.ID}, ui.Text(m.t(KeyEdit))),
			html.Textarea(html.Props{ID: "edit-" + msg.ID, Class: "edit-input", Data: map[string]string{"chat-value": editValue}, Rows: 2, OnInput: h.editInput, Aria: modAuthorDescribe(m, h.local, ModAuthorKeyEdit(msg.ID), "chatmod002-blocked-edit", nil)}),
			modAuthorLine(m, h.local, ModAuthorKeyEdit(msg.ID), "chatmod002-blocked-edit"),
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
		if msg.PersonaActor != nil {
			meta = append(meta, html.Strong(html.Props{Class: "message-author", Text: msg.Author}))
			meta = append(meta, chatux003MessageBadge(m, msg))
		} else {
			meta = append(meta, personButton(m, msg.AuthorID, msg.Author, "message-author person-name", ui.Text(msg.Author)))
		}
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
	if msg.PersonaActor != nil && msg.PersonaActor.valid() {
		lead = integrate1MessageAvatar(m, msg, "avatar")
	}
	if continued {
		lead = html.Div(html.Props{Class: "gutter-time", Aria: map[string]string{"hidden": "true"}, Text: msg.TimeLabel})
	}
	footer := []ui.Node{}
	if s := stats(m, msg); s != "" && !m.selected().Agent {
		footer = append(footer, html.Button(html.Props{Class: "message-stats", Type: "button", Disabled: !threadAvailable, Data: map[string]string{"action": "stats", "id": msg.ID}, Text: s}))
	}
	// Focusable so keyboard users reach the action toolbar and a tap on a
	// phone reveals it (actions are hidden there until the row has focus).
	contentChildren := []ui.Node{html.Div(html.Props{Class: "message-meta"}, meta...)}
	if context := agentQuestionContextIsolated(m, envelope, msg.TimeLabel); context != nil && m.selected().Agent {
		contentChildren = append(contentChildren, context)
	}
	contentChildren = append(contentChildren, body, modAuthorMaskedNote(m, msg, own), unresolvedMessageRecovery(m, msg))
	contentChildren = append(contentChildren, chatlangExtras(m, h, msg)...)
	contentChildren = append(contentChildren, renderAgentReplySources(m, envelope)...)
	if msg.PersonaActor != nil && msg.PersonaActor.valid() {
		feedback := renderAgentFeedback(m, h.local, msg.ID)
		contentChildren = append(contentChildren, feedback)
	}
	contentChildren = append(contentChildren, integrate2MessageLocations(m, msg)...)
	embedBody := envelope.Body
	if msg.PersonaActor.valid() {
		// The Sources row is the reference for an agent's documents: no second
		// preview card for a title the answer links.
		embedBody = agentAnswerPreviewBody(embedBody, envelope.Sources)
	}
	contentChildren = append(contentChildren, linkEmbeds(m, embedBody)...)
	contentChildren = append(contentChildren, docPreviewEmbeds(m, embedBody)...)
	contentChildren = append(contentChildren, projectPreviewEmbeds(m, embedBody)...)
	contentChildren = append(contentChildren, journeyPreviewEmbeds(m, embedBody)...)
	if len(msg.Attachments) > 0 {
		contentChildren = append(contentChildren, attachments(m, msg))
	}
	if chips := reactionRow(m, msg); chips != nil {
		contentChildren = append(contentChildren, chips)
	}
	contentChildren = append(contentChildren, html.Div(html.Props{Class: "message-footer"}, footer...))
	if !m.selected().Agent {
		contentChildren = append(contentChildren, personaReplyRowsForPost(m, h.local, msg.ID, time.Now())...)
	}
	children := []ui.Node{
		lead,
		html.Div(html.Props{Class: "message-content"}, contentChildren...),
	}
	if chatRowActionsVisible(h.local, msg.ID) || menuOpen || m.PickerID == msg.ID {
		children = append(children, html.Div(html.Props{Class: "message-actions", Role: "toolbar", Aria: map[string]string{"label": m.t(KeyMessageActions)}}, actions...))
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
	title := m.t(KeyCopyContents)
	if empty {
		title = m.t(KeyCopyContentsEmpty)
	}
	return html.Button(html.Props{Class: "menu-item", Type: "button", Role: "menuitem", Disabled: m.Callbacks.CopyContents == nil || empty, Title: title, Data: map[string]string{"action": "copy-contents", "id": msg.ID}}, icon("copy"), html.Span(html.Props{Text: m.t(KeyCopyContents)}))
}

func threadMessageMenu(m Model, msg Message) []ui.Node {
	menuID := "thread:" + msg.ID
	open := m.MenuID == menuID
	// The same order as the timeline's bar: Save for later, then More last.
	items := []ui.Node{chatsaveAction(m, msg, false)}
	items = append(items, html.Button(html.Props{Class: "message-action thread-more", Type: "button", Hidden: m.Callbacks.OpenMenu == nil, Disabled: m.Callbacks.OpenMenu == nil, Data: map[string]string{"action": "menu", "id": menuID}, Aria: map[string]string{"label": m.t(KeyMore), "haspopup": "menu", "expanded": boolString(open)}, Title: m.t(KeyMore)}, icon("more")))
	if open {
		items = append(items, anchoredChatLayer(html.Props{Class: "message-menu", Role: "menu", Data: map[string]string{"message-menu": menuID}, Aria: map[string]string{"label": m.t(KeyMore)}}, "menu", chatMessageMenuItems(m, msg)...))
	}
	return []ui.Node{html.Div(html.Props{Class: "message-actions thread-message-actions"}, items...)}
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
		label := m.tf(KeyReactionChip, map[string]string{"n": m.n(chip.Count), "emoji": chatEmojiSpoken(m, chip.Emoji)})
		items = append(items, html.Button(html.Props{Class: class, Type: "button", Disabled: m.Callbacks.ReactWith == nil && m.Callbacks.RemoveReactionWith == nil, Data: map[string]string{"action": "toggle-reaction", "id": msg.ID, "emoji": chip.Emoji}, Aria: map[string]string{"label": label, "pressed": boolString(chip.Mine)}, Title: label},
			html.Span(html.Props{Class: "reaction-emoji"}, chatEmojiGlyphNode(m, chip.Emoji)), html.Span(html.Props{Class: "reaction-count", Text: m.n(chip.Count)})))
	}
	if canPick && !emptyPicker {
		items = append(items, html.Button(html.Props{Class: "reaction add", Type: "button", Data: map[string]string{"action": "react-pick", "id": msg.ID}, Aria: map[string]string{"label": m.t(KeyReact), "expanded": boolString(pickerOpen)}, Title: m.t(KeyReact)}, icon("smile"), html.Span(html.Props{Text: "+"})))
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
					embed = LinkEmbed{State: "ready", SourceRoom: m.SelectedID, SourcePost: message.ID, Author: message.Author, Body: ReaderMessageBody(m, message), TimeLabel: message.TimeLabel, Channel: m.selected().Name}
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
				content = append(content, chatbug037AttachmentBadge(m, embed.AttachmentCount))
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
	if view, ok := m.ChannelStatuses[m.SelectedID]; ok {
		if notice := ChannelStatusComposerNotice(m, view); notice != nil {
			return notice
		}
	}
	id := "chat-composer"
	c := m.selected()
	target := displayName(m, c)
	if c.Kind == PublicChannel || c.Kind == PrivateChannel {
		target = "#" + target
	}
	placeholder := m.tf(KeyComposePlaceholder, map[string]string{"name": target})
	if c.Agent {
		placeholder = agentReplyFallback(m.Locale, "chat.agent.follow_up", "Ask {name} a follow-up")
		placeholder = strings.ReplaceAll(placeholder, "{name}", target)
	}
	if c.Name == "" {
		placeholder = m.t(KeyComposeUnselected)
	}
	// A refresh of an open room must not lock the composer: a keystroke or
	// Enter that lands while the box is disabled is silently dropped, and a
	// send used to trigger exactly such a refresh, eating the next message.
	disabled := m.SelectedID == "" || m.State == StateError
	if h.composerAgentName != "" {
		placeholder = ""
	}
	canSend := (m.Callbacks.SendMessage != nil || m.Callbacks.SendMessageWithReferences != nil) && !disabled && composerMessageReady(m.Draft) && m.Chatattach001.Ready()
	// The field points at the Enter hint only while the hint is drawn.
	fieldAria := map[string]string{}
	if composerHintVisible(m, h.local) {
		fieldAria["describedby"] = "composer-help"
	}
	fieldAria = modAuthorDescribe(m, h.local, ModAuthorKeyComposer(m.SelectedID), "chatmod002-blocked", fieldAria)
	return html.Form(html.Props{Class: "chat-composer", Data: map[string]string{"send-capable": boolString((m.Callbacks.SendMessage != nil || m.Callbacks.SendMessageWithReferences != nil) && !disabled && m.Chatattach001.Ready()), "format-row": composerFormatState(m, h.local)}, OnSubmit: h.composerSubmit, Aria: map[string]string{"label": m.t(KeyComposeRegion)}},
		html.Label(html.Props{Class: "sr-only", For: id}, ui.Text(m.t(KeyMessage))),
		mentionMenu(m, h.mentionView, id),
		docSuggestMenu(m, h.local.docSuggest, id),
		emojiCompletionMenu(m, h.local.emojiCompletion, id),
		composerCommandMenuView(m, h.local.commandMenu, id),
		composerNotice(h.local.composerNotice),
		chatcmd003PreviewFor(m, h.local.chatcmd003, id),
		html.Div(html.Props{Class: "composer-draft"}, composerAgentToken(h.composerAgentName),
			html.Textarea(html.Props{ID: id, Class: "composer-input", Name: "message", Placeholder: placeholder, Rows: 3, Dir: agentReplyDirection(m.Locale), Data: map[string]string{"chat-value": m.Draft, "draft-scope": m.SelectedID, "agent-indent": itoa((len([]rune(h.composerAgentName)) + 2) * 8)}, Disabled: disabled,
				OnInput: h.composerInput, OnKeyDown: h.composerKey,
				Aria: commandMenuFieldAria(h.local.commandMenu, id, emojiCompletionFieldAria(h.local.emojiCompletion, id, docSuggestFieldAria(h.local.docSuggest, id, mentionModelFieldAria(m, h.mentionView, id, fieldAria))))})),
		modAuthorLine(m, h.local, ModAuthorKeyComposer(m.SelectedID), "chatmod002-blocked"),
		chatComposerHint(m, h),
		chatlangComposerLine(m),
		chatattach001Drafts(m),
		html.Div(html.Props{Class: "composer-embeds"}, append(append(append(append(linkEmbeds(m, m.Draft), docPreviewEmbeds(m, m.Draft)...), projectPreviewEmbeds(m, m.Draft)...), journeyPreviewEmbeds(m, m.Draft)...), channelReferenceLinks(m, m.Draft)...)...),
		composerFormatRow(m, id, disabled),
		composerToolRow(m, h, id, disabled, canSend),
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
	return m.tf(KeyActiveOn, map[string]string{"date": formatShortDate(m.Locale, at.Local())})
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
	if m.searching() {
		return nil
	}
	if m.ShowPerson {
		return personPane(m)
	}
	if m.ShowThread && !m.selected().Agent {
		return threadPane(m, h)
	}
	return details(m, h)
}

func threadPane(m Model, h handlers) ui.Node {
	m = chatremoveThreadModel(m)
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
	if root != nil {
		trusted := personaTrustedMessage(m, *root)
		root = &trusted
	}
	roomName := displayName(m, m.selected())
	if k := m.selected().Kind; k == PublicChannel || k == PrivateChannel {
		roomName = "#" + roomName
	}
	// First-strong isolates keep "#general" whole inside a right-to-left
	// sentence; without them the "#" drifts to the far end of the name.
	roomName = "\u2068" + roomName + "\u2069"
	items := []ui.Node{chatux008Heading(m, roomName)}
	if root != nil {
		m.renderReferences = root.PersonaReferences
		body := ReaderMessageBody(m, *root)
		rootChildren := []ui.Node{html.Div(html.Props{Class: "message-meta"}, personButton(m, root.AuthorID, root.Author, "message-author person-name", ui.Text(root.Author)), html.Time(html.Props{Class: "message-time", Text: root.TimeLabel}),
			html.Button(html.Props{Class: "thread-view-in-channel", Type: "button", Data: map[string]string{"action": "reveal-thread-parent"}, Text: m.t(KeyViewInChannel)})), chatcmd002ProjectedBody(m, *root, html.Div(chatlangBodyProps(m, *root, html.Props{Class: "message-body", Dir: "auto"}), markdownMessageBody(m, body)...))}
		rootChildren = append(rootChildren, chatlangExtras(m, h, *root)...)
		rootChildren = append(rootChildren, integrate2MessageLocations(m, *root)...)
		rootChildren = append(rootChildren, linkEmbeds(m, body)...)
		if root.PersonaActor != nil {
			rootChildren = append(rootChildren, PersonaBadgeLocalized(m.Locale, root.PersonaActor))
		}
		rootChildren = append(rootChildren, docPreviewEmbeds(m, body)...)
		rootChildren = append(rootChildren, projectPreviewEmbeds(m, body)...)
		rootChildren = append(rootChildren, journeyPreviewEmbeds(m, body)...)
		if len(root.Attachments) > 0 {
			rootChildren = append(rootChildren, attachments(m, *root))
		}
		if chips := reactionRow(m, *root); chips != nil {
			rootChildren = append(rootChildren, chips)
		}
		rootChildren = append(rootChildren, threadMessageMenu(m, *root)...)
		items = append(items, html.Div(html.Props{Class: "thread-root"}, integrate1MessageAvatar(m, *root, "avatar small"), html.Div(html.Props{Class: "thread-root-body"}, rootChildren...)))
	}
	if m.ThreadHasOlder {
		items = append(items, html.Button(html.Props{Class: "button secondary small load-older", Type: "button", Disabled: m.Callbacks.LoadOlderThread == nil, Data: map[string]string{"action": "load-older-thread"}, Text: m.t(KeyLoadOlderThread)}))
	}
	agentRows := personaReplyRowsForPost(m, h.local, m.ThreadParentID, time.Now())
	if m.ThreadLoading && len(m.ThreadMessages) == 0 && len(agentRows) == 0 {
		items = append(items, html.Div(html.Props{Class: "thread-loading", Role: "status", Aria: map[string]string{"busy": "true"}},
			html.Div(html.Props{Class: "thread-loading-skeleton", Aria: map[string]string{"hidden": "true"}},
				html.Div(html.Props{Class: "thread-loading-row"},
					html.Span(html.Props{Class: "chat-skeleton thread-loading-avatar"}),
					html.Div(html.Props{Class: "thread-loading-lines"}, html.Span(html.Props{Class: "chat-skeleton"}), html.Span(html.Props{Class: "chat-skeleton"}))),
				html.Div(html.Props{Class: "thread-loading-row"},
					html.Span(html.Props{Class: "chat-skeleton thread-loading-avatar"}),
					html.Div(html.Props{Class: "thread-loading-lines"}, html.Span(html.Props{Class: "chat-skeleton"}), html.Span(html.Props{Class: "chat-skeleton"})))),
			html.P(html.Props{Class: "thread-empty", Text: m.t(KeyThreadLoading)})))
	} else if len(m.ThreadMessages) == 0 && len(agentRows) == 0 {
		items = append(items, html.P(html.Props{Class: "thread-empty", Text: m.t(KeyThreadEmpty)}))
	} else {
		replies := append([]ui.Node(nil), agentRows...)
		for _, msg := range m.ThreadMessages {
			msg = personaTrustedMessage(m, msg)
			if _, ok := legacyPrivateAnswerReceipt(m, msg); ok {
				continue
			}
			m.renderReferences = msg.PersonaReferences
			if agentContent, isAgent := chatux003ThreadAgentReply(m, h, msg); isAgent {
				replies = append(replies, html.Div(html.Props{Class: "thread-message", Data: map[string]string{"message-id": msg.ID}}, integrate1MessageAvatar(m, msg, "avatar small"), html.Div(html.Props{Class: "thread-message-body"}, agentContent...)))
				continue
			}
			body := ReaderMessageBody(m, msg)
			content := []ui.Node{html.Div(html.Props{Class: "message-meta"}, personButton(m, msg.AuthorID, msg.Author, "message-author person-name", ui.Text(msg.Author)), html.Time(html.Props{Class: "message-time", Text: msg.TimeLabel})), chatcmd002ProjectedBody(m, msg, html.Div(chatlangBodyProps(m, msg, html.Props{Class: "message-body", Dir: "auto"}), markdownMessageBody(m, body)...))}
			content = append(content, chatlangExtras(m, h, msg)...)
			content = append(content, integrate2MessageLocations(m, msg)...)
			content = append(content, linkEmbeds(m, body)...)
			if msg.PersonaActor != nil {
				content = append(content, PersonaBadgeLocalized(m.Locale, msg.PersonaActor))
			}
			content = append(content, docPreviewEmbeds(m, body)...)
			content = append(content, projectPreviewEmbeds(m, body)...)
			content = append(content, journeyPreviewEmbeds(m, body)...)
			content = append(content, threadMessageMenu(m, msg)...)
			replies = append(replies, html.Div(html.Props{Class: "thread-message", Data: map[string]string{"message-id": msg.ID}}, integrate1MessageAvatar(m, msg, "avatar small"), html.Div(html.Props{Class: "thread-message-body"}, content...)))
		}
		visibleCount := 0
		for _, msg := range m.ThreadMessages {
			if _, receipt := legacyPrivateAnswerReceipt(m, msg); !receipt {
				visibleCount++
			}
		}
		if visibleCount > 0 {
			items = append(items, html.Div(html.Props{Class: "thread-count", Role: "separator"}, html.Span(html.Props{Text: stats(m, Message{Replies: visibleCount})})))
		}
		items = append(items, html.Div(html.Props{Class: "thread-replies"}, replies...))
		if len(agentRows) > 0 {
			items = append(items, html.P(html.Props{Class: "thread-agent-continue", Text: agentThreadFollowUpHint(m)}))
		}
	}
	if m.ThreadHasNewer {
		items = append(items, html.Button(html.Props{Class: "button secondary small load-newer", Type: "button", Disabled: m.Callbacks.LoadNewerThread == nil, Data: map[string]string{"action": "load-newer-thread"}, Text: m.t(KeyLoadNewerThread)}))
	}
	heading := items[0]
	body := html.Div(html.Props{Class: "thread-scroll", Data: map[string]string{"chat-thread-anchor": m.SelectedID + ":" + m.ThreadParentID}}, items[1:]...)
	canReply := (m.Callbacks.ReplyInThread != nil || (m.Callbacks.ReplyInThreadWithReferences != nil && len(personaMentionCandidates(m, "")) > 0)) && m.ThreadParentID != ""
	composer := html.Form(html.Props{Class: "thread-composer", Data: map[string]string{"send-capable": boolString(canReply)}, OnSubmit: h.threadSubmit, Aria: map[string]string{"label": m.t(KeyReplySend)}},
		chatcmd003PreviewFor(m, h.local.chatcmd003, "thread-composer"),
		html.Label(html.Props{Class: "sr-only", For: "thread-composer"}, ui.Text(m.t(KeyReplySend))),
		mentionMenu(m, h.mentionView, "thread-composer"),
		emojiCompletionMenu(m, h.local.emojiCompletion, "thread-composer"),
		html.Textarea(html.Props{ID: "thread-composer", Class: "composer-input", Name: "reply", Placeholder: agentThreadPlaceholder(m), Rows: 1, Dir: agentReplyDirection(m.Locale), Disabled: !canReply, Data: map[string]string{"chat-value": m.ThreadDrafts[m.ThreadParentID]}, OnKeyDown: h.threadKey, OnInput: h.threadInput,
			Aria: modAuthorDescribe(m, h.local, ModAuthorKeyReply(m.ThreadParentID), "chatmod002-blocked-reply", emojiCompletionFieldAria(h.local.emojiCompletion, "thread-composer", mentionModelFieldAria(m, h.mentionView, "thread-composer", nil)))}),
		modAuthorLine(m, h.local, ModAuthorKeyReply(m.ThreadParentID), "chatmod002-blocked-reply"),
		composerAgentReplyHint(h.threadReplyHint),
		html.Div(html.Props{Class: "composer-toolbar"},
			formatToolbar(m, "thread-composer", !canReply),
			chattoneToolbar(m, "thread-composer", !canReply),
			chatEmojiComposerPicker(m, h.local, "thread-composer", !canReply),
			giphyPickerControl(m, "thread-composer", !canReply),
			html.Span(html.Props{Class: "composer-help kbd-hint", Text: m.t(KeyComposeHint)}),
			html.Button(html.Props{Class: "send-button", Type: "submit", Disabled: true, Aria: map[string]string{"label": m.t(KeyReplySend), "disabled": "true"}, Title: m.t(KeyReplySend)}, icon("send"), html.Span(html.Props{Class: "send-label", Text: m.t(KeyReplySend)})),
		),
	)
	return html.Aside(html.Props{Class: "chat-side thread-pane", Role: "complementary", Aria: map[string]string{"label": m.t(KeyThreadRegion)}}, paneHandle(m, "details"), heading, body, composer)
}

func details(m Model, h handlers) ui.Node {
	if !m.ShowDetails {
		return html.Aside(html.Props{Class: "chat-side chat-details collapsed", Aria: map[string]string{"label": m.t(KeyDetails), "hidden": "true"}})
	}
	return chatux005Details(m, h)
}

// integrationsSection is CHAT-08's Copy API curl move out of the everyday
// More-options menu: a developer-facing action belongs somewhere a reader
// has to go looking for it, described rather than dropped bare into a list,
// and visible only to the people who may create an app token: the workspace
// administrators and this one conversation's owner or managers -- not every
// member. The button says what it copies in plain words.
func integrationsSection(m Model, c Conversation) ui.Node {
	if !canAdministerConversation(m, c) {
		return nil
	}
	return html.Section(html.Props{Class: "details-section integrations-section"},
		html.H3(html.Props{Text: m.t(KeyIntegrationsTitle)}),
		html.P(html.Props{Class: "field-hint", Text: m.t(KeyIntegrationsHint)}),
		html.Button(html.Props{Class: "button secondary small", Type: "button", Disabled: m.Callbacks.CopyConversationAPICurl == nil, Data: map[string]string{"action": "copy-conversation-api-curl", "id": c.ID}}, icon("copy"), html.Span(html.Props{Text: chatdetailsText(m, "integrations_copy")})),
	)
}

// addMembersButton is nil (renders nothing) for a DM, which has no
// membership to add to, and for a private channel or group the viewer does
// not own.
func addMembersButton(m Model, c Conversation) ui.Node {
	if c.Kind == DirectMessage {
		return nil
	}
	allowed := c.Kind == PublicChannel || (m.CurrentUser != "" && c.OwnerID == m.CurrentUser)
	if !allowed {
		return nil
	}
	return actionButton("icon-button", "open-add-members", "", m.t(KeyAddPeople), m.Callbacks.OpenAddMembers == nil, icon("people"))
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
	if text := modAuthorErrorText(m, m.ChannelTodoError); text != "" {
		return text
	}
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

func channelTodoTrigger(m Model, h handlers) ui.Node {
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
	// C-4: this button's pressed state is its own tray ("todo"), not the
	// unrelated Details pane -- it used to read as pressed whenever Details
	// happened to be open and never otherwise.
	return html.Button(html.Props{Class: "channel-todo-trigger", Type: "button", Disabled: m.Callbacks.OpenChannelTodo == nil,
		Data: map[string]string{"action": "open-todo"}, Aria: map[string]string{"label": label, "pressed": boolString(h.local.tray == "todo"), "busy": boolString(m.ChannelTodoLoading)}, Title: label},
		icon("checklist"), html.Span(html.Props{Class: "channel-todo-trigger-label", Text: m.t(KeyTodoTitle)}),
		html.Span(html.Props{Class: "channel-todo-count", Text: count}))
}

func channelPollTrigger(m Model, h handlers) ui.Node {
	label := m.t(KeyPollTitle)
	class := "channel-poll-trigger"
	children := []ui.Node{icon("poll"), html.Span(html.Props{Class: "channel-poll-trigger-label", Text: m.t(KeyPollTitle)})}
	if m.ChannelPoll.Question != "" {
		label += ": " + m.ChannelPoll.Question
		class += " active"
		children = append(children, html.Span(html.Props{Class: "channel-poll-active-indicator", Aria: map[string]string{"hidden": "true"}, Text: "•"}))
	}
	return html.Button(html.Props{Class: class, Type: "button", Disabled: m.Callbacks.OpenChannelPoll == nil,
		// Pressed exactly while the poll tray is the open one; "active" only says a
		// poll exists, and is drawn as a dot, never as a pressed button.
		Data: map[string]string{"action": "open-poll", "id": m.SelectedID}, Aria: map[string]string{"label": label, "pressed": boolString(h.local.tray == "poll")}, Title: label}, children...)
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
					copyLabel := chatux005Text(m, "pin_copy")
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
	} else {
		if len(items) == 0 {
			content = append(content, html.P(html.Props{Class: "muted", Text: chat5Text(m, "chat.todo.empty_next")}))
		}
		content = append(content, html.Ul(html.Props{Class: "channel-todo-list"}, items...))
		pinOptions := []ui.Node{html.Option(html.Props{Raw: map[string]any{"value": ""}, Text: m.t(KeyTodoNoPin), Selected: m.ChannelTodoSourcePin == ""})}
		for _, pin := range m.ChannelPins {
			pinOptions = append(pinOptions, html.Option(html.Props{Value: pin.PostID, Text: excerpt(readerText(m, pin.PostID, pin.Body, pin.Revision), 70), Selected: m.ChannelTodoSourcePin == pin.PostID}))
		}
		// The task field and Add sit on one row; the linked message and the
		// completion rule are options most tasks never change.
		content = append(content, html.Form(html.Props{Class: "channel-todo-form", OnSubmit: h.todoSubmit},
			html.Div(html.Props{Class: "channel-todo-add-row"},
				html.Label(html.Props{Class: "sr-only", For: "chat-todo-new", Text: m.t(KeyTodoNew)}),
				html.Input(html.Props{ID: "chat-todo-new", Class: "chat-input", Type: "text", MaxLength: 500, Required: true, Placeholder: m.t(KeyTodoNew), Data: map[string]string{"chat-value": m.ChannelTodoDraft}, OnInput: h.todoDraft, Disabled: m.Callbacks.AddChannelTodo == nil || m.ChannelTodoPending}),
				// CHAT-07: an empty title submitted quietly (todoSubmit
				// returns without calling AddChannelTodo), so Add looked
				// broken instead of merely declining a blank task.
				html.Button(html.Props{Class: "button small", Type: "submit", Disabled: m.Callbacks.AddChannelTodo == nil || m.ChannelTodoPending || m.ChannelTodoError != "" || strings.TrimSpace(m.ChannelTodoDraft) == "", Text: chat5Text(m, "chat.todo.add_next")})),
			chatPolishDisclosure(html.Props{Class: "channel-todo-options"},
				chatPolishDisclosureLabel(html.Props{Text: m.t(KeyTodoMoreOptions)}),
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
		copyLabel := chatux005Text(m, "pin_copy")
		if m.PinReferenceUnavailable {
			copyLabel = m.t(KeyPinCopyGuestUnavailable)
		}
		rows = append(rows, html.Li(html.Props{Class: "pinned-row"},
			html.Div(html.Props{Class: "pinned-link"},
				html.Strong(html.Props{Text: msg.Author}),
				html.Span(html.Props{Text: excerpt(readerText(m, msg.PostID, msg.Body, msg.Revision), 96)})),
			html.Div(html.Props{Class: "pin-actions"},
				actionButton("button secondary small", "pin-jump", msg.PostID, m.t(KeyPinJump), m.Callbacks.JumpToPin == nil || msg.Sequence == 0, ui.Text(m.t(KeyPinJump))),
				actionButton("button secondary small", "pin-copy", msg.PostID, copyLabel, m.Callbacks.CopyPinReference == nil || m.PinReferenceUnavailable, ui.Text(copyLabel)))))
	}
	if len(rows) == 0 {
		return html.Span(html.Props{Class: "pinned-slot"})
	}
	return html.Section(html.Props{ID: "chat-details-pinned", Class: "details-section", TabIndex: -1, Aria: map[string]string{"label": m.t(KeyPinned)}},
		html.H3(html.Props{Text: countedLabel(m, m.t(KeyPinned), len(rows))}),
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
		preview = append(preview, html.Strong(html.Props{Text: msg.Author}), html.P(html.Props{Dir: "auto", Text: ReaderMessageBody(m, *msg)}))
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
		if chatvoiceMediaType(a.ContentType) {
			tiles = append(tiles, chatvoiceAttachment(m, msg, a))
			continue
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
				children = append(children, html.Button(html.Props{Class: "attachment-image-open", Type: "button", Data: media, Aria: map[string]string{"label": m.t(KeyOpenImage) + ": " + name}, Title: m.t(KeyOpenImage)}, html.Img(html.Props{Alt: name, OnError: chatAttachmentImageErrorHandler(), Raw: raw})))
				if m.Callbacks.DownloadAttachment != nil {
					children = append(children, html.Button(html.Props{Class: "attachment-download", Type: "button", Data: map[string]string{"action": "download-attachment", "id": msg.ID, "extra": a.ID}, Aria: map[string]string{"label": m.t(KeyDownloadAttachment) + ": " + name}, Text: m.t(KeyDownloadAttachment)}))
				}
				// C-2: a load failure (a stale grant, a fetch abort, a decode
				// error) must not leave the broken-image glyph on screen. This
				// fallback is pre-rendered and hidden; chatAttachmentImageErrorHandler
				// toggles the sibling ".failed" class that swaps visibility, so
				// nothing here mutates the DOM imperatively.
				// CHATBUG-031: while no bytes have arrived the tile shows a neutral
				// placeholder carrying the file name (the stylesheet shows it only
				// for an <img> with no src that has not failed), and the failed
				// fallback names the file beside its one action.
				loading := []ui.Node{icon("attach")}
				fallback := []ui.Node{html.Span(html.Props{Text: m.t(KeyAttachmentUnavailable)})}
				if a.Name != "" {
					loading = append(loading, html.Span(html.Props{Class: "attachment-loading-name", Dir: "auto", Text: a.Name}))
					fallback = append(fallback, html.Span(html.Props{Class: "attachment-fallback-name", Dir: "auto", Text: a.Name}))
				}
				loading = append(loading, html.Span(html.Props{Class: "attachment-loading-state", Text: m.t(KeyAttachmentLoading)}))
				children = append(children, html.Div(html.Props{Class: "attachment-loading", Aria: map[string]string{"hidden": "true"}}, loading...))
				children = append(children, html.Div(html.Props{Class: "attachment-pending attachment-fallback", Role: "status"},
					append(fallback, html.Button(html.Props{Class: "attachment-download", Type: "button", Disabled: m.Callbacks.DownloadAttachment == nil, Data: map[string]string{"action": "download-attachment", "id": msg.ID, "extra": a.ID}, Aria: map[string]string{"label": m.t(KeyDownloadAttachment) + ": " + name}, Text: m.t(KeyDownloadAttachment)}))...))
			} else if a.PreviewUnavailable || a.URL != "" {
				children = append(children, html.Div(html.Props{Class: "attachment-pending", Role: "status"},
					html.Span(html.Props{Text: m.t(KeyAttachmentUnavailable)}),
					html.Button(html.Props{Class: "attachment-download", Type: "button", Disabled: m.Callbacks.DownloadAttachment == nil, Data: map[string]string{"action": "download-attachment", "id": msg.ID, "extra": a.ID}, Aria: map[string]string{"label": m.t(KeyDownloadAttachment) + ": " + name}, Text: m.t(KeyDownloadAttachment)})))
			} else {
				children = append(children, html.Div(html.Props{Class: "attachment-pending image-loading-skeleton", Role: "status", Aria: map[string]string{"label": m.t(KeyAttachmentLoading), "live": "polite"}}, icon("attach")))
			}
			if a.IsGIF() {
				children = append(children, html.Span(html.Props{Class: "attachment-badge", Text: m.t(KeyGIF)}))
			}
			// C-2 live re-check: nothing here carried a stable key, so an
			// unrelated re-render (a stream event, a reaction, anything that
			// touches this message's props at all) was free to let the
			// reconciler recreate this figure's <button>/<img> subtree. The
			// image loader's in-flight fetch (image_loading_js.go) captures
			// that DOM node by reference; a swap mid-fetch orphaned the
			// blob it eventually got back -- the node the objectURL was
			// meant for no longer existed; it got revoked instead of
			// applied. On a quiet fixture (no live stream) this never
			// reproduced, only against a real server pushing continuous
			// updates. Keying the figure by artifact ID is what every other
			// list in this file already does to stop exactly this class of
			// reconciler churn.
			tiles = append(tiles, html.WithKey(html.Figure(html.Props{Class: class, Data: data, Title: name, Raw: frame}, children...), "attachment:"+a.ID))
			continue
		}
		tiles = append(tiles, chatattach001File(m, msg, a))
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

// directMessageSubtitle is the line under a DM's name (round 4 C-7): the
// conversation's topic when it has one, else the person's job title when
// their directory card is loaded, else "Direct message", so a DM header
// carries a subtitle the way a channel's "Public channel · 19 members" does.
func directMessageSubtitle(m Model, c Conversation, peer string) string {
	if topic := strings.TrimSpace(c.Topic); topic != "" {
		return topic
	}
	if p := m.PersonDetails; p != nil && p.ID == peer && strings.TrimSpace(p.JobTitle) != "" {
		return strings.TrimSpace(p.JobTitle)
	}
	return m.t(KeyKindDirect)
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
	return countedLabel(m, m.t(KeyMembers), n)
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
	// A conversation whose record carries no name, or only its own identifier
	// (the client falls back to the identifier until a directory answers), has
	// no name to show yet. The empty string makes every caller use its neutral
	// placeholder and an initials-free avatar; an identifier is never a label.
	// A channel is named by people and may legitimately be named like its slug;
	// a direct or group room is named after its participants, so an identifier
	// there is only ever the placeholder the client fell back to.
	if known := strings.TrimSpace(c.Name); known == "" || ((c.Kind == DirectMessage || c.Kind == GroupChat) && (known == strings.TrimSpace(c.ID) || (c.AgentID != "" && known == strings.TrimSpace(c.AgentID)))) {
		return ""
	}
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
