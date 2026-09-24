// Personal document library: each person's folders, placements and stars,
// and the owner's view and removal of who can read a document. Folders and
// stars are organization only. They never grant, widen or reveal access:
// every write first proves the actor can read the document, every count
// counts only documents the actor can still read, and nobody else ever sees
// another person's folders.
package documenthubstore

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Library errors. A folder the actor does not own reads as not found.
var (
	ErrFolderNotFound    = errors.New("document folder: not found")
	ErrFolderNameInvalid = errors.New("document folder: name must be 1 to 80 characters")
	ErrFolderNameTaken   = errors.New("document folder: you already have a folder with that name")
	ErrTooManyDocuments  = errors.New("document library: at most 100 documents per move")
	ErrOwnerAccess       = errors.New("document access: the owner's access cannot be removed")
)

// MaxMoveDocuments bounds one MoveDocuments call.
const MaxMoveDocuments = 100

// Folder is one of a person's folders with the number of documents in it
// that the person can still read.
type Folder struct {
	ID, Name      string
	DocumentCount int
	CreatedAt     time.Time
}

// Library is a person's folders and collection counts, all counting only
// documents the person can read.
type Library struct {
	Folders                          []Folder
	All, Mine, SharedWithMe, Starred int
}

// AccessEntry is one person who can read a document. Role is "owner",
// "commenter" or "viewer"; the owner is never removable.
type AccessEntry struct {
	SubjectKind, SubjectID, Role string
	Removable                    bool
}

// NormalizeFolderName trims a folder name and checks its length.
func NormalizeFolderName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 80 || !utf8.ValidString(name) {
		return "", ErrFolderNameInvalid
	}
	return name, nil
}

// GetLibrary returns the actor's folders, sorted by case-folded
// name, with the collection counts.
func (s *Store) GetLibrary(ctx context.Context, tenantID, actorID string) (Library, error) {
	if actorID == "" {
		return Library{}, ErrDenied
	}
	var lib Library
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*),
				count(*) FILTER (WHERE d.owner_id=$2),
				count(*) FILTER (WHERE d.owner_id<>$2),
				count(*) FILTER (WHERE st.document_id IS NOT NULL)
			FROM document d
			LEFT JOIN document_star st ON st.tenant_id=d.tenant_id AND st.owner_id=$2 AND st.document_id=d.id
			WHERE d.tenant_id=$1 AND `+readableDocumentSQL, tenantID, actorID).Scan(&lib.All, &lib.Mine, &lib.SharedWithMe, &lib.Starred); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT f.id, f.name, f.created_at,
				(SELECT count(*) FROM document_folder_item fi JOIN document d ON d.tenant_id=fi.tenant_id AND d.id=fi.document_id
					WHERE fi.tenant_id=f.tenant_id AND fi.owner_id=$2 AND fi.folder_id=f.id AND `+readableDocumentSQL+`)
			FROM document_folder f WHERE f.tenant_id=$1 AND f.owner_id=$2
			ORDER BY lower(f.name), f.id`, tenantID, actorID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var f Folder
			if err := rows.Scan(&f.ID, &f.Name, &f.CreatedAt, &f.DocumentCount); err != nil {
				return err
			}
			lib.Folders = append(lib.Folders, f)
		}
		return rows.Err()
	})
	if err != nil {
		return Library{}, err
	}
	if lib.Folders == nil {
		lib.Folders = []Folder{}
	}
	return lib, nil
}

// CreateFolder adds one of the actor's folders. Names are unique per person
// without regard to case.
func (s *Store) CreateFolder(ctx context.Context, tenantID, actorID, name string) (Folder, error) {
	if actorID == "" {
		return Folder{}, ErrDenied
	}
	name, err := NormalizeFolderName(name)
	if err != nil {
		return Folder{}, err
	}
	folder := Folder{ID: "docf-" + uuid.NewString(), Name: name}
	err = s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if err := folderNameFreeTx(ctx, tx, tenantID, actorID, name, ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `INSERT INTO document_folder(id,tenant_id,owner_id,name) VALUES($1,$2,$3,$4) RETURNING created_at`,
			folder.ID, tenantID, actorID, name).Scan(&folder.CreatedAt)
	})
	if err != nil {
		return Folder{}, folderWriteError(err)
	}
	return folder, nil
}

// RenameFolder renames one of the actor's own folders.
func (s *Store) RenameFolder(ctx context.Context, tenantID, actorID, folderID, name string) error {
	if actorID == "" {
		return ErrDenied
	}
	name, err := NormalizeFolderName(name)
	if err != nil {
		return err
	}
	err = s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if err := folderNameFreeTx(ctx, tx, tenantID, actorID, name, folderID); err != nil {
			return err
		}
		n, err := tx.Exec(ctx, `UPDATE document_folder SET name=$4, updated_at=now() WHERE tenant_id=$1 AND owner_id=$2 AND id=$3`, tenantID, actorID, folderID, name)
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrFolderNotFound
		}
		return nil
	})
	return folderWriteError(err)
}

// DeleteFolder removes one of the actor's folders and its placements. The
// documents themselves are untouched and return to the actor's unfiled
// documents.
func (s *Store) DeleteFolder(ctx context.Context, tenantID, actorID, folderID string) error {
	if actorID == "" {
		return ErrDenied
	}
	return s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `DELETE FROM document_folder WHERE tenant_id=$1 AND owner_id=$2 AND id=$3`, tenantID, actorID, folderID)
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrFolderNotFound
		}
		return nil
	})
}

// MoveDocuments files documents into one of the actor's folders, or unfiles
// them when folderID is empty. Every document must be readable by the
// actor, or nothing moves and the call is denied. A document sits in at
// most one of the actor's folders, so filing it moves it.
func (s *Store) MoveDocuments(ctx context.Context, tenantID, actorID string, docIDs []string, folderID string) error {
	if actorID == "" {
		return ErrDenied
	}
	ids := uniqueNonEmpty(docIDs)
	if len(ids) > MaxMoveDocuments {
		return ErrTooManyDocuments
	}
	if len(ids) == 0 {
		return nil
	}
	return s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if folderID != "" {
			var owned bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM document_folder WHERE tenant_id=$1 AND owner_id=$2 AND id=$3)`, tenantID, actorID, folderID).Scan(&owned); err != nil {
				return err
			}
			if !owned {
				return ErrFolderNotFound
			}
		}
		var readable int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM document d WHERE d.tenant_id=$1 AND d.id=ANY($3::text[]) AND `+readableDocumentSQL, tenantID, actorID, ids).Scan(&readable); err != nil {
			return err
		}
		if readable != len(ids) {
			return ErrDenied
		}
		if folderID == "" {
			_, err := tx.Exec(ctx, `DELETE FROM document_folder_item WHERE tenant_id=$1 AND owner_id=$2 AND document_id=ANY($3::text[])`, tenantID, actorID, ids)
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO document_folder_item(tenant_id,owner_id,document_id,folder_id)
			SELECT $1, $2, x, $4 FROM unnest($3::text[]) x
			ON CONFLICT (tenant_id,owner_id,document_id) DO UPDATE SET folder_id=EXCLUDED.folder_id, filed_at=now()`,
			tenantID, actorID, ids, folderID)
		return err
	})
}

// SetStarred stars or unstars one readable document for the actor. Both
// directions are idempotent.
func (s *Store) SetStarred(ctx context.Context, tenantID, actorID, docID string, starred bool) error {
	if actorID == "" || docID == "" {
		return ErrDenied
	}
	return s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if err := readableTx(ctx, tx, tenantID, actorID, docID); err != nil {
			return err
		}
		if starred {
			_, err := tx.Exec(ctx, `INSERT INTO document_star(tenant_id,owner_id,document_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, tenantID, actorID, docID)
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM document_star WHERE tenant_id=$1 AND owner_id=$2 AND document_id=$3`, tenantID, actorID, docID)
		return err
	})
}

// ListAccess returns the owner and every person with a live read allow and
// no live read deny, to the owner or a holder of the manage action. Anyone
// else, and any unknown document, gets ErrDenied.
func (s *Store) ListAccess(ctx context.Context, tenantID, actorID, docID string) ([]AccessEntry, error) {
	var out []AccessEntry
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		owner, err := accessManagerTx(ctx, tx, tenantID, actorID, docID, false)
		if err != nil {
			return err
		}
		out = append(out, AccessEntry{SubjectKind: "person", SubjectID: owner, Role: "owner"})
		rows, err := tx.Query(ctx, `SELECT DISTINCT g.subject_id,
				EXISTS (SELECT 1 FROM document_grant c WHERE c.tenant_id=g.tenant_id AND c.document_id=g.document_id AND c.subject_kind='person' AND c.subject_id=g.subject_id AND c.action='comment' AND c.effect='allow' AND c.revoked=false AND (c.expires_at IS NULL OR c.expires_at>now()))
				AND NOT EXISTS (SELECT 1 FROM document_grant c WHERE c.tenant_id=g.tenant_id AND c.document_id=g.document_id AND c.subject_kind='person' AND c.subject_id=g.subject_id AND c.action='comment' AND c.effect='deny' AND c.revoked=false AND (c.expires_at IS NULL OR c.expires_at>now()))
			FROM document_grant g
			WHERE g.tenant_id=$1 AND g.document_id=$2 AND g.subject_kind='person' AND g.subject_id<>$3
				AND g.action='read' AND g.effect='allow' AND g.revoked=false AND (g.expires_at IS NULL OR g.expires_at>now())
				AND NOT EXISTS (SELECT 1 FROM document_grant deny WHERE deny.tenant_id=g.tenant_id AND deny.document_id=g.document_id AND deny.subject_kind='person' AND deny.subject_id=g.subject_id AND deny.action='read' AND deny.effect='deny' AND deny.revoked=false AND (deny.expires_at IS NULL OR deny.expires_at>now()))
			ORDER BY g.subject_id`, tenantID, docID, owner)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var subject string
			var commenter bool
			if err := rows.Scan(&subject, &commenter); err != nil {
				return err
			}
			role := RoleViewer
			if commenter {
				role = RoleCommenter
			}
			out = append(out, AccessEntry{SubjectKind: "person", SubjectID: subject, Role: role, Removable: true})
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RevokePersonAccess revokes every live read and comment allow one person
// holds on a document. Only an actor holding the manage action may do it;
// anyone else gets ErrDenied. The owner cannot be removed. Revoking a
// person with no live allows succeeds and changes nothing.
func (s *Store) RevokePersonAccess(ctx context.Context, tenantID, actorID, docID, subjectID string) error {
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		return ErrDenied
	}
	return s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		owner, err := accessManagerTx(ctx, tx, tenantID, actorID, docID, true)
		if err != nil {
			return err
		}
		if subjectID == owner {
			return ErrOwnerAccess
		}
		rows, err := tx.Query(ctx, `SELECT id FROM document_grant WHERE tenant_id=$1 AND document_id=$2 AND subject_kind='person' AND subject_id=$3 AND action IN ('read','comment') AND effect='allow' AND revoked=false ORDER BY created_at,id`, tenantID, docID, subjectID)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, id := range ids {
			if err := revokeGrantTx(ctx, tx, tenantID, id, actorID); err != nil {
				return err
			}
		}
		return nil
	})
}

// accessManagerTx admits the owner (for reads) or a live manage holder and
// returns the document owner. Unknown, disposed and non-personal documents
// are denied like any other refusal. requireManage makes the owner pass the
// manage check too, so an owner under a manage deny cannot change access.
func accessManagerTx(ctx context.Context, tx dbport.Tx, tenantID, actorID, docID string, requireManage bool) (string, error) {
	if actorID == "" || docID == "" {
		return "", ErrDenied
	}
	var owner, home, lifecycle string
	if err := tx.QueryRow(ctx, `SELECT owner_id,home,lifecycle FROM document WHERE tenant_id=$1 AND id=$2`, tenantID, docID).Scan(&owner, &home, &lifecycle); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return "", ErrDenied
		}
		return "", err
	}
	if home != "PERSONAL" || lifecycle == "DISPOSED" {
		return "", ErrDenied
	}
	if owner == actorID && !requireManage {
		return owner, nil
	}
	if err := authorizeTx(ctx, tx, tenantID, docID, "person", actorID, ActionManage); err != nil {
		return "", err
	}
	return owner, nil
}

// readableTx denies a document the actor cannot currently read.
func readableTx(ctx context.Context, tx dbport.Tx, tenantID, actorID, docID string) error {
	var readable bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM document d WHERE d.tenant_id=$1 AND d.id=$3 AND `+readableDocumentSQL+`)`, tenantID, actorID, docID).Scan(&readable); err != nil {
		return err
	}
	if !readable {
		return ErrDenied
	}
	return nil
}

func folderNameFreeTx(ctx context.Context, tx dbport.Tx, tenantID, actorID, name, exceptID string) error {
	var taken bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM document_folder WHERE tenant_id=$1 AND owner_id=$2 AND lower(name)=lower($3) AND id<>$4)`, tenantID, actorID, name, exceptID).Scan(&taken); err != nil {
		return err
	}
	if taken {
		return ErrFolderNameTaken
	}
	return nil
}

// folderWriteError maps a concurrent duplicate name, which the unique index
// catches after the pre-check passed, to ErrFolderNameTaken.
func folderWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrFolderNameTaken
	}
	return err
}

func uniqueNonEmpty(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// Placement is the actor's own organization of one document, plus the
// number of people other than the owner who can read it. Callers show the
// reader count only to someone who may manage access.
type Placement struct {
	Starred     bool
	FolderID    string
	ReaderCount int
}

// DocumentPlacement returns the actor's star and folder for one readable
// document; an unreadable document is ErrDenied.
func (s *Store) DocumentPlacement(ctx context.Context, tenantID, actorID, docID string) (Placement, error) {
	if actorID == "" || docID == "" {
		return Placement{}, ErrDenied
	}
	var out Placement
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if err := readableTx(ctx, tx, tenantID, actorID, docID); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT
				EXISTS (SELECT 1 FROM document_star WHERE tenant_id=$1 AND owner_id=$2 AND document_id=$3),
				COALESCE((SELECT folder_id FROM document_folder_item WHERE tenant_id=$1 AND owner_id=$2 AND document_id=$3), ''),
				(SELECT count(DISTINCT g.subject_id) FROM document d JOIN document_grant g ON g.tenant_id=d.tenant_id AND g.document_id=d.id
					WHERE d.tenant_id=$1 AND d.id=$3 AND g.subject_kind='person' AND g.subject_id<>d.owner_id AND g.action='read' AND g.effect='allow' AND g.revoked=false AND (g.expires_at IS NULL OR g.expires_at>now())
					AND NOT EXISTS (SELECT 1 FROM document_grant deny WHERE deny.tenant_id=g.tenant_id AND deny.document_id=g.document_id AND deny.subject_kind='person' AND deny.subject_id=g.subject_id AND deny.action='read' AND deny.effect='deny' AND deny.revoked=false AND (deny.expires_at IS NULL OR deny.expires_at>now())))`,
			tenantID, actorID, docID).Scan(&out.Starred, &out.FolderID, &out.ReaderCount)
	})
	if err != nil {
		return Placement{}, err
	}
	return out, nil
}
