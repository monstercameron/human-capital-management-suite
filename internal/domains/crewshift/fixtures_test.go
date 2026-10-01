package crewshift

import (
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Fixture identifiers. Every ref uses a canonical UUID so values.EntityRef
// validates; the tenant is a fixed canonical slug shared by every fixture.
const fixtureTenant values.TenantId = "acme-crew"

func ref(kind, id string) values.EntityRef {
	return values.EntityRef{Tenant: fixtureTenant, Kind: values.Kind(kind), Id: id}
}

var (
	fixtureWorker    = ref("worker", "00000000-0000-4000-8000-000000000001")
	fixtureWorker2   = ref("worker", "00000000-0000-4000-8000-000000000002")
	fixtureRole      = ref("role", "00000000-0000-4000-8000-0000000000a1")
	fixtureSite      = ref("site", "00000000-0000-4000-8000-0000000000b1")
	fixtureProject   = ref("project", "00000000-0000-4000-8000-0000000000c1")
	fixtureProject2  = ref("project", "00000000-0000-4000-8000-0000000000c2")
	fixtureWorkOrder = ref("work_order", "00000000-0000-4000-8000-0000000000d1")
	fixtureApprover  = ref("approver", "00000000-0000-4000-8000-0000000000e1")
	fixtureQualA     = ref("qualification", "00000000-0000-4000-8000-0000000000f1")
	fixtureQualB     = ref("qualification", "00000000-0000-4000-8000-0000000000f2")
)

const fixtureZone = "America/New_York"

func mustLoc() *time.Location {
	loc, err := time.LoadLocation(fixtureZone)
	if err != nil {
		panic(err)
	}
	return loc
}

// fixtureDraft returns a well-formed draft shift on a plain, non-DST day.
func fixtureDraft(id string) Shift {
	loc := mustLoc()
	start := time.Date(2024, 6, 10, 9, 0, 0, 0, loc)
	end := time.Date(2024, 6, 10, 17, 0, 0, 0, loc)
	return Shift{
		ID: id, Tenant: fixtureTenant, Status: StatusDraft, Source: SourceManual,
		WorkerRef: fixtureWorker, RoleRef: fixtureRole, SiteRef: fixtureSite, ProjectRef: fixtureProject,
		Timezone: fixtureZone, Work: Interval{Start: start, End: end},
		CreatedAt: start.Add(-72 * time.Hour),
	}
}

// fixturePublishInput returns a PublishInput for shift that will clear every
// default check unless the caller overrides a field.
func fixturePublishInput(shift Shift) PublishInput {
	return PublishInput{
		Shift:         shift,
		Eligibility:   EligibilityFacts{Active: true},
		ProjectAccess: map[string]bool{shift.ProjectRef.String(): true},
		Approver:      fixtureApprover,
		AuthorityPath: AuthorityPathCrewShiftPublish,
		Now:           shift.CreatedAt,
	}
}

func mustPublish(t interface{ Fatalf(string, ...any) }, shift Shift) Shift {
	out, err := Publish(fixturePublishInput(shift), DefaultPublishChecks())
	if err != nil {
		t.Fatalf("fixture publish: %v", err)
	}
	return out.Shift
}
