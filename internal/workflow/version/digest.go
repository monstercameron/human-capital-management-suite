package version

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// Canonicalization profile identities. A digest is always prefixed with the
// profile that produced it, so bytes canonicalized under one profile can
// never be mistaken for another profile's digest. This mirrors
// internal/workflow/digest.go and internal/workflow/frontier/digest.go
// exactly: a profile-prefixed sha256 over the encoding/json rendering, with
// struct fields in declaration order and no unordered collection anywhere in
// the digested value.
const (
	definitionDigestProfile = "hcmnext.workflow.version.Definition/v1"
	recordDigestProfile     = "hcmnext.workflow.version.CompiledVersion/v1"
)

// canonicalDigest hashes a value under a profile.
func canonicalDigest(profile string, v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		// Every value this package digests is plain data assembled by this
		// package, so this is unreachable. If it ever happens, produce bytes
		// that cannot collide with a real digest rather than silently
		// returning an empty one.
		b = []byte("unencodable:" + err.Error())
	}
	h := sha256.New()
	h.Write([]byte(profile))
	h.Write([]byte{0})
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

// computeDefinitionDigest is a draft workflow definition's content identity.
// workflow.Definition carries only exported fields, so no digest field needs
// excluding the way a compiled plan's own digest does.
func computeDefinitionDigest(def workflow.Definition) string {
	return canonicalDigest(definitionDigestProfile, def)
}

// DefinitionDigest returns the canonical content identity used by published
// versions for a source workflow definition. Draft authoring uses the same
// function so an imported template can prove byte-for-byte semantic parity
// with the immutable definition it is replacing.
func DefinitionDigest(def workflow.Definition) string {
	return computeDefinitionDigest(def)
}

// recordIdentity is the subset of CompiledVersion content this package
// digests. Status is excluded for the reason CompiledVersion.Digest
// documents: it is governed lifecycle bookkeeping on a fixed artifact, not
// part of the artifact's own identity.
type recordIdentity struct {
	WorkflowID         string
	DefinitionVersion  uint32
	SemanticVersion    string
	DefinitionDigest   string
	CompiledPlanDigest string
	CompilerVersion    string
	CanonicalPlanBytes []byte
	ToolVersions       map[string]string
	FixtureRefs        []string
	DependsOn          []VersionRef
	PublishedAt        time.Time
	PublishedBy        string
}

// computeRecordDigest hashes a CompiledVersion's identity-bearing content.
func computeRecordDigest(v CompiledVersion) string {
	id := recordIdentity{
		WorkflowID:         v.WorkflowID,
		DefinitionVersion:  v.DefinitionVersion,
		SemanticVersion:    v.SemanticVersion,
		DefinitionDigest:   v.DefinitionDigest,
		CompiledPlanDigest: v.CompiledPlanDigest,
		CompilerVersion:    v.CompilerVersion,
		CanonicalPlanBytes: v.CanonicalPlanBytes,
		ToolVersions:       v.ToolVersions,
		FixtureRefs:        v.FixtureRefs,
		DependsOn:          v.DependsOn,
		PublishedAt:        v.PublishedAt,
		PublishedBy:        v.PublishedBy,
	}
	return canonicalDigest(recordDigestProfile, id)
}

// canonicalPlanBytes renders the compiled plan as deterministic, indented
// JSON: the bytes a runtime or an auditor can replay without the compiler or
// the capability registry that produced them.
func canonicalPlanBytes(plan *workflow.CompiledWorkflow) ([]byte, error) {
	b, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
