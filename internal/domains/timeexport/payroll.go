package timeexport

import (
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
)

// ColumnMapping names which TimeCard field a flat-file column carries and
// under what header text. Column order and headers are data, so a tenant's
// own ADP or Paychex company code layout is a configuration, not a code
// change.
type ColumnMapping struct {
	Header string
	Field  ColumnField
}

// ColumnField enumerates the TimeCard interval fields a flat export column
// can carry.
type ColumnField string

const (
	ColumnWorkerRef ColumnField = "WORKER_REF"
	ColumnDate      ColumnField = "DATE"
	ColumnMinutes   ColumnField = "MINUTES"
	ColumnHours     ColumnField = "HOURS" // minutes rendered as decimal hours
	ColumnPayCode   ColumnField = "PAY_CODE"
	ColumnProject   ColumnField = "PROJECT"
	ColumnCostCode  ColumnField = "COST_CODE"
	ColumnRateCode  ColumnField = "RATE_CODE"
	ColumnRevision  ColumnField = "REVISION"
)

func (f ColumnField) valid() bool {
	switch f {
	case ColumnWorkerRef, ColumnDate, ColumnMinutes, ColumnHours, ColumnPayCode, ColumnProject, ColumnCostCode, ColumnRateCode, ColumnRevision:
		return true
	}
	return false
}

func cellFor(field ColumnField, c TimeCard, iv Interval) (string, error) {
	switch field {
	case ColumnWorkerRef:
		return c.WorkerRef, nil
	case ColumnDate:
		return iv.Date, nil
	case ColumnMinutes:
		return strconv.Itoa(iv.Minutes), nil
	case ColumnHours:
		return minutesToHoursText(iv.Minutes), nil
	case ColumnPayCode:
		return iv.PayCode, nil
	case ColumnProject:
		return iv.Project, nil
	case ColumnCostCode:
		return iv.CostCode, nil
	case ColumnRateCode:
		return iv.RateCode, nil
	case ColumnRevision:
		return iv.SourceRevisionDigest, nil
	default:
		return "", fmt.Errorf("timeexport: unknown column field %q", field)
	}
}

func minutesToHoursText(minutes int) string {
	whole := minutes / 60
	remainder := minutes % 60
	frac := remainder * 10000 / 60
	return fmt.Sprintf("%d.%04d", whole, frac)
}

// ExportFlatCSV renders a timecard's intervals as a generic column-mapped
// CSV. ADPColumns and PaychexColumns below are the two named layouts the
// GREEN criterion asks for; a tenant may also supply its own mapping.
func ExportFlatCSV(c TimeCard, mapping []ColumnMapping) (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	if len(mapping) == 0 {
		return "", reject("ExportFlatCSV.Mapping", "", "column mapping is required")
	}
	for _, m := range mapping {
		if !m.Field.valid() {
			return "", reject("ExportFlatCSV.Mapping", string(m.Field), "unknown column field")
		}
	}
	var b strings.Builder
	w := csv.NewWriter(&b)
	header := make([]string, len(mapping))
	for i, m := range mapping {
		header[i] = m.Header
	}
	if err := w.Write(header); err != nil {
		return "", fmt.Errorf("timeexport: write header: %w", err)
	}
	for _, iv := range c.Intervals {
		row := make([]string, len(mapping))
		for i, m := range mapping {
			cell, err := cellFor(m.Field, c, iv)
			if err != nil {
				return "", err
			}
			row[i] = cell
		}
		if err := w.Write(row); err != nil {
			return "", fmt.Errorf("timeexport: write row: %w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return "", fmt.Errorf("timeexport: flush csv: %w", err)
	}
	return b.String(), nil
}

// ADPColumns is a generic ADP-style column layout: worker, date, hours, pay
// code and cost center. The exact company-specific ADP layout is a per-tenant
// configuration; this is the documented default.
var ADPColumns = []ColumnMapping{
	{Header: "EmployeeID", Field: ColumnWorkerRef},
	{Header: "WorkDate", Field: ColumnDate},
	{Header: "Hours", Field: ColumnHours},
	{Header: "EarningsCode", Field: ColumnPayCode},
	{Header: "CostCenter", Field: ColumnCostCode},
}

// PaychexColumns is a generic Paychex-style column layout.
var PaychexColumns = []ColumnMapping{
	{Header: "EmployeeNumber", Field: ColumnWorkerRef},
	{Header: "Date", Field: ColumnDate},
	{Header: "HoursWorked", Field: ColumnHours},
	{Header: "PayComponent", Field: ColumnPayCode},
	{Header: "Department", Field: ColumnCostCode},
}

// ExportQuickBooksIIF renders a timecard's intervals as QuickBooks Desktop
// IIF timer activity rows (the "TIMERHDR"/"TIMERENTRY" record pair IIF
// documents for imported timer data). Amounts are not computed here; IIF
// timer activities carry duration, not pay.
func ExportQuickBooksIIF(c TimeCard, customer string) (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	if strings.TrimSpace(customer) == "" {
		return "", reject("ExportQuickBooksIIF.Customer", "", "IIF timer export needs a customer/job name")
	}
	var b strings.Builder
	b.WriteString("!TIMERHDR\tVERSION\tVENDOR\n")
	b.WriteString("TIMERHDR\t7\thcm-next\n")
	b.WriteString("!TIMERENTRY\tDATE\tJOB\tEMP\tITEM\tPITEM\tDURATION\tNOTE\tBILLABLESTATUS\n")
	for _, iv := range c.Intervals {
		duration := iifDuration(iv.Minutes)
		fmt.Fprintf(&b, "TIMERENTRY\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t1\n",
			iifDate(iv.Date), customer, c.WorkerRef, iv.PayCode, iv.RateCode, duration, iv.CostCode)
	}
	return b.String(), nil
}

// iifDate renders an ISO date (2006-01-02) as IIF's MM/DD/YYYY.
func iifDate(iso string) string {
	parts := strings.Split(iso, "-")
	if len(parts) != 3 {
		return iso
	}
	return parts[1] + "/" + parts[2] + "/" + parts[0]
}

// iifDuration renders minutes as IIF's HH:MM duration.
func iifDuration(minutes int) string {
	h := minutes / 60
	m := minutes % 60
	return fmt.Sprintf("%d:%02d", h, m)
}
