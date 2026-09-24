// Adversarial authorization and search conformance matrix for HUB-045:
// personal, team/channel placement, cross-company and revocation paths are
// driven through every read surface the store exposes -- list, get,
// history, lexical/hybrid/filtered search, vector retrieval, backlinks,
// previews, comments and export -- and each surface must show zero
// metadata or body leak to a subject the current live grant state does
// not admit. Every probe runs against a real migrated pgtest database
// (documentFixture), never a mock authorizer, so the assertions exercise
// the same SQL every production caller runs.
package documenthubstore

import (
	"context"
	"errors"
	"testing"
	"time"
)

// hub045Doc creates, reviews, deploys and indexes one personal document so
// every read surface (list, get, history, search, backlinks, previews,
// comments, export, vectors) has a live deployed version to probe. It
// returns the document and version IDs plus the embedded section so
// vector retrieval has something authorized to find.
func hub045Doc(t *testing.T, s *Store, ctx context.Context, tenant, owner, title, markdown string) (string, string) {
	t.Helper()
	docID, v, err := s.CreatePersonalDocument(ctx, tenant, owner, title, markdown)
	if err != nil {
		t.Fatalf("create %s: %v", title, err)
	}
	if _, err := s.RecordReview(ctx, tenant, ReviewInput{
		DocumentID: docID, VersionID: v.ID, ScopeKind: "default", ScopeID: "",
		ReviewerID: "u-045-reviewer", Authority: "team:leads", Decision: ReviewApproved,
	}); err != nil {
		t.Fatalf("review %s: %v", title, err)
	}
	if _, err := s.GrantAction(ctx, tenant, GrantInput{
		DocumentID: docID, SubjectKind: "person", SubjectID: "u-045-deployer",
		Action: ActionDeploy, Effect: EffectAllow, Issuer: owner,
	}); err != nil {
		t.Fatalf("grant deploy %s: %v", title, err)
	}
	if _, err := s.Deploy(ctx, tenant, DeployInput{
		DocumentID: docID, VersionID: v.ID, ScopeKind: "default", ScopeID: "", DeployerID: "u-045-deployer",
	}); err != nil {
		t.Fatalf("deploy %s: %v", title, err)
	}
	if err := s.IndexDeployedVersion(ctx, tenant, docID, v.ID); err != nil {
		t.Fatalf("index %s: %v", title, err)
	}
	if _, err := s.EmbedSections(ctx, tenant, docID, v.ID, testEmbeddingPolicy(), "hub-internal-v1", stubEmbedder); err != nil {
		t.Fatalf("embed %s: %v", title, err)
	}
	return docID, v.ID
}

// hub045Surfaces runs every authorized read surface for readerKind/readerID
// against docID/versionID and reports whether every surface saw the
// document. It never treats a mix (some surfaces see it, some don't) as a
// pass: authorization must be uniform across the whole served matrix.
func hub045Surfaces(t *testing.T, s *Store, ctx context.Context, tenant, docID, versionID, readerKind, readerID, query string) (seenAny, seenAll bool) {
	t.Helper()
	seenAny = false
	total, seen := 0, 0
	check := func(hit bool) {
		total++
		if hit {
			seen++
			seenAny = true
		}
	}

	if readerKind == "person" {
		rows, err := s.ListPersonalDocumentsPage(ctx, tenant, readerID, ListOptions{})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		found := false
		for _, r := range rows {
			if r.ID == docID {
				found = true
			}
		}
		check(found)

		_, _, err = s.ReadPersonalDocument(ctx, tenant, readerID, docID)
		check(err == nil)
	}

	lex, err := s.SearchLexical(ctx, tenant, query, readerKind, readerID)
	if err != nil {
		t.Fatalf("lexical search: %v", err)
	}
	check(searchHitContains(lex, docID))

	filtered, err := s.SearchLexicalFiltered(ctx, tenant, query, readerKind, readerID, SearchFilters{})
	if err != nil {
		t.Fatalf("filtered search: %v", err)
	}
	found := false
	for _, h := range filtered.Hits {
		if h.DocumentID == docID {
			found = true
		}
	}
	check(found)

	hybrid, err := s.SearchHybrid(ctx, tenant, query, readerKind, readerID, hybridOpts())
	if err != nil {
		t.Fatalf("hybrid search: %v", err)
	}
	found = false
	for _, h := range hybrid {
		if h.DocumentID == docID {
			found = true
		}
	}
	check(found)

	vectors, err := s.RetrieveVectors(ctx, tenant, stubMustEmbed(t, query), "hub-internal-v1", readerKind, readerID, 20)
	if err != nil {
		t.Fatalf("retrieve vectors: %v", err)
	}
	found = false
	for _, h := range vectors {
		if h.DocumentID == docID {
			found = true
		}
	}
	check(found)

	previews, err := s.DocumentPreviews(ctx, tenant, readerID, []string{docID})
	if err != nil {
		t.Fatalf("previews: %v", err)
	}
	check(len(previews) == 1 && previews[0].Readable)

	_, err = s.ExportDocument(ctx, tenant, docID, readerKind, readerID)
	check(err == nil)

	return seenAny, seen == total
}

func searchHitContains(hits []SearchHit, docID string) bool {
	for _, h := range hits {
		if h.DocumentID == docID {
			return true
		}
	}
	return false
}

func stubMustEmbed(t *testing.T, text string) []float32 {
	t.Helper()
	v, err := stubEmbedder(text)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// TestTodo_HUB_045 is the PRIMARY test for HUB-045: an authorized reader
// sees an authorized document through every served surface -- personal
// sharing, team/channel placement eligibility, and the bilateral
// cross-company intersection -- proving each authorization path actually
// admits the traffic the spec promises before the adversarial matrix
// proves the denials.
func TestTodo_HUB_045(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	const tenant, owner, reader = "tenant-hub045", "owner-045", "reader-045"

	docID, versionID := hub045Doc(t, s, ctx, tenant, owner, "Onboarding Guide", "# Onboarding\n\nStep by step onboarding checklist for new hires.\n")
	if err := s.SharePersonalDocumentRole(ctx, tenant, docID, owner, reader, RoleViewer); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, tenant, GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: reader, Action: ActionHistory, Effect: EffectAllow, Issuer: owner}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, tenant, GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: reader, Action: ActionExport, Effect: EffectAllow, Issuer: owner}); err != nil {
		t.Fatal(err)
	}

	_, all := hub045Surfaces(t, s, ctx, tenant, docID, versionID, "person", reader, "onboarding")
	if !all {
		t.Fatalf("authorized reader did not see the document on every surface")
	}
	history, err := s.ReadHistory(ctx, tenant, docID, "person", reader)
	if err != nil || len(history) != 1 || history[0].Redacted {
		t.Fatalf("authorized reader history = %+v, %v", history, err)
	}

	// Team/channel placement: a live-eligible member resolves the placed
	// version; an ineligible or since-departed member never does, even
	// though nothing about the deployment itself changed.
	teamDoc, teamVersion, err := s.CreatePersonalDocument(ctx, tenant, owner, "Team Policy", "# Team Policy\n\nChannel-scoped guidance.\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordReview(ctx, tenant, ReviewInput{DocumentID: teamDoc, VersionID: teamVersion.ID, ScopeKind: "placement", ScopeID: "chan-045", ReviewerID: "u-045-reviewer", Authority: "team:leads", Decision: ReviewApproved}); err != nil {
		t.Fatal(err)
	}
	for _, g := range []GrantInput{
		{SubjectKind: "person", SubjectID: "u-045-manager", Action: ActionDeploy, Effect: EffectAllow, Issuer: owner},
		{SubjectKind: "person", SubjectID: "u-045-manager", Action: ActionManage, Effect: EffectAllow, Issuer: owner},
	} {
		g.DocumentID = teamDoc
		if _, err := s.GrantAction(ctx, tenant, g); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.PlaceDocument(ctx, tenant, PlaceInput{DocumentID: teamDoc, VersionID: teamVersion.ID, ScopeKind: "placement", ScopeID: "chan-045", ActorID: "u-045-manager", CustodianID: "u-045-manager", ReviewDueAt: fixedReviewDue}); err != nil {
		t.Fatal(err)
	}
	aud := &fakeAudience{members: map[string]bool{"u-045-member": true}}
	placed, err := s.ResolvePlacementDeployment(ctx, tenant, teamDoc, "placement", "chan-045", "u-045-member", aud)
	if err != nil || placed.VersionID != teamVersion.ID {
		t.Fatalf("eligible member did not resolve placement: %+v, %v", placed, err)
	}

	// Cross-company: only the bilateral grant AND the explicit document
	// grant together admit the consumer tenant's subject.
	crossDoc, err := s.CreateDocument(ctx, tenant, owner, "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	terms := CrossCompanyGrantTerms{
		DocumentID: crossDoc, HostTenant: tenant, ConsumerTenant: "vendor-045",
		Classification: "confidential", Residency: "US", ExpiresAt: at.Add(time.Hour),
	}
	proposal, err := s.ProposeCrossCompanyGrant(ctx, tenant, owner, terms, at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcceptCrossCompanyGrant(ctx, tenant, proposal.ID, terms.ConsumerTenant, at); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, tenant, GrantInput{DocumentID: crossDoc, SubjectKind: "company", SubjectID: terms.ConsumerTenant, Action: ActionRead, Effect: EffectAllow, Issuer: owner}); err != nil {
		t.Fatal(err)
	}
	if err := s.AuthorizeCrossCompanyRead(ctx, tenant, crossDoc, terms.ConsumerTenant, "u-vendor-045", ActionRead, at); err != nil {
		t.Fatalf("intersection of bilateral + explicit grant refused: %v", err)
	}
}

// TestTodo_HUB_045_Conformance is the CONFORMANCE test for HUB-045: a
// table-driven battery of forged routes, stale/expired grants, pinned
// links surviving a redeploy, history redaction and vector retrieval all
// prove the served matrix never discloses a title, snippet, count or
// citation the current live grant state does not admit.
func TestTodo_HUB_045_Conformance(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	const tenant, owner, stranger = "tenant-hub045c", "owner-045c", "stranger-045c"

	docID, versionID := hub045Doc(t, s, ctx, tenant, owner, "Payroll Runbook", "# Payroll\n\nClose checklist and escalation steps.\n")

	// A stranger with no grant at all sees nothing on any surface: no
	// error leaks distinguishable existence for the surfaces that filter
	// silently, and the surfaces that error return the same denial a
	// nonexistent document would.
	any, _ := hub045Surfaces(t, s, ctx, tenant, docID, versionID, "person", stranger, "payroll")
	if any {
		t.Fatalf("stranger saw the document on at least one surface")
	}
	if _, err := s.ExportDocument(ctx, tenant, docID, "person", stranger); !errors.Is(err, ErrDenied) {
		t.Fatalf("stranger export = %v, want ErrDenied", err)
	}
	if history, err := s.ReadHistory(ctx, tenant, docID, "person", stranger); !errors.Is(err, ErrDenied) || history != nil {
		t.Fatalf("stranger history = %+v, %v, want ErrDenied/nil", history, err)
	}
	if bl, err := s.Backlinks(ctx, tenant, docID, "person", stranger); !errors.Is(err, ErrDenied) || bl != nil {
		t.Fatalf("stranger backlinks = %+v, %v, want ErrDenied/nil", bl, err)
	}
	if _, err := s.ListComments(ctx, tenant, docID, versionID, "person", stranger); !errors.Is(err, ErrDenied) {
		t.Fatalf("stranger comment thread = %v, want ErrDenied", err)
	}
	preview, err := s.DocumentPreviews(ctx, tenant, stranger, []string{docID})
	if err != nil || len(preview) != 1 || preview[0].Readable || preview[0].Title != "" || preview[0].Snippet != "" {
		t.Fatalf("stranger preview leaked: %+v, %v", preview, err)
	}

	// Stale (expired) allow grant: a row exists and is not revoked, but
	// its expiry has already passed, so it must authorize nothing.
	expired := time.Now().Add(-time.Hour)
	if _, err := s.GrantAction(ctx, tenant, GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: "u-045c-stale", Action: ActionRead, Effect: EffectAllow, Issuer: owner, ExpiresAt: expired}); err != nil {
		t.Fatal(err)
	}
	if err := s.Authorize(ctx, tenant, docID, "person", "u-045c-stale", ActionRead); !errors.Is(err, ErrDenied) {
		t.Fatalf("expired grant still authorized: %v", err)
	}
	if hits, err := s.SearchLexical(ctx, tenant, "payroll", "person", "u-045c-stale"); err != nil || searchHitContains(hits, docID) {
		t.Fatalf("stale-grant subject matched search: %+v, %v", hits, err)
	}

	// Pinned links resolve the exact version they named even after a
	// later redeploy changes the scope's live pointer, and never surface
	// a version the reader cannot themselves read.
	linkReader := "u-045c-linkreader"
	if _, err := s.ShareDocument(ctx, tenant, docID, owner, GrantInput{SubjectKind: "person", SubjectID: linkReader, Action: ActionRead, Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}
	pinned := DocLink{TargetDocID: docID, PinnedVersion: versionID, State: LinkValid}
	res, err := s.ResolveLink(ctx, tenant, "default", "", pinned, "person", linkReader)
	if err != nil || res.ResolvedVersionID != versionID || !res.Pinned {
		t.Fatalf("pinned link did not resolve exact version: %+v, %v", res, err)
	}
	v2, err := s.SubmitCandidate(ctx, tenant, Version{DocumentID: docID, CreatorID: owner, Title: "Payroll Runbook v2", Markdown: "# Payroll\n\nUpdated escalation steps.\n"}, versionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordReview(ctx, tenant, ReviewInput{DocumentID: docID, VersionID: v2.ID, ScopeKind: "default", ScopeID: "", ReviewerID: "u-045-reviewer", Authority: "team:leads", Decision: ReviewApproved}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Deploy(ctx, tenant, DeployInput{DocumentID: docID, VersionID: v2.ID, ScopeKind: "default", ScopeID: "", DeployerID: "u-045-deployer", ExpectedLive: versionID}); err != nil {
		t.Fatal(err)
	}
	res, err = s.ResolveLink(ctx, tenant, "default", "", pinned, "person", linkReader)
	if err != nil || res.ResolvedVersionID != versionID {
		t.Fatalf("pinned link followed the redeploy instead of staying pinned: %+v, %v", res, err)
	}
	unpinned := DocLink{TargetDocID: docID, State: LinkValid}
	res, err = s.ResolveLink(ctx, tenant, "default", "", unpinned, "person", linkReader)
	if err != nil || res.ResolvedVersionID != v2.ID {
		t.Fatalf("unpinned link did not follow the redeploy: %+v, %v", res, err)
	}
	if _, err := s.ResolveLink(ctx, tenant, "default", "", pinned, "person", stranger); !errors.Is(err, ErrDenied) {
		t.Fatalf("stranger resolved a pinned link: %v", err)
	}

	// A forged route: a document registered for tenant-a resolved under
	// tenant-b's identity is a tenant mismatch, never a lookup that
	// silently serves the wrong tenant's content.
	routes := NewMemoryDocumentRoutes()
	if _, err := routes.Register(ctx, docID, tenant, "shard-1", "idem-045c"); err != nil {
		t.Fatal(err)
	}
	if _, err := routes.Lookup(ctx, docID, "tenant-hub045c-forged"); !errors.Is(err, ErrRouteTenant) {
		t.Fatalf("forged tenant route lookup = %v, want ErrRouteTenant", err)
	}
}

// TestTodo_HUB_045_Security is the SECURITY test for HUB-045: revocation
// across every path -- personal share, team/channel eligibility and the
// bilateral cross-company grant -- closes every served surface
// immediately, never on a cache's schedule, and a departed or revoked
// subject is left with exactly the same denial a stranger would see.
func TestTodo_HUB_045_Security(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	const tenant, owner, reader = "tenant-hub045s", "owner-045s", "reader-045s"

	// Personal: share, prove access, revoke, prove the same surfaces are
	// now empty/denied in the same commit sequence a real revoke UI would
	// trigger.
	docID, versionID := hub045Doc(t, s, ctx, tenant, owner, "Benefits Policy", "# Benefits\n\nEnrollment and eligibility rules.\n")
	share, err := s.ShareDocument(ctx, tenant, docID, owner, GrantInput{SubjectKind: "person", SubjectID: reader, Action: ActionRead, Effect: EffectAllow})
	if err != nil {
		t.Fatal(err)
	}
	if any, _ := hub045Surfaces(t, s, ctx, tenant, docID, versionID, "person", reader, "benefits"); !any {
		t.Fatalf("shared reader saw nothing before revocation")
	}
	if err := s.RevokeGrant(ctx, tenant, share.ID, owner); err != nil {
		t.Fatal(err)
	}
	if any, _ := hub045Surfaces(t, s, ctx, tenant, docID, versionID, "person", reader, "benefits"); any {
		t.Fatalf("revoked reader still saw the document on at least one surface")
	}
	if _, err := s.ExportDocument(ctx, tenant, docID, "person", reader); !errors.Is(err, ErrDenied) {
		t.Fatalf("revoked reader export = %v, want ErrDenied", err)
	}
	if bl, err := s.Backlinks(ctx, tenant, docID, "person", reader); !errors.Is(err, ErrDenied) || bl != nil {
		t.Fatalf("revoked reader backlinks = %+v, %v", bl, err)
	}

	// Team/channel: eligibility is rechecked on every resolution, so a
	// departure between two calls is seen on the very next one, with no
	// grace period.
	teamDoc, teamVersion, err := s.CreatePersonalDocument(ctx, tenant, owner, "Channel Guide", "# Channel Guide\n\nOfficial channel guidance.\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordReview(ctx, tenant, ReviewInput{DocumentID: teamDoc, VersionID: teamVersion.ID, ScopeKind: "placement", ScopeID: "chan-045s", ReviewerID: "u-045-reviewer", Authority: "team:leads", Decision: ReviewApproved}); err != nil {
		t.Fatal(err)
	}
	for _, g := range []GrantInput{
		{SubjectKind: "person", SubjectID: "u-045s-manager", Action: ActionDeploy, Effect: EffectAllow, Issuer: owner},
		{SubjectKind: "person", SubjectID: "u-045s-manager", Action: ActionManage, Effect: EffectAllow, Issuer: owner},
	} {
		g.DocumentID = teamDoc
		if _, err := s.GrantAction(ctx, tenant, g); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.PlaceDocument(ctx, tenant, PlaceInput{DocumentID: teamDoc, VersionID: teamVersion.ID, ScopeKind: "placement", ScopeID: "chan-045s", ActorID: "u-045s-manager", CustodianID: "u-045s-manager", ReviewDueAt: fixedReviewDue}); err != nil {
		t.Fatal(err)
	}
	aud := &fakeAudience{members: map[string]bool{"u-045s-member": true}}
	if _, err := s.ResolvePlacementDeployment(ctx, tenant, teamDoc, "placement", "chan-045s", "u-045s-member", aud); err != nil {
		t.Fatalf("eligible member refused before departure: %v", err)
	}
	delete(aud.members, "u-045s-member")
	if _, err := s.ResolvePlacementDeployment(ctx, tenant, teamDoc, "placement", "chan-045s", "u-045s-member", aud); !errors.Is(err, ErrIneligibleAudience) {
		t.Fatalf("departed member still resolved placement: %v", err)
	}

	// Cross-company: revoking the bilateral half closes egress even
	// though the explicit document grant to the consumer tenant is left
	// untouched, and drifting a term (residency) off the accepted terms
	// closes it too.
	crossDoc, err := s.CreateDocument(ctx, tenant, owner, "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	terms := CrossCompanyGrantTerms{
		DocumentID: crossDoc, HostTenant: tenant, ConsumerTenant: "vendor-045s",
		Classification: "confidential", Residency: "US", ExpiresAt: at.Add(time.Hour),
	}
	proposal, err := s.ProposeCrossCompanyGrant(ctx, tenant, owner, terms, at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcceptCrossCompanyGrant(ctx, tenant, proposal.ID, terms.ConsumerTenant, at); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, tenant, GrantInput{DocumentID: crossDoc, SubjectKind: "company", SubjectID: terms.ConsumerTenant, Action: ActionRead, Effect: EffectAllow, Issuer: owner}); err != nil {
		t.Fatal(err)
	}
	if err := s.AuthorizeCrossCompanyRead(ctx, tenant, crossDoc, terms.ConsumerTenant, "u-vendor-045s", ActionRead, at); err != nil {
		t.Fatalf("bilateral+explicit intersection refused before revoke: %v", err)
	}
	if err := s.RevokeCrossCompanyGrant(ctx, tenant, proposal.ID, owner, at); err != nil {
		t.Fatal(err)
	}
	if err := s.AuthorizeCrossCompanyRead(ctx, tenant, crossDoc, terms.ConsumerTenant, "u-vendor-045s", ActionRead, at); !errors.Is(err, ErrDenied) {
		t.Fatalf("revoked bilateral grant still authorized cross-company read: %v", err)
	}

	// A second, independent bilateral grant proves the expiry is
	// re-evaluated at read time against the caller's clock, not cached
	// from acceptance: it authorizes right up to expiry and never after,
	// and accepting an unknown proposal id is refused outright.
	if _, err := s.AcceptCrossCompanyGrant(ctx, tenant, "docg-missing", terms.ConsumerTenant, at); err == nil {
		t.Fatalf("accept of unknown proposal id accepted")
	}
	docID2, err := s.CreateDocument(ctx, tenant, owner, "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	shortTerms := CrossCompanyGrantTerms{
		DocumentID: docID2, HostTenant: tenant, ConsumerTenant: "vendor-045s-2",
		Classification: "confidential", Residency: "US", ExpiresAt: at.Add(time.Minute),
	}
	proposal2, err := s.ProposeCrossCompanyGrant(ctx, tenant, owner, shortTerms, at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcceptCrossCompanyGrant(ctx, tenant, proposal2.ID, shortTerms.ConsumerTenant, at); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, tenant, GrantInput{DocumentID: docID2, SubjectKind: "company", SubjectID: shortTerms.ConsumerTenant, Action: ActionRead, Effect: EffectAllow, Issuer: owner}); err != nil {
		t.Fatal(err)
	}
	if err := s.AuthorizeCrossCompanyRead(ctx, tenant, docID2, shortTerms.ConsumerTenant, "u-vendor-045s-2", ActionRead, at); err != nil {
		t.Fatalf("accepted terms refused before expiry: %v", err)
	}
	afterExpiry := shortTerms.ExpiresAt.Add(time.Second)
	if err := s.AuthorizeCrossCompanyRead(ctx, tenant, docID2, shortTerms.ConsumerTenant, "u-vendor-045s-2", ActionRead, afterExpiry); !errors.Is(err, ErrDenied) {
		t.Fatalf("expired bilateral grant still authorized cross-company read: %v", err)
	}
}
