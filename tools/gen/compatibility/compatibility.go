package compatibility

import (
	"fmt"
	"sort"
	"strings"

	"google.golang.org/protobuf/types/descriptorpb"
)

// Kind identifies the broad contract family. The comparison policy is
// deliberately the same for every kind: wire compatibility cannot become
// weaker merely because a message happens to be carried in an event, a file,
// or a connector payload.
type Kind string

const (
	KindDomain    Kind = "domain"
	KindEvent     Kind = "event"
	KindFile      Kind = "file"
	KindConnector Kind = "connector"
)

// Contract is one versioned Protobuf contract under comparison.
//
// MaterialDigest is an opaque, caller-owned digest of semantics that are not
// representable in Protobuf descriptors. It can be a canonical JSON digest,
// a signed policy digest, or any equivalent stable fingerprint. A changed
// non-empty value is material and must be accompanied by a newer Version.
//
// RequiredPaths gives proto3 fields required business semantics. Paths use
// "<fully.qualified.Message>.<field_name>". Proto2 LABEL_REQUIRED fields are
// included automatically and need not be repeated here.
type Contract struct {
	Kind           Kind                            `json:"kind"`
	Name           string                          `json:"name"`
	Version        uint32                          `json:"version"`
	MaterialDigest string                          `json:"material_digest"`
	Descriptors    *descriptorpb.FileDescriptorSet `json:"-"`
	RequiredPaths  []string                        `json:"required_paths,omitempty"`
}

// Classification is the directional wire-compatibility outcome. Forward
// means a reader of Current can accept bytes written by Previous; backward
// means a reader of Previous can accept bytes written by Current.
type Classification string

const (
	Compatible   Classification = "compatible"
	Incompatible Classification = "incompatible"
)

// ChangeCode identifies a non-rejected descriptor or semantic change.
type ChangeCode string

const (
	ChangeFieldAdded      ChangeCode = "field_added"
	ChangeFieldRemoved    ChangeCode = "field_removed"
	ChangeMessageAdded    ChangeCode = "message_added"
	ChangeMessageRemoved  ChangeCode = "message_removed"
	ChangeMaterialChanged ChangeCode = "material_changed"
)

// ViolationCode identifies a compatibility policy failure.
type ViolationCode string

const (
	ViolationInvalidContract                  ViolationCode = "invalid_contract"
	ViolationContractIdentityChanged          ViolationCode = "contract_identity_changed"
	ViolationVersionRegressed                 ViolationCode = "version_regressed"
	ViolationFieldNumberReuse                 ViolationCode = "field_number_reuse"
	ViolationFieldShapeChanged                ViolationCode = "field_shape_changed"
	ViolationRequiredSemanticsRemoved         ViolationCode = "required_semantics_removed"
	ViolationRequiredSemanticsAdded           ViolationCode = "required_semantics_added"
	ViolationIncompatibleOneofChange          ViolationCode = "incompatible_oneof_change"
	ViolationMessageRemoved                   ViolationCode = "message_removed"
	ViolationMaterialChangeWithoutVersionBump ViolationCode = "material_change_without_version_bump"
)

// Change records an observed evolution. Changes are sorted by code, path, and
// detail so reports are stable in CI and can be checked in as golden evidence.
type Change struct {
	Code   ChangeCode `json:"code"`
	Path   string     `json:"path"`
	Detail string     `json:"detail"`
}

// Violation is one rejected evolution. Its path always identifies the message
// or field that caused the decision; descriptors or callers are never asked to
// infer the rejected location from a broad compatibility boolean.
type Violation struct {
	Code   ViolationCode `json:"code"`
	Path   string        `json:"path"`
	Detail string        `json:"detail"`
}

// Report is the deterministic outcome of checking Previous against Current.
// A report is acceptable exactly when Violations is empty.
type Report struct {
	Kind       Kind           `json:"kind"`
	Name       string         `json:"name"`
	Previous   uint32         `json:"previous"`
	Current    uint32         `json:"current"`
	Forward    Classification `json:"forward"`
	Backward   Classification `json:"backward"`
	Changes    []Change       `json:"changes"`
	Violations []Violation    `json:"violations"`
}

// OK reports whether the transition satisfies the compatibility policy.
func (r Report) OK() bool { return len(r.Violations) == 0 }

// Check compares Previous and Current under the one Protobuf policy shared by
// all supported contract kinds. It does not read the network, invoke Buf, or
// use global descriptor registries; callers may therefore run it offline over
// generated descriptors or checked-in descriptor sets.
func Check(previous, current Contract) Report {
	r := Report{
		Kind:       current.Kind,
		Name:       current.Name,
		Previous:   previous.Version,
		Current:    current.Version,
		Forward:    Compatible,
		Backward:   Compatible,
		Changes:    []Change{},
		Violations: []Violation{},
	}

	previousMessages, previousRequired := validate(&r, previous, "previous")
	currentMessages, currentRequired := validate(&r, current, "current")
	if len(r.Violations) != 0 {
		// An unparseable or internally inconsistent descriptor cannot safely be
		// classified as compatible. Keep scanning for useful locations, but fail
		// the transition closed in both directions.
		markBothIncompatible(&r)
	}
	if previous.Kind != current.Kind || previous.Name != current.Name {
		addViolation(&r, ViolationContractIdentityChanged, current.Name,
			"previous contract is %s/%q, current contract is %s/%q", previous.Kind, previous.Name, current.Kind, current.Name)
		markBothIncompatible(&r)
	}
	if current.Version < previous.Version {
		addViolation(&r, ViolationVersionRegressed, current.Name,
			"current version %d is older than previous version %d", current.Version, previous.Version)
		markBothIncompatible(&r)
	}

	if previous.MaterialDigest != current.MaterialDigest && (previous.MaterialDigest != "" || current.MaterialDigest != "") {
		addChange(&r, ChangeMaterialChanged, current.Name, "material digest changed")
		if current.Version <= previous.Version {
			addViolation(&r, ViolationMaterialChangeWithoutVersionBump, current.Name,
				"material digest changed at version %d; a version greater than %d is required", current.Version, previous.Version)
			markBothIncompatible(&r)
		}
	}

	for name, oldMessage := range previousMessages {
		newMessage, exists := currentMessages[name]
		if !exists {
			addChange(&r, ChangeMessageRemoved, name, "message was removed")
			addViolation(&r, ViolationMessageRemoved, name, "message was removed")
			markBothIncompatible(&r)
			continue
		}
		compareMessage(&r, oldMessage, newMessage, previousRequired, currentRequired)
	}
	for name := range currentMessages {
		if _, exists := previousMessages[name]; !exists {
			addChange(&r, ChangeMessageAdded, name, "message was added")
		}
	}

	sort.Slice(r.Changes, func(i, j int) bool {
		if r.Changes[i].Code != r.Changes[j].Code {
			return r.Changes[i].Code < r.Changes[j].Code
		}
		if r.Changes[i].Path != r.Changes[j].Path {
			return r.Changes[i].Path < r.Changes[j].Path
		}
		return r.Changes[i].Detail < r.Changes[j].Detail
	})
	sort.Slice(r.Violations, func(i, j int) bool {
		if r.Violations[i].Code != r.Violations[j].Code {
			return r.Violations[i].Code < r.Violations[j].Code
		}
		if r.Violations[i].Path != r.Violations[j].Path {
			return r.Violations[i].Path < r.Violations[j].Path
		}
		return r.Violations[i].Detail < r.Violations[j].Detail
	})
	return r
}

type message struct {
	name           string
	fieldsByName   map[string]*descriptorpb.FieldDescriptorProto
	fieldsByNumber map[int32]*descriptorpb.FieldDescriptorProto
	oneofs         []string
}

func validate(r *Report, c Contract, side string) (map[string]message, map[string]bool) {
	messages := map[string]message{}
	if !validKind(c.Kind) {
		addViolation(r, ViolationInvalidContract, c.Name, "%s contract kind %q is not recognized", side, c.Kind)
	}
	if c.Name == "" {
		addViolation(r, ViolationInvalidContract, c.Name, "%s contract name is empty", side)
	}
	if c.Version == 0 {
		addViolation(r, ViolationInvalidContract, c.Name, "%s contract version must be positive", side)
	}
	if c.Descriptors == nil || len(c.Descriptors.GetFile()) == 0 {
		addViolation(r, ViolationInvalidContract, c.Name, "%s contract has no descriptors", side)
		return messages, map[string]bool{}
	}
	for _, file := range c.Descriptors.GetFile() {
		if file.GetPackage() == "" {
			addViolation(r, ViolationInvalidContract, c.Name, "%s descriptor %q has no package", side, file.GetName())
		}
		for _, m := range file.GetMessageType() {
			collectMessage(r, messages, file.GetPackage(), m, side)
		}
	}

	required := map[string]bool{}
	for name, m := range messages {
		for fieldName, field := range m.fieldsByName {
			if field.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REQUIRED {
				required[name+"."+fieldName] = true
			}
		}
	}
	for _, path := range c.RequiredPaths {
		mName, fName, ok := splitFieldPath(path)
		if !ok || messages[mName].fieldsByName[fName] == nil {
			addViolation(r, ViolationInvalidContract, path, "%s required path does not identify a declared field", side)
			continue
		}
		required[path] = true
	}
	return messages, required
}

func collectMessage(r *Report, into map[string]message, prefix string, desc *descriptorpb.DescriptorProto, side string) {
	name := desc.GetName()
	if name == "" {
		addViolation(r, ViolationInvalidContract, prefix, "%s descriptor declares an unnamed message", side)
		return
	}
	fullName := name
	if prefix != "" {
		fullName = prefix + "." + name
	}
	if _, exists := into[fullName]; exists {
		addViolation(r, ViolationInvalidContract, fullName, "%s descriptor declares the message more than once", side)
		return
	}
	m := message{
		name:           fullName,
		fieldsByName:   map[string]*descriptorpb.FieldDescriptorProto{},
		fieldsByNumber: map[int32]*descriptorpb.FieldDescriptorProto{},
		oneofs:         append([]string(nil), oneofNames(desc.GetOneofDecl())...),
	}
	for _, field := range desc.GetField() {
		path := fullName + "." + field.GetName()
		if field.GetName() == "" || field.GetNumber() <= 0 {
			addViolation(r, ViolationInvalidContract, path, "%s descriptor field must have a name and positive number", side)
			continue
		}
		if existing := m.fieldsByName[field.GetName()]; existing != nil {
			addViolation(r, ViolationInvalidContract, path, "%s descriptor repeats field name %q", side, field.GetName())
			continue
		}
		if existing := m.fieldsByNumber[field.GetNumber()]; existing != nil {
			addViolation(r, ViolationInvalidContract, path, "%s descriptor reuses field number %d already held by %q", side, field.GetNumber(), existing.GetName())
			continue
		}
		if index := field.OneofIndex; index != nil && (*index < 0 || int(*index) >= len(m.oneofs)) {
			addViolation(r, ViolationInvalidContract, path, "%s descriptor references undeclared oneof index %d", side, *index)
		}
		m.fieldsByName[field.GetName()] = field
		m.fieldsByNumber[field.GetNumber()] = field
	}
	into[fullName] = m
	for _, nested := range desc.GetNestedType() {
		collectMessage(r, into, fullName, nested, side)
	}
}

func compareMessage(r *Report, previous, current message, previousRequired, currentRequired map[string]bool) {
	for number, oldField := range previous.fieldsByNumber {
		path := previous.name + "." + oldField.GetName()
		newField := current.fieldsByNumber[number]
		if newField == nil {
			addChange(r, ChangeFieldRemoved, path, fmt.Sprintf("field number %d was removed", number))
			if sameNamed := current.fieldsByName[oldField.GetName()]; sameNamed != nil {
				addViolation(r, ViolationFieldNumberReuse, path,
					"field %q moved from number %d to %d", oldField.GetName(), number, sameNamed.GetNumber())
				markBothIncompatible(r)
			}
			if previousRequired[path] {
				addViolation(r, ViolationRequiredSemanticsRemoved, path, "required field was removed")
				markBackwardIncompatible(r)
			}
			continue
		}
		if oldField.GetName() != newField.GetName() {
			addViolation(r, ViolationFieldNumberReuse, previous.name+"."+newField.GetName(),
				"field number %d belonged to %q and now belongs to %q", number, oldField.GetName(), newField.GetName())
			markBothIncompatible(r)
			continue
		}
		if previousRequired[path] && !currentRequired[path] {
			addViolation(r, ViolationRequiredSemanticsRemoved, path, "required semantics were removed")
			markBackwardIncompatible(r)
		}
		if !previousRequired[path] && currentRequired[path] {
			addViolation(r, ViolationRequiredSemanticsAdded, path, "field gained required semantics")
			markForwardIncompatible(r)
		}
		if fieldShapeChanged(oldField, newField) {
			addViolation(r, ViolationFieldShapeChanged, path, "field type, cardinality, or presence changed")
			markBothIncompatible(r)
		}
		if oneof(previous, oldField) != oneof(current, newField) {
			addViolation(r, ViolationIncompatibleOneofChange, path,
				"oneof membership changed from %q to %q", oneof(previous, oldField), oneof(current, newField))
			markBothIncompatible(r)
		}
	}
	for number, newField := range current.fieldsByNumber {
		if _, existed := previous.fieldsByNumber[number]; existed {
			continue
		}
		path := current.name + "." + newField.GetName()
		addChange(r, ChangeFieldAdded, path, fmt.Sprintf("field number %d was added", number))
		if newField.OneofIndex != nil {
			addViolation(r, ViolationIncompatibleOneofChange, path,
				"field was added to oneof %q", oneof(current, newField))
			markBothIncompatible(r)
		}
		if currentRequired[path] {
			addViolation(r, ViolationRequiredSemanticsAdded, path, "new field has required semantics")
			markForwardIncompatible(r)
		}
	}
}

func fieldShapeChanged(previous, current *descriptorpb.FieldDescriptorProto) bool {
	return previous.GetType() != current.GetType() ||
		previous.GetTypeName() != current.GetTypeName() ||
		previous.GetLabel() != current.GetLabel() ||
		previous.GetProto3Optional() != current.GetProto3Optional()
}

func oneof(m message, f *descriptorpb.FieldDescriptorProto) string {
	if f.OneofIndex == nil || *f.OneofIndex < 0 || int(*f.OneofIndex) >= len(m.oneofs) {
		return ""
	}
	return m.oneofs[*f.OneofIndex]
}

func oneofNames(decls []*descriptorpb.OneofDescriptorProto) []string {
	names := make([]string, len(decls))
	for i, decl := range decls {
		names[i] = decl.GetName()
	}
	return names
}

func splitFieldPath(path string) (messageName, fieldName string, ok bool) {
	i := strings.LastIndexByte(path, '.')
	if i <= 0 || i == len(path)-1 {
		return "", "", false
	}
	return path[:i], path[i+1:], true
}

func validKind(kind Kind) bool {
	switch kind {
	case KindDomain, KindEvent, KindFile, KindConnector:
		return true
	default:
		return false
	}
}

func addChange(r *Report, code ChangeCode, path, detail string) {
	r.Changes = append(r.Changes, Change{Code: code, Path: path, Detail: detail})
}

func addViolation(r *Report, code ViolationCode, path, format string, args ...any) {
	r.Violations = append(r.Violations, Violation{Code: code, Path: path, Detail: fmt.Sprintf(format, args...)})
}

func markForwardIncompatible(r *Report)  { r.Forward = Incompatible }
func markBackwardIncompatible(r *Report) { r.Backward = Incompatible }
func markBothIncompatible(r *Report) {
	markForwardIncompatible(r)
	markBackwardIncompatible(r)
}
