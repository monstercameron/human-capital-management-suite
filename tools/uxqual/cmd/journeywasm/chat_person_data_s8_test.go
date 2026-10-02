package main

import (
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
)

// The directory names the department and carries the work contact details;
// when it cannot name the unit the code is turned into words, never printed raw.
func TestChatPersonDetailsCarryDepartmentNameAndWorkContact(t *testing.T) {
	visible := &journeyv1.ManagerRelationshipProjection{Disposition: journeyv1.ManagerRelationshipProjection_DISPOSITION_VISIBLE, ManagerWorkerRef: "priya"}
	workers := []*journeyv1.Worker{
		{WorkerRef: "priya", SubjectId: "priya", LegalName: "Priya Raman", ProfilePhotoUrl: "/photos/priya"},
		{WorkerRef: "greg", SubjectId: "greg", LegalName: "Greg Novak", OrgUnit: "safety-quality", OrgUnitName: "Safety & Quality", WorkPhone: " (303) 555-0105 ", WorkEmail: "greg.novak@ironridge.example", ManagerRelationship: visible},
		{WorkerRef: "sofia", SubjectId: "sofia", LegalName: "Sofia Beltran", OrgUnit: "project-management", ManagerRelationship: &journeyv1.ManagerRelationshipProjection{Disposition: journeyv1.ManagerRelationshipProjection_DISPOSITION_VISIBLE, ManagerWorkerRef: "greg"}, ProfilePhotoUrl: "/photos/sofia"},
	}
	greg, ok := chatPersonDetailsFromWorkers(workers, "greg")
	if !ok || greg.Department != "Safety & Quality" || greg.Phone != "(303) 555-0105" || greg.Email != "greg.novak@ironridge.example" {
		t.Fatalf("greg = %+v, %v", greg, ok)
	}
	if greg.ManagerPhotoURL != "/photos/priya" || len(greg.DirectReports) != 1 || greg.DirectReports[0].PhotoURL != "/photos/sofia" {
		t.Fatalf("relationship photos = %+v", greg)
	}
	sofia, ok := chatPersonDetailsFromWorkers(workers, "sofia")
	if !ok || sofia.Department != "Project Management" {
		t.Fatalf("a unit the directory did not name = %+v, %v", sofia, ok)
	}
}
