package projectservice

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectboard"
)

func TestTodo_PM_017_BoardCursorBindsOrderRevisionsAndEvents(t *testing.T) {
	q := projectboard.AuthorizedTaskQuery{ViewID: "view-1", ViewVersion: 3, StatusIDs: []string{"todo"}, OrderBy: projectboard.OrderTitle}
	digest := queryDigest(q)
	c := boardCursor{TenantID: "tenant-1", ProjectID: "project-1", UserID: "user-1", ViewID: q.ViewID, ViewVersion: q.ViewVersion, ProjectRevision: 7, WorkflowRevision: 11, EventSequence: 13, FilterDigest: digest, OrderBy: projectboard.OrderTitle, AfterValue: "alpha", AfterID: "task-1"}
	if !cursorMatches(c, "tenant-1", "project-1", "user-1", q, 7, 11, 13, digest) {
		t.Fatal("matching cursor rejected")
	}
	changed := q
	changed.OrderBy = projectboard.OrderPriority
	if cursorMatches(c, "tenant-1", "project-1", "user-1", changed, 7, 11, 13, queryDigest(changed)) {
		t.Fatal("cursor accepted for a different sort order")
	}
	changed = q
	changed.Filter.AssigneeID = "another-user"
	if cursorMatches(c, "tenant-1", "project-1", "user-1", changed, 7, 11, 13, queryDigest(changed)) {
		t.Fatal("cursor accepted for a different filter")
	}
	changed = q
	changed.ViewVersion++
	if cursorMatches(c, "tenant-1", "project-1", "user-1", changed, 7, 11, 13, queryDigest(changed)) {
		t.Fatal("cursor accepted after view version changed")
	}
	if cursorMatches(c, "tenant-1", "project-1", "user-1", q, 8, 11, 13, digest) {
		t.Fatal("cursor accepted after project revision changed")
	}
	if cursorMatches(c, "tenant-1", "project-1", "user-1", q, 7, 12, 13, digest) {
		t.Fatal("cursor accepted after workflow revision changed")
	}
	if cursorMatches(c, "tenant-1", "project-1", "user-1", q, 7, 11, 14, digest) {
		t.Fatal("cursor accepted after task event sequence changed")
	}
	changed = q
	changed.Descending = true
	if cursorMatches(c, "tenant-1", "project-1", "user-1", changed, 7, 11, 13, queryDigest(changed)) {
		t.Fatal("cursor accepted for a different direction")
	}
}

func TestTodo_PM_017_SignedBoardCursorRejectsTampering(t *testing.T) {
	a := StoreAdapter{CursorKey: []byte("0123456789abcdef0123456789abcdef")}
	c := boardCursor{TenantID: "tenant-1", ProjectID: "project-1", UserID: "user-1", ViewID: "view-1", ViewVersion: 2, ProjectRevision: 4, WorkflowRevision: 5, FilterDigest: "digest", OrderBy: projectboard.OrderDueDate, AfterValue: "2026-01-01", AfterID: "task-1"}
	token, err := a.encodeCursor(c)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := a.decodeCursor(token)
	if err != nil || decoded != c {
		t.Fatalf("round trip=%+v err=%v", decoded, err)
	}
	tampered := token[:len(token)-1] + "A"
	if _, err := a.decodeCursor(tampered); err == nil {
		t.Fatal("tampered cursor accepted")
	}
}
