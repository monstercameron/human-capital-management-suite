// Package contractortime coordinates the application boundary for
// contractor invoices and agency-temp time delivery. Domain packages build
// and validate the immutable pricing/export payloads; this package performs
// authorization, connector binding, idempotent recording and observation.
package contractortime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/agencytime"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/contractortime"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var (
	ErrInvalidPrincipal  = errors.New("contractortime: trusted principal required")
	ErrInvalidRequest    = errors.New("contractortime: invalid request")
	ErrUnavailable       = errors.New("contractortime: required port unavailable")
	ErrDeliveryRejected  = errors.New("contractortime: destination rejected delivery")
	ErrDeliveryMalformed = errors.New("contractortime: destination response was malformed")
)

// Capability is the closed set of application actions used by this package.
// A connector is never called until the relevant capability has succeeded.
type Capability string

const (
	CapContractorApprove  Capability = "CONTRACTOR_TIME_APPROVE"
	CapContractorDispatch Capability = "CONTRACTOR_INVOICE_DISPATCH"
	CapAgencyApprove      Capability = "AGENCY_TIME_APPROVE"
	CapAgencyDispatch     Capability = "AGENCY_VMS_DISPATCH"
)

// Authorizer is backed by current tenant policy; the service does not trust
// roles or tenant identifiers supplied in a request.
type Authorizer interface {
	Authorize(context.Context, *trust.Principal, string, string, Capability) error
}

type DeliveryKind string

const (
	KindContractorInvoice DeliveryKind = "CONTRACTOR_INVOICE"
	KindAgencyExport      DeliveryKind = "AGENCY_EXPORT"
)

type DeliveryStatus string

const (
	StatusDraft     DeliveryStatus = "DRAFT"
	StatusSent      DeliveryStatus = "SENT"
	StatusAccepted  DeliveryStatus = "ACCEPTED"
	StatusRejected  DeliveryStatus = "REJECTED"
	StatusMalformed DeliveryStatus = "MALFORMED"
)

// DeliveryRecord is the application-neutral durable shape. A data adapter
// can map it to timestore's destination_record without importing this layer.
type DeliveryRecord struct {
	TenantID         string
	ID               string
	Kind             DeliveryKind
	SourceRef        string
	SourceRevision   int64
	ReceiverRef      string
	Status           DeliveryStatus
	Revision         int64
	PayloadDigest    string
	Payload          json.RawMessage
	IdempotencyKey   string
	ApprovalActor    string
	ApprovalRevision string
	ApprovalAt       time.Time
	ReceiptRef       string
	Reason           string
}

type Receipt struct {
	TenantID      string
	DestinationID string
	Status        DeliveryStatus
	ReceiverRef   string
	ReceiptRef    string
	Reason        string
	RawDigest     string
	At            time.Time
}

// DeliveryStore owns tenant scoping, idempotency and append-only receipt
// recording. Create must return the existing record on an identical replay.
type DeliveryStore interface {
	Create(context.Context, string, DeliveryRecord) (DeliveryRecord, error)
	RecordReceipt(context.Context, string, Receipt) (DeliveryRecord, error)
}

// DeliveryPayload is the only payload a connector receives. Exactly one
// typed domain payload is populated by the service.
type DeliveryPayload struct {
	Kind          DeliveryKind
	Invoice       *contractortime.InvoiceDraft
	Agency        *agencytime.ExportPayload
	Parity        agencytime.ParityObligation
	PayloadDigest string
}

// DeliveryObservation is returned by a connector after its external call.
// Agency/VMS connectors may return Raw and let the service classify it with
// agencytime.ParseVMSResponse, preserving malformed responses as evidence.
type DeliveryObservation struct {
	Status      DeliveryStatus
	ExternalRef string
	Reason      string
	Raw         []byte
	RawDigest   string
}

type Dispatcher interface {
	Dispatch(context.Context, string, string, DeliveryPayload, string) (DeliveryObservation, error)
}

// BindingResolver resolves a tenant-owned connector binding. The binding
// reference is request data, but the resolver must enforce tenant ownership.
type BindingResolver interface {
	Resolve(context.Context, string, DeliveryKind, string) (Dispatcher, error)
}

type Service struct {
	Auth     Authorizer
	Store    DeliveryStore
	Bindings BindingResolver
	Clock    func() time.Time
}

type ContractorInvoiceRequest struct {
	Build            contractortime.BuildInvoiceRequest
	SourceRef        string
	SourceRevision   int64
	BindingRef       string
	ApprovalRevision string
	IdempotencyKey   string
}

type ContractorInvoiceResult struct {
	Invoice     contractortime.InvoiceDraft
	Record      DeliveryRecord
	Observation DeliveryObservation
}

type AgencyDeliveryRequest struct {
	Build            agencytime.BuildExportRequest
	SourceRef        string
	SourceRevision   int64
	BindingRef       string
	ApprovalRevision string
	QualifyingWeeks  int
	IdempotencyKey   string
}

type AgencyDeliveryResult struct {
	Export      agencytime.ExportPayload
	Parity      agencytime.ParityObligation
	Record      DeliveryRecord
	Observation DeliveryObservation
}

func (s Service) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func validatePrincipal(p *trust.Principal) error {
	if p == nil || p.Subject() == "" || string(p.Tenant()) == "" {
		return ErrInvalidPrincipal
	}
	return nil
}

func validateCommon(sourceRef, bindingRef, approvalRevision, key string, sourceRevision int64) error {
	if strings.TrimSpace(sourceRef) == "" || strings.TrimSpace(bindingRef) == "" ||
		strings.TrimSpace(approvalRevision) == "" || strings.TrimSpace(key) == "" || sourceRevision <= 0 {
		return ErrInvalidRequest
	}
	return nil
}

func stableID(tenant, actor, operation, key string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{tenant, actor, operation, key}, "\x00")))
	return hex.EncodeToString(sum[:16])
}

func (s Service) ready(ctx context.Context, p *trust.Principal, workerRef string, cap Capability) (string, error) {
	if err := validatePrincipal(p); err != nil {
		return "", err
	}
	if s.Auth == nil || s.Store == nil || s.Bindings == nil {
		return "", ErrUnavailable
	}
	if strings.TrimSpace(workerRef) == "" {
		return "", ErrInvalidRequest
	}
	tenant := string(p.Tenant())
	if err := s.Auth.Authorize(ctx, p, tenant, workerRef, cap); err != nil {
		return "", err
	}
	return tenant, nil
}

func (s Service) createRecord(ctx context.Context, tenant string, record DeliveryRecord) (DeliveryRecord, bool, error) {
	record, err := s.Store.Create(ctx, tenant, record)
	if err != nil {
		return DeliveryRecord{}, false, err
	}
	terminal := record.Status == StatusAccepted || record.Status == StatusRejected || record.Status == StatusMalformed
	return record, terminal, nil
}

func observationFor(kind DeliveryKind, in DeliveryObservation) DeliveryObservation {
	if kind == KindAgencyExport && len(in.Raw) != 0 {
		parsed := agencytime.ParseVMSResponse(in.Raw)
		out := DeliveryObservation{ExternalRef: parsed.ExternalRef, Reason: parsed.RejectionReason, RawDigest: parsed.RawDigest}
		switch parsed.Status {
		case agencytime.ResponseAccepted:
			out.Status = StatusAccepted
		case agencytime.ResponseRejected:
			out.Status = StatusRejected
		default:
			out.Status = StatusMalformed
		}
		return out
	}
	if in.Status == "" {
		in.Status = StatusMalformed
		if in.Reason == "" {
			in.Reason = "connector returned no classified response"
		}
	}
	return in
}

func (s Service) deliver(ctx context.Context, tenant string, p DeliveryPayload, kind DeliveryKind, sourceRef string, sourceRevision int64, bindingRef, key, approvalActor, approvalRevision string, approvalAt time.Time) (DeliveryRecord, DeliveryObservation, error) {
	if s.Bindings == nil || s.Store == nil {
		return DeliveryRecord{}, DeliveryObservation{}, ErrUnavailable
	}
	payload, err := json.Marshal(p)
	if err != nil {
		return DeliveryRecord{}, DeliveryObservation{}, fmt.Errorf("%w: payload encoding: %v", ErrInvalidRequest, err)
	}
	record, terminal, err := s.createRecord(ctx, tenant, DeliveryRecord{
		TenantID: tenant, ID: stableID(tenant, approvalActor, string(kind), key), Kind: kind,
		SourceRef: sourceRef, SourceRevision: sourceRevision, ReceiverRef: bindingRef,
		Status: StatusDraft, Revision: 1, PayloadDigest: p.PayloadDigest, Payload: payload,
		IdempotencyKey: key, ApprovalActor: approvalActor, ApprovalRevision: approvalRevision, ApprovalAt: approvalAt,
	})
	if err != nil {
		return DeliveryRecord{}, DeliveryObservation{}, err
	}
	if terminal {
		obs := DeliveryObservation{Status: record.Status, ExternalRef: record.ReceiptRef, Reason: record.Reason}
		if record.Status == StatusRejected {
			return record, obs, ErrDeliveryRejected
		}
		if record.Status == StatusMalformed {
			return record, obs, ErrDeliveryMalformed
		}
		return record, obs, nil
	}
	dispatcher, err := s.Bindings.Resolve(ctx, tenant, kind, bindingRef)
	if err != nil {
		return record, DeliveryObservation{}, err
	}
	obs, err := dispatcher.Dispatch(ctx, tenant, bindingRef, p, key)
	if err != nil {
		return record, DeliveryObservation{}, err
	}
	obs = observationFor(kind, obs)
	record, err = s.Store.RecordReceipt(ctx, tenant, Receipt{TenantID: tenant, DestinationID: record.ID, Status: StatusSent, ReceiverRef: bindingRef, At: s.now()})
	if err != nil {
		return record, obs, err
	}
	record, err = s.Store.RecordReceipt(ctx, tenant, Receipt{TenantID: tenant, DestinationID: record.ID, Status: obs.Status, ReceiverRef: bindingRef, ReceiptRef: obs.ExternalRef, Reason: obs.Reason, RawDigest: obs.RawDigest, At: s.now()})
	if err != nil {
		return record, obs, err
	}
	if obs.Status == StatusRejected {
		return record, obs, ErrDeliveryRejected
	}
	if obs.Status == StatusMalformed {
		return record, obs, ErrDeliveryMalformed
	}
	if obs.Status != StatusAccepted {
		return record, obs, ErrDeliveryMalformed
	}
	return record, obs, nil
}

// InvoiceContractor builds a draft strictly from the engagement's SOW/PO,
// rate card, currency and tax treatment, then submits it to the tenant's AP
// or contractor-platform binding. It never exposes a payroll destination.
func (s Service) InvoiceContractor(ctx context.Context, p *trust.Principal, req ContractorInvoiceRequest) (ContractorInvoiceResult, error) {
	workerRef := req.Build.Engagement.WorkerRef
	tenant, err := s.ready(ctx, p, workerRef, CapContractorApprove)
	if err != nil {
		return ContractorInvoiceResult{}, err
	}
	if err := validateCommon(req.SourceRef, req.BindingRef, req.ApprovalRevision, req.IdempotencyKey, req.SourceRevision); err != nil {
		return ContractorInvoiceResult{}, err
	}
	if err := s.Auth.Authorize(ctx, p, tenant, workerRef, CapContractorDispatch); err != nil {
		return ContractorInvoiceResult{}, err
	}
	if req.Build.ID == "" {
		req.Build.ID = stableID(tenant, p.Subject(), "contractor.invoice", req.IdempotencyKey)
	}
	draft, err := contractortime.BuildInvoice(req.Build)
	if err != nil {
		return ContractorInvoiceResult{}, err
	}
	record, obs, err := s.deliver(ctx, tenant, DeliveryPayload{Kind: KindContractorInvoice, Invoice: &draft, PayloadDigest: draft.Digest}, KindContractorInvoice, req.SourceRef, req.SourceRevision, req.BindingRef, req.IdempotencyKey, p.Subject(), req.ApprovalRevision, s.now())
	return ContractorInvoiceResult{Invoice: draft, Record: record, Observation: obs}, err
}

// ApproveAndDeliverAgency records host-manager approval and sends approved
// agency time to the tenant-owned agency/VMS binding. The AWR qualifying-week
// result is passed through to the connector; this service does not invent a
// parity rate.
func (s Service) ApproveAndDeliverAgency(ctx context.Context, p *trust.Principal, req AgencyDeliveryRequest) (AgencyDeliveryResult, error) {
	workerRef := req.Build.Assignment.WorkerRef
	tenant, err := s.ready(ctx, p, workerRef, CapAgencyApprove)
	if err != nil {
		return AgencyDeliveryResult{}, err
	}
	if err := validateCommon(req.SourceRef, req.BindingRef, req.ApprovalRevision, req.IdempotencyKey, req.SourceRevision); err != nil {
		return AgencyDeliveryResult{}, err
	}
	if err := s.Auth.Authorize(ctx, p, tenant, workerRef, CapAgencyDispatch); err != nil {
		return AgencyDeliveryResult{}, err
	}
	parity, err := agencytime.EvaluateParity(req.QualifyingWeeks)
	if err != nil {
		return AgencyDeliveryResult{}, err
	}
	export, err := agencytime.BuildExport(req.Build)
	if err != nil {
		return AgencyDeliveryResult{}, err
	}
	record, obs, err := s.deliver(ctx, tenant, DeliveryPayload{Kind: KindAgencyExport, Agency: &export, Parity: parity, PayloadDigest: export.Digest}, KindAgencyExport, req.SourceRef, req.SourceRevision, req.BindingRef, req.IdempotencyKey, p.Subject(), req.ApprovalRevision, s.now())
	return AgencyDeliveryResult{Export: export, Parity: parity, Record: record, Observation: obs}, err
}
