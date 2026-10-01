// Package timecardexport is the application boundary for approved timecard
// exchange. It reads a tenant-scoped approved-timecard projection, refuses
// anything whose approval revision is not current, and delegates the pure
// HR Open TimeCard wire mapping to internal/domains/timeexport.
package timecardexport

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/timecardservice"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timecard"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeexport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var (
	ErrInvalidRequest   = errors.New("timecardexport: invalid request")
	ErrUnavailable      = errors.New("timecardexport: required port unavailable")
	ErrInvalidPrincipal = errors.New("timecardexport: trusted principal required")
	ErrNotFound         = errors.New("timecardexport: approved timecard not found")
	ErrNotApproved      = errors.New("timecardexport: timecard is not approved")
)

// JobAllocation is the complete cost attribution carried by one interval.
// Keeping all three fields in the read model prevents a provider mapping from
// accidentally dropping a job or rate dimension.
type JobAllocation struct {
	Project  string
	CostCode string
	RateCode string
}

// ApprovedInterval is one approved worked interval. SourceRevision is the
// immutable approval evidence for that interval; it is required even when
// the timecard revision is present.
type ApprovedInterval struct {
	Date           string
	Minutes        int
	PayCode        string
	JobAllocation  JobAllocation
	SourceRevision string
}

// Allowance is a non-hour amount approved with the timecard.
type Allowance struct {
	Code     string
	Amount   string
	Currency string
}

// ApprovedTimecard is the read model consumed by this package. The reader is
// responsible for constructing it from the canonical timecard projection;
// this package does not infer approval from a caller-supplied worker or
// period. Revision is the aggregate's current revision and must equal
// ApprovedRevision for an exportable APPROVED or LOCKED record.
type ApprovedTimecard struct {
	TenantID                 string
	TimecardID               string
	WorkerRef                string
	PeriodStart              time.Time
	PeriodEnd                time.Time
	State                    timecard.State
	Revision                 uint64
	ApprovedRevision         uint64
	PreviousApprovedRevision uint64
	Intervals                []ApprovedInterval
	Allowances               []Allowance
}

// ReadModelReader returns one tenant-scoped approved-timecard projection.
// Implementations must apply the tenant predicate in their durable query.
type ReadModelReader interface {
	ReadApprovedTimecard(ctx context.Context, tenant, timecardID string) (ApprovedTimecard, error)
}

// ExportRequest identifies a timecard. Tenant and worker are deliberately
// absent: both come from the authenticated principal and read model.
type ExportRequest struct {
	TimecardID string
}

// Service authorizes and exports approved timecards. Authorizer is the
// existing timecard application port, so export uses the same least-privilege
// routing capability as other payroll-bound timecard effects.
type Service struct {
	Reader     ReadModelReader
	Authorizer timecardservice.Authorizer
}

// New constructs an application export service.
func New(reader ReadModelReader, authorizer timecardservice.Authorizer) Service {
	return Service{Reader: reader, Authorizer: authorizer}
}

func validPrincipal(p *trust.Principal) error {
	if p == nil || strings.TrimSpace(string(p.Tenant())) == "" || strings.TrimSpace(p.Subject()) == "" {
		return ErrInvalidPrincipal
	}
	return nil
}

func validateApprovedModel(m ApprovedTimecard) error {
	if strings.TrimSpace(m.TenantID) == "" || strings.TrimSpace(m.TimecardID) == "" || strings.TrimSpace(m.WorkerRef) == "" {
		return fmt.Errorf("%w: tenant, timecard and worker are required", ErrInvalidRequest)
	}
	if m.PeriodStart.IsZero() || m.PeriodEnd.IsZero() || !m.PeriodEnd.After(m.PeriodStart) {
		return fmt.Errorf("%w: period must be a non-empty interval", ErrInvalidRequest)
	}
	if m.State != timecard.Approved && m.State != timecard.Locked {
		return ErrNotApproved
	}
	if m.Revision == 0 || m.ApprovedRevision == 0 || m.Revision != m.ApprovedRevision {
		return fmt.Errorf("%w: current revision is not the approved revision", ErrNotApproved)
	}
	if m.PreviousApprovedRevision >= m.ApprovedRevision && m.PreviousApprovedRevision != 0 {
		return fmt.Errorf("%w: previous revision must precede approved revision", ErrInvalidRequest)
	}
	if len(m.Intervals) == 0 {
		return fmt.Errorf("%w: at least one approved interval is required", ErrInvalidRequest)
	}
	for i, in := range m.Intervals {
		if _, err := time.Parse("2006-01-02", in.Date); err != nil {
			return fmt.Errorf("%w: interval %d has invalid date", ErrInvalidRequest, i)
		}
		if in.Minutes <= 0 || strings.TrimSpace(in.PayCode) == "" || strings.TrimSpace(in.SourceRevision) == "" {
			return fmt.Errorf("%w: interval %d is missing approved minutes, pay code or source revision", ErrInvalidRequest, i)
		}
		if strings.TrimSpace(in.JobAllocation.Project) == "" || strings.TrimSpace(in.JobAllocation.CostCode) == "" || strings.TrimSpace(in.JobAllocation.RateCode) == "" {
			return fmt.Errorf("%w: interval %d has incomplete job allocation", ErrInvalidRequest, i)
		}
	}
	for i, a := range m.Allowances {
		if strings.TrimSpace(a.Code) == "" || strings.TrimSpace(a.Amount) == "" || strings.TrimSpace(a.Currency) == "" {
			return fmt.Errorf("%w: allowance %d is incomplete", ErrInvalidRequest, i)
		}
	}
	return nil
}

// BuildTimeCard maps the approved read model to the pure domain export
// document. It is intentionally separate from the service so the same
// mapping can be reused by payroll-provider adapters without rereading state.
func BuildTimeCard(m ApprovedTimecard) (timeexport.TimeCard, error) {
	if err := validateApprovedModel(m); err != nil {
		return timeexport.TimeCard{}, err
	}
	intervals := make([]timeexport.Interval, 0, len(m.Intervals))
	for _, in := range m.Intervals {
		intervals = append(intervals, timeexport.Interval{
			Date: in.Date, Minutes: in.Minutes, PayCode: in.PayCode,
			Project: in.JobAllocation.Project, CostCode: in.JobAllocation.CostCode,
			RateCode: in.JobAllocation.RateCode, SourceRevisionDigest: in.SourceRevision,
		})
	}
	allowances := make([]timeexport.Allowance, 0, len(m.Allowances))
	for _, a := range m.Allowances {
		allowances = append(allowances, timeexport.Allowance{Code: a.Code, Amount: a.Amount, Currency: a.Currency})
	}
	previous := ""
	if m.PreviousApprovedRevision != 0 {
		previous = strconv.FormatUint(m.PreviousApprovedRevision, 10)
	}
	return timeexport.TimeCard{
		WorkerRef:        m.WorkerRef,
		PeriodStart:      m.PeriodStart.UTC().Format("2006-01-02"),
		PeriodEnd:        m.PeriodEnd.UTC().Format("2006-01-02"),
		Revision:         strconv.FormatUint(m.ApprovedRevision, 10),
		PreviousRevision: previous,
		Intervals:        intervals,
		Allowances:       allowances,
	}, nil
}

// Export renders an approved read model as HR Open TimeCard JSON.
func Export(m ApprovedTimecard) ([]byte, error) {
	c, err := BuildTimeCard(m)
	if err != nil {
		return nil, err
	}
	return timeexport.Export(c)
}

// Export reads, authorizes, and renders one approved timecard. The tenant is
// always taken from the trusted principal and passed into the reader.
func (s Service) Export(ctx context.Context, p *trust.Principal, req ExportRequest) ([]byte, error) {
	if err := validPrincipal(p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.TimecardID) == "" {
		return nil, ErrInvalidRequest
	}
	if s.Reader == nil || s.Authorizer == nil {
		return nil, ErrUnavailable
	}
	tenant := string(p.Tenant())
	model, err := s.Reader.ReadApprovedTimecard(ctx, tenant, req.TimecardID)
	if err != nil {
		return nil, err
	}
	if model.TenantID != tenant || model.TimecardID != req.TimecardID {
		return nil, ErrNotFound
	}
	if err := validateApprovedModel(model); err != nil {
		return nil, err
	}
	if err := s.Authorizer.Authorize(ctx, p, tenant, model.WorkerRef, timecardservice.CapRouteTime); err != nil {
		return nil, err
	}
	return Export(model)
}

// UnmappedReport is the domain import report, retained as an alias so
// callers can inspect every element that was not mapped.
type UnmappedReport = timeexport.UnmappedReport

// ImportedTimeCard is the validated domain representation of an HR Open
// TimeCard document.
type ImportedTimeCard = timeexport.TimeCard

// Import parses and validates the supported HR Open TimeCard subset. Unknown
// top-level, interval, and allowance elements are returned in the report;
// they are never silently discarded.
func Import(raw []byte) (ImportedTimeCard, UnmappedReport, error) {
	return timeexport.Import(raw)
}

const publishedSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "required": ["workerId", "period", "revision", "intervals"],
  "additionalProperties": false,
  "properties": {
    "workerId": {"type": "string"},
    "period": {"type": "object", "required": ["start", "end"], "additionalProperties": false, "properties": {"start": {"type": "string"}, "end": {"type": "string"}}},
    "revision": {"type": "string"},
    "previousRevision": {"type": "string"},
    "intervals": {"type": "array", "items": {"type": "object", "required": ["date", "minutes", "payCode", "jobAllocation", "sourceRevision"], "additionalProperties": false, "properties": {"date": {"type": "string"}, "minutes": {"type": "integer"}, "payCode": {"type": "string"}, "jobAllocation": {"type": "object", "required": ["project", "costCode", "rateCode"], "additionalProperties": false, "properties": {"project": {"type": "string"}, "costCode": {"type": "string"}, "rateCode": {"type": "string"}}}, "sourceRevision": {"type": "string"}}}},
    "allowances": {"type": "array", "items": {"type": "object", "required": ["code", "amount", "currency"], "additionalProperties": false, "properties": {"code": {"type": "string"}, "amount": {"type": "string"}, "currency": {"type": "string"}}}}
  }
}`

// PublishedSchema returns a copy of the schema used by the conformance
// boundary. A fresh byte slice prevents callers from mutating package state.
func PublishedSchema() []byte { return []byte(publishedSchema) }

// ValidateConformance validates bytes against the published HR Open subset.
func ValidateConformance(raw []byte) error {
	return timeexport.ValidateAgainstSchema(PublishedSchema(), raw)
}
