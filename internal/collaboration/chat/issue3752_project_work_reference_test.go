package chat

import (
	"context"
	"errors"
	"testing"
	"time"
)

// projectWorkStub is the project authority: it lets exactly one subject read
// one task, and counts how often it was asked.
type projectWorkStub struct {
	subject, project, task string
	title                  string
	err                    error
	calls                  int
}

func (d *projectWorkStub) ProjectWorkVisible(_ context.Context, p Principal, projectID, taskID string) (string, error) {
	d.calls++
	if d.err != nil {
		return "", d.err
	}
	if p.SubjectID != d.subject || projectID != d.project || taskID != d.task {
		return "", ErrNotFound
	}
	return d.title, nil
}

func projectWorkService(t *testing.T, d ProjectWorkDirectory) (*Service, *fakeStore) {
	t.Helper()
	f := &fakeStore{
		conversation: Conversation{ID: "c1", TenantID: "t1", Kind: PublicChannel, OwnerID: "u1", Revision: 1},
		membership:   Membership{ConversationID: "c1", TenantID: "t1", HomeTenantID: "t1", SubjectID: "u1", Role: Member, Revision: 1},
	}
	s := newTestService(f, func() time.Time { return time.Unix(10, 0).UTC() })
	if d != nil {
		s.SetProjectWorkDirectory(d)
	}
	return s, f
}

func projectWorkSend(ref Reference) SendPostRequest {
	return SendPostRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", Body: "look\nhttp://x/workspace/app/project?project=p1&task=k1", IdempotencyKey: "k", References: []Reference{ref}}
}

func TestIssue3752_ProjectWorkReferenceIsStoredWhenSenderMayRead(t *testing.T) {
	d := &projectWorkStub{subject: "u1", project: "p1", task: "k1", title: "Ship it"}
	s, f := projectWorkService(t, d)
	ref := Reference{Kind: ProjectWorkReference, TenantID: "t1", ID: ProjectWorkReferenceID("p1", "k1"), Display: "forged title"}
	if _, err := s.SendPost(context.Background(), projectWorkSend(ref)); err != nil {
		t.Fatalf("send = %v", err)
	}
	if len(f.sent.References) != 1 || f.sent.References[0].Kind != ProjectWorkReference || f.sent.References[0].ID != "p1/k1" {
		t.Fatalf("stored references = %+v", f.sent.References)
	}
	if f.sent.References[0].Display != "Ship it" {
		t.Fatalf("display snapshot = %q, want the authority's title, not the caller's", f.sent.References[0].Display)
	}
}

func TestIssue3752_ProjectWorkReferenceSecurity(t *testing.T) {
	cases := map[string]struct {
		dir  *projectWorkStub
		ref  Reference
		want error
	}{
		"sender cannot read": {&projectWorkStub{subject: "other", project: "p1", task: "k1"}, Reference{Kind: ProjectWorkReference, TenantID: "t1", ID: "p1/k1"}, ErrPermissionDenied},
		"other tenant":       {&projectWorkStub{subject: "u1", project: "p1", task: "k1"}, Reference{Kind: ProjectWorkReference, TenantID: "t2", ID: "p1/k1"}, ErrPermissionDenied},
		"malformed id":       {&projectWorkStub{subject: "u1", project: "p1", task: "k1"}, Reference{Kind: ProjectWorkReference, TenantID: "t1", ID: "p1/k1/x"}, ErrInvalidArgument},
		"empty project":      {&projectWorkStub{subject: "u1", project: "p1", task: "k1"}, Reference{Kind: ProjectWorkReference, TenantID: "t1", ID: "/k1"}, ErrInvalidArgument},
		"authority down":     {&projectWorkStub{err: errors.New("down")}, Reference{Kind: ProjectWorkReference, TenantID: "t1", ID: "p1/k1"}, ErrUnavailable},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s, f := projectWorkService(t, tc.dir)
			if _, err := s.SendPost(context.Background(), projectWorkSend(tc.ref)); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if f.mutations != 0 {
				t.Fatal("a post was written despite the refusal")
			}
		})
	}
	t.Run("no project service composed", func(t *testing.T) {
		s, f := projectWorkService(t, nil)
		_, err := s.SendPost(context.Background(), projectWorkSend(Reference{Kind: ProjectWorkReference, TenantID: "t1", ID: "p1/k1"}))
		if !errors.Is(err, ErrUnavailable) || f.mutations != 0 {
			t.Fatalf("err = %v mutations = %d", err, f.mutations)
		}
	})
}

func TestIssue3752_ProjectWorkReferenceIsInertForReaderWhoMayNotOpenIt(t *testing.T) {
	d := &projectWorkStub{subject: "u1", project: "p1", task: "k1", title: "Ship it"}
	s, _ := projectWorkService(t, d)
	post := Post{ID: "p", TenantID: "t1", ConversationID: "c1", Body: "x", References: []Reference{{Kind: ProjectWorkReference, TenantID: "t1", ID: "p1/k1", Display: "Ship it"}}}
	out := s.projectConversationReferences(context.Background(), Principal{TenantID: "t1", SubjectID: "u2"}, []Post{post})
	got := out[0].References[0]
	if got.Kind != ProjectWorkReference || got.ID != "" || got.Display != restrictedProjectWorkReference {
		t.Fatalf("reader without access sees %+v", got)
	}
	out = s.projectConversationReferences(context.Background(), Principal{TenantID: "t1", SubjectID: "u1"}, []Post{post})
	if got := out[0].References[0]; got.ID != "p1/k1" || got.Display != "Ship it" {
		t.Fatalf("reader with access sees %+v", got)
	}
}

func TestIssue3752_EditedProjectWorkReferenceKeepsTheAuthoritysTitle_Security(t *testing.T) {
	d := &projectWorkStub{subject: "u1", project: "p1", task: "k1", title: "Ship it"}
	s, _ := projectWorkService(t, d)
	forged := Reference{Kind: ProjectWorkReference, TenantID: "t1", ID: "p1/k1", Display: "Fire everyone", ContentType: "image/png"}
	refs, err := s.editReferences(context.Background(), EditPostRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", References: []Reference{forged}}, Post{})
	if err != nil || len(refs) != 1 {
		t.Fatalf("editReferences = %+v, %v", refs, err)
	}
	if refs[0].Display != "Ship it" || refs[0].ContentType != "" {
		t.Fatalf("edit stored %+v, want the authority's title and no stray fields", refs[0])
	}
	if forged.Display != "Fire everyone" {
		t.Fatal("the caller's request was mutated")
	}
	other := &projectWorkStub{subject: "other", project: "p1", task: "k1"}
	s, _ = projectWorkService(t, other)
	if _, err := s.editReferences(context.Background(), EditPostRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", References: []Reference{forged}}, Post{}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("edit by a sender who cannot read it = %v", err)
	}
}

func TestIssue3752_RenamedProjectWorkShowsTheCurrentTitleToReaders(t *testing.T) {
	d := &projectWorkStub{subject: "u1", project: "p1", task: "k1", title: "Ship it v2"}
	s, _ := projectWorkService(t, d)
	post := Post{ID: "p", TenantID: "t1", ConversationID: "c1", Body: "x", References: []Reference{{Kind: ProjectWorkReference, TenantID: "t1", ID: "p1/k1", Display: "Ship it"}}}
	out := s.projectConversationReferences(context.Background(), Principal{TenantID: "t1", SubjectID: "u1"}, []Post{post, post})
	for _, p := range out {
		if got := p.References[0].Display; got != "Ship it v2" {
			t.Fatalf("display = %q, want the renamed title", got)
		}
	}
	if d.calls != 1 {
		t.Fatalf("authority asked %d times for one reference on one page, want 1", d.calls)
	}
	if post.References[0].Display != "Ship it" {
		t.Fatal("the stored post was mutated")
	}
}
