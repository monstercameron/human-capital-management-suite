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

func refreshChannelWidgets(room string, generation uint64) {
	epoch := chatRerenderEpoch
	ui.PostAsync(func() {
		if chatRerender != nil && chatRerenderEpoch == epoch && chatBrowser.generationActive(generation) && chatBrowser.selectedID() == room {
			chatRerender()
		}
	})
}

func channelTeamWidgetModel(widget *chatv1.ChannelTeamWidget) chatui.ChannelTeamWidget {
	result := chatui.ChannelTeamWidget{Revision: widget.GetRevision(), Pinned: widget.GetPinned(), CanPin: widget.GetCanPin(), Purpose: widget.GetPurpose()}
	for _, member := range widget.GetMembers() {
		if member != nil {
			result.Members = append(result.Members, chatui.ChannelTeamMember{HomeTenantID: member.GetHomeTenantId(), SubjectID: member.GetSubjectId(), Role: member.GetRole(), RoleLabel: member.GetRoleLabel()})
		}
	}
	return result
}

func channelProjectWidgetModel(widget *chatv1.ChannelProjectWidget) chatui.ChannelProjectWidget {
	result := chatui.ChannelProjectWidget{Revision: widget.GetRevision(), Pinned: widget.GetPinned(), CanPin: widget.GetCanPin(), Title: widget.GetTitle(), Summary: widget.GetSummary()}
	for _, item := range widget.GetMilestones() {
		if item != nil {
			result.Milestones = append(result.Milestones, chatui.ChannelProjectMilestone{ID: item.GetId(), Text: item.GetText(), Status: item.GetStatus(), OwnerHomeTenantID: item.GetOwnerHomeTenantId(), OwnerSubjectID: item.GetOwnerSubjectId(), DueDate: item.GetDueDate()})
		}
	}
	return result
}

func loadChannelWidgets(cfg journeyclient.Config, room string) {
	allowed := false
	for _, c := range chatBrowser.snapshot().Conversations {
		if c.ID == room && (c.Kind == chatui.PublicChannel || c.Kind == chatui.PrivateChannel) && c.Joined {
			allowed = true
			break
		}
	}
	if !allowed || room == "" {
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
			m.ChannelWidgetsLoading = true
		}
	})
	refreshChannelWidgets(room, generation)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	response, err := client.GetChannelWidgets(chatRPCContext(ctx, active), &chatv1.GetChannelWidgetsRequest{ConversationId: room, HostTenantId: channelTodoHost(room, active.Tenant)})
	current := chatBrowser.config(journeyclient.Config{})
	if active.Tenant != current.Tenant || active.Subject != current.Subject || active.Bearer != current.Bearer || generation != chatBrowser.currentGeneration() {
		return
	}
	chatBrowser.mutate(func(m *chatui.Model) {
		if m.SelectedID != room {
			return
		}
		m.ChannelWidgetsLoading = false
		if err != nil || response.GetTeam() == nil || response.GetProject() == nil || response.GetTeam().GetConversationId() != room || response.GetProject().GetConversationId() != room {
			m.ChannelWidgetsError = "load"
			return
		}
		if response.GetTeam().GetRevision() >= m.ChannelTeam.Revision {
			m.ChannelTeam = channelTeamWidgetModel(response.GetTeam())
		}
		if response.GetProject().GetRevision() >= m.ChannelProject.Revision {
			m.ChannelProject = channelProjectWidgetModel(response.GetProject())
		}
		m.ChannelWidgetsError = ""
	})
	refreshChannelWidgets(room, generation)
}

func clearChannelMilestoneForm() {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return
	}
	for id, value := range map[string]string{"channel-milestone-new": "", "channel-milestone-new-status": "PLANNED", "channel-milestone-new-owner": "", "channel-milestone-new-date": ""} {
		el := doc.Call("getElementById", id)
		if el.Truthy() {
			el.Set("value", value)
			el.Set("__chatSelectEditedVersion", js.Undefined())
		}
	}
}

func mutateChannelWidget(cfg journeyclient.Config, kind, operation string, apply func(*chatv1.MutateChannelWidgetRequest)) {
	model := chatBrowser.snapshot()
	room := model.SelectedID
	if room == "" || model.ChannelWidgetsPending || model.ChannelWidgetsLoading || model.ChannelWidgetsError != "" {
		return
	}
	revision := model.ChannelTeam.Revision
	if kind == "PROJECT" {
		revision = model.ChannelProject.Revision
	}
	if revision == 0 {
		return
	}
	client := channelTodoClient()
	if client == nil {
		return
	}
	active := chatBrowser.config(cfg)
	generation := chatBrowser.currentGeneration()
	request := &chatv1.MutateChannelWidgetRequest{ConversationId: room, HostTenantId: channelTodoHost(room, active.Tenant), Kind: kind, Operation: operation, ExpectedRevision: revision}
	apply(request)
	chatBrowser.mutate(func(m *chatui.Model) {
		if m.SelectedID == room {
			m.ChannelWidgetsPending = true
		}
	})
	refreshChannelWidgets(room, generation)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	response, err := client.MutateChannelWidget(chatRPCContext(ctx, active), request)
	current := chatBrowser.config(journeyclient.Config{})
	if active.Tenant != current.Tenant || active.Subject != current.Subject || active.Bearer != current.Bearer || generation != chatBrowser.currentGeneration() {
		return
	}
	applied := false
	chatBrowser.mutate(func(m *chatui.Model) {
		if m.SelectedID != room {
			return
		}
		m.ChannelWidgetsPending = false
		if err != nil || response == nil {
			m.ChannelWidgetsError = "save"
			return
		}
		if kind == "TEAM" {
			if response.GetTeam() == nil || response.GetTeam().GetConversationId() != room || response.GetTeam().GetRevision() < m.ChannelTeam.Revision {
				m.ChannelWidgetsError = "save"
				return
			}
			m.ChannelTeam = channelTeamWidgetModel(response.GetTeam())
		} else {
			if response.GetProject() == nil || response.GetProject().GetConversationId() != room || response.GetProject().GetRevision() < m.ChannelProject.Revision {
				m.ChannelWidgetsError = "save"
				return
			}
			m.ChannelProject = channelProjectWidgetModel(response.GetProject())
		}
		m.ChannelWidgetsError = ""
		applied = true
	})
	if applied && operation == "ADD_MILESTONE" {
		clearChannelMilestoneForm()
	}
	refreshChannelWidgets(room, generation)
}

func withChannelWidgetCallbacks(callbacks chatui.Callbacks, cfg journeyclient.Config) chatui.Callbacks {
	callbacks.RetryChannelWidgets = func() { go loadChannelWidgets(cfg, chatBrowser.selectedID()) }
	callbacks.SetChannelWidgetPinned = func(kind string, pinned bool) {
		model := chatBrowser.snapshot()
		if kind == "TEAM" && !model.ChannelTeam.CanPin || kind == "PROJECT" && !model.ChannelProject.CanPin {
			return
		}
		go mutateChannelWidget(cfg, kind, "SET_PINNED", func(r *chatv1.MutateChannelWidgetRequest) { r.Pinned = pinned })
	}
	callbacks.SetChannelTeamPurpose = func(purpose string) {
		go mutateChannelWidget(cfg, "TEAM", "SET_PURPOSE", func(r *chatv1.MutateChannelWidgetRequest) { r.Purpose = strings.TrimSpace(purpose) })
	}
	callbacks.SetChannelTeamRoleLabel = func(home, subject, label string) {
		go mutateChannelWidget(cfg, "TEAM", "SET_ROLE_LABEL", func(r *chatv1.MutateChannelWidgetRequest) {
			r.MemberHomeTenantId = home
			r.MemberSubjectId = subject
			r.RoleLabel = strings.TrimSpace(label)
		})
	}
	callbacks.SetChannelProjectDetails = func(title, summary string) {
		go mutateChannelWidget(cfg, "PROJECT", "SET_DETAILS", func(r *chatv1.MutateChannelWidgetRequest) {
			r.Title = strings.TrimSpace(title)
			r.Summary = strings.TrimSpace(summary)
		})
	}
	milestone := func(item chatui.ChannelProjectMilestone) *chatv1.ChannelProjectMilestone {
		return &chatv1.ChannelProjectMilestone{Id: item.ID, Text: strings.TrimSpace(item.Text), Status: item.Status, OwnerHomeTenantId: item.OwnerHomeTenantID, OwnerSubjectId: item.OwnerSubjectID, DueDate: item.DueDate}
	}
	callbacks.AddChannelProjectMilestone = func(item chatui.ChannelProjectMilestone) {
		if strings.TrimSpace(item.Text) != "" {
			go mutateChannelWidget(cfg, "PROJECT", "ADD_MILESTONE", func(r *chatv1.MutateChannelWidgetRequest) { r.Milestone = milestone(item) })
		}
	}
	callbacks.UpdateChannelProjectMilestone = func(item chatui.ChannelProjectMilestone) {
		if item.ID != "" && strings.TrimSpace(item.Text) != "" {
			go mutateChannelWidget(cfg, "PROJECT", "UPDATE_MILESTONE", func(r *chatv1.MutateChannelWidgetRequest) { r.MilestoneId = item.ID; r.Milestone = milestone(item) })
		}
	}
	callbacks.DeleteChannelProjectMilestone = func(id string) {
		if id != "" {
			go mutateChannelWidget(cfg, "PROJECT", "DELETE_MILESTONE", func(r *chatv1.MutateChannelWidgetRequest) { r.MilestoneId = id })
		}
	}
	return callbacks
}
