package journey

import (
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
)

func TestEnrichDemoDirectory(t *testing.T) {
	workers := func() []*journeyv1.Worker {
		return []*journeyv1.Worker{
			{WorkerRef: "ir-005-greg-novak", SubjectId: "ir-005-greg-novak", OrgUnit: "project-management"},
			{WorkerRef: "someone-else", SubjectId: "ir-006-sofia-beltran", OrgUnit: "project-management"},
			{WorkerRef: "unknown", SubjectId: "unknown", OrgUnit: "not-a-unit"},
			nil,
		}
	}
	chat := workers()
	enrichDemoDirectory("ironridge-demo", chat, true)
	if got := chat[0]; got.OrgUnitName != "Project Management" || got.WorkEmail != "greg.novak@ironridge.example" || got.WorkPhone != "(303) 555-0105" {
		t.Fatalf("greg = %+v", got)
	}
	if got := chat[1]; got.WorkEmail != "sofia.beltran@ironridge.example" {
		t.Fatalf("a worker joined on subject id = %+v", got)
	}
	if got := chat[2]; got.OrgUnitName != "" || got.WorkEmail != "" {
		t.Fatalf("an unknown worker was enriched: %+v", got)
	}

	listing := workers()
	enrichDemoDirectory("ironridge-demo", listing, false)
	if got := listing[0]; got.OrgUnitName != "Project Management" || got.WorkEmail != "" || got.WorkPhone != "" {
		t.Fatalf("the full listing gained contact fields: %+v", got)
	}

	other := workers()
	enrichDemoDirectory("some-real-tenant", other, true)
	if got := other[0]; got.OrgUnitName != "" || got.WorkEmail != "" {
		t.Fatalf("a non-demo tenant was enriched: %+v", got)
	}
}

// A demo person the worker row gave no photograph is pictured from the
// directory in both projections; a photograph the row carries is kept.
func TestEnrichDemoDirectoryPhotos(t *testing.T) {
	for _, contacts := range []bool{true, false} {
		workers := []*journeyv1.Worker{
			{WorkerRef: "ir-014-ben-whitaker", SubjectId: "ir-014-ben-whitaker"},
			{WorkerRef: "ir-005-greg-novak", SubjectId: "ir-005-greg-novak", ProfilePhotoUrl: "/kept.jpg"},
			{WorkerRef: "unknown", SubjectId: "unknown"},
		}
		enrichDemoDirectory("ironridge-demo", workers, contacts)
		if workers[0].GetProfilePhotoUrl() == "" {
			t.Errorf("contacts=%v: Ben Whitaker has no photograph", contacts)
		}
		if workers[1].GetProfilePhotoUrl() != "/kept.jpg" {
			t.Errorf("contacts=%v: a photograph on the row was replaced: %q", contacts, workers[1].GetProfilePhotoUrl())
		}
		if workers[2].GetProfilePhotoUrl() != "" {
			t.Errorf("contacts=%v: an unknown worker was given a photograph", contacts)
		}
	}
}
