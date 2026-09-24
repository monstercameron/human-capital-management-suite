//go:build js && wasm

package main

import (
	"strconv"
	"syscall/js"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

const chatHistoryStateField = "hcmChatNavigation"

type chatHistoryController struct {
	restoring bool
	seeded    bool
}

var chatHistory = &chatHistoryController{}

func (controller *chatHistoryController) seed(model chatui.Model) {
	if controller == nil || controller.seeded {
		return
	}
	state := chatNavigationStateFromModel(model)
	if !validChatNavigationState(state) {
		return
	}
	history, ok := browserHistory()
	if !ok {
		return
	}
	current, ok := browserHistoryState(history)
	if !ok {
		return
	}
	if existing, valid := readChatNavigationState(current); valid && existing == state {
		controller.seeded = true
		return
	}
	clone, cloneOK := cloneBrowserHistoryState(current)
	if !cloneOK {
		return
	}
	clone.Set(chatHistoryStateField, encodeChatNavigationState(state))
	if !browserHistoryReplaceState(history, clone, browserLocationHref()) {
		return
	}
	controller.seeded = true
}

func (controller *chatHistoryController) reset() {
	if controller != nil {
		controller.seeded = false
		controller.restoring = false
	}
}

func (controller *chatHistoryController) push(model chatui.Model) {
	controller.pushState(chatNavigationStateFromModel(model))
}

func (controller *chatHistoryController) pushState(state chatNavigationState) {
	if controller == nil || controller.restoring {
		return
	}
	if !validChatNavigationState(state) {
		return
	}
	history, ok := browserHistory()
	if !ok {
		return
	}
	current, ok := browserHistoryState(history)
	if !ok {
		return
	}
	if existing, valid := readChatNavigationState(current); valid && existing == state {
		return
	}
	clone, cloneOK := cloneBrowserHistoryState(current)
	if !cloneOK {
		return
	}
	clone.Set(chatHistoryStateField, encodeChatNavigationState(state))
	if _, ok := browserCall(history, "pushState", clone, "", browserLocationHref()); !ok {
		return
	}
	productHistory.RecordSameRoutePush()
}

func encodeChatNavigationState(state chatNavigationState) js.Value {
	entry := js.Global().Get("Object").New()
	entry.Set("tenant", state.OwnerTenantID)
	entry.Set("subject", state.OwnerSubject)
	entry.Set("conversation", state.ConversationID)
	entry.Set("details", state.ShowDetails)
	entry.Set("thread", state.ShowThread)
	entry.Set("parent", state.ThreadParentID)
	entry.Set("person", state.PersonID)
	entry.Set("focus", state.FocusMessageID)
	entry.Set("focusSequence", strconv.FormatUint(state.FocusSequence, 10))
	return entry
}

// replace annotates the current same-route entry. Hash links have already
// created their own browser entry, so they must not add a second Back stop.
func (controller *chatHistoryController) replace(model chatui.Model) {
	if controller == nil || controller.restoring {
		return
	}
	state := chatNavigationStateFromModel(model)
	if !validChatNavigationState(state) {
		return
	}
	history, ok := browserHistory()
	if !ok {
		return
	}
	current, ok := browserHistoryState(history)
	if !ok {
		return
	}
	if existing, valid := readChatNavigationState(current); valid && existing == state {
		return
	}
	clone, cloneOK := cloneBrowserHistoryState(current)
	if !cloneOK {
		return
	}
	clone.Set(chatHistoryStateField, encodeChatNavigationState(state))
	browserHistoryReplaceState(history, clone, browserLocationHref())
}

func (controller *chatHistoryController) restore(event js.Value, cfg journeyclient.Config) {
	if controller == nil {
		return
	}
	defer refreshProductHistoryControls()
	historyState, ok := browserProperty(event, "state")
	if !ok {
		return
	}
	state, ok := readChatNavigationState(historyState)
	if !ok || state.OwnerTenantID == "" || state.OwnerSubject == "" || state.OwnerTenantID != cfg.Tenant || state.OwnerSubject != cfg.Subject {
		return
	}
	controller.restoring = true
	defer func() { controller.restoring = false }()
	current := chatBrowser.snapshot()
	if state.ConversationID != "" && (current.SelectedID != state.ConversationID || current.FocusMessageID != state.FocusMessageID) {
		openChatConversationAt(cfg, state.ConversationID, state.FocusSequence, state.FocusMessageID)
	}
	current = chatBrowser.snapshot()
	if state.ConversationID != "" && current.SelectedID != state.ConversationID {
		// A rejected or unavailable room cannot own the details or thread state
		// from this history entry. The router still handles the popstate normally.
		return
	}
	if current.ShowDetails != state.ShowDetails && current.Callbacks.ToggleDetails != nil {
		current.Callbacks.ToggleDetails(state.ShowDetails)
	}
	current = chatBrowser.snapshot()
	if state.ShowThread && (!current.ShowThread || current.ThreadParentID != state.ThreadParentID) && current.Callbacks.OpenThread != nil {
		current.Callbacks.OpenThread(state.ThreadParentID)
	} else if !state.ShowThread && current.ShowThread && current.Callbacks.CloseThread != nil {
		current.Callbacks.CloseThread()
	}
	current = chatBrowser.snapshot()
	currentPersonID := ""
	if current.PersonDetails != nil {
		currentPersonID = current.PersonDetails.ID
	}
	if state.PersonID != "" && (!current.ShowPerson || currentPersonID != state.PersonID) && current.Callbacks.OpenPerson != nil {
		current.Callbacks.OpenPerson(state.PersonID)
	} else if state.PersonID == "" && current.ShowPerson && current.Callbacks.ClosePerson != nil {
		current.Callbacks.ClosePerson()
	}
}

func readChatNavigationState(historyState js.Value) (chatNavigationState, bool) {
	entry, ok := browserProperty(historyState, chatHistoryStateField)
	if !ok || entry.Type() != js.TypeObject {
		return chatNavigationState{}, false
	}
	tenant, tenantOK := browserProperty(entry, "tenant")
	subject, subjectOK := browserProperty(entry, "subject")
	conversation, conversationOK := browserProperty(entry, "conversation")
	details, detailsOK := browserProperty(entry, "details")
	thread, threadOK := browserProperty(entry, "thread")
	parent, parentOK := browserProperty(entry, "parent")
	person, personOK := browserProperty(entry, "person")
	focus, focusOK := browserProperty(entry, "focus")
	focusSequence, focusSequenceOK := browserProperty(entry, "focusSequence")
	if !tenantOK || !subjectOK || !conversationOK || !detailsOK || !threadOK || !parentOK || tenant.Type() != js.TypeString || subject.Type() != js.TypeString || conversation.Type() != js.TypeString || details.Type() != js.TypeBoolean || thread.Type() != js.TypeBoolean || parent.Type() != js.TypeString || (personOK && person.Type() != js.TypeString) || (focusOK && focus.Type() != js.TypeString) || (focusSequenceOK && focusSequence.Type() != js.TypeString) {
		return chatNavigationState{}, false
	}
	state := chatNavigationState{OwnerTenantID: tenant.String(), OwnerSubject: subject.String(), ConversationID: conversation.String(), ShowDetails: details.Bool(), ShowThread: thread.Bool(), ThreadParentID: parent.String()}
	if personOK {
		state.PersonID = person.String()
	}
	if focusOK {
		state.FocusMessageID = focus.String()
	}
	if focusSequenceOK {
		sequence, err := strconv.ParseUint(focusSequence.String(), 10, 64)
		if err != nil {
			return chatNavigationState{}, false
		}
		state.FocusSequence = sequence
	}
	return state, validChatNavigationState(state)
}
