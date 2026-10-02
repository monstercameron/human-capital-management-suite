package application

import (
	"context"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

const ChatFiltersPagePath = "/workspace/app/chat/filters"

// RenderChatFilterPanel supplies the authorized SSR tree. The workspace shell
// owns the route and hydrates it with ChatFilterPage on the wasm client.
func RenderChatFilterPanel(ctx context.Context, service *chatfilter.Service, actor chatfilter.Actor, model chatui.Model) (ui.Node, error) {
	if service == nil {
		return nil, chatfilter.ErrUnavailable
	}
	defs, err := service.List(ctx, actor)
	if err != nil {
		return nil, err
	}
	settings, err := service.ListEnablements(ctx, actor)
	if err != nil {
		return nil, err
	}
	// The panel resolves a filter's state itself: this channel's own row when it
	// has one, else the workspace row, else off.
	return chatui.ModAdminPanel(chatui.ModAdminProps{Model: model, Workspace: actor.Channel == "", Channel: actor.Channel, Definitions: defs, Enablements: settings, CanManage: true}), nil
}
