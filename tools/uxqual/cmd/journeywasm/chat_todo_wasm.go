//go:build js && wasm

package main

import (
	"context"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// A todo RPC finishes outside the UI event loop. PostAsync owns the repaint
// so the completed state reaches the DOM without waiting for another click.
func refreshChannelTodoRoute(room string, generation uint64) {
	epoch := chatRerenderEpoch
	ui.PostAsync(func() {
		if chatRerender != nil && chatRerenderEpoch == epoch && chatBrowser.generationActive(generation) && chatBrowser.selectedID() == room {
			chatRerender()
		}
	})
}

func channelTodoClient() chatv1.ChatExtensionsServiceClient {
	chatRecipientBrowser.Lock()
	defer chatRecipientBrowser.Unlock()
	return chatRecipientBrowser.client
}

func channelTodoModel(list *chatv1.ChannelTodoList) chatui.ChannelTodoList {
	result := chatui.ChannelTodoList{Revision: list.GetRevision(), Pinned: list.GetPinned()}
	for _, item := range list.GetItems() {
		if item != nil {
			projected := chatui.ChannelTodoItem{ID: item.GetId(), Text: item.GetText(), SourcePostID: item.GetSourcePostId(), Completed: item.GetCompleted(), CompletedBySubjectID: item.GetCompletedBySubjectId(), CompletedByHomeTenantID: item.GetCompletedByHomeTenantId(), CompletedAtUnix: item.GetCompletedAtUnix(), CompletionMode: item.GetCompletionMode(), CanToggle: item.GetCanToggle(), CanManageCompletionPolicy: item.GetCanManageCompletionPolicy()}
			for _, selected := range item.GetSelectedCompleters() {
				if selected != nil {
					projected.SelectedCompleters = append(projected.SelectedCompleters, chatui.ChannelTodoSelectedMember{HomeTenantID: selected.GetHomeTenantId(), SubjectID: selected.GetSubjectId()})
				}
			}
			result.Items = append(result.Items, projected)
		}
	}
	return result
}

func loadChannelTodo(cfg journeyclient.Config, room string) {
	allowed := false
	for _, conversation := range chatBrowser.snapshot().Conversations {
		if conversation.ID == room && (conversation.Kind == chatui.PublicChannel || conversation.Kind == chatui.PrivateChannel) && conversation.Joined {
			allowed = true
			break
		}
	}
	if !allowed {
		return
	}
	client := channelTodoClient()
	if client == nil || room == "" {
		return
	}
	active := chatBrowser.config(cfg)
	host := channelTodoHost(room, active.Tenant)
	generation := chatBrowser.currentGeneration()
	chatBrowser.mutate(func(model *chatui.Model) {
		if model.SelectedID == room {
			model.ChannelTodoLoading = true
		}
	})
	refreshChannelTodoRoute(room, generation)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	response, err := client.GetChannelTodoList(chatRPCContext(ctx, active), &chatv1.GetChannelTodoListRequest{ConversationId: room, HostTenantId: host})
	currentConfig := chatBrowser.config(journeyclient.Config{})
	if active.Tenant != currentConfig.Tenant || active.Subject != currentConfig.Subject || active.Bearer != currentConfig.Bearer || generation != chatBrowser.currentGeneration() {
		return
	}
	chatBrowser.mutate(func(model *chatui.Model) {
		if model.SelectedID != room {
			return
		}
		model.ChannelTodoLoading = false
		if err != nil || response.GetList() == nil || response.GetList().GetConversationId() != room {
			model.ChannelTodoError = "load"
			return
		}
		if response.GetList().GetRevision() < model.ChannelTodo.Revision {
			return
		}
		model.ChannelTodo = channelTodoModel(response.GetList())
		model.ChannelTodoError = ""
	})
	refreshChannelTodoRoute(room, generation)
}

func channelTodoHost(room, fallback string) string {
	for _, conversation := range chatBrowser.snapshot().Conversations {
		if conversation.ID == room && conversation.HostTenantID != "" {
			return conversation.HostTenantID
		}
	}
	return fallback
}

type chatTodoPolicy struct {
	mode     string
	selected []chatui.ChannelTodoSelectedMember
}

func applyChatTodoPolicy(request *chatv1.MutateChannelTodoListRequest, policy chatTodoPolicy) {
	request.CompletionMode = policy.mode
	if policy.mode != "ME_AND_SELECTED" {
		return
	}
	for _, selected := range policy.selected {
		request.SelectedCompleters = append(request.SelectedCompleters, &chatv1.ChannelTodoSelectedMember{HomeTenantId: selected.HomeTenantID, SubjectId: selected.SubjectID})
	}
}

func mutateChannelTodo(cfg journeyclient.Config, room, operation, itemID, text, sourcePostID string, completed, pinned bool, policy ...chatTodoPolicy) {
	client := channelTodoClient()
	if client == nil || room == "" {
		return
	}
	active := chatBrowser.config(cfg)
	host := channelTodoHost(room, active.Tenant)
	generation := chatBrowser.currentGeneration()
	model := chatBrowser.snapshot()
	if model.SelectedID != room || model.ChannelTodoPending || model.ChannelTodoLoading || model.ChannelTodo.Revision == 0 {
		return
	}
	chatBrowser.mutate(func(model *chatui.Model) {
		if model.SelectedID == room {
			model.ChannelTodoPending = true
		}
	})
	refreshChannelTodoRoute(room, generation)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	request := &chatv1.MutateChannelTodoListRequest{
		ConversationId: room, HostTenantId: host, ExpectedRevision: model.ChannelTodo.Revision, Operation: operation,
		ItemId: itemID, Text: text, SourcePostId: sourcePostID, Completed: completed, Pinned: pinned,
	}
	if len(policy) > 0 {
		applyChatTodoPolicy(request, policy[0])
	}
	response, err := client.MutateChannelTodoList(chatRPCContext(ctx, active), request)
	currentConfig := chatBrowser.config(journeyclient.Config{})
	if active.Tenant != currentConfig.Tenant || active.Subject != currentConfig.Subject || active.Bearer != currentConfig.Bearer || generation != chatBrowser.currentGeneration() {
		return
	}
	chatBrowser.mutate(func(current *chatui.Model) {
		if current.SelectedID != room {
			return
		}
		current.ChannelTodoPending = false
		if err != nil || response.GetList() == nil || response.GetList().GetConversationId() != room {
			current.ChannelTodoError = "save"
			return
		}
		if response.GetList().GetRevision() < current.ChannelTodo.Revision {
			return
		}
		current.ChannelTodo = channelTodoModel(response.GetList())
		current.ChannelTodoError = ""
		if operation == "ADD" && current.ChannelTodoDraft == text && current.ChannelTodoSourcePin == sourcePostID {
			current.ChannelTodoDraft, current.ChannelTodoSourcePin = "", ""
			current.ChannelTodoNewMode, current.ChannelTodoNewSelected = "EVERYONE", nil
		}
	})
	refreshChannelTodoRoute(room, generation)
}

func withChannelTodoCallbacks(callbacks chatui.Callbacks, cfg journeyclient.Config) chatui.Callbacks {
	callbacks.RetryChannelTodo = func() { go loadChannelTodo(cfg, chatBrowser.selectedID()) }
	callbacks.SetChannelTodoDraft = func(text string) { chatBrowser.mutate(func(model *chatui.Model) { model.ChannelTodoDraft = text }) }
	callbacks.SetChannelTodoSourcePin = func(id string) {
		chatBrowser.mutate(func(model *chatui.Model) { model.ChannelTodoSourcePin = id })
		refreshChatRoute()
	}
	callbacks.SetChannelTodoNewPolicy = func(mode string, selected []chatui.ChannelTodoSelectedMember) {
		if mode != "EVERYONE" && mode != "ME" && mode != "ME_AND_SELECTED" {
			return
		}
		if mode != "ME_AND_SELECTED" {
			selected = nil
		}
		if len(selected) > 20 {
			return
		}
		chatBrowser.mutate(func(model *chatui.Model) {
			if model.ChannelTodoPending || model.ChannelTodoError != "" {
				return
			}
			model.ChannelTodoNewMode = mode
			model.ChannelTodoNewSelected = append([]chatui.ChannelTodoSelectedMember(nil), selected...)
		})
		refreshChatRoute()
	}
	callbacks.AddChannelTodo = func(text, sourcePostID string) {
		text = strings.TrimSpace(text)
		if text != "" && len([]rune(text)) <= 500 {
			model := chatBrowser.snapshot()
			mode := model.ChannelTodoNewMode
			if mode == "" {
				mode = "EVERYONE"
			}
			go mutateChannelTodo(cfg, model.SelectedID, "ADD", "", text, sourcePostID, false, false, chatTodoPolicy{mode: mode, selected: append([]chatui.ChannelTodoSelectedMember(nil), model.ChannelTodoNewSelected...)})
		}
	}
	callbacks.SetChannelTodoCompleted = func(id string, completed bool) {
		go mutateChannelTodo(cfg, chatBrowser.selectedID(), "SET_COMPLETED", id, "", "", completed, false)
	}
	callbacks.DeleteChannelTodo = func(id string) {
		go mutateChannelTodo(cfg, chatBrowser.selectedID(), "DELETE", id, "", "", false, false)
	}
	callbacks.SetChannelTodoPinned = func(pinned bool) {
		go mutateChannelTodo(cfg, chatBrowser.selectedID(), "SET_PINNED", "", "", "", false, pinned)
	}
	callbacks.SetChannelTodoPolicy = func(itemID, mode string, selected []chatui.ChannelTodoSelectedMember) {
		if mode != "EVERYONE" && mode != "ME" && mode != "ME_AND_SELECTED" {
			return
		}
		go mutateChannelTodo(cfg, chatBrowser.selectedID(), "SET_COMPLETION_POLICY", itemID, "", "", false, false, chatTodoPolicy{mode: mode, selected: selected})
	}
	return callbacks
}
