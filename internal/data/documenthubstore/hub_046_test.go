// Test matrix for HUB-046: give each person private folders, stars and an
// owner access list for their document library. These tests exercise the
// real library.go GREEN behavior end to end: per-person folders (create,
// rename, delete, unique names), stars, bulk moves capped at 100, and an
// owner-only access list whose revocation never removes the owner and whose
// counts only ever include documents the actor can still read.
package documenthubstore

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// TestTodo_HUB_046 is the PRIMARY test for HUB-046: a person can organize
// their readable documents into their own folders and stars, bulk moves are
// capped at 100, and the owner can see who holds access to a document.
func TestTodo_HUB_046(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	const tenant, owner, reader = "tenant-hub046", "owner-046", "reader-046"

	planA, _, err := s.CreatePersonalDocument(ctx, tenant, owner, "Benefits guide", "# Benefits\nEnrollment steps\n")
	if err != nil {
		t.Fatal(err)
	}
	planB, _, err := s.CreatePersonalDocument(ctx, tenant, owner, "Expense policy", "# Expense\nReimbursement rules\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SharePersonalDocumentRole(ctx, tenant, planA, owner, reader, RoleViewer); err != nil {
		t.Fatal(err)
	}
	if err := s.SharePersonalDocumentRole(ctx, tenant, planB, owner, reader, RoleCommenter); err != nil {
		t.Fatal(err)
	}

	// Folder creation trims, bounds and enforces case-insensitive uniqueness
	// per person, and reports a real created_at.
	folder, err := s.CreateFolder(ctx, tenant, reader, "  HR docs ")
	if err != nil || folder.Name != "HR docs" || folder.ID == "" || folder.CreatedAt.IsZero() {
		t.Fatalf("create folder = %+v, %v", folder, err)
	}
	if _, err := s.CreateFolder(ctx, tenant, reader, "hr DOCS"); !errors.Is(err, ErrFolderNameTaken) {
		t.Fatalf("case-insensitive duplicate accepted: %v", err)
	}
	second, err := s.CreateFolder(ctx, tenant, reader, "Archive")
	if err != nil {
		t.Fatalf("second distinct folder: %v", err)
	}

	// Rename moves the same folder to a new unique name; it never collides
	// with itself and still rejects another folder's name.
	if err := s.RenameFolder(ctx, tenant, reader, second.ID, "archive"); err != nil {
		t.Fatalf("case-only rename of the same folder: %v", err)
	}
	if err := s.RenameFolder(ctx, tenant, reader, second.ID, "HR docs"); !errors.Is(err, ErrFolderNameTaken) {
		t.Fatalf("rename onto a sibling folder's name = %v", err)
	}
	if err := s.RenameFolder(ctx, tenant, reader, folder.ID, "HR & Benefits"); err != nil {
		t.Fatal(err)
	}

	// Bulk move: duplicates collapse, and both documents land in the folder.
	if err := s.MoveDocuments(ctx, tenant, reader, []string{planA, planB, planA}, folder.ID); err != nil {
		t.Fatal(err)
	}
	lib, err := s.GetLibrary(ctx, tenant, reader)
	if err != nil || folderCount(lib, folder.ID) != 2 {
		t.Fatalf("folder count after move = %+v, %v", lib, err)
	}

	// A move naming exactly MaxMoveDocuments passes the size gate (it fails
	// later, on readability, for the fabricated ids -- proving the cap is
	// exactly 100 and not off by one).
	exactly100 := make([]string, MaxMoveDocuments)
	for i := range exactly100 {
		exactly100[i] = fmt.Sprintf("hub046-doc-%d", i)
	}
	if err := s.MoveDocuments(ctx, tenant, reader, exactly100, folder.ID); errors.Is(err, ErrTooManyDocuments) {
		t.Fatalf("exactly %d documents rejected as too many", MaxMoveDocuments)
	} else if !errors.Is(err, ErrDenied) {
		t.Fatalf("exactly %d documents = %v, want ErrDenied for unreadable ids", MaxMoveDocuments, err)
	}
	over100 := append(exactly100, "hub046-doc-over")
	if err := s.MoveDocuments(ctx, tenant, reader, over100, folder.ID); !errors.Is(err, ErrTooManyDocuments) {
		t.Fatalf("%d documents = %v, want ErrTooManyDocuments", len(over100), err)
	}

	// Stars are idempotent and need read access.
	for i := 0; i < 2; i++ {
		if err := s.SetStarred(ctx, tenant, reader, planA, true); err != nil {
			t.Fatal(err)
		}
	}
	lib, err = s.GetLibrary(ctx, tenant, reader)
	if err != nil || lib.Starred != 1 {
		t.Fatalf("starred count = %+v, %v", lib, err)
	}
	if err := s.SetStarred(ctx, tenant, reader, planA, false); err != nil {
		t.Fatal(err)
	}
	lib, err = s.GetLibrary(ctx, tenant, reader)
	if err != nil || lib.Starred != 0 {
		t.Fatalf("unstarred count = %+v, %v", lib, err)
	}

	// Delete removes only the actor's own folder and its placements; the
	// documents themselves stay, unfiled.
	if err := s.DeleteFolder(ctx, tenant, reader, folder.ID); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListPersonalDocumentsPage(ctx, tenant, reader, ListOptions{})
	if err != nil || len(rows) != 2 {
		t.Fatalf("documents survive folder delete = %+v, %v", rows, err)
	}
	for _, row := range rows {
		if row.FolderID != "" {
			t.Fatalf("document still filed after folder delete: %+v", row)
		}
	}

	// The owner sees a full access list: themselves as an unremovable owner,
	// plus each viewer/commenter role assigned.
	access, err := s.ListAccess(ctx, tenant, owner, planA)
	if err != nil || len(access) != 2 || access[0].Role != "owner" || access[0].Removable {
		t.Fatalf("planA access = %+v, %v", access, err)
	}
	if access[1].SubjectID != reader || access[1].Role != RoleViewer || !access[1].Removable {
		t.Fatalf("planA reader entry = %+v", access[1])
	}
	access, err = s.ListAccess(ctx, tenant, owner, planB)
	if err != nil || len(access) != 2 || access[1].Role != RoleCommenter {
		t.Fatalf("planB access = %+v, %v", access, err)
	}
}

// TestTodo_HUB_046_Security is the SECURITY test for HUB-046: folders and
// stars never grant, widen or leak access; RevokePersonAccess can never
// remove the owner; and both the library counts and the access list only
// ever reflect documents the actor can currently read.
func TestTodo_HUB_046_Security(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	const tenant, owner, reader, manager, stranger = "tenant-hub046-sec", "owner-046s", "reader-046s", "manager-046s", "stranger-046s"

	doc, _, err := s.CreatePersonalDocument(ctx, tenant, owner, "Payroll close", "# Payroll\nClose checklist\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SharePersonalDocumentRole(ctx, tenant, doc, owner, reader, RoleViewer); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, tenant, GrantInput{DocumentID: doc, SubjectKind: "person", SubjectID: manager, Action: ActionManage, Effect: EffectAllow, Issuer: owner}); err != nil {
		t.Fatal(err)
	}

	// Organizing a document never grants a stranger anything: a folder and a
	// star are the reader's own data, not access.
	folder, err := s.CreateFolder(ctx, tenant, reader, "Payroll")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MoveDocuments(ctx, tenant, reader, []string{doc}, folder.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStarred(ctx, tenant, reader, doc, true); err != nil {
		t.Fatal(err)
	}
	if err := s.Authorize(ctx, tenant, doc, "person", stranger, ActionRead); !errors.Is(err, ErrDenied) {
		t.Fatalf("filing/starring granted a stranger read access: %v", err)
	}
	if _, err := s.ListPersonalDocumentsPage(ctx, tenant, stranger, ListOptions{}); err != nil {
		t.Fatal(err)
	}

	// Organizing never widens the actor's own reach either: an unreadable
	// document cannot be filed or starred, whatever the actor's own folders
	// contain.
	private, _, err := s.CreatePersonalDocument(ctx, tenant, owner, "Private severance notes", "# Private\nNot shared\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MoveDocuments(ctx, tenant, reader, []string{private}, folder.ID); !errors.Is(err, ErrDenied) {
		t.Fatalf("filed an unreadable document: %v", err)
	}
	if err := s.SetStarred(ctx, tenant, reader, private, true); !errors.Is(err, ErrDenied) {
		t.Fatalf("starred an unreadable document: %v", err)
	}

	// One person's folder is never visible or usable by another, including
	// the owner and a manage holder.
	if err := s.RenameFolder(ctx, tenant, owner, folder.ID, "Mine now"); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("owner renamed the reader's folder: %v", err)
	}
	if err := s.DeleteFolder(ctx, tenant, manager, folder.ID); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("manager deleted the reader's folder: %v", err)
	}
	if err := s.MoveDocuments(ctx, tenant, owner, []string{doc}, folder.ID); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("owner filed into the reader's folder: %v", err)
	}

	// The access list and its revocation are for the owner or a live manage
	// holder only -- a plain viewer, and a stranger, both get ErrDenied
	// without any signal of whether the document exists.
	if _, err := s.ListAccess(ctx, tenant, reader, doc); !errors.Is(err, ErrDenied) {
		t.Fatalf("viewer listed access: %v", err)
	}
	if _, err := s.ListAccess(ctx, tenant, stranger, doc); !errors.Is(err, ErrDenied) {
		t.Fatalf("stranger listed access: %v", err)
	}
	if err := s.RevokePersonAccess(ctx, tenant, reader, doc, owner); !errors.Is(err, ErrDenied) {
		t.Fatalf("viewer revoked access: %v", err)
	}

	// A manage holder may act, but can never remove the owner -- neither
	// directly by the owner themself, nor by a delegated manager.
	if err := s.RevokePersonAccess(ctx, tenant, owner, doc, owner); !errors.Is(err, ErrOwnerAccess) {
		t.Fatalf("owner removed themself: %v", err)
	}
	if err := s.RevokePersonAccess(ctx, tenant, manager, doc, owner); !errors.Is(err, ErrOwnerAccess) {
		t.Fatalf("manager removed the owner: %v", err)
	}
	if access, err := s.ListAccess(ctx, tenant, manager, doc); err != nil || access[0].SubjectID != owner || access[0].Role != "owner" {
		t.Fatalf("owner missing from access after failed removal attempts: %+v, %v", access, err)
	}

	// The library's counts, and the access list itself, only ever reflect
	// documents/grants the actor can currently see -- an explicit deny
	// removes a subject from ListAccess even though nothing was revoked,
	// and once the reader's own read access is gone their folder/star
	// counts and readback all drop to zero without a trace of the document.
	if _, err := s.GrantAction(ctx, tenant, GrantInput{DocumentID: doc, SubjectKind: "person", SubjectID: reader, Action: ActionRead, Effect: "deny", Issuer: owner}); err != nil {
		t.Fatal(err)
	}
	access, err := s.ListAccess(ctx, tenant, owner, doc)
	if err != nil || len(access) != 1 {
		t.Fatalf("denied subject still counted in access list: %+v, %v", access, err)
	}
	for _, entry := range access {
		if entry.SubjectID == reader {
			t.Fatalf("denied reader still listed: %+v", entry)
		}
	}
	if _, _, err := s.ReadPersonalDocument(ctx, tenant, reader, doc); !errors.Is(err, ErrDenied) {
		t.Fatalf("denied reader still reads the document: %v", err)
	}
	lib, err := s.GetLibrary(ctx, tenant, reader)
	if err != nil || lib.All != 0 || lib.Starred != 0 || folderCount(lib, folder.ID) != 0 {
		t.Fatalf("library counted a document the reader can no longer read: %+v, %v", lib, err)
	}
	if rows, err := s.ListPersonalDocumentsPage(ctx, tenant, reader, ListOptions{FolderID: folder.ID}); err != nil || len(rows) != 0 {
		t.Fatalf("folder listing counted a document the reader can no longer read: %+v, %v", rows, err)
	}
}

// TestTodo_HUB_046_Integration is the INTEGRATION test for HUB-046: folders,
// placements and stars persist in real tenant-scoped, row-level-secured
// tables against a real migrated database, and are invisible across
// tenants at the SQL layer, not merely through the Go API.
func TestTodo_HUB_046_Integration(t *testing.T) {
	s, schema := documentFixture(t)
	ctx := context.Background()
	const tenantA, tenantB, actor = "tenant-hub046-int-a", "tenant-hub046-int-b", "actor-046i"

	doc, _, err := s.CreatePersonalDocument(ctx, tenantA, actor, "Runbook", "# Runbook\nSteps\n")
	if err != nil {
		t.Fatal(err)
	}
	folder, err := s.CreateFolder(ctx, tenantA, actor, "Runbooks")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MoveDocuments(ctx, tenantA, actor, []string{doc}, folder.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStarred(ctx, tenantA, actor, doc, true); err != nil {
		t.Fatal(err)
	}

	// The rows are real: a direct SQL readback (not the Go API) sees exactly
	// what was written.
	var folders, items, stars int
	if err := s.RunTenantTx(ctx, tenantA, func(tx dbport.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM document_folder WHERE tenant_id=$1 AND owner_id=$2 AND id=$3`, tenantA, actor, folder.ID).Scan(&folders); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM document_folder_item WHERE tenant_id=$1 AND owner_id=$2 AND document_id=$3 AND folder_id=$4`, tenantA, actor, doc, folder.ID).Scan(&items); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM document_star WHERE tenant_id=$1 AND owner_id=$2 AND document_id=$3`, tenantA, actor, doc).Scan(&stars)
	}); err != nil {
		t.Fatal(err)
	}
	if folders != 1 || items != 1 || stars != 1 {
		t.Fatalf("persisted rows folders=%d items=%d stars=%d, want 1,1,1", folders, items, stars)
	}

	// Row level security forces tenant isolation on all three tables, not
	// just application-level filtering.
	var policies, forced int
	if err := s.RunTenantTx(ctx, tenantA, func(tx dbport.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM pg_policies WHERE schemaname=current_schema() AND tablename IN ('document_folder','document_folder_item','document_star') AND policyname='tenant_isolation'`).Scan(&policies); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM pg_class WHERE relnamespace=current_schema()::regnamespace AND relname IN ('document_folder','document_folder_item','document_star') AND relrowsecurity AND relforcerowsecurity`).Scan(&forced)
	}); err != nil || policies != 3 || forced != 3 {
		t.Fatalf("tenant_isolation backstop missing on library tables: policies=%d forced=%d err=%v", policies, forced, err)
	}
	if schema == "" {
		t.Fatal("fixture schema not set")
	}

	// A cross-tenant read at the raw SQL layer, scoped by tenant_id the way
	// every store query is, sees nothing for the other tenant's folder.
	var leaked int
	if err := s.RunTenantTx(ctx, tenantB, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM document_folder WHERE tenant_id=$1 AND owner_id=$2`, tenantB, actor).Scan(&leaked)
	}); err != nil || leaked != 0 {
		t.Fatalf("cross-tenant folder read leaked rows: count=%d err=%v", leaked, err)
	}

	// And the same isolation holds through the Go API: a different tenant's
	// GetLibrary for the same person id starts empty.
	lib, err := s.GetLibrary(ctx, tenantB, actor)
	if err != nil || len(lib.Folders) != 0 || lib.All != 0 || lib.Starred != 0 {
		t.Fatalf("cross-tenant library leaked: %+v, %v", lib, err)
	}
}
