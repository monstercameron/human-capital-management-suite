// Version storage for HUB-004: immutable Markdown versions with
// deterministic content hashes. Normalization here is byte-level only
// (CRLF/CR to LF); Markdown parsing and sanitizing belong to HUB-005, so
// trailing spaces are preserved because they carry Markdown meaning.
package documenthubstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ErrHashMismatch is returned when a caller-supplied content hash does not
// match the normalized Markdown it claims to cover.
var ErrHashMismatch = errors.New("document version: content hash does not match normalized Markdown")

// Version is one immutable Markdown version of a document.
type Version struct {
	ID, DocumentID, ParentID, CreatorID  string
	Title, Locale, Classification        string
	Markdown, Hash, Renderer, ChangeNote string
	CreatedAt                            time.Time
}

// NormalizeMarkdown canonicalizes line endings to LF. It never trims,
// reflows or otherwise rewrites content.
func NormalizeMarkdown(src string) string {
	out := strings.ReplaceAll(src, "\r\n", "\n")
	return strings.ReplaceAll(out, "\r", "\n")
}

// HashContent returns the hex SHA-256 digest of normalized Markdown bytes.
func HashContent(normalized string) string {
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

func checkVersionHash(v Version) error {
	if v.Hash != "" && v.Hash != HashContent(NormalizeMarkdown(v.Markdown)) {
		return ErrHashMismatch
	}
	return nil
}

// CreateDocument inserts the stable identity row for a new document and
// bootstraps the owner with every action, so a new personal document is
// private to its owner by default without locking the owner out.
func (s *Store) CreateDocument(ctx context.Context, tenantID, ownerID, home string) (string, error) {
	if strings.TrimSpace(home) == "" {
		home = "PERSONAL"
	}
	if strings.TrimSpace(ownerID) == "" {
		return "", errors.New("document: owner is required")
	}
	id := "doc-" + uuid.NewString()
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO document(id,tenant_id,owner_id,home) VALUES($1,$2,$3,$4)`, id, tenantID, ownerID, home); err != nil {
			return err
		}
		return bootstrapOwnerTx(ctx, tx, tenantID, id, ownerID)
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// InsertVersion appends one immutable version. The stored row always carries
// the normalized Markdown and its digest; a caller-supplied hash that does
// not match is refused instead of stored.
func (s *Store) InsertVersion(ctx context.Context, tenantID string, v Version) (Version, error) {
	var stored Version
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var err error
		stored, err = insertVersionTx(ctx, tx, tenantID, v)
		if err != nil {
			return err
		}
		return s.enqueueIndexTx(ctx, tx, tenantID, v.DocumentID, stored.ID)
	})
	if err != nil {
		return Version{}, err
	}
	return stored, nil
}

// ReadVersion returns one version to a subject holding the read action.
// Revocation closes this path at once: the rows survive, but no new read
// passes after the grant is gone.
func (s *Store) ReadVersion(ctx context.Context, tenantID, docID, versionID, subjectKind, subjectID string) (Version, error) {
	var version Version
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if err := authorizeTx(ctx, tx, tenantID, docID, subjectKind, subjectID, ActionRead); err != nil {
			return err
		}
		var err error
		version, err = loadVersion(ctx, tx, tenantID, docID, versionID)
		if err != nil {
			return ErrDenied
		}
		return nil
	})
	if err != nil {
		return Version{}, err
	}
	return version, nil
}

// insertVersionTx is the transaction-scoped version append shared by
// InsertVersion and SubmitCandidate, so both enforce the same normalization,
// digest and tamper checks inside their own transaction.
func insertVersionTx(ctx context.Context, tx dbport.Tx, tenantID string, v Version) (Version, error) {
	if err := checkVersionHash(v); err != nil {
		return Version{}, err
	}
	v.Markdown = NormalizeMarkdown(v.Markdown)
	v.Hash = HashContent(v.Markdown)
	if v.ID == "" {
		v.ID = "docv-" + uuid.NewString()
	}
	if v.Locale == "" {
		v.Locale = "en-US"
	}
	if v.Classification == "" {
		v.Classification = "INTERNAL"
	}
	if v.Renderer == "" {
		v.Renderer = "hub-v1"
	}
	_, err := tx.Exec(ctx, `INSERT INTO document_version(id,tenant_id,document_id,parent_id,creator_id,title,locale,classification,normalized_markdown,content_hash,renderer_profile,change_note) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		v.ID, tenantID, v.DocumentID, v.ParentID, v.CreatorID, v.Title, v.Locale, v.Classification, v.Markdown, v.Hash, v.Renderer, v.ChangeNote)
	if err != nil {
		return Version{}, err
	}
	if err := indexTermsTx(ctx, tx, tenantID, v.DocumentID, v); err != nil {
		return Version{}, err
	}
	if err := storeLinksTx(ctx, tx, tenantID, v.DocumentID, v.ID, ExtractLinks(v.Markdown)); err != nil {
		return Version{}, err
	}
	return v, nil
}
