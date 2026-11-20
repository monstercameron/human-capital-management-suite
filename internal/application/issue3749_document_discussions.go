package application

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/projectrefs"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3749: where a document is discussed. The chat store keeps a pointer
// from each message to the documents it names (no words). This service turns
// those pointers into the list a document shows, and decides what a reader may
// see of it, every time, under the reader's own authority: the document must be
// readable first (otherwise "not found", before any conversation is read), and
// a conversation, or a message in it, the reader cannot open leaves no row, no
// name, no count and no link. A conversation that cannot be checked is reported
// as incomplete, never as absent.

// DocumentDiscussionRefs is the message index as this feature reads it.
type DocumentDiscussionRefs interface {
	ListPostReferencesByTarget(ctx context.Context, tenant, kind, targetID string, limit int) ([]chatstore.PostReference, error)
}

// DocumentReadGate decides whether a reader may read a document. It is the same
// decision the document's own backlinks make.
type DocumentReadGate interface {
	CanRead(ctx context.Context, tenantID, docID, readerKind, readerID string) error
}

// DocumentDiscussion is one conversation that mentions the document, as one
// reader may see it. Name is empty for a direct message.
type DocumentDiscussion struct {
	ConversationID string `json:"conversation_id"`
	Name           string `json:"name"`
	Direct         bool   `json:"direct"`
	Messages       int    `json:"messages"`
	LatestAt       string `json:"latest_at"`
	Href           string `json:"href"`
}

// DocumentDiscussionsResult is the answer. Incomplete is true when a
// conversation could not be checked, so a short list is not taken as complete.
type DocumentDiscussionsResult struct {
	Items      []DocumentDiscussion `json:"items"`
	Incomplete bool                 `json:"incomplete"`
}

// DocumentDiscussions answers "Discussed in" for a document.
type DocumentDiscussions struct {
	Chat projectrefs.ChatLinks
	Refs DocumentDiscussionRefs
	Docs DocumentReadGate
}

const (
	documentDiscussionConversations = 50
	// Messages checked in one conversation. The count says how many of those
	// the reader can read, so it is a floor on very busy threads, never a leak.
	documentDiscussionMessages = 5
)

func (d *DocumentDiscussions) ready() bool {
	return d != nil && d.Chat != nil && d.Refs != nil && d.Docs != nil
}

// List returns the conversations that mention the document that this reader may open.
func (d *DocumentDiscussions) List(ctx context.Context, p *trust.Principal, documentID string) (DocumentDiscussionsResult, error) {
	cp, err := chatWorkPrincipal(p)
	if err != nil {
		return DocumentDiscussionsResult{}, err
	}
	if !d.ready() {
		return DocumentDiscussionsResult{}, ErrChatWorkUnavailable
	}
	if strings.TrimSpace(documentID) == "" || len(documentID) > 256 || documentID != strings.TrimSpace(documentID) {
		return DocumentDiscussionsResult{}, ErrChatWorkInvalid
	}
	switch err := d.Docs.CanRead(ctx, cp.TenantID, documentID, "person", cp.SubjectID); {
	case err == nil:
	case errors.Is(err, documenthubstore.ErrDenied), errors.Is(err, documenthubstore.ErrUnknownDocument):
		return DocumentDiscussionsResult{}, ErrChatWorkNotFound
	default:
		return DocumentDiscussionsResult{}, ErrChatWorkUnavailable
	}
	refs, err := d.Refs.ListPostReferencesByTarget(ctx, cp.TenantID, chatstore.PostReferenceDocument, documentID, chatstore.PostReferenceListLimit)
	if err != nil {
		return DocumentDiscussionsResult{}, ErrChatWorkUnavailable
	}
	// Group by conversation, keeping the order of the newest message in each.
	order := []string{}
	byConversation := map[string][]chatstore.PostReference{}
	for _, ref := range refs {
		if _, seen := byConversation[ref.ConversationID]; !seen {
			order = append(order, ref.ConversationID)
		}
		byConversation[ref.ConversationID] = append(byConversation[ref.ConversationID], ref)
	}
	result := DocumentDiscussionsResult{Items: []DocumentDiscussion{}}
	for _, conversation := range order {
		if len(result.Items) >= documentDiscussionConversations {
			break
		}
		room, _, err := d.Chat.ReadAuthorizedReference(ctx, cp, cp.TenantID, conversation, "")
		switch {
		case err == nil:
		case errors.Is(err, chat.ErrNotFound), errors.Is(err, chat.ErrPermissionDenied):
			continue
		default:
			result.Incomplete = true
			continue
		}
		item := DocumentDiscussion{ConversationID: conversation, Direct: room.Kind == chat.Direct}
		if !item.Direct {
			item.Name = strings.TrimSpace(room.Name)
		}
		checked := 0
		for _, ref := range byConversation[conversation] {
			if checked >= documentDiscussionMessages {
				break
			}
			checked++
			_, post, err := d.Chat.ReadAuthorizedReference(ctx, cp, cp.TenantID, conversation, ref.PostID)
			switch {
			case err == nil && post != nil && strings.Contains(post.Body, documentID):
				// The reader's own view of the message still names the document. A
				// message shown to this reader masked or replaced by a stand-in does
				// not, and is withheld, so the list never reveals what the view hides.
			case err == nil, errors.Is(err, chat.ErrNotFound), errors.Is(err, chat.ErrPermissionDenied):
				continue
			default:
				result.Incomplete = true
				continue
			}
			item.Messages++
			if item.Href == "" {
				item.LatestAt = ref.PostedAt.UTC().Format(time.RFC3339)
				item.Href = "/workspace/app/chat#channel=" + url.QueryEscape(conversation) + "&message=" + url.QueryEscape(post.ID) + "&at=" + strconv.FormatUint(post.Sequence, 10)
			}
		}
		if item.Messages > 0 {
			result.Items = append(result.Items, item)
		}
	}
	return result, nil
}

// NewDocumentDiscussions composes the service over the chat store and the
// document store.
func NewDocumentDiscussions(chatLinks projectrefs.ChatLinks, refs DocumentDiscussionRefs, docs DocumentReadGate) *DocumentDiscussions {
	if chatLinks == nil || refs == nil || docs == nil {
		return nil
	}
	return &DocumentDiscussions{Chat: chatLinks, Refs: refs, Docs: docs}
}
