package project

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
)

func TestMemberWirePreservesOnlyMembershipProjection(t *testing.T) {
	got := memberWire(projectservice.MembershipRecord{UserID: "alice", Role: projectaccess.RoleManager, State: projectaccess.MembershipInvited, Revision: 7})
	if got.GetUserId() != "alice" || got.GetRole() != "MANAGER" || got.GetState() != "INVITED" || got.GetRevision() != 7 {
		t.Fatalf("membership mapping mismatch: %+v", got)
	}
}
