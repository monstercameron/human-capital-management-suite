package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/intelligence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/repair"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	intentdefinitions "github.com/monstercameron/human-capital-management-suite/internal/intent/definitions"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/endpoint"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// RBAC-RT-003 tests: intent reads and actions authorized by subject and
// relationship.
//
// The harness composes a real IntentService over the production promotion
// definition, the bound capability table, the fixture domain inputs against
// the corpus workforce, and the in-memory lifecycle store â€” with a fake
// durable role store and a fake worker locator standing in for the
// execution database. That split is what makes the relationship decisive:
// the corpus population carries no durable manager rows, so every
// management-chain fact comes from the fake locator alone, and every role
// comes from the fake snapshot or the admitted credential.

// rt003Tenant is the fixture tenant every principal and record shares.
var rt003Tenant = fixtures.Tenant

// rt003RoleAccess is a role-access store answering one fixed snapshot. A
// subject with a durable assignment resolves to it; anyone else falls back
// to the admitted credential roles, exactly like production.
type rt003RoleAccess struct {
	roleaccess.Store
	snapshot roleaccess.Snapshot
	err      error
}

func (s rt003RoleAccess) Bootstrap(context.Context, values.TenantId, string) error { return nil }

func (s rt003RoleAccess) Load(context.Context, values.TenantId, string) (roleaccess.Snapshot, error) {
	if s.err != nil {
		return roleaccess.Snapshot{}, s.err
	}
	return s.snapshot, nil
}

// rt003Worker is one fake population row: its key and its manager's key.
type rt003Worker struct {
	key     string
	manager string
	id      string
}

// rt003Locator resolves the fake population by key or by entity id and
// falls back to the corpus resolver for anything else (omar-reyes's
// canonical reference, for example, still resolves when the fake carries
// no Created row of its own).
type rt003Locator struct {
	byRef map[string]WorkerLocation
}

func (l rt003Locator) locate(ctx context.Context, tenant values.TenantId, ref string) (WorkerLocation, bool, error) {
	if w, ok := l.byRef[ref]; ok {
		return w, true, nil
	}
	return corpusWorkerLocator(ctx, tenant, ref)
}

// rt003Population builds the fake reporting structure. omar-reyes is the
// intent subject; dana manages him, gus manages dana (skip level), hana
// manages ivy on a separate branch under gus, and eli is dana's other
// report (omar's peer).
func rt003Population(t *testing.T) (WorkerLocator, string) {
	t.Helper()
	omar, err := fixtures.WorkerRef("omar-reyes")
	if err != nil {
		t.Fatalf("fixtures.WorkerRef(omar-reyes): %v", err)
	}
	knownAt := time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)
	workers := map[string]rt003Worker{
		"omar-reyes": {key: "omar-reyes", manager: "dana"},
		"dana":       {key: "dana", manager: "gus"},
		"gus":        {key: "gus", manager: ""},
		"hana":       {key: "hana", manager: "gus"},
		"ivy":        {key: "ivy", manager: "hana"},
		"eli":        {key: "eli", manager: "dana"},
	}
	byRef := map[string]WorkerLocation{}
	ids := map[string]string{}
	for key, w := range workers {
		var ref values.EntityRef
		if key == "omar-reyes" {
			ref = omar
		} else {
			ref = values.EntityRef{Tenant: rt003Tenant, Kind: people.KindWorker,
				Id: uuid.NewSHA1(uuid.NameSpaceURL, []byte("hcmnext:rt003:"+key)).String()}
		}
		ids[key] = ref.Id
		workerID, err := uuid.Parse(ref.Id)
		if err != nil {
			t.Fatalf("worker %s id %q is not a UUID: %v", key, ref.Id, err)
		}
		loc := WorkerLocation{Ref: ref, Key: key, Created: &workforce.WorkerRow{
			WorkerID: workerID, WorkerKey: key, ManagerRelationshipRef: w.manager,
			EffectiveFrom: "2026-01-01", KnownAt: knownAt, RecordedAt: knownAt,
		}}
		byRef[key] = loc
		byRef[ref.Id] = loc
	}
	locate := rt003Locator{byRef: byRef}.locate
	return locate, ids["omar-reyes"]
}

// rt003Principal mints one test principal. Credential roles and the durable
// snapshot are set independently so revocation (durable narrower than
// credential) is expressible.
func rt003Principal(t *testing.T, subject string, roles []string, purpose string) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant:               rt003Tenant,
		Subject:              subject,
		SubjectKind:          trust.SubjectKindHuman,
		OrganizationScopeID:  "org-test",
		Roles:                roles,
		Purposes:             []string{purpose},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            trust.AssuranceHigh,
		SessionRef:           "session-rt003-" + subject,
		IssuedAt:             time.Now().Add(-time.Minute),
		ExpiresAt:            time.Now().Add(time.Hour),
		CredentialDigest:     "credential-digest-" + subject,
	})
	if err != nil {
		t.Fatalf("NewPrincipal(%s): %v", err, subject)
	}
	return p
}

func rt003Ctx(p *trust.Principal) context.Context {
	return trust.WithPrincipal(context.Background(), p)
}

// rt003Harness is a real IntentService with the authorization ports wired:
// the fake durable role store, the fake locator, the bound capability
// table, fixture domain inputs (bound to the same locator, so simulation
// authorizes like creation does), and the in-memory lifecycle store.
type rt003Harness struct {
	Service *IntentService
	Store   *memLifecycleStore
	omarID  string
}

func newRT003Harness(t *testing.T, snapshot roleaccess.Snapshot, useBootstrapCaps bool) *rt003Harness {
	t.Helper()
	locate, omarID := rt003Population(t)
	reg, err := intentdefinitions.NewRegistry()
	if err != nil {
		t.Fatalf("intentdefinitions.NewRegistry: %v", err)
	}
	inputs, err := NewCorpusInputs()
	if err != nil {
		t.Fatalf("NewCorpusInputs: %v", err)
	}
	inputs.BindWorkerLocator(locate)
	caps := capability.NewRegistry()
	if useBootstrapCaps {
		bound, err := newCapabilityRegistry(&domainHandlers{
			workers: inputs.Workers(),
			bands:   inputs.Bands(),
			history: &workerFieldHistory{workers: inputs.Workers()},
		})
		if err != nil {
			t.Fatalf("newCapabilityRegistry: %v", err)
		}
		caps = bound
	}
	sink := NewMemoryEvidenceSink()
	digester, err := protomap.NewDefaultDigester()
	if err != nil {
		t.Fatalf("NewDefaultDigester: %v", err)
	}
	store := newMemLifecycleStore()
	fixedNow := time.Date(2026, time.September, 12, 10, 0, 0, 0, time.UTC)
	svc, err := NewIntentService(Options{
		Definitions:   reg,
		Capabilities:  caps,
		Gateway:       capability.NewGateway(caps, sink),
		Store:         store,
		Inputs:        inputs,
		Digester:      digester,
		Controls:      NewControls(reg, caps),
		Clock:         func() values.Instant { return values.NewInstant(fixedNow) },
		Evidence:      sink,
		Idempotency:   endpoint.NewCoordinator(),
		SafePoints:    newFakeSafePoints(),
		RoleAccess:    rt003RoleAccess{snapshot: snapshot},
		WorkerLocator: locate,
	})
	if err != nil {
		t.Fatalf("NewIntentService: %v", err)
	}
	return &rt003Harness{Service: svc, Store: store, omarID: omarID}
}

// rt003Snapshot assigns every population member its durable role. The
// revoked principal keeps a comp_admin credential while the directory says
// worker_self.
func rt003Snapshot() roleaccess.Snapshot {
	assignment := func(worker string, roles ...string) roleaccess.Assignment {
		return roleaccess.Assignment{WorkerRef: worker, RoleIDs: roles}
	}
	return roleaccess.Snapshot{Assignments: []roleaccess.Assignment{
		assignment("gus", "manager"),
		assignment("dana", "manager"),
		assignment("hana", "manager"),
		assignment("eli", "worker_self"),
		assignment("omar-reyes", "worker_self"),
		assignment("admin-x", "hcm_admin"),
		assignment("revoked-x", "worker_self"),
	}}
}

// rt003Create creates a promotion about omar-reyes as principal. The
// certified-READY corpus scenario keeps kernel admission green, so any
// refusal is the authorization gate, never the payload.
func rt003Create(t *testing.T, h *rt003Harness, p *trust.Principal, key string) *intentsv1.IntentInstance {
	t.Helper()
	created, err := h.Service.CreateIntent(rt003Ctx(p), promoteWorkerCreateRequest(t, p, key))
	if err != nil {
		t.Fatalf("CreateIntent(%s): %v", key, err)
	}
	return created.GetIntent()
}

// rt003Code extracts the owned error code, failing the test on success or
// on an unowned error.
func rt003Code(t *testing.T, err error) envelope.Code {
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

// rt003Reason extracts the owned error reason reference.
func rt003Reason(t *testing.T, err error) string {
	t.Helper()
	var owned *envelope.Error
	if !errors.As(err, &owned) {
		t.Fatalf("error is not an owned envelope error: %v", err)
	}
	return owned.ReasonRef()
}

// rt003Principals mints the population the PRIMARY test reads and acts as:
// gus (the initiator, skip-level manager), dana (the direct manager), hana
// (a manager outside the subject's chain), eli (a peer), omar-reyes (the
// subject himself), admin-x (the administrator), nobody (no roles), and
// revoked-x (a comp_admin credential over a worker_self directory).
func rt003Principals(t *testing.T) map[string]*trust.Principal {
	t.Helper()
	compReview := authz.PurposeCompensationReview
	selfView := authz.PurposeSelfService
	manager := string(authz.RoleManager)
	worker := string(authz.RoleWorkerSelf)
	return map[string]*trust.Principal{
		"gus":     rt003Principal(t, "gus", []string{manager}, compReview),
		"dana":    rt003Principal(t, "dana", []string{manager}, compReview),
		"hana":    rt003Principal(t, "hana", []string{manager}, compReview),
		"eli":     rt003Principal(t, "eli", []string{worker}, selfView),
		"omar":    rt003Principal(t, "omar-reyes", []string{worker}, selfView),
		"admin":   rt003Principal(t, "admin-x", []string{"hcm_admin"}, compReview),
		"noroles": rt003Principal(t, "nobody", nil, selfView),
		"revoked": rt003Principal(t, "revoked-x", []string{"comp_admin"}, compReview),
	}
}

// TestTodo_RBAC_RT_003 is the PRIMARY contract: reads require the caller to
// be the initiator, a participant, in the subject's management chain, or to
// hold a role granting the intent's data domain; lists are filtered before
// paging; actions require the capability for the intent type.
func TestTodo_RBAC_RT_003(t *testing.T) {
	h := newRT003Harness(t, rt003Snapshot(), true)
	principals := rt003Principals(t)
	ctx := func(name string) context.Context { return rt003Ctx(principals[name]) }

	// Three promotion intents: omar's (proposed by gus), eli's (proposed by
	// dana), and ivy's (proposed by hana). The eli and ivy vectors reuse the
	// certified payload shape with only the subject reference swapped, which
	// is legitimate here because creation never resolves the payload: the
	// gate under test authorizes the declared subjects.
	omarIntent := rt003Create(t, h, principals["gus"], "rt003-omar")
	eliReq := promoteWorkerCreateRequest(t, principals["dana"], "rt003-eli")
	eliID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("hcmnext:rt003:eli")).String()
	eliReq.Subjects[0].SubjectId = eliID
	eliCreated, err := h.Service.CreateIntent(ctx("dana"), eliReq)
	if err != nil {
		t.Fatalf("CreateIntent about eli as dana: %v", err)
	}
	eliIntent := eliCreated.GetIntent()
	ivyReq := promoteWorkerCreateRequest(t, principals["hana"], "rt003-ivy")
	ivyID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("hcmnext:rt003:ivy")).String()
	ivyReq.Subjects[0].SubjectId = ivyID
	ivyCreated, err := h.Service.CreateIntent(ctx("hana"), ivyReq)
	if err != nil {
		t.Fatalf("CreateIntent about ivy as hana: %v", err)
	}
	ivyIntent := ivyCreated.GetIntent()
	if omarIntent.GetInitiator().GetPrincipalId() != "gus" {
		t.Fatalf("omar intent initiator = %q, want the server-recorded proposer",
			omarIntent.GetInitiator().GetPrincipalId())
	}

	t.Run("reads", func(t *testing.T) {
		for _, tc := range []struct {
			name      string
			principal string
			intent    string
			allow     bool
		}{
			{"initiator reads own proposal", "gus", omarIntent.GetIntentId(), true},
			{"direct manager reads the subject's proposal", "dana", omarIntent.GetIntentId(), true},
			{"subject reads own proposal", "omar", omarIntent.GetIntentId(), true},
			{"administrator reads under the data-domain grant", "admin", omarIntent.GetIntentId(), true},
			{"peer is hidden", "eli", omarIntent.GetIntentId(), false},
			{"manager outside the chain is hidden", "hana", omarIntent.GetIntentId(), false},
			{"roleless principal is hidden", "noroles", omarIntent.GetIntentId(), false},
			{"revoked administrator is hidden", "revoked", omarIntent.GetIntentId(), false},
			{"skip-level manager reads the branch proposal", "gus", eliIntent.GetIntentId(), true},
			{"subject reads own branch proposal", "eli", eliIntent.GetIntentId(), true},
			{"branch manager reads own branch proposal", "hana", ivyIntent.GetIntentId(), true},
			{"other branch is hidden from the manager", "dana", ivyIntent.GetIntentId(), false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				got, err := h.Service.GetIntent(ctx(tc.principal), &intentsv1.GetIntentRequest{IntentId: tc.intent})
				if tc.allow {
					if err != nil {
						t.Fatalf("GetIntent(%s): %v", tc.principal, err)
					}
					if got.GetIntent().GetIntentId() != tc.intent {
						t.Fatalf("GetIntent returned %q, want %q", got.GetIntent().GetIntentId(), tc.intent)
					}
					return
				}
				if code := rt003Code(t, err); code != envelope.CodeNotFound {
					t.Fatalf("GetIntent(%s) code = %s, want NOT_FOUND", tc.principal, code)
				}
				if reason := rt003Reason(t, err); reason != reasonIntentNotFound {
					t.Fatalf("GetIntent(%s) reason = %q, want %q", tc.principal, reason, reasonIntentNotFound)
				}
			})
		}
	})

	t.Run("unknown stays unknown", func(t *testing.T) {
		_, err := h.Service.GetIntent(ctx("gus"), &intentsv1.GetIntentRequest{IntentId: "00000000-0000-7000-8000-000000000099"})
		if code := rt003Code(t, err); code != envelope.CodeNotFound {
			t.Fatalf("unknown GetIntent code = %s, want NOT_FOUND", code)
		}
	})

	t.Run("lists filter before paging", func(t *testing.T) {
		ids := func(msgs []*intentsv1.IntentInstance) map[string]bool {
			out := map[string]bool{}
			for _, m := range msgs {
				out[m.GetIntentId()] = true
			}
			return out
		}
		for _, tc := range []struct {
			name      string
			principal string
			want      []string
		}{
			{"senior manager sees every proposal under him", "gus", []string{omarIntent.GetIntentId(), eliIntent.GetIntentId(), ivyIntent.GetIntentId()}},
			{"manager sees the chain proposals", "dana", []string{omarIntent.GetIntentId(), eliIntent.GetIntentId()}},
			{"branch manager sees only the branch proposal", "hana", []string{ivyIntent.GetIntentId()}},
			{"peer sees only the proposal about herself", "eli", []string{eliIntent.GetIntentId()}},
			{"subject sees only the proposal about himself", "omar", []string{omarIntent.GetIntentId()}},
			{"administrator sees every proposal", "admin", []string{omarIntent.GetIntentId(), eliIntent.GetIntentId(), ivyIntent.GetIntentId()}},
			{"revoked administrator sees nothing", "revoked", nil},
		} {
			t.Run(tc.name, func(t *testing.T) {
				resp, err := h.Service.ListIntents(ctx(tc.principal), &intentsv1.ListIntentsRequest{
					Page: &commonv1.PageRequest{PageSize: 100},
				})
				if err != nil {
					t.Fatalf("ListIntents(%s): %v", tc.principal, err)
				}
				got := ids(resp.GetIntents())
				if len(got) != len(tc.want) {
					t.Fatalf("ListIntents(%s) = %v, want %v", tc.principal, got, tc.want)
				}
				for _, want := range tc.want {
					if !got[want] {
						t.Fatalf("ListIntents(%s) = %v, missing %v", tc.principal, got, want)
					}
				}
				if resp.GetPage() == nil {
					t.Fatalf("ListIntents(%s) returned no page", tc.principal)
				}
			})
		}
		// A roleless principal is refused the list, not handed an empty page.
		if _, err := h.Service.ListIntents(ctx("noroles"), &intentsv1.ListIntentsRequest{
			Page: &commonv1.PageRequest{PageSize: 100},
		}); err == nil {
			t.Fatal("roleless ListIntents succeeded, want a refusal")
		} else if code := rt003Code(t, err); code != envelope.CodePermissionDenied {
			t.Fatalf("roleless ListIntents code = %s, want PERMISSION_DENIED", code)
		}
	})

	t.Run("pages walk the filtered list", func(t *testing.T) {
		var walked []string
		cursor := ""
		for {
			resp, err := h.Service.ListIntents(ctx("gus"), &intentsv1.ListIntentsRequest{
				Page: &commonv1.PageRequest{PageSize: 1, Cursor: cursor},
			})
			if err != nil {
				t.Fatalf("ListIntents page: %v", err)
			}
			for _, m := range resp.GetIntents() {
				walked = append(walked, m.GetIntentId())
			}
			next := resp.GetPage().GetNextCursor()
			if next == "" {
				break
			}
			if next == cursor {
				t.Fatal("list cursor did not advance")
			}
			cursor = next
		}
		if len(walked) != 3 {
			t.Fatalf("walked %d rows, want the 3 visible proposals", len(walked))
		}
		seen := map[string]bool{}
		for _, id := range walked {
			seen[id] = true
		}
		if !seen[omarIntent.GetIntentId()] || !seen[eliIntent.GetIntentId()] || !seen[ivyIntent.GetIntentId()] {
			t.Fatalf("walked %v, want the omar, eli and ivy proposals", walked)
		}
	})

	t.Run("timelines follow visibility", func(t *testing.T) {
		if _, err := h.Service.ListIntentTimeline(ctx("dana"), &intentsv1.ListIntentTimelineRequest{IntentId: omarIntent.GetIntentId()}); err != nil {
			t.Fatalf("timeline as the manager: %v", err)
		}
		if _, err := h.Service.ListIntentTimeline(ctx("eli"), &intentsv1.ListIntentTimelineRequest{IntentId: omarIntent.GetIntentId()}); err == nil {
			t.Fatal("timeline as a peer succeeded, want NOT_FOUND")
		} else if code := rt003Code(t, err); code != envelope.CodeNotFound {
			t.Fatalf("peer timeline code = %s, want NOT_FOUND", code)
		}
	})

	t.Run("simulate refuses before resolving", func(t *testing.T) {
		// revoked-x carries a comp_admin credential the simulation would
		// admit; only the read gate, over the durable worker_self set,
		// refuses. That ordering is the assertion.
		_, err := h.Service.SimulateIntent(ctx("revoked"), &intentsv1.SimulateIntentRequest{IntentId: omarIntent.GetIntentId()})
		if code := rt003Code(t, err); code != envelope.CodePermissionDenied {
			t.Fatalf("revoked SimulateIntent code = %s, want PERMISSION_DENIED", code)
		}
		if !strings.Contains(err.Error(), "not authorized to read this under the resolved purpose") {
			t.Fatalf("revoked SimulateIntent refusal = %q, want the policy refusal", err.Error())
		}
		simulated, err := h.Service.SimulateIntent(ctx("admin"), &intentsv1.SimulateIntentRequest{IntentId: omarIntent.GetIntentId()})
		if err != nil {
			t.Fatalf("SimulateIntent as the administrator: %v", err)
		}
		if simulated.GetSimulation().GetProposalRevisionId() == "" {
			t.Fatal("administrator simulation minted no proposal revision")
		}
	})

	t.Run("creates require relationship or grant", func(t *testing.T) {
		for _, tc := range []struct {
			name      string
			principal string
		}{
			{"peer proposes about a colleague", "eli"},
			{"manager outside the chain proposes", "hana"},
			{"roleless principal proposes", "noroles"},
			{"revoked administrator proposes", "revoked"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, err := h.Service.CreateIntent(ctx(tc.principal), promoteWorkerCreateRequest(t, principals[tc.principal], "rt003-deny-"+tc.principal))
				if code := rt003Code(t, err); code != envelope.CodePermissionDenied {
					t.Fatalf("CreateIntent(%s) code = %s, want PERMISSION_DENIED", tc.principal, code)
				}
				if reason := rt003Reason(t, err); reason != reasonAuthorizationDenied {
					t.Fatalf("CreateIntent(%s) reason = %q, want %q", tc.principal, reason, reasonAuthorizationDenied)
				}
			})
		}
	})

	t.Run("submit and cancel", func(t *testing.T) {
		simulated, err := h.Service.SimulateIntent(ctx("gus"), &intentsv1.SimulateIntentRequest{IntentId: omarIntent.GetIntentId()})
		if err != nil {
			t.Fatalf("SimulateIntent as the initiator: %v", err)
		}
		revision := simulated.GetSimulation().GetProposalRevisionId()
		if revision == "" {
			t.Fatal("initiator simulation minted no proposal revision")
		}
		submitted, err := h.Service.SubmitIntent(ctx("gus"), &intentsv1.SubmitIntentRequest{
			IdempotencyKey: "rt003-submit", IntentId: omarIntent.GetIntentId(),
			ProposalRevisionId: revision, ExpectedInstanceVersion: 1,
		})
		if err != nil {
			t.Fatalf("SubmitIntent as the initiator: %v", err)
		}
		if got := submitted.GetIntent().GetLifecycle().GetRequest(); got != intentsv1.RequestState_REQUEST_STATE_SUBMITTED {
			t.Fatalf("submitted request state = %s, want SUBMITTED", got)
		}
		if submitted.GetIntent().GetInstanceVersion() != omarIntent.GetInstanceVersion()+1 {
			t.Fatalf("submitted version = %d, want %d",
				submitted.GetIntent().GetInstanceVersion(), omarIntent.GetInstanceVersion()+1)
		}
		for _, tc := range []struct {
			name      string
			principal string
			call      func() error
		}{
			{"peer submits", "eli", func() error {
				_, err := h.Service.SubmitIntent(ctx("eli"), &intentsv1.SubmitIntentRequest{
					IdempotencyKey: "rt003-submit-eli", IntentId: omarIntent.GetIntentId(),
					ProposalRevisionId: revision, ExpectedInstanceVersion: 2,
				})
				return err
			}},
			{"revoked administrator submits", "revoked", func() error {
				_, err := h.Service.SubmitIntent(ctx("revoked"), &intentsv1.SubmitIntentRequest{
					IdempotencyKey: "rt003-submit-revoked", IntentId: omarIntent.GetIntentId(),
					ProposalRevisionId: revision, ExpectedInstanceVersion: 2,
				})
				return err
			}},
			{"peer cancels", "eli", func() error {
				_, err := h.Service.CancelIntent(ctx("eli"), &intentsv1.CancelIntentRequest{
					IdempotencyKey: "rt003-cancel-eli", IntentId: omarIntent.GetIntentId(),
					ExpectedInstanceVersion: 2, ReasonRef: "peer cancel",
				})
				return err
			}},
			{"peer supersedes", "eli", func() error {
				_, err := h.Service.SupersedeIntent(ctx("eli"), &intentsv1.SupersedeIntentRequest{
					IdempotencyKey: "rt003-supersede-eli", SupersededIntentId: omarIntent.GetIntentId(),
					ExpectedInstanceVersion: 2, ReasonRef: "peer supersede",
					Definition: &intentsv1.DefinitionReference{IntentTypeId: promotion.IntentType, Version: 1},
				})
				return err
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if code := rt003Code(t, tc.call()); code != envelope.CodeNotFound {
					t.Fatalf("%s code = %s, want NOT_FOUND", tc.name, code)
				}
			})
		}
		cancelled, err := h.Service.CancelIntent(ctx("dana"), &intentsv1.CancelIntentRequest{
			IdempotencyKey: "rt003-cancel", IntentId: omarIntent.GetIntentId(),
			ExpectedInstanceVersion: 2, ReasonRef: "manager cancel",
		})
		if err != nil {
			t.Fatalf("CancelIntent as the manager: %v", err)
		}
		if got := cancelled.GetIntent().GetLifecycle().GetRequest(); got != intentsv1.RequestState_REQUEST_STATE_CANCELLED {
			t.Fatalf("cancelled request state = %s, want CANCELLED", got)
		}
		if cancelled.GetIntent().GetInstanceVersion() != submitted.GetIntent().GetInstanceVersion()+1 {
			t.Fatalf("cancelled version = %d, want %d",
				cancelled.GetIntent().GetInstanceVersion(), submitted.GetIntent().GetInstanceVersion()+1)
		}
		if len(cancelled.GetIntent().GetCancellationDecisions()) == 0 {
			t.Fatal("cancelled intent carries no cancellation decision")
		}
	})
}

// TestTodo_RBAC_RT_003_Security is the SECURITY contract: unauthenticated
// callers are refused before anything else, tenants stay isolated, cursors
// are caller-bound, an unpublished capability fails closed, a claimed
// initiator buys no authority, and a revoked manager is refused even
// though the credential still carries the manager role the chain fact was
// built from.
func TestTodo_RBAC_RT_003_Security(t *testing.T) {
	snap := rt003Snapshot()
	snap.Assignments = append(snap.Assignments,
		roleaccess.Assignment{WorkerRef: "revokedmgr-x", RoleIDs: []string{"worker_self"}})
	h := newRT003Harness(t, snap, true)
	principals := rt003Principals(t)
	revokedMgr := rt003Principal(t, "revokedmgr-x", []string{string(authz.RoleManager)}, authz.PurposeCompensationReview)
	ctx := func(name string) context.Context { return rt003Ctx(principals[name]) }

	omarIntent := rt003Create(t, h, principals["gus"], "rt003sec-omar")
	// A second proposal about eli gives gus two visible rows, so the
	// cursor test below has a cursor worth stealing.
	eliReq := promoteWorkerCreateRequest(t, principals["dana"], "rt003sec-eli")
	eliReq.Subjects[0].SubjectId = uuid.NewSHA1(uuid.NameSpaceURL, []byte("hcmnext:rt003:eli")).String()
	if _, err := h.Service.CreateIntent(ctx("dana"), eliReq); err != nil {
		t.Fatalf("CreateIntent about eli as dana: %v", err)
	}

	t.Run("unauthenticated is refused first", func(t *testing.T) {
		unauth := context.Background()
		calls := map[string]func() error{
			"GetIntent": func() error {
				_, err := h.Service.GetIntent(unauth, &intentsv1.GetIntentRequest{IntentId: omarIntent.GetIntentId()})
				return err
			},
			"ListIntents": func() error {
				_, err := h.Service.ListIntents(unauth, &intentsv1.ListIntentsRequest{})
				return err
			},
			"ListIntentTimeline": func() error {
				_, err := h.Service.ListIntentTimeline(unauth, &intentsv1.ListIntentTimelineRequest{IntentId: omarIntent.GetIntentId()})
				return err
			},
			"SimulateIntent": func() error {
				_, err := h.Service.SimulateIntent(unauth, &intentsv1.SimulateIntentRequest{IntentId: omarIntent.GetIntentId()})
				return err
			},
			"CreateIntent": func() error {
				_, err := h.Service.CreateIntent(unauth, promoteWorkerCreateRequest(t, principals["gus"], "rt003sec-unauth"))
				return err
			},
			"SubmitIntent": func() error {
				_, err := h.Service.SubmitIntent(unauth, &intentsv1.SubmitIntentRequest{
					IdempotencyKey: "rt003sec-unauth", IntentId: omarIntent.GetIntentId(),
					ProposalRevisionId: "r", ExpectedInstanceVersion: 1,
				})
				return err
			},
			"CancelIntent": func() error {
				_, err := h.Service.CancelIntent(unauth, &intentsv1.CancelIntentRequest{
					IdempotencyKey: "rt003sec-unauth", IntentId: omarIntent.GetIntentId(),
					ExpectedInstanceVersion: 1, ReasonRef: "r",
				})
				return err
			},
			"SupersedeIntent": func() error {
				_, err := h.Service.SupersedeIntent(unauth, &intentsv1.SupersedeIntentRequest{
					IdempotencyKey: "rt003sec-unauth", SupersededIntentId: omarIntent.GetIntentId(),
					ExpectedInstanceVersion: 1, ReasonRef: "r",
					Definition: &intentsv1.DefinitionReference{IntentTypeId: promotion.IntentType, Version: 1},
				})
				return err
			},
		}
		for name, call := range calls {
			if code := rt003Code(t, call()); code != envelope.CodeUnauthenticated {
				t.Fatalf("%s code = %s, want UNAUTHENTICATED", name, code)
			}
		}
	})

	t.Run("tenants stay isolated", func(t *testing.T) {
		foreign, err := trust.NewPrincipal(trust.PrincipalSpec{
			Tenant:               values.TenantId("other-tenant"),
			Subject:              "dana",
			SubjectKind:          trust.SubjectKindHuman,
			OrganizationScopeID:  "org-test",
			Roles:                []string{string(authz.RoleManager)},
			Purposes:             []string{authz.PurposeCompensationReview},
			AuthenticationMethod: trust.AuthenticationMethodBearerToken,
			Assurance:            trust.AssuranceHigh,
			SessionRef:           "session-foreign",
			IssuedAt:             time.Now().Add(-time.Minute),
			ExpiresAt:            time.Now().Add(time.Hour),
			CredentialDigest:     "credential-digest-foreign",
		})
		if err != nil {
			t.Fatalf("NewPrincipal(foreign): %v", err)
		}
		foreignCtx := trust.WithPrincipal(context.Background(), foreign)
		if _, err := h.Service.GetIntent(foreignCtx, &intentsv1.GetIntentRequest{IntentId: omarIntent.GetIntentId()}); err == nil {
			t.Fatal("cross-tenant GetIntent succeeded")
		} else if code := rt003Code(t, err); code != envelope.CodeNotFound {
			t.Fatalf("cross-tenant GetIntent code = %s, want NOT_FOUND", code)
		}
		resp, err := h.Service.ListIntents(foreignCtx, &intentsv1.ListIntentsRequest{})
		if err != nil {
			t.Fatalf("cross-tenant ListIntents: %v", err)
		}
		if len(resp.GetIntents()) != 0 {
			t.Fatalf("cross-tenant ListIntents disclosed %d rows", len(resp.GetIntents()))
		}
	})

	t.Run("cursors are caller-bound", func(t *testing.T) {
		first, err := h.Service.ListIntents(ctx("gus"), &intentsv1.ListIntentsRequest{
			Page: &commonv1.PageRequest{PageSize: 1},
		})
		if err != nil {
			t.Fatalf("ListIntents: %v", err)
		}
		cursor := first.GetPage().GetNextCursor()
		if cursor == "" {
			t.Skip("single-row store produced no cursor to steal")
		}
		if _, err := h.Service.ListIntents(ctx("eli"), &intentsv1.ListIntentsRequest{
			Page: &commonv1.PageRequest{PageSize: 1, Cursor: cursor},
		}); err == nil {
			t.Fatal("a foreign cursor was honored")
		} else if code := rt003Code(t, err); code != envelope.CodeInvalidArgument {
			t.Fatalf("foreign cursor code = %s, want INVALID_ARGUMENT", code)
		}
		if _, err := h.Service.ListIntents(ctx("gus"), &intentsv1.ListIntentsRequest{
			Page: &commonv1.PageRequest{PageSize: 1, Cursor: "not-a-cursor"},
		}); err == nil {
			t.Fatal("a garbage cursor was honored")
		} else if code := rt003Code(t, err); code != envelope.CodeInvalidArgument {
			t.Fatalf("garbage cursor code = %s, want INVALID_ARGUMENT", code)
		}
	})

	t.Run("unpublished capability fails closed", func(t *testing.T) {
		bare := newRT003Harness(t, rt003Snapshot(), false)
		_, err := bare.Service.CreateIntent(ctx("admin"), promoteWorkerCreateRequest(t, principals["admin"], "rt003sec-nocaps"))
		if code := rt003Code(t, err); code != envelope.CodePermissionDenied {
			t.Fatalf("capability-less CreateIntent code = %s, want PERMISSION_DENIED", code)
		}
		if reason := rt003Reason(t, err); reason != reasonAuthorizationDenied {
			t.Fatalf("capability-less CreateIntent reason = %q, want %q", reason, reasonAuthorizationDenied)
		}
		// Reads never depended on the capability table: copy one stored
		// record across and prove it stays visible to the grant holder.
		rec, err := h.Store.LoadIntent(context.Background(), string(rt003Tenant), omarIntent.GetIntentId())
		if err != nil {
			t.Fatalf("LoadIntent: %v", err)
		}
		bare.Store.mu.Lock()
		bare.Store.byKey[memStoreKey(string(rt003Tenant), rec.IntentID)] = rec
		bare.Store.mu.Unlock()
		got, err := bare.Service.GetIntent(ctx("admin"), &intentsv1.GetIntentRequest{IntentId: omarIntent.GetIntentId()})
		if err != nil {
			t.Fatalf("capability-less GetIntent: %v", err)
		}
		if got.GetIntent().GetIntentId() != omarIntent.GetIntentId() {
			t.Fatalf("capability-less GetIntent returned %q", got.GetIntent().GetIntentId())
		}
	})

	t.Run("claimed authorship buys nothing", func(t *testing.T) {
		forged := promoteWorkerCreateRequest(t, principals["eli"], "rt003sec-forged")
		forged.Initiator.PrincipalId = "eli"
		if _, err := h.Service.CreateIntent(ctx("eli"), forged); err == nil {
			t.Fatal("self-initiated peer proposal succeeded")
		} else if code := rt003Code(t, err); code != envelope.CodePermissionDenied {
			t.Fatalf("forged-initiator CreateIntent code = %s, want PERMISSION_DENIED", code)
		}
	})

	t.Run("revoked manager loses the chain", func(t *testing.T) {
		// revokedmgr-x still signs the manager role, so the chain fact is
		// built; the durable worker_self set must still refuse.
		if _, err := h.Service.GetIntent(rt003Ctx(revokedMgr), &intentsv1.GetIntentRequest{IntentId: omarIntent.GetIntentId()}); err == nil {
			t.Fatal("revoked-manager GetIntent succeeded")
		} else if code := rt003Code(t, err); code != envelope.CodeNotFound {
			t.Fatalf("revoked-manager GetIntent code = %s, want NOT_FOUND", code)
		}
		if _, err := h.Service.SubmitIntent(rt003Ctx(revokedMgr), &intentsv1.SubmitIntentRequest{
			IdempotencyKey: "rt003sec-revokedmgr", IntentId: omarIntent.GetIntentId(),
			ProposalRevisionId: "r", ExpectedInstanceVersion: 1,
		}); err == nil {
			t.Fatal("revoked-manager SubmitIntent succeeded")
		} else if code := rt003Code(t, err); code != envelope.CodeNotFound {
			t.Fatalf("revoked-manager SubmitIntent code = %s, want NOT_FOUND", code)
		}
	})

	t.Run("roleless principal acts on nothing", func(t *testing.T) {
		if _, err := h.Service.CreateIntent(ctx("noroles"), promoteWorkerCreateRequest(t, principals["noroles"], "rt003sec-noroles")); err == nil {
			t.Fatal("roleless CreateIntent succeeded")
		} else if code := rt003Code(t, err); code != envelope.CodePermissionDenied {
			t.Fatalf("roleless CreateIntent code = %s, want PERMISSION_DENIED", code)
		}
		if _, err := h.Service.SubmitIntent(ctx("noroles"), &intentsv1.SubmitIntentRequest{
			IdempotencyKey: "rt003sec-noroles", IntentId: omarIntent.GetIntentId(),
			ProposalRevisionId: "r", ExpectedInstanceVersion: 1,
		}); err == nil {
			t.Fatal("roleless SubmitIntent succeeded")
		} else if code := rt003Code(t, err); code != envelope.CodeNotFound {
			t.Fatalf("roleless SubmitIntent code = %s, want NOT_FOUND", code)
		}
	})
}

// TestIntentAuthFieldMapping pins the GREEN mapping one intent type at a
// time: the policy fields each envelope discloses mirror the governed read
// the domain resolvers already evaluate, the authorization subject prefers
// the resolved worker, and every refusal shape projects onto its promised
// contract. It also covers the resolver error fallback and the corpus
// locator default the gates degrade to.
func TestIntentAuthFieldMapping(t *testing.T) {
	pay := []authz.FieldID{authz.FieldBaseSalary, authz.FieldBonusTarget}
	for _, tc := range []struct {
		name     string
		typeID   string
		wantGate []authz.FieldID
		wantRead []authz.FieldID
	}{
		{"promotion carries pay", promotion.IntentType, pay, peopleFields(promotion.RequiredWorkerFields())},
		{"worker-state explanation reads the worker", people.ExplainWorkerStateIntentType, nil, peopleFields(people.AllFields())},
		{"compensation simulation carries pay", rewards.SimulateCompensationIntentType, pay, nil},
		{"pay-band evaluation carries base pay", rewards.EvaluatePayBandIntentType, []authz.FieldID{authz.FieldBaseSalary}, nil},
		{"drift detection reads the comparison", dataops.DetectDriftIntentType, nil, peopleFields(comparisonPeopleFields())},
		{"repair creation reads the comparison", repair.CreateRepairPlanIntentType, nil, peopleFields(comparisonPeopleFields())},
		{"repair simulation reads the comparison", repair.SimulateRepairIntentType, nil, peopleFields(comparisonPeopleFields())},
		{"transaction explanation reads the identifier", intelligence.ExplainTransactionIntentType, nil, []authz.FieldID{authz.FieldWorkerNumber}},
		{"unknown types fall back to the non-sensitive core", "hcmnext.test.unbound", nil, []authz.FieldID{authz.FieldWorkerNumber, authz.FieldJobTitle}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gate, read := intentAuthFields(tc.typeID)
			if !slices.Equal(gate, tc.wantGate) || !slices.Equal(read, tc.wantRead) {
				t.Fatalf("intentAuthFields(%q) = (%v, %v), want (%v, %v)",
					tc.typeID, gate, read, tc.wantGate, tc.wantRead)
			}
		})
	}

	omarRef := values.EntityRef{Tenant: rt003Tenant, Kind: people.KindWorker, Id: "omar-reyes"}
	workers := []WorkerLocation{{Ref: omarRef, Key: "omar-reyes"}}
	subjects := []intent.SubjectReference{{Kind: "EMPLOYMENT", SubjectID: "omar-reyes", AuthorityDomain: "PEOPLE"}}
	if got, err := intentAuthSubject(rt003Tenant, subjects, workers); err != nil || got != omarRef {
		t.Fatalf("worker subject = %+v, %v, want %+v", got, err, omarRef)
	}
	txnSubjects := []intent.SubjectReference{{Kind: "BUSINESS_TRANSACTION", SubjectID: uuid.NewString(), AuthorityDomain: "FINANCE"}}
	txnRef, err := intentAuthSubject(rt003Tenant, txnSubjects, nil)
	if err != nil || txnRef.Id != txnSubjects[0].SubjectID {
		t.Fatalf("transaction subject = %+v, %v", txnRef, err)
	}
	for _, tc := range []struct {
		name     string
		subjects []intent.SubjectReference
	}{
		{"invalid reference authorizes nothing", []intent.SubjectReference{{Kind: "!!!", SubjectID: "!!!", AuthorityDomain: "PEOPLE"}}},
		{"no subject authorizes nothing", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := intentAuthSubject(rt003Tenant, tc.subjects, nil); !errors.Is(err, ErrAuthorizationDenied) {
				t.Fatalf("intentAuthSubject error = %v, want ErrAuthorizationDenied", err)
			}
		})
	}

	if got := readRefusal(fmt.Errorf("%w: hidden", ErrAuthorizationDenied)); got.Code() != envelope.CodeNotFound {
		t.Fatalf("policy refusal code = %s, want NOT_FOUND", got.Code())
	}
	if got := readRefusal(errors.New("cell fault")); got.Code() != envelope.CodeUnavailable {
		t.Fatalf("cell fault code = %s, want UNAVAILABLE", got.Code())
	}

	negative, _ := json.Marshal(intentListCursor{Owner: string(rt003Tenant) + "|gus", Offset: -1})
	if _, err := decodeIntentCursor(base64.RawURLEncoding.EncodeToString(negative), string(rt003Tenant), "gus"); err == nil {
		t.Fatal("a negative-offset cursor was honored")
	} else if code := rt003Code(t, err); code != envelope.CodeInvalidArgument {
		t.Fatalf("negative cursor code = %s, want INVALID_ARGUMENT", code)
	}

	var bare IntentService
	locate := bare.intentLocator()
	if locate == nil {
		t.Fatal("the locator default is nil")
	}
	if _, found, err := locate(context.Background(), rt003Tenant, "omar-reyes"); err != nil || !found {
		t.Fatalf("corpus fallback resolves omar-reyes: found=%v err=%v", found, err)
	}

	h := newRT003Harness(t, rt003Snapshot(), true)
	h.Service.roleAccess = rt003RoleAccess{err: errors.New("role store down")}
	p := rt003Principal(t, "dana", []string{"manager"}, authz.PurposeCompensationReview)
	if got := h.Service.effectiveIntentRoles(context.Background(), p); !slices.Equal(got, p.Roles()) {
		t.Fatalf("resolution failure roles = %v, want the admitted credential roles %v", got, p.Roles())
	}

	// A purpose the principal does not hold refuses at the capability,
	// before the subject rule is even evaluated.
	def, err := h.Service.defs.Resolve(intent.Ref{TypeID: promotion.IntentType, Version: 1})
	if err != nil {
		t.Fatalf("resolve promotion: %v", err)
	}
	omarWorker, err := fixtures.WorkerRef("omar-reyes")
	if err != nil {
		t.Fatalf("fixtures.WorkerRef: %v", err)
	}
	capErr, subjErr := h.Service.authorizeIntentAction(rt003Ctx(p), p, "workforce_analytics", def,
		rt003Tenant, []intent.SubjectReference{{Kind: "EMPLOYMENT", SubjectID: omarWorker.Id, AuthorityDomain: "PEOPLE"}},
		"", h.Service.clock())
	if subjErr != nil {
		t.Fatalf("purpose refusal evaluated the subject rule: %v", subjErr)
	}
	if capErr == nil || capErr.Code() != envelope.CodePermissionDenied {
		t.Fatalf("purpose refusal = %v, want PERMISSION_DENIED", capErr)
	}
}
