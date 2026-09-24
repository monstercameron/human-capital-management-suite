package chatmedia

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/asset/quarantine"
)

func TestLegacyFilesystemPNGCanBeReadAsGrantedRenditions(t *testing.T) {
	ctx := context.Background()
	store, err := NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := New(Config{
		Store: store, Scanner: testScanner{verdict: quarantine.Verdict{Safe: true}},
		Authorize: func(_ context.Context, req AccessRequest) error {
			if req.PrincipalID != "reader" {
				return ErrUnauthorized
			}
			return nil
		},
	})
	original := validChatPNG(t, 800, 400, false)
	ref, err := s.Upload(ctx, UploadRequest{
		TenantID: "tenant", ConversationID: "room", PrincipalID: "reader", DeclaredType: "image/png", Content: original,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Model an admitted artifact created before rendition metadata/files were
	// added to the filesystem format.
	_, metadataPath := store.paths(ref.ArtifactID)
	metadata, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	var artifact Artifact
	if err := json.Unmarshal(metadata, &artifact); err != nil {
		t.Fatal(err)
	}
	artifact.Renditions = nil
	metadata, err = json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metadataPath, metadata, 0600); err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{VariantThumbnail, VariantDisplay} {
		if err := os.Remove(filepath.Join(store.root, ref.ArtifactID+"."+variant)); err != nil {
			t.Fatal(err)
		}
	}

	grant, err := s.Authorize(ctx, AccessRequest{
		TenantID: "tenant", ConversationID: "room", PrincipalID: "reader", ArtifactID: ref.ArtifactID,
	}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	request := AccessRequest{
		TenantID: "tenant", ConversationID: "room", PrincipalID: "reader", ArtifactID: ref.ArtifactID, Grant: grant.Token,
	}
	for _, variant := range []string{VariantThumbnail, VariantDisplay} {
		wantWidth, wantHeight := 640, 320
		if variant == VariantDisplay {
			wantWidth, wantHeight = 800, 400
		}
		body, got, err := s.OpenVariant(ctx, request, variant, 0, nil)
		if err != nil {
			t.Fatalf("legacy %s rendition: %v", variant, err)
		}
		content, err := io.ReadAll(body)
		_ = body.Close()
		if err != nil {
			t.Fatal(err)
		}
		config, format, err := image.DecodeConfig(bytes.NewReader(content))
		if err != nil || format != "jpeg" || got.MediaType != MediaJPEG || config.Width != wantWidth || config.Height != wantHeight {
			t.Fatalf("legacy %s rendition format=%q type=%q size=%dx%d err=%v", variant, format, got.MediaType, config.Width, config.Height, err)
		}
	}
	denied := request
	denied.PrincipalID = "other"
	if _, _, err := s.OpenVariant(ctx, denied, VariantThumbnail, 0, nil); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("different principal could read a generated rendition: %v", err)
	}

	// Fallback generation is read-only: subsequent requests still see the old
	// metadata without persisted rendition files.
	if _, err := store.GetRendition(ctx, "tenant", ref.ArtifactID, VariantThumbnail); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("legacy record was unexpectedly rewritten: %v", err)
	}
}
