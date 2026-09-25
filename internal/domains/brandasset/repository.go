// Package brandasset defines the tenant-owned brand image revision contract.
package brandasset

import (
	"context"
	"errors"
)

var ErrConflict = errors.New("brandasset: stale revision")

const HistoryPageSize = 50

// Asset is one immutable revision. Content is returned only to a tenant scoped
// reader; it is never part of the appearance preference record.
type Asset struct {
	TenantID      string
	Revision      int
	Name          string
	MediaType     string
	Width, Height int
	Digest        string
	Original      []byte
	Proxy         []byte
	ProxyType     string
	Removed       bool
	Available     bool
}

// Repository stores immutable tenant revisions and a compare-and-set head.
type Repository interface {
	Save(context.Context, string, int, string, Asset) (Asset, error)
	Current(context.Context, string) (Asset, bool, error)
	Read(context.Context, string, string) (Asset, bool, error)
	History(context.Context, string, int) ([]Asset, bool, error)
	Rollback(context.Context, string, int, int, string) (Asset, error)
	Remove(context.Context, string, int, string) (Asset, error)
}
