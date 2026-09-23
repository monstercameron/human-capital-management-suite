package documenthubstore

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestTodo_HUB_038 is the PRIMARY test for HUB-038: an authorized export
// carries scoped Markdown, the version/deployment manifest, artifact refs
// and the stable link map with records policy, including held versions.
func TestTodo_HUB_038(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docB, _ := linkDoc(t, s, ctx, "tenant-a", "# Bee\n\nHoney.\n")
	// The official deployer must be able to read every linked target under
	// current policy, even though the target's bytes are not part of export.
	grantRead(t, s, ctx, "tenant-a", docB, "u-deployer")
	docA, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v1 := searchFixture(t, s, ctx, "tenant-a", docA, "See [bee](doc:"+docB+").\n")
	if err := s.StoreLinks(ctx, "tenant-a", docA, v1.ID, ExtractLinks("See [bee](doc:"+docB+").\n")); err != nil {
		t.Fatal(err)
	}
	v2, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docA, CreatorID: "u-author", Title: "Guide", Markdown: "See [bee](doc:" + docB + ").\n\nMore.\n"}, v1.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{ActionRead, ActionPropose, ActionExport} {
		if _, err := s.ShareDocument(ctx, "tenant-a", docA, "u-author", GrantInput{SubjectKind: "person", SubjectID: "u-anna", Action: action, Effect: EffectAllow}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AttachArtifact(ctx, "tenant-a", AttachmentInput{
		DocumentID: docA, VersionID: v1.ID, ActorID: "u-anna",
		ArtifactID: "sha256:fig", Filename: "fig.png", ContentType: "image/png",
		SizeBytes: 9, QuarantineState: "ADMITTED", ScannerVersion: "scan-7",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PlaceHold(ctx, "tenant-a", docA, "u-author", "audit-7"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExportDocument(ctx, "tenant-a", docA, "person", "u-stranger"); !errors.Is(err, ErrDenied) {
		t.Fatalf("export without grant: %v", err)
	}
	bundle, err := s.ExportDocument(ctx, "tenant-a", docA, "person", "u-anna")
	if err != nil {
		t.Fatalf("authorized export refused: %v", err)
	}
	if len(bundle.Versions) != 2 {
		t.Fatalf("held versions dropped: %+v", bundle)
	}
	for _, v := range bundle.Versions {
		if v.Markdown == "" || v.Hash == "" || v.Status == "" {
			t.Fatalf("version lost provenance: %+v", v)
		}
	}
	_ = v2
	if len(bundle.Deployments) != 1 || bundle.Deployments[0].VersionID != v1.ID {
		t.Fatalf("deployment manifest wrong: %+v", bundle.Deployments)
	}
	if len(bundle.Attachments) != 1 || bundle.Attachments[0].ArtifactID != "sha256:fig" {
		t.Fatalf("artifact refs wrong: %+v", bundle.Attachments)
	}
	if len(bundle.Links) == 0 || bundle.Links[0].TargetDocID != docB {
		t.Fatalf("stable link map wrong: %+v", bundle.Links)
	}
	if len(bundle.Holds) != 1 || !bundle.Holds[0].Active {
		t.Fatalf("hold state missing: %+v", bundle.Holds)
	}
	if bundle.RecordsPolicy == "" {
		t.Fatalf("records policy missing: %+v", bundle)
	}
	for _, blob := range []string{bundle.Versions[0].Markdown, bundle.Versions[1].Markdown} {
		if strings.Contains(blob, "Honey.") {
			t.Fatalf("inaccessible target bytes disclosed: %q", blob)
		}
	}
}

// TestTodo_HUB_038_Security is the SECURITY test for HUB-038: read alone
// never exports, revocation stops export, and tenants stay separate.
func TestTodo_HUB_038_Security(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docA, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	searchFixture(t, s, ctx, "tenant-a", docA, "# Cats\n\nA guide to cats.\n")
	grantRead(t, s, ctx, "tenant-a", docA, "u-anna")
	if _, err := s.ExportDocument(ctx, "tenant-a", docA, "person", "u-anna"); !errors.Is(err, ErrDenied) {
		t.Fatalf("read-only export: %v", err)
	}
	g, err := s.ShareDocument(ctx, "tenant-a", docA, "u-author", GrantInput{SubjectKind: "person", SubjectID: "u-anna", Action: ActionExport, Effect: EffectAllow})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExportDocument(ctx, "tenant-a", docA, "person", "u-anna"); err != nil {
		t.Fatalf("export after grant refused: %v", err)
	}
	if err := s.RevokeGrant(ctx, "tenant-a", g.ID, "u-author"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExportDocument(ctx, "tenant-a", docA, "person", "u-anna"); !errors.Is(err, ErrDenied) {
		t.Fatalf("revoked export grant still exports: %v", err)
	}
	if _, err := s.ExportDocument(ctx, "tenant-b", docA, "person", "u-anna"); !errors.Is(err, ErrDenied) {
		t.Fatalf("cross-tenant export: %v", err)
	}
}

// TestTodo_HUB_038_Integration is the INTEGRATION test for HUB-038: the
// export roundtrip reaches the real store and reflects withdrawal in the
// deployment manifest while retaining versions.
func TestTodo_HUB_038_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docA, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v1 := searchFixture(t, s, ctx, "tenant-a", docA, "# Cats\n\nA guide to cats.\n")
	for _, action := range []string{ActionRead, ActionExport} {
		if _, err := s.ShareDocument(ctx, "tenant-a", docA, "u-author", GrantInput{SubjectKind: "person", SubjectID: "u-anna", Action: action, Effect: EffectAllow}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docA, SubjectKind: "person", SubjectID: "u-author", Action: ActionRetire, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Withdraw(ctx, "tenant-a", WithdrawInput{DocumentID: docA, ScopeKind: "default", ScopeID: "", ActorID: "u-author", ExpectedLive: v1.ID, Reason: "expiry"}); err != nil {
		t.Fatal(err)
	}
	bundle, err := s.ExportDocument(ctx, "tenant-a", docA, "person", "u-anna")
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Deployments) != 0 {
		t.Fatalf("withdrawn deployment still manifested: %+v", bundle.Deployments)
	}
	if len(bundle.Versions) != 1 || bundle.Versions[0].Status != "retired" {
		t.Fatalf("retired version not retained: %+v", bundle.Versions)
	}
}
