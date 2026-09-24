package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/compensation"
)

// PayReadState is the server-composed result state for an authenticated
// employee pay read. Unknown states fail closed in the page adapters.
type PayReadState string

const (
	PayReadLoading  PayReadState = "loading"
	PayReadReady    PayReadState = "ready"
	PayReadNoRecord PayReadState = "no_record"
	PayReadWithheld PayReadState = "withheld"
	PayReadError    PayReadState = "error"
)

// PayValueProjection preserves the domain's field-level authorization and
// Presence result. Value is rendered only for ALLOW + VALUE.
type PayValueProjection struct {
	Included     bool
	Access       string
	Presence     string
	Value        string
	DenialReason string
}

// PaySummaryProjection carries only the employee-safe fields returned by the
// governed compensation read. It has no subject selector or authority input.
type PaySummaryProjection struct {
	State             PayReadState
	Amount            PayValueProjection
	Currency          PayValueProjection
	PayBasis          PayValueProjection
	Frequency         PayValueProjection
	EffectiveInterval PayValueProjection
	KnownAt           PayValueProjection
}

// PaySummaryFromCompensationRead adapts the governed domain result without
// widening its field mask. Error and unrecognized result states fail closed.
func PaySummaryFromCompensationRead(result compensation.Result, readErr error) PaySummaryProjection {
	if readErr != nil {
		return PaySummaryProjection{State: PayReadError}
	}
	if result.Disclosure == "WITHHELD" {
		return PaySummaryProjection{State: PayReadWithheld}
	}
	if result.Presence == "ABSENT" {
		return PaySummaryProjection{State: PayReadNoRecord}
	}
	if result.Presence != "PRESENT" || (result.Disclosure != "FULL" && result.Disclosure != "PARTIAL") {
		return PaySummaryProjection{State: PayReadError}
	}
	projection := PaySummaryProjection{State: PayReadReady}
	seen := make(map[compensation.FieldID]bool, len(result.Fields))
	for _, field := range result.Fields {
		if seen[field.Field] {
			return PaySummaryProjection{State: PayReadError}
		}
		seen[field.Field] = true
		value := PayValueProjection{Included: true, Access: field.Access.String(), Presence: field.Value.State().String(), DenialReason: field.DenialReason}
		if field.Access == compensation.EffectAllow {
			if disclosed, ok := field.Value.Get(); ok {
				value.Value = disclosed
			}
		}
		switch field.Field {
		case compensation.FieldAmount:
			projection.Amount = value
		case compensation.FieldCurrency:
			projection.Currency = value
		case compensation.FieldPayBasis:
			projection.PayBasis = value
		case compensation.FieldFrequency:
			projection.Frequency = value
		case compensation.FieldEffectiveInterval:
			projection.EffectiveInterval = value
		case compensation.FieldKnownAt:
			projection.KnownAt = value
		}
	}
	return projection
}

// PayStatementRow is the minimal, already-authorized statement projection.
// The page cannot request a worker or widen the statement field set.
type PayStatementRow struct {
	StatementRef string
	Period       string
	PayDate      string
	NetAmount    PayValueProjection
	Currency     PayValueProjection
}

type PayStatementsProjection struct {
	State      PayReadState
	Statements []PayStatementRow
}

type PayStatementOption struct {
	StatementRef string
	PeriodRef    string
	PeriodLabel  string
}

type PayDiscrepancyCategory struct {
	Code  string
	Label string
}

// PayDiscrepancyRequest deliberately contains no tenant, principal, worker,
// role, or authority fields. The authenticated intake service derives them.
type PayDiscrepancyRequest struct {
	StatementRef string
	PeriodRef    string
	Category     string
	Reason       string
}

type PayDiscrepancyProjection struct {
	Statements []PayStatementOption
	Categories []PayDiscrepancyCategory
	Submit     func(PayDiscrepancyRequest, func(error))
}

func paySummaryPage(view View) ui.Node {
	projection := view.PaySummary
	switch projection.State {
	case PayReadLoading:
		return ui.CreateElement(EmptyState, EmptyStateProps{Title: payText(view, "loading_title"), Description: payText(view, "loading_detail"), Role: "status"})
	case PayReadReady:
		return paySummaryContent(view, projection)
	case PayReadWithheld:
		return ui.CreateElement(EmptyState, EmptyStateProps{Title: payText(view, "withheld_title"), Description: payText(view, "withheld_detail"), Role: "status"})
	case PayReadNoRecord:
		return payStateFallback(view, "pay_summary", "no_record")
	case PayReadError:
		return payStateFallback(view, "pay_summary", "read_error")
	default:
		return payUnavailable(view, "pay_summary")
	}
}

func paySummaryContent(view View, projection PaySummaryProjection) ui.Node {
	rows := []ui.Node{}
	amount := payVisibleValue(projection.Amount)
	currency := payVisibleValue(projection.Currency)
	if amount != "" && currency != "" {
		scale := decimalScale(amount)
		rows = append(rows, payFactRow(view, "amount", view.Locale.FormatMoney(amount, currency, scale)))
	} else if payFieldRestricted(projection.Amount) || payFieldRestricted(projection.Currency) {
		rows = append(rows, payFactRow(view, "amount", payText(view, "restricted")))
	} else if payFieldUnknown(projection.Amount) || payFieldUnknown(projection.Currency) {
		rows = append(rows, payFactRow(view, "amount", payText(view, "unknown")))
	}
	for _, field := range []struct {
		key  string
		fact PayValueProjection
	}{{"basis", projection.PayBasis}, {"frequency", projection.Frequency}, {"effective", projection.EffectiveInterval}, {"known_at", projection.KnownAt}} {
		if value := payVisibleValue(field.fact); value != "" {
			rows = append(rows, payFactRow(view, field.key, value))
		} else if payFieldRestricted(field.fact) {
			rows = append(rows, payFactRow(view, field.key, payText(view, "restricted")))
		} else if payFieldUnknown(field.fact) {
			rows = append(rows, payFactRow(view, field.key, payText(view, "unknown")))
		}
	}
	if len(rows) == 0 {
		return ui.CreateElement(EmptyState, EmptyStateProps{Title: payText(view, "details_unavailable_title"), Description: payText(view, "details_unavailable_detail"), Role: "status"})
	}
	return html.Section(html.Props{Class: "surface pay-summary", Raw: map[string]any{"aria-labelledby": "pay-summary-details-title"}},
		html.H2(html.Props{ID: "pay-summary-details-title"}, ui.Text(payText(view, "details_title"))),
		html.Tag("dl", html.Props{Class: "pay-facts"}, rows...),
	)
}

func payFactRow(view View, key, value string) ui.Node {
	return html.Div(html.Props{Class: "pay-fact"}, html.Tag("dt", html.Props{}, ui.Text(payText(view, key+"_label"))), html.Tag("dd", html.Props{}, ui.Text(value)))
}

func payStatementsPage(view View) ui.Node {
	projection := view.PayStatements
	if projection.State == PayReadLoading {
		return ui.CreateElement(EmptyState, EmptyStateProps{Title: payText(view, "loading_title"), Description: payText(view, "loading_detail"), Role: "status"})
	}
	if projection.State == PayReadNoRecord {
		return payStateFallback(view, "pay_statements", "no_record")
	}
	if projection.State == PayReadError {
		return payStateFallback(view, "pay_statements", "read_error")
	}
	if projection.State == PayReadWithheld {
		return ui.CreateElement(EmptyState, EmptyStateProps{Title: payText(view, "withheld_title"), Description: payText(view, "withheld_detail"), Role: "status"})
	}
	if projection.State != PayReadReady || len(projection.Statements) == 0 {
		return payUnavailable(view, "pay_statements")
	}
	rows := make([]ui.Node, 0, len(projection.Statements))
	for _, statement := range projection.Statements {
		amount, currency := payVisibleValue(statement.NetAmount), payVisibleValue(statement.Currency)
		amountValue := payText(view, "unknown")
		if amount != "" && currency != "" {
			amountValue = view.Locale.FormatMoney(amount, currency, decimalScale(amount))
		} else if payFieldRestricted(statement.NetAmount) || payFieldRestricted(statement.Currency) {
			amountValue = payText(view, "restricted")
		}
		rows = append(rows, html.Tr(html.Props{}, html.Td(html.Props{}, ui.Text(statement.Period)), html.Td(html.Props{}, ui.Text(statement.PayDate)), html.Td(html.Props{}, ui.Text(amountValue))))
	}
	return html.Section(html.Props{Class: "surface pay-statements", Raw: map[string]any{"aria-labelledby": "pay-statements-list-title"}},
		html.H2(html.Props{ID: "pay-statements-list-title"}, ui.Text(payText(view, "statements_title"))),
		html.Table(html.Props{}, html.Thead(html.Props{}, html.Tr(html.Props{}, html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(payText(view, "period_label"))), html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(payText(view, "pay_date_label"))), html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(payText(view, "net_pay_label"))))), html.Tbody(html.Props{}, rows...)),
	)
}

func payDiscrepancyPage(view View) ui.Node {
	projection := view.PayDiscrepancy
	if projection.Submit == nil || len(projection.Statements) == 0 || len(projection.Categories) == 0 {
		return payUnavailable(view, "pay_discrepancy")
	}
	return payDiscrepancyForm(view, projection)
}

func payDiscrepancyForm(view View, projection PayDiscrepancyProjection) ui.Node {
	statementRef := ui.UseState("")
	periodRef := ui.UseState("")
	category := ui.UseState("")
	reason := ui.UseState("")
	busy := ui.UseState(false)
	submitted := ui.UseState(false)
	failed := ui.UseState(false)
	statementProps := html.Props{ID: "pay-discrepancy-statement", Name: "statement", Required: true, Raw: map[string]any{"aria-describedby": "pay-discrepancy-help"}}
	statementProps.OnChange = ui.UseEvent(func(event ui.InputEvent) {
		selected := event.GetValue()
		for _, option := range projection.Statements {
			if selected == option.StatementRef+"\x00"+option.PeriodRef {
				statementRef.Set(option.StatementRef)
				periodRef.Set(option.PeriodRef)
				return
			}
		}
		statementRef.Set("")
		periodRef.Set("")
	})
	categoryProps := html.Props{ID: "pay-discrepancy-category", Name: "category", Required: true}
	categoryProps.OnChange = ui.UseEvent(func(event ui.InputEvent) { category.Set(event.GetValue()) })
	reasonProps := html.Props{ID: "pay-discrepancy-reason", Name: "reason", Required: true, MaxLength: 2000, Rows: 5}
	reasonProps.OnInput = ui.UseEvent(func(event ui.InputEvent) { reason.Set(event.GetValue()) })
	status := ui.Node(nil)
	if failed.Get() {
		status = html.P(html.Props{ID: "pay-discrepancy-status", Raw: map[string]any{"role": "alert"}}, ui.Text(payText(view, "submit_failed")))
	} else if busy.Get() {
		status = html.P(html.Props{ID: "pay-discrepancy-status", Raw: map[string]any{"role": "status"}}, ui.Text(payText(view, "submitting")))
	} else if submitted.Get() {
		status = html.P(html.Props{ID: "pay-discrepancy-status", Raw: map[string]any{"role": "status"}}, ui.Text(payText(view, "submitted")))
	}
	statementOptions := []ui.Node{html.Option(html.Props{Value: ""}, ui.Text(payText(view, "choose_statement")))}
	for _, option := range projection.Statements {
		statementOptions = append(statementOptions, html.Option(html.Props{Value: option.StatementRef + "\x00" + option.PeriodRef}, ui.Text(option.PeriodLabel)))
	}
	categoryOptions := []ui.Node{html.Option(html.Props{Value: ""}, ui.Text(payText(view, "choose_category")))}
	for _, option := range projection.Categories {
		if option.Code != "" && option.Label != "" {
			categoryOptions = append(categoryOptions, html.Option(html.Props{Value: option.Code}, ui.Text(option.Label)))
		}
	}
	submit := ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		statement, selectedPeriod, selectedCategory, detail := statementRef.Get(), periodRef.Get(), category.Get(), strings.TrimSpace(reason.Get())
		request := PayDiscrepancyRequest{StatementRef: statement, PeriodRef: selectedPeriod, Category: selectedCategory, Reason: detail}
		if busy.Get() {
			return
		}
		busy.Set(true)
		failed.Set(false)
		submitted.Set(false)
		if !submitPayDiscrepancy(projection, request, func(err error) {
			busy.Set(false)
			if err != nil {
				failed.Set(true)
				return
			}
			submitted.Set(true)
		}) {
			busy.Set(false)
			return
		}
	})
	return html.Section(html.Props{Class: "surface pay-discrepancy", Raw: map[string]any{"aria-labelledby": "pay-discrepancy-form-title"}},
		html.H2(html.Props{ID: "pay-discrepancy-form-title"}, ui.Text(payText(view, "intake_title"))),
		html.P(html.Props{ID: "pay-discrepancy-help", Class: "muted"}, ui.Text(payText(view, "intake_detail"))),
		html.Form(html.Props{Class: "pay-discrepancy-form", OnSubmit: submit},
			html.Label(html.Props{For: statementProps.ID}, ui.Text(payText(view, "statement_label"))), html.Select(statementProps, statementOptions...),
			html.Label(html.Props{For: categoryProps.ID}, ui.Text(payText(view, "category_label"))), html.Select(categoryProps, categoryOptions...),
			html.Label(html.Props{For: reasonProps.ID}, ui.Text(payText(view, "reason_label"))), html.Textarea(reasonProps),
			status,
			html.Button(html.Props{Class: "button primary", Type: "submit", Disabled: busy.Get()}, ui.Text(payText(view, "submit_label"))),
		),
	)
}

func submitPayDiscrepancy(projection PayDiscrepancyProjection, request PayDiscrepancyRequest, done func(error)) bool {
	if projection.Submit == nil || done == nil || strings.TrimSpace(request.Reason) == "" {
		return false
	}
	statementOK, categoryOK := false, false
	for _, option := range projection.Statements {
		statementOK = statementOK || option.StatementRef == request.StatementRef && option.PeriodRef == request.PeriodRef && request.StatementRef != "" && request.PeriodRef != ""
	}
	for _, option := range projection.Categories {
		categoryOK = categoryOK || option.Code == request.Category && option.Code != ""
	}
	if !statementOK || !categoryOK {
		return false
	}
	projection.Submit(request, done)
	return true
}

func payUnavailable(view View, prefix string) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title: view.Locale.Text(prefix + ".unavailable_title"), Description: view.Locale.Text(prefix + ".unavailable_detail"), Role: "status",
		Action: &ActionLinkProps{Label: view.Locale.Text(prefix + ".return_home"), Href: statefulHref(view, PageHome), Class: "button primary", Navigate: view.Navigate},
	})
}

func payStateFallback(view View, prefix, state string) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title: view.Locale.Text(prefix + "." + state + "_title"), Description: view.Locale.Text(prefix + "." + state + "_detail"), Role: "status",
		Action: &ActionLinkProps{Label: view.Locale.Text(prefix + ".return_home"), Href: statefulHref(view, PageHome), Class: "button primary", Navigate: view.Navigate},
	})
}

func payText(view View, key string) string { return view.Locale.Text("pay_ui." + key) }

func payVisibleValue(value PayValueProjection) string {
	if value.Access != "ALLOW" || value.Presence != "VALUE" {
		return ""
	}
	return strings.TrimSpace(value.Value)
}

func payFieldRestricted(value PayValueProjection) bool {
	return value.Included && (value.Access == "DENY" || value.Presence == "REDACTED")
}

func payFieldUnknown(value PayValueProjection) bool {
	return value.Included && value.Access != "DENY" && value.Presence != "VALUE"
}

func decimalScale(value string) int {
	if dot := strings.IndexByte(value, '.'); dot >= 0 && len(value)-dot-1 <= 9 {
		return len(value) - dot - 1
	}
	return 0
}
