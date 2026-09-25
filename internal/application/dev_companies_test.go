package application

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func twoCompanyConfig() ServeConfig {
	cfg := devDirectoryConfig()
	cfg.Tenants = demoworkforce.HarborCarePack.Key + "," + demoworkforce.IronridgePack.Key
	return cfg
}

func TestServedTenantsListsTheDefaultFirstAndEachOnce(t *testing.T) {
	cfg := ServeConfig{Tenant: "harborcare-demo", Tenants: " ironridge-demo , harborcare-demo,,ironridge-demo"}
	if got := strings.Join(cfg.ServedTenants(), ","); got != "harborcare-demo,ironridge-demo" {
		t.Fatalf("ServedTenants = %s", got)
	}
	if got := (ServeConfig{Tenant: "harborcare-demo"}).ServedTenants(); len(got) != 1 {
		t.Fatalf("a single-tenant process serves %v", got)
	}
	if len((ServeConfig{}).ServedTenants()) != 0 {
		t.Fatal("an unconfigured process serves a tenant")
	}
}

// TestDevPersonaCatalogIsPerCompany is the persona catalog per company: each
// company gets its own four quick picks, bound to its own people, and the
// default company's keep their bare ids.
func TestDevPersonaCatalogIsPerCompany(t *testing.T) {
	now := time.Date(2026, 9, 6, 16, 0, 0, 0, time.UTC)
	cfg := twoCompanyConfig()
	verifier := devDirectoryVerifier(t, cfg, now)
	personas := composeDevPersonas(verifier, cfg, func() time.Time { return now })
	if len(personas) != 8 {
		t.Fatalf("quick picks = %d, want four per company", len(personas))
	}
	want := map[string]string{
		"admin": "hc-050-rafael-torres", "finance-partner": "hc-054-thomas-baker",
		"ironridge-demo:admin": "ir-001-walt-brennan", "ironridge-demo:hiring-manager": "ir-008-curtis-bell",
		"ironridge-demo:finance-partner": "ir-003-loretta-haynes", "ironridge-demo:individual-contributor": "ir-013-ana-flores",
	}
	byID := map[string]workspace.DevPersona{}
	for _, persona := range personas {
		byID[persona.ID] = persona
	}
	for id, worker := range want {
		persona, ok := byID[id]
		if !ok || persona.WorkerRef != worker {
			t.Errorf("persona %s = %+v, want worker %s", id, persona, worker)
		}
	}
	// A credential is minted for the chosen company's tenant, scoped to that
	// company's organization.
	for _, persona := range personas {
		principal, err := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: persona.Token, Audience: cfg.Audience})
		if err != nil {
			t.Fatalf("verify %s: %v", persona.ID, err)
		}
		pack, _ := demoworkforce.PackFor(persona.Company)
		if string(principal.Tenant()) != persona.Company || principal.OrganizationScopeID() != pack.OrgScope() || principal.Subject() != persona.WorkerRef {
			t.Errorf("%s minted tenant %s scope %s subject %s", persona.ID, principal.Tenant(), principal.OrganizationScopeID(), principal.Subject())
		}
		roles, _ := workspace.DevPersonaRoles(persona.Slot)
		got := append([]string(nil), principal.Roles()...)
		sort.Strings(got)
		sort.Strings(roles)
		if strings.Join(got, ",") != strings.Join(roles, ",") {
			t.Errorf("%s roles %v, want the %s slot's %v", persona.ID, principal.Roles(), persona.Slot, roles)
		}
	}
	// A single-company process is unchanged: HarborCare's four, bare ids.
	single := composeDevPersonas(verifier, devDirectoryConfig(), func() time.Time { return now })
	if len(single) != 4 || single[0].ID != "admin" || single[0].Company != "harborcare-demo" {
		t.Fatalf("single-company quick picks = %+v", single)
	}
}

func TestDevEmployeeCredentialsAndDirectoryAreSeparatedByCompany(t *testing.T) {
	now := time.Date(2026, 9, 6, 16, 0, 0, 0, time.UTC)
	cfg := twoCompanyConfig()
	verifier := devDirectoryVerifier(t, cfg, now)
	employees := composeDevEmployeePersonas(verifier, cfg, func() time.Time { return now })
	if len(employees) != 60+38 {
		t.Fatalf("employee credentials = %d, want 98", len(employees))
	}
	for _, persona := range employees {
		principal, err := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: persona.Token, Audience: cfg.Audience})
		if err != nil {
			t.Fatal(err)
		}
		prefix := map[string]string{"harborcare-demo": "hc-", "ironridge-demo": "ir-"}[string(principal.Tenant())]
		if prefix == "" || !strings.HasPrefix(persona.WorkerRef, prefix) || persona.Company != string(principal.Tenant()) {
			t.Fatalf("%s is minted for tenant %s", persona.WorkerRef, principal.Tenant())
		}
	}
	directory, ok := composeDevDirectory(cfg).(workspace.DevCompanyDirectory)
	if !ok {
		t.Fatal("a two-company process composed no company directory")
	}
	companies := directory.DevCompanies()
	if len(companies) != 2 || companies[0].Key != "harborcare-demo" || companies[1].Name != "Ironridge Builders" || companies[1].Headcount != 38 {
		t.Fatalf("companies = %+v", companies)
	}
	iron, found := directory.DevCompanyDirectorySnapshot("ironridge-demo")
	if !found || len(iron.Employees) != 38 {
		t.Fatalf("Ironridge directory = %d employees", len(iron.Employees))
	}
	for _, employee := range iron.Employees {
		if !strings.HasPrefix(employee.WorkerKey, "ir-") {
			t.Fatalf("the Ironridge directory lists %s", employee.WorkerKey)
		}
	}
	if _, found := directory.DevCompanyDirectorySnapshot("another-tenant"); found {
		t.Fatal("an unserved company answered")
	}
}

// TestIronridgeThemeIsAdmitted holds the seeded look to the product's own
// admission: the custom palette's colors are customer tokens, and both light
// and dark pass the contrast qualification.
func TestIronridgeThemeIsAdmitted(t *testing.T) {
	theme := demoworkforce.IronridgePack.Theme
	candidate := productui.CustomerTheme{
		BrandName: theme.BrandName, BrandMark: theme.BrandMark, ColorMode: theme.ColorMode, Palette: theme.Palette,
		Shape: theme.Shape, Density: theme.Density, Glyphs: theme.Glyphs, Typeface: theme.Typeface,
		Navigation: theme.Navigation, Motion: theme.Motion,
		TokenOverrides: theme.TokenOverrides, DarkTokenOverrides: theme.DarkTokenOverrides,
	}
	if err := productui.ValidateCustomerTheme(candidate); err != nil {
		t.Fatalf("Ironridge theme refused: %v", err)
	}
	if _, err := productui.ResolveCustomerThemeModes(candidate); err != nil {
		t.Fatalf("Ironridge theme does not resolve: %v", err)
	}
	normalized := productui.NormalizeCustomerTheme(candidate)
	if normalized.Palette != "custom" || normalized.Typeface != "modern" || normalized.Shape != "precise" || normalized.BrandName != "Ironridge Builders" {
		t.Fatalf("normalization discarded the brand: %+v", normalized)
	}
	scopes := packOrganizationScopes(demoworkforce.IronridgePack)
	if scopes[0] != "org:ironridge-demo:people" || len(scopes) != len(demoworkforce.IronridgePack.Company.Units) {
		t.Fatalf("branded scopes = %v", scopes)
	}
}

func TestServedCompanyApproversRouteOnlyAdditionalCompanies(t *testing.T) {
	finance, manager := servedCompanyApprovers(twoCompanyConfig(), pgstore.TenantID)
	iron := pgstore.TenantID("ironridge-demo")
	if len(finance) != 1 || finance[iron] != "ir-003-loretta-haynes" || manager[iron] != "ir-002-marcus-whitfield" {
		t.Fatalf("approvers = %v %v", finance, manager)
	}
	if f, _ := servedCompanyApprovers(devDirectoryConfig(), pgstore.TenantID); f != nil {
		t.Fatal("a single-company process routes per tenant")
	}
	if localDevPack("ironridge-demo") != demoworkforce.IronridgePack || localDevPack("x") != demoworkforce.HarborCarePack {
		t.Fatal("local-dev approver defaults do not follow -tenant")
	}
	if LocalDevFinancePartner != demoworkforce.HarborCarePack.FinancePartnerKey {
		t.Fatal("the local-dev finance partner drifted from HarborCare's pack")
	}
	defaults := map[string]string{}
	for _, field := range ServeConfigFieldsForArgs([]string{"-profile=local-dev", "-tenant", "ironridge-demo"}) {
		defaults[field.Name] = field.Default
	}
	if defaults[FieldExecutionFinancePartner] != "ir-003-loretta-haynes" || defaults[FieldExecutionManagerApprover] != "ir-002-marcus-whitfield" {
		t.Fatalf("Ironridge local-dev approver defaults = %s / %s", defaults[FieldExecutionFinancePartner], defaults[FieldExecutionManagerApprover])
	}
}
