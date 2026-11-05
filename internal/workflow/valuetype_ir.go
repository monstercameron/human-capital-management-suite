// WF-DATA-005: the plan IR schema version gate for the new value kinds.
//
// DURATION, RATE and REFERENCE are added under plan IR schema version 3.
// Every type knows the minimum IR version that can carry it, and a field
// list reports the maximum over its fields, so a plan using a new kind
// requires IR 3 while plans on prior IR versions validate unchanged.
//
// Integration: the compiler stamps CurrentIRSchemaVersion from
// compile.go. Promoting that constant to IRSchemaVersionWithTypeKinds is
// the pipeline's one-line step once IR 3 is the version new plans
// compile to; until then this gate is what refuses a new kind on an old
// runtime.
package workflow

// IRSchemaVersionWithTypeKinds is the plan IR schema version that
// carries DURATION, RATE and REFERENCE.
const IRSchemaVersionWithTypeKinds uint32 = 3

// priorIRSchemaVersion is the newest IR schema before the type kinds.
const priorIRSchemaVersion uint32 = 2

// RequiresIRVersion reports the minimum plan IR schema version that can
// carry t: 3 for the WF-DATA-005 kinds (through LIST elements), 2
// otherwise.
func (t ValueType) RequiresIRVersion() uint32 {
	switch t.Kind {
	case KindDuration, KindRate, KindReference:
		return IRSchemaVersionWithTypeKinds
	case KindList:
		if t.Element != nil {
			return t.Element.RequiresIRVersion()
		}
		return priorIRSchemaVersion
	default:
		return priorIRSchemaVersion
	}
}

// IRVersionForFields reports the minimum plan IR schema version that can
// carry these fields: the maximum over the field types.
func IRVersionForFields(fields []Field) uint32 {
	version := priorIRSchemaVersion
	for _, f := range fields {
		if v := f.Type.RequiresIRVersion(); v > version {
			version = v
		}
	}
	return version
}
