package application

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const agentdoc007Tenant = "agentdoc-handoff"

type agentdoc007World struct {
	documents  *documenthubstore.Store
	personas   *agentpersonastore.TenantStore
	root       *agentpersonastore.Store
	eval       *PersonaAdminEvaluationService
	actor      PersonaAdminCommandActor
	tenant     values.TenantId
	tenantUUID uuid.UUID
	agentDB    *pgtest.DB
	now        time.Time
	runSeq     int
	suiteID    string
}

func agentdoc007WorldSetup(t *testing.T) *agentdoc007World {
	t.Helper()
	ctx := context.Background()
	tenant := values.TenantId(agentdoc007Tenant)
	documents := documentServiceFixture(t).store

	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenantUUID := uuid.New()
	db.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1)`, tenantUUID)
	mapper := func(value values.TenantId) uuid.UUID {
		if value == tenant {
			return tenantUUID
		}
		return uuid.Nil
	}
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+agentstore.AppRole); err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Second).Truncate(time.Microsecond)
	suite := agenteval.PolicyHelperSuite(personaPolicyHelperSkillID)
	keyID, err := PersonaEvaluationVerificationKeyID(string(tenant), suite.ID, localAgentDemoEvaluationKeyID)
	if err != nil {
		t.Fatal(err)
	}
	reviews, err := agentpersonastore.NewDurableReviewAuthority(mapper)
	if err != nil {
		t.Fatal(err)
	}
	evaluations, err := agentpersonastore.NewEvaluationSealAuthority(map[string]ed25519.PublicKey{keyID: publicKey}, mapper, func() time.Time { return now.Add(time.Minute) })
	if err != nil {
		t.Fatal(err)
	}
	root, err := agentpersonastore.NewWithPublicationAuthorities(conn, mapper, reviews, evaluations)
	if err != nil {
		t.Fatal(err)
	}
	scoped, err := root.Scoped(tenant)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: tenant, Subject: "admin", SubjectKind: trust.SubjectKindHuman, Roles: []string{"hcm_admin"}, OrganizationScopeID: "org", AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "agentdoc-handoff", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "sha256:agentdoc-handoff"})
	if err != nil {
		t.Fatal(err)
	}
	world := &agentdoc007World{documents: documents, personas: scoped, root: root, actor: PersonaAdminCommandActor{Principal: principal, Tenant: tenant, Subject: principal.Subject()}, tenant: tenant, tenantUUID: tenantUUID, agentDB: db, now: now, suiteID: suite.ID}
	world.eval = &PersonaAdminEvaluationService{Store: personaAdminLifecycleStoreAdapter{store: root}, Versions: personaAdminInstallationVersions{store: root}, Recorder: &PersonaEvaluationEvidenceService{store: root}, Executor: agentUXSetupEvaluationExecutor{now: now}, SyntheticTenant: "agentdoc-handoff-synthetic", KeyID: localAgentDemoEvaluationKeyID, PrivateKey: privateKey, ModelDigest: "sha256:" + strings.Repeat("d", 64), Now: func() time.Time { return now.Add(time.Second) }, FreshFor: time.Hour, NewRunID: func() string { world.runSeq++; return "handoff-evaluation-" + strings.Repeat("r", world.runSeq) }}
	return world
}

func (w *agentdoc007World) seedPolicyDocument(t *testing.T, owner, reader string) (documentID string, grant documenthubstore.Grant) {
	t.Helper()
	ctx := context.Background()
	documentID, err := w.documents.CreateDocument(ctx, string(w.tenant), owner, "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v1, err := w.documents.SubmitCandidate(ctx, string(w.tenant), documenthubstore.Version{DocumentID: documentID, CreatorID: owner, Title: "Leave policy", Markdown: "# Leave\n\nVersion one leave allowance is 20 days.\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	deployDocumentVersion(t, w.documents, string(w.tenant), owner, documentID, v1.ID, "")
	v2, err := w.documents.SubmitCandidate(ctx, string(w.tenant), documenthubstore.Version{DocumentID: documentID, CreatorID: owner, Title: "Leave policy", Markdown: "# Leave\n\nVersion two leave allowance is 25 days.\n"}, v1.ID)
	if err != nil {
		t.Fatal(err)
	}
	deployDocumentVersion(t, w.documents, string(w.tenant), owner, documentID, v2.ID, v1.ID)
	grant, err = w.documents.ShareDocument(ctx, string(w.tenant), documentID, owner, documenthubstore.GrantInput{SubjectKind: "person", SubjectID: reader, Action: documenthubstore.ActionRead, Effect: documenthubstore.EffectAllow})
	if err != nil {
		t.Fatal(err)
	}
	return documentID, grant
}

func (w *agentdoc007World) draftWithReferences(t *testing.T, version int64, refs []agentdocref.Reference) agentpersonastore.PersonaVersion {
	t.Helper()
	ctx := context.Background()
	starter, _ := agenttemplate.PersonaStarterFor("hcmnext.persona_template.policy_helper", 1)
	profile := agentpersona.PersonaProfile{
		Manifest: agentpersona.AgentManifestRef{ID: "agent.policy-helper", Version: 1, SchemaVersion: 1, Digest: "manifest"}, PersonaID: "policy-helper-docs", Version: uint32(version),
		Handle: "policy-helper-docs", DisplayName: "Policy Helper", AvatarRef: "avatar", Purpose: "Answer policy questions", Owner: "admin", Steward: "steward",
		Audience: agentpersona.Audience{Roles: []string{"hcm_admin"}, Populations: []string{"employees"}, OrganizationScopes: []string{"org:agentdoc-handoff"}}, SkillPins: []agentskills.SkillPin{starter.SkillPins[0]}, TierCeiling: agentskills.TierT0,
		ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationChannel}, ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPrivate},
		Instructions: "Use approved policy sources.", DocumentReferences: refs, EvalSuiteRef: w.suiteID, EvalLimits: agentpersona.EvaluationLimits{MaxCost: 1_000_000, MaxSteps: 8, MaxLatencyMS: 120_000},
	}
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(sealed.Profile)
	if err != nil {
		t.Fatal(err)
	}
	row := agentpersonastore.PersonaVersion{TenantID: w.tenant, PersonaID: profile.PersonaID, Version: version, AgentVersion: "agent.policy-helper@1", Handle: profile.Handle, DisplayName: profile.DisplayName, Profile: body, ContentDigest: sealed.Digest, CreatedAt: w.now}
	if version == 1 {
		if err := w.personas.CreateDraft(ctx, row, agentpersonastore.PersonaOwner{TenantID: w.tenant, PersonaID: row.PersonaID, Role: agentpersonastore.BusinessOwner, PrincipalID: "admin"}, agentpersonastore.PersonaOwner{TenantID: w.tenant, PersonaID: row.PersonaID, Role: agentpersonastore.TechnicalSteward, PrincipalID: "steward"}, "admin", w.now); err != nil {
			t.Fatal(err)
		}
	} else if err := w.personas.PutVersion(ctx, row); err != nil {
		t.Fatal(err)
	}
	if version > 1 {
		if err := w.personas.AppendLifecycle(ctx, agentpersonastore.LifecycleEvent{TenantID: w.tenant, EventID: "draft-v" + string(rune('0'+version)), PersonaID: row.PersonaID, PersonaVersion: version, To: agentpersonastore.StateDraft, Reason: "new draft", ActorID: "admin", OccurredAt: w.now}); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.personas.AppendLifecycle(ctx, agentpersonastore.LifecycleEvent{TenantID: w.tenant, EventID: "request-review-v" + string(rune('0'+version)), PersonaID: row.PersonaID, PersonaVersion: version, From: agentpersonastore.StateDraft, To: agentpersonastore.StateInReview, Reason: "independent review", ActorID: "admin", OccurredAt: w.now}); err != nil {
		t.Fatal(err)
	}
	return row
}

func (w *agentdoc007World) reviewAndPublish(t *testing.T, row agentpersonastore.PersonaVersion, reviewID string) {
	t.Helper()
	ctx := trust.WithPrincipal(context.Background(), w.actor.Principal)
	w.agentDB.Exec(t, `INSERT INTO persona_review_grant(tenant_id,grant_id,principal_id,permission,granted_at,expires_at) VALUES($1,'review-grant','reviewer','persona:review',now()-interval '1 hour',now()+interval '1 day') ON CONFLICT DO NOTHING`, w.tenantUUID)
	w.agentDB.Exec(t, `INSERT INTO persona_review_decision(tenant_id,review_id,persona_id,persona_version,profile_digest,author_id,reviewer_id,grant_id,permission,decision,review_digest,reviewed_at,expires_at) VALUES($1,$2,$3,$4,$5,'admin','reviewer','review-grant','persona:review','APPROVE',$6,now()-interval '1 hour',now()+interval '1 day')`, w.tenantUUID, reviewID, row.PersonaID, row.Version, row.ContentDigest, "sha256:"+strings.Repeat("c", 64))
	result, err := w.eval.RunPersonaEvaluation(ctx, w.actor, row.PersonaID)
	if err != nil || result.Status != "PASSED" {
		t.Fatalf("evaluation result=%+v err=%v", result, err)
	}
	evidence, err := w.personas.ResolvePublicationEvidence(ctx, row.PersonaID, row.Version)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.personas.Publish(ctx, agentpersonastore.LifecycleEvent{TenantID: w.tenant, EventID: "publish-" + reviewID, PersonaID: row.PersonaID, PersonaVersion: row.Version, From: agentpersonastore.StateInReview, To: agentpersonastore.StatePublished, Reason: "reviewed references passed evaluation", ActorID: w.actor.Subject, OccurredAt: w.now.Add(2 * time.Second)}, evidence); err != nil {
		t.Fatal(err)
	}
}

func agentdoc007SealedReferences(t *testing.T, row agentpersonastore.PersonaVersion) []agentdocref.Reference {
	t.Helper()
	var envelope agentDocumentProfileEnvelope
	if err := json.Unmarshal(row.Profile, &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.DocumentReferences
}

// TestTodo_AGENTDOC_007 walks the reference-documents handoff end to end on
// real stores: pin, review, sealed publication, pinned-only answers,
// revocation, removal and republication.
func TestTodo_AGENTDOC_007(t *testing.T) {
	ctx := context.Background()
	world := agentdoc007WorldSetup(t)
	documentID, readerGrant := world.seedPolicyDocument(t, "owner", "reader")
	resolver, err := NewAgentDocumentResolver(world.documents)
	if err != nil {
		t.Fatal(err)
	}

	pinned := []agentdocref.Reference{{DocumentID: documentID, VersionMode: agentdocref.ModePinned, PinnedVersion: 1, SectionAnchor: "leave", Label: "Leave policy"}}
	v1 := world.draftWithReferences(t, 1, pinned)
	world.reviewAndPublish(t, v1, "review-v1")

	sealed, err := world.personas.GetVersion(ctx, v1.PersonaID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if sealed.ContentDigest != v1.ContentDigest || len(agentdoc007SealedReferences(t, sealed)) != 1 || agentdoc007SealedReferences(t, sealed)[0].PinnedVersion != 1 {
		t.Fatalf("publication did not seal the references: digest=%s profile=%s", sealed.ContentDigest, sealed.Profile)
	}
	if state, err := world.personas.Lifecycle(ctx, v1.PersonaID, 1); err != nil || state != agentpersonastore.StatePublished {
		t.Fatalf("v1 state=%s err=%v", state, err)
	}

	resolved, omitted, err := resolver.Resolve(ctx, agentdocref.Invoker{TenantID: string(world.tenant), SubjectID: "reader"}, agentdoc007SealedReferences(t, sealed))
	if err != nil || len(omitted) != 0 || len(resolved) != 1 || resolved[0].Version != 1 || !strings.Contains(resolved[0].Content, "20 days") || strings.Contains(resolved[0].Content, "25 days") {
		t.Fatalf("pinned answer=%+v omitted=%+v err=%v", resolved, omitted, err)
	}

	if err := world.documents.RevokeGrant(ctx, string(world.tenant), readerGrant.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	revoked, hidden, err := resolver.Resolve(ctx, agentdocref.Invoker{TenantID: string(world.tenant), SubjectID: "reader"}, agentdoc007SealedReferences(t, sealed))
	if err != nil || len(revoked) != 0 || len(hidden) != 1 || hidden[0].Reason != agentdocref.NotFound {
		t.Fatalf("revoked reader got usable content: resolved=%+v omitted=%+v err=%v", revoked, hidden, err)
	}

	v2 := world.draftWithReferences(t, 2, nil)
	world.reviewAndPublish(t, v2, "review-v2")
	republished, err := world.personas.GetVersion(ctx, v2.PersonaID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(agentdoc007SealedReferences(t, republished)) != 0 {
		t.Fatalf("removal did not seal: profile=%s", republished.Profile)
	}
	after, _, err := resolver.Resolve(ctx, agentdocref.Invoker{TenantID: string(world.tenant), SubjectID: "owner"}, agentdoc007SealedReferences(t, republished))
	if err != nil || len(after) != 0 {
		t.Fatalf("removed document is still cited: %+v err=%v", after, err)
	}
	if state, err := world.personas.Lifecycle(ctx, v2.PersonaID, 2); err != nil || state != agentpersonastore.StatePublished {
		t.Fatalf("v2 state=%s err=%v", state, err)
	}
}

// TestTodo_AGENTDOC_007_Conformance pins the handoff state machine: the
// sealed reference sequence across versions, publication immutability, and
// the review evidence binding each sealed digest.
func TestTodo_AGENTDOC_007_Conformance(t *testing.T) {
	ctx := context.Background()
	world := agentdoc007WorldSetup(t)
	documentID, _ := world.seedPolicyDocument(t, "owner", "reader")

	pinned := []agentdocref.Reference{{DocumentID: documentID, VersionMode: agentdocref.ModePinned, PinnedVersion: 1, SectionAnchor: "leave", Label: "Leave policy"}}
	v1 := world.draftWithReferences(t, 1, pinned)
	world.reviewAndPublish(t, v1, "review-v1")
	v2 := world.draftWithReferences(t, 2, nil)
	world.reviewAndPublish(t, v2, "review-v2")

	sealedV1, err := world.personas.GetVersion(ctx, v1.PersonaID, 1)
	if err != nil {
		t.Fatal(err)
	}
	sealedV2, err := world.personas.GetVersion(ctx, v2.PersonaID, 2)
	if err != nil {
		t.Fatal(err)
	}
	refsV1, refsV2 := agentdoc007SealedReferences(t, sealedV1), agentdoc007SealedReferences(t, sealedV2)
	if len(refsV1) != 1 || refsV1[0].DocumentID != documentID || refsV1[0].VersionMode != agentdocref.ModePinned || refsV1[0].PinnedVersion != 1 || len(refsV2) != 0 {
		t.Fatalf("sealed reference sequence v1=%+v v2=%+v", refsV1, refsV2)
	}
	if sealedV1.ContentDigest == "" || sealedV1.ContentDigest == sealedV2.ContentDigest {
		t.Fatalf("sealed digests do not version the references: %s %s", sealedV1.ContentDigest, sealedV2.ContentDigest)
	}
	for version, digest := range map[int64]string{1: sealedV1.ContentDigest, 2: sealedV2.ContentDigest} {
		var bound string
		err := world.agentDB.SQL.QueryRow(`SELECT profile_digest FROM persona_review_decision WHERE tenant_id=$1 AND persona_id=$2 AND persona_version=$3`, world.tenantUUID, v1.PersonaID, version).Scan(&bound)
		if err != nil || bound != digest {
			t.Fatalf("v%d review binds %q, sealed %q (err=%v)", version, bound, digest, err)
		}
		var state string
		err = world.agentDB.SQL.QueryRow(`SELECT to_state FROM persona_lifecycle_events WHERE tenant_id=$1 AND persona_id=$2 AND persona_version=$3 ORDER BY event_sequence DESC LIMIT 1`, world.tenantUUID, v1.PersonaID, version).Scan(&state)
		if err != nil || state != string(agentpersonastore.StatePublished) {
			t.Fatalf("v%d terminal state=%q err=%v", version, state, err)
		}
	}
}

// TestTodo_AGENTDOC_007_Security proves the handoff fails closed: an
// unbound document version is omitted, a replacement reference set cannot
// publish on another version's review, and a reader revoked between review
// and answer gets nothing usable or distinguishable.
func TestTodo_AGENTDOC_007_Security(t *testing.T) {
	t.Run("withdrawn version is omitted not served", func(t *testing.T) {
		ctx := context.Background()
		world := agentdoc007WorldSetup(t)
		documentID, err := world.documents.CreateDocument(ctx, string(world.tenant), "owner", "PERSONAL")
		if err != nil {
			t.Fatal(err)
		}
		v1, err := world.documents.SubmitCandidate(ctx, string(world.tenant), documenthubstore.Version{DocumentID: documentID, CreatorID: "owner", Title: "Leave policy", Markdown: "# Leave\n\nVersion one.\n"}, "")
		if err != nil {
			t.Fatal(err)
		}
		deployDocumentVersion(t, world.documents, string(world.tenant), "owner", documentID, v1.ID, "")
		if _, err := world.documents.ShareDocument(ctx, string(world.tenant), documentID, "owner", documenthubstore.GrantInput{SubjectKind: "person", SubjectID: "reader", Action: documenthubstore.ActionRead, Effect: documenthubstore.EffectAllow}); err != nil {
			t.Fatal(err)
		}
		resolver, err := NewAgentDocumentResolver(world.documents)
		if err != nil {
			t.Fatal(err)
		}
		pinned := []agentdocref.Reference{{DocumentID: documentID, VersionMode: agentdocref.ModePinned, PinnedVersion: 1, SectionAnchor: "leave", Label: "Leave policy"}}
		if _, err := world.documents.Withdraw(ctx, string(world.tenant), documenthubstore.WithdrawInput{DocumentID: documentID, ScopeKind: "default", ActorID: "owner", ExpectedLive: v1.ID, Reason: "superseded"}); err != nil {
			t.Fatal(err)
		}
		resolved, omitted, err := resolver.Resolve(ctx, agentdocref.Invoker{TenantID: string(world.tenant), SubjectID: "reader"}, pinned)
		if err != nil || len(resolved) != 0 || len(omitted) != 1 || omitted[0].Reason != agentdocref.NotPublished || omitted[0].Label != "Leave policy" {
			t.Fatalf("withdrawn resolve=%+v omitted=%+v err=%v", resolved, omitted, err)
		}
	})

	t.Run("replacement references need their own review", func(t *testing.T) {
		ctx := context.Background()
		world := agentdoc007WorldSetup(t)
		documentID, _ := world.seedPolicyDocument(t, "owner", "reader")
		pinned := []agentdocref.Reference{{DocumentID: documentID, VersionMode: agentdocref.ModePinned, PinnedVersion: 1, SectionAnchor: "leave", Label: "Leave policy"}}
		v1 := world.draftWithReferences(t, 1, pinned)
		world.reviewAndPublish(t, v1, "review-v1")
		replacement := []agentdocref.Reference{{DocumentID: documentID, VersionMode: agentdocref.ModePinned, PinnedVersion: 2, Label: "Leave policy"}}
		v2 := world.draftWithReferences(t, 2, replacement)
		stale, err := world.personas.ResolvePublicationEvidence(ctx, v1.PersonaID, 1)
		if err != nil {
			t.Fatal(err)
		}
		err = world.personas.Publish(ctx, agentpersonastore.LifecycleEvent{TenantID: world.tenant, EventID: "publish-stale", PersonaID: v2.PersonaID, PersonaVersion: 2, From: agentpersonastore.StateInReview, To: agentpersonastore.StatePublished, Reason: "stale review", ActorID: world.actor.Subject, OccurredAt: world.now.Add(3 * time.Second)}, stale)
		if err == nil {
			t.Fatal("replacement references published on another version's review")
		}
		if state, _ := world.personas.Lifecycle(ctx, v2.PersonaID, 2); state == agentpersonastore.StatePublished {
			t.Fatal("stale publish changed the lifecycle")
		}
	})

	t.Run("reader revoked between review and answer gets nothing", func(t *testing.T) {
		ctx := context.Background()
		world := agentdoc007WorldSetup(t)
		documentID, readerGrant := world.seedPolicyDocument(t, "owner", "reader")
		resolver, err := NewAgentDocumentResolver(world.documents)
		if err != nil {
			t.Fatal(err)
		}
		pinned := []agentdocref.Reference{{DocumentID: documentID, VersionMode: agentdocref.ModePinned, PinnedVersion: 1, SectionAnchor: "leave", Label: "Leave policy"}}
		v1 := world.draftWithReferences(t, 1, pinned)
		world.reviewAndPublish(t, v1, "review-v1")
		if err := world.documents.RevokeGrant(ctx, string(world.tenant), readerGrant.ID, "owner"); err != nil {
			t.Fatal(err)
		}
		sealed, err := world.personas.GetVersion(ctx, v1.PersonaID, 1)
		if err != nil {
			t.Fatal(err)
		}
		resolved, omitted, err := resolver.Resolve(ctx, agentdocref.Invoker{TenantID: string(world.tenant), SubjectID: "reader"}, agentdoc007SealedReferences(t, sealed))
		if err != nil || len(resolved) != 0 || len(omitted) != 1 || omitted[0].Label != "" || omitted[0].Reason != agentdocref.NotFound {
			t.Fatalf("revoked reader distinguished or served: resolved=%+v omitted=%+v err=%v", resolved, omitted, err)
		}
	})
}

// TestTodo_AGENTDOC_007_Browser pins the citation identity browsers render
// for grounded answers: document, pinned version and section in the source
// id, with only digest-matching documents selectable as cited.
func TestTodo_AGENTDOC_007_Browser(t *testing.T) {
	documents := []agentdocref.ResolvedDocument{
		{Reference: agentdocref.Reference{DocumentID: "doc-leave", VersionMode: agentdocref.ModePinned, PinnedVersion: 1, SectionAnchor: "leave", Label: "Leave policy"}, Version: 1, Title: "Leave policy", Content: "# Leave\n\nVersion one.\n"},
		{Reference: agentdocref.Reference{DocumentID: "doc-travel", VersionMode: agentdocref.ModePinned, PinnedVersion: 7, Label: "Travel policy"}, Version: 7, Title: "Travel policy", Content: "# Travel\n\nCurrent.\n"},
	}
	digest := personaRunT0ToolOutputDigest([]byte(documents[0].Content))
	cited := personaCitedAgentDocuments(documents, []agentsecurity.Citation{
		{SourceID: "document:doc-leave/version:1", Location: "document:doc-leave/version:1/section:leave", Digest: digest},
		{SourceID: "document:doc-travel/version:7", Location: "document:doc-travel/version:7", Digest: "sha256:stale"},
	})
	if len(cited) != 1 || cited[0].Reference.DocumentID != "doc-leave" || cited[0].Version != 1 {
		t.Fatalf("cited documents=%+v", cited)
	}
	if uncited := personaCitedAgentDocuments(documents, nil); len(uncited) != 0 {
		t.Fatalf("uncited answer kept documents: %+v", uncited)
	}
}
