package workspace_test

// UXAUDIT-014 INTEGRATION: the unit-level suite in
// internal/humanwork/workspace/todo_uxaudit_014_test.go exercises the dev
// personas against productui.PageVisible directly (roleAccess left nil,
// which is what internal/humanwork/workspace's own fixtures do). The actual
// running server does not take that path: internal/application's serve.go
// always bootstraps a roleaccessstore.Store for a named tenant
// (roleAccess.Bootstrap seeds roleaccess.DefaultRoles and
// roleaccess.DefaultPagePermissions), so serveProduct's admission check
// resolves through roleaccess.CanPageAction against those durable
// permissions, not through PageVisible directly. This test composes the
// same real cell shape cmd/hcmnext serves -- a real Postgres-backed store, a
// real roleaccessstore.Store bootstrapped exactly like production, and the
// real HTTP edge -- so the UXAUDIT-014 fix (payroll-manager's fixture
// carrying "worker_self" instead of "payroll_manager";
// internal/humanwork/workspace/dev_persona_roles.go) is proven against the
// authority that actually decides on the live server, not only against its
// registry-level fallback.

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/roleaccessstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	kernelvalues "github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transportcell "github.com/monstercameron/human-capital-management-suite/internal/transport/cell"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// uxaudit014Cell composes a real, Postgres-backed cell carrying the four
// canonical dev personas (workspace.DevPersonaRoleSets -- the same fixture
// composeDevPersonas issues real credentials from) with a real, bootstrapped
// roleaccessstore.Store, exactly as internal/application's ServeSpec does
// for any named tenant.
func uxaudit014Cell(t *testing.T) (serverURL string) {
	t.Helper()
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	store, err := pgstore.New(pool, pgstore.WithCellID("cell-uxaudit-014"),
		pgstore.WithClock(func() time.Time { return baseTime }))
	if err != nil {
		t.Fatalf("pgstore.New: %v", err)
	}
	if err := store.Bootstrap(context.Background(), testTenant); err != nil {
		t.Fatalf("bootstrap tenant: %v", err)
	}

	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key: testSigningKey, Issuer: testIssuer, Audience: testAudience,
		Now: func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		t.Fatalf("NewHMACVerifier: %v", err)
	}

	roleAccess := roleaccessstore.New(pool, func(id kernelvalues.TenantId) uuid.UUID { return pgstore.TenantID(string(id)) })
	if err := roleAccess.Bootstrap(context.Background(), kernelvalues.TenantId(testTenant), "system:uxaudit-014-test"); err != nil {
		t.Fatalf("bootstrap role access: %v", err)
	}

	devPersonas := make([]workspace.DevPersona, 0, 4)
	for _, set := range workspace.DevPersonaRoleSets() {
		token, issueErr := verifier.Issue(trust.Claims{
			Issuer: testIssuer, Audience: testAudience, Subject: "user-uxaudit014-" + set.ID, SubjectKind: "human",
			Tenant: testTenant, OrganizationScopeID: testOrgScope, Roles: set.Roles,
			Purposes: []string{authorizedPurpose}, AuthenticationMethod: "bearer_token", Assurance: "substantial",
			SessionRef:   "session-uxaudit014-" + set.ID,
			IssuedAtUnix: time.Now().Add(-time.Minute).Unix(), ExpiresAtUnix: time.Now().Add(time.Hour).Unix(),
		})
		if issueErr != nil {
			t.Fatalf("issue %s: %v", set.ID, issueErr)
		}
		devPersonas = append(devPersonas, workspace.DevPersona{
			ID: set.ID, Name: "Worker " + set.ID, Access: set.ID, Roles: set.Roles, Token: token,
		})
	}

	workspaceEnabled := true
	composed, err := app.NewCell(app.CellConfig{
		Store: store, Verifier: verifier, Audience: testAudience, MaxDeadline: 30 * time.Second,
		Now:             func() time.Time { return time.Now().UTC() },
		Workspace:       &workspaceEnabled,
		DevBrowserLogin: true,
		DevPersonas:     devPersonas,
		RoleAccess:      roleAccess,
	})
	if err != nil {
		t.Fatalf("app.NewCell: %v", err)
	}
	handler, err := transportcell.NewEdgeHandler(composed)
	if err != nil {
		t.Fatalf("EdgeHandler: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server.URL
}

// uxaudit014Login signs in as personaID the way a real browser does: a GET
// to prime the edge's SameSite CSRF cookie (internal/transport/edge's
// browser policy treats any workspace POST as browser traffic and requires
// it), then a POST carrying that cookie and a matching Origin header.
func uxaudit014Login(t *testing.T, serverURL, personaID string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	primeGet, err := client.Get(serverURL + workspace.PathLogin)
	if err != nil {
		t.Fatal(err)
	}
	primeGet.Body.Close()
	if primeGet.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200 (priming the browser CSRF cookie)", workspace.PathLogin, primeGet.StatusCode)
	}

	form := url.Values{"persona": {personaID}}
	req, err := http.NewRequest(http.MethodPost, serverURL+workspace.PathLogin, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", serverURL)
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	wantLanding := map[string]string{
		"admin":                  workspace.PathProductHome,
		"hiring-manager":         workspace.PathProductPrefix + "people",
		"payroll-manager":        workspace.PathProductPrefix + "myself",
		"individual-contributor": workspace.PathProductPrefix + "myself",
	}[personaID]
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != wantLanding {
		t.Fatalf("login as %s = %d location %q, want %q", personaID, res.StatusCode, res.Header.Get("Location"), wantLanding)
	}
	return client
}

var uxaudit014DestinationRE = regexp.MustCompile(`href="` + regexp.QuoteMeta(workspace.PathProductPrefix) + `([a-z0-9-]+)`)

func uxaudit014Get(t *testing.T, client *http.Client, serverURL, path string) (status int, destinations map[string]bool) {
	t.Helper()
	res, err := client.Get(serverURL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	set := map[string]bool{}
	for _, m := range uxaudit014DestinationRE.FindAllStringSubmatch(string(body), -1) {
		set[m[1]] = true
	}
	return res.StatusCode, set
}

// TestTodo_UXAUDIT_014_Integration proves the UXAUDIT-014 fix holds against
// the real store-backed authority the running server actually uses
// (roleaccess.Store bootstrapped with roleaccess.DefaultPagePermissions),
// not only against productui.PageVisible's registry-level fallback that the
// faster in-package suite exercises.
func TestTodo_UXAUDIT_014_Integration(t *testing.T) {
	serverURL := uxaudit014Cell(t)

	hiringClient := uxaudit014Login(t, serverURL, "hiring-manager")
	payrollClient := uxaudit014Login(t, serverURL, "payroll-manager")

	hiringStatus, hiringDestinations := uxaudit014Get(t, hiringClient, serverURL, workspace.PathProductHome)
	if hiringStatus != http.StatusOK {
		t.Fatalf("GET home as hiring-manager = %d, want 200", hiringStatus)
	}
	payrollStatus, payrollDestinations := uxaudit014Get(t, payrollClient, serverURL, workspace.PathProductHome)
	if payrollStatus != http.StatusOK {
		t.Fatalf("GET home as payroll-manager = %d, want 200", payrollStatus)
	}

	// RED clause 2, under the real roleaccess-store-backed authority: these
	// two personas hold disjoint role sets and must not render identical
	// destinations.
	if len(hiringDestinations) == len(payrollDestinations) {
		identical := true
		for d := range hiringDestinations {
			if !payrollDestinations[d] {
				identical = false
				break
			}
		}
		if identical {
			t.Fatalf("hiring-manager and payroll-manager rendered identical destinations under the real roleaccess-store authority: %v", hiringDestinations)
		}
	}
	for _, want := range []string{"people", "journeys", "work"} {
		if !hiringDestinations[want] {
			t.Errorf("hiring-manager missing %q under real roleaccess.DefaultPagePermissions wiring: %v", want, hiringDestinations)
		}
		if payrollDestinations[want] {
			t.Errorf("payroll-manager unexpectedly reaches %q under real roleaccess.DefaultPagePermissions wiring: %v", want, payrollDestinations)
		}
	}
	if !payrollDestinations["myself"] || !payrollDestinations["organization"] {
		t.Errorf("payroll-manager missing its real self-service destinations: %v", payrollDestinations)
	}

	// Direct route access, not menu omission: payroll-manager's worker_self
	// role is not one roleaccess.DefaultPagePermissions grants "people" to,
	// so a direct GET must refuse it under the real authority too.
	peopleStatus, _ := uxaudit014Get(t, payrollClient, serverURL, workspace.PathProductPrefix+"people")
	if peopleStatus != http.StatusForbidden {
		t.Errorf("GET %s as payroll-manager = %d, want 403 under real roleaccess wiring", workspace.PathProductPrefix+"people", peopleStatus)
	}
	peopleStatus, _ = uxaudit014Get(t, hiringClient, serverURL, workspace.PathProductPrefix+"people")
	if peopleStatus != http.StatusOK {
		t.Errorf("GET %s as hiring-manager = %d, want 200 under real roleaccess wiring", workspace.PathProductPrefix+"people", peopleStatus)
	}
}
