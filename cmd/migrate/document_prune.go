package main

// migrate document prune: retire test documents one owner left behind, such
// as the "Bughunt doc ..." documents .artifacts/tmp/docs-bughunt.mjs
// creates. It lists what matches and changes nothing unless -apply is
// given. Applying withdraws any live publication and then disposes the
// document through documenthubstore.DisposeDocument, which marks it
// DISPOSED: it leaves every library, search and read path, and the
// append-only history stays. A document under a records hold is reported
// and left alone.
//
//	migrate document prune -tenant harborcare-demo -owner hc-050-rafael-torres -title-prefix "Bughunt doc " [-apply=true]
//
// Bootstrap flags take an explicit value, so -apply is written -apply=true.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

const (
	fieldPruneOwner       = "owner"
	fieldPruneTitlePrefix = "title-prefix"
	fieldPruneApply       = "apply"
)

// documentPruneFields are the prune subcommand's flags, appended to the
// role's flag set.
func documentPruneFields() []bootstrap.Field {
	return []bootstrap.Field{
		{Name: fieldPruneOwner, Usage: "document prune: owner subject whose documents are pruned"},
		{Name: fieldPruneTitlePrefix, Usage: "document prune: prune only documents whose current title starts with this text"},
		{Name: fieldPruneApply, Usage: "document prune: retire the matches (-apply=true; without it prune only lists them)", Kind: bootstrap.KindBool, Default: "false"},
	}
}

type documentPruneOptions struct {
	Tenant, Owner, TitlePrefix string
	Apply                      bool
}

type pruneCandidate struct {
	ID, Title string
	Live      []documenthubstore.Deployment
}

// runDocumentPruneAction opens the document store and runs the prune.
func runDocumentPruneAction(ctx context.Context, values *bootstrap.Values, coreURL string, out io.Writer) error {
	apply, err := values.Bool(fieldPruneApply)
	if err != nil {
		return err
	}
	opts := documentPruneOptions{Tenant: values.String(fieldTenant), Owner: values.String(fieldPruneOwner), TitlePrefix: values.String(fieldPruneTitlePrefix), Apply: apply}
	if err := opts.validate(); err != nil {
		return err
	}
	store, err := documenthubstore.New(ctx, documenthubstore.Config{DSN: values.String(fieldDocumentDatabaseURL), CoreDSN: coreURL, ChatDSN: values.String(fieldChatDatabaseURL)})
	if err != nil {
		return fmt.Errorf("open document store: %w", err)
	}
	defer store.Close()
	return runDocumentPruneCommand(ctx, store, opts, out)
}

// validate refuses a prune that could match more than one owner's
// deliberately named documents: owner and a non-blank title prefix are
// both required.
func (o *documentPruneOptions) validate() error {
	if strings.TrimSpace(o.Tenant) == "" {
		o.Tenant = defaultDocumentSeedTenant
	}
	if strings.TrimSpace(o.Owner) == "" {
		return fmt.Errorf("document prune: -%s is required", fieldPruneOwner)
	}
	if strings.TrimSpace(o.TitlePrefix) == "" {
		return fmt.Errorf("document prune: -%s is required and must not be blank", fieldPruneTitlePrefix)
	}
	return nil
}

func runDocumentPruneCommand(ctx context.Context, store *documenthubstore.Store, opts documentPruneOptions, out io.Writer) error {
	if store == nil {
		return errors.New("document prune: document store is required")
	}
	if err := opts.validate(); err != nil {
		return err
	}
	candidates, err := findPruneCandidates(ctx, store, opts)
	if err != nil {
		return err
	}
	mode := "dry run, pass -apply to retire them"
	if opts.Apply {
		mode = "applying"
	}
	fmt.Fprintf(out, "document prune %s: %d documents owned by %s titled %q... (%s)\n", opts.Tenant, len(candidates), opts.Owner, opts.TitlePrefix, mode)
	var retired, held int
	for _, c := range candidates {
		state := "private"
		if len(c.Live) > 0 {
			state = "published"
		}
		if !opts.Apply {
			fmt.Fprintf(out, "  would retire %s %q (%s)\n", c.ID, c.Title, state)
			continue
		}
		reason := "migrate document prune: test document titled " + opts.TitlePrefix
		for _, live := range c.Live {
			if _, err := store.Withdraw(ctx, opts.Tenant, documenthubstore.WithdrawInput{DocumentID: c.ID, ScopeKind: live.ScopeKind, ScopeID: live.ScopeID, ActorID: opts.Owner, ExpectedLive: live.VersionID, Reason: reason}); err != nil {
				return fmt.Errorf("withdraw %q: %w", c.Title, err)
			}
		}
		if _, err := store.DisposeDocument(ctx, opts.Tenant, c.ID, opts.Owner, reason); err != nil {
			if errors.Is(err, documenthubstore.ErrHoldActive) {
				held++
				fmt.Fprintf(out, "  kept %s %q: a records hold is active\n", c.ID, c.Title)
				continue
			}
			return fmt.Errorf("retire %q: %w", c.Title, err)
		}
		retired++
		fmt.Fprintf(out, "  retired %s %q (was %s)\n", c.ID, c.Title, state)
	}
	if opts.Apply {
		fmt.Fprintf(out, "document prune %s: %d retired, %d kept under hold\n", opts.Tenant, retired, held)
	}
	return nil
}

// findPruneCandidates lists the owner's documents that are not already
// retired and whose current title starts with the prefix, with any live
// publications.
func findPruneCandidates(ctx context.Context, store *documenthubstore.Store, opts documentPruneOptions) ([]pruneCandidate, error) {
	var out []pruneCandidate
	err := store.RunTenantTx(ctx, opts.Tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT d.id, v.title FROM document d
			JOIN LATERAL (SELECT x.title FROM document_version x WHERE x.tenant_id=d.tenant_id AND x.document_id=d.id ORDER BY x.created_at DESC, x.id DESC LIMIT 1) v ON true
			WHERE d.tenant_id=$1 AND d.owner_id=$2 AND d.home='PERSONAL' AND d.lifecycle<>'DISPOSED' AND left(v.title, char_length($3)) = $3
			ORDER BY v.title, d.id`, opts.Tenant, opts.Owner, opts.TitlePrefix)
		if err != nil {
			return err
		}
		for rows.Next() {
			var c pruneCandidate
			if err := rows.Scan(&c.ID, &c.Title); err != nil {
				rows.Close()
				return err
			}
			out = append(out, c)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for i := range out {
			live, err := tx.Query(ctx, `SELECT scope_kind, scope_id, version_id FROM document_active_pointer WHERE tenant_id=$1 AND document_id=$2 ORDER BY scope_kind, scope_id`, opts.Tenant, out[i].ID)
			if err != nil {
				return err
			}
			for live.Next() {
				var d documenthubstore.Deployment
				if err := live.Scan(&d.ScopeKind, &d.ScopeID, &d.VersionID); err != nil {
					live.Close()
					return err
				}
				out[i].Live = append(out[i].Live, d)
			}
			live.Close()
			if err := live.Err(); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}
