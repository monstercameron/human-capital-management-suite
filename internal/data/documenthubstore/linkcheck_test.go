package documenthubstore

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func setupLinkedDocs(t *testing.T, s *Store) (docA, vA, docB, vB string) {
	t.Helper()
	ctx := context.Background()
	var err error
	docA, err = s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	docB, err = s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	bv, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docB, CreatorID: "u-author", Markdown: "target\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	vB = bv.ID
	av, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docA, CreatorID: "u-author", Markdown: "see [t](doc:" + docB + ") and [gone](doc:doc-missing).\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	vA = av.ID
	if err := s.StoreLinks(ctx, "tenant-a", docA, vA, ExtractLinks("see [t](doc:"+docB+") and [gone](doc:doc-missing).\n")); err != nil {
		t.Fatal(err)
	}
	return docA, vA, docB, vB
}

// TestTodo_HUB_020 is the PRIMARY test for HUB-020: official deploys ship
// only when every canonical link resolves in current policy, while
// placement scopes get warnings instead of refusals.
func TestTodo_HUB_020(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docA, vA, docB, _ := setupLinkedDocs(t, s)
	grant := func(doc, subject, action string) {
		t.Helper()
		if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: doc, SubjectKind: "person", SubjectID: subject, Action: action, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
			t.Fatal(err)
		}
	}
	grant(docB, "u-deployer", ActionRead)
	report, err := s.ValidateLinks(ctx, "tenant-a", docA, vA, "default", "", "person", "u-deployer")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 1 || report.Findings[0].Code != "unknown-target" {
		t.Fatalf("missing-target finding wrong: %+v", report)
	}
	if err := s.Authorize(ctx, "tenant-a", docA, "person", "u-deployer", ActionDeploy); err == nil {
		t.Fatal("setup leaked a deploy grant")
	}
	grant(docA, "u-deployer", ActionDeploy)
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docA, VersionID: vA, ScopeKind: "default", ScopeID: "", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Deploy(ctx, "tenant-a", DeployInput{DocumentID: docA, VersionID: vA, ScopeKind: "default", ScopeID: "", DeployerID: "u-deployer"}); !errors.Is(err, ErrLinksUnresolved) {
		t.Fatalf("official deploy shipped a dead link: %v", err)
	}
	got, err := s.ResolveDeployment(ctx, "tenant-a", docA, "default", "")
	if !errors.Is(err, ErrNoDeployment) || got.VersionID != "" {
		t.Fatalf("refused deploy leaked a pointer: %+v err=%v", got, err)
	}
	report, err = s.ValidateLinks(ctx, "tenant-a", docA, vA, "placement", "chan-A", "person", "u-deployer")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 1 || report.Findings[0].Severity != "warning" {
		t.Fatalf("placement did not surface a scoped warning: %+v", report)
	}
}

// TestTodo_HUB_020_Golden is the GOLDEN test for HUB-020: the canonical
// validation report bytes are pinned for deploy gates and review UIs.
func TestTodo_HUB_020_Golden(t *testing.T) {
	assertGoldenJSON(t, "testdata/hub020_report.golden.json", map[string]any{
		"findings": []map[string]string{
			{"block": "", "code": "unknown-target", "detail": "target document does not exist", "label": "gone", "pinned_version": "", "severity": "error", "target": "doc-missing"},
			{"block": "intro", "code": "retired-target", "detail": "pinned version is retired", "label": "old", "pinned_version": "docv-1", "severity": "warning", "target": "doc-present"},
		},
	})
}

// TestTodo_HUB_020_Property is the PROPERTY test for HUB-020: over
// generated target states, official scope deploys if and only if every
// finding is clear.
func TestTodo_HUB_020_Property(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docA, vA, docB, vB := setupLinkedDocs(t, s)
	cases := []struct {
		name   string
		links  []DocLink
		errors int
	}{
		{"clean", []DocLink{{Label: "t", TargetDocID: docB, State: LinkValid}}, 0},
		{"unknown-target", []DocLink{{Label: "t", TargetDocID: "doc-missing", State: LinkValid}}, 1},
		{"unknown-version", []DocLink{{Label: "t", TargetDocID: docB, PinnedVersion: "docv-missing", State: LinkValid}}, 1},
		{"malformed", []DocLink{{Label: "t", TargetDocID: "../evil", State: LinkMalformed}}, 1},
		{"pinned-live", []DocLink{{Label: "t", TargetDocID: docB, PinnedVersion: vB, State: LinkValid}}, 0},
	}
	_ = vA
	for _, tc := range cases {
		if err := s.StoreLinks(ctx, "tenant-a", docA, vA, tc.links); err != nil {
			t.Fatal(err)
		}
		report, err := s.ValidateLinks(ctx, "tenant-a", docA, vA, "default", "", "person", "u-author")
		if err != nil {
			t.Fatal(err)
		}
		errCount := 0
		for _, f := range report.Findings {
			if f.Severity == "error" {
				errCount++
			}
		}
		if errCount != tc.errors {
			t.Fatalf("%s: %d errors, want %d (%+v)", tc.name, errCount, tc.errors, report.Findings)
		}
	}
}

// TestTodo_HUB_020_Integration proves deployment consults link rows that
// were committed independently of the deploy transaction and rolls back
// every publication side effect when one persisted target is private.
func TestTodo_HUB_020_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	source, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	target, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	old, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: source, CreatorID: "u-author", Markdown: "public version\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: source, VersionID: old.ID, ScopeKind: "default", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: source, SubjectKind: "person", SubjectID: "u-deployer", Action: ActionDeploy, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Deploy(ctx, "tenant-a", DeployInput{DocumentID: source, VersionID: old.ID, ScopeKind: "default", DeployerID: "u-deployer"}); err != nil {
		t.Fatal(err)
	}
	private, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: target, CreatorID: "u-author", Markdown: "confidential title\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: source, CreatorID: "u-author", Markdown: "See [Confidential acquisition](doc:" + target + "@" + private.ID + "#terms).\n"}, old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.StoreLinks(ctx, "tenant-a", source, candidate.ID, ExtractLinks("See [Confidential acquisition](doc:"+target+"@"+private.ID+"#terms).\n")); err != nil {
		t.Fatal(err)
	}
	var persisted int
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM document_link WHERE tenant_id=$1 AND source_document_id=$2 AND source_version_id=$3 AND target_document_id=$4`, "tenant-a", source, candidate.ID, target).Scan(&persisted)
	}); err != nil || persisted != 1 {
		t.Fatalf("link row was not persisted before deployment: count=%d err=%v", persisted, err)
	}
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: source, VersionID: candidate.ID, ScopeKind: "default", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	beforeOutbox := 0
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM document_outbox WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='deployment.published'`, "tenant-a", source).Scan(&beforeOutbox)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Deploy(ctx, "tenant-a", DeployInput{DocumentID: source, VersionID: candidate.ID, ScopeKind: "default", DeployerID: "u-deployer", ExpectedLive: old.ID}); !errors.Is(err, ErrLinksUnresolved) {
		t.Fatalf("official deployment with persisted private link error = %v, want ErrLinksUnresolved", err)
	}
	current, err := s.ResolveDeployment(ctx, "tenant-a", source, "default", "")
	if err != nil || current.VersionID != old.ID {
		t.Fatalf("refused deployment changed active pointer: %+v err=%v", current, err)
	}
	var afterOutbox int
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM document_outbox WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='deployment.published'`, "tenant-a", source).Scan(&afterOutbox)
	}); err != nil || afterOutbox != beforeOutbox {
		t.Fatalf("refused deployment emitted event: before=%d after=%d err=%v", beforeOutbox, afterOutbox, err)
	}
}

// TestTodo_HUB_020_Security proves a restricted link gives its source
// audience no attacker controlled label, target, version, or block metadata.
func TestTodo_HUB_020_Security(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	source, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	target, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	private, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: target, CreatorID: "u-author", Title: "Secret target title", Markdown: "private\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	version, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: source, CreatorID: "u-author", Markdown: "private reference"}, "")
	if err != nil {
		t.Fatal(err)
	}
	link := DocLink{Label: "Secret acquisition plan", TargetDocID: target, PinnedVersion: private.ID, Block: "secret-terms", State: LinkValid}
	if err := s.StoreLinks(ctx, "tenant-a", source, version.ID, []DocLink{link}); err != nil {
		t.Fatal(err)
	}
	report, err := s.ValidateLinks(ctx, "tenant-a", source, version.ID, "default", "", "person", "u-deployer")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 1 {
		t.Fatalf("findings = %+v, want one restricted-reference finding", report.Findings)
	}
	finding := report.Findings[0]
	if finding.Code != LinkInaccessible || finding.Label != "Restricted reference" || finding.Severity != SeverityError {
		t.Fatalf("restricted finding = %+v", finding)
	}
	serialized := finding.Label + " " + finding.TargetDocID + " " + finding.PinnedVersion + " " + finding.Block + " " + finding.Detail
	for _, secret := range []string{"Secret acquisition plan", target, private.ID, "secret-terms", "Secret target title"} {
		if strings.Contains(serialized, secret) {
			t.Errorf("restricted finding leaked %q: %+v", secret, finding)
		}
	}
	if finding.TargetDocID != "" || finding.PinnedVersion != "" || finding.Block != "" {
		t.Fatalf("restricted finding contains target metadata: %+v", finding)
	}
}
