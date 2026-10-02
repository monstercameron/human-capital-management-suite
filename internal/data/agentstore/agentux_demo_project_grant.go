package agentstore

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type SupportProjectGrant struct {
	TenantID                             uuid.UUID
	ServiceSubject, ProjectID, GrantedBy string
	Revision                             uint64
	Active                               bool
	RecordedAt                           time.Time
}

// AppendSupportProjectGrant requires a provisioning/owner connection. The
// serving agent role deliberately has SELECT only on this authority ledger.
func (s *SupportInboxStore) AppendSupportProjectGrant(ctx context.Context, r SupportProjectGrant) error {
	if s == nil || s.runner == nil || ctx == nil || r.TenantID == uuid.Nil || !agentuxDemoClean(r.ServiceSubject, 256) || !agentuxDemoClean(r.ProjectID, 256) || !agentuxDemoClean(r.GrantedBy, 256) || r.Revision == 0 || r.RecordedAt.IsZero() {
		return ErrSupportInboxInvalid
	}
	return s.runner.RunTenantTx(ctx, r.TenantID, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "support-grant:"+r.TenantID.String()+":"+r.ServiceSubject+":"+r.ProjectID); err != nil {
			return err
		}
		var revision uint64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(max(revision),0) FROM support_project_grant WHERE tenant_id=$1 AND service_subject=$2 AND project_id=$3`, r.TenantID, r.ServiceSubject, r.ProjectID).Scan(&revision); err != nil {
			return err
		}
		if revision+1 != r.Revision {
			return ErrSupportInboxReplay
		}
		_, err := tx.Exec(ctx, `INSERT INTO support_project_grant(tenant_id,service_subject,project_id,revision,active,granted_by,recorded_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, r.TenantID, r.ServiceSubject, r.ProjectID, r.Revision, r.Active, r.GrantedBy, r.RecordedAt.UTC())
		return err
	})
}

func (s *SupportInboxStore) SupportProjectCreateAllowed(ctx context.Context, tenant uuid.UUID, subject, project string) (bool, error) {
	if s == nil || s.runner == nil || ctx == nil || tenant == uuid.Nil || !agentuxDemoClean(subject, 256) || !agentuxDemoClean(project, 256) {
		return false, ErrSupportInboxInvalid
	}
	var active bool
	err := s.runner.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT active FROM support_project_grant WHERE tenant_id=$1 AND service_subject=$2 AND project_id=$3 ORDER BY revision DESC LIMIT 1`, tenant, subject, project).Scan(&active)
	})
	if errors.Is(err, dbport.ErrNoRows) {
		return false, nil
	}
	return active, err
}
