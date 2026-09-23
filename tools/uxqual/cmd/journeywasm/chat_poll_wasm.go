//go:build js && wasm

package main

import (
	"context"
	"strings"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func channelPollModel(poll *chatv1.ChannelPoll) chatui.ChannelPoll {
	result := chatui.ChannelPoll{Revision: poll.GetRevision(), Question: poll.GetQuestion(), MyOptionID: poll.GetMyOptionId(), TotalVotes: int(poll.GetTotalVotes())}
	for _, option := range poll.GetOptions() {
		if option != nil {
			result.Options = append(result.Options, chatui.ChannelPollOption{ID: option.GetId(), Text: option.GetText(), Count: int(option.GetVoteCount())})
		}
	}
	return result
}

func refreshChannelPoll(room string, generation uint64) {
	epoch := chatRerenderEpoch
	ui.PostAsync(func() {
		if chatRerender != nil && chatRerenderEpoch == epoch && chatBrowser.generationActive(generation) && chatBrowser.selectedID() == room {
			chatRerender()
		}
	})
}

func loadChannelPoll(cfg journeyclient.Config, room string) {
	if !channelPollAllowed(room) {
		return
	}
	client := channelTodoClient()
	if client == nil {
		return
	}
	active := chatBrowser.config(cfg)
	generation := chatBrowser.currentGeneration()
	chatBrowser.mutate(func(m *chatui.Model) {
		if m.SelectedID == room {
			m.ChannelPollLoading = true
		}
	})
	refreshChannelPoll(room, generation)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	response, err := client.GetChannelPoll(chatRPCContext(ctx, active), &chatv1.GetChannelPollRequest{ConversationId: room, HostTenantId: channelTodoHost(room, active.Tenant)})
	current := chatBrowser.config(journeyclient.Config{})
	if active.Tenant != current.Tenant || active.Subject != current.Subject || active.Bearer != current.Bearer || generation != chatBrowser.currentGeneration() {
		return
	}
	chatBrowser.mutate(func(m *chatui.Model) {
		if m.SelectedID != room {
			return
		}
		m.ChannelPollLoading = false
		if err != nil || response == nil || response.GetPoll() == nil || response.GetPoll().GetConversationId() != room {
			m.ChannelPollError = "load"
			return
		}
		if response.GetPoll().GetRevision() >= m.ChannelPoll.Revision {
			m.ChannelPoll = channelPollModel(response.GetPoll())
		}
		m.ChannelPollError = ""
	})
	refreshChannelPoll(room, generation)
}

func channelPollAllowed(room string) bool {
	for _, c := range chatBrowser.snapshot().Conversations {
		if c.ID == room && (c.Kind == chatui.PublicChannel || c.Kind == chatui.PrivateChannel) && c.Joined {
			return true
		}
	}
	return false
}

func mutateChannelPoll(cfg journeyclient.Config, operation, question string, options []string, optionID string) {
	model := chatBrowser.snapshot()
	room := model.SelectedID
	if room == "" || !channelPollAllowed(room) || model.ChannelPollPending || model.ChannelPollLoading || model.ChannelPollError != "" || model.ChannelPoll.Revision == 0 {
		return
	}
	client := channelTodoClient()
	if client == nil {
		return
	}
	active := chatBrowser.config(cfg)
	generation := chatBrowser.currentGeneration()
	request := &chatv1.MutateChannelPollRequest{ConversationId: room, HostTenantId: channelTodoHost(room, active.Tenant), ExpectedRevision: model.ChannelPoll.Revision, Operation: operation, Question: strings.TrimSpace(question), Options: options, OptionId: optionID}
	chatBrowser.mutate(func(m *chatui.Model) {
		if m.SelectedID == room {
			m.ChannelPollPending = true
		}
	})
	refreshChannelPoll(room, generation)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	response, err := client.MutateChannelPoll(chatRPCContext(ctx, active), request)
	current := chatBrowser.config(journeyclient.Config{})
	if active.Tenant != current.Tenant || active.Subject != current.Subject || active.Bearer != current.Bearer || generation != chatBrowser.currentGeneration() {
		return
	}
	applied := false
	chatBrowser.mutate(func(m *chatui.Model) {
		if m.SelectedID != room {
			return
		}
		m.ChannelPollPending = false
		if err != nil || response == nil || response.GetPoll() == nil || response.GetPoll().GetConversationId() != room || response.GetPoll().GetRevision() < m.ChannelPoll.Revision {
			m.ChannelPollError = "save"
			return
		}
		m.ChannelPoll = channelPollModel(response.GetPoll())
		m.ChannelPollError = ""
		applied = true
	})
	if applied && operation == "CREATE" {
		for id := range map[string]struct{}{"channel-poll-question": {}, "channel-poll-options": {}} {
			element := js.Global().Get("document").Call("getElementById", id)
			if element.Truthy() {
				element.Set("value", "")
			}
		}
	}
	refreshChannelPoll(room, generation)
}

func withChannelPollCallbacks(callbacks chatui.Callbacks, cfg journeyclient.Config) chatui.Callbacks {
	callbacks.RetryChannelPoll = func() { go loadChannelPoll(cfg, chatBrowser.selectedID()) }
	callbacks.CreateChannelPoll = func(question string, options []string) {
		if strings.TrimSpace(question) != "" {
			go mutateChannelPoll(cfg, "CREATE", question, options, "")
		}
	}
	callbacks.VoteChannelPoll = func(optionID string) {
		model := chatBrowser.snapshot()
		for _, option := range model.ChannelPoll.Options {
			if option.ID == optionID {
				go mutateChannelPoll(cfg, "VOTE", "", nil, optionID)
				return
			}
		}
	}
	return callbacks
}
