package application

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// issue3929HTTP serves the saved-search routes for two people (tokens alice and
// bob) of one workspace and one person (carol) of another.
func issue3929HTTP(t *testing.T, port ChatSearchPort, saved *chatsearch.SavedService) http.Handler {
	t.Helper()
	now := time.Now().UTC()
	people := map[string][2]string{"alice": {"tenant", "alice"}, "bob": {"tenant", "bob"}, "carol": {"other", "carol"}}
	return OverlayChatSearch(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }),
		ChatSearchHTTP{Port: port, History: chatsearch.NewRecent(), Saved: saved},
		transport.Config{Verifier: trust.VerifierFunc(func(_ context.Context, c trust.Credential) (*trust.Principal, error) {
			who, ok := people[c.Token]
			if !ok {
				return nil, trust.ErrInvalidCredential
			}
			return trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(who[0]), Subject: who[1], SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "s-" + who[1], IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "sha256:" + who[1]})
		})})
}

type issue3929Row struct{ ID, Name, Query string }

func issue3929List(t *testing.T, h http.Handler, token string) []issue3929Row {
	t.Helper()
	w := chatsearchHTTPCall(h, "GET", ChatSearchPath+"/saved", "", token)
	var rows []issue3929Row
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &rows) != nil {
		t.Fatalf("list %d %s", w.Code, w.Body)
	}
	return rows
}

func TestIssue3929_SavedSearchRoutesCreateListEditDelete(t *testing.T) {
	f := &chatsearchHTTPFixture{}
	h := issue3929HTTP(t, f, chatsearch.NewSavedService(chatsearch.NewMemorySaved()))

	w := chatsearchHTTPCall(h, "POST", ChatSearchPath+"/saved", `{"name":"Onboarding","query":"visa in:general has:file"}`, "alice")
	var made issue3929Row
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &made) != nil || made.ID == "" || made.Query != "visa in:general has:file" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("create %d %s", w.Code, w.Body)
	}
	if rows := issue3929List(t, h, "alice"); len(rows) != 1 || rows[0].Name != "Onboarding" {
		t.Fatalf("list = %+v", rows)
	}
	w = chatsearchHTTPCall(h, "PUT", ChatSearchPath+"/saved?id="+made.ID, `{"name":"Visa","query":"visa"}`, "alice")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"Visa"`) {
		t.Fatalf("edit %d %s", w.Code, w.Body)
	}
	w = chatsearchHTTPCall(h, "DELETE", ChatSearchPath+"/saved?id="+made.ID, "", "alice")
	if w.Code != 204 || len(issue3929List(t, h, "alice")) != 0 {
		t.Fatalf("delete %d %s", w.Code, w.Body)
	}
	if f.calls != 0 {
		t.Fatal("managing a shortcut ran a search")
	}
}

func TestIssue3929_Security_SavedSearchRoutesStayWithTheirOwner(t *testing.T) {
	h := issue3929HTTP(t, &chatsearchHTTPFixture{}, chatsearch.NewSavedService(chatsearch.NewMemorySaved()))
	var made issue3929Row
	w := chatsearchHTTPCall(h, "POST", ChatSearchPath+"/saved", `{"name":"Leave","query":"parental leave"}`, "alice")
	_ = json.Unmarshal(w.Body.Bytes(), &made)
	for _, other := range []string{"bob", "carol"} {
		if rows := issue3929List(t, h, other); len(rows) != 0 {
			t.Fatalf("%s lists %+v", other, rows)
		}
		if w := chatsearchHTTPCall(h, "PUT", ChatSearchPath+"/saved?id="+made.ID, `{"name":"x","query":"y"}`, other); w.Code != 404 || strings.Contains(w.Body.String(), "parental") {
			t.Fatalf("%s edit %d %s", other, w.Code, w.Body)
		}
		if w := chatsearchHTTPCall(h, "DELETE", ChatSearchPath+"/saved?id="+made.ID, "", other); w.Code != 404 {
			t.Fatalf("%s delete %d", other, w.Code)
		}
	}
	if rows := issue3929List(t, h, "alice"); len(rows) != 1 {
		t.Fatalf("owner lost it: %+v", rows)
	}
	// No credential, no list. A body cannot name another owner.
	if w := chatsearchHTTPCall(h, "GET", ChatSearchPath+"/saved", "", ""); w.Code < 400 {
		t.Fatalf("anonymous %d", w.Code)
	}
	if w := chatsearchHTTPCall(h, "POST", ChatSearchPath+"/saved", `{"name":"x","query":"y","PersonID":"alice"}`, "bob"); w.Code != 400 {
		t.Fatalf("forged owner %d", w.Code)
	}
}

func TestIssue3929_SavedSearchRoutesSayWhatWentWrong(t *testing.T) {
	h := issue3929HTTP(t, &chatsearchHTTPFixture{}, chatsearch.NewSavedService(chatsearch.NewMemorySaved()))
	chatsearchHTTPCall(h, "POST", ChatSearchPath+"/saved", `{"name":"Budget","query":"budget"}`, "alice")
	for _, c := range []struct {
		method, path, body string
		status             int
		code               string
	}{
		{"POST", "/saved", `{"name":"budget","query":"other"}`, 409, "name_taken"},
		{"POST", "/saved", `{"name":"x","query":"a before:nonsense"}`, 400, "invalid"},
		{"POST", "/saved", `{"name":"","query":"a"}`, 400, "invalid"},
		{"PUT", "/saved", `{"name":"x","query":"a"}`, 400, "invalid"},
		{"PUT", "/saved?id=missing", `{"name":"x","query":"a"}`, 404, "not_found"},
		{"PATCH", "/saved", ``, 405, "method"},
	} {
		w := chatsearchHTTPCall(h, c.method, ChatSearchPath+c.path, c.body, "alice")
		if w.Code != c.status || !strings.Contains(w.Body.String(), c.code) {
			t.Errorf("%s %s: %d %s, want %d %s", c.method, c.path, w.Code, w.Body, c.status, c.code)
		}
	}
	none := issue3929HTTP(t, &chatsearchHTTPFixture{}, nil)
	if w := chatsearchHTTPCall(none, "GET", ChatSearchPath+"/saved", "", "alice"); w.Code != 503 {
		t.Errorf("no store: %d", w.Code)
	}
}

// A saved query is text, not a grant: running one is the ordinary search, which
// is asked as the caller and so decides access again every time.
func TestIssue3929_Security_RunningASavedSearchIsAnOrdinarySearchAsTheCaller(t *testing.T) {
	f := &chatsearchHTTPFixture{}
	h := issue3929HTTP(t, f, chatsearch.NewSavedService(chatsearch.NewMemorySaved()))
	chatsearchHTTPCall(h, "POST", ChatSearchPath+"/saved", `{"name":"Leave","query":"leave in:hr"}`, "alice")
	rows := issue3929List(t, h, "alice")
	body, _ := json.Marshal(map[string]string{"Query": rows[0].Query})
	if w := chatsearchHTTPCall(h, "POST", ChatSearchPath, string(body), "alice"); w.Code != 200 {
		t.Fatalf("run %d %s", w.Code, w.Body)
	}
	if f.q.Actor.PersonID != "alice" || f.calls != 1 {
		t.Fatalf("searched as %+v", f.q.Actor)
	}
	// Bob running the same words is asked as Bob; he never sees Alice's shortcut.
	if w := chatsearchHTTPCall(h, "POST", ChatSearchPath, string(body), "bob"); w.Code != 200 || f.q.Actor.PersonID != "bob" {
		t.Fatalf("bob %d %+v", w.Code, f.q.Actor)
	}
}
