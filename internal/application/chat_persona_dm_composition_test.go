package application

import (
	"context"
	"io/fs"
	"net/url"
	"strings"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/pressly/goose/v3"
)

type composedPrivateDM struct{ source chatcore.PersonaDMResolver }

func (r *composedPrivateDM) ResolvePersonaDM(ctx context.Context, p chatcore.Principal, tenant string) (string, error) {
	if r.source == nil {
		return "", chatcore.ErrUnavailable
	}
	return r.source.ResolvePersonaDM(ctx, p, tenant)
}

func TestTodo_AGENTP_011_ComposedPrivateReplyReachesCanonicalDM(t *testing.T) {
	ctx := context.Background()
	chatDB, coreDB := pgtest.NewEmpty(t), pgtest.NewEmpty(t)
	migrations, err := fs.Sub(chatstore.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, chatDB.SQL, migrations, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	databaseURL := func(raw, schema string) string {
		t.Helper()
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	core, err := pgxadapter.NewPool(ctx, databaseURL(coreDB.URL, coreDB.Schema), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer core.Close()
	dm := &composedPrivateDM{}
	persona := chatcore.MemberRef{TenantID: "host", SubjectID: "canonical-agent"}
	factoryCalls := 0
	deployment, err := composeChat(ctx, ServeConfig{Profile: ServeProfileLocalDev, ChatEnabled: true, ChatDatabaseURL: databaseURL(chatDB.URL, chatDB.Schema), ChatCursorKey: "private-reply-composition-cursor-key"}, time.Now, chatAdminFacts{role: "employee"}, core, nil, ChatComposition{PersonaDMFactory: func(store chatcore.Store, _ chatcore.ConversationService) (chatcore.PersonaDMResolver, error) {
		factoryCalls++
		resolver, err := NewPersonaDMResolver(store, persona)
		if err != nil {
			return nil, err
		}
		dm.source = resolver
		return dm, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer deployment.close()
	if factoryCalls != 1 || dm.source == nil {
		t.Fatal("persona DM factory was not composed exactly once")
	}
	resolver := dm.source
	provisioner, err := NewPersonaDMProvisioner(deployment.service, resolver, persona)
	if err != nil {
		t.Fatal(err)
	}
	authenticate := func(subject string) context.Context {
		t.Helper()
		at := time.Now().UTC()
		principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "host", Subject: subject, SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "test-session-" + subject, IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "test-verified-" + subject})
		if err != nil {
			t.Fatal(err)
		}
		return trust.WithPrincipal(ctx, principal)
	}
	aliceCtx, bobCtx := authenticate("alice"), authenticate("bob")
	alice, bob := chatcore.Principal{TenantID: "host", SubjectID: "alice"}, chatcore.Principal{TenantID: "host", SubjectID: "bob"}
	room, err := deployment.service.CreateConversation(aliceCtx, chatcore.CreateConversationRequest{Principal: alice, TenantID: "host", Kind: chatcore.PrivateChannel, Name: "source", MemberIDs: []string{"bob"}, IdempotencyKey: "private-reply-room"})
	if err != nil {
		t.Fatal(err)
	}
	dmID, err := provisioner.EnsurePersonaDM(aliceCtx, alice, "host")
	if err != nil {
		t.Fatal(err)
	}
	root, err := deployment.service.SendPost(aliceCtx, chatcore.SendPostRequest{Principal: alice, TenantID: "host", ConversationID: room.ID, Body: "question", IdempotencyKey: "private-reply-root"})
	if err != nil {
		t.Fatal(err)
	}
	ephemeral := deployment.service.(chatcore.EphemeralService)
	request := chatcore.SendEphemeralPostRequest{Principal: alice, TenantID: "host", ConversationID: room.ID, ThreadID: root.ID, Body: "private answer", IdempotencyKey: "private-reply-answer", DurableCopyConversationID: room.ID}
	answer, err := ephemeral.SendEphemeralPost(aliceCtx, request)
	if err != nil {
		t.Fatal(err)
	}
	if !answer.OnlyVisibleToYou || answer.RecipientSubjectID != "alice" || answer.DurableCopyConversationID != dmID || answer.DurableCopyPostID == "" {
		t.Fatalf("wrong private delivery: %+v", answer)
	}
	replay, err := ephemeral.SendEphemeralPost(aliceCtx, request)
	if err != nil || replay.ID != answer.ID || replay.DurableCopyPostID != answer.DurableCopyPostID {
		t.Fatalf("duplicate reply changed: %+v %v", replay, err)
	}
	adapter := chatstore.NewAdapter(deployment.extensions.TodoStore)
	visible, _, err := adapter.ListEphemeral(aliceCtx, alice, "host", room.ID, 0, 100)
	if err != nil || len(visible) != 1 || visible[0].Body != "private answer" {
		t.Fatalf("recipient replay=%v err=%v", visible, err)
	}
	hidden, _, err := adapter.ListEphemeral(bobCtx, bob, "host", room.ID, 0, 100)
	if err != nil || len(hidden) != 0 {
		t.Fatalf("private reply reached peer: %v %v", hidden, err)
	}
	posts, err := deployment.service.ListPosts(aliceCtx, chatcore.ListPostsRequest{Principal: alice, TenantID: "host", ConversationID: dmID, Page: chatcore.Page{PageSize: 20}})
	if err != nil || len(posts.Posts) != 1 || !strings.Contains(posts.Posts[0].Body, "private answer") {
		t.Fatalf("durable copy=%v err=%v", posts.Posts, err)
	}
	if _, err := deployment.service.GetConversation(bobCtx, chatcore.GetConversationRequest{Principal: bob, TenantID: "host", ConversationID: dmID}); err == nil {
		t.Fatal("peer read another user's persona DM")
	}
}
