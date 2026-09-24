package intelligencestore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/intelligence"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestTodo_REV_026_02_StoreRejectsUnboundPublication(t *testing.T) {
	store := New(nil)
	_, err := store.Publish(context.Background(), Publication{
		ID: uuid.New(), TenantID: uuid.New(), DecisionID: uuid.New(),
		MetricID: "metric", Version: "v1", Result: intelligence.MetricResult{},
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("unbound publication error = %v, want ErrInvalid", err)
	}
}

func TestTodo_REV_026_02_Store_Integration(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID, intentID, decisionID := uuid.New(), uuid.New(), uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1,$2,'cell-local','intelligence store','ACTIVE',now())`, tenantID, "intel-store-"+tenantID.String())
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
	pool, err := pgxadapter.NewPool(ctx, db.URL, map[string]string{"search_path": db.Schema, "role": "hcmnext_app"})
	if err != nil {
		t.Fatalf("open app pool: %v", err)
	}
	t.Cleanup(pool.Close)
	start, end := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	population := "sha256:population-store-test"
	decimal := values.MustDecimal("0.8750", 4, values.RoundingHalfUp)
	result := intelligence.MetricResult{
		Numerator: decimal, Denominator: values.MustDecimal("1.0000", 4, values.RoundingHalfUp), Value: decimal,
		SampleSize: 1, Quality: intelligence.QualityOK, SourceWatermarks: []string{"stream:1"},
		PopulationDigest: population, CalculationDigest: "sha256:calculation-store-test",
	}
	link, err := intelligence.RecordOutcomeLink(intelligence.OutcomeLinkInput{
		Tenant: tenantID.String(), DecisionRef: decisionID.String(), OutcomeRef: intentID.String(),
		Definition: "workforce:active-fte", DefinitionVersion: "v1",
		Window: intelligence.OutcomeWindow{Start: start, End: end}, PopulationRef: population,
		SourceAuthority: "authority:analytics", Attribution: intelligence.AttributionDescriptive,
		Watermark: "stream:1", Confidence: "observed",
	}, end.Add(time.Hour))
	if err != nil {
		t.Fatalf("RecordOutcomeLink: %v", err)
	}
	in := Publication{
		ID: uuid.New(), TenantID: tenantID, MetricID: "workforce:active-fte", Version: "v1",
		DecisionID: decisionID, Result: result, Outcome: link, PublishedAt: end.Add(2 * time.Hour),
	}
	store := New(pool)
	first, err := store.Publish(ctx, in)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	replay, err := store.Publish(ctx, in)
	if err != nil {
		t.Fatalf("Publish replay: %v", err)
	}
	if first.ID != replay.ID || replay.Result.Value.String() != "0.8750" || replay.Outcome.Digest != link.Digest {
		t.Fatalf("publication replay changed the typed record: first=%+v replay=%+v", first, replay)
	}
	readTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin publication read: %v", err)
	}
	defer readTx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, readTx, tenantID); err != nil {
		t.Fatalf("scope publication read: %v", err)
	}
	var storedResult, storedOutcome []byte
	if err := readTx.QueryRow(ctx, `SELECT metric_result, outcome_link FROM intelligence_metric_publication WHERE tenant_id=$1 AND publication_id=$2`, tenantID, first.ID).Scan(&storedResult, &storedOutcome); err != nil {
		t.Fatalf("read persisted publication: %v", err)
	}
	if len(storedResult) == 0 || len(storedOutcome) == 0 {
		t.Fatalf("persisted typed payloads are empty: result=%s outcome=%s", storedResult, storedOutcome)
	}
}
