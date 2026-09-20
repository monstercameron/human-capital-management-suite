package productui

// workerPayFacts is the pay fact set, matching the page
// adapter's compensation list field for field. Money and
// percentage formatting reuse the page's helpers through
// the locale-aware accessors.
var workerPayFacts = []sectionFact{
	{"person.base_pay", "base_pay", func(locale LocaleContext, person Person) string { return money(locale, person.BasePay) }},
	{"person.bonus_target", "bonus_target", func(locale LocaleContext, person Person) string { return percentage(locale, person.BonusTarget) }},
	{"person.pay_zone", "pay_zone", func(_ LocaleContext, person Person) string { return person.PayZone }},
	// The pay frequency is not a fact of its own: it is how the recorded pay
	// basis reads. Deriving it here rather than storing a second column is
	// what stops a record from ever saying ANNUAL_SALARY and "Hourly" at
	// once. A worker whose basis is withheld with their pay discloses no
	// frequency either, which is why this reads the same field the amount
	// was disclosed with.
	{"person.pay_frequency", "pay_frequency", func(locale LocaleContext, person Person) string {
		return employmentTerm(locale, person.PayBasis)
	}},
}

// ResolveWorkerPay resolves the worker pay and benefits
// section for one person record over the shared section
// engine. The record is never mutated.
func ResolveWorkerPay(locale LocaleContext, person Person, verdicts map[string]AuthorizedRecord) WorkerSection {
	return resolveWorkerSection(locale, person, verdicts, "person.compensation", "person.compensation_detail", workerPayFacts)
}
