package chat

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

// FilterIdentity resolves exemptions from current authority, never request roles.
type FilterIdentity interface {
	FilterInput(context.Context, Principal) (chatfilter.Input, error)
}
type FilterAuthorIdentity interface {
	FilterAuthorInput(context.Context, Post) (chatfilter.Input, error)
}

type FilterContentPolicy struct {
	AuthorIdentity  FilterAuthorIdentity
	Filters         *chatfilter.Service
	Identity        FilterIdentity
	AttachmentTypes func(context.Context, ContentInput) ([]string, error)
	Conversations   interface {
		GetConversation(context.Context, string, string) (Conversation, error)
	}
}

func (p *FilterContentPolicy) input(ctx context.Context, in ContentInput) (chatfilter.Input, error) {
	if p == nil || p.Filters == nil {
		return chatfilter.Input{}, chatfilter.ErrUnavailable
	}
	out := chatfilter.Input{Tenant: in.Conversation.TenantID, Subject: in.Principal.SubjectID}
	if p.Identity != nil {
		var err error
		out, err = p.Identity.FilterInput(ctx, in.Principal)
		if err != nil {
			return out, err
		}
	}
	out.Tenant = in.Conversation.TenantID
	if in.Principal.TenantID != in.Conversation.TenantID {
		out.Roles = nil
		out.Agent = false
	}
	out.Channel = in.Conversation.ID
	out.Body = in.Body
	out.Direct = in.Conversation.Kind == Direct || in.Conversation.Kind == Group
	if p.AttachmentTypes != nil {
		var err error
		out.AttachmentTypes, err = p.AttachmentTypes(ctx, in)
		if err != nil {
			return out, err
		}
	} else {
		for _, ref := range in.References {
			if ref.Kind == MediaAttachment {
				return out, chatfilter.ErrUnavailable
			}
		}
	}
	return out, nil
}
func (p *FilterContentPolicy) CheckContent(ctx context.Context, in ContentInput) error {
	input, err := p.input(ctx, in)
	if err != nil {
		return err
	}
	result, err := p.Filters.Evaluate(ctx, input, true)
	if err != nil {
		return err
	}
	return result.Refusal()
}

// MaskedBody must be called only after the ordinary conversation/history read
// authorization. It returns a derived body and never writes it into the post or
// its revisions. The author is shown the same text readers see (CHATMOD-002:
// "a masked message shows the author what readers see"); the stored original
// stays under the retention and hold rules of any message.
func (p *FilterContentPolicy) MaskedBody(ctx context.Context, post Post, reader Principal) (string, error) {
	if post.Deleted {
		return "", nil
	}
	if p == nil || p.Filters == nil {
		return "", chatfilter.ErrUnavailable
	}
	c := Conversation{ID: post.ConversationID, TenantID: post.TenantID}
	if p.Conversations != nil {
		var err error
		c, err = p.Conversations.GetConversation(ctx, post.TenantID, post.ConversationID)
		if err != nil {
			return "", err
		}
	}
	// Read projections never trust a reader's roles as the author's exemption.
	input := chatfilter.Input{Tenant: post.TenantID, Channel: post.ConversationID, Subject: post.AuthorID, Body: post.Body, Direct: c.Kind == Direct || c.Kind == Group}
	if p.AuthorIdentity != nil {
		author, err := p.AuthorIdentity.FilterAuthorInput(ctx, post)
		if err != nil {
			return "", err
		}
		if author.Tenant == post.AuthorHomeTenantID && author.Subject == post.AuthorID && post.AuthorHomeTenantID == post.TenantID {
			input.Roles = author.Roles
			input.Agent = author.Agent
		}
	}
	result, err := p.Filters.Evaluate(ctx, input, false)
	if err != nil {
		return "", err
	}
	return result.Masked, nil
}

// CheckFilterText is the shared entry point for channel metadata, transcripts,
// gate answers and agent output. Each caller authorizes its operation first.
func (p *FilterContentPolicy) CheckFilterText(ctx context.Context, principal Principal, c Conversation, text string) error {
	return p.CheckContent(ctx, ContentInput{Principal: principal, Conversation: c, Body: text})
}

var _ ContentPolicy = (*FilterContentPolicy)(nil)

func (p *FilterContentPolicy) checkPersonaReplyContent(ctx context.Context, r PersonaReplyCommitRequest, c Conversation) error {
	if p == nil || p.Filters == nil {
		return chatfilter.ErrUnavailable
	}
	if r.Proof == nil || !r.Proof.ValidFor(r.TenantID, r.ConversationID, r.AuthorID, r.ExpectedAudienceRevision, r.OutputDigest, r.Body, r.ParentID) {
		return ErrPermissionDenied
	}
	input := chatfilter.Input{Tenant: r.TenantID, Channel: r.ConversationID, Subject: r.AuthorID, Body: r.Body, Agent: true, Direct: c.Kind == Direct || c.Kind == Group}
	if p.AuthorIdentity != nil {
		author, err := p.AuthorIdentity.FilterAuthorInput(ctx, Post{TenantID: r.TenantID, ConversationID: r.ConversationID, AuthorID: r.AuthorID, AuthorHomeTenantID: r.AuthorHomeTenantID, Body: r.Body})
		if err == nil && author.Tenant == r.AuthorHomeTenantID && author.Subject == r.AuthorID && r.AuthorHomeTenantID == r.TenantID {
			input.Roles = author.Roles
		}
	}
	result, err := p.Filters.Evaluate(ctx, input, true)
	if err != nil {
		return err
	}
	return result.Refusal()
}
