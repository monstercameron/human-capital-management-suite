// GARN-007: reconcile withheld and remitted garnishments.
//
// ReconcileWithholding compares the order balance, payroll deductions,
// the remittance batch and the recipient observation per worker, order
// and period. Any discrepancy creates a protected repair case scoped to
// the affected cells. The function is kernel-pure.
package garnishment

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrGarnReconRejected is the GARN-007 sentinel for malformed input.
	ErrGarnReconRejected = errors.New("GARN_007_REJECTED")
)

// GarnReconRejection is the stable GARN-007 failure shape.
type GarnReconRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *GarnReconRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrGarnReconRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the GARN_007_REJECTED sentinel to errors.Is.
func (r *GarnReconRejection) Unwrap() error { return ErrGarnReconRejected }

func garnReconReject(field, state, reason string) error {
	return &GarnReconRejection{Field: field, State: state, Version: BalanceVersion, Reason: reason}
}

// ReconCell is the comparison vocabulary for one reconciled cell.
type ReconCell string

const (
	CellMatch   ReconCell = "MATCH"
	CellShort   ReconCell = "SHORT"
	CellExcess  ReconCell = "EXCESS"
	CellUnknown ReconCell = "UNKNOWN"
)

// PeriodComparison carries the four observed totals for one
// worker/order/period cell.
type PeriodComparison struct {
	WorkerRef         string
	OrderRef          string
	Period            string
	BalanceWithheld   values.Decimal
	PayrollDeducted   values.Decimal
	Remitted          values.Decimal
	RecipientObserved values.Decimal
	RecipientUnknown  bool
}

// CellResult is the reconciled fate of one period cell.
type CellResult struct {
	WorkerRef string
	OrderRef  string
	Period    string
	Cell      ReconCell
	Delta     values.Decimal
	Detail    string
}

// GarnRepairCase is the protected repair created for discrepancies.
type GarnRepairCase struct {
	CaseID    string
	Scope     string
	Cells     []CellResult
	CreatedAt time.Time
}

// GarnReconResult is the deterministic reconciliation outcome.
type GarnReconResult struct {
	Cells  []CellResult
	Repair *GarnRepairCase
	Digest string
}

func (r GarnReconResult) computedDigest(tenant string) string {
	cells := make([]string, 0, len(r.Cells))
	for _, c := range r.Cells {
		cells = append(cells, strings.Join([]string{c.WorkerRef, c.OrderRef, c.Period, string(c.Cell), c.Delta.String()}, "\x00"))
	}
	sort.Strings(cells)
	w := canonicalbytes.New("hcmnext.domains.garnishment.ReconResult", 1).
		String("tenant", tenant).
		SortedStrings("cells", cells)
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

// ReconcileWithholding compares order balance, payroll deductions,
// remittance and recipient observation per worker/order/period cell.
func ReconcileWithholding(tenant string, cells []PeriodComparison, now time.Time) (GarnReconResult, error) {
	if strings.TrimSpace(tenant) == "" {
		return GarnReconResult{}, garnReconReject("recon.tenant", "MISSING", "tenant is required")
	}
	if len(cells) == 0 {
		return GarnReconResult{}, garnReconReject("recon.cells", "MISSING", "at least one period cell is required")
	}
	if now.IsZero() {
		return GarnReconResult{}, garnReconReject("recon.now", "MISSING", "reconciliation instant is required")
	}
	res := GarnReconResult{}
	seen := make(map[string]struct{}, len(cells))
	for _, c := range cells {
		if strings.TrimSpace(c.WorkerRef) == "" || strings.TrimSpace(c.OrderRef) == "" || strings.TrimSpace(c.Period) == "" {
			return GarnReconResult{}, garnReconReject("recon.cell", "MISSING", "worker, order and period are required")
		}
		key := c.WorkerRef + "\x00" + c.OrderRef + "\x00" + c.Period
		if _, dup := seen[key]; dup {
			return GarnReconResult{}, garnReconReject("recon.cell", "DUPLICATE", fmt.Sprintf("cell %s repeats", key))
		}
		seen[key] = struct{}{}
		for name, d := range map[string]values.Decimal{"balance": c.BalanceWithheld, "payroll": c.PayrollDeducted, "remitted": c.Remitted} {
			if err := d.Validate(); err != nil || d.Sign() < 0 {
				return GarnReconResult{}, garnReconReject("recon."+name, "INVALID", "observed totals must be valid non-negative decimals")
			}
		}
		out := CellResult{WorkerRef: c.WorkerRef, OrderRef: c.OrderRef, Period: c.Period, Delta: zero2()}
		switch {
		case c.RecipientUnknown:
			out.Cell = CellUnknown
			out.Detail = "recipient observation unknown: no claim made"
		case !c.BalanceWithheld.Equal(c.PayrollDeducted):
			out.Cell = CellShort
			delta, err := c.BalanceWithheld.Sub(c.PayrollDeducted)
			if err != nil {
				return GarnReconResult{}, garnReconReject("recon.delta", "INEXACT", "delta is not computable")
			}
			out.Delta = delta
			out.Detail = "payroll deductions diverge from the order balance"
		case !c.PayrollDeducted.Equal(c.Remitted):
			out.Cell = CellShort
			delta, err := c.PayrollDeducted.Sub(c.Remitted)
			if err != nil {
				return GarnReconResult{}, garnReconReject("recon.delta", "INEXACT", "delta is not computable")
			}
			out.Delta = delta
			out.Detail = "remittance trails payroll deductions"
		case !c.Remitted.Equal(c.RecipientObserved):
			delta, err := c.Remitted.Sub(c.RecipientObserved)
			if err != nil {
				return GarnReconResult{}, garnReconReject("recon.delta", "INEXACT", "delta is not computable")
			}
			out.Delta = delta
			if delta.Sign() > 0 {
				out.Cell = CellShort
				out.Detail = "recipient observes less than remitted"
			} else {
				out.Cell = CellExcess
				out.Detail = "recipient observes more than remitted"
			}
		default:
			out.Cell = CellMatch
			out.Detail = "balance, payroll, remittance and recipient agree"
		}
		res.Cells = append(res.Cells, out)
	}
	sort.Slice(res.Cells, func(i, j int) bool {
		if res.Cells[i].WorkerRef != res.Cells[j].WorkerRef {
			return res.Cells[i].WorkerRef < res.Cells[j].WorkerRef
		}
		if res.Cells[i].OrderRef != res.Cells[j].OrderRef {
			return res.Cells[i].OrderRef < res.Cells[j].OrderRef
		}
		return res.Cells[i].Period < res.Cells[j].Period
	})
	var open []CellResult
	for _, c := range res.Cells {
		if c.Cell != CellMatch {
			open = append(open, c)
		}
	}
	if len(open) > 0 {
		res.Repair = &GarnRepairCase{
			CaseID:    "repair:garnishment:" + tenant,
			Scope:     tenant,
			Cells:     open,
			CreatedAt: now.UTC(),
		}
	}
	res.Digest = res.computedDigest(tenant)
	return res, nil
}
