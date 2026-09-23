package documenthubstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func seedDeployment(t *testing.T, s *Store, tenant, docID, versionID, scopeKind, scopeID string) {
	t.Helper()
	if err := s.RunTenantTx(context.Background(), tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO document_deployment(id,tenant_id,document_id,version_id,scope_kind,scope_id,deployer_id) VALUES($1,$2,$3,$4,$5,$6,'u-1')`,
			"dep-"+docID+"-"+scopeKind+"-"+scopeID+"-"+versionID, tenant, docID, versionID, scopeKind, scopeID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(context.Background(), `INSERT INTO document_active_pointer(tenant_id,document_id,scope_kind,scope_id,deployment_id,version_id) VALUES($1,$2,$3,$4,$5,$6)
			ON CONFLICT (tenant_id,document_id,scope_kind,scope_id) DO UPDATE SET deployment_id=EXCLUDED.deployment_id, version_id=EXCLUDED.version_id, updated_at=now()`,
			tenant, docID, scopeKind, scopeID, "dep-"+docID+"-"+scopeKind+"-"+scopeID+"-"+versionID, versionID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// TestTodo_HUB_007 is the PRIMARY test for HUB-007: scoped active pointers
// name exact deployed versions; candidates never leak into reader views and
// one placement never overwrites another.
func TestTodo_HUB_007(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-1", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v1, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-1", Markdown: "one\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveDeployment(ctx, "tenant-a", docID, "default", ""); !errors.Is(err, ErrNoDeployment) {
		t.Fatalf("undeployed document resolves: %v", err)
	}
	seedDeployment(t, s, "tenant-a", docID, v1.ID, "default", "")
	seedDeployment(t, s, "tenant-a", docID, v1.ID, "placement", "chan-A")
	v2, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-1", Markdown: "two\n"}, v1.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.ResolveDeployment(ctx, "tenant-a", docID, "default", "")
	if err != nil || got.VersionID != v1.ID {
		t.Fatalf("candidate changed the reader view: %+v err=%v", got, err)
	}
	seedDeployment(t, s, "tenant-a", docID, v2.ID, "placement", "chan-A")
	gotA, err := s.ResolveDeployment(ctx, "tenant-a", docID, "placement", "chan-A")
	if err != nil || gotA.VersionID != v2.ID {
		t.Fatalf("placement move failed: %+v err=%v", gotA, err)
	}
	gotDefault, err := s.ResolveDeployment(ctx, "tenant-a", docID, "default", "")
	if err != nil || gotDefault.VersionID != v1.ID {
		t.Fatalf("placement move overwrote the default pointer: %+v err=%v", gotDefault, err)
	}
	if _, err := s.ResolveDeployment(ctx, "tenant-a", docID, "placement", "chan-B"); !errors.Is(err, ErrNoDeployment) {
		t.Fatalf("unendorsed placement resolves: %v", err)
	}
	if _, err := s.ResolveDeployment(ctx, "tenant-b", docID, "default", ""); err == nil {
		t.Fatal("foreign tenant resolved a deployment")
	}
}

// TestTodo_HUB_007_Property is the PROPERTY test for HUB-007: over generated
// scope sets, resolving a scope always returns exactly its own recorded
// version, and recording a pointer preserves every other scope.
func TestTodo_HUB_007_Property(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	scopes := []struct{ kind, id string }{{"default", ""}, {"placement", "team-1"}, {"placement", "chan-1"}, {"placement", "chan-2"}}
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-1", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	versions := make([]Version, len(scopes))
	base := ""
	for i := range scopes {
		v, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-1", Markdown: "content\n"}, base)
		if err != nil {
			t.Fatal(err)
		}
		versions[i] = v
		base = v.ID
		seedDeployment(t, s, "tenant-a", docID, v.ID, scopes[i].kind, scopes[i].id)
	}
	for i, scope := range scopes {
		got, err := s.ResolveDeployment(ctx, "tenant-a", docID, scope.kind, scope.id)
		if err != nil || got.VersionID != versions[i].ID || got.ScopeKind != scope.kind || got.ScopeID != scope.id {
			t.Fatalf("scope %+v resolves to %+v, want version %q", scope, got, versions[i].ID)
		}
	}
}
