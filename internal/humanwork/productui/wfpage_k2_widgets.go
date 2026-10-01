package productui

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

var ErrRepeatingGroupCap = errors.New("workflow page: repeating group row cap reached")

type RepeatingRow struct {
	ID     string
	Values map[string]string
	Errors map[string]string
}

type RepeatingGroupProps struct {
	ID          string
	Label       string
	AddLabel    string
	RemoveLabel string
	MaxRows     int
	Rows        []RepeatingRow
}

func AddRepeatingRow(rows []RepeatingRow, max int, id string) ([]RepeatingRow, error) {
	if max <= 0 || len(rows) >= max {
		return append([]RepeatingRow(nil), rows...), ErrRepeatingGroupCap
	}
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("workflow page: row id is required")
	}
	out := cloneRepeatingRows(rows)
	out = append(out, RepeatingRow{ID: id, Values: map[string]string{}, Errors: map[string]string{}})
	return out, nil
}

func RemoveRepeatingRow(rows []RepeatingRow, id string) []RepeatingRow {
	out := make([]RepeatingRow, 0, len(rows))
	for _, row := range rows {
		if row.ID != id {
			out = append(out, cloneRepeatingRow(row))
		}
	}
	return out
}

func RepeatingGroup(props RepeatingGroupProps) ui.Node {
	rows := make([]ui.Node, 0, len(props.Rows)+2)
	for index, row := range props.Rows {
		rowID := props.ID + "-row-" + row.ID
		cells := []ui.Node{html.Strong(html.Props{}, ui.Text(fmt.Sprintf("%s %d", props.Label, index+1)))}
		fields := make([]string, 0, len(row.Values))
		for field := range row.Values {
			fields = append(fields, field)
		}
		sort.Strings(fields)
		for _, field := range fields {
			value := row.Values[field]
			inputID := rowID + "-" + field
			errorID := inputID + "-error"
			aria := map[string]string{}
			if row.Errors[field] != "" {
				aria["describedby"] = errorID
				aria["invalid"] = "true"
			}
			cells = append(cells, html.Label(html.Props{For: inputID}, ui.Text(field)), html.Input(html.Props{ID: inputID, Name: field, Value: value, Aria: aria}))
			if message := row.Errors[field]; message != "" {
				cells = append(cells, html.P(html.Props{ID: errorID, Class: "field-error", Raw: map[string]any{"role": "alert"}}, ui.Text(message)))
			}
		}
		cells = append(cells, html.Button(html.Props{Type: "button", Class: "secondary", Data: map[string]string{"workflow-page-remove-row": row.ID}}, ui.Text(props.RemoveLabel)))
		rows = append(rows, html.Fieldset(html.Props{Class: "workflow-page-repeating-row", ID: rowID}, cells...))
	}
	rows = append(rows, html.Button(html.Props{Type: "button", Class: "secondary", Disabled: props.MaxRows > 0 && len(props.Rows) >= props.MaxRows, Data: map[string]string{"workflow-page-add-row": props.ID}}, ui.Text(props.AddLabel)))
	return html.Fieldset(html.Props{Class: "workflow-page-repeating", ID: props.ID, Raw: map[string]any{"aria-label": props.Label, "data-row-cap": props.MaxRows}}, rows...)
}

type AttachmentWidgetProps struct {
	ID           string
	Label        string
	Description  string
	ReferenceID  string
	DocumentType string
	Error        string
	Disabled     bool
}

func AttachmentWidget(props AttachmentWidgetProps) ui.Node {
	children := []ui.Node{html.Label(html.Props{For: props.ID}, ui.Text(props.Label)), html.Input(html.Props{ID: props.ID, Type: "file", Disabled: props.Disabled, Raw: map[string]any{"aria-describedby": props.ID + "-description"}})}
	if props.Description != "" {
		children = append(children, html.P(html.Props{ID: props.ID + "-description", Class: "field-description"}, ui.Text(props.Description)))
	}
	if props.ReferenceID != "" {
		children = append(children, html.P(html.Props{Class: "workflow-page-attachment-reference", Raw: map[string]any{"data-evidence-ref": props.ReferenceID}}, ui.Text(props.DocumentType+" attachment admitted")))
	}
	if props.Error != "" {
		children = append(children, html.P(html.Props{Class: "field-error", Raw: map[string]any{"role": "alert"}}, ui.Text(props.Error)))
	}
	return html.Div(html.Props{Class: "workflow-page-attachment"}, children...)
}

type SignatureWidgetProps struct {
	ID             string
	Label          string
	AssuranceLevel string
	State          string
	Error          string
}

func SignatureWidget(props SignatureWidgetProps) ui.Node {
	children := []ui.Node{html.Strong(html.Props{}, ui.Text(props.Label)), html.P(html.Props{Class: "workflow-page-signature-assurance"}, ui.Text("Assurance level: "+props.AssuranceLevel)), html.Input(html.Props{ID: props.ID, Type: "hidden", Value: props.State, ReadOnly: true})}
	if props.Error != "" {
		children = append(children, html.P(html.Props{Class: "field-error", Raw: map[string]any{"role": "alert"}}, ui.Text(props.Error)))
	}
	return html.Fieldset(html.Props{Class: "workflow-page-signature", ID: props.ID, Raw: map[string]any{"aria-label": props.Label}}, children...)
}

type ComputedWidgetProps struct {
	ID          string
	Label       string
	Value       string
	Description string
	Error       string
}

func ComputedWidget(props ComputedWidgetProps) ui.Node {
	aria := map[string]string{"readonly": "true"}
	children := []ui.Node{html.Label(html.Props{For: props.ID}, ui.Text(props.Label)), html.Input(html.Props{ID: props.ID, Value: props.Value, ReadOnly: true, Aria: aria})}
	if props.Description != "" {
		children = append(children, html.P(html.Props{Class: "field-description"}, ui.Text(props.Description)))
	}
	if props.Error != "" {
		children = append(children, html.P(html.Props{Class: "field-error", Raw: map[string]any{"role": "alert"}}, ui.Text(props.Error)))
	}
	return html.Div(html.Props{Class: "workflow-page-computed", Data: map[string]string{"workflow-page-read-only": "true"}}, children...)
}

func cloneRepeatingRows(rows []RepeatingRow) []RepeatingRow {
	out := make([]RepeatingRow, len(rows))
	for i, row := range rows {
		out[i] = cloneRepeatingRow(row)
	}
	return out
}
func cloneRepeatingRow(row RepeatingRow) RepeatingRow {
	row.Values = cloneStringMap(row.Values)
	row.Errors = cloneStringMap(row.Errors)
	return row
}
func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
