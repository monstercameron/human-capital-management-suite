package extract

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

// ContractPath is the repository-relative path of the rule-pack contract the
// matrix is parsed out of.
const ContractPath = "planning/specs/legal-rule-packs-and-state-configuration.md"

// Cell is one matrix cell's legend value, from the contract's section 5.
type Cell string

// Legend values.
const (
	// CellStateRule: the research asserts a state-level rule of this kind.
	CellStateRule Cell = "Y"
	// CellLocalOnly: the research asserts only a local (sub-state) rule.
	CellLocalOnly Cell = "L"
	// CellPreempted: the state preempts local rules of this kind.
	CellPreempted Cell = "P"
	// CellFederalBaseline: no state rule; the federal baseline applies.
	CellFederalBaseline Cell = "F"
	// CellUncertain: the research is uncertain, self-contradictory, or marked
	// "verify". A pack may not be released for a state while any kind the
	// flow consumes sits here.
	CellUncertain Cell = "?"
)

// MatrixCell is one cell: its legend value plus whatever annotation the
// contract wrote beside it ("45d", "25+", "pub", "90d"). The annotation is
// evidence, not decoration: it is where a breach-notification day count or an
// E-Verify employee threshold comes from.
type MatrixCell struct {
	Value      Cell
	Annotation string
}

// Matrix is the contract's section 5, parsed. Rows are keyed by two-letter
// postal code, columns by obligation kind.
type Matrix struct {
	// Cells[state][kind] is the parsed cell. A kind absent from both tables
	// (MONITORING_CONSENT) is absent from the map.
	Cells map[string]map[legal.ObligationType]MatrixCell
	// Local[state] is the LOCAL column of table B, which is a jurisdiction
	// fact rather than an obligation kind.
	Local map[string]MatrixCell
	// Preemptions[state] lists the kinds the contract's section 6.4 records
	// that state as preempting at locality level, with its citation.
	Preemptions map[string][]PreemptionRow
	// States is every row key, sorted, so callers iterate deterministically.
	States []string
}

// PreemptionRow is one row of the contract's section 6.4 preemption table.
type PreemptionRow struct {
	Kind     legal.ObligationType
	Citation string
}

// tableAColumns maps the contract's abbreviated column headers to kinds.
var tableAColumns = map[string]legal.ObligationType{
	"NOTICE":         legal.ObligationTypeNotice,
	"PAY_TRANSP":     legal.ObligationTypePayTransparency,
	"FIELD_RESTR":    legal.ObligationTypeFieldRestriction,
	"WAGE_FLOOR":     legal.ObligationTypeWageFloor,
	"PAY_FREQ":       legal.ObligationTypePayFrequency,
	"PAY_STMT":       legal.ObligationTypePayStatement,
	"LEAVE":          legal.ObligationTypeLeaveInteraction,
	"NON_COMPETE":    legal.ObligationTypeNonCompete,
	"CLASSIFN":       legal.ObligationTypeClassification,
	"PAY_EQUITY":     legal.ObligationTypePayEquityReview,
	"RETENTION":      legal.ObligationTypeRetention,
	"PERSONNEL_FILE": legal.ObligationTypePersonnelFile,
}

var tableBColumns = map[string]legal.ObligationType{
	"FINAL_PAY":  legal.ObligationTypeFinalPayDeadline,
	"MINI_WARN":  legal.ObligationTypeMiniWARN,
	"SEP_FILING": legal.ObligationTypeSeparationFiling,
	"E_VERIFY":   legal.ObligationTypeEVerify,
	"DRUG_TEST":  legal.ObligationTypeDrugTesting,
	"ANTI_RETAL": legal.ObligationTypeAntiRetaliation,
	"JOB_SEC":    legal.ObligationTypeJobSecurity,
	"BREACH":     legal.ObligationTypeBreachNotification,
	"AUTO_DEC":   legal.ObligationTypeAutomatedDecision,
	"LOCAL":      legal.ObligationTypeUnspecified,
}

// MonitoringConsentStates are the five states the contract's section 5.2
// names in prose rather than as a matrix column: Illinois (BIPA), Colorado
// (HB 24-1130), New York (Civil Rights Law § 201-i), California (CCPA/CPRA
// applied to employee data) and Texas. None of the five binds a promotion;
// the kind is declared so a later monitoring capability does not invent it.
//
// This list is transcribed rather than parsed because it is a sentence, not a
// table. TestTodo_LEGAL_011_Conformance re-reads that sentence and fails if it
// stops naming exactly these five.
var MonitoringConsentStates = []string{"CA", "CO", "IL", "NY", "TX"}

// LoadMatrix parses the contract's section 5 tables and section 6.4
// preemption table from the contract file under root.
func LoadMatrix(root string) (*Matrix, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ContractPath)))
	if err != nil {
		return nil, fmt.Errorf("extract: reading the rule-pack contract: %w", err)
	}
	return ParseMatrix(string(data))
}

// ParseMatrix parses the contract's markdown.
func ParseMatrix(contract string) (*Matrix, error) {
	m := &Matrix{
		Cells:       map[string]map[legal.ObligationType]MatrixCell{},
		Local:       map[string]MatrixCell{},
		Preemptions: map[string][]PreemptionRow{},
	}
	lines := strings.Split(contract, "\n")
	if err := m.parseKindTable(lines, tableAColumns); err != nil {
		return nil, err
	}
	if err := m.parseKindTable(lines, tableBColumns); err != nil {
		return nil, err
	}
	m.parsePreemptionTable(lines)

	for state := range m.Cells {
		m.States = append(m.States, state)
	}
	sort.Strings(m.States)
	if len(m.States) != 51 {
		return nil, fmt.Errorf("extract: contract section 5 parsed %d state rows, want 51", len(m.States))
	}
	return m, nil
}

// parseKindTable finds the markdown table whose header is exactly the given
// column set and reads its rows. Locating the table by its header rather than
// by a line number means reflowing the contract cannot silently shift the
// parse onto a different table.
func (m *Matrix) parseKindTable(lines []string, columns map[string]legal.ObligationType) error {
	headerIndex := -1
	var order []string
	for i, line := range lines {
		cells := splitTableRow(line)
		if len(cells) != len(columns)+1 || cells[0] != "State" {
			continue
		}
		match := true
		order = order[:0]
		for _, c := range cells[1:] {
			if _, ok := columns[c]; !ok {
				match = false
				break
			}
			order = append(order, c)
		}
		if match {
			headerIndex = i
			order = append([]string(nil), order...)
			break
		}
	}
	if headerIndex < 0 {
		return fmt.Errorf("extract: no matrix table in the contract with columns %v", sortedKeys(columns))
	}

	for i := headerIndex + 2; i < len(lines); i++ {
		cells := splitTableRow(lines[i])
		if len(cells) != len(order)+1 {
			break
		}
		state := cells[0]
		if len(state) != 2 {
			break
		}
		for j, header := range order {
			cell := parseCell(cells[j+1])
			kind := columns[header]
			if kind == legal.ObligationTypeUnspecified {
				m.Local[state] = cell
				continue
			}
			if m.Cells[state] == nil {
				m.Cells[state] = map[legal.ObligationType]MatrixCell{}
			}
			m.Cells[state][kind] = cell
		}
	}
	return nil
}

// parsePreemptionTable reads the contract's section 6.4 table of states that
// preempt locality rules of a named kind.
func (m *Matrix) parsePreemptionTable(lines []string) {
	headerIndex := -1
	for i, line := range lines {
		cells := splitTableRow(line)
		if len(cells) == 3 && cells[0] == "State" && cells[1] == "Kinds preempted" && cells[2] == "Citation" {
			headerIndex = i
			break
		}
	}
	if headerIndex < 0 {
		return
	}
	for i := headerIndex + 2; i < len(lines); i++ {
		cells := splitTableRow(lines[i])
		if len(cells) != 3 || len(cells[0]) != 2 {
			break
		}
		state := cells[0]
		for _, token := range strings.Split(cells[1], ",") {
			token = strings.Trim(strings.TrimSpace(token), "`")
			if token == "" {
				continue
			}
			kind, err := legal.ParseObligationType(token)
			if err != nil {
				continue
			}
			m.Preemptions[state] = append(m.Preemptions[state], PreemptionRow{Kind: kind, Citation: cells[2]})
		}
	}
}

// splitTableRow splits a markdown table row into trimmed cells, or returns
// nil when the line is not a table row.
func splitTableRow(line string) []string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "|") || !strings.HasSuffix(trimmed, "|") {
		return nil
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(trimmed, "|"), "|")
	parts := strings.Split(inner, "|")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

// parseCell splits "Y 45d" into the legend value and its annotation.
func parseCell(text string) MatrixCell {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return MatrixCell{}
	}
	return MatrixCell{
		Value:      Cell(fields[0]),
		Annotation: strings.Join(fields[1:], " "),
	}
}

// Cell returns the cell for a state and kind. MONITORING_CONSENT has no
// column, so it is synthesised from [MonitoringConsentStates].
func (m *Matrix) Cell(state string, kind legal.ObligationType) MatrixCell {
	if kind == legal.ObligationTypeMonitoringConsent {
		for _, s := range MonitoringConsentStates {
			if s == state {
				return MatrixCell{Value: CellStateRule}
			}
		}
		return MatrixCell{Value: CellFederalBaseline}
	}
	return m.Cells[state][kind]
}

// KindTotals recounts each kind's column total the way the contract does:
// Y plus L, excluding P, F and ?.
func (m *Matrix) KindTotals() map[legal.ObligationType]int {
	totals := map[legal.ObligationType]int{}
	for _, state := range m.States {
		for _, kind := range legal.AllObligationTypes() {
			switch m.Cell(state, kind).Value {
			case CellStateRule, CellLocalOnly:
				totals[kind]++
			}
		}
	}
	return totals
}

func sortedKeys(m map[string]legal.ObligationType) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
