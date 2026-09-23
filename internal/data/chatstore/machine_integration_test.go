package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestTodo_CHAT_043_Integration_MachinePostReadAndRevoke(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	at := time.Now().UTC()
	c := chat.Conversation{ID: "channel-a", TenantID: "tenant-a", Kind: chat.PrivateChannel, Name: "Channel", OwnerID: "owner", Revision: 1}
	m := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "owner", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	if _, err := s.CreateConversation(ctx, c, []chat.Membership{m}, "create"); err != nil {
		t.Fatal(err)
	}
	human := chat.Principal{TenantID: "tenant-a", SubjectID: "owner"}
	_, err := s.SendPost(ctx, chat.SendPostRequest{Principal: human, TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "old"}, chat.Post{AuthorID: "owner", Body: "before install", CreatedAt: at.Add(-2 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.RunTenantTx(ctx, c.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_app_installation(id,tenant_id,conversation_id,app_id,version,manifest,granted_scopes,status,approver,revision,created_at,updated_at) VALUES($1,$2,$3,$4,1,$5,$6,'ACTIVE','owner',1,$7,$7)`, "tenant-a:channel-a:agent-a", c.TenantID, c.ID, "agent-a", `{"app_id":"agent-a","version":1,"agent":{"display_name":"Agent A"}}`, []string{"chat.posts.read", "chat.posts.write"}, at.Add(-time.Minute))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	identity, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "agent-a", SubjectKind: trust.SubjectKindAgent, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session-a", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	machineCtx := trust.WithPrincipal(ctx, identity)
	machine := chat.Principal{TenantID: "tenant-a", SubjectID: "agent-a"}
	post, err := s.SendPost(machineCtx, chat.SendPostRequest{Principal: machine, TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "agent-post"}, chat.Post{AuthorID: "agent-a", Body: "after install"})
	if err != nil || post.AuthorID != "agent-a" {
		t.Fatalf("machine post=%+v err=%v", post, err)
	}
	retried, err := s.SendPost(machineCtx, chat.SendPostRequest{Principal: machine, TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "  agent-post  "}, chat.Post{AuthorID: "agent-a", Body: "after install"})
	if err != nil || retried.ID != post.ID || retried.Sequence != post.Sequence {
		t.Fatalf("whitespace key retry=%+v first=%+v err=%v", retried, post, err)
	}
	page, err := s.ListPosts(machineCtx, machine, c.TenantID, c.ID, 0, chat.Page{PageSize: 10}, chat.PostWindow{})
	if err != nil || len(page.Posts) != 1 || page.Posts[0].ID != post.ID {
		t.Fatalf("machine page=%+v err=%v", page, err)
	}
	if err := s.Store.RunTenantTx(ctx, c.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_app_installation SET status='REVOKED',revision=revision+1 WHERE tenant_id=$1 AND id=$2`, c.TenantID, "tenant-a:channel-a:agent-a")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SendPost(machineCtx, chat.SendPostRequest{Principal: machine, TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "revoked-post"}, chat.Post{AuthorID: "agent-a", Body: "denied"}); !errors.Is(err, ErrNotMember) {
		t.Fatalf("revoked post err=%v", err)
	}
	page, err = s.ListPosts(machineCtx, machine, c.TenantID, c.ID, 0, chat.Page{PageSize: 10}, chat.PostWindow{})
	if err != nil || len(page.Posts) != 0 {
		t.Fatalf("revoked page=%+v err=%v", page, err)
	}
}

func TestTodo_CHAT_043_Integration_MachineEventsAllKindsAndRevoke(t *testing.T) {
	s := adapterDB(t)
	for _, kind := range []chat.ConversationKind{chat.PublicChannel, chat.PrivateChannel, chat.Direct, chat.Group} {
		t.Run(string(kind), func(t *testing.T) {
			ctx := context.Background()
			at := time.Now().UTC()
			c := chat.Conversation{ID: "machine-events-" + string(kind), TenantID: "machine-event-host", Kind: kind, Name: "Events", OwnerID: "owner", Revision: 1}
			owner := chat.Principal{TenantID: c.TenantID, SubjectID: "owner"}
			member := chat.Membership{ConversationID: c.ID, TenantID: "machine-event-host", HomeTenantID: "machine-event-host", SubjectID: "owner", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
			if _, err := s.CreateConversation(ctx, c, []chat.Membership{member}, "create-"+c.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := s.SendPost(ctx, chat.SendPostRequest{Principal: owner, TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "before"}, chat.Post{AuthorID: "owner", Body: "before", CreatedAt: at.Add(-2 * time.Minute)}); err != nil {
				t.Fatal(err)
			}
			joinedAt := time.Now().UTC()
			installationID := c.TenantID + ":" + c.ID + ":agent-a"
			if err := s.Store.RunTenantTx(ctx, c.TenantID, func(tx dbport.Tx) error {
				_, err := tx.Exec(ctx, `INSERT INTO chat_app_installation(id,tenant_id,conversation_id,app_id,version,manifest,granted_scopes,status,approver,revision,created_at,updated_at) VALUES($1,$2,$3,$4,1,$5,$6,'ACTIVE','owner',1,$7,$7)`, installationID, c.TenantID, c.ID, "agent-a", `{"app_id":"agent-a","version":1,"agent":{"display_name":"Agent A"}}`, []string{"chat.posts.read"}, joinedAt)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			identity, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "machine-event-host", Subject: "agent-a", SubjectKind: trust.SubjectKindAgent, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "events", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "digest"})
			if err != nil {
				t.Fatal(err)
			}
			machineCtx, cancel := context.WithCancel(trust.WithPrincipal(ctx, identity))
			defer cancel()
			machine := chat.Principal{TenantID: c.TenantID, SubjectID: "agent-a"}
			post, err := s.SendPost(ctx, chat.SendPostRequest{Principal: owner, TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "after"}, chat.Post{AuthorID: "owner", Body: "after"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.PutReaction(ctx, chat.Reaction{TenantID: c.TenantID, HomeTenantID: c.TenantID, ConversationID: c.ID, PostID: post.ID, SubjectID: "owner", Emoji: "👍"}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.PutMembership(ctx, owner, chat.Membership{TenantID: "machine-event-host", HomeTenantID: "machine-event-host", ConversationID: c.ID, SubjectID: "bob", Role: chat.Member, HistoryVisibility: chat.FullHistory}); err != nil {
				t.Fatal(err)
			}
			page, err := s.ReadConversationEvents(machineCtx, chat.WatchConversationRequest{Principal: machine, TenantID: c.TenantID, ConversationID: c.ID}, 0, 100)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Events) != 1 || page.Events[0].Event.Post == nil || page.Events[0].Event.Post.ID != post.ID {
				t.Fatalf("machine events=%+v", page.Events)
			}
			if _, err := s.ReadConversationEvents(machineCtx, chat.WatchConversationRequest{Principal: machine, TenantID: c.TenantID, ConversationID: "wrong"}, 0, 100); !errors.Is(err, chat.ErrPermissionDenied) {
				t.Fatalf("wrong conversation: %v", err)
			}
			if _, err := s.ReadConversationEvents(machineCtx, chat.WatchConversationRequest{Principal: machine, TenantID: "wrong", ConversationID: c.ID}, 0, 100); !errors.Is(err, chat.ErrPermissionDenied) {
				t.Fatalf("wrong tenant: %v", err)
			}
			events, fails, err := s.WatchWithErrors(machineCtx, chat.WatchConversationRequest{Principal: machine, TenantID: c.TenantID, ConversationID: c.ID, AfterSequence: page.NextOffset})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Store.RunTenantTx(ctx, c.TenantID, func(tx dbport.Tx) error {
				_, err := tx.Exec(ctx, `UPDATE chat_app_installation SET status='REVOKED',revision=revision+1 WHERE tenant_id=$1 AND id=$2`, c.TenantID, installationID)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-fails:
				if !errors.Is(err, chat.ErrPermissionDenied) {
					t.Fatalf("revoked stream error=%v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("revoked stream stayed open")
			}
			select {
			case _, ok := <-events:
				if ok {
					t.Fatal("revoked stream delivered event")
				}
			case <-time.After(time.Second):
				t.Fatal("revoked stream event channel stayed open")
			}
			if _, err := s.ReadConversationEvents(machineCtx, chat.WatchConversationRequest{Principal: machine, TenantID: c.TenantID, ConversationID: c.ID}, 0, 100); !errors.Is(err, chat.ErrPermissionDenied) {
				t.Fatalf("revoked reader: %v", err)
			}
			if err := s.Store.RunTenantTx(ctx, c.TenantID, func(tx dbport.Tx) error {
				_, err := tx.Exec(ctx, `UPDATE chat_app_installation SET status='ACTIVE',granted_scopes=$3,revision=revision+1 WHERE tenant_id=$1 AND id=$2`, c.TenantID, installationID, []string{"chat.posts.write"})
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.ReadConversationEvents(machineCtx, chat.WatchConversationRequest{Principal: machine, TenantID: c.TenantID, ConversationID: c.ID}, 0, 100); !errors.Is(err, chat.ErrPermissionDenied) {
				t.Fatalf("missing read scope: %v", err)
			}
			if _, _, err := s.WatchWithErrors(machineCtx, chat.WatchConversationRequest{Principal: machine, TenantID: c.TenantID, ConversationID: c.ID}); !errors.Is(err, chat.ErrPermissionDenied) {
				t.Fatalf("missing read scope watch: %v", err)
			}
		})
	}
}

func TestTodo_CHAT_043_Integration_MachineEventsSkipMatureHistoryBeforeLimit(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	c := chat.Conversation{ID: "mature", TenantID: "host", Kind: chat.PublicChannel, Name: "Mature", OwnerID: "owner", Revision: 1}
	owner := chat.Principal{TenantID: "host", SubjectID: "owner"}
	member := chat.Membership{TenantID: "host", HomeTenantID: "host", ConversationID: c.ID, SubjectID: "owner", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	if _, err := s.CreateConversation(ctx, c, []chat.Membership{member}, "mature-create"); err != nil {
		t.Fatal(err)
	}
	oldPost, err := s.SendPost(ctx, chat.SendPostRequest{Principal: owner, TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "old-post"}, chat.Post{AuthorID: "owner", Body: "old", CreatedAt: time.Now().UTC().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.RunTenantTx(ctx, c.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_outbox(tenant_id,aggregate_id,event_type,payload,created_at) SELECT $1,$2::text,'membership.added',jsonb_build_object('ConversationID',$2::text),$3::timestamptz+(n * interval '1 microsecond') FROM generate_series(1,2500) AS n`, c.TenantID, c.ID, time.Now().UTC().Add(-time.Hour))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	joinedAt := time.Now().UTC()
	if err := s.Store.RunTenantTx(ctx, c.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_app_installation(id,tenant_id,conversation_id,app_id,version,manifest,granted_scopes,status,approver,revision,created_at,updated_at) VALUES($1,$2,$3,$4,1,$5,$6,'ACTIVE','owner',1,$7,$7)`, "host:mature:agent", c.TenantID, c.ID, "agent", `{"app_id":"agent","version":1,"agent":{"display_name":"Agent"}}`, []string{"chat.posts.read"}, joinedAt)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.RunTenantTx(ctx, c.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_outbox(tenant_id,aggregate_id,event_type,payload,created_at) SELECT $1,$3::text,'post.edited',jsonb_build_object('ConversationID',$2::text,'TargetID',$3::text),$4::timestamptz+(n * interval '1 microsecond') FROM generate_series(1,2500) AS n`, c.TenantID, c.ID, oldPost.ID, joinedAt)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	post, err := s.SendPost(ctx, chat.SendPostRequest{Principal: owner, TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "new-post"}, chat.Post{AuthorID: "owner", Body: "new"})
	if err != nil {
		t.Fatal(err)
	}
	identity, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "host", Subject: "agent", SubjectKind: trust.SubjectKindAgent, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "mature", IssuedAt: joinedAt.Add(-time.Minute), ExpiresAt: joinedAt.Add(time.Hour), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	machine := chat.Principal{TenantID: "host", SubjectID: "agent"}
	page, err := s.ReadConversationEvents(trust.WithPrincipal(ctx, identity), chat.WatchConversationRequest{Principal: machine, TenantID: c.TenantID, ConversationID: c.ID}, 0, 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.Events[0].Event.Post == nil || page.Events[0].Event.Post.ID != post.ID || !page.Complete || page.NextOffset <= 5000 {
		t.Fatalf("first bounded page did not reach new post: %+v", page)
	}
}

func TestTodo_CHAT_043_Integration_UnknownConversationIsNotFound(t *testing.T) {
	s := adapterDB(t)
	if _, err := s.GetConversation(context.Background(), "host", "missing"); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("unknown conversation: %v", err)
	}
}

func TestTodo_CHAT_043_Integration_MachineInstallationStoreFaultIsUnavailable(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	at := time.Now().UTC()
	identity, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "host", Subject: "agent", SubjectKind: trust.SubjectKindAgent, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "store-fault", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	ctx = trust.WithPrincipal(ctx, identity)
	if err := s.Store.RunTenantTx(ctx, "host", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `ALTER TABLE chat_app_installation RENAME TO chat_app_installation_fault`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	p := chat.Principal{TenantID: "host", SubjectID: "agent"}
	if _, err := s.MachineWatchEpoch(ctx, "host", "room", p); !errors.Is(err, chat.ErrUnavailable) || errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("installation epoch store failure: %v", err)
	}
	if _, err := s.ReadConversationEvents(ctx, chat.WatchConversationRequest{Principal: p, TenantID: "host", ConversationID: "room"}, 0, 8); !errors.Is(err, chat.ErrUnavailable) || errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("installation page store failure: %v", err)
	}
}
