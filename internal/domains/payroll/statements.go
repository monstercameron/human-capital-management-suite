package payroll

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal/payrules"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// StatementField is one rendered pay statement value. Names use the keys in
// the legal pay-statement-fields registry.
type StatementField struct {
	Name  string
	Value string
}

// PayStatement is the content and delivery evidence for one worker's
// statement. Jurisdiction is the state whose registry row governs it.
type PayStatement struct {
	WorkerID     string
	Jurisdiction string
	Fields       []StatementField
	Medium       payrules.DeliveryMedium
	Consent      bool
}

// StatementManifest is the exact statement payload compiled by StatementsDigest.
type StatementManifest struct {
	PayDate    values.LocalDate
	Statements []PayStatement
}

func (s PayStatement) canonical() *canonicalbytes.Writer {
	w := canonicalbytes.New("hcmnext.domains.payroll.PayStatement", 1).
		String("worker_id", s.WorkerID).
		String("jurisdiction", s.Jurisdiction).
		String("medium", string(s.Medium)).
		Bool("consent", s.Consent).
		Count("fields", len(s.Fields))
	fields := append([]StatementField(nil), s.Fields...)
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	for _, field := range fields {
		w.String("field.name", field.Name).String("field.value", field.Value)
	}
	return w
}

func (m StatementManifest) canonical() *canonicalbytes.Writer {
	w := canonicalbytes.New("hcmnext.domains.payroll.StatementManifest", 1).
		String("pay_date", m.PayDate.String()).
		Count("statements", len(m.Statements))
	rows := append([]PayStatement(nil), m.Statements...)
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].WorkerID != rows[j].WorkerID {
			return rows[i].WorkerID < rows[j].WorkerID
		}
		return rows[i].Jurisdiction < rows[j].Jurisdiction
	})
	for _, row := range rows {
		w.Nested("statement", row.canonical())
	}
	return w
}

// Digest returns the digest that must be carried in ReleaseEffects.
func (m StatementManifest) Digest() (string, error) { return m.canonical().Digest() }

// Validate checks each statement against the supplied, resolved state registry.
// Refusals name the jurisdiction and, for content gaps, the missing field.
func (m StatementManifest) Validate(registry payrules.PayStatementRegistry) error {
	if err := registry.Validate(); err != nil {
		return releaseRefusal("statements.registry", "pay-statement-fields registry is invalid", err)
	}
	if err := m.PayDate.Validate(); err != nil {
		return releaseRefusal("statements.pay_date", "pay date must be a valid local date", ErrInvalidPayrollRelease)
	}
	if !registry.AppliesOn(m.PayDate) {
		return releaseRefusal("statements.registry", "pay-statement-fields registry is not effective on "+m.PayDate.String(), ErrInvalidPayrollRelease)
	}
	if len(m.Statements) == 0 {
		return releaseRefusal("statements", "at least one statement is required", ErrInvalidPayrollRelease)
	}
	seen := map[string]bool{}
	for i, statement := range m.Statements {
		prefix := fmt.Sprintf("statements[%d]", i)
		state := strings.ToUpper(strings.TrimSpace(statement.Jurisdiction))
		row, ok := registry.ForState(state)
		if !ok {
			return releaseRefusal(prefix+".jurisdiction", "unknown jurisdiction "+state, ErrInvalidPayrollRelease)
		}
		if strings.TrimSpace(statement.WorkerID) == "" {
			return releaseRefusal(prefix+".worker_id", "worker id is required", ErrInvalidPayrollRelease)
		}
		if seen[statement.WorkerID] {
			return releaseRefusal(prefix+".worker_id", "duplicate statement for "+statement.WorkerID, ErrInvalidPayrollRelease)
		}
		seen[statement.WorkerID] = true
		present := map[string]bool{}
		for _, field := range statement.Fields {
			if strings.TrimSpace(field.Name) == "" || strings.TrimSpace(field.Value) == "" {
				return releaseRefusal(prefix+".field", state+" statement contains an unnamed or empty field", ErrInvalidPayrollRelease)
			}
			if present[field.Name] {
				return releaseRefusal(prefix+"."+field.Name, state+" statement contains a duplicate field", ErrInvalidPayrollRelease)
			}
			present[field.Name] = true
		}
		if row.Mandate == payrules.Mandatory {
			for _, required := range row.RequiredFields {
				if !present[required] {
					return releaseRefusal(prefix+"."+required, state+" statement is missing mandatory field "+required, ErrInvalidPayrollRelease)
				}
			}
		}
		switch row.Delivery {
		case payrules.Paper:
			if statement.Medium != payrules.Paper {
				return releaseRefusal(prefix+".medium", state+" requires PAPER delivery", ErrInvalidPayrollRelease)
			}
		case payrules.Electronic:
			if statement.Medium != payrules.Electronic {
				return releaseRefusal(prefix+".medium", state+" requires ELECTRONIC delivery", ErrInvalidPayrollRelease)
			}
		case payrules.Either:
			if statement.Medium != payrules.Paper && statement.Medium != payrules.Electronic {
				return releaseRefusal(prefix+".medium", state+" requires PAPER or ELECTRONIC delivery", ErrInvalidPayrollRelease)
			}
		}
		if statement.Medium == payrules.Electronic && row.ConsentRequired && !statement.Consent {
			return releaseRefusal(prefix+".consent", state+" requires consent for ELECTRONIC delivery", ErrInvalidPayrollRelease)
		}
	}
	return nil
}
