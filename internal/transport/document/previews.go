package document

import (
	"context"
	"strings"
	"time"

	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// MaxPreviewIDs bounds one GetDocumentPreviews request.
const MaxPreviewIDs = 50

// Preview is one document unfurl. Everything but DocumentID is withheld
// unless Readable.
type Preview struct {
	DocumentID, Title, OwnerID, OwnerName, Snippet string
	UpdatedAt                                      time.Time
	Readable                                       bool
}

// PreviewService is the optional batched preview port. A composition whose
// service does not provide it answers UNAVAILABLE.
type PreviewService interface {
	GetDocumentPreviews(ctx context.Context, tenant, actor string, documentIDs []string) ([]Preview, error)
}

// ChatReferences is the caller's view of the chat references a document
// names; GetDocument fills it when chat is composed.
type ChatReferences struct {
	Channels []ChannelReference
	People   []PersonReference
	Messages []MessageReference
}

// ChannelReference is one named channel. A Locked channel carries only
// its Key.
type ChannelReference struct {
	Key, ConversationID, Name string
	MemberCount               int
	Locked, Joined, Private   bool
}

// PersonReference is one "@handle" or "@<subject-id>" that resolved.
type PersonReference struct {
	Key, SubjectID, DisplayName string
}

// MessageReference is one chat permalink. Everything but Token is withheld
// unless Readable.
type MessageReference struct {
	Token, ConversationID, ChannelName, PostID, AuthorID, AuthorName, Body string
	CreatedAt                                                              time.Time
	Readable                                                               bool
}

func (s *server) GetDocumentPreviews(ctx context.Context, req *documentv1.GetDocumentPreviewsRequest) (*documentv1.GetDocumentPreviewsResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	ids := req.GetDocumentIds()
	if len(ids) == 0 || len(ids) > MaxPreviewIDs {
		return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_previews", "between 1 and 50 document IDs are required")
	}
	for _, id := range ids {
		if strings.TrimSpace(id) == "" || len(id) > 128 {
			return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_id", "a document ID is required")
		}
	}
	previews, ok := s.service.(PreviewService)
	if !ok {
		return nil, envelope.New(envelope.CodeUnavailable, "document.service.unavailable", "document previews are not configured")
	}
	rows, err := previews.GetDocumentPreviews(ctx, tenant, actor, ids)
	if err != nil {
		return nil, owned(err)
	}
	out := &documentv1.GetDocumentPreviewsResponse{Previews: make([]*documentv1.DocumentPreview, 0, len(rows))}
	for _, row := range rows {
		out.Previews = append(out.Previews, previewMessage(row))
	}
	return out, nil
}

// previewMessage never sends a title, owner, date or snippet for a
// document the caller cannot read, whatever the service returned.
func previewMessage(row Preview) *documentv1.DocumentPreview {
	if !row.Readable {
		return &documentv1.DocumentPreview{DocumentId: row.DocumentID}
	}
	p := &documentv1.DocumentPreview{DocumentId: row.DocumentID, Readable: true, Title: row.Title, OwnerId: row.OwnerID, OwnerName: row.OwnerName, Snippet: row.Snippet}
	if !row.UpdatedAt.IsZero() {
		p.UpdatedAt = timestamppb.New(row.UpdatedAt)
	}
	return p
}

// chatReferenceMessages projects a document's chat references. A locked
// channel and an unreadable message keep only their key or token.
func chatReferenceMessages(refs ChatReferences, out *documentv1.GetDocumentResponse) {
	for _, c := range refs.Channels {
		if c.Locked {
			out.Channels = append(out.Channels, &documentv1.DocumentChannelReference{Key: c.Key, Locked: true})
			continue
		}
		out.Channels = append(out.Channels, &documentv1.DocumentChannelReference{Key: c.Key, ConversationId: c.ConversationID, Name: c.Name, MemberCount: uint32(max(c.MemberCount, 0)), Joined: c.Joined, Private: c.Private})
	}
	for _, p := range refs.People {
		out.People = append(out.People, &documentv1.DocumentPersonReference{Key: p.Key, SubjectId: p.SubjectID, DisplayName: p.DisplayName})
	}
	for _, m := range refs.Messages {
		if !m.Readable {
			// A deleted message still names its channel (DOCS-08); an
			// inaccessible or unresolvable one carries only its token.
			out.Messages = append(out.Messages, &documentv1.DocumentMessageReference{Token: m.Token, ConversationId: m.ConversationID, ChannelName: m.ChannelName})
			continue
		}
		msg := &documentv1.DocumentMessageReference{Token: m.Token, Readable: true, ConversationId: m.ConversationID, ChannelName: m.ChannelName, PostId: m.PostID, AuthorId: m.AuthorID, AuthorName: m.AuthorName, Body: m.Body}
		if !m.CreatedAt.IsZero() {
			msg.CreatedAt = timestamppb.New(m.CreatedAt)
		}
		out.Messages = append(out.Messages, msg)
	}
}
