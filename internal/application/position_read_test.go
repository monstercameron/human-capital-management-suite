package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/positionfacts"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type positionReadFacts map[string]position.PositionRevision

func (f positionReadFacts) PositionRevisionAt(_ context.Context, q position.PositionQuery) (position.PositionRevision, bool, error) {
	revision, ok := f[q.Position.Id]
	return revision, ok, nil
}

type positionReadDirectory []positionfacts.DirectoryRow

func (d positionReadDirectory) Directory(_ context.Context, tenant values.TenantId, _ position.AsOf) ([]positionfacts.DirectoryRow, error) {
	rows := make([]positionfacts.DirectoryRow, 0, len(d))
	for _, row := range d {
		if row.Position.Tenant == tenant {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

type positionReadRoleStore struct {
	snapshot     roleaccess.Snapshot
	loadedTenant values.TenantId
	loadedScope  string
}

func (s *positionReadRoleStore) Bootstrap(context.Context, values.TenantId, string) error { return nil }
func (s *positionReadRoleStore) Load(_ context.Context, tenant values.TenantId, scope string) (roleaccess.Snapshot, error) {
	s.loadedTenant, s.loadedScope = tenant, scope
	return s.snapshot, nil
}
func (*positionReadRoleStore) SaveRole(context.Context, values.TenantId, string, roleaccess.Role) (roleaccess.Role, error) {
	return roleaccess.Role{}, nil
}
func (*positionReadRoleStore) SaveAssignment(context.Context, values.TenantId, string, roleaccess.Assignment) (roleaccess.Assignment, error) {
	return roleaccess.Assignment{}, nil
}
func (*positionReadRoleStore) SaveVisibility(context.Context, values.TenantId, string, string, roleaccess.VisibilityPolicy) (roleaccess.VisibilityPolicy, error) {
	return roleaccess.VisibilityPolicy{}, nil
}
func (*positionReadRoleStore) SavePagePermission(context.Context, values.TenantId, string, roleaccess.PagePermission) (roleaccess.PagePermission, error) {
	return roleaccess.PagePermission{}, nil
}
func (*positionReadRoleStore) SaveFeaturePermission(context.Context, values.TenantId, string, roleaccess.FeaturePermission) (roleaccess.FeaturePermission, error) {
	return roleaccess.FeaturePermission{}, nil
}

func TestPositionPageAuthorizerResolvesActualViewerUnit(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "tenant-a", Subject: "viewer-1", SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID: "role-snapshot-scope", Roles: []string{"manager"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session-viewer", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "credential-viewer",
	})
	if err != nil {
		t.Fatal(err)
	}
	store := &positionReadRoleStore{snapshot: roleaccess.Snapshot{
		Roles:           []roleaccess.Role{{ID: "manager", Active: true}},
		Assignments:     []roleaccess.Assignment{{WorkerRef: "viewer-1", RoleIDs: []string{"manager"}}},
		Policies:        []roleaccess.VisibilityPolicy{{RoleID: "manager", Mode: roleaccess.VisibilityOwnUnit}},
		PagePermissions: []roleaccess.PagePermission{{RoleID: "manager", PageID: string(productui.PagePositionObject), View: true}},
	}}
	revision := position.PositionRevision{Position: values.EntityRef{Tenant: "tenant-a", Kind: position.KindPosition, Id: "position-1"}, OrgUnit: "ENGINEERING"}
	resolved := false
	authorizer := PositionPageAuthorizer(store, func(_ context.Context, got *trust.Principal) (string, error) {
		resolved = got == principal
		return "engineering", nil
	})
	if !authorizer(ctx, principal, string(productui.PagePositionObject), revision) {
		t.Fatal("authorized viewer was denied when actual assignment unit matched the position")
	}
	if !resolved || store.loadedTenant != principal.Tenant() || store.loadedScope != "role-snapshot-scope" {
		t.Fatalf("resolver/store identity mismatch: resolved=%v tenant=%q scope=%q", resolved, store.loadedTenant, store.loadedScope)
	}
	deny := PositionPageAuthorizer(store, func(context.Context, *trust.Principal) (string, error) { return "", nil })
	if deny(ctx, principal, string(productui.PagePositionObject), revision) {
		t.Fatal("viewer without a resolvable actual assignment unit must be denied")
	}
}

func TestTodo_REV_076_03_PositionReadAuthorizationAndEmpty(t *testing.T) {
	ctx := context.Background()
	tenant := values.TenantId("tenant-a")
	positionID := "11111111-1111-4111-8111-111111111111"
	positionRef := values.EntityRef{Tenant: tenant, Kind: position.KindPosition, Id: positionID}
	start, _ := values.NewLocalDate(2026, time.January, 1)
	end, _ := values.NewLocalDate(2026, time.December, 31)
	interval, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "position-test", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	revision, err := values.NewSequenceRevision("position.revision."+positionID, 10)
	if err != nil {
		t.Fatal(err)
	}
	fte, err := values.NewDecimal("2.0000", 4, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	positionRevision := position.PositionRevision{
		Position: positionRef, Revision: revision, Effective: interval, Lifecycle: position.LifecycleOpen,
		JobCode: "ENG-01", OrgUnit: "eng-platform", LegalEntity: "Example Inc.",
		Capacity:  position.CapacityPolicy{CapacityFTE: fte, CapacityHeads: 2},
		Authority: evidence.SourceAuthority{Kind: evidence.AuthorityLocal, System: "test", PolicyRef: "test/v1"},
	}
	positionRevision.Provenance = evidence.Provenance{Source: "test", EvidenceRef: "evidence-1"}
	positionRevision.Provenance.RecordedAt, err = values.NewRecordedAt(values.NewInstant(time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	ref, err := position.EncodeRevisionRef(positionRef, revision)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: tenant, Subject: "viewer-1", SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID: "eng-platform", AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceHigh, SessionRef: "session-1", IssuedAt: time.Now().Add(-time.Hour),
		ExpiresAt: time.Now().Add(time.Hour), CredentialDigest: "credential-1",
	})
	if err != nil {
		t.Fatal(err)
	}

	authorized := true
	service := PositionReadService{
		Facts:     positionReadFacts{positionID: positionRevision},
		Directory: positionReadDirectory{{Position: positionRef}},
		CanView: func(_ context.Context, got *trust.Principal, page string) bool {
			return got == principal && (page == string(productui.PagePositionObject) || page == string(productui.PagePositionOccupancy))
		},
		Authorize: func(_ context.Context, got *trust.Principal, page string, rev position.PositionRevision) bool {
			return authorized && got == principal && page == "position-object" && rev.Position == positionRef
		},
		Now: func() time.Time { return time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC) },
	}

	t.Run("authorized object and capacity", func(t *testing.T) {
		object, err := service.GetObject(ctx, principal, ref.String())
		if err != nil || object.PositionID != positionID || object.JobCode != "ENG-01" || !object.Compatible {
			t.Fatalf("GetObject = %+v, %v", object, err)
		}
		service.Authorize = func(_ context.Context, got *trust.Principal, page string, _ position.PositionRevision) bool {
			return got == principal && page == "position-occupancy"
		}
		occupancy, err := service.GetOccupancy(ctx, principal, ref.String())
		if err != nil || occupancy.PositionID != positionID || occupancy.CapacityFTE != "2.0000" || occupancy.AvailableFTE != "2.0000" || occupancy.AvailableHeads != 2 {
			t.Fatalf("GetOccupancy = %+v, %v", occupancy, err)
		}
	})
	t.Run("selector returns only authorized current references", func(t *testing.T) {
		service.Authorize = func(_ context.Context, got *trust.Principal, page string, rev position.PositionRevision) bool {
			return got == principal && page == string(productui.PagePositionObject) && rev.Position == positionRef
		}
		options, err := service.ListOptions(ctx, principal, string(productui.PagePositionObject))
		if err != nil || len(options) != 1 || options[0].PositionID != positionID || options[0].Reference == "" {
			t.Fatalf("ListOptions = %+v, %v", options, err)
		}
		selectedPosition, selectedRevision, err := position.RevisionRef(options[0].Reference).Decode()
		if err != nil || selectedPosition != positionRef || selectedRevision != revision {
			t.Fatalf("selector reference did not bind the current revision: position=%+v revision=%+v err=%v", selectedPosition, selectedRevision, err)
		}
		service.Authorize = func(context.Context, *trust.Principal, string, position.PositionRevision) bool { return false }
		options, err = service.ListOptions(ctx, principal, string(productui.PagePositionObject))
		if err != nil || len(options) != 0 {
			t.Fatalf("unauthorized selector rows = %+v, %v; want empty", options, err)
		}
	})
	t.Run("cross tenant reference is denied before any read", func(t *testing.T) {
		other := values.EntityRef{Tenant: "tenant-b", Kind: position.KindPosition, Id: positionID}
		crossTenant, err := position.EncodeRevisionRef(other, revision)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.GetObject(ctx, principal, crossTenant.String()); !errors.Is(err, position.ErrUnauthorized) {
			t.Fatalf("cross-tenant GetObject error = %v, want ErrUnauthorized", err)
		}
	})
	t.Run("unauthorized viewer is denied", func(t *testing.T) {
		service.Authorize = func(context.Context, *trust.Principal, string, position.PositionRevision) bool { return false }
		if _, err := service.GetObject(ctx, principal, ref.String()); !errors.Is(err, position.ErrUnauthorized) {
			t.Fatalf("unauthorized GetObject error = %v, want ErrUnauthorized", err)
		}
	})
	t.Run("empty reference result is empty", func(t *testing.T) {
		service.Authorize = func(context.Context, *trust.Principal, string, position.PositionRevision) bool { return true }
		service.Facts = positionReadFacts{}
		object, err := service.GetObject(ctx, principal, ref.String())
		if err != nil || object.PositionID != "" {
			t.Fatalf("missing position = %+v, %v", object, err)
		}
	})
}
