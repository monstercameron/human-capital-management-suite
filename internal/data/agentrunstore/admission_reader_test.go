package agentrunstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENT_015_AdmissionReaderReloadsAndScopesRecord(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatalf("migrate agent database: %v", err)
	}
	tenantA, tenantB := uuid.New(), uuid.New()
	for _, tenantID := range []uuid.UUID{tenantA, tenantB} {
		if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant (tenant_id) VALUES ($1)`, tenantID); err != nil {
			t.Fatalf("create tenant projection: %v", err)
		}
	}
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO persona_invocations
		(tenant_id,invocation_id,conversation_id,thread_id,post_id,invoker_id,persona_id,persona_version,installation_id,mode,skills,actor,owner_id,state)
		VALUES ($1,'invocation-1','conversation-8','thread-9','event-87','user-7','persona-3','4','install-1','ON_BEHALF_OF','{}','{}','user-7','CLAIMED')`, tenantA); err != nil {
		t.Fatalf("seed persona invocation identity: %v", err)
	}
	dsn, err := agentApplicationDSN(db.URL, db.Schema)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := agentstore.New(ctx, agentstore.Config{DSN: dsn, CoreDSN: "postgres://core:pw@core.invalid:5432/core", MaxConns: 4})
	if err != nil {
		t.Fatalf("open tenant-scoped agent store: %v", err)
	}
	t.Cleanup(runner.Close)
	repository, err := NewAdmissionRepository(runner, tenantA, values.TenantId("run-one"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	request := requestForAdmission(now)
	request.Source.TenantID = "run-one"
	request.Source.Kind = agentrun.SourcePersonaMention
	request.Source.Key = "invocation-1"
	service, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{
		Authority: repoAuthority{snapshot: repoSnapshot(request)}, Store: repository, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	record, created, err := service.Admit(ctx, request)
	if err != nil || !created {
		t.Fatalf("admit = (%+v, %t, %v)", record, created, err)
	}
	got, err := repository.GetByID(ctx, record.ID)
	if err != nil || got.ID != record.ID || got.Request.Source.TenantID != "run-one" || got.Request.Source.Key != record.Request.Source.Key || got.RequestDigest != record.RequestDigest {
		t.Fatalf("reloaded admission = (%+v, %v), want original durable identity", got, err)
	}
	otherTenant, err := NewAdmissionRepository(runner, tenantB, values.TenantId("run-two"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := otherTenant.GetByID(ctx, record.ID); !errors.Is(err, ErrAdmissionNotFound) {
		t.Fatalf("cross-tenant read = %v, want scoped not-found", err)
	}
	for _, kind := range []agentrun.SourceKind{agentrun.SourceWorkflow, agentrun.SourceSchedule, agentrun.SourceAPI, agentrun.SourceEvent, agentrun.SourceChat} {
		request := requestForAdmission(now)
		request.Source.TenantID, request.Source.Kind, request.Source.Ref = "run-one", kind, "occurrence-"+string(kind)
		service, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Authority: repoAuthority{snapshot: repoSnapshot(request)}, Store: repository, SourceConverter: agentrun.CanonicalSourceConverter{}, Now: func() time.Time { return now }})
		if err != nil {
			t.Fatal(err)
		}
		record, created, err := service.AdmitFromSource(ctx, request)
		if err != nil || !created {
			t.Fatalf("admit %s: %+v, %v", kind, record, err)
		}
		loaded, err := repository.GetByID(ctx, record.ID)
		if err != nil || loaded.Request.Source.Key != record.Request.Source.Key || loaded.RequestDigest != record.RequestDigest {
			t.Fatalf("reload canonical %s: %+v, %v", kind, loaded, err)
		}
		listed, err := repository.ListBySource(ctx, kind, 1)
		if err != nil || len(listed) != 1 || listed[0].ID != record.ID {
			t.Fatalf("list exact source %s: %+v, %v", kind, listed, err)
		}
		listed, err = otherTenant.ListBySource(ctx, kind, 1)
		if err != nil || len(listed) != 0 {
			t.Fatalf("list other tenant %s: %+v, %v", kind, listed, err)
		}
		pending, err := repository.ListPendingBySource(ctx, kind, 1)
		if err != nil || len(pending) != 1 || pending[0].ID != record.ID {
			t.Fatalf("pending %s=%+v error=%v", kind, pending, err)
		}
		runnable, err := repository.ListRunnableBySource(ctx, kind, 1, now)
		if err != nil || len(runnable) != 1 || runnable[0].ID != record.ID {
			t.Fatalf("runnable %s=%+v error=%v", kind, runnable, err)
		}
		after, err := repository.ListRunnableBySourceAfter(ctx, kind, 1, now, record.AdmittedAt, record.ID)
		if err != nil || len(after) != 0 {
			t.Fatalf("cursor repeated seen record %s=%+v error=%v", kind, after, err)
		}
		runnable, err = repository.ListRunnableBySource(ctx, kind, 1, record.Request.Deadline)
		if err != nil || len(runnable) != 0 {
			t.Fatalf("elapsed runnable %s=%+v error=%v", kind, runnable, err)
		}
		if _, err := db.SQL.ExecContext(ctx, `INSERT INTO agent_run_execution(tenant_id,run_id,admission_id,request_digest,agent_id,agent_version,agent_digest,context_digest,deadline,state,revision,fence,created_at,updated_at) SELECT tenant_id,'settled-'||request_id,request_id,request_digest,agent_id,agent_version,agent_digest,'sha256:'||repeat('b',64),deadline,'COMPLETED',1,0,admitted_at,admitted_at FROM agent_run_request WHERE tenant_id=$1 AND request_id=$2`, tenantA, record.ID); err != nil {
			t.Fatal(err)
		}
		pending, err = repository.ListPendingBySource(ctx, kind, 1)
		if err != nil || len(pending) != 0 {
			t.Fatalf("settled execution entered recovery %s=%+v error=%v", kind, pending, err)
		}
		if _, err := db.SQL.ExecContext(ctx, `UPDATE agent_run_execution SET state='RECONCILING' WHERE tenant_id=$1 AND admission_id=$2`, tenantA, record.ID); err != nil {
			t.Fatal(err)
		}
		runnable, err = repository.ListRunnableBySource(ctx, kind, 1, now)
		if err != nil || len(runnable) != 0 {
			t.Fatalf("reconciliation entered runnable %s=%+v error=%v", kind, runnable, err)
		}
		if _, err := db.SQL.ExecContext(ctx, `UPDATE agent_run_execution SET state='RUNNING',lease_owner='worker',lease_until=$3 WHERE tenant_id=$1 AND admission_id=$2`, tenantA, record.ID, now.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		runnable, err = repository.ListRunnableBySource(ctx, kind, 1, now)
		if err != nil || len(runnable) != 0 {
			t.Fatalf("live lease entered runnable %s=%+v error=%v", kind, runnable, err)
		}
		runnable, err = repository.ListRunnableBySource(ctx, kind, 1, now.Add(time.Second))
		if err != nil || len(runnable) != 1 {
			t.Fatalf("elapsed lease not recovered %s=%+v error=%v", kind, runnable, err)
		}
	}
	native := requestForAdmission(now)
	native.Source.TenantID, native.Source.Kind, native.Source.Ref, native.Source.Key = "run-one", agentrun.SourceSchedule, "native-occurrence", "schedule-owned-native-key"
	nativeService, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Authority: repoAuthority{snapshot: repoSnapshot(native)}, Store: repository, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	nativeRecord, _, err := nativeService.Admit(ctx, native)
	if err != nil {
		t.Fatal(err)
	}
	resolver := admissionNativeSourceResolver{key: native.Source.Key}
	nativeRepository, err := NewAdmissionRepositoryWithSourceResolver(runner, tenantA, "run-one", resolver)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := nativeRepository.GetByID(ctx, nativeRecord.ID)
	if err != nil || loaded.Request.Source.Key != native.Source.Key {
		t.Fatalf("native source recovery = %+v, %v", loaded, err)
	}
	wrongResolver, err := NewAdmissionRepositoryWithSourceResolver(runner, tenantA, "run-one", admissionNativeSourceResolver{key: "forged-key"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrongResolver.GetByID(ctx, nativeRecord.ID); !errors.Is(err, agentrun.ErrSourceConflict) {
		t.Fatalf("native source resolver bypassed pinned digest: %v", err)
	}
}

type admissionNativeSourceResolver struct{ key string }

func (r admissionNativeSourceResolver) ResolveSourceKey(context.Context, agentrun.Request) (string, error) {
	return r.key, nil
}
