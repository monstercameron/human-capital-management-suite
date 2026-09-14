package releaseevidence

import (
	"fmt"
	"sort"
)

// SkewKind classifies one disagreement between what runs and what was
// released.
type SkewKind string

// Skew kinds.
const (
	SkewBinary               SkewKind = "BINARY_SKEW"
	SkewSchemaDigest         SkewKind = "SCHEMA_DIGEST_SKEW"
	SkewMigrationBehind      SkewKind = "MIGRATION_BEHIND"
	SkewMigrationAhead       SkewKind = "MIGRATION_AHEAD"
	SkewDefinitionChanged    SkewKind = "DEFINITION_CHANGED"
	SkewDefinitionMissing    SkewKind = "DEFINITION_MISSING"
	SkewDefinitionUnreleased SkewKind = "DEFINITION_UNRELEASED"
)

// Runtime is what a cell observes about itself.
type Runtime struct {
	BinaryRevision string
	// SchemaDigest is the digest of the migration artifact the binary embeds.
	SchemaDigest string
	// AppliedSchemaVersion is the highest migration the database has applied.
	AppliedSchemaVersion int64
	// Definitions are the digests of the definitions the cell actually loads.
	Definitions map[string]string
}

// Skew is one finding.
type Skew struct {
	Kind     SkewKind `json:"kind"`
	Subject  string   `json:"subject,omitempty"`
	Released string   `json:"released"`
	Running  string   `json:"running"`
}

// Detect compares a running cell with its release. No findings means the cell
// runs exactly what was released.
func Detect(release Release, rt Runtime) ([]Skew, error) {
	if err := release.Verify(); err != nil {
		return nil, err
	}
	var out []Skew
	if rt.BinaryRevision != release.BinaryRevision {
		out = append(out, Skew{Kind: SkewBinary, Released: release.BinaryRevision, Running: rt.BinaryRevision})
	}
	if rt.SchemaDigest != release.SchemaDigest {
		out = append(out, Skew{Kind: SkewSchemaDigest, Released: release.SchemaDigest, Running: rt.SchemaDigest})
	}
	switch {
	case rt.AppliedSchemaVersion < release.SchemaVersion:
		out = append(out, Skew{Kind: SkewMigrationBehind, Released: fmt.Sprint(release.SchemaVersion), Running: fmt.Sprint(rt.AppliedSchemaVersion)})
	case rt.AppliedSchemaVersion > release.SchemaVersion:
		out = append(out, Skew{Kind: SkewMigrationAhead, Released: fmt.Sprint(release.SchemaVersion), Running: fmt.Sprint(rt.AppliedSchemaVersion)})
	}
	for name, released := range release.Definitions {
		running, ok := rt.Definitions[name]
		switch {
		case !ok:
			out = append(out, Skew{Kind: SkewDefinitionMissing, Subject: name, Released: released})
		case running != released:
			out = append(out, Skew{Kind: SkewDefinitionChanged, Subject: name, Released: released, Running: running})
		}
	}
	for name, running := range rt.Definitions {
		if _, ok := release.Definitions[name]; !ok {
			out = append(out, Skew{Kind: SkewDefinitionUnreleased, Subject: name, Running: running})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Subject < out[j].Subject
	})
	return out, nil
}
