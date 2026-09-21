package application

import (
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// This file composes the dev-only employee directory and the per-employee
// credentials behind it. It is deliberately additive to composeDevPersonas
// (serve.go), which still mints exactly the four canonical quick-pick
// personas and nothing else: those four ids, their bound workers and their
// role bundles are a fixture the promotion reference workflow depends on, and
// widening that function would have made the demo's separated-approver story
// a side effect of a directory feature.
//
// Every gate composeDevPersonas applies applies here unchanged: the operator
// must have enabled the dev browser login, the tenant must be the demo
// tenant, and the configured verifier must be able to issue development
// tokens. A federation verifier therefore never acquires an implicit issuer,
// and no other tenant is ever offered a HarborCare worker.

// devSeededWorkforce resolves the demo plan and the set of workers who manage
// somebody. Both the directory and the credentials derive from this one call,
// so a row a tester can see and the bundle they would sign in with cannot
// come from two different readings of the plan.
func devSeededWorkforce(cfg ServeConfig) ([]demoworkforce.Employee, map[string]bool, bool) {
	if !cfg.DevBrowserLogin || cfg.Tenant != demoworkforce.CompanyKey {
		return nil, nil, false
	}
	workers, err := demoworkforce.Plan(pgstore.TenantID(cfg.Tenant))
	if err != nil {
		return nil, nil, false
	}
	manages := make(map[string]bool, len(workers))
	for _, worker := range workers {
		manager := strings.TrimSpace(worker.ManagerKey)
		if manager == "" || manager == worker.Row.WorkerKey {
			continue
		}
		manages[manager] = true
	}
	return workers, manages, true
}

// devEmployeeBundle classifies one seeded employee into the role bundle their
// job justifies. The classification lives here, not in the workspace package,
// because it is a fact about this demo plan; the workspace package owns only
// what each bundle grants (workspace.DevEmployeeBundleAccess).
//
// Order matters and is the point: an executive who also manages people is an
// executive, and a finance or people-operations leader keeps the narrow
// partner bundle their function names rather than being widened to a manager
// because they happen to have reports. Seniority never silently adds reach.
func devEmployeeBundle(worker demoworkforce.Employee, managesAnybody bool) workspace.DevEmployeeBundle {
	// Grades E6 and E7 are the seed's executive band (chief officers and the
	// general counsel); M and P grades are management and professional.
	if strings.HasPrefix(worker.Row.Grade, "E") {
		return workspace.DevEmployeeBundleExecutive
	}
	switch worker.Organization.Code {
	case "people-operations":
		return workspace.DevEmployeeBundlePeopleOps
	case "finance":
		return workspace.DevEmployeeBundleFinance
	}
	if managesAnybody {
		return workspace.DevEmployeeBundleManager
	}
	return workspace.DevEmployeeBundleSelf
}

// composeDevEmployeePersonas mints one server-held credential per active
// seeded employee, in plan order.
//
// Every security invariant the four canonical personas hold, these hold too:
// the credential's subject is the worker it is bound to (WorkerRef ==
// Subject, which serveLoginSubmit re-checks against the verified principal
// before any access is resolved), the declared purpose is one the bundle's
// roles are granted by the live P1A policy, and the token itself never leaves
// the server - the page carries only the opaque persona id.
func composeDevEmployeePersonas(verifier trust.Verifier, cfg ServeConfig, now func() time.Time) []workspace.DevPersona {
	workers, manages, ok := devSeededWorkforce(cfg)
	if !ok {
		return nil
	}
	issuer, isIssuer := verifier.(developmentTokenIssuer)
	if !isIssuer {
		return nil
	}
	if now == nil {
		now = time.Now
	}
	timestamp := now().UTC()
	personas := make([]workspace.DevPersona, 0, len(workers))
	for _, worker := range workers {
		if worker.Row.WorkerKey == "" || worker.Row.LegalName == "" || worker.Row.LifecycleStatus != "active" {
			continue
		}
		access, resolved := workspace.DevEmployeeBundleAccess(devEmployeeBundle(worker, manages[worker.Row.WorkerKey]))
		if !resolved {
			// No canonical bundle answers this classification: issuing an
			// unscoped credential would be worse than not offering the
			// employee at all (the same rule composeDevPersonas applies).
			continue
		}
		id := workspace.DevEmployeePersonaID(worker.Row.WorkerKey)
		token, err := issuer.Issue(trust.Claims{
			Issuer: cfg.Issuer, Audience: cfg.Audience, Subject: worker.Row.WorkerKey, SubjectKind: "human", Tenant: cfg.Tenant,
			OrganizationScopeID:  "org:" + cfg.Tenant + ":" + worker.Organization.Code,
			Roles:                access.Roles,
			Purposes:             []string{access.Purpose},
			AuthenticationMethod: "bearer_token", Assurance: "substantial", SessionRef: "session-local-employee-" + worker.Row.WorkerKey,
			IssuedAtUnix: timestamp.Add(-time.Minute).Unix(), ExpiresAtUnix: timestamp.Add(8 * time.Hour).Unix(),
		})
		if err != nil {
			continue
		}
		personas = append(personas, workspace.DevPersona{
			ID: id, Name: worker.Row.LegalName, Access: access.Label,
			Roles: access.Roles, Token: token, WorkerRef: worker.Row.WorkerKey,
		})
	}
	return personas
}

// devDirectorySnapshot is the immutable directory one composition produced.
type devDirectorySnapshot struct {
	snapshot workspace.DevDirectorySnapshot
}

// DevDirectorySnapshot implements workspace.DevDirectory. It hands back
// copies so a renderer cannot reach back into the composed value.
func (directory devDirectorySnapshot) DevDirectorySnapshot() workspace.DevDirectorySnapshot {
	return workspace.DevDirectorySnapshot{
		Units:     append([]workspace.DevDirectoryUnit(nil), directory.snapshot.Units...),
		Employees: append([]workspace.DevDirectoryEmployee(nil), directory.snapshot.Employees...),
	}
}

// composeDevDirectory builds the directory the dev sign-in page renders, or
// nil. It carries only facts already public in the seed plan - name, worker
// number, job title, org unit and manager - and never a credential: the
// sign-in page selects a token by opaque id from the server-owned persona
// collection, and nothing here is capable of disclosing one.
func composeDevDirectory(cfg ServeConfig) workspace.DevDirectory {
	workers, _, ok := devSeededWorkforce(cfg)
	if !ok {
		return nil
	}
	snapshot := workspace.DevDirectorySnapshot{
		Units:     make([]workspace.DevDirectoryUnit, 0, len(demoworkforce.HarborCare.Units)),
		Employees: make([]workspace.DevDirectoryEmployee, 0, len(workers)),
	}
	for _, unit := range demoworkforce.HarborCare.Units {
		snapshot.Units = append(snapshot.Units, workspace.DevDirectoryUnit{
			Code: unit.Code, Name: unit.Name, ParentCode: unit.ParentCode,
		})
	}
	for _, worker := range workers {
		if worker.Row.WorkerKey == "" || worker.Row.LifecycleStatus != "active" {
			continue
		}
		snapshot.Employees = append(snapshot.Employees, workspace.DevDirectoryEmployee{
			PersonaID:    workspace.DevEmployeePersonaID(worker.Row.WorkerKey),
			WorkerKey:    worker.Row.WorkerKey,
			Name:         worker.Row.LegalName,
			WorkerNumber: worker.Row.WorkerNumber,
			JobTitle:     worker.JobTitle,
			UnitCode:     worker.Organization.Code,
			ManagerKey:   worker.ManagerKey,
		})
	}
	return devDirectorySnapshot{snapshot: snapshot}
}
