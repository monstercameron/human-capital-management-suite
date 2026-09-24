package productclient

import (
	"testing"
	"time"

	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestTodo_HUB_032_ProductClientProjectsAuthorizedDocumentSummaries(t *testing.T) {
	rows := projectDocuments([]*documentv1.DocumentSummary{
		{DocumentId: "doc-1", Title: "Handbook", OwnerId: "person-1", VersionId: "version-3", Status: "team_official", ScopeKind: "TEAM", ScopeId: "people", SharingState: "audience", ReviewDueAt: timestamppb.New(time.Date(2026, 10, 1, 12, 0, 0, 0, time.FixedZone("x", -4*60*60))), UpdatedAt: timestamppb.New(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC))},
		{DocumentId: "", Title: "should be omitted"},
	})
	if len(rows) != 1 {
		t.Fatalf("projected rows = %+v", rows)
	}
	got := rows[0]
	if got.ID != "doc-1" || got.OwnerID != "person-1" || got.VersionID != "version-3" || got.Scope != "TEAM:people" || got.ReviewDue != "2026-10-01" || got.UpdatedAt != "2026-09-22T12:00:00Z" || got.Status != productui.DocumentTeamOfficial {
		t.Fatalf("projection lost authorized fields: %+v", got)
	}
}

func TestDocumentCommentsProjectionPreservesVersion(t *testing.T) {
	detail := projectDocument(&documentv1.GetDocumentResponse{Document: &documentv1.DocumentSummary{DocumentId: "doc-1", Title: "Handbook", VersionId: "version-3", CanComment: true}, Markdown: "body"}, &documentv1.ListDocumentCommentsResponse{Comments: []*documentv1.DocumentComment{{Id: "comment-1", AuthorId: "person-1", Body: "Needs context", VersionId: "version-3", CreatedAt: timestamppb.New(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC))}}}, false)
	if detail == nil || !detail.CanComment || len(detail.Comments) != 1 {
		t.Fatalf("comment detail projection = %+v", detail)
	}
	comment := detail.Comments[0]
	if comment.ID != "comment-1" || comment.AuthorID != "person-1" || comment.Body != "Needs context" || comment.VersionID != "version-3" || comment.CreatedAt != "2026-09-22T12:00:00Z" {
		t.Fatalf("comment projection = %+v", comment)
	}
}

func TestDocumentCommentsProjectionPreservesLoadFailure(t *testing.T) {
	detail := projectDocument(&documentv1.GetDocumentResponse{Document: &documentv1.DocumentSummary{DocumentId: "doc-1", Title: "Handbook", VersionId: "version-3"}}, nil, true)
	if detail == nil || !detail.CommentsUnavailable || len(detail.Comments) != 0 {
		t.Fatalf("comment load failure projection = %+v", detail)
	}
}
