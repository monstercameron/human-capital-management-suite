package productclient

import (
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
)

// TestTodo_REV_070_01_Person proves the wire-to-person projection
// carries the lifecycle tokens: a terminated contractor on the
// wire becomes a terminated contractor Person, and an unreported
// pair stays unreported rather than invented.
func TestTodo_REV_070_01_Person(t *testing.T) {
	root := journeyv1.ManagerRelationshipProjection_DISPOSITION_ROOT
	people, err := projectWorkers([]*journeyv1.Worker{{
		WorkerRef: "gone", WorkerId: "id-gone",
		LifecycleStatus: "TERMINATED", WorkerType: "CONTRACTOR",
		BasePay: "90000.00", Currency: "USD",
		ManagerRelationship: &journeyv1.ManagerRelationshipProjection{Disposition: root},
	}})
	if err != nil {
		t.Fatalf("projection refuses: %v", err)
	}
	if len(people) != 1 {
		t.Fatalf("projection yields %d people", len(people))
	}
	if people[0].LifecycleStatus != "TERMINATED" {
		t.Fatalf("person lifecycle = %q, want TERMINATED", people[0].LifecycleStatus)
	}
	if people[0].WorkerType != "CONTRACTOR" {
		t.Fatalf("person worker type = %q, want CONTRACTOR", people[0].WorkerType)
	}

	bare, err := projectWorkers([]*journeyv1.Worker{{
		WorkerRef: "bare", WorkerId: "id-bare",
		ManagerRelationship: &journeyv1.ManagerRelationshipProjection{Disposition: root},
	}})
	if err != nil {
		t.Fatalf("bare projection refuses: %v", err)
	}
	if bare[0].LifecycleStatus != "" || bare[0].WorkerType != "" {
		t.Fatalf("bare projection invents %+v", bare[0])
	}
}
