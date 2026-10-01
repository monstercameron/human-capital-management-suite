package workitem

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Compartment roles: assignment authority never implies artifact access.
const (
	RoleLeaveAdministrator = "leave-administrator"
	RoleManager            = "manager"
)

// Manager-visible operational facts: the only absence facts a manager
// ever receives. Everything else stays compartmented.
var managerFacts = map[string]bool{"absence-dates": true, "expected-return": true}

// Grant is one role's time-bounded artifact access under a purpose.
type Grant struct {
	ViewFields []string
	Artifacts  []string
	Download   bool
	UntilTick  int64
}

// AccessPolicy stores one work item's compartment, purpose and grants.
type AccessPolicy struct {
	PolicyID    string
	WorkItemID  string
	Compartment string
	Purpose     string
	Grants      map[string]Grant
}

// AccessDecision is the recorded receipt for one view, download or
// derived message. Every access records one: silent access never happens.
type AccessDecision struct {
	PolicyID  string
	Role      string
	Action    string
	Permitted bool
	Tick      int64
	Digest    string
}

func accessDigest(policyID, role, action string, permitted bool, tick int64) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{"workitem-access", policyID, role, action, fmt.Sprint(permitted, tick)}, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Authorize decides one field view or artifact download. Managers receive
// only permitted operational absence facts; restricted artifacts,
// filenames, metadata, thumbnails and OCR-derived facts refuse outside an
// explicit time-bounded grant, and expired grants refuse.
func Authorize(policy AccessPolicy, role, action string, nowTick int64) (AccessDecision, error) {
	decision := AccessDecision{PolicyID: policy.PolicyID, Role: role, Action: action, Tick: nowTick}
	permit := func() (AccessDecision, error) {
		decision.Permitted = true
		decision.Digest = accessDigest(policy.PolicyID, role, action, true, nowTick)
		return decision, nil
	}
	deny := func() (AccessDecision, error) {
		decision.Digest = accessDigest(policy.PolicyID, role, action, false, nowTick)
		return decision, fmt.Errorf("workitem: %s may not %s in compartment %s", role, action, policy.Compartment)
	}
	if role == RoleManager {
		field, isField := strings.CutPrefix(action, "view-field:")
		if isField && managerFacts[field] {
			return permit()
		}
		return deny()
	}
	grant, ok := policy.Grants[role]
	if !ok {
		return deny()
	}
	if nowTick > grant.UntilTick {
		return deny()
	}
	if target, isField := strings.CutPrefix(action, "view-field:"); isField {
		for _, field := range grant.ViewFields {
			if field == target {
				return permit()
			}
		}
		return deny()
	}
	if target, isArtifact := strings.CutPrefix(action, "view-artifact:"); isArtifact {
		for _, artifact := range grant.Artifacts {
			if artifact == target {
				return permit()
			}
		}
		return deny()
	}
	if action == "download" && grant.Download {
		return permit()
	}
	return deny()
}

// PolicyRegistry guards access policies for concurrent authorization.
type PolicyRegistry struct {
	mu       sync.Mutex
	policies map[string]AccessPolicy
	log      []AccessDecision
}

// NewPolicyRegistry starts an empty registry.
func NewPolicyRegistry() *PolicyRegistry {
	return &PolicyRegistry{policies: make(map[string]AccessPolicy)}
}

// Register publishes one policy. Duplicates refuse.
func (registry *PolicyRegistry) Register(policy AccessPolicy) error {
	if registry == nil {
		return fmt.Errorf("workitem: nil policy registry")
	}
	if strings.TrimSpace(policy.PolicyID) == "" {
		return fmt.Errorf("workitem: policy id is required")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, dup := registry.policies[policy.PolicyID]; dup {
		return fmt.Errorf("workitem: policy %s is already registered", policy.PolicyID)
	}
	registry.policies[policy.PolicyID] = policy
	return nil
}

// Authorize decides and records one access under the registered policy.
func (registry *PolicyRegistry) Authorize(policyID, role, action string, nowTick int64) (AccessDecision, error) {
	if registry == nil {
		return AccessDecision{}, fmt.Errorf("workitem: nil policy registry")
	}
	registry.mu.Lock()
	policy, ok := registry.policies[policyID]
	registry.mu.Unlock()
	if !ok {
		return AccessDecision{}, fmt.Errorf("workitem: unknown policy %s", policyID)
	}
	decision, err := Authorize(policy, role, action, nowTick)
	registry.mu.Lock()
	registry.log = append(registry.log, decision)
	registry.mu.Unlock()
	return decision, err
}

// Log returns recorded access decisions in order.
func (registry *PolicyRegistry) Log() []AccessDecision {
	if registry == nil {
		return nil
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	out := append([]AccessDecision(nil), registry.log...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tick != out[j].Tick {
			return out[i].Tick < out[j].Tick
		}
		return out[i].Digest < out[j].Digest
	})
	return out
}

// CompartmentItem is one queue/search/count entry with existence control.
type CompartmentItem struct {
	ID                  string
	RestrictedExistence bool
	Fields              map[string]string
}

// VisibleCount counts only items whose existence the role may know:
// restricted task existence never leaks through counts.
func VisibleCount(items []CompartmentItem, role string) int {
	count := 0
	for _, item := range items {
		if item.RestrictedExistence && role != RoleLeaveAdministrator {
			continue
		}
		count++
	}
	return count
}

// SearchFields returns only the fields the role may see: managers get
// permitted operational facts, never restricted content.
func SearchFields(item CompartmentItem, role string, grant Grant) map[string]string {
	out := make(map[string]string)
	if item.RestrictedExistence && role != RoleLeaveAdministrator {
		return out
	}
	if role == RoleManager {
		for field, value := range item.Fields {
			if managerFacts[field] {
				out[field] = value
			}
		}
		return out
	}
	allowed := make(map[string]bool, len(grant.ViewFields))
	for _, field := range grant.ViewFields {
		allowed[field] = true
	}
	for field, value := range item.Fields {
		if allowed[field] {
			out[field] = value
		}
	}
	return out
}
