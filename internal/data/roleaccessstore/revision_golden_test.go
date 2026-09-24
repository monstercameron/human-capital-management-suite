package roleaccessstore

import (
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
)

func TestTodo_RBAC_RT_021_Golden(t *testing.T) {
	entry, err := NewRevision(uuid.MustParse("f4780b8f-e847-47a1-84cc-e7f04a2b14be"), "principal:admin", RevisionPagePermission, "manager", "", "journeys", "", roleaccess.PagePermission{
		Version: 1, RoleID: "manager", PageID: "journeys", View: true,
	}, roleaccess.PagePermission{
		Version: 2, RoleID: "manager", PageID: "journeys", View: true, Update: true,
	}, nil, "Expanded page access for the regional review team")
	if err != nil {
		t.Fatal(err)
	}
	const wantBefore = `{"Version":1,"RoleID":"manager","PageID":"journeys","View":true,"Create":false,"Update":false,"Delete":false}`
	const wantAfter = `{"Version":2,"RoleID":"manager","PageID":"journeys","View":true,"Create":false,"Update":true,"Delete":false}`
	if got := string(entry.Before); got != wantBefore {
		t.Fatalf("before image = %s, want %s", got, wantBefore)
	}
	if got := string(entry.After); got != wantAfter {
		t.Fatalf("after image = %s, want %s", got, wantAfter)
	}
	if entry.Reason != "Expanded page access for the regional review team" || entry.ActorRef != "principal:admin" {
		t.Fatalf("audit fields = actor %q, reason %q", entry.ActorRef, entry.Reason)
	}
}
