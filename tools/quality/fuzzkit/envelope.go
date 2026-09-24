// Package fuzzkit is the TOOL-013 shared fuzz-test infrastructure: a small
// seeded corpus helper (SeedCorpus) plus a correctly-bounds-checked
// reference parser (ParseEnvelope) that FuzzTodo_TOOL_013 fuzzes.
// tools/quality/testdata/fuzzdefect holds a near-identical parser with one
// planted defect, used by TestTodo_TOOL_013 to prove the same seed corpus
// finds a real bug when one exists.
package fuzzkit

import (
	"fmt"
	"strconv"
	"strings"
)

// Envelope is a minimal stand-in for a canonical wire envelope: a tenant, a
// kind, a sequence number and a decimal amount, colon-delimited.
type Envelope struct {
	Tenant   string
	Kind     string
	Sequence int64
	Decimal  string // kept as a string; this fixture only checks shape, not arithmetic
}

// ParseEnvelope parses "tenant:kind:sequence:decimal". It returns a
// descriptive error for every malformed shape instead of panicking; the
// fuzz target in fuzzkit_test.go asserts exactly that property.
func ParseEnvelope(input string) (Envelope, error) {
	parts := strings.Split(input, ":")
	if len(parts) != 4 {
		return Envelope{}, fmt.Errorf("fuzzkit: expected 4 colon-delimited fields, got %d", len(parts))
	}

	tenant, kind, seqField, decimalField := parts[0], parts[1], parts[2], parts[3]
	if tenant == "" {
		return Envelope{}, fmt.Errorf("fuzzkit: tenant field is empty")
	}
	if kind == "" {
		return Envelope{}, fmt.Errorf("fuzzkit: kind field is empty")
	}
	if !safeIdentifier(tenant) || !safeIdentifier(kind) {
		return Envelope{}, fmt.Errorf("fuzzkit: tenant and kind must use ASCII letters, digits, '_' or '-'")
	}

	seq, err := strconv.ParseInt(seqField, 10, 64)
	if err != nil {
		return Envelope{}, fmt.Errorf("fuzzkit: invalid sequence field %q: %w", seqField, err)
	}
	if seq < 0 || strconv.FormatInt(seq, 10) != seqField {
		return Envelope{}, fmt.Errorf("fuzzkit: non-canonical sequence field %q", seqField)
	}

	// A decimal field is expected to contain exactly one '.'. Unlike the
	// planted-defect variant in testdata/fuzzdefect, this checks the split
	// length before indexing into it.
	decimalParts := strings.Split(decimalField, ".")
	if len(decimalParts) != 2 || decimalParts[0] == "" || decimalParts[1] == "" {
		return Envelope{}, fmt.Errorf("fuzzkit: invalid decimal field %q", decimalField)
	}
	for _, part := range decimalParts {
		for _, r := range part {
			if r < '0' || r > '9' {
				return Envelope{}, fmt.Errorf("fuzzkit: invalid decimal field %q", decimalField)
			}
		}
	}

	return Envelope{Tenant: tenant, Kind: kind, Sequence: seq, Decimal: decimalField}, nil
}

func safeIdentifier(value string) bool {
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return false
		}
	}
	return value != ""
}
