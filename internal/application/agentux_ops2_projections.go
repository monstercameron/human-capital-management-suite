package application

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/ownerops"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// AgentOwnerRunProjection keeps the owner-authorized operational view joined
// to the durable identity and state-change coordinates used by the page.
type AgentOwnerRunProjection struct {
	View            ownerops.TaskView
	OwnerID         string
	RequestedBy     string
	Location        string
	ConversationID  string
	ViewerDirect    bool
	FailureGate     string
	FailureOwner    string
	FailureLocation string
	StartedAt       time.Time
	UpdatedAt       time.Time
	Deadline        time.Time
	LeaseUntil      time.Time
}

type AgentOwnerRunSource interface {
	DashboardProjection(context.Context, *trust.Principal, ownerops.Audience) ([]AgentOwnerRunProjection, error)
}

// AgentControlIdentity is the readable identity for one exact bound agent
// version. It is resolved only after the operational projection authorizes the
// run for the current viewer.
type AgentControlIdentity struct {
	Name      string
	OwnerName string
}

type AgentControlIdentitySource interface {
	ResolveAgentControlIdentity(context.Context, values.TenantId, string, string) (AgentControlIdentity, error)
}

// AgentFallbackControlIdentities keeps the operational projection available
// when the optional persona catalog reader is not composed. It derives a
// humanized label only from the already-authorized agent identifier and never
// exposes that identifier as technical text in the visible list.
type AgentFallbackControlIdentities struct{}

func (AgentFallbackControlIdentities) ResolveAgentControlIdentity(_ context.Context, _ values.TenantId, agentID, _ string) (AgentControlIdentity, error) {
	// The platform general agent is intentionally not a persona-catalog entry.
	// Keep its product name stable rather than deriving prose from its technical
	// identifier when an owner is inspecting an otherwise unlisted run.
	name := ""
	switch strings.TrimSpace(agentID) {
	case "general-agent", "hcmnext.platform.general":
		name = "General agent"
	default:
		name = productui.DisplayLabel(agentID)
	}
	if strings.TrimSpace(name) == "" {
		name = "Agent"
	}
	return AgentControlIdentity{Name: name}, nil
}

// DashboardProjection enriches only task IDs already admitted by ownerops.
// The second store read supplies durable state timing and owner identity; it
// never widens the admitted task set.
func (s *AgentOwnerOperations) DashboardProjection(ctx context.Context, principal *trust.Principal, audience ownerops.Audience) ([]AgentOwnerRunProjection, error) {
	views, err := s.Dashboard(ctx, principal, audience)
	if err != nil {
		return nil, err
	}
	if s == nil || s.store == nil || principal == nil {
		return nil, ownerops.ErrDenied
	}
	records, err := s.store.Records(ctx, string(principal.Tenant()), "")
	if err != nil {
		return nil, err
	}
	byID := make(map[string]AgentOwnerRunProjection, len(views))
	for _, record := range records {
		byID[record.Task.ID] = AgentOwnerRunProjection{OwnerID: record.OwnerID, RequestedBy: record.Task.UserID, StartedAt: record.Task.CreatedAt.UTC(), UpdatedAt: record.Task.UpdatedAt.UTC()}
	}
	result := make([]AgentOwnerRunProjection, 0, len(views))
	for _, view := range views {
		projection, ok := byID[view.TaskID]
		if !ok || projection.OwnerID == "" || projection.StartedAt.IsZero() || projection.UpdatedAt.IsZero() || projection.UpdatedAt.Before(projection.StartedAt) {
			return nil, ownerops.ErrInvalid
		}
		projection.View = view
		result = append(result, projection)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].UpdatedAt.After(result[j].UpdatedAt) })
	return result, nil
}

// AgentPersonaControlIdentities reads the immutable persona catalog and the
// same core directory vocabulary used by Agent setup.
type AgentPersonaControlIdentities struct {
	Personas  *agentpersonastore.Store
	Directory PersonaCatalogDirectory
}

func (s AgentPersonaControlIdentities) ResolveAgentControlIdentity(ctx context.Context, tenant values.TenantId, agentID, version string) (AgentControlIdentity, error) {
	if s.Personas == nil || s.Directory == nil || ctx == nil || tenant.Validate() != nil || strings.TrimSpace(agentID) == "" || strings.TrimSpace(version) == "" {
		return AgentControlIdentity{}, ErrPersonaCatalogDirectoryUnavailable
	}
	store, err := s.Personas.ForTenant(ctx, tenant)
	if err != nil {
		return AgentControlIdentity{}, err
	}
	entries, err := store.ListCatalog(ctx)
	if err != nil {
		return AgentControlIdentity{}, err
	}
	var match *agentpersonastore.CatalogEntry
	for index := range entries {
		entry := &entries[index]
		if entry.Version.PersonaID != agentID || (entry.Version.AgentVersion != version && strconv.FormatInt(entry.Version.Version, 10) != version) {
			continue
		}
		if match != nil {
			return AgentControlIdentity{}, fmt.Errorf("ambiguous agent identity %s/%s", agentID, version)
		}
		match = entry
	}
	if match == nil || strings.TrimSpace(match.Version.DisplayName) == "" || strings.TrimSpace(match.BusinessOwner) == "" {
		return AgentControlIdentity{}, agentpersonastore.ErrNotFound
	}
	owner, err := s.Directory.ResolvePersonaCatalogTarget(ctx, tenant, match.BusinessOwner)
	if err != nil {
		return AgentControlIdentity{}, err
	}
	if owner.ID != match.BusinessOwner || strings.TrimSpace(owner.Label) == "" {
		return AgentControlIdentity{}, ErrPersonaCatalogDirectoryUnavailable
	}
	return AgentControlIdentity{Name: strings.TrimSpace(match.Version.DisplayName), OwnerName: strings.TrimSpace(owner.Label)}, nil
}

func mergeAgentOwnerRunProjections(existing []AgentOwnerRunProjection, incoming []AgentOwnerRunProjection) []AgentOwnerRunProjection {
	indices := make(map[string]int, len(existing))
	for index := range existing {
		indices[existing[index].View.TaskID] = index
	}
	for _, row := range incoming {
		if index, ok := indices[row.View.TaskID]; ok {
			existing[index].View.CanPause = existing[index].View.CanPause || row.View.CanPause
			continue
		}
		indices[row.View.TaskID] = len(existing)
		existing = append(existing, row)
	}
	return existing
}

func projectAgentControlRun(projection AgentOwnerRunProjection, identity AgentControlIdentity) productui.AgentControlRun {
	view := projection.View
	duration := projection.UpdatedAt.Sub(projection.StartedAt)
	state := view.State
	failureGate := projection.FailureGate
	if agentRunStoppedResponding(state, projection.Deadline, projection.LeaseUntil) {
		state = "STOPPED_RESPONDING"
		if failureGate == "" {
			failureGate = "deadline"
		}
	}
	row := productui.AgentControlRun{ID: view.TaskID, AgentID: view.AgentID, ConversationID: projection.ConversationID, ViewerDirect: projection.ViewerDirect, Name: identity.Name, Revision: view.Revision, Version: view.Version, Installation: view.InstallationID, State: state, Since: projection.UpdatedAt.UTC().Format(time.RFC3339), Started: projection.StartedAt.UTC().Format(time.RFC3339), Duration: duration.Round(time.Second).String(), RequestedBy: agentControlSubjectLabel(projection.RequestedBy), Location: projection.Location, Failure: view.FailureCode, FailureGate: failureGate, FailureOwner: projection.FailureOwner, FailurePlace: projection.FailureLocation, Spend: fmt.Sprintf("%d", view.SpendMicros), Actions: []string{}, Denials: []string{}, Citations: []string{}, Evals: []string{}}
	if view.CanPause && state != "STOPPED_RESPONDING" {
		row.Actions = append(row.Actions, "pause")
	}
	for _, step := range view.Steps {
		row.Denials = append(row.Denials, step.DenialCodes...)
		if step.WakeLag > 0 {
			row.QueueLag = step.WakeLag.String()
		}
	}
	return row
}

func agentRunStoppedResponding(state string, deadline, leaseUntil time.Time) bool {
	if !strings.EqualFold(strings.TrimSpace(state), "RUNNING") || deadline.IsZero() || leaseUntil.IsZero() {
		return false
	}
	now := time.Now().UTC()
	return !leaseUntil.After(now) && !deadline.After(now)
}

func agentControlSubjectLabel(subject string) string {
	parts := strings.Split(strings.TrimSpace(subject), "-")
	if len(parts) >= 4 {
		if _, err := strconv.ParseUint(parts[1], 10, 64); err == nil {
			parts = parts[2:]
		}
	}
	return productui.DisplayLabel(strings.Join(parts, "-"))
}

var _ AgentOwnerRunSource = (*AgentOwnerOperations)(nil)
var _ AgentControlIdentitySource = AgentPersonaControlIdentities{}
var _ AgentControlIdentitySource = AgentFallbackControlIdentities{}
