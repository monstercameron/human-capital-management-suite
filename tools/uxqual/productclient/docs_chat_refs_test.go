package productclient

import (
	"testing"
	"time"

	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestProjectDocumentChatRefsWithholdsLocked(t *testing.T) {
	at := time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC)
	response := &documentv1.GetDocumentResponse{
		Document: &documentv1.DocumentSummary{DocumentId: "doc-1", Title: "Guide"},
		Channels: []*documentv1.DocumentChannelReference{
			{Key: "name:people-ops", ConversationId: "c-1", Name: "people-ops", MemberCount: 9},
			{Key: "id:c-2", Locked: true, Name: "must-not-show", MemberCount: 3},
			{Key: "name:half", Name: ""},
			nil,
		},
		People:   []*documentv1.DocumentPersonReference{{Key: "rafael.torres", SubjectId: "hc-050", DisplayName: "Rafael Torres"}, {Key: "x"}},
		Messages: []*documentv1.DocumentMessageReference{{Token: "t1", Readable: true, AuthorName: "Ana", Body: "hi", CreatedAt: timestamppb.New(at)}, {Token: "t2", Body: "secret", AuthorName: "CEO"}},
	}
	detail := projectDocument(response, nil, false)
	refs := detail.Chat
	if len(refs.Channels) != 2 || refs.Channels[0].MemberCount != 9 || refs.Channels[1].Name != "" || refs.Channels[1].MemberCount != 0 || !refs.Channels[1].Locked {
		t.Fatalf("channels = %+v", refs.Channels)
	}
	if len(refs.People) != 1 || refs.People[0].SubjectID != "hc-050" {
		t.Fatalf("people = %+v", refs.People)
	}
	if len(refs.Messages) != 2 || !refs.Messages[0].CreatedAt.Equal(at) || refs.Messages[1].Body != "" || refs.Messages[1].AuthorName != "" {
		t.Fatalf("messages = %+v", refs.Messages)
	}
}
