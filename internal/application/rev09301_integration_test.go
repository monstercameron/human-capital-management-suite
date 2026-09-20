package application

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const rev09301SigningKey = "rev-093-01-access-preview-signing-key"

// rev09301Serve composes and starts the serve role on embedded PostgreSQL
// with the demo tenant and dev personas, and returns a journey client plus
// a per-persona authenticated context builder.
func rev09301Serve(t *testing.T) (journeyv1.JourneyServiceClient, func(string) context.Context) {
	t.Helper()
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	cfg := ServeConfig{
		GRPCListen: "127.0.0.1:0", HTTPListen: "127.0.0.1:0", DatabaseURL: db.URL,
		DevHMACKey: rev09301SigningKey, Issuer: DefaultIssuer, Audience: DefaultAudience,
		Tenant: demoworkforce.CompanyKey, CellID: "cell-rev-093-01", MaxDeadline: 60 * time.Second,
		Workspace: true, DevBrowserLogin: true, OTelExporter: OTelExporterNone,
		ExecutionAuthority: true, ExecutionAuthorityDigest: "sha256:rev-093-01",
		ExecutionAuthorityRole: "promotion_operator", ExecutionApprover: "principal:promotion-approver",
		ExecutionFinancePartner: LocalDevFinancePartner,
		WorkflowPlan:            WorkflowPlanExecute, TimerTzdbVersion: DefaultTimerTzdbVersion,
		TimerCalendarVersion: DefaultTimerCalendarVersion,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("configuration: %v", err)
	}
	composed, err := ComposeServe(context.Background(), ServeInput{Config: cfg, Pool: pool, Identity: "rev-093-01", Options: Options{ProviderReceipts: newFakeProviderReceipts()}})
	if err != nil {
		t.Fatalf("ComposeServe: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		stopCtx, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		_ = composed.Stop(stopCtx)
	})
	if err := composed.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{Key: []byte(cfg.DevHMACKey), Issuer: cfg.Issuer, Audience: cfg.Audience})
	if err != nil {
		t.Fatalf("verifier: %v", err)
	}
	tokens := map[string]string{}
	for _, persona := range composeDevPersonas(verifier, cfg, time.Now) {
		tokens[persona.ID] = persona.Token
	}
	conn, err := grpc.NewClient(composed.GRPCAddr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc client: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	as := func(persona string) context.Context {
		token, ok := tokens[persona]
		if !ok {
			t.Fatalf("no persona %q", persona)
		}
		callCtx, callCancel := context.WithTimeout(context.Background(), 60*time.Second)
		t.Cleanup(callCancel)
		return metadata.AppendToOutgoingContext(callCtx, transport.AuthorizationMetadataKey, "Bearer "+token)
	}
	return journeyv1.NewJourneyServiceClient(conn), as
}

// TestTodo_REV_093_01_Integration drives PreviewRoleAccess on the served
// composition (embedded PostgreSQL, the durable role store, the live journey
// engine's workforce): a saved assignment shows up as an explicit holder
// with its inherited role, a narrowing draft names the units it hides, the
// administrator override reports every unit on both sides, the preview
// saves nothing, and non-administrators are refused.
func TestTodo_REV_093_01_Integration(t *testing.T) {
	client, as := rev09301Serve(t)

	listed, err := client.ListWorkers(as("admin"), &journeyv1.ListWorkersRequest{})
	if err != nil {
		t.Fatalf("ListWorkers: %v", err)
	}
	seen := map[string]string{}
	var holder *journeyv1.Worker
	for _, worker := range listed.GetWorkers() {
		unit := strings.TrimSpace(worker.GetOrgUnit())
		if unit == "" {
			continue
		}
		if _, ok := seen[strings.ToLower(unit)]; !ok {
			seen[strings.ToLower(unit)] = unit
		}
		if holder == nil {
			holder = worker
		}
	}
	if holder == nil || len(seen) < 2 {
		t.Fatalf("served workforce has too few units to preview: %d units", len(seen))
	}
	allUnits := make([]string, 0, len(seen))
	for _, unit := range seen {
		allUnits = append(allUnits, unit)
	}
	sort.Slice(allUnits, func(i, j int) bool { return strings.ToLower(allUnits[i]) < strings.ToLower(allUnits[j]) })

	// The demo tenant seeds a durable role set for every worker, so this
	// holder already has one: an assignment write is a compare-and-swap and
	// has to name the version it is replacing.
	existing, err := client.GetRoleAccess(as("admin"), &journeyv1.GetRoleAccessRequest{})
	if err != nil {
		t.Fatalf("GetRoleAccess: %v", err)
	}
	holderVersion := int64(0)
	for _, assignment := range existing.GetAssignments() {
		if strings.EqualFold(strings.TrimSpace(assignment.GetWorkerRef()), strings.TrimSpace(holder.GetWorkerRef())) {
			holderVersion = assignment.GetVersion()
		}
	}
	if _, err := client.SaveWorkerRoleAssignment(as("admin"), &journeyv1.SaveWorkerRoleAssignmentRequest{Assignment: &journeyv1.WorkerRoleAssignment{Version: holderVersion, WorkerRef: holder.GetWorkerRef(), RoleIds: []string{"manager", "hr_partner"}}}); err != nil {
		t.Fatalf("SaveWorkerRoleAssignment: %v", err)
	}
	before, err := client.GetRoleAccess(as("admin"), &journeyv1.GetRoleAccessRequest{})
	if err != nil {
		t.Fatalf("GetRoleAccess: %v", err)
	}

	holderUnit := strings.TrimSpace(holder.GetOrgUnit())
	draft := &journeyv1.RoleOrganizationVisibilityPolicy{RoleId: "manager", Mode: "ALLOWLIST", OrganizationUnits: []string{holderUnit}}
	preview, err := client.PreviewRoleAccess(as("admin"), &journeyv1.PreviewRoleAccessRequest{Proposed: draft})
	if err != nil {
		t.Fatalf("PreviewRoleAccess(manager): %v", err)
	}
	if preview.GetRoleId() != "manager" || preview.GetAdministratorOverride() || preview.GetHolderCount() < 1 {
		t.Fatalf("manager preview = %+v", preview)
	}
	if !rev09301Contains(preview.GetInheritedRoles(), "hr_partner") || !reflect.DeepEqual(preview.GetExplicitRoles(), []string{"manager"}) {
		t.Fatalf("role sources = explicit %v inherited %v", preview.GetExplicitRoles(), preview.GetInheritedRoles())
	}
	if !reflect.DeepEqual(preview.GetProposed().GetOrganizationUnits(), []string{seen[strings.ToLower(holderUnit)]}) {
		t.Fatalf("proposed units = %v, want [%s]", preview.GetProposed().GetOrganizationUnits(), holderUnit)
	}
	for _, removed := range preview.GetRemovedUnits() {
		if !rev09301Contains(preview.GetCurrent().GetOrganizationUnits(), removed) || rev09301Contains(preview.GetProposed().GetOrganizationUnits(), removed) {
			t.Fatalf("removed unit %q is not a unit the draft hides", removed)
		}
	}
	for _, unit := range preview.GetCurrent().GetOrganizationUnits() {
		if !rev09301Contains(preview.GetProposed().GetOrganizationUnits(), unit) && !rev09301Contains(preview.GetRemovedUnits(), unit) {
			t.Fatalf("current unit %q silently dropped", unit)
		}
	}

	for _, role := range []string{"hcm_admin", "comp_admin"} {
		override, err := client.PreviewRoleAccess(as("admin"), &journeyv1.PreviewRoleAccessRequest{Proposed: &journeyv1.RoleOrganizationVisibilityPolicy{RoleId: role, Mode: "ALLOWLIST", OrganizationUnits: []string{holderUnit}}})
		if err != nil {
			t.Fatalf("PreviewRoleAccess(%s): %v", role, err)
		}
		if !override.GetAdministratorOverride() {
			t.Fatalf("%s preview hides the administrator override", role)
		}
		if !reflect.DeepEqual(override.GetCurrent().GetOrganizationUnits(), allUnits) || !reflect.DeepEqual(override.GetProposed().GetOrganizationUnits(), allUnits) {
			t.Fatalf("%s preview narrowed: %v -> %v, want every unit %v", role, override.GetCurrent().GetOrganizationUnits(), override.GetProposed().GetOrganizationUnits(), allUnits)
		}
		if len(override.GetRemovedUnits()) != 0 {
			t.Fatalf("%s preview reported hidden units %v", role, override.GetRemovedUnits())
		}
	}

	after, err := client.GetRoleAccess(as("admin"), &journeyv1.GetRoleAccessRequest{})
	if err != nil {
		t.Fatalf("GetRoleAccess after preview: %v", err)
	}
	if len(after.GetVisibilityPolicies()) != len(before.GetVisibilityPolicies()) {
		t.Fatalf("preview wrote visibility policies: %d -> %d", len(before.GetVisibilityPolicies()), len(after.GetVisibilityPolicies()))
	}
	for i, policy := range after.GetVisibilityPolicies() {
		if policy.GetVersion() != before.GetVisibilityPolicies()[i].GetVersion() || policy.GetMode() != before.GetVisibilityPolicies()[i].GetMode() {
			t.Fatalf("preview changed saved policy %s", policy.GetRoleId())
		}
	}

	for _, persona := range []string{"hiring-manager", "individual-contributor", "finance-partner"} {
		if _, err := client.PreviewRoleAccess(as(persona), &journeyv1.PreviewRoleAccessRequest{Proposed: draft}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("%s PreviewRoleAccess code = %v, want PermissionDenied", persona, status.Code(err))
		}
	}
}

func rev09301Contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
