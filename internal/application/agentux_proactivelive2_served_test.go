package application

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentcontrols"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentaudit"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentauditstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentbudgetstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// This exercises the served factory, evidence, egress, typed OpenAI adapter,
// pricing and durable budget/audit owners. Only the HTTP wire is replaced.
func TestAgentUXProactiveLive_PostedMessage_Integration(t *testing.T) {
	proactiveLive2Served(t, "preview")
}
func TestAgentUXProactiveLive_RepairAccounting_Integration(t *testing.T) {
	for _, mode := range []string{"repair", "refused"} {
		t.Run(mode, func(t *testing.T) { proactiveLive2Served(t, mode) })
	}
}

func proactiveLive2Served(t *testing.T, mode string) {
	surface, worker, chatStore, _, now, draft := proactiveLiveFixture(t)
	*now = time.Now().UTC()
	r := worker.Runtime
	proactiveLive2Leases(t, r)
	if _, err := chatStore.SendPost(context.Background(), chat.SendPostRequest{TenantID: "tenant-a", ConversationID: "general", Body: "unleased fixture", Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "owner"}, IdempotencyKey: "unleased-write"}, chat.Post{TenantID: "tenant-a", ConversationID: "general", AuthorID: "owner", AuthorHomeTenantID: "tenant-a", Body: "unleased fixture"}); !errors.Is(err, chatstore.ErrNoRouteLease) {
		t.Fatalf("store did not enforce lease: %v", err)
	}
	ctx := context.Background()
	core := pgtest.New(t)
	tenant := r.Work.tenantUUID("tenant-a")
	core.Exec(t, `INSERT INTO tenant(tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES($1,'tenant-a','cell-local','Announcement test','ACTIVE','2026-01-01')`, tenant)
	conn := core.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	audit, err := agentauditstore.New(conn, r.Work.tenantUUID)
	if err != nil {
		t.Fatal(err)
	}
	budgetConn := core.NewConn(t)
	if _, err := budgetConn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	persistence, err := agentbudgetstore.New(budgetConn, r.Work.tenantUUID)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := agentbudget.NewWithPersistence(agentBudgetPolicy(), r.Now, persistence)
	if err != nil {
		t.Fatal(err)
	}

	stored := r.Work.routes.(agentDocEgressRoute)
	var route PersonaRunModelRoute
	if err = json.Unmarshal(stored.record.RoutePayload, &route); err != nil {
		t.Fatal(err)
	}
	deployment := modelDeploymentFixture(t)
	p := &deployment.Profiles[0]
	p.Regions = []string{"us-east"}
	p.DataClasses = []string{"PUBLIC", "INTERNAL"}
	p.TaskProfileIDs = []string{route.Route.Task.ID}
	p.SemanticsDigest = route.Route.Pin.SemanticsDigest
	p.ToolSchemaDigest = route.Route.Pin.ToolSchemaDigest
	p.Evaluation.AgentVersionDigest = route.Route.Pin.AgentVersionDigest
	p.MaxCostMicros, p.ExpectedCostMicros = 100, 10
	p.MaxLatency = time.Second
	p.ProfileDigest = agentmodel.ModelProfileDigest(*p)
	classes := []dlp.DataClass{dlp.ClassPublic, dlp.ClassInternal}
	term := &deployment.Terms[0]
	term.AllowedRegions, term.AllowedClasses = p.Regions, classes
	term.SourceRules = nil
	for _, source := range []string{"persona-profile", "persona-invoking-post", "persona-untrusted-reference-document", "persona-reference-document"} {
		term.SourceRules = append(term.SourceRules, agentegress.ProviderSourceRule{Class: source, Classes: classes})
	}
	deployment.Destinations[0].DataClasses = p.DataClasses
	deployment.Clearances[0].Classes = classes
	deployment.Credential.Scopes[0].Region = "us-east"
	deployment.Credential.Scopes[0].Purpose = route.Purpose
	deployment.Destinations[0].Purposes = []string{route.Purpose}
	deployment.BaseURL = "https://announcement-provider.invalid"
	rate := &deployment.Pricing.Entries[0]
	rate.InputMicrosPerToken, rate.OutputMicrosPerToken = 0, 0
	rate.InputMicrosPerMillionTokens, rate.OutputMicrosPerMillionTokens = 2000, 8000
	deployment.Pricing.Digest = agentmodel.PricingScheduleDigest(deployment.Pricing)
	pricingKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{3}, ed25519.SeedSize))
	deployment.Pricing.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(pricingKey, []byte(deployment.Pricing.Digest)))
	route.Route.Pin.Primary = agentmodel.ModelSelection{ProfileID: p.ID, ProfileDigest: p.ProfileDigest, Identity: p.Identity}
	route.Egress.ID = p.ID
	route.Route.Task.SemanticsDigest = route.Route.Pin.SemanticsDigest
	route.Route.Task.ToolSchemaDigest = route.Route.Pin.ToolSchemaDigest
	stored.record.RoutePayload, err = json.Marshal(route)
	if err != nil {
		t.Fatal(err)
	}
	r.Work.routes = stored

	transport := &proactiveLive2HTTP{proactiveLiveHTTP: proactiveLiveHTTP{t: t}, mode: mode}
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	owners := &personaRuntimeCurrentOwners{authority: r}
	factory, err := newPersonaServedModelFactory(personaServedModelFactoryInput{
		Config:     ServeConfig{CellID: deployment.Worker.Cell, PersonaOutputSigningSeed: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, ed25519.SeedSize)), PersonaWorkloadSigningSeed: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{8}, ed25519.SeedSize))},
		Deployment: deployment, APIKey: "local-fixture-only", Runtime: &agentRuntime{Budget: ledger, Audit: audit, Resources: agentResourceTestRuntime(t, nil)},
	})
	if err != nil {
		t.Fatal(err)
	}
	documentResolver, err := NewAgentDocumentResolver(r.Documents.(AgentAnnouncementReferenceResolver).Hub)
	if err != nil {
		t.Fatal(err)
	}
	composed, err := factory(PersonaRuntimeModelOwnerDependencies{AgentStore: r.Agents, Personas: r.Work.personas, Manifests: r.Work.manifests, Routes: r.Work.routes, Threads: personaRunModelThreadFake{}, Authority: owners, ChatClasses: owners, Documents: documentResolver, TenantUUID: r.Work.tenantUUID, Now: r.Now})
	if err != nil {
		t.Fatal(err)
	}
	model, err := NewAgentModelExecutorAdapter(composed.Gateway)
	if err != nil {
		t.Fatal(err)
	}
	r.Base.Model, r.Base.WorkerID, r.Base.LeaseTTL = model, composed.WorkerID, composed.LeaseTTL
	r.Work.leases, r.Work.workload = composed.Leases, composed.Workload
	r.Worker = composed.WorkerIdentity
	if err = bindAnnouncementModelBudget(r); err != nil {
		t.Fatal(err)
	}
	// As in composeAgentAnnouncements, the same authority pointer is retained
	// by evidence and receives only the announcement delegate.
	owners.authority = announcementAdmissionAuthority{mention: owners.authority, announcement: r}

	handler := agentcontrols.AnnouncementHandler{Surface: surface}
	call := func(path string, input any) agentcontrols.AnnouncementReply {
		t.Helper()
		body, _ := json.Marshal(input)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body)))
		if recorder.Code != http.StatusOK {
			t.Fatalf("handler %s: %d %s", path, recorder.Code, recorder.Body.String())
		}
		var reply agentcontrols.AnnouncementReply
		if err := json.Unmarshal(recorder.Body.Bytes(), &reply); err != nil {
			t.Fatal(err)
		}
		return reply
	}
	wantedCalls := 1
	if mode != "preview" {
		wantedCalls = 2
	}
	check := func(want int) {
		t.Helper()
		entries, err := audit.Query(ctx, agentaudit.Query{Viewer: agentaudit.Viewer{TenantID: "tenant-a", UserID: "owner", Role: agentaudit.ViewerAuditor, AllowedFields: map[string]bool{"model_route": true}}})
		if err != nil || len(entries) != wantedCalls {
			t.Fatalf("route records=%d want=%d: %v", len(entries), want, err)
		}
		for _, entry := range entries {
			if entry.Action != "model.route" || entry.Actor.UserID != "owner" || !strings.HasPrefix(entry.Actor.DelegationGrantID, "announcement:") || entry.Actor.TaskID == "" || entry.Actor.StepID == "" {
				t.Fatalf("sponsored route lost attribution: %+v", entry.Actor)
			}
		}
		tasks := ledger.Snapshot().Tasks
		if len(tasks) != want || transport.calls != wantedCalls || transport.counts != wantedCalls {
			t.Fatalf("calls=%d preflights=%d tasks=%+v", transport.calls, transport.counts, tasks)
		}
		for _, task := range tasks {
			if task.Limit.Steps != 2 || task.Limit.Tokens != 1100 || task.Limit.SpendMicros != 100 {
				t.Fatalf("repair expanded admitted cap: %+v", task.Limit)
			}
			if task.Used.Steps != int64(wantedCalls) || task.Used.Tokens != int64(2*wantedCalls) || task.Used.SpendMicros != int64(wantedCalls) || task.Reserved != (agentbudget.Usage{}) {
				t.Fatalf("settled usage: %+v", task)
			}
		}
		durable, err := persistence.Load(ctx, "tenant-a", r.Now())
		if err != nil || len(durable.Tasks) != want {
			t.Fatalf("durable cost: %+v %v", durable, err)
		}
		for _, task := range durable.Tasks {
			if task.Used.Steps != int64(wantedCalls) || task.Used.SpendMicros != int64(wantedCalls) || task.Used.Tokens != int64(2*wantedCalls) {
				t.Fatalf("durable settlement: %+v", task)
			}
		}
	}
	if mode == "refused" {
		body, _ := json.Marshal(draft)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, agentcontrols.AnnouncementPath+"/preview", bytes.NewReader(body)))
		if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), "corrected reply still broke this rule") || !strings.Contains(recorder.Body.String(), "plain text") || strings.Contains(recorder.Body.String(), "Source: private fixture") {
			t.Fatalf("refused reply has no plain rule: %d %s", recorder.Code, recorder.Body.String())
		}
		check(1)
		posts, err := chatStore.ListPosts(ctx, chat.Principal{TenantID: "tenant-a", SubjectID: "employee"}, "tenant-a", "general", 0, chat.Page{PageSize: 10}, chat.PostWindow{})
		if err != nil || len(posts.Posts) != 0 {
			t.Fatalf("refused reply posted %+v %v", posts, err)
		}
		return
	}
	preview := call(agentcontrols.AnnouncementPath+"/preview", draft)
	if err != nil || preview.Preview == nil || !preview.Preview.Public {
		t.Fatalf("served preview: %+v %v", preview, err)
	}
	check(1)
	draft.PreviewDigest = preview.Preview.Digest
	posted := call(agentcontrols.AnnouncementPath, draft)
	if err != nil || len(posted.Snapshot.Rows) != 1 || posted.Snapshot.Rows[0].ResultCode != agentstore.AnnouncementPosted {
		t.Fatalf("served post: %+v %v", posted, err)
	}
	check(1) // Post re-seals the preview without a second provider dispatch.
	proactiveLiveAssertPosts(t, ctx, chatStore, 1, false)
	if _, err = surface.CreateAnnouncement(ctx, draft); err != nil {
		t.Fatal(err)
	}
	check(1)
	proactiveLiveAssertPosts(t, ctx, chatStore, 1, false)
	if posted.Snapshot.Rows[0].OwnerName != "Alex Example" || posted.Snapshot.Rows[0].LastRunAt == "" || posted.Snapshot.Rows[0].MessageHref == "" {
		t.Fatalf("post outcome missing owner/time/link: %+v", posted.Snapshot.Rows[0])
	}
	posts, err := chatStore.ListPosts(ctx, chat.Principal{TenantID: "tenant-a", SubjectID: "employee"}, "tenant-a", "general", 0, chat.Page{PageSize: 10}, chat.PostWindow{})
	if err != nil || len(posts.Posts) != 1 {
		t.Fatalf("public member read: %+v %v", posts, err)
	}
	post := posts.Posts[0]
	announcement, ok := chatui.DecodeAnnouncementMessageBody(post.Body)
	if !ok || announcement.Text != preview.Preview.Text || announcement.AgentName == "Agent" || announcement.PostedAt.IsZero() || !announcement.PostedAt.Equal(r.Now()) {
		t.Fatalf("posted preview/name/time: %+v", announcement)
	}
	scoped, err := r.Work.personas.ForTenant(ctx, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	// Identity projection reads the stored icon rather than deriving it from a post.
	if _, err := scoped.(*agentpersonastore.TenantStore).BackfillIcons(ctx, "owner", r.Now()); err != nil {
		t.Fatal(err)
	}
	icon, err := scoped.(*agentpersonastore.TenantStore).GetIcon(ctx, "policy-helper")
	if err != nil {
		t.Fatal(err)
	}
	markup, err := ui.RenderToString(chatui.Build(chatui.Model{Locale: "en-US", State: chatui.StateReady, SelectedID: "general", Conversations: []chatui.Conversation{{ID: "general", Name: "general"}}, Messages: []chatui.Message{{ID: post.ID, AuthorID: post.AuthorID, Author: "Hcmnext Local Persona Assistant", Body: post.Body, SentAt: post.CreatedAt}}, ResolvedPersonaMentions: []chatui.ResolvedPersonaMention{{Reference: chatui.ChatReference{Kind: "AGENT_MENTION", ID: post.AuthorID, Display: announcement.AgentName}, Icon: icon.Value, IconRevision: icon.Revision}}}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"data-agent-announcement", "agent-badge", "agent-icon", announcement.AgentName, "Posted for Alex Example", "2026 holiday guide"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("served projection missing %s: %s", want, markup)
		}
	}
	for _, bad := range []string{"Hcmnext Local Persona Assistant", "hcm_agent_announcement", "Linked document", "0001-01-01"} {
		if strings.Contains(markup, bad) {
			t.Fatalf("raw projection contains %s: %s", bad, markup)
		}
	}
	definition, err := r.Store.Get(ctx, tenant, posted.Snapshot.Rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	occurrence := announcementManualOccurrence(definition.ID, draft.IdempotencyKey)
	id, err := agentrun.AdmissionRequestID(agentrun.SourceIdentity{TenantID: "tenant-a", Kind: agentrun.SourceAnnouncement, Key: occurrence, Ref: occurrence})
	if err != nil {
		t.Fatal(err)
	}
	repo, err := r.admissionRepository("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	admission, err := repo.GetByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := NewPersonaOpenAIModelEvidence(PersonaOpenAIModelOwnerConfig{DB: r.Agents, Personas: r.Work.personas, Manifests: r.Work.manifests, Routes: r.Work.routes, Threads: personaRunModelThreadFake{}, Authority: owners, Audit: audit, ChatClasses: owners, Documents: documentResolver, TenantUUID: r.Work.tenantUUID, Now: r.Now})
	if err != nil {
		t.Fatal(err)
	}
	bound := context.WithValue(ctx, announcementRuntimeKey{}, r)
	binding := openAIModelDispatchBinding{tenant: "tenant-a", runID: id, stepID: id + ":model:1"}
	bound = context.WithValue(bound, openAIModelDispatchContextKey{}, binding)
	loaded, err := evidence.admission(bound, binding)
	if err != nil || !reflect.DeepEqual(loaded, admission) {
		t.Fatalf("served evidence admission: %+v %v", loaded, err)
	}
	// The source owner is necessary; canonical recovery cannot masquerade as
	// an announcement. Neither tenant, trace nor provenance guards are waived.
	if _, err = evidence.admission(ctx, binding); !errors.Is(err, ErrAgentModelGatewayTenant) {
		t.Fatalf("missing source owner=%v", err)
	}
	if err = evidence.RecordRoute(bound, agentmodel.RouteRecord{TraceID: "other-step"}); !errors.Is(err, ErrAgentModelGatewayTenant) {
		t.Fatalf("forged step=%v", err)
	}
	foreign := binding
	foreign.tenant = "tenant-b"
	if _, err = evidence.admission(bound, foreign); !errors.Is(err, ErrAgentModelGatewayTenant) {
		t.Fatalf("foreign tenant=%v", err)
	}
	source := agentegress.SourceClassificationRequest{Tenant: "tenant-a", SourceClass: "persona-profile", Purpose: route.Purpose, FieldName: "model.message.0", DataClass: route.ProfileClass, ValueDigest: personaRunBytesDigest([]byte("answer questions")), Provenance: []string{"persona-run:other"}}
	if err = evidence.VerifySourceClassification(bound, source); !errors.Is(err, ErrAgentModelGatewayTenant) {
		t.Fatalf("unbound provenance=%v", err)
	}
	owners.authority = proactiveLiveDeniedAdmission{}
	if _, err = evidence.admission(bound, binding); !errors.Is(err, ErrAgentModelGatewayTenant) {
		t.Fatalf("revoked authority=%v", err)
	}
	check(1)

}

type proactiveLive2HTTP struct {
	proactiveLiveHTTP
	mode string
}

func (f *proactiveLive2HTTP) RoundTrip(request *http.Request) (*http.Response, error) {
	// The adapter transports the goal as reference data; inspect it before the
	// fixture consumes the body, including the second call's named repair rule.
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	request.Body = io.NopCloser(bytes.NewReader(raw))
	response, err := f.proactiveLiveHTTP.RoundTrip(request)
	if err != nil || strings.HasSuffix(request.URL.Path, "/input_tokens") {
		return response, err
	}
	if f.calls == 2 && f.mode != "preview" && !bytes.Contains(raw, []byte("previous answer broke this rule")) {
		f.t.Fatal("repair was not sent through served model evidence")
	}
	if f.mode == "refused" || f.mode == "repair" && f.calls == 1 {
		response.Body.Close()
		text := `{"text":"Source: private fixture","tool_proposals":[]}`
		encoded, _ := json.Marshal(map[string]any{"id": "fixture-completion", "model": "test-model", "status": "completed", "usage": map[string]int{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}, "output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": text}}}}})
		response.Body = io.NopCloser(bytes.NewReader(encoded))
	}
	return response, nil
}
