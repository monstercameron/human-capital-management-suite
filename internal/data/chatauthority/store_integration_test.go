package chatauthority

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

type dbRunner struct{ db dbport.Beginner }

func (r dbRunner) RunTx(ctx context.Context, fn func(dbport.Tx) error) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestTodo_CHAT_012_Integration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	db.Exec(t, `CREATE TABLE chat_conversation (tenant_id text NOT NULL,id text NOT NULL,PRIMARY KEY (tenant_id,id))`)
	db.Exec(t, `CREATE TABLE chat_outbox (id bigserial PRIMARY KEY,tenant_id text NOT NULL,aggregate_id text NOT NULL,event_type text NOT NULL,payload jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT now())`)
	migration, err := chatstore.Migrations.ReadFile("migrations/00003_chat_authority.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := strings.SplitN(strings.SplitN(string(migration), "-- +goose Down", 2)[0], "-- +goose Up", 2)[1]
	if _, err := db.SQL.ExecContext(ctx, up); err != nil {
		t.Fatalf("chat authority migration: %v", err)
	}
	db.Exec(t, `INSERT INTO chat_conversation (tenant_id,id) VALUES ('host','conversation')`)
	s := New(dbRunner{db: db.Conn})
	if err := s.PutPolicy(ctx, "host", "conversation", []string{"manager"}, nil, nil, nil, chatpolicy.RolesAny, "internal", "US", 0, "host-admin"); err != nil {
		t.Fatal(err)
	}
	if err := s.PutPolicy(ctx, "host", "conversation", []string{"admin"}, nil, nil, nil, chatpolicy.RolesAll, "internal", "US", 1, "host-admin"); err != nil {
		t.Fatal(err)
	}
	if err := s.PutPolicy(ctx, "host", "conversation", nil, nil, nil, nil, chatpolicy.RolesAny, "internal", "US", 1, "host-admin"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale policy update: %v", err)
	}
	p, err := s.Policy(ctx, "host", "conversation")
	if err != nil || p.Revision != 2 || len(p.RequiredRoles) != 1 || p.RequiredRoles[0] != "admin" {
		t.Fatalf("policy=%+v err=%v", p, err)
	}
	grant, err := chatpolicy.ProposeGrant("grant", "conversation", "host", "consumer", "conversation", "internal", "US", 1, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Propose(ctx, "host", "host-admin", grant); err != nil {
		t.Fatal(err)
	}
	other := grant
	other.ID = "other"
	if err := s.Propose(ctx, "host", "host-admin", other); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate active proposal: %v", err)
	}
	if _, err := s.CurrentGrant(ctx, "host", "consumer", "conversation", time.Now()); !errors.Is(err, dbport.ErrNoRows) {
		t.Fatalf("unaccepted grant admitted: %v", err)
	}
	if err := s.Accept(ctx, "host", "consumer", "conversation", "grant", "consumer-admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	current, err := s.CurrentGrant(ctx, "host", "consumer", "conversation", time.Now())
	if err != nil || !current.Current(time.Now(), "conversation", "host", "consumer") {
		t.Fatalf("accepted grant=%+v err=%v", current, err)
	}
	channel := p
	channel.Enabled = true
	channel.Private = true
	principal := chatpolicy.Principal{ID: "consumer-worker", Tenant: "consumer", Active: true, Roles: []string{"admin"}, AuthorityRevision: 1}
	membership := chatpolicy.Membership{ConversationID: "conversation", PrincipalID: "consumer-worker", Tenant: "consumer", State: chatpolicy.MembershipCurrent, Revision: 1, JoinedAt: time.Now().Add(-time.Minute)}
	in := chatpolicy.Input{Principal: principal, Channel: channel, Membership: membership, HasMembership: true, Grant: current, HasGrant: true, Now: time.Now()}
	if _, err := chatpolicy.Evaluate(chatpolicy.ActionRead, in); err != nil {
		t.Fatalf("accepted consumer denied: %v", err)
	}
	if _, err := s.CurrentGrant(ctx, "host", "consumer", "conversation", grant.ExpiresAt.Add(time.Second)); !errors.Is(err, dbport.ErrNoRows) {
		t.Fatalf("expired grant admitted: %v", err)
	}
	if err := s.Revoke(ctx, "host", "consumer", "conversation", "grant", "consumer", "consumer-admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CurrentGrant(ctx, "host", "consumer", "conversation", time.Now()); !errors.Is(err, dbport.ErrNoRows) {
		t.Fatalf("revoked grant admitted: %v", err)
	}
	in.HasGrant = false
	if _, err := chatpolicy.Evaluate(chatpolicy.ActionRead, in); !errors.Is(err, chatpolicy.ErrNotAuthorized) {
		t.Fatalf("consumer admitted after revoke: %v", err)
	}
	var events int
	if err := db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM chat_outbox WHERE tenant_id='host' AND aggregate_id='conversation'`).Scan(&events); err != nil || events != 5 {
		t.Fatalf("committed audit events=%d err=%v", events, err)
	}
	var actorTenant string
	if err := db.SQL.QueryRowContext(ctx, `SELECT payload->>'actor_tenant' FROM chat_outbox WHERE event_type='chat.grant.accepted'`).Scan(&actorTenant); err != nil || actorTenant != "consumer" {
		t.Fatalf("consumer audit actor tenant=%q err=%v", actorTenant, err)
	}
	var conversationID string
	var eventSeq int64
	if err := db.SQL.QueryRowContext(ctx, `SELECT payload->>'conversation_id',(payload->>'event_sequence')::bigint FROM chat_outbox WHERE event_type='chat.grant.accepted'`).Scan(&conversationID, &eventSeq); err != nil || conversationID != "conversation" || eventSeq <= 0 {
		t.Fatalf("watchable payload keys missing: conversation_id=%q event_sequence=%d err=%v", conversationID, eventSeq, err)
	}
}
