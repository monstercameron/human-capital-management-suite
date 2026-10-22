package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/semver"
)

// TestTodo_AGENTUX_065 is the primary contract: documents and agents carry
// sealed semantic labels chosen from three computed choices, references
// pin exactly or follow compatible lines, and every surface names labels.
func TestTodo_AGENTUX_065(t *testing.T) {
	ctx := context.Background()
	svc := documentServiceFixture(t)
	const tenant, owner = "tenant-065", "owner"
	doc, err := svc.store.CreateDocument(ctx, tenant, owner, "COMPANY")
	if err != nil {
		t.Fatal(err)
	}
	submit := func(markdown, base string) documenthubstore.Version {
		t.Helper()
		version, err := svc.store.SubmitCandidate(ctx, tenant, documenthubstore.Version{DocumentID: doc, CreatorID: owner, Title: "Paid time off policy", Markdown: markdown, Classification: "INTERNAL"}, base)
		if err != nil {
			t.Fatal(err)
		}
		return version
	}
	deploy := func(version documenthubstore.Version, expected string, bump semver.Bump) {
		t.Helper()
		if _, err := svc.store.RecordReview(ctx, tenant, documenthubstore.ReviewInput{DocumentID: doc, VersionID: version.ID, ScopeKind: "placement", ScopeID: "general", ReviewerID: "reviewer", Authority: "policy-owner", Decision: "approved"}); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.store.Deploy(ctx, tenant, documenthubstore.DeployInput{DocumentID: doc, VersionID: version.ID, ScopeKind: "placement", ScopeID: "general", DeployerID: owner, ExpectedLive: expected, Bump: bump}); err != nil {
			t.Fatal(err)
		}
	}
	live := func() string {
		t.Helper()
		deployment, err := svc.store.ResolveDeployment(ctx, tenant, doc, "placement", "general")
		if err != nil {
			t.Fatal(err)
		}
		return deployment.VersionID
	}
	v1 := submit("40 hours carry over.", "")
	deploy(v1, "", semver.BumpPatch)
	v2 := submit("40 hours carry over, pro-rated.", v1.ID)
	deploy(v2, live(), semver.BumpMinor)
	v3 := submit("Unlimited time off.", v2.ID)
	deploy(v3, live(), semver.BumpMajor)

	history, err := svc.store.ReadHistory(ctx, tenant, doc, "person", owner)
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]string{}
	for _, entry := range history {
		labels[entry.VersionID] = entry.SemanticVersion
	}
	if labels[v1.ID] != "1.0.0" || labels[v2.ID] != "1.1.0" || labels[v3.ID] != "2.0.0" {
		t.Fatalf("deployed labels = %v", labels)
	}

	resolver := newAgentDocumentResolver(documentHubAgentDocumentReader{store: svc.store})
	invoker := agentdocref.Invoker{TenantID: tenant, SubjectID: owner}
	resolveOne := func(ref agentdocref.Reference) agentdocref.ResolvedDocument {
		t.Helper()
		resolved, omitted, err := resolver.Resolve(ctx, invoker, []agentdocref.Reference{ref})
		if err != nil || len(omitted) != 0 || len(resolved) != 1 {
			t.Fatalf("label resolution: %+v %+v %v", resolved, omitted, err)
		}
		return resolved[0]
	}
	exact := resolveOne(agentdocref.Reference{DocumentID: doc, VersionMode: agentdocref.ModePinnedLabel, PinnedLabel: "1.1.0", Label: "Exact"})
	line := resolveOne(agentdocref.Reference{DocumentID: doc, VersionMode: agentdocref.ModeCompatible, CompatibleBase: "1.0.0", Label: "Line"})
	if exact.VersionLabel != "1.1.0" || line.VersionLabel != "1.1.0" {
		t.Fatalf("resolved labels = %q, %q", exact.VersionLabel, line.VersionLabel)
	}
	resolved := []agentdocref.ResolvedDocument{exact, line}
	body := renderPersonaReplyWithAgentDocuments("40 hours carry over.", tenant, "room", PersonaReplyOutputPolicy{}, resolved, nil)
	if !strings.Contains(body, "v1.1.0") || strings.Contains(body, "version 2") {
		t.Fatalf("answer sources = %q", body)
	}

	adminCtx, principal := personaAdminCommandContext(t)
	sealed, err := agentpersona.Seal(validPersonaProfileForLifecycleTest(t, principal.Subject()).Profile)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(sealed.Profile)
	tenantStore := &personaAdminLifecycleTenantFake{
		versions: []agentpersonastore.PersonaVersion{{TenantID: principal.Tenant(), PersonaID: sealed.Profile.PersonaID, Version: 1, Profile: raw, ContentDigest: sealed.Digest}},
		states:   map[int64]agentpersonastore.LifecycleState{1: agentpersonastore.StateInReview},
		labels:   map[int64]string{},
	}
	executor := NewPersonaAdminLifecycleExecutor(personaAdminLifecycleStoreFake{tenant: tenantStore}, personaAdminLifecycleAuthorizerFake{}, &PersonaAdminDraftService{Profiles: &personaStarterProfileBuilderSpy{}}, nil, nil, personaAdminPublicationEvidenceFake{evidence: agentpersonastore.PublicationEvidence{ReviewID: "review", EvaluationRunID: "evaluation"}}, nil, nil, time.Now, func() string { return "publish" }, &personaAdminRuntimeProvisionerFake{})
	actor := PersonaAdminCommandActor{Principal: principal, Tenant: principal.Tenant(), Subject: principal.Subject()}
	if err := executor.ExecutePersonaAdminCommand(adminCtx, actor, PersonaAdminCommand{Action: PersonaAdminPublish, PersonaID: sealed.Profile.PersonaID}); err != nil {
		t.Fatal(err)
	}
	if tenantStore.published.SemanticVersion != "1.0.0" {
		t.Fatalf("first publication sealed %q", tenantStore.published.SemanticVersion)
	}
}

// TestTodo_AGENTUX_065_Integration ties resolution to delivery: announcements
// resolve labels, conversions keep them, and the posted body cites them.
func TestTodo_AGENTUX_065_Integration(t *testing.T) {
	ctx := context.Background()
	svc := documentServiceFixture(t)
	const tenant, owner = "tenant-065-int", "owner"
	doc, err := svc.store.CreateDocument(ctx, tenant, owner, "COMPANY")
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := svc.store.SubmitCandidate(ctx, tenant, documenthubstore.Version{DocumentID: doc, CreatorID: owner, Title: "Guide", Markdown: "Corrected text.", Classification: "INTERNAL"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.RecordReview(ctx, tenant, documenthubstore.ReviewInput{DocumentID: doc, VersionID: candidate.ID, ScopeKind: "placement", ScopeID: "general", ReviewerID: "reviewer", Authority: "policy-owner", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.Deploy(ctx, tenant, documenthubstore.DeployInput{DocumentID: doc, VersionID: candidate.ID, ScopeKind: "placement", ScopeID: "general", DeployerID: owner, Bump: semver.BumpPatch}); err != nil {
		t.Fatal(err)
	}
	second, err := svc.store.SubmitCandidate(ctx, tenant, documenthubstore.Version{DocumentID: doc, CreatorID: owner, Title: "Guide", Markdown: "Corrected text, clarified.", Classification: "INTERNAL"}, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := svc.store.ResolveDeployment(ctx, tenant, doc, "placement", "general")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.RecordReview(ctx, tenant, documenthubstore.ReviewInput{DocumentID: doc, VersionID: second.ID, ScopeKind: "placement", ScopeID: "general", ReviewerID: "reviewer", Authority: "policy-owner", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.Deploy(ctx, tenant, documenthubstore.DeployInput{DocumentID: doc, VersionID: second.ID, ScopeKind: "placement", ScopeID: "general", DeployerID: owner, ExpectedLive: deployment.VersionID, Bump: semver.BumpMinor}); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetAnnouncementDocuments(ctx, tenant, owner, "workload:policy", "ann", []string{doc}, "general"); err != nil {
		t.Fatal(err)
	}
	resolver := AgentAnnouncementReferenceResolver{Hub: svc.store, Principals: &proactiveServicePrincipal{}}
	announcementCtx := context.WithValue(ctx, announcementDraftKey{}, agentstore.Announcement{ID: "ann", TenantKey: tenant, InstallationID: "install", Documents: []agentdocref.Reference{{DocumentID: doc, VersionMode: agentdocref.ModeCompatible, CompatibleBase: "1.0.0", Label: "Guide"}}})
	resolved, err := resolver.ResolveAnnouncementDocuments(announcementCtx, tenant, "install", []agentdocref.Reference{{DocumentID: doc, VersionMode: agentdocref.ModeCompatible, CompatibleBase: "1.0.0", Label: "Guide"}})
	if err != nil || len(resolved) != 1 || resolved[0].VersionLabel != "1.1.0" {
		t.Fatalf("announcement resolution = %+v %v", resolved, err)
	}
	converted := announcementResolvedReferences(resolved)
	if len(converted) != 1 || converted[0].VersionLabel != "1.1.0" {
		t.Fatalf("announcement conversion = %+v", converted)
	}
	body := renderPersonaReplyWithAgentDocuments("Corrected text, clarified.", tenant, "room", PersonaReplyOutputPolicy{}, converted, nil)
	if !strings.Contains(body, "v1.1.0") {
		t.Fatalf("announcement body = %q", body)
	}
}

// TestTodo_AGENTUX_065_Security: a compatible line or exact pin never
// resolves a version its reader may not open, and the served boundary
// refuses a publish choice smaller than the change requires.
func TestTodo_AGENTUX_065_Security(t *testing.T) {
	ctx := context.Background()
	svc := documentServiceFixture(t)
	const tenant, owner, stranger = "tenant-065-sec", "owner", "stranger"
	doc, err := svc.store.CreateDocument(ctx, tenant, owner, "COMPANY")
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := svc.store.SubmitCandidate(ctx, tenant, documenthubstore.Version{DocumentID: doc, CreatorID: owner, Title: "Secret", Markdown: "Secret text.", Classification: "INTERNAL"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.RecordReview(ctx, tenant, documenthubstore.ReviewInput{DocumentID: doc, VersionID: candidate.ID, ScopeKind: "placement", ScopeID: "general", ReviewerID: "reviewer", Authority: "policy-owner", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.Deploy(ctx, tenant, documenthubstore.DeployInput{DocumentID: doc, VersionID: candidate.ID, ScopeKind: "placement", ScopeID: "general", DeployerID: owner, Bump: semver.BumpPatch}); err != nil {
		t.Fatal(err)
	}
	resolver := newAgentDocumentResolver(documentHubAgentDocumentReader{store: svc.store})
	refs := []agentdocref.Reference{
		{DocumentID: doc, VersionMode: agentdocref.ModeCompatible, CompatibleBase: "1.0.0", Label: "Line"},
		{DocumentID: doc, VersionMode: agentdocref.ModePinnedLabel, PinnedLabel: "1.0.0", Label: "Exact"},
	}
	for _, ref := range refs {
		got, omitted, err := resolver.Resolve(ctx, agentdocref.Invoker{TenantID: tenant, SubjectID: stranger}, []agentdocref.Reference{ref})
		if err != nil || len(got) != 0 || len(omitted) != 1 {
			t.Fatalf("stranger resolution: %+v %+v %v", got, omitted, err)
		}
		if omitted[0].Reason != agentdocref.NotFound {
			t.Fatalf("stranger omission reason = %q", omitted[0].Reason)
		}
		got, omitted, err = resolver.Resolve(ctx, agentdocref.Invoker{TenantID: tenant, SubjectID: owner}, []agentdocref.Reference{ref})
		if err != nil || len(got) != 1 || len(omitted) != 0 {
			t.Fatalf("owner resolution: %+v %+v %v", got, omitted, err)
		}
	}

	adminCtx, principal := personaAdminCommandContext(t)
	v1 := validPersonaProfileForLifecycleTest(t, principal.Subject())
	v2profile := v1.Profile
	v2profile.Version = 2
	v2profile.SkillPins = append(append([]agentskills.SkillPin(nil), v1.Profile.SkillPins...), agentskills.SkillPin{ID: "skill-two", Version: 1, Digest: "digest-two"})
	sealed, err := agentpersona.Seal(v2profile)
	if err != nil {
		t.Fatal(err)
	}
	raw1, _ := json.Marshal(v1.Profile)
	raw2, _ := json.Marshal(sealed.Profile)
	tenantStore := &personaAdminLifecycleTenantFake{
		versions: []agentpersonastore.PersonaVersion{
			{TenantID: principal.Tenant(), PersonaID: v1.Profile.PersonaID, Version: 1, Profile: raw1, ContentDigest: v1.Digest},
			{TenantID: principal.Tenant(), PersonaID: v1.Profile.PersonaID, Version: 2, Profile: raw2, ContentDigest: sealed.Digest},
		},
		states: map[int64]agentpersonastore.LifecycleState{1: agentpersonastore.StatePublished, 2: agentpersonastore.StateInReview},
		labels: map[int64]string{1: "1.0.0"},
	}
	executor := NewPersonaAdminLifecycleExecutor(personaAdminLifecycleStoreFake{tenant: tenantStore}, personaAdminLifecycleAuthorizerFake{}, &PersonaAdminDraftService{Profiles: &personaStarterProfileBuilderSpy{}}, nil, nil, personaAdminPublicationEvidenceFake{evidence: agentpersonastore.PublicationEvidence{ReviewID: "review", EvaluationRunID: "evaluation"}}, nil, nil, time.Now, func() string { return "publish" }, &personaAdminRuntimeProvisionerFake{})
	patch := semver.BumpPatch
	actor := PersonaAdminCommandActor{Principal: principal, Tenant: principal.Tenant(), Subject: principal.Subject()}
	if err := executor.ExecutePersonaAdminCommand(adminCtx, actor, PersonaAdminCommand{Action: PersonaAdminPublish, PersonaID: v1.Profile.PersonaID, VersionBump: &patch}); !errors.Is(err, ErrPersonaAdminVersionBumpTooSmall) {
		t.Fatalf("too-small served choice err = %v", err)
	}
	if tenantStore.published.EventID != "" {
		t.Fatalf("refused publish persisted: %+v", tenantStore.published)
	}
}
