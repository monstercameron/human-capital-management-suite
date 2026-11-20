package application

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/project"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// issue3794Chat lets a conversation be read by the people named in it.
type issue3794Chat struct{ members map[string]bool }

func (c issue3794Chat) ReadAuthorizedReference(_ context.Context, p chat.Principal, tenant, conversation, post string) (chat.Conversation, *chat.Post, error) {
	if p.TenantID != tenant || conversation != "general" || !c.members[p.SubjectID] {
		return chat.Conversation{}, nil, chat.ErrPermissionDenied
	}
	room := chat.Conversation{ID: conversation, TenantID: tenant}
	if post == "" {
		return room, nil, nil
	}
	return room, &chat.Post{ID: post, TenantID: tenant, ConversationID: conversation, AuthorID: "dana"}, nil
}

// issue3794Store is an in-memory offer store with the real store's rules.
type issue3794Store struct {
	mu     sync.Mutex
	offers map[string]chatstore.WorkHandoff
	order  []string
}

func (s *issue3794Store) OfferWorkHandoff(_ context.Context, h chatstore.WorkHandoff) (chatstore.WorkHandoff, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range s.order {
		o := s.offers[id]
		if o.FromID == h.FromID && o.IdempotencyKey == h.IdempotencyKey {
			return o, false, nil
		}
		if o.ProjectID == h.ProjectID && o.TaskID == h.TaskID && o.ToID == h.ToID && o.Mode == h.Mode && o.Status == chatstore.HandoffOffered {
			return o, false, nil
		}
	}
	h.Status = chatstore.HandoffOffered
	s.offers[h.ID] = h
	s.order = append(s.order, h.ID)
	return h, true, nil
}
func (s *issue3794Store) GetWorkHandoff(_ context.Context, _, id string) (chatstore.WorkHandoff, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.offers[id]
	return o, ok, nil
}
func (s *issue3794Store) ListWorkHandoffs(_ context.Context, _, conversation, person string, _ int) ([]chatstore.WorkHandoff, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []chatstore.WorkHandoff
	for _, id := range s.order {
		if o := s.offers[id]; o.ConversationID == conversation && (o.FromID == person || o.ToID == person) {
			out = append(out, o)
		}
	}
	return out, nil
}
func (s *issue3794Store) DecideWorkHandoff(_ context.Context, _, id, status, by string, at time.Time) (chatstore.WorkHandoff, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.offers[id]
	if o.Status != chatstore.HandoffOffered {
		return o, false, nil
	}
	o.Status, o.DecidedBy, o.DecidedAt = status, by, at
	s.offers[id] = o
	return o, true, nil
}

// issue3794Tasks is the owning work service: who may read and edit, and the task.
type issue3794Tasks struct {
	task      projectservice.TaskRecord
	readers   map[string]bool
	editors   map[string]bool
	patches   []projectservice.PatchTaskRequest
	patchBy   []string
	failPatch error
}

func (t *issue3794Tasks) GetTask(_ context.Context, p *trust.Principal, _, _ string) (projectservice.TaskRecord, error) {
	if !t.readers[p.Subject()] {
		return projectservice.TaskRecord{}, projectaccess.ErrUnauthorized
	}
	r := t.task
	r.WorkflowRevision = 4
	return r, nil
}
func (t *issue3794Tasks) CanEditTask(_ context.Context, p *trust.Principal, _ string) error {
	if !t.editors[p.Subject()] {
		return projectaccess.ErrUnauthorized
	}
	return nil
}
func (t *issue3794Tasks) PatchTask(_ context.Context, p *trust.Principal, req projectservice.PatchTaskRequest) (projectservice.TaskRecord, error) {
	if t.failPatch != nil {
		return projectservice.TaskRecord{}, t.failPatch
	}
	t.patches = append(t.patches, req)
	t.patchBy = append(t.patchBy, p.Subject())
	if req.Patch.AssigneeID != nil {
		t.task.AssigneeID = *req.Patch.AssigneeID
	}
	t.task.Revision++
	return t.task, nil
}

type issue3794Fixture struct {
	h     *ChatWorkHandoffs
	tasks *issue3794Tasks
	store *issue3794Store
	proj  *issue3790Projects
}

func issue3794Setup() issue3794Fixture {
	room := issue3794Chat{members: map[string]bool{"lead": true, "priya": true, "sam": true, "dana": true}}
	proj := &issue3790Projects{creatable: map[string]bool{"ops": true}, byKey: map[string]projectservice.TaskRecord{}}
	proj.linked = []projectservice.ChatLinkedTaskRecord{{PostID: "m1", ProjectID: "ops", TaskID: "t-1", Title: "Fix March payroll", StatusID: "todo", AssigneeID: "lead", DueDate: "2026-12-04"}}
	work := NewChatMessageWork(room, proj)
	tasks := &issue3794Tasks{
		task:    projectservice.TaskRecord{ID: "t-1", ProjectID: "ops", Title: "Fix March payroll", AssigneeID: "lead", DueDate: "2026-12-04", Revision: 7},
		readers: map[string]bool{"lead": true, "priya": true}, editors: map[string]bool{"lead": true, "priya": true},
	}
	store := &issue3794Store{offers: map[string]chatstore.WorkHandoff{}}
	h := NewChatWorkHandoffs(work, tasks, store)
	h.now = func() time.Time { return time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC) }
	return issue3794Fixture{h: h, tasks: tasks, store: store, proj: proj}
}

func issue3794Req(mode, key string) ChatWorkHandoffOfferRequest {
	return ChatWorkHandoffOfferRequest{ConversationID: "general", ProjectID: "ops", TaskID: "t-1", Mode: mode, RecipientID: "priya", NextAction: "Confirm the payroll correction", Note: "Needed before Friday", IdempotencyKey: key}
}

func TestIssue3794_OfferShowsActionRecipientSourceAndWorkState_Integration(t *testing.T) {
	f := issue3794Setup()
	got, err := f.h.Offer(context.Background(), issue3790Principal(t, "lead"), issue3794Req(ChatWorkHandoffFollowUp, "attempt-0001"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "OFFERED" || got.Role != "sender" || got.ToID != "priya" || got.NextAction != "Confirm the payroll correction" || got.Note != "Needed before Friday" ||
		got.PostID != "m1" || got.Title != "Fix March payroll" || got.OwnerID != "lead" || got.DueDate != "2026-12-04" || got.OfferedDue != "2026-12-04" || got.Changed || got.Restricted {
		t.Fatalf("offer = %+v", got)
	}
	// The recipient sees the same offer from their side, with nothing else in the room.
	list, err := f.h.List(context.Background(), issue3790Principal(t, "priya"), "general")
	if err != nil || len(list) != 1 || list[0].Role != "recipient" || list[0].Title != "Fix March payroll" {
		t.Fatalf("recipient list = %+v, %v", list, err)
	}
	// Nobody else in the conversation sees it.
	if list, err = f.h.List(context.Background(), issue3790Principal(t, "sam"), "general"); err != nil || len(list) != 0 {
		t.Fatalf("bystander list = %+v, %v", list, err)
	}
	// A follow-up never touches the work.
	if len(f.tasks.patches) != 0 {
		t.Fatalf("offer changed the work: %+v", f.tasks.patches)
	}
}

func TestIssue3794_RetryRecoversTheSameOfferAndMakesNoSecondWork(t *testing.T) {
	f := issue3794Setup()
	lead := issue3790Principal(t, "lead")
	first, err := f.h.Offer(context.Background(), lead, issue3794Req(ChatWorkHandoffTransfer, "attempt-0001"))
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.h.Offer(context.Background(), lead, issue3794Req(ChatWorkHandoffTransfer, "attempt-0001"))
	if err != nil || again.ID != first.ID {
		t.Fatalf("retry = %+v, %v", again, err)
	}
	second, err := f.h.Offer(context.Background(), lead, issue3794Req(ChatWorkHandoffTransfer, "attempt-0002"))
	if err != nil || second.ID != first.ID || len(f.store.offers) != 1 {
		t.Fatalf("second attempt = %+v, %v, offers=%d", second, err, len(f.store.offers))
	}
	if len(f.proj.created) != 0 {
		t.Fatalf("a hand-off created work: %+v", f.proj.created)
	}
}

func TestIssue3794_OfferRefusals_Security(t *testing.T) {
	f := issue3794Setup()
	lead := issue3790Principal(t, "lead")
	cases := []struct {
		name   string
		who    string
		mutate func(*ChatWorkHandoffOfferRequest)
		want   error
	}{
		{"not in the conversation", "outsider", func(*ChatWorkHandoffOfferRequest) {}, ErrChatWorkNotFound},
		{"work not linked to this thread", "lead", func(r *ChatWorkHandoffOfferRequest) { r.TaskID = "t-secret" }, ErrChatWorkNotFound},
		{"recipient outside the conversation", "lead", func(r *ChatWorkHandoffOfferRequest) { r.RecipientID = "stranger" }, ErrChatWorkInvalid},
		{"offer to yourself", "lead", func(r *ChatWorkHandoffOfferRequest) { r.RecipientID = "lead" }, ErrChatWorkInvalid},
		{"no action", "lead", func(r *ChatWorkHandoffOfferRequest) { r.NextAction = "  " }, ErrChatWorkInvalid},
		{"unknown kind", "lead", func(r *ChatWorkHandoffOfferRequest) { r.Mode = "ASSIGN_ALL" }, ErrChatWorkInvalid},
		{"weak key", "lead", func(r *ChatWorkHandoffOfferRequest) { r.IdempotencyKey = "x" }, ErrChatWorkInvalid},
		{"long note", "lead", func(r *ChatWorkHandoffOfferRequest) { r.Note = strings.Repeat("n", 1001) }, ErrChatWorkInvalid},
	}
	for _, c := range cases {
		req := issue3794Req(ChatWorkHandoffFollowUp, "attempt-0001")
		c.mutate(&req)
		who := lead
		if c.who != "lead" {
			who = issue3790Principal(t, c.who)
		}
		if _, err := f.h.Offer(context.Background(), who, req); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
	// Someone who neither owns the work nor may edit it cannot offer it.
	f.tasks.readers["sam"] = true
	f.proj.linked[0].AssigneeID = "lead"
	if _, err := f.h.Offer(context.Background(), issue3790Principal(t, "sam"), issue3794Req(ChatWorkHandoffFollowUp, "attempt-0005")); !errors.Is(err, ErrChatWorkDenied) {
		t.Errorf("non-owner without edit right: %v", err)
	}
	if len(f.store.offers) != 0 {
		t.Fatalf("a refused offer was kept: %+v", f.store.offers)
	}
}

func TestIssue3794_RestrictedWorkIsNeverNamedToAViewerWhoCannotOpenIt_Security(t *testing.T) {
	f := issue3794Setup()
	if _, err := f.h.Offer(context.Background(), issue3790Principal(t, "lead"), issue3794Req(ChatWorkHandoffFollowUp, "attempt-0001")); err != nil {
		t.Fatal(err)
	}
	// The project service stops listing the work for Priya (she cannot read it).
	f.proj.linked = nil
	list, err := f.h.List(context.Background(), issue3790Principal(t, "priya"), "general")
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	got := list[0]
	if !got.Restricted || got.Title != "" || got.OwnerID != "" || got.DueDate != "" || got.ProjectID != "" || got.TaskID != "" || got.WorkStatus != "" {
		t.Fatalf("restricted offer reveals the work: %+v", got)
	}
	// She cannot open the work, so she cannot accept it either; she may still decline.
	f.tasks.readers["priya"] = false
	if _, err := f.h.Accept(context.Background(), issue3790Principal(t, "priya"), got.ID); !errors.Is(err, ErrChatWorkDenied) {
		t.Fatalf("accept without access to the work: %v", err)
	}
	if f.store.offers[got.ID].Status != "OFFERED" {
		t.Fatalf("a refused accept closed the offer: %s", f.store.offers[got.ID].Status)
	}
	if out, err := f.h.Decline(context.Background(), issue3790Principal(t, "priya"), got.ID); err != nil || out.Status != "DECLINED" || out.Title != "" || !out.Restricted {
		t.Fatalf("decline without access to the work = %+v, %v", out, err)
	}
}

func TestIssue3794_SomeoneElsesOpenOfferIsNotShownToAThirdEditor_Security(t *testing.T) {
	f := issue3794Setup()
	first, err := f.h.Offer(context.Background(), issue3790Principal(t, "lead"), issue3794Req(ChatWorkHandoffFollowUp, "attempt-0001"))
	if err != nil {
		t.Fatal(err)
	}
	// Sam may edit the work and offers the same thing to the same person.
	f.tasks.readers["sam"], f.tasks.editors["sam"] = true, true
	got, err := f.h.Offer(context.Background(), issue3790Principal(t, "sam"), issue3794Req(ChatWorkHandoffFollowUp, "attempt-0009"))
	if !errors.Is(err, ErrChatWorkConflict) || got.ID != "" || got.Note != "" || got.NextAction != "" {
		t.Fatalf("third editor saw or took the standing offer: %+v, %v", got, err)
	}
	if len(f.store.offers) != 1 || f.store.offers[first.ID].FromID != "lead" {
		t.Fatalf("offers = %+v", f.store.offers)
	}
}

func TestIssue3794_FollowUpAcceptedLeavesOwnershipAlone_Integration(t *testing.T) {
	f := issue3794Setup()
	offer, err := f.h.Offer(context.Background(), issue3790Principal(t, "lead"), issue3794Req(ChatWorkHandoffFollowUp, "attempt-0001"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.h.Accept(context.Background(), issue3790Principal(t, "priya"), offer.ID)
	if err != nil || got.Status != "ACCEPTED" || got.DecidedBy != "priya" || got.DecidedAt == "" {
		t.Fatalf("accept = %+v, %v", got, err)
	}
	if len(f.tasks.patches) != 0 || f.tasks.task.AssigneeID != "lead" {
		t.Fatalf("an informal hand-off changed the assignment: %+v owner=%s", f.tasks.patches, f.tasks.task.AssigneeID)
	}
	// Accepting again is the same answer.
	if again, err := f.h.Accept(context.Background(), issue3790Principal(t, "priya"), offer.ID); err != nil || again.Status != "ACCEPTED" {
		t.Fatalf("second accept = %+v, %v", again, err)
	}
	// And the offerer sees the real state.
	list, _ := f.h.List(context.Background(), issue3790Principal(t, "lead"), "general")
	if len(list) != 1 || list[0].Status != "ACCEPTED" || list[0].Role != "sender" {
		t.Fatalf("sender list = %+v", list)
	}
}

func TestIssue3794_TransferGoesThroughTheOwningServiceAsTheRecipient_Integration(t *testing.T) {
	f := issue3794Setup()
	offer, err := f.h.Offer(context.Background(), issue3790Principal(t, "lead"), issue3794Req(ChatWorkHandoffTransfer, "attempt-0001"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.h.Accept(context.Background(), issue3790Principal(t, "priya"), offer.ID)
	if err != nil || got.Status != "ACCEPTED" {
		t.Fatalf("accept = %+v, %v", got, err)
	}
	if len(f.tasks.patches) != 1 || f.tasks.patchBy[0] != "priya" {
		t.Fatalf("patches = %+v by %v", f.tasks.patches, f.tasks.patchBy)
	}
	req := f.tasks.patches[0]
	if req.Patch.AssigneeID == nil || *req.Patch.AssigneeID != "priya" || req.Patch.DueDate != nil || req.Patch.Title != nil ||
		req.ExpectedTaskRevision != 7 || req.ExpectedWorkflowRevision != 4 || !strings.HasPrefix(req.IdempotencyKey, "handoff:") {
		t.Fatalf("patch = %+v", req)
	}
	// A repeat accept makes no second change.
	if _, err = f.h.Accept(context.Background(), issue3790Principal(t, "priya"), offer.ID); err != nil || len(f.tasks.patches) != 1 {
		t.Fatalf("repeat accept: %v patches=%d", err, len(f.tasks.patches))
	}
}

func TestIssue3794_TransferRechecksTheRecipientAndTheWork_Security(t *testing.T) {
	// The recipient may read but not edit: the offer stays open and nothing changes.
	f := issue3794Setup()
	f.tasks.editors["priya"] = false
	offer, err := f.h.Offer(context.Background(), issue3790Principal(t, "lead"), issue3794Req(ChatWorkHandoffTransfer, "attempt-0001"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.h.Accept(context.Background(), issue3790Principal(t, "priya"), offer.ID); !errors.Is(err, ErrChatWorkDenied) {
		t.Fatalf("accept without edit right: %v", err)
	}
	if len(f.tasks.patches) != 0 || f.store.offers[offer.ID].Status != "OFFERED" {
		t.Fatalf("a refused accept moved something: patches=%d status=%s", len(f.tasks.patches), f.store.offers[offer.ID].Status)
	}
	// The recipient lost access to the work since the offer.
	f.tasks.readers["priya"] = false
	if _, err = f.h.Accept(context.Background(), issue3790Principal(t, "priya"), offer.ID); !errors.Is(err, ErrChatWorkDenied) {
		t.Fatalf("accept without read right: %v", err)
	}
	// The work was reassigned by someone else meanwhile: the offer is stale.
	f.tasks.readers["priya"], f.tasks.editors["priya"] = true, true
	f.tasks.task.AssigneeID = "sam"
	if _, err = f.h.Accept(context.Background(), issue3790Principal(t, "priya"), offer.ID); !errors.Is(err, ErrChatWorkConflict) {
		t.Fatalf("accept after reassignment: %v", err)
	}
	// So is a changed deadline.
	f.tasks.task.AssigneeID, f.tasks.task.DueDate = "lead", "2027-01-15"
	if _, err = f.h.Accept(context.Background(), issue3790Principal(t, "priya"), offer.ID); !errors.Is(err, ErrChatWorkConflict) {
		t.Fatalf("accept after the deadline moved: %v", err)
	}
	if len(f.tasks.patches) != 0 {
		t.Fatalf("a stale offer changed the work: %+v", f.tasks.patches)
	}
	// A revision race inside the owning service is a conflict, not a success.
	f.tasks.task.DueDate = "2026-12-04"
	f.tasks.failPatch = project.ErrRevisionConflict
	if _, err = f.h.Accept(context.Background(), issue3790Principal(t, "priya"), offer.ID); !errors.Is(err, ErrChatWorkConflict) {
		t.Fatalf("revision conflict: %v", err)
	}
	if f.store.offers[offer.ID].Status != "OFFERED" {
		t.Fatal("offer closed although the transfer failed")
	}
}

func TestIssue3794_UncertainTransferIsRecoveredWithoutASecondChange(t *testing.T) {
	f := issue3794Setup()
	offer, _ := f.h.Offer(context.Background(), issue3790Principal(t, "lead"), issue3794Req(ChatWorkHandoffTransfer, "attempt-0001"))
	// The transfer was made but the answer was lost before the offer was marked.
	f.tasks.task.AssigneeID = "priya"
	got, err := f.h.Accept(context.Background(), issue3790Principal(t, "priya"), offer.ID)
	if err != nil || got.Status != "ACCEPTED" || len(f.tasks.patches) != 0 {
		t.Fatalf("recover = %+v, %v, patches=%d", got, err, len(f.tasks.patches))
	}
}

func TestIssue3794_DeclineAndWithdrawNeverTouchTheWork_Security(t *testing.T) {
	f := issue3794Setup()
	lead, priya, sam := issue3790Principal(t, "lead"), issue3790Principal(t, "priya"), issue3790Principal(t, "sam")
	offer, _ := f.h.Offer(context.Background(), lead, issue3794Req(ChatWorkHandoffTransfer, "attempt-0001"))
	// Only the recipient answers; only the offerer withdraws; others learn nothing.
	for name, call := range map[string]func() error{
		"sender accepts":      func() error { _, err := f.h.Accept(context.Background(), lead, offer.ID); return err },
		"sender declines":     func() error { _, err := f.h.Decline(context.Background(), lead, offer.ID); return err },
		"bystander accepts":   func() error { _, err := f.h.Accept(context.Background(), sam, offer.ID); return err },
		"recipient withdraws": func() error { _, err := f.h.Withdraw(context.Background(), priya, offer.ID); return err },
		"unknown id":          func() error { _, err := f.h.Decline(context.Background(), priya, "wh-nope"); return err },
	} {
		if err := call(); !errors.Is(err, ErrChatWorkNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	got, err := f.h.Decline(context.Background(), priya, offer.ID)
	if err != nil || got.Status != "DECLINED" {
		t.Fatalf("decline = %+v, %v", got, err)
	}
	if _, err = f.h.Accept(context.Background(), priya, offer.ID); !errors.Is(err, ErrChatWorkConflict) {
		t.Fatalf("accept after decline: %v", err)
	}
	if len(f.tasks.patches) != 0 {
		t.Fatalf("declining changed the work: %+v", f.tasks.patches)
	}
	// After a decline the person may offer again, as a new offer.
	fresh, err := f.h.Offer(context.Background(), lead, issue3794Req(ChatWorkHandoffTransfer, "attempt-0002"))
	if err != nil || fresh.ID == offer.ID {
		t.Fatalf("fresh = %+v, %v", fresh, err)
	}
	out, err := f.h.Withdraw(context.Background(), lead, fresh.ID)
	if err != nil || out.Status != "WITHDRAWN" {
		t.Fatalf("withdraw = %+v, %v", out, err)
	}
	if _, err = f.h.Accept(context.Background(), priya, fresh.ID); !errors.Is(err, ErrChatWorkConflict) {
		t.Fatalf("accept a withdrawn offer: %v", err)
	}
}

func TestIssue3794_RecipientWhoLeftTheConversationCannotAct_Security(t *testing.T) {
	f := issue3794Setup()
	offer, _ := f.h.Offer(context.Background(), issue3790Principal(t, "lead"), issue3794Req(ChatWorkHandoffFollowUp, "attempt-0001"))
	room := f.h.work.chat.(issue3794Chat)
	room.members["priya"] = false
	if _, err := f.h.Accept(context.Background(), issue3790Principal(t, "priya"), offer.ID); !errors.Is(err, ErrChatWorkNotFound) {
		t.Fatalf("accept after leaving: %v", err)
	}
	if f.store.offers[offer.ID].Status != "OFFERED" {
		t.Fatal("offer answered by someone outside the conversation")
	}
}

// The HTTP edge: statuses and refusal shapes.
type issue3794Port struct{ h *ChatWorkHandoffs }

func (p issue3794Port) List(ctx context.Context, u *trust.Principal, c string) ([]ChatWorkHandoff, error) {
	return p.h.List(ctx, u, c)
}
func (p issue3794Port) Offer(ctx context.Context, u *trust.Principal, r ChatWorkHandoffOfferRequest) (ChatWorkHandoff, error) {
	return p.h.Offer(ctx, u, r)
}
func (p issue3794Port) Accept(ctx context.Context, u *trust.Principal, id string) (ChatWorkHandoff, error) {
	return p.h.Accept(ctx, u, id)
}
func (p issue3794Port) Decline(ctx context.Context, u *trust.Principal, id string) (ChatWorkHandoff, error) {
	return p.h.Decline(ctx, u, id)
}
func (p issue3794Port) Withdraw(ctx context.Context, u *trust.Principal, id string) (ChatWorkHandoff, error) {
	return p.h.Withdraw(ctx, u, id)
}

func issue3794Serve(t *testing.T, f issue3794Fixture, who, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if who != "" {
		req = req.WithContext(trust.WithPrincipal(req.Context(), issue3790Principal(t, who)))
	}
	rec := httptest.NewRecorder()
	ChatWorkHandoffHandler{Handoffs: issue3794Port{f.h}}.ServeHTTP(rec, req)
	return rec
}

func TestIssue3794_HTTPStatusesAndRefusalShapes_Security(t *testing.T) {
	f := issue3794Setup()
	offerBody := `{"conversation_id":"general","project_id":"ops","task_id":"t-1","mode":"FOLLOW_UP","recipient_id":"priya","next_action":"Confirm it","idempotency_key":"attempt-0001"}`
	if rec := issue3794Serve(t, f, "", http.MethodPost, ChatWorkHandoffPath, offerBody); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous = %d", rec.Code)
	}
	rec := issue3794Serve(t, f, "lead", http.MethodPost, ChatWorkHandoffPath, offerBody)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"OFFERED"`) {
		t.Fatalf("offer = %d %s", rec.Code, rec.Body.String())
	}
	if rec = issue3794Serve(t, f, "lead", http.MethodPost, ChatWorkHandoffPath, `{"unknown":1}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field = %d", rec.Code)
	}
	if rec = issue3794Serve(t, f, "lead", http.MethodDelete, ChatWorkHandoffPath, ""); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("delete = %d", rec.Code)
	}
	if rec = issue3794Serve(t, f, "lead", http.MethodGet, ChatWorkHandoffPath+"/x/accept", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("get accept = %d", rec.Code)
	}
	if rec = issue3794Serve(t, f, "lead", http.MethodPost, ChatWorkHandoffPath+"/x/erase", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown verb = %d", rec.Code)
	}
	// A bystander learns only "not found".
	id := ""
	for k := range f.store.offers {
		id = k
	}
	rec = issue3794Serve(t, f, "sam", http.MethodPost, ChatWorkHandoffPath+"/"+id+"/accept", "")
	if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "Fix March") {
		t.Fatalf("bystander = %d %s", rec.Code, rec.Body.String())
	}
	if rec = issue3794Serve(t, f, "priya", http.MethodPost, ChatWorkHandoffPath+"/"+id+"/decline", ""); rec.Code != http.StatusOK {
		t.Fatalf("decline = %d", rec.Code)
	}
	if rec = issue3794Serve(t, f, "priya", http.MethodPost, ChatWorkHandoffPath+"/"+id+"/accept", ""); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"conflict"`) {
		t.Fatalf("accept after decline = %d %s", rec.Code, rec.Body.String())
	}
	if rec = issue3794Serve(t, f, "priya", http.MethodGet, ChatWorkHandoffPath+"?conversation_id=general", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"DECLINED"`) {
		t.Fatalf("list = %d %s", rec.Code, rec.Body.String())
	}
	if rec = issue3794Serve(t, f, "outsider", http.MethodGet, ChatWorkHandoffPath+"?conversation_id=general", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("outsider list = %d", rec.Code)
	}
}
