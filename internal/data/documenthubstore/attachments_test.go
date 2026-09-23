package documenthubstore

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestTodo_HUB_024 is the PRIMARY test for HUB-024: attachments bind an
// admitted quarantine artifact to a version hash, open only for current
// readers, and never carry a public object URL.
func TestTodo_HUB_024(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v1, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "one\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	admitted := AttachmentInput{
		DocumentID: docID, VersionID: v1.ID, ActorID: "u-author",
		ArtifactID: "sha256:abc", Filename: "fig.png", ContentType: "image/png",
		SizeBytes: 12, QuarantineState: "ADMITTED", ScannerVersion: "scan-7",
	}
	stranger := admitted
	stranger.ActorID = "u-stranger"
	if _, err := s.AttachArtifact(ctx, "tenant-a", stranger); !errors.Is(err, ErrDenied) {
		t.Fatalf("attach without grant: %v", err)
	}
	for _, action := range []string{ActionRead, ActionPropose} {
		if _, err := s.ShareDocument(ctx, "tenant-a", docID, "u-author", GrantInput{SubjectKind: "person", SubjectID: "u-anna", Action: action, Effect: EffectAllow}); err != nil {
			t.Fatal(err)
		}
	}
	quarantined := admitted
	quarantined.ActorID = "u-anna"
	quarantined.QuarantineState = "QUARANTINED"
	if _, err := s.AttachArtifact(ctx, "tenant-a", quarantined); !errors.Is(err, ErrAttachmentUnsafe) {
		t.Fatalf("unscanned bytes attached: %v", err)
	}
	remote := admitted
	remote.ActorID = "u-anna"
	remote.ArtifactID = "https://cdn.example.com/fig.png"
	if _, err := s.AttachArtifact(ctx, "tenant-a", remote); !errors.Is(err, ErrAttachmentUnsafe) {
		t.Fatalf("remote URL attached: %v", err)
	}
	granted := admitted
	granted.ActorID = "u-anna"
	a, err := s.AttachArtifact(ctx, "tenant-a", granted)
	if err != nil {
		t.Fatalf("admitted attach refused: %v", err)
	}
	if a.VersionHash != v1.Hash || a.Classification != "INTERNAL" {
		t.Fatalf("attachment does not bind version evidence: %+v", a)
	}
	opened, err := s.OpenAttachment(ctx, "tenant-a", docID, a.ID, "person", "u-stranger")
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("ungranted read opened: %+v err=%v", opened, err)
	}
	opened, err = s.OpenAttachment(ctx, "tenant-a", docID, a.ID, "person", "u-anna")
	if err != nil {
		t.Fatalf("granted read refused: %v", err)
	}
	if opened.ArtifactID != "sha256:abc" || opened.Filename != "fig.png" {
		t.Fatalf("descriptor wrong: %+v", opened)
	}
}

// TestTodo_HUB_024_Security is the SECURITY test for HUB-024: revoked
// readers, reclassified versions and cross-tenant opens all fail, and a
// remote image URL never renders into served Markdown.
func TestTodo_HUB_024_Security(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v1, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "one\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	var readGrant Grant
	for _, action := range []string{ActionRead, ActionPropose} {
		g, err := s.ShareDocument(ctx, "tenant-a", docID, "u-author", GrantInput{SubjectKind: "person", SubjectID: "u-anna", Action: action, Effect: EffectAllow})
		if err != nil {
			t.Fatal(err)
		}
		if action == ActionRead {
			readGrant = g
		}
	}
	a, err := s.AttachArtifact(ctx, "tenant-a", AttachmentInput{
		DocumentID: docID, VersionID: v1.ID, ActorID: "u-anna",
		ArtifactID: "sha256:abc", Filename: "fig.png", ContentType: "image/png",
		SizeBytes: 12, QuarantineState: "ADMITTED", ScannerVersion: "scan-7",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenAttachment(ctx, "tenant-b", docID, a.ID, "person", "u-anna"); !errors.Is(err, ErrDenied) {
		t.Fatalf("cross-tenant open: %v", err)
	}
	if err := s.RevokeGrant(ctx, "tenant-a", readGrant.ID, "u-author"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenAttachment(ctx, "tenant-a", docID, a.ID, "person", "u-anna"); !errors.Is(err, ErrDenied) {
		t.Fatalf("revoked reader opened: %v", err)
	}
	rendered, err := RenderMarkdown("![fig](https://cdn.example.com/fig.png)\n")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered, "cdn.example.com") || strings.Contains(rendered, "<img") {
		t.Fatalf("remote image survived render: %s", rendered)
	}
	rendered, err = RenderMarkdown("![fig](artifact:sha256:abc)\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered, "artifact:sha256:abc") {
		t.Fatalf("admitted artifact ref lost: %s", rendered)
	}
}

// TestTodo_HUB_024_Integration is the INTEGRATION test for HUB-024: the
// attach/open roundtrip reaches the real store and reclassification
// revokes usability without rewriting history.
func TestTodo_HUB_024_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v1, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "one\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{ActionRead, ActionPropose} {
		if _, err := s.ShareDocument(ctx, "tenant-a", docID, "u-author", GrantInput{SubjectKind: "person", SubjectID: "u-anna", Action: action, Effect: EffectAllow}); err != nil {
			t.Fatal(err)
		}
	}
	a, err := s.AttachArtifact(ctx, "tenant-a", AttachmentInput{
		DocumentID: docID, VersionID: v1.ID, ActorID: "u-anna",
		ArtifactID: "sha256:abc", Filename: "fig.png", ContentType: "image/png",
		SizeBytes: 12, QuarantineState: "ADMITTED", ScannerVersion: "scan-7",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenAttachment(ctx, "tenant-a", docID, a.ID, "person", "u-anna"); err != nil {
		t.Fatalf("open before reclassify refused: %v", err)
	}
	if _, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "one\n", Classification: "RESTRICTED"}, v1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenAttachment(ctx, "tenant-a", docID, a.ID, "person", "u-anna"); !errors.Is(err, ErrAttachmentStale) {
		t.Fatalf("reclassified attachment still usable: %v", err)
	}
}
