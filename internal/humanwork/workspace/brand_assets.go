package workspace

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"path"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/brandasset"
)

// BrandAssetRepository is the tenant scoped durable library used by the served
// picker. Upload bytes and revision history are never stored in preferences.
type BrandAssetRepository = brandasset.Repository

const (
	BrandAssetMaxBytes = 2 << 20
	BrandAssetMaxSide  = 1024
	BrandAssetMinSide  = 16
)

var (
	ErrBrandAssetInvalidType       = errors.New("workspace: unsupported brand asset type")
	ErrBrandAssetInvalidDimensions = errors.New("workspace: invalid brand asset dimensions")
	ErrBrandAssetUnsafe            = errors.New("workspace: unsafe brand asset")
	ErrBrandAssetUnauthorized      = errors.New("workspace: brand asset tenant mismatch")
	ErrBrandAssetVersionConflict   = brandasset.ErrConflict
)

// BrandAssetUpload is the server-approved input to the appearance picker.
// Bytes are scanned and validated before this contract is called; the helper
// below repeats cheap content checks so an adapter cannot persist forged MIME
// or dimensions.
type BrandAssetUpload struct {
	TenantID string
	Name     string
	Bytes    []byte
}

type BrandAsset struct {
	TenantID  string
	Name      string
	MediaType string
	Width     int
	Height    int
	Digest    string
	Original  string
	Proxy     string
}

// ValidateBrandAsset validates bytes at the server boundary and returns
// immutable identity plus responsive proxy dimensions. It does not expose
// the original bytes or permit remote URLs.
func ValidateBrandAsset(upload BrandAssetUpload) (BrandAsset, error) {
	if strings.TrimSpace(upload.TenantID) == "" || strings.TrimSpace(upload.Name) == "" || len(upload.Bytes) == 0 || len(upload.Bytes) > BrandAssetMaxBytes {
		return BrandAsset{}, ErrBrandAssetUnsafe
	}
	name := strings.TrimSpace(upload.Name)
	if strings.ContainsAny(name, `/\\?#%`) || name != strings.TrimSpace(name) {
		return BrandAsset{}, ErrBrandAssetUnsafe
	}
	media, ok := brandAssetMedia(name)
	if !ok {
		return BrandAsset{}, ErrBrandAssetInvalidType
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(upload.Bytes))
	wantFormat := strings.TrimPrefix(strings.ToLower(path.Ext(name)), ".")
	if wantFormat == "jpg" {
		wantFormat = "jpeg"
	}
	if err != nil || (format != "png" && format != "jpeg" && format != "webp") || format != wantFormat {
		if err == nil && format != wantFormat {
			return BrandAsset{}, ErrBrandAssetInvalidType
		}
		return BrandAsset{}, ErrBrandAssetUnsafe
	}
	if config.Width < BrandAssetMinSide || config.Height < BrandAssetMinSide || config.Width > BrandAssetMaxSide || config.Height > BrandAssetMaxSide {
		return BrandAsset{}, ErrBrandAssetInvalidDimensions
	}
	digest := sha256.Sum256(upload.Bytes)
	return BrandAsset{TenantID: upload.TenantID, Name: name, MediaType: media, Width: config.Width, Height: config.Height, Digest: hex.EncodeToString(digest[:]), Original: "brand-original-" + hex.EncodeToString(digest[:8]) + "." + strings.ToLower(format), Proxy: "brand-proxy-" + hex.EncodeToString(digest[:8]) + ".jpg"}, nil
}

func brandAssetMedia(name string) (string, bool) {
	switch strings.ToLower(path.Ext(name)) {
	case ".png":
		return "image/png", true
	case ".jpg", ".jpeg":
		return "image/jpeg", true
	case ".webp":
		return "image/webp", true
	default:
		return "", false
	}
}

// BrandAssetRevisionStore is a tenant-scoped immutable revision head. The
// durable appearance store remains the source of published theme state; this
// adapter provides safe asset lifecycle/CAS semantics for its integration.
type BrandAssetRevisionStore struct {
	mu   sync.Mutex
	head map[string][]BrandAsset
}

func NewBrandAssetRevisionStore() *BrandAssetRevisionStore {
	return &BrandAssetRevisionStore{head: make(map[string][]BrandAsset)}
}

func (s *BrandAssetRevisionStore) Save(expected int, asset BrandAsset) (BrandAsset, int, error) {
	if s == nil || strings.TrimSpace(asset.TenantID) == "" || asset.Digest == "" {
		return BrandAsset{}, 0, ErrBrandAssetUnauthorized
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	revisions := s.head[asset.TenantID]
	if expected != len(revisions) {
		return BrandAsset{}, len(revisions), ErrBrandAssetVersionConflict
	}
	s.head[asset.TenantID] = append(revisions, asset)
	return asset, len(revisions) + 1, nil
}

func (s *BrandAssetRevisionStore) Current(tenant string) (BrandAsset, int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	revisions := s.head[tenant]
	if len(revisions) == 0 {
		return BrandAsset{}, 0, false
	}
	return revisions[len(revisions)-1], len(revisions), true
}

func (s *BrandAssetRevisionStore) Rollback(tenant string, expected, revision int) (BrandAsset, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	revisions := s.head[tenant]
	if expected != len(revisions) {
		return BrandAsset{}, len(revisions), ErrBrandAssetVersionConflict
	}
	if revision < 1 || revision > len(revisions) {
		return BrandAsset{}, len(revisions), ErrBrandAssetVersionConflict
	}
	asset := revisions[revision-1]
	s.head[tenant] = append(revisions, asset)
	return asset, len(revisions) + 1, nil
}

func (s *BrandAssetRevisionStore) Remove(tenant string, expected int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	revisions := s.head[tenant]
	if expected != len(revisions) {
		return len(revisions), ErrBrandAssetVersionConflict
	}
	if len(revisions) == 0 {
		return 0, nil
	}
	s.head[tenant] = append(revisions, BrandAsset{TenantID: tenant})
	return len(revisions) + 1, nil
}
