package application

// CHAT-045's INTEGRATION contract: an installed chat agent's proposed HCM
// action reaches the real application/intent path
// (internal/intent/app.IntentService.CreateIntent over pgtest PostgreSQL),
// through the exact production chatAppsIntentAdapter composeChat wires as
// chatapps.Service.Intent -- not a same-package fake BusinessIntentPort like
// TestTodo_CHAT_045 and TestTodo_CHAT_045_Security use. A minted, comp_admin
// principal already proven (RBAC-RT-003) to be authorized against the
// corpus's certified-READY promotion for omar-reyes proposes exactly that
// promotion through chatapps.Service.ProposeIntent; the receipt's intent id
// is then read back through cell.Service.GetIntent, proving the proposal
// became one durable, retrievable BusinessIntent record, not merely a
// same-process return value.

import (
	"context"
	"testing"
	"time"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	intentapp "github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// chat045Access answers an empty durable-assignment snapshot: the minted
// principal's comp_admin credential role is what the RBAC-RT-003 decision
// resolves on, exactly as it does for the omar-reyes scenario in
// TestTodo_RBAC_RT_003_Integration.
type chat045Access struct{ roleaccess.Store }

func (chat045Access) Load(context.Context, values.TenantId, string) (roleaccess.Snapshot, error) {
	return roleaccess.Snapshot{}, nil
}

// chat045ConversationAuthority always admits the fixed tenant/conversation
// pair this test installs its agent into; the interesting authorization
// decision under test is the kernel's RBAC-RT-003 gate inside
// IntentService.CreateIntent, not chatapps' own conversation membership
// check (covered by CHAT-045's SECURITY matrix test).
type chat045ConversationAuthority struct{ tenant, conversation string }

func (a chat045ConversationAuthority) CanManageApp(context.Context, chatapps.Actor, string) error {
	return nil
}

func (a chat045ConversationAuthority) CanUseConversation(_ context.Context, actor chatapps.Actor, conversation string) error {
	if actor.Tenant != a.tenant || conversation != a.conversation {
		return chatapps.ErrDenied
	}
	return nil
}

// chat045PromotionPayload is the corpus's own certified-READY promotion
// scenario (Omar Reyes, OPS-HRBP2/P2 -> OPS-HRBP3/P3): the same request
// TestTodo_RBAC_RT_003_Integration submits directly through CreateIntent,
// here carried instead as a chatapps.Proposal.RequestPayload.
func chat045PromotionPayload() map[string]any {
	return map[string]any{
		"worker_ref": "omar-reyes",
		"known_at":   "2026-05-15",
		"target": map[string]any{
			"job_code": "OPS-HRBP3", "grade": "P3", "org_unit": "people-ops",
			"pay_zone": "US-EAST",
		},
		"effective_date": "2026-06-01", "evaluation_date": "2026-05-15",
		"business_reason": "promotion_into_senior_hrbp",
		"current": map[string]any{
			"base": "93000.00", "currency": "USD", "pay_basis": "ANNUAL_SALARY",
			"bonus_target": "0.0500", "effective_date": "2026-06-01",
			"revision_stream": "rewards.package.omar", "revision_sequence": "11",
		},
		"proposed": map[string]any{
			"base": "98000.00", "currency": "USD", "pay_basis": "ANNUAL_SALARY",
			"bonus_target": "0.0500", "effective_date": "2026-06-01",
			"revision_stream": "rewards.package.omar", "revision_sequence": "11",
		},
		"budget": map[string]any{
			"available_amount": "50000.00", "currency": "USD", "owner_system": "adaptive.planning",
			"policy_ref": "finance.authority/2026.1", "scope": "people-ops:FY26-merit",
			"period": "FY2026", "baseline_version": "fy26-merit-r7", "observation_id": "obs_budget_fy26_merit_r7",
		},
	}
}

func TestTodo_CHAT_045_Integration(t *testing.T) {
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	store, err := pgstore.New(pool)
	if err != nil {
		t.Fatalf("pgstore.New: %v", err)
	}
	if err := store.Bootstrap(context.Background(), string(fixtures.Tenant)); err != nil {
		t.Fatalf("pgstore.Bootstrap: %v", err)
	}
	cell, err := intentapp.NewCell(intentapp.CellConfig{
		Store:      store,
		Verifier:   trust.VerifierFunc(func(context.Context, trust.Credential) (*trust.Principal, error) { return nil, nil }),
		Audience:   "hcm-next-api",
		RoleAccess: chat045Access{},
	})
	if err != nil {
		t.Fatalf("app.NewCell: %v", err)
	}

	author, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant:               fixtures.Tenant,
		Subject:              "chat045-author",
		SubjectKind:          trust.SubjectKindHuman,
		OrganizationScopeID:  "org-test",
		Roles:                []string{"comp_admin"},
		Purposes:             []string{authz.PurposeCompensationReview},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            trust.AssuranceHigh,
		SessionRef:           "session-chat045-author",
		IssuedAt:             time.Now().Add(-time.Minute),
		ExpiresAt:            time.Now().Add(time.Hour),
		CredentialDigest:     "credential-digest-chat045-author",
	})
	if err != nil {
		t.Fatalf("mint author principal: %v", err)
	}
	authorCtx := trust.WithPrincipal(context.Background(), author)

	// Install a visible agent for this conversation, exactly the shape
	// chatapps.Service.ProposeIntent requires: Active, with an Agent manifest,
	// its own AppID reflected consistently.
	const tenant, conversation, installationID = "hcmnext", "room-chat045", "hcmnext:room-chat045:promotion-assistant"
	repo := chatapps.NewMemoryRepository()
	if err := repo.Put(context.Background(), chatapps.Installation{
		ID: installationID, Tenant: tenant, Conversation: conversation, AppID: "promotion-assistant", Version: 1,
		Manifest: chatapps.Manifest{
			AppID: "promotion-assistant", Version: 1, Scopes: []string{"chat:invoke", "hcm:write"},
			Agent: &chatapps.AgentManifest{DisplayName: "Promotion Assistant", Triggers: []chatapps.TriggerSource{chatapps.TriggerMention}},
		},
		GrantedScopes: []string{"chat:invoke", "hcm:write"}, Status: chatapps.Active,
	}); err != nil {
		t.Fatalf("install agent: %v", err)
	}

	apps := &chatapps.Service{
		Repo:      repo,
		Secret:    []byte("chat045-integration-secret"),
		Now:       time.Now,
		Authority: chat045ConversationAuthority{tenant: tenant, conversation: conversation},
		// The exact production adapter composeChat wires as
		// chatapps.Service.Intent: a real IntentService, not a fake.
		Intent: chatAppsIntentAdapter{intent: cell.Service},
	}

	actor := chatapps.Actor{Tenant: tenant, Principal: author.Subject(), Conversation: conversation}
	worker, err := fixtures.WorkerRef("omar-reyes")
	if err != nil {
		t.Fatalf("resolve worker: %v", err)
	}
	proposal := chatapps.Proposal{
		Tenant: tenant, Principal: author.Subject(), Conversation: conversation,
		AgentInstallation: installationID,
		IntentType:        promotion.IntentType,
		IdempotencyKey:    "chat045-integration-1",
		Subjects:          []chatapps.ProposalSubject{{Kind: "EMPLOYMENT", ID: worker.Id, AuthorityDomain: "PEOPLE"}},
		SchemaID:          "hcmnext.people.v1.PromoteWorkerRequest",
		SchemaVersion:     1,
		ProtoFullName:     "hcmnext.people.v1.PromoteWorkerRequest",
		RequestPayload:    chat045PromotionPayload(),
	}

	receipt, err := apps.ProposeIntent(authorCtx, actor, proposal)
	if err != nil {
		t.Fatalf("ProposeIntent through the real application/intent path: %v", err)
	}
	if receipt.IntentID == "" {
		t.Fatal("ProposeIntent returned no durable intent id")
	}
	if receipt.Status == "" {
		t.Fatal("ProposeIntent returned no lifecycle status")
	}

	// The receipt is not merely a same-process return value: read the intent
	// back through the served GetIntent path, over the same pgtest-backed
	// store CreateIntent wrote it to.
	got, err := cell.Service.GetIntent(authorCtx, &intentsv1.GetIntentRequest{IntentId: receipt.IntentID})
	if err != nil {
		t.Fatalf("GetIntent(%s): %v", receipt.IntentID, err)
	}
	if got.GetIntent().GetIntentId() != receipt.IntentID {
		t.Fatalf("persisted intent id = %q, want %q", got.GetIntent().GetIntentId(), receipt.IntentID)
	}
	if got.GetIntent().GetDefinition().GetIntentTypeId() != promotion.IntentType {
		t.Fatalf("persisted definition = %q, want %q", got.GetIntent().GetDefinition().GetIntentTypeId(), promotion.IntentType)
	}
	if got.GetIntent().GetInitiator().GetPrincipalId() != author.Subject() {
		t.Fatalf("persisted initiator = %q, want the authenticated caller %q",
			got.GetIntent().GetInitiator().GetPrincipalId(), author.Subject())
	}

	// A forged proposal (installation's tenant does not match the caller's
	// authenticated tenant) must never reach CreateIntent: chatapps'
	// ProposeIntent authorization, exercised by TestTodo_CHAT_045_Security,
	// is what stands between the chat surface and this real intent path.
	forged := proposal
	forged.IdempotencyKey = "chat045-integration-forged"
	forged.Tenant = "other-tenant"
	if _, err := apps.ProposeIntent(authorCtx, chatapps.Actor{Tenant: "other-tenant", Principal: author.Subject(), Conversation: conversation}, forged); err == nil {
		t.Fatal("forged-tenant proposal reached the real intent path")
	}
}
