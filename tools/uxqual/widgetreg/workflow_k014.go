package widgetreg

import (
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/documentvalue"
	"github.com/monstercameron/human-capital-management-suite/internal/esign"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
)

// RepeatingRowInput is one submitted or draft group row: field values by
// relative binding plus already-known per-field errors to project.
type RepeatingRowInput struct {
	Values map[string]string
	Errors map[string]string
}

// RepeatingRowView is one rendered group row.
type RepeatingRowView struct {
	Index  int
	Fields []InputRenderResult
}

// renderAttachment projects the quarantine-admission contract: the control
// accepts files, and only the listed extensions survive admission.
func renderAttachment(result InputRenderResult, widget pagedef.WorkflowPageWidget) InputRenderResult {
	for _, ext := range strings.Split(widget.Accept, ",") {
		if ext = strings.ToLower(strings.TrimSpace(ext)); ext != "" {
			result.Accept = append(result.Accept, ext)
		}
	}
	return result
}

// renderSignature projects the ceremony control with its required assurance
// level. An empty assurance fails closed: the contract requires it.
func renderSignature(result InputRenderResult, widget pagedef.WorkflowPageWidget) (InputRenderResult, error) {
	if widget.Assurance == "" {
		return InputRenderResult{}, fmt.Errorf("widgetreg: signature input %q requires an assurance level", widget.Binding)
	}
	result.Assurance = widget.Assurance
	return result, nil
}

// renderComputed recalculates the read-only value from the declared inputs.
// The client value is never read here: what renders is what the server
// recomputed.
func renderComputed(result InputRenderResult, widget pagedef.WorkflowPageWidget, values map[string]string) (InputRenderResult, error) {
	value, err := RecomputeComputed(widget.ComputedOp, widget.ComputedInputs, values)
	if err != nil {
		return InputRenderResult{}, fmt.Errorf("widgetreg: computed input %q: %w", widget.Binding, err)
	}
	result.ReadOnly = true
	result.ComputedValue = value
	return result, nil
}

// renderRepeating projects every row through the registry so row fields keep
// the same accessible associations as top-level widgets. Row field IDs carry
// the row index, so labels never collide across rows.
func (r WorkflowInputRegistry) renderRepeating(result InputRenderResult, req InputRenderRequest) (InputRenderResult, error) {
	widget := req.Widget
	if widget.RowCap < 1 {
		return InputRenderResult{}, fmt.Errorf("widgetreg: repeating group %q requires a row cap", widget.Binding)
	}
	if len(req.RepeatingRows) > widget.RowCap {
		return InputRenderResult{}, fmt.Errorf("widgetreg: repeating group %q holds %d rows above the cap of %d",
			widget.Binding, len(req.RepeatingRows), widget.RowCap)
	}
	result.RowCap = widget.RowCap
	result.CanAdd = len(req.RepeatingRows) < widget.RowCap
	minRows := 0
	if widget.Required {
		minRows = 1
	}
	result.CanRemove = len(req.RepeatingRows) > minRows
	for i, row := range req.RepeatingRows {
		view := RepeatingRowView{Index: i}
		for _, rw := range widget.RowWidgets {
			field := rw
			field.ID = fmt.Sprintf("%s-r%d-%s", widget.ID, i, rw.ID)
			rendered, err := r.Render(InputRenderRequest{
				Widget: field, Locale: req.Locale,
				Error:   row.Errors[rw.Binding],
				Options: req.Options,
			})
			if err != nil {
				return InputRenderResult{}, fmt.Errorf("widgetreg: repeating group %q row %d: %w", widget.Binding, i, err)
			}
			view.Fields = append(view.Fields, rendered)
		}
		result.Rows = append(result.Rows, view)
	}
	return result, nil
}

var canonicalDecimal = regexp.MustCompile(`^-?\d+(\.\d{1,2})?$`)

// RecomputeComputed recalculates a computed widget from the current page
// values. Every declared input must be present (blank is zero for sum and
// skipped for count); anything else fails closed. Sum is exact decimal
// arithmetic: cents in a big integer, never a float.
func RecomputeComputed(op string, inputs []string, values map[string]string) (string, error) {
	switch op {
	case pagedef.ComputedOpSum:
		total := big.NewInt(0)
		for _, in := range inputs {
			raw, ok := values[in]
			if !ok {
				return "", fmt.Errorf("input %q has no value", in)
			}
			cents, err := parseCents(raw)
			if err != nil {
				return "", fmt.Errorf("input %q: %w", in, err)
			}
			total.Add(total, cents)
		}
		return formatCents(total), nil
	case pagedef.ComputedOpCount:
		n := 0
		for _, in := range inputs {
			raw, ok := values[in]
			if !ok {
				return "", fmt.Errorf("input %q has no value", in)
			}
			if strings.TrimSpace(raw) != "" {
				n++
			}
		}
		return strconv.Itoa(n), nil
	default:
		return "", fmt.Errorf("operator %q is not in the closed vocabulary", op)
	}
}

// parseCents parses canonical decimal money ("1234.56", "-7", "") into cents.
func parseCents(raw string) (*big.Int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return big.NewInt(0), nil
	}
	if !canonicalDecimal.MatchString(raw) {
		return nil, fmt.Errorf("value %q is not canonical decimal money", raw)
	}
	neg := strings.HasPrefix(raw, "-")
	digits := strings.TrimPrefix(raw, "-")
	whole, frac, _ := strings.Cut(digits, ".")
	cents := big.NewInt(0)
	cents.SetString(whole, 10)
	cents.Mul(cents, big.NewInt(100))
	if len(frac) == 1 {
		frac += "0"
	}
	if frac != "" {
		f, _ := new(big.Int).SetString(frac, 10)
		cents.Add(cents, f)
	}
	if neg {
		cents.Neg(cents)
	}
	return cents, nil
}

// formatCents renders cents as canonical decimal money ("1500.50", "-7.00").
func formatCents(cents *big.Int) string {
	neg := cents.Sign() < 0
	abs := new(big.Int).Abs(cents)
	dollars := new(big.Int).Quo(abs, big.NewInt(100))
	rem := new(big.Int).Rem(abs, big.NewInt(100))
	out := dollars.String() + "." + fmt.Sprintf("%02d", rem.Int64())
	if neg {
		return "-" + out
	}
	return out
}

// PageSubmission is the server-side view of a submitted page: scalar values
// by binding, group rows by group binding, and the admitted attachment
// references and signature ceremonies the client claims.
type PageSubmission struct {
	Values      map[string]string
	Rows        map[string][]map[string]string
	Attachments map[string]documentvalue.AttachmentReference
	Signatures  map[string]documentvalue.Signature
}

// SubmissionError is one field-scoped rejection. Row is -1 for errors that
// are not row-scoped.
type SubmissionError struct {
	Binding string
	Row     int
	Field   string
	Message string
}

func (e SubmissionError) Error() string {
	if e.Row >= 0 {
		return fmt.Sprintf("%s row %d field %s: %s", e.Binding, e.Row, e.Field, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.Binding, e.Message)
}

// ValidateSubmission is the server-side gate: it recomputes every computed
// field, enforces row caps and required fields, and re-checks admission and
// ceremony state. A nil return accepts the submission.
func ValidateSubmission(widgets []pagedef.WorkflowPageWidget, sub PageSubmission) []SubmissionError {
	var errs []SubmissionError
	byBinding := make(map[string]pagedef.WorkflowPageWidget, len(widgets))
	for _, w := range widgets {
		byBinding[w.Binding] = w
	}
	fail := func(binding string, row int, field, format string, args ...any) {
		errs = append(errs, SubmissionError{Binding: binding, Row: row, Field: field, Message: fmt.Sprintf(format, args...)})
	}
	for binding := range sub.Values {
		w, ok := byBinding[binding]
		if !ok {
			fail(binding, -1, "", "submits an undeclared binding")
			continue
		}
		switch w.Kind {
		case pagedef.WorkflowWidgetAttachment, pagedef.WorkflowWidgetSignature, pagedef.WorkflowWidgetRepeatingGroup:
			fail(binding, -1, "", "must arrive as a typed reference, not a scalar value")
		}
	}
	for binding := range sub.Rows {
		w, ok := byBinding[binding]
		if !ok || w.Kind != pagedef.WorkflowWidgetRepeatingGroup {
			fail(binding, -1, "", "submits rows for an undeclared group")
		}
	}
	for binding := range sub.Attachments {
		w, ok := byBinding[binding]
		if !ok || w.Kind != pagedef.WorkflowWidgetAttachment {
			fail(binding, -1, "", "submits an attachment for an undeclared binding")
		}
	}
	for binding := range sub.Signatures {
		w, ok := byBinding[binding]
		if !ok || w.Kind != pagedef.WorkflowWidgetSignature {
			fail(binding, -1, "", "submits a signature for an undeclared binding")
		}
	}
	for _, w := range widgets {
		switch w.Kind {
		case pagedef.WorkflowWidgetComputed:
			want, err := RecomputeComputed(w.ComputedOp, w.ComputedInputs, sub.Values)
			if err != nil {
				fail(w.Binding, -1, "", "cannot recompute: %v", err)
				continue
			}
			if got, ok := sub.Values[w.Binding]; ok && got != want {
				fail(w.Binding, -1, "", "computed value %q does not match the recalculated %q", got, want)
			}
		case pagedef.WorkflowWidgetRepeatingGroup:
			rows := sub.Rows[w.Binding]
			if len(rows) > w.RowCap {
				fail(w.Binding, -1, "", "holds %d rows above the cap of %d", len(rows), w.RowCap)
			}
			if w.Required && len(rows) == 0 {
				fail(w.Binding, -1, "", "requires at least one row")
			}
			declared := make(map[string]pagedef.WorkflowPageWidget, len(w.RowWidgets))
			for _, rw := range w.RowWidgets {
				declared[rw.Binding] = rw
			}
			for i, row := range rows {
				for field := range row {
					if _, ok := declared[field]; !ok {
						fail(w.Binding, i, field, "submits an undeclared row field")
					}
				}
				for _, rw := range w.RowWidgets {
					if rw.Required && strings.TrimSpace(row[rw.Binding]) == "" {
						fail(w.Binding, i, rw.Binding, "is required")
					}
				}
			}
		case pagedef.WorkflowWidgetAttachment:
			ref, ok := sub.Attachments[w.Binding]
			if !ok {
				if w.Required {
					fail(w.Binding, -1, "", "requires an admitted attachment")
				}
				continue
			}
			if ref.Evidence == nil {
				fail(w.Binding, -1, "", "attachment has no evidence reference")
				continue
			}
			if err := ref.Evidence.Check(); err != nil {
				fail(w.Binding, -1, "", "attachment is not admitted: %v", err)
				continue
			}
			if w.Accept != "" && !acceptAdmits(w.Accept, ref.DocumentType) {
				fail(w.Binding, -1, "", "attachment type %q is not in the accept list", ref.DocumentType)
			}
		case pagedef.WorkflowWidgetSignature:
			sig, ok := sub.Signatures[w.Binding]
			if !ok {
				if w.Required {
					fail(w.Binding, -1, "", "requires a signature")
				}
				continue
			}
			if err := sig.Validate(); err != nil {
				fail(w.Binding, -1, "", "signature is invalid: %v", err)
				continue
			}
			if sig.Ceremony.State != esign.StateSigned {
				fail(w.Binding, -1, "", "ceremony is %s, not signed", sig.Ceremony.State)
			}
		default:
			if w.Required && strings.TrimSpace(sub.Values[w.Binding]) == "" {
				fail(w.Binding, -1, "", "is required")
			}
		}
	}
	return errs
}

// acceptAdmits reports whether the admitted document type is in the widget's
// accept list. Intake derives types like PDF and JPEG; the list carries bare
// lowercase extensions.
func acceptAdmits(accept, documentType string) bool {
	for _, ext := range strings.Split(accept, ",") {
		if strings.EqualFold(strings.TrimSpace(ext), documentType) {
			return true
		}
	}
	return false
}
