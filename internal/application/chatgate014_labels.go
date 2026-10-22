package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
)

// CHATGATE-014: two reads of the channel's gate for the chat page, each asked for
// by GET and answered to anyone who is in the channel: the labels it gave and who
// holds each (Conversation details), and what the writer may address a message to
// (the composer's "Visible to").

// chatgateLabelsAction asks for the labels of one conversation and
// chatgateAudienceAction for the audiences the person may write to.
const (
	chatgateLabelsAction   = "labels"
	chatgateAudienceAction = "audience"
)

// chatgateLabelReader and chatgateAudienceReader are what the consumers of
// answers offer for the reads; *chatgate.Service implements both. They are asked
// of the consumers by assertion so a composition that supplies its own consumers
// is not made to implement them.
type chatgateLabelReader interface {
	LabelHolders(context.Context, chatgate.Actor, chatgate.Scope) ([]chatgate.LabelHolders, error)
}
type chatgateAudienceReader interface {
	AudienceOptions(context.Context, chatgate.Actor, chatgate.Scope) (chatgate.AudienceOptions, error)
}

var (
	_ chatgateLabelReader    = (*chatgate.Service)(nil)
	_ chatgateAudienceReader = (*chatgate.Service)(nil)
)

func (a *ChatgateApplication) labelsReply(ctx context.Context, actor chatgate.Actor, scope chatgate.Scope) (ChatgateReply, error) {
	reader, ok := a.Consumers.(chatgateLabelReader)
	if !ok {
		return ChatgateReply{}, chatgate.ErrUnavailable
	}
	labels, err := reader.LabelHolders(ctx, actor, scope)
	if err != nil {
		return ChatgateReply{}, err
	}
	if labels == nil {
		labels = []chatgate.LabelHolders{}
	}
	return ChatgateReply{Result: labels}, nil
}

func (a *ChatgateApplication) audienceReply(ctx context.Context, actor chatgate.Actor, scope chatgate.Scope) (ChatgateReply, error) {
	reader, ok := a.Consumers.(chatgateAudienceReader)
	if !ok {
		return ChatgateReply{}, chatgate.ErrUnavailable
	}
	options, err := reader.AudienceOptions(ctx, actor, scope)
	if err != nil {
		return ChatgateReply{}, err
	}
	return ChatgateReply{Result: options}, nil
}
