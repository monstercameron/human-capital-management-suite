// Bilateral cross-company document grants and egress gating (HUB-015). A
// cross-company chat automatically exposing all attached documentation is
// the RED this closes: crossing a tenant boundary here always needs (1) a
// bilateral, bounded, host-proposed-then-consumer-accepted grant naming
// classification, residency and a finite expiry, mirroring
// internal/collaboration/chatpolicy's ConversationGrantTerms for chat, and
// (2) an ordinary explicit document_grant on top of it (company- or
// person-scoped). Either alone is refused; only the intersection reads or
// exports.
package documenthubstore

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ErrCrossCompanyTerms is returned when a proposal or acceptance carries
// unbounded, mismatched or already-consumed terms.
var ErrCrossCompanyTerms = errors.New("document cross-company grant: invalid or unbounded terms")

// CrossCompanyGrantTerms is the bounded, host-proposed cross-company scope
// for one document. Zero expiry and blank classification or residency are
// never treated as wildcards.
type CrossCompanyGrantTerms struct {
	DocumentID, HostTenant, ConsumerTenant, Classification, Residency string
	ExpiresAt                                                         time.Time
}

// Validate requires every part of the terms to be explicit and the expiry
// to be strictly in the future of at.
func (t CrossCompanyGrantTerms) Validate(at time.Time) error {
	if at.IsZero() || !crossCompanyTerm(t.DocumentID) || !crossCompanyTerm(t.HostTenant) ||
		!crossCompanyTerm(t.ConsumerTenant) || t.HostTenant == t.ConsumerTenant ||
		!crossCompanyTerm(t.Classification) || !crossCompanyTerm(t.Residency) ||
		t.ExpiresAt.IsZero() || !at.Before(t.ExpiresAt) {
		return ErrCrossCompanyTerms
	}
	return nil
}

func crossCompanyTerm(v string) bool { return v != "" && strings.TrimSpace(v) == v }

// CrossCompanyGrant is the stored bilateral record.
type CrossCompanyGrant struct {
	ID, DocumentID, HostTenant, ConsumerTenant, Classification, Residency string
	Version                                                               uint64
	Proposed, AcceptedByHost, AcceptedByConsumer                          bool
	ExpiresAt, RevokedAt                                                  time.Time
}

// Current reports whether the stored grant is a live, bilaterally accepted
// match for exactly these terms: any drift in classification, residency or
// expiry invalidates it, so a stored record cannot be silently widened by
// changing what a caller asks for.
func (g CrossCompanyGrant) Current(at time.Time, terms CrossCompanyGrantTerms) bool {
	return terms.Validate(at) == nil && g.Version > 0 && g.Proposed && g.AcceptedByHost && g.AcceptedByConsumer &&
		g.RevokedAt.IsZero() && g.DocumentID == terms.DocumentID && g.HostTenant == terms.HostTenant &&
		g.ConsumerTenant == terms.ConsumerTenant && g.Classification == terms.Classification &&
		g.Residency == terms.Residency && storedInstant(g.ExpiresAt).Equal(storedInstant(terms.ExpiresAt)) && at.Before(g.ExpiresAt)
}

// storedInstant is t at the microsecond precision PostgreSQL keeps, so a grant
// read back from the table still matches the terms it was proposed with.
func storedInstant(t time.Time) time.Time { return t.Truncate(time.Microsecond) }

// ProposeCrossCompanyGrant records the host half of a bilateral document
// grant. The actor must hold MANAGE on the document; the terms' HostTenant
// must equal the acting tenant, so a proposal can never be filed on another
// tenant's behalf.
func (s *Store) ProposeCrossCompanyGrant(ctx context.Context, hostTenant, actorID string, terms CrossCompanyGrantTerms, at time.Time) (CrossCompanyGrant, error) {
	if terms.HostTenant != hostTenant {
		return CrossCompanyGrant{}, ErrCrossCompanyTerms
	}
	if err := terms.Validate(at); err != nil {
		return CrossCompanyGrant{}, err
	}
	var out CrossCompanyGrant
	err := s.RunTenantTx(ctx, hostTenant, func(tx dbport.Tx) error {
		if err := authorizeTx(ctx, tx, hostTenant, terms.DocumentID, "person", actorID, ActionManage); err != nil {
			return err
		}
		out = CrossCompanyGrant{
			ID: "docxg-" + uuid.NewString(), DocumentID: terms.DocumentID, HostTenant: terms.HostTenant,
			ConsumerTenant: terms.ConsumerTenant, Classification: terms.Classification, Residency: terms.Residency,
			Version: 1, Proposed: true, AcceptedByHost: true, ExpiresAt: storedInstant(terms.ExpiresAt),
		}
		_, err := tx.Exec(ctx, `INSERT INTO document_crosscompany_grant(id,tenant_id,document_id,host_tenant,consumer_tenant,classification,residency,version,proposed,accepted_by_host,expires_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,true,true,$9)`,
			out.ID, hostTenant, out.DocumentID, out.HostTenant, out.ConsumerTenant, out.Classification, out.Residency, out.Version, out.ExpiresAt)
		return err
	})
	if err != nil {
		return CrossCompanyGrant{}, err
	}
	return out, nil
}

func scanCrossCompanyGrant(row dbport.Row, g *CrossCompanyGrant, expires *time.Time, revoked *sql.NullTime) error {
	return row.Scan(&g.ID, &g.DocumentID, &g.HostTenant, &g.ConsumerTenant, &g.Classification, &g.Residency,
		&g.Version, &g.Proposed, &g.AcceptedByHost, &g.AcceptedByConsumer, expires, revoked)
}

const crossCompanyGrantColumns = `id,document_id,host_tenant,consumer_tenant,classification,residency,version,proposed,accepted_by_host,accepted_by_consumer,expires_at,revoked_at`

// AcceptCrossCompanyGrant records consumer consent for exactly the proposed
// consumer tenant, only while the proposal remains unexpired, unrevoked and
// not already accepted.
func (s *Store) AcceptCrossCompanyGrant(ctx context.Context, hostTenant, grantID, consumerTenant string, at time.Time) (CrossCompanyGrant, error) {
	if consumerTenant == "" || at.IsZero() {
		return CrossCompanyGrant{}, ErrCrossCompanyTerms
	}
	var out CrossCompanyGrant
	err := s.RunTenantTx(ctx, hostTenant, func(tx dbport.Tx) error {
		var expires time.Time
		var revoked sql.NullTime
		if err := scanCrossCompanyGrant(tx.QueryRow(ctx, `SELECT `+crossCompanyGrantColumns+` FROM document_crosscompany_grant WHERE tenant_id=$1 AND id=$2`, hostTenant, grantID), &out, &expires, &revoked); err != nil {
			return ErrCrossCompanyTerms
		}
		out.ExpiresAt = expires
		if revoked.Valid {
			out.RevokedAt = revoked.Time
		}
		if !out.Proposed || !out.AcceptedByHost || out.AcceptedByConsumer || !out.RevokedAt.IsZero() ||
			out.ConsumerTenant != consumerTenant || !at.Before(out.ExpiresAt) {
			return ErrCrossCompanyTerms
		}
		if _, err := tx.Exec(ctx, `UPDATE document_crosscompany_grant SET accepted_by_consumer=true, updated_at=now() WHERE tenant_id=$1 AND id=$2`, hostTenant, grantID); err != nil {
			return err
		}
		out.AcceptedByConsumer = true
		return nil
	})
	if err != nil {
		return CrossCompanyGrant{}, err
	}
	return out, nil
}

// RevokeCrossCompanyGrant closes one bilateral grant; the row stays for
// policy history like document_grant's own revocation.
func (s *Store) RevokeCrossCompanyGrant(ctx context.Context, hostTenant, grantID, actorID string, at time.Time) error {
	return s.RunTenantTx(ctx, hostTenant, func(tx dbport.Tx) error {
		var docID string
		if err := tx.QueryRow(ctx, `SELECT document_id FROM document_crosscompany_grant WHERE tenant_id=$1 AND id=$2 AND revoked_at IS NULL`, hostTenant, grantID).Scan(&docID); err != nil {
			return ErrDenied
		}
		if err := authorizeTx(ctx, tx, hostTenant, docID, "person", actorID, ActionManage); err != nil {
			return err
		}
		n, err := tx.Exec(ctx, `UPDATE document_crosscompany_grant SET revoked_at=$1, updated_at=now() WHERE tenant_id=$2 AND id=$3 AND revoked_at IS NULL`, at, hostTenant, grantID)
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrDenied
		}
		return nil
	})
}

// AuthorizeCrossCompanyRead gates one cross-company read or export: the
// live bilateral grant for (docID, consumerTenant) must be current, and an
// explicit document_grant must separately allow the action, either at the
// company level (subject_kind='company', subject_id=consumerTenant) or for
// the exact subject. Neither alone is enough; both fail the same closed
// ErrDenied, so a probe cannot learn which half is missing.
func (s *Store) AuthorizeCrossCompanyRead(ctx context.Context, hostTenant, docID, consumerTenant, subjectID, action string, at time.Time) error {
	if consumerTenant == "" || consumerTenant == hostTenant || at.IsZero() {
		return ErrDenied
	}
	return s.RunTenantTx(ctx, hostTenant, func(tx dbport.Tx) error {
		var g CrossCompanyGrant
		var expires time.Time
		var revoked sql.NullTime
		if err := scanCrossCompanyGrant(tx.QueryRow(ctx, `SELECT `+crossCompanyGrantColumns+` FROM document_crosscompany_grant WHERE tenant_id=$1 AND document_id=$2 AND consumer_tenant=$3 ORDER BY created_at DESC LIMIT 1`,
			hostTenant, docID, consumerTenant), &g, &expires, &revoked); err != nil {
			return ErrDenied
		}
		g.ExpiresAt = expires
		if revoked.Valid {
			g.RevokedAt = revoked.Time
		}
		if !g.Proposed || !g.AcceptedByHost || !g.AcceptedByConsumer || !g.RevokedAt.IsZero() || !at.Before(g.ExpiresAt) {
			return ErrDenied
		}
		if err := authorizeTx(ctx, tx, hostTenant, docID, "company", consumerTenant, action); err == nil {
			return nil
		}
		return authorizeTx(ctx, tx, hostTenant, docID, "person", subjectID, action)
	})
}
