package documenthubstore

import (
	"context"
	"errors"
	"testing"
)

// TestTodo_HUB_021 is the PRIMARY test for HUB-021: unpinned links track
// the scope's live version while pinned links keep their exact bytes
// across redeploys.
func TestTodo_HUB_021(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	if _, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL"); err != nil {
		t.Fatal(err)
	}
	docB, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	b1, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docB, CreatorID: "u-author", Markdown: "target one\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	b2, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docB, CreatorID: "u-author", Markdown: "target two\n"}, b1.ID)
	if err != nil {
		t.Fatal(err)
	}
	reviewDeploy := func(versionID string) {
		t.Helper()
		if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docB, VersionID: versionID, ScopeKind: "default", ScopeID: "", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docB, SubjectKind: "person", SubjectID: "u-reader", Action: ActionRead, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
			t.Fatal(err)
		}
	}
	reviewDeploy(b1.ID)
	grantDeploy := func() {
		t.Helper()
		if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docB, SubjectKind: "person", SubjectID: "u-deployer", Action: ActionDeploy, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
			t.Fatal(err)
		}
	}
	grantDeploy()
	live := ""
	deploy := func(versionID string) {
		t.Helper()
		if _, err := s.Deploy(ctx, "tenant-a", DeployInput{DocumentID: docB, VersionID: versionID, ScopeKind: "default", ScopeID: "", DeployerID: "u-deployer", ExpectedLive: live}); err != nil {
			t.Fatal(err)
		}
		live = versionID
	}
	deploy(b1.ID)
	link := DocLink{Label: "t", TargetDocID: docB, State: LinkValid}
	res, err := s.ResolveLink(ctx, "tenant-a", "default", "", link, "person", "u-reader")
	if err != nil || res.ResolvedVersionID != b1.ID || res.Pinned {
		t.Fatalf("latest did not track the pointer: %+v err=%v", res, err)
	}
	pinned := DocLink{Label: "t", TargetDocID: docB, PinnedVersion: b1.ID, Block: "intro", State: LinkValid}
	reviewDeploy(b2.ID)
	deploy(b2.ID)
	res, err = s.ResolveLink(ctx, "tenant-a", "default", "", link, "person", "u-reader")
	if err != nil || res.ResolvedVersionID != b2.ID {
		t.Fatalf("latest stuck on replaced version: %+v err=%v", res, err)
	}
	pres, err := s.ResolveLink(ctx, "tenant-a", "default", "", pinned, "person", "u-reader")
	if err != nil || pres.ResolvedVersionID != b1.ID || !pres.Pinned || pres.VersionHash != HashContent("target one\n") || pres.Block != "intro" {
		t.Fatalf("pinned moved with redeploy: %+v err=%v", pres, err)
	}
	if _, err := s.ResolveLink(ctx, "tenant-a", "default", "", link, "person", "u-stranger"); !errors.Is(err, ErrDenied) {
		t.Fatal("resolution leaked to an unauthorized reader")
	}
	if _, err := s.ResolveLink(ctx, "tenant-a", "default", "", DocLink{Label: "t", TargetDocID: "doc-missing", State: LinkValid}, "person", "u-reader"); err == nil {
		t.Fatal("missing target resolved")
	}
}

// TestTodo_HUB_021_Golden is the GOLDEN test for HUB-021: the canonical
// resolution evidence bytes are pinned for readers and auditors.
func TestTodo_HUB_021_Golden(t *testing.T) {
	assertGoldenJSON(t, "testdata/hub021_resolution.golden.json", map[string]string{
		"block": "intro", "pinned": "true", "resolved_version": "docv-fixed",
		"target": "doc-fixed", "version_hash": "9e1e2a8ea86d8c8dcfa4b4a1a0b8e1e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5",
	})
}

// TestTodo_HUB_021_Property is the PROPERTY test for HUB-021: across
// pointer moves, pinned resolutions are constant and unpinned resolutions
// always equal the live pointer.
func TestTodo_HUB_021_Property(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docB, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	base := ""
	for _, body := range []string{"one\n", "two\n", "three\n"} {
		v, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docB, CreatorID: "u-author", Markdown: body}, base)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, v.ID)
		base = v.ID
	}
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docB, VersionID: ids[0], ScopeKind: "default", ScopeID: "", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	for _, g := range []GrantInput{
		{DocumentID: docB, SubjectKind: "person", SubjectID: "u-reader", Action: ActionRead, Effect: EffectAllow, Issuer: "u-owner"},
		{DocumentID: docB, SubjectKind: "person", SubjectID: "u-deployer", Action: ActionDeploy, Effect: EffectAllow, Issuer: "u-owner"},
	} {
		if _, err := s.GrantAction(ctx, "tenant-a", g); err != nil {
			t.Fatal(err)
		}
	}
	live := ""
	for _, id := range ids {
		if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docB, VersionID: id, ScopeKind: "default", ScopeID: "", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Deploy(ctx, "tenant-a", DeployInput{DocumentID: docB, VersionID: id, ScopeKind: "default", ScopeID: "", DeployerID: "u-deployer", ExpectedLive: live}); err != nil {
			t.Fatal(err)
		}
		live = id
		unpinned, err := s.ResolveLink(ctx, "tenant-a", "default", "", DocLink{Label: "t", TargetDocID: docB, State: LinkValid}, "person", "u-reader")
		if err != nil || unpinned.ResolvedVersionID != live {
			t.Fatalf("unpinned != pointer after move: %+v err=%v", unpinned, err)
		}
		pinned, err := s.ResolveLink(ctx, "tenant-a", "default", "", DocLink{Label: "t", TargetDocID: docB, PinnedVersion: ids[0], State: LinkValid}, "person", "u-reader")
		if err != nil || pinned.ResolvedVersionID != ids[0] {
			t.Fatalf("pinned moved: %+v err=%v", pinned, err)
		}
	}
}
