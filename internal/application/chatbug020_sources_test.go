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
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/pressly/goose/v3"
)

type chatbug020PersonaDM struct{ conversationID string }

func (r chatbug020PersonaDM) ResolvePersonaDM(context.Context, chatcore.Principal, string) (string, error) {
	return r.conversationID, nil
}

// TestTodo_CHATBUG_020 reproduces the stored shape of the review answer: a
// private answer in #general whose Sources are titles only, "(version 1)" and a
// document that is placed only in #general, with a durable copy in the
// invoker's agent conversation that carries the context token. The real chat
// composition and a real document store decide every link, for the channel card
// that arrives through the served stream and for the direct conversation read
// from history.
func TestTodo_CHATBUG_020(t *testing.T) {
	ctx := context.Background()
	documents := documentServiceFixture(t)
	const tenant = "chatbug020"

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
	runtime, err := composeChat(ctx, ServeConfig{Profile: ServeProfileLocalDev, ChatEnabled: true, ChatDatabaseURL: databaseURL(chatDB.URL, chatDB.Schema), ChatCursorKey: "chatbug-020-sources-cursor-key-0123456789"}, time.Now, chatAdminFacts{role: "employee"}, core, nil)
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
	walt := chatcore.Principal{TenantID: tenant, SubjectID: "walt"}
	mallory := chatcore.Principal{TenantID: tenant, SubjectID: "mallory"}
	general, err := runtime.service.CreateConversation(authenticate("walt"), chatcore.CreateConversationRequest{Principal: walt, TenantID: tenant, Kind: chatcore.PublicChannel, Name: "general", IdempotencyKey: "chatbug-020-general"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.service.AddMembership(authenticate("walt"), chatcore.AddMembershipRequest{Principal: walt, Membership: chatcore.Membership{ConversationID: general.ID, TenantID: tenant, HomeTenantID: tenant, SubjectID: "mallory", Role: chatcore.Member, HistoryVisibility: chatcore.FullHistory}}); err != nil {
		t.Fatal(err)
	}
	direct, err := runtime.service.CreateConversation(authenticate("walt"), chatcore.CreateConversationRequest{Principal: walt, TenantID: tenant, Kind: chatcore.Direct, Name: "Policy Helper", Members: []chatcore.MemberRef{{TenantID: tenant, SubjectID: "policy-helper"}}, IdempotencyKey: "chatbug-020-direct"})
	if err != nil {
		t.Fatal(err)
	}

	// Documents. The holiday guide is placed only in #general. The policy is
	// placed in both conversations, and another deployed document carries the
	// same title, so a title alone does not name one document tenant-wide.
	place := func(owner, title, markdown, scope string, readers ...string) (string, string) {
		t.Helper()
		id, version := deployedSearchFixture(t, documents, ctx, tenant, owner, title, markdown)
		for _, action := range []string{documenthubstore.ActionManage, documenthubstore.ActionRead} {
			if _, err := documents.store.GrantAction(ctx, tenant, documenthubstore.GrantInput{DocumentID: id, SubjectKind: "person", SubjectID: "u-deployer", Action: action, Effect: documenthubstore.EffectAllow, Issuer: owner}); err != nil {
				t.Fatal(err)
			}
		}
		if scope != "" {
			placeAgain(t, documents, ctx, tenant, owner, id, version, scope)
		}
		for _, reader := range readers {
			if err := documents.ShareDocument(ctx, tenant, owner, id, reader, ""); err != nil {
				t.Fatal(err)
			}
		}
		return id, version
	}
	policyID, policyVersion := place("walt", "Paid time off policy", "## Carryover\n40 hours of unused PTO carry over.", general.ID)
	placeAgain(t, documents, ctx, tenant, "walt", policyID, policyVersion, direct.ID)
	guideID, guideVersion := place("walt", "2026 holiday guide", "Offices are closed on each listed day.", general.ID)
	otherPolicyID, _ := place("chris", "Paid time off policy", "A different company's policy.", "other-room", "walt")
	secretID, _ := place("chris", "Executive succession plan", "Not for Walt.", "")

	// The question and the private answer go through the real service, so the
	// durable copy in the direct conversation has the stored shape.
	question, err := runtime.service.SendPost(authenticate("walt"), chatcore.SendPostRequest{Principal: walt, TenantID: tenant, ConversationID: general.ID, Body: "@Policy Helper how many PTO hours carry over?", IdempotencyKey: "chatbug-020-question"})
	if err != nil {
		t.Fatal(err)
	}
	runtime.core.SetPersonaDMResolver(chatbug020PersonaDM{conversationID: direct.ID})
	served, ok := runtime.service.(*streamingChatService)
	if !ok {
		t.Fatalf("the served chat service is %T: the watch projection is not installed on it", runtime.service)
	}
	served.personaDM = chatbug020PersonaDM{conversationID: direct.ID}
	answer := "Paid time off policy — Employees may carry over up to 40 hours of unused PTO into the next calendar year (Paid time off policy, version 1, Carryover).\n\nSources\n- Paid time off policy (version 1)\n- 2026 holiday guide\n- Executive succession plan"
	if _, err := served.SendEphemeralPost(authenticate("walt"), chatcore.SendEphemeralPostRequest{Principal: walt, TenantID: tenant, ConversationID: general.ID, ThreadID: question.ID, QuestionPostID: question.ID, Body: answer, AuthorAsAgent: true, IdempotencyKey: "chatbug-020-answer"}); err != nil {
		t.Fatal(err)
	}

	policyLink := "](/workspace/app/docs?document=" + policyID + "&version=" + policyVersion + "#carryover) <!--chat.agent.source.readable:true-->"
	guideLink := "](/workspace/app/docs?document=" + guideID + "&version=" + guideVersion + ") <!--chat.agent.source.readable:true-->"
	check := func(where, body string) {
		t.Helper()
		sources := body[strings.LastIndex(body, "\n\nSources\n"):]
		if !strings.Contains(sources, "- [Paid time off policy · Carryover · v1.0.0"+policyLink) {
			t.Errorf("%s: the policy source is not a link to its cited section: %s", where, sources)
		}
		if !strings.Contains(sources, "- [2026 holiday guide · v1.0.0"+guideLink) {
			t.Errorf("%s: the holiday guide, which the reader wrote, is not a link: %s", where, sources)
		}
		if !strings.Contains(sources, "- Executive succession plan <!--chat.agent.source.readable:false-->") || strings.Contains(body, secretID) || strings.Contains(body, otherPolicyID) {
			t.Errorf("%s: a document the reader may not open was linked or named by identifier: %s", where, body)
		}
	}

	if !runtime.bindAgentSourceAccess(documents.store) {
		t.Fatal("served Chat service could not bind the document hub")
	}

	// The channel card arrives through the served stream, as the browser gets it.
	watcher, ok := runtime.service.(interface {
		WatchConversationWithErrors(context.Context, chatcore.WatchConversationRequest) (<-chan chatcore.WatchEvent, <-chan error, error)
	})
	if !ok {
		t.Fatal("served Chat service has no reporting watch")
	}
	watchCtx, cancel := context.WithTimeout(authenticate("walt"), 15*time.Second)
	defer cancel()
	events, failures, err := watcher.WatchConversationWithErrors(watchCtx, chatcore.WatchConversationRequest{Principal: walt, TenantID: tenant, ConversationID: general.ID})
	if err != nil {
		t.Fatal(err)
	}
	var card string
	for card == "" {
		select {
		case event, open := <-events:
			if !open {
				t.Fatal("watch closed before the private answer arrived")
			}
			if event.EphemeralDelivery != nil {
				card = event.EphemeralDelivery.Body
			}
		case failure := <-failures:
			t.Fatalf("watch failed: %v", failure)
		case <-watchCtx.Done():
			t.Fatal("the private answer never arrived on the stream")
		}
	}
	check("channel card (stream)", card)
	if !strings.HasPrefix(card, "Paid time off policy — Employees may carry over") {
		t.Errorf("the answer sentence was changed on the server: %s", card)
	}

	// The same answer in the direct conversation, read from history, and a
	// reader who may open nothing, projected by the same service.
	listed, err := runtime.service.ListPosts(authenticate("walt"), chatcore.ListPostsRequest{Principal: walt, TenantID: tenant, ConversationID: direct.ID, Page: chatcore.Page{PageSize: 10}})
	if err != nil || len(listed.Posts) != 1 {
		t.Fatalf("direct conversation posts = %d, err = %v", len(listed.Posts), err)
	}
	check("direct conversation (history)", listed.Posts[0].Body)
	if !strings.Contains(listed.Posts[0].Body, "chat-agent-question-context:") {
		t.Fatalf("the stored shape was not reproduced, no context token in: %s", listed.Posts[0].Body)
	}
	denied := runtime.core.ProjectWatchEvent(authenticate("mallory"), mallory, tenant, general.ID, chatcore.WatchEvent{EphemeralDelivery: &chatcore.EphemeralDelivery{ID: "x", Body: answer}}).EphemeralDelivery.Body
	if strings.Contains(denied, "/workspace/app/docs") || strings.Count(denied, "readable:false") != 3 {
		t.Errorf("a reader who may open none of the documents was given a link: %s", denied)
	}
}

// placeAgain places one deployed version in one conversation, reviewed for it.
func placeAgain(t *testing.T, documents documentService, ctx context.Context, tenant, owner, documentID, versionID, scope string) {
	t.Helper()
	if _, err := documents.store.RecordReview(ctx, tenant, documenthubstore.ReviewInput{DocumentID: documentID, VersionID: versionID, ScopeKind: "placement", ScopeID: scope, ReviewerID: "u-reviewer", Authority: "team:leads", Decision: documenthubstore.ReviewApproved}); err != nil {
		t.Fatal(err)
	}
	if _, err := documents.store.PlaceDocument(ctx, tenant, documenthubstore.PlaceInput{DocumentID: documentID, VersionID: versionID, ScopeKind: "placement", ScopeID: scope, ActorID: "u-deployer", CustodianID: owner, ReviewDueAt: time.Now().Add(24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
}
