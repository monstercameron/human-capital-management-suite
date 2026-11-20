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

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatbarrier"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatauthority"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type barrierRulesFake struct {
	mu    sync.Mutex
	rules chatbarrier.Rules
	fail  error
	reads int
}

func (f *barrierRulesFake) ListBarrierRules(_ context.Context, tenant string) (chatbarrier.Rules, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	if f.fail != nil {
		return nil, f.fail
	}
	return append(chatbarrier.Rules(nil), f.rules...), nil
}
func (f *barrierRulesFake) AddBarrierRule(_ context.Context, _ string, r chatbarrier.Rule) (chatbarrier.Rule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, have := range f.rules {
		if have.GroupA == r.GroupA && have.GroupB == r.GroupB {
			return chatbarrier.Rule{}, chat.ErrAlreadyExists
		}
	}
	f.rules = append(f.rules, r)
	return r, nil
}
func (f *barrierRulesFake) RemoveBarrierRule(_ context.Context, _, id, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, have := range f.rules {
		if have.ID == id {
			f.rules = append(f.rules[:i], f.rules[i+1:]...)
			return nil
		}
	}
	return chat.ErrNotFound
}

type barrierPeopleFake struct {
	teams map[string]string
	fail  error
	names []string
}

func (f barrierPeopleFake) BarrierTeams(_ context.Context, _ string, subjects []string) (map[string]string, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	out := map[string]string{}
	for _, s := range subjects {
		if t, ok := f.teams[s]; ok {
			out[s] = t
		}
	}
	return out, nil
}
func (f barrierPeopleFake) BarrierTeamNames(context.Context, string) ([]string, error) {
	return f.names, f.fail
}

func salesAdvisory(t *testing.T) *barrierRulesFake {
	t.Helper()
	r, err := chatbarrier.NewRule("r1", "Sales", "Advisory", "admin", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return &barrierRulesFake{rules: chatbarrier.Rules{r}}
}

func roster(subjects ...string) func(context.Context) ([]string, error) {
	return func(context.Context) ([]string, error) { return subjects, nil }
}

func TestIssue3783_GuardResolvesTeamsAndRefusesPair_Security(t *testing.T) {
	people := barrierPeopleFake{teams: map[string]string{"s1": "Sales", "a1": "advisory", "h1": "People"}}
	guard := chatBarrierGuard{Rules: salesAdvisory(t), People: people}
	if err := guard.CheckJoin(t.Context(), "t", []string{"a1"}, roster("s1", "h1")); !errors.Is(err, chat.ErrInformationBarrier) {
		t.Fatalf("advisory into a room with sales: %v", err)
	}
	if err := guard.CheckJoin(t.Context(), "t", []string{"s1"}, roster("a1")); !errors.Is(err, chat.ErrInformationBarrier) {
		t.Fatalf("sales into a room with advisory: %v", err)
	}
	if err := guard.CheckJoin(t.Context(), "t", []string{"h1"}, roster("s1", "a1")); err != nil {
		t.Fatalf("an unrelated person joins a room that already breaks the rule: %v", err)
	}
	if err := guard.CheckJoin(t.Context(), "t", []string{"s1", "a1"}, roster()); !errors.Is(err, chat.ErrInformationBarrier) {
		t.Fatalf("a new room with both sides: %v", err)
	}
	if err := guard.CheckJoin(t.Context(), "t", []string{"ghost"}, roster("s1")); err != nil {
		t.Fatalf("a person with no directory record is on no side: %v", err)
	}
}

func TestIssue3783_GuardNeverReadsTheRosterForPeopleOnNoSide(t *testing.T) {
	people := barrierPeopleFake{teams: map[string]string{"h1": "People"}}
	guard := chatBarrierGuard{Rules: salesAdvisory(t), People: people}
	read := false
	err := guard.CheckJoin(t.Context(), "t", []string{"h1"}, func(context.Context) ([]string, error) { read = true; return nil, nil })
	if err != nil || read {
		t.Fatalf("err=%v rosterRead=%v", err, read)
	}
	empty := chatBarrierGuard{Rules: &barrierRulesFake{}, People: people}
	if err := empty.CheckJoin(t.Context(), "t", []string{"s1"}, func(context.Context) ([]string, error) { read = true; return nil, nil }); err != nil || read {
		t.Fatalf("a workspace with no rules must admit without reading: %v %v", err, read)
	}
}

func TestIssue3783_GuardAnswersAnErrorWhenItCannotDecide_Security(t *testing.T) {
	rules := salesAdvisory(t)
	people := barrierPeopleFake{teams: map[string]string{"s1": "Sales"}}
	boom := errors.New("db down")
	for name, guard := range map[string]chatBarrierGuard{
		"rules unreadable":     {Rules: &barrierRulesFake{fail: boom}, People: people},
		"directory unreadable": {Rules: rules, People: barrierPeopleFake{fail: boom}},
		"no directory":         {Rules: rules},
	} {
		err := guard.CheckJoin(t.Context(), "t", []string{"s1"}, roster("x"))
		if err == nil || errors.Is(err, chat.ErrInformationBarrier) {
			t.Errorf("%s: an undecided barrier must be an error that is not a verdict, got %v", name, err)
		}
	}
	err := chatBarrierGuard{Rules: rules, People: people}.CheckJoin(t.Context(), "t", []string{"s1"}, func(context.Context) ([]string, error) { return nil, boom })
	if err == nil || errors.Is(err, chat.ErrInformationBarrier) {
		t.Errorf("an unreadable roster must be an error: %v", err)
	}
}

func TestIssue3783_GateApprovalIsGuarded_Security(t *testing.T) {
	people := barrierPeopleFake{teams: map[string]string{"s1": "Sales", "a1": "Advisory"}}
	guard := chatBarrierGuard{Rules: salesAdvisory(t), People: people}
	var wrote []string
	inner := func(_ context.Context, _ dbport.Tx, _ chatgate.Scope, actor chatgate.Actor, _ string, admit bool) error {
		wrote = append(wrote, actor.Person)
		return nil
	}
	gated := chatBarrierGateMembership(inner, guard, func(context.Context, dbport.Tx, chatgate.Scope) ([]string, error) { return []string{"s1"}, nil })
	scope := chatgate.Scope{Tenant: "t", Conversation: "c"}
	if err := gated(t.Context(), nil, scope, chatgate.Actor{Tenant: "t", Person: "a1"}, "", true); !errors.Is(err, chat.ErrInformationBarrier) || !errors.Is(err, chatgate.ErrDenied) || len(wrote) != 0 {
		t.Fatalf("approval across a barrier must read as a denial at the gate edge: err=%v wrote=%v", err, wrote)
	}
	if err := gated(t.Context(), nil, scope, chatgate.Actor{Tenant: "t", Person: "s1"}, "", true); err != nil || len(wrote) != 1 {
		t.Fatalf("approval within a side: err=%v wrote=%v", err, wrote)
	}
	// Leaving is never blocked by a barrier.
	if err := gated(t.Context(), nil, scope, chatgate.Actor{Tenant: "t", Person: "a1"}, "", false); err != nil || len(wrote) != 2 {
		t.Fatalf("leave: err=%v wrote=%v", err, wrote)
	}
	broken := chatBarrierGateMembership(inner, chatBarrierGuard{Rules: &barrierRulesFake{fail: errors.New("x")}, People: people}, nil)
	if err := broken(t.Context(), nil, scope, chatgate.Actor{Tenant: "t", Person: "s1"}, "", true); !errors.Is(err, chat.ErrUnavailable) || len(wrote) != 2 {
		t.Fatalf("an undecided approval must not admit: err=%v wrote=%v", err, wrote)
	}
}

func barrierApp(t *testing.T, rules *barrierRulesFake, adminSubject string) chatBarrierApplication {
	t.Helper()
	return chatBarrierApplication{
		Rules: rules,
		Teams: barrierPeopleFake{names: []string{"Advisory", "People", "Sales"}},
		Admin: func(context.Context, string) (string, error) {
			if adminSubject == "" {
				return "", chat.ErrPermissionDenied
			}
			return adminSubject, nil
		},
		NewID: func() string { return "new-rule" },
	}
}

func TestIssue3783_AdminSurfaceManagesRules(t *testing.T) {
	rules := &barrierRulesFake{}
	app := barrierApp(t, rules, "admin")
	p := chat.Principal{TenantID: "t", SubjectID: "admin"}
	added, err := app.AddBarrier(t.Context(), p, "Sales", " advisory ")
	if err != nil || added.ID != "new-rule" || added.GroupA != "advisory" || added.GroupB != "Sales" {
		t.Fatalf("add = %+v err=%v", added, err)
	}
	if _, err := app.AddBarrier(t.Context(), p, "ADVISORY", "sales"); !errors.Is(err, chat.ErrAlreadyExists) {
		t.Fatalf("the same pair twice: %v", err)
	}
	list, err := app.ListBarriers(t.Context(), p)
	if err != nil || len(list.Rules) != 1 || len(list.Teams) != 3 {
		t.Fatalf("list = %+v err=%v", list, err)
	}
	if _, err := app.AddBarrier(t.Context(), p, "Sales", "Narnia"); !errors.Is(err, errChatBarrierUnknownGroup) {
		t.Fatalf("a team the directory lacks: %v", err)
	}
	if _, err := app.AddBarrier(t.Context(), p, "Sales", "sales"); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("a group against itself: %v", err)
	}
	if err := app.RemoveBarrier(t.Context(), p, "new-rule"); err != nil {
		t.Fatal(err)
	}
	if err := app.RemoveBarrier(t.Context(), p, "new-rule"); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("second remove: %v", err)
	}
}

func TestIssue3783_AdminSurfaceRefusesNonAdministrators_Security(t *testing.T) {
	rules := salesAdvisory(t)
	for name, app := range map[string]chatBarrierApplication{
		"not an admin":   barrierApp(t, rules, ""),
		"another person": barrierApp(t, rules, "someone-else"),
	} {
		p := chat.Principal{TenantID: "t", SubjectID: "worker"}
		if _, err := app.ListBarriers(t.Context(), p); !errors.Is(err, chat.ErrPermissionDenied) {
			t.Errorf("%s list: %v", name, err)
		}
		if _, err := app.AddBarrier(t.Context(), p, "Sales", "People"); !errors.Is(err, chat.ErrPermissionDenied) {
			t.Errorf("%s add: %v", name, err)
		}
		if err := app.RemoveBarrier(t.Context(), p, "r1"); !errors.Is(err, chat.ErrPermissionDenied) {
			t.Errorf("%s remove: %v", name, err)
		}
	}
	if len(rules.rules) != 1 {
		t.Fatalf("a refused caller changed the rules: %+v", rules.rules)
	}
}

type barrierServiceFake struct{ calls []string }

func (f *barrierServiceFake) ListBarriers(context.Context, chat.Principal) (ChatBarrierList, error) {
	f.calls = append(f.calls, "list")
	return ChatBarrierList{Rules: []ChatBarrierRule{{ID: "r1", GroupA: "Advisory", GroupB: "Sales"}}, Teams: []string{"Sales"}}, nil
}
func (f *barrierServiceFake) AddBarrier(_ context.Context, _ chat.Principal, a, b string) (ChatBarrierRule, error) {
	f.calls = append(f.calls, "add:"+a+"+"+b)
	if a == "dup" {
		return ChatBarrierRule{}, chat.ErrAlreadyExists
	}
	if a == "ghost" {
		return ChatBarrierRule{}, errChatBarrierUnknownGroup
	}
	return ChatBarrierRule{ID: "r2", GroupA: a, GroupB: b}, nil
}
func (f *barrierServiceFake) RemoveBarrier(_ context.Context, _ chat.Principal, id string) error {
	f.calls = append(f.calls, "remove:"+id)
	if id == "gone" {
		return chat.ErrNotFound
	}
	return nil
}

func barrierRequest(t *testing.T, h http.Handler, method, path, body string, signedIn bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	ctx := req.Context()
	if signedIn {
		at := time.Now().UTC()
		principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "admin-1", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "s", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "d"})
		if err != nil {
			t.Fatal(err)
		}
		ctx = transport.WithInvocation(trust.WithPrincipal(ctx, principal), &transport.Invocation{})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func TestIssue3783_BarrierHTTPShapesAndRefusals(t *testing.T) {
	svc := &barrierServiceFake{}
	h := ChatBarrierHTTP{Service: svc}
	cases := []struct {
		name, method, path, body string
		signedIn                 bool
		status                   int
		contains                 string
	}{
		{"list", "POST", ChatBarrierPath + "list", `{}`, true, 200, `"Teams":["Sales"]`},
		{"add", "POST", ChatBarrierPath + "add", `{"GroupA":"Sales","GroupB":"Advisory"}`, true, 200, `"ID":"r2"`},
		{"duplicate", "POST", ChatBarrierPath + "add", `{"GroupA":"dup","GroupB":"x"}`, true, 409, `already_exists`},
		{"unknown group", "POST", ChatBarrierPath + "add", `{"GroupA":"ghost","GroupB":"x"}`, true, 400, `unknown_group`},
		{"remove", "POST", ChatBarrierPath + "remove", `{"ID":"r1"}`, true, 200, `"Removed":true`},
		{"remove missing", "POST", ChatBarrierPath + "remove", `{"ID":"gone"}`, true, 404, `not_found`},
		{"unknown field", "POST", ChatBarrierPath + "add", `{"GroupA":"a","Tenant":"other"}`, true, 400, `invalid_request`},
		{"wrong method", "GET", ChatBarrierPath + "list", ``, true, 405, `method_not_allowed`},
		{"unknown action", "POST", ChatBarrierPath + "purge", `{}`, true, 404, `not_found`},
		{"signed out", "POST", ChatBarrierPath + "list", `{}`, false, 403, `permission_denied`},
	}
	for _, c := range cases {
		rec := barrierRequest(t, h, c.method, c.path, c.body, c.signedIn)
		if rec.Code != c.status || !strings.Contains(rec.Body.String(), c.contains) {
			t.Errorf("%s: status %d body %q", c.name, rec.Code, rec.Body.String())
		}
		if c.status == 200 && rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: answers must not be cached", c.name)
		}
	}
	rec := barrierRequest(t, ChatBarrierHTTP{}, "POST", ChatBarrierPath+"list", `{}`, true)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no service must be 503, got %d", rec.Code)
	}
}

// The real administrator check: only a workspace administrator's own verified
// session may change a barrier, whatever principal the request names.
func TestIssue3783_AdminSurfaceUsesTheRealAdministratorCheck_Security(t *testing.T) {
	at := time.Now().UTC()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "host", Subject: "person", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "s", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "d"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), principal)
	for role, wantAllowed := range map[string]bool{"hcm_admin": true, "employee": false} {
		grants := NewChatCompanyGrants(chatAdminFacts{role: role}, chatauthority.New(nil))
		rules := &barrierRulesFake{}
		app := chatBarrierApplication{Rules: rules, Admin: func(ctx context.Context, tenant string) (string, error) { return grants.admin(ctx, tenant, at) }}
		_, err := app.AddBarrier(ctx, chat.Principal{TenantID: "host", SubjectID: "person"}, "Sales", "Advisory")
		if wantAllowed != (err == nil) || wantAllowed != (len(rules.rules) == 1) {
			t.Fatalf("role %s: err=%v rules=%d", role, err, len(rules.rules))
		}
		if _, err := app.AddBarrier(ctx, chat.Principal{TenantID: "elsewhere", SubjectID: "person"}, "A", "B"); !errors.Is(err, chat.ErrPermissionDenied) {
			t.Fatalf("role %s: a principal naming another workspace: %v", role, err)
		}
	}
	if newChatBarrierApplication(nil, nil, nil) != nil {
		t.Fatal("with no store, no administrator check and no directory the surface must not be offered")
	}
}
