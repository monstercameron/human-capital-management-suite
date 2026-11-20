package agentinvocationstore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

// TestIssue3803_ConversationOutcomes_Integration reads every invoker's runs in
// one conversation, and only that conversation, and only for one tenant.
func TestIssue3803_ConversationOutcomes_Integration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	a, b := uuid.New(), uuid.New()
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant(tenant_id) VALUES ($1),($2)`, a, b); err != nil {
		t.Fatal(err)
	}
	login, password := roleName("outcomes"), uuid.NewString()
	if err := createLogin(ctx, db.SQL, login, password); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.SQL.ExecContext(context.Background(), "DROP ROLE "+login) })
	pool, err := agentstore.New(ctx, agentstore.Config{DSN: testDSN(t, db.URL, db.Schema, login, password, "postgres"), CoreDSN: "postgres://core:pw@127.0.0.1:5432/core", MaxConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store, err := New(pool)
	if err != nil {
		t.Fatal(err)
	}
	mine := invocation(a.String(), "p", "persona", "post-1", "ada")
	theirs := invocation(a.String(), "p", "persona", "post-2", "grace")
	elsewhere := invocation(a.String(), "p", "persona", "post-3", "ada")
	elsewhere.ConversationID, elsewhere.Actor.ConversationID = "other-room", "other-room"
	foreign := invocation(b.String(), "p", "persona", "post-4", "ada")
	for _, item := range []struct {
		name string
		do   func() error
	}{
		{"mine", func() error { _, _, err := store.Claim(ctx, mine); return err }},
		{"theirs", func() error { _, _, err := store.Claim(ctx, theirs); return err }},
		{"elsewhere", func() error { _, _, err := store.Claim(ctx, elsewhere); return err }},
		{"foreign", func() error { _, _, err := store.Claim(ctx, foreign); return err }},
	} {
		if err := item.do(); err != nil {
			t.Fatalf("claim %s: %v", item.name, err)
		}
	}
	rows, err := store.ListConversationOutcomeInvocations(ctx, a.String(), "conversation")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, row := range rows {
		got[row.PostID] = row.InvokerID
		if row.ConversationID != "conversation" || row.TenantID != a.String() || row.ID == "" || row.PersonaID == "" {
			t.Fatalf("row is incomplete or from elsewhere: %+v", row)
		}
	}
	if len(rows) != 2 || got["post-1"] != "ada" || got["post-2"] != "grace" {
		t.Fatalf("a conversation read must hold both people's runs and nothing else: %+v", rows)
	}
	// The other tenant's database scope never shows this tenant's rows.
	other, err := store.ListConversationOutcomeInvocations(ctx, b.String(), "conversation")
	if err != nil || len(other) != 1 || other[0].PostID != "post-4" || other[0].TenantID != b.String() {
		t.Fatalf("cross-tenant read=%+v %v", other, err)
	}
	if _, err := store.ListConversationOutcomeInvocations(ctx, a.String(), " "); err == nil {
		t.Fatal("a blank conversation must be refused")
	}
	// Only a refused admission is a shared outcome; every other failure code is
	// the asker's own business and never leaves their page.
	for _, failure := range []PostFailure{
		{TenantID: a.String(), InvokerID: "ada", ConversationID: "conversation", ThreadID: "post-5", PostID: "post-5", Code: "ADMISSION_REFUSED"},
		{TenantID: a.String(), InvokerID: "ada", ConversationID: "conversation", ThreadID: "post-6", PostID: "post-6", Code: "DAILY_LIMIT_REACHED"},
		{TenantID: a.String(), InvokerID: "ada", ConversationID: "other-room", ThreadID: "post-7", PostID: "post-7", Code: "ADMISSION_REFUSED"},
	} {
		if err := store.RecordPostFailure(ctx, failure); err != nil {
			t.Fatalf("record %s: %v", failure.PostID, err)
		}
	}
	refusals, err := store.ListConversationRefusals(ctx, a.String(), "conversation")
	if err != nil || len(refusals) != 1 || refusals[0].PostID != "post-5" || refusals[0].InvokerID != "ada" {
		t.Fatalf("refusals=%+v %v", refusals, err)
	}
}
