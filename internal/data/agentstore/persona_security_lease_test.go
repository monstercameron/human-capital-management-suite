package agentstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestPersonaSecurityLeaseFenceSerializesRevocationAndRecovers(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := Migrate(ctx, db.SQL); err != nil {
		t.Fatalf("migrate agent store: %v", err)
	}
	tenantID := uuid.New()
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant(tenant_id) VALUES ($1)`, tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	login1, password1 := roleName("persona_fence_a"), uuid.NewString()
	login2, password2 := roleName("persona_fence_b"), uuid.NewString()
	if err := createAgentLogin(ctx, db.SQL, login1, password1); err != nil {
		t.Fatalf("create first login: %v", err)
	}
	if err := createAgentLogin(ctx, db.SQL, login2, password2); err != nil {
		t.Fatalf("create second login: %v", err)
	}
	t.Cleanup(func() {
		for _, login := range []string{login1, login2} {
			if _, err := db.SQL.ExecContext(context.Background(), "DROP ROLE "+login); err != nil {
				t.Errorf("drop login %s: %v", login, err)
			}
		}
	})
	cfg := func(login, password string) Config {
		return Config{DSN: testDSN(t, db.URL, db.Schema, login, password, "postgres"),
			CoreDSN: "postgres://core:pw@127.0.0.1:5433/core", MaxConns: 2}
	}
	first, err := New(ctx, cfg(login1, password1))
	if err != nil {
		t.Fatalf("open first agent pool: %v", err)
	}
	t.Cleanup(first.Close)
	second, err := New(ctx, cfg(login2, password2))
	if err != nil {
		t.Fatalf("open second agent pool: %v", err)
	}
	t.Cleanup(second.Close)
	seedPersonaSecurityRun(t, db, tenantID)
	at := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	lease, err := first.IssuePersonaSecurityLease(ctx, PersonaSecurityLeaseRequest{
		TenantID: tenantID, AdmissionID: "admission-persona-1", IssuedAt: at, ExpiresAt: at.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("issue lease from accepted durable admission: %v", err)
	}
	if lease.AuthorityRef != "grant-persona-1" || lease.PolicyDigest != "sha256:"+strings.Repeat("a", 64) || lease.IssuerID != AppRole {
		t.Fatalf("lease authority binding = %+v", lease)
	}
	if active, err := first.ResolveActivePersonaSecurityLease(ctx, tenantID, lease.AdmissionID, at.Add(time.Second)); err != nil || active.LeaseID != lease.LeaseID {
		t.Fatalf("resolve active durable lease = %+v, %v", active, err)
	}

	started, release := make(chan struct{}), make(chan struct{})
	stepDone := make(chan error, 1)
	go func() {
		stepDone <- first.RunPersonaSecurityStep(ctx, tenantID, lease.LeaseID, "step-1", at.Add(time.Second), func(context.Context) error {
			close(started)
			<-release
			return nil
		})
	}()
	select {
	case <-started:
	case err := <-stepDone:
		t.Fatalf("step returned before callback entered: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("step callback did not start")
	}
	revokeStarted := make(chan struct{})
	revokeDone := make(chan error, 1)
	go func() {
		close(revokeStarted)
		_, revokeErr := second.RevokePersonaSecurityScope(ctx, tenantID, PersonaSecurityScope{Kind: "PERSONA", Key: "persona-1"}, "administrator suspension", at.Add(2*time.Second))
		revokeDone <- revokeErr
	}()
	<-revokeStarted
	assertPersonaRevocationWaiting(t, db, tenantID, "persona-1")
	close(release)
	if err := <-stepDone; err != nil {
		t.Fatalf("step before revocation: %v", err)
	}
	if err := <-revokeDone; err != nil {
		t.Fatalf("cross-process revocation: %v", err)
	}
	if _, err := second.ResolveActivePersonaSecurityLease(ctx, tenantID, lease.AdmissionID, at.Add(3*time.Second)); !errors.Is(err, ErrPersonaSecurityLeaseRevoked) {
		t.Fatalf("resolve revoked durable lease = %v, want revoked", err)
	}
	postRevocationStarted := false
	err = second.RunPersonaSecurityStep(ctx, tenantID, lease.LeaseID, "step-2", at.Add(3*time.Second), func(context.Context) error {
		postRevocationStarted = true
		return nil
	})
	if !errors.Is(err, ErrPersonaSecurityLeaseRevoked) || postRevocationStarted {
		t.Fatalf("post-revoke step ran=%t err=%v, want fenced lease", postRevocationStarted, err)
	}

	// A new store object has no process-local lease state; it recovers the same
	// durable lease and sees the committed revocation before any callback.
	first.Close()
	recoveredStore, err := New(ctx, cfg(login1, password1))
	if err != nil {
		t.Fatalf("reopen agent pool after restart: %v", err)
	}
	t.Cleanup(recoveredStore.Close)
	recovered, err := recoveredStore.LoadPersonaSecurityLease(ctx, tenantID, lease.LeaseID)
	if err != nil || recovered.AdmissionID != lease.AdmissionID || recovered.AuthorityRef != lease.AuthorityRef {
		t.Fatalf("recovered lease = %+v, %v", recovered, err)
	}
	if _, err := recoveredStore.RecoverPersonaSecuritySteps(ctx, tenantID, lease.LeaseID, at.Add(4*time.Second)); err != nil {
		t.Fatalf("recover lease steps after restart: %v", err)
	}
}

func TestPersonaSecurityLeaseRecoveryMarksOrphanedStepAndAllowsNext(t *testing.T) {
	store, tenantID, lease, at := personaSecurityFixture(t)
	err := store.RunTenantTx(context.Background(), tenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO persona_security_step(tenant_id,lease_id,step_id,state,started_at)
			VALUES ($1,$2,'orphaned','STARTED',$3)`, tenantID, lease.LeaseID, at)
		return err
	})
	if err != nil {
		t.Fatalf("seed interrupted step before simulated restart: %v", err)
	}
	if _, err := store.ResolveActivePersonaSecurityLease(context.Background(), tenantID, lease.AdmissionID, at); !errors.Is(err, ErrPersonaSecurityRecovery) {
		t.Fatalf("resolve lease with orphaned step = %v, want recovery required", err)
	}
	if err := store.RunPersonaSecurityStep(context.Background(), tenantID, lease.LeaseID, "next", at.Add(time.Second), func(context.Context) error { return nil }); !errors.Is(err, ErrPersonaSecurityRecovery) {
		t.Fatalf("step with orphaned in-progress record = %v, want recovery required", err)
	}
	if count, err := store.RecoverPersonaSecuritySteps(context.Background(), tenantID, lease.LeaseID, at.Add(2*time.Second)); err != nil || count != 1 {
		t.Fatalf("recovery count = %d, %v; want 1", count, err)
	}
	if _, err := store.ResolveActivePersonaSecurityLease(context.Background(), tenantID, lease.AdmissionID, at.Add(2*time.Second)); err != nil {
		t.Fatalf("resolve lease after explicit recovery: %v", err)
	}
	if err := store.RunPersonaSecurityStep(context.Background(), tenantID, lease.LeaseID, "next", at.Add(3*time.Second), func(context.Context) error { return nil }); err != nil {
		t.Fatalf("step after explicit recovery: %v", err)
	}
}

func TestPersonaVersionReactivationKeepsOldLeaseRevoked(t *testing.T) {
	store, tenantID, lease, at := personaSecurityFixture(t)
	ctx := context.Background()
	scope := PersonaSecurityScope{Kind: "VERSION", Key: lease.PersonaID + ":" + lease.PersonaVersion}
	revokedEpoch, err := store.RevokePersonaSecurityScope(ctx, tenantID, scope, "suspended for independent review", at.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	activeEpoch, err := store.ReactivatePersonaSecurityScope(ctx, tenantID, scope, "republished with independent evidence", at.Add(2*time.Second))
	if err != nil || activeEpoch != revokedEpoch+1 {
		t.Fatalf("reviewed version reactivation epoch=%d, err=%v", activeEpoch, err)
	}
	if epoch, err := store.ReactivatePersonaSecurityScope(ctx, tenantID, scope, "idempotent publication retry", at.Add(3*time.Second)); err != nil || epoch != activeEpoch {
		t.Fatalf("reactivation retry invalidated fresh leases: epoch=%d err=%v", epoch, err)
	}
	if _, err := store.ResolveActivePersonaSecurityLease(ctx, tenantID, lease.AdmissionID, at.Add(4*time.Second)); !errors.Is(err, ErrPersonaSecurityLeaseRevoked) {
		t.Fatalf("old pre-suspension lease became usable after republication: %v", err)
	}
	if _, err := store.ReactivatePersonaSecurityScope(ctx, tenantID, PersonaSecurityScope{Kind: "VERSION", Key: "missing:99"}, "unpublished version", at); !errors.Is(err, ErrPersonaSecurityLeaseDenied) {
		t.Fatalf("unpublished version was reactivated: %v", err)
	}
	if _, err := store.ReactivatePersonaSecurityScope(ctx, tenantID, PersonaSecurityScope{Kind: "PRINCIPAL", Key: lease.PrincipalID}, "privilege recovery", at); !errors.Is(err, ErrPersonaSecurityLeaseInvalid) {
		t.Fatalf("principal scope could be reactivated: %v", err)
	}
	if err := store.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM persona_security_scope_event WHERE tenant_id=$1 AND scope_kind='VERSION' AND scope_key=$2`, tenantID, scope.Key).Scan(&count); err != nil {
			return err
		}
		if count != 2 {
			return fmt.Errorf("revocation/reactivation audit events=%d, want2", count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func personaSecurityFixture(t *testing.T) (*Store, uuid.UUID, PersonaSecurityLease, time.Time) {
	t.Helper()
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := Migrate(ctx, db.SQL); err != nil {
		t.Fatalf("migrate agent store: %v", err)
	}
	tenantID := uuid.New()
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant(tenant_id) VALUES ($1)`, tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	login, password := roleName("persona_recovery"), uuid.NewString()
	if err := createAgentLogin(ctx, db.SQL, login, password); err != nil {
		t.Fatalf("create agent login: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.SQL.ExecContext(context.Background(), "DROP ROLE "+login); err != nil {
			t.Errorf("drop agent login: %v", err)
		}
	})
	store, err := New(ctx, Config{DSN: testDSN(t, db.URL, db.Schema, login, password, "postgres"),
		CoreDSN: "postgres://core:pw@127.0.0.1:5433/core", MaxConns: 2})
	if err != nil {
		t.Fatalf("open agent pool: %v", err)
	}
	t.Cleanup(store.Close)
	seedPersonaSecurityRun(t, db, tenantID)
	at := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	lease, err := store.IssuePersonaSecurityLease(ctx, PersonaSecurityLeaseRequest{TenantID: tenantID, AdmissionID: "admission-persona-1", IssuedAt: at, ExpiresAt: at.Add(time.Minute)})
	if err != nil {
		t.Fatalf("issue persona lease: %v", err)
	}
	return store, tenantID, lease, at
}

func seedPersonaSecurityRun(t *testing.T, db *pgtest.DB, tenantID uuid.UUID) {
	seedPersonaSecurityRunWithState(t, db, tenantID, "STARTED")
}

func seedPersonaSecurityRunWithState(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, invocationState string) {
	t.Helper()
	ctx := context.Background()
	const policyDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const agentDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const contextDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	request := `{"source":{"kind":"PERSONA_MENTION","ref":"post-1"},"persona":{"id":"persona-1","version":"1"},"agent":{"id":"agent-1","version":"agent-v1","digest":"` + agentDigest + `"},"installation_id":"installation-1","principal_chain":{"mode":"ON_BEHALF_OF","invoker_id":"invoker-1"},"purpose":"test persona","audience":{"audience_id":"conversation-1"},"context_scope":{"scope_id":"thread-1"},"deadline":"2026-09-30T15:01:00Z","budget":{},"cause_id":"cause-1"}`
	authority := `{"agent":{"id":"agent-1","version":"agent-v1","digest":"` + agentDigest + `"},"installation_id":"installation-1","principal_chain":{"mode":"ON_BEHALF_OF","invoker_id":"invoker-1"},"audience":{},"context":{},"budget_ceiling":{},"grant_ref":"grant-persona-1","policy_digest":"` + policyDigest + `"}`
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO agent_run_request
		(tenant_id,request_id,source_kind,source_key_digest,source_ref,request_digest,request_payload,decision,authority_snapshot,
		admitted_at,deadline,agent_id,agent_version,agent_digest,installation_id,legal_entity_id,principal_chain,purpose,
		audience,context_scope,budget,cause_id)
		VALUES ($1,'admission-persona-1','PERSONA_MENTION',$2,'post-1',$3,$4::jsonb,'ACCEPTED',$5::jsonb,
		'2026-09-30T15:00:00Z','2026-09-30T15:01:00Z','agent-1','agent-v1',$6,'installation-1','',
		'{"mode":"ON_BEHALF_OF","invoker_id":"invoker-1"}'::jsonb,'test persona',
		'{"audience_id":"conversation-1"}'::jsonb,'{"scope_id":"thread-1"}'::jsonb,'{}'::jsonb,'cause-1')`,
		tenantID, strings.Repeat("1", 64), strings.Repeat("2", 64), request, authority, agentDigest); err != nil {
		t.Fatalf("seed accepted admission: %v", err)
	}
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO persona_versions(tenant_id,persona_id,version,agent_version,handle,display_name,profile,content_digest)
		VALUES ($1,'persona-1',1,'agent-1@agent-v1','helper','Helper','{}'::jsonb,'sha256:profile')`, tenantID); err != nil {
		t.Fatalf("seed persona version: %v", err)
	}
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO persona_lifecycle_events
		(tenant_id,event_id,persona_id,persona_version,from_state,to_state,reason,actor_id,occurred_at,
		profile_digest,review_digest,reviewer_id,evaluation_digest,evaluation_profile_digest,evaluation_suite_digest)
		VALUES ($1,'publish-1','persona-1',1,'IN_REVIEW','PUBLISHED','approved','reviewer-1','2026-09-30T14:00:00Z',
		'sha256:profile','sha256:review','reviewer-1','sha256:evaluation','sha256:profile','sha256:suite')`, tenantID); err != nil {
		t.Fatalf("seed published lifecycle event: %v", err)
	}
	channelPolicy := `{"max_tier":"T2","allowed_data_classes":[],"always_private":false,"conversation_search_allowed":false,"allowed_channel_classes":["PRIVATE"],"allow_external_members":false,"allow_cross_company_members":false}`
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO persona_installations
		(tenant_id,installation_id,persona_id,persona_version,conversation_id,conversation_class,installer_id,channel_policy,state,created_at,updated_at)
		VALUES ($1,'installation-1','persona-1',1,'conversation-1','PRIVATE','installer-1',$2::jsonb,'ACTIVE',now(),now())`, tenantID, channelPolicy); err != nil {
		t.Fatalf("seed active installation: %v", err)
	}
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO persona_invocations
		(tenant_id,invocation_id,conversation_id,thread_id,post_id,invoker_id,persona_id,persona_version,installation_id,mode,
		skills,actor,grant_payload,owner_id,admission_id,run_id,state,started_at)
		VALUES ($1,'invocation-1','conversation-1','thread-1','post-1','invoker-1','persona-1','1','installation-1','ON_BEHALF_OF',
		'{}'::jsonb,'{}'::jsonb,'{}'::jsonb,'invoker-1','admission-persona-1','run-persona-1',$2,CASE WHEN $2='STARTED' THEN now() ELSE NULL END)`, tenantID, invocationState); err != nil {
		t.Fatalf("seed started invocation: %v", err)
	}
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO agent_run_execution
		(tenant_id,run_id,admission_id,request_digest,agent_id,agent_version,agent_digest,context_digest,deadline,state,revision,fence,
		lease_owner,lease_until,cancel_requested,expire_requested,failure_requested,created_at,updated_at)
		VALUES ($1,'run-persona-1','admission-persona-1',$2,'agent-1','agent-v1',$3,$4,'2026-09-30T15:01:00Z','RUNNING',1,1,
		'worker-1','2026-09-30T15:00:30Z',false,false,false,'2026-09-30T15:00:00Z','2026-09-30T15:00:00Z')`, tenantID, strings.Repeat("2", 64), agentDigest, contextDigest); err != nil {
		t.Fatalf("seed durable run execution: %v", err)
	}
}

func assertPersonaRevocationWaiting(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, personaID string) {
	t.Helper()
	lockKey := "persona-security:" + tenantID.String() + ":PERSONA:" + personaID
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		err := db.SQL.QueryRow(`SELECT EXISTS (
			SELECT 1 FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid
			WHERE NOT l.granted AND l.locktype='advisory' AND a.wait_event_type='Lock'
		)`).Scan(&waiting)
		if err != nil {
			t.Fatalf("inspect advisory lock wait: %v", err)
		}
		if waiting {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(fmt.Sprintf("revocation did not wait on the active step's scope fence %q", lockKey))
}
