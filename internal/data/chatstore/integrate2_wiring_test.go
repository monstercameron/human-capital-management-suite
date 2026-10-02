package chatstore

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestTodo_CHATRENDER_001_Integrate2RevisionTransactions(t *testing.T) {
	store, _ := chatFixture(t)
	seedConversationRow(t, store, Conversation{ID: "room", TenantID: "tenant-a", Kind: "PUBLIC_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "owner", Role: "manager", State: "active"}})
	adapter := NewAdapter(store)
	p := chat.Principal{TenantID: "tenant-a", SubjectID: "owner"}
	post, err := adapter.SendPost(t.Context(), chat.SendPostRequest{Principal: p, TenantID: p.TenantID, ConversationID: "room", IdempotencyKey: "send"}, chat.Post{ID: uuid.NewString(), AuthorID: p.SubjectID, AuthorHomeTenantID: p.TenantID, Body: "This is the original message written for our team."})
	if err != nil {
		t.Fatal(err)
	}
	scope := RenderingScope{Principal: p, Tenant: p.TenantID, Conversation: "room"}
	verify := func(post chat.Post) {
		t.Helper()
		d, err := store.RevisionLanguage(t.Context(), scope, post.ID, post.Revision)
		if err != nil || d.Language != chatrender.Detect(post.Body).Language {
			t.Fatal("revision detection was not committed with authored bytes", post, d, err)
		}
	}
	verify(post)
	post, err = adapter.EditPost(t.Context(), chat.EditPostRequest{Principal: p, TenantID: p.TenantID, ConversationID: "room", PostID: post.ID, ExpectedRevision: 1, Body: "مرحبا هذا نص عربي واضح للفريق"})
	if err != nil {
		t.Fatal(err)
	}
	verify(post)
	rev, err := store.RevisePostCAS(t.Context(), p.TenantID, "room", post.ID, p.SubjectID, "This is an edited English paragraph for the team.", 2, false)
	if err != nil {
		t.Fatal(err)
	}
	d, err := store.RevisionLanguage(t.Context(), scope, post.ID, uint64(rev.Revision))
	if err != nil || d.Language != chatrender.Detect(rev.Body).Language {
		t.Fatal("CAS omitted language metadata", d, err)
	}
	denied := errors.New("status closed")
	ctx := chat.WithChannelMutationCheck(t.Context(), func(context.Context) error { return denied })
	if _, err = adapter.EditPost(ctx, chat.EditPostRequest{Principal: p, TenantID: p.TenantID, ConversationID: "room", PostID: post.ID, ExpectedRevision: 3, Body: "must not commit"}); !errors.Is(err, denied) {
		t.Fatal("edit bypassed fence recheck", err)
	}
	if _, err = adapter.SendPost(ctx, chat.SendPostRequest{Principal: p, TenantID: p.TenantID, ConversationID: "room", IdempotencyKey: "denied"}, chat.Post{ID: uuid.NewString(), AuthorID: p.SubjectID, Body: "must not commit"}); !errors.Is(err, denied) {
		t.Fatal("send bypassed fence recheck", err)
	}
	current, err := adapter.GetPost(t.Context(), p.TenantID, "room", post.ID)
	if err != nil || current.Body == "must not commit" || current.Revision != 3 {
		t.Fatal("denied mutation changed post", current, err)
	}
}

func TestTodo_CHATGATE_008_Integrate2MembershipTransaction(t *testing.T) {
	store, service, command := chatgateFixture(t)
	adapter := NewAdapter(store)
	service.Repository.(*GateRepository).Membership = adapter.GateMembership
	actor := chatgate.Actor{Tenant: command.Scope.Tenant, Person: "applicant"}
	mutate := func(admit bool) error {
		return store.RunTenantTx(t.Context(), actor.Tenant, func(tx dbport.Tx) error {
			return adapter.GateMembership(t.Context(), tx, command.Scope, actor, "1.0.0", admit)
		})
	}
	command.Actor = actor
	command.Key = "submit"
	sub, err := service.Submit(t.Context(), command, "1.0.0", map[string]json.RawMessage{"intro": json.RawMessage(`"Hello team"`)})
	if err != nil {
		t.Fatal(err)
	}
	membership, err := adapter.GetMembership(t.Context(), actor.Tenant, command.Scope.Conversation, actor.Tenant, actor.Person)
	if err != nil || membership.JoinedAt == nil || membership.LeftAt != nil || membership.Revision == 0 {
		t.Fatal("gate did not admit ordinary membership", membership, err)
	}
	command.Key = "withdraw"
	gate, err := service.Get(t.Context(), actor, command.Scope, false)
	if err != nil {
		t.Fatal(err)
	}
	command.ExpectedRevision = gate.Revision
	if err = service.Withdraw(t.Context(), command, sub.ID, sub.Revision); err != nil {
		t.Fatal(err)
	}
	membership, err = adapter.GetMembership(t.Context(), actor.Tenant, command.Scope.Conversation, actor.Tenant, actor.Person)
	if err != nil || membership.LeftAt == nil || membership.Revision < 2 {
		t.Fatal("gate withdrawal did not remove membership", membership, err)
	}
	if err = mutate(false); err != nil {
		t.Fatal("repeated withdrawal", err)
	}
	err = store.RunTenantTx(t.Context(), actor.Tenant, func(tx dbport.Tx) error {
		var count int
		var revision int
		if err := tx.QueryRow(t.Context(), `SELECT count(*) FROM chat_outbox WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type IN ('membership.added','membership.removed')`, actor.Tenant, command.Scope.Conversation).Scan(&count); err != nil {
			return err
		}
		if err := tx.QueryRow(t.Context(), `SELECT audience_revision FROM chat_conversation WHERE tenant_id=$1 AND id=$2`, actor.Tenant, command.Scope.Conversation).Scan(&revision); err != nil {
			return err
		}
		if count != 2 || revision < 3 {
			t.Fatalf("membership audience/events omitted: %d %d", count, revision)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// A direct membership write cannot skip a live gate, even when its HTTP surface is unavailable.
	p := chat.Principal{TenantID: actor.Tenant, SubjectID: "owner"}
	_, err = adapter.PutMembership(t.Context(), p, chat.Membership{TenantID: actor.Tenant, ConversationID: command.Scope.Conversation, HomeTenantID: actor.Tenant, SubjectID: "bypass", Role: chat.Member})
	if !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("uncomposed gate admitted direct membership", err)
	}
}

func TestTodo_CHATSEARCH_001_Integrate2Location(t *testing.T) {
	adapter, _, share := chatmapFixture(t)
	share.Place.Precision = "exact"
	share.Place.ApproximateRadius = 0
	share.Place.Label = "Crew meeting"
	if _, err := adapter.AttachLocation(t.Context(), share); err != nil {
		t.Fatal(err)
	}
	registry := chatsearch.NewRegistry()
	authority := func(_ context.Context, a chatsearch.Actor, row chatsearch.Row) (bool, error) {
		return a.PersonID == "bob" && row.Target.ConversationID == share.ConversationID, nil
	}
	if err := adapter.Store.RegisterChatSearch(registry, authority); err != nil {
		t.Fatal(err)
	}
	q := chatsearch.Request{Actor: chatsearch.Actor{TenantID: share.TenantID, HomeTenantID: share.TenantID, PersonID: "bob"}, Query: "Crew meeting", Filters: chatsearch.Filters{Kind: chatsearch.Location}, At: time.Now()}
	result, err := registry.Search(t.Context(), q)
	if err != nil || len(result.Groups) != 1 || len(result.Groups[0].Rows) != 1 {
		t.Fatal(result, err)
	}
	row := result.Groups[0].Rows[0]
	if row.ID != share.ID || row.Target.MessageID != share.PostID || strings.Contains(row.Text, "42.") {
		t.Fatal("location search lost target or exposed coordinates", row)
	}
	if err := adapter.EndLocation(t.Context(), chat.LocationKey{TenantID: share.TenantID, ConversationID: share.ConversationID, PostID: share.PostID, ID: share.ID}); err != nil {
		t.Fatal(err)
	}
	result, err = registry.Search(t.Context(), q)
	if err != nil || len(result.Groups) != 0 {
		t.Fatal("ended location remained searchable", result, err)
	}
}

func TestTodo_CHATSTATE_001_Integrate2ArchivedSearch(t *testing.T) {
	store, _ := chatFixture(t)
	seedConversationRow(t, store, Conversation{ID: "archive", TenantID: "tenant", Kind: "PRIVATE_CHANNEL", Name: "Archived work", OwnerID: "alice", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "alice", HomeTenantID: "tenant", Role: "manager", State: "active"}})
	if _, err := store.sendPostRaw(t.Context(), SendRequest{TenantID: "tenant", HomeTenantID: "tenant", ConversationID: "archive", AuthorID: "alice", ClientKey: "post", Body: "Archived work remains readable"}); err != nil {
		t.Fatal(err)
	}
	if err := store.RunTenantTx(t.Context(), "tenant", func(tx dbport.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE chat_conversation SET lifecycle='ARCHIVED' WHERE tenant_id='tenant' AND id='archive'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	registry := chatsearch.NewRegistry()
	if err := store.RegisterChatSearch(registry, func(context.Context, chatsearch.Actor, chatsearch.Row) (bool, error) { return true, nil }); err != nil {
		t.Fatal(err)
	}
	result, err := registry.Search(t.Context(), chatsearch.Request{Actor: chatsearch.Actor{TenantID: "tenant", HomeTenantID: "tenant", PersonID: "alice"}, Query: "Archived work", Filters: chatsearch.Filters{Kind: chatsearch.Message}, At: time.Now()})
	if err != nil || len(result.Groups) != 1 || len(result.Groups[0].Rows) != 1 {
		t.Fatal("archived message vanished from authorized search", result, err)
	}
}
