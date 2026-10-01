package timestore

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ListPublishedSiteShifts returns a bounded, cursor-ordered projection of
// published crew shifts for a site. The cursor is the last shift id returned;
// callers must continue with the returned next cursor rather than scanning a
// tenant workforce. Window overlap is evaluated against server-side shift
// instants and cancelled or draft rows are excluded.
func (s *Store) ListPublishedSiteShifts(ctx context.Context, tenant, site string, from, to time.Time, cursor string, limit int) ([]Shift, string, error) {
	if tenant == "" || site == "" || from.IsZero() || to.IsZero() || !to.After(from) || limit <= 0 || limit > 1000 {
		return nil, "", ErrInvalid
	}
	var out []Shift
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT tenant_id,id,worker_ref,site_ref,project_ref,revision,status,work_start,work_end,payload,created_at,updated_at
			FROM crew_shift
			WHERE tenant_id=$1 AND site_ref=$2 AND status='PUBLISHED' AND work_end>$3 AND work_start<$4 AND id>$5
			ORDER BY id LIMIT $6`, tenant, site, from, to, cursor, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var sh Shift
			var status string
			var payload []byte
			if err := rows.Scan(&sh.TenantID, &sh.ID, &sh.WorkerRef, &sh.SiteRef, &sh.ProjectRef, &sh.Revision, &status, &sh.WorkStart, &sh.WorkEnd, &payload, &sh.CreatedAt, &sh.UpdatedAt); err != nil {
				return err
			}
			sh.Status = ShiftStatus(status)
			sh.Payload = append([]byte(nil), payload...)
			out = append(out, sh)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, "", err
	}
	if len(out) > limit {
		return out[:limit], out[limit-1].ID, nil
	}
	return out, "", nil
}

// ListActiveCredentials returns only active credentials for the supplied
// worker references. It is bounded by the caller's published shift page and
// never becomes a tenant-wide credential scan.
func (s *Store) ListActiveCredentials(ctx context.Context, tenant string, workers []string) ([]Credential, error) {
	if tenant == "" || len(workers) == 0 || len(workers) > 1000 {
		return nil, ErrInvalid
	}
	var out []Credential
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+credentialColumns+` FROM time_worker_credential WHERE tenant_id=$1 AND worker_id = ANY($2::text[]) AND state='ACTIVE' ORDER BY worker_id,kind`, tenant, workers)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			c, err := scanCredential(rows)
			if err != nil {
				if errors.Is(err, ErrNotFound) {
					continue
				}
				return err
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}
