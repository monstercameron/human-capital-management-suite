// Package projectviewstore persists personal and project-scoped board views.
// It stores configuration only; authorization to view or mutate a project is
// enforced by the application service before this tenant-scoped boundary.
package projectviewstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectboard"
)

const MaxViewsPerProject = 20

var (
	ErrInvalidView = errors.New("invalid project board view record")
	ErrViewLimit   = errors.New("project board view limit reached")
)

// Store uses the project store's isolated database and tenant transaction.
type Store struct{ projects *projectstore.Store }

func New(projects *projectstore.Store) *Store { return &Store{projects: projects} }

// InitializeProjectTx writes the starter shared board view into a caller-owned
// project creation transaction. The caller must already have scoped the
// transaction to tenantID and inserted the project row.
func (s *Store) InitializeProjectTx(ctx context.Context, tx dbport.Tx, tenantID, projectID, actorID string) error {
	return InitializeProjectTx(ctx, tx, tenantID, projectID, actorID)
}

// InitializeProjectTx writes the starter shared board view into a caller-owned
// tenant transaction after project creation has inserted its project row.
func InitializeProjectTx(ctx context.Context, tx dbport.Tx, tenantID, projectID, actorID string) error {
	if tx == nil || tenantID == "" || projectID == "" || actorID == "" {
		return ErrInvalidView
	}
	view := projectboard.BoardView{
		ID: "default", Name: "Task board", Version: 1, Audience: projectboard.AudienceProject,
		SwimlaneValueOrder: []string{},
		Columns: []projectboard.Column{
			{ID: "todo", Label: "To do", StatusIDs: []string{"todo"}},
			{ID: "doing", Label: "In progress", StatusIDs: []string{"doing"}},
			{ID: "done", Label: "Done", StatusIDs: []string{"done"}},
		},
		Grouping:   projectboard.Grouping{Kind: projectboard.GroupNone},
		OrderBy:    projectboard.OrderTaskID,
		CardFields: []string{projectboard.CardTitle, projectboard.CardStatus, projectboard.CardAssignee, projectboard.CardPriority},
	}
	if err := projectboard.Validate(view); err != nil {
		return ErrInvalidView
	}
	raw, err := json.Marshal(view)
	if err != nil {
		return err
	}
	laneOrderJSON, err := json.Marshal(view.SwimlaneValueOrder)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO project_board_view(tenant_id,project_id,id,audience,owner_id,name,swimlane_value_order,revision,config_json) VALUES($1,$2,$3,'PROJECT','',$4,$5::jsonb,1,$6::jsonb)`, tenantID, projectID, view.ID, view.Name, laneOrderJSON, raw)
	if err != nil {
		return err
	}
	return recordMutation(ctx, tx, tenantID, projectID, actorID, view, 0)
}

// GetView reads a project view or the caller's personal view. A blank userID
// cannot read personal views. The service must authorize project membership.
func (s *Store) GetView(ctx context.Context, tenantID, projectID, userID, viewID string) (projectboard.BoardView, error) {
	if s == nil || s.projects == nil || tenantID == "" || projectID == "" || viewID == "" {
		return projectboard.BoardView{}, ErrInvalidView
	}
	var out projectboard.BoardView
	err := s.projects.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var raw, laneOrderJSON []byte
		var audience string
		var name string
		var revision int64
		err := tx.QueryRow(ctx, `SELECT audience,name,swimlane_value_order,revision,config_json FROM project_board_view WHERE tenant_id=$1 AND project_id=$2 AND id=$3 AND (audience='PROJECT' OR (audience='PERSONAL' AND owner_id=$4))`, tenantID, projectID, viewID, userID).Scan(&audience, &name, &laneOrderJSON, &revision, &raw)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return err
		}
		out.ID = viewID
		out.Name = name
		out.Audience = projectboard.Audience(audience)
		out.Version = uint64(revision)
		if err := json.Unmarshal(laneOrderJSON, &out.SwimlaneValueOrder); err != nil {
			return err
		}
		return nil
	})
	if errors.Is(err, dbport.ErrNoRows) {
		return projectboard.BoardView{}, projectstore.ErrNotFound
	}
	return out, err
}

// SaveView applies optimistic revision control. expectedVersion=0 creates a
// view; all later writes must name its current version. The input Version is
// validated but the stored version is assigned by the store.
func (s *Store) SaveView(ctx context.Context, tenantID, projectID, userID, actorID string, view projectboard.BoardView, expectedVersion uint64, idempotencyKey string) (projectboard.BoardView, error) {
	if s == nil || s.projects == nil || tenantID == "" || projectID == "" || actorID == "" || idempotencyKey == "" || view.ID == "" || expectedVersion >= uint64(^uint64(0)>>1) || projectboard.Validate(view) != nil {
		return projectboard.BoardView{}, ErrInvalidView
	}
	ownerID := ""
	switch view.Audience {
	case projectboard.AudienceProject:
		if userID != "" {
			return projectboard.BoardView{}, ErrInvalidView
		}
	case projectboard.AudiencePersonal:
		if userID == "" {
			return projectboard.BoardView{}, ErrInvalidView
		}
		ownerID = userID
	default:
		return projectboard.BoardView{}, ErrInvalidView
	}
	stored := view
	stored.Version = expectedVersion + 1
	if stored.SwimlaneValueOrder == nil {
		stored.SwimlaneValueOrder = []string{}
	}
	raw, err := json.Marshal(stored)
	if err != nil {
		return projectboard.BoardView{}, err
	}
	laneOrderJSON, err := json.Marshal(stored.SwimlaneValueOrder)
	if err != nil {
		return projectboard.BoardView{}, err
	}
	digest, err := fingerprint(struct {
		ProjectID string
		OwnerID   string
		View      projectboard.BoardView
		Expected  uint64
	}{projectID, ownerID, view, expectedVersion})
	if err != nil {
		return projectboard.BoardView{}, err
	}
	err = s.projects.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		replay, replayResult, err := claim(ctx, tx, tenantID, actorID, idempotencyKey, digest)
		if err != nil {
			return err
		}
		if replay {
			if err := json.Unmarshal(replayResult, &stored); err != nil {
				return fmt.Errorf("decode saved board view replay: %w", err)
			}
			return nil
		}
		// Serialize creates and the project-wide view cap through the owning
		// project row. This lock is held only inside the caller's tenant tx.
		var lockedProject string
		if err := tx.QueryRow(ctx, `SELECT id FROM project WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, projectID).Scan(&lockedProject); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return projectstore.ErrNotFound
			}
			return err
		}
		var current int64
		var currentAudience, currentOwner string
		findErr := tx.QueryRow(ctx, `SELECT revision,audience,owner_id FROM project_board_view WHERE tenant_id=$1 AND project_id=$2 AND id=$3 FOR UPDATE`, tenantID, projectID, view.ID).Scan(&current, &currentAudience, &currentOwner)
		if errors.Is(findErr, dbport.ErrNoRows) {
			if expectedVersion != 0 {
				return projectstore.ErrRevisionConflict
			}
			var count int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM project_board_view WHERE tenant_id=$1 AND project_id=$2`, tenantID, projectID).Scan(&count); err != nil {
				return err
			}
			if count >= MaxViewsPerProject {
				return ErrViewLimit
			}
			if _, err := tx.Exec(ctx, `INSERT INTO project_board_view(tenant_id,project_id,id,audience,owner_id,name,swimlane_value_order,revision,config_json) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,1,$8::jsonb)`, tenantID, projectID, view.ID, string(view.Audience), ownerID, stored.Name, laneOrderJSON, raw); err != nil {
				return err
			}
			if err := recordMutation(ctx, tx, tenantID, projectID, actorID, stored, 0); err != nil {
				return err
			}
			return finish(ctx, tx, tenantID, actorID, idempotencyKey, stored)
		}
		if findErr != nil {
			return findErr
		}
		if current != int64(expectedVersion) {
			return projectstore.ErrRevisionConflict
		}
		if currentAudience != string(view.Audience) || currentOwner != ownerID {
			return ErrInvalidView
		}
		_, err = tx.Exec(ctx, `UPDATE project_board_view SET audience=$4,owner_id=$5,name=$6,swimlane_value_order=$7::jsonb,revision=$8,config_json=$9::jsonb,updated_at=now() WHERE tenant_id=$1 AND project_id=$2 AND id=$3`, tenantID, projectID, view.ID, string(view.Audience), ownerID, stored.Name, laneOrderJSON, int64(stored.Version), raw)
		if err != nil {
			return err
		}
		if err := recordMutation(ctx, tx, tenantID, projectID, actorID, stored, expectedVersion); err != nil {
			return err
		}
		return finish(ctx, tx, tenantID, actorID, idempotencyKey, stored)
	})
	if err != nil {
		return projectboard.BoardView{}, err
	}
	return stored, nil
}

func recordMutation(ctx context.Context, tx dbport.Tx, tenantID, projectID, actorID string, view projectboard.BoardView, priorVersion uint64) error {
	payload, err := json.Marshal(map[string]any{"viewId": view.ID, "audience": view.Audience, "version": view.Version})
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO project_activity(tenant_id,id,project_id,aggregate_id,actor_id,origin,event_type,prior_revision,new_revision,config_version,payload) VALUES($1,$2,$3,$4,$5,'HUMAN','board_view.saved',$6,$7,0,$8::jsonb)`, tenantID, uuid.NewString(), projectID, view.ID, actorID, int64(priorVersion), int64(view.Version), payload); err != nil {
		return err
	}
	_, err = projectstore.AppendOutboxEventTx(ctx, tx, projectstore.AppendOutboxEvent{
		TenantID: tenantID, ProjectID: projectID, AggregateID: view.ID, EventType: "board_view.saved",
		SourceRevision: int64(view.Version), Classification: "INTERNAL",
		Value: map[string]any{"viewId": view.ID, "audience": view.Audience, "version": view.Version},
	})
	return err
}

// ListViews returns at most 20 views visible to the caller, ordered by ID.
func (s *Store) ListViews(ctx context.Context, tenantID, projectID, userID, afterID string, limit int32) ([]projectboard.BoardView, error) {
	if s == nil || s.projects == nil || tenantID == "" || projectID == "" || limit < 1 || limit > MaxViewsPerProject {
		return nil, ErrInvalidView
	}
	out := make([]projectboard.BoardView, 0, limit)
	err := s.projects.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,audience,name,swimlane_value_order,revision,config_json FROM project_board_view WHERE tenant_id=$1 AND project_id=$2 AND id>$3 AND (audience='PROJECT' OR (audience='PERSONAL' AND owner_id=$4)) ORDER BY id LIMIT $5`, tenantID, projectID, afterID, userID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var view projectboard.BoardView
			var audience string
			var laneOrderJSON []byte
			var revision int64
			var raw []byte
			if err := rows.Scan(&view.ID, &audience, &view.Name, &laneOrderJSON, &revision, &raw); err != nil {
				return err
			}
			if err := json.Unmarshal(raw, &view); err != nil {
				return err
			}
			view.Audience = projectboard.Audience(audience)
			view.Version = uint64(revision)
			if err := json.Unmarshal(laneOrderJSON, &view.SwimlaneValueOrder); err != nil {
				return err
			}
			out = append(out, view)
		}
		return rows.Err()
	})
	return out, err
}
