package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intelligencestore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/intelligence"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// IntelligenceMetricJob is an explicitly configured worker request. The
// worker resolves all population and decision facts from tenant-scoped data;
// the request supplies only the governed definition and decision identity.
type IntelligenceMetricJob struct {
	TenantID   string                        `json:"tenant_id"`
	Definition intelligence.MetricDefinition `json:"definition"`
	DecisionID string                        `json:"decision_id"`
}

type metricJobPool interface {
	dbport.Beginner
	Query(ctx context.Context, sql string, args ...any) (dbport.Rows, error)
}

func parseIntelligenceMetricJob(raw string) (IntelligenceMetricJob, error) {
	var job IntelligenceMetricJob
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&job); err != nil {
		return IntelligenceMetricJob{}, fmt.Errorf("worker: decode intelligence metric job: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return IntelligenceMetricJob{}, fmt.Errorf("worker: intelligence metric job contains trailing JSON")
	}
	tenantID, tenantErr := uuid.Parse(job.TenantID)
	decisionID, decisionErr := uuid.Parse(job.DecisionID)
	if tenantErr != nil || tenantID == uuid.Nil || decisionErr != nil || decisionID == uuid.Nil {
		return IntelligenceMetricJob{}, fmt.Errorf("worker: metric job requires non-nil tenant_id and decision_id UUIDs")
	}
	if _, err := intelligence.CompileMetric(job.Definition); err != nil {
		return IntelligenceMetricJob{}, err
	}
	return job, nil
}

// runIntelligenceMetricJob compiles and executes a metric over persisted
// tenant workforce rows, verifies the referenced persisted intent decision,
// seals its descriptive outcome link, and atomically persists both typed
// outputs. It is safe to replay: publication identity is content addressed.
func runIntelligenceMetricJob(ctx context.Context, pool metricJobPool, job IntelligenceMetricJob, tenantID uuid.UUID, publishedAt time.Time) (intelligencestore.Publication, error) {
	if pool == nil || tenantID == uuid.Nil || job.TenantID != tenantID.String() || job.DecisionID == "" || publishedAt.IsZero() {
		return intelligencestore.Publication{}, fmt.Errorf("worker: invalid intelligence metric job")
	}
	decisionID, err := uuid.Parse(job.DecisionID)
	if err != nil || decisionID == uuid.Nil {
		return intelligencestore.Publication{}, fmt.Errorf("worker: decision_id must be a UUID")
	}
	compiled, err := intelligence.CompileMetric(job.Definition)
	if err != nil {
		return intelligencestore.Publication{}, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return intelligencestore.Publication{}, fmt.Errorf("worker: begin metric snapshot: %w", err)
	}
	rollbackCtx, cancelRollback := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancelRollback()
	defer tx.Rollback(rollbackCtx)
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return intelligencestore.Publication{}, err
	}
	var tenantStatus string
	err = tx.QueryRow(ctx, `SELECT status FROM tenant WHERE tenant_id=$1`, tenantID).Scan(&tenantStatus)
	if errors.Is(err, dbport.ErrNoRows) {
		return intelligencestore.Publication{}, fmt.Errorf("worker: metric job tenant %s is not active", tenantID)
	}
	if err != nil {
		return intelligencestore.Publication{}, fmt.Errorf("worker: validate metric tenant: %w", err)
	}
	if tenantStatus != "ACTIVE" {
		return intelligencestore.Publication{}, fmt.Errorf("worker: metric job tenant %s is not active", tenantID)
	}
	var intentID uuid.UUID
	var decisionAuthority, decisionOutcome string
	var proposalDigest string
	err = tx.QueryRow(ctx, `SELECT intent_id, authority_ref, proposal_digest, decision_outcome
		FROM intent_decision WHERE tenant_id=$1 AND decision_id=$2`, tenantID, decisionID).
		Scan(&intentID, &decisionAuthority, &proposalDigest, &decisionOutcome)
	if errors.Is(err, dbport.ErrNoRows) {
		return intelligencestore.Publication{}, fmt.Errorf("worker: decision %s is not present for tenant %s", decisionID, tenantID)
	}
	if err != nil {
		return intelligencestore.Publication{}, fmt.Errorf("worker: load decision: %w", err)
	}
	if strings.TrimSpace(decisionAuthority) == "" || (decisionOutcome != "APPROVED" && decisionOutcome != "REJECTED") {
		return intelligencestore.Publication{}, fmt.Errorf("worker: decision must carry authority and a final approved or rejected outcome")
	}
	rows, err := tx.Query(ctx, `SELECT worker_id, fte::text, revision_stream, revision_sequence
		FROM journey_worker
		WHERE tenant_id=$1 AND lifecycle_status='ACTIVE' AND effective_from < $2::date
		ORDER BY worker_id`, tenantID, job.Definition.EffectiveTo.UTC().Format("2006-01-02"))
	if err != nil {
		return intelligencestore.Publication{}, fmt.Errorf("worker: load metric population: %w", err)
	}
	var metricRows []intelligence.MetricRow
	for rows.Next() {
		var workerID uuid.UUID
		var fteText, stream string
		var sequence int64
		if err := rows.Scan(&workerID, &fteText, &stream, &sequence); err != nil {
			rows.Close()
			return intelligencestore.Publication{}, fmt.Errorf("worker: scan metric population: %w", err)
		}
		fte, err := values.NewDecimal(fteText, 4, job.Definition.Rounding)
		if err != nil {
			rows.Close()
			return intelligencestore.Publication{}, fmt.Errorf("worker: invalid stored FTE for %s: %w", workerID, err)
		}
		metricRows = append(metricRows, intelligence.MetricRow{
			Included: true, Numerator: fte, Denominator: values.MustDecimal("1", 4, job.Definition.Rounding),
			NumeratorSet: true, DenominatorSet: true,
			Watermark: fmt.Sprintf("%s:%d", stream, sequence),
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return intelligencestore.Publication{}, fmt.Errorf("worker: read metric population: %w", err)
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return intelligencestore.Publication{}, fmt.Errorf("worker: close metric snapshot: %w", err)
	}
	result, err := compiled.Execute(metricRows)
	if err != nil {
		return intelligencestore.Publication{}, err
	}
	watermark := strings.Join(result.SourceWatermarks, ",")
	if watermark == "" {
		watermark = "empty-population"
	}
	link, err := intelligence.RecordOutcomeLink(intelligence.OutcomeLinkInput{
		Tenant: tenantID.String(), DecisionRef: decisionID.String(), OutcomeRef: intentID.String(),
		Definition: job.Definition.ID, DefinitionVersion: job.Definition.Version,
		Window:        intelligence.OutcomeWindow{Start: job.Definition.EffectiveFrom, End: job.Definition.EffectiveTo},
		PopulationRef: result.PopulationDigest, SourceAuthority: decisionAuthority,
		Attribution: intelligence.AttributionDescriptive, BasisRef: proposalDigest,
		Watermark: watermark, Confidence: "observed", Limitations: "descriptive workforce snapshot; no causal attribution",
	}, publishedAt)
	if err != nil {
		return intelligencestore.Publication{}, err
	}
	publicationID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(tenantID.String()+"\x00"+result.CalculationDigest+"\x00"+link.Digest))
	publication := intelligencestore.Publication{
		ID: publicationID, TenantID: tenantID, MetricID: job.Definition.ID,
		Version: job.Definition.Version, DecisionID: decisionID,
		Result: result, Outcome: link, PublishedAt: publishedAt,
	}
	stored, err := intelligencestore.New(pool).Publish(ctx, publication)
	if err != nil {
		return intelligencestore.Publication{}, err
	}
	return stored, nil
}

func runIntelligenceMetricLoop(ctx context.Context, pool metricJobPool, job IntelligenceMetricJob, pollInterval time.Duration, now func() time.Time) error {
	if pollInterval <= 0 || now == nil {
		return fmt.Errorf("worker: metric loop requires a positive interval and clock")
	}
	tenantID, err := uuid.Parse(job.TenantID)
	if err != nil || tenantID == uuid.Nil {
		return fmt.Errorf("worker: invalid metric job tenant")
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := runIntelligenceMetricJob(ctx, pool, job, tenantID, now().UTC()); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
