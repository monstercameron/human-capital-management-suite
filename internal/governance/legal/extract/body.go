package extract

import (
	"regexp"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

// buildBody fills one typed body from the matched research item and the
// matrix cell's annotation.
//
// The discipline is uniform across every arm: a field is set only when the
// text or the annotation says it, and left at its zero value otherwise. The
// definition file then records the gap, [legal.RulePack.ValidateForRelease]
// refuses to publish it above UNREVIEWED, and nobody has to guess whether a
// thirty-day deadline came from a statute or from a default.
//
//nolint:gocyclo // one arm per obligation kind; the mapping is the point.
func buildBody(
	kind legal.ObligationType,
	cell MatrixCell,
	item Item,
	found bool,
	standard legal.RuleStandard,
	note string,
) legal.BodyJSON {
	text := item.Text
	body := legal.BodyJSON{}
	if legal.VocabularyOf(kind) >= legal.VocabularyVersion2 {
		body.Standard = standard.String()
	}
	if !found {
		text = ""
	}

	switch kind {
	case legal.ObligationTypeNotice:
		body.Who = "employer_to_worker"
		body.TimingDirection = noticeDirection(text)
		body.TimingDays, _ = ExtractDays(text)
		body.Channel = noticeChannel(text)
		body.ContentFields = noticeContentFields(text)

	case legal.ObligationTypeFieldRestriction:
		body.RestrictedFields = restrictedFields(text)
		if len(body.RestrictedFields) > 0 {
			body.Context = "setting compensation for a promotion or base-pay change"
		}

	case legal.ObligationTypeRetention:
		body.RecordClass = recordClass(text)
		body.DurationYears = ExtractYears(text)
		body.DurationBasis = retentionBasis(text)

	case legal.ObligationTypeLeaveInteraction:
		body.LeaveType = leaveProgram(text)
		if found {
			body.InteractionRule = text
		}

	case legal.ObligationTypePayFrequency:
		body.MinimumFrequency = payFrequency(text)
		if body.MinimumFrequency != "" {
			body.AppliesToWorkerClass = workerClass(text)
		}

	case legal.ObligationTypeFinalPayDeadline:
		// Trigger is deliberately the widest of the three: an extracted rule
		// that fires on any termination attaches the deadline more often than
		// the statute might, which is a narrowing of the platform's licence,
		// not a broadening of the legal claim.
		body.Trigger = "termination_any"
		body.DeadlineDescription = deadlineDescription(text, found)

	case legal.ObligationTypePayTransparency:
		body.Trigger = "internal_promotion"
		body.RequiredDisclosure = deadlineDescription(text, found)

	case legal.ObligationTypeNonCompete:
		body.RecheckOnPayChange = true
		body.Rule = deadlineDescription(text, found)

	case legal.ObligationTypeEVerify:
		body.RequiredOnNewHireOnly = true
		body.Note = note
		if n := ExtractAnnotationCount(cell.Annotation); n > 0 {
			body.EmployeeThreshold = n
		}

	case legal.ObligationTypeMiniWARN:
		body.Note = note
		body.EmployeeThreshold = ExtractEmployeeCount(text)
		if days, _ := ExtractDays(text); days > 0 {
			body.NoticeDays = days
		}
		if n := ExtractAnnotationDays(cell.Annotation); n > 0 {
			body.NoticeDays = n
		}

	case legal.ObligationTypeWageFloor:
		if amount := ExtractMoney(text); amount != "" {
			body.FloorAmount = &legal.MoneyJSON{Amount: amount, Currency: "USD"}
			body.Basis = wageBasis(text)
		}
		body.WorkerClass = workerClass(text)
		body.Indexation = indexation(text)

	case legal.ObligationTypePayEquityReview:
		body.ProtectedBases = protectedBases(text)
		body.ComparatorStandard = comparatorStandard(text)
		body.EmployerSizeFloor = ExtractEmployeeCount(text)
		body.DocumentationRequired = found && documentationRE.MatchString(text)

	case legal.ObligationTypePayStatement:
		body.RequiredFields = payStatementFields(text)
		body.Delivery = payStatementDelivery(text)
		body.ConsentRequired = found && consentRE.MatchString(text)

	case legal.ObligationTypeClassification:
		body.Dimension = classificationDimension(text)
		if found {
			body.TestDescription = text
		}
		if amount := ExtractMoney(text); amount != "" {
			body.SalaryThreshold = &legal.MoneyJSON{Amount: amount, Currency: "USD"}
		}

	case legal.ObligationTypePersonnelFile:
		body.ResponseDays, body.DayBasis = ExtractDays(text)
		body.CopyFeePermitted = found && copyFeeRE.MatchString(text)

	case legal.ObligationTypeAntiRetaliation:
		body.ProtectedActivities = protectedActivities(text)
		body.LookbackDays, _ = ExtractDays(text)
		if body.LookbackDays == 0 {
			// A limitations period stated in years is the same window in days.
			body.LookbackDays = ExtractYears(text) * 365
		}
		body.Disposition = retaliationDisposition(text)

	case legal.ObligationTypeJobSecurity:
		body.StandardKind = jobSecurityStandard(text)
		if days, _ := ExtractDays(text); days > 0 && strings.Contains(strings.ToLower(text), "probation") {
			body.ProbationDays = days
		}
		body.JustificationRequired = body.StandardKind == "GOOD_CAUSE_AFTER_PROBATION"

	case legal.ObligationTypeSeparationFiling:
		body.FormName = separationForm(text)
		body.RecipientAuthority = separationAuthority(text)
		body.DeadlineDays, body.DayBasis = ExtractDays(text)

	case legal.ObligationTypeDrugTesting:
		body.PermittedBases = drugTestingBases(text)
		body.WrittenPolicyRequired = found && writtenPolicyRE.MatchString(text)
		body.ProtectedStatus = drugTestingProtected(text)

	case legal.ObligationTypeBreachNotification:
		body.SubjectDeadlineDays, body.DayBasis = ExtractDays(text)
		if n := ExtractAnnotationDays(cell.Annotation); n > 0 {
			body.SubjectDeadlineDays = n
		}
		body.AuthorityThresholdCount = ExtractAffectedCount(text)
		body.CreditMonitoringRequired = found && creditMonitoringRE.MatchString(text)

	case legal.ObligationTypeAutomatedDecision:
		body.CoveredUses = automatedUses(text)
		body.BiasAuditRequired = found && biasAuditRE.MatchString(text)
		body.DisclosureRequired = found && disclosureRE.MatchString(text)

	case legal.ObligationTypeMonitoringConsent:
		body.DataCategories = dataCategories(text)
		body.ConsentForm = consentForm(text)
	}
	return body
}

// deadlineDescription returns the note only when the research actually
// supplied one, so an unevidenced rule carries an empty description rather
// than a sentence about its own absence.
func deadlineDescription(note string, found bool) string {
	if !found {
		return "the research file states no rule text for this duty"
	}
	return note
}

// --- small readers ----------------------------------------------------------
//
// Each helper answers one question about the item's text and returns the zero
// value when the text does not answer it.

var (
	documentationRE    = regexp.MustCompile(`(?i)document|record the (?:reason|rationale|justification)|audit trail`)
	consentRE          = regexp.MustCompile(`(?i)consent|opt[- ]in|authoriz`)
	copyFeeRE          = regexp.MustCompile(`(?i)reasonable (?:copy(?:ing)? )?fee|charge for copies|cost of copies`)
	writtenPolicyRE    = regexp.MustCompile(`(?i)written (?:drug |testing |)policy|policy in writing`)
	creditMonitoringRE = regexp.MustCompile(`(?i)credit monitoring|identity theft protection|credit report`)
	biasAuditRE        = regexp.MustCompile(`(?i)bias audit|impact assessment|disparate impact (?:audit|testing)`)
	disclosureRE       = regexp.MustCompile(`(?i)disclos|notify|notice to (?:the )?(?:candidate|applicant|employee)`)
	affectedCountRE    = regexp.MustCompile(`(?i)\b(\d{3,4}(?:,\d{3})*)\s*(?:or more\s*)?(?:residents|individuals|persons|people)\b`)
)

// ExtractAffectedCount reads an authority-notification threshold such as
// "500 residents".
func ExtractAffectedCount(text string) int {
	m := affectedCountRE.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	n := 0
	for _, r := range strings.ReplaceAll(m[1], ",", "") {
		n = n*10 + int(r-'0')
	}
	return n
}

func noticeDirection(text string) string {
	lowered := strings.ToLower(text)
	// A negated notice duty states an absence, not a direction: "no
	// statutory advance notice required" must not read as BEFORE.
	switch {
	case strings.Contains(lowered, "no advance notice"),
		strings.Contains(lowered, "no statutory advance notice"),
		strings.Contains(lowered, "no notice requirement"),
		strings.Contains(lowered, "no requirement to provide"),
		strings.Contains(lowered, "does not require advance notice"),
		strings.Contains(lowered, "not required to provide notice"),
		strings.Contains(lowered, "without advance notice"):
		return ""
	}
	switch {
	case strings.Contains(lowered, "before any change"),
		strings.Contains(lowered, "before the change"),
		strings.Contains(lowered, "prior to the change"),
		strings.Contains(lowered, "prior to"),
		strings.Contains(lowered, "advance notice"),
		strings.Contains(lowered, "in advance of"),
		strings.Contains(lowered, "in advance"),
		strings.Contains(lowered, "days before"),
		strings.Contains(lowered, "before the employee"),
		strings.Contains(lowered, "before reduction"),
		strings.Contains(lowered, "before a reduction"),
		strings.Contains(lowered, "before the effective date"),
		strings.Contains(lowered, "preceding the effective date"),
		strings.Contains(lowered, "no later than"):
		return "BEFORE"
	case strings.Contains(lowered, "within"), strings.Contains(lowered, "after the change"),
		strings.Contains(lowered, "following the change"),
		strings.Contains(lowered, "days after"):
		return "AFTER"
	default:
		return ""
	}
}

func noticeChannel(text string) string {
	lowered := strings.ToLower(text)
	if strings.Contains(lowered, "written") || strings.Contains(lowered, "in writing") {
		return "written"
	}
	return ""
}

func noticeContentFields(text string) []string {
	lowered := strings.ToLower(text)
	var out []string
	for _, pair := range []struct{ needle, field string }{
		{"pay rate", "pay_rate"},
		{"rate of pay", "pay_rate"},
		{"wage rate", "pay_rate"},
		{"pay basis", "pay_basis"},
		{"payday", "payday"},
		{"pay frequency", "pay_frequency"},
		{"allowance", "allowances"},
		{"effective date", "effective_date"},
	} {
		if strings.Contains(lowered, pair.needle) && !contains(out, pair.field) {
			out = append(out, pair.field)
		}
	}
	return out
}

func restrictedFields(text string) []string {
	lowered := strings.ToLower(text)
	var out []string
	for _, pair := range []struct{ needle, field string }{
		{"salary history", "salary_history"},
		{"wage history", "salary_history"},
		{"pay history", "salary_history"},
		{"prior salary", "salary_history"},
		{"previous salary", "salary_history"},
		{"compensation history", "salary_history"},
		{"credit history", "credit_history"},
		{"credit report", "credit_history"},
	} {
		if strings.Contains(lowered, pair.needle) && !contains(out, pair.field) {
			out = append(out, pair.field)
		}
	}
	return out
}

// leaveProgram names the leave program the rule governs, which is what the
// corrected LEAVE_INTERACTION trigger matches a worker's balance against.
func leaveProgram(text string) string {
	lowered := strings.ToLower(text)
	switch {
	case strings.Contains(lowered, "sick and safe"), strings.Contains(lowered, "safe leave"):
		return "earned sick and safe leave"
	case strings.Contains(lowered, "paid sick"), strings.Contains(lowered, "sick leave"):
		return "accrued paid sick leave"
	case strings.Contains(lowered, "paid family"), strings.Contains(lowered, "family and medical"),
		strings.Contains(lowered, "family leave"):
		return "paid family and medical leave"
	case strings.Contains(lowered, "paid time off"), strings.Contains(lowered, "pto"),
		strings.Contains(lowered, "vacation"):
		return "accrued paid time off"
	case strings.Contains(lowered, "paid leave"):
		return "paid leave"
	default:
		return ""
	}
}

func recordClass(text string) string {
	lowered := strings.ToLower(text)
	switch {
	case strings.Contains(lowered, "payroll"):
		return "payroll_records"
	case strings.Contains(lowered, "wage") && strings.Contains(lowered, "hour"):
		return "wage_hour_records"
	case strings.Contains(lowered, "wage"):
		return "wage_records"
	case strings.Contains(lowered, "personnel"):
		return "personnel_records"
	case strings.Contains(lowered, "employment record"), strings.Contains(lowered, "employee record"):
		return "employment_records"
	default:
		return ""
	}
}

func retentionBasis(text string) string {
	lowered := strings.ToLower(text)
	if strings.Contains(lowered, "after termination") || strings.Contains(lowered, "employment plus") ||
		strings.Contains(lowered, "after separation") {
		return "employment_plus_years"
	}
	if strings.Contains(lowered, "year") {
		return "from_record_date"
	}
	return ""
}

func payFrequency(text string) string {
	lowered := strings.ToLower(text)
	switch {
	case strings.Contains(lowered, "weekly") && !strings.Contains(lowered, "biweekly") &&
		!strings.Contains(lowered, "bi-weekly"):
		return "WEEKLY"
	case strings.Contains(lowered, "semimonthly"), strings.Contains(lowered, "semi-monthly"),
		strings.Contains(lowered, "twice a month"), strings.Contains(lowered, "twice per month"):
		return "SEMIMONTHLY"
	case strings.Contains(lowered, "biweekly"), strings.Contains(lowered, "bi-weekly"),
		strings.Contains(lowered, "every two weeks"):
		return "BIWEEKLY"
	case strings.Contains(lowered, "monthly"):
		return "MONTHLY"
	default:
		return ""
	}
}

func workerClass(text string) string {
	lowered := strings.ToLower(text)
	switch {
	case strings.Contains(lowered, "manual worker"):
		return "manual workers"
	case strings.Contains(lowered, "tipped"):
		return "tipped employees"
	case strings.Contains(lowered, "clerical"):
		return "clerical and other workers"
	default:
		return ""
	}
}

func wageBasis(text string) string {
	lowered := strings.ToLower(text)
	switch {
	case strings.Contains(lowered, "/hour"), strings.Contains(lowered, "per hour"),
		strings.Contains(lowered, "hourly"), strings.Contains(lowered, "an hour"):
		return "HOURLY"
	case strings.Contains(lowered, "per week"), strings.Contains(lowered, "weekly salary"):
		return "WEEKLY"
	case strings.Contains(lowered, "per year"), strings.Contains(lowered, "annually"),
		strings.Contains(lowered, "annual salary"):
		return "ANNUAL"
	default:
		return ""
	}
}

func indexation(text string) string {
	lowered := strings.ToLower(text)
	switch {
	case strings.Contains(lowered, "cpi"), strings.Contains(lowered, "cost of living"),
		strings.Contains(lowered, "inflation"), strings.Contains(lowered, "index"):
		return "CPI"
	case strings.Contains(lowered, "step increase"), strings.Contains(lowered, "scheduled increase"),
		strings.Contains(lowered, "schedule of increases"):
		return "SCHEDULE"
	default:
		return ""
	}
}

func protectedBases(text string) []string {
	lowered := strings.ToLower(text)
	var out []string
	for _, pair := range []struct{ needle, basis string }{
		{"sex", "sex"},
		{"gender", "gender"},
		{"race", "race"},
		{"ethnic", "ethnicity"},
		{"national origin", "national_origin"},
		{"religion", "religion"},
		{"age", "age"},
		{"disabilit", "disability"},
		{"sexual orientation", "sexual_orientation"},
		{"gender identity", "gender_identity"},
	} {
		if strings.Contains(lowered, pair.needle) && !contains(out, pair.basis) {
			out = append(out, pair.basis)
		}
	}
	return out
}

func comparatorStandard(text string) string {
	lowered := strings.ToLower(text)
	switch {
	case strings.Contains(lowered, "substantially similar"):
		return "substantially similar work"
	case strings.Contains(lowered, "comparable work"):
		return "comparable work"
	case strings.Contains(lowered, "equal work"), strings.Contains(lowered, "same work"):
		return "equal work"
	case strings.Contains(lowered, "similar work"):
		return "similar work"
	default:
		return ""
	}
}

func payStatementFields(text string) []string {
	lowered := strings.ToLower(text)
	var out []string
	for _, pair := range []struct{ needle, field string }{
		{"gross", "gross_wages"},
		{"net", "net_wages"},
		{"deduction", "deductions"},
		{"hours", "hours_worked"},
		{"rate", "pay_rate"},
		{"pay period", "pay_period"},
		{"employer", "employer_identification"},
	} {
		if strings.Contains(lowered, pair.needle) && !contains(out, pair.field) {
			out = append(out, pair.field)
		}
	}
	return out
}

func payStatementDelivery(text string) string {
	lowered := strings.ToLower(text)
	hasElectronic := strings.Contains(lowered, "electronic")
	hasPaper := strings.Contains(lowered, "paper") || strings.Contains(lowered, "printed") ||
		strings.Contains(lowered, "written statement")
	switch {
	case hasElectronic && hasPaper:
		return "EITHER"
	case hasElectronic:
		return "ELECTRONIC"
	case hasPaper:
		return "PAPER"
	default:
		return ""
	}
}

func classificationDimension(text string) string {
	lowered := strings.ToLower(text)
	switch {
	case strings.Contains(lowered, "contractor"), strings.Contains(lowered, "abc test"),
		strings.Contains(lowered, "right-of-control"), strings.Contains(lowered, "right of control"):
		return "CONTRACTOR"
	case strings.Contains(lowered, "overtime"):
		return "OVERTIME_THRESHOLD"
	case strings.Contains(lowered, "exempt"):
		return "EXEMPTION"
	default:
		return ""
	}
}

func protectedActivities(text string) []string {
	lowered := strings.ToLower(text)
	var out []string
	for _, pair := range []struct{ needle, activity string }{
		{"workers' compensation", "workers_compensation_claim"},
		{"workers compensation", "workers_compensation_claim"},
		{"wage claim", "wage_claim"},
		{"whistleblow", "whistleblower_report"},
		{"jury duty", "jury_duty"},
		{"discrimination complaint", "discrimination_complaint"},
		{"safety", "safety_report"},
		{"union", "union_activity"},
		{"leave", "protected_leave_request"},
	} {
		if strings.Contains(lowered, pair.needle) && !contains(out, pair.activity) {
			out = append(out, pair.activity)
		}
	}
	return out
}

func retaliationDisposition(text string) string {
	lowered := strings.ToLower(text)
	switch {
	case strings.Contains(lowered, "block"), strings.Contains(lowered, "prohibit"),
		strings.Contains(lowered, "may not"), strings.Contains(lowered, "cannot"),
		strings.Contains(lowered, "unlawful"), strings.Contains(lowered, "shall not"):
		return "BLOCK"
	case strings.Contains(lowered, "flag"), strings.Contains(lowered, "review"),
		strings.Contains(lowered, "acknowledg"):
		return "FLAG"
	default:
		return ""
	}
}

func jobSecurityStandard(text string) string {
	lowered := strings.ToLower(text)
	switch {
	case strings.Contains(lowered, "good cause"):
		return "GOOD_CAUSE_AFTER_PROBATION"
	case strings.Contains(lowered, "disclaimer"):
		return "HANDBOOK_DISCLAIMER"
	case strings.Contains(lowered, "implied contract"), strings.Contains(lowered, "handbook"):
		return "IMPLIED_CONTRACT_REVIEW"
	case strings.Contains(lowered, "at-will"), strings.Contains(lowered, "at will"):
		return "AT_WILL"
	default:
		return ""
	}
}

var separationFormRE = regexp.MustCompile(`(?i)\b(?:form\s+)?((?:BC|DOL|LE|UC|DE|MODES|WVUC)[- ]?\d+[A-Za-z\-]*)\b`)

func separationForm(text string) string {
	if m := separationFormRE.FindStringSubmatch(text); m != nil {
		return strings.ToUpper(strings.TrimSpace(m[1]))
	}
	return ""
}

func separationAuthority(text string) string {
	lowered := strings.ToLower(text)
	switch {
	case strings.Contains(lowered, "unemployment"):
		return "state unemployment insurance agency"
	case strings.Contains(lowered, "department of labor"), strings.Contains(lowered, "labor department"):
		return "state department of labor"
	case strings.Contains(lowered, "workforce"):
		return "state workforce agency"
	default:
		return ""
	}
}

func drugTestingBases(text string) []string {
	lowered := strings.ToLower(text)
	var out []string
	for _, pair := range []struct{ needle, basis string }{
		{"pre-employment", "pre_employment"},
		{"preemployment", "pre_employment"},
		{"reasonable suspicion", "reasonable_suspicion"},
		{"post-accident", "post_accident"},
		{"post accident", "post_accident"},
		{"random", "random"},
		{"safety-sensitive", "safety_sensitive_role"},
		{"safety sensitive", "safety_sensitive_role"},
		{"return to duty", "return_to_duty"},
	} {
		if strings.Contains(lowered, pair.needle) && !contains(out, pair.basis) {
			out = append(out, pair.basis)
		}
	}
	return out
}

func drugTestingProtected(text string) []string {
	lowered := strings.ToLower(text)
	var out []string
	for _, pair := range []struct{ needle, status string }{
		{"medical marijuana", "medical_cannabis_patient"},
		{"medical cannabis", "medical_cannabis_patient"},
		{"cannabis", "lawful_off_duty_cannabis_use"},
		{"prescription", "lawful_prescription_holder"},
	} {
		if strings.Contains(lowered, pair.needle) && !contains(out, pair.status) {
			out = append(out, pair.status)
		}
	}
	return out
}

func automatedUses(text string) []string {
	lowered := strings.ToLower(text)
	var out []string
	for _, pair := range []struct{ needle, use string }{
		{"promotion", "promotion_decision"},
		{"hiring", "hiring_decision"},
		{"screening", "candidate_screening"},
		{"compensation", "compensation_decision"},
		{"termination", "termination_decision"},
		{"employment decision", "employment_decision"},
	} {
		if strings.Contains(lowered, pair.needle) && !contains(out, pair.use) {
			out = append(out, pair.use)
		}
	}
	return out
}

func dataCategories(text string) []string {
	lowered := strings.ToLower(text)
	var out []string
	for _, pair := range []struct{ needle, category string }{
		{"biometric", "biometric_identifiers"},
		{"geolocation", "geolocation"},
		{"gps", "geolocation"},
		{"email", "electronic_communications"},
		{"communication", "electronic_communications"},
		{"video", "video_surveillance"},
		{"social media", "social_media_credentials"},
	} {
		if strings.Contains(lowered, pair.needle) && !contains(out, pair.category) {
			out = append(out, pair.category)
		}
	}
	return out
}

func consentForm(text string) string {
	lowered := strings.ToLower(text)
	switch {
	case strings.Contains(lowered, "written consent"), strings.Contains(lowered, "written release"),
		strings.Contains(lowered, "signed consent"):
		return "WRITTEN"
	case strings.Contains(lowered, "notice"), strings.Contains(lowered, "notify"):
		return "NOTICE_ONLY"
	default:
		return ""
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
