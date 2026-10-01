// WTIME-009/010/011/012 and TCLOCK-015 (application-service half): route
// approved time to the destination its profile names -- payroll export
// (timeexport, HR Open and flat formats), a contractor invoice draft
// (contractortime), or an agency/VMS export (agencytime) -- through a
// Destination-keyed Dispatcher, and record the destination's acceptance or
// rejection. Three separate command methods, one per destination, is
// deliberate: it is what makes "contractor hours never reach payroll"
// (WTIME-005's RED) a property of which method a caller can even call,
// rather than a runtime check that could be gotten wrong.
package timecardservice

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/agencytime"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/contractortime"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeexport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// RouteToPayrollRequest builds and dispatches a payroll-bound HR Open
// TimeCard export (WTIME-009/010, TCLOCK-015). Profile must resolve to
// DestinationPayroll; a contractor or agency-temp profile is refused here by
// construction (timeprofile.Validate never lets those categories resolve to
// PAYROLL, and this method checks it again rather than trusting the caller).
type RouteToPayrollRequest struct {
	WorkerRef      string
	Profile        timeprofile.TimeProfile
	Build          timeexport.BuildTimeCardRequest
	IdempotencyKey string
}

func (s Service) RouteToPayroll(ctx context.Context, p *trust.Principal, req RouteToPayrollRequest) (DestinationReceipt, error) {
	if err := validPrincipal(p); err != nil {
		return DestinationReceipt{}, err
	}
	if req.Profile.Destination != timeprofile.DestinationPayroll {
		return DestinationReceipt{}, reject(ErrInvalidRequest, "profile.destination", string(req.Profile.Destination), "profile does not deliver to payroll")
	}
	if req.Profile.Category == timeprofile.CategoryContractor || req.Profile.Category == timeprofile.CategoryAgencyTemp {
		return DestinationReceipt{}, reject(ErrForbidden, "profile.category", string(req.Profile.Category), "contractor and agency-temp time never reaches payroll")
	}
	card, err := timeexport.BuildTimeCard(req.Build)
	if err != nil {
		return DestinationReceipt{}, err
	}
	return s.route(ctx, p, req.WorkerRef, timeprofile.DestinationPayroll, DestinationPayload{Destination: timeprofile.DestinationPayroll, PayrollTimeCard: &card}, req.IdempotencyKey)
}

// RouteToContractorInvoiceRequest builds and dispatches a contractor invoice
// draft (WTIME-011). Profile must resolve to DestinationInvoice.
type RouteToContractorInvoiceRequest struct {
	WorkerRef      string
	Profile        timeprofile.TimeProfile
	Build          contractortime.BuildInvoiceRequest
	IdempotencyKey string
}

func (s Service) RouteToContractorInvoice(ctx context.Context, p *trust.Principal, req RouteToContractorInvoiceRequest) (DestinationReceipt, error) {
	if err := validPrincipal(p); err != nil {
		return DestinationReceipt{}, err
	}
	if req.Profile.Destination != timeprofile.DestinationInvoice {
		return DestinationReceipt{}, reject(ErrInvalidRequest, "profile.destination", string(req.Profile.Destination), "profile does not deliver to a contractor invoice")
	}
	draft, err := contractortime.BuildInvoice(req.Build)
	if err != nil {
		return DestinationReceipt{}, err
	}
	return s.route(ctx, p, req.WorkerRef, timeprofile.DestinationInvoice, DestinationPayload{Destination: timeprofile.DestinationInvoice, ContractorDraft: &draft}, req.IdempotencyKey)
}

// RouteToAgencyExportRequest builds and dispatches an agency/VMS export
// (WTIME-012, TCLOCK-015). Profile must resolve to DestinationAgency.
type RouteToAgencyExportRequest struct {
	WorkerRef      string
	Profile        timeprofile.TimeProfile
	Build          agencytime.BuildExportRequest
	IdempotencyKey string
}

func (s Service) RouteToAgencyExport(ctx context.Context, p *trust.Principal, req RouteToAgencyExportRequest) (DestinationReceipt, error) {
	if err := validPrincipal(p); err != nil {
		return DestinationReceipt{}, err
	}
	if req.Profile.Destination != timeprofile.DestinationAgency {
		return DestinationReceipt{}, reject(ErrInvalidRequest, "profile.destination", string(req.Profile.Destination), "profile does not deliver to an agency or VMS export")
	}
	payload, err := agencytime.BuildExport(req.Build)
	if err != nil {
		return DestinationReceipt{}, err
	}
	return s.route(ctx, p, req.WorkerRef, timeprofile.DestinationAgency, DestinationPayload{Destination: timeprofile.DestinationAgency, AgencyExport: &payload}, req.IdempotencyKey)
}

// route is the shared authorize/dispatch/record path every destination
// command runs.
func (s Service) route(ctx context.Context, p *trust.Principal, workerRef string, dest timeprofile.Destination, payload DestinationPayload, idempotencyKey string) (DestinationReceipt, error) {
	if s.Ledger == nil {
		return DestinationReceipt{}, ErrUnavailable
	}
	if strings.TrimSpace(idempotencyKey) == "" {
		return DestinationReceipt{}, ErrInvalidRequest
	}
	tenant := tenantOf(p)
	if err := s.authorize(ctx, p, tenant, workerRef, CapRouteTime); err != nil {
		return DestinationReceipt{}, err
	}
	dispatcher, err := s.dispatcherFor(ctx, tenant, dest)
	if err != nil {
		return DestinationReceipt{}, err
	}
	receipt, err := dispatcher.Dispatch(ctx, tenant, payload)
	if err != nil {
		return DestinationReceipt{}, err
	}
	if err := s.Ledger.RecordReceipt(ctx, tenant, receipt, idempotencyKey); err != nil {
		return DestinationReceipt{}, err
	}
	if !receipt.Accepted {
		return receipt, reject(ErrDestinationRejected, "receipt", string(dest), receipt.Reason)
	}
	return receipt, nil
}
