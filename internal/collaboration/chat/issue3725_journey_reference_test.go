package chat

import (
	"context"
	"errors"
	"testing"
	"time"
)

// journeyStub is the journey authority: it lets exactly one subject read one
// journey and counts how often it was asked.
type journeyStub struct {
	subject, id, title string
	err                error
	calls              int
}

func (d *journeyStub) JourneyVisible(_ context.Context, p Principal, intentID string) (string, error) {
	d.calls++
	if d.err != nil {
		return "", d.err
	}
	if p.SubjectID != d.subject || intentID != d.id {
		return "", ErrNotFound
	}
	return d.title, nil
}

type journeyLinkRecord struct{ tenant, conversation, intent, by string }

type journeyLinkStub struct {
	got []journeyLinkRecord
	err error
}

func (l *journeyLinkStub) LinkJourneyShare(_ context.Context, tenant, conversation, intentID, sharedBy string) error {
	l.got = append(l.got, journeyLinkRecord{tenant, conversation, intentID, sharedBy})
	return l.err
}

func journeyService(t *testing.T, d JourneyDirectory, l JourneyShareLinker) (*Service, *fakeStore) {
	t.Helper()
	f := &fakeStore{
		conversation: Conversation{ID: "c1", TenantID: "t1", Kind: PublicChannel, OwnerID: "u1", Revision: 1},
		membership:   Membership{ConversationID: "c1", TenantID: "t1", HomeTenantID: "t1", SubjectID: "u1", Role: Member, Revision: 1},
	}
	s := newTestService(f, func() time.Time { return time.Unix(10, 0).UTC() })
	if d != nil {
		s.SetJourneyDirectory(d)
	}
	if l != nil {
		s.SetJourneyShareLinker(l)
	}
	return s, f
}

func journeySend(ref Reference) SendPostRequest {
	return SendPostRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", Body: "please look\nhttp://x/workspace/app/journeys?journey=j1", IdempotencyKey: "k", References: []Reference{ref}}
}

func TestIssue3725_JourneyReferenceIsStoredAndLinkedWhenSenderMayRead(t *testing.T) {
	d := &journeyStub{subject: "u1", id: "j1", title: "Promotion"}
	l := &journeyLinkStub{}
	s, f := journeyService(t, d, l)
	ref := Reference{Kind: JourneyReference, TenantID: "t1", ID: "j1", Display: "forged title", ContentType: "image/png"}
	if _, err := s.SendPost(context.Background(), journeySend(ref)); err != nil {
		t.Fatalf("send = %v", err)
	}
	if len(f.sent.References) != 1 || f.sent.References[0].Kind != JourneyReference || f.sent.References[0].ID != "j1" {
		t.Fatalf("stored references = %+v", f.sent.References)
	}
	if got := f.sent.References[0]; got.Display != "Promotion" || got.ContentType != "" {
		t.Fatalf("stored %+v, want the authority's name and no stray fields", got)
	}
	want := journeyLinkRecord{"t1", "c1", "j1", "u1"}
	if len(l.got) != 1 || l.got[0] != want {
		t.Fatalf("links = %+v, want %+v", l.got, want)
	}
}

func TestIssue3725_JourneyReferenceSecurity(t *testing.T) {
	cases := map[string]struct {
		dir  *journeyStub
		ref  Reference
		want error
	}{
		"sender cannot read": {&journeyStub{subject: "other", id: "j1"}, Reference{Kind: JourneyReference, TenantID: "t1", ID: "j1"}, ErrPermissionDenied},
		"other tenant":       {&journeyStub{subject: "u1", id: "j1"}, Reference{Kind: JourneyReference, TenantID: "t2", ID: "j1"}, ErrPermissionDenied},
		"malformed id":       {&journeyStub{subject: "u1", id: "j1"}, Reference{Kind: JourneyReference, TenantID: "t1", ID: "j1/x"}, ErrInvalidArgument},
		"authority down":     {&journeyStub{err: errors.New("down")}, Reference{Kind: JourneyReference, TenantID: "t1", ID: "j1"}, ErrUnavailable},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			l := &journeyLinkStub{}
			s, f := journeyService(t, tc.dir, l)
			if _, err := s.SendPost(context.Background(), journeySend(tc.ref)); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if f.mutations != 0 || len(l.got) != 0 {
				t.Fatalf("a post (%d) or a link (%d) was written despite the refusal", f.mutations, len(l.got))
			}
		})
	}
	t.Run("no journey service composed", func(t *testing.T) {
		s, f := journeyService(t, nil, nil)
		_, err := s.SendPost(context.Background(), journeySend(Reference{Kind: JourneyReference, TenantID: "t1", ID: "j1"}))
		if !errors.Is(err, ErrUnavailable) || f.mutations != 0 {
			t.Fatalf("err = %v mutations = %d", err, f.mutations)
		}
	})
}

func TestIssue3725_LinkFailureDoesNotLoseTheSentPost(t *testing.T) {
	d := &journeyStub{subject: "u1", id: "j1", title: "Promotion"}
	l := &journeyLinkStub{err: errors.New("link store down")}
	s, f := journeyService(t, d, l)
	if _, err := s.SendPost(context.Background(), journeySend(Reference{Kind: JourneyReference, TenantID: "t1", ID: "j1"})); err != nil {
		t.Fatalf("send = %v, want the post to stand when only the link write fails", err)
	}
	if len(f.sent.References) != 1 {
		t.Fatalf("stored references = %+v", f.sent.References)
	}
}

func TestIssue3725_JourneyReferenceIsInertForReaderWhoMayNotOpenIt(t *testing.T) {
	d := &journeyStub{subject: "u1", id: "j1", title: "Promotion v2"}
	s, _ := journeyService(t, d, nil)
	post := Post{ID: "p", TenantID: "t1", ConversationID: "c1", Body: "x", References: []Reference{{Kind: JourneyReference, TenantID: "t1", ID: "j1", Display: "Promotion"}}}
	out := s.projectConversationReferences(context.Background(), Principal{TenantID: "t1", SubjectID: "u2"}, []Post{post})
	got := out[0].References[0]
	if got.Kind != JourneyReference || got.ID != "" || got.Display != restrictedJourneyReference {
		t.Fatalf("reader without access sees %+v", got)
	}
	out = s.projectConversationReferences(context.Background(), Principal{TenantID: "t1", SubjectID: "u1"}, []Post{post, post})
	for _, p := range out {
		if got := p.References[0]; got.ID != "j1" || got.Display != "Promotion v2" {
			t.Fatalf("reader with access sees %+v", got)
		}
	}
	if d.calls != 2 {
		t.Fatalf("authority asked %d times, want once per distinct journey per reader", d.calls)
	}
}

func TestIssue3725_EditedJourneyReferenceKeepsTheAuthoritysName_Security(t *testing.T) {
	d := &journeyStub{subject: "u1", id: "j1", title: "Promotion"}
	s, _ := journeyService(t, d, nil)
	forged := Reference{Kind: JourneyReference, TenantID: "t1", ID: "j1", Display: "Fire everyone"}
	refs, err := s.editReferences(context.Background(), EditPostRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", References: []Reference{forged}}, Post{})
	if err != nil || len(refs) != 1 || refs[0].Display != "Promotion" {
		t.Fatalf("editReferences = %+v, %v", refs, err)
	}
	other := &journeyStub{subject: "other", id: "j1"}
	s, _ = journeyService(t, other, nil)
	if _, err := s.editReferences(context.Background(), EditPostRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", References: []Reference{forged}}, Post{}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("edit by a sender who cannot read it = %v", err)
	}
}
