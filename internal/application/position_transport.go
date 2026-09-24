package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	transportposition "github.com/monstercameron/human-capital-management-suite/internal/transport/position"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func positionTransportDependencies(reads PositionReadService) transportposition.Dependencies {
	list := func(page string) func(context.Context, *trust.Principal) ([]transportposition.OptionRead, error) {
		return func(ctx context.Context, principal *trust.Principal) ([]transportposition.OptionRead, error) {
			options, err := reads.ListOptions(ctx, principal, page)
			out := make([]transportposition.OptionRead, 0, len(options))
			for _, option := range options {
				out = append(out, transportposition.OptionRead{
					Reference: option.Reference, PositionID: option.PositionID, Title: option.Title,
					Organization: option.Organization, JobCode: option.JobCode, OrgUnit: option.OrgUnit,
				})
			}
			return out, err
		}
	}
	return transportposition.Dependencies{
		ListObjectOptions:    list(string(productui.PagePositionObject)),
		ListOccupancyOptions: list(string(productui.PagePositionOccupancy)),
		GetObject: func(ctx context.Context, principal *trust.Principal, ref string) (transportposition.ObjectRead, error) {
			read, err := reads.GetObject(ctx, principal, ref)
			return transportposition.ObjectRead{
				PositionID: read.PositionID, Revision: read.Revision, JobCode: read.JobCode,
				OrgUnit: read.OrgUnit, Lifecycle: read.Lifecycle, Compatible: read.Compatible,
			}, err
		},
		GetOccupancy: func(ctx context.Context, principal *trust.Principal, ref string) (transportposition.OccupancyRead, error) {
			read, err := reads.GetOccupancy(ctx, principal, ref)
			out := transportposition.OccupancyRead{
				PositionID: read.PositionID, CapacityFTE: read.CapacityFTE, ConsumedFTE: read.ConsumedFTE,
				AvailableFTE: read.AvailableFTE, CapacityHeads: read.CapacityHeads,
				ConsumedHeads: read.ConsumedHeads, AvailableHeads: read.AvailableHeads,
				Occupants: make([]transportposition.OccupantRead, 0, len(read.Occupants)),
			}
			for _, occupant := range read.Occupants {
				out.Occupants = append(out.Occupants, transportposition.OccupantRead{WorkerID: occupant.WorkerID, FTE: occupant.FTE})
			}
			return out, err
		},
	}
}
