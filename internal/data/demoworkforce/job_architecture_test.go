package demoworkforce

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/jobarchstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/jobarch"
)

// TestJobArchitectureCoversEveryStaffedRole proves the authored graph is the
// staffing catalog's own shape: one family per job-catalog discipline, a
// level and a grade for every grade a discipline actually uses, and exactly
// one profile per staffed job code -- with the profile bound to its own
// family's level and grade rather than to another family's node of the same
// name.
func TestJobArchitectureCoversEveryStaffedRole(t *testing.T) {
	architecture, err := JobArchitecture()
	if err != nil {
		t.Fatalf("JobArchitecture: %v", err)
	}
	if err := architecture.Validate(); err != nil {
		t.Fatalf("authored architecture does not validate: %v", err)
	}
	// The architecture's families are the job catalog's own families, the
	// same taxonomy JobFamilyFor records on every job row. Two different
	// meanings of "job family" in one tenant is exactly the drift this
	// assertion exists to refuse.
	wantFamilies := map[string]bool{}
	for _, seat := range allRoles() {
		family := JobFamilyFor(seat.Role.Code)
		if family == "" {
			t.Fatalf("job code %s belongs to no job family", seat.Role.Code)
		}
		wantFamilies[family] = true
	}
	gotFamilies := map[string]bool{}
	for _, family := range architecture.Families {
		if family.ParentID != "" {
			t.Errorf("family %s declares parent %s; this company's disciplines are a flat set", family.Name, family.ParentID)
		}
		gotFamilies[family.Name] = true
	}
	if len(gotFamilies) != len(wantFamilies) {
		t.Fatalf("architecture publishes %d families, want the %d job-catalog disciplines", len(gotFamilies), len(wantFamilies))
	}
	for family := range wantFamilies {
		if !gotFamilies[family] {
			t.Errorf("job-catalog family %q has no architecture family", family)
		}
	}
	profiles := map[string]jobarch.JobProfileRevision{}
	for _, profile := range architecture.Profiles {
		if _, seen := profiles[profile.JobCode]; seen {
			t.Fatalf("job code %s has two profiles", profile.JobCode)
		}
		profiles[profile.JobCode] = profile
	}
	levels := map[string]jobarch.JobLevelRevision{}
	for _, level := range architecture.Levels {
		levels[level.LevelIDOrID()] = level
	}
	for _, seat := range allRoles() {
		role := seat.Role
		{
			profile, ok := profiles[role.Code]
			if !ok {
				t.Errorf("published job code %s has no published profile", role.Code)
				continue
			}
			if profile.Title != role.Title {
				t.Errorf("profile for %s is titled %q, want %q", role.Code, profile.Title, role.Title)
			}
			family := JobFamilyFor(role.Code)
			if profile.FamilyIDOrRef() != JobFamilyID(family) {
				t.Errorf("profile for %s belongs to family %s, want %s", role.Code, profile.FamilyIDOrRef(), JobFamilyID(family))
			}
			if profile.GradeIDOrRef() != JobGradeID(family, role.Grade) {
				t.Errorf("profile for %s carries grade %s, want %s", role.Code, profile.GradeIDOrRef(), JobGradeID(family, role.Grade))
			}
			level, ok := levels[profile.LevelIDOrRef()]
			if !ok {
				t.Errorf("profile for %s names level %s, which the graph does not publish", role.Code, profile.LevelIDOrRef())
				continue
			}
			if level.Rank != gradeRank[role.Grade] {
				t.Errorf("level for %s ranks %d, want the grade ranking %d", role.Code, level.Rank, gradeRank[role.Grade])
			}
		}
	}
	counts, err := CountJobArchitecture()
	if err != nil {
		t.Fatalf("CountJobArchitecture: %v", err)
	}
	if counts.Profiles != len(profiles) || counts.Families != len(gotFamilies) {
		t.Fatalf("CountJobArchitecture = %+v, want it to agree with the graph", counts)
	}
}

// TestJobArchitectureIsDeterministic proves two builds agree byte for byte,
// which is what makes the seeder's replay check ("the tenant already
// publishes this exact graph") meaningful rather than accidental.
func TestJobArchitectureIsDeterministic(t *testing.T) {
	first, err := JobArchitecture()
	if err != nil {
		t.Fatalf("JobArchitecture: %v", err)
	}
	second, err := JobArchitecture()
	if err != nil {
		t.Fatalf("JobArchitecture: %v", err)
	}
	if first.CanonicalDigest == "" {
		t.Fatal("the authored architecture carries no canonical digest")
	}
	if first.CanonicalDigest != second.CanonicalDigest {
		t.Fatalf("two builds digest differently: %s vs %s", first.CanonicalDigest, second.CanonicalDigest)
	}
}

// TestSeedJobArchitectureLandsIsTenantScopedAndReplays proves the graph
// reaches migration 00048's tables, that a second tenant does not see it, and
// that a replay writes nothing rather than failing or duplicating.
func TestSeedJobArchitectureLandsIsTenantScopedAndReplays(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantA := seedAggregateTenant(t, db)
	tenantB := seedAggregateTenant(t, db)
	store := jobarchstore.New(db.Conn)

	wrote, err := SeedJobArchitecture(ctx, store, tenantA.String())
	if err != nil {
		t.Fatalf("SeedJobArchitecture: %v", err)
	}
	if !wrote {
		t.Fatal("the first seed reported it wrote nothing")
	}

	authored, err := JobArchitecture()
	if err != nil {
		t.Fatalf("JobArchitecture: %v", err)
	}
	stored, err := store.Current(ctx, tenantA.String(), JobArchitectureID)
	if err != nil {
		t.Fatalf("read back the seeded architecture: %v", err)
	}
	if stored.CanonicalDigest != authored.CanonicalDigest {
		t.Fatalf("stored architecture digests %s, want the authored %s", stored.CanonicalDigest, authored.CanonicalDigest)
	}
	if len(stored.Profiles) != len(authored.Profiles) || len(stored.Grades) != len(authored.Grades) ||
		len(stored.Levels) != len(authored.Levels) || len(stored.Families) != len(authored.Families) {
		t.Fatalf("stored graph has %d/%d/%d/%d families/levels/grades/profiles, want %d/%d/%d/%d",
			len(stored.Families), len(stored.Levels), len(stored.Grades), len(stored.Profiles),
			len(authored.Families), len(authored.Levels), len(authored.Grades), len(authored.Profiles))
	}

	// Tenant scoping: the second tenant publishes nothing, although the rows
	// for the first one are committed.
	if _, err := store.Current(ctx, tenantB.String(), JobArchitectureID); !errors.Is(err, jobarch.ErrStoreNotFound) {
		t.Fatalf("a second tenant reads the first tenant's architecture: %v", err)
	}

	replayed, err := SeedJobArchitecture(ctx, store, tenantA.String())
	if err != nil {
		t.Fatalf("replay SeedJobArchitecture: %v", err)
	}
	if replayed {
		t.Fatal("the replay wrote the architecture a second time")
	}
	var profileRows int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM job_profile_revision WHERE tenant_id = $1`, tenantA).Scan(&profileRows); err != nil {
		t.Fatalf("count seeded profiles: %v", err)
	}
	if profileRows != len(authored.Profiles) {
		t.Fatalf("job_profile_revision holds %d rows for the tenant, want %d", profileRows, len(authored.Profiles))
	}
	var otherTenantRows int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM job_profile_revision WHERE tenant_id = $1`, tenantB).Scan(&otherTenantRows); err != nil {
		t.Fatalf("count the other tenant's profiles: %v", err)
	}
	if otherTenantRows != 0 {
		t.Fatalf("job_profile_revision holds %d rows for the untouched tenant, want 0", otherTenantRows)
	}
}

// TestSeedJobArchitectureRefusesAForeignGraph proves the seeder treats a
// tenant that already publishes a different architecture as a hard error
// rather than overwriting what somebody else recorded.
func TestSeedJobArchitectureRefusesAForeignGraph(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenant := seedAggregateTenant(t, db)
	store := jobarchstore.New(db.Conn)

	foreign, err := jobarch.NewArchitectureRevision(jobarch.ArchitectureRevision{
		ID: JobArchitectureID, Revision: "someone-elses",
		Families: []jobarch.JobFamilyRevision{{
			FamilyID: "other.family", Revision: "1", Code: "other", Name: "Other",
			Lifecycle: jobarch.LifecyclePublished, EffectiveFrom: CatalogEffectiveFrom,
			KnownFrom: JobArchitectureKnownFrom,
		}},
	})
	if err != nil {
		t.Fatalf("build a foreign architecture: %v", err)
	}
	if err := store.Save(ctx, tenant.String(), foreign, ""); err != nil {
		t.Fatalf("save the foreign architecture: %v", err)
	}
	if _, err := SeedJobArchitecture(ctx, store, tenant.String()); err == nil {
		t.Fatal("the seeder overwrote an architecture it did not author")
	}
}

// TestSeedJobArchitectureRequiresAStore proves the seeder refuses rather than
// reporting a silent success when it was composed with nothing to write to.
func TestSeedJobArchitectureRequiresAStore(t *testing.T) {
	if _, err := SeedJobArchitecture(context.Background(), nil, uuid.New().String()); err == nil {
		t.Fatal("seeding without a store was accepted")
	}
}
