package demoworkforce

// The demo company's job architecture, as a tenant's own stored graph rather
// than a compiled-in map.
//
// migrations/00048 gave the platform job_family_revision, job_level_revision,
// job_grade_revision, job_profile_revision and job_architecture_revision, and
// internal/data/jobarchstore an adapter for them, but nothing ever wrote a
// row: the served catalog answered out of Go literals, so no tenant could
// read, scope or revise the architecture its own workers are classified
// under. This file authors HarborCare's graph -- a family per job-catalog
// discipline ([JobFamilyFor], the same taxonomy every job row records), the
// career levels and compensation grades each discipline actually uses, and
// one profile per staffed job code -- so the seeder can record it.
//
// The graph is a pure function of the staffing catalog in plan.go: every
// identity is derived from a code, every date is a declared constant, and no
// map iteration order reaches the result.

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/jobarch"
)

// JobArchitectureID and JobArchitectureRevision name the one architecture
// graph this company publishes and the revision this build authors.
const (
	JobArchitectureID       = "harborcare-demo.job-architecture"
	JobArchitectureRevision = "2026.09.1"
)

// JobArchitectureKnownFrom is when this architecture became known. It matches
// the instant SeedOrganization records the legal entity and units at, so the
// graph and the organization it classifies are known together. The effective
// date is [CatalogEffectiveFrom], which precedes every hire in the plan.
var JobArchitectureKnownFrom = harborCareEffective

// levelTitles names the career level each grade belongs to. It is the one
// place a grade's human meaning is stated; the rank comes from gradeRank so
// the ladder and the architecture can never disagree about seniority.
var levelTitles = map[string]string{
	"P2": "Associate", "P3": "Professional", "P4": "Senior", "P5": "Principal",
	"M2": "Manager", "M3": "Senior Manager", "M4": "Director", "M5": "Vice President",
	"E6": "Officer", "E7": "Executive",
}

// JobFamilyID, JobLevelID, JobGradeID and JobProfileID derive the stable
// architecture identities.
//
// The family is the DISCIPLINE a job belongs to -- the same taxonomy
// [JobFamilyFor] records on every job catalog row -- not the organization
// unit the holder currently reports into. The two are deliberately different
// dimensions and both are real: a security engineer and a systems
// administrator sit in one unit and in two families, and a general counsel
// sits in the legal unit and in the executive family. Recording the family on
// the org unit would have published a second, contradictory meaning of "job
// family" in the same tenant.
//
// Levels and grades are per family because a grade belongs to exactly one
// level and a level to exactly one family (jobarch.ArchitectureRevision
// enforces both), so "P4 in Software Engineering" and "P4 in Sales" are
// distinct nodes of the same graph.
func JobFamilyID(family string) string { return "harborcare-demo.family/" + slug(family) }

// JobLevelID is the stable identity of one career level inside a family.
func JobLevelID(family, grade string) string {
	return "harborcare-demo.level/" + slug(family) + "/" + grade
}

// JobGradeID is the stable identity of one compensation grade inside a family.
func JobGradeID(family, grade string) string {
	return "harborcare-demo.grade/" + slug(family) + "/" + grade
}

// JobProfileID is the stable identity of the profile one staffed job code
// resolves to. Job codes are unique across the company, so the profile does
// not repeat its family.
func JobProfileID(jobCode string) string { return "harborcare-demo.profile/" + jobCode }

// JobArchitecture builds the company's published architecture graph.
func JobArchitecture() (jobarch.ArchitectureRevision, error) {
	var (
		families []jobarch.JobFamilyRevision
		levels   []jobarch.JobLevelRevision
		grades   []jobarch.JobGradeRevision
		profiles []jobarch.JobProfileRevision
	)
	units := make(map[string]bool, len(HarborCare.Units))
	for _, unit := range HarborCare.Units {
		units[unit.Code] = true
	}
	familyByID := map[string]string{}
	seenNode := map[string]bool{}
	for _, seat := range allRoles() {
		role := seat.Role
		if !units[seat.Unit] {
			return jobarch.ArchitectureRevision{}, fmt.Errorf("demoworkforce: the catalog references invalid unit %q", seat.Unit)
		}
		rank, ranked := gradeRank[role.Grade]
		if !ranked {
			return jobarch.ArchitectureRevision{}, fmt.Errorf("demoworkforce: role %s carries unranked grade %q", role.Code, role.Grade)
		}
		title, titled := levelTitles[role.Grade]
		if !titled {
			return jobarch.ArchitectureRevision{}, fmt.Errorf("demoworkforce: grade %q has no career level title", role.Grade)
		}
		family := JobFamilyFor(role.Code)
		if family == "" {
			return jobarch.ArchitectureRevision{}, fmt.Errorf("demoworkforce: role %s belongs to no job family", role.Code)
		}
		familyID := JobFamilyID(family)
		if existing, seen := familyByID[familyID]; seen {
			if existing != family {
				return jobarch.ArchitectureRevision{}, fmt.Errorf(
					"demoworkforce: job families %q and %q derive the same architecture identity %s", existing, family, familyID)
			}
		} else {
			familyByID[familyID] = family
			// A discipline is a root family: this company's taxonomy is a
			// flat set of disciplines, and inventing a parent tier the
			// job catalog does not record would be a fact nobody
			// asserted.
			families = append(families, jobarch.JobFamilyRevision{
				FamilyID: familyID, Revision: JobArchitectureRevision,
				Code: slug(family), Name: family,
				Lifecycle: jobarch.LifecyclePublished, EffectiveFrom: CatalogEffectiveFrom,
				KnownFrom: JobArchitectureKnownFrom,
				Lineage:   jobarch.RevisionLineage{RootID: familyID},
			})
		}
		nodeKey := familyID + "/" + role.Grade
		if !seenNode[nodeKey] {
			seenNode[nodeKey] = true
			levels = append(levels, jobarch.JobLevelRevision{
				LevelID: JobLevelID(family, role.Grade), Revision: JobArchitectureRevision,
				FamilyID: familyID, Code: role.Grade, Title: title, Rank: rank,
				Lifecycle: jobarch.LifecyclePublished, EffectiveFrom: CatalogEffectiveFrom,
				KnownFrom: JobArchitectureKnownFrom,
				Lineage:   jobarch.RevisionLineage{RootID: JobLevelID(family, role.Grade)},
			})
			grades = append(grades, jobarch.JobGradeRevision{
				GradeID: JobGradeID(family, role.Grade), Revision: JobArchitectureRevision,
				LevelID: JobLevelID(family, role.Grade), Code: role.Grade,
				Name:      title + " (" + family + ")",
				Lifecycle: jobarch.LifecyclePublished, EffectiveFrom: CatalogEffectiveFrom,
				KnownFrom: JobArchitectureKnownFrom,
				Lineage:   jobarch.RevisionLineage{RootID: JobGradeID(family, role.Grade)},
			})
		}
		profiles = append(profiles, jobarch.JobProfileRevision{
			ProfileID: JobProfileID(role.Code), Revision: JobArchitectureRevision,
			FamilyID: familyID, LevelID: JobLevelID(family, role.Grade),
			GradeID: JobGradeID(family, role.Grade),
			JobCode: role.Code, Title: role.Title,
			Description: role.Title + ", a " + family + " role graded " + role.Grade + ".",
			Lifecycle:   jobarch.LifecyclePublished, EffectiveFrom: CatalogEffectiveFrom,
			KnownFrom: JobArchitectureKnownFrom,
			Lineage:   jobarch.RevisionLineage{RootID: JobProfileID(role.Code)},
		})
	}
	sort.Slice(families, func(i, j int) bool { return families[i].FamilyID < families[j].FamilyID })
	sort.Slice(levels, func(i, j int) bool { return levels[i].LevelID < levels[j].LevelID })
	sort.Slice(grades, func(i, j int) bool { return grades[i].GradeID < grades[j].GradeID })
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].ProfileID < profiles[j].ProfileID })

	architecture, err := jobarch.NewArchitectureRevision(jobarch.ArchitectureRevision{
		ID: JobArchitectureID, Revision: JobArchitectureRevision,
		Families: families, Levels: levels, Grades: grades, Profiles: profiles,
	})
	if err != nil {
		return jobarch.ArchitectureRevision{}, fmt.Errorf("demoworkforce: build the job architecture: %w", err)
	}
	return architecture, nil
}

// SeedJobArchitecture records the company's architecture graph through store,
// reporting whether this call wrote it.
//
// It is replay-safe by the same rule the rest of this package uses: an
// architecture the tenant already publishes is verified to be this one rather
// than saved a second time, and a stored graph that means something else is a
// hard error instead of a silent overwrite.
//
// The store owns its own transaction (internal/data/jobarchstore begins one
// per call so tenant context is established before any row is touched), so
// this seeder takes the store rather than the caller's transaction.
func SeedJobArchitecture(ctx context.Context, store jobarch.Store, tenant string) (bool, error) {
	if store == nil {
		return false, fmt.Errorf("demoworkforce: seed job architecture: a store is required")
	}
	architecture, err := JobArchitecture()
	if err != nil {
		return false, err
	}
	current, err := store.Current(ctx, tenant, JobArchitectureID)
	switch {
	case err == nil:
		if current.Revision != architecture.Revision || current.CanonicalDigest != architecture.CanonicalDigest {
			return false, fmt.Errorf(
				"demoworkforce: tenant already publishes job architecture revision %s (%s), which is not the deterministic HarborCare graph %s (%s)",
				current.Revision, current.CanonicalDigest, architecture.Revision, architecture.CanonicalDigest)
		}
		return false, nil
	case errors.Is(err, jobarch.ErrStoreNotFound):
	default:
		return false, fmt.Errorf("demoworkforce: read the job architecture: %w", err)
	}
	if err := store.Save(ctx, tenant, architecture, ""); err != nil {
		if errors.Is(err, jobarch.ErrStoreDuplicate) {
			// A concurrent seeder won the race with the same deterministic
			// graph; that is the replay case, not a conflict.
			return false, nil
		}
		return false, fmt.Errorf("demoworkforce: save the job architecture: %w", err)
	}
	return true, nil
}

// JobArchitectureCounts reports how many nodes of each kind the published
// graph carries. It exists so a seed summary can state what it wrote without
// rebuilding the graph a second time.
type JobArchitectureCounts struct{ Families, Levels, Grades, Profiles int }

// CountJobArchitecture returns the published graph's node counts.
func CountJobArchitecture() (JobArchitectureCounts, error) {
	architecture, err := JobArchitecture()
	if err != nil {
		return JobArchitectureCounts{}, err
	}
	return JobArchitectureCounts{
		Families: len(architecture.Families), Levels: len(architecture.Levels),
		Grades: len(architecture.Grades), Profiles: len(architecture.Profiles),
	}, nil
}
