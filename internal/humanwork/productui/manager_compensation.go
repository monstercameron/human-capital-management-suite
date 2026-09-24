package productui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/compensation"
)

// ManagerCompensationIntent is the presentation boundary for a governed
// worksheet or calibration submission. The application adapter binds the
// authenticated manager and tenant before translating it into domain intents.
type ManagerCompensationIntent struct {
	Kind     string
	Subjects []string
	CycleID  string
	Changes  []ManagerCompensationChange
}

type ManagerCompensationChange struct {
	Subject  string
	Amount   string
	Currency string
}

// ManagerCompensationProjection contains only compensation.Read results that
// the request composition authorized for this manager. Rows are intersected
// with the admitted People projection before any page renders them.
type ManagerCompensationProjection struct {
	Reads   []compensation.Result
	CycleID string
	Ranges  map[string]ManagerCompensationRange
	Submit  func(ManagerCompensationIntent, func(error))
}

type ManagerCompensationRange struct {
	Minimum  string
	Maximum  string
	Currency string
	Budget   string
}

type ManagerCompensationRow struct {
	Subject    string
	Name       string
	Disclosure string
	Components []ManagerCompensationComponent
	Range      *ManagerCompensationRange
}

type ManagerCompensationComponent struct {
	Kind      string
	Amount    string
	Currency  string
	Frequency string
}

var ErrManagerCompensationSubjectOutOfScope = errors.New("productui: compensation subject outside manager scope")

// scopedManagerCompensationRows is the shared population adapter for all five
// manager pages. A domain response is eligible for display only when it is a
// full, present response for a subject already admitted in the manager's
// request-scoped People projection.
func scopedManagerCompensationRows(tenant string, people []Person, projection ManagerCompensationProjection) []ManagerCompensationRow {
	byRef := make(map[string]Person, len(people))
	for _, person := range people {
		if person.WorkerID != "" {
			byRef[person.WorkerID] = person
		}
		if person.ID != "" {
			byRef[person.ID] = person
		}
	}
	rows := make([]ManagerCompensationRow, 0, len(projection.Reads))
	seen := make(map[string]bool, len(projection.Reads))
	for _, result := range projection.Reads {
		ref := result.Worker.Id
		person, ok := byRef[ref]
		if !ok || string(result.Worker.Tenant) != tenant || seen[ref] || result.Disclosure != "FULL" || result.Presence != "PRESENT" {
			continue
		}
		seen[ref] = true
		row := ManagerCompensationRow{Subject: ref, Name: person.Name, Disclosure: result.Disclosure}
		component := ManagerCompensationComponent{}
		for _, fact := range result.Fields {
			if fact.Access != compensation.EffectAllow || !fact.Value.IsValue() {
				continue
			}
			switch fact.Field {
			case compensation.FieldComponentType:
				component.Kind = fact.Value.MustValue()
			case compensation.FieldAmount:
				component.Amount = fact.Value.MustValue()
			case compensation.FieldCurrency:
				component.Currency = fact.Value.MustValue()
			case compensation.FieldFrequency:
				component.Frequency = fact.Value.MustValue()
			}
		}
		if component.Kind != "" || component.Amount != "" {
			row.Components = append(row.Components, component)
		}
		if band, exists := projection.Ranges[ref]; exists && band.Minimum != "" && band.Maximum != "" {
			copy := band
			row.Range = &copy
		}
		rows = append(rows, row)
	}
	return rows
}

func submitManagerCompensation(projection ManagerCompensationProjection, in ManagerCompensationIntent, rows []ManagerCompensationRow, done func(error)) error {
	if projection.Submit == nil || (in.Kind != "worksheet" && in.Kind != "calibration") || strings.TrimSpace(in.CycleID) == "" || in.CycleID != projection.CycleID || len(in.Subjects) == 0 {
		return ErrManagerCompensationSubjectOutOfScope
	}
	authorized := make(map[string]bool, len(rows))
	for _, row := range rows {
		authorized[row.Subject] = true
	}
	submitted := make(map[string]bool, len(in.Subjects))
	for _, subject := range in.Subjects {
		if !authorized[subject] || submitted[subject] {
			return ErrManagerCompensationSubjectOutOfScope
		}
		submitted[subject] = true
	}
	for _, change := range in.Changes {
		if !submitted[change.Subject] || strings.TrimSpace(change.Amount) == "" {
			return ErrManagerCompensationSubjectOutOfScope
		}
	}
	projection.Submit(in, done)
	return nil
}

func managerCompensationPage(view View, copyKey, intentKind string) ui.Node {
	projection := view.ManagerCompensation
	if projection == nil || strings.TrimSpace(projection.CycleID) == "" {
		return managerCompensationUnavailable(view, copyKey)
	}
	rows := scopedManagerCompensationRows(view.Tenant, view.People, *projection)
	if len(rows) == 0 {
		return managerCompensationUnavailable(view, copyKey)
	}
	children := []ui.Node{
		html.P(html.Props{Class: "muted"}, ui.Text(view.Locale.Text("page."+copyKey+".subtitle"))),
		html.P(html.Props{Class: "compensation-cycle", Raw: map[string]any{"role": "status"}}, ui.Text(managerCompensationCopy(view.Locale, "cycle")+": "+projection.CycleID)),
		html.P(html.Props{Class: "compensation-population-count"}, ui.Text(managerCompensationCopy(view.Locale, "population")+": "+view.Locale.FormatNumber(fmt.Sprint(len(rows)), 0))),
	}
	for _, row := range rows {
		facts := []ui.Node{html.H3(html.Props{}, ui.Text(row.Name))}
		if len(row.Components) == 0 {
			facts = append(facts, html.P(html.Props{Class: "muted"}, ui.Text(managerCompensationCopy(view.Locale, "no_components"))))
		}
		for _, component := range row.Components {
			label := component.Kind
			amount := component.Amount
			if component.Currency != "" && amount != "" {
				amount = view.Locale.FormatMoney(amount, component.Currency, decimalScale(amount))
			}
			if component.Frequency != "" {
				amount += " / " + component.Frequency
			}
			facts = append(facts, html.P(html.Props{}, ui.Text(label+": "+amount)))
		}
		if row.Range != nil {
			facts = append(facts, html.P(html.Props{}, ui.Text(managerCompensationCopy(view.Locale, "range")+": "+view.Locale.FormatMoney(row.Range.Minimum, row.Range.Currency, decimalScale(row.Range.Minimum))+"–"+view.Locale.FormatMoney(row.Range.Maximum, row.Range.Currency, decimalScale(row.Range.Maximum)))))
			if row.Range.Budget != "" {
				facts = append(facts, html.P(html.Props{}, ui.Text(managerCompensationCopy(view.Locale, "budget")+": "+view.Locale.FormatMoney(row.Range.Budget, row.Range.Currency, decimalScale(row.Range.Budget)))))
			}
		}
		children = append(children, html.Section(html.Props{Class: "manager-compensation-row", Raw: map[string]any{"aria-label": row.Name}}, facts...))
	}
	if intentKind != "" && projection.Submit != nil {
		children = append(children, managerCompensationForm(view, *projection, rows, intentKind))
	}
	return html.Main(html.Props{Class: "manager-compensation-page"},
		html.H1(html.Props{}, ui.Text(view.Title)),
		html.Div(html.Props{Class: "manager-compensation-population"}, children...),
	)
}

func managerCompensationUnavailable(view View, copyKey string) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text(copyKey + ".unavailable_title"),
		Description: view.Locale.Text(copyKey + ".unavailable_detail"), Role: "status",
		Action: &ActionLinkProps{Label: view.Locale.Text(copyKey + ".return_home"), Href: statefulHref(view, PageHome), Class: "button primary", Navigate: view.Navigate},
	})
}

func managerCompensationForm(view View, projection ManagerCompensationProjection, rows []ManagerCompensationRow, kind string) ui.Node {
	amounts := ui.UseState(map[string]string{})
	status := ui.UseState("")
	busy := ui.UseState(false)
	inputs := make([]ui.Node, 0, len(rows)*2+2)
	for _, row := range rows {
		id := "manager-compensation-amount-" + row.Subject
		props := html.Props{ID: id, Name: "amount-" + row.Subject, Type: "number", Required: false, Min: "0", Step: "0.01", Value: amounts.Get()[row.Subject], Raw: map[string]any{"aria-describedby": id + "-help manager-compensation-status"}}
		subject := row.Subject
		props.OnInput = ui.UseEvent(func(event ui.InputEvent) {
			next := map[string]string{}
			for key, value := range amounts.Get() {
				next[key] = value
			}
			next[subject] = event.GetValue()
			amounts.Set(next)
		})
		inputs = append(inputs,
			html.Label(html.Props{For: id}, ui.Text(managerCompensationCopy(view.Locale, "proposed_amount")+" "+row.Name)),
			html.Input(props),
			html.Span(html.Props{ID: id + "-help", Class: "sr-only"}, ui.Text(managerCompensationCopy(view.Locale, "omit"))),
		)
	}
	submit := ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		intent := ManagerCompensationIntent{Kind: kind, CycleID: projection.CycleID}
		for _, row := range rows {
			amount := strings.TrimSpace(amounts.Get()[row.Subject])
			if amount == "" {
				continue
			}
			intent.Subjects = append(intent.Subjects, row.Subject)
			change := ManagerCompensationChange{Subject: row.Subject, Amount: amount}
			if len(row.Components) > 0 {
				change.Currency = row.Components[0].Currency
			}
			intent.Changes = append(intent.Changes, change)
		}
		if len(intent.Subjects) == 0 {
			status.Set(managerCompensationCopy(view.Locale, "enter_amount"))
			return
		}
		busy.Set(true)
		status.Set(managerCompensationCopy(view.Locale, "submitting"))
		if err := submitManagerCompensation(projection, intent, rows, func(err error) {
			busy.Set(false)
			if err != nil {
				status.Set(managerCompensationCopy(view.Locale, "failed"))
				return
			}
			status.Set(managerCompensationCopy(view.Locale, "submitted"))
		}); err != nil {
			busy.Set(false)
			status.Set(managerCompensationCopy(view.Locale, "refused"))
		}
	})
	inputs = append(inputs, html.P(html.Props{Class: "manager-compensation-status", Raw: map[string]any{"role": "status"}}, ui.Text(status.Get())))
	inputs = append(inputs, html.Button(html.Props{Class: "button primary", Type: "submit", Disabled: busy.Get()}, ui.Text(managerCompensationCopy(view.Locale, "submit"))))
	return html.Form(html.Props{Class: "manager-compensation-form", OnSubmit: submit}, inputs...)
}

func managerCompensationCopy(locale LocaleContext, key string) string {
	switch locale.normalized().Resolved {
	case "de-DE":
		switch key {
		case "cycle":
			return "Zyklus"
		case "population":
			return "Freigegebene Population"
		case "no_components":
			return "Es wurden keine Vergütungsbestandteile offengelegt."
		case "range":
			return "Freigegebene Gehaltsspanne"
		case "budget":
			return "Freigegebenes Budget"
		case "proposed_amount":
			return "Vorgeschlagener Jahresbetrag für"
		case "omit":
			return "Leer lassen, um diese Person auszulassen."
		case "enter_amount":
			return "Geben Sie mindestens einen vorgeschlagenen Betrag ein."
		case "submitting":
			return "Wird übermittelt …"
		case "failed":
			return "Die Übermittlung ist fehlgeschlagen."
		case "submitted":
			return "Zur geregelten Prüfung übermittelt."
		case "refused":
			return "Die Übermittlung wurde abgelehnt."
		case "submit":
			return "Zur Prüfung übermitteln"
		}
	case "ar":
		switch key {
		case "cycle":
			return "الدورة"
		case "population":
			return "المجموعة المصرح بها"
		case "no_components":
			return "لم يتم الإفصاح عن مكونات التعويض."
		case "range":
			return "نطاق الراتب المصرح به"
		case "budget":
			return "الميزانية المصرح بها"
		case "proposed_amount":
			return "المبلغ السنوي المقترح لـ"
		case "omit":
			return "اتركه فارغًا لاستبعاد هذا الموظف."
		case "enter_amount":
			return "أدخل مبلغًا مقترحًا واحدًا على الأقل."
		case "submitting":
			return "جارٍ الإرسال…"
		case "failed":
			return "تعذر إرسال الطلب."
		case "submitted":
			return "أُرسل للمراجعة المعتمدة."
		case "refused":
			return "رُفض الإرسال."
		case "submit":
			return "إرسال للمراجعة"
		}
	default:
		switch key {
		case "cycle":
			return "Cycle"
		case "population":
			return "Authorized population"
		case "no_components":
			return "No compensation components were disclosed."
		case "range":
			return "Authorized range"
		case "budget":
			return "Authorized budget"
		case "proposed_amount":
			return "Proposed annual amount for"
		case "omit":
			return "Leave blank to omit this worker."
		case "enter_amount":
			return "Enter at least one proposed amount."
		case "submitting":
			return "Submitting…"
		case "failed":
			return "Submission failed."
		case "submitted":
			return "Submitted for governed review."
		case "refused":
			return "Submission refused."
		case "submit":
			return "Submit for review"
		}
	}
	return ""
}
