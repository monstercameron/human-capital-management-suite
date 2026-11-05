package agentpersonastore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
)

// SetIconImage stores an owner's own image over the agent's generated icon.
// The upload is validated and re-encoded before any write; the generated
// value underneath is kept, so reset and undo keep working.
func (s *TenantStore) SetIconImage(ctx context.Context, id string, expected int64, actor string, at time.Time, admins IconAdministrator, content []byte, declaredType string) (PersonaIcon, error) {
	if s == nil || strings.TrimSpace(id) == "" || strings.TrimSpace(actor) == "" || expected < 1 || at.IsZero() {
		return PersonaIcon{}, ErrInvalid
	}
	proxy, err := agenticon.DecodeUpload(content, declaredType)
	if err != nil {
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
	raw, _, currentImage, current, err := s.iconWrite(ctx, tx, id, expected, actor, admins)
	if err != nil {
		return PersonaIcon{}, err
	}
	n, err := tx.Exec(ctx, `UPDATE persona_icons SET revision=revision+1,image=$3,image_media_type='image/jpeg' WHERE tenant_id=$1 AND persona_id=$2 AND revision=$4`, s.tenantID, id, proxy, expected)
	if err != nil {
		return PersonaIcon{}, err
	}
	if n != 1 {
		return PersonaIcon{}, ErrConflict
	}
	result := PersonaIcon{Value: current.Value, Revision: expected + 1, HasImage: true, ImageMediaType: "image/jpeg"}
	if err = s.iconEvent(ctx, tx, id, result.Revision, "upload", actor, at, raw, raw, currentImage, proxy); err != nil {
		return PersonaIcon{}, err
	}
	return result, commit(ctx, tx)
}

// GetIconImage serves the stored upload for an icon. It reports ErrNotFound
// when the agent has no icon or no upload.
func (s *TenantStore) GetIconImage(ctx context.Context, id string) (content []byte, mediaType string, revision int64, err error) {
	if s == nil || strings.TrimSpace(id) == "" {
		return nil, "", 0, ErrInvalid
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return nil, "", 0, err
	}
	defer tx.Rollback(ctx)
	var image []byte
	var media string
	err = tx.QueryRow(ctx, `SELECT image,COALESCE(image_media_type,''),revision FROM persona_icons WHERE tenant_id=$1 AND persona_id=$2`, s.tenantID, id).Scan(&image, &media, &revision)
	if errors.Is(err, dbport.ErrNoRows) || len(image) == 0 {
		if err == nil {
			err = dbport.ErrNoRows
		}
		return nil, "", 0, ErrNotFound
	}
	if err != nil {
		return nil, "", 0, err
	}
	if err = commit(ctx, tx); err != nil {
		return nil, "", 0, err
	}
	return image, media, revision, nil
}
