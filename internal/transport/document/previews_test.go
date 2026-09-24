package document

import (
	"context"
	"testing"
	"time"

	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// previewFake is a service whose preview and chat projections deliberately
// carry titles and bodies on unreadable rows, so the tests prove the
// transport withholds them regardless of what the port returns.
type previewFake struct {
	*fakeService
	ids []string
}

func (p *previewFake) GetDocumentPreviews(_ context.Context, tenant, actor string, ids []string) ([]Preview, error) {
	p.tenant, p.actor, p.ids = tenant, actor, ids
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	return []Preview{
		{DocumentID: ids[0], Readable: true, Title: "Onboarding guide", OwnerID: "owner-1", OwnerName: "Ana Lopez", Snippet: "Welcome", UpdatedAt: at},
		{DocumentID: "doc-secret", Title: "Secret comp plan", OwnerID: "owner-2", Snippet: "Salary bands", UpdatedAt: at},
	}, nil
}

func (p *previewFake) GetDocument(ctx context.Context, tenant, actor, id string) (Summary, string, string, error) {
	summary, markdown, hash, err := p.fakeService.GetDocument(ctx, tenant, actor, id)
	summary.Chat = ChatReferences{
		Channels: []ChannelReference{
			{Key: "id:c-1", ConversationID: "c-1", Name: "people-ops", MemberCount: 12, Joined: true},
			{Key: "id:c-2", ConversationID: "c-2", Name: "exec-private", MemberCount: 3, Locked: true, Private: true},
		},
		People: []PersonReference{{Key: "rafael.torres", SubjectID: "hc-050-rafael-torres", DisplayName: "Rafael Torres"}},
		Messages: []MessageReference{
			{Token: "tok-ok", Readable: true, ConversationID: "c-1", ChannelName: "people-ops", PostID: "p-1", AuthorID: "a-1", AuthorName: "Ana", Body: "Ship it", CreatedAt: time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC)},
			{Token: "tok-hidden", ConversationID: "c-2", ChannelName: "exec-private", AuthorName: "CEO", Body: "layoffs"},
		},
	}
	return summary, markdown, hash, err
}

func TestDocumentPreviewsWithholdUnreadable(t *testing.T) {
	f := &previewFake{fakeService: &fakeService{}}
	s := &server{service: f}
	req := &documentv1.GetDocumentPreviewsRequest{DocumentIds: []string{"doc-1", "doc-secret"}}
	if _, err := s.GetDocumentPreviews(context.Background(), req); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unauthenticated previews = %v", err)
	}
	if _, err := s.GetDocumentPreviews(documentContextKind(t, trust.SubjectKindService), req); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("service principal previews = %v", err)
	}
	ctx := documentContext(t)
	if _, err := s.GetDocumentPreviews(ctx, &documentv1.GetDocumentPreviewsRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("empty previews = %v", err)
	}
	many := make([]string, MaxPreviewIDs+1)
	for i := range many {
		many[i] = "doc"
	}
	if _, err := s.GetDocumentPreviews(ctx, &documentv1.GetDocumentPreviewsRequest{DocumentIds: many}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("oversized previews = %v", err)
	}
	if _, err := s.GetDocumentPreviews(ctx, &documentv1.GetDocumentPreviewsRequest{DocumentIds: []string{" "}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("blank id previews = %v", err)
	}
	got, err := s.GetDocumentPreviews(ctx, req)
	if err != nil || len(got.GetPreviews()) != 2 || f.tenant != "server" || f.actor != "signed-in-user" || len(f.ids) != 2 {
		t.Fatalf("previews = %+v, %v; call = %+v", got, err, f)
	}
	ready, hidden := got.GetPreviews()[0], got.GetPreviews()[1]
	if !ready.GetReadable() || ready.GetTitle() != "Onboarding guide" || ready.GetOwnerName() != "Ana Lopez" || ready.GetUpdatedAt() == nil || ready.GetSnippet() != "Welcome" {
		t.Fatalf("readable preview = %+v", ready)
	}
	if hidden.GetReadable() || hidden.GetDocumentId() != "doc-secret" || hidden.GetTitle() != "" || hidden.GetOwnerId() != "" || hidden.GetSnippet() != "" || hidden.GetUpdatedAt() != nil {
		t.Fatalf("unreadable preview leaked %+v", hidden)
	}
	// A service without the preview port answers UNAVAILABLE.
	plain := &server{service: &fakeService{}}
	if _, err := plain.GetDocumentPreviews(ctx, req); status.Code(err) != codes.Unavailable {
		t.Fatalf("uncomposed previews = %v", err)
	}
}

func TestDocumentChatReferencesWithholdLocked(t *testing.T) {
	s := &server{service: &previewFake{fakeService: &fakeService{}}}
	read, err := s.GetDocument(documentContext(t), &documentv1.GetDocumentRequest{DocumentId: "doc-1"})
	if err != nil || len(read.GetChannels()) != 2 || len(read.GetPeople()) != 1 || len(read.GetMessages()) != 2 {
		t.Fatalf("read = %+v, %v", read, err)
	}
	open, locked := read.GetChannels()[0], read.GetChannels()[1]
	if open.GetLocked() || open.GetName() != "people-ops" || open.GetMemberCount() != 12 || !open.GetJoined() {
		t.Fatalf("visible channel = %+v", open)
	}
	if !locked.GetLocked() || locked.GetKey() != "id:c-2" || locked.GetName() != "" || locked.GetMemberCount() != 0 || locked.GetConversationId() != "" || locked.GetPrivate() {
		t.Fatalf("locked channel leaked %+v", locked)
	}
	if p := read.GetPeople()[0]; p.GetSubjectId() != "hc-050-rafael-torres" || p.GetDisplayName() != "Rafael Torres" {
		t.Fatalf("person = %+v", p)
	}
	msg, hidden := read.GetMessages()[0], read.GetMessages()[1]
	if !msg.GetReadable() || msg.GetBody() != "Ship it" || msg.GetAuthorName() != "Ana" || msg.GetCreatedAt() == nil {
		t.Fatalf("readable message = %+v", msg)
	}
	if hidden.GetReadable() || hidden.GetToken() != "tok-hidden" || hidden.GetBody() != "" || hidden.GetAuthorName() != "" || hidden.GetChannelName() != "" || hidden.GetConversationId() != "" {
		t.Fatalf("unreadable message leaked %+v", hidden)
	}
}
