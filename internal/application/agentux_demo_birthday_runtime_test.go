package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

func agentuxDemoBirthdaySeal(t *testing.T, request AgentAnnouncementRunRequest, text string) AgentAnnouncementRunResult {
	t.Helper()
	g, err := agentsecurity.NewToolGateway([]agentsecurity.ToolDescriptor{PersonaChatReplyToolDescriptor()})
	if err != nil {
		t.Fatal(err)
	}
	identity := agentsecurity.AgentIdentity{Identity: "workload:birthday", AgentID: "birthday-buddy", Tenant: request.TenantID, Purpose: "announcement", ToolSet: []string{"persona.chat_reply"}, DataScope: []string{"chat.current"}, Budget: 2}
	call := agentsecurity.ToolCall{Agent: identity, Tenant: request.TenantID, Purpose: identity.Purpose, Tool: "persona.chat_reply", Capability: "persona.reply", Version: 1, Nonce: request.OccurrenceID, Args: map[string]any{"occurrence": request.OccurrenceID}, InputTaint: []string{string(agentsecurity.TaintDerived)}, Provenance: []string{PersonaChatReplyProvenance, "chat.current"}, CostBudget: 1, DataScope: identity.DataScope, Delegation: []agentsecurity.DelegationLink{{GrantID: "fixture-grant", Delegator: request.OwnerID, Delegate: identity.AgentID, Tenant: identity.Tenant, Purpose: identity.Purpose, ToolSet: identity.ToolSet, DataScope: identity.DataScope, Budget: 2}}}
	call.ArgsDigest, err = agentsecurity.DigestArguments(call.Args)
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := g.Admit(call)
	if err != nil {
		t.Fatal(err)
	}
	grounding, err := agentuxDemoBirthdayGrounding(g, *request.Source)
	if err != nil {
		t.Fatal(err)
	}
	datum, err := g.Infer(text, grounding)
	if err != nil {
		t.Fatal(err)
	}
	final, err := g.ValidateFinalOutput(context.Background(), admitted, "persona.chat_reply", agentsecurity.FinalOutputCandidate{Complete: true, Draft: agentsecurity.AgentOutput{Schema: PersonaChatReplySchema, Value: PersonaChatReply{Text: text}, Narrative: text}, Answer: []agentsecurity.Datum{datum}}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	output, err := g.IssueFinalOutputPersistence(context.Background(), admitted, final, agentsecurity.FinalOutputIdentity{TenantID: request.TenantID, OutputID: "output-birthday", InvocationID: request.OccurrenceID, AdmissionID: "admission-birthday", RunID: "run-birthday", InvokerID: request.OwnerID, ConversationID: request.ConversationID, ThreadID: request.AnnouncementID, PostID: request.OccurrenceID, PersonaID: request.PersonaID, PersonaVersion: "1", InstallationID: request.InstallationID})
	if err != nil {
		t.Fatal(err)
	}
	return AgentAnnouncementRunResult{Output: output}
}

type agentuxDemoBirthdayFenceFixture struct {
	*agentuxDemoPeopleFixture
	tenant uuid.UUID
	calls  int
}

func (f *agentuxDemoBirthdayFenceFixture) WithBirthdayPreferenceFence(_ context.Context, tenant uuid.UUID, fn func() error) error {
	if tenant != f.tenant {
		return ErrAgentAnnouncementDenied
	}
	f.calls++
	return fn()
}

func TestAgentUXDemo_BirthdayRuntime_Security(t *testing.T) {
	f, provider, record, now := agentuxDemoPeopleSetup()
	record.ID = "birthday"
	record.InstallationID = "install"
	record.OwnerID = "owner"
	record.Instruction = "Celebrate today's birthdays."
	fence := &agentuxDemoBirthdayFenceFixture{agentuxDemoPeopleFixture: f, tenant: record.TenantID}
	provider.Preferences = fence
	runtime := &AgentAnnouncementRuntime{Sources: AgentUXDemoAnnouncementSources{Birthdays: provider}, Now: func() time.Time { return now }}
	source, err := provider.ResolveAnnouncementSource(context.Background(), record, now)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), agentuxDemoBirthdaySourceKey{}, source)
	profile := agentpersona.PersonaProfile{Instructions: "Only birthday facts."}
	manifest := agentmanifest.Manifest{Purpose: "Birthday greetings"}
	route := PersonaRunModelRoute{Route: agentmodel.RouteRequest{Task: agentmodel.TaskProfile{DataClasses: []string{"INTERNAL"}}}, Egress: agentegress.Profile{AllowedClasses: []dlp.DataClass{dlp.ClassInternal}}, ProfileClass: dlp.ClassInternal, InvokerClass: dlp.ClassInternal}
	goal, err := runtime.goal(ctx, record)
	if err != nil {
		t.Fatal(err)
	}
	request := &AgentModelExecutorRequest{Model: agentmodel.ModelRequest{Messages: []agentmodel.ModelMessage{{Role: agentmodel.RoleSystem, Content: manifest.Purpose}, {Role: agentmodel.RoleDeveloper, Content: personaDeveloperMessage(profile)}, {Role: agentmodel.RoleUser, Content: goal}}}}
	if err := runtime.agentuxDemoBirthdayModelRequest(ctx, record, request, route); err != nil {
		t.Fatal(err)
	}
	fields, err := runtime.agentuxDemoBirthdayAuthoritativeFields(ctx, record, profile, manifest, route)
	if err != nil || len(fields) != 6 || len(request.Model.Messages) != 5 || len(request.Model.ContextRefs) != 1 {
		t.Fatalf("governed fields: %+v %v", fields, err)
	}
	for i, message := range request.Model.Messages {
		field := fields["model.message."+string(rune('0'+i))]
		if field.value != message.Content || field.role != message.Role || field.source != request.FieldSources["model.message."+string(rune('0'+i))] {
			t.Fatal("birthday outbound bytes differ from current source")
		}
	}
	req := AgentAnnouncementRunRequest{TenantID: record.TenantKey, AnnouncementID: record.ID, OccurrenceID: "today", InstallationID: record.InstallationID, PersonaID: record.PersonaID, ConversationID: record.ConversationID, OwnerID: record.OwnerID, Source: &source}
	output := agentuxDemoBirthdaySeal(t, req, "Happy birthday, Ana Flores and Curtis Bell! 🎂 Wishing you a lovely day!").Output
	if err := runtime.agentuxDemoSourceReadFence(ctx, record, nil, func() error { return runtime.agentuxDemoRecheckBirthday(ctx, record, output) }); err != nil || fence.calls != 1 {
		t.Fatalf("preference fence: %v calls=%d", err, fence.calls)
	}
	f.optedOut["ana"] = true
	if err := runtime.agentuxDemoRecheckBirthday(context.Background(), record, output); err == nil {
		t.Fatal("opt-out after inference still posted")
	}
	if _, err := runtime.agentuxDemoBirthdaySource(ctx, record); err == nil {
		t.Fatal("source changed after admission")
	}
	provider.Preferences = f
	runtime.Sources = provider
	if err := runtime.agentuxDemoSourceReadFence(context.Background(), record, nil, func() error { t.Fatal("unfenced birthday delivery"); return nil }); !errors.Is(err, ErrAgentAnnouncementDenied) {
		t.Fatal("missing preference fence allowed")
	}
}

type agentuxDemoBirthdayModel struct {
	t     *testing.T
	calls int
	text  string
}

func (m *agentuxDemoBirthdayModel) RunAnnouncement(_ context.Context, r AgentAnnouncementRunRequest) (AgentAnnouncementRunResult, error) {
	m.calls++
	return agentuxDemoBirthdaySeal(m.t, r, m.text), nil
}

func TestAgentUXDemo_BirthdayAnnouncement(t *testing.T) {
	f, source, record, now := agentuxDemoPeopleSetup()
	record.ID = "birthday-schedule"
	record.InstallationID = "birthday-install"
	record.OwnerID = "owner"
	record.State = agentstore.AnnouncementActive
	record.Instruction = "Wish today's members a happy birthday."
	repo := &proactiveRepository{record: record, occurrences: map[string]agentstore.AnnouncementOccurrence{}}
	model := &agentuxDemoBirthdayModel{t: t, text: "Happy birthday, Ana Flores and Curtis Bell! 🎂 Hope you have a wonderful day!"}
	delivery := &proactiveDelivery{messages: map[string]string{}}
	runner := AgentAnnouncementRunner{Store: repo, Sources: source, Model: model, Public: proactivePublic{allowed: false}, Delivery: delivery, Now: func() time.Time { return now }}
	for range 2 {
		result, err := runner.RunOccurrence(context.Background(), record.TenantID, record.ID, "today", false)
		if err != nil || result.MessageID != "post-1" {
			t.Fatalf("birthday run: %+v %v", result, err)
		}
	}
	if model.calls != 1 || delivery.calls != 1 {
		t.Fatalf("replay: model=%d posts=%d", model.calls, delivery.calls)
	}
	f.people["ana"] = BirthdayPerson{DisplayName: "Ana Flores"}
	f.people["curtis"] = BirthdayPerson{DisplayName: "Curtis Bell"}
	for range 2 {
		result, err := runner.RunOccurrence(context.Background(), record.TenantID, record.ID, "tomorrow", false)
		if err != nil || result.MessageID != "" {
			t.Fatalf("empty day: %+v %v", result, err)
		}
	}
	if model.calls != 1 || delivery.calls != 1 {
		t.Fatal("empty day invoked model or delivery")
	}
	cron, err := AgentAnnouncementCron(AgentAnnouncementSchedule{Cadence: "DAILY", At: time.Date(2000, 1, 1, 9, 0, 0, 0, time.UTC), Zone: record.Zone})
	if err != nil || cron != "0 9 * * *" {
		t.Fatalf("birthday schedule: %s %v", cron, err)
	}
}

func TestAgentUXDemo_BirthdayOutput_Security(t *testing.T) {
	_, provider, record, now := agentuxDemoPeopleSetup()
	source, err := provider.ResolveAnnouncementSource(context.Background(), record, now)
	if err != nil {
		t.Fatal(err)
	}
	r := AgentAnnouncementRunRequest{TenantID: record.TenantKey, AnnouncementID: "birthday", OccurrenceID: "today", InstallationID: "install", PersonaID: AgentUXDemoBirthdayPersonaID, ConversationID: record.ConversationID, OwnerID: "owner", Source: &source}
	for _, text := range []string{"Happy birthday, Ana Flores and Curtis Bell! 🎂 Wishing you a lovely day!", "Happy birthday, Outsider! 🎂 Wishing you a lovely day!", "Happy birthday, Ana Flores and Curtis Bell! 🎂 Ana is 45 today."} {
		output := agentuxDemoBirthdaySeal(t, r, text)
		err := validateAnnouncementOutput(r, &output)
		if strings.Contains(text, "Outsider") || strings.Contains(text, "45") {
			if !errors.Is(err, ErrAgentAnnouncementNotPublic) {
				t.Fatalf("unsafe sealed output accepted: %q %v", text, err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
	data, ref, err := agentuxDemoBirthdayEnvelope(source)
	if err != nil || ref.ID != "people:birthdays-today" || strings.Contains(data, "age") || strings.Contains(data, "2026") || strings.Contains(data, "birth_year") || ref.Digest == "" {
		t.Fatalf("minimized envelope: %s %+v %v", data, ref, err)
	}
	if !strings.Contains(agentuxDemoBirthdayGoal(), "at most two sentences") {
		t.Fatal("bounded model goal missing")
	}
	request := &AgentModelExecutorRequest{Model: agentmodel.ModelRequest{Messages: []agentmodel.ModelMessage{{Role: agentmodel.RoleSystem}, {Role: agentmodel.RoleDeveloper}, {Role: agentmodel.RoleUser}}}}
	runtime := &AgentAnnouncementRuntime{Sources: provider, Now: func() time.Time { return now }}
	if err := runtime.agentuxDemoBirthdayModelRequest(context.Background(), record, request, PersonaRunModelRoute{}); !errors.Is(err, ErrAgentAnnouncementDenied) {
		t.Fatalf("unapproved people data egress: %v", err)
	}
}
