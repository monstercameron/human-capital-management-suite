package chatstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestTodo_CHATMOD_003_Integration(t *testing.T) {
	s := NewFilterStore(adapterDB(t).Store)
	ctx := t.Context()
	d := chatfilter.Definition{ID: "project", Name: "Project rule", Version: "1.0.0", Kind: "words", Match: []string{"quartz"}, Action: "mask"}
	if err := s.CreateVersion(ctx, "tenant-a", d); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateVersion(ctx, "tenant-a", d); !errors.Is(err, chatfilter.ErrConflict) {
		t.Fatal("duplicate version accepted", err)
	}
	d.Version = "1.1.0"
	if err := s.CreateVersion(ctx, "tenant-a", d); err != nil {
		t.Fatal(err)
	}
	defs, err := s.Definitions(ctx, "tenant-a")
	if err != nil || len(defs) != 2 {
		t.Fatalf("versions %+v %v", defs, err)
	}
	defs, err = s.Definitions(ctx, "tenant-b")
	if err != nil || len(defs) != 0 {
		t.Fatalf("tenant leaked %+v %v", defs, err)
	}
	until := time.Now().UTC().Add(7 * 24 * time.Hour)
	e := chatfilter.Enablement{RuleID: d.ID, Enabled: true, DryRunUntil: until}
	if err = s.PutEnablement(ctx, "tenant-a", e); err != nil {
		t.Fatal(err)
	}
	enabled, err := s.Enablements(ctx, "tenant-a")
	if err != nil || len(enabled) != 1 || !enabled[0].Enabled || enabled[0].DryRunUntil.Sub(until) > time.Second {
		t.Fatalf("enablement %+v %v", enabled, err)
	}
	e.Enabled = false
	if err = s.PutEnablement(ctx, "tenant-a", e); err != nil {
		t.Fatal(err)
	}
	enabled, err = s.Enablements(ctx, "tenant-a")
	if err != nil || enabled[0].Enabled {
		t.Fatal("disable failed", err)
	}
	compiled, _ := chatfilter.NewRegistry().Compile([]chatfilter.Definition{d})
	result, _ := compiled.Evaluate(chatfilter.Input{Body: "quartz"})
	record := chatfilter.Record{Tenant: "tenant-a", Channel: "c", Subject: "writer", At: time.Now().UTC(), Hit: result.Hits[0]}
	record.Hit.DryRun = true
	if err = s.RecordHits(ctx, "tenant-a", []chatfilter.Record{record}); err != nil {
		t.Fatal(err)
	}
	hits, err := s.Hits(ctx, "tenant-a")
	if err != nil || len(hits) != 1 || hits[0].Hit.Version != "1.1.0" || !hits[0].Hit.DryRun {
		t.Fatalf("hits %+v %v", hits, err)
	}
	if strings.Contains(hits[0].Hit.Masked, "quartz") {
		t.Fatal("secret retained")
	}
	batch := make([]chatfilter.Record, 205)
	for i := range batch {
		batch[i] = record
	}
	if err = s.RecordHits(ctx, "tenant-a", batch); err != nil {
		t.Fatal(err)
	}
	page, err := s.SearchHitRecords(ctx, "tenant-a", "project", "", 0)
	if err != nil || len(page) != 200 || page[199].ID == 0 {
		t.Fatalf("first hit page %+v %v", page, err)
	}
	older, err := s.SearchHitRecords(ctx, "tenant-a", "project", "", page[199].ID)
	if err != nil || len(older) != 6 || older[0].ID >= page[199].ID {
		t.Fatalf("older hits unavailable %+v %v", older, err)
	}
	if err = s.DeliverFilterHit(ctx, record); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"chat_filter_version", "chat_filter_hit"} {
		err = s.Store.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
			_, err := tx.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", "tenant-a")
			return err
		})
		if err == nil {
			t.Fatal("immutable history mutated", table)
		}
	}
}
func TestTodo_CHATMOD_003_Security(t *testing.T) {
	s := NewFilterStore(adapterDB(t).Store)
	ctx := t.Context()
	var schema string
	if err := s.Store.RunTx(ctx, func(tx dbport.Tx) error { return tx.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema) }); err != nil {
		t.Fatal(err)
	}
	role := createChatRLSRole(t, s.Store, schema)
	err := runChatAsTenantRole(ctx, s.Store, role, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_filter_enablement(tenant_id,rule_id,enabled) VALUES('tenant-b','r',true)`)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "row-level security") {
		t.Fatal("RLS cross tenant insert accepted")
	}
	if err = s.RecordHits(ctx, "tenant-a", []chatfilter.Record{{Tenant: "tenant-b"}}); !errors.Is(err, chatfilter.ErrInvalid) {
		t.Fatal("cross tenant hit accepted", err)
	}
	for _, table := range []string{"chat_filter_version", "chat_filter_enablement", "chat_filter_hit"} {
		err = s.Store.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
			var enabled, forced bool
			if err := tx.QueryRow(ctx, `SELECT relrowsecurity,relforcerowsecurity FROM pg_class WHERE oid=$1::regclass`, table).Scan(&enabled, &forced); err != nil {
				return err
			}
			if !enabled || !forced {
				t.Fatal("RLS missing", table)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

type chatfilterSearchAuthority struct{ denied bool }

func (a chatfilterSearchAuthority) AuthorizeFilters(context.Context, chatfilter.Actor, string) error {
	if a.denied {
		return chatfilter.ErrDenied
	}
	return nil
}
func (a chatfilterSearchAuthority) CanReadFilterConversation(context.Context, chatfilter.Actor, string) bool {
	return true
}
func TestTodo_CHATMOD_002(t *testing.T) {
	adapter := adapterDB(t)
	repo := NewFilterStore(adapter.Store)
	ctx := t.Context()
	d := chatfilter.Definition{ID: "project", Name: "Project rule", Version: "1.0.0", Kind: "words", Match: []string{"quartz"}, Action: "mask"}
	if err := repo.CreateVersion(ctx, "tenant-a", d); err != nil {
		t.Fatal(err)
	}
	if err := repo.PutEnablement(ctx, "tenant-a", chatfilter.Enablement{RuleID: d.ID, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	c := chat.Conversation{ID: "c", TenantID: "tenant-a", Kind: chat.PublicChannel, Name: "General", OwnerID: "writer", Revision: 1}
	m := chat.Membership{TenantID: c.TenantID, HomeTenantID: c.TenantID, ConversationID: c.ID, SubjectID: "writer", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	if _, err := adapter.CreateConversation(ctx, c, []chat.Membership{m}, ""); err != nil {
		t.Fatal(err)
	}
	policy := &chat.FilterContentPolicy{Filters: &chatfilter.Service{Store: repo, Registry: chatfilter.NewRegistry()}, Conversations: adapter}
	core := chat.NewService(adapter, time.Now)
	core.SetAuthority(forwardingAuthority{store: adapter})
	core.SetContentPolicy(policy)
	post, err := core.SendPost(ctx, chat.SendPostRequest{Principal: chat.Principal{TenantID: c.TenantID, SubjectID: "writer"}, TenantID: c.TenantID, ConversationID: c.ID, Body: "hello quartz", IdempotencyKey: "original"})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := adapter.GetPost(ctx, c.TenantID, c.ID, post.ID)
	if err != nil || stored.Body != "hello quartz" {
		t.Fatalf("original altered: %+v %v", stored, err)
	}
	masked, err := policy.MaskedBody(ctx, stored, chat.Principal{TenantID: c.TenantID, SubjectID: "reader"})
	if err != nil || masked != "hello [removed word]" {
		t.Fatalf("reader mask %q %v", masked, err)
	}
	a := chatfilter.Actor{Tenant: c.TenantID, Subject: "admin"}
	defs, err := repo.SearchFilters(ctx, a, chatfilterSearchAuthority{}, "project")
	if err != nil || len(defs) != 1 {
		t.Fatal("search filters", err)
	}
	hits, err := repo.SearchFilterHits(ctx, a, chatfilterSearchAuthority{}, "project")
	if err != nil || len(hits) != 1 || hits[0].Hit.Masked != "[removed word]" {
		t.Fatal("search hits", err)
	}
	if _, err = repo.SearchFilters(ctx, a, chatfilterSearchAuthority{denied: true}, ""); !errors.Is(err, chatfilter.ErrDenied) {
		t.Fatal("unauthorized search")
	}
}
