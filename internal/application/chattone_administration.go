package application

import (
	"context"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

// ChattoneAdministration is an explicit workspace administrator authorization port.
type ChattoneAdministration interface {
	ManageWritingStyles(context.Context, chatrewrite.Identity) error
}

// Instructions are administration data and are never returned in the public style list.
type ChattoneAdminStyle struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Instruction string `json:"instruction"`
	Register    string `json:"register"`
}
type ChattoneAdminConfig struct {
	ConversationID string               `json:"conversation_id"`
	Enabled        bool                 `json:"enabled"`
	Styles         []ChattoneAdminStyle `json:"styles"`
}

func (s *ChattoneService) ConfigureWritingStyles(ctx context.Context, in ChattoneAdminConfig) (ChattoneReply, error) {
	id, err := s.authorize(ctx, in.ConversationID)
	if err != nil {
		return ChattoneReply{}, err
	}
	if s.Administration == nil {
		return ChattoneReply{}, personachat.ErrDenied
	}
	if err := s.Administration.ManageWritingStyles(ctx, id); err != nil {
		return ChattoneReply{}, err
	}
	// Validate, write to the settings store (when there is one), then configure
	// the registry: a write that fails leaves the workspace as it was.
	if err := s.saveSettings(ctx, id.Tenant, id.Person, in.Enabled, in.Styles); err != nil {
		return ChattoneReply{}, err
	}
	styles := make([]chatrewrite.Style, len(in.Styles))
	for i, style := range in.Styles {
		styles[i] = chatrewrite.Style{ID: style.ID, Label: style.Label, Instruction: style.Instruction, Register: style.Register}
	}
	return ChattoneReply{Styles: styles, Enabled: in.Enabled}, nil
}
