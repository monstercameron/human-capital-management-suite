package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/performance"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var (
	ErrReviewParticipantsDenied  = errors.New("application: review participants read denied")
	ErrReviewParticipantsBinding = errors.New("application: review participant worker binding unavailable")
)

// ReviewParticipantWorkerResolver binds an authenticated principal to the
// exact worker identity used by a frozen performance population. Production
// composition resolves this from the tenant's authoritative Journey worker
// directory; it must never use a display label or request field.
type ReviewParticipantWorkerResolver func(context.Context, *trust.Principal) (string, error)

// ReviewParticipantGraphTenantResolver maps the authenticated tenant key to
// the canonical storage tenant used by the performance repository. The app
// tenant key can be a slug; the PostgreSQL adapter requires its UUID mapping.
type ReviewParticipantGraphTenantResolver func(values.TenantId) (performance.TenantID, error)

// ReviewParticipantsReadService discloses only the graph edges incident to
// the authenticated worker. Graphs are selected from tenant-scoped storage,
// and page authorization is derived from the admitted principal's durable
// role snapshot.
type ReviewParticipantsReadService struct {
	Graphs             performance.Store
	Roles              roleaccess.Store
	ResolveWorker      ReviewParticipantWorkerResolver
	ResolveGraphTenant ReviewParticipantGraphTenantResolver
}

// Read returns an empty cycle list when the authenticated worker has no open
// review cycle. That is the same result the graph store returns for a cycle
// whose frozen graph does not contain this worker.
func (s ReviewParticipantsReadService) Read(ctx context.Context, principal *trust.Principal) (productui.ReviewParticipantsProjection, error) {
	if ctx == nil || principal == nil || principal.Subject() == "" || principal.Tenant().String() == "" || s.Graphs == nil || s.Roles == nil || s.ResolveWorker == nil || s.ResolveGraphTenant == nil {
		return productui.ReviewParticipantsProjection{}, ErrReviewParticipantsDenied
	}
	snapshot, err := s.Roles.Load(ctx, principal.Tenant(), principal.OrganizationScopeID())
	if err != nil {
		return productui.ReviewParticipantsProjection{}, fmt.Errorf("application: load review-participant access: %w", err)
	}
	roles := roleaccess.AssignedRoles(snapshot, principal.Subject(), principal.Roles())
	permissions := roleaccess.EffectivePagePermissions(snapshot, roles)
	if !roleaccess.CanPageAction(permissions, string(productui.PageReviewParticipants), roleaccess.ActionView) {
		return productui.ReviewParticipantsProjection{}, ErrReviewParticipantsDenied
	}
	workerID, err := s.ResolveWorker(ctx, principal)
	if err != nil {
		if errors.Is(err, ErrReviewParticipantsBinding) {
			return productui.ReviewParticipantsProjection{Cycles: []productui.ReviewParticipantsCycleProjection{}}, nil
		}
		return productui.ReviewParticipantsProjection{}, fmt.Errorf("application: resolve review-participant worker: %w", err)
	}
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return productui.ReviewParticipantsProjection{Cycles: []productui.ReviewParticipantsCycleProjection{}}, nil
	}
	graphTenant, err := s.ResolveGraphTenant(principal.Tenant())
	if err != nil || graphTenant == nil || strings.TrimSpace(graphTenant.String()) == "" {
		if err == nil {
			err = errors.New("graph tenant mapping is empty")
		}
		return productui.ReviewParticipantsProjection{}, fmt.Errorf("application: resolve review-participant graph tenant: %w", err)
	}
	graphs, err := s.Graphs.ListOpenParticipantReviewerGraphsForMember(ctx, graphTenant, workerID)
	if err != nil {
		return productui.ReviewParticipantsProjection{}, fmt.Errorf("application: read frozen review-participant graphs: %w", err)
	}
	projection := productui.ReviewParticipantsProjection{Cycles: make([]productui.ReviewParticipantsCycleProjection, 0, len(graphs))}
	for _, graph := range graphs {
		if err := graph.Validate(); err != nil {
			return productui.ReviewParticipantsProjection{}, fmt.Errorf("application: validate frozen review-participant graph: %w", err)
		}
		cycle := productui.ReviewParticipantsCycleProjection{
			CycleID: graph.CycleID, CycleRevision: graph.CycleRevision,
			GraphRevision: graph.GraphRevision, GraphDigest: graph.Digest,
			Assignments: make([]productui.ReviewParticipantAssignmentProjection, 0),
		}
		for _, assignment := range graph.Reviewers {
			if assignment.ParticipantID != workerID && assignment.ReviewerID != workerID {
				continue
			}
			cycle.Assignments = append(cycle.Assignments, productui.ReviewParticipantAssignmentProjection{
				ParticipantID: assignment.ParticipantID,
				ReviewerID:    assignment.ReviewerID,
				Relationship:  string(assignment.Relationship),
			})
		}
		if len(cycle.Assignments) > 0 {
			projection.Cycles = append(projection.Cycles, cycle)
		}
	}
	return projection, nil
}
