package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaChatWriterFake struct {
	post        chatcore.Post
	err         error
	calls       int
	room        chatcore.Conversation
	members     []chatcore.Membership
	nextMembers string
}

func (f *personaChatWriterFake) SendPost(_ context.Context, request chatcore.SendPostRequest) (chatcore.Post, error) {
	f.calls++
	if f.err != nil {
		return chatcore.Post{}, f.err
	}
	if f.post.ID == "" {
		f.post = chatcore.Post{
			ID: "post-1", TenantID: request.TenantID, ConversationID: request.ConversationID,
			AuthorID: request.Principal.SubjectID, Body: request.Body, Revision: 1,
			ParentID: request.ParentID, References: append([]chatcore.Reference(nil), request.References...),
		}
	}
	return f.post, nil
}

func (f *personaChatWriterFake) GetConversation(_ context.Context, request chatcore.GetConversationRequest) (chatcore.Conversation, error) {
	if f.err != nil {
		return chatcore.Conversation{}, f.err
	}
	if f.room.ID == "" {
		return chatcore.Conversation{}, chatcore.ErrNotFound
	}
	return f.room, nil
}

func (f *personaChatWriterFake) ListMemberships(context.Context, chatcore.ListMembershipsRequest) (chatcore.ListMembershipsResponse, error) {
	if f.err != nil {
		return chatcore.ListMembershipsResponse{}, f.err
	}
	return chatcore.ListMembershipsResponse{Memberships: append([]chatcore.Membership(nil), f.members...), NextCursor: f.nextMembers}, nil
}

type personaReferenceResolverFake struct {
	mentions []agentinvoke.Mention
	err      error
	calls    int
}

func (f *personaReferenceResolverFake) ResolvePersonaMentions(_ context.Context, tenant, conversation string, references []chatcore.Reference) ([]agentinvoke.Mention, error) {
	f.calls++
	if tenant != "tenant-a" || conversation != "channel-a" || len(references) != 1 || references[0].Kind != chatcore.AgentMention || references[0].ID != "persona-comp" {
		return nil, errors.New("unexpected typed persona reference input")
	}
	return append([]agentinvoke.Mention(nil), f.mentions...), f.err
}

type personaAuthorityFake struct {
	admission    agentinvoke.Admission
	requireTuple bool
}

func (f personaAuthorityFake) Resolve(ctx context.Context, request agentinvoke.AdmissionRequest) (agentinvoke.Admission, error) {
	if f.requireTuple {
		tuple, ok := ctx.Value(personaChatAuthorityTupleKey{}).(PersonaChatAuthorityRequest)
		if !ok || tuple.Tenant.String() != request.TenantID || tuple.InvokerID != request.InvokerID || tuple.ConversationID != request.ConversationID || !required(tuple.ThreadID) || !required(tuple.InvokingPostID) {
			return agentinvoke.Admission{}, errors.New("missing server-created chat authority tuple")
		}
	}
	return f.admission, nil
}

type personaGrantFake struct {
	calls        int
	requireTuple bool
}

func (f *personaGrantFake) CreateOnBehalfOfGrant(ctx context.Context, request agentinvoke.GrantRequest) (agentinvoke.DelegationGrant, error) {
	if f.requireTuple {
		tuple, ok := ctx.Value(personaChatAuthorityTupleKey{}).(PersonaChatAuthorityRequest)
		if !ok || tuple.Tenant.String() != request.TenantID || tuple.InvokerID != request.UserID || tuple.ConversationID != request.ConversationID || tuple.ThreadID != request.ThreadID || tuple.InvokingPostID != request.InvokingPostID {
			return agentinvoke.DelegationGrant{}, errors.New("grant lost the committed chat authority tuple")
		}
	}
	f.calls++
	return agentinvoke.DelegationGrant{ID: "grant-1", UserID: request.UserID, TenantID: request.TenantID, TaskID: request.InvocationID, TargetAgentID: request.TargetAgentID, Skills: request.Skills.Clone(), ExpiresAt: request.ExpiresAt}, nil
}

type personaRunFake struct {
	requests []agentinvoke.RunRequest
	err      error
}

func (f *personaRunFake) Start(_ context.Context, request agentinvoke.RunRequest) error {
	f.requests = append(f.requests, request)
	return f.err
}

func TestPersonaT0RunStarter_PassesCompleteBoundRunToPolicy(t *testing.T) {
	var checked []agentinvoke.RunRequest
	next := &personaRunFake{}
	starter := personaT0RunStarter{
		next:   next,
		policy: personaT0PolicyFake{allowed: true, requests: &checked},
	}
	want := agentinvoke.RunRequest{
		InvocationID: "invocation-a", TenantID: "tenant-a", ConversationID: "channel-a",
		ThreadID: "post-a", InvokingPostID: "post-a", InvokerID: "alice",
		PersonaID: "persona-comp", PersonaVersion: "v1", InstallationID: "install-channel-a",
		Mode: agentinvoke.OnBehalfOf, Skills: agentinvoke.SkillScopes{"synthetic.read": {"synthetic:answer"}},
	}
	if err := starter.Start(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	if len(checked) != 1 || checked[0].InvocationID != want.InvocationID || checked[0].TenantID != want.TenantID ||
		checked[0].PersonaID != want.PersonaID || checked[0].PersonaVersion != want.PersonaVersion ||
		checked[0].InstallationID != want.InstallationID || !agentinvoke.SkillScopesSubset(checked[0].Skills, want.Skills) ||
		!agentinvoke.SkillScopesSubset(want.Skills, checked[0].Skills) {
		t.Fatalf("policy did not receive complete bound request: %+v", checked)
	}
	if len(next.requests) != 1 || next.requests[0].InvocationID != want.InvocationID {
		t.Fatalf("runner received unexpected request: %+v", next.requests)
	}
}

type personaT0PolicyFake struct {
	allowed  bool
	requests *[]agentinvoke.RunRequest
}

func (f personaT0PolicyFake) IsBoundT0Run(_ context.Context, request agentinvoke.RunRequest) (bool, error) {
	if f.requests != nil {
		*f.requests = append(*f.requests, request)
	}
	return f.allowed && request.TenantID == "tenant-a" && request.PersonaID == "persona-comp" &&
		request.PersonaVersion == "v1" && request.InstallationID == "install-channel-a" &&
		request.InvocationID != "" && len(request.Skills) == 1 &&
		len(request.Skills["synthetic.read"]) == 1 && request.Skills["synthetic.read"][0] == "synthetic:answer", nil
}

type personaFailureFake struct {
	posts []string
	errs  []error
}

func (f *personaFailureFake) RecordPersonaInvocationFailure(_ context.Context, post string, err error) {
	f.posts = append(f.posts, post)
	f.errs = append(f.errs, err)
}

func personaInvocationFixture(t *testing.T, authorKind trust.SubjectKind, admission agentinvoke.Admission, runErr error, t0Allowed ...bool) (*personaChatInvocation, *personaChatWriterFake, *personaReferenceResolverFake, *personaRunFake, *personaGrantFake, *personaFailureFake, context.Context) {
	t.Helper()
	now := time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("tenant-a"), Subject: "alice", SubjectKind: authorKind,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceLow,
		SessionRef: "persona-chat-test", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "test-credential",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), principal)
	chat := &personaChatWriterFake{}
	refs := &personaReferenceResolverFake{mentions: []agentinvoke.Mention{{Kind: agentinvoke.PersonaMention, PersonaID: "persona-comp", Display: "Comp Analyst", Canonical: true}}}
	runs, grants, failures := &personaRunFake{err: runErr}, &personaGrantFake{requireTuple: true}, &personaFailureFake{}
	allowT0 := len(t0Allowed) == 0 || t0Allowed[0]
	service, err := newPersonaChatInvocation(personaChatInvocationConfig{
		Chat: chat, References: refs, Authority: personaAuthorityFake{admission: admission, requireTuple: true},
		Grants: grants, Runs: runs, T0Skills: personaT0PolicyFake{allowed: allowT0},
		Repository: agentinvoke.NewMemoryRepository(), Failures: failures,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, chat, refs, runs, grants, failures, ctx
}

func personaAdmission() agentinvoke.Admission {
	skills := agentinvoke.SkillScopes{"synthetic.read": {"synthetic:answer"}}
	return agentinvoke.Admission{
		Persona:      agentinvoke.Persona{ID: "persona-comp", Version: "v1", PinnedSkills: skills, Current: true},
		Installation: agentinvoke.Installation{ID: "install-channel-a", Current: true, SkillCeiling: skills},
		Channel:      agentinvoke.ChannelPolicy{SkillCeiling: skills},
		Discoverable: skills, HumanMember: true, AudienceMember: true, PersonaInstalled: true,
	}
}

func personaSendRequest() chatcore.SendPostRequest {
	return chatcore.SendPostRequest{
		Principal: chatcore.Principal{TenantID: "tenant-a", SubjectID: "alice"},
		TenantID:  "tenant-a", ConversationID: "channel-a", Body: "@Comp Analyst summarize the public policy",
		IdempotencyKey: "post-idempotency-1",
		References:     []chatcore.Reference{{Kind: chatcore.AgentMention, TenantID: "tenant-a", ID: "persona-comp", Display: "Comp Analyst"}},
	}
}

func TestPersonaChatInvocation_CommitsBeforeStartingOneOnBehalfOfT0Run(t *testing.T) {
	service, chat, refs, runs, grants, _, ctx := personaInvocationFixture(t, trust.SubjectKindHuman, personaAdmission(), nil)
	post, err := service.SendPost(ctx, personaSendRequest())
	if err != nil || post.ID != "post-1" || chat.calls != 1 {
		t.Fatalf("SendPost post=%+v err=%v durable calls=%d", post, err, chat.calls)
	}
	if refs.calls != 1 || grants.calls != 1 || len(runs.requests) != 1 {
		t.Fatalf("resolver calls=%d grants=%d runs=%d, want one each", refs.calls, grants.calls, len(runs.requests))
	}
	run := runs.requests[0]
	if run.Mode != agentinvoke.OnBehalfOf || run.InvokerID != "alice" || run.InvokingPostID != post.ID ||
		run.PersonaID != "persona-comp" || len(run.Skills) != 1 || run.Skills["synthetic.read"][0] != "synthetic:answer" {
		t.Fatalf("run did not preserve invoker/persona/T0 bounds: %+v", run)
	}
}

func TestPersonaChatInvocation_RequiresEveryAuthorityPort(t *testing.T) {
	if _, err := newPersonaChatInvocation(personaChatInvocationConfig{}); !errors.Is(err, errPersonaChatInvocation) {
		t.Fatalf("missing ports error=%v, want fail-closed constructor error", err)
	}
}

func TestPersonaChatInvocation_ReplayStartsOnlyOneRun(t *testing.T) {
	service, chat, _, runs, grants, _, ctx := personaInvocationFixture(t, trust.SubjectKindHuman, personaAdmission(), nil)
	for i := 0; i < 2; i++ {
		if _, err := service.SendPost(ctx, personaSendRequest()); err != nil {
			t.Fatal(err)
		}
	}
	if chat.calls != 2 || grants.calls != 1 || len(runs.requests) != 1 {
		t.Fatalf("durable sends=%d grants=%d runs=%d, want 2 sends and one idempotent invocation", chat.calls, grants.calls, len(runs.requests))
	}
}

func TestPersonaChatInvocation_SkipsEditedAndAgentAuthoredPosts(t *testing.T) {
	for _, tc := range []struct {
		name       string
		authorKind trust.SubjectKind
		revision   uint64
	}{
		{name: "edited post", authorKind: trust.SubjectKindHuman, revision: 2},
		{name: "agent authored", authorKind: trust.SubjectKindAgent, revision: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, chat, refs, runs, grants, _, ctx := personaInvocationFixture(t, tc.authorKind, personaAdmission(), nil)
			if tc.revision != 0 {
				chat.post = chatcore.Post{ID: "post-edited", TenantID: "tenant-a", ConversationID: "channel-a", AuthorID: "alice", Body: "@Comp Analyst", Revision: tc.revision, References: personaSendRequest().References}
			}
			if _, err := service.SendPost(ctx, personaSendRequest()); err != nil {
				t.Fatal(err)
			}
			if len(runs.requests) != 0 || grants.calls != 0 || refs.calls != 0 {
				t.Fatalf("edited/non-human post invoked persona: refs=%d grants=%d runs=%d", refs.calls, grants.calls, len(runs.requests))
			}
		})
	}
}

func TestPersonaChatInvocation_UninstalledOrUnauthorizedMentionStartsNoRun(t *testing.T) {
	for _, tc := range []struct {
		name      string
		admission func() agentinvoke.Admission
	}{
		{name: "uninstalled", admission: func() agentinvoke.Admission { a := personaAdmission(); a.PersonaInstalled = false; return a }},
		{name: "outside audience", admission: func() agentinvoke.Admission { a := personaAdmission(); a.AudienceMember = false; return a }},
		{name: "nonmember", admission: func() agentinvoke.Admission { a := personaAdmission(); a.HumanMember = false; return a }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, chat, refs, runs, grants, _, ctx := personaInvocationFixture(t, trust.SubjectKindHuman, tc.admission(), nil)
			post, err := service.SendPost(ctx, personaSendRequest())
			if err != nil || post.ID == "" || chat.calls != 1 {
				t.Fatalf("unauthorized mention must not fail human post: post=%+v err=%v", post, err)
			}
			if len(runs.requests) != 0 || grants.calls != 0 || refs.calls != 1 {
				t.Fatalf("denied mention ran: refs=%d grants=%d runs=%d", refs.calls, grants.calls, len(runs.requests))
			}
		})
	}
}

func TestPersonaChatInvocation_RunFailureDoesNotFailCommittedPost(t *testing.T) {
	providerErr := errors.New("synthetic provider failed")
	service, chat, _, runs, _, failures, ctx := personaInvocationFixture(t, trust.SubjectKindHuman, personaAdmission(), providerErr)
	post, err := service.SendPost(ctx, personaSendRequest())
	if err != nil || post.ID != "post-1" || chat.calls != 1 {
		t.Fatalf("provider failure escaped after commit: post=%+v err=%v calls=%d", post, err, chat.calls)
	}
	if len(runs.requests) != 1 || len(failures.errs) != 1 || failures.posts[0] != post.ID || !errors.Is(failures.errs[0], providerErr) {
		t.Fatalf("run/failure evidence runs=%d failures=%v posts=%v", len(runs.requests), failures.errs, failures.posts)
	}
}

func TestPersonaChatInvocation_NonT0SkillIsStoppedBeforeRunner(t *testing.T) {
	service, chat, _, runs, _, failures, ctx := personaInvocationFixture(t, trust.SubjectKindHuman, personaAdmission(), nil, false)
	post, err := service.SendPost(ctx, personaSendRequest())
	if err != nil || post.ID == "" || chat.calls != 1 {
		t.Fatalf("T0 rejection changed durable post: post=%+v err=%v", post, err)
	}
	if len(runs.requests) != 0 || len(failures.errs) != 1 || !errors.Is(failures.errs[0], errPersonaChatInvocation) {
		t.Fatalf("non-T0 run reached runner or was not recorded: runs=%d failures=%v", len(runs.requests), failures.errs)
	}
}

func TestPersonaChatInvocation_ChatCommitFailureDoesNotStartRun(t *testing.T) {
	service, chat, refs, runs, grants, _, ctx := personaInvocationFixture(t, trust.SubjectKindHuman, personaAdmission(), nil)
	chat.err = errors.New("chat store failed")
	if _, err := service.SendPost(ctx, personaSendRequest()); err == nil {
		t.Fatal("SendPost hid the chat commit failure")
	}
	if refs.calls != 0 || grants.calls != 0 || len(runs.requests) != 0 {
		t.Fatalf("persona started without committed post: refs=%d grants=%d runs=%d", refs.calls, grants.calls, len(runs.requests))
	}
}

func TestAgentUXR5Srv_DirectAgentInvocation(t *testing.T) {
	service, chat, refs, runs, grants, _, ctx := personaInvocationFixture(t, trust.SubjectKindHuman, personaAdmission(), nil)
	joined := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	chat.room = chatcore.Conversation{ID: "channel-a", TenantID: "tenant-a", Kind: chatcore.Direct}
	chat.members = []chatcore.Membership{
		{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "channel-a", SubjectID: "alice", JoinedAt: &joined},
		{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "channel-a", SubjectID: "persona-comp", JoinedAt: &joined},
	}
	request := personaSendRequest()
	request.Body, request.References = "Summarize the policy", nil
	post, err := service.SendPost(ctx, request)
	if err != nil || post.ID == "" || refs.calls != 1 || grants.calls != 1 || len(runs.requests) != 1 || runs.requests[0].PersonaID != "persona-comp" {
		t.Fatalf("plain direct invocation post=%+v refs=%d grants=%d runs=%+v err=%v", post, refs.calls, grants.calls, runs.requests, err)
	}
}

func TestAgentUXR5Srv_DirectAgentInvocation_TypedReferenceIsNotDoubled(t *testing.T) {
	for _, otherMember := range []string{"persona-comp", "bound-different-agent"} {
		t.Run(otherMember, func(t *testing.T) {
			service, chat, refs, runs, grants, _, ctx := personaInvocationFixture(t, trust.SubjectKindHuman, personaAdmission(), nil)
			joined := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
			chat.room = chatcore.Conversation{ID: "channel-a", TenantID: "tenant-a", Kind: chatcore.Direct}
			chat.members = []chatcore.Membership{
				{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "channel-a", SubjectID: "alice", JoinedAt: &joined},
				{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "channel-a", SubjectID: otherMember, JoinedAt: &joined},
			}
			if _, err := service.SendPost(ctx, personaSendRequest()); err != nil {
				t.Fatal(err)
			}
			if refs.calls != 1 || grants.calls != 1 || len(runs.requests) != 1 {
				t.Fatalf("typed reference duplicated: refs=%d grants=%d runs=%d", refs.calls, grants.calls, len(runs.requests))
			}
		})
	}
}

func TestAgentUXR5Srv_DirectAgentInvocation_ThreeMemberConversationIsNotAdmitted(t *testing.T) {
	service, chat, refs, runs, grants, _, ctx := personaInvocationFixture(t, trust.SubjectKindHuman, personaAdmission(), nil)
	joined := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	chat.room = chatcore.Conversation{ID: "channel-a", TenantID: "tenant-a", Kind: chatcore.Direct}
	chat.members = []chatcore.Membership{
		{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "channel-a", SubjectID: "alice", JoinedAt: &joined},
		{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "channel-a", SubjectID: "persona-comp", JoinedAt: &joined},
		{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "channel-a", SubjectID: "bob", JoinedAt: &joined},
	}
	request := personaSendRequest()
	request.Body, request.References = "Summarize the policy", nil
	if _, err := service.SendPost(ctx, request); err != nil {
		t.Fatal(err)
	}
	// No agent is named and the conversation is not a person's own with one
	// other member, so nothing is resolved at all.
	if refs.calls != 0 || grants.calls != 0 || len(runs.requests) != 0 {
		t.Fatalf("three-member direct admitted: refs=%d grants=%d runs=%d", refs.calls, grants.calls, len(runs.requests))
	}
}
