package artifacts

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
	"github.com/monstercameron/human-capital-management-suite/internal/store/object"
)

func TestTodo_ARTIFACT_001_Served(t *testing.T) {
	content := []byte("provider-neutral served artifact")
	contentID := MultipartDigest(content)
	catalog, err := NewObjectDownloadCatalog(object.NewMemoryStore())
	if err != nil {
		t.Fatalf("NewObjectDownloadCatalog: %v", err)
	}
	if err := catalog.Register(context.Background(), DownloadRecord{
		TenantID: "tenant-a", ContentID: contentID, Revision: 1, Bytes: content,
		Classification: model.ClassInternal, State: DownloadAvailable,
		AllowedPurposes: []string{"documents.read"},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	reader, receipt, err := catalog.Open(context.Background(), DownloadRequest{Grant: DownloadGrant{
		TenantID: "tenant-a", Principal: "person-a", Purpose: "documents.read", ContentID: contentID,
		Revision: 1, AllowedClassifications: []model.ClassificationLabel{model.ClassInternal}, ExpiresAt: time.Unix(200, 0).UTC(),
	}, Start: 9}, time.Unix(100, 0).UTC())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(content[9:]) || receipt.ByteCount != int64(len(got)) || receipt.FullDigest != contentID || receipt.RangeDigest != MultipartDigest(got) {
		t.Fatalf("served download got bytes=%q receipt=%+v", got, receipt)
	}
}

func TestTodo_ARTIFACT_003_ObjectStore(t *testing.T) {
	content := []byte("scoped object-backed bytes")
	contentID := MultipartDigest(content)
	catalog, err := NewObjectDownloadCatalog(object.NewMemoryStore())
	if err != nil {
		t.Fatalf("NewObjectDownloadCatalog: %v", err)
	}
	if err := catalog.Register(context.Background(), DownloadRecord{
		TenantID: "tenant-a", ContentID: contentID, Revision: 1, Bytes: content,
		Classification: model.ClassInternal, State: DownloadAvailable,
		AllowedPurposes: []string{"documents.read"},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	_, _, err = catalog.Open(context.Background(), DownloadRequest{Grant: DownloadGrant{
		TenantID: "tenant-b", Principal: "person-b", Purpose: "documents.read", ContentID: contentID,
		Revision: 1, AllowedClassifications: []model.ClassificationLabel{model.ClassInternal}, ExpiresAt: time.Unix(200, 0).UTC(),
	}}, time.Unix(100, 0).UTC())
	if !errors.Is(err, ErrDownloadNotFound) {
		t.Fatalf("cross-tenant Open error = %v, want ErrDownloadNotFound", err)
	}
}
