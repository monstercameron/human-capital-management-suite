package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/positionfacts"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// PositionReadDirectory is the repository read needed by the occupancy view.
// The production implementation is positionfacts.Reader, which scopes both
// position and occupancy rows to the tenant in one transaction.
type PositionReadDirectory interface {
	Directory(context.Context, values.TenantId, position.AsOf) ([]positionfacts.DirectoryRow, error)
}

// PositionReadAuthorizer decides whether an authenticated viewer may see one
// resolved position on one product page. Implementations must derive all
// policy inputs from trusted server state; the viewer and page are supplied by
// the admitted transport, never by the request body.
type PositionReadAuthorizer func(context.Context, *trust.Principal, string, position.PositionRevision) bool

// PositionPageViewerAuthorizer checks a page-level view grant before a
// position reference or directory result is resolved. This prevents an empty
// directory from turning a denied page into a successful empty response.
type PositionPageViewerAuthorizer func(context.Context, *trust.Principal, string) bool

// PositionOwnUnitResolver maps an admitted principal to the subject's actual
// assignment org-unit code. OrganizationScopeID selects policy storage; it is
// not necessarily the same vocabulary as positionfacts.OrgUnit.
type PositionOwnUnitResolver func(context.Context, *trust.Principal) (string, error)

// PositionPageAuthorizer binds a read to both the durable page grant and the
// viewer's resolved organization-visibility policy. The principal is the one
// supplied by the trusted transport interceptor.
func PositionPageAuthorizer(store roleaccess.Store, resolveOwnUnit PositionOwnUnitResolver) PositionReadAuthorizer {
	return func(ctx context.Context, principal *trust.Principal, page string, revision position.PositionRevision) bool {
		if store == nil || principal == nil || principal.Subject() == "" ||
			(revision.Position.Tenant != principal.Tenant()) ||
			(page != string(productui.PagePositionObject) && page != string(productui.PagePositionOccupancy)) {
			return false
		}
		snapshot, err := store.Load(ctx, principal.Tenant(), principal.OrganizationScopeID())
		if err != nil {
			return false
		}
		roles := roleaccess.AssignedRoles(snapshot, principal.Subject(), principal.Roles())
		permissions := roleaccess.EffectivePagePermissions(snapshot, roles)
		if !roleaccess.CanPageAction(permissions, page, roleaccess.ActionView) {
			return false
		}
		policies := roleaccess.PoliciesForRoles(snapshot, roles)
		ownUnit := ""
		for _, policy := range policies {
			if strings.EqualFold(strings.TrimSpace(policy.Mode), roleaccess.VisibilityOwnUnit) {
				if resolveOwnUnit == nil {
					return false
				}
				var resolveErr error
				ownUnit, resolveErr = resolveOwnUnit(ctx, principal)
				if resolveErr != nil || strings.TrimSpace(ownUnit) == "" {
					return false
				}
				break
			}
		}
		visibility := roleaccess.NewVisibilityEvaluator(policies, ownUnit)
		return visibility.Allows(revision.OrgUnit, false)
	}
}

// PositionPageViewer checks the durable role grant independent of any row.
// Row-level visibility remains in PositionPageAuthorizer.
func PositionPageViewer(store roleaccess.Store) PositionPageViewerAuthorizer {
	return func(ctx context.Context, principal *trust.Principal, page string) bool {
		if store == nil || principal == nil || strings.TrimSpace(principal.Subject()) == "" ||
			(page != string(productui.PagePositionObject) && page != string(productui.PagePositionOccupancy)) {
			return false
		}
		snapshot, err := store.Load(ctx, principal.Tenant(), principal.OrganizationScopeID())
		if err != nil {
			return false
		}
		roles := roleaccess.AssignedRoles(snapshot, principal.Subject(), principal.Roles())
		return roleaccess.CanPageAction(roleaccess.EffectivePagePermissions(snapshot, roles), page, roleaccess.ActionView)
	}
}

// PositionReadService resolves the two read-only product views over the
// canonical Position domain and its tenant-scoped repository adapter.
type PositionReadService struct {
	Facts     position.PositionFacts
	Directory PositionReadDirectory
	CanView   PositionPageViewerAuthorizer
	Authorize PositionReadAuthorizer
	Now       func() time.Time
}

// NewPositionReadService wires the production positionfacts adapter to the
// read service. The serving composition supplies its tenant-key resolver and
// an authorizer built from the admitted principal and durable role policy.
func NewPositionReadService(
	db dbport.Beginner,
	tenantUUID func(values.TenantId) uuid.UUID,
	canView PositionPageViewerAuthorizer,
	authorize PositionReadAuthorizer,
	now func() time.Time,
) PositionReadService {
	reader := positionfacts.Reader{DB: db, TenantUUID: tenantUUID}
	return PositionReadService{Facts: reader, Directory: reader, CanView: canView, Authorize: authorize, Now: now}
}

// PositionOptionRead is one authorized directory row paired with a current,
// server-issued revision reference. The reference is the only value a browser
// needs to select; the labels are presentation-only.
type PositionOptionRead struct {
	Reference    string
	PositionID   string
	Title        string
	Organization string
	JobCode      string
	OrgUnit      string
}

// PositionObjectRead is the disclosure-safe current revision view returned
// by GetObject. An empty value means the reference no longer resolves.
type PositionObjectRead struct {
	PositionID string
	Revision   string
	JobCode    string
	OrgUnit    string
	Lifecycle  string
	Compatible bool
	Findings   []position.FindingCode
}

// PositionOccupancyRead contains exact capacity arithmetic from the Position
// domain and occupants read for this position at the same effective date.
type PositionOccupancyRead struct {
	PositionID     string
	CapacityFTE    string
	CapacityHeads  int64
	ConsumedFTE    string
	ConsumedHeads  int64
	AvailableFTE   string
	AvailableHeads int64
	Occupants      []PositionOccupantRead
}

// PositionOccupantRead is the opaque worker reference and allocated FTE
// disclosed by the authorized occupancy view.
type PositionOccupantRead struct {
	WorkerID string
	FTE      string
}

// GetObject decodes the selected revision reference, re-reads the current
// position, and asks the Position domain whether it is still compatible with
// that reference. Tenant and viewer must come from admitted server context.
func (s PositionReadService) GetObject(ctx context.Context, principal *trust.Principal, refText string) (PositionObjectRead, error) {
	ref, tenant, positionRef, revision, asOf, err := s.resolveRequest(ctx, principal, string(productui.PagePositionObject), refText)
	if err != nil {
		return PositionObjectRead{}, err
	}
	var current position.PositionRevision
	result, err := position.CheckCompatibility(ctx, s.Facts, position.CompatibilityRequest{
		Tenant: tenant, Position: positionRef, AsOf: asOf, MinRevision: revision,
		Authorize: func(rev position.PositionRevision) bool {
			if !s.Authorize(ctx, principal, "position-object", rev) {
				return false
			}
			current = rev
			return true
		},
	})
	if err != nil {
		return PositionObjectRead{}, err
	}
	if !result.Exists {
		return PositionObjectRead{}, nil
	}
	if current.Position != positionRef {
		return PositionObjectRead{}, fmt.Errorf("application: position read %s returned a different subject", ref)
	}
	out := PositionObjectRead{
		PositionID: positionRef.Id, Revision: result.Revision.String(), JobCode: current.JobCode,
		OrgUnit: current.OrgUnit, Lifecycle: current.Lifecycle.String(), Compatible: result.Compatible,
		Findings: make([]position.FindingCode, 0, len(result.Findings)),
	}
	for _, finding := range result.Findings {
		out.Findings = append(out.Findings, finding.Code)
	}
	return out, nil
}

// GetOccupancy reads the selected position's own occupants from the
// tenant-scoped directory adapter, then lets CalculateCapacity compute the
// exact consumed and available capacity. The domain applies authorization to
// its fresh revision read before any occupants are returned.
func (s PositionReadService) GetOccupancy(ctx context.Context, principal *trust.Principal, refText string) (PositionOccupancyRead, error) {
	_, tenant, positionRef, revision, asOf, err := s.resolveRequest(ctx, principal, string(productui.PagePositionOccupancy), refText)
	if err != nil {
		return PositionOccupancyRead{}, err
	}
	// Authorize the selected subject before reading its occupancy set. The
	// later capacity calculation repeats the gate against its own fresh read.
	if _, err := position.CheckCompatibility(ctx, s.Facts, position.CompatibilityRequest{
		Tenant: tenant, Position: positionRef, AsOf: asOf, MinRevision: revision,
		Authorize: func(rev position.PositionRevision) bool {
			return s.Authorize(ctx, principal, "position-occupancy", rev)
		},
	}); err != nil {
		return PositionOccupancyRead{}, err
	}
	if s.Directory == nil {
		return PositionOccupancyRead{}, errors.New("application: position directory is unavailable")
	}
	rows, err := s.Directory.Directory(ctx, tenant, asOf)
	if err != nil {
		return PositionOccupancyRead{}, fmt.Errorf("application: read position directory: %w", err)
	}
	var occupants []position.Occupant
	found := false
	for _, row := range rows {
		if row.Position != positionRef {
			continue
		}
		found = true
		occupants = row.Occupants
		break
	}
	if !found {
		return PositionOccupancyRead{}, nil
	}
	result, err := position.CalculateCapacity(ctx, s.Facts, position.CapacityRequest{
		Tenant: tenant, Position: positionRef, AsOf: asOf, Occupants: occupants,
		Authorize: func(rev position.PositionRevision) bool {
			return s.Authorize(ctx, principal, "position-occupancy", rev)
		},
	})
	if err != nil {
		return PositionOccupancyRead{}, err
	}
	if !result.Exists {
		return PositionOccupancyRead{}, nil
	}
	out := PositionOccupancyRead{
		PositionID: positionRef.Id, CapacityFTE: result.CapacityFTE.String(), CapacityHeads: result.CapacityHeads,
		ConsumedFTE: result.ConsumedFTE.String(), ConsumedHeads: result.ConsumedHeads,
		AvailableFTE: result.AvailableFTE.String(), AvailableHeads: result.AvailableHeads,
		Occupants: make([]PositionOccupantRead, 0, len(occupants)),
	}
	for _, occupant := range occupants {
		out.Occupants = append(out.Occupants, PositionOccupantRead{WorkerID: occupant.Worker.Id, FTE: occupant.FTE.String()})
	}
	return out, nil
}

// ListOptions returns only directory rows whose current position revision is
// visible to this viewer for the page making the request. Labels come from
// the tenant-scoped directory adapter; each opaque reference is minted from
// the corresponding fresh authorized Position domain answer.
func (s PositionReadService) ListOptions(ctx context.Context, principal *trust.Principal, page string) ([]PositionOptionRead, error) {
	if s.Facts == nil || s.Directory == nil || s.Authorize == nil || s.CanView == nil || principal == nil || strings.TrimSpace(principal.Subject()) == "" {
		return nil, position.ErrUnauthorized
	}
	if !s.CanView(ctx, principal, page) {
		return nil, position.ErrUnauthorized
	}
	tenant, asOf, err := s.readCoordinate(principal)
	if err != nil {
		return nil, err
	}
	rows, err := s.Directory.Directory(ctx, tenant, asOf)
	if err != nil {
		return nil, fmt.Errorf("application: read position directory: %w", err)
	}
	options := make([]PositionOptionRead, 0, len(rows))
	for _, row := range rows {
		if row.Position.Tenant != tenant || row.Position.Kind != position.KindPosition {
			continue
		}
		var current position.PositionRevision
		result, err := position.CheckCompatibility(ctx, s.Facts, position.CompatibilityRequest{
			Tenant: tenant, Position: row.Position, AsOf: asOf,
			Authorize: func(revision position.PositionRevision) bool {
				if !s.Authorize(ctx, principal, page, revision) {
					return false
				}
				current = revision
				return true
			},
		})
		if errors.Is(err, position.ErrUnauthorized) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("application: resolve position directory row: %w", err)
		}
		if !result.Exists || current.Position != row.Position {
			continue
		}
		ref, err := position.EncodeRevisionRef(row.Position, result.Revision)
		if err != nil {
			return nil, fmt.Errorf("application: encode position selector reference: %w", err)
		}
		options = append(options, PositionOptionRead{
			Reference: ref.String(), PositionID: row.Position.Id, Title: row.Title,
			Organization: row.Organization, JobCode: current.JobCode, OrgUnit: current.OrgUnit,
		})
	}
	return options, nil
}

func (s PositionReadService) resolveRequest(ctx context.Context, principal *trust.Principal, page, refText string) (position.RevisionRef, values.TenantId, values.EntityRef, values.RevisionToken, position.AsOf, error) {
	if s.Facts == nil || s.Authorize == nil || s.CanView == nil || principal == nil || strings.TrimSpace(principal.Subject()) == "" {
		return "", "", values.EntityRef{}, values.RevisionToken{}, position.AsOf{}, position.ErrUnauthorized
	}
	if !s.CanView(ctx, principal, page) {
		return "", "", values.EntityRef{}, values.RevisionToken{}, position.AsOf{}, position.ErrUnauthorized
	}
	tenant := principal.Tenant()
	ref := position.RevisionRef(strings.TrimSpace(refText))
	positionRef, revision, err := ref.Decode()
	if err != nil {
		return "", "", values.EntityRef{}, values.RevisionToken{}, position.AsOf{}, err
	}
	if err := tenant.Validate(); err != nil || positionRef.Tenant != tenant {
		return "", "", values.EntityRef{}, values.RevisionToken{}, position.AsOf{}, position.ErrUnauthorized
	}
	coordinateTenant, asOf, err := s.readCoordinate(principal)
	if err != nil {
		return "", "", values.EntityRef{}, values.RevisionToken{}, position.AsOf{}, err
	}
	if coordinateTenant != tenant {
		return "", "", values.EntityRef{}, values.RevisionToken{}, position.AsOf{}, position.ErrUnauthorized
	}
	return ref, tenant, positionRef, revision, asOf, nil
}

func (s PositionReadService) readCoordinate(principal *trust.Principal) (values.TenantId, position.AsOf, error) {
	if principal == nil || strings.TrimSpace(principal.Subject()) == "" {
		return "", position.AsOf{}, position.ErrUnauthorized
	}
	tenant := principal.Tenant()
	if err := tenant.Validate(); err != nil {
		return "", position.AsOf{}, position.ErrUnauthorized
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	date, err := values.NewLocalDate(now.Year(), now.Month(), now.Day())
	if err != nil {
		return "", position.AsOf{}, err
	}
	knownAt, err := values.NewKnownAt(values.NewInstant(now))
	if err != nil {
		return "", position.AsOf{}, err
	}
	return tenant, position.AsOf{EffectiveOn: date, KnownAt: knownAt}, nil
}
