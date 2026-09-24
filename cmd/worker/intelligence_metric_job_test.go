package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/intelligence"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

func metricJobDefinition() intelligence.MetricDefinition {
	return intelligence.MetricDefinition{
		ID: "workforce:active-fte", Version: "v1", Kind: intelligence.MetricMean,
		NullPolicy: intelligence.NullUnknownIfAny, Rounding: values.RoundingHalfUp, Scale: 4, Unit: "fte per active worker",
		EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		EffectiveTo:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	}
}

// TestTodo_REV_026_02 proves the owned typed path compiles and executes a
// metric, then seals an outcome link to an explicit persisted decision ref.
func TestTodo_REV_026_02(t *testing.T) {
	d := func(text string) values.Decimal {
		out, err := values.NewDecimal(text, 4, values.RoundingHalfUp)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	compiled, err := intelligence.CompileMetric(metricJobDefinition())
	if err != nil {
		t.Fatalf("CompileMetric: %v", err)
	}
	result, err := compiled.Execute([]intelligence.MetricRow{
		{Included: true, Numerator: d("0.7500"), NumeratorSet: true, Watermark: "workforce:1"},
		{Included: true, Numerator: d("1.0000"), NumeratorSet: true, Watermark: "workforce:2"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Value.String() != "0.8750" || result.Quality != intelligence.QualityOK || result.SampleSize != 2 {
		t.Fatalf("metric result = %+v", result)
	}
	tenantID, decisionID := uuid.New(), uuid.New()
	link, err := intelligence.RecordOutcomeLink(intelligence.OutcomeLinkInput{
		Tenant: tenantID.String(), DecisionRef: decisionID.String(), OutcomeRef: uuid.NewString(),
		Definition: metricJobDefinition().ID, DefinitionVersion: metricJobDefinition().Version,
		Window:        intelligence.OutcomeWindow{Start: metricJobDefinition().EffectiveFrom, End: metricJobDefinition().EffectiveTo},
		PopulationRef: result.PopulationDigest, SourceAuthority: "authority:test",
		Attribution: intelligence.AttributionDescriptive, Watermark: "workforce:1,workforce:2", Confidence: "observed",
	}, time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("RecordOutcomeLink: %v", err)
	}
	if link.DecisionRef != decisionID.String() || link.OutcomeRef == "" || link.Digest == "" {
		t.Fatalf("outcome link is not bound to the decision: %+v", link)
	}
}

// TestTodo_REV_026_02_Integration proves the shipped worker job reads a real
// tenant workforce and intent decision, then persists the typed result and
// outcome linkage under the same tenant boundary.
func TestTodo_REV_026_02_Integration(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID, intentID, decisionID := uuid.New(), uuid.New(), uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1,$2,'cell-local','metric integration','ACTIVE',timestamptz '2026-01-01T00:00:00Z')`,
		tenantID, "metric-"+tenantID.String())
	db.Exec(t, `INSERT INTO intent_instance (
		tenant_id,intent_id,definition_ref,definition_version,request_digest,idempotency_key,
		request_state,execution_state,business_state,consistency_state,obligation_state,created_at,last_transition_at)
		VALUES ($1,$2,'test.analytics',1,repeat('a',64),$3,
		'SUBMITTED','NOT_PLANNED','IN_PROGRESS','NOT_APPLICABLE','NOT_APPLICABLE',now(),now())`, tenantID, intentID, "metric-"+intentID.String())
	db.Exec(t, `INSERT INTO proposal_revision (
		tenant_id,intent_id,revision,proposal_digest,material_digest,schema_ref,payload,produced_by,produced_at)
		VALUES ($1,$2,1,repeat('b',64),repeat('c',64),'test.analytics@1',decode('00','hex'),'test',now())`, tenantID, intentID)
	db.Exec(t, `INSERT INTO intent_decision (
		tenant_id,decision_id,intent_id,revision,requirement_id,decision_kind,decision_outcome,
		proposal_digest,control_digest,materiality_class,decided_by,authority_ref,decision_reason,decided_at)
		VALUES ($1,$2,$3,1,'req.analytics','HUMAN_APPROVAL','APPROVED',repeat('b',64),repeat('d',64),
		'MATERIAL','principal:analytics','authority:analytics','approved analytics review',now())`, tenantID, decisionID, intentID)
	for i, fte := range []string{"0.7500", "1.0000"} {
		workerID := uuid.New()
		db.Exec(t, `INSERT INTO journey_worker (
			tenant_id,worker_id,worker_key,legal_name,preferred_name,worker_number,
			employment_id,assignment_id,job_code,grade,org_unit,position_id,location,pay_zone,
			fte,manager_relationship_ref,hire_date,effective_from,base_pay,currency,pay_basis,
			bonus_target,revision_stream,revision_sequence,known_at,created_by)
			VALUES ($1,$2,$3,'Worker','Worker',$6,'employment','assignment','JOB','P1','ORG','POS','NYC','US',
			$4,'manager',date '2026-01-01',date '2026-01-01',50000,'USD','ANNUAL',0.1,$5,1,timestamptz '2026-01-01T00:00:00Z', 'test')`,
			tenantID, workerID, fmt.Sprintf("metric-worker-%d", i), fte, fmt.Sprintf("workforce-%d", i), fmt.Sprintf("metric-worker-number-%d", i))
	}
	pool, err := pgxadapter.NewPool(ctx, db.URL, map[string]string{"search_path": db.Schema, "role": "hcmnext_app"})
	if err != nil {
		t.Fatalf("open app pool: %v", err)
	}
	t.Cleanup(pool.Close)
	job := IntelligenceMetricJob{TenantID: tenantID.String(), Definition: metricJobDefinition(), DecisionID: decisionID.String()}
	jobJSON, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("encode configured metric job: %v", err)
	}
	config, err := bootstrap.ParseConfig([]string{
		"-database-url=postgres://ignored/db", "-poll-interval=1h", "-messaging-role=false",
		"-intelligence-metric-job=" + string(jobJSON),
	}, nil, workerConfigFields())
	if err != nil {
		t.Fatalf("parse configured metric job: %v", err)
	}
	if err := validateConfig(config); err != nil {
		t.Fatalf("validate configured metric job: %v", err)
	}
	runtime, err := build(ctx, bootstrap.Deps{
		DB: pool, Values: config, Logger: discardLogger(), Identity: "metric-worker-test",
		Clock: func() time.Time { return time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("build configured metric worker: %v", err)
	}
	var metricWorkload *bootstrap.Workload
	for i := range runtime.Workloads {
		if runtime.Workloads[i].Name == "intelligence-metric" {
			metricWorkload = &runtime.Workloads[i]
			break
		}
	}
	if metricWorkload == nil {
		t.Fatalf("worker workloads do not contain intelligence-metric: %+v", runtime.Workloads)
	}
	loopCtx, cancelLoop := context.WithCancel(ctx)
	defer cancelLoop()
	loopDone := make(chan error, 1)
	go func() {
		loopDone <- metricWorkload.Run(loopCtx)
	}()
	var count int
	var publicationID uuid.UUID
	var sampleSizeText string
	var sampleSize int
	var decisionRef, outcomeRef string
	var lastReadErr error
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		readTx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin publication read: %v", err)
		}
		if err := tenancy.WithTenant(ctx, readTx, tenantID); err != nil {
			_ = readTx.Rollback(ctx)
			t.Fatalf("scope publication read: %v", err)
		}
		lastReadErr = readTx.QueryRow(ctx, `SELECT publication_id, metric_result->>'SampleSize',
			outcome_link->>'DecisionRef', outcome_link->>'OutcomeRef'
			FROM intelligence_metric_publication WHERE tenant_id=$1`, tenantID).
			Scan(&publicationID, &sampleSizeText, &decisionRef, &outcomeRef)
		_ = readTx.Rollback(ctx)
		if lastReadErr == nil {
			sampleSize, lastReadErr = strconv.Atoi(sampleSizeText)
			if lastReadErr != nil {
				t.Fatalf("invalid persisted sample size %q: %v", sampleSizeText, lastReadErr)
			}
			count = 1
			break
		}
		select {
		case err := <-loopDone:
			t.Fatalf("metric worker loop exited before publishing: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	if count != 1 {
		select {
		case loopErr := <-loopDone:
			t.Fatalf("metric worker loop did not publish (loop error %v; last read %v)", loopErr, lastReadErr)
		default:
			t.Fatalf("metric worker loop did not publish (last read %v)", lastReadErr)
		}
	}
	if sampleSize != 2 || decisionRef != decisionID.String() || outcomeRef != intentID.String() {
		t.Fatalf("loop publication sample=%d decision=%s outcome=%s", sampleSize, decisionRef, outcomeRef)
	}
	cancelLoop()
	if err := <-loopDone; err != context.Canceled {
		t.Fatalf("metric loop exit = %v, want context canceled", err)
	}
	readTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin cross-tenant publication read: %v", err)
	}
	defer readTx.Rollback(ctx)
	otherTenant := uuid.New()
	if err := tenancy.WithTenant(ctx, readTx, otherTenant); err != nil {
		t.Fatalf("scope cross-tenant read: %v", err)
	}
	if err := readTx.QueryRow(ctx, `SELECT count(*) FROM intelligence_metric_publication WHERE tenant_id=$1`, otherTenant).Scan(&count); err != nil || count != 0 {
		t.Fatalf("cross-tenant publication count=%d err=%v", count, err)
	}

	t.Run("configured worker rejects inactive tenant", func(t *testing.T) {
		db.Exec(t, `UPDATE tenant SET status='SUSPENDED' WHERE tenant_id=$1`, tenantID)
		inactiveJob := job
		inactiveJob.Definition.ID = "workforce:inactive-tenant-control"
		inactiveJSON, err := json.Marshal(inactiveJob)
		if err != nil {
			t.Fatalf("encode inactive metric job: %v", err)
		}
		inactiveConfig, err := bootstrap.ParseConfig([]string{
			"-database-url=postgres://ignored/db", "-poll-interval=1h", "-messaging-role=false",
			"-intelligence-metric-job=" + string(inactiveJSON),
		}, nil, workerConfigFields())
		if err != nil {
			t.Fatalf("parse inactive metric job: %v", err)
		}
		inactiveRuntime, err := build(ctx, bootstrap.Deps{
			DB: pool, Values: inactiveConfig, Logger: discardLogger(), Identity: "metric-worker-test",
			Clock: func() time.Time { return time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC) },
		})
		if err != nil {
			t.Fatalf("build inactive metric worker: %v", err)
		}
		var inactiveWorkload *bootstrap.Workload
		for i := range inactiveRuntime.Workloads {
			if inactiveRuntime.Workloads[i].Name == "intelligence-metric" {
				inactiveWorkload = &inactiveRuntime.Workloads[i]
				break
			}
		}
		if inactiveWorkload == nil {
			t.Fatal("inactive runtime has no intelligence-metric workload")
		}
		err = inactiveWorkload.Run(ctx)
		if err == nil || !strings.Contains(err.Error(), "not active") {
			t.Fatalf("inactive tenant workload error = %v, want not-active rejection", err)
		}
		checkTx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin inactive publication read: %v", err)
		}
		defer checkTx.Rollback(ctx)
		if err := tenancy.WithTenant(ctx, checkTx, tenantID); err != nil {
			t.Fatalf("scope inactive publication read: %v", err)
		}
		if err := checkTx.QueryRow(ctx, `SELECT count(*) FROM intelligence_metric_publication WHERE tenant_id=$1 AND metric_id=$2`, tenantID, inactiveJob.Definition.ID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("inactive tenant publications=%d err=%v", count, err)
		}
	})
}
