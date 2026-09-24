package documenthubstore

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// countingTx counts the statements run through it.
type countingTx struct {
	dbport.Tx
	statements int
}

func (c *countingTx) Query(ctx context.Context, sql string, args ...any) (dbport.Rows, error) {
	c.statements++
	return c.Tx.Query(ctx, sql, args...)
}

func (c *countingTx) QueryRow(ctx context.Context, sql string, args ...any) dbport.Row {
	c.statements++
	return c.Tx.QueryRow(ctx, sql, args...)
}

func (c *countingTx) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	c.statements++
	return c.Tx.Exec(ctx, sql, args...)
}

func TestDocumentLinkTargets_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	const tenant, owner, reader, stranger = "tenant-links", "owner-l", "reader-l", "stranger-l"
	shared, _, err := s.CreatePersonalDocument(ctx, tenant, owner, "Shared runbook", "# Shared runbook\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SharePersonalDocumentRole(ctx, tenant, shared, owner, reader, RoleViewer); err != nil {
		t.Fatal(err)
	}
	private, _, err := s.CreatePersonalDocument(ctx, tenant, owner, "Secret comp plan", "# Secret\n")
	if err != nil {
		t.Fatal(err)
	}
	foreign, _, err := s.CreatePersonalDocument(ctx, tenant, stranger, "Stranger notes", "# Notes\n")
	if err != nil {
		t.Fatal(err)
	}
	// The owner renames the shared document in a draft: the reader still
	// sees the published title, the owner the draft title.
	base, _, err := s.ReadPersonalDocument(ctx, tenant, owner, shared)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePersonalDocumentVersion(ctx, tenant, shared, owner, base.VersionID, "Shared runbook v2", "# Shared runbook v2\n"); err != nil {
		t.Fatal(err)
	}
	md := fmt.Sprintf("See [a](doc:%s), [b](doc:%s), [c](doc:%s), again [a](doc:%s), [gone](doc:doc-missing) and [bad](doc:../x).", shared, private, foreign, shared)
	got, err := s.LinkTargets(ctx, tenant, reader, md)
	if err != nil || len(got) != 4 {
		t.Fatalf("reader targets = %+v, %v", got, err)
	}
	want := []LinkTarget{{DocumentID: shared, Title: "Shared runbook", Readable: true}, {DocumentID: private}, {DocumentID: foreign}, {DocumentID: "doc-missing"}}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("reader target %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	ownerView, err := s.LinkTargets(ctx, tenant, owner, md)
	if err != nil || ownerView[0].Title != "Shared runbook v2" || !ownerView[1].Readable || ownerView[1].Title != "Secret comp plan" || ownerView[2].Readable || ownerView[2].Title != "" {
		t.Fatalf("owner targets = %+v, %v", ownerView, err)
	}
	if none, err := s.LinkTargets(ctx, tenant, reader, "No links here."); err != nil || len(none) != 0 {
		t.Fatalf("no links = %+v, %v", none, err)
	}
	if _, err := s.LinkTargets(ctx, tenant, "", md); err == nil {
		t.Fatal("anonymous targets resolved")
	}

	// Twenty links cost one statement, not twenty.
	var many []string
	var links strings.Builder
	for i := 0; i < 20; i++ {
		id, _, err := s.CreatePersonalDocument(ctx, tenant, owner, fmt.Sprintf("Linked %d", i), "# Linked\n")
		if err != nil {
			t.Fatal(err)
		}
		many = append(many, id)
		fmt.Fprintf(&links, "[l%d](doc:%s) ", i, id)
	}
	options, _, _ := normalizeListOptions(owner, ListOptions{})
	counter := &countingTx{}
	err = s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		counter.Tx = tx
		titles, err := readableTitlesTx(ctx, counter, tenant, owner, options, many)
		if err == nil && len(titles) != 20 {
			return fmt.Errorf("resolved %d of 20", len(titles))
		}
		return err
	})
	if err != nil || counter.statements != 1 {
		t.Fatalf("20 links took %d statements, %v", counter.statements, err)
	}
	all, err := s.LinkTargets(ctx, tenant, owner, links.String())
	if err != nil || len(all) != 20 || !all[19].Readable {
		t.Fatalf("20 targets = %d, %v", len(all), err)
	}
}
