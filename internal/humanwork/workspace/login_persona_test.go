package workspace

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type personaRoleAccessStore struct {
	roleaccess.Store
	snapshot roleaccess.Snapshot
	err      error
}

func (store personaRoleAccessStore) Load(context.Context, values.TenantId, string) (roleaccess.Snapshot, error) {
	return store.snapshot, store.err
}

func TestTodo_UXAUDIT_014_PersonaCopy(t *testing.T) {
	h, _ := newShellHandler(t, true)
	h.devPersonas = map[string]DevPersona{
		"admin":                  {ID: "admin", Name: "Rafael Torres", Token: frontendE2EToken(t, "copy-admin", []string{"comp_admin"})},
		"individual-contributor": {ID: "individual-contributor", Name: "Samuel Rivera", Token: frontendE2EToken(t, "copy-self", []string{"worker_self"})},
	}
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, PathLogin, nil))
	body := recorder.Body.String()
	if !strings.Contains(body, "HCM administrator") || !strings.Contains(body, "Individual contributor") {
		t.Fatalf("persona cards did not derive role-appropriate copy: %s", body)
	}
	if strings.Contains(body, "recruit") || strings.Contains(body, "payroll") || strings.Contains(body, "hiring") {
		t.Fatal("login cards promised an unverified product area")
	}
}

func TestTodo_UXAUDIT_014_Integration(t *testing.T) {
	h, token := newShellHandler(t, true)
	h.now = func() time.Time { return time.Now().UTC() }
	h.devPersonas = map[string]DevPersona{"self": {ID: "self", Name: "Samuel Rivera", WorkerRef: shellSubject, Token: token}}
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	client := server.Client()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client.Jar = jar
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.PostForm(server.URL+PathLogin, url.Values{paramLoginPersona: {"self"}})
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("login status = %d", response.StatusCode)
	}
	page, err := client.Get(server.URL + PathProductHome)
	if err != nil {
		t.Fatal(err)
	}
	defer page.Body.Close()
	if page.StatusCode != http.StatusOK {
		t.Fatalf("home status = %d", page.StatusCode)
	}
}

func TestTodo_UXAUDIT_014_Security_PersonaCopy(t *testing.T) {
	h, _ := newShellHandler(t, true)
	h.now = func() time.Time { return time.Now().UTC() }
	h.devPersonas = map[string]DevPersona{"self": {ID: "self", Name: "Samuel Rivera", Token: frontendE2EToken(t, "security-self", []string{"worker_self"})}}
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	client := server.Client()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client.Jar = jar
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	login, err := client.PostForm(server.URL+PathLogin, url.Values{paramLoginPersona: {"self"}})
	if err != nil {
		t.Fatal(err)
	}
	login.Body.Close()
	for _, route := range []string{"/workspace/app/people", "/workspace/app/admin"} {
		response, getErr := client.Get(server.URL + route)
		if getErr != nil {
			t.Fatal(getErr)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Errorf("self GET %s = %d, want 403", route, response.StatusCode)
		}
	}
}

func TestTodo_UXAUDIT_014_Security_DurablePolicy(t *testing.T) {
	const subject = "policy-subject"
	makeHandler := func() *Handler {
		h, _ := newShellHandler(t, true)
		h.now = func() time.Time { return time.Now().UTC() }
		h.devPersonas = map[string]DevPersona{"admin": {
			ID: "admin", Name: "Rafael Torres", WorkerRef: subject,
			Token: frontendE2EToken(t, subject, []string{"hcm_admin"}),
		}}
		return h
	}
	post := func(h *Handler) *httptest.ResponseRecorder {
		t.Helper()
		form := url.Values{paramLoginPersona: {"admin"}}
		req := httptest.NewRequest(http.MethodPost, PathLogin, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response := httptest.NewRecorder()
		h.ServeHTTP(response, req)
		return response
	}
	t.Run("policy read failure never promises access or issues a session", func(t *testing.T) {
		h := makeHandler()
		h.roleAccess = personaRoleAccessStore{err: errors.New("store unavailable")}
		page := httptest.NewRecorder()
		h.ServeHTTP(page, httptest.NewRequest(http.MethodGet, PathLogin, nil))
		if strings.Contains(page.Body.String(), "Open workspace administration") || !strings.Contains(page.Body.String(), "Access is temporarily unavailable") {
			t.Fatal("login card retained an administrative promise after policy load failed")
		}
		response := post(h)
		if response.Code != http.StatusServiceUnavailable || len(response.Result().Cookies()) != 0 {
			t.Fatalf("failed policy login = %d, cookies = %d", response.Code, len(response.Result().Cookies()))
		}
	})
	t.Run("configured empty policy denies all pages", func(t *testing.T) {
		h := makeHandler()
		h.roleAccess = personaRoleAccessStore{snapshot: roleaccess.Snapshot{}}
		response := post(h)
		if response.Code != http.StatusForbidden || len(response.Result().Cookies()) != 0 {
			t.Fatalf("empty policy login = %d, cookies = %d", response.Code, len(response.Result().Cookies()))
		}
	})
	t.Run("durable assignment overrides the broad credential", func(t *testing.T) {
		h := makeHandler()
		h.roleAccess = personaRoleAccessStore{snapshot: roleaccess.Snapshot{
			Assignments:     []roleaccess.Assignment{{WorkerRef: subject, RoleIDs: []string{"worker_self"}}},
			PagePermissions: roleaccess.DefaultPagePermissions(),
		}}
		page := httptest.NewRecorder()
		h.ServeHTTP(page, httptest.NewRequest(http.MethodGet, PathLogin, nil))
		if strings.Contains(page.Body.String(), "Open workspace administration") || !strings.Contains(page.Body.String(), "View your employment profile") {
			t.Fatal("login card ignored the effective self-service assignment")
		}
		response := post(h)
		if response.Code != http.StatusSeeOther || response.Header().Get("Location") != PathProductPrefix+"myself" {
			t.Fatalf("assigned self-service landing = %d, %q", response.Code, response.Header().Get("Location"))
		}
		request := httptest.NewRequest(http.MethodGet, PathProductPrefix+"admin", nil)
		for _, cookie := range response.Result().Cookies() {
			request.AddCookie(cookie)
		}
		admin := httptest.NewRecorder()
		h.ServeHTTP(admin, request)
		if admin.Code != http.StatusForbidden {
			t.Fatalf("assigned self-service credential reached Admin: %d", admin.Code)
		}
	})
}

func TestTodo_UXAUDIT_014_Regression_PersonaCopy(t *testing.T) {
	h, _ := newShellHandler(t, true)
	h.devPersonas = map[string]DevPersona{"self": {ID: "self", Name: "Samuel Rivera", WorkerRef: "different-worker", Token: frontendE2EToken(t, "regression-self", []string{"worker_self"})}}
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, PathLogin, nil))
	body := recorder.Body.String()
	if strings.Contains(body, "View your employment profile") || strings.Contains(body, "Individual contributor") {
		t.Fatal("mismatched worker binding regained role-specific promise")
	}
	form := url.Values{paramLoginPersona: {"self"}}
	request := httptest.NewRequest(http.MethodPost, PathLogin, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	login := httptest.NewRecorder()
	h.ServeHTTP(login, request)
	if login.Code != http.StatusUnauthorized || len(login.Result().Cookies()) != 0 {
		t.Fatalf("mismatched worker signed in: status %d, cookies %d", login.Code, len(login.Result().Cookies()))
	}
}

func TestDevPersonaLoginKeepsCredentialServerSide(t *testing.T) {
	h, token := newShellHandler(t, true)
	h.devPersonas = map[string]DevPersona{"admin": {ID: "admin", Name: "Jane Doe", Access: "HCM administrator", Description: "All areas.", Token: token}}
	form := url.Values{paramLoginPersona: {"admin"}}
	request := httptest.NewRequest(http.MethodPost, "http://cell.test"+PathLogin, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != PathProductHome {
		t.Fatalf("persona login = %d location %q", recorder.Code, recorder.Header().Get("Location"))
	}
	if strings.Contains(recorder.Body.String(), token) {
		t.Fatal("persona credential entered the response body")
	}
	var found bool
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == loginSessionCookie {
			found = cookie.HttpOnly && cookie.SameSite == http.SameSiteStrictMode && cookie.Value == token
		}
	}
	if !found {
		t.Fatal("persona login did not set the hardened session cookie")
	}
}

func TestDevPersonaLoginPageRendersIdentityNotCredential(t *testing.T) {
	h, token := newShellHandler(t, true)
	h.devPersonas = map[string]DevPersona{"admin": {ID: "admin", Name: "Jane Doe", Access: "HCM administrator", Description: "All areas.", Token: token}}
	request := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathLogin, nil)
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	for _, want := range []string{"Choose a workspace persona", "Jane Doe", "HCM administrator"} {
		if !strings.Contains(body, want) {
			t.Errorf("login page missing %q", want)
		}
	}
	if !strings.Contains(body, `<button type="submit" name="persona" value="admin">Continue as Jane Doe</button>`) {
		t.Fatal("persona choice is not carried by its submit button")
	}
	if strings.Contains(body, `type="hidden" name="persona"`) {
		t.Fatal("persona login still depends on a hidden selector")
	}
	if strings.Contains(body, token) {
		t.Fatal("login page disclosed a persona credential")
	}
	if got := recorder.Header().Get("Content-Security-Policy"); !strings.Contains(got, "style-src '"+sha256Source(loginStylesheet())+"'") {
		t.Fatalf("login stylesheet is not CSP-pinned: %q", got)
	}
}

func TestDevPersonaLoginCopyFollowsVerifiedRolesNotPersonaID(t *testing.T) {
	h, _ := newShellHandler(t, true)
	// The display ID says admin, but the admitted credential is self-service.
	// A card must not promise administrative pages or actions from its label.
	h.devPersonas = map[string]DevPersona{
		"admin": {
			ID: "admin", Name: "Rafael Torres", Access: "HCM administrator",
			Description: "all administration areas", Token: frontendE2EToken(t, "self-copy-mismatch", []string{"worker_self"}),
		},
	}
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://cell.test"+PathLogin, nil))
	body := recorder.Body.String()
	if strings.Contains(body, "approve compensation") || strings.Contains(body, "manage workspace settings") {
		t.Fatal("persona ID or descriptor overpromised capabilities")
	}
	for _, want := range []string{"Individual contributor", "View your employment profile"} {
		if !strings.Contains(body, want) {
			t.Errorf("role-derived login copy missing %q", want)
		}
	}
}

func TestDevPersonaLoginCopyIsNeutralWhenCredentialCannotBeVerified(t *testing.T) {
	h, _ := newShellHandler(t, true)
	h.devPersonas = map[string]DevPersona{
		"admin": {
			ID: "admin", Name: "Rafael Torres", Access: "HCM administrator",
			Description: "All product and administration areas.", Token: "not-a-token",
		},
	}
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://cell.test"+PathLogin, nil))
	body := recorder.Body.String()
	if strings.Contains(body, "All product and administration areas") || strings.Contains(body, "approve compensation") {
		t.Fatal("unverified persona copy promised capabilities")
	}
	if !strings.Contains(body, "Available pages and actions depend on your assigned access.") {
		t.Fatal("unverified persona copy did not explain the access boundary")
	}
}

func TestDevPersonaLoginCopyRejectsMismatchedWorkerBinding(t *testing.T) {
	h, _ := newShellHandler(t, true)
	h.devPersonas = map[string]DevPersona{
		"individual-contributor": {
			ID: "individual-contributor", Name: "Samuel Rivera", WorkerRef: "another-worker",
			Token: frontendE2EToken(t, "self-copy-bound", []string{"worker_self"}),
		},
	}
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://cell.test"+PathLogin, nil))
	body := recorder.Body.String()
	if strings.Contains(body, "View your employment profile") || strings.Contains(body, "Individual contributor") {
		t.Fatal("mismatched worker binding retained role-specific copy")
	}
	if !strings.Contains(body, "Available pages and actions depend on your assigned access.") {
		t.Fatal("mismatched worker binding did not fall back to neutral copy")
	}
}
