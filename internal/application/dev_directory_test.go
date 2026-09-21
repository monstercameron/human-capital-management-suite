package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

func devDirectoryConfig() ServeConfig {
	return ServeConfig{
		DevBrowserLogin: true, DevHMACKey: testDevKey,
		Issuer: DefaultIssuer, Audience: DefaultAudience, Tenant: LocalDevTenant,
	}
}

func devDirectoryVerifier(t *testing.T, cfg ServeConfig, now time.Time) trust.Verifier {
	t.Helper()
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key: []byte(cfg.DevHMACKey), Issuer: cfg.Issuer, Audience: cfg.Audience,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return verifier
}

func TestComposeDevEmployeePersonasMakesEverySeededEmployeeSignable(t *testing.T) {
	now := time.Date(2026, 9, 6, 16, 0, 0, 0, time.UTC)
	cfg := devDirectoryConfig()
	verifier := devDirectoryVerifier(t, cfg, now)
	personas := composeDevEmployeePersonas(verifier, cfg, func() time.Time { return now })

	planned, err := demoworkforce.Plan(pgstore.TenantID(cfg.Tenant))
	if err != nil {
		t.Fatal(err)
	}
	active := 0
	for _, worker := range planned {
		if worker.Row.LifecycleStatus == "active" {
			active++
		}
	}
	if active == 0 || len(personas) != active {
		t.Fatalf("personas = %d, want one per active seeded employee (%d)", len(personas), active)
	}

	// The four canonical quick-pick ids keep their own space: composeDevPersonas
	// still returns exactly four, and no employee entry can shadow one.
	canonical := composeDevPersonas(verifier, cfg, func() time.Time { return now })
	if len(canonical) != 4 {
		t.Fatalf("canonical personas = %d, want the four quick picks untouched", len(canonical))
	}
	ids := map[string]bool{}
	for _, persona := range canonical {
		ids[persona.ID] = true
	}

	subjects := map[string]bool{}
	for _, persona := range personas {
		if !workspace.IsDevEmployeePersonaID(persona.ID) {
			t.Errorf("employee persona %q is outside the employee id space", persona.ID)
		}
		if ids[persona.ID] {
			t.Errorf("employee persona %q collides with a quick-pick id", persona.ID)
		}
		ids[persona.ID] = true
		principal, verifyErr := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: persona.Token, Audience: cfg.Audience})
		if verifyErr != nil {
			t.Fatalf("verify %s: %v", persona.ID, verifyErr)
		}
		// The binding serveLoginSubmit re-checks before resolving any access.
		if persona.WorkerRef != principal.Subject() {
			t.Errorf("%s worker binding = %q, want the verified subject %q", persona.ID, persona.WorkerRef, principal.Subject())
		}
		if persona.ID != workspace.DevEmployeePersonaID(principal.Subject()) {
			t.Errorf("%s id is not derived from its own subject %q", persona.ID, principal.Subject())
		}
		if subjects[principal.Subject()] {
			t.Errorf("duplicate subject %q", principal.Subject())
		}
		subjects[principal.Subject()] = true
		if strings.TrimSpace(persona.Access) == "" || len(persona.Roles) == 0 {
			t.Errorf("%s carries no access label or no roles", persona.ID)
		}
	}
	if len(ids) != len(personas)+len(canonical) {
		t.Fatalf("distinct persona ids = %d, want %d", len(ids), len(personas)+len(canonical))
	}
}

// TestComposeDevEmployeePersonasPurposesAreBackedByPolicy is the sixty-fold
// version of UXAUDIT-014's assertion: a purpose a credential declares, and any
// capability its access label's own wording implies, must be granted to the
// role bundle that credential actually carries by the live P1A policy table -
// not by a hand-written expectation of it.
func TestComposeDevEmployeePersonasPurposesAreBackedByPolicy(t *testing.T) {
	now := time.Date(2026, 9, 6, 16, 0, 0, 0, time.UTC)
	cfg := devDirectoryConfig()
	verifier := devDirectoryVerifier(t, cfg, now)
	personas := composeDevEmployeePersonas(verifier, cfg, func() time.Time { return now })
	if len(personas) == 0 {
		t.Fatal("no employee personas composed")
	}
	knownPurposes := []string{
		authz.PurposeSelfService, authz.PurposeCompensationReview, authz.PurposePayrollProcessing,
		authz.PurposePerformanceReview, authz.PurposeAccommodationCase, authz.PurposeCaseManagement,
		authz.PurposeImmigrationCase, authz.PurposeAuditReview,
	}
	for _, persona := range personas {
		principal, err := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: persona.Token, Audience: cfg.Audience})
		if err != nil {
			t.Fatalf("verify %s: %v", persona.ID, err)
		}
		purposes := principal.Purposes()
		if len(purposes) == 0 {
			t.Errorf("%s declares no purpose", persona.ID)
		}
		for _, purpose := range purposes {
			if !purposeBackedByRoles(persona.Roles, purpose) {
				t.Errorf("%s: signed purpose %q is not granted to role bundle %v by any P1A domain rule", persona.ID, purpose, persona.Roles)
			}
		}
		access := strings.ToLower(persona.Access)
		for _, purpose := range knownPurposes {
			keyword, _, cut := strings.Cut(purpose, "_")
			if !cut || !strings.Contains(access, keyword) {
				continue
			}
			if !purposeBackedByRoles(persona.Roles, purpose) {
				t.Errorf("%s: access label %q implies purpose %q that role bundle %v does not back", persona.ID, persona.Access, purpose, persona.Roles)
			}
		}
	}
}

func TestDevEmployeeBundleFollowsTheJobNotSeniority(t *testing.T) {
	cfg := devDirectoryConfig()
	workers, manages, ok := devSeededWorkforce(cfg)
	if !ok {
		t.Fatal("the seeded workforce did not resolve for the demo tenant")
	}
	byNumber := map[string]demoworkforce.Employee{}
	for _, worker := range workers {
		byNumber[worker.Row.WorkerNumber] = worker
	}
	// Spot checks against the seed's own records, so a re-numbered plan fails
	// here rather than silently changing who can do what.
	for number, want := range map[string]workspace.DevEmployeeBundle{
		"HC-21001": workspace.DevEmployeeBundleExecutive, // Chief Executive Officer
		"HC-21050": workspace.DevEmployeeBundlePeopleOps, // Director of People Operations
		"HC-21054": workspace.DevEmployeeBundleFinance,   // Finance Director
	} {
		worker, found := byNumber[number]
		if !found {
			t.Fatalf("worker %s is not in the seed plan", number)
		}
		if got := devEmployeeBundle(worker, manages[worker.Row.WorkerKey]); got != want {
			t.Errorf("%s (%s, %s) classified as %q, want %q", number, worker.JobTitle, worker.Organization.Code, got, want)
		}
	}

	counts := map[workspace.DevEmployeeBundle]int{}
	for _, worker := range workers {
		bundle := devEmployeeBundle(worker, manages[worker.Row.WorkerKey])
		counts[bundle]++
		access, resolved := workspace.DevEmployeeBundleAccess(bundle)
		if !resolved {
			t.Fatalf("%s classified into the unresolvable bundle %q", worker.Row.WorkerNumber, bundle)
		}
		switch bundle {
		case workspace.DevEmployeeBundleExecutive:
			if !strings.HasPrefix(worker.Row.Grade, "E") {
				t.Errorf("%s is an executive without an executive grade (%q)", worker.Row.WorkerNumber, worker.Row.Grade)
			}
		case workspace.DevEmployeeBundlePeopleOps:
			if worker.Organization.Code != "people-operations" {
				t.Errorf("%s got the HR bundle from outside people operations (%q)", worker.Row.WorkerNumber, worker.Organization.Code)
			}
		case workspace.DevEmployeeBundleFinance:
			if worker.Organization.Code != "finance" {
				t.Errorf("%s got the finance bundle from outside finance (%q)", worker.Row.WorkerNumber, worker.Organization.Code)
			}
		case workspace.DevEmployeeBundleManager:
			if !manages[worker.Row.WorkerKey] {
				t.Errorf("%s got the manager bundle without managing anybody", worker.Row.WorkerNumber)
			}
		case workspace.DevEmployeeBundleSelf:
			if manages[worker.Row.WorkerKey] {
				t.Errorf("%s manages people but was narrowed to self-service", worker.Row.WorkerNumber)
			}
			if len(access.Roles) != 1 || access.Roles[0] != "worker_self" {
				t.Errorf("the self bundle carries %v", access.Roles)
			}
		}
	}
	// Every bundle must be reachable from this plan, or a bundle nobody holds
	// is untested policy shipping as if it were exercised.
	for _, bundle := range []workspace.DevEmployeeBundle{
		workspace.DevEmployeeBundleExecutive, workspace.DevEmployeeBundlePeopleOps,
		workspace.DevEmployeeBundleFinance, workspace.DevEmployeeBundleManager,
		workspace.DevEmployeeBundleSelf,
	} {
		if counts[bundle] == 0 {
			t.Errorf("no seeded employee resolves to the %q bundle", bundle)
		}
	}
	// A manager who is also a finance or people leader keeps the narrow
	// bundle: seniority must never silently widen reach.
	for _, worker := range workers {
		if !manages[worker.Row.WorkerKey] {
			continue
		}
		switch worker.Organization.Code {
		case "finance", "people-operations":
			if got := devEmployeeBundle(worker, true); got == workspace.DevEmployeeBundleManager {
				t.Errorf("%s was widened to the manager bundle because they have reports", worker.Row.WorkerNumber)
			}
		}
	}
}

func TestComposeDevDirectoryCarriesPublicFactsAndNoCredential(t *testing.T) {
	now := time.Date(2026, 9, 6, 16, 0, 0, 0, time.UTC)
	cfg := devDirectoryConfig()
	directory := composeDevDirectory(cfg)
	if directory == nil {
		t.Fatal("no directory composed for the demo tenant with dev login on")
	}
	snapshot := directory.DevDirectorySnapshot()
	if len(snapshot.Units) != len(demoworkforce.HarborCare.Units) {
		t.Fatalf("units = %d, want %d", len(snapshot.Units), len(demoworkforce.HarborCare.Units))
	}
	personas := composeDevEmployeePersonas(devDirectoryVerifier(t, cfg, now), cfg, func() time.Time { return now })
	if len(snapshot.Employees) != len(personas) {
		t.Fatalf("directory rows = %d, credentials = %d; a row without a credential offers a dead control", len(snapshot.Employees), len(personas))
	}
	held := map[string]workspace.DevPersona{}
	for _, persona := range personas {
		held[persona.ID] = persona
	}
	units := map[string]bool{}
	for _, unit := range snapshot.Units {
		units[unit.Code] = true
	}
	roots := 0
	for _, unit := range snapshot.Units {
		if unit.ParentCode == "" {
			roots++
			continue
		}
		if !units[unit.ParentCode] {
			t.Errorf("unit %q names the unknown parent %q", unit.Code, unit.ParentCode)
		}
	}
	if roots != 1 {
		t.Fatalf("organization has %d roots, want exactly one", roots)
	}
	workerKeys := map[string]bool{}
	for _, employee := range snapshot.Employees {
		workerKeys[employee.WorkerKey] = true
	}
	for _, employee := range snapshot.Employees {
		persona, ok := held[employee.PersonaID]
		if !ok {
			t.Errorf("directory row %q has no server-held credential", employee.PersonaID)
			continue
		}
		if persona.WorkerRef != employee.WorkerKey {
			t.Errorf("row %q is bound to %q but its credential is bound to %q", employee.PersonaID, employee.WorkerKey, persona.WorkerRef)
		}
		for name, value := range map[string]string{
			"name": employee.Name, "worker number": employee.WorkerNumber,
			"job title": employee.JobTitle, "org unit": employee.UnitCode,
		} {
			if strings.TrimSpace(value) == "" {
				t.Errorf("row %q has no %s, so a tester cannot tell them apart", employee.PersonaID, name)
			}
		}
		if !units[employee.UnitCode] {
			t.Errorf("row %q sits in the unknown unit %q", employee.PersonaID, employee.UnitCode)
		}
		// The manager is either a seeded worker or the seed's board sentinel
		// for the chief executive; never a dangling reference.
		if employee.ManagerKey != "" && !workerKeys[employee.ManagerKey] && !strings.HasPrefix(employee.ManagerKey, "board:") {
			t.Errorf("row %q names the unknown manager %q", employee.PersonaID, employee.ManagerKey)
		}
	}

	// The snapshot hands back copies: a renderer cannot reach into the value
	// the composition holds.
	snapshot.Employees[0].Name = "mutated"
	snapshot.Units[0].Name = "mutated"
	fresh := directory.DevDirectorySnapshot()
	if fresh.Employees[0].Name == "mutated" || fresh.Units[0].Name == "mutated" {
		t.Fatal("the composed directory is aliased into every render")
	}
}

func TestComposeDevDirectoryAndEmployeePersonasStayOffOutsideLocalDev(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 9, 6, 16, 0, 0, 0, time.UTC) }
	base := devDirectoryConfig()
	verifier := devDirectoryVerifier(t, base, now())

	off := base
	off.DevBrowserLogin = false
	foreign := base
	foreign.Tenant = "another-tenant"
	for name, cfg := range map[string]ServeConfig{
		"dev browser login off": off,
		"another tenant":        foreign,
		"zero config":           {},
	} {
		if got := composeDevDirectory(cfg); got != nil {
			t.Errorf("%s composed a directory", name)
		}
		if got := composeDevEmployeePersonas(verifier, cfg, now); len(got) != 0 {
			t.Errorf("%s composed %d employee credentials", name, len(got))
		}
	}
	// A verifier that cannot issue development tokens never acquires an
	// implicit issuer, exactly as composeDevPersonas refuses one.
	if got := composeDevEmployeePersonas(nonIssuingVerifier{}, base, now); len(got) != 0 {
		t.Fatalf("a non-issuing verifier produced %d credentials", len(got))
	}
	if directory := composeDevDirectory(base); directory == nil {
		t.Fatal("the enabled demo configuration composed no directory")
	}
}

// nonIssuingVerifier is a trust.Verifier with no Issue method, the shape a
// federation verifier has.
type nonIssuingVerifier struct{}

func (nonIssuingVerifier) Verify(context.Context, trust.Credential) (*trust.Principal, error) {
	return nil, nil
}
