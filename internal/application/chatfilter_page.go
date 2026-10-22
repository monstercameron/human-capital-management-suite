package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

const ChatFiltersPagePath = "/workspace/app/chat/filters"

// RenderChatFilterPanel supplies the authorized SSR markup. The workspace shell
// owns the route and hydrates it with ChatFilterPage on the wasm client.
func RenderChatFilterPanel(ctx context.Context, service *chatfilter.Service, actor chatfilter.Actor, model chatui.Model) (string, error) {
	if service == nil {
		return "", chatfilter.ErrUnavailable
	}
	defs, err := service.List(ctx, actor)
	if err != nil {
		return "", err
	}
	settings, err := service.ListEnablements(ctx, actor)
	if err != nil {
		return "", err
	}
	// The panel resolves a filter's state itself: this channel's own row when it
	// has one, else the workspace row, else off.
	return chatui.RenderModAdminPanelHTML(chatui.ModAdminProps{Model: model, Workspace: actor.Channel == "", Channel: actor.Channel, Definitions: defs, Enablements: settings, CanManage: true})
}
