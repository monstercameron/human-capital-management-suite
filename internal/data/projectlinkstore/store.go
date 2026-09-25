// Package projectlinkstore persists only typed references owned by project
// tasks. Target previews and source content remain in their owning systems.
package projectlinkstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectlink"
)

var (
	ErrInvalidRecord = errors.New("invalid project link record")
	ErrNotFound      = errors.New("project link not found")
	ErrConflict      = errors.New("project link id conflicts with existing link")
	ErrTenantScope   = errors.New("project link transaction tenant does not match record")
)

type LinkRecord struct {
	ID, TenantID, ProjectID, TaskID string
	Reference                       projectlink.Reference
}

type Store struct{ projects *projectstore.Store }

func New(projects *projectstore.Store) (*Store, error) {
	if projects == nil {
		return nil, errors.New("project store is required")
	}
	return &Store{projects: projects}, nil
}

// Add inserts a typed target reference. Repeating the same stable link ID and
// values succeeds; reusing that ID for different task or target values fails.
func (s *Store) Add(ctx context.Context, link LinkRecord) error {
	if s == nil || s.projects == nil || !validRecord(link) {
		return ErrInvalidRecord
	}
	return s.projects.RunTenantTx(ctx, link.TenantID, func(tx dbport.Tx) error { return AddTx(ctx, tx, link) })
}

// AddTx inserts a reference inside the caller's transaction. It does not
// commit, roll back, or change the tenant binding; the transaction must
// already be scoped to link.TenantID.
func AddTx(ctx context.Context, tx dbport.Tx, link LinkRecord) error {
	if tx == nil || !validRecord(link) {
		return ErrInvalidRecord
	}
	var tenantID string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(current_setting('hcmnext.tenant_id', true),'')`).Scan(&tenantID); err != nil {
		return err
	}
	if tenantID != link.TenantID {
		return ErrTenantScope
	}
	r := link.Reference
	_, err := tx.Exec(ctx, `INSERT INTO project_task_link(tenant_id,id,project_id,task_id,kind,target_id,conversation_id,version_id,scope_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT (tenant_id,id) DO NOTHING`,
		link.TenantID, link.ID, link.ProjectID, link.TaskID, string(r.Kind), r.ID, nullable(r.ConversationID), nullable(r.Version), nullable(r.ScopeID))
	if err != nil {
		return err
	}
	var projectID, taskID, kind, targetID string
	var conversationID, versionID, scopeID *string
	err = tx.QueryRow(ctx, `SELECT project_id,task_id,kind,target_id,conversation_id,version_id,scope_id FROM project_task_link WHERE tenant_id=$1 AND id=$2`, link.TenantID, link.ID).
		Scan(&projectID, &taskID, &kind, &targetID, &conversationID, &versionID, &scopeID)
	if err != nil {
		return err
	}
	if projectID != link.ProjectID || taskID != link.TaskID || kind != string(r.Kind) || targetID != r.ID || deref(conversationID) != r.ConversationID || deref(versionID) != r.Version || deref(scopeID) != r.ScopeID {
		return ErrConflict
	}
	return nil
}

// List returns a bounded ID-ordered page for one task. afterID is an opaque
// stable link ID from the previous page, or empty for the first page.
func (s *Store) List(ctx context.Context, tenantID, projectID, taskID, afterID string, limit int32) ([]LinkRecord, error) {
	if s == nil || s.projects == nil || strings.TrimSpace(tenantID) == "" || !validID(projectID) || !validID(taskID) || limit < 1 || limit > 100 {
		return nil, ErrInvalidRecord
	}
	out := make([]LinkRecord, 0, limit)
	err := s.projects.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,kind,target_id,conversation_id,version_id,scope_id FROM project_task_link WHERE tenant_id=$1 AND project_id=$2 AND task_id=$3 AND id>$4 AND removed_at IS NULL ORDER BY id LIMIT $5`, tenantID, projectID, taskID, afterID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x LinkRecord
			var kind, target string
			var conversationID, versionID, scopeID *string
			if err := rows.Scan(&x.ID, &kind, &target, &conversationID, &versionID, &scopeID); err != nil {
				return err
			}
			x.TenantID, x.ProjectID, x.TaskID = tenantID, projectID, taskID
			x.Reference = projectlink.Reference{Kind: projectlink.Kind(kind), ID: target, ConversationID: deref(conversationID), Version: deref(versionID), ScopeID: deref(scopeID)}
			if !validRecord(x) {
				return fmt.Errorf("persisted project link is invalid: %w", ErrInvalidRecord)
			}
			out = append(out, x)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func validRecord(x LinkRecord) bool {
	return validID(x.ID) && validID(x.TenantID) && validID(x.ProjectID) && validID(x.TaskID) && x.Reference.Validate() == nil
}
func validID(v string) bool { return v != "" && v == strings.TrimSpace(v) && len(v) <= 128 }
func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}
func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// LinkedTask is one live task that links a given target.
type LinkedTask struct {
	ProjectID, TaskID, Title, StatusID string
}

// ListByTarget finds the live, unarchived tasks that link one target, the
// reverse of List. The caller must still authorize each task's project for
// the viewer before disclosing it.
func (s *Store) ListByTarget(ctx context.Context, tenantID string, kind projectlink.Kind, targetID string, limit int32) ([]LinkedTask, error) {
	if s == nil || s.projects == nil || strings.TrimSpace(tenantID) == "" || !validID(targetID) || limit < 1 || limit > 100 || (kind != projectlink.Journey && kind != projectlink.WorkItem && kind != projectlink.WorkOrder) {
		return nil, ErrInvalidRecord
	}
	out := make([]LinkedTask, 0, limit)
	err := s.projects.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT ON (l.project_id,l.task_id) l.project_id,l.task_id,t.title,t.status_id FROM project_task_link l JOIN project_task t ON t.tenant_id=l.tenant_id AND t.project_id=l.project_id AND t.id=l.task_id WHERE l.tenant_id=$1 AND l.kind=$2 AND l.target_id=$3 AND l.removed_at IS NULL AND t.archived=false ORDER BY l.project_id,l.task_id LIMIT $4`, tenantID, string(kind), targetID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x LinkedTask
			if err := rows.Scan(&x.ProjectID, &x.TaskID, &x.Title, &x.StatusID); err != nil {
				return err
			}
			out = append(out, x)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
