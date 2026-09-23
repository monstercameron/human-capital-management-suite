package documenthubstore

import (
	"context"
	"errors"
	"testing"
)

func TestPersonalDocumentRead_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	const tenant, owner, reader = "tenant-a", "owner-a", "reader-a"
	id, first, err := s.CreatePersonalDocument(ctx, tenant, owner, "First guide", "# First guide\n")
	if err != nil {
		t.Fatal(err)
	}
	read := func(actor, title, markdown, versionID string) {
		t.Helper()
		summary, version, err := s.ReadPersonalDocument(ctx, tenant, actor, id)
		if err != nil || summary.Title != title || summary.VersionID != versionID || version.Markdown != markdown || version.Hash != HashContent(markdown) {
			t.Fatalf("read %s = %+v %+v %v; want %q %q %q", actor, summary, version, err, title, markdown, versionID)
		}
	}
	denied := func(tenantID, actor, documentID string) {
		t.Helper()
		if _, _, err := s.ReadPersonalDocument(ctx, tenantID, actor, documentID); !errors.Is(err, ErrDenied) {
			t.Fatalf("read %s/%s/%s = %v; want denied", tenantID, actor, documentID, err)
		}
	}
	read(owner, "First guide", "# First guide\n", first.ID)
	denied(tenant, reader, id)
	denied("tenant-b", owner, id)
	denied(tenant, owner, "doc-missing")
	if _, err := s.ShareDocument(ctx, tenant, id, owner, GrantInput{SubjectID: reader, Action: ActionRead, Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}
	denied(tenant, reader, id)
	if _, err := s.RecordReview(ctx, tenant, ReviewInput{DocumentID: id, VersionID: first.ID, ScopeKind: "default", ReviewerID: "reviewer", Authority: "personal", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Deploy(ctx, tenant, DeployInput{DocumentID: id, VersionID: first.ID, ScopeKind: "default", DeployerID: owner}); err != nil {
		t.Fatal(err)
	}
	read(reader, "First guide", "# First guide\n", first.ID)
	second, err := s.SubmitCandidate(ctx, tenant, Version{DocumentID: id, CreatorID: owner, Title: "Private revision", Markdown: "# Private revision\n"}, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	read(owner, "Private revision", "# Private revision\n", second.ID)
	read(reader, "First guide", "# First guide\n", first.ID)
	if _, err := s.GrantAction(ctx, tenant, GrantInput{DocumentID: id, SubjectID: reader, Action: ActionRead, Effect: EffectDeny, Issuer: owner}); err != nil {
		t.Fatal(err)
	}
	denied(tenant, reader, id)
}
