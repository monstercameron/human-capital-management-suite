package projection_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection"
)

type barrierQuerier struct {
	sourceHead       int64
	checkpoint       int64
	checkpointStatus string
	sourceErr        error
	checkpointErr    error
}

func (q *barrierQuerier) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return nil, errors.New("unexpected multi-row query")
}

func (q *barrierQuerier) QueryRow(_ context.Context, sql string, _ ...any) dbport.Row {
	if strings.Contains(sql, "FROM stream_head") {
		return barrierRow{scan: func(dest ...any) error {
			if q.sourceErr != nil {
				return q.sourceErr
			}
			if len(dest) != 1 {
				return fmt.Errorf("source-head scan received %d destinations", len(dest))
			}
			head, ok := dest[0].(*int64)
			if !ok {
				return fmt.Errorf("source-head destination has type %T", dest[0])
			}
			*head = q.sourceHead
			return nil
		}}
	}
	return barrierRow{scan: func(dest ...any) error {
		if q.checkpointErr != nil {
			return q.checkpointErr
		}
		if len(dest) != 3 {
			return fmt.Errorf("checkpoint scan received %d destinations", len(dest))
		}
		sequence, ok := dest[0].(*int64)
		if !ok {
			return fmt.Errorf("checkpoint sequence destination has type %T", dest[0])
		}
		digest, ok := dest[1].(**string)
		if !ok {
			return fmt.Errorf("checkpoint digest destination has type %T", dest[1])
		}
		status, ok := dest[2].(*string)
		if !ok {
			return fmt.Errorf("checkpoint status destination has type %T", dest[2])
		}
		*sequence = q.checkpoint
		*digest = nil
		*status = q.checkpointStatus
		return nil
	}}
}

type barrierRow struct {
	scan func(...any) error
}

func (r barrierRow) Scan(dest ...any) error { return r.scan(dest...) }

func runBarrierCheckStatusesAndTimeoutDiagnostics(t *testing.T) {
	tenant := uuid.New()
	deadline := time.Now().UTC().Add(time.Minute)
	tests := []struct {
		name       string
		querier    *barrierQuerier
		minimum    int64
		deadline   time.Time
		wantStatus projection.BarrierStatus
	}{
		{name: "ready", querier: &barrierQuerier{sourceHead: 5, checkpoint: 5, checkpointStatus: "CURRENT"}, minimum: 3, deadline: deadline, wantStatus: projection.BarrierReady},
		{name: "stale", querier: &barrierQuerier{sourceHead: 5, checkpoint: 2, checkpointStatus: "CURRENT"}, minimum: 3, deadline: deadline, wantStatus: projection.BarrierStale},
		{name: "source below minimum", querier: &barrierQuerier{sourceHead: 2, checkpoint: 2, checkpointStatus: "CURRENT"}, minimum: 3, deadline: deadline, wantStatus: projection.BarrierUnavailable},
		{name: "degraded", querier: &barrierQuerier{sourceHead: 5, checkpoint: 5, checkpointStatus: "LAGGING"}, minimum: 3, deadline: deadline, wantStatus: projection.BarrierDegraded},
		{name: "rebuilding", querier: &barrierQuerier{sourceHead: 5, checkpoint: 5, checkpointStatus: "REBUILDING"}, minimum: 3, deadline: deadline, wantStatus: projection.BarrierRebuilding},
		{name: "expired ready watermark", querier: &barrierQuerier{sourceHead: 7, checkpoint: 6, checkpointStatus: "CURRENT"}, minimum: 3, deadline: time.Now().UTC().Add(-time.Second), wantStatus: projection.BarrierTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := projection.Check(context.Background(), tt.querier, projection.ReadRequirement{
				Tenant: tenant, ProjectionName: projectionName, StreamKey: streamKey,
				MinimumSequence: tt.minimum, Deadline: tt.deadline,
			})
			if got.Status != tt.wantStatus {
				t.Fatalf("status = %s, want %s (err %v)", got.Status, tt.wantStatus, err)
			}
			if tt.wantStatus == projection.BarrierReady {
				if err != nil || got.Checkpoint.LastAppliedSequence != 5 || got.SourceHead != 5 {
					t.Fatalf("ready result = %+v, %v", got, err)
				}
				return
			}
			var barrierErr projection.BarrierError
			if !errors.As(err, &barrierErr) || barrierErr.Status != tt.wantStatus {
				t.Fatalf("typed barrier error = %#v, %v", barrierErr, err)
			}
			if barrierErr.Required != tt.minimum || barrierErr.Current != tt.querier.checkpoint || barrierErr.SourceHead != tt.querier.sourceHead {
				t.Fatalf("barrier diagnostics = %+v", barrierErr)
			}
			if tt.wantStatus == projection.BarrierTimeout && (got.Checkpoint.LastAppliedSequence != 6 || got.SourceHead != 7 || barrierErr.RetryAfter != 100*time.Millisecond) {
				t.Fatalf("timeout result lost observed heads or retry hint: result=%+v error=%+v", got, barrierErr)
			}
		})
	}
}

func runBarrierCheckUnavailableAndReadFailures(t *testing.T) {
	tenant := uuid.New()
	deadline := time.Now().UTC().Add(time.Minute)
	queryErr := errors.New("database unavailable")
	tests := []struct {
		name           string
		querier        *barrierQuerier
		wantStatus     projection.BarrierStatus
		wantCause      error
		wantWrapped    string
		wantSourceHead int64
		checkAwait     bool
	}{
		{name: "missing source head", querier: &barrierQuerier{sourceErr: dbport.ErrNoRows}, wantStatus: projection.BarrierUnavailable, wantCause: dbport.ErrNoRows},
		{name: "source head query failure", querier: &barrierQuerier{sourceErr: queryErr}, wantWrapped: "projection: read source head: database unavailable", checkAwait: true},
		{name: "missing checkpoint", querier: &barrierQuerier{sourceHead: 4, checkpointErr: dbport.ErrNoRows}, wantStatus: projection.BarrierUnavailable, wantSourceHead: 4},
		{name: "checkpoint query failure", querier: &barrierQuerier{sourceHead: 4, checkpointErr: queryErr}, wantWrapped: "projection: read checkpoint: read projection checkpoint worker_state/worker:1: database unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := projection.Check(context.Background(), tt.querier, projection.ReadRequirement{
				Tenant: tenant, ProjectionName: projectionName, StreamKey: streamKey,
				MinimumSequence: 2, Deadline: deadline,
			})
			if tt.wantWrapped != "" {
				if err == nil || err.Error() != tt.wantWrapped {
					t.Fatalf("error = %v, want %q", err, tt.wantWrapped)
				}
				if tt.checkAwait {
					_, awaitErr := projection.Await(context.Background(), tt.querier, projection.ReadRequirement{
						Tenant: tenant, ProjectionName: projectionName, StreamKey: streamKey,
						MinimumSequence: 2, Deadline: deadline,
					})
					if awaitErr == nil || awaitErr.Error() != tt.wantWrapped {
						t.Fatalf("await error = %v, want %q", awaitErr, tt.wantWrapped)
					}
				}
				return
			}
			var barrierErr projection.BarrierError
			if got.Status != tt.wantStatus || !errors.As(err, &barrierErr) || barrierErr.Status != tt.wantStatus {
				t.Fatalf("result/error = %+v, %v", got, err)
			}
			if tt.wantCause != nil && !errors.Is(err, tt.wantCause) {
				t.Fatalf("error %v does not preserve cause %v", err, tt.wantCause)
			}
			if got.SourceHead != tt.wantSourceHead || barrierErr.SourceHead != tt.wantSourceHead || barrierErr.Required != 2 {
				t.Fatalf("unavailable diagnostics = result %+v, error %+v", got, barrierErr)
			}
		})
	}
}

func runBarrierAwaitCancellationAndExpiration(t *testing.T) {
	tenant := uuid.New()
	querier := &barrierQuerier{sourceHead: 4, checkpoint: 1, checkpointStatus: "CURRENT"}
	req := projection.ReadRequirement{Tenant: tenant, ProjectionName: projectionName, StreamKey: streamKey, MinimumSequence: 2, Deadline: time.Now().UTC().Add(time.Minute)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := projection.Await(ctx, querier, req)
	if !errors.Is(err, context.Canceled) || got.Status != projection.BarrierStale {
		t.Fatalf("cancelled await = %+v, %v", got, err)
	}

	got, err = projection.Await(context.Background(), querier, projection.ReadRequirement{
		Tenant: tenant, ProjectionName: projectionName, StreamKey: streamKey,
		MinimumSequence: 2, Deadline: time.Now().UTC().Add(20 * time.Millisecond),
	})
	var barrierErr projection.BarrierError
	if !errors.As(err, &barrierErr) || barrierErr.Status != projection.BarrierTimeout || got.Status != projection.BarrierTimeout {
		t.Fatalf("expired await = %+v, %v", got, err)
	}
	if barrierErr.Required != 2 || barrierErr.Current != 1 || barrierErr.SourceHead != 4 {
		t.Fatalf("expired await diagnostics = %+v", barrierErr)
	}
}

type barrierTx struct {
	affected int64
	err      error
	called   bool
	sql      string
	args     []any
}

func (tx *barrierTx) Exec(_ context.Context, sql string, args ...any) (int64, error) {
	tx.called = true
	tx.sql = sql
	tx.args = args
	return tx.affected, tx.err
}

func (*barrierTx) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return nil, errors.New("unexpected multi-row query")
}

func (*barrierTx) QueryRow(context.Context, string, ...any) dbport.Row {
	return barrierRow{scan: func(...any) error { return errors.New("unexpected row query") }}
}

func (*barrierTx) Commit(context.Context) error   { return nil }
func (*barrierTx) Rollback(context.Context) error { return nil }

func runBarrierErrorAndSetStatusOutcomes(t *testing.T) {
	cause := errors.New("root cause")
	barrierErr := projection.BarrierError{Status: projection.BarrierStale, Required: 4, Current: 2, SourceHead: 6, Cause: cause}
	if got, want := barrierErr.Error(), "projection: read barrier STALE: required 4, current 2, source head 6"; got != want {
		t.Fatalf("barrier error = %q, want %q", got, want)
	}
	if !errors.Is(barrierErr, cause) {
		t.Fatalf("barrier error %v does not unwrap cause %v", barrierErr, cause)
	}

	tenant := uuid.New()
	tests := []struct {
		name       string
		status     string
		affected   int64
		execErr    error
		wantErr    string
		wantCalled bool
	}{
		{name: "invalid status", status: "UNKNOWN", wantErr: `projection: unsupported status "UNKNOWN"`},
		{name: "write failure", status: projection.StatusCurrent, execErr: cause, wantErr: "projection: set status: root cause", wantCalled: true},
		{name: "missing checkpoint", status: projection.StatusCurrent, wantErr: "PROJECTION_CHECKPOINT_NOT_FOUND: projection worker_state has no checkpoint on stream worker:1", wantCalled: true},
		{name: "updated checkpoint", status: "REBUILDING", affected: 1, wantCalled: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &barrierTx{affected: tt.affected, err: tt.execErr}
			err := projection.SetStatus(context.Background(), tx, tenant, projectionName, streamKey, tt.status)
			if tx.called != tt.wantCalled {
				t.Fatalf("exec called = %v, want %v", tx.called, tt.wantCalled)
			}
			if tt.wantCalled && (!strings.Contains(tx.sql, "UPDATE projection_checkpoint") || len(tx.args) != 4 || tx.args[0] != tenant || tx.args[1] != projectionName || tx.args[2] != streamKey || tx.args[3] != tt.status) {
				t.Fatalf("set status statement = %q with args %#v", tx.sql, tx.args)
			}
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("set status error = %v", err)
				}
				return
			}
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("set status error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
