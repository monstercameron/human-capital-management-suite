package agentaccess

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentconnect"
)

// The console answers what one population's agents could do: the skills a
// grant gives that population, each with the roles and organization scopes
// that still gate it per member, and the same warnings a person preview
// carries. Nothing is changed.
func TestTodo_AGENT2_019_PopulationPreview(t *testing.T) {
	console, _ := newConsoleFixture(t)
	draft := readDraft()
	draft.Grants = []agentconnect.GrantScope{
		managersGrant("workers.read", "promotions.submit"),
		{ID: "contractors", Roles: []string{"contractor"}, Population: "contractors", OrganizationScopes: []string{"org-b"}, Skills: []string{"workers.read"}},
		{ID: "everyone-read", Roles: []string{agentconnect.AnyScope}, Population: agentconnect.AnyScope, OrganizationScopes: []string{"org-a"}, Skills: []string{"workers.read"}},
	}
	created, err := console.Create("tenant-a", "admin-one", draft)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := console.PreviewPopulation("tenant-a", "admin-one", created.ID(), "contractors")
	if err != nil {
		t.Fatal(err)
	}
	if preview.Population != "contractors" {
		t.Fatalf("population = %q", preview.Population)
	}
	if len(preview.Skills) != 1 || preview.Skills[0].ID != "workers.read" || preview.Skills[0].Tier != agentconnect.TierT0 {
		t.Fatalf("skills = %+v, want only the read skill members of contractors can reach", preview.Skills)
	}
	got := preview.Skills[0]
	if strings.Join(got.Roles, ",") != "*,contractor" || strings.Join(got.OrganizationScopes, ",") != "org-a,org-b" {
		t.Fatalf("gating = roles %v scopes %v, want the union of the matching grants", got.Roles, got.OrganizationScopes)
	}
	// The T3 skill reaches managers only: contractors are not shown it.
	managers, err := console.PreviewPopulation("tenant-a", "admin-one", created.ID(), "employees")
	if err != nil || len(managers.Skills) != 2 {
		t.Fatalf("managers preview = %+v, %v", managers, err)
	}
	// An unknown population matches nothing but the wildcard grant.
	strangers, err := console.PreviewPopulation("tenant-a", "admin-one", created.ID(), "partners")
	if err != nil || len(strangers.Skills) != 1 || strangers.Skills[0].ID != "workers.read" {
		t.Fatalf("unknown population = %+v, %v", strangers, err)
	}
	// A population is not a person: leading and trailing space is trimmed, and
	// the wildcard alone is refused rather than guessed at.
	trimmed, err := console.PreviewPopulation("tenant-a", "admin-one", created.ID(), "  contractors ")
	if err != nil || len(trimmed.Skills) != 1 {
		t.Fatalf("trimmed population = %+v, %v", trimmed, err)
	}
	for _, bad := range []string{"", "   ", "*"} {
		if _, err := console.PreviewPopulation("tenant-a", "admin-one", created.ID(), bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("population %q = %v, want ErrInvalid", bad, err)
		}
	}
	if _, err := console.PreviewPopulation("tenant-a", "admin-one", "hris#99", "contractors"); !errors.Is(err, ErrRevisionAbsent) {
		t.Fatalf("unknown revision = %v", err)
	}
}

// A population preview warns like a person preview: a T3 or T4 skill granted
// to everyone is named, and an unapproved high-impact revision says it needs
// a second administrator.
func TestTodo_AGENT2_019_PopulationPreviewWarnings(t *testing.T) {
	console, _ := newConsoleFixture(t)
	risky := readDraft()
	risky.Grants = []agentconnect.GrantScope{{ID: "all", Roles: []string{agentconnect.AnyScope}, Population: agentconnect.AnyScope, OrganizationScopes: []string{"org-a"}, Skills: []string{"workers.read", "promotions.submit"}}}
	created, err := console.Create("tenant-a", "admin-one", risky)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := console.PreviewPopulation("tenant-a", "admin-one", created.ID(), "employees")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(preview.Warnings, "\n")
	if !strings.Contains(joined, "promotions.submit (T3) is granted to everyone.") {
		t.Fatalf("warnings lack the everyone grant: %v", preview.Warnings)
	}
	if !strings.Contains(joined, "Publishing needs a second administrator.") {
		t.Fatalf("warnings lack the second-administrator notice: %v", preview.Warnings)
	}
	if strings.Contains(joined, "workers.read (T0)") {
		t.Fatalf("a read skill was warned about: %v", preview.Warnings)
	}
}

// Only an administrator of the revision's tenant may preview a population,
// and a preview changes nothing.
func TestTodo_AGENT2_019_PopulationPreviewSecurity(t *testing.T) {
	console, publisher := newConsoleFixture(t)
	created, err := console.Create("tenant-a", "admin-one", readDraft())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := console.PreviewPopulation("tenant-a", "ana", created.ID(), "employees"); !errors.Is(err, ErrDenied) {
		t.Fatalf("a person who is not an administrator previewed: %v", err)
	}
	if _, err := console.PreviewPopulation("tenant-b", "admin-one", created.ID(), "employees"); !errors.Is(err, ErrRevisionAbsent) {
		t.Fatalf("another tenant's administrator previewed: %v", err)
	}
	if len(publisher.published) != 0 {
		t.Fatalf("published = %v", publisher.published)
	}
	revisions, err := console.Revisions("tenant-a", "admin-one")
	if err != nil || len(revisions) != 1 || revisions[0].Status != StatusDraft {
		t.Fatalf("previewing changed the revision: %+v, %v", revisions, err)
	}
}

// The page data for a population preview: the subject says it is a
// population, and every skill carries its tier and its per-member gating.
func TestTodo_AGENT2_019_PopulationPreviewSnapshot(t *testing.T) {
	console, _ := newConsoleFixture(t)
	draft := readDraft()
	draft.Grants = []agentconnect.GrantScope{managersGrant("workers.read", "promotions.submit")}
	created, err := console.Create("tenant-a", "admin-one", draft)
	if err != nil {
		t.Fatal(err)
	}
	session := console.For("tenant-a", "admin-one")
	snapshot, err := session.SnapshotForPopulation("Workforce", "employees", created.ID())
	if err != nil || !snapshot.PreviewReady || snapshot.PreviewSubject != "Workforce" {
		t.Fatalf("snapshot = %+v, %v", snapshot, err)
	}
	if len(snapshot.Preview.Connections) != 1 || len(snapshot.Preview.Connections[0].Skills) != 2 {
		t.Fatalf("preview = %+v", snapshot.Preview)
	}
	first := snapshot.Preview.Connections[0].Skills[0]
	if first.ID != "promotions.submit" || first.Tier != string(agentconnect.TierT3) {
		t.Fatalf("skills are not sorted with tiers: %+v", snapshot.Preview.Connections[0].Skills)
	}
	if !strings.Contains(first.Scope, "manager") || !strings.Contains(first.Scope, "org-a") {
		t.Fatalf("skill scope hides the per-member gating: %q", first.Scope)
	}
	if len(snapshot.Preview.Warnings) == 0 {
		t.Fatal("population preview dropped the warnings")
	}
	// Without a label the subject names the population, so the panel cannot be
	// mistaken for one person's preview.
	defaulted, err := session.SnapshotForPopulation("", "employees", created.ID())
	if err != nil || defaulted.PreviewSubject != "Population employees" {
		t.Fatalf("defaulted subject = %+v, %v", defaulted, err)
	}
	if _, err := session.SnapshotForPopulation("", "  ", created.ID()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("blank population snapshot = %v", err)
	}
}
