package application

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

// ChatTextFilter is the content policy as the extension surfaces (to-do items,
// polls, team and project widgets) see it: one call per piece of text a person
// types, evaluated on the server before anything is stored.
type ChatTextFilter interface {
	CheckFilterText(context.Context, chat.Principal, chat.Conversation, string) error
}

// checkTexts runs every non-blank text through the workspace's filters. It
// returns the first refusal unchanged so the transport can tell the author what
// was refused. A composition without a filter allows every text, as before.
func (s *ChatExtensions) checkTexts(ctx context.Context, p chat.Principal, host, conversation string, texts ...string) error {
	if s == nil || s.ContentFilter == nil {
		return nil
	}
	var pending []string
	for _, text := range texts {
		if strings.TrimSpace(text) != "" {
			pending = append(pending, strings.TrimSpace(text))
		}
	}
	if len(pending) == 0 {
		return nil
	}
	if host == "" {
		host = p.TenantID
	}
	c, err := s.Conversations.GetConversation(ctx, chat.GetConversationRequest{Principal: p, TenantID: host, ConversationID: conversation})
	if err != nil {
		return err
	}
	for _, text := range pending {
		if err = s.ContentFilter.CheckFilterText(ctx, p, c, text); err != nil {
			return err
		}
	}
	return nil
}

// chatmod002PublicDefinitions is what a browser is sent about the filters. The
// built-in lists are the product's data: a browser needs their names and
// actions to draw a switch, never the words themselves (CHATMOD-002: a filter
// reveals nothing of a list beyond a term that matched).
func chatmod002PublicDefinitions(defs []chatfilter.Definition) []chatfilter.Definition {
	out := make([]chatfilter.Definition, len(defs))
	for i, d := range defs {
		if d.Product {
			d.Match = nil
		}
		out[i] = d
	}
	return out
}
