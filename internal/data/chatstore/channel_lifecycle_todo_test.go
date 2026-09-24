package chatstore

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestTodo_CHAT_014_Integration(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	c := chat.Conversation{ID: "chat-014-lifecycle", TenantID: "tenant-a", Kind: chat.PrivateChannel, Name: "Old name", OwnerID: "alice", Revision: 1}
	members := []chat.Membership{
		{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory},
		{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "bob", Role: chat.Member, HistoryVisibility: chat.FullHistory},
	}
	created, err := s.CreateConversation(ctx, c, members, "")
	if err != nil {
		t.Fatal(err)
	}
	actor := chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}
	post, err := s.SendPost(ctx, chat.SendPostRequest{Principal: actor, TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "history-before-lifecycle"}, chat.Post{AuthorID: actor.SubjectID, AuthorHomeTenantID: actor.TenantID, Body: "history stays"})
	if err != nil {
		t.Fatal(err)
	}

	created.Name = "Renamed channel"
	created.OwnerID = "bob"
	created.Archived = true
	archived, err := s.UpdateConversation(ctx, actor, created, 1)
	if err != nil {
		t.Fatalf("archive and transfer: %v", err)
	}
	if archived.ID != c.ID || archived.OwnerID != "bob" || archived.Name != "Renamed channel" || !archived.Archived || archived.Revision != 2 {
		t.Fatalf("archived channel = %+v", archived)
	}

	archived.Archived = false
	restored, err := s.UpdateConversation(ctx, actor, archived, 2)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.ID != c.ID || restored.OwnerID != "bob" || restored.Archived || restored.Revision != 3 {
		t.Fatalf("restored channel = %+v", restored)
	}
	posts, err := s.ListPosts(ctx, actor, c.TenantID, c.ID, 0, chat.Page{PageSize: 10}, chat.PostWindow{})
	if err != nil || len(posts.Posts) != 1 || posts.Posts[0].ID != post.ID || posts.Posts[0].Body != "history stays" {
		t.Fatalf("posts after lifecycle updates = %+v, err=%v", posts, err)
	}
}

func TestTodo_CHAT_014_Security(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	c := chat.Conversation{ID: "chat-014-security", TenantID: "tenant-a", Kind: chat.PrivateChannel, Name: "Security", OwnerID: "alice", Revision: 1}
	members := []chat.Membership{
		{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory},
		{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "bob", Role: chat.Member, HistoryVisibility: chat.FullHistory},
	}
	if _, err := s.CreateConversation(ctx, c, members, ""); err != nil {
		t.Fatal(err)
	}
	c.Name = "forged rename"
	if _, err := s.UpdateConversation(ctx, chat.Principal{TenantID: c.TenantID, SubjectID: "bob"}, c, 1); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("member rename error = %v, want permission denied", err)
	}
	c.Name = "invalid transfer"
	c.OwnerID = "nonmember"
	if _, err := s.UpdateConversation(ctx, chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}, c, 1); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("transfer to nonmember error = %v, want invalid argument", err)
	}
	c.Name = "stale update"
	c.OwnerID = "alice"
	if _, err := s.UpdateConversation(ctx, chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}, c, 2); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("stale update error = %v, want conflict", err)
	}
	var name, owner string
	if err := s.Store.RunTenantTx(ctx, c.TenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT name,owner_id FROM chat_conversation WHERE tenant_id=$1 AND id=$2`, c.TenantID, c.ID).Scan(&name, &owner)
	}); err != nil {
		t.Fatal(err)
	}
	if name != "Security" || owner != "alice" {
		t.Fatalf("unauthorized or stale update changed row: name=%q owner=%q", name, owner)
	}
}

func TestTodo_CHAT_014_Golden(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	c := chat.Conversation{ID: "chat-014-golden", TenantID: "tenant-a", Kind: chat.PrivateChannel, Name: "Before", OwnerID: "alice", Revision: 1}
	m := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	if _, err := s.CreateConversation(ctx, c, []chat.Membership{m}, ""); err != nil {
		t.Fatal(err)
	}
	c.Name = "After"
	c.Archived = true
	updated, err := s.UpdateConversation(ctx, chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}, c, 1)
	if err != nil {
		t.Fatal(err)
	}
	var payload []byte
	if err := s.Store.RunTenantTx(ctx, c.TenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT payload FROM chat_outbox WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type=$3`, c.TenantID, updated.ID, "conversation.updated").Scan(&payload)
	}); err != nil {
		t.Fatal(err)
	}
	var envelope OutboxEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("decode conversation lifecycle event: %v", err)
	}
	if envelope.ConversationID != c.ID || envelope.ActorID != "alice" || envelope.ActorHomeTenantID != c.TenantID || envelope.TargetID != c.ID || envelope.Revision != 2 || envelope.PolicyRevision != 2 || envelope.EventSequence != 3 || envelope.SchemaVersion != OutboxSchemaVersion {
		t.Fatalf("conversation lifecycle envelope = %+v", envelope)
	}
	if _, err := uuid.Parse(envelope.CorrelationID); err != nil {
		t.Fatalf("correlation id %q is not a UUID: %v", envelope.CorrelationID, err)
	}
	const wantValue = `{"ID":"chat-014-golden","TenantID":"tenant-a","Kind":"PRIVATE_CHANNEL","Name":"After","OwnerID":"alice","Revision":2,"Archived":true,"Joined":false,"MemberCount":0,"LastActivityAt":null}`
	gotCanonical, err := canonicalJSON(envelope.Value)
	if err != nil {
		t.Fatalf("canonicalize conversation lifecycle value: %v", err)
	}
	wantCanonical, err := canonicalJSON([]byte(wantValue))
	if err != nil {
		t.Fatalf("canonicalize expected lifecycle value: %v", err)
	}
	if string(gotCanonical) != string(wantCanonical) {
		t.Fatalf("conversation lifecycle value = %s\nwant semantic JSON %s", gotCanonical, wantCanonical)
	}
}

func canonicalJSON(input []byte) ([]byte, error) {
	var value any
	if err := json.Unmarshal(input, &value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func TestTodo_CHAT_014_GoldenCanonicalization(t *testing.T) {
	got, err := canonicalJSON([]byte(`{"Name": "Operations", "ID":"stable-channel"}`))
	if err != nil {
		t.Fatal(err)
	}
	want, err := canonicalJSON([]byte(`{"ID":"stable-channel","Name":"Operations"}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("canonical JSON = %s, want %s", got, want)
	}
	changed, err := canonicalJSON([]byte(`{"ID":"stable-channel","Name":"Renamed"}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) == string(changed) {
		t.Fatal("canonical JSON ignored a changed semantic field")
	}
}
