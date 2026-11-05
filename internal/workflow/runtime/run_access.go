// Run access scopes each run's context reads, inspector, timeline, notes and
// audit behind one classification and participant wall. Investigations and
// medical leave data are visible only to named participants: a definition's
// maximum classification alone never admits anyone to a walled run.
//
// The policy is an immutable value. A run starts open (visible to the
// workflow read scope, like today) and may be walled once; after that the
// wall only narrows — participants are removed, never added, and the
// classification never moves. Every denial is one non-disclosing refusal,
// whether the viewer is missing, revoked, removed or right for the wrong
// purpose.
//
// Durable carry lands with WF-EXT-007: the start, revalidation and terminal
// contracts it is rebuilding are what will record and reload the policy.
// Until then a bound policy travels with the run through the read edges:
// [BoundRunAccess] carries the run identity and policy digest next to the
// loaded instance, and the inspector adapter builds its views from the
// decision this file produces.
package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// Run-access refusal code. It lives here rather than in errors.go so the
// whole run-access contract ships in one hunk.
const CodeRunNotDisclosed = "RUN_NOT_DISCLOSED"

// Run-access sentinels. Classify with [errors.Is].
var (
	// ErrRunNotDisclosed is the single non-disclosing denial: missing,
	// forbidden, revoked, removed and wrong-purpose reads refuse identically,
	// so no caller learns which wall stopped them.
	ErrRunNotDisclosed = errors.New("runtime: run access denied")
	// ErrRunAccessInvalid marks a malformed authorization input: an empty
	// classification or purpose, an unknown surface, a duplicated or empty
	// participant, or a narrowing that names nobody on the wall.
	ErrRunAccessInvalid = errors.New("runtime: invalid run access policy")
)

// RunSurface is one governed read surface of a run. The set is closed: an
// undeclared surface fails closed rather than inheriting some other
// surface's ruling.
type RunSurface string

// The governed read surfaces.
const (
	SurfaceContextReads RunSurface = "CONTEXT"
	SurfaceInspector    RunSurface = "INSPECTOR"
	SurfaceTimeline     RunSurface = "TIMELINE"
	SurfaceNotes        RunSurface = "NOTES"
	SurfaceAudit        RunSurface = "AUDIT"
)

// RunSurfaces returns the governed surfaces in a fixed order.
func RunSurfaces() []RunSurface {
	return []RunSurface{
		SurfaceContextReads,
		SurfaceInspector,
		SurfaceTimeline,
		SurfaceNotes,
		SurfaceAudit,
	}
}

func (s RunSurface) valid() bool {
	switch s {
	case SurfaceContextReads, SurfaceInspector, SurfaceTimeline, SurfaceNotes, SurfaceAudit:
		return true
	default:
		return false
	}
}

// RunParticipant is one named principal on a run's wall. The role is
// recorded for audit and explanation; the wall itself is binary — a
// participant sees every surface, a non-participant sees none.
type RunParticipant struct {
	Principal string `json:"principal"`
	Role      string `json:"role"`
}

// RunAccessPolicy is one run's classification and participant wall. The zero
// value is invalid and fails closed. Policies are immutable: [RunAccessPolicy.Wall],
// [RunAccessPolicy.Narrow] and [RunAccessPolicy.Revoke] return new policies.
type RunAccessPolicy struct {
	classification string
	purpose        string
	participants   []RunParticipant
	revoked        []string
}

// NewOpenRunAccess starts an open run policy: visible to the workflow read
// scope, as runs are today. Classification is exact and purpose-bound from
// the start so walling later never invents them.
func NewOpenRunAccess(classification, purpose string) (RunAccessPolicy, error) {
	p := RunAccessPolicy{classification: classification, purpose: purpose}
	if err := p.Validate(); err != nil {
		return RunAccessPolicy{}, err
	}
	return p, nil
}

// Validate reports whether the policy is usable on its own terms.
func (p RunAccessPolicy) Validate() error {
	if strings.TrimSpace(p.classification) == "" || strings.TrimSpace(p.purpose) == "" {
		return fmt.Errorf("%w: classification and purpose are required", ErrRunAccessInvalid)
	}
	seen := make(map[string]struct{}, len(p.participants))
	for _, m := range p.participants {
		if strings.TrimSpace(m.Principal) == "" {
			return fmt.Errorf("%w: participant principal is required", ErrRunAccessInvalid)
		}
		if _, dup := seen[m.Principal]; dup {
			return fmt.Errorf("%w: participant %s is named twice", ErrRunAccessInvalid, m.Principal)
		}
		seen[m.Principal] = struct{}{}
	}
	for _, r := range p.revoked {
		if strings.TrimSpace(r) == "" {
			return fmt.Errorf("%w: revoked principal is required", ErrRunAccessInvalid)
		}
		if _, active := seen[r]; active {
			return fmt.Errorf("%w: participant %s is both active and revoked", ErrRunAccessInvalid, r)
		}
	}
	return nil
}

// Classification returns the run's exact classification. It never moves:
// narrowing removes participants, it never re-labels the run.
func (p RunAccessPolicy) Classification() string { return p.classification }

// Purpose returns the purpose every read must state.
func (p RunAccessPolicy) Purpose() string { return p.purpose }

// Participants returns the active wall in principal order.
func (p RunAccessPolicy) Participants() []RunParticipant {
	return append([]RunParticipant(nil), p.participants...)
}

// RevokedPrincipals returns the removed principals in sorted order.
func (p RunAccessPolicy) RevokedPrincipals() []string {
	return append([]string(nil), p.revoked...)
}

// Walled reports whether the run is walled. A wall narrowed to zero members
// stays walled: an empty wall admits nobody, and reopening it would widen.
func (p RunAccessPolicy) Walled() bool { return len(p.participants) > 0 || len(p.revoked) > 0 }

// ParticipantRole returns the role of an active participant. Removed,
// revoked and never-named principals all report false.
func (p RunAccessPolicy) ParticipantRole(principal string) (string, bool) {
	for _, m := range p.participants {
		if m.Principal == principal {
			return m.Role, true
		}
	}
	return "", false
}

// Digest is the stable policy version stamped wherever the run's access is
// decided or audited.
func (p RunAccessPolicy) Digest() string {
	body, _ := json.Marshal(struct {
		Classification string           `json:"classification"`
		Purpose        string           `json:"purpose"`
		Participants   []RunParticipant `json:"participants"`
		Revoked        []string         `json:"revoked"`
	}{p.classification, p.purpose, p.participants, p.revoked})
	sum := sha256.Sum256(append([]byte("hcmnext.workflow.RunAccessPolicy/v1\n"), body...))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// deny builds the one non-disclosing denial. Code, detail and cause are
// constant: denials never vary by surface, viewer or cause.
func deny() error {
	return wrap(CodeRunNotDisclosed, "", "", ErrRunNotDisclosed, "run is not disclosed to this caller")
}

// Authorize is the single decision point for every governed read surface.
// An open run admits any viewer stating the run's purpose; a walled run
// admits only active participants stating it. Anything else — including an
// unusable policy or an undeclared surface — is denied.
func (p RunAccessPolicy) Authorize(viewer, purpose string, surface RunSurface) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if !surface.valid() {
		return fmt.Errorf("%w: surface %q is not governed", ErrRunAccessInvalid, string(surface))
	}
	if p.purpose != purpose {
		return deny()
	}
	if !p.Walled() {
		if strings.TrimSpace(viewer) == "" {
			return deny()
		}
		return nil
	}
	if strings.TrimSpace(viewer) == "" {
		return deny()
	}
	if _, ok := p.ParticipantRole(viewer); !ok {
		return deny()
	}
	return nil
}

// Wall raises the wall on an open run. It is refused on a walled run: the
// wall only narrows, so a second wall — which could re-admit the removed —
// never replaces the first.
func (p RunAccessPolicy) Wall(participants ...RunParticipant) (RunAccessPolicy, error) {
	if err := p.Validate(); err != nil {
		return RunAccessPolicy{}, err
	}
	if p.Walled() {
		return RunAccessPolicy{}, fmt.Errorf("%w: run is already walled", ErrRunAccessInvalid)
	}
	if len(participants) == 0 {
		return RunAccessPolicy{}, fmt.Errorf("%w: a wall names at least one participant", ErrRunAccessInvalid)
	}
	next := RunAccessPolicy{classification: p.classification, purpose: p.purpose}
	next.participants = append([]RunParticipant(nil), participants...)
	sort.Slice(next.participants, func(i, j int) bool { return next.participants[i].Principal < next.participants[j].Principal })
	if err := next.Validate(); err != nil {
		return RunAccessPolicy{}, err
	}
	return next, nil
}

// Narrow removes participants from a walled run and records them as revoked
// so they can never be re-added. Every named principal must be active:
// narrowing a stranger fails closed rather than silently passing.
func (p RunAccessPolicy) Narrow(principals ...string) (RunAccessPolicy, error) {
	if err := p.Validate(); err != nil {
		return RunAccessPolicy{}, err
	}
	if !p.Walled() {
		return RunAccessPolicy{}, fmt.Errorf("%w: an open run has no wall to narrow", ErrRunAccessInvalid)
	}
	drop := make(map[string]struct{}, len(principals))
	for _, name := range principals {
		if _, ok := p.ParticipantRole(name); !ok {
			return RunAccessPolicy{}, fmt.Errorf("%w: %s is not an active participant", ErrRunAccessInvalid, name)
		}
		drop[name] = struct{}{}
	}
	next := RunAccessPolicy{
		classification: p.classification,
		purpose:        p.purpose,
		revoked:        append([]string(nil), p.revoked...),
	}
	for _, m := range p.participants {
		if _, remove := drop[m.Principal]; remove {
			next.revoked = append(next.revoked, m.Principal)
			continue
		}
		next.participants = append(next.participants, m)
	}
	sort.Strings(next.revoked)
	return next, nil
}

// Revoke removes one participant. It is [RunAccessPolicy.Narrow] for the
// common single-removal case, named after the case-compartment operation it
// mirrors.
func (p RunAccessPolicy) Revoke(principal string) (RunAccessPolicy, error) {
	return p.Narrow(principal)
}

// BoundRunAccess is a policy bound to the run it governs: the identity the
// read edges carry next to the loaded instance until the durable store lands
// with WF-EXT-007.
type BoundRunAccess struct {
	Policy     RunAccessPolicy
	TenantID   uuid.UUID
	InstanceID uuid.UUID
	Digest     string
}

// Bind attaches the policy to one run. The digest is snapshotted so a read
// edge can prove it decided under the version it claims.
func (p RunAccessPolicy) Bind(tenantID, instanceID uuid.UUID) (BoundRunAccess, error) {
	if err := p.Validate(); err != nil {
		return BoundRunAccess{}, err
	}
	if tenantID == uuid.Nil || instanceID == uuid.Nil {
		return BoundRunAccess{}, fmt.Errorf("%w: tenant and instance are required", ErrRunAccessInvalid)
	}
	return BoundRunAccess{Policy: p, TenantID: tenantID, InstanceID: instanceID, Digest: p.Digest()}, nil
}

// Authorize decides under the bound policy.
func (b BoundRunAccess) Authorize(viewer, purpose string, surface RunSurface) error {
	return b.Policy.Authorize(viewer, purpose, surface)
}
