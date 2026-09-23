package chatrecordstore

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func (s *Store) GetRetentionPolicy(ctx context.Context, tenant, kind string) (chatrecords.RetentionPolicy, bool, error) {
	if tenant == "" {
		return chatrecords.RetentionPolicy{}, false, chatrecords.ErrInvalid
	}
	var out chatrecords.RetentionPolicy
	found := false
	err := s.txTenant(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT mode,age_days,before_date,budget_bytes,revision,updated_by,updated_at FROM chat_retention_policy WHERE tenant_id=$1 AND conversation_kind=$2`, tenant, kind)
		if err != nil {
			return err
		}
		defer rows.Close()
		if !rows.Next() {
			return rows.Err()
		}
		out.TenantID, out.Kind = tenant, kind
		if err := rows.Scan(&out.Mode, &out.AgeDays, &out.BeforeDate, &out.BudgetBytes, &out.Revision, &out.UpdatedBy, &out.UpdatedAt); err != nil {
			return err
		}
		if out.BeforeDate.Equal(time.Unix(0, 0)) {
			out.BeforeDate = time.Time{}
		}
		found = true
		return rows.Err()
	})
	return out, found, err
}

func (s *Store) PutRetentionPolicy(ctx context.Context, p chatrecords.RetentionPolicy, expected uint64) (chatrecords.RetentionPolicy, error) {
	if p.TenantID == "" || p.Revision == 0 || p.Revision != expected+1 || p.UpdatedBy == "" || p.UpdatedAt.IsZero() {
		return chatrecords.RetentionPolicy{}, chatrecords.ErrInvalid
	}
	before := p.BeforeDate
	if before.IsZero() {
		before = time.Unix(0, 0).UTC()
	}
	err := s.txTenant(ctx, p.TenantID, func(tx dbport.Tx) error {
		if expected == 0 {
			rows, err := tx.Exec(ctx, `INSERT INTO chat_retention_policy(tenant_id,conversation_kind,mode,age_days,before_date,budget_bytes,revision,updated_by,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT DO NOTHING`, p.TenantID, p.Kind, p.Mode, p.AgeDays, before, p.BudgetBytes, p.Revision, p.UpdatedBy, p.UpdatedAt)
			if err != nil {
				return err
			}
			if rows != 1 {
				return chatrecords.ErrConflict
			}
			return nil
		}
		rows, err := tx.Exec(ctx, `UPDATE chat_retention_policy SET mode=$1,age_days=$2,before_date=$3,budget_bytes=$4,revision=$5,updated_by=$6,updated_at=$7 WHERE tenant_id=$8 AND conversation_kind=$9 AND revision=$10`, p.Mode, p.AgeDays, before, p.BudgetBytes, p.Revision, p.UpdatedBy, p.UpdatedAt, p.TenantID, p.Kind, expected)
		if err != nil {
			return err
		}
		if rows != 1 {
			return chatrecords.ErrConflict
		}
		return nil
	})
	if err != nil {
		return chatrecords.RetentionPolicy{}, err
	}
	return p, nil
}

var _ chatrecords.RetentionRepository = (*Store)(nil)
