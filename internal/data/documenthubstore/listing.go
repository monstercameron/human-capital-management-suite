package documenthubstore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// DocumentSummary is the bounded, authorized personal document projection.
// Team and channel documents require live audience eligibility and are not
// exposed by this path until that authority is composed. Starred and
// FolderID are the actor's own organization; ReaderCount is filled only
// for an actor who may manage access.
type DocumentSummary struct {
	ID, Title, OwnerID, VersionID string
	Status                        string
	UpdatedAt                     time.Time
	Shared, CanManage, CanEdit    bool
	Starred                       bool
	FolderID                      string
	ReaderCount                   int
	// TitleKey is the database's lower-cased title, the title cursor key.
	TitleKey string
	// Snippet and Match explain a search hit; both are empty in a plain
	// listing.
	Snippet, Match string
}

// List sort orders.
const (
	SortUpdated    = "updated"
	SortUpdatedAsc = "updated_asc"
	SortTitle      = "title"
	SortTitleDesc  = "title_desc"
	// SortOwner and SortOwnerDesc order by the owner's display name from
	// ListOptions.OwnerNames (the owner ID where no name is known), then
	// newest first, then ID. They page by Offset only.
	SortOwner     = "owner"
	SortOwnerDesc = "owner_desc"
	// SortRelevance applies only while searching; a plain listing sorts by
	// SortUpdated instead.
	SortRelevance = "relevance"
)

// ListOptions selects and pages the actor's documents. FolderID narrows to
// one of the actor's folders, Starred to the actor's stars and OwnerID to
// one owner. Sort is one of the Sort constants; the default is relevance
// while searching and newest first otherwise. A plain listing pages by
// keyset (BeforeTime/BeforeID for the updated orders, AfterTitle/BeforeID
// for the title orders, where AfterTitle is the last row's TitleKey) or by
// Offset. Mode, QueryVector and VectorModel apply to
// SearchPersonalDocuments only.
type ListOptions struct {
	Limit      int
	Query      string
	Collection string
	FolderID   string
	Starred    bool
	OwnerID    string
	Sort       string
	BeforeTime time.Time
	BeforeID   string
	AfterTitle *string
	Offset     int
	Mode       string
	// QueryVector is the query embedded by VectorModel, for meaning search.
	QueryVector []float32
	VectorModel string
	// OwnerNames maps owner IDs to display names for the owner sorts.
	OwnerNames map[string]string
}

// CreatePersonalDocument atomically creates the private document, owner
// grants, first immutable candidate, and its outbox event.
func (s *Store) CreatePersonalDocument(ctx context.Context, tenantID, ownerID, title, markdown string) (string, Version, error) {
	if strings.TrimSpace(ownerID) == "" || strings.TrimSpace(title) == "" {
		return "", Version{}, errors.New("document: owner and title are required")
	}
	id := "doc-" + uuid.NewString()
	var version Version
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO document(id,tenant_id,owner_id,home) VALUES($1,$2,$3,'PERSONAL')`, id, tenantID, ownerID); err != nil {
			return err
		}
		if err := bootstrapOwnerTx(ctx, tx, tenantID, id, ownerID); err != nil {
			return err
		}
		var err error
		version, err = insertVersionTx(ctx, tx, tenantID, Version{
			DocumentID: id, CreatorID: ownerID, Title: title, Markdown: markdown,
		})
		if err != nil {
			return err
		}
		if err := s.enqueueIndexTx(ctx, tx, tenantID, id, version.ID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO document_outbox(tenant_id,aggregate_id,event_type,payload)
			VALUES($1::text,$2::text,'document.created',jsonb_build_object('document_id',$2::text,'version_id',$3::text))`, tenantID, id, version.ID)
		return err
	})
	if err != nil {
		return "", Version{}, err
	}
	return id, version, nil
}

// ListPersonalDocuments returns personal documents owned by or explicitly
// shared with the actor. A recipient sees only an active deployment, while
// the owner can see their latest candidate. A live deny always wins.
func (s *Store) ListPersonalDocuments(ctx context.Context, tenantID, actorID string, limit int) ([]DocumentSummary, error) {
	return s.ListPersonalDocumentsPage(ctx, tenantID, actorID, ListOptions{Limit: limit})
}

// readableDocumentSQL is the live visibility rule for document alias d and
// actor parameter $2: a personal, undisposed document with a live read
// allow and no live read deny for the actor, which the owner sees as a
// draft and anyone else only once a default deployment exists.
const readableDocumentSQL = `d.home='PERSONAL' AND d.lifecycle<>'DISPOSED'
	AND EXISTS (SELECT 1 FROM document_grant g WHERE g.tenant_id=d.tenant_id
		AND g.document_id=d.id AND g.subject_kind='person' AND g.subject_id=$2
		AND g.action='read' AND g.effect='allow' AND g.revoked=false
		AND (g.expires_at IS NULL OR g.expires_at>now()))
	AND NOT EXISTS (SELECT 1 FROM document_grant g WHERE g.tenant_id=d.tenant_id
		AND g.document_id=d.id AND g.subject_kind='person' AND g.subject_id=$2
		AND g.action='read' AND g.effect='deny' AND g.revoked=false
		AND (g.expires_at IS NULL OR g.expires_at>now()))
	AND (d.owner_id=$2 OR EXISTS (SELECT 1 FROM document_active_pointer ap WHERE ap.tenant_id=d.tenant_id
		AND ap.document_id=d.id AND ap.scope_kind='default' AND ap.scope_id=''))`

// listSelectionSQL is the FROM and WHERE of one list selection, shared by
// the page and its total so both count the same documents. Parameters:
// $1 tenant, $2 actor, $3 collection, $4 query terms, $5 raw query,
// $6 folder, $7 starred only, $8 owner. The version is the owner's latest
// candidate or, for anyone else, the active deployment; a query matches
// only that version, so readers never search an unpublished draft.
const listSelectionSQL = `FROM document d
	LEFT JOIN document_active_pointer p ON p.tenant_id=d.tenant_id AND p.document_id=d.id
		AND p.scope_kind='default' AND p.scope_id=''
	LEFT JOIN LATERAL (
		SELECT x.id,x.title,x.normalized_markdown,x.created_at FROM document_version x
		WHERE x.tenant_id=d.tenant_id AND x.document_id=d.id
			AND x.status<>'retired'
			AND (d.owner_id=$2 OR x.id=p.version_id)
		ORDER BY x.created_at DESC,x.id DESC LIMIT 1
	) v ON true
	LEFT JOIN document_folder_item fi ON fi.tenant_id=d.tenant_id AND fi.owner_id=$2 AND fi.document_id=d.id
	LEFT JOIN document_star st ON st.tenant_id=d.tenant_id AND st.owner_id=$2 AND st.document_id=d.id
	WHERE d.tenant_id=$1 AND ` + readableDocumentSQL + `
		AND ($3::text='all' OR ($3::text='private' AND d.owner_id=$2) OR ($3::text='shared' AND d.owner_id<>$2))
		AND ($4::text[] IS NULL OR (v.id IS NOT NULL AND (
			(EXISTS (SELECT 1 FROM document_search_term t WHERE t.tenant_id=d.tenant_id AND t.version_id=v.id)
				AND NOT EXISTS (SELECT 1 FROM unnest($4::text[]) q(term) WHERE NOT EXISTS (
					SELECT 1 FROM document_search_term t WHERE t.tenant_id=d.tenant_id AND t.version_id=v.id
						AND starts_with(t.term, q.term))))
			OR (NOT EXISTS (SELECT 1 FROM document_search_term t WHERE t.tenant_id=d.tenant_id AND t.version_id=v.id)
				AND (position(lower($5::text) in lower(v.title))>0 OR position(lower($5::text) in lower(v.normalized_markdown))>0)))))
		AND ($6::text='' OR fi.folder_id=$6::text)
		AND (NOT $7::boolean OR st.document_id IS NOT NULL)
		AND ($8::text='' OR d.owner_id=$8::text)`

// listProjectionSQL is the page's column list: reader count (people other
// than the owner with a live read allow and no live read deny), the owner's
// manage and propose capabilities, and the actor's own star and folder.
const listProjectionSQL = `SELECT d.id, d.owner_id,
		COALESCE(v.title, ''), COALESCE(v.id, ''),
		CASE WHEN d.owner_id=$2 THEN 'private' ELSE 'shared' END,
		COALESCE(v.created_at, d.created_at),
		(SELECT count(DISTINCT g.subject_id) FROM document_grant g WHERE g.tenant_id=d.tenant_id AND g.document_id=d.id AND g.subject_kind='person' AND g.subject_id<>d.owner_id AND g.action='read' AND g.effect='allow' AND g.revoked=false AND (g.expires_at IS NULL OR g.expires_at>now()) AND NOT EXISTS (SELECT 1 FROM document_grant deny WHERE deny.tenant_id=g.tenant_id AND deny.document_id=g.document_id AND deny.subject_kind='person' AND deny.subject_id=g.subject_id AND deny.action='read' AND deny.effect='deny' AND deny.revoked=false AND (deny.expires_at IS NULL OR deny.expires_at>now()))),
		(d.owner_id=$2 AND EXISTS (SELECT 1 FROM document_grant g WHERE g.tenant_id=d.tenant_id AND g.document_id=d.id AND g.subject_kind='person' AND g.subject_id=$2 AND g.action='manage' AND g.effect='allow' AND g.revoked=false AND (g.expires_at IS NULL OR g.expires_at>now())) AND NOT EXISTS (SELECT 1 FROM document_grant g WHERE g.tenant_id=d.tenant_id AND g.document_id=d.id AND g.subject_kind='person' AND g.subject_id=$2 AND g.action='manage' AND g.effect='deny' AND g.revoked=false AND (g.expires_at IS NULL OR g.expires_at>now()))),
		(d.owner_id=$2 AND EXISTS (SELECT 1 FROM document_grant g WHERE g.tenant_id=d.tenant_id AND g.document_id=d.id AND g.subject_kind='person' AND g.subject_id=$2 AND g.action='propose' AND g.effect='allow' AND g.revoked=false AND (g.expires_at IS NULL OR g.expires_at>now())) AND NOT EXISTS (SELECT 1 FROM document_grant g WHERE g.tenant_id=d.tenant_id AND g.document_id=d.id AND g.subject_kind='person' AND g.subject_id=$2 AND g.action='propose' AND g.effect='deny' AND g.revoked=false AND (g.expires_at IS NULL OR g.expires_at>now()))),
		st.document_id IS NOT NULL, COALESCE(fi.folder_id, ''), lower(COALESCE(v.title, ''))
	`

// ListPersonalDocumentsPage applies live authorization to each bounded
// page. A query matches when every term is a word, or the start of a word,
// in the version the actor may read: the owner's latest draft, or the
// published version for anyone else.
func (s *Store) ListPersonalDocumentsPage(ctx context.Context, tenantID, actorID string, options ListOptions) ([]DocumentSummary, error) {
	options, terms, err := normalizeListOptions(actorID, options)
	if err != nil {
		return nil, err
	}
	if options.Query != "" && len(terms) == 0 {
		return []DocumentSummary{}, nil
	}
	pageSQL := listProjectionSQL + listSelectionSQL
	args := append(listSelectionArgs(tenantID, actorID, options, terms), options.Limit)
	titleKey := `lower(COALESCE(v.title,''))`
	updated := `COALESCE(v.created_at,d.created_at)`
	switch options.Sort {
	case SortOwner, SortOwnerDesc:
		ids, names := ownerNameArrays(options.OwnerNames)
		dir := "ASC"
		if options.Sort == SortOwnerDesc {
			dir = "DESC"
		}
		pageSQL = listProjectionSQL + listSelectionSQL + `
			ORDER BY lower(COALESCE((SELECT o.name FROM unnest($10::text[], $11::text[]) AS o(owner_id, name) WHERE o.owner_id=d.owner_id LIMIT 1), d.owner_id)) ` + dir + `,
				` + updated + ` DESC, d.id ASC LIMIT $9 OFFSET $12`
		args = append(args, ids, names, options.Offset)
	case SortTitle, SortTitleDesc:
		var after any
		if options.AfterTitle != nil {
			after = *options.AfterTitle
		}
		cmp, dir := ">", "ASC"
		if options.Sort == SortTitleDesc {
			cmp, dir = "<", "DESC"
		}
		pageSQL += ` AND ($10::text IS NULL OR (` + titleKey + `,d.id)` + cmp + `($10::text,$11::text))
			ORDER BY ` + titleKey + ` ` + dir + `,d.id ` + dir + ` LIMIT $9 OFFSET $12`
		args = append(args, after, options.BeforeID, options.Offset)
	default:
		cmp, dir := "<", "DESC"
		if options.Sort == SortUpdatedAsc {
			cmp, dir = ">", "ASC"
		}
		pageSQL += ` AND ($10::timestamptz IS NULL OR (` + updated + `,d.id)` + cmp + `($10::timestamptz,$11::text))
			ORDER BY ` + updated + ` ` + dir + `,d.id ` + dir + ` LIMIT $9 OFFSET $12`
		args = append(args, nullableTime(options.BeforeTime), options.BeforeID, options.Offset)
	}
	var out []DocumentSummary
	err = s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, pageSQL, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			d, err := scanSummary(rows)
			if err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// scanSummary reads one listProjectionSQL row. The reader count is kept
// only for an actor who may manage access.
func scanSummary(rows dbport.Rows) (DocumentSummary, error) {
	var d DocumentSummary
	if err := rows.Scan(&d.ID, &d.OwnerID, &d.Title, &d.VersionID, &d.Status, &d.UpdatedAt, &d.ReaderCount, &d.CanManage, &d.CanEdit, &d.Starred, &d.FolderID, &d.TitleKey); err != nil {
		return DocumentSummary{}, err
	}
	d.Shared = d.ReaderCount > 0
	if !d.CanManage {
		d.ReaderCount = 0
	}
	return d, nil
}

// CountPersonalDocuments counts every document the selection matches,
// across all pages; the cursor and limit in options are ignored.
func (s *Store) CountPersonalDocuments(ctx context.Context, tenantID, actorID string, options ListOptions) (int, error) {
	options, terms, err := normalizeListOptions(actorID, options)
	if err != nil {
		return 0, err
	}
	if options.Query != "" && len(terms) == 0 {
		return 0, nil
	}
	var total int
	err = s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) `+listSelectionSQL, listSelectionArgs(tenantID, actorID, options, terms)...).Scan(&total)
	})
	if err != nil {
		return 0, err
	}
	return total, nil
}

func normalizeListOptions(actorID string, options ListOptions) (ListOptions, []string, error) {
	if actorID == "" {
		return options, nil, ErrDenied
	}
	if options.Limit <= 0 || options.Limit > 101 {
		options.Limit = 101
	}
	if options.Collection != "private" && options.Collection != "shared" {
		options.Collection = "all"
	}
	options.Query = strings.TrimSpace(options.Query)
	switch options.Sort {
	case SortUpdatedAsc, SortTitle, SortTitleDesc, SortUpdated, SortOwner, SortOwnerDesc:
	case SortRelevance, "":
		options.Sort = SortUpdated
		if options.Query != "" {
			options.Sort = SortRelevance
		}
	default:
		options.Sort = SortUpdated
	}
	if options.Offset < 0 {
		options.Offset = 0
	}
	if len(options.Query) > 200 {
		return options, nil, errors.New("document: query is too long")
	}
	var terms []string
	seen := map[string]bool{}
	for _, term := range lexTokenize(options.Query) {
		if !seen[term] {
			terms = append(terms, term)
			seen[term] = true
		}
	}
	return options, terms, nil
}

func listSelectionArgs(tenantID, actorID string, options ListOptions, terms []string) []any {
	var termArg any
	if options.Query != "" {
		termArg = terms
	}
	return []any{tenantID, actorID, options.Collection, termArg, options.Query, options.FolderID, options.Starred, options.OwnerID}
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

// ownerNameArrays flattens owner display names into parallel arrays for
// the owner sorts' unnest join.
func ownerNameArrays(names map[string]string) ([]string, []string) {
	ids := make([]string, 0, len(names))
	values := make([]string, 0, len(names))
	for id, name := range names {
		ids = append(ids, id)
		values = append(values, name)
	}
	return ids, values
}

// SelectionOwners lists the distinct owners of the documents a selection
// can show, so a caller can resolve their display names for the owner
// sorts. The query, cursor and paging in options are ignored.
func (s *Store) SelectionOwners(ctx context.Context, tenantID, actorID string, options ListOptions) ([]string, error) {
	options.Query = ""
	options, _, err := normalizeListOptions(actorID, options)
	if err != nil {
		return nil, err
	}
	var out []string
	err = s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT d.owner_id `+listSelectionSQL+` ORDER BY 1`, listSelectionArgs(tenantID, actorID, options, nil)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			out = append(out, id)
		}
		return rows.Err()
	})
	return out, err
}
