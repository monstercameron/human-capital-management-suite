package docquery

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Predicate operators. The vocabulary is closed: Eq and Range are the only
// operators the builder emits, which is what keeps every query a bounded
// indexed lookup instead of an arbitrary script.
const (
	opEq  = "$eq"
	opGte = "$gte"
	opLte = "$lte"
)

// allowedOperators is the closed operator set for defense in depth: the
// encoder refuses anything outside it even if a Predicate is built by hand.
var allowedOperators = map[string]bool{opEq: true, opGte: true, opLte: true}

// Predicate is one sealed field condition. Construct with Eq or Range;
// the zero value is invalid and the encoder rejects it.
type Predicate struct {
	field  string
	op     string
	lo     any
	hi     any
	range_ bool
}

// Eq builds an equality predicate: field == value. Value must be a JSON
// scalar (string, bool, or a number); objects, arrays, and null are
// rejected so no sub-document or operator object can be smuggled in.
func Eq(field string, value any) Predicate {
	return Predicate{field: field, op: opEq, lo: value}
}

// Range builds an inclusive range predicate: lo <= field <= hi. Either
// bound may be nil for an open-ended range, but not both. Bounds must be
// JSON scalars of the same JSON kind.
func Range(field string, lo, hi any) Predicate {
	return Predicate{field: field, range_: true, lo: lo, hi: hi}
}

// MaxLimit caps page sizes so a query can never ask a store for an
// unbounded result set.
const MaxLimit = 1000

// DefaultLimit applies when the caller sets no limit.
const DefaultLimit = 100

// Query is a sealed, validated query: the only shape Execute accepts.
type Query struct {
	typeName string
	preds    []Predicate
	limit    int
}

// Querier is the store seam queries execute against: the subset of the
// datasource connector surface the builder needs. Production wiring
// adapts datasource.Connector.Query; tests supply a fake.
type Querier interface {
	// Query runs predicate JSON against the named source and returns
	// raw result documents.
	Query(source string, predicate json.RawMessage, limit int) ([]json.RawMessage, error)
}

// QueryBuilder accumulates predicates for one document type. The zero
// value is unusable: start from Registry.Query.
type QueryBuilder struct {
	reg    *Registry
	typ    *DocumentType
	preds  []Predicate
	limit  int
	failed error
}

// Query starts a query over a registered document type.
func (r *Registry) Query(typeName string) *QueryBuilder {
	t, err := r.Lookup(typeName)
	if err != nil {
		return &QueryBuilder{failed: err}
	}
	return &QueryBuilder{reg: r, typ: t, limit: DefaultLimit}
}

// Where appends predicates. Each predicate's field must be declared
// Indexed on the document type; the first violation fails the builder and
// every later call is a no-op, so a bad predicate can never be half-added.
func (b *QueryBuilder) Where(preds ...Predicate) *QueryBuilder {
	if b.failed != nil {
		return b
	}
	for _, p := range preds {
		if err := checkPredicate(p); err != nil {
			b.failed = err
			return b
		}
		if !fieldNameRE.MatchString(p.field) {
			b.failed = fmt.Errorf("%w: field %q", ErrInvalidFieldName, p.field)
			return b
		}
		if !b.typ.indexed(p.field) {
			b.failed = fmt.Errorf("%w: %q on %q", ErrUnindexedField, p.field, b.typ.Name)
			return b
		}
		b.preds = append(b.preds, p)
	}
	return b
}

// Limit sets the page size in 1..MaxLimit.
func (b *QueryBuilder) Limit(n int) *QueryBuilder {
	if b.failed != nil {
		return b
	}
	if n < 1 || n > MaxLimit {
		b.failed = fmt.Errorf("%w: %d not in 1..%d", ErrInvalidLimit, n, MaxLimit)
		return b
	}
	b.limit = n
	return b
}

// Build seals the builder into an executable Query.
func (b *QueryBuilder) Build() (Query, error) {
	if b.failed != nil {
		return Query{}, b.failed
	}
	if b.typ == nil {
		return Query{}, fmt.Errorf("%w: builder has no document type", ErrInvalidPredicate)
	}
	preds := make([]Predicate, len(b.preds))
	copy(preds, b.preds)
	return Query{typeName: b.typ.Name, preds: preds, limit: b.limit}, nil
}

// checkPredicate validates one predicate's shape: known operator, scalar
// values, and (for ranges) same-kind bounds with at least one end set.
func checkPredicate(p Predicate) error {
	if p.field == "" {
		return fmt.Errorf("%w: empty field", ErrInvalidPredicate)
	}
	if p.range_ {
		if p.lo == nil && p.hi == nil {
			return fmt.Errorf("%w: range over %q has no bounds", ErrInvalidPredicate, p.field)
		}
		if p.lo != nil && !isScalar(p.lo) {
			return fmt.Errorf("%w: range lower bound over %q is %T, want scalar", ErrInvalidPredicate, p.field, p.lo)
		}
		if p.hi != nil && !isScalar(p.hi) {
			return fmt.Errorf("%w: range upper bound over %q is %T, want scalar", ErrInvalidPredicate, p.field, p.hi)
		}
		if p.lo != nil && p.hi != nil && scalarKind(p.lo) != scalarKind(p.hi) {
			return fmt.Errorf("%w: range bounds over %q mix %s and %s",
				ErrInvalidPredicate, p.field, scalarKind(p.lo), scalarKind(p.hi))
		}
		return nil
	}
	if p.op != opEq {
		return fmt.Errorf("%w: operator %q", ErrInvalidPredicate, p.op)
	}
	if !isScalar(p.lo) {
		return fmt.Errorf("%w: equality value over %q is %T, want scalar", ErrInvalidPredicate, p.field, p.lo)
	}
	return nil
}

// isScalar reports whether v is a JSON scalar: string, bool, or number.
// Sized ints, uints, and float64 encode as JSON numbers; everything else
// (objects, arrays, nil, time.Time) is rejected so predicates can never
// carry sub-documents or operator objects.
func isScalar(v any) bool {
	switch t := v.(type) {
	case string, bool, float64,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return true
	case json.Number:
		// encoding/json splices json.Number bytes verbatim, so the
		// token must itself be valid JSON: "NaN" or "1},..." would
		// otherwise break out of the predicate.
		return json.Valid([]byte(t.String()))
	default:
		return false
	}
}

// scalarKind names the JSON kind for same-kind range checks.
func scalarKind(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "bool"
	default:
		return "number"
	}
}

// Encode renders the query predicate as canonical JSON: keys sorted, only
// $eq/$gte/$lte operators. Malformed hand-built predicates are an error,
// never silently rewritten.
func (q Query) Encode() (json.RawMessage, error) {
	fields := make([]string, 0, len(q.preds))
	seen := map[string][]Predicate{}
	for _, p := range q.preds {
		if err := checkPredicate(p); err != nil {
			return nil, err
		}
		if _, ok := seen[p.field]; !ok {
			fields = append(fields, p.field)
		}
		seen[p.field] = append(seen[p.field], p)
	}
	sort.Strings(fields)
	var sb strings.Builder
	sb.WriteByte('{')
	for i, f := range fields {
		if i > 0 {
			sb.WriteByte(',')
		}
		fkey, err := json.Marshal(f)
		if err != nil {
			return nil, fmt.Errorf("%w: field %q: %v", ErrInvalidPredicate, f, err)
		}
		sb.Write(fkey)
		sb.WriteByte(':')
		cond, err := encodeField(seen[f])
		if err != nil {
			return nil, err
		}
		sb.WriteString(cond)
	}
	sb.WriteByte('}')
	return json.RawMessage(sb.String()), nil
}

// encodeField renders one field's condition object with operators in
// fixed $eq/$gte/$lte order.
func encodeField(preds []Predicate) (string, error) {
	ops := map[string]any{}
	for _, p := range preds {
		if p.range_ {
			if p.lo != nil {
				if _, dup := ops[opGte]; dup {
					return "", fmt.Errorf("%w: duplicate lower bound over %q", ErrInvalidPredicate, p.field)
				}
				ops[opGte] = p.lo
			}
			if p.hi != nil {
				if _, dup := ops[opLte]; dup {
					return "", fmt.Errorf("%w: duplicate upper bound over %q", ErrInvalidPredicate, p.field)
				}
				ops[opLte] = p.hi
			}
			continue
		}
		if !allowedOperators[p.op] {
			return "", fmt.Errorf("%w: operator %q", ErrInvalidPredicate, p.op)
		}
		if _, dup := ops[p.op]; dup {
			return "", fmt.Errorf("%w: duplicate %s over %q", ErrInvalidPredicate, p.op, p.field)
		}
		ops[p.op] = p.lo
	}
	var sb strings.Builder
	sb.WriteByte('{')
	first := true
	for _, op := range []string{opEq, opGte, opLte} {
		v, ok := ops[op]
		if !ok {
			continue
		}
		if !first {
			sb.WriteByte(',')
		}
		first = false
		sb.WriteString(`"` + op + `":`)
		raw, err := json.Marshal(v)
		if err != nil {
			return "", fmt.Errorf("%w: value: %v", ErrInvalidPredicate, err)
		}
		sb.Write(raw)
	}
	sb.WriteByte('}')
	return sb.String(), nil
}

// Execute validates each result document against the registered type on
// the way out: rows that drifted from the schema quarantine with a reason
// instead of reaching the caller, and only validated documents return.
func (q Query) Execute(reg *Registry, store Querier) ([]ValidatedDocument, error) {
	t, err := reg.Lookup(q.typeName)
	if err != nil {
		return nil, err
	}
	pred, err := q.Encode()
	if err != nil {
		return nil, err
	}
	source := DefaultSource
	if t.Classification == ClassificationRestricted {
		source = VaultSource
	}
	rows, err := store.Query(source, pred, q.limit)
	if err != nil {
		return nil, err
	}
	out := make([]ValidatedDocument, 0, len(rows))
	for _, row := range rows {
		doc, quarantined, err := reg.ValidateDocument(q.typeName, row)
		if err != nil {
			return nil, err
		}
		if quarantined != nil {
			// Quarantined rows stay in the ledger; the caller
			// sees only documents that still match the schema.
			continue
		}
		out = append(out, doc)
	}
	return out, nil
}
