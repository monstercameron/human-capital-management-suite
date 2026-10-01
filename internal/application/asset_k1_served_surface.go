package application

import (
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/asset"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ServedAssetSurface exposes the equipment inventory and physical custody
// contract through the application boundary composed by hcmnext serve. The
// asset domain remains the semantic owner; these method values only make its
// immutable and compare-and-set operations reachable from a served process.
type ServedAssetSurface struct {
	Version           func() int
	NewCustodyStore   func() *asset.CustodyStore
	RegisterInventory func(*asset.CustodyStore, asset.InventoryRevision) error
	Assign            func(*asset.CustodyStore, values.EntityRef, values.EntityRef, string, string, asset.Receipt, asset.Receipt, values.RevisionToken, values.RevisionToken, time.Time) error
	BeginReturn       func(*asset.CustodyStore, values.EntityRef, values.EntityRef, string, string, values.RevisionToken, values.RevisionToken, time.Time) error
	CompleteReturn    func(*asset.CustodyStore, values.EntityRef, string, string, asset.Receipt, asset.Receipt, values.RevisionToken, values.RevisionToken, time.Time) error
	Explain           func(asset.InventoryRevision, []asset.CustodyRevision) (asset.AssetExplanation, error)
}

// NewServedAssetSurface returns the asset capabilities reachable from a
// composed serving application. It creates no process-wide or tenant-wide
// state; callers own each custody store and supply all tenant-bound facts.
func NewServedAssetSurface() ServedAssetSurface {
	return ServedAssetSurface{
		Version:           asset.Version,
		NewCustodyStore:   asset.NewCustodyStore,
		RegisterInventory: (*asset.CustodyStore).RegisterInventory,
		Assign:            (*asset.CustodyStore).Assign,
		BeginReturn:       (*asset.CustodyStore).BeginReturn,
		CompleteReturn:    (*asset.CustodyStore).CompleteReturn,
		Explain:           asset.Explain,
	}
}

// Asset returns the equipment inventory and custody surface exposed by a
// composed application. A nil application has no served capabilities.
func (a *App) Asset() ServedAssetSurface {
	if a == nil {
		return ServedAssetSurface{}
	}
	return NewServedAssetSurface()
}
