package workflow

import (
	"fmt"
	"strings"
)

// DataClassification is the closed lattice of field sensitivity
// (WF-DATA-004): PUBLIC < INTERNAL < CONFIDENTIAL < RESTRICTED. It is the
// same vocabulary tenant field classifications and the event-subscription
// lattice use.
//
// Every field type carries its classification (ValueType.Classification,
// blank meaning PUBLIC); every node input may declare the highest
// classification it accepts (Field.MaxClassification, blank meaning
// uncapped). Capability inputs, message template slots, document bindings
// and connector requests all arrive at the compiler as node input fields,
// so their builders declare caps on the fields they generate and the
// compiler enforces every mapping uniformly, whatever the step type.
type DataClassification string

const (
	// DataPublic is data safe for unrestricted destinations.
	DataPublic DataClassification = "PUBLIC"
	// DataInternal is data for internal destinations only.
	DataInternal DataClassification = "INTERNAL"
	// DataConfidential is data for named confidential destinations.
	DataConfidential DataClassification = "CONFIDENTIAL"
	// DataRestricted is data for explicitly approved destinations only.
	DataRestricted DataClassification = "RESTRICTED"
)

// Valid reports whether c is a lattice member. The empty label is not a
// member; it is the legacy spelling of PUBLIC.
func (c DataClassification) Valid() bool {
	switch c {
	case DataPublic, DataInternal, DataConfidential, DataRestricted:
		return true
	default:
		return false
	}
}

func (c DataClassification) rank() int {
	switch c {
	case DataPublic:
		return 0
	case DataInternal:
		return 1
	case DataConfidential:
		return 2
	case DataRestricted:
		return 3
	default:
		return -1
	}
}

// Dominates reports whether c is at least as restrictive as need. Either
// side outside the lattice fails closed.
func (c DataClassification) Dominates(need DataClassification) bool {
	if !c.Valid() || !need.Valid() {
		return false
	}
	return c.rank() >= need.rank()
}

// ParseDataClassification resolves one exact lattice token. Matching is
// case-sensitive; surrounding whitespace is tolerated and nothing else is.
// Qualified HR labels such as CONFIDENTIAL_HR are not lattice members: new
// declarations use the lattice, and unknown labels fail closed.
func ParseDataClassification(raw string) (DataClassification, error) {
	c := DataClassification(strings.TrimSpace(raw))
	if !c.Valid() {
		return "", fmt.Errorf("workflow: data classification %q is not a declared lattice member", raw)
	}
	return c, nil
}

// labelOf normalizes a source label: blank (legacy, unlabelled) reads PUBLIC.
func labelOf(label DataClassification) DataClassification {
	if label == "" {
		return DataPublic
	}
	return label
}

// flowAllowed reports whether a value labelled source may flow to a
// destination capped at max. A blank cap is the legacy uncapped
// destination and accepts everything; anything else must dominate the
// source label. This is the one comparison every flow check reuses,
// including context reads.
func flowAllowed(source, max DataClassification) bool {
	if max == "" {
		return true
	}
	return max.Dominates(labelOf(source))
}

// checkMappingFlow refuses a mapping whose source label exceeds the target
// input's declared maximum without a referenced declassification. Unknown
// labels are left to validateFields, which already reported them.
func checkMappingFlow(n *Node, m Mapping, source ValueType, loc Location, c *collector) {
	if source.Classification != "" && !source.Classification.Valid() {
		return
	}
	var max DataClassification
	for _, f := range n.Inputs {
		if f.Path == m.Target {
			max = f.MaxClassification
			break
		}
	}
	if max != "" && !max.Valid() {
		return
	}
	if flowAllowed(source.Classification, max) {
		return
	}
	if strings.TrimSpace(m.DeclassificationRef) != "" {
		return
	}
	c.add(CodeRestrictedFlow, loc,
		"%s data flows into %q capped at %s without a referenced declassification",
		labelOf(source.Classification), m.Target, max)
}

// checkOutputFlow refuses a workflow output whose declared maximum sits
// below the terminal value's label. The exception lives on the mapping
// routing into the terminal input: terminal outputs have no mapping of
// their own to reference one.
func checkOutputFlow(n *Node, out Field, got ValueType, loc Location, c *collector) {
	if out.MaxClassification == "" || !out.MaxClassification.Valid() {
		return
	}
	if got.Classification != "" && !got.Classification.Valid() {
		return
	}
	if flowAllowed(got.Classification, out.MaxClassification) {
		return
	}
	for _, m := range n.InputMappings {
		if m.Target == out.Path && strings.TrimSpace(m.DeclassificationRef) != "" {
			return
		}
	}
	c.add(CodeRestrictedFlow, loc,
		"workflow output %q capped at %s receives %s data without a referenced declassification",
		out.Path, out.MaxClassification, labelOf(got.Classification))
}
