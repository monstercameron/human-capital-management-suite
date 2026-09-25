package project

import (
	"context"
	"testing"
	"time"

	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	applicationactivity "github.com/monstercameron/human-capital-management-suite/internal/application/projectactivity"
	domainactivity "github.com/monstercameron/human-capital-management-suite/internal/domains/projectactivity"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeActivityService struct {
	seen    *trust.Principal
	err     error
	add     applicationactivity.AddCommentRequest
	edit    applicationactivity.ReviseCommentRequest
	deleted applicationactivity.ReviseCommentRequest
	list    applicationactivity.ListRequest
}

func (f *fakeActivityService) comment() domainactivity.Comment {
	return domainactivity.Comment{ID: "comment-1", CreatedAt: time.Unix(10, 0).UTC(), Revisions: []domainactivity.Revision{{Number: 2, Text: domainactivity.SafeText{Source: "safe source", HTML: "<p>safe html</p>"}, ActorID: "member-1", At: time.Unix(20, 0).UTC()}}}
}

func (f *fakeActivityService) AddComment(_ context.Context, p *trust.Principal, req applicationactivity.AddCommentRequest) (domainactivity.Comment, error) {
	f.seen, f.add = p, req
	if f.err != nil {
		return domainactivity.Comment{}, f.err
	}
	return f.comment(), nil
}
func (f *fakeActivityService) EditComment(_ context.Context, p *trust.Principal, req applicationactivity.ReviseCommentRequest) (domainactivity.Comment, error) {
	f.seen, f.edit = p, req
	if f.err != nil {
		return domainactivity.Comment{}, f.err
	}
	return f.comment(), nil
}
func (f *fakeActivityService) DeleteComment(_ context.Context, p *trust.Principal, req applicationactivity.ReviseCommentRequest) (domainactivity.Comment, error) {
	f.seen, f.deleted = p, req
	if f.err != nil {
		return domainactivity.Comment{}, f.err
	}
	comment := f.comment()
	comment.Revisions[0].Tombstone = true
	comment.Revisions[0].Text = domainactivity.SafeText{Source: "must not be exposed", HTML: "<script>unsafe</script>"}
	return comment, nil
}
func (f *fakeActivityService) ListComments(_ context.Context, p *trust.Principal, req applicationactivity.ListRequest) (applicationactivity.CommentPage, error) {
	f.seen, f.list = p, req
	if f.err != nil {
		return applicationactivity.CommentPage{}, f.err
	}
	return applicationactivity.CommentPage{Comments: []domainactivity.Comment{f.comment()}, NextCursor: "next-comment-cursor"}, nil
}
func (f *fakeActivityService) ListActivity(_ context.Context, p *trust.Principal, req applicationactivity.ListRequest) (applicationactivity.ActivityPage, error) {
	f.seen, f.list = p, req
	if f.err != nil {
		return applicationactivity.ActivityPage{}, f.err
	}
	return applicationactivity.ActivityPage{Entries: []domainactivity.Activity{{Sequence: 3, CommentID: "comment-1", ActorID: "member-1", Kind: "COMMENT_CORRECTED", Revision: 2, At: time.Unix(20, 0).UTC()}}, NextCursor: "next-activity-cursor"}, nil
}

func TestTaskCommentAndActivityRPCsDelegateSafely(t *testing.T) {
	ctx, principal := testContext(t)
	fake := &fakeActivityService{}
	s := &server{activity: fake}
	added, err := s.AddTaskComment(ctx, &projectv1.AddTaskCommentRequest{ProjectId: "p1", TaskId: "t1", IdempotencyKey: "add-key", BodyText: "<b>hello</b>"})
	if err != nil || added.GetComment().GetSafeHtml() != "<p>safe html</p>" || added.GetComment().GetSourceText() != "safe source" || added.GetComment().GetActorId() != "member-1" {
		t.Fatalf("add comment=%+v err=%v", added, err)
	}
	if fake.seen != principal || fake.add.ProjectID != "p1" || fake.add.TaskID != "t1" || fake.add.Text != "<b>hello</b>" || fake.add.IdempotencyKey != "add-key" {
		t.Fatalf("add request/principal: %+v, %p", fake.add, fake.seen)
	}
	edited, err := s.EditTaskComment(ctx, &projectv1.EditTaskCommentRequest{ProjectId: "p1", TaskId: "t1", CommentId: "comment-1", ExpectedRevision: 1, IdempotencyKey: "edit-key", BodyText: "corrected"})
	if err != nil || edited.GetComment().GetCurrentRevision() != 2 || fake.seen != principal || fake.edit.ExpectedRevision != 1 || fake.edit.IdempotencyKey != "edit-key" || fake.edit.Text != "corrected" {
		t.Fatalf("edit comment=%+v request=%+v err=%v", edited, fake.edit, err)
	}
	deleted, err := s.DeleteTaskComment(ctx, &projectv1.DeleteTaskCommentRequest{ProjectId: "p1", TaskId: "t1", CommentId: "comment-1", ExpectedRevision: 2, IdempotencyKey: "delete-key"})
	if err != nil || !deleted.GetComment().GetTombstone() || deleted.GetComment().GetSafeHtml() != "" || deleted.GetComment().GetSourceText() != "" || fake.seen != principal || fake.deleted.ExpectedRevision != 2 {
		t.Fatalf("delete comment=%+v request=%+v err=%v", deleted, fake.deleted, err)
	}
	comments, err := s.ListTaskComments(ctx, &projectv1.ListTaskCommentsRequest{ProjectId: "p1", TaskId: "t1", PageSize: 10, PageCursor: ""})
	if err != nil || len(comments.GetComments()) != 1 || comments.GetNextPageCursor() != "next-comment-cursor" || fake.seen != principal || fake.list.PageSize != 10 {
		t.Fatalf("list comments=%+v request=%+v err=%v", comments, fake.list, err)
	}
	activity, err := s.ListTaskActivity(ctx, &projectv1.ListTaskActivityRequest{ProjectId: "p1", TaskId: "t1", PageSize: 0})
	if err != nil || len(activity.GetEntries()) != 1 || activity.GetEntries()[0].GetActorId() != "member-1" || activity.GetEntries()[0].GetSequence() != 3 || fake.seen != principal || fake.list.PageSize != domainactivity.DefaultPageSize {
		t.Fatalf("list activity=%+v request=%+v err=%v", activity, fake.list, err)
	}
}

func TestTaskActivityUnavailableDeniedAndBounds(t *testing.T) {
	ctx, _ := testContext(t)
	if _, err := (&server{}).ListTaskActivity(ctx, &projectv1.ListTaskActivityRequest{ProjectId: "p1", TaskId: "t1"}); status.Code(err) != codes.Unavailable {
		t.Fatalf("missing activity service=%v", err)
	}
	fake := &fakeActivityService{err: domainactivity.ErrDenied}
	s := &server{activity: fake}
	if _, err := s.AddTaskComment(ctx, &projectv1.AddTaskCommentRequest{ProjectId: "p1", TaskId: "t1"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("denied add=%v", err)
	}
	fake.err = nil
	for _, req := range []*projectv1.ListTaskCommentsRequest{{ProjectId: "p1", TaskId: "t1", PageSize: -1}, {ProjectId: "p1", TaskId: "t1", PageSize: 101}, {ProjectId: "p1", TaskId: "t1", PageCursor: "malformed"}} {
		if _, err := s.ListTaskComments(ctx, req); status.Code(err) != codes.InvalidArgument {
			t.Errorf("bad comment page %+v: %v", req, err)
		}
	}
	if _, err := s.ListTaskActivity(ctx, &projectv1.ListTaskActivityRequest{ProjectId: "p1", TaskId: "t1", PageSize: 101}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("oversized activity page=%v", err)
	}
}
