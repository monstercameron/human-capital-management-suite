package lineage

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	datalogger "github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
)

// Append validates a CORRECTION request's target - refusing a dangling or
// cross-tenant reference before anything is written - and then forwards to
// internal/data/ledger.Append unchanged. Every other assertion class passes
// through untouched: lineage only has an opinion about corrections.
//
// internal/data/ledger.Append (LEDGER-004) already refuses a CORRECTION
// request with no target at all, or a non-CORRECTION request that carries
// one; Append here adds the one check that requires a database round trip -
// that the named target actually exists for this tenant - which the
// in-memory validation in append.go cannot perform on its own.
//
// Append writes nothing itself and exposes no update or delete: the only
// effect of a successful call is the single INSERT
// internal/data/ledger.Appender.Append performs. An optional appender lets a
// caller preserve its configured clock and digest behavior; absent one, the
// default ledger appender is used.
func Append(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, req datalogger.AppendRequest, appenders ...*datalogger.Appender) (datalogger.AppendReceipt, error) {
	if req.Tenant != tenant {
		return datalogger.AppendReceipt{}, fmt.Errorf("lineage: tenant argument does not match append request")
	}
	if len(appenders) > 1 {
		return datalogger.AppendReceipt{}, fmt.Errorf("lineage: at most one ledger appender may be supplied")
	}
	if req.AssertionClass == datalogger.Correction && req.Corrects != nil {
		if _, err := ValidateCorrectionTarget(ctx, tx, tenant, *req.Corrects); err != nil {
			return datalogger.AppendReceipt{}, err
		}
	}
	appender := datalogger.New()
	if len(appenders) == 1 {
		if appenders[0] == nil {
			return datalogger.AppendReceipt{}, fmt.Errorf("lineage: supplied ledger appender is nil")
		}
		appender = appenders[0]
	}
	return appender.Append(ctx, tx, req)
}
