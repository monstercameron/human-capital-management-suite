package application

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentcontrols"
)

type proactiveOwner struct {
	actor  AgentAnnouncementActor
	denied bool
}

func (a *proactiveOwner) AgentAnnouncementActor(context.Context) (AgentAnnouncementActor, error) {
	return a.actor, nil
}
func (a *proactiveOwner) ManageAgentInstallation(_ context.Context, actor AgentAnnouncementActor, install, persona, conversation string) (bool, error) {
	return !a.denied && actor == a.actor && install == "install" && persona == "policy-helper" && conversation == "general", nil
}

type proactiveSchedules struct{ creates, updates, pauses, resumes, deletes int }

func (*proactiveSchedules) NextAnnouncementRun(context.Context, AgentAnnouncementActor, agentstore.Announcement) (*time.Time, error) {
	return nil, nil
}

func (s *proactiveSchedules) CreateAnnouncementSchedule(_ context.Context, _ AgentAnnouncementActor, a agentstore.Announcement, _ string) (string, *time.Time, error) {
	s.creates++
	return a.SchedulerID, nil, nil
}
func (s *proactiveSchedules) UpdateAnnouncementSchedule(context.Context, AgentAnnouncementActor, agentstore.Announcement, string) (*time.Time, error) {
	s.updates++
	return nil, nil
}
func (s *proactiveSchedules) PauseAnnouncementSchedule(context.Context, AgentAnnouncementActor, string, uint64) error {
	s.pauses++
	return nil
}
func (s *proactiveSchedules) ResumeAnnouncementSchedule(context.Context, AgentAnnouncementActor, string, uint64) (*time.Time, error) {
	s.resumes++
	return nil, nil
}
func (s *proactiveSchedules) DeleteAnnouncementSchedule(context.Context, AgentAnnouncementActor, string, uint64) error {
	s.deletes++
	return nil
}

type proactiveNames struct{}

func (proactiveNames) AgentAnnouncementNames(context.Context, AgentAnnouncementActor, agentstore.Announcement) (string, string, string, error) {
	return "Policy Helper", "#general", "Alex Example", nil
}
func (proactiveNames) AgentAnnouncementChoices(context.Context, AgentAnnouncementActor) ([]productui.AgentAnnouncementAgent, error) {
	return []productui.AgentAnnouncementAgent{{PersonaID: "policy-helper", Name: "Policy Helper", Conversations: []productui.AgentAnnouncementConversation{{ID: "general", Name: "#general", InstallationID: "install"}}}}, nil
}

func proactiveControls(t *testing.T) (*AgentAnnouncementControlSurface, *proactiveRepository, *proactiveSchedules, *proactiveOwner, *proactiveModel, *proactiveDelivery, agentcontrols.AnnouncementDraft) {
	t.Helper()
	now := func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }
	repo := &proactiveRepository{occurrences: map[string]agentstore.AnnouncementOccurrence{}}
	owner := &proactiveOwner{actor: AgentAnnouncementActor{TenantID: "tenant-a", TenantUUID: uuid.New(), SubjectID: "owner"}}
	schedules := &proactiveSchedules{}
	model := &proactiveModel{t: t}
	delivery := &proactiveDelivery{messages: map[string]string{}}
	runner := &AgentAnnouncementRunner{Store: repo, Documents: proactiveDocuments{documents: []AgentAnnouncementResolvedDocument{{DocumentID: "holiday-guide", Version: "1", Title: "2026 holiday guide", Content: "Thanksgiving", Digest: personaRunT0ToolOutputDigest([]byte("Thanksgiving"))}}}, Model: model, Public: proactivePublic{allowed: true}, Delivery: delivery, Now: now, ConversationName: func(context.Context, string, string) (string, error) { return "#general", nil }}
	surface := &AgentAnnouncementControlSurface{Service: &AgentAnnouncementService{Store: repo, Authority: owner, Schedules: schedules, Now: now}, Runner: runner, Actors: owner, Names: proactiveNames{}, Now: now, Preview: AgentAnnouncementDraftPreview{Authority: owner, Runner: runner, Reader: proactiveDocumentAuthority{}}}
	draft := agentcontrols.AnnouncementDraft{InstallationID: "install", PersonaID: "policy-helper", ConversationID: "general", Instruction: "Tell employees which company holidays are coming up, using the 2026 holiday guide", Documents: []agentdocref.Reference{{DocumentID: "holiday-guide", VersionMode: agentdocref.ModeLatestPublished, Label: "2026 holiday guide"}}, Cadence: "WEEKLY", Weekdays: []int{1}, Time: "09:00", Zone: "America/New_York", IdempotencyKey: "request-123"}
	return surface, repo, schedules, owner, model, delivery, draft
}

func TestAgentUXProactive_ControlsAndReplay(t *testing.T) {
	s, repo, schedules, owner, _, _, draft := proactiveControls(t)
	ctx := context.Background()
	for range 2 {
		reply, err := s.CreateAnnouncement(ctx, draft)
		if err != nil || len(reply.Snapshot.Rows) != 1 || reply.Snapshot.Rows[0].Editor.Weekdays[0] != 1 {
			t.Fatalf("create: %+v %v", reply, err)
		}
	}
	if schedules.creates != 1 || repo.record.OwnerID != "owner" || repo.record.TenantKey != "tenant-a" {
		t.Fatalf("replay or requester binding: %+v %+v", schedules, repo.record)
	}
	changed := draft
	changed.Instruction = "Different instruction"
	if _, err := s.CreateAnnouncement(ctx, changed); !errors.Is(err, agentcontrols.ErrConflict) {
		t.Fatalf("reused key changed content: %v", err)
	}
	id := repo.record.ID
	draft.ID, draft.ExpectedRevision, draft.IdempotencyKey = id, 1, "update-123"
	draft.Instruction = "Tell employees about the next holiday."
	for range 2 {
		if _, err := s.UpdateAnnouncement(ctx, draft); err != nil {
			t.Fatal(err)
		}
	}
	if repo.record.Revision != 2 || schedules.updates != 1 {
		t.Fatalf("update replay changed revision: %+v", repo.record)
	}
	for i, action := range []string{"PAUSE", "RESUME", "DELETE"} {
		command := agentcontrols.AnnouncementCommand{ID: id, Action: action, ExpectedRevision: uint64(i + 2), IdempotencyKey: strings.ToLower(action) + "-12345"}
		for range 2 {
			if _, err := s.ControlAnnouncement(ctx, command); err != nil {
				t.Fatalf("%s: %v", action, err)
			}
		}
	}
	if schedules.pauses != 1 || schedules.resumes != 1 || schedules.deletes != 1 || repo.record.State != agentstore.AnnouncementDeleted {
		t.Fatalf("control replay mutated twice: %+v %+v", schedules, repo.record)
	}
	owner.denied = true
	if _, err := s.Service.Create(ctx, owner.actor, AgentAnnouncementDraft{ID: "other", InstallationID: "install", PersonaID: "policy-helper", ConversationID: "general", Instruction: "Post a holiday", Documents: draft.Documents, Schedule: AgentAnnouncementSchedule{Cadence: "NOW", At: s.Now(), Zone: "UTC"}}); !errors.Is(err, ErrAgentAnnouncementDenied) || schedules.creates != 1 {
		t.Fatalf("authorization followed a side effect: %v", err)
	}
}

func TestAgentUXProactive_PreviewRefusalAndPause(t *testing.T) {
	s, repo, _, owner, model, delivery, draft := proactiveControls(t)
	ctx := context.Background()
	preview, err := s.PreviewAnnouncement(ctx, draft)
	if err != nil || preview.Preview == nil || !preview.Preview.Public || preview.Preview.Text == "" || len(preview.Preview.Sources) != 1 || repo.record.ID != "" || len(repo.occurrences) != 0 || delivery.calls != 0 {
		t.Fatalf("preview saved or posted: %+v %v", preview, err)
	}
	s.Runner.Public = proactivePublic{}
	preview, err = s.PreviewAnnouncement(ctx, draft)
	if err != nil || preview.Preview.Public || len(repo.occurrences) != 0 {
		t.Fatalf("refusal preview: %+v %v", preview, err)
	}
	owner.denied = true
	if _, err := s.PreviewAnnouncement(ctx, draft); !errors.Is(err, agentcontrols.ErrDenied) || model.calls != 2 {
		t.Fatalf("unauthorized preview invoked model: %v %d", err, model.calls)
	}
	owner.denied = false
	draft.Cadence, draft.Weekdays = "NOW", nil
	reply, err := s.CreateAnnouncement(ctx, draft)
	if err != nil || len(reply.Snapshot.Rows) != 1 || reply.Snapshot.Rows[0].ResultCode != agentstore.AnnouncementRefused || reply.Snapshot.Rows[0].Reason != "1 of the documents cannot be read by everyone in #general" || delivery.calls != 0 {
		t.Fatalf("refusal projection: %+v %v", reply, err)
	}
	repo.record.State = agentstore.AnnouncementPaused
	before := model.calls
	if _, err := s.Runner.RunOccurrence(ctx, owner.actor.TenantUUID, repo.record.ID, "monday", false); !errors.Is(err, ErrAgentAnnouncementDenied) || model.calls != before {
		t.Fatalf("paused occurrence ran: %v", err)
	}
	s.Runner.Model = proactiveFailedModel{}
	reply, err = s.ControlAnnouncement(ctx, agentcontrols.AnnouncementCommand{ID: repo.record.ID, Action: "POST_NOW", ExpectedRevision: 1, IdempotencyKey: "failure-123"})
	if err != nil || reply.Snapshot.Rows[0].ResultCode != agentstore.AnnouncementFailed || reply.Snapshot.Rows[0].Reason != "The service is busy or unavailable. Try again in a few minutes." {
		t.Fatalf("failure projection: %+v %v", reply, err)
	}
}

func TestAgentUXProactive_PreviewRequiresOwnerDocumentAccess_Security(t *testing.T) {
	s, repo, _, owner, model, delivery, draft := proactiveControls(t)
	s.Preview = AgentAnnouncementDraftPreview{Authority: owner, Runner: s.Runner, Reader: proactiveDocumentAuthority{denied: map[string]bool{"owner": true}}}
	if reply, err := s.PreviewAnnouncement(context.Background(), draft); !errors.Is(err, agentcontrols.ErrDenied) || reply.Preview != nil || model.calls != 0 || delivery.calls != 0 || repo.record.ID != "" {
		t.Fatalf("preview disclosed a document the owner cannot read: %+v %v", reply, err)
	}
}

type proactiveFailedModel struct{}

func (proactiveFailedModel) RunAnnouncement(context.Context, AgentAnnouncementRunRequest) (AgentAnnouncementRunResult, error) {
	return AgentAnnouncementRunResult{}, ErrAgentAnnouncementUnavailable
}

func TestAgentUXProactive_ConcurrentOccurrenceReplay(t *testing.T) {
	s, repo, _, owner, model, delivery, draft := proactiveControls(t)
	if _, err := s.CreateAnnouncement(context.Background(), draft); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errCh := make(chan error, 12)
	for range 12 {
		wg.Go(func() {
			result, err := s.Runner.RunOccurrence(context.Background(), owner.actor.TenantUUID, repo.record.ID, "monday", false)
			if err == nil && result.MessageID != "post-1" {
				err = errors.New("replay lost the posted message")
			}
			errCh <- err
		})
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if model.calls != 1 || delivery.calls != 1 || len(repo.occurrences) != 1 {
		t.Fatalf("concurrent replay repeated work: model=%d delivery=%d results=%d", model.calls, delivery.calls, len(repo.occurrences))
	}
}

type proactiveWithdrawnDocuments struct{}

func (proactiveWithdrawnDocuments) ResolveAnnouncementDocuments(context.Context, string, string, []agentdocref.Reference) ([]AgentAnnouncementResolvedDocument, error) {
	return nil, AgentAnnouncementDocumentRefusal{Unreadable: 1}
}

func TestAgentUXProactive_WithdrawnPlacementRefusesOccurrence_Security(t *testing.T) {
	s, repo, _, owner, model, delivery, draft := proactiveControls(t)
	if _, err := s.CreateAnnouncement(context.Background(), draft); err != nil {
		t.Fatal(err)
	}
	s.Runner.Documents = proactiveWithdrawnDocuments{}
	if _, err := s.Runner.RunOccurrence(context.Background(), owner.actor.TenantUUID, repo.record.ID, "monday", false); !errors.Is(err, ErrAgentAnnouncementNotPublic) {
		t.Fatalf("withdrawn placement: %v", err)
	}
	result := repo.occurrences["monday"]
	if result.Result != agentstore.AnnouncementRefused || result.Reason != "1 of the documents cannot be read by everyone in #general" || model.calls != 0 || delivery.calls != 0 {
		t.Fatalf("withdrawn source was modeled or posted: %+v", result)
	}
}

func TestAgentUXProactive_OwnerAndHTTPAdmission_Security(t *testing.T) {
	ctx, p := personaAdminCommandContext(t)
	id := uuid.New()
	a := AgentAnnouncementOwnerAuthority{Now: func() time.Time { return time.Unix(100, 0) }, TenantUUID: func(values.TenantId) uuid.UUID { return id }}
	actor, err := a.AgentAnnouncementActor(ctx)
	if err != nil || actor.SubjectID != p.Subject() || actor.TenantID != p.Tenant().String() || actor.TenantUUID != id {
		t.Fatalf("trusted actor: %+v %v", actor, err)
	}
	if _, err := a.AgentAnnouncementActor(context.Background()); !errors.Is(err, ErrAgentAnnouncementDenied) {
		t.Fatal("anonymous actor accepted")
	}
	if ok, err := a.ManageAgentInstallation(ctx, actor, "install", "forged-persona", "general"); ok || !errors.Is(err, ErrAgentAnnouncementDenied) {
		t.Fatal("missing installation authority accepted")
	}
	nextCalls := 0
	h := OverlayAgentAnnouncements(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { nextCalls++; w.WriteHeader(204) }), nil, transport.Config{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/other", nil))
	if w.Code != 204 || nextCalls != 1 {
		t.Fatal("unrelated route intercepted")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, agentcontrols.AnnouncementPath, nil))
	if w.Code < 400 || nextCalls != 1 {
		t.Fatal("unauthenticated announcement bypassed admission")
	}
}

func TestAgentUXProactive_CatalogNames_Security(t *testing.T) {
	owner := &proactiveOwner{actor: AgentAnnouncementActor{TenantID: "tenant-a", TenantUUID: uuid.New(), SubjectID: "owner"}}
	source := AgentAnnouncementCatalogNames{Versions: catalogVersions{{Profile: agentpersona.PersonaVersion{Profile: agentpersona.PersonaProfile{PersonaID: "policy-helper", Version: 1, DisplayName: "Policy Helper"}}}}, Installations: catalogInstalls{{ID: "install", PersonaID: "policy-helper", PersonaVersion: 1, ConversationID: "general", Conversation: "#general", ConversationKind: "PUBLIC_CHANNEL", Active: true}, {ID: "foreign-install", PersonaID: "policy-helper", PersonaVersion: 1, ConversationID: "private", Conversation: "Restricted", ConversationKind: "PRIVATE_CHANNEL", Active: true}}, Authority: owner, OwnerNames: func(context.Context, string, []string) (map[string]string, error) {
		return map[string]string{"owner": "Alex Example"}, nil
	}}
	choices, err := source.AgentAnnouncementChoices(context.Background(), owner.actor)
	if err != nil || len(choices) != 1 || len(choices[0].Conversations) != 1 || choices[0].Conversations[0].InstallationID != "install" {
		t.Fatalf("choices leaked unauthorized placements: %+v %v", choices, err)
	}
	name, room, person, err := source.AgentAnnouncementNames(context.Background(), owner.actor, agentstore.Announcement{TenantID: owner.actor.TenantUUID, OwnerID: "owner", PersonaID: "policy-helper", InstallationID: "install", ConversationID: "general"})
	if err != nil || name != "Policy Helper" || room != "#general" || person != "Alex Example" {
		t.Fatalf("names: %s %s %s %v", name, room, person, err)
	}
	owner.denied = true
	choices, err = source.AgentAnnouncementChoices(context.Background(), owner.actor)
	if err != nil || len(choices) != 0 {
		t.Fatal("revoked management authority remained a choice")
	}
}
