package application

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/projectrefs"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	intentapp "github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3726: where a journey was discussed. A conversation remembers that a
// journey was shared into it (a pointer, no words). Whether a reader sees that
// conversation is asked of Chat under the reader's own authority every time:
// a conversation the reader cannot open leaves no row, no count and no
// placeholder, and a journey the reader cannot open answers "not found" before
// any conversation is read. A source that cannot answer is reported as
// incomplete, never as an empty list.

// JourneyDiscussionStore is the pointer table as this feature reads and writes it.
type JourneyDiscussionStore interface {
	ListWorkLinksBySource(ctx context.Context, tenant, kind, sourceID string, limit int) ([]chatstore.WorkLink, error)
	LinkWork(context.Context, chatstore.WorkLink) error
}

// JourneyDiscussion is one conversation a journey was discussed in, as one
// reader may see it. Name is empty for a direct message.
type JourneyDiscussion struct {
	ConversationID string `json:"conversation_id"`
	Name           string `json:"name"`
	Direct         bool   `json:"direct"`
	SharedAt       string `json:"shared_at"`
	Href           string `json:"href"`
}

// JourneyDiscussionsResult is the answer. Incomplete is true when a
// conversation could not be checked, so a short list is not taken as complete.
type JourneyDiscussionsResult struct {
	Items      []JourneyDiscussion `json:"items"`
	Incomplete bool                `json:"incomplete"`
}

// JourneyDiscussions answers "Discussed in" for a journey and records shares.
type JourneyDiscussions struct {
	Chat    projectrefs.ChatLinks
	Links   JourneyDiscussionStore
	Intents ChatWorkIntentReader
	Now     func() time.Time
}

const journeyDiscussionMax = 50

func (d *JourneyDiscussions) ready() bool {
	return d != nil && d.Chat != nil && d.Links != nil && d.Intents != nil
}

// openJourney is the one place the right to see a journey is decided.
func (d *JourneyDiscussions) openJourney(ctx context.Context, p *trust.Principal, journeyID string) error {
	if strings.TrimSpace(journeyID) == "" || len(journeyID) > 128 || journeyID != strings.TrimSpace(journeyID) {
		return ErrChatWorkInvalid
	}
	inst, err := d.Intents.ReadIntent(ctx, p, journeyID)
	switch {
	case err == nil && inst != nil:
		return nil
	case errors.Is(err, ErrChatWorkNotFound), errors.Is(err, ErrChatWorkDenied):
		return ErrChatWorkNotFound
	}
	return ErrChatWorkUnavailable
}

// List returns the conversations the journey was discussed in that this reader may open.
func (d *JourneyDiscussions) List(ctx context.Context, p *trust.Principal, journeyID string) (JourneyDiscussionsResult, error) {
	cp, err := chatWorkPrincipal(p)
	if err != nil {
		return JourneyDiscussionsResult{}, err
	}
	if !d.ready() {
		return JourneyDiscussionsResult{}, ErrChatWorkUnavailable
	}
	if err := d.openJourney(ctx, p, journeyID); err != nil {
		return JourneyDiscussionsResult{}, err
	}
	result := JourneyDiscussionsResult{Items: []JourneyDiscussion{}}
	links, err := d.Links.ListWorkLinksBySource(ctx, cp.TenantID, chatstore.WorkLinkJourneyShare, journeyID, journeyDiscussionMax)
	if err != nil {
		return JourneyDiscussionsResult{}, ErrChatWorkUnavailable
	}
	for _, link := range links {
		room, _, err := d.Chat.ReadAuthorizedReference(ctx, cp, cp.TenantID, link.ConversationID, "")
		switch {
		case err == nil:
		case errors.Is(err, chat.ErrNotFound), errors.Is(err, chat.ErrPermissionDenied):
			continue
		default:
			result.Incomplete = true
			continue
		}
		direct := room.Kind == chat.Direct
		name := strings.TrimSpace(room.Name)
		if direct {
			name = ""
		}
		result.Items = append(result.Items, JourneyDiscussion{
			ConversationID: link.ConversationID, Name: name, Direct: direct,
			SharedAt: link.LinkedAt.UTC().Format(time.RFC3339),
			Href:     "/workspace/app/chat#channel=" + url.QueryEscape(link.ConversationID),
		})
	}
	return result, nil
}

// Record remembers that the reader shared the journey into a conversation. The
// reader must be able to open both; a refusal reads the same as an absence.
func (d *JourneyDiscussions) Record(ctx context.Context, p *trust.Principal, journeyID, conversationID string) error {
	cp, err := chatWorkPrincipal(p)
	if err != nil {
		return err
	}
	if !d.ready() {
		return ErrChatWorkUnavailable
	}
	if strings.TrimSpace(conversationID) == "" {
		return ErrChatWorkInvalid
	}
	if err := d.openJourney(ctx, p, journeyID); err != nil {
		return err
	}
	if _, _, err := d.Chat.ReadAuthorizedReference(ctx, cp, cp.TenantID, conversationID, ""); err != nil {
		if errors.Is(err, chat.ErrNotFound) || errors.Is(err, chat.ErrPermissionDenied) {
			return ErrChatWorkNotFound
		}
		return ErrChatWorkUnavailable
	}
	linkedAt := time.Now()
	if d.Now != nil {
		linkedAt = d.Now()
	}
	if err := d.Links.LinkWork(ctx, chatstore.WorkLink{
		TenantID: cp.TenantID, ConversationID: conversationID, Kind: chatstore.WorkLinkJourneyShare,
		SourceID: journeyID, ProposedByID: cp.SubjectID, LinkedAt: linkedAt.UTC(),
	}); err != nil {
		return ErrChatWorkUnavailable
	}
	return nil
}

// NewJourneyDiscussions composes the service over the chat store and the
// intent service, which decides under the reader's own authority whether a
// journey may be opened at all.
func NewJourneyDiscussions(chatLinks projectrefs.ChatLinks, links JourneyDiscussionStore, intents *intentapp.IntentService) *JourneyDiscussions {
	if chatLinks == nil || links == nil || intents == nil {
		return nil
	}
	return &JourneyDiscussions{Chat: chatLinks, Links: links, Intents: chatWorkIntentAdapter{service: intents}}
}
