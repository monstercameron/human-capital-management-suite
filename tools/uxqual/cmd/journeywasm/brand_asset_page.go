package main

import (
	"encoding/json"
	"strconv"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// brandAssetPageWire is the GET /workspace/brand-assets answer. Decoding it
// in Go rather than reading js.Value fields keeps an omitted field -- the
// server leaves next_before out on the last page -- a zero instead of a
// syscall/js panic that exits the whole WASM client.
type brandAssetPageWire struct {
	Assets []struct {
		Revision     int    `json:"revision"`
		HeadRevision int    `json:"head_revision"`
		Digest       string `json:"digest"`
		Name         string `json:"name"`
		URL          string `json:"url"`
		Width        int    `json:"width"`
		Height       int    `json:"height"`
		Removed      bool   `json:"removed"`
		CanRollback  bool   `json:"can_rollback"`
		CanRemove    bool   `json:"can_remove"`
	} `json:"assets"`
	NextBefore int `json:"next_before"`
}

// decodeBrandAssetPage projects one page of brand-asset revisions into the
// picker's options, skipping removed revisions, and returns the cursor for
// the next page (zero on the last one).
func decodeBrandAssetPage(body []byte, locale productui.LocaleContext) ([]productui.BrandAssetOption, int, error) {
	var wire brandAssetPageWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, 0, err
	}
	assets := make([]productui.BrandAssetOption, 0, len(wire.Assets))
	for _, row := range wire.Assets {
		if row.Removed {
			continue
		}
		assets = append(assets, productui.BrandAssetOption{
			Label: row.Name + " · " + locale.Text("appearance.asset_revision_label", map[string]string{"revision": strconv.Itoa(row.Revision)}),
			URL:   row.URL, Revision: row.Revision, HeadRevision: row.HeadRevision,
			CanRollback: row.CanRollback, CanRemove: row.CanRemove, Digest: row.Digest,
			Width: row.Width, Height: row.Height,
		})
	}
	return assets, wire.NextBefore, nil
}
