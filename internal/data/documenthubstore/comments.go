// Version-anchored comments for HUB-018: immutable rows bind the exact
// version hash under discussion, so content replacement never invalidates
// the thread. Mentions are extracted from the body and start unconsented;
// only the mentioned user consents, which records them as joined.
package documenthubstore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// CommentInput carries a new comment; the version hash is server-resolved.
// A non-empty Quote anchors the comment to a passage of VersionID (see
// comment_threads.go); ParentID makes it a reply. Start and End are filled
// by the store.
type CommentInput struct {
	DocumentID, VersionID, AuthorID, AnchorBlock, Quote, Body string
	Prefix, Suffix, ParentID                                  string
	Start, End                                                int
}

// Comment is one stored comment with its mentions and consents.
// Start and End are code-point offsets of the quote in the version being
// read (-1 when unanchored or orphaned).
type Comment struct {
	ID, DocumentID, VersionID, VersionHash string
	AuthorID, AnchorBlock, Quote, Body     string
	Prefix, Suffix, ParentID               string
	Start, End                             int
	Resolved, Orphaned                     bool
	Mentions, Consented                    []string
	CreatedAt                              time.Time
}

// extractMentions returns @handles in body order without duplicates.
func extractMentions(body string) []string {
	var mentions []string
	seen := map[string]bool{}
	for i := 0; i < len(body); i++ {
		if body[i] != '@' || (i > 0 && isHandleChar(body[i-1])) {
			continue
		}
		j := i + 1
		for j < len(body) && isHandleChar(body[j]) {
			j++
		}
		if handle := body[i+1 : j]; handle != "" && !seen[handle] {
			seen[handle] = true
			mentions = append(mentions, handle)
		}
		i = j
	}
	return mentions
}

func isHandleChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_'
}

// AddComment appends one comment by an actor holding the comment action,
// binding the version's current hash.
func (s *Store) AddComment(ctx context.Context, tenantID string, in CommentInput) (Comment, error) {
	if strings.TrimSpace(in.Body) == "" {
		return Comment{}, errors.New("document comment: body is required")
	}
	var comment Comment
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if err := authorizeTx(ctx, tx, tenantID, in.DocumentID, "person", in.AuthorID, ActionComment); err != nil {
			return err
		}
		version, err := loadVersion(ctx, tx, tenantID, in.DocumentID, in.VersionID)
		if err != nil {
			return err
		}
		if err := prepareCommentTx(ctx, tx, tenantID, version, &in); err != nil {
			return err
		}
		comment = Comment{
			ID: "docc-" + uuid.NewString(), DocumentID: in.DocumentID, VersionID: in.VersionID,
			VersionHash: version.Hash, AuthorID: in.AuthorID,
			AnchorBlock: in.AnchorBlock, Quote: in.Quote, Body: in.Body,
			Prefix: in.Prefix, Suffix: in.Suffix, ParentID: in.ParentID, Start: in.Start, End: in.End,
			Mentions: extractMentions(in.Body),
		}
		if err := tx.QueryRow(ctx, `INSERT INTO document_comment(id,tenant_id,document_id,version_id,version_hash,author_id,anchor_block,quote,body,anchor_prefix,anchor_suffix,anchor_start,anchor_end,parent_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING created_at`,
			comment.ID, tenantID, comment.DocumentID, comment.VersionID, comment.VersionHash, comment.AuthorID, comment.AnchorBlock, comment.Quote, comment.Body,
			comment.Prefix, comment.Suffix, comment.Start, comment.End, comment.ParentID).Scan(&comment.CreatedAt); err != nil {
			return err
		}
		for _, m := range comment.Mentions {
			if _, err := tx.Exec(ctx, `INSERT INTO document_mention(tenant_id,comment_id,mentioned_id) VALUES($1,$2,$3)`, tenantID, comment.ID, m); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Comment{}, err
	}
	return comment, nil
}

// ConsentMention records a mentioned user's consent to join the thread.
// Only the mentioned user can consent, and only within their tenant.
func (s *Store) ConsentMention(ctx context.Context, tenantID, commentID, actorID string) error {
	return s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `UPDATE document_mention SET consented=true, consented_at=now() WHERE tenant_id=$1 AND comment_id=$2 AND mentioned_id=$3 AND consented=false`, tenantID, commentID, actorID)
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("document mention: no pending mention for actor")
		}
		return nil
	})
}

// ListComments returns one version's thread to a subject holding the read
// action, with mentions and consents attached.
func (s *Store) ListComments(ctx context.Context, tenantID, docID, versionID, subjectKind, subjectID string) ([]Comment, error) {
	var thread []Comment
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if err := authorizeTx(ctx, tx, tenantID, docID, subjectKind, subjectID, ActionRead); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id,document_id,version_id,version_hash,author_id,anchor_block,quote,body,created_at FROM document_comment WHERE tenant_id=$1 AND document_id=$2 AND version_id=$3 ORDER BY created_at`, tenantID, docID, versionID)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var c Comment
			if err := rows.Scan(&c.ID, &c.DocumentID, &c.VersionID, &c.VersionHash, &c.AuthorID, &c.AnchorBlock, &c.Quote, &c.Body, &c.CreatedAt); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, c.ID)
			thread = append(thread, c)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for i := range thread {
			mrows, err := tx.Query(ctx, `SELECT mentioned_id,consented FROM document_mention WHERE tenant_id=$1 AND comment_id=$2 ORDER BY mentioned_id`, tenantID, ids[i])
			if err != nil {
				return err
			}
			for mrows.Next() {
				var id string
				var consented bool
				if err := mrows.Scan(&id, &consented); err != nil {
					mrows.Close()
					return err
				}
				thread[i].Mentions = append(thread[i].Mentions, id)
				if consented {
					thread[i].Consented = append(thread[i].Consented, id)
				}
			}
			mrows.Close()
			if err := mrows.Err(); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return thread, nil
}
