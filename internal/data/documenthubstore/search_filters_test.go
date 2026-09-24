package documenthubstore

import (
	"context"
	"testing"
	"time"
)

// searchFilteredFixture deploys one version of docID for tenant with the
// given locale, so filter tests can pick it out precisely.
func searchFilteredFixture(t *testing.T, s *Store, ctx context.Context, tenant, docID, title, markdown, locale string) Version {
	t.Helper()
	v, err := s.SubmitCandidate(ctx, tenant, Version{DocumentID: docID, CreatorID: "u-author", Title: title, Markdown: markdown, Locale: locale}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordReview(ctx, tenant, ReviewInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "default", ScopeID: "", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, tenant, GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: "u-deployer", Action: ActionDeploy, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Deploy(ctx, tenant, DeployInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "default", ScopeID: "", DeployerID: "u-deployer"}); err != nil {
		t.Fatal(err)
	}
	if err := s.IndexDeployedVersion(ctx, tenant, docID, v.ID); err != nil {
		t.Fatal(err)
	}
	return v
}

// TestTodo_HUB_030 is the PRIMARY test for HUB-030: typed team, channel,
// status, owner, locale and date filters each narrow the result set to
// exactly the matching, authorized document.
func TestTodo_HUB_030(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()

	docEN, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	vEN := searchFilteredFixture(t, s, ctx, "tenant-a", docEN, "Onboarding Guide", "# Onboarding\n\nWelcome onboard.\n", "en-US")
	if _, err := s.ShareDocument(ctx, "tenant-a", docEN, "u-author", GrantInput{SubjectKind: "person", SubjectID: "u-anna", Action: ActionRead, Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}

	docDE, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	searchFilteredFixture(t, s, ctx, "tenant-a", docDE, "Onboarding Leitfaden", "# Onboarding\n\nWillkommen an Bord.\n", "de-DE")
	if _, err := s.ShareDocument(ctx, "tenant-a", docDE, "u-author", GrantInput{SubjectKind: "person", SubjectID: "u-anna", Action: ActionRead, Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}

	docOtherOwner, err := s.CreateDocument(ctx, "tenant-a", "u-other-owner", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	searchFilteredFixture(t, s, ctx, "tenant-a", docOtherOwner, "Onboarding Checklist", "# Onboarding\n\nChecklist for onboard.\n", "en-US")
	if _, err := s.ShareDocument(ctx, "tenant-a", docOtherOwner, "u-other-owner", GrantInput{SubjectKind: "person", SubjectID: "u-anna", Action: ActionRead, Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}

	docTeam, err := s.CreateDocument(ctx, "tenant-a", "u-author", "TEAM")
	if err != nil {
		t.Fatal(err)
	}
	searchFilteredFixture(t, s, ctx, "tenant-a", docTeam, "Onboarding Team Policy", "# Onboarding\n\nTeam-scoped onboard policy.\n", "en-US")
	if _, err := s.ShareDocument(ctx, "tenant-a", docTeam, "u-author", GrantInput{SubjectKind: "person", SubjectID: "u-anna", Action: ActionRead, Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docTeam, SubjectKind: "team", SubjectID: "team-people-ops", Action: ActionRead, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}

	docChannel, err := s.CreateDocument(ctx, "tenant-a", "u-author", "CHANNEL")
	if err != nil {
		t.Fatal(err)
	}
	searchFilteredFixture(t, s, ctx, "tenant-a", docChannel, "Onboarding Channel Policy", "# Onboarding\n\nChannel-scoped onboard policy.\n", "en-US")
	if _, err := s.ShareDocument(ctx, "tenant-a", docChannel, "u-author", GrantInput{SubjectKind: "person", SubjectID: "u-anna", Action: ActionRead, Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docChannel, SubjectKind: "channel", SubjectID: "chan-general", Action: ActionRead, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}

	// Locale filter narrows to exactly the German version.
	res, err := s.SearchLexicalFiltered(ctx, "tenant-a", "onboarding", "person", "u-anna", SearchFilters{Locale: "de-DE"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].DocumentID != docDE {
		t.Fatalf("locale filter wrong result: %+v", res.Hits)
	}
	if res.Total != 1 {
		t.Fatalf("locale filter total wrong: %d", res.Total)
	}

	// Owner filter narrows to exactly the other owner's document.
	res, err = s.SearchLexicalFiltered(ctx, "tenant-a", "onboarding", "person", "u-anna", SearchFilters{OwnerID: "u-other-owner"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].DocumentID != docOtherOwner {
		t.Fatalf("owner filter wrong result: %+v", res.Hits)
	}

	// Status filter: every deployed version has status "deployed"; an
	// unknown status excludes everything without erroring.
	res, err = s.SearchLexicalFiltered(ctx, "tenant-a", "onboarding", "person", "u-anna", SearchFilters{Status: "deployed"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 5 {
		t.Fatalf("status filter wrong count: %d", len(res.Hits))
	}
	res, err = s.SearchLexicalFiltered(ctx, "tenant-a", "onboarding", "person", "u-anna", SearchFilters{Status: "retired"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 0 {
		t.Fatalf("status filter should exclude non-matching status: %+v", res.Hits)
	}

	// Team filter narrows to the document granted to that team.
	res, err = s.SearchLexicalFiltered(ctx, "tenant-a", "onboarding", "person", "u-anna", SearchFilters{TeamID: "team-people-ops"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].DocumentID != docTeam {
		t.Fatalf("team filter wrong result: %+v", res.Hits)
	}

	// Channel filter narrows to the document granted to that channel.
	res, err = s.SearchLexicalFiltered(ctx, "tenant-a", "onboarding", "person", "u-anna", SearchFilters{ChannelID: "chan-general"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].DocumentID != docChannel {
		t.Fatalf("channel filter wrong result: %+v", res.Hits)
	}

	// Date filter: a from-date after every deployment excludes everything;
	// the zero-value (no filter) includes them all.
	res, err = s.SearchLexicalFiltered(ctx, "tenant-a", "onboarding", "person", "u-anna", SearchFilters{DateFrom: time.Now().Add(24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 0 {
		t.Fatalf("future date-from should exclude everything: %+v", res.Hits)
	}
	res, err = s.SearchLexicalFiltered(ctx, "tenant-a", "onboarding", "person", "u-anna", SearchFilters{DateFrom: time.Now().Add(-24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 5 {
		t.Fatalf("past date-from should include everything: %d", len(res.Hits))
	}

	// Citation fields are populated: exact version, status, effective date.
	if vEN.ID == "" {
		t.Fatal("fixture version missing id")
	}
	res, err = s.SearchLexicalFiltered(ctx, "tenant-a", "onboarding", "person", "u-anna", SearchFilters{Locale: "en-US", OwnerID: "u-author"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range res.Hits {
		if h.DocumentID == docEN {
			found = true
			if h.VersionID != vEN.ID || h.Status != "deployed" || h.DeployedAt.IsZero() {
				t.Fatalf("citation fields incomplete: %+v", h)
			}
		}
	}
	if !found {
		t.Fatalf("expected docEN among en-US/u-author hits: %+v", res.Hits)
	}
}

// TestTodo_HUB_030_Security is the SECURITY test for HUB-030: a document
// outside the reader's authorized scope never appears through any facet,
// including a team/channel filter that names its own grant, an owner
// filter naming its own owner, or the aggregate total count.
func TestTodo_HUB_030_Security(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()

	// Restricted document: team-granted, but the reader has no personal
	// read grant to it at all.
	docRestricted, err := s.CreateDocument(ctx, "tenant-a", "u-owner", "TEAM")
	if err != nil {
		t.Fatal(err)
	}
	searchFilteredFixture(t, s, ctx, "tenant-a", docRestricted, "Secret Payroll Policy", "# Payroll\n\nConfidential payroll details.\n", "en-US")
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docRestricted, SubjectKind: "team", SubjectID: "team-finance", Action: ActionRead, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}
	// Note: no person-level read grant is issued to u-stranger.

	res, err := s.SearchLexicalFiltered(ctx, "tenant-a", "payroll", "person", "u-stranger", SearchFilters{TeamID: "team-finance"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 0 || res.Total != 0 {
		t.Fatalf("team facet leaked a restricted document: %+v", res)
	}

	// The owner filter naming the true owner also must not leak it to an
	// unauthorized reader.
	res, err = s.SearchLexicalFiltered(ctx, "tenant-a", "payroll", "person", "u-stranger", SearchFilters{OwnerID: "u-owner"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 0 || res.Total != 0 {
		t.Fatalf("owner facet leaked a restricted document: %+v", res)
	}

	// An explicit deny beats a channel allow.
	docDenied, err := s.CreateDocument(ctx, "tenant-a", "u-owner", "CHANNEL")
	if err != nil {
		t.Fatal(err)
	}
	searchFilteredFixture(t, s, ctx, "tenant-a", docDenied, "Denied Channel Notice", "# Notice\n\nChannel notice content.\n", "en-US")
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docDenied, SubjectKind: "channel", SubjectID: "chan-restricted", Action: ActionRead, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ShareDocument(ctx, "tenant-a", docDenied, "u-owner", GrantInput{SubjectKind: "person", SubjectID: "u-mallory", Action: ActionRead, Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ShareDocument(ctx, "tenant-a", docDenied, "u-owner", GrantInput{SubjectKind: "person", SubjectID: "u-mallory", Action: ActionRead, Effect: EffectDeny}); err != nil {
		t.Fatal(err)
	}
	res, err = s.SearchLexicalFiltered(ctx, "tenant-a", "notice", "person", "u-mallory", SearchFilters{ChannelID: "chan-restricted"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 0 {
		t.Fatalf("denied reader saw a channel-filtered restricted document: %+v", res.Hits)
	}

	// Cross-tenant isolation: a same-ID team filter in another tenant must
	// not surface tenant-a's document.
	res, err = s.SearchLexicalFiltered(ctx, "tenant-b", "payroll", "person", "u-stranger", SearchFilters{TeamID: "team-finance"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 0 {
		t.Fatalf("cross-tenant facet leaked a document: %+v", res.Hits)
	}
}

// TestTodo_HUB_030_Integration is the INTEGRATION test for HUB-030: the
// filtered search reaches the real pgtest-backed store, applying the SQL
// filters and per-hit authorization recheck against actual rows.
func TestTodo_HUB_030_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()

	docA, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v := searchFilteredFixture(t, s, ctx, "tenant-a", docA, "Benefits Overview", "# Benefits\n\nHealth and retirement benefits overview.\n", "en-US")
	if _, err := s.ShareDocument(ctx, "tenant-a", docA, "u-author", GrantInput{SubjectKind: "person", SubjectID: "u-anna", Action: ActionRead, Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}

	res, err := s.SearchLexicalFiltered(ctx, "tenant-a", "benefits", "person", "u-anna", SearchFilters{Locale: "en-US", Status: "deployed"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].VersionID != v.ID {
		t.Fatalf("integration filtered search missing deployed row: %+v", res.Hits)
	}

	// Withdraw the version; the store's real trigger/retirement path must
	// remove it from the filtered result exactly as it does for
	// SearchLexical, proving the filter query reaches live DB state.
	if _, err := s.Withdraw(ctx, "tenant-a", WithdrawInput{DocumentID: docA, ScopeKind: "default", ScopeID: "", ActorID: "u-author", ExpectedLive: v.ID, Reason: "superseded"}); err != nil {
		t.Fatal(err)
	}
	res, err = s.SearchLexicalFiltered(ctx, "tenant-a", "benefits", "person", "u-anna", SearchFilters{Locale: "en-US"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 0 {
		t.Fatalf("withdrawn version still filtered-searchable: %+v", res.Hits)
	}
}
