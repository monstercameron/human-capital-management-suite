package application

import (
	"context"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type integrate2GateSurface struct {
	ChatgateSurface
	Routes interface {
		ChatWriteContext(context.Context, string, string) (context.Context, error)
	}
	// Filter judges the free text of a submission and of a reviewer's reason
	// (CHATMOD-002). Nil leaves the gate unfiltered.
	Filter *chat.FilterContentPolicy
}

func (s integrate2GateSurface) GateRequest(ctx context.Context, r ChatgateRequest) (ChatgateReply, error) {
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil {
		return ChatgateReply{}, chatgate.ErrDenied
	}
	if s.Routes == nil || s.ChatgateSurface == nil {
		return ChatgateReply{}, chatgate.ErrUnavailable
	}
	if err := s.checkGateText(ctx, r); err != nil {
		return ChatgateReply{}, err
	}
	leased, err := s.Routes.ChatWriteContext(ctx, p.Tenant().String(), r.Conversation)
	if err != nil {
		return ChatgateReply{}, err
	}
	return s.ChatgateSurface.GateRequest(leased, r)
}
