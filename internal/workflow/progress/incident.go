package progress

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/opsmeta"
)

// Route is the authoritative incident routing policy for stuck workflows. It
// is application policy, never derived from a finding, so a detection cannot
// grant itself an owner.
type Route struct {
	PrimaryOwner   string
	SecondaryRoute string
	StormLimit     int
	StormWindow    time.Duration
}

// ErrNoFindings reports an attempt to raise an incident for a healthy instance.
var ErrNoFindings = errors.New("progress: no missed expectation to raise")

// incidentNamespace keys stuck-workflow incidents apart from every other
// alert source in the operations store.
const incidentNamespace = "workflow-progress:v1"

// IncidentKey is the operations-store key for one stuck condition: the
// instance plus the identities of the expectations it missed. The same
// condition detected again produces the same key, so the operations store's
// unique key links the repeat to the incident already open instead of opening
// another; a genuinely different condition on the same instance is a new key.
func IncidentKey(instanceID string, findings []Finding) string {
	return incidentNamespace + ":" + instanceID + ":" + identityDigest(findings)[:24]
}

func identityDigest(findings []Finding) string {
	ids := make([]string, 0, len(findings))
	for _, f := range findings {
		ids = append(ids, f.Identity())
	}
	sum := sha256.Sum256([]byte(strings.Join(ids, "\n")))
	return hex.EncodeToString(sum[:])
}

// severityFor ranks a stuck condition: a poison node or an instance nothing
// can advance is SEV2; a missed timer, abandoned lease or missed escalation
// is SEV3; the rest are SEV4.
func severityFor(findings []Finding) string {
	sev := "SEV4"
	for _, f := range findings {
		switch f.Kind {
		case KindPoisonNode, KindNoProgressMechanism:
			return "SEV2"
		case KindTimerOverdue, KindLeaseAbandoned, KindSLAEscalationMissed:
			sev = "SEV3"
		}
	}
	return sev
}

// declaredAt is the earliest instant any finding was due, or the instance's
// last recorded progress when no finding carries a due instant. It is derived
// from the durable rows, never from the evaluation clock, so re-detecting the
// same condition reproduces it exactly.
func declaredAt(s Snapshot, findings []Finding) time.Time {
	var at time.Time
	for _, f := range findings {
		if !f.DueAt.IsZero() && (at.IsZero() || f.DueAt.Before(at)) {
			at = f.DueAt
		}
	}
	if at.IsZero() {
		at = s.Instance.LastRecordedAt
	}
	if at.IsZero() {
		at = s.Instance.CreatedAt
	}
	return at.UTC().Truncate(time.Microsecond)
}

// RaiseIncident opens, or links to the already-open, operational incident for
// one stuck instance. It writes only the operations store's
// operational_incident row through opsmeta.RouteAlert and nothing else: no
// runtime, work item or business row is touched. created is false when the
// same condition was already raised.
func RaiseIncident(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, s Snapshot, findings []Finding, route Route) (opsmeta.AlertIncidentResult, error) {
	if len(findings) == 0 {
		return opsmeta.AlertIncidentResult{}, ErrNoFindings
	}
	if tenant == uuid.Nil || tenant.String() != s.Instance.TenantID {
		return opsmeta.AlertIncidentResult{}, fmt.Errorf("progress: finding tenant %q does not match %s", s.Instance.TenantID, tenant)
	}
	key := IncidentKey(s.Instance.InstanceID, findings)
	kinds := make([]string, 0, len(findings))
	nodes := map[string]bool{}
	for _, f := range findings {
		kinds = append(kinds, string(f.Kind))
		if f.NodeID != "" {
			nodes[f.NodeID] = true
		}
	}
	nodeList := make([]string, 0, len(nodes))
	for n := range nodes {
		nodeList = append(nodeList, n)
	}
	scope, err := json.Marshal(map[string]any{
		"source":        incidentNamespace,
		"workflow_id":   s.Instance.WorkflowID,
		"instance_ref":  s.Instance.InstanceID,
		"runtime_state": s.Instance.RuntimeStatus,
		"finding_kinds": kinds,
		"node_ids":      sortedStrings(nodeList),
	})
	if err != nil {
		return opsmeta.AlertIncidentResult{}, fmt.Errorf("progress: incident scope: %w", err)
	}
	return opsmeta.RouteAlert(ctx, tx, opsmeta.AlertIncident{
		TenantID:       tenant,
		IncidentID:     uuid.NewSHA1(uuid.NameSpaceOID, []byte(tenant.String()+"|"+key)),
		IncidentKey:    key,
		Severity:       severityFor(findings),
		Scope:          scope,
		CorrelationKey: s.Instance.CorrelationID,
		EvidenceDigest: identityDigest(findings),
		DeclaredAt:     declaredAt(s, findings),
		PrimaryOwner:   route.PrimaryOwner,
		SecondaryRoute: route.SecondaryRoute,
		StormLimit:     route.StormLimit,
		StormWindow:    route.StormWindow,
	})
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
