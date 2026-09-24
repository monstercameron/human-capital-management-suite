// Package customobject owns the bounded, typed schema contract for tenant
// custom objects. It deliberately contains no execution hooks: custom
// schemas describe data only and are compiled into an immutable descriptor.
package customobject

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

const (
	// DefaultMaxFields prevents a tenant definition from becoming an
	// unbounded storage or indexing contract.
	DefaultMaxFields         = 128
	DefaultMaxFieldNameBytes = 64
	DefaultMaxTypeBytes      = 64
)

var (
	namePattern      = regexp.MustCompile(`^[A-Z][A-Za-z0-9]{1,62}$`)
	namespacePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:\.[a-z][a-z0-9]*)*$`)
	fieldPattern     = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

// Field is one declared custom-object property. Type is a SchemaFlux wire
// primitive name, never Go code or an expression.
type Field struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

// Limits makes resource bounds part of the schema contract. Zero values use
// the package defaults; callers may only tighten the field count.
type Limits struct {
	MaxFields int `json:"max_fields"`
}

// CustomObjectType is the source definition submitted for compilation.
type CustomObjectType struct {
	Name      string  `json:"name"`
	Namespace string  `json:"namespace"`
	Owner     string  `json:"owner"`
	Version   uint32  `json:"version"`
	Fields    []Field `json:"fields"`
	Limits    Limits  `json:"limits"`
}

// CompiledSchema is the immutable SchemaFlux projection. Digest is stable
// for semantically identical definitions regardless of field input order.
type CompiledSchema struct {
	Name      string  `json:"name"`
	Namespace string  `json:"namespace"`
	Owner     string  `json:"owner"`
	Version   uint32  `json:"version"`
	Fields    []Field `json:"fields"`
	Limits    Limits  `json:"limits"`
	Digest    string  `json:"digest"`
}

var supportedTypes = map[string]struct{}{
	"string": {}, "bool": {}, "int32": {}, "int64": {}, "uint32": {},
	"[]string": {}, "[]byte": {},
	"EntityId": {}, "EntityRef": {}, "ResourceKey": {}, "RevisionToken": {},
	"FixedDecimal": {}, "Money": {}, "Percentage": {}, "Quantity": {}, "Rate": {},
	"TemporalInstant": {}, "LocalDate": {}, "LocalTime": {}, "ZonedDateTime": {},
	"TemporalInterval": {}, "RecordedAt": {}, "KnownAt": {}, "PresenceValue": {},
}

var reservedNames = map[string]struct{}{
	"id": {}, "tenant_id": {}, "namespace": {}, "object_type": {}, "schema_version": {},
	"revision": {}, "created_at": {}, "updated_at": {}, "deleted_at": {},
}

// Compile validates and compiles a custom object into a deterministic
// SchemaFlux descriptor. It never executes user supplied code.
func Compile(src CustomObjectType) (CompiledSchema, error) {
	if !namePattern.MatchString(src.Name) {
		return CompiledSchema{}, fmt.Errorf("custom object name %q is invalid", src.Name)
	}
	if !namespacePattern.MatchString(src.Namespace) {
		return CompiledSchema{}, fmt.Errorf("custom object namespace %q is invalid", src.Namespace)
	}
	if strings.TrimSpace(src.Owner) == "" {
		return CompiledSchema{}, fmt.Errorf("custom object %q has no owner", src.Name)
	}
	if src.Version == 0 {
		return CompiledSchema{}, fmt.Errorf("custom object %q has no schema version", src.Name)
	}
	max := src.Limits.MaxFields
	if max == 0 {
		max = DefaultMaxFields
	}
	if max < 1 || max > DefaultMaxFields {
		return CompiledSchema{}, fmt.Errorf("max_fields must be between 1 and %d", DefaultMaxFields)
	}
	if len(src.Fields) == 0 {
		return CompiledSchema{}, fmt.Errorf("custom object %q has no fields", src.Name)
	}
	if len(src.Fields) > max {
		return CompiledSchema{}, fmt.Errorf("custom object %q has %d fields, exceeding max %d", src.Name, len(src.Fields), max)
	}

	fields := append([]Field(nil), src.Fields...)
	seen := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		if !fieldPattern.MatchString(f.Name) {
			return CompiledSchema{}, fmt.Errorf("field name %q is invalid", f.Name)
		}
		if _, ok := reservedNames[f.Name]; ok {
			return CompiledSchema{}, fmt.Errorf("field name %q is reserved", f.Name)
		}
		if _, ok := seen[f.Name]; ok {
			return CompiledSchema{}, fmt.Errorf("duplicate field %q", f.Name)
		}
		seen[f.Name] = struct{}{}
		if len(f.Name) > DefaultMaxFieldNameBytes {
			return CompiledSchema{}, fmt.Errorf("field %q exceeds name limit", f.Name)
		}
		if len(f.Type) == 0 || len(f.Type) > DefaultMaxTypeBytes {
			return CompiledSchema{}, fmt.Errorf("field %q has invalid type", f.Name)
		}
		if _, ok := supportedTypes[f.Type]; !ok {
			return CompiledSchema{}, fmt.Errorf("field %q uses unsupported primitive %q", f.Name, f.Type)
		}
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	canonical := struct {
		Name, Namespace, Owner string
		Version                uint32
		Fields                 []Field
		Limits                 Limits
	}{src.Name, src.Namespace, src.Owner, src.Version, fields, Limits{MaxFields: max}}
	b, err := json.Marshal(canonical)
	if err != nil {
		return CompiledSchema{}, fmt.Errorf("marshal custom object: %w", err)
	}
	return CompiledSchema{src.Name, src.Namespace, src.Owner, src.Version, fields, Limits{MaxFields: max}, canonicalbytes.Digest(b)}, nil
}

// CompileSchema is an explicit alias useful to callers that distinguish
// source schemas from the compiled descriptor.
func CompileSchema(src CustomObjectType) (CompiledSchema, error) { return Compile(src) }
