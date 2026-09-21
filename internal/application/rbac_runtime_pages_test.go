package application

// Page-visibility and per-page CRUD cases for TestRBACRuntime (Gate B,
// RBAC-RT-015..021). They run after every case in rbacCases because several
// change grants; each isolates itself on custom roles (rbac_probe,
// rbac_viewer, rbac_role_admin, rbac_newpage, rbac_navprobe) and on users no
// earlier case uses, and every write to a built-in role is restored by the
// operation that made it.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	kernelvalues "github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const rbacFixtureActor = "system:rbac-runtime-fixture"

// rbacPageCase is one case plus the state it needs first.
type rbacPageCase struct {
	rbacCase
	setup func(*rbacHarness)
}

// issuePageUsers signs the users only these cases use. None has a durable
// assignment until a case's setup gives it one, and none carries a product
// role in its credential.
func (h *rbacHarness) issuePageUsers() {
	h.t.Helper()
	now := time.Now()
	for _, u := range []rbacUser{
		{"viewer", "rbac-val", nil, "compensation_review"},
		{"roleAdmin", "rbac-ria", nil, "compensation_review"},
		{"selfAdmin", "rbac-ada", []string{"hcm_admin"}, "compensation_review"},
		{"pageUser", "rbac-pia", nil, "compensation_review"},
		{"navUser", "rbac-nia", nil, "compensation_review"},
	} {
		h.tokens[u.name] = h.issue(h.verifier, u, h.cfg.Tenant, now.Add(-time.Minute), now.Add(8*time.Hour))
	}
	h.tokens["otherTenantNoRoles"] = h.issue(h.verifier, rbacUser{"otherTenantNoRoles", "principal:rbac-other-no-roles", nil, "compensation_review"},
		"other-tenant", now.Add(-time.Minute), now.Add(time.Hour))
}

// --- direct store setup (the state a case probes, not the thing it tests) ---

func (h *rbacHarness) tenantID() kernelvalues.TenantId { return kernelvalues.TenantId(h.cfg.Tenant) }

func (h *rbacHarness) snapshot() roleaccess.Snapshot {
	h.t.Helper()
	snapshot, err := h.composed.Cell().RoleAccess.Load(context.Background(), h.tenantID(), rbacOrgScope)
	if err != nil {
		h.t.Fatalf("load role access: %v", err)
	}
	return snapshot
}

func (h *rbacHarness) roleOf(id string) (roleaccess.Role, bool) {
	for _, role := range h.snapshot().Roles {
		if role.ID == id {
			return role, true
		}
	}
	return roleaccess.Role{}, false
}

// storeRole creates role id, or updates it to name/active at its version.
func (h *rbacHarness) storeRole(id, name, description string, active bool) {
	h.t.Helper()
	role := roleaccess.Role{ID: id, Name: name, Description: description, Active: true}
	if current, ok := h.roleOf(id); ok {
		role.Version, role.Active = current.Version, active
	} else if !active {
		h.t.Fatalf("storeRole %s: cannot create an inactive role", id)
	}
	if _, err := h.composed.Cell().RoleAccess.SaveRole(context.Background(), h.tenantID(), rbacFixtureActor, role); err != nil {
		h.t.Fatalf("SaveRole %s: %v", id, err)
	}
}

func (h *rbacHarness) pageRow(role, page string) (roleaccess.PagePermission, bool) {
	for _, p := range h.snapshot().PagePermissions {
		if p.RoleID == role && p.PageID == page {
			return p, true
		}
	}
	return roleaccess.PagePermission{}, false
}

func (h *rbacHarness) storePage(p roleaccess.PagePermission) {
	h.t.Helper()
	p.Version = 0
	if current, ok := h.pageRow(p.RoleID, p.PageID); ok {
		p.Version = current.Version
	}
	if _, err := h.composed.Cell().RoleAccess.SavePagePermission(context.Background(), h.tenantID(), rbacFixtureActor, p); err != nil {
		h.t.Fatalf("SavePagePermission %s/%s: %v", p.RoleID, p.PageID, err)
	}
}

func (h *rbacHarness) featureRow(role, page, feature string) (roleaccess.FeaturePermission, bool) {
	for _, f := range h.snapshot().FeaturePermissions {
		if f.RoleID == role && f.PageID == page && f.FeatureID == feature {
			return f, true
		}
	}
	return roleaccess.FeaturePermission{}, false
}

func (h *rbacHarness) storeFeature(f roleaccess.FeaturePermission) {
	h.t.Helper()
	f.Version = 0
	if current, ok := h.featureRow(f.RoleID, f.PageID, f.FeatureID); ok {
		f.Version = current.Version
	}
	if _, err := h.composed.Cell().RoleAccess.SaveFeaturePermission(context.Background(), h.tenantID(), rbacFixtureActor, f); err != nil {
		h.t.Fatalf("SaveFeaturePermission %s/%s/%s: %v", f.RoleID, f.PageID, f.FeatureID, err)
	}
}

func (h *rbacHarness) storeAssign(worker string, roles ...string) {
	h.t.Helper()
	assignment := roleaccess.Assignment{WorkerRef: worker, RoleIDs: roles}
	for _, a := range h.snapshot().Assignments {
		if a.WorkerRef == worker {
			assignment.Version = a.Version
		}
	}
	if _, err := h.composed.Cell().RoleAccess.SaveAssignment(context.Background(), h.tenantID(), rbacFixtureActor, assignment); err != nil {
		h.t.Fatalf("SaveAssignment %s %v: %v", worker, roles, err)
	}
}

// pageBody fetches one workspace page as a user and returns status and body.
func (h *rbacHarness) pageBody(user, page string) (int, string, error) {
	req, err := http.NewRequest(http.MethodGet, "http://"+h.composed.HTTPAddr()+"/workspace/app/"+page, nil)
	if err != nil {
		return 0, "", err
	}
	req.AddCookie(&http.Cookie{Name: "hcmnext_session", Value: h.tokens[user]})
	res, err := h.http.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	return res.StatusCode, string(body), err
}

// auditTrail counts the durable records a permission change could leave:
// ledger events naming a role or permission, and rows in any revision,
// history or audit table of the role-access model.
func (h *rbacHarness) auditTrail() int64 {
	h.t.Helper()
	ctx := context.Background()
	var total int64
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM ledger_event WHERE stream_key ILIKE '%role%' OR stream_key ILIKE '%permission%'
		OR schema_ref ILIKE '%role%' OR schema_ref ILIKE '%permission%'`).Scan(&total); err != nil {
		h.t.Fatalf("count ledger events: %v", err)
	}
	rows, err := h.pool.Query(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema()
		AND table_name ~ '(role|permission|access).*(revision|history|audit)'`)
	if err != nil {
		h.t.Fatalf("list revision tables: %v", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			h.t.Fatalf("scan table: %v", err)
		}
		tables = append(tables, name)
	}
	rows.Close()
	for _, table := range tables {
		var n int64
		if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil {
			h.t.Fatalf("count %s: %v", table, err)
		}
		total += n
	}
	return total
}

// adminSavePage is admin's SaveRolePagePermission for role/page at its
// current version.
func (h *rbacHarness) adminSavePage(user string, p roleaccess.PagePermission) error {
	if current, ok := h.pageRow(p.RoleID, p.PageID); ok {
		p.Version = current.Version
	}
	_, err := h.journey.SaveRolePagePermission(h.rpc(user), &journeyv1.SaveRolePagePermissionRequest{Permission: &journeyv1.RolePagePermission{
		Version: p.Version, RoleId: p.RoleID, PageId: p.PageID, CanView: p.View, CanCreate: p.Create, CanUpdate: p.Update, CanDelete: p.Delete}})
	return err
}

// splitSubject splits "a|b" subjects.
func splitSubject(subject string) (string, string) {
	a, b, _ := strings.Cut(subject, "|")
	return a, b
}

func init() {
	ops := map[string]rbacOp{
		// The first message of the stream, or its refusal.
		"Journey.WatchJourney": func(h *rbacHarness, user, _ string) rbacResult {
			ctx, cancel := context.WithTimeout(h.rpc(user), 15*time.Second)
			defer cancel()
			stream, err := h.journey.WatchJourney(ctx, &journeyv1.WatchJourneyRequest{IntentId: h.intentID})
			if err == nil {
				_, err = stream.Recv()
			}
			return grpcResult(err, false)
		},
		"Journey.GetProductPreferences": func(h *rbacHarness, user, _ string) rbacResult {
			_, err := h.journey.GetProductPreferences(h.rpc(user), &journeyv1.GetProductPreferencesRequest{})
			return grpcResult(err, false)
		},
		"Journey.GetWorkerIDPolicy": func(h *rbacHarness, user, _ string) rbacResult {
			_, err := h.journey.GetWorkerIDPolicy(h.rpc(user), &journeyv1.GetWorkerIDPolicyRequest{})
			return grpcResult(err, false)
		},
		"Journey.RecordWorkflowUse": func(h *rbacHarness, user, _ string) rbacResult {
			_, err := h.journey.RecordWorkflowUse(h.rpc(user), &journeyv1.RecordWorkflowUseRequest{WorkflowId: "rbac-runtime-probe"})
			return grpcResult(err, false)
		},
		"Journey.AddJourneyNote": func(h *rbacHarness, user, _ string) rbacResult {
			_, err := h.journey.AddJourneyNote(h.rpc(user), &journeyv1.AddJourneyNoteRequest{
				IntentId: h.intentID, Body: "rbac runtime probe", IdempotencyKey: "rbac-note-" + user})
			return grpcResult(err, false)
		},
		// subject "role|page": grant view on the page.
		"Journey.SaveRolePagePermission": func(h *rbacHarness, user, subject string) rbacResult {
			role, page := splitSubject(subject)
			return grpcResult(h.adminSavePage(user, roleaccess.PagePermission{RoleID: role, PageID: page, View: true}), true)
		},
		// subject "role|page": revoke every action on the page.
		"Journey.SaveRolePagePermission/revoke": func(h *rbacHarness, user, subject string) rbacResult {
			role, page := splitSubject(subject)
			return grpcResult(h.adminSavePage(user, roleaccess.PagePermission{RoleID: role, PageID: page}), true)
		},
		// subject "role|page": view on the page's content feature.
		"Journey.SaveRoleFeaturePermission": func(h *rbacHarness, user, subject string) rbacResult {
			role, page := splitSubject(subject)
			f := roleaccess.FeaturePermission{RoleID: role, PageID: page, FeatureID: "content", View: true}
			if current, ok := h.featureRow(role, page, "content"); ok {
				f.Version = current.Version
			}
			_, err := h.journey.SaveRoleFeaturePermission(h.rpc(user), &journeyv1.SaveRoleFeaturePermissionRequest{Permission: &journeyv1.RoleFeaturePermission{
				Version: f.Version, RoleId: f.RoleID, PageId: f.PageID, FeatureId: f.FeatureID, CanView: f.View}})
			return grpcResult(err, true)
		},
		// subject: a role to deactivate, keeping its name.
		"Journey.SaveAccessRole/deactivate": func(h *rbacHarness, user, subject string) rbacResult {
			before, ok := h.roleOf(subject)
			if !ok {
				h.t.Fatalf("no role %s", subject)
			}
			_, err := h.journey.SaveAccessRole(h.rpc(user), &journeyv1.SaveAccessRoleRequest{Role: &journeyv1.AccessRole{
				Version: before.Version, RoleId: before.ID, Name: before.Name, Description: before.Description, System: before.System, Active: false}})
			if err == nil && before.System {
				h.storeRole(before.ID, before.Name, before.Description, true) // restore a built-in role
			}
			return grpcResult(err, true)
		},
		// subject: a role to rename; a built-in role is restored afterwards.
		"Journey.SaveAccessRole/rename": func(h *rbacHarness, user, subject string) rbacResult {
			before, ok := h.roleOf(subject)
			if !ok {
				h.t.Fatalf("no role %s", subject)
			}
			_, err := h.journey.SaveAccessRole(h.rpc(user), &journeyv1.SaveAccessRoleRequest{Role: &journeyv1.AccessRole{
				Version: before.Version, RoleId: before.ID, Name: before.Name + " (renamed)", Description: before.Description, System: before.System, Active: before.Active}})
			if err == nil {
				h.storeRole(before.ID, before.Name, before.Description, before.Active)
			}
			return grpcResult(err, true)
		},
		// subject "worker|role,role": assign exactly these roles.
		"Journey.SaveWorkerRoleAssignment/set": func(h *rbacHarness, user, subject string) rbacResult {
			worker, roles := splitSubject(subject)
			var version int64
			for _, a := range h.snapshot().Assignments {
				if a.WorkerRef == worker {
					version = a.Version
				}
			}
			_, err := h.journey.SaveWorkerRoleAssignment(h.rpc(user), &journeyv1.SaveWorkerRoleAssignmentRequest{
				Assignment: &journeyv1.WorkerRoleAssignment{Version: version, WorkerRef: worker, RoleIds: strings.Split(roles, ",")}})
			return grpcResult(err, true)
		},
		// The last-administrator guard: every role but hcm_admin loses its
		// roles:update grant directly in the store, then admin removes
		// hcm_admin's through the RPC. Every row is restored afterwards.
		"Journey.SaveRolePagePermission/lockout": func(h *rbacHarness, user, _ string) rbacResult {
			var holders []roleaccess.PagePermission
			for _, p := range h.snapshot().PagePermissions {
				if p.PageID == "roles" && p.Update {
					holders = append(holders, p)
				}
			}
			defer func() {
				for _, p := range holders {
					h.storePage(p)
				}
			}()
			var admin roleaccess.PagePermission
			for _, p := range holders {
				if p.RoleID == "hcm_admin" {
					admin = p
					continue
				}
				narrowed := p
				narrowed.Update = false
				h.storePage(narrowed)
			}
			if admin.RoleID == "" {
				h.t.Fatal("hcm_admin holds no roles:update grant to remove")
			}
			admin.Update = false
			return grpcResult(h.adminSavePage(user, admin), true)
		},
		// row: after the change, an audit record exists that did not before.
		"Audit.SaveRolePagePermission": func(h *rbacHarness, user, subject string) rbacResult {
			role, page := splitSubject(subject)
			before := h.auditTrail()
			current, _ := h.pageRow(role, page)
			err := h.adminSavePage(user, roleaccess.PagePermission{RoleID: role, PageID: page, View: !current.View})
			r := grpcResult(err, false)
			if err == nil {
				after := h.auditTrail()
				r.row = boolPtr(after > before)
				r.detail = fmt.Sprintf("audit records %d -> %d", before, after)
			}
			return r
		},
		// row: GetRoleAccess shows a feature row granting view to role on page.
		"Journey.GetRoleAccess/feature-grant": func(h *rbacHarness, user, subject string) rbacResult {
			role, page := splitSubject(subject)
			resp, err := h.journey.GetRoleAccess(h.rpc(user), &journeyv1.GetRoleAccessRequest{})
			r := grpcResult(err, false)
			if err == nil {
				found := false
				for _, f := range resp.GetFeaturePermissions() {
					found = found || f.GetRoleId() == role && f.GetPageId() == page && f.GetCanView()
				}
				r.row = boolPtr(found)
			}
			return r
		},
		// subject "page|linked": the page's HTML links to /workspace/app/linked.
		"Workspace.nav": func(h *rbacHarness, user, subject string) rbacResult {
			page, linked := splitSubject(subject)
			code, body, err := h.pageBody(user, page)
			if err != nil {
				return rbacResult{class: rbacError, detail: err.Error()}
			}
			if code != http.StatusOK {
				return rbacResult{class: rbacError, detail: fmt.Sprintf("HTTP %d fetching %s", code, page)}
			}
			return rbacResult{class: rbacAllow, detail: "HTTP 200", row: boolPtr(strings.Contains(body, `"/workspace/app/`+linked+`"`))}
		},
	}
	for name, op := range ops {
		rbacOps[name] = op
	}
}

// Setups.

func setupProbe(h *rbacHarness) {
	h.storeRole("rbac_probe", "RBAC probe", "Inert role the RBAC runtime suite writes.", true)
}

// setupViewer: rbac_viewer holds the people page and its content and
// directory features, and is rbac-val's only role.
func setupViewer(h *rbacHarness) {
	h.storeRole("rbac_viewer", "RBAC viewer", "People directory viewer for the RBAC runtime suite.", true)
	h.storePage(roleaccess.PagePermission{RoleID: "rbac_viewer", PageID: "people", View: true})
	h.storeFeature(roleaccess.FeaturePermission{RoleID: "rbac_viewer", PageID: "people", FeatureID: "content", View: true})
	h.storeFeature(roleaccess.FeaturePermission{RoleID: "rbac_viewer", PageID: "people", FeatureID: "directory", View: true})
	h.storeAssign("rbac-val", "rbac_viewer")
}

// setupRoleAdmin: rbac_role_admin holds the roles page (view, update) and
// its content, actions, role_assignments and feature_access features, and is rbac-ria's
// only role. rbac-ria's credential carries no administrator role.
func setupRoleAdmin(h *rbacHarness) {
	h.storeRole("rbac_role_admin", "RBAC role administrator", "Stored-grant role administrator for the RBAC runtime suite.", true)
	h.storePage(roleaccess.PagePermission{RoleID: "rbac_role_admin", PageID: "roles", View: true, Update: true})
	h.storeFeature(roleaccess.FeaturePermission{RoleID: "rbac_role_admin", PageID: "roles", FeatureID: "content", View: true}) // content is view-only
	for _, feature := range []string{"actions", "role_assignments", "feature_access"} {
		h.storeFeature(roleaccess.FeaturePermission{RoleID: "rbac_role_admin", PageID: "roles", FeatureID: feature, View: true, Update: true})
	}
	h.storeAssign("rbac-ria", "rbac_role_admin")
}

// setupNewPage: rbac_newpage holds nothing yet and is rbac-pia's only role.
func setupNewPage(h *rbacHarness) {
	h.storeRole("rbac_newpage", "RBAC new page", "Post-bootstrap page grant probe for the RBAC runtime suite.", true)
	h.storeAssign("rbac-pia", "rbac_newpage")
}

// setupNewPageFeature gives rbac_newpage the insights content feature a
// correct page grant would have created, so revoking the page can be seen to
// remove (or leave) it.
func setupNewPageFeature(h *rbacHarness) {
	h.storePage(roleaccess.PagePermission{RoleID: "rbac_newpage", PageID: "insights", View: true})
	h.storeFeature(roleaccess.FeaturePermission{RoleID: "rbac_newpage", PageID: "insights", FeatureID: "content", View: true})
}

// setupNav: rbac_navprobe holds home, help and insights pages; home and help
// content features grant view, insights content is revoked (a row, view
// false). rbac-nia's only role.
func setupNav(h *rbacHarness) {
	h.storeRole("rbac_navprobe", "RBAC navigation probe", "Navigation agreement probe for the RBAC runtime suite.", true)
	for _, page := range []string{"home", "help", "insights"} {
		h.storePage(roleaccess.PagePermission{RoleID: "rbac_navprobe", PageID: page, View: true})
		h.storeFeature(roleaccess.FeaturePermission{RoleID: "rbac_navprobe", PageID: page, FeatureID: "content", View: page != "insights"})
	}
	h.storeAssign("rbac-nia", "rbac_navprobe")
}

// rbacPageCases. Sources cited:
//
//	[T016] planning/todos.md RBAC-RT-016: every served call is gated by page,
//	       feature and action; a write needs the write action
//	[T015] RBAC-RT-015: missing rows deny; every tenant bootstrapped
//	[T017] RBAC-RT-017: an inactive role grants nothing; system roles protected
//	[T018] RBAC-RT-018: role administration from stored roles grants; no
//	       self-escalation; last-administrator guard
//	[T019] RBAC-RT-019: page grants create, and revocations remove, feature rows
//	[T020] RBAC-RT-020: navigation agrees with the server's page decision
//	[T021] RBAC-RT-021: every permission change leaves an append-only record
var rbacPageCases = []rbacPageCase{
	// P1. Calls with no page/feature gate [T016].
	// P1-01..03 pass today, refused by the engine (journey.watch.denied),
	// not by a page gate: the transport runs no feature check on the stream.
	// P1-05 is refused by worker_ids' own role check (journey.worker_ids.role_required).
	{rbacCase{"P1-01", "eli", "Journey.WatchJourney", "", rbacDeny}, nil},     // worker_self holds neither journeys nor work
	{rbacCase{"P1-02", "hana", "Journey.WatchJourney", "", rbacDeny}, nil},    // outside the subject's chain
	{rbacCase{"P1-03", "noRoles", "Journey.WatchJourney", "", rbacDeny}, nil}, // no role, no page
	{rbacCase{"P1-04", "noRoles", "Journey.GetProductPreferences", "", rbacDeny}, nil},
	{rbacCase{"P1-05", "noRoles", "Journey.GetWorkerIDPolicy", "", rbacDeny}, nil}, // admin/worker-ids page data
	{rbacCase{"P1-06", "noRoles", "Journey.RecordWorkflowUse", "", rbacDeny}, nil}, // a write with no grant at all
	// The finance partner holds work:update but work/assigned_queue is a
	// view-only feature and it has no journeys page: a note is a write on
	// the journey detail and needs update there.
	{rbacCase{"P1-07", "financePartner", "Journey.AddJourneyNote", "", rbacDeny}, nil},

	// P2. Page/feature permission administration [T018 + ADM].
	{rbacCase{"P2-01", "dana", "Journey.SaveRolePagePermission", "rbac_probe|help", rbacDeny}, setupProbe},
	{rbacCase{"P2-02", "eli", "Journey.SaveRolePagePermission", "rbac_probe|help", rbacDeny}, nil},
	{rbacCase{"P2-03", "hrPartner", "Journey.SaveRolePagePermission", "rbac_probe|help", rbacDeny}, nil},
	{rbacCase{"P2-04", "admin", "Journey.SaveRolePagePermission", "rbac_probe|help", rbacAllow}, nil},
	{rbacCase{"P2-05", "dana", "Journey.SaveRoleFeaturePermission", "rbac_probe|help", rbacDeny}, nil},
	{rbacCase{"P2-06", "eli", "Journey.SaveRoleFeaturePermission", "rbac_probe|help", rbacDeny}, nil},
	{rbacCase{"P2-07", "hrPartner", "Journey.SaveRoleFeaturePermission", "rbac_probe|help", rbacDeny}, nil},
	{rbacCase{"P2-08", "admin", "Journey.SaveRoleFeaturePermission", "rbac_probe|help", rbacAllow}, nil},

	// P3. Inactive roles grant nothing; system roles are protected [T017].
	{rbacCase{"P3-01", "viewer", "Workspace.GET", "people", rbacAllow}, setupViewer}, // control: the active grant works
	{rbacCase{"P3-02", "viewer", "Journey.ListWorkers", "", rbacAllow}, nil},         // control: people/directory view
	{rbacCase{"P3-03", "admin", "Journey.SaveAccessRole/deactivate", "rbac_viewer", rbacAllow}, nil},
	{rbacCase{"P3-04", "viewer", "Workspace.GET", "people", rbacDeny}, nil}, // the only role is inactive
	{rbacCase{"P3-05", "viewer", "Journey.ListWorkers", "", rbacDeny}, nil}, // same, on the gated RPC
	{rbacCase{"P3-06", "admin", "Journey.SaveAccessRole/rename", "hcm_admin", rbacDeny}, nil},
	{rbacCase{"P3-07", "admin", "Journey.SaveAccessRole/deactivate", "hcm_admin", rbacDeny}, nil},

	// P4. Role administration from stored grants [T018].
	{rbacCase{"P4-01", "roleAdmin", "Journey.SaveWorkerRoleAssignment", "rbac-otto", rbacAllow}, setupRoleAdmin},             // stored roles:update + role_assignments
	{rbacCase{"P4-02", "selfAdmin", "Journey.SaveWorkerRoleAssignment/set", "rbac-ada|hcm_admin,comp_admin", rbacDeny}, nil}, // own assignment
	{rbacCase{"P4-03", "admin", "Journey.SaveRolePagePermission/lockout", "", rbacDeny}, nil},                                // leaves no role administrator

	// P5. Page and feature rows stay consistent [T019].
	{rbacCase{"P5-01", "admin", "Journey.SaveRolePagePermission", "rbac_newpage|insights", rbacAllow}, setupNewPage},
	{rbacCase{"P5-02", "pageUser", "Workspace.GET", "insights", rbacAllow}, nil}, // the page grant alone must make the page usable
	{rbacCase{"P5-03", "admin", "Journey.SaveRolePagePermission/revoke", "rbac_newpage|insights", rbacAllow}, setupNewPageFeature},
	{rbacCase{"P5-04", "admin", "Journey.GetRoleAccess/feature-grant", "rbac_newpage|insights", rbacRowNo}, nil}, // revoking the page removed its feature rows

	// P6. Navigation agrees with the page decision [T020].
	{rbacCase{"P6-01", "navUser", "Workspace.nav", "home|help", rbacRowOn}, setupNav}, // control: a viewable page is linked
	{rbacCase{"P6-02", "navUser", "Workspace.GET", "insights", rbacDeny}, nil},        // control: content feature revoked
	{rbacCase{"P6-03", "navUser", "Workspace.nav", "home|insights", rbacRowNo}, nil},  // so navigation must not list it
	// control: the shell does filter links by page grant (no roles page for
	// rbac_navprobe, so none is linked); P6-03 is therefore about features.
	{rbacCase{"P6-04", "navUser", "Workspace.nav", "home|admin/roles", rbacRowNo}, nil},

	// P7. A permission change leaves an audit record [T021].
	{rbacCase{"P7-01", "admin", "Audit.SaveRolePagePermission", "rbac_probe|help", rbacRowOn}, setupProbe},

	// P8. Fail closed for a tenant with no permission rows [T015]. The
	// cell bootstraps only its configured tenant, so other-tenant has an
	// empty role-access model inside the same cell; no second compose.
	{rbacCase{"P8-01", "otherTenant", "Journey.ListJourneys", "", rbacDeny}, nil},
	{rbacCase{"P8-02", "otherTenantNoRoles", "Journey.ListJourneys", "", rbacDeny}, nil},
	{rbacCase{"P8-03", "otherTenantNoRoles", "Journey.ListWorkers", "", rbacDeny}, nil},
	{rbacCase{"P8-04", "otherTenantNoRoles", "Workspace.GET", "home", rbacDeny}, nil}, // the shell already fails closed
}
