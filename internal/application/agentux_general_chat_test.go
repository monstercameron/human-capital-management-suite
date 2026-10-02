package application

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func agentUXGeneralChatMention(t *testing.T, ctx context.Context, chat chatcore.ConversationService, lazy *lazyPersonaReferenceSource, personas *agentpersonastore.Store, core dbport.Beginner, documents *documenthubstore.Store, conversation string, profile agentpersona.PersonaProfile, now time.Time) {
	t.Helper()
	var records []agentskills.SkillRecord
	for _, pin := range profile.SkillPins {
		records = append(records, agentskills.SkillRecord{Definition: agentskills.SkillDefinition{ID: pin.ID, Version: pin.Version}, Digest: pin.Digest, Status: agentskills.StatusActive})
	}
	// The installed Assistant also pins the workspace search skill.
	for _, pin := range AssistantWorkspaceStarter().SkillPins {
		if pin.ID == personaWorkspaceSearchSkillID {
			records = append(records, agentskills.SkillRecord{Definition: agentskills.SkillDefinition{ID: pin.ID, Version: pin.Version}, Digest: pin.Digest, Status: agentskills.StatusActive})
		}
	}
	directory := agentUXGeneralDirectory{profile: profile}
	audience := &DatabasePersonaAudienceSource{Chat: chat, Installations: personaInstallationStore{store: personas}, Directory: directory}
	available := &TenantAvailablePersonaReader{Backend: AgentPersonaStoreBackend{Store: personas}, Audience: &CurrentPersonaAudience{Source: audience}, Skills: agentUXAvailableSkills{records: records}}
	identities, err := newProductionPersonaChatIdentityDirectory(personas)
	if err != nil {
		t.Fatal(err)
	}
	lookup, err := newProductionPersonaReferenceLookup(identities, personas)
	if err != nil {
		t.Fatal(err)
	}
	references, err := newPersonaChatReferenceSource(available, identities, lookup, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	lazy.bind(references)
	principal := chatcore.Principal{TenantID: localAgentDemoTenant, SubjectID: localAgentDemoAdmin}
	list, err := references.ListPersonaReferenceCandidates(ctx, principal, localAgentDemoTenant, conversation, "assistant")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ref := range list {
		if ref.Reference.ID == localAgentDemoAssistantAgentID {
			found = true
		}
	}
	if !found {
		t.Fatalf("Assistant missing from Chat mentions: %+v", list)
	}
	scoped, _ := personas.Scoped(values.TenantId(localAgentDemoTenant))
	rows, err := scoped.ListActiveInstallations(ctx, conversation)
	if err != nil {
		t.Fatal(err)
	}
	installation := ""
	personaVersion := ""
	policyInstallation, policyVersion := "", ""
	for _, row := range rows {
		if row.PersonaID == localAgentDemoAssistantPersonaID {
			installation = row.InstallationID
			personaVersion = fmt.Sprint(row.PersonaVersion)
		}
		if row.PersonaID == localAgentDemoPersonaID {
			policyInstallation, policyVersion = row.InstallationID, fmt.Sprint(row.PersonaVersion)
		}
	}
	resolver, err := newPersonaChatReferenceResolver(lookup, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	worker := &agentUXGeneralHolidayWorker{chat: chat, documents: documents, principal: principal, identities: PersonaRunAgentPrincipalResolver{Bindings: AgentPersonaRunPrincipalBindingReader{Store: personas}, Principals: GovernancePersonaPrincipalAuthority{DB: core, TenantUUID: tenantKeyMapper[values.TenantId](pgstore.TenantID)}, Now: func() time.Time { return now }}}
	scopes := agentinvoke.SkillScopes{personaPolicyHelperSkillID: {"chat.current"}}
	invocation, err := newPersonaChatInvocation(personaChatInvocationConfig{Chat: chat, References: resolver, Authority: agentUXLiveAuthority{personaID: localAgentDemoAssistantPersonaID, version: personaVersion, installationID: installation, skills: scopes}, Grants: agentUXLiveGrant{}, Runs: worker, T0Skills: agentUXLiveT0Policy{personaID: localAgentDemoAssistantPersonaID}, Repository: agentinvoke.NewMemoryRepository()})
	if err != nil {
		t.Fatal(err)
	}
	post, err := invocation.SendPost(ctx, chatcore.SendPostRequest{Principal: principal, TenantID: localAgentDemoTenant, ConversationID: conversation, Body: "@assistant tell employees which holidays are coming up, using the 2026 holiday guide", IdempotencyKey: "general-holiday-mention", References: []chatcore.Reference{{Kind: chatcore.AgentMention, TenantID: localAgentDemoTenant, ID: localAgentDemoAssistantAgentID, Display: "Assistant"}}})
	if err != nil {
		t.Fatal(err)
	}
	posts, err := chat.ListPosts(ctx, chatcore.ListPostsRequest{Principal: principal, TenantID: localAgentDemoTenant, ConversationID: conversation, Page: chatcore.Page{PageSize: 20}})
	if err != nil || worker.calls != 1 || len(posts.Posts) != 2 || posts.Posts[1].ParentID != post.ID || !strings.Contains(posts.Posts[1].Body, "Thanksgiving Day | Nov 26 | Thursday") || !strings.Contains(posts.Posts[1].Body, "[2026 holiday guide](document:") {
		t.Fatalf("document-grounded Assistant mention=%+v calls=%d err=%v", posts.Posts, worker.calls, err)
	}
	policyInvocation, err := newPersonaChatInvocation(personaChatInvocationConfig{Chat: chat, References: resolver, Authority: agentUXLiveAuthority{personaID: localAgentDemoPersonaID, version: policyVersion, installationID: policyInstallation, skills: scopes}, Grants: agentUXLiveGrant{}, Runs: worker, T0Skills: agentUXLiveT0Policy{personaID: localAgentDemoPersonaID}, Repository: agentinvoke.NewMemoryRepository()})
	if err != nil {
		t.Fatal(err)
	}
	policyPost, err := policyInvocation.SendPost(ctx, chatcore.SendPostRequest{Principal: principal, TenantID: localAgentDemoTenant, ConversationID: conversation, Body: "@policy-helper How much PTO can I carry over?", IdempotencyKey: "general-policy-mention", References: []chatcore.Reference{{Kind: chatcore.AgentMention, TenantID: localAgentDemoTenant, ID: localAgentDemoAgentID, Display: "Policy Helper"}}})
	if err != nil {
		t.Fatal(err)
	}
	posts, err = chat.ListPosts(ctx, chatcore.ListPostsRequest{Principal: principal, TenantID: localAgentDemoTenant, ConversationID: conversation, Page: chatcore.Page{PageSize: 20}})
	if err != nil || worker.calls != 2 || len(posts.Posts) != 4 || posts.Posts[3].ParentID != policyPost.ID || !strings.Contains(posts.Posts[3].Body, "40 hours") || !strings.Contains(posts.Posts[3].Body, "[Paid time off policy](document:") {
		t.Fatalf("Policy Helper after upgrade=%+v calls=%d err=%v", posts.Posts, worker.calls, err)
	}
}

type agentUXGeneralDirectory struct{ profile agentpersona.PersonaProfile }

func (d agentUXGeneralDirectory) ResolvePersonaAudienceMember(_ context.Context, tenant, subject string) (PersonaAudienceMember, error) {
	if tenant != localAgentDemoTenant || subject != localAgentDemoAdmin && subject != "ir-008-curtis-bell" {
		return PersonaAudienceMember{}, ErrPersonaAudienceDirectoryFactsMissing
	}
	return PersonaAudienceMember{SubjectID: subject, Roles: d.profile.Audience.Roles, Populations: d.profile.Audience.Populations, OrganizationScope: d.profile.Audience.OrganizationScopes[0]}, nil
}

// This deterministic worker exercises Chat, exact-version identity resolution,
// and official document reads without invoking a paid provider.
type agentUXGeneralHolidayWorker struct {
	chat       chatcore.ConversationService
	documents  *documenthubstore.Store
	principal  chatcore.Principal
	identities PersonaRunAgentPrincipalResolver
	calls      int
}

func (w *agentUXGeneralHolidayWorker) Start(ctx context.Context, request agentinvoke.RunRequest) error {
	version, err := strconv.ParseInt(request.PersonaVersion, 10, 64)
	if err != nil || (request.PersonaID != localAgentDemoAssistantPersonaID && request.PersonaID != localAgentDemoPersonaID) || request.Mode != agentinvoke.OnBehalfOf {
		return errors.New("unexpected Assistant run")
	}
	if _, err := w.identities.Resolve(ctx, values.TenantId(request.TenantID), request.PersonaID, version); err != nil {
		return err
	}
	if request.PersonaID == localAgentDemoPersonaID {
		return w.replyFromPolicy(ctx, request)
	}
	hits, err := w.documents.SearchOfficialPlacementLexical(ctx, request.TenantID, request.ConversationID, "Thanksgiving", "person", request.InvokerID)
	if err != nil || len(hits) != 1 || hits[0].Title != localAgentDemoHolidayTitle {
		return errors.New("official holiday guide unavailable")
	}
	guide, err := w.documents.ReadVersion(ctx, request.TenantID, hits[0].DocumentID, hits[0].VersionID, "person", request.InvokerID)
	if err != nil {
		return err
	}
	if !strings.Contains(guide.Markdown, "| Thanksgiving Day | Nov 26 | Thursday |") {
		return errors.New("holiday guide does not support the answer")
	}
	body := fmt.Sprintf("Thanksgiving Day | Nov 26 | Thursday\nDay after Thanksgiving | Nov 27 | Friday\nChristmas Day | Dec 25 | Friday\nSource: [2026 holiday guide](document:%s)", hits[0].DocumentID)
	_, err = w.chat.SendPost(ctx, chatcore.SendPostRequest{Principal: w.principal, TenantID: request.TenantID, ConversationID: request.ConversationID, ParentID: request.ThreadID, Body: body, IdempotencyKey: "general-reply:" + request.InvocationID})
	if err == nil {
		w.calls++
	}
	return err
}

func (w *agentUXGeneralHolidayWorker) replyFromPolicy(ctx context.Context, request agentinvoke.RunRequest) error {
	hits, err := w.documents.SearchOfficialPlacementLexical(ctx, request.TenantID, request.ConversationID, "carryover", "person", request.InvokerID)
	if err != nil || len(hits) != 1 || hits[0].Title != localAgentDemoPolicyTitle {
		return errors.New("official PTO policy unavailable")
	}
	policy, err := w.documents.ReadVersion(ctx, request.TenantID, hits[0].DocumentID, hits[0].VersionID, "person", request.InvokerID)
	if err != nil {
		return err
	}
	if !strings.Contains(policy.Markdown, "up to 40 hours") || !strings.Contains(policy.Markdown, "March 31") {
		return errors.New("PTO policy does not support the answer")
	}
	body := fmt.Sprintf("You can carry over up to 40 hours of PTO; use it by March 31. Source: [Paid time off policy](document:%s)", hits[0].DocumentID)
	_, err = w.chat.SendPost(ctx, chatcore.SendPostRequest{Principal: w.principal, TenantID: request.TenantID, ConversationID: request.ConversationID, ParentID: request.ThreadID, Body: body, IdempotencyKey: "general-policy-reply:" + request.InvocationID})
	if err == nil {
		w.calls++
	}
	return err
}
