package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type issue3788Port struct {
	calls     []string
	principal chat.Principal
	host      string
	org       string
	sendAt    time.Time
	revision  uint64
	rows      []chatrecipient.ScheduledPost
	err       error
}

func (f *issue3788Port) ScheduleSend(_ context.Context, p chat.Principal, host, conversation, body string, sendAt time.Time, zone, org string, _ time.Time) (chatrecipient.ScheduledPost, error) {
	f.calls, f.principal, f.host, f.org, f.sendAt = append(f.calls, "schedule:"+conversation+":"+body+":"+zone), p, host, org, sendAt
	return chatrecipient.ScheduledPost{ID: "s1", ConversationID: conversation, Body: body, SendAt: sendAt, Zone: zone, State: "pending", Revision: 1, QuietPeople: 1}, f.err
}
func (f *issue3788Port) ScheduledPosts(_ context.Context, p chat.Principal, host string) ([]chatrecipient.ScheduledPost, error) {
	f.calls, f.principal, f.host = append(f.calls, "list"), p, host
	return f.rows, f.err
}
func (f *issue3788Port) EditScheduled(_ context.Context, p chat.Principal, host, id, body string, sendAt time.Time, zone string, expected uint64, _ time.Time) (chatrecipient.ScheduledPost, error) {
	f.calls, f.principal, f.host, f.revision = append(f.calls, "edit:"+id+":"+body), p, host, expected
	return chatrecipient.ScheduledPost{ID: id, Body: body, SendAt: sendAt, Zone: zone, State: "pending", Revision: expected + 1}, f.err
}
func (f *issue3788Port) CancelScheduled(_ context.Context, p chat.Principal, host, id string) error {
	f.calls, f.principal, f.host = append(f.calls, "cancel:"+id), p, host
	return f.err
}

func issue3788Serve(t *testing.T, port ScheduledPostsPort, method, target, body string, kind trust.SubjectKind) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, target, strings.NewReader(body)).WithContext(chatsaveHTTPContext(t, kind))
	at := time.Date(2026, 3, 3, 12, 0, 0, 0, time.UTC)
	ScheduledPostsHandler{Service: port, Now: func() time.Time { return at }}.ServeHTTP(w, r)
	return w
}

func TestIssue3788_ScheduledPostsHTTP_TheSessionIsTheAuthor(t *testing.T) {
	port := &issue3788Port{}
	sendAt := time.Date(2026, 3, 4, 7, 30, 0, 0, time.UTC)
	body := `{"conversation_id":"room","body":"Training is tomorrow","send_at":"` + sendAt.Format(time.RFC3339) + `","zone":"Europe/Berlin"}`
	w := issue3788Serve(t, port, http.MethodPost, ScheduledPostsPath+"?subject=someone-else", body, trust.SubjectKindHuman)
	if w.Code != http.StatusCreated || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if port.principal.SubjectID != "person" || port.principal.TenantID != "tenant-a" || port.host != "tenant-a" || !port.sendAt.Equal(sendAt) {
		t.Fatalf("identity not taken from the session: %+v host=%s", port.principal, port.host)
	}
	var reply ScheduledPostReply
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || reply.Post.ID != "s1" || reply.Post.QuietPeople != 1 || reply.Post.Zone != "Europe/Berlin" {
		t.Fatalf("reply=%s err=%v", w.Body.String(), err)
	}
}

func TestIssue3788_ScheduledPostsHTTP_ListEditCancel(t *testing.T) {
	at := time.Date(2026, 3, 4, 7, 30, 0, 0, time.UTC)
	port := &issue3788Port{rows: []chatrecipient.ScheduledPost{{ID: "a", ConversationID: "room", Body: "hello", SendAt: at, Zone: "UTC", State: "failed", Refusal: "channel_closed", Revision: 3, OrgScopeID: "never-sent"}}}
	w := issue3788Serve(t, port, http.MethodGet, ScheduledPostsPath, "", trust.SubjectKindHuman)
	var list ScheduledPostsReply
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || w.Code != 200 || len(list.Posts) != 1 || list.Posts[0].Refusal != "channel_closed" || list.Posts[0].Revision != 3 {
		t.Fatalf("list=%s err=%v", w.Body.String(), err)
	}
	if strings.Contains(w.Body.String(), "never-sent") {
		t.Fatal("the organization scope reached the browser")
	}
	w = issue3788Serve(t, port, http.MethodPut, ScheduledPostsPath, `{"id":"a","body":"new","send_at":"`+at.Format(time.RFC3339)+`","zone":"UTC","revision":3}`, trust.SubjectKindHuman)
	if w.Code != 200 || port.revision != 3 {
		t.Fatalf("edit=%d %s revision=%d", w.Code, w.Body.String(), port.revision)
	}
	w = issue3788Serve(t, port, http.MethodDelete, ScheduledPostsPath, `{"id":"a"}`, trust.SubjectKindHuman)
	if w.Code != 200 || port.calls[len(port.calls)-1] != "cancel:a" {
		t.Fatalf("cancel=%d %s calls=%v", w.Code, w.Body.String(), port.calls)
	}
}

func TestIssue3788_ScheduledPostsHTTP_RefusalShapes_Security(t *testing.T) {
	for _, kind := range []trust.SubjectKind{trust.SubjectKindAgent, trust.SubjectKindService, trust.SubjectKindIntegration} {
		port := &issue3788Port{}
		w := issue3788Serve(t, port, http.MethodPost, ScheduledPostsPath, `{"conversation_id":"room","body":"x","send_at":"2026-03-04T07:30:00Z"}`, kind)
		if w.Code != http.StatusForbidden || len(port.calls) != 0 {
			t.Fatalf("%v: status=%d calls=%v", kind, w.Code, port.calls)
		}
	}
	for err, want := range map[error]int{
		chat.ErrNotFound:                  http.StatusNotFound,
		chat.ErrPermissionDenied:          http.StatusForbidden,
		chat.ErrConflict:                  http.StatusConflict,
		chat.ErrInvalidArgument:           http.StatusBadRequest,
		chatrecipient.ErrScheduledTime:    http.StatusBadRequest,
		chatrecipient.ErrScheduledLimit:   http.StatusUnprocessableEntity,
		chat.ErrUnavailable:               http.StatusServiceUnavailable,
		errors.New("database said no..."): http.StatusServiceUnavailable,
	} {
		w := issue3788Serve(t, &issue3788Port{err: err}, http.MethodDelete, ScheduledPostsPath, `{"id":"a"}`, trust.SubjectKindHuman)
		if w.Code != want || strings.Contains(w.Body.String(), "database") {
			t.Fatalf("%v: status=%d body=%s", err, w.Code, w.Body.String())
		}
	}
	for name, body := range map[string]string{"unknown field": `{"id":"a","author":"someone-else"}`, "trailing data": `{"id":"a"}{}`, "not json": `nope`} {
		if w := issue3788Serve(t, &issue3788Port{}, http.MethodDelete, ScheduledPostsPath, body, trust.SubjectKindHuman); w.Code != http.StatusBadRequest {
			t.Fatalf("%s: status=%d", name, w.Code)
		}
	}
	if w := issue3788Serve(t, &issue3788Port{}, http.MethodPatch, ScheduledPostsPath, "", trust.SubjectKindHuman); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PATCH = %d", w.Code)
	}
	w := httptest.NewRecorder()
	ScheduledPostsHandler{Service: &issue3788Port{}}.ServeHTTP(w, httptest.NewRequest(http.MethodGet, ScheduledPostsPath, nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("no session = %d", w.Code)
	}
}

// ---- delivery ----------------------------------------------------------

type issue3788Row struct {
	chatrecipient.ScheduledPost
	released, finished bool
	failed             string
	claims             int
}

type issue3788Rows struct {
	mu   sync.Mutex
	rows map[string]*issue3788Row
}

func (s *issue3788Rows) InsertScheduled(context.Context, chatrecipient.ScheduledPost, int) (chatrecipient.ScheduledPost, error) {
	return chatrecipient.ScheduledPost{}, nil
}
func (s *issue3788Rows) ListScheduled(context.Context, string, string, string) ([]chatrecipient.ScheduledPost, error) {
	return nil, nil
}
func (s *issue3788Rows) UpdateScheduled(context.Context, string, string, string, string, string, time.Time, string, uint64) (chatrecipient.ScheduledPost, error) {
	return chatrecipient.ScheduledPost{}, nil
}
func (s *issue3788Rows) DeleteScheduled(context.Context, string, string, string, string) error {
	return nil
}
func (s *issue3788Rows) DueScheduled(_ context.Context, host string, now time.Time, _ int) ([]chatrecipient.ScheduledPost, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []chatrecipient.ScheduledPost
	for _, row := range s.rows {
		if row.HostTenantID == host && row.State == chatrecipient.ScheduledPending && !row.SendAt.After(now) {
			out = append(out, row.ScheduledPost)
		}
	}
	return out, nil
}
func (s *issue3788Rows) ClaimScheduled(_ context.Context, _, id string, _ time.Time, _ time.Duration) (chatrecipient.ScheduledPost, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.rows[id]
	if row == nil || row.State != chatrecipient.ScheduledPending {
		return chatrecipient.ScheduledPost{}, false, nil
	}
	row.State, row.Attempts = chatrecipient.ScheduledSending, row.Attempts+1
	row.claims++
	return row.ScheduledPost, true, nil
}
func (s *issue3788Rows) FinishScheduled(_ context.Context, _, id string) error {
	s.rows[id].finished = true
	return nil
}
func (s *issue3788Rows) FailScheduled(_ context.Context, _, id, refusal string) error {
	s.rows[id].failed, s.rows[id].State = refusal, chatrecipient.ScheduledFailed
	return nil
}
func (s *issue3788Rows) ReleaseScheduled(_ context.Context, _, id string, _ int) error {
	s.rows[id].released, s.rows[id].State = true, chatrecipient.ScheduledPending
	return nil
}

type issue3788Sender struct {
	requests []chat.SendPostRequest
	contexts []context.Context
	errs     map[string]error
}

func (s *issue3788Sender) SendPost(ctx context.Context, r chat.SendPostRequest) (chat.Post, error) {
	s.requests, s.contexts = append(s.requests, r), append(s.contexts, ctx)
	if err := s.errs[r.ConversationID]; err != nil {
		return chat.Post{}, err
	}
	return chat.Post{ID: "p", ConversationID: r.ConversationID, Body: r.Body}, nil
}

var issue3788Now = time.Date(2026, 3, 4, 7, 30, 0, 0, time.UTC)

func issue3788Fixture(rows ...chatrecipient.ScheduledPost) (*issue3788Rows, *issue3788Sender, ScheduledPostDelivery) {
	store := &issue3788Rows{rows: map[string]*issue3788Row{}}
	for _, row := range rows {
		if row.State == "" {
			row.State = chatrecipient.ScheduledPending
		}
		store.rows[row.ID] = &issue3788Row{ScheduledPost: row}
	}
	sender := &issue3788Sender{errs: map[string]error{}}
	return store, sender, ScheduledPostDelivery{Store: store, Send: sender, Now: func() time.Time { return issue3788Now }}
}

func issue3788Due(id, conversation string) chatrecipient.ScheduledPost {
	return chatrecipient.ScheduledPost{ID: id, HostTenantID: "tenant-a", HomeTenantID: "tenant-a", AuthorID: "alice", OrgScopeID: "org-1", ConversationID: conversation, Body: "Reminder: training", SendAt: issue3788Now.Add(-time.Minute), Zone: "UTC"}
}

func TestIssue3788_Delivery_SendsAsTheAuthorThroughTheOrdinarySendService(t *testing.T) {
	store, sender, delivery := issue3788Fixture(issue3788Due("s1", "room"))
	sent, err := delivery.Deliver(context.Background(), "tenant-a")
	if err != nil || sent != 1 || len(sender.requests) != 1 {
		t.Fatalf("sent=%d err=%v requests=%d", sent, err, len(sender.requests))
	}
	r := sender.requests[0]
	if r.Principal.SubjectID != "alice" || r.Principal.TenantID != "tenant-a" || r.TenantID != "tenant-a" || r.ConversationID != "room" || r.Body != "Reminder: training" || r.IdempotencyKey != "scheduled:s1" {
		t.Fatalf("request = %+v", r)
	}
	// The send runs under a human principal for exactly the author, flagged as
	// the delivery job, with the organization scope kept at scheduling time.
	verified, ok := trust.FromContext(sender.contexts[0])
	if !ok || verified.Subject() != "alice" || verified.Tenant().String() != "tenant-a" || verified.SubjectKind() != trust.SubjectKindHuman || verified.OrganizationScopeID() != "org-1" {
		t.Fatalf("delivery identity = %+v ok=%v", verified, ok)
	}
	if !scheduledDeliveryFor(sender.contexts[0], "tenant-a", "alice") || scheduledDeliveryFor(sender.contexts[0], "tenant-a", "bob") || scheduledDeliveryFor(context.Background(), "tenant-a", "alice") {
		t.Fatal("the delivery mark must name exactly the author and never exist on an ordinary context")
	}
	if !store.rows["s1"].finished {
		t.Fatal("a sent row must be deleted")
	}
}

func TestIssue3788_Delivery_RefusalPostsNothingAndTellsOnlyTheAuthor_Security(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		"no longer a member":      {chat.ErrPermissionDenied, chatrecipient.ScheduledRefusedAccess},
		"the channel went away":   {chat.ErrNotFound, chatrecipient.ScheduledRefusedAccess},
		"channel locked":          {chat.ErrChannelStatus, chatrecipient.ScheduledRefusedChannel},
		"under a record hold":     {chat.ErrChannelHeld, chatrecipient.ScheduledRefusedChannel},
		"blocked by a filter":     {fmt.Errorf("%w", chatfilter.ErrBlocked), chatrecipient.ScheduledRefusedContent},
		"blocked by a named rule": {&chatfilter.BlockedError{RuleName: "payroll numbers"}, chatrecipient.ScheduledRefusedContent},
	} {
		t.Run(name, func(t *testing.T) {
			store, sender, delivery := issue3788Fixture(issue3788Due("s1", "room"))
			sender.errs["room"] = tc.err
			sent, err := delivery.Deliver(context.Background(), "tenant-a")
			if err != nil || sent != 0 {
				t.Fatalf("sent=%d err=%v", sent, err)
			}
			row := store.rows["s1"]
			if row.failed != tc.want || row.finished || row.released {
				t.Fatalf("row = %+v", row)
			}
			if strings.Contains(row.failed, "payroll") {
				t.Fatal("the name of a rule must not become the reason")
			}
		})
	}
}

func TestIssue3788_Delivery_ATransientFailureIsTriedAgainNotLost(t *testing.T) {
	store, sender, delivery := issue3788Fixture(issue3788Due("s1", "room"))
	sender.errs["room"] = chat.ErrUnavailable
	if sent, err := delivery.Deliver(context.Background(), "tenant-a"); err != nil || sent != 0 {
		t.Fatalf("sent=%d err=%v", sent, err)
	}
	row := store.rows["s1"]
	if !row.released || row.failed != "" || row.finished {
		t.Fatalf("row = %+v", row)
	}
	// The next pass sends it, with the same key, so nothing is posted twice.
	delete(sender.errs, "room")
	if sent, _ := delivery.Deliver(context.Background(), "tenant-a"); sent != 1 {
		t.Fatalf("second pass sent %d", sent)
	}
	if sender.requests[0].IdempotencyKey != sender.requests[1].IdempotencyKey {
		t.Fatalf("keys differ: %q %q", sender.requests[0].IdempotencyKey, sender.requests[1].IdempotencyKey)
	}
}

func TestIssue3788_Delivery_OnlyDueRowsOfTheWorkspaceAreSent(t *testing.T) {
	early := issue3788Due("early", "room")
	early.SendAt = issue3788Now.Add(time.Hour)
	elsewhere := issue3788Due("elsewhere", "room")
	elsewhere.HostTenantID = "tenant-b"
	refused := issue3788Due("refused", "room")
	refused.State = chatrecipient.ScheduledFailed
	store, sender, delivery := issue3788Fixture(early, elsewhere, refused, issue3788Due("due", "room"))
	if sent, err := delivery.Deliver(context.Background(), "tenant-a"); err != nil || sent != 1 {
		t.Fatalf("sent=%d err=%v", sent, err)
	}
	if len(sender.requests) != 1 || sender.requests[0].IdempotencyKey != "scheduled:due" {
		t.Fatalf("requests = %+v", sender.requests)
	}
	for _, id := range []string{"early", "elsewhere", "refused"} {
		if store.rows[id].claims != 0 {
			t.Fatalf("%s was claimed", id)
		}
	}
}

func TestIssue3788_Delivery_AClaimedRowIsNotSentTwice(t *testing.T) {
	store, sender, delivery := issue3788Fixture(issue3788Due("s1", "room"))
	store.rows["s1"].State = chatrecipient.ScheduledSending
	if sent, _ := delivery.Deliver(context.Background(), "tenant-a"); sent != 0 || len(sender.requests) != 0 {
		t.Fatalf("a row another worker holds was sent: %d", len(sender.requests))
	}
}

func TestIssue3788_Delivery_OneRefusalDoesNotStopTheRest(t *testing.T) {
	store, sender, delivery := issue3788Fixture(issue3788Due("bad", "locked"), issue3788Due("good", "open"))
	sender.errs["locked"] = chat.ErrChannelStatus
	if sent, err := delivery.Deliver(context.Background(), "tenant-a"); err != nil || sent != 1 {
		t.Fatalf("sent=%d err=%v", sent, err)
	}
	if !store.rows["good"].finished || store.rows["bad"].failed != chatrecipient.ScheduledRefusedChannel {
		t.Fatalf("good=%+v bad=%+v", store.rows["good"], store.rows["bad"])
	}
}

func TestIssue3788_Delivery_WorkloadNeedsItsPorts(t *testing.T) {
	_, _, delivery := issue3788Fixture()
	if scheduledPostWorkload(ScheduledPostDelivery{}, []string{"tenant-a"}, time.Second) != nil {
		t.Fatal("a delivery with no store or sender must not run")
	}
	if scheduledPostWorkload(delivery, nil, time.Second) != nil || scheduledPostWorkload(delivery, []string{" tenant-a"}, time.Second) != nil || scheduledPostWorkload(delivery, []string{"tenant-a"}, 0) != nil {
		t.Fatal("a delivery with no valid workspace or interval must not run")
	}
	if w := scheduledPostWorkload(delivery, []string{"tenant-a", "tenant-a"}, time.Second); w == nil || w.Name != "chat-scheduled-posts" {
		t.Fatalf("workload = %+v", w)
	}
}

func TestIssue3788_ScheduledRefusal(t *testing.T) {
	if scheduledRefusal(chat.ErrUnavailable) != "" || scheduledRefusal(errors.New("boom")) != "" {
		t.Fatal("an error that may pass is not a refusal")
	}
}

type issue3788RevokedSessions struct{ asked int }

func (r *issue3788RevokedSessions) CheckRevocation(context.Context, string, time.Time) error {
	r.asked++
	return errors.New("session revoked")
}

// A message chosen for 08:30 must go out although the author signed out at 23:00,
// and the exemption is for the delivery job acting as exactly that author.
func TestIssue3788_DeliveryDoesNotNeedTheAuthorsBrowserSession_Security(t *testing.T) {
	sessions := &issue3788RevokedSessions{}
	facts := currentRoleChatFacts{sessions: sessions}
	post := issue3788Due("s1", "room")
	ctx, err := scheduledAuthorContext(context.Background(), post, issue3788Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := facts.CheckChatSession(ctx, "tenant-a", "alice", issue3788Now); err != nil || sessions.asked != 0 {
		t.Fatalf("the delivery job was held to a browser session: err=%v asked=%d", err, sessions.asked)
	}
	// An ordinary request of the same person is still refused when the session is revoked.
	plain := trust.WithPrincipal(context.Background(), mustTrustPrincipal3788(t, "tenant-a", "alice"))
	if err := facts.CheckChatSession(plain, "tenant-a", "alice", issue3788Now); err == nil || sessions.asked != 1 {
		t.Fatalf("a revoked session was accepted: err=%v asked=%d", err, sessions.asked)
	}
	// The mark names one author: it does not carry over to another person's identity.
	other := trust.WithPrincipal(ctx, mustTrustPrincipal3788(t, "tenant-a", "bob"))
	if err := facts.CheckChatSession(other, "tenant-a", "bob", issue3788Now); err == nil {
		t.Fatal("the delivery mark for alice excused bob")
	}
	// A different workspace's alice is a different person.
	if err := facts.CheckChatSession(trust.WithPrincipal(ctx, mustTrustPrincipal3788(t, "tenant-b", "alice")), "tenant-b", "alice", issue3788Now); err == nil {
		t.Fatal("the delivery mark excused a person of another workspace")
	}
}

func mustTrustPrincipal3788(t *testing.T, tenant, subject string) *trust.Principal {
	t.Helper()
	now := time.Now()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(tenant), Subject: subject, SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceLow, SessionRef: "session-" + subject, IssuedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CredentialDigest: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
