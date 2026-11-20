package application

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type caseStoreFake struct {
	binding chat.CaseBinding
	bound   bool
	binds   int
	unbinds int
	err     error
}

func (f *caseStoreFake) ActiveCase(context.Context, string, string) (chat.CaseBinding, bool, error) {
	return f.binding, f.bound, f.err
}
func (f *caseStoreFake) BindCase(_ context.Context, tenant, _ string, b chat.CaseBinding) (chat.CaseBinding, error) {
	f.binds++
	b.TenantID = tenant
	f.binding, f.bound = b, true
	return b, nil
}
func (f *caseStoreFake) UnbindCase(context.Context, string, string, string) error {
	f.unbinds++
	f.bound = false
	return nil
}

// caseConversations opens "thread" for the people it lists and nothing else.
type caseConversations struct{ readers map[string]bool }

func (c caseConversations) GetConversation(_ context.Context, r chat.GetConversationRequest) (chat.Conversation, error) {
	if r.ConversationID != "thread" {
		return chat.Conversation{}, chat.ErrNotFound
	}
	if !c.readers[r.Principal.SubjectID] {
		return chat.Conversation{}, chat.ErrPermissionDenied
	}
	return chat.Conversation{ID: "thread", TenantID: r.TenantID}, nil
}

func caseApp(store *caseStoreFake, admin string, readers ...string) chatCaseApplication {
	set := map[string]bool{}
	for _, r := range readers {
		set[r] = true
	}
	return chatCaseApplication{Store: store, Conversations: caseConversations{set}, NewID: func() string { return "b1" },
		Admin: func(_ context.Context, _ string) (string, error) {
			if admin == "" {
				return "", chat.ErrPermissionDenied
			}
			return admin, nil
		}}
}

func TestIssue3812_AdministratorBindsAndLifts(t *testing.T) {
	store := &caseStoreFake{}
	app := caseApp(store, "hr", "hr", "emp")
	hr := chat.Principal{TenantID: "t", SubjectID: "hr"}
	view, err := app.Bind(t.Context(), hr, "thread", " ER-2041 ", "hr", true)
	if err != nil || !view.Bound || view.CaseRef != "ER-2041" || !view.Restricted || !view.OwnerIsMe || !view.CanManage {
		t.Fatalf("bind = %+v err=%v", view, err)
	}
	if store.binding.BoundBy != "hr" || store.binding.OwnerID != "hr" {
		t.Fatalf("stored = %+v; the binder is the verified caller", store.binding)
	}
	// A participant who is not an administrator is told it is bound, and nothing more.
	seen, err := app.Case(t.Context(), chat.Principal{TenantID: "t", SubjectID: "emp"}, "thread")
	if err != nil || !seen.Bound || !seen.Restricted || seen.CanManage || seen.OwnerIsMe {
		t.Fatalf("participant view = %+v err=%v", seen, err)
	}
	lifted, err := app.Unbind(t.Context(), hr, "thread")
	if err != nil || lifted.Bound || store.unbinds != 1 {
		t.Fatalf("unbind = %+v err=%v", lifted, err)
	}
}

func TestIssue3812_BindingRefusals_Security(t *testing.T) {
	store := &caseStoreFake{}
	emp := chat.Principal{TenantID: "t", SubjectID: "emp"}
	hr := chat.Principal{TenantID: "t", SubjectID: "hr"}

	// Not an administrator: refused, nothing written.
	app := caseApp(store, "hr", "hr", "emp")
	if _, err := app.Bind(t.Context(), emp, "thread", "ER-1", "emp", true); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("participant bind: %v", err)
	}
	if _, err := app.Unbind(t.Context(), emp, "thread"); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("participant unbind: %v", err)
	}
	// An administrator who cannot open the thread is not told it exists.
	app = caseApp(store, "hr", "emp")
	if _, err := app.Bind(t.Context(), hr, "thread", "ER-1", "emp", true); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("administrator outside the thread: %v", err)
	}
	if _, err := app.Case(t.Context(), hr, "thread"); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("a non-participant read: %v", err)
	}
	if _, err := app.Case(t.Context(), hr, "nowhere"); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("a missing conversation: %v", err)
	}
	// Bad input.
	app = caseApp(store, "hr", "hr")
	for name, args := range map[string][3]string{"no case": {" ", "hr", ""}, "no owner": {"ER-1", " ", ""}, "long case": {strings.Repeat("x", chatstore.MaxCaseRefRunes+1), "hr", ""}} {
		if _, err := app.Bind(t.Context(), hr, "thread", args[0], args[1], true); !errors.Is(err, chat.ErrInvalidArgument) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if store.binds != 0 || store.unbinds != 0 {
		t.Fatalf("a refused caller changed a binding: binds=%d unbinds=%d", store.binds, store.unbinds)
	}
	// A store that cannot answer is unavailable, never "not bound".
	store.err = errors.New("db down")
	if _, err := app.Case(t.Context(), hr, "thread"); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("store failure: %v", err)
	}
}

type caseServiceFake struct{ calls []string }

func (f *caseServiceFake) Case(_ context.Context, _ chat.Principal, c string) (ChatCaseView, error) {
	f.calls = append(f.calls, "get:"+c)
	if c == "hidden" {
		return ChatCaseView{}, chat.ErrNotFound
	}
	return ChatCaseView{Bound: true, CaseRef: "ER-1", Restricted: true}, nil
}
func (f *caseServiceFake) Bind(_ context.Context, _ chat.Principal, c, ref, owner string, restricted bool) (ChatCaseView, error) {
	f.calls = append(f.calls, "bind:"+c+":"+ref+":"+owner)
	if c == "denied" {
		return ChatCaseView{}, chat.ErrPermissionDenied
	}
	return ChatCaseView{Bound: true, CaseRef: ref, OwnerID: owner, Restricted: restricted}, nil
}
func (f *caseServiceFake) Unbind(_ context.Context, _ chat.Principal, c string) (ChatCaseView, error) {
	f.calls = append(f.calls, "unbind:"+c)
	return ChatCaseView{}, nil
}

func caseRequest(h http.Handler, method, path, body string, signedIn bool, kind trust.SubjectKind) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	ctx := req.Context()
	if signedIn {
		at := time.Now().UTC()
		principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "hr", SubjectKind: kind, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "s", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "d"})
		if err != nil {
			panic(err)
		}
		ctx = transport.WithInvocation(trust.WithPrincipal(ctx, principal), &transport.Invocation{})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func TestIssue3812_CaseHTTPShapesAndRefusals(t *testing.T) {
	svc := &caseServiceFake{}
	h := ChatCaseHTTP{Service: svc}
	for _, tc := range []struct {
		name, method, path, body string
		signedIn                 bool
		kind                     trust.SubjectKind
		status                   int
		contains                 string
	}{
		{"get", "POST", ChatCasePath + "get", `{"conversation_id":"thread"}`, true, trust.SubjectKindHuman, 200, `"restricted":true`},
		{"get hidden", "POST", ChatCasePath + "get", `{"conversation_id":"hidden"}`, true, trust.SubjectKindHuman, 404, `not_found`},
		{"bind", "POST", ChatCasePath + "bind", `{"conversation_id":"thread","case_ref":"ER-9","owner_id":"hr","restricted":true}`, true, trust.SubjectKindHuman, 200, `"case_ref":"ER-9"`},
		{"bind refused", "POST", ChatCasePath + "bind", `{"conversation_id":"denied","case_ref":"x","owner_id":"y"}`, true, trust.SubjectKindHuman, 403, `permission_denied`},
		{"unbind", "POST", ChatCasePath + "unbind", `{"conversation_id":"thread"}`, true, trust.SubjectKindHuman, 200, `"bound":false`},
		{"unknown field", "POST", ChatCasePath + "bind", `{"conversation_id":"thread","tenant":"other"}`, true, trust.SubjectKindHuman, 400, `invalid_request`},
		{"wrong method", "GET", ChatCasePath + "get", ``, true, trust.SubjectKindHuman, 405, `method_not_allowed`},
		{"unknown action", "POST", ChatCasePath + "purge", `{}`, true, trust.SubjectKindHuman, 404, `not_found`},
		{"signed out", "POST", ChatCasePath + "get", `{"conversation_id":"thread"}`, false, trust.SubjectKindHuman, 403, `permission_denied`},
	} {
		rec := caseRequest(h, tc.method, tc.path, tc.body, tc.signedIn, tc.kind)
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.contains) {
			t.Errorf("%s: status=%d body=%s; want %d containing %q", tc.name, rec.Code, rec.Body.String(), tc.status, tc.contains)
		}
	}
}

func TestIssue3812_CaseHTTPRefusesAgents_Security(t *testing.T) {
	svc := &caseServiceFake{}
	rec := caseRequest(ChatCaseHTTP{Service: svc}, "POST", ChatCasePath+"bind", `{"conversation_id":"thread","case_ref":"ER-9","owner_id":"agent"}`, true, trust.SubjectKindAgent)
	if rec.Code != http.StatusForbidden || len(svc.calls) != 0 {
		t.Fatalf("agent bind: status=%d calls=%v; want a refusal before the service", rec.Code, svc.calls)
	}
}

type workRoom struct{ err error }

func (w workRoom) ReadAuthorizedReference(context.Context, chat.Principal, string, string, string) (chat.Conversation, *chat.Post, error) {
	return chat.Conversation{}, nil, chat.ErrNotFound
}
func (w workRoom) CaseRestriction(context.Context, string, string, chat.CaseAction) error {
	return w.err
}

func TestIssue3812_RestrictedCaseMessageIsNotTurnedIntoWork_Security(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want error
	}{
		"restricted":  {&chat.CaseRestrictionError{Action: chat.CaseActionSave}, ErrChatWorkCaseRestricted},
		"unavailable": {chat.ErrUnavailable, ErrChatWorkUnavailable},
		"open":        {nil, nil},
	} {
		w := &ChatMessageWork{chat: workRoom{err: tc.err}}
		err := w.caseRestriction(t.Context(), "t", "thread")
		if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v; want %v", name, err, tc.want)
		}
		if tc.want != nil && !errors.Is(err, ErrChatWorkDenied) && name == "restricted" {
			t.Errorf("%s: a case refusal must be a kind of denial", name)
		}
	}
	rec := httptest.NewRecorder()
	chatMessageWorkHTTPError(rec, ErrChatWorkCaseRestricted)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "case_restricted") {
		t.Fatalf("http = %d %s", rec.Code, rec.Body.String())
	}
}

func TestIssue3812_GateApprovalRefusedInBoundConversation_Security(t *testing.T) {
	wrote := 0
	inner := chatstore.GateMembershipPort(func(context.Context, dbport.Tx, chatgate.Scope, chatgate.Actor, string, bool) error {
		wrote++
		return nil
	})
	scope, eve := chatgate.Scope{Tenant: "t", Conversation: "thread"}, chatgate.Actor{Person: "eve"}
	bound := func(context.Context, dbport.Tx, string, string) (bool, error) { return true, nil }
	err := chatCaseGateMembership(inner, bound)(t.Context(), nil, scope, eve, "k", true)
	if !errors.Is(err, chatgate.ErrDenied) || !errors.Is(err, chat.ErrPermissionDenied) || wrote != 0 {
		t.Fatalf("approval into a bound conversation: err=%v wrote=%d; want a denial before the membership is written", err, wrote)
	}
	if reason, ok := chat.CaseReason(err); !ok || reason != chat.ReasonCaseMembership {
		t.Fatalf("reason = %q, %v", reason, ok)
	}
	// A refusal to admit is not a way in, so it is left to the inner port.
	if err := chatCaseGateMembership(inner, bound)(t.Context(), nil, scope, eve, "k", false); err != nil || wrote != 1 {
		t.Fatalf("decline: err=%v wrote=%d", err, wrote)
	}
	// A conversation with no binding is admitted as before.
	open := func(context.Context, dbport.Tx, string, string) (bool, error) { return false, nil }
	if err := chatCaseGateMembership(inner, open)(t.Context(), nil, scope, eve, "k", true); err != nil || wrote != 2 {
		t.Fatalf("unbound approval: err=%v wrote=%d", err, wrote)
	}
	// A binding that cannot be read does not admit.
	broken := func(context.Context, dbport.Tx, string, string) (bool, error) { return false, errors.New("down") }
	if err := chatCaseGateMembership(inner, broken)(t.Context(), nil, scope, eve, "k", true); !errors.Is(err, chat.ErrUnavailable) || wrote != 2 {
		t.Fatalf("undecided approval: err=%v wrote=%d", err, wrote)
	}
}
