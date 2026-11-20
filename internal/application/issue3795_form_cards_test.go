package application

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// issue3795Chat is a conversation "room" of tenant-a whose members are the
// people who may read it, holding one form card, one plain message and one card
// in a room the readers are not in.
type issue3795Chat struct {
	posts   map[string]chat.Post
	readers map[string]bool
	members []chat.Membership
}

func (f *issue3795Chat) ReadAuthorizedReference(_ context.Context, p chat.Principal, tenant, conversation, post string) (chat.Conversation, *chat.Post, error) {
	if tenant != "tenant-a" || p.TenantID != tenant || conversation != "room" || !f.readers[p.SubjectID] {
		return chat.Conversation{}, nil, chat.ErrNotFound
	}
	got, ok := f.posts[post]
	if !ok {
		return chat.Conversation{}, nil, chat.ErrNotFound
	}
	return chat.Conversation{ID: conversation, TenantID: tenant}, &got, nil
}

func (f *issue3795Chat) ListMemberships(_ context.Context, r chat.ListMembershipsRequest) (chat.ListMembershipsResponse, error) {
	if !f.readers[r.Principal.SubjectID] {
		return chat.ListMembershipsResponse{}, chat.ErrNotFound
	}
	return chat.ListMembershipsResponse{Memberships: f.members}, nil
}

type issue3795Store struct {
	rows  map[string]chatstore.FormSubmission
	fail  error
	calls int
}

func (s *issue3795Store) RecordFormSubmission(_ context.Context, f chatstore.FormSubmission) (bool, error) {
	s.calls++
	if s.fail != nil {
		return false, s.fail
	}
	if _, done := s.rows[f.PostID]; done {
		return false, nil
	}
	s.rows[f.PostID] = f
	return true, nil
}
func (s *issue3795Store) GetFormSubmission(_ context.Context, _, _, post string) (chatstore.FormSubmission, bool, error) {
	row, ok := s.rows[post]
	return row, ok, nil
}
func (s *issue3795Store) FormSubmittedAt(_ context.Context, _, _ string, posts []string) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	for _, id := range posts {
		if row, ok := s.rows[id]; ok {
			out[id] = row.SubmittedAt
		}
	}
	return out, nil
}

func issue3795Fixture(t *testing.T) (*ChatFormCards, *issue3795Store) {
	t.Helper()
	body, err := chat.FormCardBody(chat.FormCard{
		Title: "Salary adjustment", Addressee: "person", FormRef: "salary-adjustment", RunRef: "run-1",
		Fields: []chat.FormField{
			{ID: "reason", Label: "Reason", Type: chat.FormFieldText, Required: true, MaxLength: 40},
			{ID: "effective", Label: "Effective date", Type: chat.FormFieldDate, Required: true},
			{ID: "code", Label: "Reason code", Type: chat.FormFieldSelect, Required: true, Options: []chat.FormOption{{Value: "merit", Label: "Merit"}, {Value: "market", Label: "Market"}}},
			{ID: "approver", Label: "Budget approver", Type: chat.FormFieldPerson},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := time.Unix(1, 0)
	chatFake := &issue3795Chat{
		posts: map[string]chat.Post{
			"card":  {ID: "card", ConversationID: "room", TenantID: "tenant-a", AuthorID: "agent-1", Body: body},
			"plain": {ID: "plain", ConversationID: "room", TenantID: "tenant-a", AuthorID: "agent-1", Body: "hello"},
		},
		readers: map[string]bool{"person": true, "colleague": true, "agent-1": true},
		members: []chat.Membership{
			{SubjectID: "person", HomeTenantID: "tenant-a", JoinedAt: &joined},
			{SubjectID: "boss", HomeTenantID: "tenant-a", JoinedAt: &joined},
			{SubjectID: "gone", HomeTenantID: "tenant-a", JoinedAt: &joined, LeftAt: &joined},
			{SubjectID: "guest", HomeTenantID: "tenant-z", JoinedAt: &joined},
		},
	}
	store := &issue3795Store{rows: map[string]chatstore.FormSubmission{}}
	return &ChatFormCards{Chat: chatFake, Store: store}, store
}

func issue3795Person(t *testing.T, subject string, kind trust.SubjectKind) *trust.Principal {
	t.Helper()
	ctx := chatsaveHTTPContext(t, kind)
	p, _ := trust.FromContext(ctx)
	if subject == "person" {
		return p
	}
	// chatsaveHTTPContext fixes the subject; build the other readers the same way.
	now := time.Now()
	other, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: p.Tenant(), Subject: subject, SubjectKind: kind, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceLow, SessionRef: "session", IssuedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CredentialDigest: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	return other
}

func issue3795Good() map[string]string {
	return map[string]string{"reason": "Promotion", "effective": "2027-02-01", "code": "merit", "approver": "boss"}
}

func TestIssue3795_AddresseeAnswersOnceAndTheRunReadsIt_Integration(t *testing.T) {
	forms, store := issue3795Fixture(t)
	person := issue3795Person(t, "person", trust.SubjectKindHuman)
	var resumed []ChatFormResult
	forms.OnSubmitted = func(_ context.Context, r ChatFormResult) error { resumed = append(resumed, r); return nil }

	views, err := forms.Views(context.Background(), person, "room", []string{"card", "plain", "missing"})
	if err != nil || len(views) != 1 || !views["card"].CanSubmit || views["card"].Submitted {
		t.Fatalf("views before = %+v, %v (only the form card is described)", views, err)
	}
	view, err := forms.Submit(context.Background(), person, "room", "card", issue3795Good())
	if err != nil || !view.Submitted || view.Mine["code"] != "merit" {
		t.Fatalf("submit = %+v, %v", view, err)
	}
	if len(resumed) != 1 || resumed[0].RunRef != "run-1" || resumed[0].Values["approver"] != "boss" {
		t.Fatalf("the waiting run was told %+v", resumed)
	}
	if _, err := forms.Submit(context.Background(), person, "room", "card", issue3795Good()); !errors.Is(err, ErrChatFormConflict) || store.calls != 1 {
		t.Fatalf("second submit = %v after %d writes; a card takes one answer", err, store.calls)
	}
	if views, _ = forms.Views(context.Background(), person, "room", []string{"card"}); views["card"].CanSubmit || !views["card"].Submitted || views["card"].Mine["reason"] != "Promotion" {
		t.Fatalf("views after = %+v", views)
	}
	result, err := forms.Result(context.Background(), chat.Principal{TenantID: "tenant-a", SubjectID: "agent-1"}, "room", "card")
	if err != nil || result.Values["effective"] != "2027-02-01" || result.SubmittedBy != "person" || result.FormRef != "salary-adjustment" || result.RunRef != "run-1" {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestIssue3795_AnswersAreCheckedAgainstTheCard_Security(t *testing.T) {
	forms, store := issue3795Fixture(t)
	person := issue3795Person(t, "person", trust.SubjectKindHuman)
	bad := issue3795Good()
	bad["effective"] = "tomorrow"
	bad["code"] = "bonus"
	bad["approver"] = "gone"
	_, err := forms.Submit(context.Background(), person, "room", "card", bad)
	var invalid *ChatFormInvalidError
	if !errors.As(err, &invalid) || invalid.Fields["effective"] != chat.FormErrBadDate || invalid.Fields["code"] != chat.FormErrBadOption || invalid.Fields["approver"] != chat.FormErrUnknownUser || len(store.rows) != 0 {
		t.Fatalf("err = %v, fields %+v, rows %d", err, invalid, len(store.rows))
	}
	for _, name := range []string{"guest", "stranger"} {
		bad["approver"] = name
		if _, err := forms.Submit(context.Background(), person, "room", "card", bad); !errors.As(err, &invalid) || invalid.Fields["approver"] != chat.FormErrUnknownUser {
			t.Errorf("%s as approver: %v", name, err)
		}
	}
	// A refused answer does not use up the card.
	if _, err := forms.Submit(context.Background(), person, "room", "card", issue3795Good()); err != nil {
		t.Fatalf("a good answer after refused ones: %v", err)
	}
}

func TestIssue3795_OnlyTheAddresseeAnswersAndOnlyTheyReadTheAnswer_Security(t *testing.T) {
	forms, store := issue3795Fixture(t)
	colleague := issue3795Person(t, "colleague", trust.SubjectKindHuman)
	if _, err := forms.Submit(context.Background(), colleague, "room", "card", issue3795Good()); !errors.Is(err, ErrChatFormDenied) || len(store.rows) != 0 {
		t.Fatalf("a colleague answered someone else's form: %v", err)
	}
	if views, err := forms.Views(context.Background(), colleague, "room", []string{"card"}); err != nil || views["card"].CanSubmit {
		t.Fatalf("a colleague is offered the form: %+v %v", views, err)
	}
	if _, err := forms.Submit(context.Background(), issue3795Person(t, "person", trust.SubjectKindHuman), "room", "card", issue3795Good()); err != nil {
		t.Fatal(err)
	}
	views, err := forms.Views(context.Background(), colleague, "room", []string{"card"})
	if err != nil || !views["card"].Submitted || views["card"].Mine != nil {
		t.Fatalf("a colleague sees %+v (%v): they may learn it was answered, not what was said", views, err)
	}
	// Reading the answer back is the asking agent's alone.
	if _, err := forms.Result(context.Background(), chat.Principal{TenantID: "tenant-a", SubjectID: "colleague"}, "room", "card"); !errors.Is(err, ErrChatFormDenied) {
		t.Fatalf("a colleague read the result: %v", err)
	}
	if _, err := forms.Result(context.Background(), chat.Principal{TenantID: "tenant-a", SubjectID: "person"}, "room", "card"); !errors.Is(err, ErrChatFormDenied) {
		t.Fatalf("the person read it through the agent hook: %v", err)
	}
}

func TestIssue3795_CardsOutsideTheReadersRoomsDoNotExist_Security(t *testing.T) {
	forms, store := issue3795Fixture(t)
	stranger := issue3795Person(t, "stranger", trust.SubjectKindHuman)
	if views, err := forms.Views(context.Background(), stranger, "room", []string{"card"}); err != nil || len(views) != 0 {
		t.Fatalf("a person outside the room sees %+v (%v)", views, err)
	}
	if _, err := forms.Submit(context.Background(), stranger, "room", "card", issue3795Good()); !errors.Is(err, ErrChatFormNotFound) {
		t.Fatalf("submit from outside the room: %v", err)
	}
	person := issue3795Person(t, "person", trust.SubjectKindHuman)
	for _, id := range []string{"plain", "nope"} {
		if _, err := forms.Submit(context.Background(), person, "room", id, issue3795Good()); !errors.Is(err, ErrChatFormNotFound) {
			t.Errorf("submit to %q: %v", id, err)
		}
	}
	if _, err := forms.Submit(context.Background(), person, "other-room", "card", issue3795Good()); !errors.Is(err, ErrChatFormNotFound) {
		t.Errorf("submit through another conversation: %v", err)
	}
	if _, err := forms.Result(context.Background(), chat.Principal{TenantID: "tenant-b", SubjectID: "agent-1"}, "room", "card"); !errors.Is(err, ErrChatFormNotFound) {
		t.Errorf("result across tenants: %v", err)
	}
	// Agents answer nothing here.
	agent := issue3795Person(t, "person", trust.SubjectKindAgent)
	if _, err := forms.Submit(context.Background(), agent, "room", "card", issue3795Good()); !errors.Is(err, ErrChatFormDenied) || len(store.rows) != 0 {
		t.Errorf("an agent answered: %v", err)
	}
	// An unanswered card has no result yet.
	if _, err := forms.Result(context.Background(), chat.Principal{TenantID: "tenant-a", SubjectID: "agent-1"}, "room", "card"); !errors.Is(err, ErrChatFormNotFound) {
		t.Errorf("result before an answer: %v", err)
	}
}

func TestIssue3795_AFailedResumeNeverTakesTheAnswerBack(t *testing.T) {
	forms, store := issue3795Fixture(t)
	forms.OnSubmitted = func(context.Context, ChatFormResult) error { return errors.New("run is gone") }
	if _, err := forms.Submit(context.Background(), issue3795Person(t, "person", trust.SubjectKindHuman), "room", "card", issue3795Good()); err != nil || len(store.rows) != 1 {
		t.Fatalf("submit = %v, rows %d", err, len(store.rows))
	}
}

func issue3795Serve(t *testing.T, forms ChatFormPort, kind trust.SubjectKind, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, strings.NewReader(body)).WithContext(chatsaveHTTPContext(t, kind))
	recorder := httptest.NewRecorder()
	ChatFormHandler{Forms: forms}.ServeHTTP(recorder, request)
	return recorder
}

func TestIssue3795_FormRoutesAnswerWithTheCardAndRefuseWithShortCodes_Security(t *testing.T) {
	forms, store := issue3795Fixture(t)
	read := issue3795Serve(t, forms, trust.SubjectKindHuman, http.MethodGet, "/api/chat/conversations/room/forms?post=card&post=plain", "")
	var reply chatFormReply
	if err := json.Unmarshal(read.Body.Bytes(), &reply); read.Code != 200 || err != nil || !reply.Views["card"].CanSubmit || len(reply.Views) != 1 {
		t.Fatalf("read = %d %s", read.Code, read.Body.String())
	}
	invalid := issue3795Serve(t, forms, trust.SubjectKindHuman, http.MethodPost, "/api/chat/conversations/room/forms/card/answers", `{"values":{"reason":"","effective":"2027-13-01","code":"merit"}}`)
	var refused chatFormError
	if err := json.Unmarshal(invalid.Body.Bytes(), &refused); invalid.Code != 400 || err != nil || refused.Code != "invalid_form" || refused.Fields["reason"] != chat.FormErrRequired || refused.Fields["effective"] != chat.FormErrBadDate {
		t.Fatalf("invalid = %d %s", invalid.Code, invalid.Body.String())
	}
	if len(store.rows) != 0 {
		t.Fatal("a refused answer was recorded")
	}
	ok := issue3795Serve(t, forms, trust.SubjectKindHuman, http.MethodPost, "/api/chat/conversations/room/forms/card/answers", `{"values":{"reason":"Promotion","effective":"2027-02-01","code":"merit"}}`)
	if ok.Code != 200 || !strings.Contains(ok.Body.String(), `"submitted":true`) || len(store.rows) != 1 {
		t.Fatalf("ok = %d %s", ok.Code, ok.Body.String())
	}
	cases := []struct {
		name         string
		kind         trust.SubjectKind
		method, path string
		body         string
		want         int
	}{
		{"second answer", trust.SubjectKindHuman, http.MethodPost, "/api/chat/conversations/room/forms/card/answers", `{"values":{"reason":"x","effective":"2027-02-01","code":"merit"}}`, 409},
		{"agent", trust.SubjectKindAgent, http.MethodPost, "/api/chat/conversations/room/forms/card/answers", `{"values":{}}`, 403},
		{"wrong method", trust.SubjectKindHuman, http.MethodGet, "/api/chat/conversations/room/forms/card/answers", "", 405},
		{"unknown member", trust.SubjectKindHuman, http.MethodPost, "/api/chat/conversations/room/forms/card/answers", `{"values":{},"is_admin":true}`, 400},
		{"no values", trust.SubjectKindHuman, http.MethodPost, "/api/chat/conversations/room/forms/card/answers", `{}`, 400},
		{"not a form", trust.SubjectKindHuman, http.MethodPost, "/api/chat/conversations/room/forms/plain/answers", `{"values":{}}`, 404},
		{"no posts asked", trust.SubjectKindHuman, http.MethodGet, "/api/chat/conversations/room/forms", "", 400},
		{"other route", trust.SubjectKindHuman, http.MethodGet, "/api/chat/conversations/room/forms/card", "", 404},
	}
	for _, c := range cases {
		got := issue3795Serve(t, forms, c.kind, c.method, c.path, c.body)
		if got.Code != c.want || strings.Contains(got.Body.String(), "chat form:") {
			t.Errorf("%s: status %d body %s, want %d with a short code", c.name, got.Code, got.Body.String(), c.want)
		}
	}
	if got := issue3795Serve(t, nil, trust.SubjectKindHuman, http.MethodGet, "/api/chat/conversations/room/forms?post=card", ""); got.Code != 503 {
		t.Errorf("no port: %d", got.Code)
	}
}
