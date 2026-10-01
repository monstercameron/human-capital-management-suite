package application

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type backgroundReplyCurrentAuthority struct {
	snapshot agentrun.AuthoritySnapshot
	revoked  bool
	calls    int
	public   *chatstore.Store
}

func (a *backgroundReplyCurrentAuthority) VerifyAdmission(ctx context.Context, _ agentrun.Request) (agentrun.AuthoritySnapshot, error) {
	a.calls++
	if _, ok := trust.FromContext(ctx); ok {
		return agentrun.AuthoritySnapshot{}, errors.New("unexpected manufactured human")
	}
	if a.revoked {
		return agentrun.AuthoritySnapshot{}, agentrun.ErrAuthorityRefusal
	}
	if a.public != nil {
		if audience, err := a.public.CapturePublicAudienceSnapshot(ctx, "tenant-a", "room-a"); err != nil {
			return agentrun.AuthoritySnapshot{}, err
		} else if audience.TenantID != "tenant-a" || audience.ConversationID != "room-a" || len(audience.Eligible) != 2 {
			return agentrun.AuthoritySnapshot{}, errors.New("public audience snapshot lost its fenced values")
		}
		if _, err := a.public.CapturePersonaChannelPolicy(ctx, "tenant-a", "room-a", ""); err != nil {
			return agentrun.AuthoritySnapshot{}, err
		}
	}
	return a.snapshot, nil
}

func TestTodo_AGENTP_011_BackgroundSealedDeliveryRecoveryAndRecipientIsolation(t *testing.T) {
	for _, kind := range []chat.ConversationKind{chat.PrivateChannel, chat.PublicChannel} {
		t.Run(string(kind), func(t *testing.T) { testBackgroundSealedDeliveryRecovery(t, kind) })
	}
}

func testBackgroundSealedDeliveryRecovery(t *testing.T, kind chat.ConversationKind) {
	ctx := context.Background()
	chatDB := pgtest.NewEmpty(t)
	applyPersonaChatMigrations(t, chatDB)
	dsn := personaChatSchemaDSN(t, chatDB.URL, chatDB.Schema)
	openChat := func() *chatstore.Adapter {
		t.Helper()
		raw, e := chatstore.New(ctx, chatstore.Config{DSN: dsn})
		if e != nil {
			t.Fatal(e)
		}
		adapter := chatstore.NewAdapter(raw)
		t.Cleanup(adapter.Close)
		return adapter
	}
	store := openChat()
	_, err := store.CreateConversation(ctx, chat.Conversation{ID: "room-a", TenantID: "tenant-a", Kind: kind, OwnerID: "alice", Revision: 1}, []chat.Membership{{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "room-a", SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}, {TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "room-a", SubjectID: "bob", Role: chat.Member, HistoryVisibility: chat.FullHistory}}, "")
	if err != nil {
		t.Fatal(err)
	}
	root, err := store.SendPost(ctx, chat.SendPostRequest{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "alice"}, TenantID: "tenant-a", ConversationID: "room-a", IdempotencyKey: "source"}, chat.Post{AuthorID: "alice", Body: "Explain this policy"})
	if err != nil {
		t.Fatal(err)
	}
	validator, record, run, _, outputAuthority := personaRunOutputFixture(t)
	record.Request.Source.Ref = root.ID
	record.Request.Context.ID = root.ID
	record.Request.Principal.Mode = agentrun.ModeOnBehalfOf
	record.ID, err = agentrun.AdmissionRequestID(record.Request.Source)
	if err != nil {
		t.Fatal(err)
	}
	record.RequestDigest, err = agentrun.AdmissionRequestDigest(record.Request)
	if err != nil {
		t.Fatal(err)
	}
	run.ID = record.ID
	run.AdmissionID = record.ID
	run.RequestDigest = record.RequestDigest
	output, err := validator.ValidateAndPersistPersonaOutput(ctx, record, run, agentmodel.ModelResult{Text: "I can explain the policy.", Finish: agentmodel.FinishComplete})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	authority := &backgroundReplyCurrentAuthority{snapshot: record.Authority}
	if kind == chat.PublicChannel {
		if _, err = store.Store.PutPublicAudiencePolicy(ctx, "tenant-a", "room-a", 0, chatstore.PublicAudiencePolicy{Classification: "INTERNAL", Principals: []chatstore.PublicAudiencePrincipal{{HomeTenantID: "tenant-a", SubjectID: "alice"}, {HomeTenantID: "tenant-a", SubjectID: "bob"}}}); err != nil {
			t.Fatal(err)
		}
		if _, err = store.Store.PutPersonaChannelPolicy(ctx, "tenant-a", "room-a", 0, chatstore.PersonaChannelPolicy{PlacementClass: "INTERNAL", MaxTier: "T0", AllowedDataClasses: []string{"INTERNAL"}, AllowedChannelClasses: []string{"PUBLIC"}}); err != nil {
			t.Fatal(err)
		}
		authority.public = store.Store
	}
	cfg := PersonaBackgroundReplyDeliveryConfig{Authority: authority, OutputAuthority: outputAuthority, Worker: privateChatGatewayVerifiedWorker(t, now), Threads: store, Now: func() time.Time { return now }}
	delivery, err := NewDatabasePersonaBackgroundReplyDelivery(cfg, store.Store)
	if err != nil {
		t.Fatal(err)
	}
	deliveryCtx, cancelDelivery := context.WithTimeout(ctx, 5*time.Second)
	receipt, err := delivery.DeliverBackgroundPersonaReply(deliveryCtx, record, run, output)
	cancelDelivery()
	if err != nil || !receipt.Private || receipt.Public || receipt.EphemeralPostID == "" || receipt.DurableCopyPostID == "" {
		t.Fatalf("background delivery = %+v, %v", receipt, err)
	}
	if _, ok := trust.FromContext(ctx); ok {
		t.Fatal("background installed a principal")
	}
	// Persist the signed output in the separate agent database, close both owner
	// connections, and recover it through a new gateway before replaying chat.
	agentDB := pgtest.NewEmpty(t)
	if err = agentstore.Migrate(ctx, agentDB.SQL); err != nil {
		t.Fatal(err)
	}
	tenantID := uuid.New()
	agentDB.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1)`, tenantID)
	openOutputs := func() (*agentpersonastore.TenantStore, func()) {
		t.Helper()
		conn := agentDB.NewConn(t)
		if _, e := conn.Exec(ctx, "SET ROLE "+agentstore.AppRole); e != nil {
			t.Fatal(e)
		}
		root, e := agentpersonastore.New(conn, func(key values.TenantId) uuid.UUID {
			if key == "tenant-a" {
				return tenantID
			}
			return uuid.Nil
		})
		if e != nil {
			t.Fatal(e)
		}
		scoped, e := root.ForTenant(ctx, "tenant-a")
		if e != nil {
			t.Fatal(e)
		}
		return scoped, func() { _ = conn.Close(ctx) }
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := agentsecurity.NewFinalOutputRecoveryAuthority("reply-test", private)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := agentsecurity.NewFinalOutputRecoveryVerifier(map[string]ed25519.PublicKey{"reply-test": public})
	if err != nil {
		t.Fatal(err)
	}
	outputs, closeOutput := openOutputs()
	if err = outputs.PutFinalOutput(ctx, output, signer); err != nil {
		t.Fatal(err)
	}
	closeOutput()
	store.Close()
	store = openChat()
	cfg.Threads = store
	if kind == chat.PublicChannel {
		authority.public = store.Store
	}
	_, _, _, _, freshAuthority := personaRunOutputFixture(t)
	cfg.OutputAuthority = freshAuthority
	outputs, _ = openOutputs()
	recovered, err := outputs.RecoverFinalOutputForIdentity(ctx, output.Identity(), verifier, backgroundReplyTestRehydrator{record: record, run: run, authority: freshAuthority})
	if err != nil || recovered.Digest() != output.Digest() {
		t.Fatalf("signed restart recovery: %v", err)
	}
	restarted, err := NewDatabasePersonaBackgroundReplyDelivery(cfg, store.Store)
	if err != nil {
		t.Fatal(err)
	}
	replayCtx, cancelReplay := context.WithTimeout(ctx, 5*time.Second)
	replay, err := restarted.DeliverBackgroundPersonaReply(replayCtx, record, run, recovered)
	cancelReplay()
	if err != nil || replay != receipt {
		t.Fatalf("restart replay changed receipt: %+v %v", replay, err)
	}
	delivery = restarted
	posts, err := store.ListPosts(ctx, chat.Principal{TenantID: "tenant-a", SubjectID: "alice"}, "tenant-a", receipt.DurableCopyConversationID, 0, chat.Page{PageSize: 10}, chat.PostWindow{})
	if err != nil || len(posts.Posts) != 1 || posts.Posts[0].AuthorID != "persona-a" || posts.Posts[0].Body != "I can explain the policy." {
		t.Fatalf("canonical DM = %+v %v", posts, err)
	}
	alice, _, err := store.ListEphemeral(ctx, chat.Principal{TenantID: "tenant-a", SubjectID: "alice"}, "tenant-a", "room-a", 0, 10)
	if err != nil || len(alice) != 1 || alice[0].DurableCopyPostID != receipt.DurableCopyPostID {
		t.Fatalf("invoker replay = %+v %v", alice, err)
	}
	bob, _, err := store.ListEphemeral(ctx, chat.Principal{TenantID: "tenant-a", SubjectID: "bob"}, "tenant-a", "room-a", 0, 10)
	if err != nil || len(bob) != 0 {
		t.Fatalf("other invoker observed output: %+v %v", bob, err)
	}
	source, err := store.ListPosts(ctx, chat.Principal{TenantID: "tenant-a", SubjectID: "bob"}, "tenant-a", "room-a", 0, chat.Page{PageSize: 10}, chat.PostWindow{})
	if err != nil || len(source.Posts) != 1 {
		t.Fatalf("private output entered source history: %+v %v", source, err)
	}
	originalGrounding := freshAuthority.authority.Grounding
	changedSource := "The policy source was revised."
	changedDigest := sha256.Sum256([]byte(changedSource))
	changed, err := freshAuthority.authority.Gateway.Observe(agentsecurity.SourceChat, changedSource, agentsecurity.KindObservation, agentsecurity.Citation{SourceID: "chat:post-a", Location: "conversation:room-a/post-a", Digest: "sha256:" + hex.EncodeToString(changedDigest[:])})
	if err != nil {
		t.Fatal(err)
	}
	freshAuthority.authority.Grounding = []agentsecurity.Datum{changed}
	if _, err = delivery.DeliverBackgroundPersonaReply(ctx, record, run, recovered); !errors.Is(err, errPersonaBackgroundDelivery) {
		t.Fatalf("changed current source citation authorized old output: %v", err)
	}
	freshAuthority.authority.Grounding = originalGrounding
	authority.revoked = true
	if _, err = delivery.DeliverBackgroundPersonaReply(ctx, record, run, output); !errors.Is(err, errPersonaBackgroundDelivery) {
		t.Fatalf("current revoke replay = %v", err)
	}
	authority.revoked = false
	if err = store.Store.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE chat_membership SET state='left' WHERE tenant_id='tenant-a' AND conversation_id='room-a' AND member_id='alice'`)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = delivery.DeliverBackgroundPersonaReply(ctx, record, run, output); !errors.Is(err, errPersonaBackgroundDelivery) {
		t.Fatalf("current membership removal = %v", err)
	}
}

type backgroundReplyTestRehydrator struct {
	record    agentrun.Record
	run       runstate.Run
	authority PersonaRunChatReplyAuthoritySource
}

func (r backgroundReplyTestRehydrator) RehydrateFinalOutput(ctx context.Context, stored agentsecurity.FinalOutputRecoveryRecord) (agentsecurity.FinalOutputPersistence, error) {
	var body struct {
		Payload struct{ Narrative string } `json:"payload"`
	}
	if err := json.Unmarshal(stored.SealedPayload, &body); err != nil {
		return agentsecurity.FinalOutputPersistence{}, err
	}
	current, err := r.authority.ResolvePersonaRunChatReplyAuthority(ctx, r.record, r.run)
	if err != nil {
		return agentsecurity.FinalOutputPersistence{}, err
	}
	return sealPersonaRunChatReply(ctx, r.record, r.run, current, body.Payload.Narrative)
}

func TestTodo_AGENTP_011_BackgroundSealedDeliveryRejectsForgedInput(t *testing.T) {
	if _, err := NewPersonaBackgroundReplyDelivery(PersonaBackgroundReplyDeliveryConfig{}); !errors.Is(err, errPersonaBackgroundDelivery) {
		t.Fatalf("incomplete composition: %v", err)
	}
	d := &PersonaBackgroundReplyDelivery{}
	if _, err := d.DeliverBackgroundPersonaReply(context.Background(), agentrun.Record{}, runstate.Run{}, agentsecurity.FinalOutputPersistence{}); !errors.Is(err, errPersonaBackgroundDelivery) {
		t.Fatalf("zero sealed projection: %v", err)
	}
	if _, err := d.AuthorizeSealedBackgroundPersonaReply(context.Background(), agentsecurity.FinalOutputPersistence{}); !errors.Is(err, errPersonaBackgroundDelivery) {
		t.Fatalf("missing exact durable binding: %v", err)
	}
	if _, err := chatstore.NewSealedBackgroundPersonaDelivery(nil, d, d, time.Now); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("native missing store: %v", err)
	}
}
