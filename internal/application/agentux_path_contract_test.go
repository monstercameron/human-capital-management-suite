package application

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

type agentUXPathLogHandler struct{ lines []string }

func (*agentUXPathLogHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *agentUXPathLogHandler) Handle(_ context.Context, record slog.Record) error {
	line := record.Message
	record.Attrs(func(attr slog.Attr) bool {
		line += " " + attr.Key + "=" + attr.Value.String()
		return true
	})
	h.lines = append(h.lines, line)
	return nil
}
func (h *agentUXPathLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *agentUXPathLogHandler) WithGroup(string) slog.Handler      { return h }
func (h *agentUXPathLogHandler) String() string                     { return strings.Join(h.lines, "\n") }

type agentUXPathEphemeralRecorder struct {
	chatcore.ConversationService
	requests []chatcore.SendEphemeralPostRequest
}

func (r *agentUXPathEphemeralRecorder) SendEphemeralPost(_ context.Context, request chatcore.SendEphemeralPostRequest) (chatcore.EphemeralPost, error) {
	r.requests = append(r.requests, request)
	return chatcore.EphemeralPost{ID: "ephemeral", DurableCopyConversationID: request.DurableCopyConversationID}, nil
}

type agentUXPathDMResolver string

func (r agentUXPathDMResolver) ResolvePersonaDM(context.Context, chatcore.Principal, string) (string, error) {
	return string(r), nil
}

func TestTodo_AGENTUX_PATH_T1_ModelInstructionMatchesReplyValidator(t *testing.T) {
	if err := validatePersonaChatReply("The Paid time off policy says carryover is limited to 40 hours."); err != nil {
		t.Fatalf("instruction-compliant reply refused: %v", err)
	}
	for _, reply := range []string{"See [Paid time off policy].", "Source: Paid time off policy"} {
		if err := validatePersonaChatReply(reply); !errors.Is(err, errPersonaChatReplyUnsafe) {
			t.Fatalf("unsafe instructed-against reply %q error=%v", reply, err)
		}
	}
}

func TestTodo_AGENTUX_PATH_T3_PrivateReplyGrantShapes(t *testing.T) {
	record, run, now := privateChatGatewayIdentityFixture(t)
	legacy := privateChatGatewayGrant(record, run, now)
	if !privatePersonaReplyGrantMatches(legacy, record, now) {
		t.Fatal("legacy persona.reply/chat.current grant refused")
	}
	bound := personaChatAuthorityResource(record.Request.Source.TenantID, record.Request.Audience.ID, record.Request.Context.ID, record.Request.Source.Ref)
	threadBound := privateChatGatewayGrant(record, run, now)
	threadBound.SkillScopes["persona.chat_reply"] = []string{"chat.current"}
	authority := trust.SkillAuthority{Capabilities: []string{"chat.current"}, Resources: []string{bound}, Purposes: []string{record.Request.Purpose}}
	threadBound.SkillAuthorities["persona.chat_reply"] = authority
	threadBound.Authority.SkillAuthorities["persona.chat_reply"] = authority
	if !privatePersonaReplyGrantMatches(threadBound, record, now) {
		t.Fatal("exact thread-bound grant refused")
	}

	for _, resource := range []string{
		personaChatAuthorityResource("other-tenant", record.Request.Audience.ID, record.Request.Context.ID, record.Request.Source.Ref),
		personaChatAuthorityResource(record.Request.Source.TenantID, "other-conversation", record.Request.Context.ID, record.Request.Source.Ref),
		personaChatAuthorityResource(record.Request.Source.TenantID, record.Request.Audience.ID, "other-thread", record.Request.Source.Ref),
		personaChatAuthorityResource(record.Request.Source.TenantID, record.Request.Audience.ID, record.Request.Context.ID, "other-post"),
	} {
		changed := threadBound
		changed.SkillAuthorities = trust.CloneSkillAuthorities(threadBound.SkillAuthorities)
		changed.Authority.SkillAuthorities = trust.CloneSkillAuthorities(threadBound.Authority.SkillAuthorities)
		a := changed.SkillAuthorities["persona.chat_reply"]
		a.Resources = []string{resource}
		changed.SkillAuthorities["persona.chat_reply"] = a
		changed.Authority.SkillAuthorities["persona.chat_reply"] = a
		if privatePersonaReplyGrantMatches(changed, record, now) {
			t.Fatalf("foreign thread-bound resource accepted: %q", resource)
		}
	}
	mixed := threadBound
	mixed.Authority.SkillAuthorities = trust.CloneSkillAuthorities(threadBound.Authority.SkillAuthorities)
	mixed.Authority.SkillAuthorities["persona.chat_reply"] = legacy.SkillAuthorities["persona.chat_reply"]
	if privatePersonaReplyGrantMatches(mixed, record, now) {
		t.Fatal("mixed legacy and thread-bound authorities accepted")
	}
	legacy.Revoked = true
	if privatePersonaReplyGrantMatches(legacy, record, now) {
		t.Fatal("previously refused revoked grant accepted")
	}
}

func TestTodo_AGENTUX_PATH_T4_CompositionSelectsStreamingReplyService(t *testing.T) {
	inner := &agentUXPathEphemeralRecorder{}
	streaming := &streamingChatService{ConversationService: inner, personaDM: agentUXPathDMResolver("canonical-agent-dm")}
	service, ok := personaRuntimeReplyService(streaming).(chatcore.EphemeralService)
	if !ok {
		t.Fatal("composed reply service lost ephemeral delivery")
	}
	request := chatcore.SendEphemeralPostRequest{Principal: chatcore.Principal{TenantID: "tenant-a", SubjectID: "person-a"}, TenantID: "tenant-a", ConversationID: "audience-room", DurableCopyConversationID: "forged-dm"}
	if _, err := service.SendEphemeralPost(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if len(inner.requests) != 1 || inner.requests[0].DurableCopyConversationID != "canonical-agent-dm" {
		t.Fatalf("inner routed service requests=%+v", inner.requests)
	}
}

func TestTodo_AGENTUX_PATH_T6_LocalDemoSummaryAndOwnerDSN(t *testing.T) {
	config := LocalAgentDemoConfig{AgentDatabaseURL: "serving", AgentOwnerDatabaseURL: "owner"}
	if got := localAgentDemoRunPolicyDSN(config); got != "owner" {
		t.Fatalf("run policy DSN=%q, want owner", got)
	}
	config.AgentOwnerDatabaseURL = ""
	if got := localAgentDemoRunPolicyDSN(config); got != "serving" {
		t.Fatalf("fallback run policy DSN=%q", got)
	}
	if !(LocalAgentDemoSummary{PolicyDocumentPlacements: 1}).Changed() || (LocalAgentDemoSummary{}).Changed() {
		t.Fatal("policy placement count is not part of Changed")
	}
}

func TestTodo_AGENTUX_PATH_T7_LocalPersonaPurposes(t *testing.T) {
	got := localPersonaPurposes("compensation_review")
	if len(got) != 2 || got[0] != "compensation_review" || got[1] != personaChatReplyPurpose {
		t.Fatalf("local purposes=%q", got)
	}
	got = localPersonaPurposes(personaChatReplyPurpose)
	if len(got) != 1 || got[0] != personaChatReplyPurpose {
		t.Fatalf("duplicate mention purpose=%q", got)
	}
}

type agentUXPathDiscoveryContext struct {
	user     agentgate.UserContext
	subjects []agentgate.Subject
}

func (c agentUXPathDiscoveryContext) Resolve(context.Context, *trust.Principal, string) (agentgate.UserContext, []agentgate.Subject, []authz.FieldID, error) {
	return c.user, c.subjects, nil, nil
}

func TestTodo_AGENTUX_PATH_T8_ChatReplyUsesGrantedDiscovery(t *testing.T) {
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "user-a", SubjectKind: trust.SubjectKindHuman, Purposes: []string{"purpose.read", personaChatReplyPurpose}, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "cred"})
	if err != nil {
		t.Fatal(err)
	}
	record := agentskills.SkillRecord{Definition: agentskills.SkillDefinition{ID: "skill.reply", Version: 1, Description: "Reply", RequiredPurposes: []string{personaChatReplyPurpose}}, Digest: "digest", Status: agentskills.StatusActive}
	user := agentgate.UserContext{Principal: principal, Population: "employees", Roles: []string{"employee"}, OrganizationScopes: []string{"org-a"}}
	current := agentUXPathDiscoveryContext{user: user, subjects: []agentgate.Subject{{Ref: values.EntityRef{Tenant: "foreign", Kind: "person", Id: "other"}}}}
	source, err := NewAgentSkillSource(sourceSkillCatalog{records: []agentskills.SkillRecord{record}}, agentgate.StaticGrants{{ID: "grant", Tenant: principal.Tenant(), Skill: record.Definition.Key(), Roles: []string{"employee"}, Population: "employees", OrganizationScopes: []string{"org-a"}, Purposes: []string{personaChatReplyPurpose, "purpose.read"}}}, current)
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), principal)
	got, err := source.Discover(ctx, principal, personaChatReplyPurpose)
	if err != nil || len(got) != 1 {
		t.Fatalf("chat granted discovery=%#v err=%v", got, err)
	}
	record.Definition.RequiredPurposes = []string{"purpose.read"}
	source, err = NewAgentSkillSource(sourceSkillCatalog{records: []agentskills.SkillRecord{record}}, agentgate.StaticGrants{{ID: "grant", Tenant: principal.Tenant(), Skill: record.Definition.Key(), Roles: []string{"employee"}, Population: "employees", OrganizationScopes: []string{"org-a"}, Purposes: []string{"purpose.read"}}}, current)
	if err != nil {
		t.Fatal(err)
	}
	got, err = source.Discover(ctx, principal, "purpose.read")
	if err != nil || len(got) != 0 {
		t.Fatalf("non-chat discovery bypassed exact subjects: %#v err=%v", got, err)
	}
}

func TestTodo_AGENTUX_PATH_T8_ProjectionOmissionsLogAndContinue(t *testing.T) {
	handler := &agentUXPathLogHandler{}
	previous := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(previous) })

	pin := agentskills.SkillPin{ID: "skill.people.read", Version: 2, Digest: "digest-v2"}
	persona := agentUserCatalogPersona(t, "people-coach", "People Coach", "agent.self_service", pin)
	principal := availablePersonaPrincipal(t)
	reader := &TenantAvailablePersonaReader{
		Backend:  &availableBackendFake{items: []agentpersonastore.PersonaVersion{{PersonaID: "broken", Version: 1, Profile: []byte("{")}, availablePersonaCandidate(t, persona)}},
		Audience: &availableAudienceFake{},
		Skills:   &agentUserCatalogDiscovery{byPurpose: map[string][]agentskills.SkillRecord{personaChatReplyPurpose: {{Definition: agentskills.SkillDefinition{ID: pin.ID, Version: pin.Version}, Digest: pin.Digest, Status: agentskills.StatusActive}}}},
	}
	got, err := reader.ListAvailable(trust.WithPrincipal(context.Background(), principal), principal)
	if err != nil || len(got) != 1 || got[0].Profile.PersonaID != persona.Profile.PersonaID {
		t.Fatalf("available projection=%#v err=%v", got, err)
	}
	if !strings.Contains(handler.String(), "reason=invalid_profile") {
		t.Fatalf("available omission log=%q", handler.String())
	}

	surface, ctx, _, _, _ := personaSurfaceFixture(t)
	references := surface.References.(*personaSurfaceReferencesFixture)
	references.candidates = append([]chatcore.ReferenceCandidate{{Reference: chatcore.Reference{Kind: chatcore.AgentMention, TenantID: "tenant-a", ID: "agent:stale", Display: "Stale", ConversationID: "channel-a"}, Eligible: true}}, references.candidates...)
	directory, err := surface.Directory(ctx, "channel-a")
	if err != nil || len(directory.Personas) != 1 || directory.Personas[0].Reference.ID != "agent:coach" {
		t.Fatalf("chat projection=%#v err=%v", directory, err)
	}
	if !strings.Contains(handler.String(), "projection=chat_directory") || !strings.Contains(handler.String(), "reason=reference_not_current") {
		t.Fatalf("chat omission log=%q", handler.String())
	}
}

func TestTodo_AGENTUX_PATH_T9_PolicySearcherRefusalsAreLocatedAndSanitized(t *testing.T) {
	secret := "request-content-must-not-escape"
	var searcher *PersonaPolicyDocumentSearcher
	_, err := searcher.SearchPersonaPolicyDocuments(context.Background(), personaDocumentSearchCall{ConversationID: secret, Query: secret})
	if !errors.Is(err, errPersonaRuntimeTools) || !strings.Contains(err.Error(), "persona_policy_document_searcher.go:") || strings.Contains(err.Error(), secret) {
		t.Fatalf("nil search refusal=%v", err)
	}
	_, err = NewPersonaPolicyDocumentSearcher(nil, nil)
	if !errors.Is(err, errPersonaRuntimeTools) || !strings.Contains(err.Error(), "persona_policy_document_searcher.go:") || strings.Contains(err.Error(), secret) {
		t.Fatalf("constructor refusal=%v", err)
	}
}
