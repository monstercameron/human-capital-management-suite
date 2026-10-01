package application

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/agentqualification"
)

// TestAgentQualificationDurableMixedLoad measures real PostgreSQL-backed model
// tasks and human chat in two seeded tenants with separate chat/core stores.
// The deterministic provider and absent schedule/workflow metrics must remain
// explicit UNKNOWN evidence; this fixture cannot close AGENT-047 or AGENT-048.
func TestAgentQualificationDurableMixedLoad(t *testing.T) {
	f := newAgentFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pack := demoworkforce.HarborCarePack
	core, err := pgstore.New(f.pool, pgstore.WithCellID("cell-agent-test"))
	if err != nil {
		t.Fatal(err)
	}
	if err := core.Bootstrap(ctx, pack.Key); err != nil {
		t.Fatal(err)
	}
	if _, _, err := bootstrapLocalDevWorkforce(ctx, f.pool, pack.Key); err != nil {
		t.Fatal(err)
	}
	if err := f.cell.RoleAccess.Bootstrap(ctx, values.TenantId(pack.Key), "system:bootstrap"); err != nil {
		t.Fatal(err)
	}
	if _, err := bootstrapLocalDevRoleAssignments(ctx, f.pool, pack.Key); err != nil {
		t.Fatal(err)
	}
	workers, err := pack.Plan(pgstore.TenantID(pack.Key))
	if err != nil || len(workers) < 51 {
		t.Fatalf("second workforce=%d err=%v", len(workers), err)
	}
	secondWorker := workers[50].Row.WorkerKey
	for _, tenant := range []string{f.tenant, pack.Key} {
		if err := f.cell.AgentSettings.SetAgentsEnabled(ctx, values.TenantId(tenant), true, "system:qualification"); err != nil {
			t.Fatal(err)
		}
	}
	principals := map[string]*trust.Principal{f.tenant: agentTestPrincipal(t, f.tenant, agentTestWorker)}
	now := time.Now().UTC()
	principals[pack.Key], err = trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(pack.Key), Subject: secondWorker, SubjectKind: trust.SubjectKindHuman, OrganizationScopeID: pack.OrgScope(), Roles: []string{"worker_self"}, Purposes: []string{"self_service_view"}, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "qualification-second-worker", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "sha256:qualification-second-worker"})
	if err != nil {
		t.Fatal(err)
	}
	var admissionMu sync.Mutex
	admissions := make(map[string][]agentqualification.ResourceAdmission)
	resourceRuntime, err := NewAgentResourceRuntime(AgentResourceRuntimeConfig{
		Policy: DefaultAgentResourcePolicy("cell-agent-test"),
		ObserveAdmission: func(observation AgentResourceObservation) {
			admissionMu.Lock()
			defer admissionMu.Unlock()
			identity := observation.Identity
			admissions[identity.TaskID] = append(admissions[identity.TaskID], agentqualification.ResourceAdmission{
				Tenant: identity.TenantID, User: identity.UserID, Task: identity.TaskID, Lane: string(identity.Lane),
				Provider: observation.ProviderID, Outcome: observation.Outcome, PoolWait: observation.PoolWait,
			})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	f.runtime, err = composeAgentRuntime(ctx, agentRuntimeInput{Pool: f.pool, Cell: f.cell, Config: ServeConfig{Profile: ServeProfileLocalDev, CellID: "cell-agent-test"}, Logger: f.logger, Env: func(string) string { return "" }, Now: func() time.Time { return time.Now().UTC() }, Tenants: []string{f.tenant, pack.Key}, Resources: resourceRuntime})
	if err != nil || f.runtime == nil {
		t.Fatalf("compose observed runtime: %v", err)
	}

	chatStore := streamIntegrationStore(t)
	chatService := chat.NewService(chatStore, time.Now)
	chatService.SetAuthority(streamIntegrationAuthority{store: chatStore})
	for tenant, principal := range principals {
		_, err := chatStore.CreateConversation(ctx, chat.Conversation{ID: "qualification-" + tenant, TenantID: tenant, Kind: chat.PublicChannel, Name: "Qualification", OwnerID: principal.Subject(), Revision: 1}, []chat.Membership{{TenantID: tenant, HomeTenantID: tenant, ConversationID: "qualification-" + tenant, SubjectID: principal.Subject(), Role: chat.Manager, HistoryVisibility: chat.FullHistory}}, "qualification-create-"+tenant)
		if err != nil {
			t.Fatal(err)
		}
	}
	output := os.Getenv("HCMNEXT_AGENT_QUALIFICATION_OUTPUT")
	authorPublic, authorKey := agentQualificationKey(t, output, "workload-author")
	reviewerPublic, reviewerKey := agentQualificationKey(t, output, "measurement-reviewer")
	plan := agentqualification.Plan{ID: "durable-local-mixed-v1", Profile: "pooled", Runtime: "composeAgentRuntime+chat.Service/PostgreSQL;deterministic-model", Issuer: "local-workload-author", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(5 * time.Minute), Concurrency: 4, P99Limit: 30 * time.Second}
	for _, tenant := range []string{f.tenant, pack.Key} {
		count := 2
		if tenant == f.tenant {
			count = 6
		}
		for index := range count {
			for _, kind := range []string{"model", "chat"} {
				plan.Jobs = append(plan.Jobs, agentqualification.Job{ID: fmt.Sprintf("%s-%s-%d", tenant, kind, index), Tenant: tenant, Kind: kind})
			}
		}
	}
	signedPlan, err := agentqualification.SignPlan(plan, authorKey)
	if err != nil {
		t.Fatal(err)
	}
	operations := map[string]agentqualification.Operation{
		"model": func(ctx context.Context, job agentqualification.Job) (agentqualification.Observation, error) {
			started, err := f.runtime.Starter.StartTask(ctx, principals[job.Tenant], "Summarize my own job details for capacity qualification")
			if err != nil {
				return agentqualification.Observation{}, err
			}
			runner, err := f.runtime.Platform.ForTenant(ctx, values.TenantId(job.Tenant))
			if err != nil {
				return agentqualification.Observation{}, err
			}
			task, err := runner.Runtime.GetTask(ctx, started.ID)
			if err != nil {
				return agentqualification.Observation{}, err
			}
			if task.State != agentrun.StateCompleted || task.TenantID != job.Tenant || task.UserID != principals[job.Tenant].Subject() || len(task.Ledger.Entries) == 0 {
				return agentqualification.Observation{}, fmt.Errorf("task outcome incomplete: %s", task.State)
			}
			used, _, ok := runner.BudgetUsage(task.ID)
			if !ok {
				return agentqualification.Observation{}, fmt.Errorf("task cost observation absent")
			}
			admissionMu.Lock()
			observed := append([]agentqualification.ResourceAdmission(nil), admissions[task.ID]...)
			admissionMu.Unlock()
			if len(observed) == 0 {
				return agentqualification.Observation{}, fmt.Errorf("served task resource admissions unobserved")
			}
			var poolWait time.Duration
			for _, admission := range observed {
				if admission.Tenant != task.TenantID || admission.User != task.UserID || admission.Task != task.ID || admission.Outcome != "ADMITTED" {
					return agentqualification.Observation{}, fmt.Errorf("resource admission task binding mismatch")
				}
				poolWait += admission.PoolWait
			}
			return agentqualification.Observation{DurableRef: "agent_task:" + task.ID, Provider: "fake", Model: string(f.runtime.Model.Kind), CostMicros: &used.SpendMicros, PoolWait: &poolWait, Resources: observed}, nil
		},
		"chat": func(ctx context.Context, job agentqualification.Job) (agentqualification.Observation, error) {
			p := principals[job.Tenant]
			principal := chat.Principal{TenantID: job.Tenant, SubjectID: p.Subject()}
			request := chat.SendPostRequest{Principal: principal, TenantID: job.Tenant, ConversationID: "qualification-" + job.Tenant, IdempotencyKey: job.ID, Body: "Human capacity qualification " + job.ID}
			post, err := chatService.SendPost(ctx, request)
			if err != nil {
				return agentqualification.Observation{}, err
			}
			stored, err := chatStore.GetPost(ctx, job.Tenant, request.ConversationID, post.ID)
			if err != nil || stored.Body != request.Body || stored.Sequence == 0 {
				return agentqualification.Observation{}, fmt.Errorf("chat durable readback: %w", err)
			}
			retry, err := chatService.SendPost(ctx, request)
			if err != nil || retry.ID != post.ID || retry.Sequence != post.Sequence {
				return agentqualification.Observation{}, fmt.Errorf("chat retry changed outcome: %w", err)
			}
			return agentqualification.Observation{DurableRef: "chat_post:" + post.ID}, nil
		},
	}
	authorKeys := map[string]ed25519.PublicKey{"local-workload-author": authorPublic}
	report, err := agentqualification.Run(ctx, signedPlan, authorKeys, operations)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "UNKNOWN" || len(report.Samples) != 16 {
		t.Fatalf("partial fixture promoted or samples lost: %+v", report)
	}
	for _, sample := range report.Samples {
		if sample.Failure != "" || sample.Observation.DurableRef == "" {
			t.Fatalf("durable operation failed: %+v", sample)
		}
	}
	if len(report.Summaries) != 4 {
		t.Fatalf("tenant/workload summaries=%+v", report.Summaries)
	}
	for tenant := range principals {
		if snapshot := resourceRuntime.SnapshotForTenant(tenant); snapshot.Active != 0 || snapshot.Queued != 0 {
			t.Fatalf("worker resource leak for %s: %+v", tenant, snapshot)
		}
		if snapshot := resourceRuntime.ProviderSnapshotForTenant(tenant); snapshot.Active != 0 || snapshot.Queued != 0 {
			t.Fatalf("provider resource leak for %s: %+v", tenant, snapshot)
		}
	}
	signedReport, err := agentqualification.SignReport(report, "local-measurement-reviewer", reviewerKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := agentqualification.VerifyReport(signedReport, authorKeys, map[string]ed25519.PublicKey{"local-measurement-reviewer": reviewerPublic}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if output != "" {
		encoded, err := json.MarshalIndent(signedReport, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(output, "durable-mixed-report.json"), append(encoded, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, summary := range report.Summaries {
		t.Logf("%s/%s: n=%d p50=%s p95=%s p99=%s failures=%d", summary.Tenant, summary.Kind, summary.Count, summary.P50, summary.P95, summary.P99, summary.Failed)
	}
	t.Logf("qualification=%s findings=%v", report.Status, report.Findings)
}

func agentQualificationKey(t *testing.T, output, name string) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	if output != "" {
		if err := os.MkdirAll(filepath.Join(output, "keys"), 0700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(output, "keys", name+".key")
		if raw, err := os.ReadFile(path); err == nil {
			if len(raw) != ed25519.PrivateKeySize {
				t.Fatal("invalid persisted qualification key")
			}
			key := ed25519.PrivateKey(raw)
			return key.Public().(ed25519.PublicKey), key
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if output != "" {
		if err := os.WriteFile(filepath.Join(output, "keys", name+".key"), private, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(output, "keys", name+".pub"), public, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return public, private
}
