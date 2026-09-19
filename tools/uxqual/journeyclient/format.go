package journeyclient

import (
	"math/big"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// Formatting is in its own file because it is the one part of the
// projection that has to be exactly right rather than merely reasonable:
// money that loses a digit, a percentage that rounds the wrong way or a
// date that reads as the wrong locale's ordering are all wrong in a way a reader
// cannot detect from the page. Every value the engine sends is a string
// (decimal pay) or a Protobuf timestamp; nothing is a float until it has
// already been rounded to a display string.

// Display layouts. Times are absolute and always UTC, because the reader of
// an audit surface needs to compare what they see with what the ledger says,
// and a browser-local rendering makes that comparison a mental time-zone
// conversion. Legacy English detail dates use a short month; other locales
// use the shared product formatter so their date order is not inferred here.
const (
	// One date vocabulary: an instant reads in the same date form as a
	// civil date, with its clock and zone after it, rather than switching to
	// a machine date beside a human one (UXLIVE-016).
	timeLayout = "2 Jan 2006, 15:04"
	dateLayout = "2 Jan 2006"
	isoDate    = "2006-01-02"
)

// protoTimestamp is the shape of a generated *timestamppb.Timestamp, taken
// as an interface so this package never imports google.golang.org/protobuf.
// The generated getters are nil-safe, so a nil timestamp arrives here as a
// non-nil interface holding a nil pointer and answers 0, which is exactly
// what [timeOf] treats as "unset".
type protoTimestamp interface {
	GetSeconds() int64
	GetNanos() int32
}

// timeOf converts a Protobuf timestamp, reporting whether it was set.
//
// The proto3 zero value and the Unix epoch are the same bytes, so they are
// the same answer here: an HCM record stamped 1 January 1970 is a bug
// upstream, and rendering "1 Jan 1970" would hide it behind a plausible
// date.
func timeOf(ts protoTimestamp) (time.Time, bool) {
	if ts == nil {
		return time.Time{}, false
	}
	sec, nsec := ts.GetSeconds(), ts.GetNanos()
	if sec == 0 && nsec == 0 {
		return time.Time{}, false
	}
	return time.Unix(sec, int64(nsec)).UTC(), true
}

// formatTime renders one instant, or "" when it is unset.
func formatTime(ts protoTimestamp) string {
	t, ok := timeOf(ts)
	if !ok {
		return ""
	}
	return t.UTC().Format(timeLayout) + " UTC"
}

func formatTimeLocale(locale string, ts protoTimestamp) string {
	copy := productui.ResolveProductLocale(locale)
	if copy.Resolved == productui.DefaultProductLocale {
		return formatTime(ts)
	}
	t, ok := timeOf(ts)
	if !ok {
		return ""
	}
	return copy.FormatDate(t) + ", " + t.Format("15:04") + " UTC"
}

// formatTimeOr renders one instant, or fallback when it is unset. The
// fallback is an em dash in tables, where a blank cell reads as a rendering
// failure rather than as an absent fact.
func formatTimeOr(ts protoTimestamp, fallback string) string {
	if s := formatTime(ts); s != "" {
		return s
	}
	return fallback
}

// formatDateOf renders the date half of one instant, or "" when unset.
func formatDateOf(ts protoTimestamp) string {
	t, ok := timeOf(ts)
	if !ok {
		return ""
	}
	return t.UTC().Format(dateLayout)
}

func formatDateOfLocale(locale string, ts protoTimestamp) string {
	if productui.ResolveProductLocale(locale).Resolved == productui.DefaultProductLocale {
		return formatDateOf(ts)
	}
	t, ok := timeOf(ts)
	if !ok {
		return ""
	}
	return productui.ResolveProductLocale(locale).FormatDate(t)
}

// formatDate renders an ISO-8601 (YYYY-MM-DD) date the way the page shows
// dates.
//
// An unparseable value is returned unchanged rather than blanked: the
// effective date is a governed fact, and showing the reader the raw string
// the engine sent is more honest than showing them nothing.
func formatDate(iso string) string {
	s := strings.TrimSpace(iso)
	if s == "" {
		return ""
	}
	t, err := time.Parse(isoDate, s)
	if err != nil {
		return s
	}
	return t.Format(dateLayout)
}

func formatDateLocale(locale, iso string) string {
	if productui.ResolveProductLocale(locale).Resolved == productui.DefaultProductLocale {
		return formatDate(iso)
	}
	s := strings.TrimSpace(iso)
	if s == "" {
		return ""
	}
	t, err := time.Parse(isoDate, s)
	if err != nil {
		return s
	}
	return productui.ResolveProductLocale(locale).FormatDate(t)
}

// decimalOf parses one of the engine's decimal strings exactly.
//
// big.Rat is used rather than float64 throughout: pay is money, and the
// difference between two float64 amounts is not the amount a person would
// compute from the same two numbers on paper.
func decimalOf(s string) (*big.Rat, bool) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return nil, false
	}
	r, ok := new(big.Rat).SetString(trimmed)
	if !ok {
		return nil, false
	}
	return r, true
}

// formatMoney renders a decimal amount with thousands separators and
// exactly two decimal places ("93000" and "93000.004" both become
// "93,000.00"). An unparseable value is returned unchanged, for the same
// reason [formatDate] does.
func formatMoney(amount string) string {
	r, ok := decimalOf(amount)
	if !ok {
		return strings.TrimSpace(amount)
	}
	return groupThousands(r.FloatString(2))
}

func formatMoneyLocale(locale, amount string) string {
	if productui.ResolveProductLocale(locale).Resolved == productui.DefaultProductLocale {
		return formatMoney(amount)
	}
	if _, ok := decimalOf(amount); !ok {
		return strings.TrimSpace(amount)
	}
	return productui.ResolveProductLocale(locale).FormatNumber(amount, 2)
}

// formatAmount renders a currency and an amount together ("USD 93,000.00").
func formatAmount(currency, amount string) string {
	money := formatMoney(amount)
	if money == "" {
		return ""
	}
	if c := strings.TrimSpace(currency); c != "" {
		return c + " " + money
	}
	return money
}

func formatAmountLocale(locale, currency, amount string) string {
	if productui.ResolveProductLocale(locale).Resolved == productui.DefaultProductLocale {
		return formatAmount(currency, amount)
	}
	if _, ok := decimalOf(amount); !ok {
		return strings.TrimSpace(amount)
	}
	return productui.ResolveProductLocale(locale).FormatMoney(amount, currency, 2)
}

// groupThousands inserts a comma every three digits of the integer part of
// an already-rounded decimal string.
func groupThousands(s string) string {
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	whole, frac, hasFrac := strings.Cut(s, ".")
	var b strings.Builder
	for i, digit := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(digit)
	}
	out := sign + b.String()
	if hasFrac {
		out += "." + frac
	}
	return out
}

// percentDelta renders the signed percentage change from one decimal amount
// to another ("+5.4%"), reporting false when there is no meaningful change
// to show: either amount unparseable, a zero baseline (every increase from
// nothing is infinite, which is not a number a page should print), or two
// amounts that round to the same tenth of a percent.
func percentDelta(from, to string) (string, bool) {
	a, okA := decimalOf(from)
	b, okB := decimalOf(to)
	if !okA || !okB || a.Sign() == 0 {
		return "", false
	}
	diff := new(big.Rat).Sub(b, a)
	if diff.Sign() == 0 {
		return "", false
	}
	pct := new(big.Rat).Quo(diff, a)
	pct.Mul(pct, big.NewRat(100, 1))
	s := pct.FloatString(1)
	if s == "0.0" || s == "-0.0" {
		return "", false
	}
	return withSign(s) + "%", true
}

// amountDelta renders the signed difference between two decimal amounts
// ("+USD 5,000.00"), reporting false when there is none to show.
func amountDelta(currency, from, to string) (string, bool) {
	a, okA := decimalOf(from)
	b, okB := decimalOf(to)
	if !okA || !okB {
		return "", false
	}
	diff := new(big.Rat).Sub(b, a)
	if diff.Sign() == 0 {
		return "", false
	}
	money := groupThousands(diff.FloatString(2))
	if c := strings.TrimSpace(currency); c != "" {
		return signedAmount(money, c), true
	}
	return withSign(money), true
}

func amountDeltaLocale(locale, currency, from, to string) (string, bool) {
	if productui.ResolveProductLocale(locale).Resolved == productui.DefaultProductLocale {
		return amountDelta(currency, from, to)
	}
	a, okA := decimalOf(from)
	b, okB := decimalOf(to)
	if !okA || !okB {
		return "", false
	}
	diff := new(big.Rat).Sub(b, a)
	if diff.Sign() == 0 {
		return "", false
	}
	abs := new(big.Rat).Abs(diff).FloatString(2)
	formatted := formatAmountLocale(locale, currency, abs)
	if diff.Sign() < 0 {
		return "-" + formatted, true
	}
	return "+" + formatted, true
}

func percentDeltaLocale(locale, from, to string) (string, bool) {
	pct, ok := percentDelta(from, to)
	if !ok || productui.ResolveProductLocale(locale).Resolved == productui.DefaultProductLocale {
		return pct, ok
	}
	decimal := strings.TrimSuffix(pct, "%")
	sign := ""
	if strings.HasPrefix(decimal, "+") || strings.HasPrefix(decimal, "-") {
		sign, decimal = decimal[:1], decimal[1:]
	}
	resolved := productui.ResolveProductLocale(locale)
	return sign + resolved.FormatNumber(decimal, 1) + resolved.PercentSign(), true
}

// signedAmount puts the sign in front of the currency code rather than in
// front of the digits ("+USD 5,000.00", not "USD +5,000.00"), which is how
// the comparison table reads a change.
func signedAmount(money, currency string) string {
	if strings.HasPrefix(money, "-") {
		return "-" + currency + " " + money[1:]
	}
	return "+" + currency + " " + money
}

// withSign prefixes a non-negative decimal string with "+". A negative one
// already carries its own sign.
func withSign(s string) string {
	if s == "" || strings.HasPrefix(s, "-") {
		return s
	}
	return "+" + s
}

// payLine is the list card's one-line pay change: "USD 93,000.00 →
// 98,000.00 (+5.4%)". The currency is stated once because both amounts are
// in it, and the percentage is dropped rather than printed as zero when
// there is no change.
func payLine(currency, current, proposed string) string {
	from := formatAmount(currency, current)
	to := formatMoney(proposed)
	switch {
	case from == "" && to == "":
		return ""
	case from == "":
		return formatAmount(currency, proposed)
	case to == "":
		return from
	}
	line := from + " → " + to
	if pct, ok := percentDelta(current, proposed); ok {
		line += " (" + pct + ")"
	}
	return line
}

func payLineLocale(locale, currency, current, proposed string) string {
	if productui.ResolveProductLocale(locale).Resolved == productui.DefaultProductLocale {
		return payLine(currency, current, proposed)
	}
	from := formatAmountLocale(locale, currency, current)
	to := formatMoneyLocale(locale, proposed)
	switch {
	case from == "" && to == "":
		return ""
	case from == "":
		return formatAmountLocale(locale, currency, proposed)
	case to == "":
		return from
	}
	line := from + " → " + to
	if pct, ok := percentDeltaLocale(locale, current, proposed); ok {
		line += " (" + pct + ")"
	}
	return line
}

// headline is the list card's placement change: "OPS-HRBP2 · P2 →
// OPS-HRBP3 · P3". Either side degrades to whichever half it has rather
// than printing a stray separator.
func headline(currentJob, currentGrade, targetJob, targetGrade string) string {
	from := joinPlacement(currentJob, currentGrade)
	to := joinPlacement(targetJob, targetGrade)
	switch {
	case from == "" && to == "":
		return ""
	case from == "":
		return to
	case to == "":
		return from
	}
	return from + " → " + to
}

func joinPlacement(job, grade string) string {
	job, grade = strings.TrimSpace(job), strings.TrimSpace(grade)
	switch {
	case job == "" && grade == "":
		return ""
	case job == "":
		return grade
	case grade == "":
		return job
	}
	return job + " · " + grade
}
