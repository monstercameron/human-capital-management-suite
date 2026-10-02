package application

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

func TestAgentUXProactive_ScheduleMath(t *testing.T) {
	at := time.Date(2000, 1, 1, 9, 0, 0, 0, time.UTC)
	cron, err := AgentAnnouncementCron(AgentAnnouncementSchedule{Cadence: "WEEKLY", Weekdays: []time.Weekday{time.Monday}, At: at, Zone: "America/New_York"})
	if err != nil || cron != "0 9 * * 1" {
		t.Fatalf("weekly cron = %q, %v", cron, err)
	}
	for _, tc := range []struct {
		schedule AgentAnnouncementSchedule
		want     string
	}{
		{AgentAnnouncementSchedule{Cadence: "DAILY", At: at, Zone: "Europe/Berlin"}, "0 9 * * *"},
		{AgentAnnouncementSchedule{Cadence: "MONTHLY", MonthDay: 15, At: at, Zone: "Asia/Dubai"}, "0 9 15 * *"},
		{AgentAnnouncementSchedule{Cadence: "WEEKLY", Weekdays: []time.Weekday{time.Friday, time.Monday}, At: at, Zone: "UTC"}, "0 9 * * 1,5"},
	} {
		if got, err := AgentAnnouncementCron(tc.schedule); err != nil || got != tc.want {
			t.Errorf("cron = %q, %v, want %q", got, err, tc.want)
		}
	}
	def := schedule.TriggerDefinition{ID: "announcement-holidays", Version: "1", TenantID: "tenant", Target: intent.Ref{TypeID: "hcmnext.test.announcement", Version: 1}, InputTemplateDigest: "sha256:" + strings.Repeat("a", 64), Purpose: "agent.announcement", Owner: "owner", Source: schedule.TriggerSource{Kind: schedule.SourceCron, Cron: schedule.CronSource{Expression: cron}}, Overlap: schedule.OverlapSkip, Storm: schedule.StormPolicy{MaxFiringsPerWindow: 20, Window: 30 * 24 * time.Hour}, ExecutionMode: intent.ModeExecute, ExecutionEnvironment: intent.EnvironmentProduction}
	published, err := schedule.Publish(schedule.NewRegistry(), def, []schedule.AuthorizedTarget{{Ref: def.Target, Purposes: []string{def.Purpose}, AllowedModes: []intent.Mode{intent.ModeExecute}, PublisherRef: intent.Ref{TypeID: "hcmnext.scheduling.publish_triggers", Version: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := schedule.CalculateOccurrences(published, schedule.OccurrenceRequest{Window: schedule.OccurrenceWindow{Start: values.NewInstant(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)), End: values.NewInstant(time.Date(2026, 3, 17, 0, 0, 0, 0, time.UTC))}, Zone: values.ZoneRef{ID: "America/New_York", TzdbVersion: "2026a"}, Misfire: schedule.MisfireConfig{Policy: schedule.MisfireCatchUpOnce, MaxCatchUp: 1}})
	if err != nil || len(result.Occurrences) != 3 {
		t.Fatalf("DST occurrences = %+v, %v", result.Occurrences, err)
	}
	if first, second := result.Occurrences[0].At.Time(), result.Occurrences[1].At.Time(); first.Hour() != 14 || second.Hour() != 13 {
		t.Fatalf("09:00 local did not cross DST correctly: %s, %s", first, second)
	}
	for _, tc := range []struct {
		cadence                    AgentAnnouncementSchedule
		start, end                 time.Time
		count, firstHour, lastHour int
	}{
		{AgentAnnouncementSchedule{Cadence: "DAILY", At: at, Zone: "Europe/Berlin"}, time.Date(2026, 3, 28, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC), 3, 8, 7},
		{AgentAnnouncementSchedule{Cadence: "MONTHLY", MonthDay: 31, At: at, Zone: "Asia/Dubai"}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC), 2, 5, 5},
	} {
		expression, err := AgentAnnouncementCron(tc.cadence)
		if err != nil {
			t.Fatal(err)
		}
		def.Source.Cron.Expression = expression
		def.Storm = schedule.StormPolicy{MaxFiringsPerWindow: 400, Window: 366 * 24 * time.Hour}
		trigger, err := schedule.Publish(schedule.NewRegistry(), def, []schedule.AuthorizedTarget{{Ref: def.Target, Purposes: []string{def.Purpose}, AllowedModes: []intent.Mode{intent.ModeExecute}, PublisherRef: intent.Ref{TypeID: "hcmnext.scheduling.publish_triggers", Version: 1}}})
		if err != nil {
			t.Fatal(err)
		}
		got, err := schedule.CalculateOccurrences(trigger, schedule.OccurrenceRequest{Window: schedule.OccurrenceWindow{Start: values.NewInstant(tc.start), End: values.NewInstant(tc.end)}, Zone: values.ZoneRef{ID: tc.cadence.Zone, TzdbVersion: "2026a"}, Misfire: schedule.MisfireConfig{Policy: schedule.MisfireCatchUpOnce, MaxCatchUp: 1}})
		if err != nil || len(got.Occurrences) != tc.count {
			t.Fatalf("%s dates: %+v %v", tc.cadence.Cadence, got, err)
		}
		if got.Occurrences[0].At.Time().Hour() != tc.firstHour || got.Occurrences[len(got.Occurrences)-1].At.Time().Hour() != tc.lastHour {
			t.Fatalf("%s changed the owner's clock: %+v", tc.cadence.Cadence, got.Occurrences)
		}
	}
}

type proactiveRepository struct {
	mu          sync.Mutex
	record      agentstore.Announcement
	occurrences map[string]agentstore.AnnouncementOccurrence
	commands    map[string]string
}

func (r *proactiveRepository) CheckCommand(_ context.Context, _ uuid.UUID, owner, key, id, digest string) (bool, error) {
	prior := r.commands[owner+":"+key]
	if prior == "" {
		return false, nil
	}
	if prior != id+":"+digest {
		return false, agentstore.ErrAnnouncementRevision
	}
	return true, nil
}
func (r *proactiveRepository) RecordCommand(_ context.Context, _ uuid.UUID, owner, key, id, digest string) error {
	if r.commands == nil {
		r.commands = map[string]string{}
	}
	r.commands[owner+":"+key] = id + ":" + digest
	return nil
}

func (r *proactiveRepository) WithAnnouncementFence(_ context.Context, _ uuid.UUID, _ string, fn func() error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return fn()
}

func (r *proactiveRepository) WithAnnouncementCommandFence(ctx context.Context, tenant uuid.UUID, id, _, _ string, fn func() error) error {
	return r.WithAnnouncementFence(ctx, tenant, id, fn)
}
func (r *proactiveRepository) GetOccurrence(_ context.Context, _ uuid.UUID, _ string, key string) (agentstore.AnnouncementOccurrence, bool, error) {
	out, ok := r.occurrences[key]
	return out, ok, nil
}

func (r *proactiveRepository) Create(_ context.Context, record agentstore.Announcement) error {
	r.record = record
	return nil
}
func (r *proactiveRepository) Save(_ context.Context, record agentstore.Announcement, expected uint64) error {
	if r.record.Revision != expected {
		return agentstore.ErrAnnouncementRevision
	}
	r.record = record
	return nil
}
func (r *proactiveRepository) Get(context.Context, uuid.UUID, string) (agentstore.Announcement, error) {
	if r.record.ID == "" {
		return agentstore.Announcement{}, agentstore.ErrAnnouncementNotFound
	}
	return r.record, nil
}
func (r *proactiveRepository) ListOwner(context.Context, uuid.UUID, string) ([]agentstore.Announcement, error) {
	return []agentstore.Announcement{r.record}, nil
}
func (r *proactiveRepository) RecordOccurrence(_ context.Context, occurrence agentstore.AnnouncementOccurrence) (bool, error) {
	if prior, ok := r.occurrences[occurrence.OccurrenceID]; ok {
		return false, map[bool]error{true: nil, false: agentstore.ErrAnnouncementRevision}[prior == occurrence]
	}
	r.occurrences[occurrence.OccurrenceID] = occurrence
	r.record.LastResult, r.record.LastReason, r.record.LastMessageID, r.record.LastOccurrence = occurrence.Result, occurrence.Reason, occurrence.MessageID, occurrence.OccurrenceID
	return true, nil
}

type proactiveDocuments struct {
	documents []AgentAnnouncementResolvedDocument
}

func (d proactiveDocuments) ResolveAnnouncementDocuments(context.Context, string, string, []agentdocref.Reference) ([]AgentAnnouncementResolvedDocument, error) {
	return append([]AgentAnnouncementResolvedDocument(nil), d.documents...), nil
}

type proactiveModel struct {
	calls int
	t     *testing.T
}

func (m *proactiveModel) RunAnnouncement(ctx context.Context, request AgentAnnouncementRunRequest) (AgentAnnouncementRunResult, error) {
	if saved, ok := ctx.Value(announcementSavedPreviewKey{}).(AgentAnnouncementRunResult); ok {
		return proactiveSealedResult(m.t, request, saved.Text), nil
	}
	m.calls++
	if request.Today != "2026-10-01" || request.Zone != "America/New_York" || len(request.Documents) != 1 {
		return AgentAnnouncementRunResult{}, errors.New("prompt inputs missing")
	}
	return proactiveSealedResult(m.t, request, "The next company holiday is Thanksgiving."), nil
}

func proactiveSealedResult(t *testing.T, request AgentAnnouncementRunRequest, text string) AgentAnnouncementRunResult {
	t.Helper()
	gateway, err := agentsecurity.NewToolGateway([]agentsecurity.ToolDescriptor{PersonaChatReplyToolDescriptor()})
	if err != nil {
		t.Fatal(err)
	}
	call := agentsecurity.ToolCall{
		Agent:      agentsecurity.AgentIdentity{Identity: "workload:announcement", AgentID: "agent", Tenant: request.TenantID, Purpose: "announcement", ToolSet: []string{"persona.chat_reply"}, DataScope: []string{"chat.current"}, Budget: 2},
		Delegation: []agentsecurity.DelegationLink{{GrantID: "fixture-grant", Delegator: request.OwnerID, Delegate: "agent", Tenant: request.TenantID, Purpose: "announcement", ToolSet: []string{"persona.chat_reply"}, DataScope: []string{"chat.current"}, Budget: 2}},
		Tenant:     request.TenantID, Purpose: "announcement", Tool: "persona.chat_reply", Capability: "persona.reply", Version: 1, Nonce: request.OccurrenceID, Args: map[string]any{"occurrence": request.OccurrenceID}, InputTaint: []string{string(agentsecurity.TaintDerived)}, Provenance: []string{PersonaChatReplyProvenance, "chat.current"}, CostBudget: 1, DataScope: []string{"chat.current"},
	}
	call.ArgsDigest, err = agentsecurity.DigestArguments(call.Args)
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := gateway.Admit(call)
	if err != nil {
		t.Fatal(err)
	}
	var grounding []agentsecurity.Datum
	for _, doc := range request.Documents {
		source := "document:" + doc.DocumentID + "/version:" + doc.Version
		datum, err := gateway.Observe(agentsecurity.SourceDocument, doc.Content, agentsecurity.KindObservation, agentsecurity.Citation{SourceID: source, Location: source, Digest: doc.Digest})
		if err != nil {
			t.Fatal(err)
		}
		grounding = append(grounding, datum)
	}
	record := agentrun.Record{ID: request.OccurrenceID, Request: agentrun.Request{Source: agentrun.SourceIdentity{TenantID: request.TenantID, Kind: agentrun.SourcePersonaMention, Key: request.OccurrenceID, Ref: request.OccurrenceID}, Persona: &agentrun.PersonaRef{ID: request.PersonaID, Version: "1", Digest: "sha256:" + strings.Repeat("a", 64)}, InstallationID: request.InstallationID, Principal: agentrun.PrincipalChain{InvokerID: request.OwnerID}, Audience: agentrun.AudienceScope{ID: request.ConversationID}, Context: agentrun.ContextScope{ID: request.OccurrenceID}}}
	authority := PersonaRunChatReplyAuthority{Schema: PersonaRunOutputSchemaRef{ID: PersonaChatReplySchema, Version: 1, Digest: PersonaChatReplySchemaDigest, PersonaDigest: record.Request.Persona.Digest}, ModelDigest: "sha256:" + strings.Repeat("b", 64), Gateway: gateway, Admission: admitted, Grounding: grounding}
	output, err := sealPersonaRunChatReply(context.Background(), record, runstate.Run{ID: record.ID, TenantID: request.TenantID}, authority, text)
	if err != nil {
		t.Fatal(err)
	}
	return AgentAnnouncementRunResult{Output: output}
}

type proactivePublic struct{ allowed bool }

func (p proactivePublic) AuthorizeAnnouncementDocuments(context.Context, string, string, []AgentAnnouncementResolvedDocument, []string) (bool, int, error) {
	if p.allowed {
		return true, 0, nil
	}
	return false, 1, ErrAgentAnnouncementNotPublic
}

type proactiveDelivery struct {
	messages map[string]string
	calls    int
}

func (d *proactiveDelivery) PostAnnouncement(_ context.Context, _ AgentAnnouncementRunRequest, _ AgentAnnouncementRunResult, key string) (string, error) {
	if id := d.messages[key]; id != "" {
		return id, nil
	}
	d.calls++
	d.messages[key] = "post-1"
	return "post-1", nil
}

func TestAgentUXProactive_IdempotentOccurrence(t *testing.T) {
	tenant := uuid.New()
	repository := &proactiveRepository{record: agentstore.Announcement{TenantID: tenant, TenantKey: "tenant-a", ID: "holidays", InstallationID: "install", PersonaID: "policy-helper", ConversationID: "general", Instruction: "Tell employees which company holidays are coming up.", Documents: []agentdocref.Reference{{DocumentID: "holiday-guide", VersionMode: agentdocref.ModeLatestPublished, Label: "2026 holiday guide"}}, Cadence: "WEEKLY", Weekdays: []int16{1}, LocalTime: time.Date(2000, 1, 1, 9, 0, 0, 0, time.UTC), Zone: "America/New_York", State: agentstore.AnnouncementActive, OwnerID: "owner", SchedulerID: "announcement:holidays", Revision: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}, occurrences: map[string]agentstore.AnnouncementOccurrence{}}
	model := &proactiveModel{t: t}
	delivery := &proactiveDelivery{messages: map[string]string{}}
	runner := AgentAnnouncementRunner{Store: repository, Documents: proactiveDocuments{documents: []AgentAnnouncementResolvedDocument{{DocumentID: "holiday-guide", Version: "1", Title: "2026 holiday guide", Content: "Thanksgiving", Digest: personaRunT0ToolOutputDigest([]byte("Thanksgiving"))}}}, Model: model, Public: proactivePublic{allowed: true}, Delivery: delivery, Now: func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }}
	for range 2 {
		result, err := runner.RunOccurrence(context.Background(), tenant, "holidays", "occurrence-monday", false)
		if err != nil || result.MessageID != "post-1" {
			t.Fatalf("run result = %+v, %v", result, err)
		}
	}
	if model.calls != 1 || delivery.calls != 1 || len(repository.occurrences) != 1 {
		t.Fatalf("replay posted %d messages and stored %d results", delivery.calls, len(repository.occurrences))
	}
}

type proactiveChat struct {
	chat.ConversationService
	conversation chat.Conversation
}

func (c proactiveChat) GetConversation(context.Context, chat.GetConversationRequest) (chat.Conversation, error) {
	return c.conversation, nil
}

type proactiveAudience struct {
	snapshot chatrecipient.AudienceSnapshot
}

func (a proactiveAudience) CurrentAudience(context.Context, chat.Conversation) (chatrecipient.AudienceSnapshot, error) {
	return a.snapshot, nil
}
func (proactiveAudience) AuthorizeDisclosure(context.Context, chatrecipient.AudiencePrincipal, chatrecipient.Disclosure) error {
	return nil
}
func (proactiveAudience) AllowDataClass(context.Context, chat.Conversation, dlp.DataClass) error {
	return nil
}

type proactiveDocumentAuthority struct {
	placed  bool
	denied  map[string]bool
	foreign bool
}

func (a proactiveDocumentAuthority) OfficialConversationPlacement(_ context.Context, tenant, _, _, _, _, _ string) (bool, error) {
	if a.foreign || tenant != "tenant-a" {
		return false, errors.New("wrong tenant")
	}
	return a.placed, nil
}
func (a proactiveDocumentAuthority) AuthorizeDocumentRead(_ context.Context, tenant, _, _, _, _, home, subject string) error {
	if a.foreign || tenant != "tenant-a" || home != tenant || a.denied[subject] {
		return errors.New("denied")
	}
	return nil
}

func TestAgentUXProactive_PublicRule_Security(t *testing.T) {
	conversation := chat.Conversation{TenantID: "tenant-a", ID: "general", Kind: chat.PublicChannel, Revision: 3}
	members := []chatrecipient.AudiencePrincipal{{TenantID: "tenant-a", SubjectID: "owner"}, {TenantID: "tenant-a", SubjectID: "employee"}}
	document := AgentAnnouncementResolvedDocument{DocumentID: "holiday-guide", Version: "1", Digest: "sha256:" + strings.Repeat("c", 64)}
	build := func(documents proactiveDocumentAuthority, current []chatrecipient.AudiencePrincipal) AgentAnnouncementAudienceAuthority {
		return AgentAnnouncementAudienceAuthority{Chat: proactiveChat{conversation: conversation}, Audience: proactiveAudience{snapshot: chatrecipient.AudienceSnapshot{TenantID: "tenant-a", ConversationID: "general", Revision: 8, CurrentMembers: current, EligibilityPopulation: current, Complete: true, EligibilityComplete: true, GuestAndExternalComplete: true}}, Documents: documents, Principal: func(context.Context, string) (chat.Principal, error) {
			return chat.Principal{TenantID: "tenant-a", SubjectID: "owner"}, nil
		}}
	}
	for _, tc := range []struct {
		name      string
		authority proactiveDocumentAuthority
		current   []chatrecipient.AudiencePrincipal
		want      bool
	}{
		{"official placement", proactiveDocumentAuthority{placed: true}, members, true},
		{"one member denied", proactiveDocumentAuthority{denied: map[string]bool{"employee": true}}, members, false},
		{"other tenant source", proactiveDocumentAuthority{foreign: true}, members, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			allowed, _, _ := build(tc.authority, tc.current).AuthorizeAnnouncementDocuments(context.Background(), "tenant-a", "general", []AgentAnnouncementResolvedDocument{document}, []string{"holiday-guide"})
			if allowed != tc.want {
				t.Fatalf("allowed=%t, want %t", allowed, tc.want)
			}
		})
	}
	large := make([]chatrecipient.AudiencePrincipal, 201)
	for i := range large {
		large[i] = chatrecipient.AudiencePrincipal{TenantID: "tenant-a", SubjectID: "member-" + strconv.Itoa(i)}
	}
	if allowed, _, _ := build(proactiveDocumentAuthority{}, large).AuthorizeAnnouncementDocuments(context.Background(), "tenant-a", "general", []AgentAnnouncementResolvedDocument{document}, []string{"holiday-guide"}); allowed {
		t.Fatal("unplaced document became public above 200 members")
	}
}
