package workspace

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DefaultLocale is the presentation locale used when a caller does not name
// one. It is a display default only: it never selects a legal, policy, tax,
// payroll, or authorization context.
const DefaultLocale = "en-US"

// LocaleFallback records why a requested locale did not become the resolved
// presentation locale. The value is carried into the rendered document, so a
// caller can see an unsupported request was not silently reinterpreted.
type LocaleFallback string

const (
	LocaleFallbackNone        LocaleFallback = ""
	LocaleFallbackUnsupported LocaleFallback = "unsupported_locale"
)

// LocaleContext carries only presentation selection. In particular, it has
// no jurisdiction, country, legal regime, policy, payroll, or authorization
// authority. Those decisions remain in their dedicated trusted contexts.
type LocaleContext struct {
	Requested string
	Resolved  string
	Fallback  LocaleFallback
}

// TranslationDiagnostic reports a visible English fallback for a catalog key
// that has not yet been translated for the resolved presentation locale.
// Keeping this structured lets a caller log or test the gap without parsing
// an HTML string.
type TranslationDiagnostic struct {
	Key      string
	Locale   string
	Fallback string
}

// ErrLocaleValue is returned when a localized number, money value, or date is
// malformed, non-canonical, or ambiguous for the resolved locale.
var ErrLocaleValue = errors.New("workspace: invalid localized presentation value")

type localeSpec struct {
	decimal string
	group   string
	months  []string
}

var supportedLocaleSpecs = map[string]localeSpec{
	"en-US": {
		decimal: ".", group: ",",
		months: []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"},
	},
	"de-DE": {
		decimal: ",", group: ".",
		months: []string{"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"},
	},
	"ar": {
		decimal: ".", group: ",",
		months: []string{"يناير", "فبراير", "مارس", "أبريل", "مايو", "يونيو", "يوليو", "أغسطس", "سبتمبر", "أكتوبر", "نوفمبر", "ديسمبر"},
	},
}

// ResolveLocale accepts the small, reviewed pilot locale set. Unsupported
// tags resolve to DefaultLocale with an explicit fallback marker; the tag is
// never treated as a proxy for jurisdiction or any governed context.
func ResolveLocale(requested string) LocaleContext {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return LocaleContext{Resolved: DefaultLocale}
	}
	if _, ok := supportedLocaleSpecs[requested]; ok {
		return LocaleContext{Requested: requested, Resolved: requested}
	}
	return LocaleContext{
		Requested: requested,
		Resolved:  DefaultLocale,
		Fallback:  LocaleFallbackUnsupported,
	}
}

// SupportedLocales returns the reviewed pilot locale tags in stable order.
func SupportedLocales() []string {
	return []string{"en-US", "de-DE", "ar"}
}

func (c LocaleContext) spec() (localeSpec, error) {
	s, ok := supportedLocaleSpecs[c.Resolved]
	if !ok {
		return localeSpec{}, fmt.Errorf("%w: unsupported locale %q", ErrLocaleValue, c.Resolved)
	}
	return s, nil
}

// FormatNumber formats a canonical decimal for presentation. Canonical input
// is deliberately separate from localized input: a canonical value is the
// only value the workspace uses for simulation or intent identity.
func FormatNumber(c LocaleContext, canonical string) (string, error) {
	s, err := c.spec()
	if err != nil {
		return "", err
	}
	negative, whole, fraction, err := canonicalNumber(canonical)
	if err != nil {
		return "", err
	}
	return sign(negative) + grouped(whole, s.group) + decimalSuffix(fraction, s.decimal), nil
}

// ParseNumber parses only the unambiguous grammar emitted by FormatNumber for
// this locale. Cross-locale separators, malformed grouping, whitespace, and
// competing decimal conventions are rejected rather than guessed.
func ParseNumber(c LocaleContext, presentation string) (string, error) {
	s, err := c.spec()
	if err != nil {
		return "", err
	}
	if presentation == "" || strings.TrimSpace(presentation) != presentation {
		return "", fmt.Errorf("%w: number is empty or padded", ErrLocaleValue)
	}
	negative := strings.HasPrefix(presentation, "-")
	if negative {
		presentation = strings.TrimPrefix(presentation, "-")
	}
	if presentation == "" || strings.HasPrefix(presentation, "+") || strings.Contains(presentation, "-") {
		return "", fmt.Errorf("%w: malformed sign", ErrLocaleValue)
	}
	if strings.Count(presentation, s.decimal) > 1 {
		return "", fmt.Errorf("%w: multiple decimal separators", ErrLocaleValue)
	}
	parts := strings.SplitN(presentation, s.decimal, 2)
	whole := parts[0]
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
		if fraction == "" || !asciiDigits(fraction) {
			return "", fmt.Errorf("%w: malformed fractional part", ErrLocaleValue)
		}
	}
	if err := validateGroupedWhole(whole, s.group); err != nil {
		return "", err
	}
	whole = strings.ReplaceAll(whole, s.group, "")
	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	return sign(negative) + whole + decimalSuffix(fraction, "."), nil
}

// FormatCurrency formats an amount and its explicit ISO currency code. The
// code is never inferred from locale, which avoids silent currency or legal
// jurisdiction selection.
func FormatCurrency(c LocaleContext, amount, currency string) (string, error) {
	if !validCurrency(currency) {
		return "", fmt.Errorf("%w: currency must be an ISO code", ErrLocaleValue)
	}
	number, err := FormatNumber(c, amount)
	if err != nil {
		return "", err
	}
	if c.Resolved == "de-DE" {
		return number + " " + currency, nil
	}
	return currency + " " + number, nil
}

// ParseCurrency parses the exact locale-specific representation emitted by
// FormatCurrency. The caller must provide the already-governed currency code;
// locale can choose punctuation, never monetary authority.
func ParseCurrency(c LocaleContext, presentation, currency string) (string, error) {
	if !validCurrency(currency) {
		return "", fmt.Errorf("%w: currency must be an ISO code", ErrLocaleValue)
	}
	var amount string
	if c.Resolved == "de-DE" {
		suffix := " " + currency
		if !strings.HasSuffix(presentation, suffix) || strings.Count(presentation, " ") != 1 {
			return "", fmt.Errorf("%w: currency layout does not match %s", ErrLocaleValue, c.Resolved)
		}
		amount = strings.TrimSuffix(presentation, suffix)
	} else {
		prefix := currency + " "
		if !strings.HasPrefix(presentation, prefix) || strings.Count(presentation, " ") != 1 {
			return "", fmt.Errorf("%w: currency layout does not match %s", ErrLocaleValue, c.Resolved)
		}
		amount = strings.TrimPrefix(presentation, prefix)
	}
	return ParseNumber(c, amount)
}

// FormatDate converts canonical YYYY-MM-DD into an unambiguous localized
// label with a named month. Date controls keep their canonical machine value,
// while this function supplies the adjacent human presentation.
func FormatDate(c LocaleContext, canonical string) (string, error) {
	s, err := c.spec()
	if err != nil {
		return "", err
	}
	date, err := time.Parse("2006-01-02", canonical)
	if err != nil || date.Format("2006-01-02") != canonical {
		return "", fmt.Errorf("%w: date must be canonical YYYY-MM-DD", ErrLocaleValue)
	}
	month := s.months[int(date.Month())-1]
	if c.Resolved == "de-DE" || c.Resolved == "ar" {
		if c.Resolved == "ar" {
			return fmt.Sprintf("%02d %s %04d", date.Day(), month, date.Year()), nil
		}
		return fmt.Sprintf("%02d. %s %04d", date.Day(), month, date.Year()), nil
	}
	return fmt.Sprintf("%s %02d, %04d", month, date.Day(), date.Year()), nil
}

// ParseDate parses only the named-month format emitted by FormatDate. That
// grammar deliberately refuses numeric day/month strings whose meaning could
// depend on a reader's convention.
func ParseDate(c LocaleContext, presentation string) (string, error) {
	s, err := c.spec()
	if err != nil {
		return "", err
	}
	if presentation == "" || strings.TrimSpace(presentation) != presentation {
		return "", fmt.Errorf("%w: date is empty or padded", ErrLocaleValue)
	}
	var dayText, monthText, yearText string
	if c.Resolved == "de-DE" || c.Resolved == "ar" {
		parts := strings.Split(presentation, " ")
		if len(parts) != 3 || (c.Resolved == "de-DE" && !strings.HasSuffix(parts[0], ".")) {
			return "", fmt.Errorf("%w: date layout does not match %s", ErrLocaleValue, c.Resolved)
		}
		dayText, monthText, yearText = strings.TrimSuffix(parts[0], "."), parts[1], parts[2]
	} else {
		parts := strings.Split(presentation, " ")
		if len(parts) != 3 || !strings.HasSuffix(parts[1], ",") {
			return "", fmt.Errorf("%w: date layout does not match %s", ErrLocaleValue, c.Resolved)
		}
		monthText, dayText, yearText = parts[0], strings.TrimSuffix(parts[1], ","), parts[2]
	}
	if len(dayText) != 2 || len(yearText) != 4 || !asciiDigits(dayText) || !asciiDigits(yearText) {
		return "", fmt.Errorf("%w: date components are not fixed-width digits", ErrLocaleValue)
	}
	month := 0
	for i, candidate := range s.months {
		if monthText == candidate {
			month = i + 1
			break
		}
	}
	if month == 0 {
		return "", fmt.Errorf("%w: unknown month %q", ErrLocaleValue, monthText)
	}
	day, _ := strconv.Atoi(dayText)
	year, _ := strconv.Atoi(yearText)
	date := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	canonical := date.Format("2006-01-02")
	again, err := FormatDate(c, canonical)
	if err != nil || again != presentation {
		return "", fmt.Errorf("%w: calendar date is invalid", ErrLocaleValue)
	}
	return canonical, nil
}

// Translate returns the localized catalog value or a visible, structured
// missing-translation report. It never returns an empty string for an
// untranslated key, because silently hiding text would make the gap invisible
// to both the person using the workspace and operating diagnostics.
func Translate(c LocaleContext, key, english string) (string, *TranslationDiagnostic) {
	if localized, ok := localeTranslations[c.Resolved][key]; ok {
		return localized, nil
	}
	diagnostic := &TranslationDiagnostic{Key: key, Locale: c.Resolved, Fallback: english}
	return "[missing translation: " + key + "] " + english, diagnostic
}

var localeTranslations = map[string]map[string]string{
	"en-US": {
		"title.promotion":               "Promotion",
		"field.worker":                  "Worker",
		"field.current_job":             "Current job code",
		"field.current_grade":           "Current grade",
		"field.current_base":            "Current base pay",
		"field.proposed_job":            "Proposed job code",
		"field.proposed_grade":          "Proposed grade",
		"field.target_position":         "Target position",
		"field.target_org_unit":         "Target organization",
		"field.proposed_base":           "Proposed base pay",
		"field.effective_date":          "Effective date",
		"field.business_reason":         "Business reason",
		"field.band_position":           "Pay band position",
		"field.annualized_increase":     "Annualized increase",
		"field.compensation_disclosure": "Compensation disclosure",
		"action.run_simulation":         "Run simulation",
		"action.submit_for_approval":    "Submit for approval",
		"action.force_execute":          "Force execute",
	},
	"de-DE": {
		"title.promotion":               "Beförderung",
		"field.worker":                  "Mitarbeitende Person",
		"field.current_job":             "Aktueller Jobcode",
		"field.current_grade":           "Aktuelle Vergütungsstufe",
		"field.current_base":            "Aktuelles Grundgehalt",
		"field.proposed_job":            "Vorgeschlagener Jobcode",
		"field.proposed_grade":          "Vorgeschlagene Vergütungsstufe",
		"field.target_position":         "Zielposition",
		"field.target_org_unit":         "Zielorganisation",
		"field.proposed_base":           "Vorgeschlagenes Grundgehalt",
		"field.effective_date":          "Wirksamkeitsdatum",
		"field.business_reason":         "Geschäftsgrund",
		"field.band_position":           "Position im Gehaltsband",
		"field.annualized_increase":     "Hochgerechnete Erhöhung",
		"field.compensation_disclosure": "Vergütungsoffenlegung",
		"action.run_simulation":         "Simulation ausführen",
		"action.submit_for_approval":    "Zur Genehmigung einreichen",
		"action.force_execute":          "Ausführung erzwingen",
	},
	"ar": {
		"title.promotion":               "ترقية",
		"field.worker":                  "الموظف",
		"field.current_job":             "رمز الوظيفة الحالي",
		"field.current_grade":           "الدرجة الحالية",
		"field.current_base":            "الأجر الأساسي الحالي",
		"field.proposed_job":            "رمز الوظيفة المقترح",
		"field.proposed_grade":          "الدرجة المقترحة",
		"field.target_position":         "المنصب المستهدف",
		"field.target_org_unit":         "المؤسسة المستهدفة",
		"field.proposed_base":           "الأجر الأساسي المقترح",
		"field.effective_date":          "تاريخ السريان",
		"field.business_reason":         "مبرر العمل",
		"field.band_position":           "الموضع ضمن نطاق الأجور",
		"field.annualized_increase":     "الزيادة السنوية",
		"field.compensation_disclosure": "الإفصاح عن التعويضات",
		"action.run_simulation":         "تشغيل المحاكاة",
		"action.submit_for_approval":    "إرسال للموافقة",
		"action.force_execute":          "فرض التنفيذ",
	},
}

func canonicalNumber(value string) (negative bool, whole, fraction string, err error) {
	if value == "" || strings.TrimSpace(value) != value {
		return false, "", "", fmt.Errorf("%w: canonical number is empty or padded", ErrLocaleValue)
	}
	negative = strings.HasPrefix(value, "-")
	if negative {
		value = strings.TrimPrefix(value, "-")
	}
	if value == "" || strings.HasPrefix(value, "+") || strings.Contains(value, "-") || strings.Count(value, ".") > 1 {
		return false, "", "", fmt.Errorf("%w: malformed canonical number", ErrLocaleValue)
	}
	parts := strings.SplitN(value, ".", 2)
	whole = parts[0]
	if whole == "" || !asciiDigits(whole) {
		return false, "", "", fmt.Errorf("%w: malformed canonical whole number", ErrLocaleValue)
	}
	if len(parts) == 2 {
		fraction = parts[1]
		if fraction == "" || !asciiDigits(fraction) {
			return false, "", "", fmt.Errorf("%w: malformed canonical fraction", ErrLocaleValue)
		}
	}
	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	return negative, whole, fraction, nil
}

func validateGroupedWhole(value, group string) error {
	if value == "" {
		return fmt.Errorf("%w: missing whole number", ErrLocaleValue)
	}
	parts := strings.Split(value, group)
	if len(parts) == 1 {
		if !asciiDigits(value) {
			return fmt.Errorf("%w: non-digit whole number", ErrLocaleValue)
		}
		return nil
	}
	if len(parts[0]) < 1 || len(parts[0]) > 3 || !asciiDigits(parts[0]) {
		return fmt.Errorf("%w: malformed leading group", ErrLocaleValue)
	}
	for _, part := range parts[1:] {
		if len(part) != 3 || !asciiDigits(part) {
			return fmt.Errorf("%w: malformed digit group", ErrLocaleValue)
		}
	}
	return nil
}

func grouped(whole, group string) string {
	first := len(whole) % 3
	if first == 0 {
		first = 3
	}
	var out strings.Builder
	out.WriteString(whole[:first])
	for i := first; i < len(whole); i += 3 {
		out.WriteString(group)
		out.WriteString(whole[i : i+3])
	}
	return out.String()
}

func decimalSuffix(fraction, decimal string) string {
	if fraction == "" {
		return ""
	}
	return decimal + fraction
}

func sign(negative bool) string {
	if negative {
		return "-"
	}
	return ""
}

func asciiDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func validCurrency(value string) bool {
	if len(value) != 3 {
		return false
	}
	for _, r := range value {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}
