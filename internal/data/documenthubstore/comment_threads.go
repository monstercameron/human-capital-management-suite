// Passage anchors, replies and resolution for version-anchored comments.
// A comment may quote a passage of the version it was written on: the
// client sends the quote with a little context (W3C TextQuoteSelector
// style) and the store finds the passage in that version's plain text,
// refusing a quote that is not there. When a thread is read on a later
// version the passage is found again by quote and context; a quote that
// no longer occurs is reported orphaned but kept, so the thread still
// reads. Comment rows stay immutable; resolution is an append-only event.
package documenthubstore

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Anchor and thread errors.
var (
	ErrInvalidAnchor   = errors.New("document comment: the quoted passage is not in this version")
	ErrInvalidParent   = errors.New("document comment: replies answer a visible top-level comment of the same document")
	ErrUnknownComment  = errors.New("document comment: not found")
	ErrResolveDenied   = errors.New("document comment: only its author or a manager may resolve it")
	ErrResolveNotTopic = errors.New("document comment: only a top-level comment can be resolved")
)

// Anchor bounds, in code points.
const (
	MaxAnchorQuote   = 500
	MaxAnchorContext = 64
)

// PlainText is the plain text comment anchors index into: the version's
// Markdown stripped the way search snippets strip it, with whitespace
// collapsed. Anchor offsets count Unicode code points in this text.
func PlainText(markdown string) string { return stripMarkdown(markdown) }

// textBlock is one heading section's code-point range in the plain text.
type textBlock struct {
	id         string
	start, end int
}

// plainBlocks maps each heading section to its range in PlainText.
func plainBlocks(markdown, plain string) []textBlock {
	var out []textBlock
	cursor := 0
	for _, sec := range SplitSections(markdown) {
		text := stripMarkdown(sec.Text)
		if text == "" {
			continue
		}
		i := strings.Index(plain[cursor:], text)
		if i < 0 {
			continue
		}
		start := cursor + i
		out = append(out, textBlock{id: sec.BlockID, start: utf8.RuneCountInString(plain[:start]), end: utf8.RuneCountInString(plain[:start+len(text)])})
		cursor = start + len(text)
	}
	return out
}

func blockAt(blocks []textBlock, offset int) string {
	id := ""
	for _, b := range blocks {
		if b.start <= offset {
			id = b.id
		}
	}
	return id
}

func collapseSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

// locateAnchor finds quote in plain, preferring the occurrence whose
// surroundings best match prefix and suffix and then one inside hintBlock.
// It returns code-point offsets and the enclosing block.
func locateAnchor(markdown, plain, quote, prefix, suffix, hintBlock string) (int, int, string, bool) {
	quote = collapseSpace(quote)
	prefix, suffix = collapseSpace(prefix), collapseSpace(suffix)
	if quote == "" {
		return 0, 0, "", false
	}
	blocks := plainBlocks(markdown, plain)
	best, bestScore := -1, -1
	for from := 0; from <= len(plain); {
		i := strings.Index(plain[from:], quote)
		if i < 0 {
			break
		}
		at := from + i
		score := commonSuffix(strings.TrimSpace(plain[:at]), prefix) + commonPrefix(strings.TrimSpace(plain[at+len(quote):]), suffix)
		if hintBlock != "" && blockAt(blocks, utf8.RuneCountInString(plain[:at])) == hintBlock {
			score += MaxAnchorContext + 1
		}
		if score > bestScore {
			best, bestScore = at, score
		}
		from = at + 1
	}
	if best < 0 {
		return 0, 0, "", false
	}
	start := utf8.RuneCountInString(plain[:best])
	end := start + utf8.RuneCountInString(quote)
	return start, end, blockAt(blocks, start), true
}

func commonSuffix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[len(a)-1-n] == b[len(b)-1-n] {
		n++
	}
	return n
}

func commonPrefix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

// validateAnchorInput checks lengths before any lookup.
func validateAnchorInput(in CommentInput) error {
	if utf8.RuneCountInString(in.Quote) > MaxAnchorQuote || utf8.RuneCountInString(in.Prefix) > MaxAnchorContext || utf8.RuneCountInString(in.Suffix) > MaxAnchorContext {
		return ErrInvalidAnchor
	}
	if strings.TrimSpace(in.Quote) == "" && (in.Prefix != "" || in.Suffix != "") {
		return ErrInvalidAnchor
	}
	return nil
}

// versionVisibleSQL admits comments written where the actor may read
// them: every comment for the owner, otherwise one written on a version
// that was published at some point or on the version being read, so a
// quote from an unpublished draft never reaches a reader. $1 tenant, $3
// actor, $4 version being read.
func versionVisibleSQL(alias string) string {
	return `(EXISTS (SELECT 1 FROM document od WHERE od.tenant_id=` + alias + `.tenant_id AND od.id=` + alias + `.document_id AND od.owner_id=$3)
	OR ` + alias + `.version_id=$4
	OR EXISTS (SELECT 1 FROM document_deployment dd WHERE dd.tenant_id=` + alias + `.tenant_id AND dd.document_id=` + alias + `.document_id AND dd.version_id=` + alias + `.version_id))`
}

// commentVisibleSQL applies versionVisibleSQL to a top-level comment c, and
// to a reply's parent, so a reply is seen by everyone who sees its thread.
var commentVisibleSQL = `((c.parent_id='' AND ` + versionVisibleSQL("c") + `)
	OR (c.parent_id<>'' AND EXISTS (SELECT 1 FROM document_comment pc WHERE pc.tenant_id=c.tenant_id AND pc.id=c.parent_id AND ` + versionVisibleSQL("pc") + `)))`

// ListThread returns every comment of a document the actor may see, with
// anchors located in versionID's text, replies' parents, resolution state
// and mentions. The actor must hold read.
func (s *Store) ListThread(ctx context.Context, tenantID, docID, versionID, actorID string) ([]Comment, error) {
	var thread []Comment
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if err := authorizeTx(ctx, tx, tenantID, docID, "person", actorID, ActionRead); err != nil {
			return err
		}
		read, err := loadVersion(ctx, tx, tenantID, docID, versionID)
		if err != nil {
			return ErrDenied
		}
		plain := PlainText(read.Markdown)
		rows, err := tx.Query(ctx, `SELECT c.id,c.document_id,c.version_id,c.version_hash,c.author_id,c.anchor_block,c.quote,c.anchor_prefix,c.anchor_suffix,c.anchor_start,c.anchor_end,c.parent_id,c.body,c.created_at,
				COALESCE((SELECT r.resolved FROM document_comment_resolution r WHERE r.tenant_id=c.tenant_id AND r.comment_id=c.id ORDER BY r.created_at DESC, r.id DESC LIMIT 1), false)
			FROM document_comment c WHERE c.tenant_id=$1 AND c.document_id=$2 AND `+commentVisibleSQL+`
			ORDER BY c.created_at, c.id`, tenantID, docID, actorID, versionID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var c Comment
			if err := rows.Scan(&c.ID, &c.DocumentID, &c.VersionID, &c.VersionHash, &c.AuthorID, &c.AnchorBlock, &c.Quote, &c.Prefix, &c.Suffix, &c.Start, &c.End, &c.ParentID, &c.Body, &c.CreatedAt, &c.Resolved); err != nil {
				rows.Close()
				return err
			}
			thread = append(thread, c)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		visible := map[string]bool{}
		for _, c := range thread {
			visible[c.ID] = true
		}
		kept := thread[:0]
		for _, c := range thread {
			if c.ParentID != "" && !visible[c.ParentID] {
				continue
			}
			if c.Quote != "" && (c.VersionID != versionID || c.Start < 0) {
				if start, end, block, ok := locateAnchor(read.Markdown, plain, c.Quote, c.Prefix, c.Suffix, c.AnchorBlock); ok {
					c.Start, c.End, c.AnchorBlock = start, end, block
				} else {
					c.Start, c.End, c.Orphaned = -1, -1, true
				}
			}
			kept = append(kept, c)
		}
		thread = kept
		for i := range thread {
			if err := loadMentionsTx(ctx, tx, tenantID, &thread[i]); err != nil {
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

func loadMentionsTx(ctx context.Context, tx dbport.Tx, tenantID string, c *Comment) error {
	rows, err := tx.Query(ctx, `SELECT mentioned_id,consented FROM document_mention WHERE tenant_id=$1 AND comment_id=$2 ORDER BY mentioned_id`, tenantID, c.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var consented bool
		if err := rows.Scan(&id, &consented); err != nil {
			return err
		}
		c.Mentions = append(c.Mentions, id)
		if consented {
			c.Consented = append(c.Consented, id)
		}
	}
	return rows.Err()
}

// ResolveComment records that a top-level comment's thread is resolved or
// reopened. The actor must be able to read the document and see the
// comment, and must be its author or hold manage. Recording the state the
// thread is already in changes nothing.
func (s *Store) ResolveComment(ctx context.Context, tenantID, docID, commentID, actorID string, resolved bool) error {
	return s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if err := authorizeTx(ctx, tx, tenantID, docID, "person", actorID, ActionRead); err != nil {
			return err
		}
		var author, parent string
		var current bool
		err := tx.QueryRow(ctx, `SELECT c.author_id, c.parent_id,
				COALESCE((SELECT r.resolved FROM document_comment_resolution r WHERE r.tenant_id=c.tenant_id AND r.comment_id=c.id ORDER BY r.created_at DESC, r.id DESC LIMIT 1), false)
			FROM document_comment c WHERE c.tenant_id=$1 AND c.document_id=$2 AND c.id=$5 AND `+commentVisibleSQL,
			tenantID, docID, actorID, "", commentID).Scan(&author, &parent, &current)
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrUnknownComment
		}
		if err != nil {
			return err
		}
		if parent != "" {
			return ErrResolveNotTopic
		}
		if author != actorID {
			if err := authorizeTx(ctx, tx, tenantID, docID, "person", actorID, ActionManage); err != nil {
				if errors.Is(err, ErrDenied) {
					return ErrResolveDenied
				}
				return err
			}
		}
		if current == resolved {
			return nil
		}
		_, err = tx.Exec(ctx, `INSERT INTO document_comment_resolution(id,tenant_id,document_id,comment_id,resolved,actor_id) VALUES($1,$2,$3,$4,$5,$6)`,
			"docr-"+uuid.NewString(), tenantID, docID, commentID, resolved, actorID)
		return err
	})
}

// prepareCommentTx validates a new comment's anchor and parent inside the
// write transaction and fills the located offsets.
func prepareCommentTx(ctx context.Context, tx dbport.Tx, tenantID string, version Version, in *CommentInput) error {
	if err := validateAnchorInput(*in); err != nil {
		return err
	}
	in.Start, in.End = -1, -1
	if in.ParentID != "" {
		if strings.TrimSpace(in.Quote) != "" {
			return ErrInvalidAnchor
		}
		var parentOfParent string
		err := tx.QueryRow(ctx, `SELECT c.parent_id FROM document_comment c WHERE c.tenant_id=$1 AND c.document_id=$2 AND c.id=$5 AND `+commentVisibleSQL,
			tenantID, in.DocumentID, in.AuthorID, in.VersionID, in.ParentID).Scan(&parentOfParent)
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrInvalidParent
		}
		if err != nil {
			return err
		}
		if parentOfParent != "" {
			return ErrInvalidParent
		}
		in.AnchorBlock = ""
		return nil
	}
	if strings.TrimSpace(in.Quote) == "" {
		return nil
	}
	start, end, block, ok := locateAnchor(version.Markdown, PlainText(version.Markdown), in.Quote, in.Prefix, in.Suffix, in.AnchorBlock)
	if !ok {
		return ErrInvalidAnchor
	}
	in.Quote, in.Prefix, in.Suffix = collapseSpace(in.Quote), collapseSpace(in.Prefix), collapseSpace(in.Suffix)
	in.Start, in.End, in.AnchorBlock = start, end, block
	return nil
}
