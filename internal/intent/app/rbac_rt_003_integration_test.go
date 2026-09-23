package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// TestTodo_RBAC_RT_003_Integration is the INTEGRATION contract: the full
// promotion lifecycle through a served-shaped cell over real PostgreSQL,
// with the durable role store wired. The author (comp_admin credential, no
// durable assignment) creates, reads, lists, simulates, submits and cancels
// omar-reyes's promotion; a peer whose credential still signs comp_admin
// but whose directory says worker_self is hidden everywhere; a roleless
// principal is refused the list itself.
//
// The label is literal: every row passes through pgstore and embedded
// PostgreSQL, every decision through the real policy engine.
func TestTodo_RBAC_RT_003_Integration(t *testing.T) {
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
	snapshot := roleaccess.Snapshot{Assignments: []roleaccess.Assignment{
		{WorkerRef: "peer-x", RoleIDs: []string{"worker_self"}},
	}}
	cell, err := app.NewCell(app.CellConfig{
		Store:      store,
		Verifier:   trust.VerifierFunc(func(context.Context, trust.Credential) (*trust.Principal, error) { return nil, nil }),
		Audience:   "hcm-next-api",
		RoleAccess: rt003Access{snapshot: snapshot},
	})
	if err != nil {
		t.Fatalf("app.NewCell: %v", err)
	}

	compReview := authz.PurposeCompensationReview
	selfView := authz.PurposeSelfService
	mint := func(subject string, roles []string, purpose string) *trust.Principal {
		t.Helper()
		p, err := trust.NewPrincipal(trust.PrincipalSpec{
			Tenant:               fixtures.Tenant,
			Subject:              subject,
			SubjectKind:          trust.SubjectKindHuman,
			OrganizationScopeID:  "org-test",
			Roles:                roles,
			Purposes:             []string{purpose},
			AuthenticationMethod: trust.AuthenticationMethodBearerToken,
			Assurance:            trust.AssuranceHigh,
			SessionRef:           "session-rt003int-" + subject,
			IssuedAt:             time.Now().Add(-time.Minute),
			ExpiresAt:            time.Now().Add(time.Hour),
			CredentialDigest:     "credential-digest-" + subject,
		})
		if err != nil {
			t.Fatalf("NewPrincipal(%s): %v", subject, err)
		}
		return p
	}
	author := mint("int-author", []string{"comp_admin"}, compReview)
	peer := mint("peer-x", []string{"comp_admin"}, compReview)
	noroles := mint("nobody-x", nil, selfView)
	authorCtx := trust.WithPrincipal(context.Background(), author)
	peerCtx := trust.WithPrincipal(context.Background(), peer)
	norolesCtx := trust.WithPrincipal(context.Background(), noroles)
	codeOf := func(err error) envelope.Code {
		t.Helper()
		if err == nil {
			t.Fatal("succeeded, want a refusal")
		}
		var owned *envelope.Error
		if !errors.As(err, &owned) {
			t.Fatalf("error is not an owned envelope error: %v", err)
		}
		return owned.Code()
	}

	created, err := cell.Service.CreateIntent(authorCtx, rt003PromotionRequest(t, author, "rt003int-omar"))
	if err != nil {
		t.Fatalf("CreateIntent as the author: %v", err)
	}
	intentID := created.GetIntent().GetIntentId()
	if intentID == "" {
		t.Fatal("CreateIntent minted no intent id")
	}
	if created.GetIntent().GetInitiator().GetPrincipalId() != "int-author" {
		t.Fatalf("initiator = %q, want the server-recorded author",
			created.GetIntent().GetInitiator().GetPrincipalId())
	}

	t.Run("reads", func(t *testing.T) {
		got, err := cell.Service.GetIntent(authorCtx, &intentsv1.GetIntentRequest{IntentId: intentID})
		if err != nil {
			t.Fatalf("GetIntent as the author: %v", err)
		}
		if got.GetIntent().GetIntentId() != intentID {
			t.Fatalf("GetIntent returned %q, want %q", got.GetIntent().GetIntentId(), intentID)
		}
		if _, err := cell.Service.ListIntentTimeline(authorCtx, &intentsv1.ListIntentTimelineRequest{IntentId: intentID}); err != nil {
			t.Fatalf("ListIntentTimeline as the author: %v", err)
		}
		if _, err := cell.Service.GetIntent(peerCtx, &intentsv1.GetIntentRequest{IntentId: intentID}); err == nil {
			t.Fatal("revoked peer GetIntent succeeded")
		} else if code := codeOf(err); code != envelope.CodeNotFound {
			t.Fatalf("revoked peer GetIntent code = %s, want NOT_FOUND", code)
		}
		if _, err := cell.Service.ListIntentTimeline(peerCtx, &intentsv1.ListIntentTimelineRequest{IntentId: intentID}); err == nil {
			t.Fatal("revoked peer ListIntentTimeline succeeded")
		} else if code := codeOf(err); code != envelope.CodeNotFound {
			t.Fatalf("revoked peer timeline code = %s, want NOT_FOUND", code)
		}
	})

	t.Run("lists", func(t *testing.T) {
		authorList, err := cell.Service.ListIntents(authorCtx, &intentsv1.ListIntentsRequest{})
		if err != nil {
			t.Fatalf("ListIntents as the author: %v", err)
		}
		found := false
		for _, m := range authorList.GetIntents() {
			if m.GetIntentId() == intentID {
				found = true
			}
		}
		if !found {
			t.Fatal("author list omits the created proposal")
		}
		peerList, err := cell.Service.ListIntents(peerCtx, &intentsv1.ListIntentsRequest{})
		if err != nil {
			t.Fatalf("ListIntents as the revoked peer: %v", err)
		}
		for _, m := range peerList.GetIntents() {
			if m.GetIntentId() == intentID {
				t.Fatal("revoked peer list discloses the proposal")
			}
		}
		if _, err := cell.Service.ListIntents(norolesCtx, &intentsv1.ListIntentsRequest{}); err == nil {
			t.Fatal("roleless ListIntents succeeded")
		} else if code := codeOf(err); code != envelope.CodePermissionDenied {
			t.Fatalf("roleless ListIntents code = %s, want PERMISSION_DENIED", code)
		}
	})

	t.Run("create refuses the revoked peer", func(t *testing.T) {
		if _, err := cell.Service.CreateIntent(peerCtx, rt003PromotionRequest(t, peer, "rt003int-peer")); err == nil {
			t.Fatal("revoked peer CreateIntent succeeded")
		} else if code := codeOf(err); code != envelope.CodePermissionDenied {
			t.Fatalf("revoked peer CreateIntent code = %s, want PERMISSION_DENIED", code)
		}
	})

	t.Run("simulate submit cancel", func(t *testing.T) {
		simulated, err := cell.Service.SimulateIntent(authorCtx, &intentsv1.SimulateIntentRequest{IntentId: intentID})
		if err != nil {
			t.Fatalf("SimulateIntent as the author: %v", err)
		}
		revision := simulated.GetSimulation().GetProposalRevisionId()
		if revision == "" {
			t.Fatal("author simulation minted no proposal revision")
		}
		if _, err := cell.Service.SimulateIntent(peerCtx, &intentsv1.SimulateIntentRequest{IntentId: intentID}); err == nil {
			t.Fatal("revoked peer SimulateIntent succeeded")
		} else if code := codeOf(err); code != envelope.CodePermissionDenied {
			t.Fatalf("revoked peer SimulateIntent code = %s, want PERMISSION_DENIED", code)
		}

		submitted, err := cell.Service.SubmitIntent(authorCtx, &intentsv1.SubmitIntentRequest{
			IdempotencyKey: "rt003int-submit", IntentId: intentID,
			ProposalRevisionId: revision, ExpectedInstanceVersion: 1,
		})
		if err != nil {
			t.Fatalf("SubmitIntent as the author: %v", err)
		}
		if got := submitted.GetIntent().GetLifecycle().GetRequest(); got != intentsv1.RequestState_REQUEST_STATE_SUBMITTED {
			t.Fatalf("submitted state = %s, want SUBMITTED", got)
		}
		if submitted.GetIntent().GetInstanceVersion() != 2 {
			t.Fatalf("submitted version = %d, want 2", submitted.GetIntent().GetInstanceVersion())
		}
		if _, err := cell.Service.SubmitIntent(peerCtx, &intentsv1.SubmitIntentRequest{
			IdempotencyKey: "rt003int-submit-peer", IntentId: intentID,
			ProposalRevisionId: revision, ExpectedInstanceVersion: 2,
		}); err == nil {
			t.Fatal("revoked peer SubmitIntent succeeded")
		} else if code := codeOf(err); code != envelope.CodeNotFound {
			t.Fatalf("revoked peer SubmitIntent code = %s, want NOT_FOUND", code)
		}

		cancelled, err := cell.Service.CancelIntent(authorCtx, &intentsv1.CancelIntentRequest{
			IdempotencyKey: "rt003int-cancel", IntentId: intentID,
			ExpectedInstanceVersion: 2, ReasonRef: "integration cancel",
		})
		if err != nil {
			t.Fatalf("CancelIntent as the author: %v", err)
		}
		if got := cancelled.GetIntent().GetLifecycle().GetRequest(); got != intentsv1.RequestState_REQUEST_STATE_CANCELLED {
			t.Fatalf("cancelled state = %s, want CANCELLED", got)
		}
		if cancelled.GetIntent().GetInstanceVersion() != 3 {
			t.Fatalf("cancelled version = %d, want 3", cancelled.GetIntent().GetInstanceVersion())
		}
	})
}

// rt003Access answers one fixed durable snapshot behind the integration
// cell: peer-x resolves to worker_self, everyone else to the admitted
// credential roles.
type rt003Access struct {
	roleaccess.Store
	snapshot roleaccess.Snapshot
}

func (s rt003Access) Load(context.Context, values.TenantId, string) (roleaccess.Snapshot, error) {
	return s.snapshot, nil
}

// rt003PromotionRequest builds the corpus's own certified-READY promotion
// scenario (Omar Reyes, OPS-HRBP2/P2 -> OPS-HRBP3/P3), so any refusal is
// authorization, never the payload.
func rt003PromotionRequest(t *testing.T, author *trust.Principal, key string) *intentsv1.CreateIntentRequest {
	t.Helper()
	worker, err := fixtures.WorkerRef("omar-reyes")
	if err != nil {
		t.Fatalf("resolve worker: %v", err)
	}
	raw, err := structpb.NewStruct(map[string]any{
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
	})
	if err != nil {
		t.Fatalf("encode request payload: %v", err)
	}
	wire, err := proto.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal request payload: %v", err)
	}
	return &intentsv1.CreateIntentRequest{
		IdempotencyKey: key,
		Definition:     &intentsv1.DefinitionReference{IntentTypeId: promotion.IntentType, Version: 1},
		Initiator: &intentsv1.PrincipalReference{
			PrincipalId: author.Subject(), Kind: intentsv1.InitiatorKind_INITIATOR_KIND_HUMAN,
			IdentityAssuranceRef: author.EvidenceID(),
		},
		Subjects: []*intentsv1.SubjectReference{
			{SubjectKind: "EMPLOYMENT", SubjectId: worker.Id, AuthorityDomain: "PEOPLE"},
		},
		Request: &intentsv1.TypedPayload{
			Schema: &intentsv1.SchemaReference{
				SchemaId: "hcmnext.people.v1.PromoteWorkerRequest", Version: 1,
				ProtobufFullName: "hcmnext.people.v1.PromoteWorkerRequest",
			},
			ProtobufWireBytes: wire,
		},
		ExecutionMode: intentsv1.ExecutionMode_EXECUTION_MODE_SIMULATE,
	}
}
