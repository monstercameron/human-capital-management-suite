package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// Canonicalization profile identities. A digest is always prefixed with the
// profile that produced it, so bytes canonicalized under one profile can never
// be mistaken for another profile's digest.
const (
	planDigestProfile    = "hcmnext.workflow.CompiledWorkflow/v1"
	planDigestProfileV2  = "hcmnext.workflow.CompiledWorkflow/v2"
	mappingDigestProfile = "hcmnext.workflow.InputMappingSet/v1"
)

// canonicalDigest hashes a value under a profile. The canonical form is the
// encoding/json rendering of the normalized value: struct fields in
// declaration order, map keys sorted, no floating point anywhere in the plan.
// Normalization — sorting nodes, edges, mappings and summaries — happens
// before this point, so the same definition always canonicalizes identically.
func canonicalDigest(profile string, v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		// A compiled plan is plain data, so this is unreachable. If it ever
		// happens, produce bytes that cannot collide with a real plan digest
		// rather than silently returning an empty digest.
		b = []byte("unencodable:" + err.Error())
	}
	h := sha256.New()
	h.Write([]byte(profile))
	h.Write([]byte{0})
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

// computePlanDigest is the compiled plan's content identity. The unexported
// digest field is excluded from the canonical bytes, so recomputing over a
// plan reproduces the digest it was minted with.
func computePlanDigest(p *CompiledWorkflow) string {
	if p == nil {
		return canonicalDigest(planDigestProfile, p)
	}
	switch p.SchemaVersion() {
	case 1:
		return canonicalDigest(planDigestProfile, p)
	case CurrentIRSchemaVersion:
		return canonicalDigest(planDigestProfileV2, p)
	default:
		return canonicalDigest("hcmnext.workflow.CompiledWorkflow/unsupported", p)
	}
}

// mappingDigest is the input-snapshot identity a DECISION or TRANSFORM records
// with its evaluation.
func mappingDigest(mappings []CompiledMapping) string {
	return canonicalDigest(mappingDigestProfile, mappings)
}

// taintLineage renders a transform's declared provenance in a stable order:
// which input contributed at which level, which pinned lookups it consulted
// and which sanitizer receipt (if any) authorized a downgrade.
func taintLineage(t *TransformSpec) []string {
	if t == nil {
		return nil
	}
	out := make([]string, 0, len(t.InputTaint)+len(t.Lookups)+1)
	for _, in := range t.InputTaint {
		out = append(out, "input:"+in.Source+"="+string(in.Level))
	}
	for _, l := range t.Lookups {
		out = append(out, "lookup:"+l.Ref+"@"+l.SnapshotDigest)
	}
	sort.Strings(out)
	if t.SanitizerReceiptRef != "" {
		out = append(out, "sanitizer:"+t.SanitizerReceiptRef)
	}
	out = append(out, "output="+string(t.OutputTaint))
	return out
}
