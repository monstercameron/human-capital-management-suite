package application

import (
	"context"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/pressly/goose/v3"
)

// TestTodo_CHATBUG_006 drives the composed Chat runtime and the composed
// document hub: an agent answer's source is a link for a reader the hub lets
// read the document, with the cited section, and is a title with the access
// note, and no link, for a reader it does not.
func TestTodo_CHATBUG_006(t *testing.T) {
	ctx := context.Background()
	documents := documentServiceFixture(t)
	const tenant = "host"
	documentID, err := documents.store.CreateDocument(ctx, tenant, "alice", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	version, err := documents.store.SubmitCandidate(ctx, tenant, documenthubstore.Version{DocumentID: documentID, CreatorID: "alice", Title: "Paid time off policy", Markdown: "## Carryover\n40 hours", Classification: "INTERNAL"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = documents.store.ShareDocument(ctx, tenant, documentID, "alice", documenthubstore.GrantInput{SubjectKind: "person", SubjectID: "bob", Action: documenthubstore.ActionRead, Effect: documenthubstore.EffectAllow}); err != nil {
		t.Fatal(err)
	}

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
	runtime, err := composeChat(ctx, ServeConfig{Profile: ServeProfileLocalDev, ChatEnabled: true, ChatDatabaseURL: databaseURL(chatDB.URL, chatDB.Schema), ChatCursorKey: "chatbug-006-sources-cursor-key"}, time.Now, chatAdminFacts{role: "employee"}, core, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.close()
	authenticate := func(subject string) context.Context {
		at := time.Now().UTC()
		principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: tenant, Subject: subject, SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "test-session-" + subject, IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "test-verified-" + subject})
		if err != nil {
			t.Fatal(err)
		}
		return trust.WithPrincipal(ctx, principal)
	}
	alice := chatcore.Principal{TenantID: tenant, SubjectID: "alice"}
	room, err := runtime.service.CreateConversation(authenticate("alice"), chatcore.CreateConversationRequest{Principal: alice, TenantID: tenant, Kind: chatcore.PublicChannel, Name: "policies", IdempotencyKey: "chatbug-006-room"})
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range []string{"bob", "mallory"} {
		if _, err := runtime.service.AddMembership(authenticate("alice"), chatcore.AddMembershipRequest{Principal: alice, Membership: chatcore.Membership{ConversationID: room.ID, TenantID: tenant, HomeTenantID: tenant, SubjectID: member, Role: chatcore.Member, HistoryVisibility: chatcore.FullHistory}}); err != nil {
			t.Fatal(err)
		}
	}
	link := "/workspace/app/docs?document=" + documentID + "&version=" + version.ID + "#carryover"
	body := "Carryover is 40 hours ([Paid time off policy](" + link + ")).\n\nSources\n- [Paid time off policy](" + link + ")"
	if _, err := runtime.service.SendPost(authenticate("alice"), chatcore.SendPostRequest{Principal: alice, TenantID: tenant, ConversationID: room.ID, Body: body, IdempotencyKey: "chatbug-006-answer"}); err != nil {
		t.Fatal(err)
	}
	read := func(subject string) string {
		t.Helper()
		listed, err := runtime.service.ListPosts(authenticate(subject), chatcore.ListPostsRequest{Principal: chatcore.Principal{TenantID: tenant, SubjectID: subject}, TenantID: tenant, ConversationID: room.ID, Page: chatcore.Page{PageSize: 10}})
		if err != nil {
			t.Fatalf("%s lists posts: %v", subject, err)
		}
		// The lines Chat records when members are added (CHATUX-021) are not the
		// answer under test.
		said := agentUX070Said(listed.Posts)
		if len(said) != 1 {
			t.Fatalf("%s sees %d posts", subject, len(said))
		}
		return said[0].Body
	}
	wantLink := "/workspace/app/docs?document=" + documentID
	sources := func(rendered string) string { return rendered[strings.LastIndex(rendered, "\n\nSources\n"):] }

	// Before the hub is bound the projection fails closed, for everyone.
	if got := sources(read("alice")); strings.Contains(got, wantLink) || !strings.Contains(got, "readable:false") {
		t.Fatalf("unbound projection linked a source: %s", got)
	}
	if !runtime.bindAgentSourceAccess(documents.store) {
		t.Fatal("served Chat service could not bind the document hub")
	}
	for _, reader := range []string{"alice", "bob"} {
		got := read(reader)
		if !strings.Contains(sources(got), "](/workspace/app/docs?document="+documentID+"&version="+version.ID+"#carryover)") || !strings.Contains(sources(got), "readable:true") || strings.Contains(sources(got), "readable:false") {
			t.Fatalf("%s, who may read the document, got no link: %s", reader, got)
		}
		if !strings.Contains(got, "[Paid time off policy]("+wantLink) {
			t.Fatalf("%s: the inline citation lost its link: %s", reader, got)
		}
	}
	for _, reader := range []string{"mallory"} {
		got := read(reader)
		if strings.Contains(got, "/workspace/app/docs") || !strings.Contains(sources(got), "- Paid time off policy <!--chat.agent.source.readable:false-->") {
			t.Fatalf("%s, who may not read the document, was given a link or lost the title: %s", reader, got)
		}
	}
	if (composedChat{}).bindAgentSourceAccess(documents.store) || runtime.bindAgentSourceAccess(nil) {
		t.Fatal("binding without Chat or without the hub reported success")
	}
}

// The served composition installs the binding once the document runtime exists:
// composeChat runs before composeDocument, so the binder is called after both.
func TestTodo_CHATBUG_006_ServedAssemblyBindsDocuments(t *testing.T) {
	source, err := os.ReadFile("serve.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	chat, document, bind := strings.Index(text, "composeChat(ctx, cfg"), strings.Index(text, "composeDocument(ctx, cfg)"), strings.Index(text, "chatRuntime.bindAgentSourceAccess(documentRuntime.store)")
	if chat < 0 || document < chat || bind < document {
		t.Fatalf("serve.go does not bind the document hub onto Chat after composing both: chat=%d document=%d bind=%d", chat, document, bind)
	}
}
