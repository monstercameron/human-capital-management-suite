// Candidate submission for HUB-006: every submit appends an immutable
// candidate version against an expected base. The base is the document tip
// the author started from; a stale base returns both versions for explicit
// merge instead of overwriting. The tip is the version no other version
// claims as parent, so conflict detection never depends on wall-clock
// order. Submits serialize on the document row lock, so concurrent
// proposers cannot fork the chain silently.
package documenthubstore

import (
	"context"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ErrVersionConflict is the sentinel for stale-base submits; errors.As for
// *VersionConflict carries both versions for explicit merge.
var ErrVersionConflict = errors.New("document version: stale expected base")

// ErrUnknownDocument is returned when a submit names no document;
// ErrUnknownBase when the expected base names no version of the document.
var (
	ErrUnknownDocument = errors.New("document version: unknown document")
	ErrUnknownBase     = errors.New("document version: unknown expected base")
)

// VersionConflict describes a stale submit: Expected is the base the author
// named, Current is the tip that superseded it. Both carry their Markdown
// so the caller can merge explicitly.
type VersionConflict struct {
	Expected, Current Version
}

func (e *VersionConflict) Error() string {
	return "document version: expected base " + e.Expected.ID + " superseded by " + e.Current.ID
}

func (e *VersionConflict) Unwrap() error { return ErrVersionConflict }

// CreatePersonalDocumentVersion appends an owner's edit to the immutable
// version chain. The expected base, ownership, and live propose grant are
// checked under the same document lock as the insert. A shared document's
// active deployment remains on its published version.
func (s *Store) CreatePersonalDocumentVersion(ctx context.Context, tenantID, docID, ownerID, expectedBase, title, markdown string) (Version, error) {
	if strings.TrimSpace(title) == "" || strings.TrimSpace(markdown) == "" || expectedBase == "" {
		return Version{}, errors.New("document version: title, body and base version are required")
	}
	var stored Version
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var owner, home, lifecycle string
		if err := tx.QueryRow(ctx, `SELECT owner_id,home,lifecycle FROM document WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, docID).Scan(&owner, &home, &lifecycle); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return ErrDenied
			}
			return err
		}
		if owner != ownerID || home != "PERSONAL" || lifecycle == "DISPOSED" {
			return ErrDenied
		}
		for _, action := range []string{ActionRead, ActionPropose} {
			if err := authorizeTx(ctx, tx, tenantID, docID, "person", ownerID, action); err != nil {
				return err
			}
		}
		tip, err := versionTip(ctx, tx, tenantID, docID)
		if err != nil {
			return err
		}
		if tip == nil {
			return ErrUnknownBase
		}
		if tip.ID != expectedBase {
			expected, err := loadVersion(ctx, tx, tenantID, docID, expectedBase)
			if err != nil {
				return err
			}
			return &VersionConflict{Expected: expected, Current: *tip}
		}
		stored, err = insertVersionTx(ctx, tx, tenantID, Version{
			DocumentID: docID, ParentID: tip.ID, CreatorID: ownerID,
			Title: title, Markdown: markdown, Locale: tip.Locale,
			Classification: tip.Classification, Renderer: tip.Renderer,
		})
		return err
	})
	if err != nil {
		return Version{}, err
	}
	return stored, nil
}

// SubmitCandidate appends v as a candidate of its document. expectedBase is
// the tip the author started from, or empty for the first version. A stale
// base fails with *VersionConflict and stores nothing.
func (s *Store) SubmitCandidate(ctx context.Context, tenantID string, v Version, expectedBase string) (Version, error) {
	if err := checkVersionHash(v); err != nil {
		return Version{}, err
	}
	var stored Version
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var locked int
		if err := tx.QueryRow(ctx, `SELECT 1 FROM document WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, v.DocumentID).Scan(&locked); err != nil {
			return ErrUnknownDocument
		}
		tip, err := versionTip(ctx, tx, tenantID, v.DocumentID)
		if err != nil {
			return err
		}
		switch {
		case tip == nil && expectedBase == "":
			v.ParentID = ""
		case tip != nil && expectedBase == tip.ID:
			v.ParentID = tip.ID
		default:
			current := Version{}
			if tip != nil {
				current = *tip
			}
			expected, err := loadVersion(ctx, tx, tenantID, v.DocumentID, expectedBase)
			if err != nil {
				return err
			}
			return &VersionConflict{Expected: expected, Current: current}
		}
		stored, err = insertVersionTx(ctx, tx, tenantID, v)
		return err
	})
	if err != nil {
		return Version{}, err
	}
	return stored, nil
}

// versionTip returns the version of the document no other version claims as
// parent, or nil when the document has no versions yet. Multiple tips mean
// a forked chain, which submits serialize to prevent.
func versionTip(ctx context.Context, tx dbport.Tx, tenantID, docID string) (*Version, error) {
	rows, err := tx.Query(ctx, `SELECT id FROM document_version WHERE tenant_id=$1 AND document_id=$2 AND id NOT IN (SELECT parent_id FROM document_version WHERE tenant_id=$1 AND document_id=$2 AND parent_id<>'')`, tenantID, docID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tips []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		tips = append(tips, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(tips) == 0 {
		return nil, nil
	}
	if len(tips) > 1 {
		return nil, errors.New("document version: forked chain")
	}
	ver, err := loadVersion(ctx, tx, tenantID, docID, tips[0])
	if err != nil {
		return nil, err
	}
	return &ver, nil
}

func loadVersion(ctx context.Context, tx dbport.Tx, tenantID, docID, id string) (Version, error) {
	var v Version
	err := tx.QueryRow(ctx, `SELECT id,document_id,parent_id,creator_id,title,locale,classification,normalized_markdown,content_hash,renderer_profile,change_note,created_at FROM document_version WHERE tenant_id=$1 AND document_id=$2 AND id=$3`,
		tenantID, docID, id).Scan(&v.ID, &v.DocumentID, &v.ParentID, &v.CreatorID, &v.Title, &v.Locale, &v.Classification, &v.Markdown, &v.Hash, &v.Renderer, &v.ChangeNote, &v.CreatedAt)
	if err != nil {
		return Version{}, ErrUnknownBase
	}
	return v, nil
}
