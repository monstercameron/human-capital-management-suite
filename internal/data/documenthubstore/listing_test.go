package documenthubstore

import (
	"context"
	"fmt"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestPersonalDocumentKeysetSearch_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	const tenant, owner = "tenant-page", "owner-page"
	for i := 0; i < 112; i++ {
		title := fmt.Sprintf("Guide %03d", i)
		if i == 4 {
			title = "Needle policy"
		}
		if _, _, err := s.CreatePersonalDocument(ctx, tenant, owner, title, "# Body\n"); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.ListPersonalDocumentsPage(ctx, tenant, owner, ListOptions{Limit: 51, Collection: "private"})
	if err != nil || len(first) != 51 {
		t.Fatalf("first page = %d, %v", len(first), err)
	}
	if _, _, err := s.CreatePersonalDocument(ctx, tenant, owner, "New head", "# New\n"); err != nil {
		t.Fatal(err)
	}
	last := first[len(first)-1]
	second, err := s.ListPersonalDocumentsPage(ctx, tenant, owner, ListOptions{Limit: 51, Collection: "private", BeforeTime: last.UpdatedAt, BeforeID: last.ID})
	if err != nil || len(second) != 51 {
		t.Fatalf("second page = %d, %v", len(second), err)
	}
	last = second[len(second)-1]
	third, err := s.ListPersonalDocumentsPage(ctx, tenant, owner, ListOptions{Limit: 51, Collection: "private", BeforeTime: last.UpdatedAt, BeforeID: last.ID})
	if err != nil || len(third) != 10 {
		t.Fatalf("third page = %d, %v", len(third), err)
	}
	seen := map[string]bool{}
	for _, row := range append(append(first, second...), third...) {
		if seen[row.ID] || row.Title == "New head" {
			t.Fatalf("duplicate or inserted row: %+v", row)
		}
		seen[row.ID] = true
	}
	if len(seen) != 112 {
		t.Fatalf("walked %d of 112 original documents", len(seen))
	}
	found, err := s.ListPersonalDocumentsPage(ctx, tenant, owner, ListOptions{Limit: 51, Collection: "private", Query: "Needle"})
	if err != nil || len(found) != 0 {
		t.Fatalf("private document searchable = %+v, %v", found, err)
	}
	other, err := s.ListPersonalDocumentsPage(ctx, tenant, "other", ListOptions{Limit: 51, Query: "Needle"})
	if err != nil || len(other) != 0 {
		t.Fatalf("unauthorized search = %+v, %v", other, err)
	}
}

func TestTodo_HUB_012_PersonalListing_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	tenant := "tenant-a"
	owner, reader := "owner-a", "reader-a"
	id, err := s.CreateDocument(ctx, tenant, owner, "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	assertList := func(actor string, count int, title string) {
		t.Helper()
		rows, err := s.ListPersonalDocuments(ctx, tenant, actor, 20)
		if err != nil || len(rows) != count {
			t.Fatalf("list %s = %+v, %v; want %d", actor, rows, err, count)
		}
		if count > 0 && (rows[0].ID != id || rows[0].Title != title) {
			t.Fatalf("list %s = %+v; want %q", actor, rows[0], title)
		}
	}
	assertList(owner, 1, "")
	assertList(reader, 0, "")
	v, err := s.SubmitCandidate(ctx, tenant, Version{DocumentID: id, CreatorID: owner, Title: "Owner draft", Markdown: "# Draft\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	assertList(owner, 1, "Owner draft")
	if _, err := s.ShareDocument(ctx, tenant, id, owner, GrantInput{SubjectID: reader, Action: ActionRead, Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}
	assertList(reader, 0, "") // A grant does not publish a candidate.
	if _, err := s.RecordReview(ctx, tenant, ReviewInput{DocumentID: id, VersionID: v.ID, ScopeKind: "default", ReviewerID: "reviewer", Authority: "personal", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Deploy(ctx, tenant, DeployInput{DocumentID: id, VersionID: v.ID, ScopeKind: "default", DeployerID: owner}); err != nil {
		t.Fatal(err)
	}
	assertList(reader, 1, "Owner draft")
	if _, err := s.SubmitCandidate(ctx, tenant, Version{DocumentID: id, CreatorID: owner, Title: "Secret next draft", Markdown: "# Secret\n"}, v.ID); err != nil {
		t.Fatal(err)
	}
	assertList(owner, 1, "Secret next draft")
	assertList(reader, 1, "Owner draft")
	if _, err := s.GrantAction(ctx, tenant, GrantInput{DocumentID: id, SubjectID: reader, Action: ActionRead, Effect: EffectDeny, Issuer: owner}); err != nil {
		t.Fatal(err)
	}
	assertList(reader, 0, "")
	assertList(owner, 1, "Secret next draft")
}

func TestPersonalDocumentCreate_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	id, v, err := s.CreatePersonalDocument(ctx, "tenant-a", "owner-a", "First guide", "# First guide\n")
	if err != nil || id == "" || v.ID == "" || v.DocumentID != id {
		t.Fatalf("create = %q %+v %v", id, v, err)
	}
	rows, err := s.ListPersonalDocuments(ctx, "tenant-a", "owner-a", 10)
	if err != nil || len(rows) != 1 || rows[0].Title != "First guide" || rows[0].VersionID != v.ID {
		t.Fatalf("creator list = %+v %v", rows, err)
	}
	other, err := s.ListPersonalDocuments(ctx, "tenant-a", "other", 10)
	if err != nil || len(other) != 0 {
		t.Fatalf("private document leaked = %+v %v", other, err)
	}
	if _, _, err := s.CreatePersonalDocument(ctx, "tenant-a", "owner-a", "", "# No title"); err == nil {
		t.Fatal("blank title accepted")
	}
}

func TestTodo_HUB_012_OwnerShareSearch_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	const tenant, owner, reader = "tenant-share-ui", "owner-ui", "reader-ui"
	id, version, err := s.CreatePersonalDocument(ctx, tenant, owner, "Orientation guide", "# Benefits\nCurrent terms\n")
	if err != nil {
		t.Fatal(err)
	}
	assertSearch := func(actor string, want int) {
		t.Helper()
		rows, err := s.ListPersonalDocumentsPage(ctx, tenant, actor, ListOptions{Limit: 10, Query: "Orientation"})
		if err != nil || len(rows) != want {
			t.Fatalf("search as %s = %+v, %v; want %d", actor, rows, err, want)
		}
	}
	assertSearch(owner, 0)
	if err := s.SharePersonalDocument(ctx, tenant, id, reader, "stranger"); err != ErrDenied {
		t.Fatalf("reader shared owner's draft: %v", err)
	}
	if _, _, err := s.ReadPersonalDocument(ctx, tenant, reader, id); err != ErrDenied {
		t.Fatalf("reader opened private URI: %v", err)
	}
	if err := s.SharePersonalDocument(ctx, tenant, id, owner, reader); err != nil {
		t.Fatal(err)
	}
	assertSearch(owner, 1)
	assertSearch(reader, 1)
	row, readVersion, err := s.ReadPersonalDocument(ctx, tenant, reader, id)
	if err != nil || row.OwnerID != owner || readVersion.ID != version.ID || !row.Shared || row.CanManage {
		t.Fatalf("shared read = %+v %+v, %v", row, readVersion, err)
	}
	if err := s.SharePersonalDocument(ctx, tenant, id, owner, "second-reader"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubmitCandidate(ctx, tenant, Version{DocumentID: id, CreatorID: owner, Title: "Secret revision", Markdown: "# Secret\nUnpublished terms\n"}, version.ID); err != nil {
		t.Fatal(err)
	}
	secret, err := s.ListPersonalDocumentsPage(ctx, tenant, owner, ListOptions{Limit: 10, Query: "Secret"})
	if err != nil || len(secret) != 0 {
		t.Fatalf("private revision searched = %+v, %v", secret, err)
	}
	assertSearch(owner, 1)
	_, readVersion, err = s.ReadPersonalDocument(ctx, tenant, reader, id)
	if err != nil || readVersion.ID != version.ID {
		t.Fatalf("reader saw draft version = %+v, %v", readVersion, err)
	}
	if _, _, err := s.CreatePersonalDocument(ctx, tenant, owner, "Private next", "# Private\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, tenant, GrantInput{DocumentID: id, SubjectID: reader, Action: ActionRead, Effect: EffectDeny, Issuer: owner}); err != nil {
		t.Fatal(err)
	}
	assertSearch(reader, 0)
	if _, _, err := s.ReadPersonalDocument(ctx, tenant, reader, id); err != ErrDenied {
		t.Fatalf("denied reader opened URI: %v", err)
	}
	if err := s.SharePersonalDocument(ctx, tenant, id, owner, reader); err != ErrDenied {
		t.Fatalf("reshare over active deny = %v", err)
	}
	if _, err := s.GrantAction(ctx, tenant, GrantInput{DocumentID: id, SubjectID: owner, Action: ActionManage, Effect: EffectDeny, Issuer: owner}); err != nil {
		t.Fatal(err)
	}
	if err := s.SharePersonalDocument(ctx, tenant, id, owner, "third-reader"); err != ErrDenied {
		t.Fatalf("owner shared after manage deny = %v", err)
	}
	row, _, err = s.ReadPersonalDocument(ctx, tenant, owner, id)
	if err != nil || row.CanManage {
		t.Fatalf("owner manage projection after deny = %+v, %v", row, err)
	}
}

func TestTodo_HUB_019_CreateStoresDocumentLinks_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	const tenant = "tenant-linked-create"
	target, _, err := s.CreatePersonalDocument(ctx, tenant, "owner", "Target", "# Target\n")
	if err != nil {
		t.Fatal(err)
	}
	source, version, err := s.CreatePersonalDocument(ctx, tenant, "owner", "Source", "See [the target](doc:"+target+").\n")
	if err != nil {
		t.Fatal(err)
	}
	var got string
	err = s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT target_document_id FROM document_link WHERE tenant_id=$1 AND source_document_id=$2 AND source_version_id=$3`, tenant, source, version.ID).Scan(&got)
	})
	if err != nil || got != target {
		t.Fatalf("stored link target = %q, %v; want %q", got, err, target)
	}
}
