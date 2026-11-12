package importing

import (
	"errors"
	"fmt"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/importdraft"
	"math/big"
	"regexp"
	"strings"
	"time"
)

// MaxDistinctTracked bounds the per-column distinct-value set. A profile that
// tracked every distinct value of an unbounded-cardinality column (a free-text
// note, a GUID) would grow without limit; past the bound the profile still
// reports an exact null/blank count and an exact min/max, and marks the
// distinct count as a lower bound rather than fabricating a number.
const MaxDistinctTracked = 5000

// Profile errors. All are matchable with errors.Is.
var ErrProfileInvalid = errors.New("importing: profile input is invalid")

// ColumnKind is the closed set of types this package infers for a column. It
// widens as needed - a column of integers and decimals profiles as DECIMAL,
// never as two different kinds - and it never guesses a currency or a date
// layout: KindMoney and KindDate are chosen only when every non-blank value
// in the column agrees on one concrete format.
type ColumnKind uint8

// Column kinds.
const (
	// KindUnspecified is the zero value and is never a legal result.
	KindUnspecified ColumnKind = iota
	// KindEmpty means every value in the column is null or blank.
	KindEmpty
	// KindBoolean means every non-blank value is "true" or "false"
	// (case-insensitive).
	KindBoolean
	// KindInteger means every non-blank value is a plain signed integer.
	KindInteger
	// KindDecimal means every non-blank value is a plain signed integer or
	// fixed-point decimal, with at least one fractional value present.
	KindDecimal
	// KindMoney means every non-blank value is a signed decimal amount
	// carrying the same ISO 4217 currency token.
	KindMoney
	// KindDate means every non-blank value parses under the same one of the
	// fixed candidate date layouts.
	KindDate
	// KindIdentifier means every non-blank value is a canonical UUID, or
	// every non-blank value is a zero-padded numeric token (a form that
	// KindInteger would silently strip the leading zeros from).
	KindIdentifier
	// KindText is the fallback: a column whose values do not agree on any of
	// the more specific kinds above.
	KindText
)

var columnKindWire = map[ColumnKind]string{
	KindEmpty:      "EMPTY",
	KindBoolean:    "BOOLEAN",
	KindInteger:    "INTEGER",
	KindDecimal:    "DECIMAL",
	KindMoney:      "MONEY",
	KindDate:       "DATE",
	KindIdentifier: "IDENTIFIER",
	KindText:       "TEXT",
}

// String returns the stable wire token, or "KIND_UNSPECIFIED".
func (k ColumnKind) String() string {
	if s, ok := columnKindWire[k]; ok {
		return s
	}
	return "KIND_UNSPECIFIED"
}

// Valid reports whether k is a declared column kind.
func (k ColumnKind) Valid() bool { _, ok := columnKindWire[k]; return ok }

// dateLayouts is the fixed, ordered candidate list for date-format detection.
// A column is KindDate only when every non-blank value parses under the same
// entry of this list; the list is tried in this order so that detection is a
// pure function of the values, never of which layout happened to be tried
// last.
var dateLayouts = []string{
	"2006-01-02",
	time.RFC3339,
	"2006/01/02",
	"01/02/2006",
	"20060102",
}

var (
	booleanTrue  = map[string]bool{"true": true, "TRUE": true, "True": true}
	booleanFalse = map[string]bool{"false": true, "FALSE": true, "False": true}

	// integerPattern excludes a leading zero (other than the bare digit "0")
	// so that a zero-padded token such as "00042" - whose leading zeros an
	// integer parse would silently discard - is left for identifier
	// detection instead of being misread as the number 42.
	integerPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)
	decimalPattern = regexp.MustCompile(`^-?[0-9]+\.[0-9]+$`)
	uuidPattern    = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	zeroPadPattern = regexp.MustCompile(`^0[0-9]+$`)

	// moneyPattern accepts an optional leading or trailing ISO 4217 code (not
	// both), an optional leading $, an optional leading minus, digits with
	// optional comma grouping, and an optional two-or-more digit fraction.
	moneyPattern = regexp.MustCompile(
		`^(?:(?P<pre>[A-Z]{3})\s)?\$?(?P<sign>-)?(?P<int>[0-9]{1,3}(?:,[0-9]{3})*|[0-9]+)(?:\.(?P<frac>[0-9]+))?(?:\s(?P<post>[A-Z]{3}))?$`,
	)
)

// isBlank reports whether a cell counts as null/blank for profiling: empty
// after trimming ASCII and Unicode space.
func isBlank(cell string) bool { return strings.TrimSpace(cell) == "" }

// moneyMatch reports whether s structurally matches moneyPattern, and the
// ISO-4217-shaped currency token it carries, if any. token is "" when the
// value is a bare decimal amount with no embedded currency marker; ok is
// false when s does not match moneyPattern at all, or carries a token on both
// sides (ambiguous).
func moneyMatch(s string) (token string, ok bool) {
	m := moneyPattern.FindStringSubmatch(s)
	if m == nil {
		return "", false
	}
	names := moneyPattern.SubexpNames()
	var pre, post string
	for i, name := range names {
		switch name {
		case "pre":
			pre = m[i]
		case "post":
			post = m[i]
		}
	}
	if pre != "" && post != "" {
		return "", false
	}
	if pre != "" {
		return pre, true
	}
	return post, true
}

// moneyCurrency reports the explicit currency token carried by s, and whether
// one is present at all. Unlike moneyMatch, a bare decimal amount with no
// embedded currency marker reports ok=false: column-level MONEY detection
// requires every value to carry an explicit, agreeing token, so that a column
// of plain decimals is never silently reclassified as money.
func moneyCurrency(s string) (string, bool) {
	token, ok := moneyMatch(s)
	if !ok || token == "" {
		return "", false
	}
	return token, true
}

// moneyDecimalText normalizes a money value to a plain signed decimal string
// (currency token and comma grouping stripped), given that it already matched
// moneyPattern.
func moneyDecimalText(s string) string {
	m := moneyPattern.FindStringSubmatch(s)
	names := moneyPattern.SubexpNames()
	var sign, intPart, frac string
	for i, name := range names {
		switch name {
		case "sign":
			sign = m[i]
		case "int":
			intPart = m[i]
		case "frac":
			frac = m[i]
		}
	}
	intPart = strings.ReplaceAll(intPart, ",", "")
	if frac == "" {
		return sign + intPart
	}
	return sign + intPart + "." + frac
}

// ColumnProfile is the deterministic profile of one column.
type ColumnProfile struct {
	Name   string
	Kind   ColumnKind
	Format string // e.g. a date layout, "MONEY:USD", "IDENTIFIER:UUID"; empty when not applicable.

	NullCount  int
	ValueCount int

	DistinctCount   int
	DistinctBounded bool // true when DistinctCount is a lower bound, capped at MaxDistinctTracked.

	// Min and Max are the original cell text of the extreme values, compared
	// exactly under the column's kind (integer/decimal magnitude, parsed
	// date, or else raw byte order). Both are empty when ValueCount is 0.
	Min string
	Max string
}

// Validate reports whether the column profile is internally coherent.
func (c ColumnProfile) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("%w: column has no name", ErrProfileInvalid)
	}
	if !c.Kind.Valid() {
		return fmt.Errorf("%w: column %q has no kind", ErrProfileInvalid, c.Name)
	}
	if c.NullCount < 0 || c.ValueCount < 0 || c.DistinctCount < 0 {
		return fmt.Errorf("%w: column %q has a negative count", ErrProfileInvalid, c.Name)
	}
	return nil
}

func (c ColumnProfile) canonical(w *importdraft.Canon) {
	w.Str(c.Name).Str(c.Kind.String()).Str(c.Format).
		I64(int64(c.NullCount)).I64(int64(c.ValueCount)).
		I64(int64(c.DistinctCount)).Bool(c.DistinctBounded).
		Str(c.Min).Str(c.Max)
}

// Profile is the deterministic, digested profile of a staged batch.
type Profile struct {
	BatchDigest string
	RowCount    int
	Columns     []ColumnProfile // in the batch's header order.
	Digest      string
}

// Column returns the profile for a column by name.
func (p Profile) Column(name string) (ColumnProfile, bool) {
	for _, c := range p.Columns {
		if c.Name == name {
			return c, true
		}
	}
	return ColumnProfile{}, false
}

// columnAccumulator tracks the running state needed to profile one column.
// Values are tracked verbatim (not case-folded, not trimmed) so that Min/Max
// reproduce exactly what was staged.
type columnAccumulator struct {
	name       string
	nullCount  int
	values     []string // non-blank values, in row order; bounded by MaxRows on the batch already.
	distinct   map[string]struct{}
	overflowed bool
}

func newColumnAccumulator(name string) *columnAccumulator {
	return &columnAccumulator{name: name, distinct: make(map[string]struct{})}
}

func (a *columnAccumulator) add(cell string) {
	if isBlank(cell) {
		a.nullCount++
		return
	}
	a.values = append(a.values, cell)
	if a.overflowed {
		return
	}
	if _, ok := a.distinct[cell]; !ok {
		if len(a.distinct) >= MaxDistinctTracked {
			a.overflowed = true
			return
		}
		a.distinct[cell] = struct{}{}
	}
}

// classify infers the column's kind, format and exact min/max in one
// deterministic pass over the accumulated non-blank values.
func (a *columnAccumulator) classify() ColumnProfile {
	col := ColumnProfile{
		Name:            a.name,
		NullCount:       a.nullCount,
		ValueCount:      len(a.values),
		DistinctCount:   len(a.distinct),
		DistinctBounded: a.overflowed,
	}
	if len(a.values) == 0 {
		col.Kind = KindEmpty
		return col
	}

	switch {
	case allMatch(a.values, isBooleanLiteral):
		col.Kind = KindBoolean
		col.Min, col.Max = lexMinMax(a.values)
	case allMatch(a.values, integerPattern.MatchString):
		col.Kind = KindInteger
		col.Min, col.Max = bigIntMinMax(a.values)
	case allMatch(a.values, isIntegerOrDecimal) && anyMatch(a.values, decimalPattern.MatchString):
		col.Kind = KindDecimal
		col.Min, col.Max = bigRatMinMax(a.values)
	case moneyKind(a.values, &col):
		// col populated by moneyKind.
	case dateKind(a.values, &col):
		// col populated by dateKind.
	case allMatch(a.values, uuidPattern.MatchString):
		col.Kind = KindIdentifier
		col.Format = "IDENTIFIER:UUID"
		col.Min, col.Max = lexMinMax(a.values)
	case allMatch(a.values, zeroPadPattern.MatchString):
		col.Kind = KindIdentifier
		col.Format = "IDENTIFIER:ZERO_PADDED_NUMERIC"
		col.Min, col.Max = lexMinMax(a.values)
	default:
		col.Kind = KindText
		col.Min, col.Max = lexMinMax(a.values)
	}
	return col
}

func isBooleanLiteral(s string) bool { return booleanTrue[s] || booleanFalse[s] }
func isIntegerOrDecimal(s string) bool {
	return integerPattern.MatchString(s) || decimalPattern.MatchString(s)
}

func allMatch(values []string, pred func(string) bool) bool {
	for _, v := range values {
		if !pred(v) {
			return false
		}
	}
	return true
}

func anyMatch(values []string, pred func(string) bool) bool {
	for _, v := range values {
		if pred(v) {
			return true
		}
	}
	return false
}

func lexMinMax(values []string) (string, string) {
	min, max := values[0], values[0]
	for _, v := range values[1:] {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	return min, max
}

func bigIntMinMax(values []string) (string, string) {
	minText, maxText := values[0], values[0]
	minN := new(big.Int)
	minN.SetString(values[0], 10)
	maxN := new(big.Int).Set(minN)
	for _, v := range values[1:] {
		n := new(big.Int)
		n.SetString(v, 10)
		if n.Cmp(minN) < 0 {
			minN, minText = n, v
		}
		if n.Cmp(maxN) > 0 {
			maxN, maxText = n, v
		}
	}
	return minText, maxText
}

func bigRatMinMax(values []string) (string, string) {
	minText, maxText := values[0], values[0]
	minR := new(big.Rat)
	minR.SetString(values[0])
	maxR := new(big.Rat).Set(minR)
	for _, v := range values[1:] {
		r := new(big.Rat)
		r.SetString(v)
		if r.Cmp(minR) < 0 {
			minR, minText = r, v
		}
		if r.Cmp(maxR) > 0 {
			maxR, maxText = r, v
		}
	}
	return minText, maxText
}

// moneyKind reports whether every value is money under one shared currency
// token, and if so populates col.
func moneyKind(values []string, col *ColumnProfile) bool {
	currency := ""
	for i, v := range values {
		c, ok := moneyCurrency(v)
		if !ok || c == "" {
			return false
		}
		if i == 0 {
			currency = c
		} else if c != currency {
			return false
		}
	}
	col.Kind = KindMoney
	col.Format = "MONEY:" + currency

	minText, maxText := values[0], values[0]
	minR := new(big.Rat)
	minR.SetString(moneyDecimalText(values[0]))
	maxR := new(big.Rat).Set(minR)
	for _, v := range values[1:] {
		r := new(big.Rat)
		r.SetString(moneyDecimalText(v))
		if r.Cmp(minR) < 0 {
			minR, minText = r, v
		}
		if r.Cmp(maxR) > 0 {
			maxR, maxText = r, v
		}
	}
	col.Min, col.Max = minText, maxText
	return true
}

// dateKind reports whether every value parses under one shared layout from
// dateLayouts, tried in that fixed order, and if so populates col.
func dateKind(values []string, col *ColumnProfile) bool {
	for _, layout := range dateLayouts {
		allParse := true
		times := make([]time.Time, len(values))
		for i, v := range values {
			t, err := time.Parse(layout, v)
			if err != nil {
				allParse = false
				break
			}
			times[i] = t
		}
		if !allParse {
			continue
		}
		col.Kind = KindDate
		col.Format = layout
		minIdx, maxIdx := 0, 0
		for i := 1; i < len(times); i++ {
			if times[i].Before(times[minIdx]) {
				minIdx = i
			}
			if times[i].After(times[maxIdx]) {
				maxIdx = i
			}
		}
		col.Min, col.Max = values[minIdx], values[maxIdx]
		return true
	}
	return false
}

// ProfileBatch produces the deterministic profile of a staged batch. Two
// calls over the same batch always return byte-identical profiles: no map is
// iterated to build the output, and every classification rule is a total,
// order-independent function of the column's non-blank values.
func ProfileBatch(b Batch) (Profile, error) {
	if err := b.Validate(); err != nil {
		return Profile{}, err
	}
	header := b.Header()
	accs := make([]*columnAccumulator, len(header))
	for i, name := range header {
		accs[i] = newColumnAccumulator(name)
	}
	for _, row := range b.Rows() {
		cells := row.Cells()
		for i, cell := range cells {
			accs[i].add(cell)
		}
	}

	columns := make([]ColumnProfile, len(accs))
	for i, acc := range accs {
		columns[i] = acc.classify()
		if err := columns[i].Validate(); err != nil {
			return Profile{}, err
		}
	}

	p := Profile{
		BatchDigest: b.Digest(),
		RowCount:    b.RowCount(),
		Columns:     columns,
	}

	w := importdraft.NewCanon("hcmnext.dataops.importing.Profile", 1)
	w.Str(p.BatchDigest).I64(int64(p.RowCount)).U64(uint64(len(p.Columns)))
	for _, c := range p.Columns {
		c.canonical(w)
	}
	p.Digest = w.DigestHex()
	return p, nil
}
