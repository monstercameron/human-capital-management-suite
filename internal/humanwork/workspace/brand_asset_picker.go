package workspace

import "strconv"

// AssetPreviewVariant is one governed rendering of a validated brand asset.
// The picker serves only these content-derived variants: an administrator
// picks from validated uploads and previews, never by typing a path.
type AssetPreviewVariant struct {
	Name     string
	Original string
	Proxy    string
	Width    int
	Height   int
}

// PreviewAssetVariants returns the shell, favicon, compact and
// light/dark-contrast previews for a validated asset. Every variant retains
// the asset's original and responsive-proxy identities so the preview and
// the published theme resolve to the same bytes.
func PreviewAssetVariants(asset BrandAsset) []AssetPreviewVariant {
	if asset.Digest == "" {
		return nil
	}
	names := []string{"shell", "favicon", "compact", "contrast-light", "contrast-dark"}
	variants := make([]AssetPreviewVariant, 0, len(names))
	for _, name := range names {
		variants = append(variants, AssetPreviewVariant{
			Name:     name,
			Original: asset.Original,
			Proxy:    asset.Proxy,
			Width:    asset.Width,
			Height:   asset.Height,
		})
	}
	return variants
}

// BrandAssetChange is one exact field difference between two asset revisions.
// The Appearance save/publish action stays beside this diff so an
// administrator approves content, not a path.
type BrandAssetChange struct {
	Field string
	From  string
	To    string
}

// DiffBrandAssets reports the exact field differences between two revisions.
// Identical revisions produce no changes.
func DiffBrandAssets(before, after BrandAsset) []BrandAssetChange {
	fields := []struct {
		name          string
		before, after string
	}{
		{"name", before.Name, after.Name},
		{"media_type", before.MediaType, after.MediaType},
		{"width", strconv.Itoa(before.Width), strconv.Itoa(after.Width)},
		{"height", strconv.Itoa(before.Height), strconv.Itoa(after.Height)},
		{"digest", before.Digest, after.Digest},
		{"original", before.Original, after.Original},
		{"proxy", before.Proxy, after.Proxy},
	}
	var out []BrandAssetChange
	for _, field := range fields {
		if field.before == field.after {
			continue
		}
		out = append(out, BrandAssetChange{Field: field.name, From: field.before, To: field.after})
	}
	return out
}
