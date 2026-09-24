package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

func TestDocumentPruneValidation(t *testing.T) {
	for _, opts := range []documentPruneOptions{
		{Owner: "", TitlePrefix: "Bughunt doc "},
		{Owner: "hc-050-rafael-torres", TitlePrefix: "   "},
	} {
		if err := runDocumentPruneCommand(context.Background(), &documenthubstore.Store{}, opts, &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted %+v", opts)
		}
	}
	if err := runDocumentPruneCommand(context.Background(), nil, documentPruneOptions{Owner: "o", TitlePrefix: "p"}, &bytes.Buffer{}); err == nil {
		t.Fatal("nil store accepted")
	}
	fields := append(migrateConfigFields(), documentPruneFields()...)
	v, err := bootstrap.ParseConfig([]string{"-owner=hc-050-rafael-torres", "-title-prefix=Bughunt doc ", "-apply=true"}, func(string) (string, bool) { return "", false }, fields)
	if err != nil {
		t.Fatalf("parse prune flags: %v", err)
	}
	if apply, _ := v.Bool(fieldPruneApply); !apply || v.String(fieldPruneOwner) != "hc-050-rafael-torres" || v.String(fieldPruneTitlePrefix) != "Bughunt doc " {
		t.Fatalf("flags = %q %q", v.String(fieldPruneOwner), v.String(fieldPruneTitlePrefix))
	}
	if err := validateDocumentCommand("prune", "postgres://h/docs", "postgres://h/core", ""); err != nil {
		t.Fatalf("prune not a document command: %v", err)
	}
}

// TestDocumentPruneDryRunThenApply_Integration: the dry run lists and
// changes nothing; apply retires only the owner's prefixed documents,
// including a shared (published) one, and a rerun finds nothing.
func TestDocumentPruneDryRunThenApply_Integration(t *testing.T) {
	store := documentSeedStore(t)
	ctx := context.Background()
	const tenant, owner, other = "harborcare-demo", "hc-050-rafael-torres", "hc-051-person-51"
	create := func(who, title string) string {
		t.Helper()
		id, _, err := store.CreatePersonalDocument(ctx, tenant, who, title, "# "+title+"\n\nBody.")
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	private := create(owner, "Bughunt doc 1727000000000")
	shared := create(owner, "Bughunt doc 1727000000001")
	if err := store.SharePersonalDocumentRole(ctx, tenant, shared, owner, other, documenthubstore.RoleViewer); err != nil {
		t.Fatal(err)
	}
	keep := create(owner, "Bughunt notes, keep")
	othersDoc := create(other, "Bughunt doc 1727000000002")
	visible := func(who string) map[string]bool {
		t.Helper()
		rows, err := store.ListPersonalDocumentsPage(ctx, tenant, who, documenthubstore.ListOptions{Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, r := range rows {
			out[r.ID] = true
		}
		return out
	}
	opts := documentPruneOptions{Tenant: tenant, Owner: owner, TitlePrefix: "Bughunt doc "}
	var out bytes.Buffer
	if err := runDocumentPruneCommand(ctx, store, opts, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "2 documents") || !strings.Contains(out.String(), "dry run") || !strings.Contains(out.String(), "would retire "+private) || !strings.Contains(out.String(), "would retire "+shared) || strings.Contains(out.String(), keep) {
		t.Fatalf("dry run = %q", out.String())
	}
	if v := visible(owner); !v[private] || !v[shared] || !v[keep] {
		t.Fatalf("dry run changed the library: %v", v)
	}
	out.Reset()
	opts.Apply = true
	if err := runDocumentPruneCommand(ctx, store, opts, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "2 retired, 0 kept under hold") || !strings.Contains(out.String(), "(was published)") {
		t.Fatalf("apply = %q", out.String())
	}
	if v := visible(owner); v[private] || v[shared] || !v[keep] {
		t.Fatalf("owner library after apply: %v", v)
	}
	if v := visible(other); v[shared] || !v[othersDoc] {
		t.Fatalf("recipient library after apply: %v", v)
	}
	out.Reset()
	if err := runDocumentPruneCommand(ctx, store, opts, &out); err != nil || !strings.Contains(out.String(), "0 documents") {
		t.Fatalf("rerun = %q, %v", out.String(), err)
	}
}
