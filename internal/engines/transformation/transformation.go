// Package transformation owns the versioned, declarative contract for data
// transformations. It deliberately contains no domain or adapter types.
package transformation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const ContractVersion = 1
const DigestAlgorithm = "sha256"

var (
	ErrInvalidDefinition = errors.New("transformation: invalid definition")
	ErrUntypedPath       = errors.New("transformation: path must be typed")
	ErrUnknownOperation  = errors.New("transformation: unknown operation")
	ErrArbitraryCode     = errors.New("transformation: arbitrary code is not permitted")
	ErrAmbientDependency = errors.New("transformation: ambient dependency is not permitted")
	ErrUndeclaredEffect  = errors.New("transformation: side effect or failure must be declared")
)

var nameRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.:/-]*$`)

// Type is the closed set of scalar types understood by the engine.
type Type string

const (
	TypeString    Type = "string"
	TypeBool      Type = "bool"
	TypeInt       Type = "int"
	TypeDecimal   Type = "decimal"
	TypeDate      Type = "date"
	TypeTimestamp Type = "timestamp"
)

type Field struct {
	Name     string `json:"name"`
	Type     Type   `json:"type"`
	Required bool   `json:"required"`
}
type Schema struct {
	Name    string  `json:"name"`
	Version int     `json:"version"`
	Fields  []Field `json:"fields"`
}

// Path includes its schema and the declared field type; a bare string path is
// intentionally not representable in this contract.
type Path struct {
	Schema string `json:"schema"`
	Field  string `json:"field"`
	Type   Type   `json:"type"`
}

type OperationKind string

const (
	OpCopy      OperationKind = "copy"
	OpConvert   OperationKind = "convert"
	OpRename    OperationKind = "rename"
	OpDefault   OperationKind = "default"
	OpConcat    OperationKind = "concat"
	OpTransform OperationKind = "transform"
)

type Operation struct {
	Kind        OperationKind     `json:"kind"`
	Source      *Path             `json:"source,omitempty"`
	Destination Path              `json:"destination"`
	TargetType  Type              `json:"target_type,omitempty"`
	Literal     string            `json:"literal,omitempty"`
	Sources     []Path            `json:"sources,omitempty"`
	Function    string            `json:"function,omitempty"`
	Argument    string            `json:"argument,omitempty"`
	Lookup      map[string]string `json:"lookup,omitempty"`
}

type Compatibility struct {
	MinimumSourceVersion int  `json:"minimum_source_version"`
	MaximumSourceVersion int  `json:"maximum_source_version"`
	Breaking             bool `json:"breaking"`
}
type ResourceLimits struct {
	MaxOperations  int   `json:"max_operations"`
	MaxInputBytes  int64 `json:"max_input_bytes"`
	MaxOutputBytes int64 `json:"max_output_bytes"`
	MaxExpansion   int   `json:"max_expansion"`
}
type FailurePolicy string

const (
	FailureReject FailurePolicy = "reject"
	FailureSkip   FailurePolicy = "skip"
)

type SideEffectPolicy string

const SideEffectsNone SideEffectPolicy = "none"

type TransformationDefinition struct {
	Version       int              `json:"version"`
	Name          string           `json:"name"`
	Owner         string           `json:"owner"`
	Phase         string           `json:"phase"`
	Source        Schema           `json:"source"`
	Destination   Schema           `json:"destination"`
	Operations    []Operation      `json:"operations"`
	Compatibility Compatibility    `json:"compatibility"`
	Limits        ResourceLimits   `json:"limits"`
	Failure       FailurePolicy    `json:"failure"`
	SideEffects   SideEffectPolicy `json:"side_effects"`
	// AmbientDependencies names inputs that would be resolved from process,
	// network, clock, locale, or other ambient state. They are deliberately
	// forbidden: every value consumed by a transformation must be in its
	// typed source schema (or a literal declared by an operation).
	AmbientDependencies []string `json:"ambient_dependencies,omitempty"`
}

func validType(t Type) bool {
	switch t {
	case TypeString, TypeBool, TypeInt, TypeDecimal, TypeDate, TypeTimestamp:
		return true
	}
	return false
}
func validateSchema(s Schema) error {
	if !nameRE.MatchString(s.Name) || s.Version < 1 || len(s.Fields) == 0 {
		return fmt.Errorf("%w: invalid schema", ErrInvalidDefinition)
	}
	seen := map[string]bool{}
	for _, f := range s.Fields {
		if !nameRE.MatchString(f.Name) || !validType(f.Type) || seen[f.Name] {
			return fmt.Errorf("%w: invalid schema field %q", ErrInvalidDefinition, f.Name)
		}
		seen[f.Name] = true
	}
	return nil
}
func pathIn(p Path, s Schema) bool {
	for _, f := range s.Fields {
		if f.Name == p.Field {
			return f.Type == p.Type && p.Schema == s.Name
		}
	}
	return false
}

// Validate enforces the closed vocabulary and all declarations required for
// deterministic, bounded execution.
func (d TransformationDefinition) Validate() error {
	if d.Version != ContractVersion || !nameRE.MatchString(d.Name) || d.Owner == "" || d.Phase == "" {
		return fmt.Errorf("%w: version, name, owner and phase are required", ErrInvalidDefinition)
	}
	if err := validateSchema(d.Source); err != nil {
		return err
	}
	if err := validateSchema(d.Destination); err != nil {
		return err
	}
	if d.Limits.MaxOperations <= 0 || d.Limits.MaxInputBytes <= 0 || d.Limits.MaxOutputBytes <= 0 || d.Limits.MaxExpansion <= 0 || len(d.Operations) > d.Limits.MaxOperations {
		return fmt.Errorf("%w: invalid resource limits", ErrInvalidDefinition)
	}
	if d.Failure != FailureReject && d.Failure != FailureSkip {
		return fmt.Errorf("%w: %w", ErrInvalidDefinition, ErrUndeclaredEffect)
	}
	if d.SideEffects != SideEffectsNone {
		return fmt.Errorf("%w: side effects must be none", ErrUndeclaredEffect)
	}
	if len(d.AmbientDependencies) != 0 {
		return fmt.Errorf("%w: %v", ErrAmbientDependency, d.AmbientDependencies)
	}
	if d.Compatibility.MinimumSourceVersion < 1 || (d.Compatibility.MaximumSourceVersion > 0 && d.Compatibility.MaximumSourceVersion < d.Compatibility.MinimumSourceVersion) {
		return fmt.Errorf("%w: invalid compatibility", ErrInvalidDefinition)
	}
	for _, op := range d.Operations {
		if op.Kind == "" || strings.Contains(string(op.Kind), "code") || strings.Contains(string(op.Kind), "script") {
			return fmt.Errorf("%w: %q", ErrArbitraryCode, op.Kind)
		}
		switch op.Kind {
		case OpCopy, OpConvert, OpRename:
			if op.Source == nil || !pathIn(*op.Source, d.Source) || !pathIn(op.Destination, d.Destination) {
				return ErrUntypedPath
			}
			if op.Kind == OpConvert && !validType(op.TargetType) {
				return ErrUntypedPath
			}
			if op.Kind != OpConvert && op.Source.Type != op.Destination.Type {
				return fmt.Errorf("%w: source and destination types differ", ErrUntypedPath)
			}
			if op.Kind == OpConvert && op.TargetType != op.Destination.Type {
				return fmt.Errorf("%w: conversion target and destination types differ", ErrUntypedPath)
			}
		case OpDefault:
			if !pathIn(op.Destination, d.Destination) || op.Literal == "" {
				return fmt.Errorf("%w: default", ErrInvalidDefinition)
			}
		case OpConcat:
			if !pathIn(op.Destination, d.Destination) || len(op.Sources) < 2 {
				return fmt.Errorf("%w: concat", ErrInvalidDefinition)
			}
			for _, p := range op.Sources {
				if !pathIn(p, d.Source) {
					return ErrUntypedPath
				}
			}
		case OpTransform:
			if op.Source == nil || !pathIn(*op.Source, d.Source) || !pathIn(op.Destination, d.Destination) || op.Source.Type != TypeString || op.Destination.Type != TypeString || op.Function == "" {
				return fmt.Errorf("%w: transform", ErrUntypedPath)
			}
			if len(op.Sources) != 0 || op.TargetType != "" || op.Literal != "" {
				return fmt.Errorf("%w: transform carries unrelated operands", ErrInvalidDefinition)
			}
			switch op.Function {
			case "trim", "upper", "lower", "compose":
				if len(op.Lookup) != 0 {
					return fmt.Errorf("%w: transform lookup not allowed", ErrInvalidDefinition)
				}
			case "lookup":
				if len(op.Lookup) == 0 || op.Argument != "" {
					return fmt.Errorf("%w: transform lookup", ErrInvalidDefinition)
				}
				for k, v := range op.Lookup {
					if k == "" || v == "" {
						return fmt.Errorf("%w: transform lookup entry", ErrInvalidDefinition)
					}
				}
			case "date_parse", "money_parse":
				if op.Argument == "" || len(op.Lookup) != 0 {
					return fmt.Errorf("%w: transform argument", ErrInvalidDefinition)
				}
			default:
				return fmt.Errorf("%w: transform function %q", ErrUnknownOperation, op.Function)
			}
		default:
			return fmt.Errorf("%w: %q", ErrUnknownOperation, op.Kind)
		}
	}
	return nil
}

type canonicalDefinition struct{ TransformationDefinition }

func (d TransformationDefinition) CanonicalBytes() ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	c := d
	c.Source.Fields = append([]Field(nil), d.Source.Fields...)
	c.Destination.Fields = append([]Field(nil), d.Destination.Fields...)
	sort.Slice(c.Source.Fields, func(i, j int) bool { return c.Source.Fields[i].Name < c.Source.Fields[j].Name })
	sort.Slice(c.Destination.Fields, func(i, j int) bool { return c.Destination.Fields[i].Name < c.Destination.Fields[j].Name })
	return json.Marshal(canonicalDefinition{c})
}
func (d TransformationDefinition) Digest() (string, error) {
	b, err := d.CanonicalBytes()
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return DigestAlgorithm + ":" + hex.EncodeToString(h[:]), nil
}
