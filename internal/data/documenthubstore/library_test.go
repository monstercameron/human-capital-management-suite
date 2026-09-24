package documenthubstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func liveAllowCount(t *testing.T, s *Store, tenant, docID, subject, action string) int {
	t.Helper()
	var n int
	err := s.RunTenantTx(context.Background(), tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM document_grant WHERE tenant_id=$1 AND document_id=$2 AND subject_kind='person' AND subject_id=$3 AND action=$4 AND effect='allow' AND revoked=false`, tenant, docID, subject, action).Scan(&n)
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func folderCount(lib Library, id string) int {
	for _, f := range lib.Folders {
		if f.ID == id {
			return f.DocumentCount
		}
	}
	return -1
}

func TestDocumentLibraryFoldersStarsAccess_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	const tenant, owner, reader, stranger = "tenant-library", "owner-lib", "reader-lib", "stranger-lib"
	onboarding, _, err := s.CreatePersonalDocument(ctx, tenant, owner, "Onboarding checklist", "# Onboarding\nBadge and laptop\n")
	if err != nil {
		t.Fatal(err)
	}
	payroll, _, err := s.CreatePersonalDocument(ctx, tenant, owner, "Payroll runbook", "# Payroll\nClose steps\n")
	if err != nil {
		t.Fatal(err)
	}
	draft, _, err := s.CreatePersonalDocument(ctx, tenant, owner, "Handbook draft", "# Draft\nUnreleased handbook terms\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SharePersonalDocumentRole(ctx, tenant, onboarding, owner, reader, RoleViewer); err != nil {
		t.Fatal(err)
	}
	if err := s.SharePersonalDocumentRole(ctx, tenant, payroll, owner, reader, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SharePersonalDocumentRole(ctx, tenant, payroll, owner, stranger, "editor"); !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("unknown role = %v", err)
	}

	lib, err := s.GetLibrary(ctx, tenant, reader)
	if err != nil || lib.All != 2 || lib.Mine != 0 || lib.SharedWithMe != 2 || lib.Starred != 0 || len(lib.Folders) != 0 {
		t.Fatalf("reader library = %+v, %v", lib, err)
	}
	lib, err = s.GetLibrary(ctx, tenant, owner)
	if err != nil || lib.All != 3 || lib.Mine != 3 || lib.SharedWithMe != 0 {
		t.Fatalf("owner library = %+v, %v", lib, err)
	}

	// Folder names are trimmed, bounded and unique per person without case.
	folder, err := s.CreateFolder(ctx, tenant, reader, "  Policies ")
	if err != nil || folder.Name != "Policies" || folder.ID == "" || folder.CreatedAt.IsZero() {
		t.Fatalf("create folder = %+v, %v", folder, err)
	}
	if _, err := s.CreateFolder(ctx, tenant, reader, "policies"); !errors.Is(err, ErrFolderNameTaken) {
		t.Fatalf("duplicate folder = %v", err)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("x", 81)} {
		if _, err := s.CreateFolder(ctx, tenant, reader, bad); !errors.Is(err, ErrFolderNameInvalid) {
			t.Fatalf("folder name %q = %v", bad, err)
		}
	}
	if _, err := s.CreateFolder(ctx, tenant, reader, strings.Repeat("é", 80)); err != nil {
		t.Fatalf("80-character name refused: %v", err)
	}
	ownerFolder, err := s.CreateFolder(ctx, tenant, owner, "Policies")
	if err != nil {
		t.Fatalf("another person's same-named folder refused: %v", err)
	}

	// Filing needs a folder the actor owns and documents the actor can read.
	if err := s.MoveDocuments(ctx, tenant, reader, []string{onboarding, payroll, onboarding}, folder.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.MoveDocuments(ctx, tenant, reader, []string{onboarding, draft}, folder.ID); !errors.Is(err, ErrDenied) {
		t.Fatalf("filed an unreadable document = %v", err)
	}
	if err := s.MoveDocuments(ctx, tenant, reader, []string{onboarding}, ownerFolder.ID); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("filed into another person's folder = %v", err)
	}
	tooMany := make([]string, MaxMoveDocuments+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("doc-%d", i)
	}
	if err := s.MoveDocuments(ctx, tenant, reader, tooMany, folder.ID); !errors.Is(err, ErrTooManyDocuments) {
		t.Fatalf("oversized move = %v", err)
	}
	if err := s.MoveDocuments(ctx, tenant, owner, []string{onboarding}, ownerFolder.ID); err != nil {
		t.Fatal(err)
	}

	// Stars are idempotent and need read access.
	for i := 0; i < 2; i++ {
		if err := s.SetStarred(ctx, tenant, reader, onboarding, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetStarred(ctx, tenant, reader, draft, true); !errors.Is(err, ErrDenied) {
		t.Fatalf("starred an unreadable document = %v", err)
	}
	if err := s.SetStarred(ctx, tenant, reader, "doc-missing", true); !errors.Is(err, ErrDenied) {
		t.Fatalf("starred a missing document = %v", err)
	}

	inFolder, err := s.ListPersonalDocumentsPage(ctx, tenant, reader, ListOptions{FolderID: folder.ID})
	if err != nil || len(inFolder) != 2 || inFolder[0].FolderID != folder.ID || inFolder[1].FolderID != folder.ID {
		t.Fatalf("folder listing = %+v, %v", inFolder, err)
	}
	if total, err := s.CountPersonalDocuments(ctx, tenant, reader, ListOptions{FolderID: folder.ID, Limit: 1}); err != nil || total != 2 {
		t.Fatalf("folder total = %d, %v", total, err)
	}
	starred, err := s.ListPersonalDocumentsPage(ctx, tenant, reader, ListOptions{Starred: true})
	if err != nil || len(starred) != 1 || starred[0].ID != onboarding || !starred[0].Starred {
		t.Fatalf("starred listing = %+v, %v", starred, err)
	}
	// Another person's folders never narrow or reveal anything.
	if leaked, err := s.ListPersonalDocumentsPage(ctx, tenant, owner, ListOptions{FolderID: folder.ID}); err != nil || len(leaked) != 0 {
		t.Fatalf("owner listed the reader's folder = %+v, %v", leaked, err)
	}
	ownerRows, err := s.ListPersonalDocumentsPage(ctx, tenant, owner, ListOptions{})
	if err != nil || len(ownerRows) != 3 {
		t.Fatalf("owner rows = %+v, %v", ownerRows, err)
	}
	for _, row := range ownerRows {
		if row.Starred || (row.ID != onboarding && row.FolderID != "") {
			t.Fatalf("owner saw the reader's organization: %+v", row)
		}
		wantReaders := map[string]int{onboarding: 1, payroll: 1, draft: 0}[row.ID]
		if row.ReaderCount != wantReaders || row.Shared != (wantReaders > 0) {
			t.Fatalf("owner reader count = %+v; want %d", row, wantReaders)
		}
	}
	for _, row := range inFolder {
		if row.ReaderCount != 0 || row.CanManage {
			t.Fatalf("reader saw the audience size: %+v", row)
		}
	}
	lib, err = s.GetLibrary(ctx, tenant, reader)
	if err != nil || lib.Starred != 1 || len(lib.Folders) != 2 || folderCount(lib, folder.ID) != 2 {
		t.Fatalf("reader library after filing = %+v, %v", lib, err)
	}
	if got, err := s.DocumentPlacement(ctx, tenant, reader, onboarding); err != nil || !got.Starred || got.FolderID != folder.ID || got.ReaderCount != 1 {
		t.Fatalf("reader placement = %+v, %v", got, err)
	}
	if got, err := s.DocumentPlacement(ctx, tenant, owner, onboarding); err != nil || got.Starred || got.FolderID != ownerFolder.ID {
		t.Fatalf("owner placement = %+v, %v", got, err)
	}
	if _, err := s.DocumentPlacement(ctx, tenant, reader, draft); !errors.Is(err, ErrDenied) {
		t.Fatalf("placement of an unreadable document = %v", err)
	}

	// Access listing is for the owner or a manager only.
	access, err := s.ListAccess(ctx, tenant, owner, onboarding)
	if err != nil || len(access) != 2 || access[0].Role != "owner" || access[0].Removable || access[1].SubjectID != reader || access[1].Role != RoleViewer || !access[1].Removable {
		t.Fatalf("onboarding access = %+v, %v", access, err)
	}
	access, err = s.ListAccess(ctx, tenant, owner, payroll)
	if err != nil || len(access) != 2 || access[1].Role != RoleCommenter {
		t.Fatalf("payroll access = %+v, %v", access, err)
	}
	if _, err := s.ListAccess(ctx, tenant, reader, onboarding); !errors.Is(err, ErrDenied) {
		t.Fatalf("reader listed access = %v", err)
	}
	if _, err := s.ListAccess(ctx, tenant, owner, "doc-missing"); !errors.Is(err, ErrDenied) {
		t.Fatalf("missing document access = %v", err)
	}
	if _, err := s.ListAccess(ctx, "tenant-other", owner, onboarding); !errors.Is(err, ErrDenied) {
		t.Fatalf("cross-tenant access = %v", err)
	}

	// Re-sharing changes the role in place and never duplicates grants.
	if err := s.SharePersonalDocumentRole(ctx, tenant, onboarding, owner, reader, RoleCommenter); err != nil {
		t.Fatal(err)
	}
	if err := s.SharePersonalDocumentRole(ctx, tenant, onboarding, owner, reader, RoleCommenter); err != nil {
		t.Fatal(err)
	}
	if n, c := liveAllowCount(t, s, tenant, onboarding, reader, ActionRead), liveAllowCount(t, s, tenant, onboarding, reader, ActionComment); n != 1 || c != 1 {
		t.Fatalf("commenter re-share grants read=%d comment=%d", n, c)
	}
	if err := s.Authorize(ctx, tenant, onboarding, "person", reader, ActionComment); err != nil {
		t.Fatalf("upgraded reader cannot comment: %v", err)
	}
	if err := s.SharePersonalDocumentRole(ctx, tenant, payroll, owner, reader, RoleViewer); err != nil {
		t.Fatal(err)
	}
	if n, c := liveAllowCount(t, s, tenant, payroll, reader, ActionRead), liveAllowCount(t, s, tenant, payroll, reader, ActionComment); n != 1 || c != 0 {
		t.Fatalf("viewer re-share grants read=%d comment=%d", n, c)
	}
	if err := s.Authorize(ctx, tenant, payroll, "person", reader, ActionComment); !errors.Is(err, ErrDenied) {
		t.Fatalf("downgraded reader can still comment: %v", err)
	}
	access, err = s.ListAccess(ctx, tenant, owner, payroll)
	if err != nil || len(access) != 2 || access[1].Role != RoleViewer {
		t.Fatalf("payroll access after downgrade = %+v, %v", access, err)
	}

	// Revocation is for managers, never removes the owner, and hides the
	// document from the removed person's folders and stars.
	if err := s.RevokePersonAccess(ctx, tenant, reader, onboarding, owner); !errors.Is(err, ErrDenied) {
		t.Fatalf("reader revoked access = %v", err)
	}
	if err := s.RevokePersonAccess(ctx, tenant, owner, onboarding, owner); !errors.Is(err, ErrOwnerAccess) {
		t.Fatalf("owner removed = %v", err)
	}
	if err := s.RevokePersonAccess(ctx, tenant, owner, onboarding, reader); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokePersonAccess(ctx, tenant, owner, onboarding, reader); err != nil {
		t.Fatalf("repeat revoke = %v", err)
	}
	if n, c := liveAllowCount(t, s, tenant, onboarding, reader, ActionRead), liveAllowCount(t, s, tenant, onboarding, reader, ActionComment); n != 0 || c != 0 {
		t.Fatalf("revoked grants left read=%d comment=%d", n, c)
	}
	if _, _, err := s.ReadPersonalDocument(ctx, tenant, reader, onboarding); !errors.Is(err, ErrDenied) {
		t.Fatalf("revoked reader opened document = %v", err)
	}
	lib, err = s.GetLibrary(ctx, tenant, reader)
	if err != nil || lib.All != 1 || lib.Starred != 0 || folderCount(lib, folder.ID) != 1 {
		t.Fatalf("reader library after revoke = %+v, %v", lib, err)
	}
	if access, err := s.ListAccess(ctx, tenant, owner, onboarding); err != nil || len(access) != 1 {
		t.Fatalf("access after revoke = %+v, %v", access, err)
	}

	// Rename and delete touch only the actor's own folders; deleting never
	// deletes documents.
	if err := s.RenameFolder(ctx, tenant, reader, folder.ID, "HR policies"); err != nil {
		t.Fatal(err)
	}
	if err := s.RenameFolder(ctx, tenant, reader, folder.ID, "hr POLICIES"); err != nil {
		t.Fatalf("case-only rename refused: %v", err)
	}
	if err := s.RenameFolder(ctx, tenant, reader, folder.ID, strings.Repeat("é", 80)); !errors.Is(err, ErrFolderNameTaken) {
		t.Fatalf("rename onto a taken name = %v", err)
	}
	if err := s.RenameFolder(ctx, tenant, reader, ownerFolder.ID, "Mine now"); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("renamed another person's folder = %v", err)
	}
	if err := s.DeleteFolder(ctx, tenant, reader, ownerFolder.ID); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("deleted another person's folder = %v", err)
	}
	if err := s.DeleteFolder(ctx, tenant, reader, folder.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteFolder(ctx, tenant, reader, folder.ID); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("repeat delete = %v", err)
	}
	rows, err := s.ListPersonalDocumentsPage(ctx, tenant, reader, ListOptions{})
	if err != nil || len(rows) != 1 || rows[0].ID != payroll || rows[0].FolderID != "" {
		t.Fatalf("documents after folder delete = %+v, %v", rows, err)
	}
	if err := s.MoveDocuments(ctx, tenant, owner, []string{onboarding}, ""); err != nil {
		t.Fatal(err)
	}
	if rows, err := s.ListPersonalDocumentsPage(ctx, tenant, owner, ListOptions{FolderID: ownerFolder.ID}); err != nil || len(rows) != 0 {
		t.Fatalf("unfiled document still in folder = %+v, %v", rows, err)
	}
}

func TestDocumentListSortSearchOwner_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	const tenant, owner, other, reader = "tenant-sort", "owner-sort", "other-sort", "reader-sort"
	titles := []string{"delta plan", "Alpha guide", "charlie notes", "Bravo runbook", "echo FAQ"}
	for _, title := range titles {
		if _, _, err := s.CreatePersonalDocument(ctx, tenant, owner, title, "# "+title+"\nOnboarding details\n"); err != nil {
			t.Fatal(err)
		}
	}
	shared, _, err := s.CreatePersonalDocument(ctx, tenant, other, "Zulu benefits", "# Zulu\nBenefits enrollment\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SharePersonalDocumentRole(ctx, tenant, shared, other, owner, RoleViewer); err != nil {
		t.Fatal(err)
	}
	var walked []string
	options := ListOptions{Limit: 2, Sort: SortTitle}
	for page := 0; page < 5; page++ {
		rows, err := s.ListPersonalDocumentsPage(ctx, tenant, owner, options)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			walked = append(walked, row.Title)
		}
		if len(rows) < options.Limit {
			break
		}
		last := rows[len(rows)-1].TitleKey
		if last != strings.ToLower(rows[len(rows)-1].Title) {
			t.Fatalf("title key = %q for %q", last, rows[len(rows)-1].Title)
		}
		options.AfterTitle, options.BeforeID = &last, rows[len(rows)-1].ID
	}
	want := "Alpha guide,Bravo runbook,charlie notes,delta plan,echo FAQ,Zulu benefits"
	if got := strings.Join(walked, ","); got != want {
		t.Fatalf("title walk = %s; want %s", got, want)
	}
	if total, err := s.CountPersonalDocuments(ctx, tenant, owner, ListOptions{Sort: SortTitle, Limit: 2}); err != nil || total != 6 {
		t.Fatalf("title total = %d, %v", total, err)
	}
	byOwner, err := s.ListPersonalDocumentsPage(ctx, tenant, owner, ListOptions{OwnerID: other})
	if err != nil || len(byOwner) != 1 || byOwner[0].ID != shared {
		t.Fatalf("owner filter = %+v, %v", byOwner, err)
	}
	// Word-prefix search over the owner's own private drafts.
	found, err := s.ListPersonalDocumentsPage(ctx, tenant, owner, ListOptions{Query: "onboard", Collection: "private"})
	if err != nil || len(found) != 5 {
		t.Fatalf("prefix search = %+v, %v", found, err)
	}
	if total, err := s.CountPersonalDocuments(ctx, tenant, owner, ListOptions{Query: "alpha onboarding"}); err != nil || total != 1 {
		t.Fatalf("all-terms total = %d, %v", total, err)
	}
	if total, err := s.CountPersonalDocuments(ctx, tenant, owner, ListOptions{Query: "!!"}); err != nil || total != 0 {
		t.Fatalf("termless query total = %d, %v", total, err)
	}
	if rows, err := s.ListPersonalDocumentsPage(ctx, tenant, reader, ListOptions{Query: "onboard"}); err != nil || len(rows) != 0 {
		t.Fatalf("stranger searched private drafts = %+v, %v", rows, err)
	}
	if rows, err := s.ListPersonalDocumentsPage(ctx, tenant, owner, ListOptions{Query: "benefit"}); err != nil || len(rows) != 1 || rows[0].ID != shared {
		t.Fatalf("shared search = %+v, %v", rows, err)
	}
	if _, err := s.CountPersonalDocuments(ctx, tenant, "", ListOptions{}); !errors.Is(err, ErrDenied) {
		t.Fatalf("anonymous count = %v", err)
	}
	if _, err := s.GetLibrary(ctx, tenant, ""); !errors.Is(err, ErrDenied) {
		t.Fatalf("anonymous library = %v", err)
	}
}
