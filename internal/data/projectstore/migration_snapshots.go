package projectstore

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	projectdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/project"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
)

// ActiveTaskSnapshots reads the current project-owned task state needed by a
// workflow migration preview. One SQL statement gives a consistent snapshot;
// rows are ordered by stable task ID and the limit is capped at max+1 so an
// oversized project fails closed without returning partial task data.
func (s *Store) ActiveTaskSnapshots(ctx context.Context, tenantID, projectID string) ([]projectworkflow.TaskSnapshot, error) {
	if tenantID == "" || projectID == "" {
		return nil, ErrInvalidRecord
	}
	out := make([]projectworkflow.TaskSnapshot, 0)
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		limit := projectworkflow.MaxTaskSnapshotsPerPreview + 1
		rows, err := tx.Query(ctx, `SELECT p.id,t.id,t.type_id,t.status_id,t.revision,t.fields_json
			FROM project p
			LEFT JOIN LATERAL (
				SELECT id,type_id,status_id,revision,fields_json
				FROM project_task
				WHERE tenant_id=p.tenant_id AND project_id=p.id AND archived=false
				ORDER BY id
				LIMIT $3
			) t ON true
			WHERE p.tenant_id=$1 AND p.id=$2
			ORDER BY t.id`, tenantID, projectID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		foundProject := false
		for rows.Next() {
			var foundID string
			var id, typeID, statusID *string
			var revision *int64
			var fields []byte
			if err := rows.Scan(&foundID, &id, &typeID, &statusID, &revision, &fields); err != nil {
				return err
			}
			foundProject = foundID == projectID
			if id == nil {
				continue
			}
			fieldValues, err := DecodeTaskFieldValues(fields)
			if err != nil {
				return err
			}
			out = append(out, projectworkflow.TaskSnapshot{ID: *id, TypeID: deref(typeID), StatusID: deref(statusID), Revision: derefInt64(revision), Fields: fieldValues})
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if !foundProject {
			return ErrNotFound
		}
		if len(out) > projectworkflow.MaxTaskSnapshotsPerPreview {
			return projectworkflow.ErrSnapshotLimitExceeded
		}
		return nil
	})
	if errors.Is(err, projectworkflow.ErrSnapshotLimitExceeded) {
		return nil, projectworkflow.ErrSnapshotLimitExceeded
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DecodeTaskFieldValues gives both preview and publication the same canonical
// values, regardless of whether a task was written using field edits or raw
// JSON values. The returned map does not alias the storage buffer.
func DecodeTaskFieldValues(fields []byte) (map[string]json.RawMessage, error) {
	var storedValues map[string]json.RawMessage
	if len(fields) > 0 {
		if err := json.Unmarshal(fields, &storedValues); err != nil {
			return nil, err
		}
	}
	fieldValues := make(map[string]json.RawMessage, len(storedValues))
	for fieldID, raw := range storedValues {
		var edit projectdomain.TaskFieldEdit
		if err := json.Unmarshal(raw, &edit); err == nil && edit.FieldID == fieldID && edit.CanonicalValue != "" {
			value := json.RawMessage(edit.CanonicalValue)
			textual := false
			switch projectworkflow.FieldType(edit.Type) {
			case projectworkflow.FieldText, projectworkflow.FieldDate, projectworkflow.FieldEnum, projectworkflow.FieldPerson, projectworkflow.FieldLink:
				textual = true
			}
			var decodedString string
			if !json.Valid(value) || (textual && json.Unmarshal(value, &decodedString) != nil) {
				encoded, marshalErr := json.Marshal(edit.CanonicalValue)
				if marshalErr != nil {
					return nil, marshalErr
				}
				value = encoded
			}
			fieldValues[fieldID] = append(json.RawMessage(nil), value...)
		} else {
			fieldValues[fieldID] = append(json.RawMessage(nil), raw...)
		}
	}
	return fieldValues, nil
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func derefInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
