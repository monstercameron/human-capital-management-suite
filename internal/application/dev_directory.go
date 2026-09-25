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
//
// It reads the default tenant's company; [devServedCompanies] lists every
// company a multi-tenant process serves.
func devSeededWorkforce(cfg ServeConfig) ([]demoworkforce.Employee, map[string]bool, bool) {
	pack, isDemo := demoworkforce.PackFor(cfg.Tenant)
	if !cfg.DevBrowserLogin || !isDemo {
		return nil, nil, false
	}
	return devSeededWorkforceFor(pack)
}

// devServedCompanies is every demo company this process serves, the default
// tenant's first. A served tenant that is not a shipped demo company has no
// personas and no directory, exactly as a lone non-demo tenant never did.
func devServedCompanies(cfg ServeConfig) []*demoworkforce.Pack {
	if !cfg.DevBrowserLogin {
		return nil
	}
	packs := make([]*demoworkforce.Pack, 0, 2)
	for _, tenant := range cfg.ServedTenants() {
		if pack, isDemo := demoworkforce.PackFor(tenant); isDemo {
			packs = append(packs, pack)
		}
	}
	return packs
}

// devSeededWorkforceFor resolves one company's plan and its managers.
func devSeededWorkforceFor(pack *demoworkforce.Pack) ([]demoworkforce.Employee, map[string]bool, bool) {
	workers, err := pack.Plan(pgstore.TenantID(pack.Key))
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
//
// The classification is the worker's own company's (Pack.BundleFor): for
// HarborCare, grades E6 and E7 are the executive band and the people
// operations and finance units are the functional partners; another company
// names its own executive and finance jobs.
func devEmployeeBundle(worker demoworkforce.Employee, managesAnybody bool) workspace.DevEmployeeBundle {
	pack, known := demoworkforce.PackForJob(worker.Row.JobCode)
	if !known {
		pack = demoworkforce.HarborCarePack
	}
	return workspace.DevEmployeeBundle(pack.BundleFor(worker, managesAnybody))
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
//
// A process serving several demo companies mints every company's employees,
// each credential bound to its own company's tenant.
func composeDevEmployeePersonas(verifier trust.Verifier, cfg ServeConfig, now func() time.Time) []workspace.DevPersona {
	issuer, isIssuer := verifier.(developmentTokenIssuer)
	if !isIssuer {
		return nil
	}
	if now == nil {
		now = time.Now
	}
	var personas []workspace.DevPersona
	for _, pack := range devServedCompanies(cfg) {
		personas = append(personas, composeCompanyEmployeePersonas(issuer, cfg, pack, now().UTC())...)
	}
	return personas
}

// composeCompanyEmployeePersonas mints one company's employee credentials.
func composeCompanyEmployeePersonas(issuer developmentTokenIssuer, cfg ServeConfig, pack *demoworkforce.Pack, timestamp time.Time) []workspace.DevPersona {
	workers, manages, ok := devSeededWorkforceFor(pack)
	if !ok {
		return nil
	}
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
			Issuer: cfg.Issuer, Audience: cfg.Audience, Subject: worker.Row.WorkerKey, SubjectKind: "human", Tenant: pack.Key,
			OrganizationScopeID:  "org:" + pack.Key + ":" + worker.Organization.Code,
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
			Roles: access.Roles, Token: token, WorkerRef: worker.Row.WorkerKey, Company: pack.Key,
		})
	}
	return personas
}

// devDirectorySnapshot is the immutable directory one composition produced.
//
// snapshot is the default company's; companies holds every served
// company's, keyed by tenant, for the sign-in page's company selector.
type devDirectorySnapshot struct {
	snapshot  workspace.DevDirectorySnapshot
	companies map[string]workspace.DevDirectorySnapshot
	offered   []workspace.DevCompany
}

// DevCompanies implements workspace.DevCompanyDirectory.
func (directory devDirectorySnapshot) DevCompanies() []workspace.DevCompany {
	return append([]workspace.DevCompany(nil), directory.offered...)
}

// DevDirectorySnapshot implements workspace.DevDirectory. It hands back
// copies so a renderer cannot reach back into the composed value.
func (directory devDirectorySnapshot) DevDirectorySnapshot() workspace.DevDirectorySnapshot {
	return copyDevDirectorySnapshot(directory.snapshot)
}

// DevCompanyDirectorySnapshot implements workspace.DevCompanyDirectory.
func (directory devDirectorySnapshot) DevCompanyDirectorySnapshot(company string) (workspace.DevDirectorySnapshot, bool) {
	snapshot, ok := directory.companies[company]
	if !ok {
		return workspace.DevDirectorySnapshot{}, false
	}
	return copyDevDirectorySnapshot(snapshot), true
}

func copyDevDirectorySnapshot(snapshot workspace.DevDirectorySnapshot) workspace.DevDirectorySnapshot {
	return workspace.DevDirectorySnapshot{
		Units:     append([]workspace.DevDirectoryUnit(nil), snapshot.Units...),
		Employees: append([]workspace.DevDirectoryEmployee(nil), snapshot.Employees...),
	}
}

// composeDevDirectory builds the directory the dev sign-in page renders, or
// nil. It carries only facts already public in the seed plan - name, worker
// number, job title, org unit and manager - and never a credential: the
// sign-in page selects a token by opaque id from the server-owned persona
// collection, and nothing here is capable of disclosing one.
func composeDevDirectory(cfg ServeConfig) workspace.DevDirectory {
	if _, _, ok := devSeededWorkforce(cfg); !ok {
		return nil
	}
	directory := devDirectorySnapshot{companies: map[string]workspace.DevDirectorySnapshot{}, offered: composeDevCompanies(cfg)}
	for _, pack := range devServedCompanies(cfg) {
		snapshot, ok := companyDevDirectory(pack)
		if !ok {
			continue
		}
		directory.companies[pack.Key] = snapshot
		if pack.Key == cfg.Tenant {
			directory.snapshot = snapshot
		}
	}
	return directory
}

// companyDevDirectory is one company's org chart and employee list.
func companyDevDirectory(pack *demoworkforce.Pack) (workspace.DevDirectorySnapshot, bool) {
	workers, _, ok := devSeededWorkforceFor(pack)
	if !ok {
		return workspace.DevDirectorySnapshot{}, false
	}
	snapshot := workspace.DevDirectorySnapshot{
		Units:     make([]workspace.DevDirectoryUnit, 0, len(pack.Company.Units)),
		Employees: make([]workspace.DevDirectoryEmployee, 0, len(workers)),
	}
	for _, unit := range pack.Company.Units {
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
	return snapshot, true
}

// composeDevCompanies lists the demo companies the sign-in page's company
// selector offers, the default tenant's first.
func composeDevCompanies(cfg ServeConfig) []workspace.DevCompany {
	packs := devServedCompanies(cfg)
	companies := make([]workspace.DevCompany, 0, len(packs))
	for _, pack := range packs {
		companies = append(companies, workspace.DevCompany{
			Key: pack.Key, Name: pack.Company.Name, ShortName: pack.DisplayName,
			Description: pack.Tagline, Headcount: pack.WorkerCount, Logo: pack.LogoAsset,
		})
	}
	return companies
}
