package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// stageStore is a store that can write a system line: it records the line it was
// handed and the author it was asked to attribute it to.
type stageStore struct {
	*fakeStore
	lines []Post
	keys  []string
	err   error
}

func (s *stageStore) SendSystemLine(_ context.Context, r SendPostRequest, p Post) (Post, error) {
	if s.err != nil {
		return Post{}, s.err
	}
	s.lines = append(s.lines, p)
	s.keys = append(s.keys, r.IdempotencyKey)
	return p, nil
}

func stageService(t *testing.T, dir JourneyDirectory) (*Service, *stageStore) {
	t.Helper()
	f := &fakeStore{conversation: Conversation{ID: "c1", TenantID: "t1", Kind: PublicChannel, OwnerID: "u1", Revision: 1}}
	st := &stageStore{fakeStore: f}
	s := NewService(st, func() time.Time { return time.Unix(10, 0).UTC() })
	s.SetAuthority(verifiedAuthority{store: f})
	if dir != nil {
		s.SetJourneyDirectory(dir)
	}
	return s, st
}

func stagePost(outcome string) JourneyStagePost {
	return JourneyStagePost{TenantID: "t1", ConversationID: "c1", JourneyID: "j1", Outcome: outcome, Decider: Principal{TenantID: "t1", SubjectID: "approver"}, IdempotencyKey: "journey-stage:j1:" + outcome}
}

func TestIssue3728_BodyRoundTripsAndNamesNoReason(t *testing.T) {
	for _, outcome := range []string{JourneyStageApproved, JourneyStageRejected, JourneyStageBlocked} {
		body, err := JourneyStageBody(JourneyStageLine{Outcome: outcome, Decider: "approver"})
		if err != nil {
			t.Fatal(err)
		}
		line, ok := ParseJourneyStage(body)
		if !ok || line.Outcome != outcome || line.Decider != "approver" {
			t.Fatalf("%s: %+v %v", outcome, line, ok)
		}
		if !IsSystemBody(body) {
			t.Fatalf("%s: a stage line is a system line", outcome)
		}
	}
	for _, bad := range []JourneyStageLine{{Outcome: "approved"}, {Outcome: "shrugged", Decider: "a"}, {Outcome: "approved", Decider: "a|b"}, {Outcome: "approved", Decider: " a"}} {
		if _, err := JourneyStageBody(bad); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	if _, ok := ParseJourneyStage("approved by a person"); ok {
		t.Fatal("plain text parsed as a stage line")
	}
}

func TestIssue3728_StageLineIsPostedAttributedToTheDeciderWithATypedReference_Integration(t *testing.T) {
	dir := &journeyStub{subject: "approver", id: "j1", title: "Promotion"}
	s, st := stageService(t, dir)
	post, err := s.PostJourneyStage(context.Background(), stagePost(JourneyStageApproved))
	if err != nil {
		t.Fatal(err)
	}
	if len(st.lines) != 1 || post.ID == "" {
		t.Fatalf("lines %d", len(st.lines))
	}
	line := st.lines[0]
	if line.AuthorID != "approver" || line.TenantID != "t1" || line.ConversationID != "c1" {
		t.Fatalf("attribution %+v", line)
	}
	if parsed, ok := ParseJourneyStage(line.Body); !ok || parsed.Outcome != JourneyStageApproved || parsed.Decider != "approver" {
		t.Fatalf("body %q", line.Body)
	}
	if len(line.References) != 1 || line.References[0].Kind != JourneyReference || line.References[0].ID != "j1" || line.References[0].Display != "Promotion" {
		t.Fatalf("references %+v", line.References)
	}
	if st.keys[0] != "journey-stage:j1:approved" {
		t.Fatalf("key %q", st.keys[0])
	}
}

func TestIssue3728_StageLineRefusals_Security(t *testing.T) {
	good := &journeyStub{subject: "approver", id: "j1", title: "Promotion"}
	cases := map[string]struct {
		dir    JourneyDirectory
		mutate func(*JourneyStagePost, *Service, *stageStore)
	}{
		"decider cannot open the journey": {dir: &journeyStub{subject: "someone-else", id: "j1"}},
		"no journey authority composed":   {dir: nil},
		"unknown outcome":                 {dir: good, mutate: func(r *JourneyStagePost, _ *Service, _ *stageStore) { r.Outcome = "approved\nrejected" }},
		"other tenant":                    {dir: good, mutate: func(r *JourneyStagePost, _ *Service, _ *stageStore) { r.TenantID = "t2" }},
		"decider from another tenant":     {dir: good, mutate: func(r *JourneyStagePost, _ *Service, _ *stageStore) { r.Decider.TenantID = "t2" }},
		"malformed journey id":            {dir: good, mutate: func(r *JourneyStagePost, _ *Service, _ *stageStore) { r.JourneyID = "../j1" }},
		"missing key":                     {dir: good, mutate: func(r *JourneyStagePost, _ *Service, _ *stageStore) { r.IdempotencyKey = " " }},
		"archived conversation": {dir: good, mutate: func(_ *JourneyStagePost, _ *Service, st *stageStore) {
			st.fakeStore.conversation.Archived = true
		}},
		"missing conversation": {dir: good, mutate: func(r *JourneyStagePost, _ *Service, _ *stageStore) { r.ConversationID = "" }},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s, st := stageService(t, tc.dir)
			r := stagePost(JourneyStageApproved)
			if tc.mutate != nil {
				tc.mutate(&r, s, st)
			}
			if _, err := s.PostJourneyStage(context.Background(), r); err == nil {
				t.Fatal("posted a stage line")
			}
			if len(st.lines) != 0 {
				t.Fatalf("a refused stage line was written: %+v", st.lines)
			}
		})
	}
}

func TestIssue3728_StoreWithoutSystemLinesFailsClosed(t *testing.T) {
	f := &fakeStore{conversation: Conversation{ID: "c1", TenantID: "t1", Kind: PublicChannel, OwnerID: "u1", Revision: 1}}
	s := NewService(f, func() time.Time { return time.Unix(10, 0).UTC() })
	s.SetAuthority(verifiedAuthority{store: f})
	s.SetJourneyDirectory(&journeyStub{subject: "approver", id: "j1"})
	if _, err := s.PostJourneyStage(context.Background(), stagePost(JourneyStageRejected)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err %v", err)
	}
	if f.mutations != 0 {
		t.Fatal("wrote through the ordinary post path")
	}
}

// Only PostJourneyStage writes the line: a person cannot send, or edit a post
// into, a body that reads as a decision.
func TestIssue3728_PeopleCannotForgeAStageLine_Security(t *testing.T) {
	s, st := stageService(t, &journeyStub{subject: "u1", id: "j1"})
	body, _ := JourneyStageBody(JourneyStageLine{Outcome: JourneyStageApproved, Decider: "u1"})
	for _, b := range []string{body, "see " + body} {
		_, err := s.SendPost(context.Background(), SendPostRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", Body: b, IdempotencyKey: "k"})
		if !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("send %q: %v", strings.TrimSpace(b), err)
		}
	}
	if st.fakeStore.mutations != 0 || len(st.lines) != 0 {
		t.Fatal("a forged stage line was stored")
	}
	if !carriesJourneyStageMarker("x" + body) {
		t.Fatal("marker not detected mid-body")
	}
}

// A decision line is a record of the room, like the "added people" line: Save
// for later refuses it, so the saved list never shows the marker text.
func TestIssue3728_DecisionLineIsNotSaved(t *testing.T) {
	s, f, r := chatsaveService()
	body, err := JourneyStageBody(JourneyStageLine{Outcome: JourneyStageApproved, Decider: "approver"})
	if err != nil {
		t.Fatal(err)
	}
	f.post.Body = body
	if _, err := s.SaveForLater(context.Background(), r); !errors.Is(err, ErrInvalidArgument) || len(f.items) != 0 {
		t.Fatalf("the decision line was saved: %v, %d items", err, len(f.items))
	}
}
