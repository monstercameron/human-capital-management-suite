package rolloutplan

import (
	"fmt"
	"strings"
)

// Service/configuration rollout conformance (ROLLOUT-008) proves every
// rollout implementation honors identical activation invariants — pinned
// target, fenced epoch, health gate, pause semantics, rollback plan and
// explanation — while retaining its artifact-specific activation hook. A
// binary rolls out as a service; configuration and semantic packs roll
// out through their own hooks under the same rules.
//
// The check is pure: it persists no rows, emits no events and mutates no
// input, so a seeded defect can only surface as a rejection.

// ConformArtifactKind names the rollout implementation under test.
type ConformArtifactKind string

// Rollout implementations.
const (
	ConformService       ConformArtifactKind = "SERVICE"
	ConformConfiguration ConformArtifactKind = "CONFIGURATION"
	ConformSemantic      ConformArtifactKind = "SEMANTIC"
)

// Valid reports whether k is a declared rollout implementation.
func (k ConformArtifactKind) Valid() bool {
	return k == ConformService || k == ConformConfiguration || k == ConformSemantic
}

// hookPrefix is the activation-hook namespace each implementation owns.
func (k ConformArtifactKind) hookPrefix() string {
	switch k {
	case ConformService:
		return "service:"
	case ConformConfiguration:
		return "config:"
	default:
		return "semantic:"
	}
}

// ConformSnapshot is one implementation's activation candidate.
type ConformSnapshot struct {
	Kind            ConformArtifactKind
	Target          string
	Version         string
	Epoch           uint64
	Healthy         bool
	Paused          bool
	RollbackVersion string
	Explain         string
	ActivationHook  string
}

// Rejection is one conformance refusal carrying the offending field, the
// artifact state and the version it refused, so a seeded defect is
// attributable without narration.
type Rejection struct {
	Code    string
	Field   string
	State   string
	Version string
}

func (r *Rejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s", r.Code, r.Field, r.State, r.Version)
}

// AsRejection reports whether err is a conformance rejection.
func AsRejection(err error) (*Rejection, bool) {
	r, ok := err.(*Rejection)
	return r, ok && r != nil
}

func reject(field, state, version string) *Rejection {
	return &Rejection{Code: "ROLLOUT_008_REJECTED", Field: field, State: state, Version: version}
}

// CheckConformance verifies the identical activation invariants for every
// implementation. The first violated invariant wins, so a rejection names
// exactly one field.
func CheckConformance(snap ConformSnapshot) error {
	state := snap.State()
	version := snap.Version
	if version == "" {
		version = "unversioned"
	}
	switch {
	case !snap.Kind.Valid():
		return reject("kind", state, version)
	case strings.TrimSpace(snap.Target) == "":
		return reject("target", state, snap.Version)
	case strings.TrimSpace(snap.Version) == "" || snap.Version != strings.TrimSpace(snap.Version):
		return reject("version", state, snap.Version)
	case snap.Epoch == 0:
		return reject("epoch", state, snap.Version)
	case !snap.Healthy:
		return reject("healthy", state, snap.Version)
	case snap.Paused:
		return reject("paused", state, snap.Version)
	case strings.TrimSpace(snap.RollbackVersion) == "":
		return reject("rollback_version", state, snap.Version)
	case snap.RollbackVersion == snap.Version:
		return reject("rollback_version", state, snap.Version)
	case strings.TrimSpace(snap.Explain) == "":
		return reject("explain", state, snap.Version)
	case !strings.HasPrefix(snap.ActivationHook, snap.Kind.hookPrefix()):
		return reject("activation_hook", state, snap.Version)
	}
	return nil
}

// State renders the artifact state the verdict was reached in.
func (s ConformSnapshot) State() string {
	if s.Paused {
		return "PAUSED"
	}
	if !s.Healthy {
		return "UNHEALTHY"
	}
	return "READY"
}
