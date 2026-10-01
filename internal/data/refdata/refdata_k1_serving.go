package refdata

import (
	"context"
	"time"

	"github.com/google/uuid"

	dataref "github.com/monstercameron/human-capital-management-suite/internal/domains/refdata"
)

// ServingContractID identifies the durable reference-data adapter linked by
// the running application cell. The check deliberately performs no database
// I/O; database lifecycle coverage remains in the integration tests.
const ServingContractID = "hcmnext.conformance.reference-data-store/v1"

// ServingPort is the complete durable lifecycle surface required by serving
// projections. Keeping it here makes an adapter omission a compile-time
// failure instead of an unreachability gap discovered after deployment.
type ServingPort interface {
	PutRelease(context.Context, dataref.Release) error
	GetRelease(context.Context, string, string) (dataref.Release, bool, error)
	Adopt(context.Context, uuid.UUID, dataref.Release, dataref.AdoptionRequest, time.Time) (dataref.Adoption, error)
	Rollback(context.Context, uuid.UUID, dataref.Release, string, time.Time, time.Time) (dataref.Adoption, error)
	LatestAdoption(context.Context, uuid.UUID, string) (dataref.Adoption, bool, error)
	ListAdoptions(context.Context, uuid.UUID, string) ([]dataref.Adoption, error)
}

// ValidateServingContract verifies that the production adapter exposes every
// operation needed by a serving projection without constructing a connection.
func ValidateServingContract() error {
	var _ ServingPort = (*Store)(nil)
	return nil
}
