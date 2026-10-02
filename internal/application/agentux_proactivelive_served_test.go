package application

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

type proactiveLiveHTTP struct {
	t             *testing.T
	calls, counts int
}

func (f *proactiveLiveHTTP) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host != "announcement-provider.invalid" {
		return nil, fmt.Errorf("unexpected provider host")
	}
	var body map[string]any
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		return nil, err
	}
	if body["model"] != "test-model" || (req.URL.Path == "/responses" && body["store"] != false) {
		f.t.Fatalf("provider model/store invalid on %s", req.URL.Path)
	}
	text := `{"object":"response.input_tokens","input_tokens":1}`
	if strings.HasSuffix(req.URL.Path, "/input_tokens") {
		f.counts++
	} else {
		f.calls++
		text = `{"id":"fixture-completion","model":"test-model","status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2},"output":[{"type":"message","content":[{"type":"output_text","text":"{\"text\":\"The next company holidays are Thanksgiving on November 26 and the day after Thanksgiving on November 27.\",\"tool_proposals\":[]}"}]}]}`
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(text)), Request: req}, nil
}

// This exercises the served factory, evidence, egress, typed OpenAI adapter,
// pricing and durable budget/audit owners. Only the HTTP wire is replaced.
func TestAgentUXProactiveLive_PreviewAndPost_Integration(t *testing.T) {
	surface, worker, chatStore, _, now, draft := proactiveLiveFixture(t)
	*now = time.Now().UTC()
	r := worker.Runtime
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

	transport := &proactiveLiveHTTP{t: t}
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

	preview, err := surface.PreviewAnnouncement(ctx, draft)
	if err != nil || preview.Preview == nil || !preview.Preview.Public {
		t.Fatalf("served preview: %+v %v", preview, err)
	}
	check := func(want int) {
		t.Helper()
		entries, err := audit.Query(ctx, agentaudit.Query{Viewer: agentaudit.Viewer{TenantID: "tenant-a", UserID: "owner", Role: agentaudit.ViewerAuditor, AllowedFields: map[string]bool{"model_route": true}}})
		if err != nil || len(entries) != want {
			t.Fatalf("route records=%d want=%d: %v", len(entries), want, err)
		}
		for _, entry := range entries {
			if entry.Action != "model.route" || entry.Actor.UserID != "owner" || !strings.HasPrefix(entry.Actor.DelegationGrantID, "announcement:") || entry.Actor.TaskID == "" || entry.Actor.StepID == "" {
				t.Fatalf("sponsored route lost attribution: %+v", entry.Actor)
			}
		}
		tasks := ledger.Snapshot().Tasks
		if len(tasks) != want || transport.calls != want || transport.counts != want {
			t.Fatalf("calls=%d preflights=%d tasks=%+v", transport.calls, transport.counts, tasks)
		}
		for _, task := range tasks {
			if task.Used.Steps != 1 || task.Used.Tokens != 2 || task.Used.SpendMicros != 1 || task.Reserved != (agentbudget.Usage{}) {
				t.Fatalf("settled usage: %+v", task)
			}
		}
		durable, err := persistence.Load(ctx, "tenant-a", r.Now())
		if err != nil || len(durable.Tasks) != want {
			t.Fatalf("durable cost: %+v %v", durable, err)
		}
		for _, task := range durable.Tasks {
			if task.Used.Steps != 1 || task.Used.SpendMicros != 1 || task.Used.Tokens != 2 {
				t.Fatalf("durable settlement: %+v", task)
			}
		}
	}
	check(1)
	draft.PreviewDigest = preview.Preview.Digest
	posted, err := surface.CreateAnnouncement(ctx, draft)
	if err != nil || len(posted.Snapshot.Rows) != 1 || posted.Snapshot.Rows[0].ResultCode != agentstore.AnnouncementPosted {
		t.Fatalf("served post: %+v %v", posted, err)
	}
	check(1) // posting sends the previewed text; it admits no second model call
	proactiveLiveAssertPosts(t, ctx, chatStore, 1, false)
	if _, err = surface.CreateAnnouncement(ctx, draft); err != nil {
		t.Fatal(err)
	}
	check(1)
	proactiveLiveAssertPosts(t, ctx, chatStore, 1, false)
	if posted.Snapshot.Rows[0].OwnerName != "Alex Example" || posted.Snapshot.Rows[0].LastRunAt == "" || posted.Snapshot.Rows[0].MessageHref == "" {
		t.Fatalf("post outcome missing owner/time/link: %+v", posted.Snapshot.Rows[0])
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

type proactiveLiveDeniedAdmission struct{}

func (proactiveLiveDeniedAdmission) VerifyAdmission(context.Context, agentrun.Request) (agentrun.AuthoritySnapshot, error) {
	return agentrun.AuthoritySnapshot{}, agentrun.ErrAuthorityRefusal
}
