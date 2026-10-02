package agentpersonastore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var ErrIconDenied = errors.New("agentpersonastore: icon change denied")

type PersonaIcon struct {
	Value    agenticon.Value `json:"value"`
	Revision int64           `json:"revision"`
}

// IconAdministrator checks a trusted current administrator grant. An HTTP
// request cannot provide this dependency or assert administrator status.
type IconAdministrator interface {
	AuthorizeAgentIconAdministrator(context.Context, values.TenantId, string) error
}

// IconInput reads only the semantic fields of an immutable profile.
func IconInput(version PersonaVersion) agenticon.Input {
	var profile struct {
		Purpose      string `json:"purpose"`
		Description  string `json:"description"`
		Instructions string `json:"instructions"`
		SkillPins    []struct {
			ID string `json:"id"`
		} `json:"skill_pins"`
		Skills []struct {
			ID string `json:"id"`
		} `json:"skills"`
	}
	_ = json.Unmarshal(version.Profile, &profile)
	input := agenticon.Input{Name: version.DisplayName, Description: profile.Purpose + " " + profile.Description, Instructions: profile.Instructions}
	for _, skill := range append(profile.SkillPins, profile.Skills...) {
		input.Skills = append(input.Skills, skill.ID)
	}
	return input
}

// borrowedIconTransaction lets the existing draft writer participate in the
// icon owner's transaction. Only the outer operation commits or rolls back.
type borrowedIconTransaction struct{ dbport.Tx }

func (tx borrowedIconTransaction) Begin(context.Context) (dbport.Tx, error) { return tx, nil }
func (borrowedIconTransaction) Commit(context.Context) error                { return nil }
func (borrowedIconTransaction) Rollback(context.Context) error              { return nil }

func (s *TenantStore) lockIcons(ctx context.Context, tx dbport.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 69))`, s.tenantID.String())
	return err
}

// CreateDraftWithIcon generates and persists an identity in the same transaction
// as the profile, owners and lifecycle event, serializing workspace collisions.
func (s *TenantStore) CreateDraftWithIcon(ctx context.Context, version PersonaVersion, owner, steward PersonaOwner, actor string, at time.Time) error {
	if s == nil {
		return ErrInvalid
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = s.lockIcons(ctx, tx); err != nil {
		return err
	}
	draftStore := *s
	draftStore.db = borrowedIconTransaction{tx}
	if err = draftStore.CreateDraft(ctx, version, owner, steward, actor, at); err != nil {
		return err
	}
	if _, err = s.ensureIcon(ctx, tx, version, actor, at, "create"); err != nil {
		return err
	}
	return commit(ctx, tx)
}

func (s *TenantStore) usedIcons(ctx context.Context, tx dbport.Tx, except string) ([]agenticon.Value, error) {
	rows, err := tx.Query(ctx, `SELECT icon FROM persona_icons WHERE tenant_id=$1 AND persona_id<>$2 ORDER BY persona_id`, s.tenantID, except)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var used []agenticon.Value
	for rows.Next() {
		var raw []byte
		var value agenticon.Value
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &value); err != nil || !value.Valid() {
			continue
		}
		used = append(used, value)
	}
	return used, rows.Err()
}

func (s *TenantStore) ensureIcon(ctx context.Context, tx dbport.Tx, version PersonaVersion, actor string, at time.Time, action string) (bool, error) {
	var present bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM persona_icons WHERE tenant_id=$1 AND persona_id=$2)`, s.tenantID, version.PersonaID).Scan(&present); err != nil {
		return false, err
	}
	used, err := s.usedIcons(ctx, tx, version.PersonaID)
	if err != nil {
		return false, err
	}
	if present {
		var raw []byte
		var revision int64
		var current agenticon.Value
		if err = tx.QueryRow(ctx, `SELECT icon,revision FROM persona_icons WHERE tenant_id=$1 AND persona_id=$2 FOR UPDATE`, s.tenantID, version.PersonaID).Scan(&raw, &revision); err != nil {
			return false, err
		}
		valid := json.Unmarshal(raw, &current) == nil && current.Valid()
		collided := false
		for _, other := range used {
			if current.Glyph == other.Glyph && current.Shape == other.Shape && current.Foreground == other.Foreground && current.Background == other.Background {
				collided = true
			}
		}
		if valid && !collided {
			return false, nil
		}
		value, available := agenticon.Unique(IconInput(version), used, 0)
		if !available {
			return false, ErrConflict
		}
		next, err := json.Marshal(value)
		if err != nil {
			return false, err
		}
		if _, err = tx.Exec(ctx, `UPDATE persona_icons SET icon=$3::jsonb,initial_icon=$3::jsonb,revision=revision+1 WHERE tenant_id=$1 AND persona_id=$2`, s.tenantID, version.PersonaID, string(next)); err != nil {
			return false, err
		}
		return true, s.iconEvent(ctx, tx, version.PersonaID, revision+1, "regenerate", actor, at, raw, next)
	}

	if err != nil {
		return false, err
	}
	value, _ := agenticon.Unique(IconInput(version), used, 0)
	raw, err := json.Marshal(value)
	if err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO persona_icons(tenant_id,persona_id,icon,initial_icon,revision) VALUES($1,$2,$3::jsonb,$3::jsonb,1)`, s.tenantID, version.PersonaID, string(raw)); err != nil {
		return false, err
	}
	if err = s.iconEvent(ctx, tx, version.PersonaID, 1, action, actor, at, nil, raw); err != nil {
		return false, err
	}
	return true, nil
}

func (s *TenantStore) iconEvent(ctx context.Context, tx dbport.Tx, id string, revision int64, action, actor string, at time.Time, previous, value []byte) error {
	var prior any
	if previous != nil {
		prior = string(previous)
	}
	_, err := tx.Exec(ctx, `INSERT INTO persona_icon_events(tenant_id,persona_id,revision,action,actor_id,occurred_at,previous_icon,icon) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$8::jsonb)`, s.tenantID, id, revision, action, actor, at.UTC(), prior, string(value))
	return err
}

func (s *TenantStore) GetIcon(ctx context.Context, id string) (PersonaIcon, error) {
	if s == nil || strings.TrimSpace(id) == "" {
		return PersonaIcon{}, ErrInvalid
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return PersonaIcon{}, err
	}
	defer tx.Rollback(ctx)
	var raw []byte
	var icon PersonaIcon
	err = tx.QueryRow(ctx, `SELECT icon,revision FROM persona_icons WHERE tenant_id=$1 AND persona_id=$2`, s.tenantID, id).Scan(&raw, &icon.Revision)
	if errors.Is(err, dbport.ErrNoRows) {
		return PersonaIcon{}, ErrNotFound
	}
	if err != nil {
		return PersonaIcon{}, err
	}
	if json.Unmarshal(raw, &icon.Value) != nil || !icon.Value.Valid() {
		return PersonaIcon{}, ErrInvalid
	}
	return icon, commit(ctx, tx)
}

// BackfillIcons fills every missing identity from its latest profile, atomically
// and idempotently. It is an operator preparation action, never a page read.
func (s *TenantStore) BackfillIcons(ctx context.Context, actor string, at time.Time) (int, error) {
	if s == nil || strings.TrimSpace(actor) == "" || at.IsZero() {
		return 0, ErrInvalid
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if err = s.lockIcons(ctx, tx); err != nil {
		return 0, err
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (persona_id) persona_id,version,agent_version,handle,display_name,profile,content_digest,created_at FROM persona_versions WHERE tenant_id=$1 ORDER BY persona_id,version DESC`, s.tenantID)
	if err != nil {
		return 0, err
	}
	var versions []PersonaVersion
	for rows.Next() {
		v, scanErr := scanVersion(rows, s.tenant)
		if scanErr != nil {
			rows.Close()
			return 0, scanErr
		}
		versions = append(versions, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	count := 0
	for _, version := range versions {
		added, err := s.ensureIcon(ctx, tx, version, actor, at, "backfill")
		if err != nil {
			return 0, err
		}
		if added {
			count++
		}
	}
	return count, commit(ctx, tx)
}

// ChangeIcon checks persisted ownership or a trusted administrator authority
// before side effects. ExpectedRevision fences stale commands and replays.
func (s *TenantStore) ChangeIcon(ctx context.Context, id string, expected int64, action, actor string, at time.Time, admins IconAdministrator) (PersonaIcon, error) {
	return s.changeIcon(ctx, id, expected, action, actor, at, admins, false)
}

// PreviewIcon runs the identical authorization and selection without writing.
func (s *TenantStore) PreviewIcon(ctx context.Context, id string, expected int64, action, actor string, at time.Time, admins IconAdministrator) (PersonaIcon, error) {
	return s.changeIcon(ctx, id, expected, action, actor, at, admins, true)
}

func (s *TenantStore) changeIcon(ctx context.Context, id string, expected int64, action, actor string, at time.Time, admins IconAdministrator, preview bool) (PersonaIcon, error) {
	if s == nil || strings.TrimSpace(id) == "" || strings.TrimSpace(actor) == "" || expected < 1 || at.IsZero() {
		return PersonaIcon{}, ErrInvalid
	}
	if action != "regenerate" && action != "shuffle" && action != "reset" && action != "undo" {
		return PersonaIcon{}, ErrInvalid
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return PersonaIcon{}, err
	}
	defer tx.Rollback(ctx)
	if err = s.lockIcons(ctx, tx); err != nil {
		return PersonaIcon{}, err
	}
	owners, err := tx.Query(ctx, `SELECT principal_id FROM persona_owners WHERE tenant_id=$1 AND persona_id=$2 FOR UPDATE`, s.tenantID, id)
	if err != nil {
		return PersonaIcon{}, err
	}
	owned := false
	for owners.Next() {
		var principal string
		if err = owners.Scan(&principal); err != nil {
			owners.Close()
			return PersonaIcon{}, err
		}
		owned = owned || principal == actor
	}
	err = owners.Err()
	owners.Close()
	if err != nil {
		return PersonaIcon{}, err
	}
	if !owned && (admins == nil || admins.AuthorizeAgentIconAdministrator(ctx, s.tenant, actor) != nil) {
		return PersonaIcon{}, ErrIconDenied
	}
	var raw, initial []byte
	var current PersonaIcon
	err = tx.QueryRow(ctx, `SELECT icon,initial_icon,revision FROM persona_icons WHERE tenant_id=$1 AND persona_id=$2 FOR UPDATE`, s.tenantID, id).Scan(&raw, &initial, &current.Revision)
	if errors.Is(err, dbport.ErrNoRows) {
		return PersonaIcon{}, ErrNotFound
	}
	if err != nil {
		return PersonaIcon{}, err
	}
	if current.Revision != expected {
		return PersonaIcon{}, ErrConflict
	}
	if json.Unmarshal(raw, &current.Value) != nil || !current.Value.Valid() {
		return PersonaIcon{}, ErrInvalid
	}
	value := current.Value
	switch action {
	case "reset":
		if json.Unmarshal(initial, &value) != nil || !value.Valid() {
			return PersonaIcon{}, ErrInvalid
		}
	case "undo":
		var previous []byte
		if err = tx.QueryRow(ctx, `SELECT previous_icon FROM persona_icon_events WHERE tenant_id=$1 AND persona_id=$2 AND revision=$3`, s.tenantID, id, expected).Scan(&previous); err != nil {
			return PersonaIcon{}, err
		}
		if json.Unmarshal(previous, &value) != nil || !value.Valid() {
			return PersonaIcon{}, ErrConflict
		}
	default:
		version, err := scanVersion(tx.QueryRow(ctx, `SELECT persona_id,version,agent_version,handle,display_name,profile,content_digest,created_at FROM persona_versions WHERE tenant_id=$1 AND persona_id=$2 ORDER BY version DESC LIMIT 1`, s.tenantID, id), s.tenant)
		if err != nil {
			return PersonaIcon{}, err
		}
		used, err := s.usedIcons(ctx, tx, id)
		if err != nil {
			return PersonaIcon{}, err
		}
		var available bool
		if action == "shuffle" {
			value, available = agenticon.Shuffle(current.Value, used)
		} else {
			value, available = agenticon.Unique(IconInput(version), used, 0)
		}
		if !available {
			return PersonaIcon{}, fmt.Errorf("%w: icon variations exhausted", ErrConflict)
		}
	}
	result := PersonaIcon{Value: value, Revision: expected + 1}
	if preview {
		result.Revision = expected
		return result, nil
	}
	next, err := json.Marshal(value)
	if err != nil {
		return PersonaIcon{}, err
	}
	n, err := tx.Exec(ctx, `UPDATE persona_icons SET icon=$3::jsonb,revision=revision+1 WHERE tenant_id=$1 AND persona_id=$2 AND revision=$4`, s.tenantID, id, string(next), expected)
	if err != nil {
		return PersonaIcon{}, err
	}
	if n != 1 {
		return PersonaIcon{}, ErrConflict
	}
	if err = s.iconEvent(ctx, tx, id, result.Revision, action, actor, at, raw, next); err != nil {
		return PersonaIcon{}, err
	}
	return result, commit(ctx, tx)
}
