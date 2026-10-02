package chat

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

type chatfilterFixture struct {
	action  string
	records []chatfilter.Record
	defs    []chatfilter.Definition
}

type chatfilterTestStore struct {
	*fakeStore
	ChannelStatusStore
}

func (s chatfilterTestStore) ReadChannelStatus(_ context.Context, tenant, channel string) (ChannelStatus, error) {
	return ChannelStatus{TenantID: tenant, ConversationID: channel, Status: chatpolicy.StatusOpen, Revision: 1}, nil
}
func (s chatfilterTestStore) SweepChannelStatuses(context.Context, string, time.Time) (int, error) {
	return 0, ErrUnavailable
}
func (s chatfilterTestStore) CommitChannelStatus(context.Context, ChangeChannelStatusRequest, time.Time, func(context.Context, ChannelStatus) error) (ChannelStatus, error) {
	return ChannelStatus{}, ErrUnavailable
}
func newChatfilterTestService(f *fakeStore, clock Clock) *Service {
	s := NewService(chatfilterTestStore{fakeStore: f}, clock)
	s.SetAuthority(verifiedAuthority{store: f})
	return s
}

func (f *chatfilterFixture) Definitions(context.Context, string) ([]chatfilter.Definition, error) {
	if f.defs != nil {
		return f.defs, nil
	}
	return []chatfilter.Definition{{ID: "rule", Name: "Project rule", Version: "1.0.0", Kind: "words", Match: []string{"quartz"}, Action: f.action, Hard: true}}, nil
}

type filterMediaDirectory struct{ calls int }

func (f *filterMediaDirectory) MediaArtifact(context.Context, string, string, string) (MediaFacts, error) {
	f.calls++
	return MediaFacts{ContentType: "application/x-executable", Admitted: true}, nil
}
func TestTodo_CHATMOD_003_Attachment(t *testing.T) {
	f := &fakeStore{conversation: Conversation{ID: "c", TenantID: "t", Kind: PublicChannel, Revision: 1}, membership: Membership{SubjectID: "writer"}}
	repo := &chatfilterFixture{defs: []chatfilter.Definition{{ID: "rule", Name: "Attachment rule", Version: "1.0.0", Kind: "attachment", Match: []string{"application/x-executable"}, Action: "block"}}}
	policy := &FilterContentPolicy{Filters: &chatfilter.Service{Store: repo, Registry: chatfilter.NewRegistry()}}
	core := newChatfilterTestService(f, time.Now)
	core.SetContentPolicy(policy)
	WithFilterMetadata(core)
	media := &filterMediaDirectory{}
	core.SetMediaDirectory(media)
	_, err := core.SendPost(t.Context(), SendPostRequest{Principal: Principal{TenantID: "t", SubjectID: "writer"}, TenantID: "t", ConversationID: "c", Body: "attachment", IdempotencyKey: "file", References: []Reference{{Kind: MediaAttachment, TenantID: "t", ID: "file"}}})
	if !errors.Is(err, chatfilter.ErrBlocked) || f.mutations != 0 || media.calls != 2 {
		t.Fatalf("omitted MIME bypass: %v mutations %d resolutions %d", err, f.mutations, media.calls)
	}
}
func (f *chatfilterFixture) CreateVersion(context.Context, string, chatfilter.Definition) error {
	return nil
}
func (f *chatfilterFixture) Enablements(context.Context, string) ([]chatfilter.Enablement, error) {
	return []chatfilter.Enablement{{RuleID: "rule", Enabled: true}}, nil
}
func (f *chatfilterFixture) PutEnablement(context.Context, string, chatfilter.Enablement) error {
	return nil
}
func (f *chatfilterFixture) RecordHits(_ context.Context, _ string, records []chatfilter.Record) error {
	f.records = append(f.records, records...)
	return nil
}
func (f *chatfilterFixture) Hits(context.Context, string) ([]chatfilter.Record, error) {
	return f.records, nil
}
func TestTodo_CHATMOD_002(t *testing.T) {
	f := &fakeStore{conversation: Conversation{ID: "c", TenantID: "t", Kind: PublicChannel, Name: "General", Revision: 1}, membership: Membership{SubjectID: "writer", Role: Manager, Revision: 1}, post: Post{ID: "p", TenantID: "t", ConversationID: "c", AuthorID: "writer", AuthorHomeTenantID: "t", Body: "draft", Revision: 1}}
	repo := &chatfilterFixture{action: "block"}
	policy := &FilterContentPolicy{Filters: &chatfilter.Service{Store: repo, Registry: chatfilter.NewRegistry()}}
	s := newChatfilterTestService(f, func() time.Time { return time.Now().UTC() })
	s.SetContentPolicy(policy)
	p := Principal{TenantID: "t", SubjectID: "writer"}
	for _, parent := range []string{"", "p"} {
		before := f.mutations
		_, err := s.SendPost(t.Context(), SendPostRequest{Principal: p, TenantID: "t", ConversationID: "c", Body: "quartz", ParentID: parent, IdempotencyKey: "test"})
		var blocked *chatfilter.BlockedError
		if !errors.As(err, &blocked) || blocked.Span.End != 6 || before != f.mutations {
			t.Fatalf("send/thread bypass: %v mutations %d", err, f.mutations)
		}
	}
	before := f.mutations
	_, err := s.EditPost(t.Context(), EditPostRequest{Principal: p, TenantID: "t", ConversationID: "c", PostID: "p", Body: "quartz", ExpectedRevision: 1})
	if !errors.Is(err, chatfilter.ErrBlocked) || before != f.mutations {
		t.Fatalf("edit bypass: %v", err)
	}
	if err = policy.CheckFilterText(t.Context(), p, f.conversation, "quartz"); !errors.Is(err, chatfilter.ErrBlocked) {
		t.Fatal("metadata entry bypass")
	}
	if len(repo.records) != 4 || repo.records[0].Hit.Version != "1.0.0" {
		t.Fatalf("judgements %+v", repo.records)
	}
}
func TestTodo_CHATMOD_003(t *testing.T) {
	repo := &chatfilterFixture{action: "mask"}
	p := &FilterContentPolicy{Filters: &chatfilter.Service{Store: repo, Registry: chatfilter.NewRegistry()}}
	post := Post{ID: "p", TenantID: "t", AuthorHomeTenantID: "t", AuthorID: "writer", ConversationID: "c", Body: "hello quartz"}
	if err := p.CheckContent(t.Context(), ContentInput{Principal: Principal{SubjectID: "writer"}, Conversation: Conversation{TenantID: "t", ID: "c"}, Body: post.Body}); err != nil {
		t.Fatal(err)
	}
	reader, err := p.MaskedBody(t.Context(), post, Principal{TenantID: "t", SubjectID: "reader"})
	if err != nil || reader != "hello [removed word]" || post.Body != "hello quartz" {
		t.Fatalf("masked reader %q original %q %v", reader, post.Body, err)
	}
	// CHATMOD-002: the author is shown what readers see; the stored post keeps
	// the original text.
	writer, err := p.MaskedBody(t.Context(), post, Principal{TenantID: "t", SubjectID: "writer"})
	if err != nil || writer != "hello [removed word]" || post.Body != "hello quartz" {
		t.Fatalf("author view %q original %q %v", writer, post.Body, err)
	}
	post.Deleted = true
	reader, err = p.MaskedBody(t.Context(), post, Principal{})
	if err != nil || reader != "" {
		t.Fatal("deleted original leaked")
	}
}
