package mongo

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/datasource"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// canonicalJSON renders one decoded row as a canonical-JSON document:
// object keys sort ascending (encoding/json guarantees this for maps),
// integers keep integer syntax, and BSON-only types use relaxed-ExtJSON
// marker objects that parseDocument/parsePredicate read back.
func canonicalJSON(row bson.M) (datasource.Document, error) {
	normalized, err := normalizeValue(row)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(normalized); err != nil {
		return nil, fmt.Errorf("%w: encoding row: %v", datasource.ErrInvalidQuery, err)
	}
	return datasource.Document(bytes.TrimRight(buf.Bytes(), "\n")), nil
}

// normalizeValue converts decoded BSON values into JSON-marshalable
// values. BSON types without a JSON spelling become single-key marker
// maps; types the adapter refuses to represent are an error, never a
// silent rewrite.
func normalizeValue(v any) (any, error) {
	switch t := v.(type) {
	case nil, string, bool:
		return t, nil
	case int32:
		return json.Number(strconv.FormatInt(int64(t), 10)), nil
	case int64:
		return json.Number(strconv.FormatInt(t, 10)), nil
	case float64:
		if isNonFinite(t) {
			return nil, fmt.Errorf("%w: non-finite double %v has no JSON spelling", datasource.ErrInvalidQuery, t)
		}
		return json.Number(strconv.FormatFloat(t, 'g', -1, 64)), nil
	case bson.M:
		return normalizeDoc(t)
	case map[string]any:
		return normalizeDoc(t)
	case bson.A:
		return normalizeArray(t)
	case []any:
		return normalizeArray(t)
	case primitive.ObjectID:
		return map[string]any{"$oid": t.Hex()}, nil
	case primitive.DateTime:
		return map[string]any{"$date": t.Time().UTC().Format(time.RFC3339Nano)}, nil
	case primitive.Decimal128:
		return map[string]any{"$numberDecimal": t.String()}, nil
	case primitive.Binary:
		return map[string]any{"$binary": map[string]any{
			"base64":  base64.StdEncoding.EncodeToString(t.Data),
			"subType": strings.ToUpper(hex.EncodeToString([]byte{t.Subtype})),
		}}, nil
	case primitive.Timestamp:
		return map[string]any{"$timestamp": map[string]any{
			"t": json.Number(strconv.FormatUint(uint64(t.T), 10)),
			"i": json.Number(strconv.FormatUint(uint64(t.I), 10)),
		}}, nil
	case primitive.MinKey:
		return map[string]any{"$minKey": json.Number("1")}, nil
	case primitive.MaxKey:
		return map[string]any{"$maxKey": json.Number("1")}, nil
	case primitive.Regex:
		return map[string]any{"$regularExpression": map[string]any{
			"pattern": t.Pattern,
			"options": t.Options,
		}}, nil
	default:
		return nil, fmt.Errorf("%w: unsupported BSON type %T", datasource.ErrInvalidQuery, v)
	}
}

func isNonFinite(f float64) bool {
	return math.IsNaN(f) || math.IsInf(f, 0)
}

func normalizeDoc(doc map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(doc))
	for k, v := range doc {
		nv, err := normalizeValue(v)
		if err != nil {
			return nil, err
		}
		out[k] = nv
	}
	return out, nil
}

func normalizeArray(arr []any) ([]any, error) {
	out := make([]any, len(arr))
	for i, v := range arr {
		nv, err := normalizeValue(v)
		if err != nil {
			return nil, err
		}
		out[i] = nv
	}
	return out, nil
}

// parsePredicate decodes a canonical-JSON predicate object into a native
// filter. Empty input matches every document.
func parsePredicate(raw string) (bson.M, error) {
	if strings.TrimSpace(raw) == "" {
		return bson.M{}, nil
	}
	decoded, err := decodeJSONObject(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", datasource.ErrInvalidQuery, err)
	}
	return denormalizeDoc(decoded, datasource.ErrInvalidQuery)
}

// parseDocument decodes one canonical-JSON write document into native
// BSON. Documents must be objects: mongo has no top-level arrays.
func parseDocument(raw datasource.Document) (bson.M, error) {
	decoded, err := decodeJSONObject(string(raw))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", datasource.ErrInvalidWrite, err)
	}
	return denormalizeDoc(decoded, datasource.ErrInvalidWrite)
}

// decodeJSONObject decodes with json.Number so integers survive the
// round trip exactly instead of degrading to float64.
func decodeJSONObject(raw string) (map[string]any, error) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("malformed JSON: %v", err)
	}
	doc, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("top level must be an object, got %T", v)
	}
	return doc, nil
}

// denormalizeDoc converts decoded JSON back to native BSON values. A
// single-key map whose key is a known marker becomes the typed value;
// every other map (including operator objects such as {"$gt": 5})
// recurses as an ordinary document. Malformed markers are an error: a
// literal single field that collides with a marker name is
// unrepresentable by design, so corruption can never pass silently.
func denormalizeDoc(doc map[string]any, sentinel error) (bson.M, error) {
	out := make(bson.M, len(doc))
	for k, v := range doc {
		dv, err := denormalizeValue(v, sentinel)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", k, err)
		}
		out[k] = dv
	}
	return out, nil
}

func denormalizeValue(v any, sentinel error) (any, error) {
	switch t := v.(type) {
	case nil, string, bool:
		return t, nil
	case json.Number:
		return denormalizeNumber(t, sentinel)
	case map[string]any:
		if len(t) == 1 {
			if dv, ok, err := denormalizeMarker(t, sentinel); ok || err != nil {
				return dv, err
			}
		}
		sub := make(map[string]any, len(t))
		for k, sv := range t {
			dv, err := denormalizeValue(sv, sentinel)
			if err != nil {
				return nil, fmt.Errorf("field %q: %w", k, err)
			}
			sub[k] = dv
		}
		return sub, nil
	case []any:
		out := make([]any, len(t))
		for i, ev := range t {
			dv, err := denormalizeValue(ev, sentinel)
			if err != nil {
				return nil, fmt.Errorf("index %d: %w", i, err)
			}
			out[i] = dv
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%w: unsupported JSON value %T", sentinel, v)
	}
}

func denormalizeNumber(n json.Number, sentinel error) (any, error) {
	if i, err := n.Int64(); err == nil && isIntegerSyntax(n.String()) {
		return i, nil
	}
	f, err := n.Float64()
	if err != nil {
		return nil, fmt.Errorf("%w: number %q is not representable", sentinel, n.String())
	}
	return f, nil
}

func isIntegerSyntax(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' || s[i] == 'e' || s[i] == 'E' {
			return false
		}
	}
	return true
}

// denormalizeMarker converts one known marker object. It reports
// ok=false for maps that are not markers; malformed markers are an
// error, never passed through as literals.
func denormalizeMarker(obj map[string]any, sentinel error) (dv any, ok bool, err error) {
	fail := func(format string, args ...any) (any, bool, error) {
		return nil, true, fmt.Errorf(("%w: " + format), append([]any{sentinel}, args...)...)
	}
	for key, val := range obj {
		switch key {
		case "$oid":
			s, isStr := val.(string)
			if !isStr {
				return fail("oid marker must wrap a string, got %T", val)
			}
			id, convErr := primitive.ObjectIDFromHex(s)
			if convErr != nil {
				return fail("oid %q is not 24 hex characters: %v", s, convErr)
			}
			return id, true, nil
		case "$date":
			s, isStr := val.(string)
			if !isStr {
				return fail("date marker must wrap a string, got %T", val)
			}
			ts, convErr := time.Parse(time.RFC3339Nano, s)
			if convErr != nil {
				return fail("date %q is not RFC 3339: %v", s, convErr)
			}
			return primitive.NewDateTimeFromTime(ts), true, nil
		case "$numberDecimal":
			s, isStr := val.(string)
			if !isStr {
				return fail("decimal marker must wrap a string, got %T", val)
			}
			d, convErr := primitive.ParseDecimal128(s)
			if convErr != nil {
				return fail("decimal %q is malformed: %v", s, convErr)
			}
			return d, true, nil
		case "$binary":
			sub, isObj := val.(map[string]any)
			if !isObj {
				return fail("binary marker must wrap an object, got %T", val)
			}
			return denormalizeBinary(sub, sentinel)
		case "$timestamp":
			sub, isObj := val.(map[string]any)
			if !isObj {
				return fail("timestamp marker must wrap an object, got %T", val)
			}
			return denormalizeTimestamp(sub, sentinel)
		case "$minKey":
			if !isNumberOne(val) {
				return fail("minKey marker must wrap 1, got %v", val)
			}
			return primitive.MinKey{}, true, nil
		case "$maxKey":
			if !isNumberOne(val) {
				return fail("maxKey marker must wrap 1, got %v", val)
			}
			return primitive.MaxKey{}, true, nil
		case "$regularExpression":
			sub, isObj := val.(map[string]any)
			if !isObj {
				return fail("regex marker must wrap an object, got %T", val)
			}
			pattern, _ := sub["pattern"].(string)
			opts, _ := sub["options"].(string)
			if _, hasPattern := sub["pattern"]; !hasPattern {
				return fail("regex marker needs a pattern string")
			}
			if _, hasOpts := sub["options"]; !hasOpts {
				return fail("regex marker needs an options string")
			}
			return primitive.Regex{Pattern: pattern, Options: opts}, true, nil
		default:
			return nil, false, nil
		}
	}
	return nil, false, nil
}

func denormalizeBinary(sub map[string]any, sentinel error) (any, bool, error) {
	b64, _ := sub["base64"].(string)
	hexType, _ := sub["subType"].(string)
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || b64 == "" {
		return nil, true, fmt.Errorf("%w: binary marker has malformed base64", sentinel)
	}
	raw, err := hex.DecodeString(hexType)
	if err != nil || len(raw) != 1 {
		return nil, true, fmt.Errorf("%w: binary marker subType %q must be one hex byte", sentinel, hexType)
	}
	return primitive.Binary{Subtype: raw[0], Data: data}, true, nil
}

func denormalizeTimestamp(sub map[string]any, sentinel error) (any, bool, error) {
	t, okT := markerUint(sub["t"])
	i, okI := markerUint(sub["i"])
	if !okT || !okI {
		return nil, true, fmt.Errorf("%w: timestamp marker needs unsigned t and i", sentinel)
	}
	return primitive.Timestamp{T: uint32(t), I: uint32(i)}, true, nil
}

func markerUint(v any) (uint64, bool) {
	n, ok := v.(json.Number)
	if !ok || !isIntegerSyntax(n.String()) {
		return 0, false
	}
	u, err := strconv.ParseUint(n.String(), 10, 32)
	if err != nil {
		return 0, false
	}
	return u, true
}

func isNumberOne(v any) bool {
	n, ok := v.(json.Number)
	return ok && n.String() == "1"
}
