package workspace

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

const clockNavHref = `href="/workspace/app/time/clock"`

func uxblind123Server(t *testing.T, clockEnabled bool) *httptest.Server {
	t.Helper()
	handler, _ := newShellHandler(t, true)
	handler.roleAccess = launcherRoleAccessStore{snapshot: roleaccess.Snapshot{PagePermissions: roleaccess.DefaultPagePermissions()}}
	handler.now = func() time.Time { return time.Now().UTC() }
	handler.clockEnabled = clockEnabled
	handler.devPersonas = map[string]DevPersona{}
	for _, persona := range []uxblind122Persona{uxblind122Admin, uxblind122Worker} {
		handler.devPersonas[persona.id] = DevPersona{ID: persona.id, Name: persona.name, WorkerRef: persona.subject, Token: frontendE2EToken(t, persona.subject, persona.roles)}
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

// renderClockIslandPage renders the Time clock route from the served island
// through the same projection the browser client installs, with the reason the
// clock service gave for this worker.
func renderClockIslandPage(t *testing.T, config JourneyConfig, locale string, reason productui.ClockReason) string {
	t.Helper()
	view := productui.ApplyLocale(productui.NewView(productui.PageClock, config.Tenant, config.Subject, ""), productui.ResolveProductLocale(locale))
	view = productui.ApplyPagePermissions(view, productPagePermissions(config.PagePermissions))
	view = productui.ApplyClockAvailability(view, ProductClockAvailability(config.Clock))
	view = productui.ApplyLocale(view, productui.ResolveProductLocale(locale))
	view.ClockProjection = productui.ClockProjection{State: productui.ClockProjectionUnavailable, Reason: reason}
	markup, err := ui.RenderToString(productui.ClockPage(view, view.ClockProjection))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

// TestTodo_UXBLIND_123_Browser drives the browser-facing HTTP contract through
// a real loopback server with signed-in personas: the served navigation, the
// config island and the page rendered from that island, on a cell that runs the
// time clock and on one that does not. It does not launch a browser; the
// interactive pass is recorded in the devlog and the lane report.
func TestTodo_UXBLIND_123_Browser(t *testing.T) {
	clockURL := "/workspace/app/time/clock"

	off := uxblind123Server(t, false)
	worker := uxblind122SignIn(t, off, uxblind122Worker)
	admin := uxblind122SignIn(t, off, uxblind122Admin)

	// A cell that does not run the clock: a worker's menu does not advertise a
	// dead end, and a direct visit still says why. The administrator's menu
	// keeps the entry, and the page says how to turn the clock on.
	if body := uxblind122Get(t, worker, off.URL+PathProductHome); strings.Contains(body, clockNavHref) {
		t.Fatal("a worker's menu advertises a time clock the workspace does not run")
	}
	workerIsland := island(t, uxblind122Get(t, worker, off.URL+clockURL))
	if workerIsland.Clock == nil || workerIsland.Clock.Enabled || workerIsland.Clock.ViewerIsAdmin {
		t.Fatalf("worker island = %+v", workerIsland.Clock)
	}
	page := renderClockIslandPage(t, workerIsland, "en-US", productui.ClockReasonNone)
	for _, want := range []string{"The time clock is not turned on for this workspace.", `data-clock-reason="NOT_ENABLED"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("worker direct visit missing %q: %s", want, page)
		}
	}
	for _, forbidden := range []string{"supervisor", "For administrators", "deployment settings", "time-database-url"} {
		if strings.Contains(page, forbidden) {
			t.Fatalf("worker direct visit must not contain %q: %s", forbidden, page)
		}
	}
	adminBody := uxblind122Get(t, admin, off.URL+clockURL)
	if !strings.Contains(adminBody, clockNavHref) {
		t.Fatal("an administrator's menu hides the time clock instead of explaining it")
	}
	adminIsland := island(t, adminBody)
	if adminIsland.Clock == nil || adminIsland.Clock.Enabled || !adminIsland.Clock.ViewerIsAdmin {
		t.Fatalf("admin island = %+v", adminIsland.Clock)
	}
	for locale, want := range map[string][]string{
		"en-US": {"For administrators", "deployment settings"},
		"de-DE": {"Für Administratoren", "Bereitstellungseinstellungen"},
		"ar":    {"للمسؤولين", "إعدادات نشر"},
	} {
		page := renderClockIslandPage(t, adminIsland, locale, productui.ClockReasonNone)
		for _, w := range want {
			if !strings.Contains(page, w) {
				t.Fatalf("%s admin page missing %q: %s", locale, w, page)
			}
		}
	}

	// A cell that runs the clock advertises it to everyone. What a worker is
	// told is the clock service's decision about them, and only an
	// administrator is shown how to change it.
	on := uxblind123Server(t, true)
	worker = uxblind122SignIn(t, on, uxblind122Worker)
	admin = uxblind122SignIn(t, on, uxblind122Admin)
	for name, client := range map[string]*http.Client{"worker": worker, "admin": admin} {
		if body := uxblind122Get(t, client, on.URL+PathProductHome); !strings.Contains(body, clockNavHref) {
			t.Fatalf("%s menu does not advertise a running time clock", name)
		}
	}
	workerIsland = island(t, uxblind122Get(t, worker, on.URL+clockURL))
	if workerIsland.Clock == nil || !workerIsland.Clock.Enabled || workerIsland.Clock.ViewerIsAdmin {
		t.Fatalf("worker island on a running clock = %+v", workerIsland.Clock)
	}
	adminIsland = island(t, uxblind122Get(t, admin, on.URL+clockURL))
	if adminIsland.Clock == nil || !adminIsland.Clock.Enabled || !adminIsland.Clock.ViewerIsAdmin {
		t.Fatalf("admin island on a running clock = %+v", adminIsland.Clock)
	}
	for _, reason := range []productui.ClockReason{productui.ClockReasonNoTimeProfile, productui.ClockReasonExempt} {
		workerPage := renderClockIslandPage(t, workerIsland, "en-US", reason)
		adminPage := renderClockIslandPage(t, adminIsland, "en-US", reason)
		if !strings.Contains(workerPage, `data-clock-reason="`+string(reason)+`"`) || strings.Contains(workerPage, "For administrators") || strings.Contains(workerPage, "supervisor") {
			t.Fatalf("worker page for %s = %s", reason, workerPage)
		}
		if !strings.Contains(adminPage, "For administrators") || strings.Contains(adminPage, "supervisor") {
			t.Fatalf("admin page for %s = %s", reason, adminPage)
		}
	}
}

// uxblind123Access is the viewer's access as the durable role policy resolves
// it: the default policy narrowed to the roles the viewer holds.
func uxblind123Access(roles ...string) productAccess {
	snapshot := roleaccess.Snapshot{PagePermissions: roleaccess.DefaultPagePermissions()}
	return productAccess{roles: roles, configured: true, permissions: roleaccess.EffectivePagePermissions(snapshot, roles)}
}

// TestTodo_UXBLIND_123_Security proves the availability the browser is given
// is derived on the server and cannot be forged from the client: the
// administrator bit comes from the durable role policy, a missing island fails
// closed, and an unknown reason is never rendered.
func TestTodo_UXBLIND_123_Security(t *testing.T) {
	h := &Handler{clockEnabled: true}
	for _, tc := range []struct {
		name      string
		access    productAccess
		wantAdmin bool
	}{
		{name: "worker", access: uxblind123Access("worker_self")},
		{name: "administrator", access: uxblind123Access(productui.RoleHCMAdmin), wantAdmin: true},
		{name: "no role policy", access: productAccess{}},
	} {
		got := h.resolveClock(tc.access)
		if got == nil || !got.Enabled || got.ViewerIsAdmin != tc.wantAdmin {
			t.Fatalf("%s: resolveClock = %+v, want admin %v", tc.name, got, tc.wantAdmin)
		}
	}
	if got := (&Handler{}).resolveClock(uxblind123Access(productui.RoleHCMAdmin)); got == nil || got.Enabled {
		t.Fatalf("a cell that composed no clock reported one: %+v", got)
	}
	// A missing island fails closed, and a forged one claims nothing it was
	// not given: the client never learns the administrator bit from the page.
	if got := ProductClockAvailability(nil); got.Enabled || got.ViewerIsAdmin {
		t.Fatalf("missing island projection = %+v", got)
	}
	// A worker's served island never carries the administrator bit, whatever
	// the query string asks for.
	server := uxblind123Server(t, true)
	worker := uxblind122SignIn(t, server, uxblind122Worker)
	served := island(t, uxblind122Get(t, worker, server.URL+"/workspace/app/time/clock?viewer_is_admin=true&enabled=false"))
	if served.Clock == nil || served.Clock.ViewerIsAdmin || !served.Clock.Enabled {
		t.Fatalf("query string influenced the clock island: %+v", served.Clock)
	}
	// An unknown reason from a hostile boundary is not rendered.
	page := renderClockIslandPage(t, served, "en-US", productui.ClockReason("ASK_YOUR_SUPERVISOR"))
	if strings.Contains(page, "ASK_YOUR_SUPERVISOR") || strings.Contains(strings.ToLower(page), "supervisor") {
		t.Fatalf("an unknown reason reached the page: %s", page)
	}
}
