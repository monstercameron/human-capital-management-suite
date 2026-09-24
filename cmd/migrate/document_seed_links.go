package main

// Relinking an already-seeded library. An earlier seed linked documents to
// targets much of their audience could not open; the plan now links only
// to targets the whole audience can read. For a document that already
// exists, its "Related documents" section is rewritten through the store:
// a new owner version, published (reviewed by a colleague and deployed)
// when the document is shared, so readers see working links too. A
// pending owner draft is rewritten on top of the published fix, so the
// owner keeps their draft. Timestamps stay next to the document's
// existing history.

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
)

const relatedHeading = "\n## Related documents\n"

// withRelated replaces markdown's trailing "Related documents" section with
// section (which may be empty) and normalizes the result like the store.
func withRelated(markdown, section string) string {
	if i := strings.Index(markdown, relatedHeading); i >= 0 {
		markdown = markdown[:i]
	}
	return documenthubstore.NormalizeMarkdown(strings.TrimRight(markdown, "\n") + "\n" + section)
}

// seedLinkCoverage counts, over the plan, links whose target the source's
// whole audience can read.
func seedLinkCoverage(plan documentSeedPlan) (total, covered int) {
	for _, d := range plan.Docs {
		for _, j := range d.Links {
			total++
			if audienceCovered(d, plan.Docs[j]) {
				covered++
			}
		}
	}
	return total, covered
}

// relinkSeedDocuments brings reused documents up to the plan: it shares
// each with any planned reader it lacks (the plan widens a same-owner
// target to the audience that links to it), then rewrites the
// related-links section where it differs. It returns how many documents
// were rewritten, how many versions it wrote and how many shares it added.
func relinkSeedDocuments(ctx context.Context, store *documenthubstore.Store, tenant string, plan documentSeedPlan, ids []string, reused []bool) (int, int, int, error) {
	var docs, versions, shares int
	for i, d := range plan.Docs {
		if !reused[i] {
			continue
		}
		var links []seedLink
		for _, j := range d.Links {
			links = append(links, seedLink{Title: plan.Docs[j].Title, ID: ids[j]})
		}
		section := relatedSection(links)
		id, owner := ids[i], d.Owner.Key
		widened, err := convergeSeedShares(ctx, store, tenant, id, d)
		if err != nil {
			return docs, versions, shares, err
		}
		shares += widened
		_, latest, err := store.ReadPersonalDocument(ctx, tenant, owner, id)
		if err != nil {
			return docs, versions, shares, fmt.Errorf("read %q: %w", d.Title, err)
		}
		live, err := store.ResolveDeployment(ctx, tenant, id, "default", "")
		deployed := err == nil
		if err != nil && !errors.Is(err, documenthubstore.ErrNoDeployment) {
			return docs, versions, shares, fmt.Errorf("resolve %q: %w", d.Title, err)
		}
		if deployed {
			// A publication nobody but the owner can read any more (every
			// share revoked) is left as is; only the owner's version is fixed.
			access, err := store.ListAccess(ctx, tenant, owner, id)
			if err != nil {
				return docs, versions, shares, fmt.Errorf("read audience of %q: %w", d.Title, err)
			}
			if len(access) < 2 {
				deployed, live = false, documenthubstore.Deployment{}
			}
		}
		published := latest
		if deployed && live.VersionID != latest.ID {
			if published, err = store.ReadVersion(ctx, tenant, id, live.VersionID, "person", owner); err != nil {
				return docs, versions, shares, fmt.Errorf("read published %q: %w", d.Title, err)
			}
		}
		fixedLatest := withRelated(latest.Markdown, section)
		fixedPublished := withRelated(published.Markdown, section)
		latestOK, publishedOK := sameMarkdown(fixedLatest, latest.Markdown), sameMarkdown(fixedPublished, published.Markdown)
		if latestOK && (!deployed || publishedOK) {
			continue
		}
		var written []string
		tip := latest.ID
		if deployed && !publishedOK {
			v, err := store.CreatePersonalDocumentVersion(ctx, tenant, id, owner, tip, published.Title, fixedPublished)
			if err != nil {
				return docs, versions, shares, fmt.Errorf("relink %q: %w", d.Title, err)
			}
			if err := publishSeedVersion(ctx, store, tenant, d, id, v.ID, live.VersionID); err != nil {
				return docs, versions, shares, fmt.Errorf("publish %q: %w", d.Title, err)
			}
			tip = v.ID
			written = append(written, v.ID)
		}
		if live.VersionID != latest.ID || !deployed {
			if !latestOK || tip != latest.ID {
				v, err := store.CreatePersonalDocumentVersion(ctx, tenant, id, owner, tip, latest.Title, fixedLatest)
				if err != nil {
					return docs, versions, shares, fmt.Errorf("relink draft %q: %w", d.Title, err)
				}
				written = append(written, v.ID)
			}
		}
		if err := backdateRelink(ctx, store, tenant, latest.CreatedAt, written); err != nil {
			return docs, versions, shares, err
		}
		docs++
		versions += len(written)
	}
	return docs, versions, shares, nil
}

// convergeSeedShares shares a reused document with every planned reader
// who cannot read it yet, and returns how many it added.
func convergeSeedShares(ctx context.Context, store *documenthubstore.Store, tenant, id string, d seedDocPlan) (int, error) {
	if len(d.Shares) == 0 {
		return 0, nil
	}
	access, err := store.ListAccess(ctx, tenant, d.Owner.Key, id)
	if err != nil {
		return 0, fmt.Errorf("read audience of %q: %w", d.Title, err)
	}
	has := map[string]bool{}
	for _, a := range access {
		has[a.SubjectID] = true
	}
	added := 0
	for _, sh := range d.Shares {
		if has[sh.Recipient] {
			continue
		}
		if err := store.SharePersonalDocumentRole(ctx, tenant, id, d.Owner.Key, sh.Recipient, sh.Role); err != nil {
			return added, fmt.Errorf("share %q with %s: %w", d.Title, sh.Recipient, err)
		}
		added++
	}
	return added, nil
}

// publishSeedVersion has a colleague who shares the document approve the
// version and the owner deploy it over the current publication.
func publishSeedVersion(ctx context.Context, store *documenthubstore.Store, tenant string, d seedDocPlan, docID, versionID, expectedLive string) error {
	reviewer := ""
	for _, sh := range d.Shares {
		if sh.Recipient != d.Owner.Key {
			reviewer = sh.Recipient
			break
		}
	}
	if reviewer == "" {
		// Shared outside the plan (someone shared it by hand): any current
		// reader can review.
		access, err := store.ListAccess(ctx, tenant, d.Owner.Key, docID)
		if err != nil {
			return err
		}
		for _, a := range access {
			if a.SubjectID != d.Owner.Key {
				reviewer = a.SubjectID
				break
			}
		}
	}
	if reviewer == "" {
		return errors.New("no colleague to review the relinked version")
	}
	if _, err := store.RecordReview(ctx, tenant, documenthubstore.ReviewInput{DocumentID: docID, VersionID: versionID, ScopeKind: "default", ReviewerID: reviewer, Authority: "personal", Decision: documenthubstore.ReviewApproved, Note: "Related links updated"}); err != nil {
		return err
	}
	_, err := store.Deploy(ctx, tenant, documenthubstore.DeployInput{DocumentID: docID, VersionID: versionID, ScopeKind: "default", DeployerID: d.Owner.Key, ExpectedLive: expectedLive})
	return err
}

// backdateRelink places the relink versions (and any deployment of them)
// one second apart just after the document's previous latest version, so
// the library's dates do not jump to today.
func backdateRelink(ctx context.Context, store *documenthubstore.Store, tenant string, after time.Time, versionIDs []string) error {
	if len(versionIDs) == 0 {
		return nil
	}
	return store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
			return err
		}
		for k, id := range versionIDs {
			at := after.Add(time.Duration(k+1) * time.Second)
			for _, q := range []string{
				`UPDATE document_version SET created_at=$3 WHERE tenant_id=$1 AND id=$2`,
				`UPDATE document_deployment SET created_at=$3, effective_at=$3 WHERE tenant_id=$1 AND version_id=$2`,
				`UPDATE document_active_pointer SET updated_at=$3 WHERE tenant_id=$1 AND version_id=$2`,
				`UPDATE document_review SET decided_at=$3 WHERE tenant_id=$1 AND version_id=$2`,
			} {
				if _, err := tx.Exec(ctx, q, tenant, id, at); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// sameMarkdown ignores differences in blank lines, which the body
// generators and normalization do not keep consistent.
func sameMarkdown(a, b string) bool {
	squash := func(s string) string { return blankRuns.ReplaceAllString(strings.TrimSpace(s), "\n\n") }
	return squash(a) == squash(b)
}

var blankRuns = regexp.MustCompile(`\n{2,}`)
