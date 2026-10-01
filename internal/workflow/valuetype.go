package workflow

import "fmt"

// Kind is the base kind of a typed workflow value.
//
// There is deliberately no floating-point kind: the workflow context contract
// forbids floats in material data, so a money or rate value is DECIMAL or
// MONEY, never a float that silently loses cents.
type Kind string

// The declared value kinds.
const (
	KindString    Kind = "STRING"
	KindInteger   Kind = "INTEGER"
	KindDecimal   Kind = "DECIMAL"
	KindBool      Kind = "BOOL"
	KindInstant   Kind = "INSTANT"
	KindLocalDate Kind = "LOCAL_DATE"
	KindMoney     Kind = "MONEY"
	KindEnum      Kind = "ENUM"
	KindMessage   Kind = "MESSAGE"
	KindList      Kind = "LIST"
)

// ValueType is one node's declared type for an input or output field. It is
// the compiler's whole type algebra: nullability, branded identity, enum and
// message identity and list element types. Two values are assignable only when
// every component agrees; the compiler performs no implicit coercion, because
// a coercion the author did not write is a semantic change nobody reviewed.
type ValueType struct {
	Kind Kind `json:"kind"`
	// Nullable marks a value that may be absent. A nullable source never
	// flows into a non-nullable target.
	Nullable bool `json:"nullable,omitempty"`
	// Brand names a branded identity, for example "WorkerID". A branded value
	// is not assignable to a differently branded or unbranded target: a
	// tenant-scoped opaque reference never substitutes for a bare string.
	Brand string `json:"brand,omitempty"`
	// EnumRef names the published enum, required when Kind is ENUM.
	EnumRef string `json:"enum_ref,omitempty"`
	// MessageRef names the published message schema, required when Kind is
	// MESSAGE.
	MessageRef string `json:"message_ref,omitempty"`
	// Element is the element type, required when Kind is LIST.
	Element *ValueType `json:"element,omitempty"`
}

func (t ValueType) String() string {
	s := string(t.Kind)
	switch t.Kind {
	case KindEnum:
		s += "<" + t.EnumRef + ">"
	case KindMessage:
		s += "<" + t.MessageRef + ">"
	case KindList:
		if t.Element != nil {
			s += "<" + t.Element.String() + ">"
		} else {
			s += "<?>"
		}
	}
	if t.Brand != "" {
		s += "#" + t.Brand
	}
	if t.Nullable {
		s += "?"
	}
	return s
}

// Validate reports whether the type is well formed: a declared kind, and the
// component every kind requires.
func (t ValueType) Validate() error {
	switch t.Kind {
	case KindString, KindInteger, KindDecimal, KindBool, KindInstant, KindLocalDate, KindMoney:
	case KindEnum:
		if t.EnumRef == "" {
			return fmt.Errorf("ENUM requires enum_ref")
		}
	case KindMessage:
		if t.MessageRef == "" {
			return fmt.Errorf("MESSAGE requires message_ref")
		}
	case KindList:
		if t.Element == nil {
			return fmt.Errorf("LIST requires an element type")
		}
		if err := t.Element.Validate(); err != nil {
			return fmt.Errorf("LIST element: %w", err)
		}
	case "":
		return fmt.Errorf("kind is required")
	default:
		return fmt.Errorf("unknown kind %q", string(t.Kind))
	}
	return nil
}

// AssignableTo reports whether a value of t may flow into a target of type
// want. It returns a diagnostic detail rather than a bool so the compiler can
// say which component disagreed.
func (t ValueType) AssignableTo(want ValueType) error {
	if t.Kind != want.Kind {
		return fmt.Errorf("kind %s is not assignable to %s", t.Kind, want.Kind)
	}
	if t.Brand != want.Brand {
		return fmt.Errorf("brand %q is not assignable to %q", t.Brand, want.Brand)
	}
	if t.Kind == KindEnum && t.EnumRef != want.EnumRef {
		return fmt.Errorf("enum %q is not assignable to %q", t.EnumRef, want.EnumRef)
	}
	if t.Kind == KindMessage && t.MessageRef != want.MessageRef {
		return fmt.Errorf("message %q is not assignable to %q", t.MessageRef, want.MessageRef)
	}
	if t.Kind == KindList {
		if t.Element == nil || want.Element == nil {
			return fmt.Errorf("list element type is undeclared")
		}
		if err := t.Element.AssignableTo(*want.Element); err != nil {
			return fmt.Errorf("list element: %w", err)
		}
	}
	if t.Nullable && !want.Nullable {
		return fmt.Errorf("nullable value is not assignable to non-nullable target")
	}
	return nil
}

// clone deep-copies a type so a compiled plan never aliases a draft.
func (t ValueType) clone() ValueType {
	if t.Element != nil {
		e := t.Element.clone()
		t.Element = &e
	}
	return t
}

// Field is one named, typed input or output field. Field paths are the whole
// dataflow surface: a node never receives an untyped bag, and nothing reads a
// path its producer did not declare.
type Field struct {
	Path string    `json:"path"`
	Type ValueType `json:"type"`
	// Label and Description are immutable definition metadata. They are
	// presentation hints only; the path and type remain the workflow's
	// authority and are what the compiler validates.
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	Group       string `json:"group,omitempty"`
}

func fieldsByPath(fields []Field) map[string]ValueType {
	out := make(map[string]ValueType, len(fields))
	for _, f := range fields {
		out[f.Path] = f.Type
	}
	return out
}

func cloneFields(fields []Field) []Field {
	if fields == nil {
		return nil
	}
	out := make([]Field, len(fields))
	for i, f := range fields {
		out[i] = Field{Path: f.Path, Type: f.Type.clone(), Label: f.Label, Description: f.Description, Group: f.Group}
	}
	return out
}
