// Safe attachments for HUB-024: immutable references to quarantine-admitted
// artifacts, bound to the exact version hash and classification they were
// attached under. Only admitted content may attach; every open re-checks
// the reader's current grant and refuses when the document has since been
// reclassified. Descriptors never carry an object URL: bytes leave through
// transport streaming, never a public address.
package documenthubstore

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/asset/quarantine"
)

var (
	ErrAttachmentUnsafe = errors.New("document attachment: artifact is not admitted")
	ErrAttachmentStale  = errors.New("document attachment: version reclassified")
)

// AttachmentInput carries a new attachment; the version hash and
// classification are server-resolved from the bound version.
type AttachmentInput struct {
	DocumentID, VersionID, ActorID  string
	ArtifactID                      string
	Filename, ContentType           string
	SizeBytes                       int64
	QuarantineState, ScannerVersion string
}

// Attachment is the stored descriptor. It deliberately has no URL field:
// there is no public object address to leak, only the content id the
// transport resolves through protected storage.
type Attachment struct {
	ID, DocumentID, VersionID, VersionHash string
	ArtifactID                             string
	Filename, ContentType, Classification  string
	SizeBytes                              int64
	QuarantineState, ScannerVersion        string
}

// AttachArtifact binds an admitted artifact to a version. The document owner
// may attach directly; anyone else needs the propose action. Anything but an
// ADMITTED quarantine verdict, a remote URL, or an empty filename, type or
// scanner version is refused before anything is stored.
func (s *Store) AttachArtifact(ctx context.Context, tenantID string, in AttachmentInput) (Attachment, error) {
	if strings.TrimSpace(in.Filename) == "" || strings.TrimSpace(in.ContentType) == "" ||
		in.SizeBytes <= 0 || strings.TrimSpace(in.ScannerVersion) == "" {
		return Attachment{}, ErrAttachmentUnsafe
	}
	if in.QuarantineState != string(quarantine.Admitted) || !isContentRef(in.ArtifactID) {
		return Attachment{}, ErrAttachmentUnsafe
	}
	var attachment Attachment
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var owner string
		if err := tx.QueryRow(ctx, `SELECT owner_id FROM document WHERE tenant_id=$1 AND id=$2`,
			tenantID, in.DocumentID).Scan(&owner); err != nil {
			return ErrDenied
		}
		if owner != in.ActorID {
			if err := authorizeTx(ctx, tx, tenantID, in.DocumentID, "person", in.ActorID, ActionPropose); err != nil {
				return err
			}
		}
		version, err := loadVersion(ctx, tx, tenantID, in.DocumentID, in.VersionID)
		if err != nil {
			return err
		}
		attachment = Attachment{
			ID: "doca-" + uuid.NewString(), DocumentID: in.DocumentID, VersionID: in.VersionID,
			VersionHash: version.Hash, ArtifactID: in.ArtifactID,
			Filename: in.Filename, ContentType: in.ContentType, Classification: version.Classification,
			SizeBytes: in.SizeBytes, QuarantineState: in.QuarantineState, ScannerVersion: in.ScannerVersion,
		}
		_, err = tx.Exec(ctx, `INSERT INTO document_attachment(id,tenant_id,document_id,version_id,version_hash,artifact_id,filename,content_type,size_bytes,classification,quarantine_state,scanner_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
			attachment.ID, tenantID, attachment.DocumentID, attachment.VersionID, attachment.VersionHash,
			attachment.ArtifactID, attachment.Filename, attachment.ContentType, attachment.SizeBytes,
			attachment.Classification, attachment.QuarantineState, attachment.ScannerVersion)
		return err
	})
	if err != nil {
		return Attachment{}, err
	}
	return attachment, nil
}

// OpenAttachment resolves one attachment for a reader holding the current
// read grant. Unknown ids fail as denied so readers cannot probe for
// attachments, and a document reclassified after attach revokes usability
// without rewriting the immutable row.
func (s *Store) OpenAttachment(ctx context.Context, tenantID, docID, attachmentID, readerKind, readerID string) (Attachment, error) {
	var attachment Attachment
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT id,document_id,version_id,version_hash,artifact_id,filename,content_type,size_bytes,classification,quarantine_state,scanner_version FROM document_attachment WHERE tenant_id=$1 AND document_id=$2 AND id=$3`,
			tenantID, docID, attachmentID).Scan(
			&attachment.ID, &attachment.DocumentID, &attachment.VersionID, &attachment.VersionHash,
			&attachment.ArtifactID, &attachment.Filename, &attachment.ContentType, &attachment.SizeBytes,
			&attachment.Classification, &attachment.QuarantineState, &attachment.ScannerVersion); err != nil {
			return ErrDenied
		}
		if err := authorizeTx(ctx, tx, tenantID, docID, readerKind, readerID, ActionRead); err != nil {
			return err
		}
		var current string
		if err := tx.QueryRow(ctx, `SELECT classification FROM document_version WHERE tenant_id=$1 AND document_id=$2 ORDER BY created_at DESC, id DESC LIMIT 1`,
			tenantID, docID).Scan(&current); err != nil {
			return ErrDenied
		}
		if current != attachment.Classification {
			return ErrAttachmentStale
		}
		return nil
	})
	if err != nil {
		return Attachment{}, err
	}
	return attachment, nil
}

// isContentRef admits only content-addressed references. Anything with a
// URL scheme or an empty body is a remote address, not protected storage.
func isContentRef(ref string) bool {
	if ref == "" || strings.Contains(ref, "://") {
		return false
	}
	return true
}
