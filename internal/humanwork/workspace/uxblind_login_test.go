package workspace

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestUXBLIND001PersonaLoginUsesServerOwnedCredential(t *testing.T) {
	h, token := newShellHandler(t, true)
	h.devPersonas = map[string]DevPersona{
		"admin": {ID: "admin", Name: "Rafael Torres", Access: "HCM administrator", Token: token},
	}
	form := url.Values{paramLoginPersona: {"admin"}}
	req := httptest.NewRequest(http.MethodPost, "http://cell.test"+PathLogin, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != PathProductHome {
		t.Fatalf("persona login = %d location %q", rec.Code, rec.Header().Get("Location"))
	}
	if strings.Contains(rec.Body.String(), token) {
		t.Fatal("persona credential was disclosed")
	}
}

func TestUXBLIND002LoginFailureOffersSafeRecovery(t *testing.T) {
	h, _ := newShellHandler(t, true)
	h.devPersonas = map[string]DevPersona{"admin": {ID: "admin", Name: "Rafael Torres", Access: "HCM administrator", Token: "wrong"}}
	form := url.Values{paramLoginPersona: {"admin"}}
	req := httptest.NewRequest(http.MethodPost, "http://cell.test"+PathLogin, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{"sign you in right now", "Try again", "workspace administrator", "Use a bearer credential"} {
		if !strings.Contains(body, want) {
			t.Errorf("login recovery missing %q", want)
		}
	}
	if strings.Contains(body, "wrong") || strings.Contains(body, "credential was not accepted") {
		t.Fatal("login failure exposed credential-specific diagnostics")
	}
}

func TestUXBLIND003LoginFailureKeepsPersonaActionsBeforeFeedback(t *testing.T) {
	h, _ := newShellHandler(t, true)
	h.devPersonas = map[string]DevPersona{"admin": {ID: "admin", Name: "Rafael Torres", Access: "HCM administrator", Token: "wrong"}}
	form := url.Values{paramLoginPersona: {"admin"}}
	req := httptest.NewRequest(http.MethodPost, "http://cell.test"+PathLogin, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	if strings.Index(body, `class="persona-grid"`) > strings.Index(body, `class="status-banner"`) {
		t.Fatal("failure feedback was rendered before the stable persona grid")
	}
	if !strings.Contains(body, `role="alert"`) {
		t.Fatal("login failure was not announced")
	}
}

func TestUXBLIND004LoginFailureOpensCredentialRecovery(t *testing.T) {
	h, _ := newShellHandler(t, true)
	h.devPersonas = map[string]DevPersona{"admin": {ID: "admin", Name: "Rafael Torres", Access: "HCM administrator", Token: "wrong"}}
	form := url.Values{paramLoginPersona: {"admin"}}
	req := httptest.NewRequest(http.MethodPost, "http://cell.test"+PathLogin, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, `<details class="advanced" id="credential-sign-in" open>`) || !strings.Contains(body, `href="#credential-sign-in"`) {
		t.Fatal("failed login did not expose a discoverable credential recovery path")
	}
}

// TestUXBLIND005PersonaCopyNamesUsefulTasks is UXAUDIT-014's fix for its own
// former defect: this test used to pin the exact overpromising prose RED
// flags ("Review hiring and organization requests" for a role with no
// hiring surface, "Review payroll information, reports" for a role with no
// payroll surface at all, and identical copy for two personas holding
// disjoint role sets). loginPersonaDescription now derives its copy from
// each persona's Roles against the live productui registry
// (admittedDestinationLabels), so this test drives that through the real
// sign-in page and checks the rendered copy against the registry instead of
// against hand-written strings that could drift back into the same defect.
func TestUXBLIND005PersonaCopyNamesUsefulTasks(t *testing.T) {
	h, _ := newShellHandler(t, true)
	h.devPersonas = map[string]DevPersona{}
	for _, id := range []string{"admin", "hiring-manager", "payroll-manager", "individual-contributor"} {
		roles, ok := DevPersonaRoles(id)
		if !ok {
			t.Fatalf("no canonical role fixture for persona %q", id)
		}
		h.devPersonas[id] = DevPersona{ID: id, Name: "Worker " + id, Access: id, Roles: roles, Token: frontendE2EToken(t, id+"-copy", roles)}
	}
	req := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathLogin, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	cards := personaCards(t, body, "admin", "hiring-manager", "payroll-manager", "individual-contributor")

	// RED clause 1: no persona may promise a page it cannot open. Assert
	// this generically against the live registry rather than against a
	// fixed list of banned phrases, so the check keeps holding after the
	// registry changes.
	//
	// UXAUDIT-014 REFACTOR: "admit" is checked here through
	// roleaccess.CanPageAction over roleaccess.DefaultPagePermissions, the
	// same projection loginPersonaDescription itself derives from (handler.go)
	// and the one serveProduct and the rendered menu actually enforce --
	// productui.PageVisible alone denies worker_self "insights" that
	// roleaccess legitimately grants, which is not a defect this check should
	// flag. See todo_uxaudit_014_test.go's REFACTOR comment for the full
	// account.
	for id, card := range cards {
		roles, _ := DevPersonaRoles(id)
		permissions := roleaccess.EffectivePagePermissions(roleaccess.Snapshot{PagePermissions: roleaccess.DefaultPagePermissions()}, roles)
		for _, definition := range productui.PageDefinitions() {
			if strings.Contains(card, definition.Label) && !(definition.Admitted && definition.PrimaryNav && roleaccess.CanPageAction(permissions, string(definition.ID), roleaccess.ActionView)) {
				t.Errorf("%s persona copy names %q, which its roles do not admit as a top-level destination", id, definition.Label)
			}
		}
	}

	// RED clause 2 at the copy layer: hiring-manager and payroll-manager
	// hold disjoint role sets ({hiring_manager,manager,intent_author} vs
	// {worker_self}) and must not render identical persona copy.
	if cards["hiring-manager"] == cards["payroll-manager"] {
		t.Fatal("hiring-manager and payroll-manager, whose signed roles are disjoint, render identical persona copy")
	}
	if !strings.Contains(cards["hiring-manager"], "People") || !strings.Contains(cards["hiring-manager"], "Journeys") {
		t.Error("hiring-manager copy omits a workforce destination its manager role admits")
	}
	if strings.Contains(cards["payroll-manager"], "People") || strings.Contains(cards["payroll-manager"], "Journeys") {
		t.Error("payroll-manager copy promises a workforce destination its worker_self role does not admit")
	}
	if !strings.Contains(cards["payroll-manager"], "Organization") || !strings.Contains(cards["individual-contributor"], "Organization") {
		t.Error("a self-service persona's copy omits the one real destination its role admits")
	}
	if !strings.Contains(cards["admin"], "Admin") {
		t.Error("admin persona copy does not name its admin-only destination")
	}
	if strings.Contains(cards["hiring-manager"], "Admin") || strings.Contains(cards["payroll-manager"], "Admin") || strings.Contains(cards["individual-contributor"], "Admin") {
		t.Error("a non-admin persona's copy names the admin-only destination")
	}
}

// personaCards splits the rendered sign-in page into one substring per
// persona card, in the fixed order writeLoginPage renders them, keyed by id.
func personaCards(t *testing.T, body string, wantIDs ...string) map[string]string {
	t.Helper()
	order := []string{"admin", "hiring-manager", "payroll-manager", "individual-contributor"}
	parts := strings.Split(body, `<form class="persona"`)
	if len(parts) != len(order)+1 {
		t.Fatalf("rendered %d persona cards, want %d", len(parts)-1, len(order))
	}
	cards := make(map[string]string, len(order))
	for i, id := range order {
		cards[id] = parts[i+1]
	}
	for _, id := range wantIDs {
		if _, ok := cards[id]; !ok {
			t.Fatalf("no rendered card for persona %q", id)
		}
	}
	return cards
}
