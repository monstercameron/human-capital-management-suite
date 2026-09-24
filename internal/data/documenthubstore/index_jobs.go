package documenthubstore

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Index job states.
const (
	IndexQueued  = "queued"
	IndexRunning = "running"
	IndexDone    = "done"
	IndexFailed  = "failed"
	IndexSkipped = "skipped"
	// IndexLost reports a job whose lease expired and was reclaimed by
	// another worker before this one finished; its result was discarded.
	IndexLost = "lost"
)

// IndexJob is one claimed unit of indexing work.
type IndexJob struct {
	ID                                       int64
	TenantID, DocumentID, VersionID, ModelID string
	Attempts                                 int
}

// IndexRetry bounds retries: after MaxAttempts failed attempts a job is
// failed with its last error; before that it waits Backoff(attempts).
type IndexRetry struct {
	MaxAttempts int
	Backoff     func(attempts int) time.Duration
}

// IndexQueueStats counts one tenant's jobs for one model by state.
type IndexQueueStats struct {
	Queued, Running, Done, Failed, Skipped int
}

// SetIndexModel names the embedding model whose index jobs searchable
// writes enqueue. Empty, the default, enqueues nothing. Call it while
// composing, before the store serves requests.
func (s *Store) SetIndexModel(modelID string) { s.indexModel = modelID }

// IndexModel returns the model writes enqueue jobs for.
func (s *Store) IndexModel() string { return s.indexModel }

// enqueueIndexTx queues indexing of one version in the caller's write
// transaction, so a rolled-back write leaves no job behind.
func (s *Store) enqueueIndexTx(ctx context.Context, tx dbport.Tx, tenantID, docID, versionID string) error {
	if s.indexModel == "" {
		return nil
	}
	return enqueueIndexJobTx(ctx, tx, tenantID, docID, versionID, s.indexModel, false)
}

// enqueueIndexJobTx inserts one job. An existing job is left alone unless
// it was skipped (a draft a remote model may not see, which a deployment
// now makes eligible) or, when retryFailed is set, failed.
func enqueueIndexJobTx(ctx context.Context, tx dbport.Tx, tenantID, docID, versionID, modelID string, retryFailed bool) error {
	_, err := tx.Exec(ctx, `INSERT INTO document_index_job(tenant_id,document_id,version_id,model_id) VALUES($1,$2,$3,$4)
		ON CONFLICT (tenant_id,document_id,version_id,model_id) DO UPDATE
		SET status='queued', attempts=0, next_attempt_at=now(), last_error='', lease_owner='', lease_until=NULL, updated_at=now()
		WHERE document_index_job.status='skipped' OR ($5 AND document_index_job.status='failed')`,
		tenantID, docID, versionID, modelID, retryFailed)
	return err
}

// EnqueueMissingIndexJobs queues every searchable version of the tenant
// that has no vectors for modelID, requeueing skipped and failed jobs. It
// returns how many jobs it created or requeued.
func (s *Store) EnqueueMissingIndexJobs(ctx context.Context, tenantID, modelID string) (int, error) {
	if modelID == "" {
		return 0, ErrEmbeddingModel
	}
	refs, err := s.VersionsToIndex(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	queued := 0
	err = s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		for _, ref := range refs {
			var indexed bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM document_section_vector WHERE tenant_id=$1 AND version_id=$2 AND model_id=$3)`, tenantID, ref.VersionID, modelID).Scan(&indexed); err != nil {
				return err
			}
			if indexed {
				continue
			}
			n, err := tx.Exec(ctx, `INSERT INTO document_index_job(tenant_id,document_id,version_id,model_id) VALUES($1,$2,$3,$4)
				ON CONFLICT (tenant_id,document_id,version_id,model_id) DO UPDATE
				SET status='queued', attempts=0, next_attempt_at=now(), last_error='', lease_owner='', lease_until=NULL, updated_at=now()
				WHERE document_index_job.status IN ('skipped','failed')`, tenantID, ref.DocumentID, ref.VersionID, modelID)
			if err != nil {
				return err
			}
			queued += int(n)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return queued, nil
}

// ClaimIndexJobs leases up to limit due jobs: queued jobs whose next
// attempt has come, and running jobs whose lease ran out. FOR UPDATE SKIP
// LOCKED keeps concurrent workers from claiming the same row, and each
// claim counts as an attempt.
func (s *Store) ClaimIndexJobs(ctx context.Context, tenantID, modelID, owner string, limit int, lease time.Duration) ([]IndexJob, error) {
	if owner == "" || limit <= 0 || lease <= 0 {
		return nil, errors.New("document index: owner, limit and lease are required")
	}
	var out []IndexJob
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `UPDATE document_index_job j SET status='running', lease_owner=$3, lease_until=now()+make_interval(secs => $5), attempts=j.attempts+1, updated_at=now()
			WHERE j.id IN (SELECT id FROM document_index_job
				WHERE tenant_id=$1 AND model_id=$2
					AND ((status='queued' AND next_attempt_at<=now()) OR (status='running' AND lease_until<=now()))
				ORDER BY next_attempt_at, id
				FOR UPDATE SKIP LOCKED LIMIT $4)
			RETURNING j.id, j.tenant_id, j.document_id, j.version_id, j.model_id, j.attempts`,
			tenantID, modelID, owner, limit, lease.Seconds())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var j IndexJob
			if err := rows.Scan(&j.ID, &j.TenantID, &j.DocumentID, &j.VersionID, &j.ModelID, &j.Attempts); err != nil {
				return err
			}
			out = append(out, j)
		}
		return rows.Err()
	})
	return out, err
}

// RunIndexJob processes one claimed job and records its outcome under the
// claimant's lease: skipped when the version is no longer what anyone
// searches (superseded, retired or disposed) or is a draft a remote model
// may not see; done once vectors exist; otherwise retried with backoff and
// failed after the last attempt. It returns the recorded state, or
// IndexLost when the lease was reclaimed first.
func (s *Store) RunIndexJob(ctx context.Context, job IndexJob, owner string, model EmbeddingModel, embed func(context.Context, []string) ([][]float32, error), retry IndexRetry) (string, error) {
	if model.ID != job.ModelID {
		return "", ErrEmbeddingModel
	}
	var searchable bool
	err := s.RunTenantTx(ctx, job.TenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM document d
			JOIN document_version v ON v.tenant_id=d.tenant_id AND v.document_id=d.id AND v.id=$3 AND v.status<>'retired'
			WHERE d.tenant_id=$1 AND d.id=$2 AND d.lifecycle<>'DISPOSED'
				AND (EXISTS (SELECT 1 FROM document_active_pointer p WHERE p.tenant_id=d.tenant_id AND p.document_id=d.id AND p.version_id=v.id)
					OR v.id=(SELECT x.id FROM document_version x WHERE x.tenant_id=d.tenant_id AND x.document_id=d.id AND x.status<>'retired' ORDER BY x.created_at DESC, x.id DESC LIMIT 1)))`,
			job.TenantID, job.DocumentID, job.VersionID).Scan(&searchable)
	})
	if err != nil {
		return "", err
	}
	if !searchable {
		return s.finishIndexJob(ctx, job, owner, IndexSkipped, "superseded: no longer the owner's latest or a published version", time.Time{})
	}
	_, err = s.IndexVersionVectors(ctx, job.TenantID, job.DocumentID, job.VersionID, model, embed)
	switch {
	case errors.Is(err, ErrDraftEgress):
		return s.finishIndexJob(ctx, job, owner, IndexSkipped, err.Error(), time.Time{})
	case err != nil:
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if job.Attempts >= retry.MaxAttempts {
			return s.finishIndexJob(ctx, job, owner, IndexFailed, err.Error(), time.Time{})
		}
		wait := time.Second
		if retry.Backoff != nil {
			wait = retry.Backoff(job.Attempts)
		}
		return s.finishIndexJob(ctx, job, owner, IndexQueued, err.Error(), time.Now().Add(wait))
	}
	return s.finishIndexJob(ctx, job, owner, IndexDone, "", time.Time{})
}

func (s *Store) finishIndexJob(ctx context.Context, job IndexJob, owner, state, lastError string, next time.Time) (string, error) {
	if len(lastError) > 2000 {
		lastError = lastError[:2000]
	}
	var n int64
	err := s.RunTenantTx(ctx, job.TenantID, func(tx dbport.Tx) error {
		var err error
		n, err = tx.Exec(ctx, `UPDATE document_index_job SET status=$4, last_error=$5, next_attempt_at=COALESCE($6::timestamptz, next_attempt_at), lease_owner='', lease_until=NULL, updated_at=now()
			WHERE tenant_id=$1 AND id=$2 AND lease_owner=$3 AND status='running'`,
			job.TenantID, job.ID, owner, state, lastError, nullableTime(next))
		return err
	})
	if err != nil {
		return "", err
	}
	if n == 0 {
		return IndexLost, nil
	}
	return state, nil
}

// IndexQueueStats counts the tenant's jobs for modelID by state.
func (s *Store) IndexQueueStats(ctx context.Context, tenantID, modelID string) (IndexQueueStats, error) {
	var st IndexQueueStats
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status='queued'), count(*) FILTER (WHERE status='running'),
				count(*) FILTER (WHERE status='done'), count(*) FILTER (WHERE status='failed'), count(*) FILTER (WHERE status='skipped')
			FROM document_index_job WHERE tenant_id=$1 AND model_id=$2`, tenantID, modelID).Scan(&st.Queued, &st.Running, &st.Done, &st.Failed, &st.Skipped)
	})
	return st, err
}

// SemanticPending counts the documents the actor can read whose readable
// version has no vectors for modelID yet and whose indexing has not ended
// as failed or skipped, the honest "still indexing" number for the UI.
func (s *Store) SemanticPending(ctx context.Context, tenantID, actorID, modelID string) (int, error) {
	options, _, err := normalizeListOptions(actorID, ListOptions{})
	if err != nil {
		return 0, err
	}
	var n int
	err = s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) `+listSelectionSQL+` AND v.id IS NOT NULL
			AND NOT EXISTS (SELECT 1 FROM document_section_vector sv WHERE sv.tenant_id=d.tenant_id AND sv.version_id=v.id AND sv.model_id=$9)
			AND NOT EXISTS (SELECT 1 FROM document_index_job j WHERE j.tenant_id=d.tenant_id AND j.version_id=v.id AND j.model_id=$9 AND j.status IN ('failed','skipped'))`,
			append(listSelectionArgs(tenantID, actorID, options, nil), modelID)...).Scan(&n)
	})
	return n, err
}
