package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// This is an isolated pipeline fixture, not provider qualification or evidence
// for publication. Only the chat persistence and delivery are real PostgreSQL;
// the model, grants, document result and output persister are explicit fixtures.
func TestTodo_AGENTP_008_Integration_ToolContinuationDeliversOnlyInScopeReplies(t *testing.T) {
	for _, tc := range []struct {
		name, second string
		allowed      bool
		dropPolicy   bool
		public       bool
		audienceRace bool
	}{
		{name: "policy reply", second: `{"text":"The fictional policy allows annual leave.","tool_proposals":[],"requested_actions":["read_policy"]}`, allowed: true},
		{name: "out of scope after search", second: `{"text":"","tool_proposals":[],"requested_actions":["change_compensation"]}`},
		{name: "action policy removed after search", dropPolicy: true},
		{name: "shared policy reply", second: `{"text":"The fictional policy allows annual leave.","tool_proposals":[],"requested_actions":["read_policy"]}`, allowed: true, public: true},
		{name: "audience changes before shared commit", second: `{"text":"The fictional policy allows annual leave.","tool_proposals":[],"requested_actions":["read_policy"]}`, allowed: true, public: true, audienceRace: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := pgtest.NewEmpty(t)
			applyPersonaChatMigrations(t, db)
			raw, err := chatstore.New(context.Background(), chatstore.Config{DSN: personaChatSchemaDSN(t, db.URL, db.Schema)})
			if err != nil {
				t.Fatal(err)
			}
			chatStore := chatstore.NewAdapter(raw)
			t.Cleanup(chatStore.Close)
			now := time.Now().UTC()
			clock := func() time.Time { return now }
			chat := chatcore.NewService(chatStore, clock)
			chat.SetAuthority(servedPersonaChatAuthority{})
			chat.SetEphemeralStore(chatStore)
			alice := chatcore.Principal{TenantID: "tenant-a", SubjectID: "alice"}
			ctx := context.Background()
			for _, room := range []string{"room-a", "alice-persona-dm"} {
				kind := chatcore.PrivateChannel
				if tc.public && room == "room-a" {
					kind = chatcore.PublicChannel
				}
				if _, err := chat.CreateConversation(ctx, chatcore.CreateConversationRequest{Principal: alice, TenantID: alice.TenantID, ConversationID: room, Kind: kind, Name: room}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := chat.AddMembership(ctx, chatcore.AddMembershipRequest{Principal: alice, Membership: chatcore.Membership{TenantID: alice.TenantID, HomeTenantID: alice.TenantID, ConversationID: "room-a", SubjectID: "bob", HistoryVisibility: chatcore.FullHistory}}); err != nil {
				t.Fatal(err)
			}
			chat.SetPersonaDMResolver(agentp011PersonaDM{conversationID: "alice-persona-dm"})
			root, err := chat.SendPost(ctx, chatcore.SendPostRequest{Principal: alice, TenantID: alice.TenantID, ConversationID: "room-a", Body: "Explain the fictional leave policy.", IdempotencyKey: "pipeline-root"})
			if err != nil {
				t.Fatal(err)
			}
			principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "alice", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "pipeline-test", CredentialDigest: "pipeline-test", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)})
			if err != nil {
				t.Fatal(err)
			}
			ctx = trust.WithPrincipal(ctx, principal)
			validator, original, _, persisted, outputAuthority := personaRunOutputFixture(t)
			facts := personaRunRequestBuilderFacts()
			facts.TenantID, facts.Agent, facts.PersonaDigest = "tenant-a", original.Request.Agent, original.Request.Persona.Digest
			facts.Audience.ID, facts.Context.ID, facts.TriggerID, facts.Deadline = "room-a", root.ID, "invocation-a", now.Add(time.Minute)
			invocation := personaRunRequestBuilderInvocation()
			invocation.TenantID, invocation.InvocationID, invocation.InvokerID = "tenant-a", "invocation-a", "alice"
			invocation.ConversationID, invocation.ThreadID, invocation.InvokingPostID = "room-a", root.ID, root.ID
			invocation.PersonaID, invocation.PersonaVersion = original.Request.Persona.ID, original.Request.Persona.Version
			invocation.InstallationID = original.Request.InstallationID
			invocation.Grant.TenantID, invocation.Grant.UserID = "tenant-a", "alice"
			invocation.Actor.UserID, invocation.Actor.InvocationID = "alice", "invocation-a"
			invocation.Actor.PersonaID, invocation.Actor.PersonaVersion = invocation.PersonaID, invocation.PersonaVersion
			invocation.Actor.InstallationID, invocation.Actor.ConversationID, invocation.Actor.InvokingPostID = invocation.InstallationID, "room-a", root.ID
			builder, err := NewPersonaRunRequestBuilder(&personaRunFactsSourceFake{facts: facts})
			if err != nil {
				t.Fatal(err)
			}
			request, err := builder.BuildPersonaChatAdmission(ctx, invocation)
			if err != nil {
				t.Fatal(err)
			}
			request.Purpose = original.Request.Purpose // Matches the explicit sealed output-authority fixture.
			admissions, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Authority: personaChatAdmissionAuthorityFake{}, Store: agentrun.NewMemoryAdmissionStore(), Now: clock})
			if err != nil {
				t.Fatal(err)
			}
			admission, _, err := admissions.Admit(ctx, request)
			if err != nil || admission.Decision != agentrun.DecisionAccepted {
				t.Fatalf("admit pipeline: %v", err)
			}
			tools, toolPolicy, search, journal, _, _ := personaRunT0ToolFixture(t)
			toolPolicy.binding.Invocation = PersonaT0Invocation{TenantID: "tenant-a", PersonaID: invocation.PersonaID, PersonaVersion: invocation.PersonaVersion, InstallationID: invocation.InstallationID, InvocationID: invocation.InvocationID}
			tools.t0, err = NewPersonaT0SkillPolicy(tools.t0.catalog, tools.t0.revoked, []PersonaT0SkillPin{toolPolicy.binding})
			if err != nil {
				t.Fatal(err)
			}
			groundedTools := &personaPipelineTools{PersonaRunT0ToolExecutor: tools, authority: outputAuthority}
			provider := &personaPipelineProvider{bodies: []string{`{"text":"","tool_proposals":[{"id":"search-1","name":"documents_search","arguments_json":"{\"query\":\"leave\"}"}],"requested_actions":["read_policy"]}`, tc.second}}
			pricing, err := agentmodel.NewPricingSchedule(agentmodel.PricingSchedule{Version: "fixture", Authority: "fixture", Signature: "fixture", Entries: []agentmodel.PricingEntry{{Identity: provider.Identity(), InputMicrosPerToken: 1, OutputMicrosPerToken: 1}}})
			if err != nil {
				t.Fatal(err)
			}
			typedModel, err := agentmodel.NewSchemaFluxAdapter(provider, agentmodel.ModelSelection{ProfileID: "model-profile", ProfileDigest: strings.Repeat("a", 64), Identity: provider.Identity()}, pricing)
			if err != nil {
				t.Fatal(err)
			}
			var floor chatcore.PersonaAudienceFloor = personaReplyDeliveryFloorFake{}
			var committer chatcore.PersonaReplyCommitter = chatStore
			currentReply := &personaPipelineCurrentReplyAuthority{runtimeReplyCurrentAuthorityFake: &runtimeReplyCurrentAuthorityFake{}, store: chatStore, advance: tc.audienceRace}
			if tc.public {
				floor = personaPipelinePublicFloor{chatStore}
				committer, err = newPersonaPublicReplyCommitter(chatStore.Store, currentReply, privateChatGatewayVerifiedWorker(t, now), clock)
				if err != nil {
					t.Fatal(err)
				}
			}
			delivery, err := NewPersonaReplyDelivery(chat, committer, floor)
			if err != nil {
				t.Fatal(err)
			}
			runStore := runstate.NewMemoryStore()
			state, err := runstate.New(runStore, personaChatAdmissionRecheckerFake{})
			if err != nil {
				t.Fatal(err)
			}
			model := &personaPipelineModel{adapter: typedModel}
			receipts := &personaSurfaceReceiptFixture{}
			var reply PersonaRunReplyDeliverer = delivery
			if tc.public {
				reply = &personaReplyReceiptRecorder{next: delivery, store: receipts, actors: &personaReceiptActorFixture{actor: personaReplyActor{AgentID: "agent:canonical", Display: "Policy Helper", InvokerHandle: "alice"}}}
			}
			executor := &personaAdmittedRunExecutor{state: state, store: runStore, model: model, work: &personaPipelineWork{t: t, dropPolicy: tc.dropPolicy}, tools: groundedTools, output: validator, reply: reply, workerID: "pipeline-worker", leaseTTL: time.Minute, now: clock}
			run, runErr := executor.Start(ctx, admission)
			if tc.dropPolicy {
				if provider.calls != 1 || search.calls != 1 || journal.calls != 1 || run.TerminalCode != "MODEL_BINDING_INVALID" || runErr == nil || persisted.calls != 0 {
					t.Fatalf("removed action policy reached inference or output: models=%d searches=%d journal=%d output=%d run=%s error=%v", provider.calls, search.calls, journal.calls, persisted.calls, run.TerminalCode, runErr)
				}
			} else if provider.calls != 2 || search.calls != 1 || journal.calls != 1 || len(provider.requests) != 2 || provider.requests[0].TraceID == provider.requests[1].TraceID {
				t.Fatalf("pipeline did not search once and continue with a unique model step: model=%d search=%d journal=%d error=%v", provider.calls, search.calls, journal.calls, runErr)
			}
			if !tc.dropPolicy {
				continuation := model.requests[1]
				if len(continuation.Tools) != 0 || len(continuation.Messages) < 3 || continuation.Messages[len(continuation.Messages)-1].Role != agentmodel.RoleTool || continuation.Messages[len(continuation.Messages)-1].Content != string(journal.bytes) {
					t.Fatal("second model turn lost the exact executed tool result or retained executable tools")
				}
			}
			if tc.allowed {
				if runErr != nil || run.State != runstate.StateCompleted || persisted.calls != 1 {
					t.Fatalf("positive chat pipeline state=%s output=%d error=%v", run.State, persisted.calls, runErr)
				}
				if tc.public && !tc.audienceRace {
					if currentReply.calls != 1 || currentReply.identity != persisted.projection.Identity() || receipts.writes != 1 {
						t.Fatal("public delivery lost current output authorization or actual actor receipt")
					}
					for _, subject := range []string{"alice", "bob"} {
						posts, err := chatStore.ListPosts(ctx, chatcore.Principal{TenantID: "tenant-a", SubjectID: subject}, "tenant-a", "room-a", 0, chatcore.Page{PageSize: 10}, chatcore.PostWindow{})
						if err != nil || len(posts.Posts) != 2 || posts.Posts[1].AuthorID != invocation.PersonaID || posts.Posts[1].ParentID != root.ID || posts.Posts[1].Body != "The fictional policy allows annual leave." {
							t.Fatalf("shared persona reply not visible in its source thread to %s: %+v %v", subject, posts, err)
						}
						viewer, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: subject, SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "pipeline-viewer", CredentialDigest: "pipeline-viewer", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)})
						if err != nil {
							t.Fatal(err)
						}
						surface := &PersonaChatSurface{Chat: chat, Receipts: receipts}
						actors, err := surface.visiblePostActors(trust.WithPrincipal(context.Background(), viewer), viewer, chatcore.Conversation{ID: "room-a", TenantID: "tenant-a"})
						if err != nil || len(actors) != 1 || actors[0].PostID != posts.Posts[1].ID || actors[0].AgentID != "agent:canonical" || actors[0].InvokerHandle != "alice" || actors[0].Display != "Policy Helper" {
							t.Fatalf("chat UI lost the actual reply's agent and invoker attribution: %+v %v", actors, err)
						}
					}
				} else {
					posts, _, err := chatStore.ListEphemeral(ctx, alice, "tenant-a", "room-a", 0, 10)
					if err != nil || len(posts) != 1 || posts[0].Body != "The fictional policy allows annual leave." || posts[0].ThreadID != root.ID || posts[0].DurableCopyPostID == "" {
						t.Fatalf("actual private chat reply=%+v error=%v", posts, err)
					}
					dm, err := chatStore.ListPosts(ctx, alice, "tenant-a", "alice-persona-dm", 0, chatcore.Page{PageSize: 10}, chatcore.PostWindow{})
					if err != nil || len(dm.Posts) != 1 || dm.Posts[0].ID != posts[0].DurableCopyPostID || !strings.Contains(dm.Posts[0].Body, posts[0].Body) || !strings.Contains(dm.Posts[0].Body, posts[0].ThreadLink) {
						t.Fatalf("private reply lost its durable copy or source backlink: %+v %v", dm, err)
					}
					if tc.audienceRace {
						shared, err := chatStore.ListPosts(ctx, chatcore.Principal{TenantID: "tenant-a", SubjectID: "bob"}, "tenant-a", "room-a", 0, chatcore.Page{PageSize: 10}, chatcore.PostWindow{})
						if err != nil || len(shared.Posts) != 2 || shared.Posts[1].Body != "The persona reply was sent privately to you." || shared.Posts[1].AuthorID != "alice" || len(receipts.receipts) != 1 || receipts.receipts[0].PublicPostID != "" {
							t.Fatalf("audience race disclosed the answer or forged a public receipt: %+v %v", shared, err)
						}
					}
				}
				_, answer, err := persisted.projection.Payload()
				if err != nil || len(answer.Parts) != 1 || len(answer.Parts[0].Citations) != 1 || answer.Parts[0].Citations[0].SourceID != "doc-1" {
					t.Fatalf("sealed answer lost document grounding: %+v %v", answer, err)
				}
				if _, err := executor.Start(ctx, admission); err != nil || provider.calls != 2 || persisted.calls != 1 {
					t.Fatal("replay reran the model or persisted another output")
				}
				if tc.public && receipts.writes != 1 {
					t.Fatal("replay recorded another public delivery receipt")
				}
			} else {
				var failure *PersonaRunFailure
				wantCode := "OUT_OF_SCOPE"
				if tc.dropPolicy {
					wantCode = "MODEL_BINDING_INVALID"
				}
				if !errors.As(runErr, &failure) || failure.Code != wantCode || run.State != runstate.StateFailed || run.TerminalCode != wantCode || persisted.calls != 0 {
					t.Fatalf("second-turn refusal lost its typed reason or persisted output: run=%s/%s output=%d error=%v", run.State, run.TerminalCode, persisted.calls, runErr)
				}
				if !tc.dropPolicy {
					found := false
					for _, checkpoint := range run.Checkpoints {
						if checkpoint.Phase == runstate.PhaseModelCall && checkpoint.Attempt == 2 && checkpoint.Ref == model.requests[1].TraceID && checkpoint.Digest != "" {
							found = true
						}
					}
					if !found {
						t.Fatal("refused second model turn lost its result checkpoint")
					}
				}
				posts, _, err := chatStore.ListEphemeral(ctx, alice, "tenant-a", "room-a", 0, 10)
				if err != nil || len(posts) != 0 {
					t.Fatalf("refused model response reached chat: %+v %v", posts, err)
				}
			}
			bobPosts, _, err := chatStore.ListEphemeral(ctx, chatcore.Principal{TenantID: "tenant-a", SubjectID: "bob"}, "tenant-a", "room-a", 0, 10)
			if err != nil || len(bobPosts) != 0 {
				t.Fatalf("private result leaked to another room member: %+v %v", bobPosts, err)
			}
		})
	}
}

// The test's disclosure owner explicitly allows this fictional document to
// the fixture audience. Production uses PersonaRuntimeAudienceFloor instead.
type personaPipelinePublicFloor struct{ store *chatstore.Adapter }

type personaPipelineCurrentReplyAuthority struct {
	*runtimeReplyCurrentAuthorityFake
	store   *chatstore.Adapter
	advance bool
}

func (a *personaPipelineCurrentReplyAuthority) AuthorizeRecoveredFinalOutput(ctx context.Context, output agentsecurity.FinalOutputPersistence) error {
	if err := a.runtimeReplyCurrentAuthorityFake.AuthorizeRecoveredFinalOutput(ctx, output); err != nil {
		return err
	}
	if a.advance {
		i := output.Identity()
		return a.store.AdvanceAudienceRevision(ctx, i.TenantID, i.ConversationID)
	}
	return nil
}

func (f personaPipelinePublicFloor) AuthorizePersonaOutput(ctx context.Context, output agentsecurity.FinalOutputPersistence) (chatcore.PersonaAudienceDecision, error) {
	i := output.Identity()
	revision, err := f.store.AudienceRevision(ctx, i.TenantID, i.ConversationID)
	if err != nil {
		return chatcore.PersonaAudienceDecision{}, err
	}
	_, answer, err := output.Payload()
	if err != nil || len(answer.Parts) != 1 {
		return chatcore.PersonaAudienceDecision{}, chatcore.ErrPermissionDenied
	}
	return chatcore.PersonaAudienceDecision{Revision: revision, Body: answer.Parts[0].Text, ParentID: i.ThreadID}, nil
}

type personaPipelineWork struct {
	t          *testing.T
	calls      int
	dropPolicy bool
}

func (w *personaPipelineWork) BuildPersonaRunModelWork(_ context.Context, admission agentrun.Record, run runstate.Run) (PersonaRunModelWork, error) {
	w.calls++
	r := executorAdapterRequest(w.t)
	task, err := NewTrustedModelTask(run.TenantID, run.ID, run.AgentDigest, admission.Request.Principal.AgentPrincipalID)
	if err != nil {
		return PersonaRunModelWork{}, err
	}
	r.Task, r.StepID = task, run.ID
	r.Route.Pin.AgentVersionDigest, r.Route.Task.AgentVersionDigest = run.AgentDigest, run.AgentDigest
	r.Route.TraceID, r.Outbound.TaskID, r.Outbound.Tenant, r.Lease.Tenant = run.ID, run.ID, run.TenantID, run.TenantID
	r.ToolResultClass = trustdlp.ClassInternal
	r.Model = agentmodel.ModelRequest{ContractVersion: agentmodel.ContractVersion, TaskProfile: "profile-1", ModelProfile: "model-profile", TraceID: run.ID,
		Messages: []agentmodel.ModelMessage{{Role: agentmodel.RoleUser, Content: "Explain the fictional leave policy."}}, Output: agentmodel.OutputConstraint{Mode: agentmodel.OutputText},
		Deadline: admission.Request.Deadline, Limits: agentmodel.ModelLimits{MaxOutputTokens: 300, MaxCostMicros: 1000},
		Processing:   agentmodel.ProcessingPolicy{Residency: "us", Retention: "zero", TrainingUse: agentmodel.UseDenied, Logging: agentmodel.UseDenied},
		ActionPolicy: &agentmodel.RequestedActionPolicy{ProfileDigest: admission.Request.Persona.Digest, Allowed: []agentmodel.RequestedAction{agentmodel.ActionReadPolicy}}}
	if w.dropPolicy && w.calls > 1 {
		r.Model.ActionPolicy = nil
	}
	r.Outbound.Fields = []agentegress.Field{{Name: "model.message.0", Value: r.Model.Messages[0].Content, Class: trustdlp.ClassPublic, Taint: []string{"PERSONA_INVOKING_POST"}, Provenance: []string{"post:" + admission.Request.Source.Ref}}}
	r.Outbound.DeclaredFields = []string{"model.message.0"}
	r.FieldSources = map[string]string{"model.message.0": "persona-invoking-post"}
	return PersonaRunModelWork{Request: r}, nil
}

type personaPipelineProvider struct {
	bodies   []string
	calls    int
	requests []agentmodel.ModelRequest
}

func (*personaPipelineProvider) Identity() agentmodel.ModelIdentity {
	return agentmodel.ModelIdentity{ProviderID: "fixture", ModelID: "persona-fixture", Version: "1"}
}
func (*personaPipelineProvider) Capabilities() agentmodel.AdapterCapabilities {
	return agentmodel.AdapterCapabilities{ContractVersions: []int{agentmodel.ContractVersion}, Features: []agentmodel.ModelFeature{agentmodel.FeatureTools, agentmodel.FeatureStructuredJSON}, OutputModes: []agentmodel.OutputMode{agentmodel.OutputText, agentmodel.OutputSchema}, MaxTools: 1}
}
func (p *personaPipelineProvider) Invoke(_ context.Context, r agentmodel.ModelRequest) (agentmodel.ModelResult, error) {
	if p.calls >= len(p.bodies) {
		return agentmodel.ModelResult{}, errors.New("unexpected fixture model call")
	}
	body := p.bodies[p.calls]
	p.calls++
	p.requests = append(p.requests, r)
	return agentmodel.ModelResult{ContractVersion: agentmodel.ContractVersion, Provider: p.Identity(), Finish: agentmodel.FinishComplete, Text: body, Structured: json.RawMessage(body), Usage: agentmodel.ModelUsage{InputTokens: 20, OutputTokens: 10, TotalTokens: 30}}, nil
}

type personaPipelineModel struct {
	adapter  *agentmodel.SchemaFluxAdapter
	requests []agentmodel.ModelRequest
}

func (m *personaPipelineModel) Execute(ctx context.Context, r AgentModelExecutorRequest) (AgentModelExecutorResult, error) {
	m.requests = append(m.requests, r.Model)
	result, err := m.adapter.Invoke(ctx, r.Model)
	return AgentModelExecutorResult{Result: result}, err
}

type personaPipelineTools struct {
	*PersonaRunT0ToolExecutor
	authority *personaRunChatReplyAuthorityFake
}

func (p *personaPipelineTools) Execute(ctx context.Context, record agentrun.Record, run runstate.Run, proposal agentmodel.ToolProposal) ([]byte, string, string, error) {
	result, ref, digest, err := p.PersonaRunT0ToolExecutor.Execute(ctx, record, run, proposal)
	if err != nil {
		return nil, "", "", err
	}
	evidence, err := p.authority.authority.Gateway.Observe(agentsecurity.SourceDocument, string(result), agentsecurity.KindObservation, agentsecurity.Citation{SourceID: "doc-1", Location: "document:doc-1/version-4", Digest: digest})
	if err != nil {
		return nil, "", "", err
	}
	p.authority.authority.Grounding = []agentsecurity.Datum{evidence}
	return result, ref, digest, nil
}
