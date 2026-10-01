package application

import (
	"context"
	"io/fs"
	"net"
	"net/url"
	"testing"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatstream"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportchat "github.com/monstercameron/human-capital-management-suite/internal/transport/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/pressly/goose/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// TestTodo_AGENTP_008_011_ServedGRPCHumanMentionIntegration drives the
// published chat RPC through the production persona decorator over a real
// PostgreSQL chat store. ComposeServe must wire this same decorator before the
// served application can satisfy the test.
func TestTodo_AGENTP_008_011_ServedGRPCHumanMentionIntegration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	applyPersonaChatMigrations(t, db)
	dsn := personaChatSchemaDSN(t, db.URL, db.Schema)
	raw, err := chatstore.New(context.Background(), chatstore.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("open real chat store: %v", err)
	}
	store := chatstore.NewAdapter(raw)
	t.Cleanup(store.Close)
	// The durable replay query also checks expiry against PostgreSQL's current
	// clock. Anchor the fixture to today so it stays visible on later test runs.
	fixtureTime := time.Now().UTC()
	now := func() time.Time { return fixtureTime }
	service := chatcore.NewService(store, now)
	service.SetAuthority(servedPersonaChatAuthority{})
	service.SetReferenceDirectory(servedPersonaReferenceDirectory{})
	runs := &personaRunFake{}
	grants := &personaGrantFake{}
	refs := &servedPersonaMentionResolver{}
	failures := &personaFailureFake{}
	invocation, err := newPersonaChatInvocation(personaChatInvocationConfig{
		Chat: service, References: refs,
		Authority: personaAuthorityFake{admission: personaAdmission()}, Grants: grants, Runs: runs,
		T0Skills: personaT0PolicyFake{allowed: true}, Repository: agentinvoke.NewMemoryRepository(), Failures: failures,
	})
	if err != nil {
		t.Fatalf("compose persona decorator: %v", err)
	}
	decorated, err := newPersonaChatConversationService(service, service, invocation, nil)
	if err != nil {
		t.Fatalf("compose decorated chat service: %v", err)
	}
	ctx := context.Background()
	alice := chatcore.Principal{TenantID: "tenant-a", SubjectID: "alice"}
	if _, err := service.CreateConversation(ctx, chatcore.CreateConversationRequest{Principal: alice, TenantID: "tenant-a", ConversationID: "room-1", Kind: chatcore.PublicChannel, Name: "room"}); err != nil {
		t.Fatalf("create real chat conversation: %v", err)
	}
	if _, err := service.AddMembership(ctx, chatcore.AddMembershipRequest{Principal: alice, Membership: chatcore.Membership{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "room-1", SubjectID: "bob"}}); err != nil {
		t.Fatalf("add authorized audience member: %v", err)
	}
	if _, err := service.CreateConversation(ctx, chatcore.CreateConversationRequest{Principal: alice, TenantID: "tenant-a", ConversationID: "alice-persona-dm", Kind: chatcore.Direct, Name: "persona dm"}); err != nil {
		t.Fatalf("create persona DM: %v", err)
	}
	service.SetPersonaDMResolver(agentp011PersonaDM{conversationID: "alice-persona-dm"})

	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(grpcserver.UnaryInterceptor(transport.Config{Verifier: servedPersonaVerifier{now: fixtureTime}, NewRequestID: func() string { return "persona-e2e" }})),
	)
	transportchat.Register(grpcServer, transportchat.Dependencies{Service: decorated})
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen served chat gRPC: %v", err)
	}
	go func() { _ = grpcServer.Serve(lis) }()
	t.Cleanup(func() { grpcServer.Stop(); _ = lis.Close() })
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial served chat gRPC: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := chatv1.NewConversationServiceClient(conn)
	post, err := client.SendPost(metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer served-persona-test"), &chatv1.SendPostRequest{
		TenantId: "tenant-a", ConversationId: "room-1", Body: "@Comp Analyst summarize the public policy", IdempotencyKey: "persona-e2e-1",
		References: []*chatv1.Reference{{Kind: chatv1.ReferenceKind_REFERENCE_KIND_AGENT_MENTION, TenantId: "tenant-a", Id: "persona-comp", Display: "Comp Analyst"}},
	})
	if err != nil {
		t.Fatalf("served human post: %v", err)
	}
	if post.GetPost().GetId() == "" {
		t.Fatal("served gRPC post returned no durable post id")
	}
	if len(runs.requests) != 1 || grants.calls != 1 {
		t.Fatalf("served mention refs=%d runs=%d grants=%d failures=%v, want one idempotent run and grant", refs.calls, len(runs.requests), grants.calls, failures.errs)
	}
	postReplay, err := client.SendPost(metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer served-persona-test"), &chatv1.SendPostRequest{
		TenantId: "tenant-a", ConversationId: "room-1", Body: "@Comp Analyst summarize the public policy", IdempotencyKey: "persona-e2e-1",
		References: []*chatv1.Reference{{Kind: chatv1.ReferenceKind_REFERENCE_KIND_AGENT_MENTION, TenantId: "tenant-a", Id: "persona-comp", Display: "Comp Analyst"}},
	})
	if err != nil || postReplay.GetPost().GetId() != post.GetPost().GetId() {
		t.Fatalf("replayed served post=%v err=%v, want same durable post", postReplay.GetPost().GetId(), err)
	}
	if len(runs.requests) != 1 || grants.calls != 1 {
		t.Fatalf("replay started duplicate run=%d or grant=%d", len(runs.requests), grants.calls)
	}

	// The run's response is delivered as an ephemeral post. The stream replay
	// path must return it to Alice and never expose it to Bob.
	service.SetEphemeralStore(store)
	root, err := store.SendPost(ctx, chatcore.SendPostRequest{Principal: alice, TenantID: "tenant-a", ConversationID: "room-1", IdempotencyKey: "persona-root", Body: "root"}, chatcore.Post{AuthorID: "alice", Body: "root"})
	if err != nil {
		t.Fatalf("seed response thread: %v", err)
	}
	response, err := service.SendEphemeralPost(ctx, chatcore.SendEphemeralPostRequest{Principal: alice, TenantID: "tenant-a", ConversationID: "room-1", ThreadID: root.ID, Body: "private persona answer", IdempotencyKey: "persona-response"})
	if err != nil {
		t.Fatalf("persist ephemeral response: %v", err)
	}
	if response.Sequence == 0 {
		t.Fatal("ephemeral response has no durable stream sequence")
	}
	reader := chatServiceReader{service: service, membership: store, events: store}
	stream, err := chatstream.New(chatstream.Config{Key: []byte("persona-e2e-stream-key"), Reader: reader, Authorizer: chatServiceStreamAuthorizer{service: service, membership: store}, QueueSize: 8, ReplayLimit: 8, CursorTTL: time.Minute, Clock: now})
	if err != nil {
		t.Fatalf("compose response stream: %v", err)
	}
	watchCtx, cancelWatch := context.WithTimeout(ctx, 2*time.Second)
	defer cancelWatch()
	aliceWatch, err := stream.Watch(watchCtx, chatstream.WatchRequest{TenantID: "tenant-a", HomeTenantID: "tenant-a", SubjectID: "alice", ConversationID: "room-1", MembershipEpoch: 1, AfterSequence: response.Sequence - 1})
	if err != nil {
		t.Fatalf("alice response replay: %v", err)
	}
	defer aliceWatch.Close()
	event, err := aliceWatch.Next(watchCtx)
	if err != nil || !event.Ephemeral || string(event.Payload) == "" {
		t.Fatalf("alice response event=%+v err=%v", event, err)
	}
	bobWatch, err := stream.Watch(watchCtx, chatstream.WatchRequest{TenantID: "tenant-a", HomeTenantID: "tenant-a", SubjectID: "bob", ConversationID: "room-1", MembershipEpoch: 1, AfterSequence: response.Sequence - 1})
	if err != nil {
		t.Fatalf("bob response replay: %v", err)
	}
	defer bobWatch.Close()
	bobCtx, cancelBob := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancelBob()
	if _, err := bobWatch.Next(bobCtx); err == nil {
		t.Fatal("recipient-scoped response was visible to Bob")
	}
}

func applyPersonaChatMigrations(t testing.TB, db *pgtest.DB) {
	t.Helper()
	migrations, err := fs.Sub(chatstore.Migrations, "migrations")
	if err != nil {
		t.Fatalf("chat migrations filesystem: %v", err)
	}
	fsys, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrations, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatalf("chat migrations provider: %v", err)
	}
	if _, err := fsys.Up(context.Background()); err != nil {
		t.Fatalf("chat migrations: %v", err)
	}
}

func personaChatSchemaDSN(t testing.TB, raw, schema string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse test database URL: %v", err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

type servedPersonaVerifier struct{ now time.Time }

func (v servedPersonaVerifier) Verify(context.Context, trust.Credential) (*trust.Principal, error) {
	now := v.now
	return trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "alice", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "persona-e2e", CredentialDigest: "persona-e2e", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)})
}

type servedPersonaChatAuthority struct{}

func (servedPersonaChatAuthority) Authorize(_ context.Context, p chatcore.Principal, c chatcore.Conversation, _ chatpolicy.Action, now time.Time) (chatpolicy.Input, error) {
	return chatpolicy.Input{Principal: chatpolicy.Principal{ID: p.SubjectID, Tenant: p.TenantID, Active: true}, Channel: chatpolicy.Channel{ID: c.ID, HostTenant: c.TenantID, Enabled: true, Revision: c.Revision}, Membership: chatpolicy.Membership{ConversationID: c.ID, PrincipalID: p.SubjectID, Tenant: p.TenantID, State: chatpolicy.MembershipCurrent, Revision: 1, JoinedAt: now.Add(-time.Hour)}, HasMembership: true, Now: now}, nil
}

type servedPersonaReferenceDirectory struct{}

type servedPersonaMentionResolver struct{ calls int }

func (r *servedPersonaMentionResolver) ResolvePersonaMentions(context.Context, string, string, []chatcore.Reference) ([]agentinvoke.Mention, error) {
	r.calls++
	return []agentinvoke.Mention{{Kind: agentinvoke.PersonaMention, PersonaID: "persona-comp", Canonical: true}}, nil
}

func (servedPersonaReferenceDirectory) People(context.Context, chatcore.Principal, string, string, string) ([]chatcore.ReferenceCandidate, error) {
	return nil, nil
}
func (servedPersonaReferenceDirectory) Agents(context.Context, chatcore.Principal, string, string, string) ([]chatcore.ReferenceCandidate, error) {
	return []chatcore.ReferenceCandidate{{Reference: chatcore.Reference{Kind: chatcore.AgentMention, TenantID: "tenant-a", ID: "persona-comp", Display: "Comp Analyst"}, Eligible: true}}, nil
}
func (servedPersonaReferenceDirectory) Conversations(context.Context, chatcore.Principal, string, string, string) ([]chatcore.ReferenceCandidate, error) {
	return nil, nil
}
func (servedPersonaReferenceDirectory) AgentEligible(context.Context, string, string, string) bool {
	return true
}

var _ chatcore.AgentReferenceDirectory = servedPersonaReferenceDirectory{}
