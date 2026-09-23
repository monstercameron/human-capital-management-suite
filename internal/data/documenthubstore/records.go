// Records retention, hold and disposition for HUB-037: the document
// records series names every table holding document evidence, legal holds
// freeze disposal, and disposal removes only mutable derivatives while the
// immutable core stays as the archive. Disposal refuses held documents and
// live deployments, and every step reports a verified inventory.
package documenthubstore

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var (
	ErrHoldActive   = errors.New("document records: hold is active")
	ErrDocumentLive = errors.New("document records: deployment is live")
)

// Hold is one legal hold freezing disposal of a document.
type Hold struct {
	ID, DocumentID, Reason, PlacedBy string
	Active                           bool
	PlacedAt                         time.Time
}

// RecordsClass is the verified inventory of one records class.
type RecordsClass struct {
	Class             string
	Removed, Retained int64
	Tables            []string
}

// RecordsDisposition is the verified records inventory of one document.
type RecordsDisposition struct {
	DocumentID     string
	Held, Disposed bool
	Classes        []RecordsClass
}

type seriesTable struct {
	class, table string
	mutable      bool
}

// recordsSeries declares the document records series: every table holding
// document evidence and whether disposition may remove its rows. Exports
// name a governed class with no hub table until HUB-038 lands it.
var recordsSeries = []seriesTable{
	{"versions", "document_version", false},
	{"deployments", "document_deployment", false},
	{"deployments", "document_active_pointer", true},
	{"comments", "document_comment", false},
	{"comments", "document_mention", true},
	{"grants", "document_grant", true},
	{"assets", "document_attachment", false},
	{"derivatives", "document_search_term", true},
	{"derivatives", "document_section_vector", true},
	{"derivatives", "document_block", true},
	{"derivatives", "document_link", true},
}

func seriesCountSQL(table string) string {
	switch table {
	case "document_mention":
		return `SELECT count(*) FROM document_mention m JOIN document_comment c ON c.tenant_id=m.tenant_id AND c.id=m.comment_id WHERE c.tenant_id=$1 AND c.document_id=$2`
	case "document_link":
		return `SELECT count(*) FROM document_link WHERE tenant_id=$1 AND source_document_id=$2`
	default:
		return `SELECT count(*) FROM ` + table + ` WHERE tenant_id=$1 AND document_id=$2`
	}
}

// authorizeOwnerTx admits the document owner or a manager.
func authorizeOwnerTx(ctx context.Context, tx dbport.Tx, tenantID, docID, actorID string) error {
	var owner string
	if err := tx.QueryRow(ctx, `SELECT owner_id FROM document WHERE tenant_id=$1 AND id=$2`, tenantID, docID).Scan(&owner); err != nil {
		return ErrDenied
	}
	if owner == actorID {
		return nil
	}
	return authorizeTx(ctx, tx, tenantID, docID, "person", actorID, ActionManage)
}

// PlaceHold freezes disposal of a document under an audited reason.
func (s *Store) PlaceHold(ctx context.Context, tenantID, docID, actorID, reason string) (Hold, error) {
	var hold Hold
	if reason == "" {
		return Hold{}, errors.New("document records: reason is required")
	}
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if err := authorizeOwnerTx(ctx, tx, tenantID, docID, actorID); err != nil {
			return err
		}
		hold = Hold{ID: "doch-" + uuid.NewString(), DocumentID: docID, Reason: reason, PlacedBy: actorID, Active: true}
		_, err := tx.Exec(ctx, `INSERT INTO document_hold(id,tenant_id,document_id,reason,placed_by,active) VALUES($1,$2,$3,$4,$5,true)`,
			hold.ID, tenantID, docID, reason, actorID)
		return err
	})
	if err != nil {
		return Hold{}, err
	}
	return hold, nil
}

// ReleaseHold lifts one hold; other active holds keep disposal frozen.
func (s *Store) ReleaseHold(ctx context.Context, tenantID, holdID, actorID string) error {
	return s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var docID string
		if err := tx.QueryRow(ctx, `SELECT document_id FROM document_hold WHERE tenant_id=$1 AND id=$2 AND active`,
			tenantID, holdID).Scan(&docID); err != nil {
			return ErrDenied
		}
		if err := authorizeOwnerTx(ctx, tx, tenantID, docID, actorID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE document_hold SET active=false, released_at=now() WHERE tenant_id=$1 AND id=$2`,
			tenantID, holdID)
		return err
	})
}

func heldTx(ctx context.Context, tx dbport.Tx, tenantID, docID string) (bool, error) {
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM document_hold WHERE tenant_id=$1 AND document_id=$2 AND active`,
		tenantID, docID).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

func liveTx(ctx context.Context, tx dbport.Tx, tenantID, docID string) (bool, error) {
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM document_active_pointer WHERE tenant_id=$1 AND document_id=$2`,
		tenantID, docID).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// inventoryTx counts every series table for one document.
func inventoryTx(ctx context.Context, tx dbport.Tx, tenantID, docID string) ([]RecordsClass, error) {
	counts := map[string]int64{}
	tables := map[string][]string{}
	order := []string{}
	for _, st := range recordsSeries {
		var n int64
		if err := tx.QueryRow(ctx, seriesCountSQL(st.table), tenantID, docID).Scan(&n); err != nil {
			return nil, err
		}
		counts[st.class] += n
		if _, ok := tables[st.class]; !ok {
			order = append(order, st.class)
		}
		tables[st.class] = append(tables[st.class], st.table)
	}
	classes := make([]RecordsClass, 0, len(order)+1)
	for _, class := range order {
		classes = append(classes, RecordsClass{Class: class, Retained: counts[class], Tables: tables[class]})
	}
	return append(classes, RecordsClass{Class: "exports", Tables: []string{}}), nil
}

// VerifyDisposition reports the verified records inventory of one
// document: per-class counts, hold state and disposal state.
func (s *Store) VerifyDisposition(ctx context.Context, tenantID, docID string) (RecordsDisposition, error) {
	var out RecordsDisposition
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var lifecycle string
		if err := tx.QueryRow(ctx, `SELECT lifecycle FROM document WHERE tenant_id=$1 AND id=$2`,
			tenantID, docID).Scan(&lifecycle); err != nil {
			return ErrDenied
		}
		held, err := heldTx(ctx, tx, tenantID, docID)
		if err != nil {
			return err
		}
		classes, err := inventoryTx(ctx, tx, tenantID, docID)
		if err != nil {
			return err
		}
		out = RecordsDisposition{DocumentID: docID, Held: held, Disposed: lifecycle == "DISPOSED", Classes: classes}
		return nil
	})
	if err != nil {
		return RecordsDisposition{}, err
	}
	return out, nil
}

// DisposeDocument removes mutable derivatives, tombstones the document and
// reports the verified inventory. Held documents and live deployments
// refuse; the immutable core stays as the archive.
func (s *Store) DisposeDocument(ctx context.Context, tenantID, docID, actorID, reason string) (RecordsDisposition, error) {
	var out RecordsDisposition
	if reason == "" {
		return RecordsDisposition{}, errors.New("document records: reason is required")
	}
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if err := authorizeOwnerTx(ctx, tx, tenantID, docID, actorID); err != nil {
			return err
		}
		held, err := heldTx(ctx, tx, tenantID, docID)
		if err != nil {
			return err
		}
		if held {
			return ErrHoldActive
		}
		live, err := liveTx(ctx, tx, tenantID, docID)
		if err != nil {
			return err
		}
		if live {
			return ErrDocumentLive
		}
		before, err := inventoryTx(ctx, tx, tenantID, docID)
		if err != nil {
			return err
		}
		deletes := []string{
			`DELETE FROM document_mention m USING document_comment c WHERE c.tenant_id=m.tenant_id AND c.id=m.comment_id AND c.tenant_id=$1 AND c.document_id=$2`,
			`DELETE FROM document_grant WHERE tenant_id=$1 AND document_id=$2`,
			`DELETE FROM document_link WHERE tenant_id=$1 AND source_document_id=$2`,
			`DELETE FROM document_block WHERE tenant_id=$1 AND document_id=$2`,
			`DELETE FROM document_search_term WHERE tenant_id=$1 AND document_id=$2`,
			`DELETE FROM document_section_vector WHERE tenant_id=$1 AND document_id=$2`,
			`DELETE FROM document_active_pointer WHERE tenant_id=$1 AND document_id=$2`,
		}
		for _, q := range deletes {
			if _, err := tx.Exec(ctx, q, tenantID, docID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE document SET lifecycle='DISPOSED' WHERE tenant_id=$1 AND id=$2`, tenantID, docID); err != nil {
			return err
		}
		after, err := inventoryTx(ctx, tx, tenantID, docID)
		if err != nil {
			return err
		}
		prior := map[string]int64{}
		for _, c := range before {
			prior[c.Class] = c.Retained
		}
		for i, c := range after {
			after[i].Removed = prior[c.Class] - c.Retained
		}
		out = RecordsDisposition{DocumentID: docID, Disposed: true, Classes: after}
		return nil
	})
	if err != nil {
		return RecordsDisposition{}, err
	}
	return out, nil
}

// dispositionManifest renders a disposition as canonical evidence rows for
// golden pins.
func dispositionManifest(d RecordsDisposition) []map[string]any {
	rows := make([]map[string]any, 0, len(d.Classes))
	for _, c := range d.Classes {
		rows = append(rows, map[string]any{
			"class": c.Class, "doc": d.DocumentID, "disposed": d.Disposed,
			"held": d.Held, "removed": c.Removed, "retained": c.Retained, "tables": c.Tables,
		})
	}
	return rows
}
